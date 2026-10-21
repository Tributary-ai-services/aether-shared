package workload

import (
	"regexp"
	"strings"
	"unicode"
)

// TurnStart flags: what a human's prompt does, read at the moment a turn starts.
//
// A request carries the whole messages array, so at the start of a turn the extractor can see the
// human's prompt AND the agent message it replies to. Most of what decides the turn act (approve,
// answer, instruct, ...) is in the pair: "yes" is an approval after "want me to run it?" and an
// instruction after a status list that asked nothing. Text is READ to set the flags and is never
// retained: a TurnStartFlags value holds only booleans, counts and a bucket, like every other
// feature in this package.
//
// This is the inline vantage. It cannot see what the agent does next, so it cannot tell a fault
// from a success; it only classifies what the human just did.

// TurnStartExtractorVersion stamps TurnStartFlags. Bump it when a flag's meaning changes, not when
// one is added (same rule as ExtractorVersion).
const TurnStartExtractorVersion = "ts-1"

// TurnAct is what the human did this turn. A prompt can carry several.
type TurnAct string

const (
	ActInstruct  TurnAct = "instruct"   // told the agent to do something
	ActAskStatus TurnAct = "ask_status" // where are we on X
	ActAskInfo   TurnAct = "ask_info"   // explain, look up, show me
	ActCorrect   TurnAct = "correct"    // fixing something the agent got wrong
	ActChallenge TurnAct = "challenge"  // questioning the agent's reasoning and asking it to verify
	ActApprove   TurnAct = "approve"    // a gate on something the agent offered or queued
	ActAnswer    TurnAct = "answer"     // information the agent lacked, in reply to its question
)

// LenBucket is a coarse prompt length. Exact lengths are not kept.
type LenBucket string

const (
	LenTiny   LenBucket = "tiny"   // 1-3 words: "yes", "do 1"
	LenShort  LenBucket = "short"  // 4-12
	LenMedium LenBucket = "medium" // 13-60
	LenLong   LenBucket = "long"   // 61+
)

// TurnStartInput is the pair read at turn start. PrevAssistant is empty when there is no prior agent
// message (the first turn) or the caller did not keep it.
type TurnStartInput struct {
	Prompt        string
	PrevAssistant string
}

// TurnStartFlags holds booleans, counts and a bucket only.
type TurnStartFlags struct {
	Version string

	PromptLen LenBucket
	HasPrev   bool

	StartsApproval bool // yes / yep / ok / go ahead / sounds good ...
	StartsNegation bool // no / don't / stop / wait / actually ...
	SelectionReply bool // "do 1", "1 and 3": picks items from the agent's list
	HasQuestion    bool // a question mark, or a wh-/aux-verb opening
	Imperative     bool // opens with, or contains after an approval, an action verb
	CorrectionCue  bool // "you didn't", "not what I asked", "that's wrong"
	VerifyCue      bool // "verify", "are you sure", "seems inconsistent", "why did you"
	StatusCue      bool // "status", "where are we", "state of"
	InfoCue        bool // "explain", "show me", "what is", "how does"

	PrevEndsQuestion bool // the agent's last message ends on a question
	PrevExplicitAsk  bool // "want me to", "say the word", "go-ahead", "shall I", "awaiting your go"
	PrevOffersChoice bool // the offer lists alternatives ("... or ...")
	PrevStatusList   bool // a numbered or bulleted list with no ask: a report, not a gate

	TicketIDsPrompt int
	TicketIDsPrev   int
}

var (
	reTicket    = regexp.MustCompile(`(?i)\b[a-z]{2,6}-\d{1,4}\b`)
	reSelection = regexp.MustCompile(`(?i)^\s*(?:(?:do|go with|take|yes,?|ok,?|okay,?)\s+)?(?:#?\d+|[a-c])(?:\s*(?:,|and|&|then)\s*(?:#?\d+|[a-c]))*\s*[.!]?\s*$`)
	reListLine  = regexp.MustCompile(`(?m)^\s*(?:\d+[.)]|[-*•])\s+\S`)
)

var approvalOpeners = []string{
	"yes", "yep", "yeah", "yup", "ok", "okay", "sure", "agreed", "agree", "approved", "approve", "lgtm",
	"go ahead", "go for it", "do it", "sounds good", "looks good", "proceed", "please do", "correct", "right",
	"perfect", "great", "good", "fine", "that works", "that's fine", "thats fine", "i agree", "yes please",
}

var negationOpeners = []string{
	"no", "nope", "don't", "dont", "do not", "stop", "wait", "hold on", "actually", "not ", "never", "that's not",
	"thats not", "that isn't", "that is not",
}

var imperativeVerbs = map[string]bool{
	"add": true, "fix": true, "run": true, "deploy": true, "build": true, "create": true, "update": true, "check": true,
	"look": true, "show": true, "write": true, "make": true, "merge": true, "push": true, "commit": true, "open": true,
	"remove": true, "delete": true, "rename": true, "move": true, "implement": true, "start": true, "stop": true,
	"use": true, "set": true, "change": true, "try": true, "test": true, "review": true, "read": true, "find": true,
	"list": true, "pull": true, "roll": true, "restart": true, "install": true, "configure": true, "document": true,
	"save": true, "record": true, "put": true, "get": true, "give": true, "tell": true, "let's": true, "lets": true,
	"also": true, "now": true, "next": true, "then": true, "continue": true, "do": true, "please": true,
	"investigate": true, "audit": true, "design": true, "plan": true, "draft": true, "generate": true, "publish": true,
	"close": true, "re-run": true, "rerun": true, "apply": true, "approve": true, "verify": true, "confirm": true,
}

var whOpeners = []string{
	"what", "why", "how", "where", "when", "which", "who", "can you", "could you", "would you", "is ", "are ",
	"does ", "do you", "did ", "will ", "should ", "have you", "has ", "do we", "do i", "did we", "are we", "is there",
	"are there", "won't ", "wont ", "isn't ", "doesn't ", "wouldn't ",
}

var correctionCues = []string{
	"you didn't", "you did not", "you forgot", "you missed", "i didn't ask", "i did not ask", "i didn't say",
	"not what i", "that's wrong", "thats wrong", "that is wrong", "incorrect", "that's not right", "is wrong", "are wrong",
	"you misread", "you misunderstood", "i meant", "i said", "it's not about", "its not about", "not about",
	"should have been", "should be ", "instead of", "that isn't what", "that's not what", "wrong ",
	"doesn't exist", "doesnt exist", "does not exist", "no such",
}

var verifyCues = []string{
	"verify", "double check", "double-check", "are you sure", "seems inconsistent", "doesn't seem", "doesnt seem",
	"does that make sense", "why did you", "why would", "how do you know", "did you check", "did you actually",
	"can you confirm", "make sure", "prove", "evidence", "i'm not sure", "im not sure", "i don't think", "i dont think",
	"shouldn't", "shouldnt", "really?", "are we sure", "won't this", "wont this", "wouldn't that", "doesn't that",
	"isn't that", "isnt that", "won't that", "wont that",
}

var statusCues = []string{
	"status", "where are we", "where do we stand", "state of", "what's the state", "what is the state", "progress",
	"update on", "how is ", "how's ", "hows ", "what's left", "whats left", "what is left", "what's next", "whats next",
	"what remains", "what's open", "whats open", "anything else", "what else", "next row", "next step", "next one",
	"what is next", "what is the next",
}

var infoCues = []string{
	"explain", "show me", "tell me", "what is", "what are", "what's", "whats", "how does", "how do", "why ",
	"describe", "walk me", "summarize", "summarise", "compare", "difference", "can you tell", "do you know", "is there", "are there",
}

var explicitAsks = []string{
	"want me to", "would you like", "do you want", "shall i", "should i", "say the word", "go-ahead", "go ahead",
	"ok to ", "okay to ", "let me know if you want", "let me know whether", "i can ", "i'll do", "i will do if",
	"if you'd like", "if you want", "confirm and i", "approve and i", "your call", "which do you", "which one",
	"or should", "or do you", "your preference", "tell me which", "tell me if", "awaiting your go", "awaiting your",
	"your go", "when you're ready", "when you are ready",
}

// ExtractTurnStart reads the prompt and the agent message it replies to and returns flags only.
// It is pure: no I/O, no allocation of retained text.
func ExtractTurnStart(in TurnStartInput) TurnStartFlags {
	p := strings.TrimSpace(in.Prompt)
	lp := strings.ToLower(p)
	prev := strings.TrimSpace(in.PrevAssistant)
	lprev := strings.ToLower(prev)

	f := TurnStartFlags{Version: TurnStartExtractorVersion, HasPrev: prev != ""}
	f.PromptLen = lenBucket(p)

	f.StartsApproval = startsWithAny(lp, approvalOpeners)
	f.StartsNegation = startsWithAny(lp, negationOpeners)
	f.SelectionReply = reSelection.MatchString(lp)
	f.HasQuestion = strings.Contains(p, "?") || startsWithAny(lp, whOpeners)
	f.Imperative = firstWordIsImperative(lp)
	if !f.Imperative && f.StartsApproval {
		f.Imperative = containsImperativeAfterOpener(lp)
	}
	f.CorrectionCue = containsAny(lp, correctionCues)
	f.VerifyCue = containsAny(lp, verifyCues)
	f.StatusCue = containsAny(lp, statusCues)
	f.InfoCue = containsAny(lp, infoCues)

	f.PrevEndsQuestion = endsWithQuestion(prev)
	f.PrevExplicitAsk = containsAny(lprev, explicitAsks)
	f.PrevOffersChoice = (f.PrevExplicitAsk || f.PrevEndsQuestion) && strings.Contains(tail(lprev, 260), " or ")
	f.PrevStatusList = len(reListLine.FindAllStringIndex(prev, 3)) >= 2 && !f.PrevExplicitAsk && !f.PrevEndsQuestion

	f.TicketIDsPrompt = len(reTicket.FindAllStringIndex(p, 8))
	f.TicketIDsPrev = len(reTicket.FindAllStringIndex(prev, 8))
	return f
}

func lenBucket(s string) LenBucket {
	n := len(strings.Fields(s))
	switch {
	case n <= 3:
		return LenTiny
	case n <= 12:
		return LenShort
	case n <= 60:
		return LenMedium
	default:
		return LenLong
	}
}

func startsWithAny(lower string, openers []string) bool {
	for _, o := range openers {
		if strings.HasPrefix(lower, o) {
			// A word boundary matters: "no" must not match "node", "ok" must not match "okta".
			if strings.HasSuffix(o, " ") || len(lower) == len(o) || !isWordRune(rune(lower[len(o)])) {
				return true
			}
		}
	}
	return false
}

func containsAny(lower string, cues []string) bool {
	for _, c := range cues {
		if strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func firstWordIsImperative(lower string) bool {
	w := lower
	if i := strings.IndexFunc(w, func(r rune) bool { return !isWordRune(r) && r != '\'' && r != '-' }); i >= 0 {
		w = w[:i]
	}
	return imperativeVerbs[w]
}

func containsImperativeAfterOpener(lower string) bool {
	// "yep, also note in the plan..." / "yes, deploy it": an action verb within the first few words after the opener.
	fields := strings.FieldsFunc(lower, func(r rune) bool { return !isWordRune(r) && r != '\'' && r != '-' })
	for i, w := range fields {
		if i == 0 || i > 6 {
			if i > 6 {
				break
			}
			continue
		}
		if imperativeVerbs[w] && w != "do" && w != "good" {
			return true
		}
	}
	return false
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func endsWithQuestion(s string) bool {
	s = strings.TrimRight(s, " \t\r\n*_`)\"'")
	if s == "" {
		return false
	}
	if strings.HasSuffix(s, "?") {
		return true
	}
	// The question is often the second-to-last paragraph before a sign-off; look at the last non-empty line only.
	return false
}
