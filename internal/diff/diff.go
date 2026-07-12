// Package diff compares a registered contract against a scanned
// implementation and produces a set of violations.
package diff

import (
"fmt"
"strings"

"github.com/dever-labs/catalog-drift/internal/scanner"
codescanner "github.com/dever-labs/catalog-drift/internal/scanner/code"
"gopkg.in/yaml.v3"
)

// Severity indicates how serious a violation is.
type Severity string

const (
SeverityWarning Severity = "warning"
SeverityError   Severity = "error"
)

// RuleType identifies which drift rule was triggered.
type RuleType string

const (
RuleMissingEndpoint     RuleType = "missing-endpoint"
RuleUndeclaredEndpoint  RuleType = "undeclared-endpoint"
RuleMissingField        RuleType = "missing-field"
RuleTypeMismatch        RuleType = "type-mismatch"
RuleMissingChannel      RuleType = "missing-channel"
RuleMissingRPCMethod    RuleType = "missing-rpc-method"
RuleUndeclaredRPCMethod RuleType = "undeclared-rpc-method"
)

// Violation is a single point of drift between the contract and the local spec.
type Violation struct {
Rule     RuleType
Path     string
Message  string
Severity Severity
}

// Endpoint is a path+method pair extracted from a contract definition.
type Endpoint struct {
Method string
Path   string
}

// Engine dispatches diff logic based on contract type.
type Engine struct{}

// New creates a new diff Engine.
func New() *Engine { return &Engine{} }

// DiffBreaking compares an old spec (registered baseline from Backstage) against
// a proposed new spec (local file) and returns only breaking violations:
// removals and incompatible changes that would affect existing consumers.
func (e *Engine) DiffBreaking(contractType, oldDef string, newSpec scanner.SpecFile) ([]Violation, error) {
	switch contractType {
	case "openapi", "swagger":
		return diffBreakingOpenAPI(oldDef, newSpec.Content)
	case "asyncapi", "mqtt":
		return diffBreakingAsyncAPI(oldDef, newSpec.Content)
	case "grpc":
		return diffBreakingGRPC(oldDef, newSpec.Content)
	default:
		return nil, fmt.Errorf("unsupported contract type %q for breaking diff", contractType)
	}
}

// Diff compares the Backstage-registered contract definition against a local
// spec file and returns any detected violations.
func (e *Engine) Diff(contractType, contractDef string, local scanner.SpecFile) ([]Violation, error) {
switch contractType {
case "openapi", "swagger":
return diffOpenAPI(contractDef, local.Content)
case "asyncapi", "mqtt":
return diffAsyncAPI(contractDef, local.Content)
case "grpc":
return diffGRPC(contractDef, local.Content)
default:
return nil, fmt.Errorf("unsupported contract type %q", contractType)
}
}

// DiffCodeRoutes compares the endpoints declared in an OpenAPI contract
// definition against routes extracted from source code.
func (e *Engine) DiffCodeRoutes(contractDef string, routes []codescanner.Route) ([]Violation, error) {
if contractDef == "" {
return nil, nil
}
spec, err := parseSpec(contractDef)
if err != nil {
return nil, fmt.Errorf("parse contract: %w", err)
}
contractPaths := extractOpenAPIPaths(spec)

// Index code routes: path → set of methods (lowercase).
codeRoutes := make(map[string]map[string]bool)
for _, r := range routes {
m := strings.ToLower(r.Method)
if codeRoutes[r.Path] == nil {
codeRoutes[r.Path] = make(map[string]bool)
}
codeRoutes[r.Path][m] = true
}

var violations []Violation

// Contract endpoints missing from code.
for path, contractMethods := range contractPaths {
codeMethods, exists := codeRoutes[path]
if !exists {
violations = append(violations, Violation{
Rule:     RuleMissingEndpoint,
Path:     "paths." + path,
Message:  fmt.Sprintf("path %q is declared in the contract but no route was found in the code", path),
Severity: SeverityError,
})
continue
}
if codeMethods["*"] {
continue
}
for method := range contractMethods {
if !codeMethods[strings.ToLower(method)] {
violations = append(violations, Violation{
Rule:     RuleMissingEndpoint,
Path:     fmt.Sprintf("paths.%s.%s", path, method),
Message:  fmt.Sprintf("%s %s is declared in the contract but no matching route was found in the code", strings.ToUpper(method), path),
Severity: SeverityError,
})
}
}
}

// Code routes not declared in contract.
for path, codeMethods := range codeRoutes {
contractMethods, exists := contractPaths[path]
if !exists {
violations = append(violations, Violation{
Rule:     RuleUndeclaredEndpoint,
Path:     "paths." + path,
Message:  fmt.Sprintf("path %q exists in the code but is not declared in the contract", path),
Severity: SeverityWarning,
})
continue
}
if codeMethods["*"] {
continue
}
for method := range codeMethods {
if _, ok := contractMethods[method]; !ok {
violations = append(violations, Violation{
Rule:     RuleUndeclaredEndpoint,
Path:     fmt.Sprintf("paths.%s.%s", path, method),
Message:  fmt.Sprintf("%s %s exists in the code but is not declared in the contract", strings.ToUpper(method), path),
Severity: SeverityWarning,
})
}
}
}

return violations, nil
}

// ExtractEndpoints returns all path+method pairs from an OpenAPI contract definition.
// Returns nil for non-OpenAPI types without error.
func ExtractEndpoints(contractType, contractDef string) ([]Endpoint, error) {
if contractType != "openapi" {
return nil, nil
}
spec, err := parseSpec(contractDef)
if err != nil {
return nil, fmt.Errorf("parse contract: %w", err)
}
paths := extractOpenAPIPaths(spec)
var endpoints []Endpoint
for path, methods := range paths {
for method := range methods {
endpoints = append(endpoints, Endpoint{
Method: strings.ToUpper(method),
Path:   path,
})
}
}
return endpoints, nil
}

// parseSpec unmarshals a YAML or JSON spec string into a generic map.
func parseSpec(content string) (map[string]any, error) {
var m map[string]any
if err := yaml.Unmarshal([]byte(content), &m); err != nil {
return nil, fmt.Errorf("parse spec: %w", err)
}
return m, nil
}

// nestedMap safely retrieves a nested map[string]any from a parent map.
func nestedMap(m map[string]any, key string) map[string]any {
v, _ := m[key].(map[string]any)
return v
}
