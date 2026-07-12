// Package gateway generates API gateway configuration to enforce API sunset
// by returning HTTP 410 Gone for deprecated endpoints past their sunset date.
package gateway

import (
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"
)

// SunsetRule describes a single deprecated endpoint to be enforced.
type SunsetRule struct {
	Path       string    // e.g. /api/v1/users
	Methods    []string  // HTTP methods, empty means all
	SunsetDate time.Time // when the API was/will be sunset
	Message    string    // human-readable deprecation message
	Successor  string    // replacement path or API name
}

// Format identifies the target gateway.
type Format string

const (
	FormatNginx  Format = "nginx"
	FormatEnvoy  Format = "envoy"
	FormatKong   Format = "kong"
)

// ParseFormat validates and returns a Format.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(s)) {
	case FormatNginx, FormatEnvoy, FormatKong:
		return Format(strings.ToLower(s)), nil
	default:
		return "", fmt.Errorf("unknown gateway format %q: must be nginx, envoy, or kong", s)
	}
}

// Generate writes gateway configuration for the given sunset rules.
func Generate(rules []SunsetRule, format Format, w io.Writer) error {
	switch format {
	case FormatNginx:
		return generateNginx(rules, w)
	case FormatEnvoy:
		return generateEnvoy(rules, w)
	case FormatKong:
		return generateKong(rules, w)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

// sunsetBody builds the standard JSON body for a 410 response.
func sunsetBody(rule SunsetRule) string {
	msg := rule.Message
	if msg == "" {
		msg = fmt.Sprintf("This API endpoint was retired on %s.", rule.SunsetDate.Format("2006-01-02"))
		if rule.Successor != "" {
			msg += fmt.Sprintf(" Please migrate to %s.", rule.Successor)
		}
	}
	body := fmt.Sprintf(`{"error":"gone","message":%q,"sunset":%q`,
		msg, rule.SunsetDate.Format("2006-01-02"))
	if rule.Successor != "" {
		body += fmt.Sprintf(`,"successor":%q`, rule.Successor)
	}
	body += "}"
	return body
}

// ── Nginx ────────────────────────────────────────────────────────────────────

var nginxTmpl = template.Must(template.New("nginx").Funcs(template.FuncMap{
	"sunsetBody": sunsetBody,
	"joinMethods": func(methods []string) string {
		return strings.Join(methods, " ")
	},
}).Parse(`# catalog-drift sunset enforcement
# Generated: {{ .GeneratedAt }}
# Do not edit manually — regenerate with: catalog-drift enforce

{{ range .Rules }}
# {{ .Path }} — sunset {{ .SunsetDate.Format "2006-01-02" }}
location = {{ .Path }} {
    default_type application/json;
    {{ if .Methods }}
    limit_except {{ joinMethods .Methods }} { deny all; }
    {{ end }}
    add_header Sunset "{{ .SunsetDate.Format "Mon, 02 Jan 2006 00:00:00 GMT" }}" always;
    {{ if .Successor }}add_header Link "<{{ .Successor }}>; rel=\"successor-version\"" always;{{ end }}
    return 410 '{{ sunsetBody . }}';
}
{{ end }}
`))

func generateNginx(rules []SunsetRule, w io.Writer) error {
	return nginxTmpl.Execute(w, map[string]any{
		"GeneratedAt": time.Now().UTC().Format(time.RFC3339),
		"Rules":       rules,
	})
}

// ── Envoy ─────────────────────────────────────────────────────────────────────

var envoyTmpl = template.Must(template.New("envoy").Funcs(template.FuncMap{
	"sunsetBody": sunsetBody,
}).Parse(`# catalog-drift sunset enforcement
# Generated: {{ .GeneratedAt }}
routes:
{{ range .Rules }}  - match:
      path: "{{ .Path }}"
    direct_response:
      status: 410
      body:
        inline_string: '{{ sunsetBody . }}'
    response_headers_to_add:
      - header:
          key: Content-Type
          value: application/json
      - header:
          key: Sunset
          value: "{{ .SunsetDate.Format "Mon, 02 Jan 2006 00:00:00 GMT" }}"
      {{ if .Successor }}- header:
          key: Link
          value: "<{{ .Successor }}>; rel=\"successor-version\""
      {{ end }}
{{ end }}
`))

func generateEnvoy(rules []SunsetRule, w io.Writer) error {
	return envoyTmpl.Execute(w, map[string]any{
		"GeneratedAt": time.Now().UTC().Format(time.RFC3339),
		"Rules":       rules,
	})
}

// ── Kong ─────────────────────────────────────────────────────────────────────

var kongTmpl = template.Must(template.New("kong").Funcs(template.FuncMap{
	"sunsetBody": sunsetBody,
	"slugify": func(s string) string {
		s = strings.NewReplacer("/", "-", "{", "", "}", "").Replace(s)
		return strings.Trim(s, "-")
	},
}).Parse(`# catalog-drift sunset enforcement
# Generated: {{ .GeneratedAt }}
_format_version: "3.0"

routes:
{{ range .Rules }}  - name: sunset{{ slugify .Path }}
    paths:
      - "{{ .Path }}"
    {{ if .Methods }}methods:
      {{ range .Methods }}- {{ . }}
      {{ end }}{{ end }}plugins:
      - name: request-termination
        config:
          status_code: 410
          message: '{{ sunsetBody . }}'
          content_type: application/json
      - name: response-transformer
        config:
          add:
            headers:
              - "Sunset: {{ .SunsetDate.Format "Mon, 02 Jan 2006 00:00:00 GMT" }}"
              {{ if .Successor }}- "Link: <{{ .Successor }}>; rel=\"successor-version\""{{ end }}
{{ end }}
`))

func generateKong(rules []SunsetRule, w io.Writer) error {
	return kongTmpl.Execute(w, map[string]any{
		"GeneratedAt": time.Now().UTC().Format(time.RFC3339),
		"Rules":       rules,
	})
}
