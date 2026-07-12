package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dever-labs/catalog-drift/internal/diff"
	"github.com/dever-labs/catalog-drift/internal/reporter"
	"github.com/dever-labs/catalog-drift/internal/usage"
)

// runDeprecated scans the caller's source tree for calls to any deprecated API
// found in the Backstage catalog. If --component is provided, only APIs consumed
// by that component are checked; otherwise all deprecated APIs in the catalog
// are used as the reference set.
func runDeprecated(args []string) error {
	fs := flag.NewFlagSet("deprecated", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
	component    := fs.String("component", "", "Component name — if set, only consumed APIs are checked")
	namespace    := fs.String("namespace", "default", "Backstage namespace (used with --component)")
	token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
	source       := fs.String("source", ".", "Source directory to scan for deprecated usage")
	format       := fs.String("format", "text", "Output format: text, json, junit")
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))

	type contractEntry struct {
		name       string
		apiType    string
		definition string
	}

	var deprecatedEntries []contractEntry

	if *component != "" {
		// Only check APIs this component declares it consumes.
		consumed, err := client.FetchConsumedContracts(ctx, *component, *namespace)
		if err != nil {
			return fmt.Errorf("fetch consumed contracts: %w", err)
		}
		for _, c := range consumed {
			if c.Deprecation.IsDeprecated {
				deprecatedEntries = append(deprecatedEntries, contractEntry{
					name:       c.Entity.Metadata.Name,
					apiType:    c.APISpec.Type,
					definition: c.APISpec.Definition,
				})
			}
		}
	} else {
		// Catalog-wide: find all deprecated APIs regardless of component.
		all, err := client.FetchDeprecatedContracts(ctx)
		if err != nil {
			return fmt.Errorf("fetch deprecated contracts: %w", err)
		}
		for _, c := range all {
			deprecatedEntries = append(deprecatedEntries, contractEntry{
				name:       c.Entity.Metadata.Name,
				apiType:    c.APISpec.Type,
				definition: c.APISpec.Definition,
			})
		}
	}

	if len(deprecatedEntries) == 0 {
		fmt.Fprintln(os.Stderr, "No deprecated APIs found.")
		return nil
	}

	// Build deprecated-path → contract-name map.
	deprecatedPaths := make(map[string]string)
	for _, e := range deprecatedEntries {
		if e.definition == "" {
			continue
		}
		endpoints, err := diff.ExtractEndpoints(e.apiType, e.definition)
		if err != nil {
			return fmt.Errorf("extract endpoints from %q: %w", e.name, err)
		}
		for _, ep := range endpoints {
			deprecatedPaths[ep.Path] = e.name
		}
	}

	if len(deprecatedPaths) == 0 {
		fmt.Fprintln(os.Stderr, "No deprecated API endpoints to check (definitions may be empty).")
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

	label := *component
	if label == "" {
		label = "catalog"
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

	if err := reporter.Write(findings, label, outFormat, os.Stdout); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	errors, warnings := countSeverities(findings)
	if errors > 0 || (*failOnWarn && warnings > 0) {
		os.Exit(exitViolations)
	}
	return nil
}
