package workload

import "testing"

func ex(sel bool, o Observation) Example {
	o.Vantage = VantageSettled
	return Example{Features: Extract(o), Selected: sel}
}

// The happy path, and the reason the learner exists: an operator names a set,
// and the set becomes a predicate that can classify tomorrow's requests.
func TestLearnsASeparableSelection(t *testing.T) {
	var ex1 []Example
	for i := 0; i < 60; i++ {
		ex1 = append(ex1, ex(true, Observation{JSONSchemaOut: true, InputTokens: 900, OutputTokens: 120}))
	}
	for i := 0; i < 60; i++ {
		ex1 = append(ex1, ex(false, Observation{CalledTools: []ToolCall{{Name: "Bash"}}, InputTokens: 5000, OutputTokens: 300}))
	}
	rule, rep := LearnRule(ex1, LearnOptions{})
	if !rep.Usable {
		t.Fatalf("not usable: %+v", rep)
	}
	if rep.Precision != 1 || rep.Recall != 1 {
		t.Errorf("precision/recall = %.2f/%.2f, want 1/1 on a cleanly separable set", rep.Precision, rep.Recall)
	}
	if len(rule.AllOf) == 0 || rule.Note == "" {
		t.Error("a usable rule must carry conditions and a sentence explaining them")
	}
	if rule.Confidence <= 0 || rule.Confidence > 1 {
		t.Errorf("confidence %v outside (0,1]", rule.Confidence)
	}
}

// The finding that matters most: when a selection is NOT structurally
// distinguishable, the learner must say so rather than return a rule that
// quietly matches the wrong traffic.
func TestUnlearnableSelectionReportsWhy(t *testing.T) {
	// Identical features on both sides — the operator selected on something
	// the vector cannot see.
	var exs []Example
	for i := 0; i < 40; i++ {
		exs = append(exs, ex(true, Observation{CalledTools: []ToolCall{{Name: "Edit"}}, InputTokens: 1000, OutputTokens: 100}))
		exs = append(exs, ex(false, Observation{CalledTools: []ToolCall{{Name: "Edit"}}, InputTokens: 1000, OutputTokens: 100}))
	}
	_, rep := LearnRule(exs, LearnOptions{})
	if rep.Usable {
		t.Fatalf("claimed a usable rule for an indistinguishable selection: %+v", rep)
	}
	if rep.Reason == "" {
		t.Error("no reason given — 'we could not separate this' is the finding, and it has to be said")
	}
}

// A rule fitted to a handful of requests describes those requests.
func TestRefusesToLearnFromTooFewExamples(t *testing.T) {
	var exs []Example
	for i := 0; i < 5; i++ {
		exs = append(exs, ex(true, Observation{JSONSchemaOut: true}))
		exs = append(exs, ex(false, Observation{CalledTools: []ToolCall{{Name: "Bash"}}}))
	}
	rule, rep := LearnRule(exs, LearnOptions{})
	if rep.Usable || len(rule.AllOf) != 0 {
		t.Error("learned a rule from five examples")
	}
	if rep.Support != 5 {
		t.Errorf("support = %d, want 5", rep.Support)
	}
	if rep.Reason == "" {
		t.Error("no reason given for refusing")
	}
}

// Selecting everything gives the learner nothing to contrast against, and a
// rule that matches everything is not a class.
func TestRefusesWhenEverythingIsSelected(t *testing.T) {
	var exs []Example
	for i := 0; i < 40; i++ {
		exs = append(exs, ex(true, Observation{JSONSchemaOut: true}))
	}
	_, rep := LearnRule(exs, LearnOptions{})
	if rep.Usable {
		t.Error("claimed a rule when the selection was everything")
	}
	if rep.Reason == "" {
		t.Error("no reason given")
	}
}

// Two replicas learning from the same examples must produce the same rule, or
// the class space becomes a function of which host ran the job.
func TestLearningIsDeterministic(t *testing.T) {
	var exs []Example
	for i := 0; i < 40; i++ {
		exs = append(exs, ex(true, Observation{CalledTools: []ToolCall{{Name: "Edit"}, {Name: "Edit"}}, InputTokens: 2000, OutputTokens: 400}))
		exs = append(exs, ex(false, Observation{CalledTools: []ToolCall{{Name: "Read"}}, InputTokens: 900, OutputTokens: 80}))
	}
	first, firstRep := LearnRule(exs, LearnOptions{})
	for i := 0; i < 20; i++ {
		got, rep := LearnRule(exs, LearnOptions{})
		if explain(got.AllOf) != explain(first.AllOf) || rep.F1 != firstRep.F1 {
			t.Fatalf("learning varied across runs: %q then %q", explain(first.AllOf), explain(got.AllOf))
		}
	}
}

// Readability is a hard requirement, not a preference: a class nobody can read
// is a class nobody can approve, and an unapprovable class cannot become a
// route rule.
func TestRuleLengthIsCapped(t *testing.T) {
	var exs []Example
	for i := 0; i < 60; i++ {
		exs = append(exs, ex(true, Observation{
			CalledTools: []ToolCall{{Name: "Edit"}}, InputTokens: 1000 + i, OutputTokens: 100 + i,
			JSONSchemaOut: true, Depth: i,
		}))
		exs = append(exs, ex(false, Observation{
			CalledTools: []ToolCall{{Name: "Bash"}}, InputTokens: 9000 - i, OutputTokens: 900 - i, Depth: i,
		}))
	}
	rule, _ := LearnRule(exs, LearnOptions{MaxConditions: 2})
	if len(rule.AllOf) > 2 {
		t.Errorf("rule has %d conditions, cap was 2", len(rule.AllOf))
	}
}

// A learned rule has to be usable by the thing it was learned for — the class
// space evaluates it as a lookup.
func TestLearnedRuleWorksInAClassSpace(t *testing.T) {
	var exs []Example
	for i := 0; i < 50; i++ {
		exs = append(exs, ex(true, Observation{JSONSchemaOut: true, InputTokens: 800, OutputTokens: 60}))
		exs = append(exs, ex(false, Observation{CalledTools: []ToolCall{{Name: "Grep"}}, InputTokens: 4000, OutputTokens: 300}))
	}
	rule, rep := LearnRule(exs, LearnOptions{})
	if !rep.Usable {
		t.Fatalf("expected a usable rule: %+v", rep)
	}
	space := Space{Version: "learned-1", Classes: []Class{{
		ID: "extraction.contracts", Label: "Contract extraction",
		Origin: OriginDeclared, Status: StatusProposed, Rules: []Rule{rule},
	}}}
	if errs := space.Validate(); len(errs) != 0 {
		t.Fatalf("learned rule produced an invalid space: %v", errs)
	}
	got := space.Assign(Extract(Observation{Vantage: VantageSettled, JSONSchemaOut: true, InputTokens: 800, OutputTokens: 60}))
	if got.ClassID != "extraction.contracts" {
		t.Errorf("learned rule did not match its own training shape: %q", got.ClassID)
	}
}

// The explanation is what an operator checks the rule against, so it has to
// read as language rather than as a struct dump.
func TestExplanationsReadAsSentences(t *testing.T) {
	cases := map[string]Cond{
		"json schema out":               {Feature: FJSONSchemaOut, Op: OpEq, Num: 1},
		"no has tools":                  {Feature: FHasTools, Op: OpEq, Num: 0},
		"dominant family is edit":       {Feature: FDominantFamily, Op: OpEq, Str: "edit"},
		"input tokens is at least 1000": {Feature: FInputTokens, Op: OpGte, Num: 1000},
		"depth is at most 2":            {Feature: FDepth, Op: OpLte, Num: 2},
	}
	for want, c := range cases {
		if got := explainCond(c); got != want {
			t.Errorf("explainCond = %q, want %q", got, want)
		}
	}
}
