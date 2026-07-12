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
	codescanner "github.com/dever-labs/catalog-drift/internal/scanner/code"
)

// runBreaking checks for breaking changes in two complementary ways:
//
//  1. Spec-based (--spec): fetches the registered spec from Backstage, diffs it
//     against a proposed spec file using oasdiff. Catches design-time regressions.
//
//  2. Code-based (--source): scans actual code routes and compares them against the
//     registered spec. Catches endpoints removed from code that weren't removed from
//     the spec — i.e. the spec file is stale but the implementation already broke the
//     contract.
//
// At least one of --spec or --source must be provided. Both can be used together.
func runBreaking(args []string) error {
	fs := flag.NewFlagSet("breaking", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
	component    := fs.String("component", "", "Component name in Backstage (required)")
	namespace    := fs.String("namespace", "default", "Backstage namespace")
	token        := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
	specPath     := fs.String("spec", "", "Path to proposed spec file (spec-based check)")
	source       := fs.String("source", "", "Source directory for code-route scanning (code-based check)")
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
	hasSpec   := *specPath != ""
	hasSource := *source != ""
	if !hasSpec && !hasSource {
		return fmt.Errorf("at least one of --spec (spec file) or --source (code directory) is required")
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

	engine := diff.New()
	var findings []reporter.Finding

	// ── Code-based check ──────────────────────────────────────────────────────
	// Scan actual code routes and report any endpoint present in the registered
	// spec that is no longer implemented — a silent breaking removal.
	if hasSource {
		absSource, err := filepath.Abs(*source)
		if err != nil {
			return fmt.Errorf("resolve source path: %w", err)
		}
		codeRoutes, err := codescanner.New(absSource).Scan()
		if err != nil {
			return fmt.Errorf("scan code %s: %w", absSource, err)
		}

		for _, contract := range contracts {
			if contract.APISpec.Type != "openapi" || contract.APISpec.Definition == "" {
				continue
			}
			apiName := contract.Entity.Metadata.Name
			vs, err := engine.DiffCodeRoutes(contract.APISpec.Definition, codeRoutes)
			if err != nil {
				return fmt.Errorf("code diff %q: %w", apiName, err)
			}
			for _, v := range vs {
				findings = append(findings, reporter.Finding{
					Kind:     "breaking",
					APIName:  apiName,
					Severity: string(v.Severity),
					Message:  fmt.Sprintf("[code] %s", v.Message),
					Detail:   v.Path,
				})
			}
		}
	}

	// ── Spec-based check ──────────────────────────────────────────────────────
	// Load the proposed spec file and run a formal breaking-change diff against
	// the currently registered spec using oasdiff.
	if hasSpec {
		absSpec, err := filepath.Abs(*specPath)
		if err != nil {
			return fmt.Errorf("resolve spec path: %w", err)
		}
		specContent, err := os.ReadFile(absSpec)
		if err != nil {
			return fmt.Errorf("read spec file: %w", err)
		}

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
					Message:  fmt.Sprintf("[spec] %s", v.Message),
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
