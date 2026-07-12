package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dever-labs/catalog-drift/internal/diff"
	"github.com/dever-labs/catalog-drift/internal/gateway"
)

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
