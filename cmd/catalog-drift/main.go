package main

import (
"context"
"flag"
"fmt"
"os"
"path/filepath"
"strconv"
"strings"
"time"

"github.com/dever-labs/catalog-drift/internal/backstage"
"github.com/dever-labs/catalog-drift/internal/consumer"
"github.com/dever-labs/catalog-drift/internal/deprecation"
"github.com/dever-labs/catalog-drift/internal/diff"
"github.com/dever-labs/catalog-drift/internal/gateway"
"github.com/dever-labs/catalog-drift/internal/reporter"
"github.com/dever-labs/catalog-drift/internal/scanner"
codescanner "github.com/dever-labs/catalog-drift/internal/scanner/code"
"github.com/dever-labs/catalog-drift/internal/usage"
)

const (
exitOK         = 0
exitViolations = 1
exitToolError  = 2
)

func main() {
args := os.Args[1:]
if len(args) == 0 {
printUsage()
os.Exit(exitToolError)
}

var err error
switch args[0] {
case "check":
err = runCheck(args[1:])
case "usage":
err = runUsage(args[1:])
case "consumers":
err = runConsumers(args[1:])
case "enforce":
err = runEnforce(args[1:])
case "help", "--help", "-h":
printUsage()
default:
// Backward-compatible: treat bare flags as "check".
err = runCheck(args)
}

if err != nil {
fmt.Fprintf(os.Stderr, "error: %v\n", err)
os.Exit(exitToolError)
}
}

func printUsage() {
fmt.Fprintln(os.Stderr, `catalog-drift — API contract drift detection

Usage:
  catalog-drift check      [flags]   Diff Backstage contracts against local spec files or code
  catalog-drift usage      [flags]   Scan code for calls to deprecated API endpoints
  catalog-drift consumers  [flags]   Discover consumers of deprecated APIs from gateway logs
  catalog-drift enforce    [flags]   Generate gateway config to return 410 for sunsetted APIs

Run 'catalog-drift <command> --help' for per-command flags.`)
}

// ── Shared flag helpers ───────────────────────────────────────────────────────

func newBackstageClient(backstageURL, token, envToken string) *backstage.Client {
if token == "" {
token = envToken
}
opts := []backstage.Option{}
if token != "" {
opts = append(opts, backstage.WithToken(token))
}
return backstage.NewClient(backstageURL, opts...)
}

func fetchContracts(ctx context.Context, client *backstage.Client, component, namespace string) ([]backstage.Contract, error) {
contracts, err := client.FetchContracts(ctx, component, namespace)
if err != nil {
return nil, fmt.Errorf("fetch contracts: %w", err)
}
if len(contracts) == 0 {
fmt.Fprintf(os.Stderr, "warning: no API contracts found for component %q in namespace %q\n", component, namespace)
}
return contracts, nil
}

// ── check ─────────────────────────────────────────────────────────────────────

func runCheck(args []string) error {
fs := flag.NewFlagSet("check", flag.ContinueOnError)
fs.SetOutput(os.Stderr)

backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
component    := fs.String("component", "", "Component name in Backstage (required)")
namespace    := fs.String("namespace", "default", "Backstage namespace")
token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
source       := fs.String("source", ".", "Source directory to scan")
format       := fs.String("format", "text", "Output format: text, json, junit")
errorAfter   := fs.String("error-after", "", "Duration after deprecated-since before error (e.g. 90d)")
failOnWarn   := fs.Bool("fail-on-warn", false, "Exit 1 on warnings as well as errors")
scanCode     := fs.Bool("scan-code", false, "Also diff contract against extracted code routes (Go)")

if err := fs.Parse(args); err != nil {
if err == flag.ErrHelp {
return nil
}
return err
}
if *backstageURL == "" {
return fmt.Errorf("--backstage-url is required")
}
if *component == "" {
return fmt.Errorf("--component is required")
}

outFormat, err := reporter.ParseFormat(*format)
if err != nil {
return err
}

var depCheckerCfg deprecation.Config
if *errorAfter != "" {
d, err := parseDuration(*errorAfter)
if err != nil {
return fmt.Errorf("--error-after: %w", err)
}
depCheckerCfg.ErrorAfter = d
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
contracts, err := fetchContracts(ctx, client, *component, *namespace)
if err != nil {
return err
}

absSource, err := filepath.Abs(*source)
if err != nil {
return fmt.Errorf("resolve source path: %w", err)
}

specFiles, err := scanner.New(absSource).Scan()
if err != nil {
return fmt.Errorf("scan %s: %w", absSource, err)
}

var codeRoutes []codescanner.Route
if *scanCode {
codeRoutes, err = codescanner.New(absSource).Scan()
if err != nil {
return fmt.Errorf("scan code %s: %w", absSource, err)
}
}

engine := diff.New()
depChecker := deprecation.NewChecker(depCheckerCfg)
var findings []reporter.Finding

for _, contract := range contracts {
apiName := contract.Entity.Metadata.Name

if v := depChecker.Check(contract); v != nil {
findings = append(findings, reporter.Finding{
Kind:     "deprecation",
APIName:  apiName,
Severity: v.Severity.String(),
Message:  v.Message,
})
}

if contract.APISpec.Definition == "" {
findings = append(findings, reporter.Finding{
Kind:     "drift",
APIName:  apiName,
Severity: "warning",
Message:  "contract definition is empty in Backstage; cannot diff",
})
continue
}

// Spec-file diff.
match := matchSpec(specFiles, contract.APISpec.Type, apiName)
if match == nil {
findings = append(findings, reporter.Finding{
Kind:     "drift",
APIName:  apiName,
Severity: "warning",
Message:  fmt.Sprintf("no local %s spec file found for contract %q", contract.APISpec.Type, apiName),
})
} else {
vs, err := engine.Diff(contract.APISpec.Type, contract.APISpec.Definition, *match)
if err != nil {
return fmt.Errorf("diff %q: %w", apiName, err)
}
for _, v := range vs {
findings = append(findings, reporter.Finding{
Kind:     "drift",
APIName:  apiName,
Severity: string(v.Severity),
Message:  v.Message,
Detail:   v.Path,
})
}
}

// Code-route diff (OpenAPI only).
if *scanCode && contract.APISpec.Type == "openapi" {
vs, err := engine.DiffCodeRoutes(contract.APISpec.Definition, codeRoutes)
if err != nil {
return fmt.Errorf("code diff %q: %w", apiName, err)
}
for _, v := range vs {
findings = append(findings, reporter.Finding{
Kind:     "code-drift",
APIName:  apiName,
Severity: string(v.Severity),
Message:  v.Message,
Detail:   v.Path,
})
}
}
}

if err := reporter.Write(findings, *component, outFormat, os.Stdout); err != nil {
return fmt.Errorf("write report: %w", err)
}

errors, warnings := countSeverities(findings)
if errors > 0 || (*failOnWarn && warnings > 0) {
os.Exit(exitViolations)
}
return nil
}

// ── usage ─────────────────────────────────────────────────────────────────────

func runUsage(args []string) error {
fs := flag.NewFlagSet("usage", flag.ContinueOnError)
fs.SetOutput(os.Stderr)

backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
component    := fs.String("component", "", "Component name in Backstage (required)")
namespace    := fs.String("namespace", "default", "Backstage namespace")
token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
source       := fs.String("source", ".", "Source directory to scan for deprecated usage")
format       := fs.String("format", "text", "Output format: text, json, junit")

if err := fs.Parse(args); err != nil {
if err == flag.ErrHelp {
return nil
}
return err
}
if *backstageURL == "" {
return fmt.Errorf("--backstage-url is required")
}
if *component == "" {
return fmt.Errorf("--component is required")
}

outFormat, err := reporter.ParseFormat(*format)
if err != nil {
return err
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
contracts, err := fetchContracts(ctx, client, *component, *namespace)
if err != nil {
return err
}

// Build deprecated-path → contract-name map.
deprecatedPaths := make(map[string]string)
for _, c := range contracts {
if !c.Deprecation.IsDeprecated || c.APISpec.Definition == "" {
continue
}
endpoints, err := diff.ExtractEndpoints(c.APISpec.Type, c.APISpec.Definition)
if err != nil {
return fmt.Errorf("extract endpoints from %q: %w", c.Entity.Metadata.Name, err)
}
for _, ep := range endpoints {
deprecatedPaths[ep.Path] = c.Entity.Metadata.Name
}
}

if len(deprecatedPaths) == 0 {
fmt.Fprintln(os.Stderr, "No deprecated APIs found for this component.")
return nil
}

absSource, err := filepath.Abs(*source)
if err != nil {
return fmt.Errorf("resolve source path: %w", err)
}

usages, err := usage.New(absSource).Scan(deprecatedPaths)
if err != nil {
return fmt.Errorf("scan for deprecated usage: %w", err)
}

var findings []reporter.Finding
for _, u := range usages {
findings = append(findings, reporter.Finding{
Kind:     "deprecated-usage",
APIName:  u.ContractName,
Severity: "warning",
Message:  fmt.Sprintf("call to deprecated endpoint %q at %s:%d", u.DeprecatedPath, u.File, u.Line),
Detail:   u.Context,
})
}

if err := reporter.Write(findings, *component, outFormat, os.Stdout); err != nil {
return fmt.Errorf("write report: %w", err)
}
return nil
}

// ── consumers ────────────────────────────────────────────────────────────────

func runConsumers(args []string) error {
fs := flag.NewFlagSet("consumers", flag.ContinueOnError)
fs.SetOutput(os.Stderr)

backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
component    := fs.String("component", "", "Component name in Backstage (required)")
namespace    := fs.String("namespace", "default", "Backstage namespace")
token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
logsFile     := fs.String("logs-file", "", "Path to gateway access log file (required)")
logsFormat   := fs.String("logs-format", "nginx", "Log format: nginx, envoy, kong, prometheus")
format       := fs.String("format", "text", "Output format: text, json, junit")

if err := fs.Parse(args); err != nil {
if err == flag.ErrHelp {
return nil
}
return err
}
if *backstageURL == "" {
return fmt.Errorf("--backstage-url is required")
}
if *component == "" {
return fmt.Errorf("--component is required")
}
if *logsFile == "" {
return fmt.Errorf("--logs-file is required")
}

logFmt, err := consumer.ParseLogFormat(*logsFormat)
if err != nil {
return err
}
outFormat, err := reporter.ParseFormat(*format)
if err != nil {
return err
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
contracts, err := fetchContracts(ctx, client, *component, *namespace)
if err != nil {
return err
}

// Collect deprecated paths.
deprecatedSet := make(map[string]struct{})
pathToContract := make(map[string]string)
for _, c := range contracts {
if !c.Deprecation.IsDeprecated || c.APISpec.Definition == "" {
continue
}
endpoints, err := diff.ExtractEndpoints(c.APISpec.Type, c.APISpec.Definition)
if err != nil {
return err
}
for _, ep := range endpoints {
deprecatedSet[ep.Path] = struct{}{}
pathToContract[ep.Path] = c.Entity.Metadata.Name
}
}

records, err := consumer.New(logFmt).IngestFile(*logsFile, deprecatedSet)
if err != nil {
return fmt.Errorf("ingest logs: %w", err)
}

var findings []reporter.Finding
for _, r := range records {
contractName := pathToContract[r.Path]
if contractName == "" {
contractName = r.Path
}
findings = append(findings, reporter.Finding{
Kind:     "consumer",
APIName:  contractName,
Severity: "warning",
Message: fmt.Sprintf("client %q made %d request(s) to deprecated %s %s (last seen: %s)",
r.ClientID, r.RequestCount, r.Method, r.Path,
r.LastSeen.Format("2006-01-02")),
Detail: r.Path,
})
}

if err := reporter.Write(findings, *component, outFormat, os.Stdout); err != nil {
return fmt.Errorf("write report: %w", err)
}
return nil
}

// ── enforce ───────────────────────────────────────────────────────────────────

func runEnforce(args []string) error {
fs := flag.NewFlagSet("enforce", flag.ContinueOnError)
fs.SetOutput(os.Stderr)

backstageURL  := fs.String("backstage-url", "", "Backstage instance URL (required)")
component     := fs.String("component", "", "Component name in Backstage (required)")
namespace     := fs.String("namespace", "default", "Backstage namespace")
token         := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
gatewayFormat := fs.String("gateway", "nginx", "Gateway format: nginx, envoy, kong")
output        := fs.String("output", "-", "Output file path (- for stdout)")
onlySunset    := fs.Bool("only-past-sunset", false, "Only generate rules for APIs past their sunset date")

if err := fs.Parse(args); err != nil {
if err == flag.ErrHelp {
return nil
}
return err
}
if *backstageURL == "" {
return fmt.Errorf("--backstage-url is required")
}
if *component == "" {
return fmt.Errorf("--component is required")
}

gwFormat, err := gateway.ParseFormat(*gatewayFormat)
if err != nil {
return err
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
contracts, err := fetchContracts(ctx, client, *component, *namespace)
if err != nil {
return err
}

now := time.Now()
var rules []gateway.SunsetRule

for _, c := range contracts {
if !c.Deprecation.IsDeprecated {
continue
}
dep := c.Deprecation
if dep.SunsetDate == nil {
continue
}
if *onlySunset && !now.After(*dep.SunsetDate) {
continue
}

endpoints, err := diff.ExtractEndpoints(c.APISpec.Type, c.APISpec.Definition)
if err != nil {
return fmt.Errorf("extract endpoints for %q: %w", c.Entity.Metadata.Name, err)
}

for _, ep := range endpoints {
rules = append(rules, gateway.SunsetRule{
Path:       ep.Path,
Methods:    []string{ep.Method},
SunsetDate: *dep.SunsetDate,
Message:    dep.Message,
Successor:  dep.Successor,
})
}
}

if len(rules) == 0 {
fmt.Fprintln(os.Stderr, "No sunset rules to generate.")
return nil
}

var out *os.File
if *output == "-" {
out = os.Stdout
} else {
out, err = os.Create(*output)
if err != nil {
return fmt.Errorf("create output file: %w", err)
}
defer out.Close()
}

if err := gateway.Generate(rules, gwFormat, out); err != nil {
return fmt.Errorf("generate gateway config: %w", err)
}

if *output != "-" {
fmt.Fprintf(os.Stderr, "wrote %d rule(s) to %s\n", len(rules), *output)
}
return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func matchSpec(files []scanner.SpecFile, contractType, contractName string) *scanner.SpecFile {
ct := scanner.Type(contractType)
var fallback *scanner.SpecFile
for i, f := range files {
if f.Type != ct {
continue
}
if fallback == nil {
fallback = &files[i]
}
base := strings.ToLower(strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path)))
if strings.Contains(base, strings.ToLower(contractName)) ||
strings.Contains(strings.ToLower(contractName), base) {
return &files[i]
}
}
return fallback
}

func parseDuration(s string) (time.Duration, error) {
if strings.HasSuffix(s, "d") {
n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
if err != nil || n <= 0 {
return 0, fmt.Errorf("invalid duration %q: must be a positive integer followed by d", s)
}
return time.Duration(n) * 24 * time.Hour, nil
}
return time.ParseDuration(s)
}

func countSeverities(findings []reporter.Finding) (errors, warnings int) {
for _, f := range findings {
if f.Severity == "error" {
errors++
} else {
warnings++
}
}
return
}
