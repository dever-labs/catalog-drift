// Package consumer discovers which clients are calling deprecated API endpoints
// by ingesting gateway access logs or Prometheus metrics.
package consumer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Record summarises one client's usage of an API path.
type Record struct {
	ClientID     string
	Method       string
	Path         string
	RequestCount int64
	LastSeen     time.Time
}

// LogFormat identifies the source log format.
type LogFormat string

const (
	FormatNginx      LogFormat = "nginx"
	FormatEnvoy      LogFormat = "envoy"
	FormatKong       LogFormat = "kong"
	FormatPrometheus LogFormat = "prometheus"
)

// ParseLogFormat validates and returns a LogFormat.
func ParseLogFormat(s string) (LogFormat, error) {
	switch LogFormat(strings.ToLower(s)) {
	case FormatNginx, FormatEnvoy, FormatKong, FormatPrometheus:
		return LogFormat(strings.ToLower(s)), nil
	default:
		return "", fmt.Errorf("unknown log format %q: must be nginx, envoy, kong, or prometheus", s)
	}
}

// Discovery aggregates access records for deprecated paths.
type Discovery struct {
	format LogFormat
}

// New creates a Discovery for the given log format.
func New(format LogFormat) *Discovery { return &Discovery{format: format} }

// IngestFile reads records from a file, returning only those matching deprecatedPaths.
// deprecatedPaths is a set of API paths to filter on; nil or empty means keep all.
func (d *Discovery) IngestFile(path string, deprecatedPaths map[string]struct{}) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return d.IngestReader(f, deprecatedPaths)
}

// IngestReader reads records from r, returning only those matching deprecatedPaths.
func (d *Discovery) IngestReader(r io.Reader, deprecatedPaths map[string]struct{}) ([]Record, error) {
	agg := make(map[string]*Record) // key: clientID|method|path

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var entry accessEntry
		var err error
		switch d.format {
		case FormatNginx:
			entry, err = parseNginx(line)
		case FormatEnvoy:
			entry, err = parseEnvoy(line)
		case FormatKong:
			entry, err = parseKong(line)
		case FormatPrometheus:
			entry, err = parsePrometheus(line)
		}
		if err != nil || entry.path == "" {
			continue
		}

		// Filter to deprecated paths if a filter is provided.
		if len(deprecatedPaths) > 0 {
			if _, ok := deprecatedPaths[entry.path]; !ok {
				continue
			}
		}

		key := entry.clientID + "|" + entry.method + "|" + entry.path
		if rec, ok := agg[key]; ok {
			rec.RequestCount += entry.count
			if entry.ts.After(rec.LastSeen) {
				rec.LastSeen = entry.ts
			}
		} else {
			agg[key] = &Record{
				ClientID:     entry.clientID,
				Method:       entry.method,
				Path:         entry.path,
				RequestCount: entry.count,
				LastSeen:     entry.ts,
			}
		}
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}

	records := make([]Record, 0, len(agg))
	for _, r := range agg {
		records = append(records, *r)
	}
	return records, nil
}

// accessEntry is a normalised single log line.
type accessEntry struct {
	clientID string
	method   string
	path     string
	ts       time.Time
	count    int64
}

// ── Nginx JSON ───────────────────────────────────────────────────────────────
// {"time_local":"...","remote_addr":"...","request":"GET /path HTTP/1.1","status":"200"}

func parseNginx(line string) (accessEntry, error) {
	var raw struct {
		TimeLocal  string `json:"time_local"`
		RemoteAddr string `json:"remote_addr"`
		Request    string `json:"request"` // "METHOD /path HTTP/x.y"
		XFF        string `json:"http_x_forwarded_for"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return accessEntry{}, err
	}
	method, path := splitRequest(raw.Request)
	client := raw.RemoteAddr
	if raw.XFF != "" {
		client = strings.SplitN(raw.XFF, ",", 2)[0]
		client = strings.TrimSpace(client)
	}
	ts, _ := time.Parse("02/Jan/2006:15:04:05 -0700", raw.TimeLocal)
	return accessEntry{clientID: client, method: method, path: path, ts: ts, count: 1}, nil
}

// ── Envoy JSON ───────────────────────────────────────────────────────────────
// {"start_time":"...","method":"GET","path":"/path","upstream_cluster":"svc","response_code":200}

func parseEnvoy(line string) (accessEntry, error) {
	var raw struct {
		StartTime       string `json:"start_time"`
		Method          string `json:"method"`
		Path            string `json:"path"`
		UpstreamCluster string `json:"upstream_cluster"`
		DownstreamAddr  string `json:"downstream_remote_address"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return accessEntry{}, err
	}
	client := raw.UpstreamCluster
	if client == "" {
		client = raw.DownstreamAddr
	}
	ts, _ := time.Parse(time.RFC3339Nano, raw.StartTime)
	return accessEntry{clientID: client, method: raw.Method, path: raw.Path, ts: ts, count: 1}, nil
}

// ── Kong JSON ────────────────────────────────────────────────────────────────
// {"request":{"uri":"/path","method":"GET","headers":{"x-consumer-username":"svc"}},"client_ip":"...","started_at":1704099600000}

func parseKong(line string) (accessEntry, error) {
	var raw struct {
		Request struct {
			URI     string            `json:"uri"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
		ClientIP  string `json:"client_ip"`
		StartedAt int64  `json:"started_at"` // ms epoch
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return accessEntry{}, err
	}
	client := raw.Request.Headers["x-consumer-username"]
	if client == "" {
		client = raw.ClientIP
	}
	var ts time.Time
	if raw.StartedAt > 0 {
		ts = time.UnixMilli(raw.StartedAt)
	}
	return accessEntry{clientID: client, method: raw.Request.Method, path: raw.Request.URI, ts: ts, count: 1}, nil
}

// ── Prometheus text format ───────────────────────────────────────────────────
// http_requests_total{method="GET",path="/path",consumer="svc"} 42 1704099600000

var promRe = regexp.MustCompile(`^(\w+)\{([^}]*)\}\s+([\d.]+)(?:\s+(\d+))?`)
var promLabelRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func parsePrometheus(line string) (accessEntry, error) {
	if strings.HasPrefix(line, "#") {
		return accessEntry{}, nil
	}
	m := promRe.FindStringSubmatch(line)
	if m == nil {
		return accessEntry{}, fmt.Errorf("not a prometheus metric line")
	}
	// Parse labels
	labels := make(map[string]string)
	for _, lm := range promLabelRe.FindAllStringSubmatch(m[2], -1) {
		labels[lm[1]] = lm[2]
	}
	count, _ := strconv.ParseFloat(m[3], 64)
	var ts time.Time
	if m[4] != "" {
		ms, _ := strconv.ParseInt(m[4], 10, 64)
		ts = time.UnixMilli(ms)
	}
	return accessEntry{
		clientID: labels["consumer"],
		method:   strings.ToUpper(labels["method"]),
		path:     labels["path"],
		ts:       ts,
		count:    int64(count),
	}, nil
}

// splitRequest splits "METHOD /path HTTP/x.y" into method and path.
func splitRequest(req string) (method, path string) {
	parts := strings.Fields(req)
	if len(parts) >= 2 {
		return strings.ToUpper(parts[0]), parts[1]
	}
	return "", req
}
