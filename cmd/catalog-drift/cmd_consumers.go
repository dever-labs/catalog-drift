package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dever-labs/catalog-drift/internal/consumer"
	"github.com/dever-labs/catalog-drift/internal/diff"
	"github.com/dever-labs/catalog-drift/internal/reporter"
)

func runConsumers(args []string) error {
	fs := flag.NewFlagSet("consumers", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	backstageURL := fs.String("backstage-url", "", "Backstage instance URL (required)")
	component := fs.String("component", "", "Component name in Backstage (required)")
	namespace := fs.String("namespace", "default", "Backstage namespace")
	token := fs.String("token", "", "Backstage Bearer token (env: BACKSTAGE_TOKEN)")
	gateway := fs.String("gateway", "file", "Consumer source: prometheus | file")
	prometheusURL := fs.String("prometheus-url", "", "Prometheus base URL (for --gateway prometheus)")
	promMetric := fs.String("prometheus-metric", "http_requests_total", "Prometheus counter metric name")
	lookback := fs.String("lookback", "30d", "Lookback window for Prometheus queries (e.g. 30d, 7d)")
	logsFile := fs.String("logs-file", "", "Path to gateway access log file (for --gateway file)")
	logsFormat := fs.String("logs-format", "nginx", "Log format: nginx, envoy, kong, prometheus")
	format := fs.String("format", "text", "Output format: text, json, junit")

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

	switch *gateway {
	case "prometheus":
		if *prometheusURL == "" {
			return fmt.Errorf("--prometheus-url is required when --gateway prometheus")
		}
	case "file":
		if *logsFile == "" {
			return fmt.Errorf("--logs-file is required when --gateway file")
		}
	default:
		return fmt.Errorf("unknown --gateway %q: must be prometheus or file", *gateway)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := newBackstageClient(*backstageURL, *token, os.Getenv("BACKSTAGE_TOKEN"))
	contracts, err := fetchContracts(ctx, client, *component, *namespace)
	if err != nil {
		return err
	}

	// Collect deprecated paths.
	deprecatedSet := make(map[string]struct{})
	pathToContract := make(map[string]string)
	var deprecatedEndpoints []string
	for _, c := range contracts {
		if !c.Deprecation.IsDeprecated || c.APISpec.Definition == "" {
			continue
		}
		endpoints, err := diff.ExtractEndpoints(c.APISpec.Type, c.APISpec.Definition)
		if err != nil {
			return err
		}
		for _, ep := range endpoints {
			deprecatedSet[ep.Path] = struct{}{}
			pathToContract[ep.Path] = c.Entity.Metadata.Name
			deprecatedEndpoints = append(deprecatedEndpoints, ep.Path)
		}
	}

	if len(deprecatedEndpoints) == 0 {
		fmt.Fprintln(os.Stderr, "No deprecated endpoints found for this component.")
		return nil
	}

	var records []consumer.Record

	switch *gateway {
	case "prometheus":
		lookbackDur, err := parseDuration(*lookback)
		if err != nil {
			return fmt.Errorf("--lookback: %w", err)
		}
		src := consumer.NewPrometheusSource(*prometheusURL, *promMetric)
		records, err = src.Query(ctx, deprecatedEndpoints, lookbackDur)
		if err != nil {
			return fmt.Errorf("query prometheus: %w", err)
		}

	case "file":
		logFmt, err := consumer.ParseLogFormat(*logsFormat)
		if err != nil {
			return err
		}
		records, err = consumer.New(logFmt).IngestFile(*logsFile, deprecatedSet)
		if err != nil {
			return fmt.Errorf("ingest logs: %w", err)
		}

	default:
		return fmt.Errorf("unknown --gateway %q: must be prometheus or file", *gateway)
	}

	var findings []reporter.Finding
	for _, r := range records {
		contractName := pathToContract[r.Path]
		if contractName == "" {
			contractName = r.Path
		}
		findings = append(findings, reporter.Finding{
			Kind:     "consumer",
			APIName:  contractName,
			Severity: "warning",
			Message: fmt.Sprintf("client %q made %d request(s) to deprecated %s %s (last seen: %s)",
				r.ClientID, r.RequestCount, r.Method, r.Path,
				r.LastSeen.Format("2006-01-02")),
			Detail: r.Path,
		})
	}

	if err := reporter.Write(findings, *component, outFormat, os.Stdout); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
