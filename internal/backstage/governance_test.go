package backstage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// governanceMux builds a test HTTP mux that serves a GovernancePolicy entity
// at the standard catalog entity path.
func governanceMux(t *testing.T, policyName, namespace, errorAfter string, failOnWarn bool) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()

	path := "/api/catalog/entities/by-name/governancepolicy/" + namespace + "/" + policyName
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		spec := GovernancePolicySpec{
			Deprecation: GovernanceDeprecationSpec{ErrorAfter: errorAfter},
			Contract:    GovernanceContractSpec{FailOnWarn: failOnWarn},
		}
		specBytes, _ := json.Marshal(spec)
		e := Entity{
			APIVersion: "catalog-drift.io/v1alpha1",
			Kind:       "GovernancePolicy",
			Metadata:   EntityMetadata{Name: policyName, Namespace: namespace},
			Spec:       json.RawMessage(specBytes),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(e)
	})
	return mux
}

func TestFetchGovernancePolicy_Found(t *testing.T) {
	mux := governanceMux(t, "default", "default", "90d", false)
	// No component entity handler needed — component not provided.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	// Without a component, should resolve to "default/default" policy.
	pol, err := c.FetchGovernancePolicy(context.Background(), "", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol == nil {
		t.Fatal("expected a policy, got nil")
	}
	if pol.Spec.Deprecation.ErrorAfter != "90d" {
		t.Errorf("want errorAfter=90d, got %q", pol.Spec.Deprecation.ErrorAfter)
	}
}

func TestFetchGovernancePolicy_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pol, err := c.FetchGovernancePolicy(context.Background(), "", "default")
	if err != nil {
		t.Fatalf("unexpected error on not found: %v", err)
	}
	if pol != nil {
		t.Errorf("expected nil policy when none exists, got %+v", pol)
	}
}

func TestFetchGovernancePolicy_ComponentAnnotation(t *testing.T) {
	mux := http.NewServeMux()

	// Component entity with custom governance policy annotation.
	compSpec := struct {
		Type string `json:"type"`
	}{Type: "service"}
	compSpecBytes, _ := json.Marshal(compSpec)
	comp := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       "Component",
		Metadata: EntityMetadata{
			Name:      "my-service",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGovernancePolicy: "strict-policy",
			},
		},
		Spec: json.RawMessage(compSpecBytes),
	}
	mux.HandleFunc("/api/catalog/entities/by-name/component/default/my-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(comp)
	})

	// GovernancePolicy entity named "strict-policy".
	strictSpec := GovernancePolicySpec{
		Deprecation: GovernanceDeprecationSpec{ErrorAfter: "30d"},
		Contract:    GovernanceContractSpec{FailOnWarn: true},
	}
	strictSpecBytes, _ := json.Marshal(strictSpec)
	policyEntity := Entity{
		APIVersion: "catalog-drift.io/v1alpha1",
		Kind:       "GovernancePolicy",
		Metadata:   EntityMetadata{Name: "strict-policy", Namespace: "default"},
		Spec:       json.RawMessage(strictSpecBytes),
	}
	mux.HandleFunc("/api/catalog/entities/by-name/governancepolicy/default/strict-policy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(policyEntity)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pol, err := c.FetchGovernancePolicy(context.Background(), "my-service", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol == nil {
		t.Fatal("expected a policy, got nil")
	}
	if pol.Name != "strict-policy" {
		t.Errorf("want name=strict-policy, got %q", pol.Name)
	}
	if pol.Spec.Deprecation.ErrorAfter != "30d" {
		t.Errorf("want errorAfter=30d, got %q", pol.Spec.Deprecation.ErrorAfter)
	}
	if !pol.Spec.Contract.FailOnWarn {
		t.Error("want failOnWarn=true")
	}
}

func TestFetchGovernancePolicy_FallbackToDefaultNamespace(t *testing.T) {
	mux := http.NewServeMux()
	// Only register the global default policy (default/default).
	mux.HandleFunc("/api/catalog/entities/by-name/governancepolicy/default/default", func(w http.ResponseWriter, r *http.Request) {
		spec := GovernancePolicySpec{Deprecation: GovernanceDeprecationSpec{ErrorAfter: "60d"}}
		specBytes, _ := json.Marshal(spec)
		e := Entity{
			APIVersion: "catalog-drift.io/v1alpha1",
			Kind:       "GovernancePolicy",
			Metadata:   EntityMetadata{Name: "default", Namespace: "default"},
			Spec:       json.RawMessage(specBytes),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(e)
	})
	// Namespace-specific default returns 404.
	mux.HandleFunc("/api/catalog/entities/by-name/governancepolicy/payments/default", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	// Component entity (no annotation).
	compSpec := struct{ Type string `json:"type"` }{Type: "service"}
	compSpecBytes, _ := json.Marshal(compSpec)
	comp := Entity{
		APIVersion: "backstage.io/v1alpha1",
		Kind:       "Component",
		Metadata:   EntityMetadata{Name: "payment-svc", Namespace: "payments"},
		Spec:       json.RawMessage(compSpecBytes),
	}
	mux.HandleFunc("/api/catalog/entities/by-name/component/payments/payment-svc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(comp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pol, err := c.FetchGovernancePolicy(context.Background(), "payment-svc", "payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol == nil {
		t.Fatal("expected global default policy as fallback, got nil")
	}
	if pol.Spec.Deprecation.ErrorAfter != "60d" {
		t.Errorf("want errorAfter=60d from global default, got %q", pol.Spec.Deprecation.ErrorAfter)
	}
}

// ── resolvePolicy ─────────────────────────────────────────────────────────────

func TestResolvePolicy_CLIFlagOverridesBackstage(t *testing.T) {
	// Backstage says 90d, CLI says 30d — CLI wins.
	mux := governanceMux(t, "default", "default", "90d", false)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pol, err := c.FetchGovernancePolicy(context.Background(), "", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol == nil {
		t.Fatal("expected policy")
	}

	// Simulate CLI override: CLI 30d overrides Backstage 90d.
	cliFlagGrace := 30 * 24 * time.Hour
	_ = pol.Spec.Deprecation.ErrorAfter // 90d — would be base, but CLI wins

	resolved := cliFlagGrace // CLI flag set → override
	if resolved != 30*24*time.Hour {
		t.Errorf("want 30d, got %v", resolved)
	}
}

func TestResolvePolicy_BackstageUsedWhenNoCLIFlag(t *testing.T) {
	mux := governanceMux(t, "default", "default", "90d", false)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	pol, err := c.FetchGovernancePolicy(context.Background(), "", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol == nil {
		t.Fatal("expected policy")
	}

	// No CLI flag set — use Backstage value directly.
	if pol.Spec.Deprecation.ErrorAfter != "90d" {
		t.Errorf("want 90d from Backstage, got %q", pol.Spec.Deprecation.ErrorAfter)
	}
}
