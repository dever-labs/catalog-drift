package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

var deprecatedPaths = map[string]string{
	"/api/v1/users":  "users-api-v1",
	"/api/v1/orders": "orders-api-v1",
}

func TestScan_DetectsHTTPGetCall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "client.go", `package main
import "net/http"
func getUsers() {
	resp, _ := http.Get("https://api.example.com/api/v1/users")
	_ = resp
}`)

	usages, err := New(dir).Scan(deprecatedPaths)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(usages) == 0 {
		t.Fatal("expected at least one usage, got none")
	}
	if usages[0].DeprecatedPath != "/api/v1/users" {
		t.Errorf("DeprecatedPath = %q, want /api/v1/users", usages[0].DeprecatedPath)
	}
	if usages[0].ContractName != "users-api-v1" {
		t.Errorf("ContractName = %q, want users-api-v1", usages[0].ContractName)
	}
	if usages[0].Line == 0 {
		t.Error("Line should be non-zero")
	}
	if usages[0].File == "" {
		t.Error("File should not be empty")
	}
}

func TestScan_DetectsStringLiteral(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.go", `package main
const deprecatedEndpoint = "/api/v1/orders"
`)

	usages, err := New(dir).Scan(deprecatedPaths)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(usages) == 0 {
		t.Fatal("expected usage from string literal, got none")
	}
}

func TestScan_NoFalsePositives(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "clean.go", `package main
func callNewAPI() {
	http.Get("https://api.example.com/api/v2/users")
}`)

	usages, err := New(dir).Scan(deprecatedPaths)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(usages) != 0 {
		t.Errorf("expected 0 usages for /api/v2/users, got %d", len(usages))
	}
}

func TestScan_MultipleFilesMultiplePaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "users_client.go", `package main
func fetchUsers() { http.Get("/api/v1/users") }`)
	writeFile(t, dir, "orders_client.go", `package main
func fetchOrders() { http.Get("/api/v1/orders") }`)

	usages, err := New(dir).Scan(deprecatedPaths)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(usages) != 2 {
		t.Errorf("expected 2 usages, got %d", len(usages))
	}
}

func TestScan_EmptyDeprecatedPaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main`)

	usages, err := New(dir).Scan(nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if usages != nil {
		t.Errorf("expected nil usages for empty paths, got %v", usages)
	}
}

func TestScan_ContextField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main
func call() { http.Get("/api/v1/users") }`)

	usages, err := New(dir).Scan(deprecatedPaths)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(usages) == 0 {
		t.Fatal("expected usage")
	}
	if usages[0].Context == "" {
		t.Error("Context should contain the source line")
	}
}

func TestScan_SkipsVendorDir(t *testing.T) {
	dir := t.TempDir()
	vendor := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, vendor, "dep.go", `package dep
func call() { http.Get("/api/v1/users") }`)

	usages, err := New(dir).Scan(deprecatedPaths)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(usages) != 0 {
		t.Errorf("expected 0 usages from vendor dir, got %d", len(usages))
	}
}

// ── isSkipped ─────────────────────────────────────────────────────────────────

func TestUsageIsSkipped_HiddenDir(t *testing.T) {
for _, name := range []string{".git", ".github"} {
if !isSkipped(name) {
t.Errorf("isSkipped(%q) = false, want true", name)
}
}
}

func TestUsageIsSkipped_KnownDirs(t *testing.T) {
for _, name := range []string{"vendor", "node_modules", "dist", "build", "target"} {
if !isSkipped(name) {
t.Errorf("isSkipped(%q) = false, want true", name)
}
}
}

func TestUsageIsSkipped_NormalDirs(t *testing.T) {
for _, name := range []string{"internal", "cmd", "src", "lib"} {
if isSkipped(name) {
t.Errorf("isSkipped(%q) = true, want false", name)
}
}
}

// ── isSourceFile ──────────────────────────────────────────────────────────────

func TestIsSourceFile_KnownExtensions(t *testing.T) {
for _, path := range []string{"main.go", "app.ts", "index.js", "api.py", "Main.java", "Service.cs", "client.rb"} {
if !isSourceFile(path) {
t.Errorf("isSourceFile(%q) = false, want true", path)
}
}
}

func TestIsSourceFile_UnknownExtensions(t *testing.T) {
for _, path := range []string{"README.md", "Makefile", "config.yaml", "data.json"} {
if isSourceFile(path) {
t.Errorf("isSourceFile(%q) = true, want false", path)
}
}
}

// ── Scan skips directories ────────────────────────────────────────────────────

func TestScan_SkipsNodeModules(t *testing.T) {
dir := t.TempDir()
nm := filepath.Join(dir, "node_modules")
if err := os.MkdirAll(nm, 0o755); err != nil {
t.Fatal(err)
}
writeFile(t, nm, "lib.go", `package lib
func call() { http.Get("/api/v1/users") }`)

usages, err := New(dir).Scan(deprecatedPaths)
if err != nil {
t.Fatalf("Scan: %v", err)
}
if len(usages) != 0 {
t.Errorf("expected 0 usages from node_modules, got %d", len(usages))
}
}

func TestScan_SkipsDotGit(t *testing.T) {
dir := t.TempDir()
gitDir := filepath.Join(dir, ".git")
if err := os.MkdirAll(gitDir, 0o755); err != nil {
t.Fatal(err)
}
writeFile(t, gitDir, "hook.go", `package hook
func run() { http.Get("/api/v1/users") }`)

usages, err := New(dir).Scan(deprecatedPaths)
if err != nil {
t.Fatalf("Scan: %v", err)
}
if len(usages) != 0 {
t.Errorf("expected 0 usages from .git dir, got %d", len(usages))
}
}
