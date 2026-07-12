package consumer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrometheusSource_Query(t *testing.T) {
	response := promQueryResult{
		Status: "success",
		Data: promQueryData{
			ResultType: "vector",
			Result: []promSample{
				{
					Metric: map[string]string{
						"source_service": "order-service",
						"path":           "/api/v1/payments",
						"method":         "post",
					},
					Value: [2]any{1704099600.0, "42"},
				},
				{
					Metric: map[string]string{
						"source_service": "checkout-service",
						"path":           "/api/v1/payments",
						"method":         "GET",
					},
					Value: [2]any{1704099600.0, "15"},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		query := r.URL.Query().Get("query")
		if query == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	src := NewPrometheusSource(srv.URL, "http_requests_total")
	records, err := src.Query(context.Background(), []string{"/api/v1/payments"}, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Verify record contents.
	seen := map[string]int64{}
	for _, r := range records {
		seen[r.ClientID] = r.RequestCount
	}
	if seen["order-service"] != 42 {
		t.Errorf("order-service: want 42, got %d", seen["order-service"])
	}
	if seen["checkout-service"] != 15 {
		t.Errorf("checkout-service: want 15, got %d", seen["checkout-service"])
	}
}

func TestPrometheusSource_EmptyEndpoints(t *testing.T) {
	src := NewPrometheusSource("http://localhost:9090", "")
	records, err := src.Query(context.Background(), nil, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestPrometheusSource_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	src := NewPrometheusSource(srv.URL, "http_requests_total")
	_, err := src.Query(context.Background(), []string{"/api/v1/payments"}, 30*24*time.Hour)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPrometheusSource_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promQueryResult{
			Status: "error",
			Error:  "query failed",
		})
	}))
	defer srv.Close()

	src := NewPrometheusSource(srv.URL, "http_requests_total")
	_, err := src.Query(context.Background(), []string{"/api/v1/payments"}, 30*24*time.Hour)
	if err == nil {
		t.Fatal("expected error from prometheus status=error")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * 24 * time.Hour, "30d"},
		{7 * 24 * time.Hour, "7d"},
		{2 * time.Hour, "2h"},
		{45 * time.Minute, "45m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestNewPrometheusSource_DefaultMetric(t *testing.T) {
	src := NewPrometheusSource("http://localhost:9090", "")
	if src.metric != "http_requests_total" {
		t.Errorf("expected default metric http_requests_total, got %q", src.metric)
	}
}
