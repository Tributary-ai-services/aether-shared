package resilience

// Controls are per-tenant runtime switches for routing features that are
// otherwise gated by a gateway-wide default (a process env flag). They ride
// every resolution, like Constraints, because they bound routing as a whole
// rather than a single traffic pattern — a tenant either runs the breaker or it
// does not, independent of which rule matched.
//
// Each field is a *bool with three meaningful states:
//
//	nil    → inherit the gateway default (the env flag). No preference expressed.
//	&true  → force the feature ON for this tenant, even if the gateway default is off.
//	&false → force the feature OFF for this tenant, even if the gateway default is on.
//
// The tri-state is the whole point: "this tenant has not expressed a
// preference" must stay distinct from "this tenant wants it off", or turning on
// a gateway-wide default would silently override a tenant that deliberately
// opted out.
type Controls struct {
	// BreakerEnabled toggles passive outlier ejection + retry budgets for the
	// tenant's traffic.
	BreakerEnabled *bool `json:"breaker_enabled,omitempty"`
	// AffinityEnabled toggles provider affinity (warm-cache stickiness) for the
	// tenant's traffic.
	AffinityEnabled *bool `json:"affinity_enabled,omitempty"`
	// BreakerIsolated, when true, gives the tenant its OWN breaker keyspace:
	// a provider tripping under this tenant's traffic ejects it only for this
	// tenant, and this tenant's selection is unaffected by other tenants'
	// failures. When nil/false the tenant shares the fleet-wide breaker state
	// (the default), so a provider genuinely down is avoided for everyone.
	//
	// Isolation is an economic/blast-radius choice, not a compliance one:
	// isolation trades a shared, cheaply-cached ejection view for a per-tenant
	// one that must be read from the store per request.
	BreakerIsolated *bool `json:"breaker_isolated,omitempty"`
}

// IsZero reports whether the tenant expressed no preference on any feature, so a
// resolver can omit the block rather than send an empty object that would read
// as deliberate configuration.
func (c Controls) IsZero() bool {
	return c.BreakerEnabled == nil && c.AffinityEnabled == nil && c.BreakerIsolated == nil
}

// BreakerIsolatedOr resolves the effective breaker-isolation setting, folding
// the tenant's preference over the gateway default (shared). A nil pointer (no
// preference) yields def.
func (c Controls) BreakerIsolatedOr(def bool) bool {
	if c.BreakerIsolated != nil {
		return *c.BreakerIsolated
	}
	return def
}

// BreakerEnabledOr resolves the effective breaker enablement, folding the
// tenant's preference over the gateway default. A nil pointer (no preference)
// yields def.
func (c Controls) BreakerEnabledOr(def bool) bool {
	if c.BreakerEnabled != nil {
		return *c.BreakerEnabled
	}
	return def
}

// AffinityEnabledOr resolves the effective affinity enablement, folding the
// tenant's preference over the gateway default. A nil pointer (no preference)
// yields def.
func (c Controls) AffinityEnabledOr(def bool) bool {
	if c.AffinityEnabled != nil {
		return *c.AffinityEnabled
	}
	return def
}
