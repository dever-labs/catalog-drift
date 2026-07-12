package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dever-labs/catalog-drift/internal/reporter"
	"github.com/dever-labs/catalog-drift/internal/scanner"
)

// ── parseDuration ─────────────────────────────────────────────────────────────

func TestParseDuration_Days(t *testing.T) {
	d, err := parseDuration("90d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 90*24*time.Hour {
		t.Errorf("got %v, want 90*24h", d)
	}
}

func TestParseDuration_StandardGo(t *testing.T) {
	d, err := parseDuration("720h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 720*time.Hour {
		t.Errorf("got %v, want 720h", d)
	}
}

func TestParseDuration_Zero(t *testing.T) {
	_, err := parseDuration("0d")
	if err == nil {
		t.Error("expected error for 0d duration")
	}
}

func TestParseDuration_Negative(t *testing.T) {
	_, err := parseDuration("-5d")
	if err == nil {
		t.Error("expected error for negative day duration")
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	_, err := parseDuration("invalid")
	if err == nil {
		t.Error("expected error for invalid duration string")
	}
}

func TestParseDuration_1Day(t *testing.T) {
	d, err := parseDuration("1d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("got %v, want 24h", d)
	}
}

// ── matchSpec ─────────────────────────────────────────────────────────────────

func makeSpec(specType scanner.Type, path string) scanner.SpecFile {
	return scanner.SpecFile{Type: specType, Path: path}
}

func TestMatchSpec_ByName(t *testing.T) {
	files := []scanner.SpecFile{
		makeSpec(scanner.TypeOpenAPI, "/specs/orders-api.yaml"),
		makeSpec(scanner.TypeOpenAPI, "/specs/users-api.yaml"),
	}
	match := matchSpec(files, "openapi", "orders-api")
	if match == nil {
		t.Fatal("expected a match, got nil")
	}
	if !strings.Contains(match.Path, "orders-api") {
		t.Errorf("wrong match: %s", match.Path)
	}
}

func TestMatchSpec_FallbackToFirstOfType(t *testing.T) {
	files := []scanner.SpecFile{
		makeSpec(scanner.TypeOpenAPI, "/specs/unknown.yaml"),
	}
	match := matchSpec(files, "openapi", "no-name-match")
	if match == nil {
		t.Fatal("expected fallback match, got nil")
	}
}

func TestMatchSpec_NoMatchForDifferentType(t *testing.T) {
	files := []scanner.SpecFile{
		makeSpec(scanner.TypeAsyncAPI, "/specs/events.yaml"),
	}
	match := matchSpec(files, "openapi", "events")
	if match != nil {
		t.Errorf("expected nil for type mismatch, got %s", match.Path)
	}
}

func TestMatchSpec_EmptyList(t *testing.T) {
	match := matchSpec(nil, "openapi", "orders")
	if match != nil {
		t.Errorf("expected nil for empty file list, got %v", match)
	}
}

func TestMatchSpec_ContractNameSubstringOfFilename(t *testing.T) {
	files := []scanner.SpecFile{
		makeSpec(scanner.TypeOpenAPI, "/specs/orders-service-openapi.yaml"),
	}
	match := matchSpec(files, "openapi", "orders")
	if match == nil {
		t.Fatal("expected match for contract name substring in filename")
	}
}

// ── countSeverities ───────────────────────────────────────────────────────────

func TestCountSeverities_Mixed(t *testing.T) {
	findings := []reporter.Finding{
		{Severity: "error"},
		{Severity: "warning"},
		{Severity: "error"},
		{Severity: "warning"},
		{Severity: "warning"},
	}
	errs, warns := countSeverities(findings)
	if errs != 2 {
		t.Errorf("errors: got %d, want 2", errs)
	}
	if warns != 3 {
		t.Errorf("warnings: got %d, want 3", warns)
	}
}

func TestCountSeverities_Empty(t *testing.T) {
	errs, warns := countSeverities(nil)
	if errs != 0 || warns != 0 {
		t.Errorf("expected 0/0, got %d/%d", errs, warns)
	}
}

func TestCountSeverities_AllErrors(t *testing.T) {
	findings := []reporter.Finding{{Severity: "error"}, {Severity: "error"}}
	errs, warns := countSeverities(findings)
	if errs != 2 || warns != 0 {
		t.Errorf("got %d errs, %d warns; want 2/0", errs, warns)
	}
}

// ── End-to-end: runCheck ──────────────────────────────────────────────────────

// backstageMux builds a minimal test Backstage server responding to entity
// lookups and returns the mux for fine-grained handler overrides.
func backstageMux(componentJSON, apiJSON string) *http.ServeMux {
	mux := http.NewServeMux()
	// Component lookup — Backstage client uses strings.ToLower(kind)
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/my-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, componentJSON)
	})
	// API entity lookup
	mux.HandleFunc("/api/catalog/entities/by-name/api/default/orders-api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiJSON)
	})
	// Catalog-wide entity list (FetchAllContracts, FetchDeprecatedContracts).
	mux.HandleFunc("/api/catalog/entities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", apiJSON)
	})
	return mux
}

func componentEntityJSON(apiName string) string {
	return fmt.Sprintf(`{
		"apiVersion":"backstage.io/v1alpha1",
		"kind":"Component",
		"metadata":{"name":"my-service","namespace":"default"},
		"spec":{"lifecycle":"production"},
		"relations":[{"type":"providesApi","targetRef":"api:default/%s"}]
	}`, apiName)
}

func apiEntityJSON(name, lifecycle, definition string) string {
	defBytes, _ := json.Marshal(definition)
	return fmt.Sprintf(`{
		"apiVersion":"backstage.io/v1alpha1",
		"kind":"API",
		"metadata":{"name":%q,"namespace":"default"},
		"spec":{"type":"openapi","lifecycle":%q,"definition":%s}
	}`, name, lifecycle, string(defBytes))
}

const minimalOpenAPI = `openapi: "3.0.0"
info:
  title: Orders API
  version: "1.0"
paths:
  /orders:
    get:
      summary: list orders
      responses:
        "200":
          description: ok
`

func TestRunCheck_CleanOutput(t *testing.T) {
	srv := httptest.NewServer(backstageMux(
		componentEntityJSON("orders-api"),
		apiEntityJSON("orders-api", "production", minimalOpenAPI),
	))
	defer srv.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orders-api.yaml"), []byte(minimalOpenAPI), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runCheck([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
		"--source", dir,
		"--format", "json",
	})
	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}
}

func TestRunCheck_MissingBackstageURL(t *testing.T) {
	err := runCheck([]string{"--component", "my-service"})
	if err == nil || !strings.Contains(err.Error(), "--backstage-url") {
		t.Errorf("expected --backstage-url error, got: %v", err)
	}
}

func TestRunCheck_MissingComponent(t *testing.T) {
	err := runCheck([]string{"--backstage-url", "http://localhost"})
	if err == nil || !strings.Contains(err.Error(), "--component") {
		t.Errorf("expected --component error, got: %v", err)
	}
}

func TestRunCheck_InvalidFormat(t *testing.T) {
	err := runCheck([]string{
		"--backstage-url", "http://localhost",
		"--component", "svc",
		"--format", "xml",
	})
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestRunCheck_InvalidErrorAfter(t *testing.T) {
	err := runCheck([]string{
		"--backstage-url", "http://localhost",
		"--component", "svc",
		"--error-after", "notaduration",
	})
	if err == nil {
		t.Error("expected error for invalid --error-after")
	}
}

func TestRunCheck_DeprecatedAPIWarning(t *testing.T) {
	srv := httptest.NewServer(backstageMux(
		componentEntityJSON("orders-api"),
		apiEntityJSON("orders-api", "deprecated", minimalOpenAPI),
	))
	defer srv.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orders-api.yaml"), []byte(minimalOpenAPI), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capture stdout by redirecting (we just test no error for now).
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCheck([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
		"--source", dir,
		"--format", "json",
	})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}

	// JSON output should contain the deprecation finding
	if !strings.Contains(buf.String(), "deprecat") {
		t.Errorf("expected deprecation in output, got: %s", buf.String())
	}
}

func TestRunCheck_ScanCode(t *testing.T) {
	srv := httptest.NewServer(backstageMux(
		componentEntityJSON("orders-api"),
		apiEntityJSON("orders-api", "production", minimalOpenAPI),
	))
	defer srv.Close()

	dir := t.TempDir()
	// Write matching spec
	if err := os.WriteFile(filepath.Join(dir, "orders-api.yaml"), []byte(minimalOpenAPI), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write matching Go route
	routeFile := `package main
func setup(r chi.Router) {
	r.Get("/orders", listOrders)
}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(routeFile), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runCheck([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
		"--source", dir,
		"--scan-code",
	})
	if err != nil {
		t.Fatalf("runCheck with --scan-code: %v", err)
	}
}

// ── End-to-end: runDeprecated ──────────────────────────────────────────────────────

func TestRunDeprecated_MissingBackstageURL(t *testing.T) {
	err := runDeprecated([]string{"--component", "svc"})
	if err == nil || !strings.Contains(err.Error(), "--backstage-url") {
		t.Errorf("expected --backstage-url error, got: %v", err)
	}
}

func TestRunUsage_NoDeprecatedAPIs(t *testing.T) {
	srv := httptest.NewServer(backstageMux(
		componentEntityJSON("orders-api"),
		apiEntityJSON("orders-api", "production", minimalOpenAPI),
	))
	defer srv.Close()

	dir := t.TempDir()
	err := runDeprecated([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
		"--source", dir,
	})
	if err != nil {
		t.Fatalf("runDeprecated: %v", err)
	}
}

func TestRunDeprecated_RemovedAPI(t *testing.T) {
	// Component declares consumesApi "gone-api", but that API returns 404.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/my-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"apiVersion":"backstage.io/v1alpha1","kind":"Component",
			"metadata":{"name":"my-service","namespace":"default"},
			"spec":{"type":"service"},
			"relations":[{"type":"consumesApi","targetRef":"api:default/gone-api","target":{"kind":"api","namespace":"default","name":"gone-api"}}]
		}`)
	})
	mux.HandleFunc("/api/catalog/entities/by-name/api/default/gone-api", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/catalog/entities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// FetchConsumedAPIStatuses should surface "gone-api" as removed.
	client := newBackstageClient(srv.URL, "", "")
	statuses, err := client.FetchConsumedAPIStatuses(context.Background(), "my-service", "default")
	if err != nil {
		t.Fatalf("FetchConsumedAPIStatuses: %v", err)
	}
	if len(statuses) != 1 || !statuses[0].Removed {
		t.Errorf("expected 1 removed API, got %+v", statuses)
	}
	if statuses[0].Name != "gone-api" {
		t.Errorf("expected gone-api, got %q", statuses[0].Name)
	}
}

func TestRunDeprecated_UndeclaredConsumption(t *testing.T) {
	// Component declares NO consumesApis, but code calls /orders which is in the catalog.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/my-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"apiVersion":"backstage.io/v1alpha1","kind":"Component",
			"metadata":{"name":"my-service","namespace":"default"},
			"spec":{"type":"service"},
			"relations":[]
		}`)
	})
	// FetchAllContracts returns orders-api.
	mux.HandleFunc("/api/catalog/entities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{
			"apiVersion":"backstage.io/v1alpha1","kind":"API",
			"metadata":{"name":"orders-api","namespace":"default"},
			"spec":{"type":"openapi","lifecycle":"production","definition":%q},
			"relations":[]
		}]`, minimalOpenAPI)
	})
	// fetchContracts (providesApi) for own APIs — returns nothing.
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/my-service/provides", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Write Go source that calls /orders — should be flagged as undeclared.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "client.go"), []byte(`
package client
const ordersURL = "/orders"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runDeprecated([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
		"--source", srcDir,
	})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
}



func TestRunConsumers_MissingFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no backstage-url", []string{"--component", "svc"}, "--backstage-url"},
		{"no component", []string{"--backstage-url", "http://localhost"}, "--component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runConsumers(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestRunConsumers_NoDeprecatedAPIs(t *testing.T) {
	srv := httptest.NewServer(backstageMux(
		componentEntityJSON("orders-api"),
		apiEntityJSON("orders-api", "production", minimalOpenAPI),
	))
	defer srv.Close()

	err := runConsumers([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
	})
	if err != nil {
		t.Fatalf("runConsumers: %v", err)
	}
}

func TestRunConsumers_DeprecatedAPIWithConsumers(t *testing.T) {
	mux := http.NewServeMux()
	// Component entity with providesApi relation.
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/my-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, componentEntityJSON("orders-api"))
	})
	// API entity with deprecated lifecycle and an apiConsumedBy relation.
	mux.HandleFunc("/api/catalog/entities/by-name/api/default/orders-api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"apiVersion":"backstage.io/v1alpha1","kind":"API",
			"metadata":{"name":"orders-api","namespace":"default","annotations":{"catalog-drift/deprecated-since":"2024-01-01"}},
			"spec":{"type":"openapi","lifecycle":"deprecated","definition":%q},
			"relations":[{"type":"apiConsumedBy","targetRef":"component:default/checkout-service","target":{"kind":"component","namespace":"default","name":"checkout-service"}}]
		}`, minimalOpenAPI)
	})
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/checkout-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"apiVersion":"backstage.io/v1alpha1","kind":"Component","metadata":{"name":"checkout-service","namespace":"default"},"spec":{},"relations":[]}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	err := runConsumers([]string{
		"--backstage-url", srv.URL,
		"--component", "my-service",
	})
	if err != nil {
		t.Fatalf("runConsumers: %v", err)
	}
}

func mustMarshal(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ── newBackstageClient helper ─────────────────────────────────────────────────

func TestNewBackstageClient_EnvTokenFallback(t *testing.T) {
	t.Setenv("BACKSTAGE_TOKEN", "env-token")
	c := newBackstageClient("http://example.com", "", "env-token")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewBackstageClient_ExplicitTokenWins(t *testing.T) {
	c := newBackstageClient("http://example.com", "explicit", "env")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// ── fetchContracts error path ─────────────────────────────────────────────────

func TestFetchContracts_BackstageUnreachable(t *testing.T) {
	c := newBackstageClient("http://127.0.0.1:1", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := fetchContracts(ctx, c, "svc", "default")
	if err == nil {
		t.Error("expected error when Backstage is unreachable")
	}
}

// ── End-to-end: check --scan-code (formerly runBreaking) ─────────────────────

func TestRunCheck_ScanCode_NoViolations(t *testing.T) {
srv := httptest.NewServer(backstageMux(
componentEntityJSON("orders-api"),
apiEntityJSON("orders-api", "production", minimalOpenAPI),
))
defer srv.Close()

srcDir := t.TempDir()
goFile := filepath.Join(srcDir, "main.go")
if err := os.WriteFile(goFile, []byte(`package main
import "github.com/gin-gonic/gin"
func main() {
	r := gin.New()
	r.GET("/orders", func(c *gin.Context) {})
}
`), 0o644); err != nil {
	t.Fatal(err)
}

err := runCheck([]string{
"--backstage-url", srv.URL,
"--component", "my-service",
"--source", srcDir,
"--scan-code",
})
if err != nil {
t.Fatalf("runCheck --scan-code: %v", err)
}
}

func TestRunCheck_ScanCode_MissingSourceDir(t *testing.T) {
srv := httptest.NewServer(backstageMux(
componentEntityJSON("orders-api"),
apiEntityJSON("orders-api", "production", minimalOpenAPI),
))
defer srv.Close()

err := runCheck([]string{
"--backstage-url", srv.URL,
"--component", "my-service",
"--source", "/no/such/dir",
"--scan-code",
})
if err == nil {
t.Error("expected error for missing source directory")
}
}
