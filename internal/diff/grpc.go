package diff

import (
	"fmt"
	"regexp"
	"strings"
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
	for key, service := range contractMethods {
		if _, ok := localMethods[key]; !ok {
			// key is "ServiceName.MethodName"
			parts := strings.SplitN(key, ".", 2)
			method := parts[len(parts)-1]
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc method %q (service %q) is declared in the contract but missing from the local proto", method, service),
				Severity: SeverityError,
			})
		}
	}

	// Methods in local but not declared in contract.
	for key, service := range localMethods {
		if _, ok := contractMethods[key]; !ok {
			parts := strings.SplitN(key, ".", 2)
			method := parts[len(parts)-1]
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc method %q (service %q) exists in the local proto but is not declared in the contract", method, service),
				Severity: SeverityWarning,
			})
		}
	}

	return violations, nil
}

// extractRPCMethods returns a map of "ServiceName.MethodName" → ServiceName.
// Using a composite key prevents false negatives when two services share a
// method name. Methods found outside any service block use key "unknown.Method".
func extractRPCMethods(content string) map[string]string {
	result := make(map[string]string)

	serviceMatches := serviceRe.FindAllStringSubmatchIndex(content, -1)

	for i, sm := range serviceMatches {
		serviceName := content[sm[2]:sm[3]]

		blockStart := sm[0]
		blockEnd := len(content)
		if i+1 < len(serviceMatches) {
			blockEnd = serviceMatches[i+1][0]
		}
		block := content[blockStart:blockEnd]

		for _, mm := range rpcMethodRe.FindAllStringSubmatch(block, -1) {
			key := serviceName + "." + mm[1]
			result[key] = serviceName
		}
	}

	// Methods not inside any service block.
	if len(serviceMatches) == 0 {
		for _, mm := range rpcMethodRe.FindAllStringSubmatch(content, -1) {
			key := "unknown." + mm[1]
			result[key] = "unknown"
		}
	}

	return result
}
