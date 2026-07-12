package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dever-labs/catalog-drift/internal/deprecation"
	"github.com/dever-labs/catalog-drift/internal/diff"
	"github.com/dever-labs/catalog-drift/internal/reporter"
	"github.com/dever-labs/catalog-drift/internal/scanner"
	codescanner "github.com/dever-labs/catalog-drift/internal/scanner/code"
)

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
	// --scan-code: scan code routes vs Backstage contract (errors on removed endpoints)
	// and vs local spec file (warns if spec is stale). This is the main CI gate.
	scanCode     := fs.Bool("scan-code", false, "Scan code routes against Backstage contract and local spec")

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

		if *scanCode && contract.APISpec.Type == "openapi" {
			// Code vs Backstage: catches endpoints removed from code without updating the catalog.
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

			// Code vs local spec: catches endpoints updated in code but not in the local spec file.
			// This is a warning only — the spec file may just be stale; Backstage is the authority.
			if match != nil {
				vs, err := engine.DiffCodeRoutes(string(match.Content), codeRoutes)
				if err == nil {
					for _, v := range vs {
						findings = append(findings, reporter.Finding{
							Kind:     "spec-drift",
							APIName:  apiName,
							Severity: "warning",
							Message:  fmt.Sprintf("[local spec out of sync] %s", v.Message),
							Detail:   v.Path,
						})
					}
				}
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
