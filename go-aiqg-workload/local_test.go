package workload

import (
	"encoding/json"
	"strings"
	"testing"
)

// The enforcement that makes tenant-local attribution safe to add at all: it
// cannot serialize into anything pooled, no matter who marshals a Features.
func TestLocalAttributionNeverSerializes(t *testing.T) {
	f := Extract(Observation{Local: map[string]string{"source_app": "acme-billing", "user_id": "u-123"}})
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"acme-billing", "u-123", "source_app"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("serialized Features contains %q — tenant attribution must never be poolable", leak)
		}
	}
}

// Only attribution that generalises to tomorrow's traffic survives. A rule
// learned on a conversation id reproduces the selection and describes nothing.
func TestExtractDropsNonGeneralisingAttribution(t *testing.T) {
	f := Extract(Observation{Local: map[string]string{
		"source_app":        "nightly-extract",
		"conversation_id":   "conv-9f2",
		"response_event_id": "ev-1",
		"client_ip":         "10.0.0.4",
		"agent_id":          "   ",
		"Model":             "claude-haiku-4-5",
	}})
	if f.Local["source_app"] != "nightly-extract" {
		t.Error("allowlisted source_app was dropped")
	}
	if f.Local["model"] != "claude-haiku-4-5" {
		t.Error("allowlist should be case-insensitive on the key")
	}
	for _, k := range []string{"conversation_id", "response_event_id", "client_ip", "agent_id"} {
		if _, ok := f.Local[k]; ok {
			t.Errorf("%s survived extraction — it does not generalise, or was empty", k)
		}
	}
}

// The case #75 exists for: structure cannot separate the selection, attribution
// can. Before this, the learner refused; now it learns — and says the rule is
// tenant-local.
func TestLearnerUsesAttributionWhenStructureCannotSeparate(t *testing.T) {
	var exs []Example
	for i := 0; i < 40; i++ {
		shape := Observation{Vantage: VantageSettled, InputTokens: 1200, OutputTokens: 150}
		sel := shape
		sel.Local = map[string]string{"source_app": "nightly-extract"}
		other := shape
		other.Local = map[string]string{"source_app": "support-bot"}
		exs = append(exs, Example{Features: Extract(sel), Selected: true})
		exs = append(exs, Example{Features: Extract(other), Selected: false})
	}
	rule, rep := LearnRule(exs, LearnOptions{})
	if !rep.Usable {
		t.Fatalf("attribution should have separated this selection: %+v", rep)
	}
	if rep.Precision != 1 || rep.Recall != 1 {
		t.Errorf("precision/recall = %.2f/%.2f, want 1/1", rep.Precision, rep.Recall)
	}
	if !rep.UsesTenantLocal || !rule.TenantLocal {
		t.Error("a rule resting on source_app must be marked tenant-local")
	}
	if !strings.Contains(rep.Explanation, "source app is nightly-extract") {
		t.Errorf("explanation %q should read as a sentence without the local: prefix", rep.Explanation)
	}
}

// A structural rule stays transferable, so the flag must not be set on it.
func TestStructuralRuleIsNotMarkedTenantLocal(t *testing.T) {
	var exs []Example
	for i := 0; i < 40; i++ {
		exs = append(exs, Example{Features: Extract(Observation{Vantage: VantageSettled, JSONSchemaOut: true}), Selected: true})
		exs = append(exs, Example{Features: Extract(Observation{Vantage: VantageSettled, CalledTools: []ToolCall{{Name: "Bash"}}}), Selected: false})
	}
	rule, rep := LearnRule(exs, LearnOptions{})
	if !rep.Usable {
		t.Fatalf("expected a usable structural rule: %+v", rep)
	}
	if rep.UsesTenantLocal || rule.TenantLocal {
		t.Error("a purely structural rule was marked tenant-local")
	}
}

// A global or seed class resting on one tenant's attribution would be proposed
// to every other tenant and match nothing there — or, worse, match a
// coincidentally identical source_app name.
func TestGlobalClassCannotDependOnTenantAttribution(t *testing.T) {
	local := Rule{AllOf: []Cond{{Feature: LocalPrefix + "source_app", Op: OpEq, Str: "x"}}, Confidence: 0.9}
	for _, origin := range []Origin{OriginGlobal, OriginSeed} {
		s := Space{Version: "v", Classes: []Class{{ID: "c", Label: "C", Origin: origin, Status: StatusProposed, Rules: []Rule{local}}}}
		if errs := s.Validate(); len(errs) == 0 {
			t.Errorf("%s class with a tenant-local rule passed validation", origin)
		}
	}
	ok := Space{Version: "v", Classes: []Class{{ID: "c", Label: "C", Origin: OriginDeclared, Status: StatusProposed, Rules: []Rule{local}}}}
	if errs := ok.Validate(); len(errs) != 0 {
		t.Errorf("a declared class with a tenant-local rule should validate: %v", errs)
	}
}

// The check reads the conditions, not the flag, so a rule deserialised without
// the flag still cannot slip into the global layer.
func TestTenantLocalDetectionDoesNotTrustTheFlag(t *testing.T) {
	r := Rule{AllOf: []Cond{{Feature: LocalPrefix + "agent_id", Op: OpEq, Str: "a"}}, Confidence: 0.9, TenantLocal: false}
	if !r.UsesTenantLocal() {
		t.Error("detection trusted a false flag over a local condition")
	}
}

// A request with no such attribute simply does not match; it must not error.
func TestAbsentLocalAttributeDoesNotMatchOrError(t *testing.T) {
	c := Cond{Feature: LocalPrefix + "source_app", Op: OpEq, Str: "nightly-extract"}
	ok, err := c.eval(Extract(Observation{}))
	if err != nil {
		t.Fatalf("absent attribute errored: %v", err)
	}
	if ok {
		t.Error("absent attribute matched")
	}
}

// Round-trip: a learned tenant-local rule classifies through a class space.
func TestTenantLocalRuleAssignsInASpace(t *testing.T) {
	r := Rule{AllOf: []Cond{{Feature: LocalPrefix + "source_app", Op: OpEq, Str: "nightly-extract"}}, Confidence: 1, TenantLocal: true}
	s := Space{Version: "t1-labels-1", Classes: []Class{{ID: "extraction.contracts", Label: "Contract extraction", Origin: OriginDeclared, Status: StatusProposed, Rules: []Rule{r}}}}
	if errs := s.Validate(); len(errs) != 0 {
		t.Fatalf("invalid space: %v", errs)
	}
	got := s.Assign(Extract(Observation{Vantage: VantageSettled, Local: map[string]string{"source_app": "nightly-extract"}}))
	if got.ClassID != "extraction.contracts" {
		t.Errorf("class = %q, want extraction.contracts", got.ClassID)
	}
}
