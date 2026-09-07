package workload

import "testing"

// Families are the point of the extractor: they are what survives when tool
// names cannot travel, and what actually predicts behaviour. A coding agent's
// editor is spelled differently in every harness, but they all edit.
func TestFamilyOf(t *testing.T) {
	cases := map[string]ToolFamily{
		"Edit": FamilyEdit, "str_replace_editor": FamilyEdit, "Write": FamilyEdit,
		"Grep": FamilySearch, "Glob": FamilySearch, "vector_search": FamilySearch,
		"Bash": FamilyExec, "run_terminal_cmd": FamilyExec,
		"Read": FamilyRead, "WebFetch": FamilyRead, "NotebookRead": FamilyRead,
		"Task": FamilyDelegate, "TodoWrite": FamilyDelegate,
		"":                FamilyOther,
		"acme_billing_v2": FamilyOther,
	}
	for name, want := range cases {
		if got := FamilyOf(name); got != want {
			t.Errorf("FamilyOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// An unrecognised verb must land in Other rather than a plausible guess. A
// wrong family moves a request into a class it does not belong to, and nothing
// downstream can tell that happened.
func TestFamilyOfDoesNotGuess(t *testing.T) {
	for _, name := range []string{"frobnicate", "xyzzy_2", "customer_thing"} {
		if got := FamilyOf(name); got != FamilyOther {
			t.Errorf("FamilyOf(%q) = %q, want other — an invented family is worse than an unknown one", name, got)
		}
	}
}

// Called tools are evidence; offered tools are only opportunity. When both are
// present the called set must win, or a settled row would describe the menu
// rather than the meal.
func TestCalledToolsBeatOfferedTools(t *testing.T) {
	f := Extract(Observation{
		Vantage:      VantageSettled,
		OfferedTools: []string{"Read", "Grep", "Edit", "Bash", "Task"},
		CalledTools:  []ToolCall{{Name: "Edit"}, {Name: "Edit"}, {Name: "Bash"}},
	})
	if f.ToolCount != 3 {
		t.Errorf("ToolCount = %d, want 3 (called, not offered)", f.ToolCount)
	}
	fam, share := f.DominantFamily()
	if fam != FamilyEdit {
		t.Errorf("dominant family = %q, want edit", fam)
	}
	if share < 0.66 || share > 0.67 {
		t.Errorf("share = %.3f, want ~0.667 — the share is what separates 'mostly editing' from 'edited once'", share)
	}
}

// The inline vantage has no called tools, so it must fall back to what was
// offered rather than reporting a toolless request.
func TestInlineFallsBackToOfferedTools(t *testing.T) {
	f := Extract(Observation{Vantage: VantageInline, OfferedTools: []string{"Read", "Grep"}})
	if !f.HasTools || f.ToolCount != 2 {
		t.Fatalf("HasTools=%v ToolCount=%d, want true/2", f.HasTools, f.ToolCount)
	}
	if f.OutputTokens != 0 || f.InOutRatio != 0 {
		t.Error("inline vantage cannot know output — those must stay zero, not be invented")
	}
}

// The signature must be order-independent and case-insensitive, or the same
// workload fragments across arbitrarily many values and clustering sees noise
// where there is none.
func TestToolSignatureIsStableAcrossOrderAndCase(t *testing.T) {
	a := Extract(Observation{OfferedTools: []string{"Read", "Grep", "Edit"}})
	b := Extract(Observation{OfferedTools: []string{"edit", "READ", "grep"}})
	if a.ToolSignature != b.ToolSignature {
		t.Errorf("signature differs on order/case: %d vs %d", a.ToolSignature, b.ToolSignature)
	}
	c := Extract(Observation{OfferedTools: []string{"Read", "Grep"}})
	if a.ToolSignature == c.ToolSignature {
		t.Error("a different tool set must produce a different signature")
	}
	if (Features{}).ToolSignature != 0 {
		t.Error("no tools must signature to 0")
	}
}

// Duplicates are a set, not a multiset: calling Edit twice is the same tool
// surface as calling it once, and only FamilyMix should register the count.
func TestToolSignatureIgnoresDuplicates(t *testing.T) {
	one := Extract(Observation{CalledTools: []ToolCall{{Name: "Edit"}}})
	two := Extract(Observation{CalledTools: []ToolCall{{Name: "Edit"}, {Name: "Edit"}}})
	if one.ToolSignature != two.ToolSignature {
		t.Error("signature must treat the tool set as a set")
	}
	if two.FamilyMix[FamilyEdit] != 2 {
		t.Errorf("FamilyMix should count calls: got %d, want 2", two.FamilyMix[FamilyEdit])
	}
}

// A zero output count must not divide. Pre-route there is no output at all,
// and an infinity or a NaN here would poison every aggregate downstream.
func TestInOutRatioNeverDividesByZero(t *testing.T) {
	f := Extract(Observation{InputTokens: 1200, OutputTokens: 0})
	if f.InOutRatio != 0 {
		t.Errorf("InOutRatio = %v, want 0 when output is unknown", f.InOutRatio)
	}
	g := Extract(Observation{InputTokens: 1200, OutputTokens: 300})
	if g.InOutRatio != 4 {
		t.Errorf("InOutRatio = %v, want 4", g.InOutRatio)
	}
}

// Retrieval markers are what distinguish a RAG-shaped prompt from a long one,
// and they are counted across every message and the system prompt.
func TestRetrievalMarkersCounted(t *testing.T) {
	f := Extract(Observation{
		SystemPrompt: "Answer from the provided context.",
		Messages: []Message{
			{Role: "user", Text: "Document 1: alpha\nDocument 2: beta\nSource: gamma"},
			{Role: "user", Text: "no markers here"},
		},
	})
	if f.RetrievalMarkers != 3 {
		t.Errorf("RetrievalMarkers = %d, want 3", f.RetrievalMarkers)
	}
}

// Truncation is derived, not asserted, because the caller that knows the
// finish reason is not always the caller that cares about the cap.
func TestTruncationDerivedFromFinishReason(t *testing.T) {
	if !Extract(Observation{FinishReason: "length"}).Truncated {
		t.Error("finish_reason=length must set Truncated")
	}
	if Extract(Observation{FinishReason: "stop"}).Truncated {
		t.Error("finish_reason=stop must not set Truncated")
	}
}

// Failed tool calls are counted here because they are the raw material the
// coding outcome adapter reads later; losing them at extraction time would
// make the adapter re-parse the trace.
func TestToolFailuresCounted(t *testing.T) {
	f := Extract(Observation{CalledTools: []ToolCall{
		{Name: "Edit", Failed: true}, {Name: "Edit"}, {Name: "Bash", Failed: true},
	}})
	if f.ToolFailures != 2 {
		t.Errorf("ToolFailures = %d, want 2", f.ToolFailures)
	}
}

// Size buckets exist so a system prompt's length can inform a class without its
// content doing so — and so a one-token edit cannot move a request between
// classes.
func TestSystemBytesBucket(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want int
	}{{0, 0}, {1, 1}, {1023, 1}, {1024, 2}, {8191, 2}, {8192, 3}, {100000, 3}} {
		if got := bytesBucket(tc.n); got != tc.want {
			t.Errorf("bytesBucket(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// Ties must resolve identically on every host. Map iteration order would make
// the same request classify differently across replicas, which is the kind of
// bug that shows up as unexplainable drift months later.
func TestDominantFamilyIsDeterministicOnTies(t *testing.T) {
	obs := Observation{CalledTools: []ToolCall{{Name: "Read"}, {Name: "Edit"}}}
	first, _ := Extract(obs).DominantFamily()
	for i := 0; i < 50; i++ {
		if got, _ := Extract(obs).DominantFamily(); got != first {
			t.Fatalf("tie resolved differently across runs: %q then %q", first, got)
		}
	}
	if fam, share := (Features{}).DominantFamily(); fam != "" || share != 0 {
		t.Errorf("no tools must yield no dominant family, got %q/%v", fam, share)
	}
}

// The version stamp is what tells a consumer whether a stored row needs
// recomputing, so it must actually be stamped.
func TestExtractStampsVersionAndVantage(t *testing.T) {
	f := Extract(Observation{Vantage: VantageSettled})
	if f.ExtractorVersion != ExtractorVersion {
		t.Errorf("ExtractorVersion = %q, want %q", f.ExtractorVersion, ExtractorVersion)
	}
	if f.Vantage != VantageSettled {
		t.Errorf("Vantage = %q, want settled", f.Vantage)
	}
}

// Tokenization is what makes token-equality matching safe, so the shapes real
// tool names actually take all have to split correctly.
func TestTokenize(t *testing.T) {
	cases := map[string][]string{
		"str_replace_editor": {"str", "replace", "editor"},
		"NotebookRead":       {"notebook", "read"},
		"run_terminal_cmd":   {"run", "terminal", "cmd"},
		"web-fetch":          {"web", "fetch"},
		"mcp.server.query":   {"mcp", "server", "query"},
		"HTTPFetch":          {"http", "fetch"},
		"Bash":               {"bash"},
		"":                   nil,
	}
	for in, want := range cases {
		got := tokenize(in)
		if len(got) != len(want) {
			t.Errorf("tokenize(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("tokenize(%q) = %v, want %v", in, got, want)
				break
			}
		}
	}
}

// A name carrying two verbs must resolve by a fixed precedence, not by which
// token happened to be read first. TodoWrite is a delegation that writes.
func TestFamilyOfResolvesMultiVerbNamesByPrecedence(t *testing.T) {
	cases := map[string]ToolFamily{
		"TodoWrite":     FamilyDelegate, // delegate beats edit
		"run_tests":     FamilyExec,     // exec beats... exec; both tokens agree
		"agent_search":  FamilyDelegate, // delegate beats search
		"read_and_edit": FamilyEdit,     // edit beats read
	}
	for name, want := range cases {
		if got := FamilyOf(name); got != want {
			t.Errorf("FamilyOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// The specific trap that substring matching fell into, kept as a regression:
// "frobnicate" contains "cat" and must still be unknown.
func TestFamilyOfDoesNotMatchSubstringsInsideWords(t *testing.T) {
	for _, name := range []string{"frobnicate", "concatenate", "categorize", "forget_me", "runtime_info"} {
		if got := FamilyOf(name); got != FamilyOther {
			t.Errorf("FamilyOf(%q) = %q — matched a cue inside a word, want other", name, got)
		}
	}
}
