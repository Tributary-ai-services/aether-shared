// Package matcher is the single evaluator for AIQG traffic matchers.
//
// Two subsystems select traffic: route rules (aiqg-dashboard-be, matcher →
// policy bundle) and experiment cohorts (tas-llm-router, cohort → variant).
// They were implemented separately and diverged on the same field name —
// url_path was an RE2 regex in a route rule and a substring in a cohort, so
// two operators writing "the same" matcher selected different traffic. They
// agreed on "/v1/chat" and diverged the moment anyone typed a "." or a "*".
//
// This package exists so that cannot happen again. It is deliberately a
// separate module consumed by both services via a sibling replace directive,
// following the precedent of aether-shared/go-events.
//
// Semantics, which are the route-rule semantics because they are the stricter
// of the two:
//
//   - an absent or empty field is NO CONSTRAINT
//   - set fields combine with AND; lists OR within, case-insensitively
//   - url_path is an RE2 regex evaluated with MatchString (a substring search
//     over the pattern, so an unanchored pattern still behaves like "contains")
//   - a malformed regex FAILS CLOSED — it matches nothing rather than widening
//   - unknown fields are REJECTED, because lenient decoding silently turned a
//     narrow rule into a match-all one
//
// RE2 over substring is deliberate: substring cannot express anchoring, so a
// cohort keyed on "/v1/chat" also claims "/v1/chat_admin". Regex can express
// substring; substring cannot express regex.
package matcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Matcher mirrors the stored JSONB shape for both route rules and experiment
// cohorts. Field tags are the wire contract — renaming one is a breaking
// change for stored rows.
type Matcher struct {
	Model               []string     `json:"model,omitempty"`
	WorkflowType        []string     `json:"workflow_type,omitempty"`
	SourceApp           []string     `json:"source_app,omitempty"`
	Vendor              []string     `json:"vendor,omitempty"`
	URLPath             string       `json:"url_path,omitempty"`
	CustomerHeaderMatch *HeaderMatch `json:"customer_header_match,omitempty"`
}

// HeaderMatch matches one named header's value by RE2 regex. Header name
// comparison is case-insensitive per HTTP semantics.
type HeaderMatch struct {
	HeaderName string `json:"header_name"`
	RegexValue string `json:"regex_value"`
}

// Attrs are the request attributes a matcher is evaluated against.
//
// A field left empty means "the caller could not supply this". That is not the
// same as "the request has no such value", and it matters: a matcher
// constraining a field the caller never populates can never match. Callers
// should validate with Supported before storing a matcher — see ErrUnsupported.
type Attrs struct {
	Model        string
	WorkflowType string
	SourceApp    string
	Vendor       string
	Path         string
	Headers      map[string]string
}

// Parse decodes a stored matcher. Decoding is STRICT: an unknown field is an
// error rather than something to ignore.
//
// Default encoding/json behaviour is the dangerous option here. A matcher
// stored as {"path": "/v1/*"} — the field is url_path — decodes to the zero
// value under lenient decoding, and a zero Matcher matches everything. A rule
// authored to be narrow silently became match-all. That was live.
//
// A nil or empty payload decodes to the zero value, which is the documented
// "no constraint" case and is intentional.
func Parse(raw json.RawMessage) (Matcher, error) {
	var m Matcher
	if len(bytes.TrimSpace(raw)) == 0 {
		return m, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("matcher: %w (valid fields: model, workflow_type, source_app, vendor, url_path, customer_header_match)", err)
	}
	return m, nil
}

// Validate reports whether a matcher is usable, independent of whether the
// caller can supply the attributes it constrains. Use at write time so an
// unusable matcher is rejected with an error the author can act on, rather
// than stored and silently skipped forever.
func (m Matcher) Validate() error {
	if m.URLPath != "" {
		if _, err := regexp.Compile(m.URLPath); err != nil {
			return fmt.Errorf("matcher.url_path is an RE2 regex and failed to compile: %w", err)
		}
	}
	if m.CustomerHeaderMatch != nil {
		if strings.TrimSpace(m.CustomerHeaderMatch.HeaderName) == "" {
			return fmt.Errorf("matcher.customer_header_match.header_name must not be empty")
		}
		if _, err := regexp.Compile(m.CustomerHeaderMatch.RegexValue); err != nil {
			return fmt.Errorf("matcher.customer_header_match.regex_value failed to compile: %w", err)
		}
	}
	return nil
}

// IsEmpty reports whether the matcher constrains nothing, i.e. matches all
// traffic. Useful for UI summaries and for warning on an unintended match-all.
func (m Matcher) IsEmpty() bool {
	return len(m.Model) == 0 && len(m.WorkflowType) == 0 && len(m.SourceApp) == 0 &&
		len(m.Vendor) == 0 && m.URLPath == "" &&
		(m.CustomerHeaderMatch == nil || m.CustomerHeaderMatch.HeaderName == "")
}

// Fields returns the constrained field names, in a stable order. For UI
// summaries and for Supported.
func (m Matcher) Fields() []string {
	var f []string
	if len(m.Model) > 0 {
		f = append(f, "model")
	}
	if len(m.WorkflowType) > 0 {
		f = append(f, "workflow_type")
	}
	if len(m.SourceApp) > 0 {
		f = append(f, "source_app")
	}
	if len(m.Vendor) > 0 {
		f = append(f, "vendor")
	}
	if m.URLPath != "" {
		f = append(f, "url_path")
	}
	if m.CustomerHeaderMatch != nil && m.CustomerHeaderMatch.HeaderName != "" {
		f = append(f, "customer_header_match")
	}
	return f
}

// Compiled is a Matcher with its regexes pre-compiled. Evaluation happens on
// the request path, so compiling per request — which the original route-rule
// implementation did — is wasted work on every request that reaches a rule.
//
// A Compiled built from a matcher that fails Validate matches NOTHING. That is
// the fail-closed contract: a broken rule is inert, never universal.
type Compiled struct {
	m         Matcher
	path      *regexp.Regexp
	headerRe  *regexp.Regexp
	headerKey string
	broken    bool
}

// Compile prepares a matcher for repeated evaluation. It never returns an
// error: an invalid matcher yields a Compiled that matches nothing, so a bad
// rule cannot widen. Use Validate at write time to surface the reason.
func (m Matcher) Compile() Compiled {
	c := Compiled{m: m}
	if m.URLPath != "" {
		re, err := regexp.Compile(m.URLPath)
		if err != nil {
			c.broken = true
			return c
		}
		c.path = re
	}
	if m.CustomerHeaderMatch != nil && m.CustomerHeaderMatch.HeaderName != "" {
		re, err := regexp.Compile(m.CustomerHeaderMatch.RegexValue)
		if err != nil {
			c.broken = true
			return c
		}
		c.headerRe = re
		c.headerKey = m.CustomerHeaderMatch.HeaderName
	}
	return c
}

// Matches reports whether the compiled matcher accepts these attributes.
func (c Compiled) Matches(a Attrs) bool {
	if c.broken {
		return false
	}
	if len(c.m.Model) > 0 && !containsFold(c.m.Model, a.Model) {
		return false
	}
	if len(c.m.WorkflowType) > 0 && !containsFold(c.m.WorkflowType, a.WorkflowType) {
		return false
	}
	if len(c.m.SourceApp) > 0 && !containsFold(c.m.SourceApp, a.SourceApp) {
		return false
	}
	if len(c.m.Vendor) > 0 && !containsFold(c.m.Vendor, a.Vendor) {
		return false
	}
	if c.path != nil && !c.path.MatchString(a.Path) {
		return false
	}
	if c.headerRe != nil && !c.headerRe.MatchString(headerLookup(a.Headers, c.headerKey)) {
		return false
	}
	return true
}

// Matches is the one-shot form, for callers that evaluate a matcher once.
// Prefer Compile + Compiled.Matches on the request path.
func (m Matcher) Matches(a Attrs) bool { return m.Compile().Matches(a) }

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}

// headerLookup does a case-insensitive header lookup, per HTTP semantics.
func headerLookup(headers map[string]string, name string) string {
	if v, ok := headers[name]; ok {
		return v
	}
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// SubstringToRegex converts a legacy substring pattern into the equivalent RE2
// regex, preserving today's behaviour exactly rather than guessing at intent.
//
// Used by the one-shot migration of stored experiment cohorts, whose url_path
// was a substring match. QuoteMeta escapes regex metacharacters, so a cohort
// keyed on "v1.chat" continues to mean the literal text and does not silently
// become "v1<any char>chat".
func SubstringToRegex(substr string) string {
	if substr == "" {
		return ""
	}
	return regexp.QuoteMeta(substr)
}

// Capability describes which attributes a given call site can actually supply.
//
// This exists because a matcher field is useless — worse than useless, because
// it is authorable — when the evaluating service never populates the attribute
// it constrains. Measured on 2026-08-19: the gateway sends the policy resolver
// only tenant, source app and path at request receipt, and re-resolves with
// model and workflow at routing time. It never sends vendor, and never sends
// headers at all. So `vendor` and `customer_header_match` rules could be
// created through the API and in the UI, and could never fire.
//
// Rather than delete those fields — which would break stored rows and lose the
// ability to support them later — a call site declares what it can supply, and
// writes are rejected with an explanation.
type Capability struct {
	Model        bool
	WorkflowType bool
	SourceApp    bool
	Vendor       bool
	Path         bool
	Headers      bool
}

// GatewayResolve is what tas-llm-router can currently supply to the policy
// resolver. Model and WorkflowType are true because they arrive on the
// routing-time re-resolve, even though they are absent at receipt.
//
// Update this — and only this — when the gateway starts sending more.
var GatewayResolve = Capability{
	Model:        true,
	WorkflowType: true,
	SourceApp:    true,
	Path:         true,
	Vendor:       false, // never populated by the gateway
	Headers:      false, // gateway does not forward customer headers
}

// Full supplies everything; for tests and for call sites that build Attrs
// themselves from a complete request.
var Full = Capability{Model: true, WorkflowType: true, SourceApp: true, Vendor: true, Path: true, Headers: true}

// Unsupported returns the constrained fields this capability cannot supply, in
// stable order. Empty means the matcher is evaluable end to end.
func (m Matcher) Unsupported(c Capability) []string {
	var bad []string
	if len(m.Model) > 0 && !c.Model {
		bad = append(bad, "model")
	}
	if len(m.WorkflowType) > 0 && !c.WorkflowType {
		bad = append(bad, "workflow_type")
	}
	if len(m.SourceApp) > 0 && !c.SourceApp {
		bad = append(bad, "source_app")
	}
	if len(m.Vendor) > 0 && !c.Vendor {
		bad = append(bad, "vendor")
	}
	if m.URLPath != "" && !c.Path {
		bad = append(bad, "url_path")
	}
	if m.CustomerHeaderMatch != nil && m.CustomerHeaderMatch.HeaderName != "" && !c.Headers {
		bad = append(bad, "customer_header_match")
	}
	return bad
}

// ValidateFor is Validate plus a capability check. Use at write time.
func (m Matcher) ValidateFor(c Capability) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if bad := m.Unsupported(c); len(bad) > 0 {
		return fmt.Errorf("matcher constrains %s, which the evaluating service does not supply — a rule using %s can never match",
			strings.Join(bad, ", "), pluralise(len(bad)))
	}
	return nil
}

func pluralise(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// ---------------------------------------------------------------------------
// On engine choice: RE2 here, Hyperscan in Gatekeeper.
//
// These are different jobs, not a capability downgrade, and the distinction is
// worth recording so nobody "upgrades" this module later and breaks a build.
//
// Expressiveness is essentially identical. Both RE2 and Hyperscan are
// automata-based; NEITHER supports backreferences or arbitrary lookaround, for
// the same reason — those constructs cannot be evaluated in guaranteed linear
// time. Hyperscan is in places MORE restrictive: it accepts ^ and $ only where
// they can match at a buffer boundary. So a pattern expressible here is very
// nearly the set expressible there.
//
// What Hyperscan buys is throughput on a different shape of problem: scanning
// one large body against thousands of patterns simultaneously, in streaming
// mode, with SIMD acceleration. That is exactly Gatekeeper's job — inspecting
// prompts and completions — and exactly not this one, which evaluates a handful
// of rules against six short attribute strings.
//
// What RE2 buys is that this module stays pure Go. aiqg-dashboard-be builds
// CGO_ENABLED=0 onto distroless/static; Hyperscan would require CGO, a
// libhyperscan runtime and a Debian base. That is a large deployment change to
// buy pattern syntax we already have.
//
// The line to hold: if route rules ever need to match on request CONTENT rather
// than attributes, that is not a bigger regex here — it is a call into the
// scanner, which already has the content, the engine and the pattern catalogue.
// Growing this module toward content matching would duplicate Gatekeeper badly.
//
// One real papercut, documented rather than solved: a policy pattern and a route
// matcher are now evaluated by different engines, so a pattern cannot be copied
// verbatim between them with total confidence. Both reject backreferences, but
// their anchor handling differs. Anything shared between the two should be
// tested against both.
