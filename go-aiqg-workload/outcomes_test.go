package workload

import "testing"

func find(sigs []Signal, name string) (Signal, bool) {
	for _, s := range sigs {
		if s.Name == name {
			return s, true
		}
	}
	return Signal{}, false
}

// The discipline the whole adapter rests on: a turn that made no edits has no
// edit-applied signal, not a zero one. Otherwise a model that proposes nothing
// scores a perfect application rate.
func TestNoSignalRatherThanZero(t *testing.T) {
	sigs := CodingAdapter{}.Signals([]TurnRecord{
		{Role: "assistant", Calls: []ToolCall{{Name: "Read", Resolved: true}}},
	}, 0)
	if _, ok := find(sigs, SignalEditApplied); ok {
		t.Error("a turn with no edits emitted an edit-applied signal")
	}
	if _, ok := find(sigs, SignalCommandExit); ok {
		t.Error("a turn with no commands emitted a command-exit signal")
	}
}

func TestEditAppliedAndCommandExitRates(t *testing.T) {
	sess := []TurnRecord{{Role: "assistant", Calls: []ToolCall{
		{Name: "Edit", Resolved: true},
		{Name: "Edit", Resolved: true, Failed: true},
		{Name: "Bash", Resolved: true},
		{Name: "Bash", Resolved: true},
	}}}
	sigs := CodingAdapter{}.Signals(sess, 0)
	if s, _ := find(sigs, SignalEditApplied); s.Value != 0.5 {
		t.Errorf("edit_applied = %v, want 0.5", s.Value)
	}
	if s, _ := find(sigs, SignalCommandExit); s.Value != 1 {
		t.Errorf("command_exit = %v, want 1", s.Value)
	}
	for _, s := range sigs {
		if s.Version != CodingAdapterVersion || s.Adapter != "coding" {
			t.Errorf("%s not stamped with adapter/version", s.Name)
		}
	}
}

// An unresolved call is not a failed one. A session interrupted mid-call would
// otherwise penalise whichever model happened to be running.
func TestUnresolvedCallsAreExcludedNotFailed(t *testing.T) {
	sigs := CodingAdapter{}.Signals([]TurnRecord{{Role: "assistant", Calls: []ToolCall{
		{Name: "Edit", Resolved: true},
		{Name: "Edit"}, // no result came back
	}}}, 0)
	s, ok := find(sigs, SignalEditApplied)
	if !ok || s.Value != 1 {
		t.Errorf("edit_applied = %v (observed=%v), want 1 over the one resolved call", s.Value, ok)
	}
}

// A repair loop only exists after a failure. An agent editing one file
// repeatedly while everything succeeds is working, not repairing.
func TestRepairLoopRequiresAFailure(t *testing.T) {
	clean := []TurnRecord{
		{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Target: "a.go"}}},
		{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Target: "a.go"}}},
	}
	if _, ok := find(CodingAdapter{}.Signals(clean, 0), SignalRepairLoop); ok {
		t.Error("repair loop fired with no failure — repeated successful edits are work, not rework")
	}
}

func TestRepairLoopCountsCorrectiveTurns(t *testing.T) {
	sess := []TurnRecord{
		{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Failed: true, Target: "a.go"}}},
		{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Target: "a.go"}}},
		{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Target: "a.go"}}},
	}
	s, ok := find(CodingAdapter{}.Signals(sess, 0), SignalRepairLoop)
	if !ok {
		t.Fatal("repair loop not observed after a failure")
	}
	if s.Value != 1.0/3.0 {
		t.Errorf("repair_loop = %v, want 1/3 for two corrective turns", s.Value)
	}
}

// Beyond the window the agent has moved on; counting a later unrelated edit as
// repair would inflate the signal on exactly the files worked on most.
func TestRepairLoopIsWindowed(t *testing.T) {
	sess := []TurnRecord{{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Failed: true, Target: "a.go"}}}}
	for i := 0; i < repairWindow+3; i++ {
		sess = append(sess, TurnRecord{Role: "assistant"})
	}
	sess = append(sess, TurnRecord{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true, Target: "a.go"}}})
	s, _ := find(CodingAdapter{}.Signals(sess, 0), SignalRepairLoop)
	if s.Value != 1 {
		t.Errorf("repair_loop = %v, want 1 — the later edit is outside the window", s.Value)
	}
}

func TestUserCorrection(t *testing.T) {
	mk := func(next string) (Signal, bool) {
		return find(CodingAdapter{}.Signals([]TurnRecord{
			{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true}}},
			{Role: "user", Text: next},
		}, 0), SignalUserCorrection)
	}
	if s, _ := mk("no, that's wrong"); s.Value != 0 {
		t.Errorf("a rejection scored %v, want 0", s.Value)
	}
	if s, _ := mk("thanks, now add the tests"); s.Value != 1 {
		t.Errorf("a continuation scored %v, want 1", s.Value)
	}
	// Nothing followed: not observed, and emphatically not a pass.
	if _, ok := find(CodingAdapter{}.Signals([]TurnRecord{{Role: "assistant"}}, 0), SignalUserCorrection); ok {
		t.Error("user_correction fired with no following user turn")
	}
}

// The session outcome is a property of the SESSION and must be attributed
// once, or one fact multiplies into hundreds and a long session outvotes a
// short one on nothing.
func TestSessionCompletedFiresOnceOnTheLastAssistantTurn(t *testing.T) {
	sess := []TurnRecord{
		{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true}}},
		{Role: "assistant", Calls: []ToolCall{{Name: "Bash", Resolved: true, Target: "git commit -m x"}}},
		{Role: "user", Text: "done"},
	}
	if _, ok := find(CodingAdapter{}.Signals(sess, 0), SignalSessionCompleted); ok {
		t.Error("session_completed fired on a non-final assistant turn")
	}
	s, ok := find(CodingAdapter{}.Signals(sess, 1), SignalSessionCompleted)
	if !ok || s.Value != 1 {
		t.Errorf("session_completed = %v (observed %v), want 1 — the session reached a commit", s.Value, ok)
	}
}

func TestSessionCompletedZeroWhenAbandoned(t *testing.T) {
	sess := []TurnRecord{{Role: "assistant", Calls: []ToolCall{{Name: "Edit", Resolved: true}}}}
	s, ok := find(CodingAdapter{}.Signals(sess, 0), SignalSessionCompleted)
	if !ok || s.Value != 0 {
		t.Errorf("session_completed = %v, want 0 for a session with no commit", s.Value)
	}
}

// Evidence must never carry the path or command itself.
func TestDigestTargetHidesTheTarget(t *testing.T) {
	d := DigestTarget("/home/someone/secret-project/billing.go")
	if d == "" || len(d) != 18 {
		t.Fatalf("digest %q malformed", d)
	}
	if d == DigestTarget("/home/someone/other.go") {
		t.Error("different targets collided")
	}
	if DigestTarget("  ") != "" {
		t.Error("empty target should digest to empty")
	}
}

// A user turn is not an assistant turn and has no outcomes of its own.
func TestNonAssistantTurnsYieldNothing(t *testing.T) {
	if got := (CodingAdapter{}).Signals([]TurnRecord{{Role: "user", Text: "hi"}}, 0); got != nil {
		t.Errorf("user turn produced %d signals", len(got))
	}
	if got := (CodingAdapter{}).Signals(nil, 5); got != nil {
		t.Error("out-of-range index produced signals")
	}
}
