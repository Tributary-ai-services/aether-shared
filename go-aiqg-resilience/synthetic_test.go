package resilience

import "testing"

// The measurement that forced this to exist: every gpt-4o-mini event in 30 days
// of production came from gateway verification probes averaging three output
// tokens, against a real figure of ~48.
func TestKnownProbeTrafficIsRecognised(t *testing.T) {
	for _, app := range []string{"claude code", "step1-gate", "other-app", "kafka-smoke", "ui-smoke-test"} {
		if !IsSyntheticSourceApp(app) {
			t.Errorf("%q was not recognised as synthetic; it would poison the verbosity table", app)
		}
	}
}

// Generators suffix run ids, so exact matching would miss the very traffic the
// list exists to catch.
func TestPrefixMatchingCatchesSuffixedGenerators(t *testing.T) {
	for _, app := range []string{"aiqg-demo-flows", "aiqg-demo-customer-001", "latexp-rag-2026"} {
		if !IsSyntheticSourceApp(app) {
			t.Errorf("%q not matched; generators suffix run ids", app)
		}
	}
}

func TestCaseAndWhitespaceInsensitive(t *testing.T) {
	if !IsSyntheticSourceApp("  Claude Code  ") {
		t.Fatal("matching must ignore case and surrounding whitespace")
	}
}

// Over-matching is its own failure: excluding real traffic makes the table
// emptier and the abstention silent.
func TestRealTrafficIsNotExcluded(t *testing.T) {
	for _, app := range []string{"checkout", "support-bot", "aether-be", "agent-builder", ""} {
		if IsSyntheticSourceApp(app) {
			t.Errorf("%q was wrongly treated as synthetic", app)
		}
	}
}

func TestDenylistIsInspectable(t *testing.T) {
	// A UI should be able to explain WHY a row was excluded rather than only
	// showing a smaller number.
	if len(SyntheticSourceApps()) == 0 {
		t.Fatal("denylist is not inspectable")
	}
	// And the copy must not be the live slice.
	got := SyntheticSourceApps()
	got[0] = "mutated"
	if IsSyntheticSourceApp("mutated") {
		t.Fatal("SyntheticSourceApps exposed the underlying slice")
	}
}
