package main

import (
"context"
"flag"
"fmt"
"os"
"path/filepath"
"time"

"github.com/dever-labs/catalog-drift/internal/backstage"
"github.com/dever-labs/catalog-drift/internal/diff"
"github.com/dever-labs/catalog-drift/internal/reporter"
"github.com/dever-labs/catalog-drift/internal/usage"
)

// runDeprecated runs three checks:
//
//  1. Deprecated usage — code calls an endpoint marked deprecated in the catalog.
//     Severity escalates from warning to error after --error-after.
//
//  2. Removed API — a declared consumesApis entry no longer exists in the catalog.
//     Always an error: you depend on something that is gone.
//
//  3. Undeclared consumption — code calls an endpoint from a catalog API that is
//     not declared in your consumesApis. Warning: you have a hidden dependency.
//
// Checks 2 and 3 require --component. Check 1 runs with or without it.
func runDeprecated(args []string) error {
fs := flag.NewFlagSet("deprecated", flag.ContinueOnError)
fs.SetOutput(os.Stderr)

backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
component    := fs.String("component", "", "Component name — scopes checks to declared consumesApis")
namespace    := fs.String("namespace", "default", "Backstage namespace")
token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
source       := fs.String("source", ".", "Source directory to scan")
format       := fs.String("format", "text", "Output format: text, json, junit")
errorAfter   := fs.String("error-after", "", "Grace period before deprecated usage becomes an error (e.g. 90d)")
failOnWarn   := fs.Bool("fail-on-warn", false, "Exit 1 on warnings as well as errors")

if err := fs.Parse(args); err != nil {
if err == flag.ErrHelp {
return nil
}
return err
}
if *backstageURL == "" {
return fmt.Errorf("--backstage-url is required")
}

outFormat, err := reporter.ParseFormat(*format)
if err != nil {
return err
}

var gracePeriod time.Duration
if *errorAfter != "" {
gracePeriod, err = parseDuration(*errorAfter)
if err != nil {
return fmt.Errorf("--error-after: %w", err)
}
}

absSource, err := filepath.Abs(*source)
if err != nil {
return fmt.Errorf("resolve source path: %w", err)
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))

var findings []reporter.Finding

// ── Check 2: Removed APIs ─────────────────────────────────────────────────
// Any API declared in consumesApis that no longer exists in the catalog.
// This is always an error — you have a hard dependency on something gone.
if *component != "" {
statuses, err := client.FetchConsumedAPIStatuses(ctx, *component, *namespace)
if err != nil {
return fmt.Errorf("fetch consumed API statuses: %w", err)
}
for _, s := range statuses {
if s.Removed {
findings = append(findings, reporter.Finding{
Kind:     "removed-api",
APIName:  s.Name,
Severity: "error",
Message: fmt.Sprintf(
"API %q is declared in consumesApis but no longer exists in the catalog — remove the dependency or contact the owning team",
s.Name,
),
})
}
}
}

// ── Check 1: Deprecated usage ─────────────────────────────────────────────
// Code calls an endpoint the catalog has marked deprecated.
// Severity is warning by default, escalating to error when:
//   - the API's sunset date has passed, OR
//   - --error-after is set and time since DeprecatedSince exceeds the grace period.
var deprecatedPaths map[string]string                    // path → contract name
depInfoByAPI := make(map[string]backstage.DeprecationInfo) // contract name → deprecation metadata

if *component != "" {
// Scoped: only APIs this component declared it consumes.
statuses, err := client.FetchConsumedAPIStatuses(ctx, *component, *namespace)
if err != nil {
return fmt.Errorf("fetch consumed API statuses: %w", err)
}
deprecatedPaths = make(map[string]string)
for _, s := range statuses {
if s.Removed || s.Contract == nil || !s.Contract.Deprecation.IsDeprecated {
continue
}
endpoints, err := diff.ExtractEndpoints(s.Contract.APISpec.Type, s.Contract.APISpec.Definition)
if err != nil {
continue
}
depInfoByAPI[s.Name] = s.Contract.Deprecation
for _, ep := range endpoints {
deprecatedPaths[ep.Path] = s.Name
}
}
} else {
// Catalog-wide: all deprecated APIs.
all, err := client.FetchDeprecatedContracts(ctx)
if err != nil {
return fmt.Errorf("fetch deprecated contracts: %w", err)
}
deprecatedPaths = make(map[string]string)
for _, c := range all {
endpoints, err := diff.ExtractEndpoints(c.APISpec.Type, c.APISpec.Definition)
if err != nil {
continue
}
depInfoByAPI[c.Entity.Metadata.Name] = c.Deprecation
for _, ep := range endpoints {
deprecatedPaths[ep.Path] = c.Entity.Metadata.Name
}
}
}

if len(deprecatedPaths) > 0 {
usages, err := usage.New(absSource).Scan(deprecatedPaths)
if err != nil {
return fmt.Errorf("scan for deprecated usage: %w", err)
}
now := time.Now()
for _, u := range usages {
dep := depInfoByAPI[u.ContractName]
sev := deprecatedUsageSeverity(dep, gracePeriod, now)
msg := fmt.Sprintf("call to deprecated endpoint %q at %s:%d", u.DeprecatedPath, u.File, u.Line)
if dep.SunsetDate != nil {
msg += fmt.Sprintf(" (sunset: %s)", dep.SunsetDate.Format("2006-01-02"))
}
if dep.Message != "" {
msg += " — " + dep.Message
}
findings = append(findings, reporter.Finding{
Kind:     "deprecated-usage",
APIName:  u.ContractName,
Severity: sev,
Message:  msg,
Detail:   u.Context,
})
}
}

// ── Check 3: Undeclared consumption ───────────────────────────────────────
// Code calls an endpoint from a known catalog API that isn't in consumesApis.
// Requires --component so we know what's declared.
if *component != "" {
// Fetch all catalog APIs to build a full endpoint → API name map.
all, err := client.FetchAllContracts(ctx)
if err != nil {
return fmt.Errorf("fetch all contracts: %w", err)
}

// Build set of what the component already declared.
statuses, err := client.FetchConsumedAPIStatuses(ctx, *component, *namespace)
if err != nil {
return fmt.Errorf("fetch consumed API statuses: %w", err)
}
declared := make(map[string]bool)
for _, s := range statuses {
declared[s.Name] = true
}

// Also exclude the component's own provided APIs (not a consumption).
ownContracts, _ := fetchContracts(ctx, client, *component, *namespace)
for _, c := range ownContracts {
declared[c.Entity.Metadata.Name] = true
}

// Map: endpoint path → catalog API name, excluding already-declared ones.
undeclaredPaths := make(map[string]string)
for _, c := range all {
if declared[c.Entity.Metadata.Name] {
continue
}
endpoints, err := diff.ExtractEndpoints(c.APISpec.Type, c.APISpec.Definition)
if err != nil {
continue
}
for _, ep := range endpoints {
undeclaredPaths[ep.Path] = c.Entity.Metadata.Name
}
}

if len(undeclaredPaths) > 0 {
usages, err := usage.New(absSource).Scan(undeclaredPaths)
if err != nil {
return fmt.Errorf("scan for undeclared usage: %w", err)
}
for _, u := range usages {
findings = append(findings, reporter.Finding{
Kind:     "undeclared-consumption",
APIName:  u.ContractName,
Severity: "warning",
Message: fmt.Sprintf(
"code calls %q (from catalog API %q) but %q is not declared in consumesApis — add it to your Backstage component definition",
u.DeprecatedPath, u.ContractName, u.ContractName,
),
Detail: fmt.Sprintf("%s:%d", u.File, u.Line),
})
}
}
}

label := *component
if label == "" {
label = "catalog"
}

if err := reporter.Write(findings, label, outFormat, os.Stdout); err != nil {
return fmt.Errorf("write report: %w", err)
}

errors, warnings := countSeverities(findings)
if errors > 0 || (*failOnWarn && warnings > 0) {
os.Exit(exitViolations)
}
return nil
}

// deprecatedUsageSeverity returns the severity for a deprecated-usage finding.
//
// Rules (in priority order):
//  1. Past SunsetDate → error (the API is effectively end-of-life).
//  2. gracePeriod > 0 and DeprecatedSince is known and elapsed time >= gracePeriod → error.
//  3. Everything else → warning (not yet past the threshold, or no threshold configured).
func deprecatedUsageSeverity(dep backstage.DeprecationInfo, gracePeriod time.Duration, now time.Time) string {
if dep.SunsetDate != nil && now.After(*dep.SunsetDate) {
return "error"
}
if gracePeriod > 0 && dep.DeprecatedSince != nil && now.Sub(*dep.DeprecatedSince) >= gracePeriod {
return "error"
}
return "warning"
}
