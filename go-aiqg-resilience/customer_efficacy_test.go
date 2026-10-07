package resilience

import (
	"strings"
	"testing"
)

// Tier 3 — the customer-supplied efficacy floor.
//
// Every test here fixes Samples well above the floor and varies only
// CustomerSamples, because the defect this floor is most likely to grow is the
// one the judged floor was written to avoid: testing evidence against the row's
// Samples instead of against the sub-signal's own count. That mistake passes
// every happy-path test and lets two late tickets decide a verdict.

func customerSignal(efficacy float64, customerSamples int, sources ...string) QualitySignal {
	return QualitySignal{
		Model: "m", Workflow: "w",
		Samples:               1000, // abundant structural evidence throughout
		Efficacy:              100,  // structural floor can never be the cause
		EfficacyCustomer:      efficacy,
		CustomerSamples:       customerSamples,
		CustomerSignalSources: sources,
	}
}

func TestCustomerFloorExcludesAndNamesItsSource(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 10}
	got := s.Gate(customerSignal(40, 50, "ticket_resolved", "human_override"), true)
	if got.Eligible {
		t.Fatalf("customer efficacy 40 against a floor of 70 must exclude, got %+v", got)
	}
	if got.Dimension != "customer_efficacy" {
		t.Errorf("Dimension = %q, want customer_efficacy", got.Dimension)
	}
	// The disclosure requirement: a verdict a customer signal decided must name it.
	if len(got.Source) != 2 {
		t.Errorf("Source = %v, want both labels", got.Source)
	}
	for _, want := range []string{"ticket_resolved", "human_override"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("Reason %q does not name %q", got.Reason, want)
		}
	}
}

// The core nil-is-a-value test. A customer who has instrumented NOTHING reports
// EfficacyCustomer 0 with CustomerSamples 0, which is numerically identical to a
// customer reporting total failure. Reading the first as the second would
// exclude every uninstrumented candidate the moment anyone set this floor.
func TestNeverReportedIsNotReportedZero(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 10}
	got := s.Gate(customerSignal(0, 0), true)
	if !got.Eligible {
		t.Fatalf("an uninstrumented candidate must stay eligible, got %+v", got)
	}
	if !strings.Contains(got.Reason, "abstained") {
		t.Errorf("abstention must be stated, got Reason=%q", got.Reason)
	}
	// Abstaining is not the same as passing, and must not name a source.
	if len(got.Source) != 0 {
		t.Errorf("an abstention must name no source, got %v", got.Source)
	}
}

// Evidence is counted against CustomerSamples, NOT the row's Samples. This is
// the test that bites if someone reuses sig.Samples here.
func TestCustomerEvidenceIsCountedSeparatelyFromSamples(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 200}
	// 1000 structural samples, 3 customer ones, a failing customer mean.
	got := s.Gate(customerSignal(10, 3), true)
	if !got.Eligible {
		t.Fatalf("3 customer samples against a floor of 200 must ABSTAIN, not exclude; got %+v", got)
	}
	if !strings.Contains(got.Reason, "3 customer samples") {
		t.Errorf("Reason should cite the customer count, got %q", got.Reason)
	}
}

func TestCustomerThinDataCanExcludeWhenAskedTo(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 200, OnInsufficientData: InsufficientExclude}
	got := s.Gate(customerSignal(100, 3), true)
	if got.Eligible {
		t.Fatalf("InsufficientExclude must exclude on thin customer evidence, got %+v", got)
	}
	if got.Dimension != "customer_samples" {
		t.Errorf("Dimension = %q, want customer_samples", got.Dimension)
	}
}

// Both sub-floors can abstain at once. Overwriting one with the other hides a
// fact: "we could not judge it" and "the customer never told us" are different.
func TestJudgedAndCustomerAbstentionsAreBothReported(t *testing.T) {
	s := Signals{MinJudgedEfficacy: 70, MinCustomerEfficacy: 70, MinSamples: 200}
	sig := customerSignal(0, 0)
	sig.JudgedSamples = 2
	sig.EfficacyJudged = 90
	got := s.Gate(sig, true)
	if !got.Eligible {
		t.Fatalf("both sub-signals thin must abstain, got %+v", got)
	}
	if !strings.Contains(got.Reason, "judged efficacy gate abstained") ||
		!strings.Contains(got.Reason, "customer efficacy gate abstained") {
		t.Errorf("both abstentions must appear, got %q", got.Reason)
	}
}

func TestPassingCustomerFloorNamesNoSource(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 10}
	got := s.Gate(customerSignal(95, 50, "tests_passed"), true)
	if !got.Eligible || got.Reason != "" || len(got.Source) != 0 {
		t.Fatalf("a clean pass must be silent, got %+v", got)
	}
}

// A customer-only rule must be VALID. The "no floor is set" guard predates this
// floor, so until it learns the third one it rejects exactly the rule Tier 3
// exists to enable.
func TestCustomerOnlyRuleIsValid(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 200}
	if err := s.Validate(); err != nil {
		t.Fatalf("a customer-only floor must validate, got %v", err)
	}
	if s.IsZero() {
		t.Error("a rule with a customer floor is not zero-valued")
	}
}

func TestCustomerFloorRangeIsChecked(t *testing.T) {
	for _, v := range []int{-1, 101} {
		if err := (Signals{MinCustomerEfficacy: v}).Validate(); err == nil {
			t.Errorf("MinCustomerEfficacy=%d must be rejected", v)
		}
	}
}

func TestUnnamedCustomerVerdictSaysSo(t *testing.T) {
	s := Signals{MinCustomerEfficacy: 70, MinSamples: 10}
	got := s.Gate(customerSignal(40, 50), true) // no sources supplied
	if got.Eligible {
		t.Fatal("expected exclusion")
	}
	if !strings.Contains(got.Reason, "unnamed") {
		t.Errorf("a sourceless customer verdict must read as unnamed, got %q", got.Reason)
	}
}
