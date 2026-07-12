package diff

import "fmt"

// diffAsyncAPI compares two AsyncAPI specs at the channel level.
func diffAsyncAPI(contractDef string, localContent []byte) ([]Violation, error) {
	contract, err := parseSpec(contractDef)
	if err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	local, err := parseSpec(string(localContent))
	if err != nil {
		return nil, fmt.Errorf("parse local spec: %w", err)
	}

	contractChannels := extractChannels(contract)
	localChannels := extractChannels(local)

	var violations []Violation

	// Channels declared in contract but missing from local.
	for ch := range contractChannels {
		if _, ok := localChannels[ch]; !ok {
			violations = append(violations, Violation{
				Rule:     RuleMissingChannel,
				Path:     "channels." + ch,
				Message:  fmt.Sprintf("channel %q is declared in the contract but missing from the local spec", ch),
				Severity: SeverityError,
			})
		}
	}

	// Channels in local but not declared in contract.
	for ch := range localChannels {
		if _, ok := contractChannels[ch]; !ok {
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredEndpoint,
				Path:     "channels." + ch,
				Message:  fmt.Sprintf("channel %q exists in the local spec but is not declared in the contract", ch),
				Severity: SeverityWarning,
			})
		}
	}

	return violations, nil
}

// extractChannels returns the set of channel names from an AsyncAPI spec.
func extractChannels(spec map[string]any) map[string]any {
	channels := nestedMap(spec, "channels")
	if channels == nil {
		return map[string]any{}
	}
	return channels
}
