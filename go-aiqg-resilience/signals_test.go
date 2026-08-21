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
