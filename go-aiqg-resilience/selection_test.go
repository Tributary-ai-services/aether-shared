package resilience

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Selection.
// ---------------------------------------------------------------------------

func TestSelectionZeroIsValidAndMeansTodaysBehaviour(t *testing.T) {
	var s Selection
	if err := s.Validate(); err != nil {
		t.Fatalf("zero selection rejected: %v", err)
	}
	if !s.IsZero() {
		t.Fatal("zero selection should report IsZero")
	}
}

func TestSelectionAcceptsEveryStrategy(t *testing.T) {
	for _, st := range Strategies {
		s := Selection{Strategy: st}
		if st == StrategyWeighted {
			s.Weights = map[string]int{"openai": 1}
		}
		if err := s.Validate(); err != nil {
			t.Errorf("strategy %q rejected: %v", st, err)
		}
	}
	if err := (Selection{Strategy: "cheapest"}).Validate(); err == nil {
		t.Fatal("an invented strategy was accepted")
	}
}

// Weights that no strategy reads are a silent no-op, and a weighted split that
// never happens is the kind of thing an operator discovers from a bill.
func TestWeightsWithoutWeightedStrategyRejected(t *testing.T) {
	err := Selection{Strategy: StrategyCost, Weights: map[string]int{"openai": 50}}.Validate()
	if err == nil {
		t.Fatal("weights were accepted with a strategy that ignores them")
	}
	if !strings.Contains(err.Error(), "no effect") {
		t.Fatalf("error %q should say the weights do nothing", err)
	}
}

func TestWeightedRequiresUsableWeights(t *testing.T) {
	if err := (Selection{Strategy: StrategyWeighted}).Validate(); err == nil {
		t.Fatal("strategy=weighted accepted with no weights")
	}
	// All-zero would divide by zero at selection time, and "no traffic
	// anywhere" is never what someone means.
	err := Selection{Strategy: StrategyWeighted, Weights: map[string]int{"openai": 0, "anthropic": 0}}.Validate()
	if err == nil {
		t.Fatal("all-zero weights accepted")
	}
	if !strings.Contains(err.Error(), "ever be selected") {
		t.Fatalf("error %q should explain the consequence", err)
	}
	if err := (Selection{Strategy: StrategyWeighted, Weights: map[string]int{"openai": -1}}).Validate(); err == nil {
		t.Fatal("a negative weight was accepted")
	}
}

// ---------------------------------------------------------------------------
// Switching.
// ---------------------------------------------------------------------------

func TestSwitchingValidate(t *testing.T) {
	if err := (Switching{}).Validate(); err != nil {
		t.Fatalf("zero switching rejected: %v", err)
	}
	good := Switching{MinImprovementPct: 25, DwellSeconds: 60, WarmCacheBiasPct: 15}
	if err := good.Validate(); err != nil {
		t.Fatalf("typical switching rejected: %v", err)
	}
	for _, bad := range []Switching{
		{MinImprovementPct: 101}, {MinImprovementPct: -1},
		{DwellSeconds: -1}, {DwellSeconds: 99999},
		{WarmCacheBiasPct: 101},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("invalid switching accepted: %+v", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// Verbosity — the measurement that makes expected_cost honest.
// ---------------------------------------------------------------------------

// Routing money on a handful of requests is how a measurement-driven router
// becomes worse than the guess it replaced.
func TestVerbosityAbstainsBelowTheSampleFloor(t *testing.T) {
	v := Verbosity{Model: "gpt-4o-mini", MeanOutputTokens: 3, Samples: 13}
	if v.Usable(DefaultVerbositySampleFloor) {
		t.Fatal("a 13-sample measurement was accepted as usable")
	}
	v.Samples = 412
	if !v.Usable(DefaultVerbositySampleFloor) {
		t.Fatal("a 412-sample measurement was rejected")
	}
}

func TestStaleVerbosityIsUnusableRegardlessOfSampleSize(t *testing.T) {
	v := Verbosity{MeanOutputTokens: 131, Samples: 100000, Stale: true}
	if v.Usable(0) {
		t.Fatal("a stale measurement was used; sample size does not make an old number current")
	}
}

func TestZeroMeanIsUnusable(t *testing.T) {
	// A mean of zero cannot price anything, and would make a model look free.
	v := Verbosity{MeanOutputTokens: 0, Samples: 100000}
	if v.Usable(0) {
		t.Fatal("a zero-mean measurement was accepted")
	}
}

// The rule that makes the economics legible: "cheaper per token" and "cheaper
// per request" are different claims.
func TestVerbosityBudget(t *testing.T) {
	// A model priced at half the output rate may be twice as verbose and still
	// break even.
	if got := VerbosityBudget(0.075, 0.0375); got != 2 {
		t.Fatalf("VerbosityBudget = %v, want 2", got)
	}
	// Equal prices leave no headroom at all.
	if got := VerbosityBudget(0.01, 0.01); got != 1 {
		t.Fatalf("equal prices should give a budget of 1, got %v", got)
	}
	// A missing price must answer "cannot say" rather than a number that looks
	// authoritative.
	if got := VerbosityBudget(0, 0.01); got != 0 {
		t.Fatalf("missing price should yield 0, got %v", got)
	}
}
