package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Quality signals as a routing input — routing-decision.md §5.8, step 6.
//
// # Floors and gates, never an optimisation target
//
// CLEAR dimensions FILTER the candidate set; cost and latency choose among the
// survivors. The ordering is lexicographic on purpose, so there is never a 1:1
// dollar-versus-quality trade to argue about — a candidate either clears the
// floor or it does not, and price only decides between candidates that already
// have.
//
// This matters most for what step 5 shipped. expected_cost optimises for fewer
// output tokens, which is not the same as better answers: unguarded it prefers a
// model that replies badly in half the words. A quality floor is the guard, and
// it works because it runs BEFORE selection rather than as a term inside it.
//
// # Dimensions, never composite
//
// Composite renormalises over whichever dimensions happened to be present, and
// is dominated by cost — measured, Opus scores 100 on every quality dimension
// and lands at 67 composite. Routing on composite would mean a dashboard
// weighting change silently re-routes production traffic. Gates read the
// dimensions directly.
//
// # Two loops at two timescales
//
// Fast signals — 5xx, timeout, 429 — decide EJECTION sub-second, and that is
// the breaker's job. Slow CLEAR aggregates decide ELIGIBILITY over minutes to
// days, and that is this. Post-hoc scoring cannot eject; 5xx counting cannot
// judge quality. Conflating them produces a system that is both too slow to
// protect and too twitchy to trust.
//
// # Synthetic traffic must be excluded, and the reason is sharper than it looks
//
// Measured 2026-08-21 over 30 days, with and without the synthetic denylist:
//
//	model              n (all)  eff (all)  assur (all) | n (real)  eff  assur
//	claude-haiku-4-5       562         81           97 |       73   84     87
//	gpt-4o-mini             23         96          100 |        0    -      -
//
// Every gpt-4o-mini score came from probes that asked for the single word "ok",
// finished with `stop`, and contained nothing to redact — so they scored near
// perfect on both dimensions. Synthetic traffic INFLATES quality, and it does so
// on efficacy and assurance together.
//
// Combined with step 5's finding — the same probes made the model look ~16x
// cheaper — an unguarded router would conclude that whatever we test with is
// both the cheapest AND the best model available, and send it everything. The
// two biases point the same way, which is what makes them dangerous rather than
// merely noisy.

// Severity is an assurance finding's worst severity, in ascending order.
type Severity string

const (
	SeverityNone     Severity = "none"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// severityRank orders severities for comparison. Assurance is already bucketed
// on WORST severity rather than a count — the reasoning being that one
// unauthorized disclosure invalidates otherwise perfect performance — so it is
// already gate-shaped and complements constraints exactly.
var severityRank = map[Severity]int{
	SeverityNone: 0, SeverityLow: 1, SeverityMedium: 2, SeverityHigh: 3, SeverityCritical: 4,
}

// Severities lists valid values in ascending order.
var Severities = []Severity{SeverityNone, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}

// AtMost reports whether s is no worse than limit.
func (s Severity) AtMost(limit Severity) bool {
	sr, ok1 := severityRank[Severity(strings.ToLower(string(s)))]
	lr, ok2 := severityRank[Severity(strings.ToLower(string(limit)))]
	if !ok1 || !ok2 {
		// An unrecognised severity must not silently pass a gate. Treating it
		// as acceptable would make a typo in stored data equivalent to
		// disabling the control.
		return false
	}
	return sr <= lr
}

// InsufficientDataPolicy selects what to do when the evidence is too thin to
// judge.
type InsufficientDataPolicy string

const (
	// InsufficientIgnore treats a thin-data candidate as eligible.
	//
	// The default, and the safe one. A gate that excluded every unmeasured
	// candidate would, on a fresh tenant, exclude EVERY candidate and fail all
	// traffic — a quality control that takes the service down when it has no
	// evidence is worse than no control.
	InsufficientIgnore InsufficientDataPolicy = "ignore"
	// InsufficientExclude treats a thin-data candidate as ineligible. For
	// tenants who would rather fail than route on unmeasured quality — a real
	// position, but one that must be chosen deliberately.
	InsufficientExclude InsufficientDataPolicy = "exclude"
)

// Signals is a rule's quality gating.
type Signals struct {
	// MinEfficacy excludes candidates scoring below this (0-100).
	MinEfficacy int `json:"min_efficacy,omitempty"`
	// MaxAssuranceSeverity excludes candidates whose worst observed finding is
	// more severe than this.
	MaxAssuranceSeverity Severity `json:"max_assurance_severity,omitempty"`
	// MinSamples is the evidence required before a gate may exclude anything.
	MinSamples int `json:"min_samples,omitempty"`
	// MaxStalenessHours bounds how old an aggregate may be and still gate.
	MaxStalenessHours int `json:"max_staleness_hours,omitempty"`
	// ExcludeSynthetic drops test and demo traffic from the aggregates.
	// Default true — see the package doc for why this is not optional in
	// practice.
	ExcludeSynthetic *bool `json:"exclude_synthetic,omitempty"`
	// OnInsufficientData selects behaviour below the sample floor.
	OnInsufficientData InsufficientDataPolicy `json:"on_insufficient_data,omitempty"`
}

// Default sample floor and staleness. 200 is deliberately high relative to
// current volume: on today's data NOTHING clears it, so gating is a no-op — the
// correct state for a control whose evidence does not yet exist.
const (
	DefaultSignalMinSamples   = 200
	DefaultSignalStalenessHrs = 24
	DefaultExcludeSynthetic   = true
)

// IsZero reports whether no gating is configured.
func (s Signals) IsZero() bool {
	return s.MinEfficacy == 0 && s.MaxAssuranceSeverity == "" &&
		s.MinSamples == 0 && s.MaxStalenessHours == 0 &&
		s.ExcludeSynthetic == nil && s.OnInsufficientData == ""
}

// Excludes reports whether the tenant wants synthetic traffic dropped.
func (s Signals) Excludes() bool {
	if s.ExcludeSynthetic == nil {
		return DefaultExcludeSynthetic
	}
	return *s.ExcludeSynthetic
}

// MinSamplesOrDefault returns the effective sample floor.
func (s Signals) MinSamplesOrDefault() int {
	if s.MinSamples <= 0 {
		return DefaultSignalMinSamples
	}
	return s.MinSamples
}

// StalenessOrDefault returns the effective staleness bound, in hours.
func (s Signals) StalenessOrDefault() int {
	if s.MaxStalenessHours <= 0 {
		return DefaultSignalStalenessHrs
	}
	return s.MaxStalenessHours
}

// Validate checks a signals block.
func (s Signals) Validate() error {
	var errs []string
	if s.MinEfficacy < 0 || s.MinEfficacy > 100 {
		errs = append(errs, "min_efficacy must be between 0 and 100")
	}
	if s.MaxAssuranceSeverity != "" {
		if _, ok := severityRank[Severity(strings.ToLower(string(s.MaxAssuranceSeverity)))]; !ok {
			errs = append(errs, fmt.Sprintf("max_assurance_severity %q is not one of %s",
				s.MaxAssuranceSeverity, severityNames()))
		}
	}
	if s.MinSamples < 0 {
		errs = append(errs, "min_samples must not be negative")
	}
	if s.MaxStalenessHours < 0 || s.MaxStalenessHours > 24*30 {
		errs = append(errs, "max_staleness_hours must be between 0 and 720")
	}
	if s.OnInsufficientData != "" &&
		s.OnInsufficientData != InsufficientIgnore && s.OnInsufficientData != InsufficientExclude {
		errs = append(errs, fmt.Sprintf("on_insufficient_data %q is not ignore or exclude", s.OnInsufficientData))
	}
	// A gate with no threshold gates nothing. Refusing it beats storing a rule
	// whose author believes quality is being enforced.
	if s.MinEfficacy == 0 && s.MaxAssuranceSeverity == "" &&
		(s.MinSamples > 0 || s.MaxStalenessHours > 0 || s.OnInsufficientData != "") {
		errs = append(errs, "no quality floor is set (min_efficacy or max_assurance_severity), so nothing is gated and the other settings have no effect")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// QualitySignal is a measured CLEAR aggregate for one (model, workflow).
//
// Dimensions are carried separately and composite is deliberately absent — see
// the package doc.
type QualitySignal struct {
	Model    string `json:"model"`
	Workflow string `json:"workflow,omitempty"`
	// Efficacy is the mean efficacy score (0-100).
	Efficacy float64 `json:"efficacy"`
	// EfficacyCoverage is the share of samples that carried an efficacy score
	// at all. A mean over 70% coverage is a different claim from one over
	// 100%, and a gate reading only the mean would not know the difference.
	EfficacyCoverage float64 `json:"efficacy_coverage"`
	// WorstAssurance is the worst severity observed.
	WorstAssurance Severity `json:"worst_assurance,omitempty"`
	// Samples is the evidence behind the aggregate.
	Samples int `json:"samples"`
	// UpdatedAt is RFC3339.
	UpdatedAt string `json:"updated_at,omitempty"`
	// Stale marks an aggregate past its staleness bound.
	Stale bool `json:"stale,omitempty"`
}

// GateResult explains a gating decision, for the event and the UI.
type GateResult struct {
	Eligible bool
	// Dimension names which floor excluded the candidate — efficacy or
	// assurance — so "excluded by signals" is never the whole answer.
	Dimension string
	Reason    string
}

// Gate evaluates a candidate against the floors.
//
// Returns eligible with an explanatory reason when the evidence is too thin,
// so a caller can record that the gate ABSTAINED rather than passed. Those are
// different facts: one says the candidate is good, the other says we do not
// know.
func (s Signals) Gate(sig QualitySignal, found bool) GateResult {
	if s.IsZero() {
		return GateResult{Eligible: true}
	}
	thin := !found || sig.Samples < s.MinSamplesOrDefault() || sig.Stale
	if thin {
		if s.OnInsufficientData == InsufficientExclude {
			return GateResult{Eligible: false, Dimension: "samples",
				Reason: "insufficient quality evidence and this rule excludes unmeasured candidates"}
		}
		return GateResult{Eligible: true,
			Reason: "quality gate abstained: insufficient evidence (" + samplesDesc(sig, found, s.MinSamplesOrDefault()) + ")"}
	}
	if s.MinEfficacy > 0 && sig.Efficacy < float64(s.MinEfficacy) {
		return GateResult{Eligible: false, Dimension: "efficacy",
			Reason: fmt.Sprintf("efficacy %.0f is below the floor of %d", sig.Efficacy, s.MinEfficacy)}
	}
	if s.MaxAssuranceSeverity != "" && sig.WorstAssurance != "" &&
		!sig.WorstAssurance.AtMost(s.MaxAssuranceSeverity) {
		return GateResult{Eligible: false, Dimension: "assurance",
			Reason: fmt.Sprintf("worst observed finding %q exceeds the limit of %q",
				sig.WorstAssurance, s.MaxAssuranceSeverity)}
	}
	return GateResult{Eligible: true}
}

func samplesDesc(sig QualitySignal, found bool, floor int) string {
	if !found {
		return "no measurements for this model and workflow"
	}
	if sig.Stale {
		return "measurements are stale"
	}
	return fmt.Sprintf("%d samples against a floor of %d", sig.Samples, floor)
}

func severityNames() string {
	out := make([]string, len(Severities))
	for i, s := range Severities {
		out[i] = string(s)
	}
	return strings.Join(out, ", ")
}
