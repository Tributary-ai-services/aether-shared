# AIQG Routing Decision — unified resolve contract

---

**Metadata**

```yaml
service: aiqg-dashboard-be + tas-llm-router
model: AIQGRoutingDecision
database: PostgreSQL (aiqg schema)
version: 0.1.0
last_updated: 2026-08-18
status: proposed (additive; no existing type, endpoint or column changes shape)
spec_refs: source-spec-v0.2.md §3.5.2 (route-attached policy / resolution order)
plan_ref: build-vs-reuse.md §2.5 (routes), §7.5 (MVP vs Phase-2 matchers)
supersedes: nothing — extends route-rule.md §2 resolve response
```

---

## 1. Overview

Three subsystems currently answer the same question — *what should happen to this
request* — with three matcher languages, three storage models, and no defined
precedence between them:

| system | matches on | decides | where it runs |
|---|---|---|---|
| **route rules** | url_path, model, vendor, source_app, workflow_type, header | which **policy bundle** applies | `aiqg-dashboard-be` `/internal/policy/resolve` |
| **experiments** | url_path, source_app, model, workflow_type | **model swap + params** for a cohort | `tas-llm-router` `pkg/aiqg/experiments`, at routing time |
| **router strategy** | nothing (global config) | provider choice + **fallback** | `tas-llm-router` `internal/routing` |

They overlap on matcher vocabulary (`model`, `source_app`, `workflow_type`,
`url_path` appear in two of the three) while composing by accident rather than by
contract. Nothing states what happens when a route rule targets Anthropic, an
experiment swaps the model to a GPT variant, and the router's cost-optimized
strategy prefers a third provider.

This note proposes one resolve contract that returns a **routing decision**
rather than a policy bundle, and fixes the order in which the three systems
compose. It is a contract change, not a feature: the point is to settle the seam
before enforcement (Stage 4.1), fallback policy and the model registry are all
built against the current bundle-only shape and have to be reworked.

### 1.1 What already exists (audited 2026-08-18)

Capability inventory, so the design starts from facts rather than intent:

| capability | state | evidence |
|---|---|---|
| Policy bundle resolution | **live** | `source=route_rule` proven for tenant `65ea3247`; precedence explicit→route_rule→tenant_active→default |
| Policy **enforcement** | **not built** | `pkg/aiqg/policy` header: *"Phase 4.0 — observation-only … takes no enforcement action yet"*; no `block`/`redact` handling anywhere in the gateway |
| A/B model testing | **live, separate surface** | `experiments.NewResolver` wired at `server.go:513/545`; `ExperimentsPage.tsx`; override axes `model` + params |
| Provider fallback | **live, global only** | `internal/routing/router.go` sets `FallbackUsed`; `fallback_enabled` is process-wide config |
| Per-rule provider steering | **dead column** | `route_rule.provider_override` is stored, CRUD'd and validated, but `bundleResolveResponse` never returns it and no gateway code reads it |
| Context-window / vendor config | **hardcoded** | `anthropic/provider.go:88` = 200000, `openai/provider.go:88` = 128000 |
| Model registry | **unbuilt** | tas-llm-router #2–#6 all open; #5 is registry-driven fallback |
| `time_window` matcher | **unbuilt** | designed in route-rule.md §78–86, absent from code |
| `workflow_type` matcher | **drift** | design says MVP must *reject* it; implementation accepts and matches on it |
| Experiment axes `system_prompt`, `prompt_template_id`, `policy_bundle_id`, `gatekeeper_profile` | **unbuilt** | experiment.md notes "not all wired"; none are |

Two documentation drifts to correct in the same change: route-rule.md §10.2
places matcher evaluation in `tas-llm-router/internal/policy/resolver.go` (it is
in `aiqg-dashboard-be/internal/handlers/route_match.go`), and §10.4 describes
Neo4j migrations (route rules are PostgreSQL).

---

## 2. Schema Definition

The resolve response grows from a bundle reference to a decision. Every field
except the existing three is optional, so an older gateway ignores what it does
not understand and behaves exactly as today.

```jsonc
// POST /internal/policy/resolve → 200
{
  // ---- existing, unchanged ----
  "bundle_id":   "6da56444-…",
  "bundle_name": "Compliance",
  "source":      "route_rule",       // explicit | route_rule | tenant_active | default
  "reduction":   { /* extraction policy */ },

  // ---- new, all optional ----
  "target": {                         // where the request should go
    "provider": "anthropic",          // from route_rule.provider_override
    "model":    null,                 // null = keep the caller's model
    "source":   "route_rule"          // which rung set the target
  },
  "fallback": {                       // what to do when the target fails
    "chain": [                        // ordered; empty = use global router strategy
      {"provider": "anthropic", "model": "claude-haiku-4-5-20251001"},
      {"provider": "openai",    "model": "gpt-4o-mini"}
    ],
    "on": ["vendor_error", "timeout", "context_overflow"],
    "max_attempts": 2
  },
  "limits": {                         // vendor config overrides (model registry)
    "max_context_window": 200000,     // null = provider default
    "max_output_tokens":  4096
  },
  "enforcement": {                    // Stage 4.1; absent = observe-only
    "mode": "observe"                 // observe | enforce
  }
}
```

### 2.1 Why `target` is a struct, not a string

`provider_override` today is a bare string column. A decision needs to say
*which rung decided* — a provider chosen by an explicit header, by a route rule,
or by global strategy have different debugging and audit meanings, and the event
already carries `resolved_policy_bundle_source` for exactly that reason. Keeping
the shape parallel to the bundle fields means one mental model for both.

---

## 3. Composition order

The contract is: **resolution decides, experiments override, the router
executes.** Each stage may only narrow or replace what the previous stage
produced, and every stage stamps its identity on the event.

```
1. RESOLVE      (dashboard-be)   matcher → {bundle, target, fallback, limits}
2. OVERLAY      (gateway)        running experiment may replace target.model / params
3. EXECUTE      (gateway)        router honours target; on failure walks fallback.chain
4. ENFORCE      (gateway, 4.1)   bundle rules applied per enforcement.mode
```

Rules that follow from this ordering:

- **An experiment outranks a route rule on `model`, and only on `model`.** That
  is the whole point of an experiment: deliberately diverting a slice of traffic
  from what policy would otherwise pick. It must never override `bundle` —
  a test may not quietly relax someone's compliance policy — which is why the
  designed-but-unwired `policy_bundle_id` experiment axis should stay unwired
  until enforcement exists and can express that intent explicitly.
- **Fallback never crosses a policy boundary.** Every entry in `fallback.chain`
  is evaluated against the same resolved bundle. If a tenant's policy forbids a
  vendor, that vendor cannot appear in a chain — validated at write time, not
  discovered at failover time.
- **Global router strategy is the floor, not the ceiling.** With no `target`,
  behaviour is exactly today's config-driven strategy. `target` present pins the
  provider; `fallback.chain` empty falls back to the global strategy.

### 3.1 Precedence table

| decision | set by | beaten by |
|---|---|---|
| policy bundle | explicit header → route rule → tenant active → default | nothing (experiments must not touch it) |
| provider/model target | route rule `provider_override` | a running experiment's `override.model` |
| params (temperature, top_p, max_tokens) | caller | experiment `override.params` |
| context window / output cap | model registry, then `limits` | nothing |
| fallback chain | route rule | nothing (chain is walked in order) |

---

## 4. Validation Rules

1. `target.provider` must name a configured, enabled provider for the tenant —
   rejected at write time, not at request time.
2. Every `fallback.chain` entry must be a valid `(provider, model)` pair per the
   model registry once it exists; until then, against the static capability
   table.
3. A chain may not contain the target as its first entry (that is a retry, not a
   fallback) and may not repeat an entry.
4. `limits.max_context_window` may only *lower* the provider's advertised
   window, never raise it. Raising it converts a clean pre-flight rejection into
   a vendor error.
5. `enforcement.mode = enforce` requires the bundle to have at least one rule
   with a non-`log` action, so "enforcing" never silently means "doing nothing".

---

## 5. Relationships

```
AIQGRouteRule ──matches──▶ RoutingDecision ──references──▶ AIQGPolicyBundle
                                │                              │
                                ├──target/fallback──▶ ModelRegistry (#2–#6)
                                └──overlaid by──▶ AIQGExperiment (model axis only)
```

---

## 6. Migration Strategy

Deliberately incremental, because the contract is worth proving on one field
before the whole shape is committed.

**Step 1 — wire `provider_override` end to end.** It already exists in schema,
store, API and UI, and is already validated. Return it as `target.provider` from
the resolver and have the gateway honour it. One field, no new storage, and it
converts a dead column into the proof that resolution can steer execution.

**Step 2 — `fallback.chain`.** New JSONB column on `route_rule`. Until the model
registry lands, validate entries against the static capability table.

**Step 3 — `limits`.** Meaningful only once the model registry (#2–#6) supplies
real per-model windows; before that it would encode the same hardcoded numbers
in a second place.

**Step 4 — `enforcement.mode`.** Stage 4.1. Ships last deliberately: it is the
only stage that can change a customer-visible outcome, and it should sit on a
contract already exercised by steps 1–3.

Every step is additive. No existing field changes shape or meaning, per
build-vs-reuse §1.2.

---

## 7. Open Questions

1. **Does an experiment's model swap survive a fallback?** If a variant routes to
   `gpt-4o-mini` and OpenAI is down, does the chain apply (leaving the request in
   the experiment on a different model) or does the request leave the experiment
   and get attributed to control? Attribution correctness argues for leaving the
   experiment and marking it `abandoned`; continuity argues the other way.
2. **Is `provider_override` a hard pin or a preference?** A hard pin plus an
   outage means failure; a preference means the global strategy can silently
   contradict an explicit tenant instruction.
3. **Do route rules need `time_window` before fallback?** It is designed and
   unbuilt; a maintenance-window rule is a common ask and would be cheaper to add
   while the matcher is being touched than afterwards.
4. **Should `workflow_type` be formally promoted?** The code already matches on
   it against a design that says MVP must reject it. Either the doc or the code
   is wrong, and the doc is easier to change.

---

## 8. Risks

- **Three matcher languages become one more, not one fewer.** This note does not
  unify the experiment cohort matcher with the route matcher; it only fixes
  precedence. Unifying them is the obvious follow-up and is deliberately out of
  scope, because a shared matcher is a breaking change to stored experiments.
- **Route-rule matching is barely exercised.** As of 2026-08-18 exactly one
  production tenant has a rule, it is match-all, and matching was proven via a
  direct resolver call rather than live traffic. Building steering and fallback
  on a path with one observed match is a real risk; the mitigation is to require
  a live end-to-end match with an event carrying `source=route_rule` before
  step 2.
- **Enforcement raises the cost of a matcher bug.** A rule that matches too
  broadly is invisible today and account-wide once actions apply. The strict
  matcher decoding and write-time validation added in aiqg-dashboard-be #123 are
  prerequisites, not niceties.

---

## 9. Related Documentation

- [[route-rule]] — matcher schema, resolution order, Phase-2 placeholders
- [[policy-bundle]] · [[policy-rule]] — what a bundle contains and what an action means
- [[experiment]] — cohort matcher, override axes, guardrails, lifecycle
- [[extraction-policy]] — the `reduction` block already carried on resolve
- `tas-llm-router` #2–#6 — model registry phases (source of `limits` and registry-driven fallback)
