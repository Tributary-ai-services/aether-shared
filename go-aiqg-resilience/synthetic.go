package resilience

import "strings"

// Identifying synthetic traffic — a prerequisite for measurement-driven
// routing (routing-decision.md §5.7).
//
// # Why this exists at all
//
// A verbosity table computed from our own events is only as good as the events.
// Measured 2026-08-21 over 30 days, EVERY gpt-4o-mini event in production came
// from gateway verification probes: 13 requests sending max_tokens=12 and asking
// for the single word "ok", averaging THREE output tokens. The same model's real
// output on the same workflow measures ~48 tokens.
//
// A naive expected-cost router reading that table would price gpt-4o-mini
// roughly 16x cheaper than reality and route everything to it — a
// measurement-driven router made worse than the guess it replaced. Excluding
// synthetic traffic is therefore not a refinement; it is the difference between
// the feature working and actively harming.
//
// # Two mechanisms, deliberately
//
// MARK AT SOURCE is the real answer: traffic declares itself synthetic when it
// is generated, via the TAS-Synthetic header or a token flagged synthetic. It
// is exact, it survives renaming, and it cannot silently start counting a new
// generator nobody added to a list.
//
// THE DENYLIST IS THE INTERIM, for traffic already in the store and for
// generators not yet updated. It is a heuristic on source app, it WILL rot, and
// it lives here — shared — so the two services cannot disagree about what counts
// as synthetic while looking at the same rows.
//
// The denylist should shrink over time. If it is still growing a year from now,
// mark-at-source has not actually been adopted.

// SyntheticHeader is set by traffic that knows it is not real.
const SyntheticHeader = "TAS-Synthetic"

// syntheticSourceApps is the interim denylist: source apps known to be probes,
// smoke tests or demo generators rather than customer traffic.
//
// Matched case-insensitively, as a prefix, because generators tend to suffix a
// run id or timestamp.
var syntheticSourceApps = []string{
	"claude code",   // agent-driven verification probes
	"step1-gate",    // routing step-1 acceptance rule
	"other-app",     // the control arm of that same check
	"aiqg-demo",     // demo-flow generators (aiqg-demo-flows, aiqg-demo-customer-*)
	"kafka-smoke",   // emitter smoke tests
	"ui-smoke-test", // UI smoke checks
	"dashboard-resolver-smoke",
	"latexp-", // synthetic latency-experiment flows
	"test",    // bare "test" tokens
	"qa",
}

// IsSyntheticSourceApp reports whether a source app is a known non-customer
// generator.
//
// A prefix match rather than equality: generators suffix run ids, and a list
// that only matched exact names would miss "aiqg-demo-flows-2026-08-21" while
// matching "aiqg-demo-flows".
func IsSyntheticSourceApp(app string) bool {
	a := strings.ToLower(strings.TrimSpace(app))
	if a == "" {
		return false
	}
	for _, s := range syntheticSourceApps {
		if strings.HasPrefix(a, s) {
			return true
		}
	}
	return false
}

// SyntheticSourceApps returns the denylist, for a UI that wants to explain why
// a row was excluded rather than merely showing a smaller number.
func SyntheticSourceApps() []string {
	out := make([]string, len(syntheticSourceApps))
	copy(out, syntheticSourceApps)
	return out
}

// SyntheticReason classifies why an event was treated as synthetic, so an
// operator can tell a declared marking from a guessed one — and so the
// denylist's contribution can be watched as it (hopefully) shrinks.
const (
	// SyntheticDeclared: the request said so via header or token.
	SyntheticDeclared = "declared"
	// SyntheticSourceApp: inferred from the interim denylist.
	SyntheticSourceApp = "source_app"
)
