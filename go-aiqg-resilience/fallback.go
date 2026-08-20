package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Ordered failover and compliance constraints — routing-decision.md §5.4,
// step 3.
//
// # Why an ordered chain rather than a boolean
//
// Fallback is currently one process-wide flag: on, and the router picks
// something. That cannot express the thing operators actually need, which is
// an ORDER — try the cheap model, then the capable one, then the other vendor
// — nor can it express a tenant for whom one of those vendors is forbidden.
// A boolean answers "may we fail over?"; the real questions are "to what, in
// what order, and is that allowed?"
//
// # Eject-eligibility and fallback-eligibility are different axes
//
// This is the subtlety worth stating plainly, because conflating the two is
// the natural mistake and it produces two distinct bugs.
//
// Whether a failure should EJECT a provider asks: is the provider broken?
// Whether it should ADVANCE THE CHAIN asks: would a different provider do
// better? Those come apart in both directions:
//
//	429 rate limit ......... never ejects (the vendor is healthy and throttling)
//	                         but SHOULD fall back — another vendor has capacity.
//	context overflow ....... never ejects (our request was too big for this
//	                         model) but SHOULD fall back — a larger-window
//	                         model can serve it. This is the case a naive "4xx
//	                         means don't retry elsewhere" rule gets wrong.
//	malformed request ...... never ejects AND must NOT fall back. Every
//	                         provider will reject it identically, so failing
//	                         over only multiplies one client error across every
//	                         vendor, burning the chain and the latency budget
//	                         to arrive at the same answer.
//	auth failure ........... never ejects the vendor for other tenants, and
//	                         must not fall back: a bad key is ours to fix, and
//	                         silently serving from a different vendor hides it.
//	5xx / timeout .......... ejects AND falls back.
//
// So the two taxonomies are defined separately and deliberately do not share a
// type. The gateway's breaker owns the first; this owns the second.
//
// # Why entries are validated at write time
//
// A chain is exercised precisely when something is already wrong. Discovering
// then that an entry names a model the provider does not serve, or a vendor
// this tenant is forbidden to use, converts a routine failover into a second
// incident — and a compliance breach discovered during an outage is the worst
// possible time to discover one. Every entry is therefore checked against the
// provider catalogue and the tenant's constraints before it can be stored.

// FailureClass names a reason an attempt failed, for deciding whether the
// fallback chain should advance. Kept separate from the breaker's ejection
// taxonomy — see the doc above.
type FailureClass string

const (
	// FailureVendorError is a 5xx or connection failure. The provider is
	// broken; another may not be.
	FailureVendorError FailureClass = "vendor_error"
	// FailureTimeout is the provider not answering in time.
	FailureTimeout FailureClass = "timeout"
	// FailureContextOverflow is the request exceeding this model's window. A
	// larger-window model can serve it unchanged, which is exactly what a
	// chain is for.
	FailureContextOverflow FailureClass = "context_overflow"
	// FailureRateLimited is a 429. The vendor is healthy but has no capacity
	// for us right now; another vendor does.
	FailureRateLimited FailureClass = "rate_limited"
	// FailureContentFilter is the provider refusing on policy grounds.
	// Eligible but NOT a default: vendors disagree about content policy, so
	// failing over on a refusal means shopping for a vendor that will comply,
	// which a tenant should have to opt into deliberately rather than inherit.
	FailureContentFilter FailureClass = "content_filter"
)

// DefaultFallbackOn is what a chain falls back on when `on` is unset. These
// three share a property the others do not: the request is well-formed and
// permitted, and only this provider could not serve it. That is precisely the
// case where trying another provider is likely to help and unlikely to cause
// harm.
//
// rate_limited is excluded from the default despite being eligible, because
// failing over on throttling shifts load onto a second vendor at the moment
// the first is already saturated — usually right, occasionally how a partial
// brownout becomes a total one. It is opt-in.
var DefaultFallbackOn = []FailureClass{FailureVendorError, FailureTimeout, FailureContextOverflow}

// AllFailureClasses is every class a chain may be configured to act on.
var AllFailureClasses = []FailureClass{
	FailureVendorError, FailureTimeout, FailureContextOverflow,
	FailureRateLimited, FailureContentFilter,
}

func (f FailureClass) valid() bool {
	for _, c := range AllFailureClasses {
		if c == f {
			return true
		}
	}
	return false
}

// ChainEntry is one failover tier: a concrete provider and model.
//
// Model is REQUIRED here, unlike in a routing target where an empty model
// means "keep the caller's". A fallback is often reached because the caller's
// model was the problem — it overflowed, or its provider is down — so
// carrying that model forward to the next tier reproduces the failure. Making
// the operator name the model forces the question "what should serve this
// instead?" to be answered when there is time to think about it.
type ChainEntry struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (e ChainEntry) String() string { return e.Provider + "/" + e.Model }

// Fallback is an ordered failover chain.
type Fallback struct {
	// Chain is tried in order. Empty means no failover.
	Chain []ChainEntry `json:"chain,omitempty"`
	// On lists the failure classes that advance the chain. Empty means
	// DefaultFallbackOn.
	On []FailureClass `json:"on,omitempty"`
}

// IsZero reports whether no failover is configured.
func (f Fallback) IsZero() bool { return len(f.Chain) == 0 && len(f.On) == 0 }

// Classes returns the effective failure classes, applying the default.
func (f Fallback) Classes() []FailureClass {
	if len(f.On) == 0 {
		return DefaultFallbackOn
	}
	return f.On
}

// Advances reports whether this failure class should move to the next tier.
func (f Fallback) Advances(c FailureClass) bool {
	for _, want := range f.Classes() {
		if want == c {
			return true
		}
	}
	return false
}

// Constraints is compliance AS DECLARED for a tenant: what routing is
// permitted to do, irrespective of what any individual rule asks for.
//
// It sits at tenant level rather than on a rule because it is a property of
// the customer's obligations, not of a traffic pattern. A per-rule constraint
// would be one forgotten rule away from being violated, and "we are not
// allowed to send this customer's data to vendor X" must not depend on
// remembering to repeat it.
type Constraints struct {
	// DenyVendors names providers this tenant may never be routed to, by any
	// rule, including as a fallback.
	DenyVendors []string `json:"deny_vendors,omitempty"`
	// RequireZDR restricts routing to providers under a zero-data-retention
	// agreement.
	RequireZDR bool `json:"require_zdr,omitempty"`
}

// IsZero reports whether nothing is constrained.
func (c Constraints) IsZero() bool { return len(c.DenyVendors) == 0 && !c.RequireZDR }

// Denies reports whether a provider is forbidden. Case-insensitive, because a
// constraint that can be bypassed by capitalisation is not a constraint.
func (c Constraints) Denies(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	for _, d := range c.DenyVendors {
		if strings.ToLower(strings.TrimSpace(d)) == p {
			return true
		}
	}
	return false
}

// MaxChainLength bounds a chain. Each tier costs a full request timeout on the
// way to failing, so a long chain turns one slow failure into a very slow one —
// and past a handful of tiers the caller has almost certainly given up.
const MaxChainLength = 5

// ProviderCatalogue reports what a gateway can actually route to, so a chain
// entry can be checked against reality rather than against a hardcoded list
// that drifts. Supplied by the caller because the catalogue is deployment
// state, not contract.
type ProviderCatalogue interface {
	// HasModel reports whether provider serves model.
	HasModel(provider, model string) bool
	// Providers lists configured providers, for error messages that tell the
	// operator what they COULD have picked.
	Providers() []string
}

// Validate checks a chain in isolation: shape, duplicates, length, and known
// failure classes. Use ValidateFor to also check it against the provider
// catalogue, the tenant's constraints, and the rule's own target.
func (f Fallback) Validate() error {
	var errs []string
	if len(f.Chain) > MaxChainLength {
		errs = append(errs, fmt.Sprintf("chain has %d entries; the maximum is %d — each tier costs a full timeout before it is tried", len(f.Chain), MaxChainLength))
	}
	seen := map[string]bool{}
	for i, e := range f.Chain {
		p := strings.ToLower(strings.TrimSpace(e.Provider))
		m := strings.TrimSpace(e.Model)
		if p == "" {
			errs = append(errs, fmt.Sprintf("chain[%d]: provider is required", i))
			continue
		}
		if m == "" {
			// See ChainEntry: inheriting the caller's model reproduces the
			// failure that reached the chain in the first place.
			errs = append(errs, fmt.Sprintf("chain[%d] (%s): model is required — a fallback that keeps the caller's model repeats the failure that triggered it", i, p))
			continue
		}
		key := p + "/" + strings.ToLower(m)
		if seen[key] {
			errs = append(errs, fmt.Sprintf("chain[%d] (%s/%s) is a duplicate; a repeated tier can only fail the same way twice", i, p, m))
		}
		seen[key] = true
	}
	for _, c := range f.On {
		if !c.valid() {
			errs = append(errs, fmt.Sprintf("unknown failure class %q; valid classes are %s", c, joinClasses(AllFailureClasses)))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ValidateFor checks a chain against everything that must hold before it can be
// stored: its own shape, the tenant's constraints, the provider catalogue, and
// the rule's target.
//
// targetProvider/targetModel may be empty when the rule pins nothing.
// cat may be nil when no catalogue is available, in which case model existence
// is not checked — better than refusing every chain because the caller could
// not supply one.
func (f Fallback) ValidateFor(c Constraints, cat ProviderCatalogue, targetProvider, targetModel string) error {
	var errs []string
	if err := f.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	tp := strings.ToLower(strings.TrimSpace(targetProvider))
	tm := strings.ToLower(strings.TrimSpace(targetModel))

	for i, e := range f.Chain {
		p := strings.ToLower(strings.TrimSpace(e.Provider))
		m := strings.TrimSpace(e.Model)
		if p == "" || m == "" {
			continue // already reported by Validate
		}
		// The whole point of write-time validation: a compliance breach must
		// not be discovered during an outage.
		if c.Denies(p) {
			errs = append(errs, fmt.Sprintf("chain[%d]: %s is denied for this tenant", i, p))
		}
		if cat != nil && !cat.HasModel(p, m) {
			errs = append(errs, fmt.Sprintf("chain[%d]: %s does not serve model %q (configured providers: %s)", i, p, m, strings.Join(cat.Providers(), ", ")))
		}
		// §6.1 rule 3: a chain may not begin with the target. Retrying the
		// same target is a retry, governed by budgets; putting it first here
		// spends a whole tier re-attempting what just failed.
		if i == 0 && tp != "" && p == tp && (tm == "" || strings.ToLower(m) == tm) {
			errs = append(errs, "chain[0] repeats the rule's own target; that is a retry, which budgets already govern — the chain should start at the first ALTERNATIVE")
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ValidateTarget checks a rule's own target against the tenant's constraints.
// A denied vendor must be refused as a target for the same reason it is
// refused in a chain — and it would be absurd to police the fallback path more
// strictly than the primary one.
func (c Constraints) ValidateTarget(provider string) error {
	if provider != "" && c.Denies(provider) {
		return fmt.Errorf("%s is denied for this tenant", strings.ToLower(strings.TrimSpace(provider)))
	}
	return nil
}

// Validate checks the constraints themselves.
func (c Constraints) Validate() error {
	var errs []string
	seen := map[string]bool{}
	for _, d := range c.DenyVendors {
		v := strings.ToLower(strings.TrimSpace(d))
		if v == "" {
			errs = append(errs, "deny_vendors contains an empty entry")
			continue
		}
		if seen[v] {
			errs = append(errs, fmt.Sprintf("deny_vendors lists %q twice", v))
		}
		seen[v] = true
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func joinClasses(cs []FailureClass) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(c)
	}
	return strings.Join(out, ", ")
}
