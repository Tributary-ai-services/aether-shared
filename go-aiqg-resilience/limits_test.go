package resilience

import (
	"strings"
	"testing"
)

var haiku = ModelCaps{Provider: "anthropic", Model: "claude-haiku-4-5", MaxContextWindow: 200000, MaxOutputTokens: 64000}
var mini = ModelCaps{Provider: "openai", Model: "gpt-4o-mini", MaxContextWindow: 128000, MaxOutputTokens: 16384}

func TestZeroLimitsAreValidAndInert(t *testing.T) {
	var l Limits
	if !l.IsZero() {
		t.Fatal("zero limits should report IsZero")
	}
	if err := l.ValidateFor([]ModelCaps{haiku}); err != nil {
		t.Fatalf("zero limits rejected: %v", err)
	}
	// Inert means the advertised values pass through untouched.
	if got := l.EffectiveContextWindow(200000); got != 200000 {
		t.Fatalf("effective window = %d, want the advertised value", got)
	}
}

// The rule that gives limits their point: raising a window does not extend a
// model's capability, it just moves the failure back to the vendor.
func TestALimitMayNotRaiseAWindow(t *testing.T) {
	err := Limits{MaxContextWindow: 500000}.ValidateFor([]ModelCaps{haiku})
	if err == nil {
		t.Fatal("a limit above the advertised window was accepted")
	}
	// An operator who typed 500000 needs to see the real window, not merely
	// that their number was rejected.
	if !strings.Contains(err.Error(), "200000") {
		t.Fatalf("error %q should name the advertised window", err)
	}
	if !strings.Contains(err.Error(), "only lower") {
		t.Fatalf("error %q should state the rule", err)
	}
}

func TestALimitMayLower(t *testing.T) {
	if err := (Limits{MaxContextWindow: 50000, MaxOutputTokens: 4096}).ValidateFor([]ModelCaps{haiku}); err != nil {
		t.Fatalf("a lowering limit was rejected: %v", err)
	}
}

// Validated against EVERY candidate model, since a rule may route to several
// and a window valid for one can exceed another's.
func TestValidatedAgainstEveryCandidate(t *testing.T) {
	// 150000 fits Haiku's 200000 but exceeds gpt-4o-mini's 128000.
	err := Limits{MaxContextWindow: 150000}.ValidateFor([]ModelCaps{haiku, mini})
	if err == nil {
		t.Fatal("a limit valid for one model but not another was accepted")
	}
	if !strings.Contains(err.Error(), "gpt-4o-mini") {
		t.Fatalf("error %q should name the model that cannot honour it", err)
	}
}

func TestOutputCapIsAlsoBounded(t *testing.T) {
	if err := (Limits{MaxOutputTokens: 999999}).ValidateFor([]ModelCaps{haiku}); err == nil {
		t.Fatal("an output cap above the advertised maximum was accepted")
	}
}

// A stale rule — written before a model's window shrank — must not raise a
// limit at run time even though write-time validation once passed.
func TestEffectiveWindowClampsAStaleLimit(t *testing.T) {
	l := Limits{MaxContextWindow: 500000}
	if got := l.EffectiveContextWindow(200000); got != 200000 {
		t.Fatalf("effective window = %d; a configured value above advertised must clamp", got)
	}
}

func TestEffectiveWindowTakesTheSmaller(t *testing.T) {
	l := Limits{MaxContextWindow: 50000}
	if got := l.EffectiveContextWindow(200000); got != 50000 {
		t.Fatalf("effective window = %d, want the configured lower value", got)
	}
	// No opinion from either side falls back to the other.
	if got := (Limits{}).EffectiveContextWindow(0); got != 0 {
		t.Fatalf("with nothing known the window should be 0 (unbounded), got %d", got)
	}
}

// The caller's own max_tokens participates rather than being overridden:
// asking for fewer than the cap is a legitimate choice, and a cap is a ceiling
// rather than a target.
func TestEffectiveOutputTakesTheSmallestOfThree(t *testing.T) {
	l := Limits{MaxOutputTokens: 4096}
	if got := l.EffectiveOutputTokens(64000, 100); got != 100 {
		t.Fatalf("got %d; the caller asking for less must win", got)
	}
	if got := l.EffectiveOutputTokens(64000, 50000); got != 4096 {
		t.Fatalf("got %d; the tenant cap must bound a larger request", got)
	}
	if got := (Limits{}).EffectiveOutputTokens(16384, 50000); got != 16384 {
		t.Fatalf("got %d; the advertised maximum must bound the request", got)
	}
	if got := (Limits{}).EffectiveOutputTokens(0, 0); got != 0 {
		t.Fatalf("with nothing known the cap should be 0, got %d", got)
	}
}

// The overage tells an operator whether to trim a prompt or change models;
// "too large" does not.
func TestExceedsContextReportsTheOverage(t *testing.T) {
	l := Limits{MaxContextWindow: 200000}
	over, by := l.ExceedsContext(203000, 1000000)
	if !over || by != 3000 {
		t.Fatalf("over=%v by=%d, want a 3000-token overage against the configured window", over, by)
	}
	if under, _ := l.ExceedsContext(199999, 1000000); under {
		t.Fatal("a request inside the window was reported as exceeding it")
	}
	// Unbounded means nothing can exceed.
	if over, _ := (Limits{}).ExceedsContext(9_999_999, 0); over {
		t.Fatal("an unbounded window reported an overage")
	}
}
