package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Policy enforcement — routing-decision.md §5, step 8 (Stage 4.1).
//
// # What this replaces, which is not "nothing"
//
// The gateway already enforces, in a way that ignores policy entirely: a
// hardcoded rule blocks any request carrying a CRITICAL finding, and a separate
// flag redacts unconditionally. Both operate outside the bundle system that is
// supposed to govern them, so a bundle can display `block` on a rule while a
// completely different mechanism decides what actually happens.
//
// Step 8 makes enforcement policy-driven. It is therefore a higher-risk change
// than adding a feature: it alters what already happens to live traffic.
//
// # observe does not mean "off"
//
// This is the decision that keeps the upgrade safe. In `observe`, the
// pre-existing controls keep running EXACTLY as they do now, and policy decides
// nothing. Policy still records what it WOULD have done, which makes the
// enforcement dry run live rather than retrospective.
//
// The alternative — having `observe` supersede and therefore disable the
// existing critical-block and redaction — would make adopting this feature
// quietly REDUCE protection on every tenant that had not yet configured
// enforcement. A security feature whose rollout weakens security is the wrong
// shape, however clean the resulting architecture.

// Mode selects how a bundle's rule actions are applied.
type Mode string

const (
	// ModeObserve records what policy would have done and changes nothing.
	// Pre-existing gateway controls continue to operate.
	ModeObserve Mode = "observe"
	// ModeEnforce applies the bundle's rule actions.
	ModeEnforce Mode = "enforce"
)

// DefaultMode is observe. Absent configuration must never start blocking
// traffic — an operator who has not chosen enforcement has not consented to it.
const DefaultMode = ModeObserve

// FailMode selects behaviour when policy cannot be evaluated at all.
type FailMode string

const (
	// FailOpen serves the request when policy is unavailable.
	//
	// The default, and the right one for a resolver outage: policy being
	// unreachable is an availability problem, and converting it into a total
	// outage means one degraded dependency takes down every tenant. The failure
	// is recorded on the event so it is visible rather than silent.
	FailOpen FailMode = "open"
	// FailClosed refuses the request when policy is unavailable.
	//
	// For tenants who would rather fail than serve un-scanned traffic. A real
	// position — but one that converts a policy-service blip into a customer
	// outage, so it must be chosen deliberately.
	FailClosed FailMode = "closed"
)

// Enforcement is a bundle's enforcement configuration.
type Enforcement struct {
	// Mode is observe or enforce. Empty means observe.
	Mode Mode `json:"mode,omitempty"`
	// OnPolicyUnavailable selects fail-open or fail-closed. Empty means open.
	OnPolicyUnavailable FailMode `json:"on_policy_unavailable,omitempty"`
}

// IsZero reports whether nothing is configured.
func (e Enforcement) IsZero() bool { return e == Enforcement{} }

// Enforcing reports whether rule actions should be applied.
func (e Enforcement) Enforcing() bool { return e.Mode == ModeEnforce }

// FailsClosed reports whether an unavailable policy should refuse the request.
func (e Enforcement) FailsClosed() bool { return e.OnPolicyUnavailable == FailClosed }

// Validate checks the shape.
func (e Enforcement) Validate() error {
	var errs []string
	if e.Mode != "" && e.Mode != ModeObserve && e.Mode != ModeEnforce {
		errs = append(errs, fmt.Sprintf("mode %q is not observe or enforce", e.Mode))
	}
	if e.OnPolicyUnavailable != "" && e.OnPolicyUnavailable != FailOpen && e.OnPolicyUnavailable != FailClosed {
		errs = append(errs, fmt.Sprintf("on_policy_unavailable %q is not open or closed", e.OnPolicyUnavailable))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ValidateFor checks enforcement against the bundle it governs.
//
// §6.1 rule 10: enforcing requires at least one rule that can actually change
// an outcome. A bundle of nothing but `log` rules set to `enforce` claims to be
// protecting something and protects nothing — and unlike most misconfigurations
// this one is invisible, because the traffic flows exactly as it did before.
// Refusing it at write time is the only point at which anyone would notice.
func (e Enforcement) ValidateFor(enabledNonLogRules int) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Enforcing() && enabledNonLogRules == 0 {
		return errors.New("this bundle has no enabled block or redact rules, so enforcing it would change nothing; add a rule that acts, or leave it in observe")
	}
	return nil
}

// Outcome is what enforcement decided for one request.
type Outcome string

const (
	// OutcomeAllowed: no rule matched, or every match was log-only.
	OutcomeAllowed Outcome = "allowed"
	// OutcomeRedacted: content was removed before the vendor saw it.
	OutcomeRedacted Outcome = "redacted"
	// OutcomeBlocked: the request was refused.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeWouldBlock / OutcomeWouldRedact: observe mode recording what
	// enforcing WOULD have done.
	//
	// Recorded distinctly from the real outcomes so a report can never confuse
	// "we blocked this" with "we would have" — the difference is the entire
	// value of observe mode, and a shared label would erase it.
	OutcomeWouldBlock  Outcome = "would_block"
	OutcomeWouldRedact Outcome = "would_redact"
)

// Observed converts an enforcing outcome into its observe-mode counterpart.
func (o Outcome) Observed() Outcome {
	switch o {
	case OutcomeBlocked:
		return OutcomeWouldBlock
	case OutcomeRedacted:
		return OutcomeWouldRedact
	default:
		return OutcomeAllowed
	}
}
