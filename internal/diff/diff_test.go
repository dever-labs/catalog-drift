package diff

import (
	"strings"
	"testing"

	"github.com/dever-labs/catalog-drift/internal/scanner"
	codescanner "github.com/dever-labs/catalog-drift/internal/scanner/code"
)

const openapiBase = `
openapi: 3.0.0
info:
  title: Orders API
  version: 1.0.0
paths:
  /orders:
    get:
      summary: List orders
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                required: [id, status]
                properties:
                  id:
                    type: string
                  status:
                    type: string
    post:
      summary: Create order
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [item]
              properties:
                item:
                  type: string
                quantity:
                  type: integer
      responses:
        '201':
          description: Created
  /orders/{id}:
    get:
      summary: Get order
      responses:
        '200':
          description: OK
    delete:
      summary: Cancel order
      responses:
        '204':
          description: No Content
`

func diffSpec(t *testing.T, specType, contract string, local []byte) []Violation {
	t.Helper()
	e := New()
	v, err := e.Diff(specType, contract, scanner.SpecFile{Content: local})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return v
}

func hasViolation(vs []Violation, rule RuleType, pathSubstr string) bool {
	for _, v := range vs {
		if v.Rule == rule && strings.Contains(v.Path, pathSubstr) {
			return true
		}
	}
	return false
}

// ── OpenAPI tests ────────────────────────────────────────────────────────────

func TestDiffOpenAPI_NoDrift(t *testing.T) {
	vs := diffSpec(t, "openapi", openapiBase, []byte(openapiBase))
	if len(vs) != 0 {
		t.Errorf("expected no violations for identical specs, got %d: %v", len(vs), vs)
	}
}

func TestDiffOpenAPI_MissingPath(t *testing.T) {
	local := `
openapi: 3.0.0
paths:
  /orders:
    get:
      responses:
        '200':
          description: OK
`
	vs := diffSpec(t, "openapi", openapiBase, []byte(local))
	if !hasViolation(vs, RuleMissingEndpoint, "/orders/{id}") {
		t.Errorf("expected missing-endpoint for /orders/{id}, violations: %v", vs)
	}
}

func TestDiffOpenAPI_MissingMethod(t *testing.T) {
	local := `
openapi: 3.0.0
paths:
  /orders:
    get:
      responses:
        '200':
          description: OK
  /orders/{id}:
    get:
      responses:
        '200':
          description: OK
`
	vs := diffSpec(t, "openapi", openapiBase, []byte(local))
	if !hasViolation(vs, RuleMissingEndpoint, "post") {
		t.Errorf("expected missing-endpoint for POST /orders, violations: %v", vs)
	}
	if !hasViolation(vs, RuleMissingEndpoint, "delete") {
		t.Errorf("expected missing-endpoint for DELETE /orders/{id}, violations: %v", vs)
	}
}

func TestDiffOpenAPI_UndeclaredPath(t *testing.T) {
	local := openapiBase + `
  /internal/health:
    get:
      responses:
        '200':
          description: OK
`
	vs := diffSpec(t, "openapi", openapiBase, []byte(local))
	if !hasViolation(vs, RuleUndeclaredEndpoint, "/internal/health") {
		t.Errorf("expected undeclared-endpoint for /internal/health, violations: %v", vs)
	}
}

func TestDiffOpenAPI_UndeclaredMethod(t *testing.T) {
	local := strings.ReplaceAll(openapiBase, "delete:", "patch:\n      responses:\n        '200':\n          description: OK\n    delete:")
	vs := diffSpec(t, "openapi", openapiBase, []byte(local))
	if !hasViolation(vs, RuleUndeclaredEndpoint, "patch") {
		t.Errorf("expected undeclared-endpoint for PATCH, violations: %v", vs)
	}
}

func TestDiffOpenAPI_MissingRequiredField(t *testing.T) {
	local := `
openapi: 3.0.0
paths:
  /orders:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
    post:
      responses:
        '201':
          description: Created
  /orders/{id}:
    get:
      responses:
        '200':
          description: OK
    delete:
      responses:
        '204':
          description: No Content
`
	vs := diffSpec(t, "openapi", openapiBase, []byte(local))
	if !hasViolation(vs, RuleMissingField, "status") {
		t.Errorf("expected missing-field for 'status', violations: %v", vs)
	}
}

func TestDiffOpenAPI_TypeMismatch(t *testing.T) {
	local := `
openapi: 3.0.0
paths:
  /orders:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: integer
                  status:
                    type: string
    post:
      responses: {}
  /orders/{id}:
    get:
      responses: {}
    delete:
      responses: {}
`
	vs := diffSpec(t, "openapi", openapiBase, []byte(local))
	if !hasViolation(vs, RuleTypeMismatch, "id") {
		t.Errorf("expected type-mismatch for 'id' (string→integer), violations: %v", vs)
	}
}

func TestDiffOpenAPI_SkipsRefSchemas(t *testing.T) {
	contract := `
openapi: 3.0.0
paths:
  /orders:
    get:
      responses:
        '200':
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
`
	vs := diffSpec(t, "openapi", contract, []byte(contract))
	if len(vs) != 0 {
		t.Errorf("expected no violations for $ref schemas (not resolved), got: %v", vs)
	}
}

// ── AsyncAPI tests ───────────────────────────────────────────────────────────

const asyncapiBase = `
asyncapi: 2.0.0
channels:
  order/created:
    subscribe:
      message:
        payload:
          type: object
  order/cancelled:
    subscribe:
      message:
        payload:
          type: object
`

func TestDiffAsyncAPI_NoDrift(t *testing.T) {
	vs := diffSpec(t, "asyncapi", asyncapiBase, []byte(asyncapiBase))
	if len(vs) != 0 {
		t.Errorf("expected no violations for identical specs, got %d", len(vs))
	}
}

func TestDiffAsyncAPI_MissingChannel(t *testing.T) {
	local := `
asyncapi: 2.0.0
channels:
  order/created:
    subscribe:
      message:
        payload:
          type: object
`
	vs := diffSpec(t, "asyncapi", asyncapiBase, []byte(local))
	if !hasViolation(vs, RuleMissingChannel, "order/cancelled") {
		t.Errorf("expected missing-channel for order/cancelled, violations: %v", vs)
	}
}

func TestDiffAsyncAPI_UndeclaredChannel(t *testing.T) {
	local := asyncapiBase + `
  order/updated:
    subscribe:
      message:
        payload:
          type: object
`
	vs := diffSpec(t, "asyncapi", asyncapiBase, []byte(local))
	if !hasViolation(vs, RuleUndeclaredEndpoint, "order/updated") {
		t.Errorf("expected undeclared channel for order/updated, violations: %v", vs)
	}
}

// ── gRPC tests ───────────────────────────────────────────────────────────────

const protoBase = `
syntax = "proto3";
package orders;

service OrderService {
  rpc CreateOrder (CreateOrderRequest) returns (Order);
  rpc GetOrder (GetOrderRequest) returns (Order);
  rpc CancelOrder (CancelOrderRequest) returns (Empty);
}
`

func TestDiffGRPC_NoDrift(t *testing.T) {
	vs := diffSpec(t, "grpc", protoBase, []byte(protoBase))
	if len(vs) != 0 {
		t.Errorf("expected no violations for identical protos, got %d", len(vs))
	}
}

func TestDiffGRPC_MissingMethod(t *testing.T) {
	local := `
syntax = "proto3";
service OrderService {
  rpc CreateOrder (CreateOrderRequest) returns (Order);
  rpc GetOrder (GetOrderRequest) returns (Order);
}
`
	vs := diffSpec(t, "grpc", protoBase, []byte(local))
	if !hasViolation(vs, RuleMissingRPCMethod, "CancelOrder") {
		t.Errorf("expected missing-rpc-method for CancelOrder, violations: %v", vs)
	}
}

func TestDiffGRPC_UndeclaredMethod(t *testing.T) {
	local := protoBase + `
service AdminService {
  rpc DeleteOrder (DeleteOrderRequest) returns (Empty);
}
`
	vs := diffSpec(t, "grpc", protoBase, []byte(local))
	if !hasViolation(vs, RuleUndeclaredRPCMethod, "DeleteOrder") {
		t.Errorf("expected undeclared-rpc-method for DeleteOrder, violations: %v", vs)
	}
}

func TestDiff_UnsupportedType(t *testing.T) {
	e := New()
	_, err := e.Diff("graphql", "type Query { hello: String }", scanner.SpecFile{Content: []byte("type Query { hello: String }")})
	if err == nil {
		t.Error("expected error for unsupported type, got nil")
	}
}

// ── Code route diff tests ─────────────────────────────────────────────────────

func codeRoutes(pairs ...string) []codescanner.Route {
var routes []codescanner.Route
for i := 0; i+1 < len(pairs); i += 2 {
routes = append(routes, codescanner.Route{Method: pairs[i], Path: pairs[i+1]})
}
return routes
}

func TestDiffCodeRoutes_NoDrift(t *testing.T) {
routes := codeRoutes("GET", "/orders", "POST", "/orders", "GET", "/orders/{id}", "DELETE", "/orders/{id}")
vs, err := New().DiffCodeRoutes(openapiBase, routes)
if err != nil {
t.Fatalf("DiffCodeRoutes: %v", err)
}
if len(vs) != 0 {
t.Errorf("expected no violations for matching routes, got %d: %v", len(vs), vs)
}
}

func TestDiffCodeRoutes_MissingRoute(t *testing.T) {
// Missing DELETE /orders/{id}
routes := codeRoutes("GET", "/orders", "POST", "/orders", "GET", "/orders/{id}")
vs, err := New().DiffCodeRoutes(openapiBase, routes)
if err != nil {
t.Fatalf("DiffCodeRoutes: %v", err)
}
if !hasViolation(vs, RuleMissingEndpoint, "delete") {
t.Errorf("expected missing-endpoint for DELETE, violations: %v", vs)
}
}

func TestDiffCodeRoutes_UndeclaredRoute(t *testing.T) {
routes := codeRoutes("GET", "/orders", "POST", "/orders", "GET", "/orders/{id}", "DELETE", "/orders/{id}", "GET", "/orders/internal")
vs, err := New().DiffCodeRoutes(openapiBase, routes)
if err != nil {
t.Fatalf("DiffCodeRoutes: %v", err)
}
if !hasViolation(vs, RuleUndeclaredEndpoint, "/orders/internal") {
t.Errorf("expected undeclared-endpoint for /orders/internal, violations: %v", vs)
}
}

func TestDiffCodeRoutes_WildcardSatisfiesAnyMethod(t *testing.T) {
// Route registered with HandleFunc (no method) satisfies all contract methods
routes := codeRoutes("*", "/orders", "*", "/orders/{id}")
vs, err := New().DiffCodeRoutes(openapiBase, routes)
if err != nil {
t.Fatalf("DiffCodeRoutes: %v", err)
}
if len(vs) != 0 {
t.Errorf("wildcard route should satisfy all methods, got violations: %v", vs)
}
}

func TestDiffCodeRoutes_EmptyContractDef(t *testing.T) {
vs, err := New().DiffCodeRoutes("", codeRoutes("GET", "/orders"))
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if vs != nil {
t.Errorf("expected nil violations for empty contract, got %v", vs)
}
}

// ── ExtractEndpoints tests ────────────────────────────────────────────────────

func TestExtractEndpoints_OpenAPI(t *testing.T) {
eps, err := ExtractEndpoints("openapi", openapiBase)
if err != nil {
t.Fatalf("ExtractEndpoints: %v", err)
}
if len(eps) == 0 {
t.Fatal("expected endpoints, got none")
}
found := false
for _, ep := range eps {
if ep.Method == "GET" && ep.Path == "/orders" {
found = true
}
}
if !found {
t.Errorf("expected GET /orders in endpoints, got: %v", eps)
}
}

func TestExtractEndpoints_NonOpenAPI(t *testing.T) {
eps, err := ExtractEndpoints("asyncapi", "asyncapi: 2.0.0")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if eps != nil {
t.Errorf("expected nil for non-openapi type, got %v", eps)
}
}

func TestExtractEndpoints_ReturnsUppercaseMethods(t *testing.T) {
eps, err := ExtractEndpoints("openapi", openapiBase)
if err != nil {
t.Fatalf("ExtractEndpoints: %v", err)
}
for _, ep := range eps {
if ep.Method != strings.ToUpper(ep.Method) {
t.Errorf("method %q is not uppercase", ep.Method)
}
}
}
