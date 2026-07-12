// Package backstage provides a client for fetching API contracts
// registered in a Backstage catalog.
package backstage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sentinel errors returned by the client.
var (
	// ErrNotFound is returned when an entity does not exist in the catalog.
	ErrNotFound = errors.New("entity not found")
	// ErrUnauthorized is returned when the server rejects the request due to missing or invalid credentials.
	ErrUnauthorized = errors.New("unauthorized: verify your Backstage token")
)

// Entity represents a Backstage catalog entity.
type Entity struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   EntityMetadata  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
	Relations  []Relation      `json:"relations"`
}

// EntityMetadata holds the metadata block of a Backstage entity.
type EntityMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Title       string            `json:"title,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Relation describes a directed relationship to another entity.
type Relation struct {
	Type      string         `json:"type"`
	TargetRef string         `json:"targetRef"`
	Target    RelationTarget `json:"target"`
}

// RelationTarget is the parsed entity reference of a relation.
type RelationTarget struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// APISpec is the spec section of a Backstage API entity.
type APISpec struct {
	Type       string `json:"type"`       // openapi | asyncapi | grpc | graphql
	Lifecycle  string `json:"lifecycle"`
	Owner      string `json:"owner"`
	Definition string `json:"definition"` // raw spec content
}

// Contract bundles a Backstage API entity with its parsed spec and deprecation info.
type Contract struct {
	Entity      Entity
	APISpec     APISpec
	Deprecation DeprecationInfo
}

// Client fetches API contracts from a Backstage catalog.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the Bearer token for Backstage authentication.
func WithToken(token string) Option {
	return func(c *Client) {
		c.token = token
	}
}

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// NewClient creates a new Backstage catalog client targeting baseURL.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// FetchContracts returns all API contracts provided by the named component.
// namespace defaults to "default" when empty.
func (c *Client) FetchContracts(ctx context.Context, component, namespace string) ([]Contract, error) {
	if namespace == "" {
		namespace = "default"
	}
	comp, err := c.fetchEntity(ctx, "component", namespace, component)
	if err != nil {
		return nil, fmt.Errorf("fetch component %q: %w", component, err)
	}

	var contracts []Contract
	for _, rel := range comp.Relations {
		if rel.Type != "providesApi" {
			continue
		}
		target := resolveTarget(rel, "api", "default")
		apiEntity, err := c.fetchEntity(ctx, target.Kind, target.Namespace, target.Name)
		if err != nil {
			return nil, fmt.Errorf("fetch api %q: %w", rel.TargetRef, err)
		}
		contract, err := buildContract(apiEntity)
		if err != nil {
			return nil, fmt.Errorf("parse spec for %q: %w", rel.TargetRef, err)
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

// FetchContract fetches a single named API entity directly.
// namespace defaults to "default" when empty.
func (c *Client) FetchContract(ctx context.Context, apiName, namespace string) (*Contract, error) {
	if namespace == "" {
		namespace = "default"
	}
	apiEntity, err := c.fetchEntity(ctx, "api", namespace, apiName)
	if err != nil {
		return nil, err
	}
	contract, err := buildContract(apiEntity)
	if err != nil {
		return nil, fmt.Errorf("parse spec for %q: %w", apiName, err)
	}
	return &contract, nil
}

func parseAPISpec(entity *Entity) (APISpec, error) {
	var spec APISpec
	if err := json.Unmarshal(entity.Spec, &spec); err != nil {
		return APISpec{}, err
	}
	return spec, nil
}

func buildContract(entity *Entity) (Contract, error) {
	spec, err := parseAPISpec(entity)
	if err != nil {
		return Contract{}, err
	}
	return Contract{
		Entity:      *entity,
		APISpec:     spec,
		Deprecation: deprecationFromEntity(*entity, spec),
	}, nil
}

// FetchConsumedContracts returns all API contracts consumed by the named
// component (i.e. following its consumesApi relations).
// namespace defaults to "default" when empty.
func (c *Client) FetchConsumedContracts(ctx context.Context, component, namespace string) ([]Contract, error) {
	if namespace == "" {
		namespace = "default"
	}
	comp, err := c.fetchEntity(ctx, "component", namespace, component)
	if err != nil {
		return nil, fmt.Errorf("fetch component %q: %w", component, err)
	}

	var contracts []Contract
	for _, rel := range comp.Relations {
		if rel.Type != "consumesApi" {
			continue
		}
		target := resolveTarget(rel, "api", "default")
		apiEntity, err := c.fetchEntity(ctx, target.Kind, target.Namespace, target.Name)
		if err != nil {
			return nil, fmt.Errorf("fetch api %q: %w", rel.TargetRef, err)
		}
		contract, err := buildContract(apiEntity)
		if err != nil {
			return nil, fmt.Errorf("parse spec for %q: %w", rel.TargetRef, err)
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

// FetchDeprecatedContracts returns all API entities in the catalog whose
// lifecycle is "deprecated", across all components and namespaces.
func (c *Client) FetchDeprecatedContracts(ctx context.Context) ([]Contract, error) {
	var entities []Entity
	path := "/api/catalog/entities?filter=kind=API,spec.lifecycle=deprecated"
	if err := c.get(ctx, path, &entities); err != nil {
		return nil, fmt.Errorf("fetch deprecated contracts: %w", err)
	}
	contracts := make([]Contract, 0, len(entities))
	for i := range entities {
		contract, err := buildContract(&entities[i])
		if err != nil {
			continue // skip malformed entities
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

// FetchAPIConsumers returns all Component entities in the catalog that declare
// a consumesApi relation pointing at the given API name.
// Backstage exposes this via the entity's relations[] array — every component
// that lists an API under consumesApis will have a "consumesApi" relation on
// the API entity pointing back at it.
func (c *Client) FetchAPIConsumers(ctx context.Context, apiName, namespace string) ([]Entity, error) {
	if namespace == "" {
		namespace = "default"
	}
	// Fetch the API entity itself and walk its relations for "apiConsumedBy".
	entity, err := c.fetchEntity(ctx, "api", namespace, apiName)
	if err != nil {
		return nil, fmt.Errorf("fetch api %q: %w", apiName, err)
	}

	var consumers []Entity
	for _, rel := range entity.Relations {
		if rel.Type != "apiConsumedBy" {
			continue
		}
		target := resolveTarget(rel, "component", namespace)
		comp, err := c.fetchEntity(ctx, target.Kind, target.Namespace, target.Name)
		if err != nil {
			// Non-fatal: catalog may have stale refs.
			continue
		}
		consumers = append(consumers, *comp)
	}
	return consumers, nil
}

// BuildContractForTest is exported for use in tests outside this package.
// Use the client methods in production code.
var BuildContractForTest = buildContract

func (c *Client) fetchEntity(ctx context.Context, kind, namespace, name string) (*Entity, error) {
	path := fmt.Sprintf("/api/catalog/entities/by-name/%s/%s/%s",
		strings.ToLower(kind), namespace, name)

	var entity Entity
	if err := c.get(ctx, path, &entity); err != nil {
		return nil, err
	}
	return &entity, nil
}

func (c *Client) get(ctx context.Context, rawPath string, out any) error {
	// Split at '?' before JoinPath to prevent query string encoding.
	pathPart, query, _ := strings.Cut(rawPath, "?")
	u, err := url.JoinPath(c.baseURL, pathPart)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	if query != "" {
		u += "?" + query
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s: %w", rawPath, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// ok
	case http.StatusNotFound:
		return fmt.Errorf("%s: %w", rawPath, ErrNotFound)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s: %w", rawPath, ErrUnauthorized)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: unexpected status %d: %s", rawPath, resp.StatusCode, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s: %w", rawPath, err)
	}
	return nil
}

// resolveTarget returns a RelationTarget from the structured Target field when populated,
// falling back to parsing TargetRef. Backstage guarantees targetRef is always set;
// the target sub-object may be absent in older catalog versions.
func resolveTarget(rel Relation, defaultKind, defaultNamespace string) RelationTarget {
	if rel.Target.Kind != "" && rel.Target.Name != "" {
		return rel.Target
	}
	return parseEntityRef(rel.TargetRef, defaultKind, defaultNamespace)
}

// parseEntityRef parses a Backstage entity ref string of the form
// [kind:][namespace/]name, applying defaultKind and defaultNamespace where absent.
func parseEntityRef(ref, defaultKind, defaultNamespace string) RelationTarget {
	kind := defaultKind
	namespace := defaultNamespace
	rest := ref

	if idx := strings.Index(rest, ":"); idx != -1 {
		kind = strings.ToLower(rest[:idx])
		rest = rest[idx+1:]
	}
	if idx := strings.Index(rest, "/"); idx != -1 {
		namespace = rest[:idx]
		rest = rest[idx+1:]
	}
	return RelationTarget{Kind: kind, Namespace: namespace, Name: rest}
}
