package main

import (
"context"
"flag"
"fmt"
"os"
"time"

"github.com/dever-labs/catalog-drift/internal/reporter"
)

// runConsumers fetches all deprecated APIs for a component from Backstage, then
// queries which other components in the catalog declare they consume each one.
// Backstage is the source of truth — no log scraping or Prometheus queries needed.
func runConsumers(args []string) error {
fs := flag.NewFlagSet("consumers", flag.ContinueOnError)
fs.SetOutput(os.Stderr)

backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
component    := fs.String("component", "", "Component name in Backstage (required)")
namespace    := fs.String("namespace", "default", "Backstage namespace")
token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
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

// Fetch the APIs this component provides.
contracts, err := fetchContracts(ctx, client, *component, *namespace)
if err != nil {
return err
}

var findings []reporter.Finding

for _, contract := range contracts {
if !contract.Deprecation.IsDeprecated {
continue
}
apiName := contract.Entity.Metadata.Name

// Find all components in Backstage that declare they consume this API.
consumers, err := client.FetchAPIConsumers(ctx, apiName, *namespace)
if err != nil {
return fmt.Errorf("fetch consumers of %q: %w", apiName, err)
}

if len(consumers) == 0 {
findings = append(findings, reporter.Finding{
Kind:     "consumer",
APIName:  apiName,
Severity: "info",
Message:  "no registered consumers found — safe to sunset",
})
continue
}

for _, c := range consumers {
findings = append(findings, reporter.Finding{
Kind:     "consumer",
APIName:  apiName,
Severity: "warning",
Message: fmt.Sprintf(
"component %q declares it consumes deprecated API %q — notify before sunset",
c.Metadata.Name, apiName,
),
Detail: fmt.Sprintf("%s/%s", c.Metadata.Namespace, c.Metadata.Name),
})
}
}

if len(findings) == 0 {
fmt.Fprintln(os.Stderr, "No deprecated APIs with registered consumers found.")
return nil
}

return reporter.Write(findings, *component, outFormat, os.Stdout)
}
