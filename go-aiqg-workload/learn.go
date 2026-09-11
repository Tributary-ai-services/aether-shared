package workload

import (
	"fmt"
	"sort"
	"strings"
)

// Learning a rule that reproduces a labelled selection.
//
// # What this is for
//
// An operator filters traffic they recognise and names it. That names a SET,
// not a definition — and a set cannot classify tomorrow's requests. This turns
// the set into a predicate the gateway can evaluate, and reports how well the
// predicate actually reproduces what they selected.
//
// # The report is the product, not a diagnostic
//
// If no rule reproduces the selection, THAT IS THE FINDING: the segment is not
// structurally distinguishable, and the operator needs to see why rather than
// receive a rule that quietly matches the wrong traffic. A learner that always
// returns something is worse than one that admits defeat, because the failure
// then surfaces months later as a class that never behaved as its name implied.
//
// # Readable or it does not ship
//
// Rules are capped at a few conditions and built only from features a person
// can be shown. A class nobody can read is a class nobody can approve, and an
// unapprovable class cannot become a route rule — which is the whole point of
// learning it.

// Example is one request, and whether the operator's filter selected it.
type Example struct {
	Features Features
	Selected bool
}

// LearnOptions tunes the search. Zero values are sensible.
type LearnOptions struct {
	// MaxConditions caps conjunction length. Small on purpose: every extra
	// condition buys accuracy and costs readability, and past three or four a
	// rule stops being something an operator can check at a glance.
	MaxConditions int
	// MinPrecision and MinRecall are the bar for calling a rule usable. Below
	// either, the report says so rather than shipping the rule anyway.
	MinPrecision float64
	MinRecall    float64
	// MinSupport is the fewest selected examples worth learning from. A rule
	// fitted to three requests describes those three requests.
	MinSupport int
}

func (o LearnOptions) withDefaults() LearnOptions {
	if o.MaxConditions <= 0 {
		o.MaxConditions = 3
	}
	if o.MinPrecision <= 0 {
		o.MinPrecision = 0.8
	}
	if o.MinRecall <= 0 {
		o.MinRecall = 0.5
	}
	if o.MinSupport <= 0 {
		o.MinSupport = 20
	}
	return o
}

// FitReport is how well a learned rule reproduces the selection.
type FitReport struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`

	TruePositives  int `json:"true_positives"`
	FalsePositives int `json:"false_positives"`
	FalseNegatives int `json:"false_negatives"`
	Support        int `json:"support"` // examples the operator selected

	// Usable is true only when the rule clears both floors. A rule that does
	// not is still returned — an operator learns more from seeing the closest
	// approximation and why it fell short than from an empty response.
	Usable bool `json:"usable"`
	// Reason explains an unusable or absent rule in the words an operator
	// would use.
	Reason string `json:"reason,omitempty"`
	// Explanation renders the rule as a sentence.
	Explanation string `json:"explanation,omitempty"`
	// UsesTenantLocal is true when the rule depends on tenant attribution.
	// Surfaced because it changes what the rule is: valid here, meaningless in
	// any other tenant, and never a candidate for the global layer.
	UsesTenantLocal bool `json:"uses_tenant_local"`
}

// LearnRule fits a conjunction that reproduces the selected examples.
//
// Greedy: it repeatedly adds the single condition that most improves F1, which
// keeps the search cheap and — more importantly — keeps the result explicable,
// since every condition earned its place by a stated improvement.
func LearnRule(examples []Example, opts LearnOptions) (Rule, FitReport) {
	o := opts.withDefaults()

	support := 0
	for _, e := range examples {
		if e.Selected {
			support++
		}
	}
	rep := FitReport{Support: support}

	if support < o.MinSupport {
		rep.Reason = fmt.Sprintf(
			"only %d of the selected requests are available to learn from (need at least %d) — a rule fitted to this few describes those requests rather than the workload",
			support, o.MinSupport)
		return Rule{}, rep
	}
	if support == len(examples) {
		rep.Reason = "the filter selected every request available, so there is nothing to tell the selection apart from"
		return Rule{}, rep
	}

	candidates := candidateConds(examples)
	if len(candidates) == 0 {
		rep.Reason = "the selected requests share no structural feature that distinguishes them"
		return Rule{}, rep
	}

	var chosen []Cond
	best := score(examples, chosen)
	for len(chosen) < o.MaxConditions {
		bestCond, bestScore, found := Cond{}, best, false
		for _, c := range candidates {
			if containsCond(chosen, c) {
				continue
			}
			s := score(examples, append(append([]Cond{}, chosen...), c))
			// Strict improvement, and ties broken by the candidate order —
			// which is sorted — so the same input always yields the same rule.
			if s.F1 > bestScore.F1+1e-9 {
				bestCond, bestScore, found = c, s, true
			}
		}
		if !found {
			break
		}
		chosen = append(chosen, bestCond)
		best = bestScore
	}

	if len(chosen) == 0 {
		rep.Reason = "no single structural condition separates the selected requests from the rest"
		return Rule{}, rep
	}

	rep.Precision, rep.Recall, rep.F1 = best.Precision, best.Recall, best.F1
	rep.TruePositives, rep.FalsePositives, rep.FalseNegatives = best.TP, best.FP, best.FN
	rep.Explanation = explain(chosen)
	rep.Usable = rep.Precision >= o.MinPrecision && rep.Recall >= o.MinRecall
	if !rep.Usable {
		rep.Reason = fmt.Sprintf(
			"the closest rule matches %.0f%% of what you selected and %.0f%% of what it matches is right — below the bar, so this selection is not cleanly separable by structure alone",
			rep.Recall*100, rep.Precision*100)
	}

	rule := Rule{AllOf: chosen, Confidence: clamp01(rep.Precision), Note: rep.Explanation}
	rule.TenantLocal = rule.UsesTenantLocal()
	rep.UsesTenantLocal = rule.TenantLocal

	// Confidence mirrors precision: a rule right four times in five should not
	// present as certain.
	return rule, rep
}

type scored struct {
	Precision, Recall, F1 float64
	TP, FP, FN            int
}

// score evaluates a conjunction against the labelled examples. An empty
// conjunction matches everything, which is the correct starting point for a
// greedy search: it has perfect recall and whatever precision the base rate
// gives.
func score(examples []Example, conds []Cond) scored {
	var s scored
	for _, e := range examples {
		matched := true
		for _, c := range conds {
			ok, err := c.eval(e.Features)
			if err != nil || !ok {
				matched = false
				break
			}
		}
		switch {
		case matched && e.Selected:
			s.TP++
		case matched && !e.Selected:
			s.FP++
		case !matched && e.Selected:
			s.FN++
		}
	}
	if s.TP+s.FP > 0 {
		s.Precision = float64(s.TP) / float64(s.TP+s.FP)
	}
	if s.TP+s.FN > 0 {
		s.Recall = float64(s.TP) / float64(s.TP+s.FN)
	}
	if s.Precision+s.Recall > 0 {
		s.F1 = 2 * s.Precision * s.Recall / (s.Precision + s.Recall)
	}
	return s
}

// candidateConds proposes the conditions worth trying.
//
// Derived from the SELECTED examples only: a threshold that no selected
// request satisfies cannot help, and generating the cross-product of every
// feature and every observed value would search mostly-useless space.
func candidateConds(examples []Example) []Cond {
	seen := map[string]Cond{}
	add := func(c Cond) { seen[condKey(c)] = c }

	boolFeatures := []string{FHasTools, FJSONSchemaOut, FStreaming, FMultimodal, FTruncated}
	numFeatures := []string{
		FMessageCount, FDepth, FSystemBytes, FInputTokens, FOutputTokens, FInOutRatio,
		FToolCount, FFamilyShare, FRetrievalMarkers, FAttachments, FToolFailures,
		FShareRead, FShareSearch, FShareEdit, FShareExec, FShareDelegate, FShareOther,
		FReadonlyShare,
	}
	strFeatures := []string{FDominantFamily, FFinishReason}

	// Numeric thresholds come from the selected set's own distribution, so
	// every candidate is one a selected request actually satisfies.
	values := map[string][]float64{}
	for _, e := range examples {
		if !e.Selected {
			continue
		}
		for _, f := range boolFeatures {
			if v, ok := numFeature(f, e.Features); ok {
				add(Cond{Feature: f, Op: OpEq, Num: v})
			}
		}
		for _, f := range numFeatures {
			if v, ok := numFeature(f, e.Features); ok {
				values[f] = append(values[f], v)
			}
		}
		for _, f := range strFeatures {
			if v, ok := stringFeature(f, e.Features); ok && v != "" {
				add(Cond{Feature: f, Op: OpEq, Str: v})
			}
		}
		// Tenant-local attribution. Already allowlisted by Extract, so every
		// candidate here is one that generalises to tomorrow's traffic.
		for k, v := range e.Features.Local {
			add(Cond{Feature: LocalPrefix + k, Op: OpEq, Str: v})
		}
	}
	for f, vs := range values {
		for _, q := range quantiles(vs) {
			add(Cond{Feature: f, Op: OpGte, Num: q})
			add(Cond{Feature: f, Op: OpLte, Num: q})
		}
	}

	out := make([]Cond, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	// Sorted so the greedy search is deterministic: two replicas learning from
	// the same examples must produce the same rule, or the class space becomes
	// a function of which host happened to run the job.
	sort.Slice(out, func(i, j int) bool { return condKey(out[i]) < condKey(out[j]) })
	return out
}

// quantiles returns a few representative cut points. Deliberately coarse: a
// threshold at the 37th percentile is not more true than one at the median,
// and a rule that reads "input tokens ≥ 18,431" invites false confidence in a
// number that came from one sample.
func quantiles(vs []float64) []float64 {
	if len(vs) == 0 {
		return nil
	}
	sorted := append([]float64(nil), vs...)
	sort.Float64s(sorted)
	pick := []float64{0.1, 0.25, 0.5, 0.75, 0.9}
	seen := map[float64]bool{}
	out := []float64{}
	for _, p := range pick {
		v := round2(sorted[int(float64(len(sorted)-1)*p)])
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func condKey(c Cond) string {
	return fmt.Sprintf("%s|%s|%g|%s", c.Feature, c.Op, c.Num, c.Str)
}

func containsCond(cs []Cond, c Cond) bool {
	k := condKey(c)
	for _, x := range cs {
		if condKey(x) == k {
			return true
		}
	}
	return false
}

// explain renders a conjunction as a sentence an operator can check.
func explain(conds []Cond) string {
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		parts = append(parts, explainCond(c))
	}
	return strings.Join(parts, " and ")
}

func explainCond(c Cond) string {
	name := strings.ReplaceAll(strings.TrimPrefix(c.Feature, LocalPrefix), "_", " ")
	if c.Str != "" {
		return fmt.Sprintf("%s is %s", name, c.Str)
	}
	switch c.Feature {
	case FHasTools, FJSONSchemaOut, FStreaming, FMultimodal, FTruncated:
		if c.Num == 1 {
			return name
		}
		return "no " + name
	}
	switch c.Op {
	case OpGte:
		return fmt.Sprintf("%s is at least %g", name, c.Num)
	case OpLte:
		return fmt.Sprintf("%s is at most %g", name, c.Num)
	case OpGt:
		return fmt.Sprintf("%s is above %g", name, c.Num)
	case OpLt:
		return fmt.Sprintf("%s is below %g", name, c.Num)
	default:
		return fmt.Sprintf("%s is %g", name, c.Num)
	}
}

func round2(v float64) float64 {
	if v > -1000 && v < 1000 {
		return float64(int(v*100)) / 100
	}
	return float64(int(v))
}

func clamp01(v float64) float64 {
	if v < 0.01 {
		return 0.01
	}
	if v > 1 {
		return 1
	}
	return v
}
