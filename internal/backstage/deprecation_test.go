package backstage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeprecationFromEntity_NotDeprecated(t *testing.T) {
	entity := Entity{Metadata: EntityMetadata{Name: "my-api"}}
	spec := APISpec{Lifecycle: "production"}

	info := deprecationFromEntity(entity, spec)

	if info.IsDeprecated {
		t.Error("expected IsDeprecated=false for lifecycle=production")
	}
	if info.SunsetDate != nil || info.DeprecatedSince != nil {
		t.Error("expected nil dates for non-deprecated entity")
	}
}

func TestDeprecationFromEntity_DeprecatedNoAnnotations(t *testing.T) {
	entity := Entity{Metadata: EntityMetadata{Name: "old-api"}}
	spec := APISpec{Lifecycle: "deprecated"}

	info := deprecationFromEntity(entity, spec)

	if !info.IsDeprecated {
		t.Error("expected IsDeprecated=true for lifecycle=deprecated")
	}
	if info.DeprecatedSince != nil {
		t.Error("expected nil DeprecatedSince when annotation is absent")
	}
	if info.SunsetDate != nil {
		t.Error("expected nil SunsetDate when annotation is absent")
	}
	if info.Message != "" || info.Successor != "" {
		t.Error("expected empty message and successor when annotations are absent")
	}
}

func TestDeprecationFromEntity_FullAnnotations(t *testing.T) {
	entity := Entity{
		Metadata: EntityMetadata{
			Name: "old-api",
			Annotations: map[string]string{
				AnnotationDeprecatedSince: "2025-01-15",
				AnnotationSunsetDate:      "2026-01-15",
				AnnotationDeprecationMsg:  "Migrate to v2-api",
				AnnotationSuccessor:       "v2-api",
			},
		},
	}
	spec := APISpec{Lifecycle: "deprecated"}

	info := deprecationFromEntity(entity, spec)

	if !info.IsDeprecated {
		t.Fatal("expected IsDeprecated=true")
	}
	if info.DeprecatedSince == nil {
		t.Fatal("expected non-nil DeprecatedSince")
	}
	if info.DeprecatedSince.Year() != 2025 || info.DeprecatedSince.Month() != 1 || info.DeprecatedSince.Day() != 15 {
		t.Errorf("DeprecatedSince = %v, want 2025-01-15", info.DeprecatedSince)
	}
	if info.SunsetDate == nil {
		t.Fatal("expected non-nil SunsetDate")
	}
	if info.SunsetDate.Year() != 2026 {
		t.Errorf("SunsetDate year = %d, want 2026", info.SunsetDate.Year())
	}
	if info.Message != "Migrate to v2-api" {
		t.Errorf("Message = %q, want %q", info.Message, "Migrate to v2-api")
	}
	if info.Successor != "v2-api" {
		t.Errorf("Successor = %q, want %q", info.Successor, "v2-api")
	}
}

func TestDeprecationFromEntity_RFC3339Date(t *testing.T) {
	entity := Entity{
		Metadata: EntityMetadata{
			Annotations: map[string]string{
				AnnotationDeprecatedSince: "2025-06-01T00:00:00Z",
				AnnotationSunsetDate:      "2025-12-31T23:59:59Z",
			},
		},
	}
	spec := APISpec{Lifecycle: "deprecated"}

	info := deprecationFromEntity(entity, spec)

	if info.DeprecatedSince == nil || info.DeprecatedSince.Year() != 2025 {
		t.Errorf("DeprecatedSince not parsed from RFC3339: %v", info.DeprecatedSince)
	}
	if info.SunsetDate == nil || info.SunsetDate.Year() != 2025 {
		t.Errorf("SunsetDate not parsed from RFC3339: %v", info.SunsetDate)
	}
}

func TestDeprecationFromEntity_MalformedDate(t *testing.T) {
	entity := Entity{
		Metadata: EntityMetadata{
			Annotations: map[string]string{
				AnnotationDeprecatedSince: "not-a-date",
				AnnotationSunsetDate:      "also-bad",
			},
		},
	}
	spec := APISpec{Lifecycle: "deprecated"}

	info := deprecationFromEntity(entity, spec)

	if info.DeprecatedSince != nil {
		t.Errorf("expected nil DeprecatedSince for malformed date, got %v", info.DeprecatedSince)
	}
	if info.SunsetDate != nil {
		t.Errorf("expected nil SunsetDate for malformed date, got %v", info.SunsetDate)
	}
}

func TestDeprecationFromEntity_AnnotationsIgnoredWhenNotDeprecated(t *testing.T) {
	entity := Entity{
		Metadata: EntityMetadata{
			Annotations: map[string]string{
				AnnotationDeprecatedSince: "2025-01-01",
				AnnotationSunsetDate:      "2026-01-01",
			},
		},
	}
	spec := APISpec{Lifecycle: "production"}

	info := deprecationFromEntity(entity, spec)

	if info.IsDeprecated {
		t.Error("lifecycle=production should not be deprecated regardless of annotations")
	}
	if info.DeprecatedSince != nil || info.SunsetDate != nil {
		t.Error("dates should not be populated for non-deprecated entity")
	}
}

// TestBuildContract_PopulatesDeprecation verifies the full pipeline from entity → Contract.
func TestBuildContract_PopulatesDeprecation(t *testing.T) {
	sunset := "2026-06-30"
	spec := APISpec{
		Type:      "openapi",
		Lifecycle: "deprecated",
	}
	specBytes, _ := json.Marshal(spec)
	entity := Entity{
		Metadata: EntityMetadata{
			Name: "legacy-api",
			Annotations: map[string]string{
				AnnotationSunsetDate:     sunset,
				AnnotationDeprecationMsg: "Use new-api",
				AnnotationSuccessor:      "new-api",
			},
		},
		Spec: json.RawMessage(specBytes),
	}

	contract, err := buildContract(&entity)
	if err != nil {
		t.Fatalf("buildContract: %v", err)
	}

	if !contract.Deprecation.IsDeprecated {
		t.Error("expected contract to be deprecated")
	}
	if contract.Deprecation.SunsetDate == nil {
		t.Fatal("expected non-nil SunsetDate")
	}
	want := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if !contract.Deprecation.SunsetDate.Equal(want) {
		t.Errorf("SunsetDate = %v, want %v", contract.Deprecation.SunsetDate, want)
	}
	if contract.Deprecation.Successor != "new-api" {
		t.Errorf("Successor = %q, want new-api", contract.Deprecation.Successor)
	}
}
