package workload

import "testing"

func mk(ids ...string) []Assignment {
	out := make([]Assignment, 0, len(ids))
	for _, id := range ids {
		out = append(out, Assignment{ClassID: id, Version: "v", Vantage: VantageSettled})
	}
	return out
}

// The rule the measurement chose: the most consequential activity names the
// segment, because the reads were in service of the edit.
func TestPrecedenceNamesTheSegmentByItsConsequentialAct(t *testing.T) {
	got := RollUp(mk("code.discovery", "code.discovery", "code.discovery", "code.modification"), RollUpPrecedence)
	if got.ClassID != "code.modification" {
		t.Errorf("class = %q, want code.modification — three reads served one edit", got.ClassID)
	}
	if got.Evidence == "" {
		t.Error("roll-up carries no evidence")
	}
}

func TestPluralityPicksTheMostFrequent(t *testing.T) {
	got := RollUp(mk("code.discovery", "code.discovery", "code.discovery", "code.modification"), RollUpPlurality)
	if got.ClassID != "code.discovery" {
		t.Errorf("class = %q, want code.discovery", got.ClassID)
	}
}

// Coverage is the confidence: an archetype resting on one classified turn out
// of ten is exactly that thin, and the caller has to be able to see it.
func TestConfidenceReportsClassifiedCoverage(t *testing.T) {
	turns := mk(ClassUnclassified, ClassUnclassified, ClassUnclassified, "code.modification")
	got := RollUp(turns, RollUpPrecedence)
	if got.ClassID != "code.modification" {
		t.Fatalf("class = %q", got.ClassID)
	}
	if got.Confidence != 0.25 {
		t.Errorf("confidence = %v, want 0.25 — one classified turn in four", got.Confidence)
	}
}

// A segment nothing could classify abstains with a reason, like every other
// abstention in this package.
func TestRollUpAbstainsWhenNothingClassified(t *testing.T) {
	got := RollUp(mk(ClassUnclassified, ClassUnclassified), RollUpPrecedence)
	if got.ClassID != ClassUnclassified {
		t.Errorf("class = %q, want unclassified", got.ClassID)
	}
	if got.Reason == "" {
		t.Error("abstention with no reason")
	}
	if empty := RollUp(nil, RollUpPrecedence); empty.ClassID != ClassUnclassified {
		t.Error("an empty segment must abstain")
	}
}

// A class outside the precedence list must not be discarded — falling back to
// plurality keeps a real classification rather than throwing it away.
func TestPrecedenceFallsBackForUnlistedClasses(t *testing.T) {
	got := RollUp(mk("custom.thing", "custom.thing", "other.thing"), RollUpPrecedence)
	if got.ClassID != "custom.thing" {
		t.Errorf("class = %q, want custom.thing via plurality fallback", got.ClassID)
	}
}

// Ties must not depend on map iteration.
func TestRollUpPluralityTiesAreDeterministic(t *testing.T) {
	turns := mk("code.execution", "code.discovery")
	first := RollUp(turns, RollUpPlurality).ClassID
	for i := 0; i < 50; i++ {
		if got := RollUp(turns, RollUpPlurality).ClassID; got != first {
			t.Fatalf("tie resolved differently: %q then %q", first, got)
		}
	}
}
