package deprecation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dever-labs/catalog-drift/internal/backstage"
)

var (
	jan2025 = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	jul2025 = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	jan2026 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func makeContract(name string, lifecycle string, annotations map[string]string) backstage.Contract {
	spec := backstage.APISpec{Type: "openapi", Lifecycle: lifecycle}
	specBytes, _ := json.Marshal(spec)
	entity := backstage.Entity{
		Metadata: backstage.EntityMetadata{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: json.RawMessage(specBytes),
	}
	contract, _ := backstage.BuildContractForTest(&entity)
	return contract
}

func TestCheck_NotDeprecated(t *testing.T) {
	checker := NewChecker(Config{now: fixedNow(jul2025)})
	contract := makeContract("active-api", "production", nil)

	if v := checker.Check(contract); v != nil {
		t.Errorf("expected nil violation for active API, got %+v", v)
	}
}

func TestCheck_DeprecatedNoDatesSunset_Warning(t *testing.T) {
	checker := NewChecker(Config{now: fixedNow(jul2025)})
	contract := makeContract("old-api", "deprecated", nil)

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected a violation, got nil")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", v.Severity)
	}
	if !strings.Contains(v.Message, "old-api") {
		t.Errorf("message %q does not mention API name", v.Message)
	}
}

func TestCheck_DeprecatedBeforeSunset_Warning(t *testing.T) {
	// now = jul2025, sunset = jan2026 → still in grace period
	checker := NewChecker(Config{now: fixedNow(jul2025)})
	contract := makeContract("old-api", "deprecated", map[string]string{
		backstage.AnnotationDeprecatedSince: "2025-01-01",
		backstage.AnnotationSunsetDate:      "2026-01-01",
	})

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", v.Severity)
	}
	if v.DaysUntilSunset <= 0 {
		t.Errorf("DaysUntilSunset = %d, expected positive", v.DaysUntilSunset)
	}
	if !strings.Contains(v.Message, "2026-01-01") {
		t.Errorf("message %q should mention sunset date", v.Message)
	}
}

func TestCheck_DeprecatedPastSunset_Error(t *testing.T) {
	// now = jan2026, sunset = jul2025 → past sunset
	checker := NewChecker(Config{now: fixedNow(jan2026)})
	contract := makeContract("old-api", "deprecated", map[string]string{
		backstage.AnnotationSunsetDate: "2025-07-01",
	})

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if v.Severity != SeverityError {
		t.Errorf("severity = %v, want error", v.Severity)
	}
	if v.DaysUntilSunset >= 0 {
		t.Errorf("DaysUntilSunset = %d, expected negative (past sunset)", v.DaysUntilSunset)
	}
	if !strings.Contains(v.Message, "passed its sunset date") {
		t.Errorf("message %q should mention sunset passed", v.Message)
	}
}

func TestCheck_ErrorAfterThreshold(t *testing.T) {
	// deprecated since jan2025, threshold 90 days → error after ~apr2025
	// now = jul2025 → should be error
	checker := NewChecker(Config{
		ErrorAfter: 90 * 24 * time.Hour,
		now:        fixedNow(jul2025),
	})
	contract := makeContract("aging-api", "deprecated", map[string]string{
		backstage.AnnotationDeprecatedSince: "2025-01-01",
	})

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if v.Severity != SeverityError {
		t.Errorf("severity = %v, want error (past ErrorAfter threshold)", v.Severity)
	}
}

func TestCheck_WithinErrorAfterThreshold_Warning(t *testing.T) {
	// deprecated since jul2025, threshold 90 days → error after ~oct2025
	// now = jul2025 + 10 days → still a warning
	now := jul2025.Add(10 * 24 * time.Hour)
	checker := NewChecker(Config{
		ErrorAfter: 90 * 24 * time.Hour,
		now:        fixedNow(now),
	})
	contract := makeContract("new-deprecated-api", "deprecated", map[string]string{
		backstage.AnnotationDeprecatedSince: "2025-07-01",
	})

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning (within threshold)", v.Severity)
	}
}

func TestCheck_SunsetTakesPrecedenceOverErrorAfter(t *testing.T) {
	// sunset is in the future → should be warning even though ErrorAfter is exceeded
	checker := NewChecker(Config{
		ErrorAfter: 1 * time.Hour, // very short threshold
		now:        fixedNow(jul2025),
	})
	contract := makeContract("mixed-api", "deprecated", map[string]string{
		backstage.AnnotationDeprecatedSince: "2025-01-01",
		backstage.AnnotationSunsetDate:      "2026-01-01", // future sunset
	})

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning (sunset is future, sunset takes precedence)", v.Severity)
	}
}

func TestCheck_SuccessorIncludedInMessage(t *testing.T) {
	checker := NewChecker(Config{now: fixedNow(jul2025)})
	contract := makeContract("v1-api", "deprecated", map[string]string{
		backstage.AnnotationSuccessor: "v2-api",
	})

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if !strings.Contains(v.Message, "v2-api") {
		t.Errorf("message %q should mention successor v2-api", v.Message)
	}
}

func TestCheck_CustomNamespace_InMessage(t *testing.T) {
	checker := NewChecker(Config{now: fixedNow(jul2025)})

	spec := backstage.APISpec{Type: "openapi", Lifecycle: "deprecated"}
	specBytes, _ := json.Marshal(spec)
	entity := backstage.Entity{
		Metadata: backstage.EntityMetadata{
			Name:      "old-api",
			Namespace: "payments",
		},
		Spec: json.RawMessage(specBytes),
	}
	contract, _ := backstage.BuildContractForTest(&entity)

	v := checker.Check(contract)
	if v == nil {
		t.Fatal("expected violation")
	}
	if !strings.Contains(v.Message, "payments/old-api") {
		t.Errorf("message %q should include namespace/name", v.Message)
	}
}

func TestCheckAll_FiltersNonDeprecated(t *testing.T) {
	checker := NewChecker(Config{now: fixedNow(jul2025)})
	contracts := []backstage.Contract{
		makeContract("active", "production", nil),
		makeContract("old", "deprecated", nil),
		makeContract("also-active", "experimental", nil),
	}

	violations := checker.CheckAll(contracts)
	if len(violations) != 1 {
		t.Errorf("got %d violations, want 1", len(violations))
	}
	if violations[0].APIName != "old" {
		t.Errorf("violation API = %q, want old", violations[0].APIName)
	}
}

// ── Severity.String ───────────────────────────────────────────────────────────

func TestSeverityString_Warning(t *testing.T) {
if got := SeverityWarning.String(); got != "warning" {
t.Errorf("SeverityWarning.String() = %q, want warning", got)
}
}

func TestSeverityString_Error(t *testing.T) {
if got := SeverityError.String(); got != "error" {
t.Errorf("SeverityError.String() = %q, want error", got)
}
}

func TestSeverityString_Unknown(t *testing.T) {
if got := Severity(99).String(); got != "unknown" {
t.Errorf("Severity(99).String() = %q, want unknown", got)
}
}

// ── NewChecker zero-config ────────────────────────────────────────────────────

func TestNewChecker_ZeroConfigDoesNotPanic(t *testing.T) {
c := NewChecker(Config{})
if c == nil {
t.Fatal("expected non-nil checker")
}
}

func TestNewChecker_ZeroConfigGivesWarningForDeprecated(t *testing.T) {
	c := NewChecker(Config{})
	contract := makeContract("test-api", "deprecated", nil)
	v := c.Check(contract)
	if v == nil {
		t.Fatal("expected violation, got nil")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning (zero ErrorAfter → always warning)", v.Severity)
	}
}

// ── CheckAll ──────────────────────────────────────────────────────────────────

func TestCheckAll_MixedContracts(t *testing.T) {
	c := NewChecker(Config{})
	contracts := []backstage.Contract{
		makeContract("prod-api", "production", nil),
		makeContract("dep-api-1", "deprecated", nil),
		makeContract("dep-api-2", "deprecated", nil),
	}
	vs := c.CheckAll(contracts)
	if len(vs) != 2 {
		t.Errorf("expected 2 violations, got %d", len(vs))
	}
}

func TestCheckAll_Empty(t *testing.T) {
c := NewChecker(Config{})
if vs := c.CheckAll(nil); len(vs) != 0 {
t.Errorf("expected 0 violations, got %d", len(vs))
}
}
