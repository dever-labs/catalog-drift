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
"github.com/dever-labs/catalog-drift/internal/reporter"
"github.com/dever-labs/catalog-drift/internal/scanner"
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
case "deprecated":
err = runDeprecated(args[1:])
case "usage":
// Backward-compatible alias for "deprecated".
err = runDeprecated(args[1:])
case "consumers":
err = runConsumers(args[1:])
case "enforce":
fmt.Fprintln(os.Stderr, "enforce has been removed — 410 gateway enforcement is out of scope for this tool")
os.Exit(exitToolError)
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
fmt.Fprintln(os.Stderr, `catalog-drift — API contract drift and deprecation detection

Usage:
  catalog-drift check       [flags]   Diff local spec/code against the registered Backstage contract
  catalog-drift deprecated  [flags]   Scan source code for calls to deprecated APIs
  catalog-drift consumers   [flags]   List catalog components that consume a deprecated API

Run 'catalog-drift <command> --help' for per-command flags.`)
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func newBackstageClient(backstageURL, token, envToken string) *backstage.Client {
if token == "" {
token = envToken
}
var opts []backstage.Option
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

// isFlagSet reports whether the named flag was explicitly set by the caller.
func isFlagSet(fs *flag.FlagSet, name string) bool {
found := false
fs.Visit(func(f *flag.Flag) {
if f.Name == name {
found = true
}
})
return found
}

// resolvedPolicy holds the merged result of a Backstage GovernancePolicy and
// CLI flag overrides. CLI flags always win over the catalog policy.
type resolvedPolicy struct {
GracePeriod time.Duration // 0 means no error escalation
FailOnWarn  bool
}

// resolvePolicy fetches the GovernancePolicy from Backstage (if any) and merges
// it with the CLI flags. CLI flags are represented as pointers: a non-nil pointer
// means the user explicitly set the flag, which always overrides the catalog value.
//
// component may be empty; if so, no Backstage lookup is performed and the
// returned policy contains only the CLI flag values.
func resolvePolicy(
ctx context.Context,
client *backstage.Client,
component, namespace string,
cliErrorAfter string, // "" = not set
cliFailOnWarn bool,
cliFailOnWarnSet bool, // whether --fail-on-warn was explicitly passed
) (resolvedPolicy, error) {
p := resolvedPolicy{FailOnWarn: cliFailOnWarn}

// Parse CLI error-after first (used as fallback when no catalog policy).
var cliGrace time.Duration
if cliErrorAfter != "" {
var err error
cliGrace, err = parseDuration(cliErrorAfter)
if err != nil {
return p, fmt.Errorf("--error-after: %w", err)
}
}

if component == "" {
p.GracePeriod = cliGrace
return p, nil
}

policy, err := client.FetchGovernancePolicy(ctx, component, namespace)
if err != nil {
// Non-fatal: log and proceed with CLI values only.
fmt.Fprintf(os.Stderr, "warning: could not fetch governance policy: %v\n", err)
p.GracePeriod = cliGrace
return p, nil
}

if policy != nil {
// Apply Backstage policy as the base.
if policy.Spec.Deprecation.ErrorAfter != "" {
if d, err := parseDuration(policy.Spec.Deprecation.ErrorAfter); err == nil {
	p.GracePeriod = d
}
}
if policy.Spec.Contract.FailOnWarn {
p.FailOnWarn = true
}
}

// CLI flags override catalog values.
if cliErrorAfter != "" {
p.GracePeriod = cliGrace
}
if cliFailOnWarnSet {
p.FailOnWarn = cliFailOnWarn
}

return p, nil
}
