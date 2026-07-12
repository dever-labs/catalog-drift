package diff

import (
	"fmt"
	"strings"
)

// diffOpenAPI compares two OpenAPI specs at the path/method and schema level.
func diffOpenAPI(contractDef string, localContent []byte) ([]Violation, error) {
	contract, err := parseSpec(contractDef)
	if err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	local, err := parseSpec(string(localContent))
	if err != nil {
		return nil, fmt.Errorf("parse local spec: %w", err)
	}

	var violations []Violation
	violations = append(violations, diffOpenAPIPaths(contract, local)...)
	return violations, nil
}

func diffOpenAPIPaths(contract, local map[string]any) []Violation {
	contractPaths := extractOpenAPIPaths(contract)
	localPaths := extractOpenAPIPaths(local)

	var violations []Violation

	// Paths in contract but missing or incomplete in local.
	for path, contractMethods := range contractPaths {
		localMethods, exists := localPaths[path]
		if !exists {
			violations = append(violations, Violation{
				Rule:     RuleMissingEndpoint,
				Path:     "paths." + path,
				Message:  fmt.Sprintf("path %q is declared in the contract but missing from the local spec", path),
				Severity: SeverityError,
			})
			continue
		}

		for method, contractOp := range contractMethods {
			localOp, ok := localMethods[method]
			if !ok {
				violations = append(violations, Violation{
					Rule:     RuleMissingEndpoint,
					Path:     fmt.Sprintf("paths.%s.%s", path, method),
					Message:  fmt.Sprintf("%s %s is declared in the contract but missing from the local spec", strings.ToUpper(method), path),
					Severity: SeverityError,
				})
				continue
			}
			// Schema-level check for this operation.
			violations = append(violations, diffOperationSchemas(path, method, contractOp, localOp)...)
		}
	}

	// Paths in local but not declared in contract.
	for path, localMethods := range localPaths {
		contractMethods, exists := contractPaths[path]
		if !exists {
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredEndpoint,
				Path:     "paths." + path,
				Message:  fmt.Sprintf("path %q exists in the local spec but is not declared in the contract", path),
				Severity: SeverityWarning,
			})
			continue
		}
		for method := range localMethods {
			if _, ok := contractMethods[method]; !ok {
				violations = append(violations, Violation{
					Rule:     RuleUndeclaredEndpoint,
					Path:     fmt.Sprintf("paths.%s.%s", path, method),
					Message:  fmt.Sprintf("%s %s exists in the local spec but is not declared in the contract", strings.ToUpper(method), path),
					Severity: SeverityWarning,
				})
			}
		}
	}

	return violations
}

// diffOperationSchemas checks request body and response schemas for a single operation.
func diffOperationSchemas(path, method string, contractOp, localOp map[string]any) []Violation {
	var violations []Violation
	basePath := fmt.Sprintf("paths.%s.%s", path, method)

	// Request body schema.
	if cs := extractInlineSchema(contractOp, "requestBody"); cs != nil {
		ls := extractInlineSchema(localOp, "requestBody")
		if ls == nil {
			ls = map[string]any{}
		}
		violations = append(violations, diffSchemas(cs, ls, basePath+".requestBody.schema")...)
	}

	// Response schemas (check all status codes).
	contractResponses, _ := contractOp["responses"].(map[string]any)
	localResponses, _ := localOp["responses"].(map[string]any)
	for status, cr := range contractResponses {
		crMap, _ := cr.(map[string]any)
		cs := extractInlineSchema(crMap, "")
		if cs == nil {
			continue
		}
		lrMap := map[string]any{}
		if lr, ok := localResponses[status]; ok {
			lrMap, _ = lr.(map[string]any)
		}
		ls := extractInlineSchema(lrMap, "")
		if ls == nil {
			ls = map[string]any{}
		}
		violations = append(violations, diffSchemas(cs, ls, fmt.Sprintf("%s.responses.%s.schema", basePath, status))...)
	}

	return violations
}

// extractInlineSchema navigates to the first application/json schema it can find.
// When key is "requestBody", it looks under requestBody.content.application/json.schema.
// When key is "" (for responses), it looks directly under content.application/json.schema.
// Returns nil when no inline schema is found or when the schema uses a $ref.
func extractInlineSchema(op map[string]any, key string) map[string]any {
	if op == nil {
		return nil
	}
	var container map[string]any
	if key != "" {
		container, _ = op[key].(map[string]any)
	} else {
		container = op
	}
	content := nestedMap(container, "content")
	mediaType := nestedMap(content, "application/json")
	schema := nestedMap(mediaType, "schema")
	if schema == nil {
		return nil
	}
	// Skip $ref schemas — we don't resolve references.
	if _, hasRef := schema["$ref"]; hasRef {
		return nil
	}
	return schema
}

// diffSchemas compares two OpenAPI schema objects and returns field-level violations.
func diffSchemas(contract, local map[string]any, basePath string) []Violation {
	var violations []Violation

	contractProps := nestedMap(contract, "properties")
	localProps := nestedMap(local, "properties")

	// Required fields in contract must exist in local.
	required, _ := contract["required"].([]any)
	for _, r := range required {
		field, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := localProps[field]; !exists {
			violations = append(violations, Violation{
				Rule:     RuleMissingField,
				Path:     basePath + ".properties." + field,
				Message:  fmt.Sprintf("required field %q is declared in the contract but missing from the local spec", field),
				Severity: SeverityError,
			})
		}
	}

	// Type mismatches for fields present in both.
	for field, cv := range contractProps {
		lv, ok := localProps[field]
		if !ok {
			continue
		}
		cm, _ := cv.(map[string]any)
		lm, _ := lv.(map[string]any)
		if cm == nil || lm == nil {
			continue
		}
		ct, _ := cm["type"].(string)
		lt, _ := lm["type"].(string)
		if ct != "" && lt != "" && ct != lt {
			violations = append(violations, Violation{
				Rule:     RuleTypeMismatch,
				Path:     basePath + ".properties." + field,
				Message:  fmt.Sprintf("field %q type changed from %q (contract) to %q (local)", field, ct, lt),
				Severity: SeverityError,
			})
		}
	}

	return violations
}

// extractOpenAPIPaths returns a map of path → method → operation for an OpenAPI spec.
func extractOpenAPIPaths(spec map[string]any) map[string]map[string]map[string]any {
	result := make(map[string]map[string]map[string]any)
	paths := nestedMap(spec, "paths")

	httpMethods := map[string]bool{
		"get": true, "post": true, "put": true, "patch": true,
		"delete": true, "head": true, "options": true,
	}

	for path, pathItem := range paths {
		pathMap, ok := pathItem.(map[string]any)
		if !ok {
			continue
		}
		methods := make(map[string]map[string]any)
		for k, v := range pathMap {
			if httpMethods[strings.ToLower(k)] {
				if op, ok := v.(map[string]any); ok {
					methods[strings.ToLower(k)] = op
				}
			}
		}
		if len(methods) > 0 {
			result[path] = methods
		}
	}
	return result
}
