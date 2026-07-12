// Package scanner walks a codebase and extracts API surface information
// for OpenAPI, AsyncAPI, and gRPC (proto) contracts.
package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// Type identifies the kind of API specification found in a file.
type Type string

const (
	TypeOpenAPI  Type = "openapi"
	TypeAsyncAPI Type = "asyncapi"
	TypeGRPC     Type = "grpc"
	// TypeMQTT identifies an AsyncAPI spec that uses the MQTT protocol binding.
	// It is handled identically to TypeAsyncAPI in the diff engine.
	TypeMQTT Type = "mqtt"
)

// SpecFile is an API specification file found on disk.
type SpecFile struct {
	Path    string
	Type    Type
	Content []byte
}

// Scanner walks a directory tree looking for API spec files.
type Scanner struct {
	root string
}

// New creates a Scanner rooted at dir.
func New(dir string) *Scanner {
	return &Scanner{root: dir}
}

// Scan walks the source tree and returns every API spec file found.
// Directories named .git, vendor, node_modules, dist, build, target,
// and any directory whose name starts with "." are skipped.
func (s *Scanner) Scan() ([]SpecFile, error) {
	var files []SpecFile

	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != s.root && isSkipped(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		t, ok := detect(path)
		if !ok {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, SpecFile{Path: path, Type: t, Content: content})
		return nil
	})

	return files, err
}

// detect returns the spec type for a file path, or false if the file is not
// a recognised API spec. Detection uses the file name prefix first, then
// falls back to content inspection for YAML/JSON files.
func detect(path string) (Type, bool) {
	name := strings.ToLower(filepath.Base(path))
	ext := filepath.Ext(name)

	switch {
	case (strings.HasPrefix(name, "openapi") || strings.HasPrefix(name, "swagger")) &&
		(ext == ".yaml" || ext == ".yml" || ext == ".json"):
		return TypeOpenAPI, true

	case strings.HasPrefix(name, "asyncapi") &&
		(ext == ".yaml" || ext == ".yml" || ext == ".json"):
		return TypeAsyncAPI, true

	case ext == ".proto":
		return TypeGRPC, true
	}

	// Content-based fallback for YAML/JSON files with non-standard names.
	if ext == ".yaml" || ext == ".yml" || ext == ".json" {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", false
		}
		return detectFromContent(content)
	}

	return "", false
}

// detectFromContent inspects spec file content to determine its type.
// MQTT is identified as AsyncAPI specs that also declare MQTT protocol bindings.
func detectFromContent(content []byte) (Type, bool) {
	s := string(content)
	switch {
	case strings.Contains(s, "asyncapi:"):
		if strings.Contains(s, "mqtt") {
			return TypeMQTT, true
		}
		return TypeAsyncAPI, true
	case strings.Contains(s, "openapi:") || strings.Contains(s, "swagger:"):
		return TypeOpenAPI, true
	}
	return "", false
}

// isSkipped reports whether a directory name should be excluded from the walk.
func isSkipped(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "vendor", "node_modules", "dist", "build", "target":
		return true
	}
	return false
}
