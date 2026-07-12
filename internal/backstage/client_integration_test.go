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

// startMockly starts a Mockly server for the test, registering cleanup automatically.
func startMockly(t *testing.T) *mocklydriver.Server {
	t.Helper()
	srv, err := mocklydriver.Ensure(mocklydriver.Options{}, mocklydriver.InstallOptions{})
	if err != nil {
		t.Fatalf("start mockly: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Stop(); err != nil {
			t.Logf("stop mockly: %v", err)
		}
	})
	return srv
}

// registerMock registers a mock and returns its ID.
func registerMock(t *testing.T, srv *mocklydriver.Server, method, path string, status int, body any) string {
	t.Helper()
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal mock body: %v", err)
	}
	mock := mocklydriver.Mock{
		Request: mocklydriver.MockRequest{
			Method: method,
			Path:   path,
		},
		Response: mocklydriver.MockResponse{
			Status: status,
			Body:   string(bodyJSON),
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		},
	}
	if err := srv.AddMock(mock); err != nil {
		t.Fatalf("add mock %s %s: %v", method, path, err)
	}
	mocks, err := srv.ListMocks()
	if err != nil {
		t.Fatalf("list mocks: %v", err)
	}
	for _, m := range mocks {
		if m.Request.Method == method && m.Request.Path == path {
			return m.ID
		}
	}
	t.Fatalf("could not find registered mock for %s %s", method, path)
	return ""
}

func TestIntegration_FetchContract(t *testing.T) {
	srv := startMockly(t)

	spec := APISpec{
		Type:       "openapi",
		Lifecycle:  "production",
		Owner:      "team-a",
		Definition: "openapi: 3.0.0\ninfo:\n  title: Payments API\n  version: 1.0.0",
	}
	entity := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       "API",
		Metadata:   EntityMetadata{Name: "payments-api", Namespace: "default"},
		Spec:       mustMarshalIntegration(t, spec),
	}

	mockID := registerMock(t, srv,
		http.MethodGet,
		"/api/catalog/entities/by-name/api/default/payments-api",
		http.StatusOK,
		entity,
	)

	client := NewClient(srv.HTTPBase)
	contract, err := client.FetchContract(context.Background(), "payments-api", "")
	if err != nil {
		t.Fatalf("FetchContract: %v", err)
	}

	if contract.Entity.Metadata.Name != "payments-api" {
		t.Errorf("entity name = %q, want payments-api", contract.Entity.Metadata.Name)
	}
	if contract.APISpec.Type != "openapi" {
		t.Errorf("spec type = %q, want openapi", contract.APISpec.Type)
	}
	if contract.APISpec.Owner != "team-a" {
		t.Errorf("spec owner = %q, want team-a", contract.APISpec.Owner)
	}

	summary, err := srv.WaitForCalls(mockID, 1, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForCalls: %v", err)
	}
	if summary.Count != 1 {
		t.Errorf("call count = %d, want 1", summary.Count)
	}
}

func TestIntegration_FetchContract_WithToken(t *testing.T) {
	srv := startMockly(t)

	entity := Entity{
		Metadata: EntityMetadata{Name: "secure-api", Namespace: "default"},
		Spec:     mustMarshalIntegration(t, APISpec{Type: "openapi"}),
	}

	mockID := registerMock(t, srv,
		http.MethodGet,
		"/api/catalog/entities/by-name/api/default/secure-api",
		http.StatusOK,
		entity,
	)

	client := NewClient(srv.HTTPBase, WithToken("my-token"))
	_, err := client.FetchContract(context.Background(), "secure-api", "")
	if err != nil {
		t.Fatalf("FetchContract: %v", err)
	}

	summary, err := srv.WaitForCalls(mockID, 1, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForCalls: %v", err)
	}
	call := summary.Calls[0]
	if call.Headers["Authorization"] != "Bearer my-token" {
		t.Errorf("Authorization header = %q, want %q", call.Headers["Authorization"], "Bearer my-token")
	}
}

func TestIntegration_FetchContracts(t *testing.T) {
	srv := startMockly(t)

	component := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       "Component",
		Metadata:   EntityMetadata{Name: "order-service", Namespace: "default"},
		Spec:       mustMarshalIntegration(t, map[string]string{"type": "service"}),
		Relations: []Relation{
			{
				Type:      "providesApi",
				TargetRef: "api:default/orders-rest",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "orders-rest"},
			},
			{
				Type:      "providesApi",
				TargetRef: "api:default/orders-events",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "orders-events"},
			},
			{
				Type:      "consumesApi",
				TargetRef: "api:default/inventory-api",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "inventory-api"},
			},
		},
	}
	restAPI := Entity{
		Metadata: EntityMetadata{Name: "orders-rest", Namespace: "default"},
		Spec:     mustMarshalIntegration(t, APISpec{Type: "openapi", Definition: "openapi: 3.0.0"}),
	}
	eventsAPI := Entity{
		Metadata: EntityMetadata{Name: "orders-events", Namespace: "default"},
		Spec:     mustMarshalIntegration(t, APISpec{Type: "asyncapi", Definition: "asyncapi: 2.0.0"}),
	}

	registerMock(t, srv, http.MethodGet, "/api/catalog/entities/by-name/component/default/order-service", http.StatusOK, component)
	registerMock(t, srv, http.MethodGet, "/api/catalog/entities/by-name/api/default/orders-rest", http.StatusOK, restAPI)
	registerMock(t, srv, http.MethodGet, "/api/catalog/entities/by-name/api/default/orders-events", http.StatusOK, eventsAPI)

	client := NewClient(srv.HTTPBase)
	contracts, err := client.FetchContracts(context.Background(), "order-service", "")
	if err != nil {
		t.Fatalf("FetchContracts: %v", err)
	}

	if len(contracts) != 2 {
		t.Fatalf("got %d contracts, want 2", len(contracts))
	}

	types := map[string]bool{}
	for _, c := range contracts {
		types[c.APISpec.Type] = true
	}
	if !types["openapi"] {
		t.Error("expected an openapi contract")
	}
	if !types["asyncapi"] {
		t.Error("expected an asyncapi contract")
	}
}

func TestIntegration_FetchContracts_ComponentNotFound(t *testing.T) {
	srv := startMockly(t)

	registerMock(t, srv,
		http.MethodGet,
		"/api/catalog/entities/by-name/component/default/ghost-service",
		http.StatusNotFound,
		map[string]string{"error": "not found"},
	)

	client := NewClient(srv.HTTPBase)
	_, err := client.FetchContracts(context.Background(), "ghost-service", "")
	if err == nil {
		t.Fatal("expected error for missing component, got nil")
	}
}

func mustMarshalIntegration(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return json.RawMessage(b)
}
