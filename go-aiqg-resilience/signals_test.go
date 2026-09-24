package resilience

import (
	"strings"
	"testing"
)

func sig(eff float64, worst Severity, samples int) QualitySignal {
	return QualitySignal{Model: "m", Efficacy: eff, WorstAssurance: worst, Samples: samples}
}

// ---------------------------------------------------------------------------
// Gates are floors, not optimisation terms: a candidate either clears or does
// not, and price only chooses between those that already have.
// ---------------------------------------------------------------------------

func TestEfficacyFloorExcludes(t *testing.T) {
	s := Signals{MinEfficacy: 70, MinSamples: 10}
	r := s.Gate(sig(51, SeverityNone, 100), true)
	if r.Eligible {
		t.Fatal("a candidate below the efficacy floor was admitted")
	}
	// "Excluded by signals" is never the whole answer — which dimension did it.
	if r.Dimension != "efficacy" {
		t.Fatalf("dimension = %q, want efficacy", r.Dimension)
	}
	if !strings.Contains(r.Reason, "below the floor") {
		t.Fatalf("reason %q should state the comparison", r.Reason)
	}
}

// Assurance is bucketed on WORST severity, not a count: one unauthorized
// disclosure invalidates otherwise perfect performance.
func TestAssuranceGateUsesWorstSeverity(t *testing.T) {
	s := Signals{MaxAssuranceSeverity: SeverityMedium, MinSamples: 10}
	if r := s.Gate(sig(100, SeverityHigh, 100), true); r.Eligible {
		t.Fatal("a high-severity finding passed a medium limit")
	} else if r.Dimension != "assurance" {
		t.Fatalf("dimension = %q, want assurance", r.Dimension)
	}
	if r := s.Gate(sig(100, SeverityLow, 100), true); !r.Eligible {
		t.Fatalf("a low-severity finding was excluded under a medium limit: %s", r.Reason)
	}
}

// An unrecognised severity must not silently pass: a typo in stored data would
// otherwise be equivalent to disabling the control.
func TestUnknownSeverityFailsClosed(t *testing.T) {
	if Severity("catastrophic").AtMost(SeverityCritical) {
		t.Fatal("an unrecognised severity passed a gate")
	}
	if SeverityLow.AtMost("bogus") {
		t.Fatal("an unrecognised limit admitted a candidate")
	}
}

func TestSeverityOrdering(t *testing.T) {
	if !SeverityNone.AtMost(SeverityCritical) || !SeverityMedium.AtMost(SeverityMedium) {
		t.Fatal("ordering is wrong at the permissive end")
	}
	if SeverityCritical.AtMost(SeverityHigh) {
		t.Fatal("critical passed a high limit")
	}
}

// ---------------------------------------------------------------------------
// Thin data. A control that takes the service down when it has no evidence is
// worse than no control.
// ---------------------------------------------------------------------------

func TestThinDataIsANoOpByDefault(t *testing.T) {
	s := Signals{MinEfficacy: 70, MinSamples: 200}
	r := s.Gate(sig(10, SeverityCritical, 5), true)
	if !r.Eligible {
		t.Fatal("a thin-data candidate was excluded by default; on a fresh tenant this would fail all traffic")
	}
	// Abstained is a different fact from passed, and the caller must be able to
	// record which.
	if !strings.Contains(r.Reason, "abstained") {
		t.Fatalf("reason %q should record that the gate abstained rather than passed", r.Reason)
	}
}

func TestExcludePolicyIsOptIn(t *testing.T) {
	s := Signals{MinEfficacy: 70, MinSamples: 200, OnInsufficientData: InsufficientExclude}
	if r := s.Gate(sig(100, SeverityNone, 5), true); r.Eligible {
		t.Fatal("on_insufficient_data=exclude admitted an unmeasured candidate")
	}
}

func TestMissingAndStaleCountAsThin(t *testing.T) {
	s := Signals{MinEfficacy: 70, MinSamples: 10}
	if r := s.Gate(QualitySignal{}, false); !r.Eligible || !strings.Contains(r.Reason, "no measurements") {
		t.Fatalf("missing evidence: eligible=%v reason=%q", r.Eligible, r.Reason)
	}
	stale := sig(10, SeverityCritical, 5000)
	stale.Stale = true
	// Sample size does not make an old number current.
	if r := s.Gate(stale, true); !r.Eligible || !strings.Contains(r.Reason, "stale") {
		t.Fatalf("stale evidence: eligible=%v reason=%q", r.Eligible, r.Reason)
	}
}

func TestNoSignalsConfiguredIsANoOp(t *testing.T) {
	var s Signals
	if !s.IsZero() {
		t.Fatal("zero signals should report IsZero")
	}
	if r := s.Gate(sig(1, SeverityCritical, 10000), true); !r.Eligible {
		t.Fatal("an unconfigured gate excluded a candidate")
	}
}

// ---------------------------------------------------------------------------
// Validation.
// ---------------------------------------------------------------------------

func TestSignalsValidate(t *testing.T) {
	if err := (Signals{}).Validate(); err != nil {
		t.Fatalf("zero signals rejected: %v", err)
	}
	good := Signals{MinEfficacy: 70, MaxAssuranceSeverity: SeverityMedium, MinSamples: 200, MaxStalenessHours: 24}
	if err := good.Validate(); err != nil {
		t.Fatalf("typical signals rejected: %v", err)
	}
	for _, bad := range []Signals{
		{MinEfficacy: 101}, {MinEfficacy: -1},
		{MaxAssuranceSeverity: "spicy"},
		{MinSamples: -1}, {MaxStalenessHours: -1}, {MaxStalenessHours: 100000},
		{MinEfficacy: 70, OnInsufficientData: "maybe"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("invalid signals accepted: %+v", bad)
		}
	}
}

// Settings that only matter alongside a floor are refused rather than stored,
// so nobody believes quality is enforced when nothing is gated.
func TestSettingsWithoutAFloorAreRefused(t *testing.T) {
	err := Signals{MinSamples: 200, MaxStalenessHours: 24}.Validate()
	if err == nil {
		t.Fatal("sample/staleness settings accepted with no quality floor")
	}
	if !strings.Contains(err.Error(), "nothing is gated") {
		t.Fatalf("error %q should say nothing is gated", err)
	}
}

// Synthetic exclusion defaults ON. Measured over 30 days, synthetic traffic
// inflated BOTH efficacy and assurance — probes asking for "ok" finish with
// stop and contain nothing to redact.
func TestSyntheticExclusionDefaultsOn(t *testing.T) {
	if !(Signals{}).Excludes() {
		t.Fatal("synthetic exclusion defaulted off")
	}
	off := false
	if (Signals{ExcludeSynthetic: &off}).Excludes() {
		t.Fatal("explicit false was not honoured")
	}
}

func TestDefaultsAreConservative(t *testing.T) {
	var s Signals
	// 200 is deliberately high relative to current volume: on today's data
	// nothing clears it, so gating is a no-op — the correct state for a control
	// whose evidence does not yet exist.
	if s.MinSamplesOrDefault() != DefaultSignalMinSamples {
		t.Fatalf("sample floor default = %d", s.MinSamplesOrDefault())
	}
	if s.StalenessOrDefault() != DefaultSignalStalenessHrs {
		t.Fatalf("staleness default = %d", s.StalenessOrDefault())
	}
}

// ---------------------------------------------------------------------------
// The judged floor. Structural efficacy is finish-reason-only, so a fluent
// wrong answer scores 100 against it; the judged floor is what can see that.
// It is a SEPARATE floor over SEPARATE evidence, and the tests below pin both
// halves of that separation.
// ---------------------------------------------------------------------------

func judged(eff, judgedEff float64, samples, judgedSamples int) QualitySignal {
	q := sig(eff, SeverityNone, samples)
	q.EfficacyJudged = judgedEff
	q.JudgedSamples = judgedSamples
	if samples > 0 {
		q.EfficacyJudgedCoverage = float64(judgedSamples) / float64(samples)
	}
	return q
}

// The case the whole tier exists for: structural says 100, the judge says 53.
// Measured in production on claude-haiku-4-5 / classification_extraction.
func TestJudgedFloorExcludesWhatStructuralCannotSee(t *testing.T) {
	s := Signals{MinEfficacy: 70, MinJudgedEfficacy: 85, MinSamples: 10}
	r := s.Gate(judged(100, 53, 100, 100), true)
	if r.Eligible {
		t.Fatal("a fluent-but-wrong candidate cleared both floors; the judged floor is not gating")
	}
	if r.Dimension != "judged_efficacy" {
		t.Fatalf("dimension = %q, want judged_efficacy", r.Dimension)
	}
	if !strings.Contains(r.Reason, "judged efficacy") {
		t.Fatalf("reason %q should name the judged dimension, not the structural one", r.Reason)
	}
}

// The floors are independent: clearing one says nothing about the other.
func TestJudgedFloorIsIndependentOfStructural(t *testing.T) {
	s := Signals{MinEfficacy: 90, MinJudgedEfficacy: 50, MinSamples: 10}
	// Truncating a lot (structural 60) but answering correctly when it does.
	r := s.Gate(judged(60, 95, 100, 100), true)
	if r.Eligible {
		t.Fatal("a candidate below the structural floor was admitted because its judged score was high")
	}
	if r.Dimension != "efficacy" {
		t.Fatalf("dimension = %q, want efficacy", r.Dimension)
	}
}

// A cell can hold a thousand structural samples and no judged ones. Gating the
// judged floor on Samples would read that as strong evidence for a measurement
// nobody took.
func TestJudgedFloorAbstainsOnItsOwnSampleCount(t *testing.T) {
	s := Signals{MinJudgedEfficacy: 85, MinSamples: 200}
	r := s.Gate(judged(100, 0, 1145, 0), true)
	if !r.Eligible {
		t.Fatal("a candidate with no judged samples was excluded; zero judged evidence is not a zero score")
	}
	// Passing because nobody could judge it must not look like passing on merit.
	if !strings.Contains(r.Reason, "abstained") {
		t.Fatalf("reason %q should record the judged abstention", r.Reason)
	}
}

// Same thin-judged-evidence case, but for the tenant who would rather fail than
// route on unmeasured quality.
func TestJudgedThinEvidenceCanExcludeWhenOptedIn(t *testing.T) {
	s := Signals{MinJudgedEfficacy: 85, MinSamples: 200, OnInsufficientData: InsufficientExclude}
	r := s.Gate(judged(100, 0, 1145, 3), true)
	if r.Eligible {
		t.Fatal("on_insufficient_data=exclude admitted a candidate with 3 judged samples")
	}
	if r.Dimension != "judged_samples" {
		t.Fatalf("dimension = %q, want judged_samples", r.Dimension)
	}
}

// A judged floor alone is a real gate — it must not trip the "nothing is
// gated" refusal that exists to catch a rule whose author thinks it enforces
// something.
func TestJudgedFloorAloneIsAValidRule(t *testing.T) {
	s := Signals{MinJudgedEfficacy: 85, MinSamples: 200}
	if err := s.Validate(); err != nil {
		t.Fatalf("a judged-only rule was rejected: %v", err)
	}
	if s.IsZero() {
		t.Fatal("a rule with a judged floor reported itself as no gating configured")
	}
	if err := (Signals{MinJudgedEfficacy: 101}).Validate(); err == nil {
		t.Fatal("min_judged_efficacy above 100 was accepted")
	}
}
