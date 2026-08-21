package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Selection and switching — routing-decision.md §5.7, step 5.
//
// # What expected_cost fixes
//
// Cost routing today prices a candidate using max_tokens — the CEILING — as its
// output estimate, defaulting to a literal 100 when unset. Output is 90-99% of
// the bill. So cost routing optimises the ~8% it can see and guesses the ~92%
// that decides the price.
//
// Measured across our own events on one workflow with near-identical input,
// models differ in output length by 7.3x. expected_cost replaces the guess with
// a measurement:
//
//	in_tokens x p_in  +  E[out_tokens | model, workflow] x p_out
//
// # The verbosity budget rule
//
// A model's verbosity budget IS its output-price ratio: B beats A while
//
//	out_B / out_A  <  p_out_A / p_out_B
//
// A model with a 10% price advantage has 10% of headroom. That framing is what
// makes the report legible: "cheaper per token" and "cheaper per request" are
// different claims, and only the second is the bill.
//
// # Why switching ships in the same step
//
// An expected-cost router without hysteresis flaps, and flapping pays the
// prompt-cache re-warm twice. Abandoning a warm prefix costs
// (1.25 - 0.10) x p_in x prefix_tokens as a one-off; at a measured 3,324-token
// RAG prefix, a 5% per-request saving needs 126 further requests to break even.
// A router free to chase a 5% improvement every minute never gets there.
//
// # Two guards that are not optional
//
// VERBOSITY IS NOT QUALITY. Unguarded, expected_cost optimises for terseness —
// it will happily prefer a model that answers badly in fewer tokens. It pairs
// with the efficacy floor in §5.8; until that exists, expected_cost should be
// treated as an opt-in for workloads where brevity is not a defect.
//
// MAX_TOKENS STAYS IN THE ESTIMATE AS A CAP, because a truncated answer is what
// you actually pay for. The expectation is what you expect to pay; the cap is
// what you cannot exceed.

// Strategy names a selection algorithm.
type Strategy string

const (
	// StrategyCost is today's behaviour: list price, with output estimated at
	// max_tokens. Kept as the default because it is what every existing route
	// already does.
	StrategyCost Strategy = "cost"
	// StrategyExpectedCost prices output from measured verbosity.
	StrategyExpectedCost Strategy = "expected_cost"
	// StrategyLatency prefers the fastest observed provider.
	StrategyLatency Strategy = "latency"
	// StrategyWeighted splits traffic by explicit weights.
	//
	// A canary rollout and a load split are the SAME mechanism, differing only
	// in who watches the result — which is the experiment runner's job, not
	// this one's. Modelling them separately would duplicate the machinery and
	// then have to keep the two in step.
	StrategyWeighted Strategy = "weighted"
	// StrategyPinned defers entirely to the rule's target.
	StrategyPinned Strategy = "pinned"
)

// Strategies lists every valid strategy.
var Strategies = []Strategy{StrategyCost, StrategyExpectedCost, StrategyLatency, StrategyWeighted, StrategyPinned}

// Selection is a rule's selection settings.
type Selection struct {
	// Strategy is the algorithm. Empty means cost — today's behaviour.
	Strategy Strategy `json:"strategy,omitempty"`
	// Weights maps provider to relative weight, for StrategyWeighted. Values
	// are relative, not percentages, so adding a third provider does not
	// require renormalising the other two by hand.
	Weights map[string]int `json:"weights,omitempty"`
}

// IsZero reports whether selection is unconfigured.
func (s Selection) IsZero() bool { return s.Strategy == "" && len(s.Weights) == 0 }

// Switching is the hysteresis that stops an expected-cost router from flapping.
type Switching struct {
	// MinImprovementPct is how much cheaper an alternative must be before a
	// switch is worth its cost. Below this, staying put wins.
	MinImprovementPct int `json:"min_improvement_pct,omitempty"`
	// DwellSeconds is the minimum time between switches for one routing
	// context, so a price oscillation cannot drive a switch per request.
	DwellSeconds int `json:"dwell_seconds,omitempty"`
	// WarmCacheBiasPct handicaps alternatives when the current provider holds
	// a warm prompt cache.
	//
	// This is the term that makes hysteresis honest rather than arbitrary: the
	// re-warm has a real price, so a switch should have to beat the
	// alternative PLUS that price, not merely beat the alternative.
	WarmCacheBiasPct int `json:"warm_cache_bias_pct,omitempty"`
}

// IsZero reports whether switching is unconfigured.
func (s Switching) IsZero() bool { return s == Switching{} }

// Defaults chosen from the measured break-even above: a 5% improvement needs
// 126 requests to repay a re-warm, a 25% improvement repays quickly. Dwell is
// deliberately longer than the 5m vendor cache is short, so a switch decision
// outlives at least a few turns.
const (
	DefaultMinImprovementPct = 25
	DefaultDwellSeconds      = 60
	DefaultWarmCacheBiasPct  = 15
)

// Validate checks selection settings.
func (s Selection) Validate() error {
	var errs []string
	if s.Strategy != "" && !validStrategy(s.Strategy) {
		errs = append(errs, fmt.Sprintf("strategy %q is not one of %s", s.Strategy, strategyNames()))
	}
	// Weights without the strategy that reads them is a silent no-op, and a
	// weighted split that never happens is the kind of thing an operator only
	// discovers from a bill.
	if len(s.Weights) > 0 && s.Strategy != StrategyWeighted {
		errs = append(errs, "weights are only used by strategy=weighted; with any other strategy they have no effect")
	}
	if s.Strategy == StrategyWeighted {
		if len(s.Weights) == 0 {
			errs = append(errs, "strategy=weighted requires weights")
		}
		total := 0
		for p, w := range s.Weights {
			if w < 0 {
				errs = append(errs, fmt.Sprintf("weight for %s is negative", p))
			}
			total += w
		}
		// All-zero weights would divide by zero at selection time, and "no
		// traffic anywhere" is never what someone means.
		if len(s.Weights) > 0 && total == 0 {
			errs = append(errs, "weights sum to zero, so no provider would ever be selected")
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Validate checks switching settings.
func (s Switching) Validate() error {
	var errs []string
	if s.MinImprovementPct < 0 || s.MinImprovementPct > 100 {
		errs = append(errs, "min_improvement_pct must be between 0 and 100")
	}
	if s.DwellSeconds < 0 || s.DwellSeconds > 3600 {
		errs = append(errs, "dwell_seconds must be between 0 and 3600")
	}
	if s.WarmCacheBiasPct < 0 || s.WarmCacheBiasPct > 100 {
		errs = append(errs, "warm_cache_bias_pct must be between 0 and 100")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Verbosity is a measured expectation of output length for one
// (model, workflow) pair.
//
// # Why this is measured rather than configured
//
// Nobody knows how many tokens a model will emit for a workload until it has
// emitted them. A configured number is a guess that ages silently; a measured
// one carries its own sample size and staleness, so a consumer can tell the
// difference between "we know" and "we have a number".
type Verbosity struct {
	Model    string `json:"model"`
	Workflow string `json:"workflow,omitempty"`
	// MeanOutputTokens is E[out_tokens] for this pair.
	MeanOutputTokens float64 `json:"mean_output_tokens"`
	// StdDevOutputTokens lets a consumer show how much to trust the mean. A
	// mean of 131 with a standard deviation of 61 is a very different fact
	// from a mean of 131 with a standard deviation of 4.
	StdDevOutputTokens float64 `json:"stddev_output_tokens,omitempty"`
	// Samples is the number of events behind the mean.
	Samples int `json:"samples"`
	// UpdatedAt is when this row was last computed, RFC3339.
	UpdatedAt string `json:"updated_at,omitempty"`
	// Stale marks a row whose window has passed without a refresh.
	Stale bool `json:"stale,omitempty"`
}

// DefaultVerbositySampleFloor is the minimum sample before a measurement may
// steer routing.
//
// Measured 2026-08-21 against 30 days of our own events, this floor excludes
// almost everything — one (model, workflow) pair clears it. That is the
// correct outcome, not a problem to tune away: routing money on a handful of
// requests is how a measurement-driven router becomes worse than the guess it
// replaced.
const DefaultVerbositySampleFloor = 100

// Usable reports whether a measurement may steer routing.
//
// A row below the floor, or stale, must not silently fall back to a guess: the
// caller is expected to say so on the event, so "we abstained" is visible
// rather than indistinguishable from "we measured and this is what we got".
func (v Verbosity) Usable(floor int) bool {
	if floor <= 0 {
		floor = DefaultVerbositySampleFloor
	}
	return !v.Stale && v.Samples >= floor && v.MeanOutputTokens > 0
}

// VerbosityBudget returns how much more verbose b may be than a before it
// stops being cheaper, given their output prices.
//
// Returns 1 when prices are equal (b must be no more verbose), and 0 when a
// price is missing — the honest answer being "cannot say" rather than a number
// that looks authoritative.
func VerbosityBudget(pOutA, pOutB float64) float64 {
	if pOutA <= 0 || pOutB <= 0 {
		return 0
	}
	return pOutA / pOutB
}

func validStrategy(s Strategy) bool {
	for _, v := range Strategies {
		if v == s {
			return true
		}
	}
	return false
}

func strategyNames() string {
	out := make([]string, len(Strategies))
	for i, s := range Strategies {
		out[i] = string(s)
	}
	return strings.Join(out, ", ")
}
