package workload

import (
	"fmt"
	"sort"
	"strings"
)

// The class space: a workload class is a HYPOTHESIS about routing — "models
// rank differently on this subset of traffic than on its parent" — and this
// file is the part that evaluates one, not the part that proves it. Proof is
// the separation test, which lives where the experiments do.
//
// # Rules are data, not code
//
// A Class carries a declarative predicate rather than a Go function, because
// the space is a versioned ARTIFACT: discovery computes it in a batch job, it
// ships to the gateway, and the gateway evaluates it as a lookup on the
// request path. A predicate that were code could not be shipped, diffed,
// stored beside the evidence that promoted it, or rolled back to the version a
// stored measurement was computed under.
//
// # Abstention is a result
//
// Assign returns Unclassified with a reason rather than a best guess. A
// classifier that guesses is worse than one that abstains: a stratification
// error corrupts every number computed downstream of it and leaves no trace of
// having done so, while an abstention is visible and countable.

// ClassUnclassified is the assignment for traffic no rule claims. It is a
// first-class outcome, not a failure — see the package's abstention note.
const ClassUnclassified = "unclassified"

// ClassResidual absorbs traffic that matched nothing but is too small to
// warrant its own class. Distinct from Unclassified: residual means "we looked
// and it belongs nowhere in particular", unclassified means "no rule applied".
const ClassResidual = "residual"

// Origin records where a class came from, because provenance is the difference
// between a hypothesis and a finding.
type Origin string

const (
	OriginGlobal   Origin = "global"            // the shared Tier-A/B prior — untested on THIS tenant
	OriginSeed     Origin = "seed"              // shipped bootstrap, same status as global
	OriginDeclared Origin = "declared"          // the customer named it (retro-label or header)
	OriginCluster  Origin = "clustered"         // unsupervised structure over the feature vector
	OriginResidual Origin = "residual_variance" // proposed by a split within an existing class
)

// Status is the lifecycle. A class earns permanence only by separating.
type Status string

const (
	StatusProposed Status = "proposed"
	StatusTesting  Status = "testing"
	StatusPromoted Status = "promoted"
	StatusMerged   Status = "merged"
	StatusRetired  Status = "retired"
)

// Op is a comparison in the predicate language. Deliberately tiny: every
// operator here has to be implementable identically in whatever evaluates the
// artifact next, and a rich language is a compatibility liability.
type Op string

const (
	OpEq  Op = "eq"
	OpNe  Op = "ne"
	OpGt  Op = "gt"
	OpGte Op = "gte"
	OpLt  Op = "lt"
	OpLte Op = "lte"
	OpIn  Op = "in"
)

// Feature names addressable from a rule. Strings rather than struct field
// references because the artifact is JSON on the wire; the cost is that a
// typo is a runtime miss, which is why Validate exists and why an unknown
// feature is an error rather than a silent false.
const (
	FMessageCount   = "message_count"
	FDepth          = "depth"
	FSystemBytes    = "system_bytes_bucket"
	FInputTokens    = "input_tokens"
	FOutputTokens   = "output_tokens"
	FInOutRatio     = "in_out_ratio"
	FHasTools       = "has_tools"
	FToolCount      = "tool_count"
	FDominantFamily = "dominant_family"
	FFamilyShare    = "family_share"
	// Per-family shares. Rules should prefer these over dominant_family +
	// family_share: the dominant family is resolved by a tie-break precedence,
	// and a rule built on it inherits that tie-break as a hidden premise. An
	// explicit share asks the question the rule actually means.
	FShareRead     = "share_read"
	FShareSearch   = "share_search"
	FShareEdit     = "share_edit"
	FShareExec     = "share_exec"
	FShareDelegate = "share_delegate"
	FShareOther    = "share_other"
	// FReadonlyShare is the fraction of tool calls with no side effect
	// (read + search). Discovery is a two-family concept, so no single-family
	// share can express it: a turn split evenly between reading and grepping
	// is entirely discovery and is dominant in neither.
	FReadonlyShare    = "readonly_share"
	FJSONSchemaOut    = "json_schema_out"
	FStreaming        = "streaming"
	FMultimodal       = "multimodal"
	FRetrievalMarkers = "retrieval_markers"
	FAttachments      = "attachments"
	FFinishReason     = "finish_reason"
	FTruncated        = "truncated"
	FToolFailures     = "tool_failures"
)

// settledOnly lists features that do not exist before the response does.
//
// A rule touching one of these cannot be evaluated at the inline vantage, and
// the honest answer there is "not applicable", never "false". Treating an
// unknowable feature as false would let a rule match for the wrong reason and
// then disagree with the settled vantage — manufacturing exactly the drift the
// drift monitor is meant to detect.
var settledOnly = map[string]bool{
	FOutputTokens: true, FInOutRatio: true, FFinishReason: true,
	FTruncated: true, FToolFailures: true,
}

// Cond is one comparison.
type Cond struct {
	Feature string   `json:"feature"`
	Op      Op       `json:"op"`
	Num     float64  `json:"num,omitempty"`
	Str     string   `json:"str,omitempty"`
	Strs    []string `json:"strs,omitempty"`
}

// Rule is a conjunction. Disjunction is expressed as multiple Rules on a
// class, which keeps every rule readable as a single sentence — a class an
// operator cannot read is a class they cannot approve.
type Rule struct {
	AllOf []Cond `json:"all_of"`
	// Confidence a match confers, 0–1. Rules built from strong structural
	// evidence claim more than rules built from weak; the consumer applies its
	// own floor (an open decision, deliberately not fixed here).
	Confidence float64 `json:"confidence"`
	// Note is the sentence this rule means, for the operator-facing surface.
	Note string `json:"note,omitempty"`
}

// Class is one workload class in the space.
type Class struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Origin      Origin `json:"origin"`
	Status      Status `json:"status"`
	ParentID    string `json:"parent_id,omitempty"`
	Rules       []Rule `json:"rules"`
	// MinViableSamples is the powered sample size below which this class can
	// never reach a verdict. A class smaller than this is a slice of the
	// residual bucket wearing a name.
	MinViableSamples int `json:"min_viable_samples,omitempty"`
	// SeparationEvidence is the test that promoted or merged this class. Empty
	// on anything not locally proven — which is how a global prior stays
	// visibly a hypothesis.
	SeparationEvidence map[string]any `json:"separation_evidence,omitempty"`
}

// LocallyProven reports whether this class has been shown to separate on THIS
// tenant's traffic. A global class that has not may seed, propose and
// prioritise; it may not gate a routing decision.
func (c Class) LocallyProven() bool {
	return c.Status == StatusPromoted && len(c.SeparationEvidence) > 0
}

// Space is a versioned class space.
type Space struct {
	Version string  `json:"version"`
	Classes []Class `json:"classes"`
}

// Assignment is one classification outcome.
type Assignment struct {
	ClassID    string  `json:"class_id"`
	Confidence float64 `json:"confidence"`
	Version    string  `json:"class_space_version"`
	Vantage    Vantage `json:"vantage"`
	// Evidence names the rule that fired, in the words the rule carried.
	Evidence string `json:"evidence,omitempty"`
	// Reason explains an Unclassified outcome: what was missing, not merely
	// that something was.
	Reason string `json:"reason,omitempty"`
	// VantageLimited marks that at least one rule could not be evaluated here
	// because it needs the response. Surfaced so an inline/settled
	// disagreement can be attributed to the vantage rather than to drift.
	VantageLimited bool `json:"vantage_limited,omitempty"`
}

// Assign classifies one feature vector against the space.
//
// Every class is evaluated, not the first match: highest confidence wins, ties
// break on class id. Deterministic on purpose — first-match-wins would make
// the answer depend on the order discovery happened to emit classes in, and
// two replicas holding the same artifact must agree.
func (s Space) Assign(f Features) Assignment {
	out := Assignment{ClassID: ClassUnclassified, Version: s.Version, Vantage: f.Vantage}

	type hit struct {
		id   string
		conf float64
		note string
	}
	var hits []hit
	limited := false

	for _, c := range s.Classes {
		if c.Status == StatusMerged || c.Status == StatusRetired {
			continue // a retired class must not keep claiming traffic
		}
		for _, r := range c.Rules {
			ok, vl := r.match(f)
			if vl {
				limited = true
			}
			if ok {
				hits = append(hits, hit{id: c.ID, conf: r.Confidence, note: r.Note})
			}
		}
	}
	out.VantageLimited = limited

	if len(hits) == 0 {
		out.Reason = "no rule in this class space matched"
		if limited {
			out.Reason = "no rule matched; some rules need the response and this is the inline vantage"
		}
		return out
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].conf != hits[j].conf {
			return hits[i].conf > hits[j].conf
		}
		return hits[i].id < hits[j].id
	})
	out.ClassID = hits[0].id
	out.Confidence = hits[0].conf
	out.Evidence = hits[0].note
	return out
}

// match reports whether every condition holds, and whether any condition could
// not be evaluated at this vantage. An unevaluable condition fails the rule —
// it never passes on a default.
func (r Rule) match(f Features) (matched bool, vantageLimited bool) {
	if len(r.AllOf) == 0 {
		return false, false // an empty rule would claim all traffic
	}
	for _, c := range r.AllOf {
		if f.Vantage == VantageInline && settledOnly[c.Feature] {
			return false, true
		}
		ok, err := c.eval(f)
		if err != nil || !ok {
			return false, vantageLimited
		}
	}
	return true, vantageLimited
}

// eval resolves one condition. Unknown features are an error rather than
// false, so a typo surfaces as a miss that Validate can catch rather than as a
// class that quietly never matches.
func (c Cond) eval(f Features) (bool, error) {
	if str, ok := stringFeature(c.Feature, f); ok {
		switch c.Op {
		case OpEq:
			return str == c.Str, nil
		case OpNe:
			return str != c.Str, nil
		case OpIn:
			for _, s := range c.Strs {
				if s == str {
					return true, nil
				}
			}
			return false, nil
		default:
			return false, fmt.Errorf("op %q is not valid on string feature %q", c.Op, c.Feature)
		}
	}
	num, ok := numFeature(c.Feature, f)
	if !ok {
		return false, fmt.Errorf("unknown feature %q", c.Feature)
	}
	switch c.Op {
	case OpEq:
		return num == c.Num, nil
	case OpNe:
		return num != c.Num, nil
	case OpGt:
		return num > c.Num, nil
	case OpGte:
		return num >= c.Num, nil
	case OpLt:
		return num < c.Num, nil
	case OpLte:
		return num <= c.Num, nil
	default:
		return false, fmt.Errorf("op %q is not valid on numeric feature %q", c.Op, c.Feature)
	}
}

func stringFeature(name string, f Features) (string, bool) {
	switch name {
	case FDominantFamily:
		fam, _ := f.DominantFamily()
		return string(fam), true
	case FFinishReason:
		return f.FinishReason, true
	}
	return "", false
}

func numFeature(name string, f Features) (float64, bool) {
	b := func(v bool) float64 {
		if v {
			return 1
		}
		return 0
	}
	switch name {
	case FMessageCount:
		return float64(f.MessageCount), true
	case FDepth:
		return float64(f.Depth), true
	case FSystemBytes:
		return float64(f.SystemBytesBucket), true
	case FInputTokens:
		return float64(f.InputTokens), true
	case FOutputTokens:
		return float64(f.OutputTokens), true
	case FInOutRatio:
		return f.InOutRatio, true
	case FHasTools:
		return b(f.HasTools), true
	case FToolCount:
		return float64(f.ToolCount), true
	case FFamilyShare:
		_, share := f.DominantFamily()
		return share, true
	case FShareRead:
		return f.FamilyShare(FamilyRead), true
	case FShareSearch:
		return f.FamilyShare(FamilySearch), true
	case FShareEdit:
		return f.FamilyShare(FamilyEdit), true
	case FShareExec:
		return f.FamilyShare(FamilyExec), true
	case FShareDelegate:
		return f.FamilyShare(FamilyDelegate), true
	case FShareOther:
		return f.FamilyShare(FamilyOther), true
	case FReadonlyShare:
		return f.FamilyShare(FamilyRead) + f.FamilyShare(FamilySearch), true
	case FJSONSchemaOut:
		return b(f.JSONSchemaOut), true
	case FStreaming:
		return b(f.Streaming), true
	case FMultimodal:
		return b(f.Multimodal), true
	case FRetrievalMarkers:
		return float64(f.RetrievalMarkers), true
	case FAttachments:
		return float64(f.Attachments), true
	case FTruncated:
		return b(f.Truncated), true
	case FToolFailures:
		return float64(f.ToolFailures), true
	}
	return 0, false
}

// Validate checks a space before it is shipped, because a malformed artifact
// fails as a class that silently never matches — the least debuggable outcome
// available. Returns every problem, not the first: an operator fixing an
// artifact wants the list.
func (s Space) Validate() []error {
	var errs []error
	if strings.TrimSpace(s.Version) == "" {
		errs = append(errs, fmt.Errorf("space has no version — stored assignments could not name what produced them"))
	}
	seen := map[string]bool{}
	probe := Features{Vantage: VantageSettled, FamilyMix: map[ToolFamily]int{}}
	for _, c := range s.Classes {
		if c.ID == "" {
			errs = append(errs, fmt.Errorf("a class has no id"))
			continue
		}
		if seen[c.ID] {
			errs = append(errs, fmt.Errorf("duplicate class id %q", c.ID))
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Label) == "" {
			errs = append(errs, fmt.Errorf("class %q has no label — a class nobody can name cannot be approved as a route rule", c.ID))
		}
		if len(c.Rules) == 0 && c.Status != StatusProposed {
			errs = append(errs, fmt.Errorf("class %q has no rules but is %s", c.ID, c.Status))
		}
		for i, r := range c.Rules {
			if len(r.AllOf) == 0 {
				errs = append(errs, fmt.Errorf("class %q rule %d is empty and would claim all traffic", c.ID, i))
			}
			if r.Confidence <= 0 || r.Confidence > 1 {
				errs = append(errs, fmt.Errorf("class %q rule %d confidence %v is outside (0,1]", c.ID, i, r.Confidence))
			}
			for _, cond := range r.AllOf {
				if _, err := cond.eval(probe); err != nil {
					errs = append(errs, fmt.Errorf("class %q rule %d: %w", c.ID, i, err))
				}
			}
		}
	}
	return errs
}
