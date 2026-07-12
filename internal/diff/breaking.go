package diff

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker"
	oasdiff "github.com/oasdiff/oasdiff/diff"
)

// diffBreakingOpenAPI detects breaking changes between an old OpenAPI spec
// (baseline) and a new one (proposed) using oasdiff.
func diffBreakingOpenAPI(oldDef string, newContent []byte) ([]Violation, error) {
	loader := openapi3.NewLoader()

	oldT, err := loader.LoadFromData([]byte(oldDef))
	if err != nil {
		return nil, fmt.Errorf("load old spec: %w", err)
	}

	newT, err := loader.LoadFromData(newContent)
	if err != nil {
		return nil, fmt.Errorf("load new spec: %w", err)
	}

	diffReport, err := oasdiff.Get(oasdiff.NewConfig(), oldT, newT)
	if err != nil {
		return nil, fmt.Errorf("diff openapi specs: %w", err)
	}

	operationsSources := oasdiff.OperationsSourcesMap{}
	changes := checker.CheckBackwardCompatibility(
		checker.NewConfig(checker.GetAllChecks()),
		diffReport,
		&operationsSources,
	)
	localizer := checker.NewLocalizer("en")

	violations := make([]Violation, 0, len(changes))
	for _, c := range changes {
		var severity Severity
		switch c.GetLevel() {
		case checker.ERR:
			severity = SeverityError
		case checker.WARN:
			severity = SeverityWarning
		default:
			continue
		}

		violations = append(violations, Violation{
			Rule:     RuleType(c.GetId()),
			Path:     c.GetPath(),
			Message:  c.GetUncolorizedText(localizer),
			Severity: severity,
		})
	}

	return violations, nil
}

// ── AsyncAPI breaking diff ────────────────────────────────────────────────────

func diffBreakingAsyncAPI(oldDef string, newContent []byte) ([]Violation, error) {
	oldSpec, err := parseSpec(oldDef)
	if err != nil {
		return nil, fmt.Errorf("parse old spec: %w", err)
	}
	newSpec, err := parseSpec(string(newContent))
	if err != nil {
		return nil, fmt.Errorf("parse new spec: %w", err)
	}

	oldChannels := extractChannels(oldSpec)
	newChannels := extractChannels(newSpec)

	var vs []Violation
	for ch := range oldChannels {
		if _, ok := newChannels[ch]; !ok {
			vs = append(vs, Violation{
				Rule:     RuleMissingChannel,
				Path:     "channels." + ch,
				Message:  fmt.Sprintf("channel %q was removed — existing consumers will break", ch),
				Severity: SeverityError,
			})
		}
	}
	return vs, nil
}

// ── gRPC breaking diff ────────────────────────────────────────────────────────

// diffBreakingGRPC detects breaking changes between an old (baseline) and new
// proto definition. Delegates to diffGRPC — the same proto-to-proto comparison
// used for contract drift, but here old=contract and new=local.
func diffBreakingGRPC(oldDef string, newContent []byte) ([]Violation, error) {
	return diffGRPC(oldDef, newContent)
}
