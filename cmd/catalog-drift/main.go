package main

import (
"context"
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
  catalog-drift check       [flags]   Diff Backstage contracts against local spec files and/or code
  catalog-drift deprecated  [flags]   Scan code for calls to any deprecated API in the catalog
  catalog-drift consumers   [flags]   Discover consumers of deprecated APIs from gateway logs
  catalog-drift enforce     [flags]   Generate gateway config to return 410 for sunsetted APIs

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
