package backstage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Annotation key that points a component at a named GovernancePolicy entity.
// If absent, the CLI looks for a policy named "default" in the same namespace,
// then "default" in the "default" namespace.
const AnnotationGovernancePolicy = "catalog-drift/governance-policy"

// GovernancePolicySpec is the spec block of a GovernancePolicy entity.
// All fields are optional — unset fields leave the corresponding CLI flag values
// in effect (or the built-in defaults).
type GovernancePolicySpec struct {
	Deprecation GovernanceDeprecationSpec `json:"deprecation"`
	Contract    GovernanceContractSpec    `json:"contract"`
}

// GovernanceDeprecationSpec configures how deprecated-usage findings escalate.
type GovernanceDeprecationSpec struct {
	// ErrorAfter is the duration string after which deprecated usage becomes an
	// error (measured from the API's DeprecatedSince date).
	// Example values: "30d", "90d", "6m", "1y".
	// Empty means deprecated usage is always a warning (never escalates).
	ErrorAfter string `json:"errorAfter"`

	// WarnBeforeSunset is how far in advance of SunsetDate to start emitting
	// warnings even if the API is not yet marked deprecated.
	// Example: "30d" — start warning 30 days before the sunset date.
	WarnBeforeSunset string `json:"warnBeforeSunset"`
}

// GovernanceContractSpec configures how contract-checking findings are handled.
type GovernanceContractSpec struct {
	// FailOnWarn makes the pipeline exit 1 when any warning-severity finding
	// is reported, not just errors.
	FailOnWarn bool `json:"failOnWarn"`
}

// GovernancePolicy is a resolved Backstage GovernancePolicy entity.
// A nil value means no policy was found; callers should fall back to CLI flags.
type GovernancePolicy struct {
	Name      string
	Namespace string
	Spec      GovernancePolicySpec
}

// FetchGovernancePolicy resolves the active GovernancePolicy for a component.
//
// Resolution order:
//  1. The policy named by the component's catalog-drift/governance-policy annotation.
//  2. A policy named "default" in the component's namespace.
//  3. A policy named "default" in the "default" namespace.
//
// Returns nil (no error) when no policy is configured anywhere in the chain.
// CLI flags should be applied on top of the returned policy; flags win.
func (c *Client) FetchGovernancePolicy(ctx context.Context, component, namespace string) (*GovernancePolicy, error) {
	if namespace == "" {
		namespace = "default"
	}

	// Determine the policy name — either from the component annotation or "default".
	policyName := "default"
	policyNamespace := namespace

	comp, err := c.fetchEntity(ctx, "component", namespace, component)
	if err != nil && !isNotFound(err) {
		return nil, fmt.Errorf("fetch component %q: %w", component, err)
	}
	if comp != nil {
		if ann := comp.Metadata.Annotations[AnnotationGovernancePolicy]; ann != "" {
			policyName = ann
		}
	}

	// Try the resolved name in the component's namespace.
	policy, err := c.fetchGovernancePolicyEntity(ctx, policyNamespace, policyName)
	if err != nil {
		return nil, err
	}
	if policy != nil {
		return policy, nil
	}

	// Fall back to "default" in the "default" namespace if not already tried.
	if policyNamespace != "default" || policyName != "default" {
		policy, err = c.fetchGovernancePolicyEntity(ctx, "default", "default")
		if err != nil {
			return nil, err
		}
	}
	return policy, nil
}

func (c *Client) fetchGovernancePolicyEntity(ctx context.Context, namespace, name string) (*GovernancePolicy, error) {
	entity, err := c.fetchEntity(ctx, "GovernancePolicy", namespace, name)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch governance policy %q/%q: %w", namespace, name, err)
	}

	var spec GovernancePolicySpec
	if err := json.Unmarshal(entity.Spec, &spec); err != nil {
		return nil, fmt.Errorf("parse governance policy spec for %q: %w", name, err)
	}
	return &GovernancePolicy{
		Name:      entity.Metadata.Name,
		Namespace: entity.Metadata.Namespace,
		Spec:      spec,
	}, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
