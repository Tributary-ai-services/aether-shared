package workload

import "testing"

func settled(o Observation) Features { o.Vantage = VantageSettled; return Extract(o) }

// The shipped artifact has to be well-formed, since a malformed one fails as a
// class that silently never matches — the least debuggable outcome available.
func TestSeedSpaceValidates(t *testing.T) {
	if errs := SeedSpace().Validate(); len(errs) != 0 {
		for _, e := range errs {
			t.Errorf("seed space invalid: %v", e)
		}
	}
}

// Every seed class is a hypothesis and must say so: origin seed, no separation
// evidence, and therefore not locally proven. A seed class that read as proven
// could gate a routing decision it has never earned.
func TestSeedClassesAreNotLocallyProven(t *testing.T) {
	for _, c := range SeedSpace().Classes {
		if c.Origin != OriginSeed {
			t.Errorf("class %q origin = %q, want seed", c.ID, c.Origin)
		}
		if c.LocallyProven() {
			t.Errorf("class %q reports itself locally proven — it has never been tested on any tenant's traffic", c.ID)
		}
	}
}

func TestSeedAssignsCodingClassesFromFamilyMix(t *testing.T) {
	s := SeedSpace()
	cases := []struct {
		name  string
		calls []ToolCall
		want  string
	}{
		{"mostly edits", []ToolCall{{Name: "Edit"}, {Name: "Edit"}, {Name: "Read"}}, "code.modification"},
		{"reading around", []ToolCall{{Name: "Read"}, {Name: "Grep"}, {Name: "Read"}, {Name: "Glob"}}, "code.discovery"},
		{"running things", []ToolCall{{Name: "Bash"}, {Name: "Bash"}, {Name: "Read"}}, "code.execution"},
		{"delegating", []ToolCall{{Name: "Task"}, {Name: "TodoWrite"}}, "code.orchestration"},
	}
	for _, tc := range cases {
		got := s.Assign(settled(Observation{CalledTools: tc.calls}))
		if got.ClassID != tc.want {
			t.Errorf("%s: class = %q (conf %.2f), want %q", tc.name, got.ClassID, got.Confidence, tc.want)
		}
		if got.Evidence == "" {
			t.Errorf("%s: assignment carries no evidence — an operator cannot check a claim with no reason attached", tc.name)
		}
	}
}

func TestSeedAssignsNonCodingArchetypes(t *testing.T) {
	s := SeedSpace()
	cases := []struct {
		name string
		obs  Observation
		want string
	}{
		{"schema-constrained", Observation{JSONSchemaOut: true}, "extraction.structured"},
		{"rag", Observation{Messages: []Message{{Text: "Document 1: a\nDocument 2: b\nSource: c"}}}, "rag.qa"},
		{"summarize", Observation{InputTokens: 20000, OutputTokens: 200}, "summarization"},
		{"chat", Observation{Depth: 3, InputTokens: 400, Messages: []Message{{Text: "and then?"}}}, "conversation"},
		{"one-shot", Observation{Depth: 0, InputTokens: 40, Messages: []Message{{Text: "what is 2+2"}}}, "single_turn_qa"},
	}
	for _, tc := range cases {
		if got := s.Assign(settled(tc.obs)); got.ClassID != tc.want {
			t.Errorf("%s: class = %q, want %q", tc.name, got.ClassID, tc.want)
		}
	}
}

// Traffic no rule claims must abstain WITH A REASON, not land in a nearest
// neighbour. The reason is what tells an operator whether to label it.
func TestUnmatchedTrafficAbstainsWithAReason(t *testing.T) {
	s := SeedSpace()
	// Tools present, but no family dominant enough for any coding rule, and
	// every non-coding rule requires no tools.
	got := s.Assign(settled(Observation{CalledTools: []ToolCall{
		{Name: "acme_billing"}, {Name: "acme_ledger"}, {Name: "acme_export"},
	}}))
	if got.ClassID != ClassUnclassified {
		t.Fatalf("class = %q, want unclassified", got.ClassID)
	}
	if got.Reason == "" {
		t.Error("abstention carries no reason — 'nothing matched' and 'we could not look' are different facts")
	}
	if got.Confidence != 0 {
		t.Errorf("confidence = %v on an abstention, want 0", got.Confidence)
	}
}

// A rule needing the response must NOT be evaluated inline, and the assignment
// must say the vantage limited it — otherwise an inline/settled disagreement
// looks like drift when it is only a difference in what was knowable.
func TestInlineVantageCannotEvaluateSettledOnlyRules(t *testing.T) {
	s := SeedSpace()
	obs := Observation{Vantage: VantageInline, InputTokens: 20000} // would be summarization if output were known
	got := s.Assign(Extract(obs))
	if got.ClassID == "summarization" {
		t.Fatal("inline matched a rule that needs the response — an unknowable feature must never default to a value")
	}
	if !got.VantageLimited {
		t.Error("assignment does not report that the vantage limited evaluation")
	}
}

// Ties break deterministically. Two replicas holding the same artifact must
// agree, or the disagreement surfaces later as unexplainable drift.
func TestAssignIsDeterministic(t *testing.T) {
	s := SeedSpace()
	f := settled(Observation{CalledTools: []ToolCall{{Name: "Edit"}, {Name: "Read"}}})
	first := s.Assign(f)
	for i := 0; i < 50; i++ {
		if got := s.Assign(f); got.ClassID != first.ClassID || got.Confidence != first.Confidence {
			t.Fatalf("assignment varied across runs: %q/%v then %q/%v", first.ClassID, first.Confidence, got.ClassID, got.Confidence)
		}
	}
}

// Merged and retired classes must stop claiming traffic, or a taxonomy could
// only ever grow and merge pressure would be one-directional.
func TestRetiredAndMergedClassesDoNotClaimTraffic(t *testing.T) {
	rule := Rule{AllOf: []Cond{{Feature: FHasTools, Op: OpEq, Num: 0}}, Confidence: 0.9, Note: "n/a"}
	s := Space{Version: "t", Classes: []Class{
		{ID: "gone", Label: "Gone", Status: StatusRetired, Rules: []Rule{rule}},
		{ID: "folded", Label: "Folded", Status: StatusMerged, Rules: []Rule{rule}},
	}}
	if got := s.Assign(settled(Observation{})); got.ClassID != ClassUnclassified {
		t.Errorf("class = %q, want unclassified — a retired class kept claiming traffic", got.ClassID)
	}
}

// Validate has to catch the artifact mistakes that would otherwise present as
// a class that never matches.
func TestValidateCatchesMalformedSpaces(t *testing.T) {
	bad := Space{Classes: []Class{
		{ID: "a", Label: "", Status: StatusPromoted, Rules: []Rule{{AllOf: []Cond{{Feature: "nope", Op: OpEq}}, Confidence: 0.5}}},
		{ID: "a", Label: "Dup", Status: StatusPromoted, Rules: []Rule{{Confidence: 2}}},
	}}
	errs := bad.Validate()
	if len(errs) < 4 {
		t.Fatalf("expected version, label, unknown-feature, duplicate-id and empty-rule errors; got %d: %v", len(errs), errs)
	}
}

// The gap is part of the product, not a footnote: a surface showing four
// coding classes has to be able to say why it is not seven.
func TestUnseparableIntentsAreNamed(t *testing.T) {
	gaps := UnseparableIntents()
	for _, want := range []string{"code.generation", "code.diagnosis", "code.validation"} {
		if reason, ok := gaps[want]; !ok || reason == "" {
			t.Errorf("%s is not named as unseparable, or carries no reason", want)
		}
	}
	for id := range gaps {
		for _, c := range SeedSpace().Classes {
			if c.ID == id {
				t.Errorf("%s is listed as unseparable but the seed space ships it anyway", id)
			}
		}
	}
}

// The execution class is known to conflate three things; a surface showing it
// must be able to say so, because "31% of your spend is execution" invites
// exactly the question this answers.
func TestExecutionConflationIsNamed(t *testing.T) {
	got := ExecutionConflates()
	if len(got) < 3 {
		t.Fatalf("expected the three conflated behaviours, got %v", got)
	}
	var shell bool
	for _, s := range got {
		if len(s) > 0 && (s[0] == 'd') {
			shell = true
		}
	}
	if !shell {
		t.Error("discovery-through-a-shell is the non-obvious one and must be named")
	}
}

// Discovery is a two-family concept: a turn split evenly between reading and
// grepping is entirely side-effect-free and dominant in neither family. This
// is the case that forced readonly_share to exist.
func TestReadonlyShareCoversSplitDiscovery(t *testing.T) {
	f := settled(Observation{CalledTools: []ToolCall{
		{Name: "Read"}, {Name: "Grep"}, {Name: "Read"}, {Name: "Glob"},
	}})
	if got := f.FamilyShare(FamilyRead); got != 0.5 {
		t.Errorf("share_read = %v, want 0.5", got)
	}
	if got := f.FamilyShare(FamilySearch); got != 0.5 {
		t.Errorf("share_search = %v, want 0.5", got)
	}
	if a := SeedSpace().Assign(f); a.ClassID != "code.discovery" {
		t.Errorf("class = %q, want code.discovery — neither family is dominant, but every call was side-effect-free", a.ClassID)
	}
}

// Every per-family share resolves, and an empty turn divides by nothing.
func TestPerFamilySharesResolve(t *testing.T) {
	f := settled(Observation{CalledTools: []ToolCall{{Name: "Edit"}, {Name: "Bash"}, {Name: "Task"}, {Name: "acme_x"}}})
	for feat, want := range map[string]float64{
		FShareEdit: 0.25, FShareExec: 0.25, FShareDelegate: 0.25, FShareOther: 0.25,
		FShareRead: 0, FShareSearch: 0, FReadonlyShare: 0,
	} {
		got, ok := numFeature(feat, f)
		if !ok {
			t.Fatalf("%s did not resolve", feat)
		}
		if got != want {
			t.Errorf("%s = %v, want %v", feat, got, want)
		}
	}
	if (Features{}).FamilyShare(FamilyEdit) != 0 {
		t.Error("an empty turn must share to 0, not divide by zero")
	}
}
