package workload

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

// Domain adapters: objective quality derived from what happened AFTER a turn.
//
// # Why these exist
//
// The quality input the routing stack has today cannot discriminate between
// models at all — CLEAR Efficacy scores only the vendor finish_reason, so a
// confidently wrong answer that completes cleanly scores 100. Coding is the
// one workload whose quality signals are already sitting in the trace: a patch
// either applied or it did not, a test either exited zero or it did not.
// These need no ground truth, no eval set and no judge.
//
// # Observed is not the same as zero
//
// Every Signal carries Observed. A turn that made no edits has NO
// edit-applied signal, not a zero one — and the difference is the whole
// discipline: a model that proposes nothing would otherwise score a perfect
// application rate. Nothing here emits a value it did not measure.
//
// # These are proxies, and two of them are weak
//
// They are derived, not asserted. A rate is meaningless without its coverage
// and the volume beneath it, user-correction is a heuristic over free text,
// and none of them is a substitute for the customer telling us what worked.
// The asserted-vs-derived split exists precisely so the two are never
// averaged together.

// Signal names. Namespaced by domain so an open adapter set cannot collide.
const (
	SignalEditApplied      = "code.edit_applied"
	SignalCommandExit      = "code.command_exit"
	SignalRepairLoop       = "code.repair_loop"
	SignalUserCorrection   = "code.user_correction"
	SignalSessionCompleted = "code.session_completed"
)

// Signal is one derived outcome.
type Signal struct {
	Name  string  `json:"signal"`
	Value float64 `json:"value"`
	// Observed is false when the turn gave the adapter nothing to measure.
	// A caller must skip these rather than store them as zero.
	Observed bool `json:"observed"`
	// Evidence is a digest — never a path, a command or response text.
	Evidence string `json:"evidence,omitempty"`
	Adapter  string `json:"adapter"`
	Version  string `json:"adapter_version"`
}

// TurnRecord is one turn of a session as the adapters see it.
type TurnRecord struct {
	Role  string // assistant | user
	Text  string // read, never retained
	Calls []ToolCall
}

// Adapter derives signals for one turn of a session.
type Adapter interface {
	Name() string
	Version() string
	// Signals returns the outcomes observable for session[i]. It may look
	// forward — a repair loop is only visible from what came next — which is
	// why outcomes are computed in a batch consumer and not on the hot path.
	Signals(session []TurnRecord, i int) []Signal
}

// CodingAdapterVersion is bumped when a signal's MEANING changes. Consumers
// key stored rows on it, so a bump means recompute.
const CodingAdapterVersion = "coding-1"

// CodingAdapter derives the five coding outcome signals.
type CodingAdapter struct{}

func (CodingAdapter) Name() string    { return "coding" }
func (CodingAdapter) Version() string { return CodingAdapterVersion }

// correctionCue matches a user turn that rejects or corrects what just
// happened. Deliberately narrow: this is the weakest of the five signals, it
// never gates on its own, and a loose pattern would make it worse than absent.
var correctionCue = regexp.MustCompile(`(?i)\b(that('s| is) (wrong|not right|incorrect)|no,? (that|it|this)|doesn't work|does not work|still (failing|broken|fails)|undo|revert that|not what i (asked|wanted)|you (broke|missed))\b`)

// commitCue matches an execution that ends work rather than continuing it.
var commitCue = regexp.MustCompile(`(?i)\b(git\s+commit|git\s+push|gh\s+pr\s+create|git\s+merge)\b`)

// repairWindow bounds how far ahead a repair loop is looked for. Beyond a few
// turns the agent has moved on, and counting a later unrelated edit to the
// same file as a repair would inflate the signal on exactly the files that get
// worked on most.
const repairWindow = 4

func (a CodingAdapter) Signals(session []TurnRecord, i int) []Signal {
	if i < 0 || i >= len(session) || session[i].Role != "assistant" {
		return nil
	}
	turn := session[i]
	var out []Signal

	if s, ok := a.rate(turn, FamilyEdit, SignalEditApplied); ok {
		out = append(out, s)
	}
	if s, ok := a.rate(turn, FamilyExec, SignalCommandExit); ok {
		out = append(out, s)
	}
	if s, ok := a.repairLoop(session, i); ok {
		out = append(out, s)
	}
	if s, ok := a.userCorrection(session, i); ok {
		out = append(out, s)
	}
	if s, ok := a.sessionCompleted(session, i); ok {
		out = append(out, s)
	}
	return out
}

// rate is the success fraction over one family's RESOLVED calls.
//
// Unresolved calls are excluded rather than counted as failures: a session that
// was interrupted mid-call did not fail, and scoring it as failure would
// penalise whichever model happened to be running when someone hit Ctrl-C.
func (a CodingAdapter) rate(t TurnRecord, fam ToolFamily, name string) (Signal, bool) {
	var n, ok int
	for _, c := range t.Calls {
		if FamilyOf(c.Name) != fam || !c.Resolved {
			continue
		}
		n++
		if !c.Failed {
			ok++
		}
	}
	if n == 0 {
		return Signal{}, false // not observed — never a zero
	}
	return a.sig(name, float64(ok)/float64(n), fmt.Sprintf("%d/%d resolved %s calls succeeded", ok, n, fam)), true
}

// repairLoop scores how much corrective rework followed a failure here.
//
// Value is 1/(1+n) so it reads the same direction as the others: 1.0 is clean,
// and it degrades as the agent has to come back to the same target. Only fires
// when something actually failed — an agent editing one file repeatedly while
// everything succeeds is working, not repairing.
func (a CodingAdapter) repairLoop(session []TurnRecord, i int) (Signal, bool) {
	failed := map[string]bool{}
	for _, c := range session[i].Calls {
		if c.Resolved && c.Failed && c.Target != "" {
			failed[c.Target] = true
		}
	}
	if len(failed) == 0 {
		return Signal{}, false
	}
	retries := 0
	for j := i + 1; j < len(session) && j <= i+repairWindow; j++ {
		if session[j].Role != "assistant" {
			continue
		}
		for _, c := range session[j].Calls {
			if failed[c.Target] {
				retries++
				break // one retry per turn; a turn is the unit of rework
			}
		}
	}
	return a.sig(SignalRepairLoop, 1/float64(1+retries),
		fmt.Sprintf("%d corrective turn(s) followed a failure on %d target(s)", retries, len(failed))), true
}

// userCorrection reads the NEXT user turn. Weak by construction, included
// because it is the only signal that catches "applied cleanly, actually wrong".
func (a CodingAdapter) userCorrection(session []TurnRecord, i int) (Signal, bool) {
	for j := i + 1; j < len(session); j++ {
		if session[j].Role != "user" {
			continue
		}
		txt := session[j].Text
		if strings.TrimSpace(txt) == "" {
			return Signal{}, false
		}
		if correctionCue.MatchString(txt) {
			return a.sig(SignalUserCorrection, 0, "the next user turn read as a correction"), true
		}
		return a.sig(SignalUserCorrection, 1, "the next user turn did not read as a correction"), true
	}
	return Signal{}, false // nothing followed — not observed, not a pass
}

// sessionCompleted fires once, on the last assistant turn, and asks whether
// the session ended in an action that concludes work.
//
// Session-level on purpose: "did this end in a commit" is a property of the
// session, and attributing it to every turn would multiply one fact into
// hundreds and let a long session outvote a short one on nothing.
func (a CodingAdapter) sessionCompleted(session []TurnRecord, i int) (Signal, bool) {
	for j := i + 1; j < len(session); j++ {
		if session[j].Role == "assistant" {
			return Signal{}, false // not the last assistant turn
		}
	}
	for _, t := range session {
		for _, c := range t.Calls {
			if FamilyOf(c.Name) == FamilyExec && commitCue.MatchString(c.Target) {
				return a.sig(SignalSessionCompleted, 1, "the session reached a commit, push or PR"), true
			}
		}
	}
	return a.sig(SignalSessionCompleted, 0, "the session ended without a commit, push or PR"), true
}

func (a CodingAdapter) sig(name string, v float64, evidence string) Signal {
	return Signal{
		Name: name, Value: v, Observed: true,
		Evidence: evidence, Adapter: a.Name(), Version: a.Version(),
	}
}

// DigestTarget hashes a path or command so evidence can name WHICH target
// without carrying the customer's path or command line with it.
func DigestTarget(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("t_%016x", h.Sum64())
}
