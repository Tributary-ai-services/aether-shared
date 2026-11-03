package workload

import (
	"regexp"
	"strings"
	"unicode"
)

// Turn boundary: does this request start a human turn, and which turn is it?
//
// A request carries the whole messages array. The turn starts when the last message is text a
// human typed; when the last message returns tool results, or is something the client injected (a
// slash-command echo, a task notification, an interrupt marker), the agent is mid-turn. This is the
// inline vantage and it is deterministic: no model, no network, no state. It is the G3 function of
// the continuous-labeling design (8.6): one definition shared by the gateway, the importer and the
// research corpus, so "a turn" means the same thing in all three.
//
// The rules restate the research corpus's episode splitter (tas-session-corpus
// scripts/build_episodes.py): a user message holding any tool_result is not a turn; system-reminder
// spans are stripped before anything else is read; injected prefixes and interrupt markers are not
// turns. Text is READ and never retained.

// TurnBoundaryVersion stamps TurnBoundary. Bump it when a rule's meaning changes.
const TurnBoundaryVersion = "tb-1"

// MessageKind is what one message is, for the purpose of finding turns.
type MessageKind string

const (
	KindHuman        MessageKind = "human"        // text a person typed (pasted content included)
	KindContinuation MessageKind = "continuation" // a compaction summary that restarts the array
	KindToolResult   MessageKind = "tool_result"  // returns tool output: the agent is mid-turn
	KindInjected     MessageKind = "injected"     // a command echo, task notification, hook output or caveat
	KindInterrupt    MessageKind = "interrupt"    // "[Request interrupted by user]"
	KindEmpty        MessageKind = "empty"        // nothing left after stripping reminders
	KindAgent        MessageKind = "agent"        // assistant, system, developer or tool role
)

// BoundaryMessage is the neutral input: the role, the message's text parts joined, and how many
// tool_result parts it held. Callers translate their wire types into it.
type BoundaryMessage struct {
	Role        string
	Text        string
	ToolResults int
}

// TurnBoundary holds the decision and counts only.
type TurnBoundary struct {
	Version string
	// TurnStart is true when the last message starts a turn (human or continuation).
	TurnStart bool
	// Kind is the last message's kind.
	Kind MessageKind
	// TurnIndex counts turn-starting messages in the array, so it is the 1-based index of the latest
	// turn. It is an index within this array, not within the session: compaction drops the history and
	// the count restarts, which is why Continuation is reported.
	TurnIndex int
	// Continuation is true when the array begins with a compaction summary.
	Continuation bool
}

var (
	reReminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	reInjected = regexp.MustCompile(`^<(task-notification|command-name|command-message|command-args|local-command|bash-|teammate|user-prompt-submit-hook)`)
	reCompact  = regexp.MustCompile(`(?i)^This session is being continued from a previous conversation`)
)

// StripSystemReminders removes <system-reminder> spans and trims the rest. An unterminated span drops
// everything after its opening tag: text the client injected is never read as the human's.
func StripSystemReminders(s string) string {
	if !strings.Contains(s, reminderOpen) {
		return strings.TrimSpace(s)
	}
	s = reReminder.ReplaceAllString(s, "")
	if i := strings.Index(s, reminderOpen); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

const (
	reminderOpen  = "<system-reminder>"
	reminderClose = "</system-reminder>"
)

// leadingText returns the message from its first character outside a reminder span, or "" when
// nothing but reminders and space is left. Only the start matters for the kind, so it skips leading
// spans without copying: this runs on every user message of every request, inline.
func leadingText(s string) string {
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		if !strings.HasPrefix(s, reminderOpen) {
			return s
		}
		end := strings.Index(s, reminderClose)
		if end < 0 {
			return "" // unterminated: the rest is injected text
		}
		s = s[end+len(reminderClose):]
	}
}

// ClassifyMessage decides one message's kind.
func ClassifyMessage(m BoundaryMessage) MessageKind {
	if !strings.EqualFold(m.Role, "user") {
		return KindAgent
	}
	if m.ToolResults > 0 {
		return KindToolResult
	}
	s := leadingText(m.Text)
	switch {
	case s == "":
		return KindEmpty
	case reInjected.MatchString(s) || strings.HasPrefix(s, "Caveat:"):
		return KindInjected
	case strings.HasPrefix(s, "[Request interrupted"):
		return KindInterrupt
	case reCompact.MatchString(s):
		return KindContinuation
	}
	return KindHuman
}

// ExtractTurnBoundary reads the messages array and reports whether its last message starts a turn.
func ExtractTurnBoundary(msgs []BoundaryMessage) TurnBoundary {
	b := TurnBoundary{Version: TurnBoundaryVersion, Kind: KindEmpty}
	for i, m := range msgs {
		k := ClassifyMessage(m)
		if k == KindHuman || k == KindContinuation {
			b.TurnIndex++
		}
		if k == KindContinuation && b.TurnIndex == 1 {
			b.Continuation = true
		}
		if i == len(msgs)-1 {
			b.Kind = k
			b.TurnStart = k == KindHuman || k == KindContinuation
		}
	}
	return b
}
