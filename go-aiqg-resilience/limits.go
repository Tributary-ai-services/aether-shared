package resilience

import (
	"errors"
	"fmt"
	"strings"
)

// Per-model context and output caps — routing-decision.md §5, step 7.
//
// # What limits are for
//
// Turning a vendor error into a decision. Without them, a request that exceeds
// a model's context window is discovered by the VENDOR: a 400 arrives after the
// full round trip, with a message shaped by whoever wrote that provider's API,
// and the gateway has already spent the latency. With them, the gateway knows
// the window before it dials and can say so — or route somewhere that fits.
//
// # A limit may only LOWER an advertised window
//
// Raising one does not extend a model's capability; it just moves the failure
// from a clean pre-flight rejection back to a vendor error, which is exactly
// what limits exist to prevent. So the effective window is always
// min(advertised, configured), and configuring above the advertised value is
// refused at write time rather than silently clamped — silently clamping would
// leave an operator believing they had raised something.
//
// # Why lowering is useful at all
//
// Two reasons, both real. A tenant may want to bound spend or latency by
// refusing very large prompts before they are sent. And a model's advertised
// window is not always its USABLE window — quality and recall degrade well
// before the hard limit — so a tenant may prefer to fail or fall back at 200k
// rather than send a 900k-token request that technically fits.
//
// # The interaction that matters: limits and fallback
//
// A pre-flight rejection must feed the SAME path as a vendor context-overflow,
// not bypass it. Context overflow is fallback-eligible precisely because a
// larger-window model can serve the identical request — so rejecting locally
// and returning an error would lose the recovery that the fallback chain was
// designed to provide, and would make limits strictly worse than having none.
// Callers translate a limit breach into a context-overflow failure and let the
// chain do its job.

// Limits caps context and output for a routing target.
type Limits struct {
	// MaxContextWindow caps total input tokens. Zero means the model's
	// advertised window applies.
	MaxContextWindow int `json:"max_context_window,omitempty"`
	// MaxOutputTokens caps the response. Zero means the model's advertised
	// maximum applies.
	//
	// Distinct from a request's own max_tokens: this is a ceiling the tenant
	// imposes, and the effective cap is the smaller of the two. A caller
	// asking for more than the tenant permits gets the tenant's number, not an
	// error — the request is still serviceable, just bounded.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
}

// IsZero reports whether no limits are configured.
func (l Limits) IsZero() bool { return l == Limits{} }

// Validate checks the shape in isolation.
func (l Limits) Validate() error {
	var errs []string
	if l.MaxContextWindow < 0 {
		errs = append(errs, "max_context_window must not be negative")
	}
	if l.MaxOutputTokens < 0 {
		errs = append(errs, "max_output_tokens must not be negative")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ModelCaps is a model's advertised capability, supplied by the caller.
//
// Today this comes from the gateway's configured capability matrix. The model
// registry (tas-llm-router #2-#6) will eventually replace that source with
// synced vendor data — which changes where the numbers come from, not what
// limits mean.
type ModelCaps struct {
	Provider         string
	Model            string
	MaxContextWindow int
	MaxOutputTokens  int
}

// ValidateFor checks limits against what a model actually advertises.
//
// Refuses any limit ABOVE the advertised value, naming both numbers: an
// operator who typed 500000 against a 200000 window needs to see the real
// window, not merely that their number was rejected.
func (l Limits) ValidateFor(caps []ModelCaps) error {
	if l.IsZero() || len(caps) == 0 {
		return l.Validate()
	}
	if err := l.Validate(); err != nil {
		return err
	}
	var errs []string
	for _, c := range caps {
		if l.MaxContextWindow > 0 && c.MaxContextWindow > 0 && l.MaxContextWindow > c.MaxContextWindow {
			errs = append(errs, fmt.Sprintf(
				"max_context_window %d exceeds what %s/%s advertises (%d); a limit may only lower a window, never raise it",
				l.MaxContextWindow, c.Provider, c.Model, c.MaxContextWindow))
		}
		if l.MaxOutputTokens > 0 && c.MaxOutputTokens > 0 && l.MaxOutputTokens > c.MaxOutputTokens {
			errs = append(errs, fmt.Sprintf(
				"max_output_tokens %d exceeds what %s/%s advertises (%d)",
				l.MaxOutputTokens, c.Provider, c.Model, c.MaxOutputTokens))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// EffectiveContextWindow returns the smaller of the advertised and configured
// windows. Zero on either side means "no opinion from that side".
func (l Limits) EffectiveContextWindow(advertised int) int {
	switch {
	case l.MaxContextWindow <= 0:
		return advertised
	case advertised <= 0:
		return l.MaxContextWindow
	case l.MaxContextWindow < advertised:
		return l.MaxContextWindow
	default:
		// Configured above advertised should have been refused at write time.
		// Clamping here rather than trusting it keeps a stale rule — written
		// before a model's window shrank — from raising a limit at run time.
		return advertised
	}
}

// EffectiveOutputTokens returns the smallest of the advertised maximum, the
// tenant's cap, and the caller's own request.
//
// The caller's request participates rather than being overridden: asking for
// fewer tokens than the cap is a legitimate choice, and a cap is a ceiling
// rather than a target.
func (l Limits) EffectiveOutputTokens(advertised, requested int) int {
	best := 0
	consider := func(v int) {
		if v <= 0 {
			return
		}
		if best == 0 || v < best {
			best = v
		}
	}
	consider(advertised)
	consider(l.MaxOutputTokens)
	consider(requested)
	return best
}

// ExceedsContext reports whether an estimated input exceeds the effective
// window, and by how much.
//
// The overage is returned so a caller can say "3,000 tokens over a 200,000
// window" rather than "too large" — the first tells an operator whether to trim
// a prompt or change models, the second does not.
func (l Limits) ExceedsContext(estimatedTokens, advertised int) (bool, int) {
	window := l.EffectiveContextWindow(advertised)
	if window <= 0 || estimatedTokens <= window {
		return false, 0
	}
	return true, estimatedTokens - window
}
