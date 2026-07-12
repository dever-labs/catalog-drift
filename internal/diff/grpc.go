package diff

import (
	"fmt"
	"regexp"
)

var (
	rpcMethodRe = regexp.MustCompile(`\brpc\s+(\w+)\s*\(`)
	serviceRe   = regexp.MustCompile(`\bservice\s+(\w+)\s*\{`)
)

// diffGRPC compares two proto definitions at the rpc method level using
// a lightweight regex approach (no full proto parser required).
func diffGRPC(contractDef string, localContent []byte) ([]Violation, error) {
	contractMethods := extractRPCMethods(contractDef)
	localMethods := extractRPCMethods(string(localContent))

	var violations []Violation

	// Methods declared in contract but missing from local.
	for method, service := range contractMethods {
		if _, ok := localMethods[method]; !ok {
			path := service + "." + method
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     path,
				Message:  fmt.Sprintf("rpc method %q (service %q) is declared in the contract but missing from the local proto", method, service),
				Severity: SeverityError,
			})
		}
	}

	// Methods in local but not declared in contract.
	for method, service := range localMethods {
		if _, ok := contractMethods[method]; !ok {
			path := service + "." + method
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredRPCMethod,
				Path:     path,
				Message:  fmt.Sprintf("rpc method %q (service %q) exists in the local proto but is not declared in the contract", method, service),
				Severity: SeverityWarning,
			})
		}
	}

	return violations, nil
}

// extractRPCMethods returns a map of rpc-method-name → service-name.
// When a method appears outside a service block, the service name is "unknown".
func extractRPCMethods(content string) map[string]string {
	result := make(map[string]string)

	// Find service blocks: capture service name and the methods within.
	serviceMatches := serviceRe.FindAllStringSubmatchIndex(content, -1)

	for i, sm := range serviceMatches {
		serviceName := content[sm[2]:sm[3]]

		// Determine the extent of this service block (up to the next service or end).
		blockStart := sm[0]
		blockEnd := len(content)
		if i+1 < len(serviceMatches) {
			blockEnd = serviceMatches[i+1][0]
		}
		block := content[blockStart:blockEnd]

		for _, mm := range rpcMethodRe.FindAllStringSubmatch(block, -1) {
			result[mm[1]] = serviceName
		}
	}

	// Methods not inside any service block.
	if len(serviceMatches) == 0 {
		for _, mm := range rpcMethodRe.FindAllStringSubmatch(content, -1) {
			result[mm[1]] = "unknown"
		}
	}

	return result
}
