package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func mkdir(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	return path
}

func TestScan_FindsOpenAPIFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "openapi.yaml", "openapi: 3.0.0")
	writeFile(t, dir, "swagger.json", `{"swagger":"2.0"}`)
	writeFile(t, dir, "openapi.yml", "openapi: 3.0.0")

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("got %d files, want 3", len(files))
	}
	for _, f := range files {
		if f.Type != TypeOpenAPI {
			t.Errorf("%s: type = %q, want openapi", f.Path, f.Type)
		}
	}
}

func TestScan_FindsAsyncAPIFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "asyncapi.yaml", "asyncapi: 2.0.0")
	writeFile(t, dir, "asyncapi.json", `{"asyncapi":"2.0.0"}`)

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2", len(files))
	}
	for _, f := range files {
		if f.Type != TypeAsyncAPI {
			t.Errorf("%s: type = %q, want asyncapi", f.Path, f.Type)
		}
	}
}

func TestScan_FindsProtoFiles(t *testing.T) {
	dir := t.TempDir()
	sub := mkdir(t, dir, "proto")
	writeFile(t, sub, "orders.proto", `syntax = "proto3";`)
	writeFile(t, sub, "users.proto", `syntax = "proto3";`)

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2", len(files))
	}
	for _, f := range files {
		if f.Type != TypeGRPC {
			t.Errorf("%s: type = %q, want grpc", f.Path, f.Type)
		}
	}
}

func TestScan_IgnoresUnknownFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "README.md", "# hello")
	writeFile(t, dir, "openapi.yaml", "openapi: 3.0.0")

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("got %d files, want 1", len(files))
	}
}

func TestScan_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	hidden := mkdir(t, dir, ".hidden")
	writeFile(t, hidden, "openapi.yaml", "openapi: 3.0.0")
	writeFile(t, dir, "asyncapi.yaml", "asyncapi: 2.0.0")

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("got %d files, want 1 (hidden dir should be skipped)", len(files))
	}
}

func TestScan_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	nm := mkdir(t, dir, "node_modules")
	writeFile(t, nm, "openapi.yaml", "openapi: 3.0.0")

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0 (node_modules should be skipped)", len(files))
	}
}

func TestScan_SkipsVendorDir(t *testing.T) {
	dir := t.TempDir()
	v := mkdir(t, dir, "vendor")
	writeFile(t, v, "openapi.yaml", "openapi: 3.0.0")

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0 (vendor should be skipped)", len(files))
	}
}

func TestScan_ReadsContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "openapi.yaml", "openapi: 3.0.0\ninfo:\n  title: Test")

	files, err := New(dir).Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if string(files[0].Content) != "openapi: 3.0.0\ninfo:\n  title: Test" {
		t.Errorf("content mismatch: %q", files[0].Content)
	}
}

func TestDetect_Table(t *testing.T) {
	cases := []struct {
		name     string
		wantType Type
		wantOK   bool
	}{
		{"openapi.yaml", TypeOpenAPI, true},
		{"openapi.yml", TypeOpenAPI, true},
		{"openapi.json", TypeOpenAPI, true},
		{"OPENAPI.YAML", TypeOpenAPI, true},
		{"swagger.yaml", TypeOpenAPI, true},
		{"swagger.json", TypeOpenAPI, true},
		{"asyncapi.yaml", TypeAsyncAPI, true},
		{"asyncapi.yml", TypeAsyncAPI, true},
		{"asyncapi.json", TypeAsyncAPI, true},
		{"service.proto", TypeGRPC, true},
		{"main.go", "", false},
		{"README.md", "", false},
		{"api.txt", "", false},
		{"openapi.txt", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := detect(c.name)
			if ok != c.wantOK {
				t.Errorf("detect(%q) ok = %v, want %v", c.name, ok, c.wantOK)
			}
			if ok && got != c.wantType {
				t.Errorf("detect(%q) type = %q, want %q", c.name, got, c.wantType)
			}
		})
	}
}
