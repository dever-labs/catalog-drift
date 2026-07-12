package consumer

import (
	"os"
	"strings"
	"testing"
	"time"
)

var deprecatedSet = map[string]struct{}{
	"/api/v1/users":  {},
	"/api/v1/orders": {},
}

// ── Nginx ─────────────────────────────────────────────────────────────────────

const nginxLines = `
{"time_local":"01/Jan/2024:10:00:00 +0000","remote_addr":"10.0.1.5","request":"GET /api/v1/users HTTP/1.1","status":"200"}
{"time_local":"01/Jan/2024:10:00:01 +0000","remote_addr":"10.0.1.5","request":"GET /api/v1/users HTTP/1.1","status":"200"}
{"time_local":"01/Jan/2024:10:00:02 +0000","remote_addr":"10.0.1.6","request":"POST /api/v1/orders HTTP/1.1","status":"201"}
{"time_local":"01/Jan/2024:10:00:03 +0000","remote_addr":"10.0.1.5","request":"GET /api/v2/users HTTP/1.1","status":"200"}
`

func TestNginx_AggregatesRequests(t *testing.T) {
	records, err := New(FormatNginx).IngestReader(strings.NewReader(nginxLines), deprecatedSet)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (v2 path filtered out)", len(records))
	}
	// Find the /api/v1/users record
	var usersRec *Record
	for i, r := range records {
		if r.Path == "/api/v1/users" {
			usersRec = &records[i]
		}
	}
	if usersRec == nil {
		t.Fatal("missing /api/v1/users record")
	}
	if usersRec.RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", usersRec.RequestCount)
	}
	if usersRec.ClientID != "10.0.1.5" {
		t.Errorf("ClientID = %q, want 10.0.1.5", usersRec.ClientID)
	}
}

func TestNginx_FiltersToDeprecatedPaths(t *testing.T) {
	records, err := New(FormatNginx).IngestReader(strings.NewReader(nginxLines), deprecatedSet)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	for _, r := range records {
		if _, ok := deprecatedSet[r.Path]; !ok {
			t.Errorf("got non-deprecated path %q in results", r.Path)
		}
	}
}

func TestNginx_NoFilter_ReturnsAll(t *testing.T) {
	records, err := New(FormatNginx).IngestReader(strings.NewReader(nginxLines), nil)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	if len(records) != 3 { // 3 unique client|method|path combos
		t.Errorf("got %d records without filter, want 3", len(records))
	}
}

// ── Envoy ─────────────────────────────────────────────────────────────────────

const envoyLines = `
{"start_time":"2024-01-01T10:00:00.000Z","method":"GET","path":"/api/v1/users","upstream_cluster":"order-service","response_code":200}
{"start_time":"2024-01-01T10:00:01.000Z","method":"DELETE","path":"/api/v1/orders","upstream_cluster":"billing-service","response_code":204}
`

func TestEnvoy_ParsesFields(t *testing.T) {
	records, err := New(FormatEnvoy).IngestReader(strings.NewReader(envoyLines), deprecatedSet)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	var ordersRec *Record
	for i, r := range records {
		if r.Path == "/api/v1/orders" {
			ordersRec = &records[i]
		}
	}
	if ordersRec == nil {
		t.Fatal("missing /api/v1/orders record")
	}
	if ordersRec.ClientID != "billing-service" {
		t.Errorf("ClientID = %q, want billing-service", ordersRec.ClientID)
	}
	if ordersRec.Method != "DELETE" {
		t.Errorf("Method = %q, want DELETE", ordersRec.Method)
	}
}

// ── Kong ──────────────────────────────────────────────────────────────────────

const kongLines = `
{"request":{"uri":"/api/v1/users","method":"GET","headers":{"x-consumer-username":"frontend-svc"}},"client_ip":"10.0.1.1","response":{"status":200},"started_at":1704099600000}
{"request":{"uri":"/api/v1/users","method":"GET","headers":{"x-consumer-username":"frontend-svc"}},"client_ip":"10.0.1.1","response":{"status":200},"started_at":1704099700000}
`

func TestKong_UsesConsumerUsername(t *testing.T) {
	records, err := New(FormatKong).IngestReader(strings.NewReader(kongLines), deprecatedSet)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1 (aggregated)", len(records))
	}
	if records[0].ClientID != "frontend-svc" {
		t.Errorf("ClientID = %q, want frontend-svc", records[0].ClientID)
	}
	if records[0].RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", records[0].RequestCount)
	}
}

func TestKong_LastSeenIsLatest(t *testing.T) {
	records, err := New(FormatKong).IngestReader(strings.NewReader(kongLines), deprecatedSet)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("no records")
	}
	expected := time.UnixMilli(1704099700000)
	if !records[0].LastSeen.Equal(expected) {
		t.Errorf("LastSeen = %v, want %v", records[0].LastSeen, expected)
	}
}

// ── Prometheus ────────────────────────────────────────────────────────────────

const promLines = `
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",path="/api/v1/users",consumer="svc-a"} 150 1704099600000
http_requests_total{method="POST",path="/api/v1/orders",consumer="svc-b"} 42 1704099600000
http_requests_total{method="GET",path="/api/v2/users",consumer="svc-a"} 500 1704099600000
`

func TestPrometheus_ParsesMetrics(t *testing.T) {
	records, err := New(FormatPrometheus).IngestReader(strings.NewReader(promLines), deprecatedSet)
	if err != nil {
		t.Fatalf("IngestReader: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (v2 filtered)", len(records))
	}
	var usersRec *Record
	for i, r := range records {
		if r.Path == "/api/v1/users" {
			usersRec = &records[i]
		}
	}
	if usersRec == nil {
		t.Fatal("missing /api/v1/users record")
	}
	if usersRec.RequestCount != 150 {
		t.Errorf("RequestCount = %d, want 150", usersRec.RequestCount)
	}
	if usersRec.ClientID != "svc-a" {
		t.Errorf("ClientID = %q, want svc-a", usersRec.ClientID)
	}
}

func TestParseLogFormat_Valid(t *testing.T) {
	for _, f := range []string{"nginx", "envoy", "kong", "prometheus", "NGINX"} {
		_, err := ParseLogFormat(f)
		if err != nil {
			t.Errorf("ParseLogFormat(%q) unexpected error: %v", f, err)
		}
	}
}

func TestParseLogFormat_Invalid(t *testing.T) {
	_, err := ParseLogFormat("datadog")
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

// ── IngestFile ────────────────────────────────────────────────────────────────

func TestIngestFile_ReadsFromDisk(t *testing.T) {
content := `{"time_local":"01/Jan/2024:10:00:00 +0000","remote_addr":"1.2.3.4","request":"GET /api/v1/users HTTP/1.1","status":"200"}`
f, err := os.CreateTemp("", "access-*.log")
if err != nil {
t.Fatal(err)
}
defer os.Remove(f.Name())
if _, err := f.WriteString(content); err != nil {
t.Fatal(err)
}
f.Close()

records, err := New(FormatNginx).IngestFile(f.Name(), deprecatedSet)
if err != nil {
t.Fatalf("IngestFile: %v", err)
}
if len(records) != 1 {
t.Fatalf("expected 1 record, got %d", len(records))
}
if records[0].Path != "/api/v1/users" {
t.Errorf("path = %q, want /api/v1/users", records[0].Path)
}
}

func TestIngestFile_NonExistentFile(t *testing.T) {
_, err := New(FormatNginx).IngestFile("/no/such/file.log", deprecatedSet)
if err == nil {
t.Error("expected error for non-existent file")
}
}

// ── parseNginx edge cases ─────────────────────────────────────────────────────

func TestNginx_UsesXForwardedFor(t *testing.T) {
line := `{"time_local":"01/Jan/2024:10:00:00 +0000","remote_addr":"10.0.0.1","http_x_forwarded_for":"203.0.113.5, 10.0.0.1","request":"GET /api/v1/users HTTP/1.1","status":"200"}`
records, err := New(FormatNginx).IngestReader(strings.NewReader(line), deprecatedSet)
if err != nil {
t.Fatalf("IngestReader: %v", err)
}
if len(records) == 0 {
t.Fatal("expected a record")
}
if records[0].ClientID != "203.0.113.5" {
t.Errorf("clientID = %q, want 203.0.113.5 (first XFF entry)", records[0].ClientID)
}
}

func TestNginx_InvalidJSON_Skipped(t *testing.T) {
lines := "not-json\n" +
`{"time_local":"01/Jan/2024:10:00:00 +0000","remote_addr":"1.2.3.4","request":"GET /api/v1/users HTTP/1.1","status":"200"}` + "\n"
records, err := New(FormatNginx).IngestReader(strings.NewReader(lines), deprecatedSet)
if err != nil {
t.Fatalf("IngestReader: %v", err)
}
// The valid line should still produce a record.
if len(records) != 1 {
t.Errorf("expected 1 record after skipping invalid JSON, got %d", len(records))
}
}

// ── parseEnvoy edge cases ─────────────────────────────────────────────────────

func TestEnvoy_FallsBackToDownstreamAddr(t *testing.T) {
line := `{"start_time":"2024-01-01T10:00:00Z","method":"GET","path":"/api/v1/users","downstream_remote_address":"10.0.1.9:12345"}`
records, err := New(FormatEnvoy).IngestReader(strings.NewReader(line), deprecatedSet)
if err != nil {
t.Fatalf("IngestReader: %v", err)
}
if len(records) == 0 {
t.Fatal("expected a record")
}
if records[0].ClientID != "10.0.1.9:12345" {
t.Errorf("clientID = %q, want 10.0.1.9:12345", records[0].ClientID)
}
}

// ── splitRequest edge cases ───────────────────────────────────────────────────

func TestSplitRequest_Valid(t *testing.T) {
m, p := splitRequest("DELETE /api/v1/orders HTTP/1.1")
if m != "DELETE" || p != "/api/v1/orders" {
t.Errorf("got (%q, %q), want (DELETE, /api/v1/orders)", m, p)
}
}

func TestSplitRequest_SingleToken(t *testing.T) {
// Only one token — returns empty method and the whole string as path
m, p := splitRequest("/only-path")
if m != "" {
t.Errorf("method = %q, want empty", m)
}
if p != "/only-path" {
t.Errorf("path = %q, want /only-path", p)
}
}

func TestSplitRequest_Empty(t *testing.T) {
m, p := splitRequest("")
if m != "" || p != "" {
t.Errorf("got (%q, %q), want empty/empty", m, p)
}
}

// ── ParseLogFormat ────────────────────────────────────────────────────────────

func TestParseLogFormat_CaseInsensitive(t *testing.T) {
for _, s := range []string{"Nginx", "NGINX", "nginx"} {
f, err := ParseLogFormat(s)
if err != nil {
t.Errorf("ParseLogFormat(%q): %v", s, err)
}
if f != FormatNginx {
t.Errorf("ParseLogFormat(%q) = %v, want FormatNginx", s, f)
}
}
}
