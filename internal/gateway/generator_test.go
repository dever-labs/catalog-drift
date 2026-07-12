package gateway

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

var testRules = []SunsetRule{
	{
		Path:       "/api/v1/users",
		Methods:    []string{"GET", "POST"},
		SunsetDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Message:    "Users API v1 has been retired.",
		Successor:  "/api/v2/users",
	},
	{
		Path:       "/api/v1/orders/{id}",
		SunsetDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Successor:  "/api/v2/orders/{id}",
	},
}

func render(t *testing.T, rules []SunsetRule, format Format) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Generate(rules, format, &buf); err != nil {
		t.Fatalf("Generate(%s): %v", format, err)
	}
	return buf.String()
}

// ── Nginx ─────────────────────────────────────────────────────────────────────

func TestNginx_Contains410(t *testing.T) {
	out := render(t, testRules, FormatNginx)
	if !strings.Contains(out, "return 410") {
		t.Errorf("nginx output missing 'return 410':\n%s", out)
	}
}

func TestNginx_ContainsBothPaths(t *testing.T) {
	out := render(t, testRules, FormatNginx)
	for _, path := range []string{"/api/v1/users", "/api/v1/orders/{id}"} {
		if !strings.Contains(out, path) {
			t.Errorf("nginx output missing path %q:\n%s", path, out)
		}
	}
}

func TestNginx_ContainsSunsetHeader(t *testing.T) {
	out := render(t, testRules, FormatNginx)
	if !strings.Contains(out, "Sunset") {
		t.Errorf("nginx output missing Sunset header:\n%s", out)
	}
}

func TestNginx_ContainsSuccessorLink(t *testing.T) {
	out := render(t, testRules, FormatNginx)
	if !strings.Contains(out, "successor-version") {
		t.Errorf("nginx output missing successor-version link:\n%s", out)
	}
}

// ── Envoy ─────────────────────────────────────────────────────────────────────

func TestEnvoy_ContainsDirectResponse(t *testing.T) {
	out := render(t, testRules, FormatEnvoy)
	if !strings.Contains(out, "direct_response") {
		t.Errorf("envoy output missing 'direct_response':\n%s", out)
	}
}

func TestEnvoy_Contains410Status(t *testing.T) {
	out := render(t, testRules, FormatEnvoy)
	if !strings.Contains(out, "status: 410") {
		t.Errorf("envoy output missing 'status: 410':\n%s", out)
	}
}

func TestEnvoy_ContainsSuccessorLink(t *testing.T) {
	out := render(t, testRules, FormatEnvoy)
	if !strings.Contains(out, "successor-version") {
		t.Errorf("envoy output missing Link successor header:\n%s", out)
	}
}

func TestEnvoy_ContainsBothPaths(t *testing.T) {
	out := render(t, testRules, FormatEnvoy)
	for _, path := range []string{"/api/v1/users", "/api/v1/orders/{id}"} {
		if !strings.Contains(out, path) {
			t.Errorf("envoy output missing path %q", path)
		}
	}
}

// ── Kong ─────────────────────────────────────────────────────────────────────

func TestKong_ContainsRequestTermination(t *testing.T) {
	out := render(t, testRules, FormatKong)
	if !strings.Contains(out, "request-termination") {
		t.Errorf("kong output missing 'request-termination':\n%s", out)
	}
}

func TestKong_Contains410StatusCode(t *testing.T) {
	out := render(t, testRules, FormatKong)
	if !strings.Contains(out, "status_code: 410") {
		t.Errorf("kong output missing 'status_code: 410':\n%s", out)
	}
}

func TestKong_ContainsBothPaths(t *testing.T) {
	out := render(t, testRules, FormatKong)
	for _, path := range []string{"/api/v1/users", "/api/v1/orders/{id}"} {
		if !strings.Contains(out, path) {
			t.Errorf("kong output missing path %q", path)
		}
	}
}

// ── ParseFormat ───────────────────────────────────────────────────────────────

func TestParseFormat_Valid(t *testing.T) {
	for _, f := range []string{"nginx", "envoy", "kong", "NGINX", "Envoy"} {
		_, err := ParseFormat(f)
		if err != nil {
			t.Errorf("ParseFormat(%q): unexpected error: %v", f, err)
		}
	}
}

func TestParseFormat_Invalid(t *testing.T) {
	_, err := ParseFormat("haproxy")
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

func TestGenerate_EmptyRules(t *testing.T) {
	for _, fmt := range []Format{FormatNginx, FormatEnvoy, FormatKong} {
		var buf bytes.Buffer
		if err := Generate(nil, fmt, &buf); err != nil {
			t.Errorf("Generate(%s) with empty rules: %v", fmt, err)
		}
	}
}

func TestSunsetBody_IncludesSuccessor(t *testing.T) {
	rule := SunsetRule{Path: "/old", SunsetDate: time.Now(), Successor: "/new"}
	body := sunsetBody(rule)
	if !strings.Contains(body, "/new") {
		t.Errorf("sunsetBody missing successor:\n%s", body)
	}
	if !strings.Contains(body, "gone") {
		t.Errorf("sunsetBody missing 'gone' error:\n%s", body)
	}
}
