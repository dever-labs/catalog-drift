package reporter

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

var sampleFindings = []Finding{
	{Kind: "drift", APIName: "orders-api", Severity: "error", Message: "GET /orders is missing from local spec", Detail: "paths./orders.get"},
	{Kind: "drift", APIName: "orders-api", Severity: "warning", Message: "GET /internal/health is undeclared", Detail: "paths./internal/health.get"},
	{Kind: "deprecation", APIName: "legacy-api", Severity: "warning", Message: `API "legacy-api" is deprecated; migrate to "new-api"`, Detail: ""},
	{Kind: "deprecation", APIName: "sunset-api", Severity: "error", Message: `API "sunset-api" has passed its sunset date`, Detail: ""},
}

func renderTo(t *testing.T, findings []Finding, component string, format Format) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(findings, component, format, &buf); err != nil {
		t.Fatalf("Write(%s): %v", format, err)
	}
	return buf.String()
}

// ── Text ──────────────────────────────────────────────────────────────────────

func TestWriteText_ContainsComponentName(t *testing.T) {
	out := renderTo(t, sampleFindings, "my-service", FormatText)
	if !strings.Contains(out, "my-service") {
		t.Errorf("text output missing component name:\n%s", out)
	}
}

func TestWriteText_ContainsAPINames(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatText)
	for _, name := range []string{"orders-api", "legacy-api", "sunset-api"} {
		if !strings.Contains(out, name) {
			t.Errorf("text output missing API name %q:\n%s", name, out)
		}
	}
}

func TestWriteText_ContainsSeveritySymbols(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatText)
	if !strings.Contains(out, "✗") {
		t.Errorf("text output missing error symbol ✗:\n%s", out)
	}
	if !strings.Contains(out, "⚠") {
		t.Errorf("text output missing warning symbol ⚠:\n%s", out)
	}
}

func TestWriteText_SummaryLine(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatText)
	if !strings.Contains(out, "2 error(s)") {
		t.Errorf("text output missing error count:\n%s", out)
	}
	if !strings.Contains(out, "2 warning(s)") {
		t.Errorf("text output missing warning count:\n%s", out)
	}
}

func TestWriteText_NoFindings(t *testing.T) {
	out := renderTo(t, nil, "svc", FormatText)
	if !strings.Contains(out, "No drift") {
		t.Errorf("text output should mention no issues:\n%s", out)
	}
}

// ── JSON ──────────────────────────────────────────────────────────────────────

func TestWriteJSON_ValidJSON(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatJSON)
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
}

func TestWriteJSON_ContainsComponent(t *testing.T) {
	out := renderTo(t, sampleFindings, "my-svc", FormatJSON)
	var result map[string]any
	json.Unmarshal([]byte(out), &result)
	if result["component"] != "my-svc" {
		t.Errorf("component = %v, want my-svc", result["component"])
	}
}

func TestWriteJSON_SummaryFields(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatJSON)
	var result struct {
		Summary struct {
			Errors   int `json:"errors"`
			Warnings int `json:"warnings"`
			Total    int `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Summary.Errors != 2 {
		t.Errorf("errors = %d, want 2", result.Summary.Errors)
	}
	if result.Summary.Warnings != 2 {
		t.Errorf("warnings = %d, want 2", result.Summary.Warnings)
	}
	if result.Summary.Total != 4 {
		t.Errorf("total = %d, want 4", result.Summary.Total)
	}
}

func TestWriteJSON_NoFindings(t *testing.T) {
	out := renderTo(t, nil, "svc", FormatJSON)
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

// ── JUnit ─────────────────────────────────────────────────────────────────────

func TestWriteJUnit_ValidXML(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatJUnit)
	var suites struct {
		XMLName xml.Name `xml:"testsuites"`
	}
	if err := xml.Unmarshal([]byte(out), &suites); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, out)
	}
}

func TestWriteJUnit_FailuresForErrors(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatJUnit)
	if !strings.Contains(out, "<failure") {
		t.Errorf("JUnit output missing <failure> elements:\n%s", out)
	}
}

func TestWriteJUnit_SkippedForWarnings(t *testing.T) {
	out := renderTo(t, sampleFindings, "svc", FormatJUnit)
	if !strings.Contains(out, "<skipped") {
		t.Errorf("JUnit output missing <skipped> elements:\n%s", out)
	}
}

func TestWriteJUnit_NoFindings(t *testing.T) {
	out := renderTo(t, nil, "svc", FormatJUnit)
	if !strings.Contains(out, "testsuites") {
		t.Errorf("JUnit output missing testsuites element:\n%s", out)
	}
}

func TestParseFormat(t *testing.T) {
	cases := []struct {
		input   string
		want    Format
		wantErr bool
	}{
		{"text", FormatText, false},
		{"json", FormatJSON, false},
		{"junit", FormatJUnit, false},
		{"TEXT", FormatText, false},
		{"JSON", FormatJSON, false},
		{"JUNIT", FormatJUnit, false},
		{"xml", "", true},
		{"csv", "", true},
	}
	for _, c := range cases {
		got, err := ParseFormat(c.input)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseFormat(%q) err = %v, wantErr = %v", c.input, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
