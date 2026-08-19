package matcher

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The bug this package exists to prevent: two implementations, same field
// name, different semantics. url_path was RE2 in a route rule and a substring
// in a cohort.
// ---------------------------------------------------------------------------

func TestURLPathIsRegexNotSubstring(t *testing.T) {
	m := Matcher{URLPath: "^/v1/chat$"}
	if m.Matches(Attrs{Path: "/v1/chat_admin"}) {
		t.Fatal("anchored pattern matched /v1/chat_admin — url_path must be RE2, not substring")
	}
	if !m.Matches(Attrs{Path: "/v1/chat"}) {
		t.Fatal("anchored pattern failed to match its exact path")
	}
}

// Substring semantics are still expressible, which is why regex was chosen
// over substring rather than the reverse.
func TestUnanchoredPatternBehavesLikeContains(t *testing.T) {
	m := Matcher{URLPath: "/v1/chat"}
	if !m.Matches(Attrs{Path: "/api/v1/chat/completions"}) {
		t.Fatal("unanchored pattern should still match anywhere in the path")
	}
}

// The migration must preserve today's cohort behaviour exactly rather than
// guess at intent: a substring containing regex metacharacters becomes a
// literal, not a wildcard.
func TestSubstringToRegexEscapesMetacharacters(t *testing.T) {
	got := SubstringToRegex("v1.chat")
	m := Matcher{URLPath: got}
	if m.Matches(Attrs{Path: "v1Xchat"}) {
		t.Fatalf("converted pattern %q matched v1Xchat — '.' must stay literal", got)
	}
	if !m.Matches(Attrs{Path: "/api/v1.chat/x"}) {
		t.Fatalf("converted pattern %q failed to match the literal text", got)
	}
	if SubstringToRegex("") != "" {
		t.Fatal("empty substring must convert to empty, not to a match-everything pattern")
	}
}

// ---------------------------------------------------------------------------
// Strict decoding. A typo'd field previously decoded to the zero value, and a
// zero matcher matches everything — so a narrow rule silently became match-all.
// ---------------------------------------------------------------------------

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse(json.RawMessage(`{"path": "/v1/*"}`))
	if err == nil {
		t.Fatal("unknown field accepted; a typo'd matcher silently widens to match-all")
	}
	if !strings.Contains(err.Error(), "url_path") {
		t.Fatalf("error %q does not name the valid fields — an author who typed 'path' needs telling", err)
	}
}

func TestParseAcceptsEveryKnownField(t *testing.T) {
	m, err := Parse(json.RawMessage(`{
		"model":["gpt-4o"],"workflow_type":["rag"],"source_app":["checkout"],
		"vendor":["openai"],"url_path":"^/v1/chat",
		"customer_header_match":{"header_name":"X-Team","regex_value":"^eng$"}}`))
	if err != nil {
		t.Fatalf("valid matcher rejected: %v", err)
	}
	if len(m.Fields()) != 6 {
		t.Fatalf("Fields() = %v, want all six", m.Fields())
	}
}

func TestParseEmptyIsMatchAll(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(``), json.RawMessage(`{}`), json.RawMessage(`  `)} {
		m, err := Parse(raw)
		if err != nil {
			t.Fatalf("empty matcher rejected: %v", err)
		}
		if !m.IsEmpty() {
			t.Fatal("empty matcher should report IsEmpty")
		}
		if !m.Matches(Attrs{Model: "anything", Path: "/v1/chat"}) {
			t.Fatal("empty matcher must match all traffic — the documented no-constraint case")
		}
	}
}

// ---------------------------------------------------------------------------
// Fail closed. A broken rule must be inert, never universal.
// ---------------------------------------------------------------------------

func TestBadRegexMatchesNothing(t *testing.T) {
	m := Matcher{URLPath: "^/v1/(unclosed"}
	if m.Matches(Attrs{Path: "/v1/chat"}) {
		t.Fatal("malformed regex matched — must fail closed")
	}
	if m.Matches(Attrs{}) {
		t.Fatal("malformed regex matched empty attrs — must fail closed")
	}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate must report the malformed regex so a write can be rejected")
	}
}

func TestBadHeaderRegexMatchesNothing(t *testing.T) {
	m := Matcher{CustomerHeaderMatch: &HeaderMatch{HeaderName: "X-Team", RegexValue: "("}}
	if m.Matches(Attrs{Headers: map[string]string{"X-Team": "eng"}}) {
		t.Fatal("malformed header regex matched — must fail closed")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		m       Matcher
		wantErr bool
	}{
		{"empty", Matcher{}, false},
		{"valid path", Matcher{URLPath: "^/v1/"}, false},
		{"bad path", Matcher{URLPath: "^/v1/("}, true},
		{"header without name", Matcher{CustomerHeaderMatch: &HeaderMatch{RegexValue: "x"}}, true},
		{"header bad regex", Matcher{CustomerHeaderMatch: &HeaderMatch{HeaderName: "X", RegexValue: "("}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.m.Validate()
			if c.wantErr != (err != nil) {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Matching semantics: AND across fields, OR within, case-insensitive.
// ---------------------------------------------------------------------------

func TestListsOrWithinAndAcross(t *testing.T) {
	m := Matcher{Model: []string{"gpt-4o", "gpt-4o-mini"}, SourceApp: []string{"checkout"}}
	if !m.Matches(Attrs{Model: "gpt-4o-mini", SourceApp: "checkout"}) {
		t.Fatal("OR within a list failed")
	}
	if m.Matches(Attrs{Model: "gpt-4o-mini", SourceApp: "support"}) {
		t.Fatal("AND across fields failed — a non-matching field must reject")
	}
}

func TestCaseInsensitiveAndTrimmed(t *testing.T) {
	m := Matcher{Model: []string{"  GPT-4o  "}}
	if !m.Matches(Attrs{Model: "gpt-4o"}) {
		t.Fatal("list comparison must be case-insensitive and trimmed")
	}
}

func TestHeaderLookupIsCaseInsensitive(t *testing.T) {
	m := Matcher{CustomerHeaderMatch: &HeaderMatch{HeaderName: "x-team", RegexValue: "^eng$"}}
	if !m.Matches(Attrs{Headers: map[string]string{"X-Team": "eng"}}) {
		t.Fatal("header name comparison must be case-insensitive per HTTP semantics")
	}
}

func TestMissingAttributeDoesNotMatchConstrainedField(t *testing.T) {
	m := Matcher{Vendor: []string{"openai"}}
	if m.Matches(Attrs{}) {
		t.Fatal("a constrained field with no supplied attribute must not match")
	}
}

// ---------------------------------------------------------------------------
// Capability. A matcher constraining an attribute the evaluator never supplies
// is authorable and can never fire — measured live for vendor and headers.
// ---------------------------------------------------------------------------

func TestUnsupportedFieldsForGateway(t *testing.T) {
	m := Matcher{
		Model:               []string{"gpt-4o"},
		Vendor:              []string{"openai"},
		CustomerHeaderMatch: &HeaderMatch{HeaderName: "X-Team", RegexValue: "^eng$"},
	}
	bad := m.Unsupported(GatewayResolve)
	if len(bad) != 2 {
		t.Fatalf("Unsupported() = %v, want vendor and customer_header_match", bad)
	}
	if m.Unsupported(Full) != nil {
		t.Fatal("Full capability should support every field")
	}
}

func TestValidateForRejectsUnsupported(t *testing.T) {
	m := Matcher{Vendor: []string{"openai"}}
	err := m.ValidateFor(GatewayResolve)
	if err == nil {
		t.Fatal("a vendor matcher must be rejected — the gateway never supplies vendor")
	}
	if !strings.Contains(err.Error(), "can never match") {
		t.Fatalf("error %q should say the rule can never match, not merely that it is invalid", err)
	}
}

func TestValidateForAcceptsSupported(t *testing.T) {
	m := Matcher{Model: []string{"gpt-4o"}, SourceApp: []string{"checkout"}, URLPath: "^/v1/"}
	if err := m.ValidateFor(GatewayResolve); err != nil {
		t.Fatalf("supported matcher rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Compile is an optimisation, never a behaviour change.
// ---------------------------------------------------------------------------

func TestCompiledMatchesOneShot(t *testing.T) {
	m := Matcher{URLPath: "^/v1/chat", Model: []string{"gpt-4o"}}
	c := m.Compile()
	for _, a := range []Attrs{
		{Path: "/v1/chat", Model: "gpt-4o"},
		{Path: "/v1/chat", Model: "other"},
		{Path: "/other", Model: "gpt-4o"},
		{},
	} {
		if c.Matches(a) != m.Matches(a) {
			t.Fatalf("Compiled and one-shot disagree for %+v", a)
		}
	}
}

func BenchmarkCompiledMatches(b *testing.B) {
	c := Matcher{URLPath: "^/v1/chat", Model: []string{"gpt-4o"}}.Compile()
	a := Attrs{Path: "/v1/chat/completions", Model: "gpt-4o"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Matches(a)
	}
}

func BenchmarkOneShotMatches(b *testing.B) {
	m := Matcher{URLPath: "^/v1/chat", Model: []string{"gpt-4o"}}
	a := Attrs{Path: "/v1/chat/completions", Model: "gpt-4o"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Matches(a)
	}
}
