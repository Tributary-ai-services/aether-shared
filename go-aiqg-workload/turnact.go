package workload

// Confidence of a provisional turn act. The decision table is a first pass over flags, not a
// classifier: Low means "a default, nothing in the flags argued for it".
type ActConfidence string

const (
	ConfLow  ActConfidence = "low"
	ConfMed  ActConfidence = "med"
	ConfHigh ActConfidence = "high"
)

// ProvisionalTurnAct maps TurnStartFlags to turn acts by a small ordered decision table. The order is
// the decision: earlier rules win. It reads flags only, never text, so it can run wherever the flags do.
//
// The rules restate the labeling skill (tas-session-corpus skills/sdlc-turn-labeling, section 5):
//   - an approval opener after an explicit ask or a question is approve; with nothing asked it is still
//     approve if the prompt is only the opener, because a closing "yes" is a gate;
//   - a status list with no ask is a report, so a directive after it is instruct, not approve;
//   - a reply to the agent's question that is not itself a question is answer;
//   - corrections and challenges are rarely phrased as such, so those two are low-recall by design.
func ProvisionalTurnAct(f TurnStartFlags) ([]TurnAct, ActConfidence) {
	asked := f.PrevExplicitAsk || f.PrevEndsQuestion

	// "do 1", "1 and 3": a pick from the agent's list. A gate when something was offered, else a directive.
	if f.SelectionReply {
		if asked {
			return []TurnAct{ActApprove, ActAnswer}, ConfMed
		}
		return []TurnAct{ActInstruct}, ConfLow
	}

	// Corrections: the human saying the agent was wrong. Checked before approval so "yes but you forgot X" is a correction.
	if f.CorrectionCue && !f.StartsApproval {
		return []TurnAct{ActCorrect}, ConfMed
	}

	// A question or doubt that asks the agent to check its own work.
	if f.VerifyCue && (f.HasQuestion || f.StartsNegation || f.PromptLen == LenMedium) {
		return []TurnAct{ActChallenge}, ConfLow
	}

	if f.StartsApproval {
		switch {
		case asked:
			if f.Imperative && f.PromptLen != LenTiny {
				return []TurnAct{ActApprove, ActInstruct}, ConfMed
			}
			return []TurnAct{ActApprove}, ConfHigh
		case f.PrevStatusList || f.Imperative:
			// A status list is a report, not a gate, and a directive with nothing offered is an instruction.
			return []TurnAct{ActInstruct}, ConfMed
		default:
			return []TurnAct{ActApprove}, ConfLow // a bare "yes" with nothing asked: a closing gate, weak evidence
		}
	}

	if f.StatusCue && (f.HasQuestion || f.PromptLen == LenTiny || f.PromptLen == LenShort) {
		return []TurnAct{ActAskStatus}, ConfMed
	}

	if f.HasQuestion {
		return []TurnAct{ActAskInfo}, ConfMed
	}

	// Not a question, not an approval opener.
	if f.Imperative || f.PromptLen == LenTiny {
		switch {
		case f.PrevExplicitAsk && f.PrevOffersChoice:
			return []TurnAct{ActApprove, ActAnswer}, ConfLow // picks one of the agent's alternatives
		case f.PrevExplicitAsk:
			return []TurnAct{ActApprove}, ConfMed // a directive that takes up the agent's offer is the gate
		case f.PrevEndsQuestion:
			return []TurnAct{ActAnswer}, ConfLow
		}
		return []TurnAct{ActInstruct}, ConfMed
	}

	// A statement. With an agent message above it, it is most often the human reporting back or supplying a fact.
	if f.HasPrev {
		return []TurnAct{ActAnswer}, ConfLow
	}
	return []TurnAct{ActInstruct}, ConfLow
}
