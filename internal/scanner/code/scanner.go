// Package code scans Go source files to extract HTTP route registrations.
// It supports net/http, chi, gin, echo, and gorilla/mux patterns.
package code

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Route is an HTTP route registration found in source code.
type Route struct {
	Method string // uppercase HTTP method, or "*" for any/unspecified
	Path   string // normalised to {param} style
	File   string
	Line   int
}

var (
	// .Get("/path",  .POST("/path",  .Route("/path",  — chi, gin, echo (case-insensitive)
	methodPathRe = regexp.MustCompile(
		`(?i)\.(Get|Post|Put|Patch|Delete|Options|Head|Any|Route)\s*\(\s*` +
			`(?:"([^"]+)"|` + "`([^`]+)`)")

	// HandleFunc("/path",  Handle("/path",  — stdlib, gorilla/mux
	handleFuncRe = regexp.MustCompile(
		`\b(?:HandleFunc|Handle)\s*\(\s*` +
			`(?:"([^"]+)"|` + "`([^`]+)`)")

	// .Methods("GET", "POST") — gorilla/mux chain
	methodsRe = regexp.MustCompile(
		`\.Methods\s*\(\s*"([A-Z]+)"`)

	// colonParam: :id → {id}
	colonParamRe = regexp.MustCompile(`:(\w+)`)
)

// Scanner walks a directory tree and extracts HTTP route registrations
// from Go source files.
type Scanner struct {
	root string
}

// New creates a Scanner rooted at dir.
func New(dir string) *Scanner { return &Scanner{root: dir} }

// Scan returns all route registrations found under the root directory.
func (s *Scanner) Scan() ([]Route, error) {
	var routes []Route

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
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rs, err := scanFile(path)
		if err != nil {
			return err
		}
		routes = append(routes, rs...)
		return nil
	})

	return routes, err
}

func scanFile(path string) ([]Route, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var routes []Route
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Framework method-style: .Get("/path", .Post("/path", etc.
		if m := methodPathRe.FindStringSubmatch(line); m != nil {
			method := strings.ToUpper(m[1])
			path := firstNonEmpty(m[2], m[3])
			if method == "ROUTE" || method == "ANY" {
				method = "*"
			}
			routes = append(routes, Route{
				Method: method,
				Path:   normalisePath(path),
				File:   path,
				Line:   lineNum,
			})
			continue
		}

		// Stdlib-style: HandleFunc("/path",
		if m := handleFuncRe.FindStringSubmatch(line); m != nil {
			apiPath := firstNonEmpty(m[1], m[2])
			// Try to find chained .Methods on the same line.
			method := "*"
			if mm := methodsRe.FindStringSubmatch(line); mm != nil {
				method = mm[1]
			}
			routes = append(routes, Route{
				Method: method,
				Path:   normalisePath(apiPath),
				File:   path,
				Line:   lineNum,
			})
		}
	}

	return routes, scanner.Err()
}

// normalisePath converts colon-style path parameters to OpenAPI {param} style
// and strips trailing slashes.
func normalisePath(p string) string {
	p = colonParamRe.ReplaceAllString(p, `{$1}`)
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

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
