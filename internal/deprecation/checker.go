// Package deprecation evaluates API deprecation status and determines
// whether a usage constitutes a pipeline warning or a blocking error.
package deprecation

import (
	"fmt"
	"time"

	"github.com/dever-labs/catalog-drift/internal/backstage"
)

// Severity indicates how serious a deprecation violation is.
type Severity int

const (
	SeverityWarning Severity = iota + 1
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// Violation describes a deprecation issue found for a contract.
type Violation struct {
	APIName         string
	Namespace       string
	Severity        Severity
	Message         string
	Successor       string
	DeprecatedSince *time.Time
	SunsetDate      *time.Time
	// DaysUntilSunset is negative when the sunset date is in the past.
	// Zero when there is no sunset date.
	DaysUntilSunset int
}

// Config controls how the Checker determines warning vs error severity.
type Config struct {
	// ErrorAfter is the duration after DeprecatedSince at which a warning
	// escalates to an error. Only used when SunsetDate is not set on the API.
	// A zero value means deprecations without a sunset date remain warnings indefinitely.
	ErrorAfter time.Duration

	// now is injectable for testing; defaults to time.Now().
	now func() time.Time
}

// Checker evaluates backstage.Contracts and surfaces deprecation violations.
type Checker struct {
	cfg Config
}

// NewChecker creates a Checker with the given config.
func NewChecker(cfg Config) *Checker {
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Checker{cfg: cfg}
}

// Check evaluates the deprecation status of a contract.
// Returns nil when the contract is not deprecated.
func (c *Checker) Check(contract backstage.Contract) *Violation {
	dep := contract.Deprecation
	if !dep.IsDeprecated {
		return nil
	}

	now := c.cfg.now()
	v := &Violation{
		APIName:         contract.Entity.Metadata.Name,
		Namespace:       contract.Entity.Metadata.Namespace,
		Severity:        SeverityWarning,
		Successor:       dep.Successor,
		DeprecatedSince: dep.DeprecatedSince,
		SunsetDate:      dep.SunsetDate,
	}

	if dep.SunsetDate != nil {
		days := int(dep.SunsetDate.Sub(now).Hours() / 24)
		v.DaysUntilSunset = days

		if now.After(*dep.SunsetDate) {
			v.Severity = SeverityError
		}
	} else if c.cfg.ErrorAfter > 0 && dep.DeprecatedSince != nil {
		deadline := dep.DeprecatedSince.Add(c.cfg.ErrorAfter)
		if now.After(deadline) {
			v.Severity = SeverityError
		}
	}

	v.Message = buildMessage(v)
	return v
}

// CheckAll evaluates a slice of contracts and returns all violations.
func (c *Checker) CheckAll(contracts []backstage.Contract) []Violation {
	var violations []Violation
	for _, contract := range contracts {
		if v := c.Check(contract); v != nil {
			violations = append(violations, *v)
		}
	}
	return violations
}

func buildMessage(v *Violation) string {
	name := v.APIName
	if v.Namespace != "" && v.Namespace != "default" {
		name = v.Namespace + "/" + name
	}

	switch {
	case v.SunsetDate != nil && v.DaysUntilSunset < 0:
		msg := fmt.Sprintf("API %q has passed its sunset date (%s) and must be replaced",
			name, v.SunsetDate.Format("2006-01-02"))
		if v.Successor != "" {
			msg += fmt.Sprintf("; migrate to %q", v.Successor)
		}
		return msg

	case v.SunsetDate != nil:
		msg := fmt.Sprintf("API %q is deprecated and will be removed in %d day(s) on %s",
			name, v.DaysUntilSunset, v.SunsetDate.Format("2006-01-02"))
		if v.Successor != "" {
			msg += fmt.Sprintf("; migrate to %q", v.Successor)
		}
		return msg

	default:
		msg := fmt.Sprintf("API %q is deprecated", name)
		if v.Successor != "" {
			msg += fmt.Sprintf("; migrate to %q", v.Successor)
		}
		return msg
	}
}
