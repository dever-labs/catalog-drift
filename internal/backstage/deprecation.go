package backstage

import (
	"time"
)

// Annotation keys used by catalog-drift to carry deprecation metadata.
// These are set on Backstage API entities under metadata.annotations.
const (
	AnnotationDeprecatedSince  = "catalog-drift/deprecated-since"  // RFC3339 date
	AnnotationSunsetDate       = "catalog-drift/sunset-date"        // RFC3339 date
	AnnotationDeprecationMsg   = "catalog-drift/deprecation-message"
	AnnotationSuccessor        = "catalog-drift/successor" // name of the replacement API
)

// DeprecationInfo holds deprecation metadata extracted from a Backstage entity.
// IsDeprecated is true when spec.lifecycle == "deprecated".
type DeprecationInfo struct {
	IsDeprecated    bool
	DeprecatedSince *time.Time // nil if not annotated
	SunsetDate      *time.Time // nil if not annotated
	Message         string
	Successor       string     // name of the replacement API, empty if not set
}

// deprecationFromEntity extracts deprecation metadata from a Backstage entity.
// The entity is considered deprecated when its APISpec lifecycle is "deprecated".
func deprecationFromEntity(entity Entity, spec APISpec) DeprecationInfo {
	info := DeprecationInfo{
		IsDeprecated: spec.Lifecycle == "deprecated",
	}
	if !info.IsDeprecated {
		return info
	}

	ann := entity.Metadata.Annotations
	info.DeprecatedSince = parseAnnotationDate(ann, AnnotationDeprecatedSince)
	info.SunsetDate = parseAnnotationDate(ann, AnnotationSunsetDate)
	info.Message = ann[AnnotationDeprecationMsg]
	info.Successor = ann[AnnotationSuccessor]

	return info
}

func parseAnnotationDate(ann map[string]string, key string) *time.Time {
	raw, ok := ann[key]
	if !ok || raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	return nil
}
