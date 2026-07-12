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
	codescanner "github.com/dever-labs/catalog-drift/internal/scanner/code"
)

// runBreaking fetches the registered spec from Backstage (the contract consumers
// depend on) and compares it against what is actually implemented in the code.
// Any endpoint present in Backstage but missing from the code is a breaking change
// — the contract has been violated without updating the catalog.
func runBreaking(args []string) error {
	fs := flag.NewFlagSet("breaking", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
	component    := fs.String("component", "", "Component name in Backstage (required)")
	namespace    := fs.String("namespace", "default", "Backstage namespace")
	token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
	source       := fs.String("source", ".", "Source directory to scan for implemented routes (required)")
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

	outFormat, err := reporter.ParseFormat(*format)
	if err != nil {
		return err
	}

	absSource, err := filepath.Abs(*source)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Fetch what Backstage says this component provides.
	client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
	contracts, err := fetchContracts(ctx, client, *component, *namespace)
	if err != nil {
		return err
	}

	// Scan what the code actually implements.
	codeRoutes, err := codescanner.New(absSource).Scan()
	if err != nil {
		return fmt.Errorf("scan code %s: %w", absSource, err)
	}

	engine := diff.New()
	var findings []reporter.Finding

	for _, contract := range contracts {
		if contract.APISpec.Definition == "" {
			continue
		}
		apiName := contract.Entity.Metadata.Name

		vs, err := engine.DiffCodeRoutes(contract.APISpec.Definition, codeRoutes)
		if err != nil {
			return fmt.Errorf("diff %q: %w", apiName, err)
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
