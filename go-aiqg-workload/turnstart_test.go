package workload

import (
	"reflect"
	"testing"
)

func acts(in TurnStartInput) []TurnAct {
	a, _ := ProvisionalTurnAct(ExtractTurnStart(in))
	return a
}

// Synthetic cases only: real prompts never belong in this repo. Each case encodes one rule of the labeling skill.
func TestProvisionalTurnAct_Rules(t *testing.T) {
	cases := []struct {
		name string
		in   TurnStartInput
		want []TurnAct
	}{
		{"yes after an explicit offer is a gate", TurnStartInput{"yes", "I fixed the test. Want me to merge it?"}, []TurnAct{ActApprove}},
		{"yes after a question is a gate", TurnStartInput{"yep", "Should I redeploy the service?"}, []TurnAct{ActApprove}},
		{"approval plus a directive carries both", TurnStartInput{"yep, also add a note to the plan", "Want me to update the doc?"}, []TurnAct{ActApprove, ActInstruct}},
		{"a status list is a report, so a directive after it is instruct", TurnStartInput{"yes, build and deploy v2", "Status:\n1. Build v2\n2. Roll out\nWaiting on you."}, []TurnAct{ActInstruct}},
		{"a pick from an offered list is approve and answer", TurnStartInput{"do 1", "I can do either:\n1. merge\n2. close\nWhich do you want?"}, []TurnAct{ActApprove, ActAnswer}},
		{"information in reply to a question is an answer", TurnStartInput{"The staging cluster is the one in the east region", "Which cluster do you mean?"}, []TurnAct{ActAnswer}},
		{"a fresh question is ask_info", TurnStartInput{"how does the cache key get built?", ""}, []TurnAct{ActAskInfo}},
		{"a status question is ask_status", TurnStartInput{"where are we on the migration?", "Done with the first batch."}, []TurnAct{ActAskStatus}},
		{"a plain directive is instruct", TurnStartInput{"add a retry to the uploader", "Tests pass."}, []TurnAct{ActInstruct}},
		{"a correction", TurnStartInput{"I didn't ask for an accounting, just the cost", "Here is the full breakdown."}, []TurnAct{ActCorrect}},
		{"asking the agent to verify is a challenge", TurnStartInput{"this seems inconsistent, can you verify the count?", "The count is 12."}, []TurnAct{ActChallenge}},
	}
	for _, c := range cases {
		if got := acts(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestExtractTurnStart_FlagsHoldNoText(t *testing.T) {
	f := ExtractTurnStart(TurnStartInput{Prompt: "yes please merge OPS-21 and PR-3", PrevAssistant: "Want me to merge OPS-21?"})
	if f.Version != TurnStartExtractorVersion || !f.StartsApproval || !f.PrevExplicitAsk || !f.PrevEndsQuestion {
		t.Fatalf("unexpected flags: %+v", f)
	}
	if f.TicketIDsPrompt != 2 || f.TicketIDsPrev != 1 {
		t.Fatalf("ticket counts: %+v", f)
	}
}

func TestStartsWithAnyRespectsWordBoundary(t *testing.T) {
	if f := ExtractTurnStart(TurnStartInput{Prompt: "node 20 is installed"}); f.StartsNegation {
		t.Error(`"node" must not read as the negation "no"`)
	}
	if f := ExtractTurnStart(TurnStartInput{Prompt: "okta is the idp"}); f.StartsApproval {
		t.Error(`"okta" must not read as the approval "ok"`)
	}
}

func TestPromptLenBuckets(t *testing.T) {
	cases := map[string]LenBucket{"yes": LenTiny, "do the next one please ok": LenShort}
	for p, want := range cases {
		if got := ExtractTurnStart(TurnStartInput{Prompt: p}).PromptLen; got != want {
			t.Errorf("%q: got %s want %s", p, got, want)
		}
	}
}
