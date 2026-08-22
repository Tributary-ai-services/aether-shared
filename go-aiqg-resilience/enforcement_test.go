package resilience

import (
	"strings"
	"testing"
)

// Absent configuration must never start blocking traffic: an operator who has
// not chosen enforcement has not consented to it.
func TestDefaultIsObserve(t *testing.T) {
	var e Enforcement
	if e.Enforcing() {
		t.Fatal("unconfigured enforcement was enforcing")
	}
	if DefaultMode != ModeObserve {
		t.Fatalf("DefaultMode = %v, want observe", DefaultMode)
	}
	if !e.IsZero() {
		t.Fatal("zero enforcement should report IsZero")
	}
}

// Rule 10. A bundle of nothing but log rules set to enforce claims to protect
// something and protects nothing — and the misconfiguration is INVISIBLE,
// because traffic flows exactly as before. Write time is the only moment anyone
// would notice.
func TestEnforcingRequiresARuleThatActs(t *testing.T) {
	e := Enforcement{Mode: ModeEnforce}
	err := e.ValidateFor(0)
	if err == nil {
		t.Fatal("a bundle with no acting rules was allowed to enforce")
	}
	if !strings.Contains(err.Error(), "would change nothing") {
		t.Fatalf("error %q should say why it was refused", err)
	}
	if err := e.ValidateFor(1); err != nil {
		t.Fatalf("a bundle with an acting rule was refused: %v", err)
	}
}

// Observe is always allowed, including on an all-logging bundle — that is
// precisely the bundle observe exists for.
func TestObserveIsAlwaysAllowed(t *testing.T) {
	if err := (Enforcement{Mode: ModeObserve}).ValidateFor(0); err != nil {
		t.Fatalf("observe was refused on an all-logging bundle: %v", err)
	}
}

func TestValidateRejectsUnknownValues(t *testing.T) {
	if err := (Enforcement{Mode: "block-everything"}).Validate(); err == nil {
		t.Fatal("an invented mode was accepted")
	}
	if err := (Enforcement{OnPolicyUnavailable: "maybe"}).Validate(); err == nil {
		t.Fatal("an invented fail mode was accepted")
	}
}

// Policy being unreachable is an availability problem; converting it into a
// total outage means one degraded dependency takes down every tenant.
func TestFailOpenIsTheDefault(t *testing.T) {
	if (Enforcement{Mode: ModeEnforce}).FailsClosed() {
		t.Fatal("unconfigured failure handling defaulted to closed")
	}
	if !(Enforcement{OnPolicyUnavailable: FailClosed}).FailsClosed() {
		t.Fatal("explicit fail-closed was not honoured")
	}
}

// The difference between "we blocked this" and "we would have" is the entire
// value of observe mode; a shared label would erase it.
func TestObservedOutcomesAreDistinctFromRealOnes(t *testing.T) {
	if OutcomeBlocked.Observed() != OutcomeWouldBlock {
		t.Fatal("a blocked outcome did not map to would_block in observe")
	}
	if OutcomeRedacted.Observed() != OutcomeWouldRedact {
		t.Fatal("a redacted outcome did not map to would_redact in observe")
	}
	if OutcomeAllowed.Observed() != OutcomeAllowed {
		t.Fatal("an allowed outcome changed under observe")
	}
	// And they must not collide as strings.
	if string(OutcomeBlocked) == string(OutcomeWouldBlock) {
		t.Fatal("blocked and would_block share a label")
	}
}
