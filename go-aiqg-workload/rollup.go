package workload

import "sort"

// Rolling per-turn classifications up to a segment or session archetype.
//
// # Why this is not the same rule as turn classification
//
// The turn classifier requires a clear plurality of one tool family, which is
// right for a single turn and wrong for a prompt segment. Measured against
// SWE-chat's labelled intents, the expected family was PRESENT in 62–93% of
// segments but dominant at 60% in only 7–74% — a segment is a mixture of
// reading, searching, running and editing, so a dominance threshold tuned for
// a turn dilutes to nothing when aggregated over a whole prompt.
//
// Lowering the turn threshold would be the tempting fix and the wrong one: it
// would make every mixed turn claim a class on thin evidence. The unit needs
// its own rule.

// RollUpStrategy selects how per-turn classes become one archetype.
type RollUpStrategy string

const (
	// RollUpPlurality picks the most frequent classified turn class. Honest
	// and weak: a segment that reads ten files to make one edit is scored as
	// discovery, though the edit is what the prompt was about.
	RollUpPlurality RollUpStrategy = "plurality"

	// RollUpPrecedence picks the highest-precedence class PRESENT, on the
	// principle that the most consequential activity names the segment: an
	// edit among reads is a modification, because the reads were in service of
	// the edit. This matches the taxonomy's own claim that Modification is the
	// dominant coding-agent action, and it is what the measurement selected —
	// 54.2% against plurality's 45.9% and the flat single-observation
	// approach's 33.9%.
	//
	// It is not uniformly better, and the trade is worth knowing: precedence
	// wins hugely on edit intents (refactor 78.6% vs 25.4%, create 65.4% vs
	// 18.2%) and loses on non-edit ones (git 67.2% vs 83.4%), because an agent
	// that edits incidentally while running commands has its segment renamed a
	// modification. It is chosen anyway because modification is the class a
	// routing decision most needs to get right.
	//
	// A gated variant — precedence only above a minimum share — was built and
	// measured at four thresholds. It lost to pure precedence at every one
	// (53.9% at 0.15 down to 46.8% at 0.5) and was removed rather than left as
	// a knob someone could turn on.
	RollUpPrecedence RollUpStrategy = "precedence"
)

// archetypePrecedence orders classes by how much they determine what a segment
// was FOR. Side effects outrank reads: an edit is the point, a read is
// preparation.
var archetypePrecedence = []string{
	"code.modification",
	"code.orchestration",
	"code.execution",
	"code.discovery",
	"extraction.structured",
	"rag.qa",
	"summarization",
	"conversation",
	"single_turn_qa",
}

// RollUp derives one archetype from the turn assignments of a segment.
//
// Unclassified turns are excluded from the decision but counted: a segment
// where nine turns of ten abstained has an archetype resting on one turn, and
// the caller needs to know that before believing it. Returns Unclassified with
// a reason when nothing was classifiable.
func RollUp(turns []Assignment, strategy RollUpStrategy) Assignment {
	counts := map[string]int{}
	classified := 0
	version, vantage := "", Vantage("")
	for _, a := range turns {
		if version == "" {
			version, vantage = a.Version, a.Vantage
		}
		if a.ClassID == ClassUnclassified || a.ClassID == "" {
			continue
		}
		counts[a.ClassID]++
		classified++
	}
	out := Assignment{ClassID: ClassUnclassified, Version: version, Vantage: vantage}
	if classified == 0 {
		out.Reason = "no turn in this segment was classifiable"
		return out
	}
	// Coverage is the confidence: an archetype resting on one classified turn
	// out of ten is exactly as thin as that sounds, and saying so is cheaper
	// than discovering it downstream.
	out.Confidence = float64(classified) / float64(len(turns))

	switch strategy {
	case RollUpPlurality:
		ids := make([]string, 0, len(counts))
		for id := range counts {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if counts[ids[i]] != counts[ids[j]] {
				return counts[ids[i]] > counts[ids[j]]
			}
			return ids[i] < ids[j] // deterministic ties
		})
		out.ClassID = ids[0]
		out.Evidence = "most classified turns in this segment"
	default:
		for _, id := range archetypePrecedence {
			if counts[id] > 0 {
				out.ClassID = id
				out.Evidence = "the most consequential activity present in this segment"
				break
			}
		}
		if out.ClassID == ClassUnclassified {
			// Classified into something outside the precedence list — fall
			// back to plurality rather than discarding a real classification.
			return RollUp(turns, RollUpPlurality)
		}
	}
	return out
}
