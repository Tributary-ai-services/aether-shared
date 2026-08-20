package resilience

import (
	"strings"
	"testing"
)

// stubCatalogue is a fixed provider/model table.
type stubCatalogue struct{ models map[string][]string }

func (s stubCatalogue) HasModel(provider, model string) bool {
	for _, m := range s.models[strings.ToLower(provider)] {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return false
}
func (s stubCatalogue) Providers() []string { return []string{"anthropic", "openai"} }

var cat = stubCatalogue{models: map[string][]string{
	"anthropic": {"claude-haiku-4-5", "claude-opus-4-6"},
	"openai":    {"gpt-4o-mini", "gpt-4o"},
}}

func chain(entries ...ChainEntry) Fallback { return Fallback{Chain: entries} }

// ---------------------------------------------------------------------------
// The distinction this file exists to protect: eject-eligibility and
// fallback-eligibility are different axes.
// ---------------------------------------------------------------------------

// A context overflow is a 4xx — it never ejects the provider — but a
// larger-window model can serve the same request unchanged, so it MUST
// advance the chain. A rule of "4xx means don't try elsewhere" gets this
// wrong.
func TestContextOverflowAdvancesByDefault(t *testing.T) {
	if !chain().Advances(FailureContextOverflow) {
		t.Fatal("context_overflow must advance the chain — another model's window can serve it")
	}
}

func TestVendorErrorAndTimeoutAdvanceByDefault(t *testing.T) {
	for _, c := range []FailureClass{FailureVendorError, FailureTimeout} {
		if !chain().Advances(c) {
			t.Errorf("%s must advance by default", c)
		}
	}
}

// Failing over on throttling shifts load onto a second vendor exactly when the
// first is saturated. Usually right, occasionally how a partial brownout
// becomes a total one — so it is opt-in.
func TestRateLimitIsEligibleButNotDefault(t *testing.T) {
	if chain().Advances(FailureRateLimited) {
		t.Fatal("rate_limited must not advance by default")
	}
	optedIn := Fallback{On: []FailureClass{FailureRateLimited}}
	if !optedIn.Advances(FailureRateLimited) {
		t.Fatal("rate_limited must advance when explicitly configured")
	}
}

// Failing over on a content refusal means shopping for a vendor that will
// comply. A tenant should choose that deliberately, not inherit it.
func TestContentFilterIsNotDefault(t *testing.T) {
	if chain().Advances(FailureContentFilter) {
		t.Fatal("content_filter must not advance by default")
	}
}

// An explicit `on` replaces the default outright rather than adding to it —
// otherwise a tenant could never NARROW the set.
func TestExplicitOnReplacesTheDefault(t *testing.T) {
	f := Fallback{On: []FailureClass{FailureTimeout}}
	if !f.Advances(FailureTimeout) {
		t.Fatal("configured class does not advance")
	}
	if f.Advances(FailureVendorError) {
		t.Fatal("explicit `on` must replace the default set, not extend it")
	}
}

// ---------------------------------------------------------------------------
// Chain shape.
// ---------------------------------------------------------------------------

func TestEmptyChainIsValidAndMeansNoFailover(t *testing.T) {
	var f Fallback
	if err := f.Validate(); err != nil {
		t.Fatalf("empty chain rejected: %v", err)
	}
	if !f.IsZero() {
		t.Fatal("empty Fallback should report IsZero")
	}
}

// A fallback is often reached BECAUSE the caller's model was the problem, so
// carrying it forward reproduces the failure.
func TestChainEntryRequiresAModel(t *testing.T) {
	err := chain(ChainEntry{Provider: "openai"}).Validate()
	if err == nil {
		t.Fatal("chain entry without a model accepted")
	}
	if !strings.Contains(err.Error(), "repeats the failure") {
		t.Fatalf("error %q should explain why, not merely that the field is required", err)
	}
}

func TestChainEntryRequiresAProvider(t *testing.T) {
	if err := chain(ChainEntry{Model: "gpt-4o-mini"}).Validate(); err == nil {
		t.Fatal("chain entry without a provider accepted")
	}
}

func TestDuplicateTiersRejected(t *testing.T) {
	f := chain(
		ChainEntry{Provider: "openai", Model: "gpt-4o-mini"},
		ChainEntry{Provider: "OpenAI", Model: "GPT-4o-mini"},
	)
	err := f.Validate()
	if err == nil {
		t.Fatal("duplicate tier accepted (case-insensitively identical)")
	}
	if !strings.Contains(err.Error(), "fail the same way twice") {
		t.Fatalf("error %q should say why a duplicate is useless", err)
	}
}

func TestChainLengthIsBounded(t *testing.T) {
	var entries []ChainEntry
	for i := 0; i < MaxChainLength+1; i++ {
		entries = append(entries, ChainEntry{Provider: "openai", Model: string(rune('a' + i))})
	}
	err := chain(entries...).Validate()
	if err == nil {
		t.Fatal("over-long chain accepted")
	}
	// The reason matters: each tier costs a full timeout on the way to failing.
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error %q should explain the cost of a long chain", err)
	}
}

func TestUnknownFailureClassRejected(t *testing.T) {
	f := Fallback{On: []FailureClass{"kaboom"}}
	err := f.Validate()
	if err == nil {
		t.Fatal("unknown failure class accepted")
	}
	// An author who guessed a class name needs to know the real ones.
	if !strings.Contains(err.Error(), "vendor_error") {
		t.Fatalf("error %q should list the valid classes", err)
	}
}

// ---------------------------------------------------------------------------
// Validation against constraints, catalogue and target — the write-time gate.
// ---------------------------------------------------------------------------

// The acceptance criterion for this step: a denied vendor is rejected at write
// time, not discovered at failover.
func TestDeniedVendorRejectedInChain(t *testing.T) {
	c := Constraints{DenyVendors: []string{"openai"}}
	err := chain(ChainEntry{Provider: "openai", Model: "gpt-4o-mini"}).ValidateFor(c, cat, "", "")
	if err == nil {
		t.Fatal("a denied vendor was accepted as a fallback tier")
	}
	if !strings.Contains(err.Error(), "denied for this tenant") {
		t.Fatalf("error %q should name the constraint", err)
	}
}

// A constraint bypassable by capitalisation is not a constraint.
func TestDenyIsCaseInsensitive(t *testing.T) {
	c := Constraints{DenyVendors: []string{"OpenAI"}}
	if !c.Denies("openai") || !c.Denies("  OPENAI ") {
		t.Fatal("deny matching must be case- and whitespace-insensitive")
	}
}

// Policing the fallback path more strictly than the primary one would be
// absurd.
func TestDeniedVendorRejectedAsTarget(t *testing.T) {
	c := Constraints{DenyVendors: []string{"openai"}}
	if err := c.ValidateTarget("openai"); err == nil {
		t.Fatal("a denied vendor was accepted as a rule target")
	}
	if err := c.ValidateTarget("anthropic"); err != nil {
		t.Fatalf("an allowed vendor was rejected: %v", err)
	}
}

func TestUnknownModelRejectedAgainstCatalogue(t *testing.T) {
	err := chain(ChainEntry{Provider: "anthropic", Model: "gpt-4o-mini"}).ValidateFor(Constraints{}, cat, "", "")
	if err == nil {
		t.Fatal("a model the provider does not serve was accepted")
	}
	// This is the #151 failure mode, caught at write time instead of as an
	// opaque 500 during a failover.
	if !strings.Contains(err.Error(), "does not serve model") {
		t.Fatalf("error %q should say the provider does not serve that model", err)
	}
}

// Refusing every chain because no catalogue was available would be worse than
// skipping the model check.
func TestNilCatalogueSkipsModelCheck(t *testing.T) {
	if err := chain(ChainEntry{Provider: "anthropic", Model: "anything"}).ValidateFor(Constraints{}, nil, "", ""); err != nil {
		t.Fatalf("nil catalogue should skip model validation, got %v", err)
	}
}

// §6.1 rule 3. Retrying the same target is a retry, governed by budgets;
// spending the first tier on it re-attempts what just failed.
func TestChainMayNotBeginWithTheTarget(t *testing.T) {
	f := chain(
		ChainEntry{Provider: "openai", Model: "gpt-4o-mini"},
		ChainEntry{Provider: "anthropic", Model: "claude-haiku-4-5"},
	)
	err := f.ValidateFor(Constraints{}, cat, "openai", "gpt-4o-mini")
	if err == nil {
		t.Fatal("chain beginning with the rule's own target accepted")
	}
	if !strings.Contains(err.Error(), "that is a retry") {
		t.Fatalf("error %q should explain the distinction from a retry", err)
	}
}

// The same target LATER in the chain is legitimate: by then the earlier tiers
// have been tried, and coming back to it is a real alternative.
func TestTargetIsAllowedLaterInTheChain(t *testing.T) {
	f := chain(
		ChainEntry{Provider: "anthropic", Model: "claude-haiku-4-5"},
		ChainEntry{Provider: "openai", Model: "gpt-4o-mini"},
	)
	if err := f.ValidateFor(Constraints{}, cat, "openai", "gpt-4o-mini"); err != nil {
		t.Fatalf("target appearing after the first tier should be allowed: %v", err)
	}
}

func TestValidChainPasses(t *testing.T) {
	f := Fallback{
		Chain: []ChainEntry{
			{Provider: "anthropic", Model: "claude-haiku-4-5"},
			{Provider: "openai", Model: "gpt-4o"},
		},
		On: []FailureClass{FailureVendorError, FailureTimeout},
	}
	if err := f.ValidateFor(Constraints{DenyVendors: []string{"cohere"}}, cat, "openai", "gpt-4o-mini"); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
}

// An operator fixing a chain wants every problem at once.
func TestValidateForReportsAllProblems(t *testing.T) {
	f := chain(
		ChainEntry{Provider: "openai", Model: "nonexistent"},
		ChainEntry{Provider: "cohere", Model: "command"},
	)
	err := f.ValidateFor(Constraints{DenyVendors: []string{"cohere"}}, cat, "", "")
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"does not serve model", "denied for this tenant"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q omits %q", err, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Constraints.
// ---------------------------------------------------------------------------

func TestConstraintsValidate(t *testing.T) {
	if err := (Constraints{}).Validate(); err != nil {
		t.Fatalf("empty constraints rejected: %v", err)
	}
	if !(Constraints{}).IsZero() {
		t.Fatal("empty constraints should report IsZero")
	}
	if (Constraints{RequireZDR: true}).IsZero() {
		t.Fatal("require_zdr alone is not zero")
	}
	if err := (Constraints{DenyVendors: []string{"openai", "OpenAI"}}).Validate(); err == nil {
		t.Fatal("duplicate deny entry accepted")
	}
	if err := (Constraints{DenyVendors: []string{"  "}}).Validate(); err == nil {
		t.Fatal("empty deny entry accepted")
	}
}
