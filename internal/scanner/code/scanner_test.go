package code

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func findRoute(routes []Route, method, path string) *Route {
	for i, r := range routes {
		if r.Method == method && r.Path == path {
			return &routes[i]
		}
	}
	return nil
}

func TestScan_ChiRoutes(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "routes.go", `package main
import "github.com/go-chi/chi/v5"
func routes(r chi.Router) {
	r.Get("/users", listUsers)
	r.Post("/users", createUser)
	r.Put("/users/{id}", updateUser)
	r.Delete("/users/{id}", deleteUser)
}`)

	routes, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/users"},
		{"POST", "/users"},
		{"PUT", "/users/{id}"},
		{"DELETE", "/users/{id}"},
	} {
		if findRoute(routes, tc.method, tc.path) == nil {
			t.Errorf("missing route %s %s", tc.method, tc.path)
		}
	}
}

func TestScan_GinRoutes(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "server.go", `package main
func setup(r *gin.Engine) {
	r.GET("/orders", getOrders)
	r.POST("/orders", createOrder)
	r.PATCH("/orders/:id", patchOrder)
}`)

	routes, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if findRoute(routes, "GET", "/orders") == nil {
		t.Error("missing GET /orders")
	}
	if findRoute(routes, "POST", "/orders") == nil {
		t.Error("missing POST /orders")
	}
	// colon param normalised to {id}
	if findRoute(routes, "PATCH", "/orders/{id}") == nil {
		t.Error("missing PATCH /orders/{id} (normalised from :id)")
	}
}

func TestScan_StdlibHandleFunc(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main
import "net/http"
func main() {
	http.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/api/v1/ping", pingHandler)
}`)

	routes, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if findRoute(routes, "*", "/healthz") == nil {
		t.Error("missing wildcard route for /healthz")
	}
	if findRoute(routes, "*", "/api/v1/ping") == nil {
		t.Error("missing wildcard route for /api/v1/ping")
	}
}

func TestScan_WildcardRouteMethod(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "routes.go", `package main
func setup(r chi.Router) {
	r.Route("/admin", adminRoutes)
}`)

	routes, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if findRoute(routes, "*", "/admin") == nil {
		t.Error("Route() call should produce a wildcard method route")
	}
}

func TestScan_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "routes_test.go", `package main
func TestRoutes(t *testing.T) {
	r.Get("/test-only", testHandler)
}`)
	writeGoFile(t, dir, "routes.go", `package main
func setup(r chi.Router) {
	r.Get("/real", realHandler)
}`)

	routes, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if findRoute(routes, "GET", "/test-only") != nil {
		t.Error("should not scan _test.go files")
	}
	if findRoute(routes, "GET", "/real") == nil {
		t.Error("should scan non-test files")
	}
}

func TestScan_PathNormalisation(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/users/:id", "/users/{id}"},
		{"/a/:b/c/:d", "/a/{b}/c/{d}"},
		{"/users/{id}", "/users/{id}"},
		{"/trailing/", "/trailing"},
		{"/", "/"},
	}
	for _, c := range cases {
		got := normalisePath(c.input)
		if got != c.want {
			t.Errorf("normalisePath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestScan_IgnoresNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "routes.ts"), []byte(`app.get("/users", handler)`), 0o644); err != nil {
		t.Fatal(err)
	}
	routes, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes from .ts file, got %d", len(routes))
	}
}

// ── isSkipped ─────────────────────────────────────────────────────────────────

func TestIsSkipped_HiddenDir(t *testing.T) {
for _, name := range []string{".git", ".github", ".idea"} {
if !isSkipped(name) {
t.Errorf("isSkipped(%q) = false, want true", name)
}
}
}

func TestIsSkipped_KnownDirs(t *testing.T) {
for _, name := range []string{"vendor", "node_modules", "dist", "build", "target"} {
if !isSkipped(name) {
t.Errorf("isSkipped(%q) = false, want true", name)
}
}
}

func TestIsSkipped_NormalDirs(t *testing.T) {
for _, name := range []string{"internal", "cmd", "pkg", "src", "api"} {
if isSkipped(name) {
t.Errorf("isSkipped(%q) = true, want false", name)
}
}
}

// ── firstNonEmpty ─────────────────────────────────────────────────────────────

func TestFirstNonEmpty_BothEmpty(t *testing.T) {
if got := firstNonEmpty("", ""); got != "" {
t.Errorf("got %q, want empty string", got)
}
}

func TestFirstNonEmpty_FirstIsNonEmpty(t *testing.T) {
if got := firstNonEmpty("a", "b"); got != "a" {
t.Errorf("got %q, want a", got)
}
}

func TestFirstNonEmpty_FallsBackToSecond(t *testing.T) {
if got := firstNonEmpty("", "b"); got != "b" {
t.Errorf("got %q, want b", got)
}
}

// ── Scan skips directories ────────────────────────────────────────────────────

func TestScan_SkipsVendorDir(t *testing.T) {
dir := t.TempDir()
vendor := filepath.Join(dir, "vendor")
if err := os.MkdirAll(vendor, 0o755); err != nil {
t.Fatal(err)
}
writeGoFile(t, vendor, "dep.go", `package dep
func setup(r chi.Router) { r.Get("/vendor-route", h) }`)

routes, err := New(dir).Scan()
if err != nil {
t.Fatalf("Scan: %v", err)
}
for _, r := range routes {
if r.Path == "/vendor-route" {
t.Error("should not scan vendor directory")
}
}
}

func TestScan_SkipsNodeModules(t *testing.T) {
dir := t.TempDir()
nm := filepath.Join(dir, "node_modules")
if err := os.MkdirAll(nm, 0o755); err != nil {
t.Fatal(err)
}
writeGoFile(t, nm, "dep.go", `package dep
func setup(r chi.Router) { r.Get("/nm-route", h) }`)

routes, err := New(dir).Scan()
if err != nil {
t.Fatalf("Scan: %v", err)
}
for _, r := range routes {
if r.Path == "/nm-route" {
t.Error("should not scan node_modules directory")
}
}
}
