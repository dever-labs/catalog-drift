//go:build integration

package backstage

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	mocklydriver "github.com/dever-labs/mockly/clients/go"
)

func TestIntegration_FetchContract_DeprecatedWithSunset(t *testing.T) {
	srv := startMockly(t)

	sunset := time.Now().Add(30 * 24 * time.Hour).Format("2006-01-02")
	entity := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       "API",
		Metadata: EntityMetadata{
			Name:      "legacy-payments",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationDeprecatedSince: time.Now().Add(-60 * 24 * time.Hour).Format("2006-01-02"),
				AnnotationSunsetDate:      sunset,
				AnnotationDeprecationMsg:  "Use payments-v2",
				AnnotationSuccessor:       "payments-v2",
			},
		},
		Spec: mustRawJSON(t, APISpec{
			Type:      "openapi",
			Lifecycle: "deprecated",
		}),
	}

	if err := srv.AddMock(mocklydriver.Mock{
		Request:  mocklydriver.MockRequest{Method: http.MethodGet, Path: "/api/catalog/entities/by-name/api/default/legacy-payments"},
		Response: mocklydriver.MockResponse{Status: http.StatusOK, Body: mustJSONString(t, entity), Headers: map[string]string{"Content-Type": "application/json"}},
	}); err != nil {
		t.Fatalf("AddMock: %v", err)
	}

	client := NewClient(srv.HTTPBase)
	contract, err := client.FetchContract(context.Background(), "legacy-payments", "")
	if err != nil {
		t.Fatalf("FetchContract: %v", err)
	}

	if !contract.Deprecation.IsDeprecated {
		t.Error("expected deprecated contract")
	}
	if contract.Deprecation.SunsetDate == nil {
		t.Fatal("expected non-nil SunsetDate")
	}
	if contract.Deprecation.Successor != "payments-v2" {
		t.Errorf("Successor = %q, want payments-v2", contract.Deprecation.Successor)
	}
	if contract.Deprecation.DeprecatedSince == nil {
		t.Error("expected non-nil DeprecatedSince")
	}
}

func TestIntegration_FetchContract_ActiveAPINotDeprecated(t *testing.T) {
	srv := startMockly(t)

	entity := Entity{
		Metadata: EntityMetadata{Name: "active-api", Namespace: "default"},
		Spec:     mustRawJSON(t, APISpec{Type: "openapi", Lifecycle: "production"}),
	}

	if err := srv.AddMock(mocklydriver.Mock{
		Request:  mocklydriver.MockRequest{Method: http.MethodGet, Path: "/api/catalog/entities/by-name/api/default/active-api"},
		Response: mocklydriver.MockResponse{Status: http.StatusOK, Body: mustJSONString(t, entity), Headers: map[string]string{"Content-Type": "application/json"}},
	}); err != nil {
		t.Fatalf("AddMock: %v", err)
	}

	client := NewClient(srv.HTTPBase)
	contract, err := client.FetchContract(context.Background(), "active-api", "")
	if err != nil {
		t.Fatalf("FetchContract: %v", err)
	}

	if contract.Deprecation.IsDeprecated {
		t.Error("expected non-deprecated contract for lifecycle=production")
	}
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return json.RawMessage(b)
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}
