// Package usage scans source code to find calls to deprecated API endpoints.
package usage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Usage represents one place in the codebase that references a deprecated endpoint.
type Usage struct {
	DeprecatedPath string // the deprecated API path matched
	ContractName   string // which deprecated Backstage contract this belongs to
	File           string
	Line           int
	Context        string // the source line for human context
}

// Scanner walks source files looking for references to deprecated API paths.
type Scanner struct {
	root string
}

// New creates a Scanner rooted at dir.
func New(dir string) *Scanner { return &Scanner{root: dir} }

// Scan searches for usages of any of the given deprecated paths.
// deprecatedPaths maps API path → contract name, e.g. {"/api/v1/users": "users-api-v1"}.
func (s *Scanner) Scan(deprecatedPaths map[string]string) ([]Usage, error) {
	if len(deprecatedPaths) == 0 {
		return nil, nil
	}

	var entries []scanEntry
	for path, contract := range deprecatedPaths {
		pattern := fmt.Sprintf(`["` + "`" + `]([^"` + "`" + `]*%s[^"` + "`" + `]*)["` + "`" + `]`,
			regexp.QuoteMeta(path))
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile pattern for %q: %w", path, err)
		}
		entries = append(entries, scanEntry{re: re, path: path, contractName: contract})
	}

	var usages []Usage

	err := filepath.Walk(s.root, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if filePath != s.root && isSkipped(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(filePath) {
			return nil
		}

		found, err := scanFile(filePath, entries)
		if err != nil {
			return err
		}
		usages = append(usages, found...)
		return nil
	})

	return usages, err
}

type scanEntry struct {
	re           *regexp.Regexp
	path         string
	contractName string
}

func scanFile(filePath string, entries []scanEntry) ([]Usage, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var usages []Usage
	sc := bufio.NewScanner(f)
	lineNum := 0

	for sc.Scan() {
		lineNum++
		line := sc.Text()

		for _, e := range entries {
			if e.re.MatchString(line) {
				usages = append(usages, Usage{
					DeprecatedPath: e.path,
					ContractName:   e.contractName,
					File:           filePath,
					Line:           lineNum,
					Context:        strings.TrimSpace(line),
				})
				break // one entry per line is enough
			}
		}
	}

	return usages, sc.Err()
}

// isSourceFile returns true for files we want to scan for API usage.
func isSourceFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".go", ".ts", ".js", ".py", ".java", ".cs", ".rb":
		return true
	}
	return false
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
