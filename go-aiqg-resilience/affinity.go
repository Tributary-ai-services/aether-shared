package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Provider affinity — routing-decision.md §5.5, cache-keys-and-sessions.md §4.
//
// # Why this lives in a module called "resilience"
//
// It is not resilience, and the name is worth a word. This module is in
// practice the shared ROUTING-DECISION CONTRACT: the blocks a route rule
// carries that both aiqg-dashboard-be (write-time validation) and
// tas-llm-router (run-time application) must agree about. Affinity is one of
// those blocks. A separate module for one struct would mean a second sibling
// replace in two services and a second thing to keep in step, to buy a more
// accurate package name — a bad trade. Renaming the module would churn every
// import for the same reason.
//
// # What affinity is for
//
// Keeping a conversation on the provider whose vendor prompt cache is warm,
// because switching providers discards that cache and pays a full cold
// rebuild. That framing sets its precedence: it is an ECONOMIC optimisation and
// yields to every correctness concern — constraints, health, and an explicit
// pin — above it.

// AffinityKeySource names the identity affinity sticks on. These reuse the
// experiment runner's existing enum rather than inventing a second vocabulary
// with the same fallbacks and the same edge cases.
var AffinityKeySources = []string{"conversation", "user", "flow", "principal", "ip"}

// AffinityScopes are the granularities affinity can pin at.
var AffinityScopes = []string{"vendor", "vendor+model"}

// AffinityOnBreak values describe what happens when affinity cannot be honoured.
var AffinityOnBreakValues = []string{"prefer_same", "allow_switch", "fail"}

// Affinity is a rule's provider-affinity settings. The zero value means no
// affinity, which is the correct default for single-shot API traffic.
type Affinity struct {
	// KeySource is the identity to stick on. Empty disables affinity.
	KeySource string `json:"key_source,omitempty"`
	// Scope is "vendor" or "vendor+model". Empty means vendor+model.
	//
	// vendor+model is the honest default because vendor prompt caches are
	// MODEL-scoped: affinity to a vendor alone can land on a different model
	// and rebuild the cache anyway, appearing to hold while buying nothing.
	Scope string `json:"scope,omitempty"`
	// TTLSeconds bounds both the stored affinity and the idle gap that ends an
	// epoch. Zero means the gateway default (300s), chosen to match Anthropic's
	// cache lifetime — affinity should expire when the thing it protects does.
	TTLSeconds int `json:"ttl_seconds,omitempty"`
	// OnBreak selects the behaviour when the affine target is unusable. Empty
	// means prefer_same.
	OnBreak string `json:"on_break,omitempty"`
}

// IsZero reports whether affinity is unconfigured.
func (a Affinity) IsZero() bool { return a == Affinity{} }

// MaxAffinityTTLSeconds bounds the TTL. Beyond an hour, affinity is no longer
// protecting a vendor cache — the longest vendor TTL available is an hour — it
// is just pinning traffic, which costs routing freedom and buys nothing.
const MaxAffinityTTLSeconds = 3600

// Validate reports every problem at once.
func (a Affinity) Validate() error {
	var errs []string
	if a.KeySource != "" && !oneOf(a.KeySource, AffinityKeySources) {
		errs = append(errs, fmt.Sprintf("key_source %q is not one of %s", a.KeySource, strings.Join(AffinityKeySources, ", ")))
	}
	if a.Scope != "" && !oneOf(a.Scope, AffinityScopes) {
		errs = append(errs, fmt.Sprintf("scope %q is not one of %s", a.Scope, strings.Join(AffinityScopes, ", ")))
	}
	if a.OnBreak != "" && !oneOf(a.OnBreak, AffinityOnBreakValues) {
		errs = append(errs, fmt.Sprintf("on_break %q is not one of %s", a.OnBreak, strings.Join(AffinityOnBreakValues, ", ")))
	}
	if a.TTLSeconds < 0 || a.TTLSeconds > MaxAffinityTTLSeconds {
		errs = append(errs, fmt.Sprintf("ttl_seconds must be between 0 and %d — beyond that affinity is no longer protecting a vendor cache, only pinning traffic", MaxAffinityTTLSeconds))
	}
	// A setting that only takes effect alongside another is worth refusing
	// rather than silently ignoring: someone who set scope or on_break clearly
	// intended affinity to be on.
	if a.KeySource == "" && (a.Scope != "" || a.OnBreak != "" || a.TTLSeconds > 0) {
		errs = append(errs, "affinity settings were given without key_source, so affinity is off and none of them apply")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(v), a) {
			return true
		}
	}
	return false
}
