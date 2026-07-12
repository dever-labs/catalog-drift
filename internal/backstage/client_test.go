package backstage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// entityJSON builds a minimal Backstage entity JSON payload.
func entityJSON(t *testing.T, kind, namespace, name string, spec any, relations []Relation) string {
	t.Helper()
	specBytes, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	e := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       kind,
		Metadata:   EntityMetadata{Name: name, Namespace: namespace},
		Spec:       json.RawMessage(specBytes),
		Relations:  relations,
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal entity: %v", err)
	}
	return string(b)
}

func TestNewClient_DefaultsHTTPClient(t *testing.T) {
	c := NewClient("https://backstage.example.com")
	if c.httpClient == nil {
		t.Fatal("expected non-nil http client")
	}
	if c.baseURL != "https://backstage.example.com" {
		t.Fatalf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewClient_StripsTrailingSlash(t *testing.T) {
	c := NewClient("https://backstage.example.com///")
	if strings.HasSuffix(c.baseURL, "/") {
		t.Fatalf("baseURL should not end with slash: %s", c.baseURL)
	}
}

func TestWithToken_SetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		spec := APISpec{Type: "openapi", Definition: "openapi: 3.0.0"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Entity{
			APIVersion: "backstage.io/v1alpha1",
			Kind:       "API",
			Metadata:   EntityMetadata{Name: "my-api", Namespace: "default"},
			Spec:       mustMarshal(t, spec),
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, WithToken("super-secret"))
	_, _ = client.FetchContract(context.Background(), "my-api", "")

	if gotAuth != "Bearer super-secret" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer super-secret")
	}
}

func TestFetchContract_Happy(t *testing.T) {
	spec := APISpec{
		Type:       "openapi",
		Lifecycle:  "production",
		Owner:      "team-a",
		Definition: "openapi: 3.0.0\ninfo:\n  title: My API",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/catalog/entities/by-name/api/default/my-api" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Entity{
			APIVersion: "backstage.io/v1alpha1",
			Kind:       "API",
			Metadata:   EntityMetadata{Name: "my-api", Namespace: "default"},
			Spec:       mustMarshal(t, spec),
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	contract, err := client.FetchContract(context.Background(), "my-api", "")
	if err != nil {
		t.Fatalf("FetchContract: %v", err)
	}
	if contract.Entity.Metadata.Name != "my-api" {
		t.Errorf("entity name = %q, want %q", contract.Entity.Metadata.Name, "my-api")
	}
	if contract.APISpec.Type != "openapi" {
		t.Errorf("spec type = %q, want %q", contract.APISpec.Type, "openapi")
	}
	if contract.APISpec.Definition != spec.Definition {
		t.Errorf("spec definition mismatch")
	}
}

func TestFetchContract_ExplicitNamespace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/catalog/entities/by-name/api/my-ns/my-api" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Entity{
			Metadata: EntityMetadata{Name: "my-api", Namespace: "my-ns"},
			Spec:     mustMarshal(t, APISpec{Type: "asyncapi"}),
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	contract, err := client.FetchContract(context.Background(), "my-api", "my-ns")
	if err != nil {
		t.Fatalf("FetchContract: %v", err)
	}
	if contract.APISpec.Type != "asyncapi" {
		t.Errorf("spec type = %q, want asyncapi", contract.APISpec.Type)
	}
}

func TestFetchContract_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchContract(context.Background(), "missing-api", "")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want errors.Is(ErrNotFound)", err)
	}
}

func TestFetchContract_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchContract(context.Background(), "my-api", "")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want errors.Is(ErrUnauthorized)", err)
	}
}

func TestFetchContract_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchContract(context.Background(), "my-api", "")
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want errors.Is(ErrUnauthorized)", err)
	}
}

func TestFetchContract_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchContract(context.Background(), "my-api", "")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want to mention status 500", err.Error())
	}
}

func TestFetchContracts_Happy(t *testing.T) {
	apiSpecA := APISpec{Type: "openapi", Definition: "openapi: 3.0.0"}
	apiSpecB := APISpec{Type: "asyncapi", Definition: "asyncapi: 2.0.0"}

	component := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       "Component",
		Metadata:   EntityMetadata{Name: "my-svc", Namespace: "default"},
		Spec:       mustMarshal(t, map[string]string{"type": "service"}),
		Relations: []Relation{
			{
				Type:      "providesApi",
				TargetRef: "api:default/api-a",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "api-a"},
			},
			{
				Type:      "providesApi",
				TargetRef: "api:default/api-b",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "api-b"},
			},
			// consumesApi should be ignored
			{
				Type:      "consumesApi",
				TargetRef: "api:default/external-api",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "external-api"},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/catalog/entities/by-name/component/default/my-svc":
			json.NewEncoder(w).Encode(component)
		case "/api/catalog/entities/by-name/api/default/api-a":
			json.NewEncoder(w).Encode(Entity{
				Metadata: EntityMetadata{Name: "api-a", Namespace: "default"},
				Spec:     mustMarshal(t, apiSpecA),
			})
		case "/api/catalog/entities/by-name/api/default/api-b":
			json.NewEncoder(w).Encode(Entity{
				Metadata: EntityMetadata{Name: "api-b", Namespace: "default"},
				Spec:     mustMarshal(t, apiSpecB),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	contracts, err := client.FetchContracts(context.Background(), "my-svc", "")
	if err != nil {
		t.Fatalf("FetchContracts: %v", err)
	}
	if len(contracts) != 2 {
		t.Fatalf("got %d contracts, want 2", len(contracts))
	}
	if contracts[0].APISpec.Type != "openapi" {
		t.Errorf("contracts[0].Type = %q, want openapi", contracts[0].APISpec.Type)
	}
	if contracts[1].APISpec.Type != "asyncapi" {
		t.Errorf("contracts[1].Type = %q, want asyncapi", contracts[1].APISpec.Type)
	}
}

func TestFetchContracts_NoProvidedAPIs(t *testing.T) {
	component := Entity{
		Metadata:  EntityMetadata{Name: "my-svc", Namespace: "default"},
		Spec:      mustMarshal(t, map[string]string{"type": "service"}),
		Relations: []Relation{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(component)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	contracts, err := client.FetchContracts(context.Background(), "my-svc", "")
	if err != nil {
		t.Fatalf("FetchContracts: %v", err)
	}
	if len(contracts) != 0 {
		t.Errorf("got %d contracts, want 0", len(contracts))
	}
}

func TestFetchContracts_ComponentNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchContracts(context.Background(), "ghost", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want errors.Is(ErrNotFound)", err)
	}
}

func TestFetchContracts_APIEntityNotFound(t *testing.T) {
	component := Entity{
		Metadata: EntityMetadata{Name: "my-svc", Namespace: "default"},
		Spec:     mustMarshal(t, map[string]string{}),
		Relations: []Relation{
			{
				Type:      "providesApi",
				TargetRef: "api:default/missing-api",
				Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "missing-api"},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "my-svc") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(component)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.FetchContracts(context.Background(), "my-svc", "")
	if err == nil {
		t.Fatal("expected error for missing API entity, got nil")
	}
	if !strings.Contains(err.Error(), "fetch api") {
		t.Errorf("error = %q, want to mention 'fetch api'", err.Error())
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return json.RawMessage(b)
}

func TestParseEntityRef_FullRef(t *testing.T) {
	got := parseEntityRef("api:default/my-api", "component", "fallback-ns")
	if got.Kind != "api" || got.Namespace != "default" || got.Name != "my-api" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestParseEntityRef_NoKind(t *testing.T) {
	got := parseEntityRef("default/my-api", "api", "fallback-ns")
	if got.Kind != "api" || got.Namespace != "default" || got.Name != "my-api" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestParseEntityRef_NameOnly(t *testing.T) {
	got := parseEntityRef("my-api", "api", "default")
	if got.Kind != "api" || got.Namespace != "default" || got.Name != "my-api" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestParseEntityRef_KindLowercased(t *testing.T) {
	got := parseEntityRef("API:default/my-api", "component", "fallback")
	if got.Kind != "api" {
		t.Errorf("kind should be lowercase, got %q", got.Kind)
	}
}

func TestResolveTarget_UsesTargetWhenPopulated(t *testing.T) {
	rel := Relation{
		TargetRef: "api:default/from-ref",
		Target:    RelationTarget{Kind: "api", Namespace: "default", Name: "from-target"},
	}
	got := resolveTarget(rel, "component", "fallback")
	if got.Name != "from-target" {
		t.Errorf("should prefer Target when populated, got name=%q", got.Name)
	}
}

func TestResolveTarget_FallsBackToTargetRef(t *testing.T) {
	rel := Relation{
		TargetRef: "api:payments/orders-api",
		Target:    RelationTarget{}, // empty — older Backstage
	}
	got := resolveTarget(rel, "api", "default")
	if got.Kind != "api" || got.Namespace != "payments" || got.Name != "orders-api" {
		t.Errorf("fallback parse incorrect: %+v", got)
	}
}

// TestFetchContracts_TargetRefFallback verifies that FetchContracts works when
// Backstage returns relations with targetRef only (no target sub-object).
func TestFetchContracts_TargetRefFallback(t *testing.T) {
	apiSpec := APISpec{Type: "openapi", Definition: "openapi: 3.0.0"}

	component := Entity{
		Metadata: EntityMetadata{Name: "my-svc", Namespace: "default"},
		Spec:     mustMarshal(t, map[string]string{"type": "service"}),
		Relations: []Relation{
			{
				Type:      "providesApi",
				TargetRef: "api:default/my-api",
				// Target intentionally empty — simulates older Backstage response
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/catalog/entities/by-name/component/default/my-svc":
			json.NewEncoder(w).Encode(component)
		case "/api/catalog/entities/by-name/api/default/my-api":
			json.NewEncoder(w).Encode(Entity{
				Metadata: EntityMetadata{Name: "my-api", Namespace: "default"},
				Spec:     mustMarshal(t, apiSpec),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	contracts, err := client.FetchContracts(context.Background(), "my-svc", "")
	if err != nil {
		t.Fatalf("FetchContracts: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("got %d contracts, want 1", len(contracts))
	}
	if contracts[0].APISpec.Type != "openapi" {
		t.Errorf("spec type = %q, want openapi", contracts[0].APISpec.Type)
	}
}
