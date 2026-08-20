// Package resilience is the shared contract for the `health` and `budgets`
// blocks of a routing decision (routing-decision.md §5, step 2).
//
// # Why this is a shared module rather than a struct in each service
//
// Two services must agree about these values, for different reasons and at
// different times. aiqg-dashboard-be validates them at WRITE time, so an
// operator learns immediately that eject_for of -5s is nonsense. tas-llm-router
// applies them at RUN time, when nobody is watching. If each kept its own copy
// of the rules, the two would drift, and the failure mode is the worst kind:
// the control plane accepts a configuration the gateway then interprets
// differently, or silently ignores.
//
// That is not hypothetical. It is exactly what happened with `url_path`, which
// meant RE2 in a route rule and a substring in an experiment cohort — the same
// field name, two evaluators, two meanings — and it is why go-aiqg-matcher
// exists. This module applies the same remedy before the divergence happens
// rather than after.
//
// # Why seconds, not time.Duration
//
// These values cross a JSON boundary and are stored as JSONB. time.Duration
// marshals to an integer count of NANOSECONDS, which is unreadable in a
// database row, hostile to hand-editing, and silently wrong if anyone assumes
// the number is seconds. Integer seconds survive the round trip legibly, and
// the conversion to Duration happens once, at the edge that needs it.
//
// # Zero means "unset", never "zero"
//
// Every field is optional, and a zero value means the consumer's default
// applies. This matters because these blocks are partial by design: a route
// rule that only wants to change eject_for should say exactly that, and
// inherit everything else. Distinguishing "set to 0" from "not set" would need
// pointers throughout, which buys nothing here — every field's meaningful
// range starts above zero.
package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Health configures passive outlier detection: when a routing target stops
// receiving traffic because it is failing.
type Health struct {
	// ConsecutiveErrors ejects a target after this many server errors in a
	// row. Catches a hard outage quickly.
	ConsecutiveErrors int `json:"consecutive_errors,omitempty"`
	// ErrorRatePercent ejects when this share of requests in the window fail.
	// Catches partial degradation, which a consecutive count never sees: a
	// target failing 40% of requests rarely strings together a long streak
	// but is badly broken.
	ErrorRatePercent int `json:"error_rate_percent,omitempty"`
	// MinRequests is the smallest sample the rate rule will act on. Without
	// it the first failure reads as 100% and ejects a healthy target on a
	// sample of one.
	MinRequests int `json:"min_requests,omitempty"`
	// WindowSeconds is the rolling period for rate accounting.
	WindowSeconds int `json:"window_seconds,omitempty"`
	// EjectForSeconds is how long an ejected target is withheld before a
	// single probe tests recovery. The most consequential setting here: too
	// short re-hammers a struggling provider, too long extends an outage
	// past its cause.
	EjectForSeconds int `json:"eject_for_seconds,omitempty"`
}

// Budgets caps retries so that retrying does not become the load that keeps a
// degraded target down.
type Budgets struct {
	// RetryRatio caps retries as a fraction of live requests (0.1 = at most
	// 10% of request volume may be retries).
	//
	// A per-request attempt cap cannot do this job. "3 attempts per request"
	// is a bound on ONE request; when a target degrades, every concurrent
	// request exercises its full allowance at once and the retries triple the
	// offered load precisely when the target can least absorb it. A budget
	// bounds retries in AGGREGATE, which is the quantity that actually hurts.
	RetryRatio float64 `json:"retry_ratio,omitempty"`
	// MinRetries is a floor always permitted regardless of the ratio, so
	// low-traffic tenants can retry at all — 10% of 2 requests per second
	// rounds to zero, which would disable retries entirely for exactly the
	// tenants least able to cause a storm.
	MinRetries int `json:"min_retries,omitempty"`
	// MaxAttempts caps attempts for a single request, including the first.
	// Complements the ratio rather than replacing it: the ratio bounds fleet
	// load, this bounds one caller's latency.
	MaxAttempts int `json:"max_attempts,omitempty"`
}

// Bounds that are refused outright. These are not tuning opinions — they are
// values that cannot mean anything sensible.
const (
	// MaxWindowSeconds — beyond an hour the "rolling window" stops tracking
	// current behaviour and starts averaging away the incident.
	MaxWindowSeconds = 3600
	// MaxEjectForSeconds — an ejection longer than an hour is an outage
	// policy, not a health policy, and should be a deliberate rule change.
	MaxEjectForSeconds = 3600
	// MaxAttemptsCeiling — more attempts than this is a retry storm with
	// extra steps.
	MaxAttemptsCeiling = 10
)

// Validate reports every problem at once rather than the first, because an
// operator fixing a form wants the whole list, not one round trip per mistake.
func (h Health) Validate() error {
	var errs []string
	if h.ConsecutiveErrors < 0 {
		errs = append(errs, "consecutive_errors must not be negative")
	}
	if h.ErrorRatePercent < 0 || h.ErrorRatePercent > 100 {
		errs = append(errs, "error_rate_percent must be between 0 and 100")
	}
	if h.MinRequests < 0 {
		errs = append(errs, "min_requests must not be negative")
	}
	if h.WindowSeconds < 0 || h.WindowSeconds > MaxWindowSeconds {
		errs = append(errs, fmt.Sprintf("window_seconds must be between 0 and %d", MaxWindowSeconds))
	}
	if h.EjectForSeconds < 0 || h.EjectForSeconds > MaxEjectForSeconds {
		errs = append(errs, fmt.Sprintf("eject_for_seconds must be between 0 and %d", MaxEjectForSeconds))
	}
	// A rate rule with no minimum sample ejects on the first failure. That is
	// almost always a mistake, and it is silent — the target simply vanishes
	// from rotation and the operator has no reason to suspect this field.
	if h.ErrorRatePercent > 0 && h.MinRequests > 0 && h.MinRequests < 5 {
		errs = append(errs, "min_requests below 5 makes error_rate_percent act on a sample too small to be meaningful")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Validate reports every problem at once. See Health.Validate.
func (b Budgets) Validate() error {
	var errs []string
	if b.RetryRatio < 0 || b.RetryRatio > 1 {
		errs = append(errs, "retry_ratio must be between 0 and 1 (it is a fraction of live requests, not a percentage)")
	}
	if b.MinRetries < 0 {
		errs = append(errs, "min_retries must not be negative")
	}
	if b.MaxAttempts < 0 || b.MaxAttempts > MaxAttemptsCeiling {
		errs = append(errs, fmt.Sprintf("max_attempts must be between 0 and %d", MaxAttemptsCeiling))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// IsZero reports whether nothing is configured, so a caller can omit the block
// entirely rather than serialising an all-defaults object that reads as
// deliberate configuration.
func (h Health) IsZero() bool  { return h == Health{} }
func (b Budgets) IsZero() bool { return b == Budgets{} }
