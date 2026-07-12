package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ConsumerSource is a pluggable backend for querying consumer access records.
type ConsumerSource interface {
	Query(ctx context.Context, endpoints []string, lookback time.Duration) ([]Record, error)
}

// PrometheusSource queries the Prometheus HTTP API for consumer records.
type PrometheusSource struct {
	baseURL string
	metric  string
	client  *http.Client
}

// NewPrometheusSource creates a Prometheus-backed ConsumerSource.
// baseURL should be the Prometheus base URL (e.g. https://prometheus.example.com).
// metric is the counter metric name (default: http_requests_total).
func NewPrometheusSource(baseURL, metric string) *PrometheusSource {
	if metric == "" {
		metric = "http_requests_total"
	}
	return &PrometheusSource{
		baseURL: strings.TrimRight(baseURL, "/"),
		metric:  metric,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Query returns consumer records for the given endpoints over the lookback window.
// The PromQL query aggregates request counts grouped by source_service label.
func (p *PrometheusSource) Query(ctx context.Context, endpoints []string, lookback time.Duration) ([]Record, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}

	// Build a regex alternation for the path filter.
	pathPattern := strings.Join(quotePaths(endpoints), "|")
	lookbackStr := formatDuration(lookback)

	query := fmt.Sprintf(
		`sum by (source_service, path, method) (increase(%s{path=~"%s"}[%s]))`,
		p.metric, pathPattern, lookbackStr,
	)

	records, err := p.execQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("prometheus query: %w", err)
	}
	return records, nil
}

func (p *PrometheusSource) execQuery(ctx context.Context, query string) ([]Record, error) {
	u := p.baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned status %d", resp.StatusCode)
	}

	var result promQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus query error: %s", result.Error)
	}

	return result.toRecords(), nil
}

// ── Prometheus response types ─────────────────────────────────────────────────

type promQueryResult struct {
	Status string        `json:"status"`
	Error  string        `json:"error,omitempty"`
	Data   promQueryData `json:"data"`
}

type promQueryData struct {
	ResultType string        `json:"resultType"`
	Result     []promSample  `json:"result"`
}

type promSample struct {
	Metric map[string]string `json:"metric"`
	Value  [2]any            `json:"value"` // [timestamp, value]
}

func (r *promQueryResult) toRecords() []Record {
	var records []Record
	for _, s := range r.Data.Result {
		clientID := s.Metric["source_service"]
		if clientID == "" {
			clientID = s.Metric["client"]
			if clientID == "" {
				clientID = "unknown"
			}
		}
		path := s.Metric["path"]
		method := strings.ToUpper(s.Metric["method"])

		var count int64
		if valStr, ok := s.Value[1].(string); ok {
			fmt.Sscanf(valStr, "%d", &count)
		}

		records = append(records, Record{
			ClientID:     clientID,
			Method:       method,
			Path:         path,
			RequestCount: count,
			LastSeen:     time.Now(), // Prometheus doesn't provide last-seen directly
		})
	}
	return records
}

// ── helpers ───────────────────────────────────────────────────────────────────

// quotePaths escapes path strings for use in a Prometheus regex.
func quotePaths(paths []string) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		// Escape regex metacharacters in paths.
		result[i] = url.PathEscape(p)
	}
	return result
}

// formatDuration converts a time.Duration to a Prometheus range selector like "30d".
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
