// Package reporter formats and outputs drift violations.
// Supports text, JSON, and JUnit (for CI integration).
package reporter

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Finding is a unified representation of a drift or deprecation violation.
type Finding struct {
	Kind     string // "drift" | "deprecation"
	APIName  string
	Severity string // "warning" | "error"
	Message  string
	Detail   string // path (drift) or additional context
}

// Format is an output format for the reporter.
type Format string

const (
	FormatText  Format = "text"
	FormatJSON  Format = "json"
	FormatJUnit Format = "junit"
)

// ParseFormat validates and returns a Format, or an error for unknown values.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(s)) {
	case FormatText:
		return FormatText, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatJUnit:
		return FormatJUnit, nil
	default:
		return "", fmt.Errorf("unknown format %q: must be text, json, or junit", s)
	}
}

// Write renders findings to w in the given format.
func Write(findings []Finding, component string, format Format, w io.Writer) error {
	switch format {
	case FormatText:
		return writeText(findings, component, w)
	case FormatJSON:
		return writeJSON(findings, component, w)
	case FormatJUnit:
		return writeJUnit(findings, component, w)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

// ── Text ─────────────────────────────────────────────────────────────────────

func writeText(findings []Finding, component string, w io.Writer) error {
	sep := strings.Repeat("─", 60)
	fmt.Fprintf(w, "catalog-drift — %s\n%s\n", component, sep)

	if len(findings) == 0 {
		fmt.Fprintln(w, "✓ No drift or deprecation issues found.")
		fmt.Fprintln(w, sep)
		return nil
	}

	// Group by API name.
	grouped := groupByAPI(findings)
	for _, apiName := range orderedKeys(grouped) {
		group := grouped[apiName]
		fmt.Fprintf(w, "\nAPI: %s\n", apiName)
		for _, f := range group {
			symbol := "⚠"
			if f.Severity == "error" {
				symbol = "✗"
			}
			line := fmt.Sprintf("  %s [%s] %s", symbol, f.Severity, f.Message)
			if f.Detail != "" {
				line += fmt.Sprintf(" (%s)", f.Detail)
			}
			fmt.Fprintln(w, line)
		}
	}

	errors, warnings := countSeverities(findings)
	fmt.Fprintf(w, "\n%s\n", sep)
	fmt.Fprintf(w, "%d error(s), %d warning(s)\n", errors, warnings)
	return nil
}

// ── JSON ─────────────────────────────────────────────────────────────────────

type jsonOutput struct {
	Component string          `json:"component"`
	APIs      []jsonAPIResult `json:"apis"`
	Summary   jsonSummary     `json:"summary"`
}

type jsonAPIResult struct {
	API      string    `json:"api"`
	Findings []Finding `json:"findings"`
}

type jsonSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Total    int `json:"total"`
}

func writeJSON(findings []Finding, component string, w io.Writer) error {
	grouped := groupByAPI(findings)
	var apis []jsonAPIResult
	for _, apiName := range orderedKeys(grouped) {
		apis = append(apis, jsonAPIResult{API: apiName, Findings: grouped[apiName]})
	}
	errors, warnings := countSeverities(findings)
	out := jsonOutput{
		Component: component,
		APIs:      apis,
		Summary:   jsonSummary{Errors: errors, Warnings: warnings, Total: len(findings)},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// ── JUnit ─────────────────────────────────────────────────────────────────────

type junitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	TestSuites []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	TestCases []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string         `xml:"name,attr"`
	ClassName string         `xml:"classname,attr"`
	Failure   *junitFailure  `xml:"failure,omitempty"`
	Skipped   *junitSkipped  `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

func writeJUnit(findings []Finding, component string, w io.Writer) error {
	grouped := groupByAPI(findings)
	var suites []junitTestSuite

	for _, apiName := range orderedKeys(grouped) {
		group := grouped[apiName]
		failures := 0
		var cases []junitTestCase

		for _, f := range group {
			tc := junitTestCase{
				Name:      f.Message,
				ClassName: fmt.Sprintf("%s.%s", component, f.Kind),
			}
			if f.Severity == "error" {
				failures++
				tc.Failure = &junitFailure{
					Message: f.Message,
					Type:    f.Kind,
					Text:    f.Detail,
				}
			} else {
				tc.Skipped = &junitSkipped{Message: f.Message}
			}
			cases = append(cases, tc)
		}

		suites = append(suites, junitTestSuite{
			Name:      apiName,
			Tests:     len(cases),
			Failures:  failures,
			TestCases: cases,
		})
	}

	fmt.Fprint(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(junitTestSuites{TestSuites: suites}); err != nil {
		return err
	}
	return enc.Flush()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func groupByAPI(findings []Finding) map[string][]Finding {
	m := make(map[string][]Finding)
	for _, f := range findings {
		m[f.APIName] = append(m[f.APIName], f)
	}
	return m
}

func orderedKeys(m map[string][]Finding) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Stable sort for deterministic output.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func countSeverities(findings []Finding) (errors, warnings int) {
	for _, f := range findings {
		if f.Severity == "error" {
			errors++
		} else {
			warnings++
		}
	}
	return
}
