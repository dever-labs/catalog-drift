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
	"github.com/dever-labs/catalog-drift/internal/scanner"
)

// runBreaking fetches the currently-registered spec from Backstage and diffs it
// against a proposed spec file (e.g. the version in a PR). Fails if the proposed
// change would break existing consumers — removed endpoints, incompatible schema
// changes, new required fields, etc.
//
// Use `catalog-drift check` to detect implementation drift (code vs spec).
// Use `catalog-drift breaking` on PRs to gate spec changes.
func runBreaking(args []string) error {
	fs := flag.NewFlagSet("breaking", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
	component    := fs.String("component", "", "Component name in Backstage (required)")
	namespace    := fs.String("namespace", "default", "Backstage namespace")
	token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
	specPath     := fs.String("spec", "", "Path to the proposed spec file (required)")
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
	if *component == "" {
		return fmt.Errorf("--component is required")
	}
	// Treat empty string (e.g. from action.yml passing --spec=) as not provided.
	if *specPath == "" {
		return fmt.Errorf("--spec is required: provide the path to your proposed spec file")
	}

	outFormat, err := reporter.ParseFormat(*format)
	if err != nil {
		return err
	}

	absSpec, err := filepath.Abs(*specPath)
	if err != nil {
		return fmt.Errorf("resolve spec path: %w", err)
	}
	specContent, err := os.ReadFile(absSpec)
	if err != nil {
		return fmt.Errorf("read spec file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
	contracts, err := fetchContracts(ctx, client, *component, *namespace)
	if err != nil {
		return err
	}

	engine := diff.New()
	var findings []reporter.Finding

	for _, contract := range contracts {
		if contract.APISpec.Definition == "" {
			continue
		}
		apiName := contract.Entity.Metadata.Name
		apiType := contract.APISpec.Type

		localSpec := scanner.SpecFile{
			Path:    absSpec,
			Type:    scanner.Type(apiType),
			Content: specContent,
		}
		vs, err := engine.DiffBreaking(apiType, contract.APISpec.Definition, localSpec)
		if err != nil {
			continue // unsupported type — skip
		}
		for _, v := range vs {
			findings = append(findings, reporter.Finding{
				Kind:     "breaking",
				APIName:  apiName,
				Severity: string(v.Severity),
				Message:  v.Message,
				Detail:   v.Path,
			})
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
