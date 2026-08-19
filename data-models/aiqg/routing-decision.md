# AIQG Routing Decision — design & implementation plan

---

**Metadata**

```yaml
service: aiqg-dashboard-be + tas-llm-router
model: AIQGRoutingDecision
database: PostgreSQL (aiqg schema)
version: 1.0.0
last_updated: 2026-08-19
status: proposed (additive; no existing type, endpoint or column changes shape)
spec_refs: source-spec-v0.2.md §3.5.2 (route-attached policy / resolution order)
plan_ref: build-vs-reuse.md §2.5 (routes), §7.5 (MVP vs Phase-2 matchers)
supersedes: nothing — extends route-rule.md §2 resolve response
```

---

## 1. Summary

Today a route rule resolves to **a policy bundle**. This proposes it resolve to a
**routing decision** — the bundle *plus* where the request should go, what to do
when that fails, and which candidates are eligible at all — and fixes the order
in which routing, experiments and the router's global strategy compose.

Nothing here changes an existing field's shape or meaning. Every addition is
optional; absent means today's behaviour exactly.

### 1.1 What is changing

| # | Change | Why | Size |
|---|---|---|---|
| 1 | **One matcher language**, shared by route rules and experiment cohorts | `url_path` is a regex in one and a substring in the other — the same matcher selects different traffic | M |
| 2 | **`target`** — resolve returns a provider/model, wiring the dead `provider_override` column | Routing cannot steer execution at all today | S |
| 3 | **`health` + `budgets`** — per-target cooldown, ratio-based retry budget, 429 ≠ outage | A failing provider is retried at full rate on every request | M |
| 4 | **`fallback.chain` + `constraints`** — ordered failover tiers, compliance-constrained | Fallback is one global boolean; a tenant's forbidden vendor is unexpressible | M |
| 5 | **`affinity` + session `epoch`** | Switching providers silently destroys vendor prompt caches | M |
| 6 | **`selection`** — `expected_cost`, weights, `p2c_ewma` | Cost routing prices output at `max_tokens`, the ceiling | M |
| 7 | **`switching`** — hysteresis and dwell | Without it, an expected-cost router flaps and pays the cache re-warm twice | S |
| 8 | **`signals`** — CLEAR floors and gates as routing inputs | We measure quality and compliance outcomes and use them only for dashboards | L |
| 9 | **`limits`** — per-model context/output caps | Context windows are hardcoded in provider source | S |
| 10 | **`enforcement`** — Stage 4.1 | Bundles can display `block` while nothing blocks | L |

### 1.2 What is deliberately *not* changing

- **Composite CLEAR stays a reporting number, not a routing input.** Decided
  2026-08-19. Its weights are a presentation choice, coverage varies by model,
  and equal weighting trades a dollar against a quality point 1:1. Routing uses
  named dimensions with named thresholds (§5.8).
- **Token-based rate limiting** belongs with quotas and spend governance.
- **Quality *cascades*** (cheap model first, escalate on low confidence) change
  which answer the customer receives — that belongs behind the same gate as
  enforcement, not in a routing contract.
- **The experiment → policy-bundle override axis** stays unwired. A test must not
  be able to relax a compliance policy.

---

## 2. Why now

Three subsystems already answer *what should happen to this request*, with three
matcher languages and no defined precedence:

| system | matches on | decides | runs in |
|---|---|---|---|
| route rules | url_path, model, vendor, source_app, workflow_type, header | which policy bundle | `aiqg-dashboard-be` |
| experiments | url_path, source_app, model, workflow_type | model swap + params | `tas-llm-router`, at routing time |
| router strategy | *nothing* — global config | provider choice + fallback | `tas-llm-router` |

`model`, `source_app`, `workflow_type` and `url_path` each appear in two of the
three. Nothing states what happens when a route rule targets Anthropic, an
experiment swaps to a GPT variant, and cost-optimised strategy prefers a third.

**The cost of waiting is rework, not risk.** Enforcement (Stage 4.1), fallback
policy and the model registry are each about to be built against the current
bundle-only shape, and would then have to be reworked when it grows.

---

## 3. The evidence

Every figure below is measured on our own system. Method and queries in
Appendix C.

### 3.1 What exists today

| capability | state | evidence |
|---|---|---|
| Policy bundle resolution | **live** | `source=route_rule` proven for tenant `65ea3247` |
| Policy **enforcement** | **not built** | `pkg/aiqg/policy`: *"Phase 4.0 — observation-only"*; no `block`/`redact` handling anywhere |
| A/B model testing | **live**, separate surface | `experiments.NewResolver`, `server.go:513/545` |
| Provider fallback | **live**, global only | `internal/routing/router.go`; `fallback_enabled` is process-wide |
| Per-rule provider steering | **dead column** | `provider_override` stored, CRUD'd, validated — never returned, never read |
| Context windows | **hardcoded** | `anthropic/provider.go:88` = 200000; `openai/provider.go:88` = 128000 |
| Vendor prompt-cache control | **measurement only** | `cachePrefixHash` probe exists; #100 — router drops client `cache_control` |
| Model registry | **unbuilt** | tas-llm-router #2–#6 open |

### 3.2 Cost routing prices the wrong thing

`EstimateCost` in both providers estimates output as **`max_tokens` — the
ceiling** (`anthropic/provider.go:312`), defaulting to a literal 100 when unset.
Output is **90–99% of the bill**:

| model (single_turn_qa) | n | avg in | avg out | $/req | output share |
|---|---|---|---|---|---|
| `claude-opus-4-6` | 33 | 31.0 | **452.4** | 0.034395 | 99% |
| `claude-haiku-4-5` | 1258 | 32.8 | **61.6** | 0.000273 | 90% |
| `gpt-4o-mini` | 48 | 13.9 | **48.0** | 0.000031 | 93% |

Same workflow, near-identical input, a **7.3× verbosity spread**. Cost routing
optimises the 8% it can see and guesses the 92% that decides the bill.

### 3.3 Switching a provider throws away a paid-for cache

Anthropic prices a cache write at **1.25×** base input and a read at **0.10×**,
so abandoning a warm prefix costs `(1.25 − 0.10) × p_in × prefix_tokens` as a
one-off. At our measured 3,324-token RAG prefix that is **$0.00306**, against a
cached request costing **$0.000486**:

| per-request saving | further requests needed to break even |
|---|---|
| 5% | **126** |
| 10% | **63** |
| 25% | 25 |
| 50% | 13 |

The default TTL is **five minutes**. A 10% cheaper route must attract 63 more
requests from the same conversation inside five minutes before it is actually
cheaper.

### 3.4 CLEAR is measured but not yet usable for routing

| model | n | efficacy cov. | efficacy | assurance | reliability | composite |
|---|---|---|---|---|---|---|
| `claude-haiku-4-5` | 1864 | 88% | 70.2 | 92.6 | 100.0 | 91.3 |
| `gpt-4o-mini` | 114 | 96% | 87.9 | 100.0 | 100.0 | 97.6 |
| `claude-opus-4-6` | 33 | 100% | **100.0** | **100.0** | **100.0** | **67.0** |
| `gpt-4o` | 5 | 60% | 86.7 | 100.0 | 100.0 | 98.4 |

Three problems, all fixable. Composite renormalises over whichever dimensions
were present (`equal-weight-non-nil` on **100%** of 2,042 events; coverage
60–100% by model). Composite is dominated by cost — Opus scores 100 on all three
quality dimensions and lands at 67. And efficacy is contaminated by our own
synthetic traffic: it is finish-reason-only, and our probes truncated at
`max_tokens` 5 and 64, scoring 60.

---

## 4. Composition order

**Resolution decides · experiments overlay · the router executes · enforcement
applies.** Each stage may only narrow or replace what the previous produced, and
each stamps its identity on the event.

```
1. RESOLVE   (dashboard-be)  matcher → {bundle, target, selection, fallback,
                                        affinity, health, budgets, limits,
                                        constraints, signals}
2. OVERLAY   (gateway)       a running experiment may replace target.model / params
3. EXECUTE   (gateway)       affinity → selection → attempt → health/budgets → fallback.chain
4. ENFORCE   (gateway, 4.1)  bundle rules applied per enforcement.mode
```

| decision | set by | beaten by |
|---|---|---|
| policy bundle | explicit header → route rule → tenant active → default | nothing |
| provider/model target | route rule `provider_override` | a running experiment's `override.model` |
| affinity | route rule | `ttl` expiry or `on_break: allow_switch` |
| selection strategy | route rule, else global config | affinity |
| params | caller | experiment `override.params` |
| context/output caps | model registry, then `limits` | nothing |
| fallback chain | route rule | `constraints` — a denied vendor is removed |

Four rules follow, each preventing a specific failure:

- **An experiment may override `model`, and only `model`** — a test must not
  quietly relax a compliance policy.
- **Fallback never crosses a policy boundary** — chain entries are validated
  against `constraints` at write time, not discovered during an outage.
- **Affinity outranks selection** — a warm cache is worth more than a marginally
  cheaper provider (§3.3).
- **Global strategy is the floor** — no `target`, no `selection` means exactly
  today's behaviour.

---

## 5. What we are adding

### 5.1 One matcher, not two

Route rules and experiment cohorts share four field names with different
semantics — `url_path` is an **RE2 regex** in a rule and a **substring** in a
cohort. Two operators writing "the same" matcher select different traffic; they
agree on `/v1/chat` and diverge the moment anyone types `.` or `*`.

One schema, one evaluator, RE2 throughout: substring cannot express anchoring, so
a cohort keyed on `/v1/chat` also claims `/v1/chat_admin`, while regex can
express substring. Absent field = no constraint; lists OR within, AND across;
malformed regex **fails closed**; unknown fields **rejected**.

**Migration is a rewrite, not a dual-write.** Measured: 9 experiments (7
archived, 1 dry_run, 1 draft), 18 variants, **0 running**. Stored cohorts convert
in place — `url_path` substring → `regexp.QuoteMeta(substring)`, preserving
today's behaviour exactly rather than guessing intent. Archived experiments
migrate too; they are read for historical attribution. *If this lands after
experiments carry real traffic, this section is void and route-rule.md §10.3's
dual-write path applies.*

The evaluator must be a **shared library**: matching runs in dashboard-be at
resolve time and in the gateway at routing time, and a duplicated matcher is how
the two semantics diverged. `tas-llm-router` already consumes
`aether-shared/go-events` and `Gatekeeper` as siblings.

### 5.2 `target` — resolution steers execution

`provider_override` already exists in schema, store, API and UI, and is now
validated. It is never returned by the resolver and never read by the gateway.
Returning it as `target.provider` converts a dead column into proof that the
contract works, using one field and no new storage.

`target` is a struct rather than a string because a decision must record *which
rung decided* — an explicit header, a route rule and global strategy carry
different audit meanings, exactly as `resolved_policy_bundle_source` already does.

### 5.3 `health` + `budgets` — the resilience primitives

```jsonc
"health": {
  "eject_after":     {"consecutive_errors": 5, "error_rate_pct": 50},
  "ejection_window": "30s",
  "half_open_after": "60s",
  "treat_429_as":    "backoff"     // backoff | unhealthy
},
"budgets": {
  "retry_ratio_pct": 20,           // share of active requests, not a per-request count
  "max_attempts":    2,
  "hedge_after_ms":  null          // null = off; a hedge DOUBLES cost
}
```

- **`treat_429_as: backoff` by default** — a throttled provider is healthy, just
  not for us right now, and `retry-after` says exactly when. Ejecting on 429
  discards capacity that returns in seconds.
- **Retry budget as a ratio** — fixed per-request retries are what turn a partial
  outage into a full one, and here the blast radius is billed.
- **Hedging off by default** — it halves tail latency, doubles cost, and
  guarantees a prompt-cache miss. Defensible for a short interactive call,
  indefensible for a 3,324-token RAG call.

### 5.4 `fallback.chain` + `constraints`

Ordered tiers rather than a boolean, with every entry validated against the
tenant's compliance constraints at write time.

```jsonc
"fallback":    { "chain": [{"provider":"anthropic","model":"claude-haiku-4-5-20251001"},
                           {"provider":"openai","model":"gpt-4o-mini"}],
                 "on": ["vendor_error","timeout","context_overflow"] },
"constraints": { "deny_vendors": ["…"], "require_zdr": false }
```

`constraints` is compliance **as declared**; §5.8's assurance gate is compliance
**as observed**. Together they make "fallback never crosses a policy boundary"
enforceable rather than aspirational.

### 5.5 `affinity` — three needs, one field

"Session stickiness" covers three unrelated things:

| need | key | what breaks | cost of breaking |
|---|---|---|---|
| **prompt-cache affinity** | prefix hash | the vendor's cached prefix | §3.3 — up to 57× TTFT on self-hosted analogues |
| **assignment affinity** | conversation/user/flow | the experiment bucket | already solved by the experiment runner |
| **conversation coherence** | conversation | the assistant's voice mid-thread | a product bug, not a metric |

```jsonc
"affinity": { "key_source":"conversation", "scope":"vendor+model",
              "ttl":"5m", "on_break":"prefer_same" }
```

`ttl` defaults to 5m to match Anthropic's cache lifetime — affinity should expire
when the thing it protects expires. `key_source` reuses the experiment runner's
existing enum rather than inventing a second vocabulary.

This pairs with **#100** (`cache_control` pass-through). Neither is worth much
alone: affinity keeps requests on a provider whose cache we currently cannot
populate.

### 5.6 Session `epoch` — when a session really changed

Long sessions wander across topics, but the three senses of "changed" have three
different answers, and only one involves topics:

| question | signal | topic drift relevant? |
|---|---|---|
| Is the vendor cache still warm? | **TTL + stable-prefix change** | **No** |
| Is our semantic cache entry valid? | per-request similarity + L2 guards | n/a |
| Is it safe to switch models? | **topic drift** | **Yes — as a seam** |

A three-hour session across eight topics keeps its vendor cache the whole time,
provided the system prompt and tool set are stable and requests stay inside the
TTL: the cache is keyed on **prefix bytes**, not meaning.

```
epoch = (stable_prefix_hash, idle_bucket)
key   = (conversation_id, epoch)
```

The epoch increments when the stable prefix changes (the cache is cold anyway) or
the idle gap exceeds `affinity.ttl` (the cache has expired). No embeddings;
`cachePrefixHash` already computes half of it.

Topic drift earns its place by inverting the usual framing — a model switch is
jarring mid-topic and nearly invisible at a boundary, so drift **schedules a
deferred switch** rather than invalidating anything. **Anti-pattern:** do not use
drift to invalidate prompt-cache affinity; it discards warm caches while leaving
genuinely stale pins in place.

### 5.7 `selection` + `switching` — expected cost, with hysteresis

```jsonc
"selection": { "strategy": "expected_cost",     // cost | expected_cost | latency | p2c_ewma | weighted | pinned
               "weights": {"anthropic": 90, "openai": 10} },
"switching": { "min_improvement_pct": 25, "dwell": "60s", "warm_cache_bias_pct": 15 }
```

`expected_cost` prices a candidate as
`in_tokens × p_in + E[out_tokens | model, workflow] × p_out`, with the
expectation **measured from our own events** and **abstaining** below a sample
floor.

> **The verbosity budget rule.** A model's verbosity budget *is* its output-price
> ratio: B beats A while `out_B/out_A < p_out_A/p_out_B`.

`gpt-4o-mini` may be **6.7×** more verbose than Haiku and still win; it is 0.78×,
and would need **451** output tokens — 9.4× its current length — to break even. A
model with a 10% price advantage has 10% of headroom.

`switching` ships **in the same step**: an expected-cost router without hysteresis
is precisely the machine §3.3 describes, and flapping pays the re-warm twice.

Two guards are not optional. **Verbosity is not quality** — unguarded, this
optimises for terseness, so it pairs with §5.8's efficacy floor. And `max_tokens`
stays in the estimate as a **cap**, because a truncated answer is what you
actually pay for.

*Canary is load balancing with a different intent*: a 90/10 weight split and a
canary rollout are the same mechanism, differing only in who watches the result —
the experiment runner's existing job.

### 5.8 `signals` — CLEAR as a routing input

```jsonc
"signals": { "min_efficacy": 70, "max_assurance_severity": "medium",
             "min_samples": 200, "max_staleness": "24h",
             "exclude_synthetic": true, "on_insufficient_data": "ignore" }
```

Four rules make this safe:

1. **Dimensions, never composite** (§1.2) — so a dashboard weighting change
   cannot silently re-route production traffic.
2. **Two loops at two timescales.** Fast signals (5xx, timeout, 429) decide
   ejection sub-second; slow CLEAR aggregates decide *eligibility* over
   minutes–days. Post-hoc scoring cannot eject; 5xx counting cannot judge quality.
3. **Floors and gates, not an optimisation target.** CLEAR filters the candidate
   set; cost and latency choose among survivors. Lexicographic, so there is no
   1:1 dollar-versus-quality trade, and it is a no-op when data is thin.
4. **Exclude synthetic traffic** — demo and experiment traffic must not score a
   vendor, which makes demo-flow attribution on events a prerequisite (§7).

**Assurance first.** It is already bucketed on *worst* severity rather than count
— the code's reasoning is that "one unauthorized disclosure invalidates otherwise
perfect performance" — so it is already gate-shaped, and it complements
`constraints` exactly.

### 5.9 Cache keys — three caches, three keys

| cache | key today | TTL | store |
|---|---|---|---|
| C1 exact | sha256(tenant, vendor, model, messages, temperature, seed…) | 10m | `redis-shared` |
| C4 semantic | embedding(`lastUserText`) + scope(tenant, model, scoring_version) | 30m | `redis-semcache` |
| vendor prompt cache | not ours — the vendor hashes our prefix bytes | 5m / 1h | vendor |

Six rules, of which two carry the weight:

- **Cross-model reuse is an opt-in second lookup, not a key change.** Dropping
  `model` from the key silently serves an Opus answer to a Haiku request,
  corrupting attribution and making CLEAR scores incomparable. If wanted, it is a
  separate lookup stamped `cache_state=cross_model_hit`.
- **Reduction must be deterministic**, or it fragments C1 *and* the vendor prefix
  at once. Currently true — reduction is query-anchored — but the invariant is
  implicit and load-bearing for two caches, so it should be stated and tested.

The rest: normalise before hashing (key order, whitespace, string-vs-array
content fragment silently); exclude routing-only fields so retries are not
misses; key the prefix on the **stable span only**, never the trailing user turn.

---

## 6. The contract

```jsonc
// POST /internal/policy/resolve → 200
{
  // ---- existing, unchanged ----
  "bundle_id":   "6da56444-…",
  "bundle_name": "Compliance",
  "source":      "route_rule",       // explicit | route_rule | tenant_active | default
  "reduction":   { /* extraction policy */ },

  // ---- new, all optional; absent = today's behaviour ----
  "target":      { "provider":"anthropic", "model":null, "source":"route_rule" },
  "selection":   { "strategy":"expected_cost", "weights":null },
  "switching":   { "min_improvement_pct":25, "dwell":"60s", "warm_cache_bias_pct":15 },
  "fallback":    { "chain":[{"provider":"anthropic","model":"claude-haiku-4-5-20251001"},
                            {"provider":"openai","model":"gpt-4o-mini"}],
                   "on":["vendor_error","timeout","context_overflow"] },
  "affinity":    { "key_source":"conversation", "scope":"vendor+model",
                   "ttl":"5m", "on_break":"prefer_same" },
  "health":      { "eject_after":{"consecutive_errors":5}, "ejection_window":"30s",
                   "half_open_after":"60s", "treat_429_as":"backoff" },
  "budgets":     { "retry_ratio_pct":20, "max_attempts":2, "hedge_after_ms":null },
  "signals":     { "min_efficacy":70, "max_assurance_severity":"medium",
                   "min_samples":200, "max_staleness":"24h",
                   "exclude_synthetic":true, "on_insufficient_data":"ignore" },
  "limits":      { "max_context_window":200000, "max_output_tokens":4096 },
  "constraints": { "deny_vendors":["…"], "require_zdr":false },
  "cache":       { "cross_model_reuse":false },
  "enforcement": { "mode":"observe" }   // Stage 4.1; absent = observe-only
}
```

### 6.1 Validation

1. `target.provider` must name a configured, enabled provider for the tenant.
2. Every `fallback.chain` entry must be a valid `(provider, model)` pair and
   satisfy `constraints` — rejected at write time.
3. A chain may not begin with the target (that is a retry) or repeat an entry.
4. `limits.max_context_window` may only **lower** a provider's advertised window;
   raising it turns a clean pre-flight rejection into a vendor error.
5. `affinity.ttl` may not exceed the vendor's cache lifetime.
6. `budgets.hedge_after_ms` requires `max_attempts ≥ 2`, and is rejected when
   `affinity.scope` is set — a hedge to another vendor is a guaranteed cache miss.
7. `selection.strategy = expected_cost` requires a verbosity factor above the
   sample floor; below it, fall back to the `max_tokens` estimate and say so on
   the event.
8. `switching.min_improvement_pct` may not be 0 while `affinity` is set.
9. `cache.cross_model_reuse = true` requires the tenant to acknowledge that CLEAR
   scores become incomparable across reused entries.
10. `enforcement.mode = enforce` requires at least one bundle rule with a
    non-`log` action, so "enforcing" never silently means "doing nothing".

---

## 7. Implementation plan

Nine steps, each independently shippable and additive. Every step lists work in
all three repositories plus the UI, because several of these features are
unusable without an editor — a fallback chain nobody can author is not a feature.

**Cross-cutting UI decision.** `RoutingManager` today is a table with an inline
create row, adequate for *matcher → bundle*. Steps 1–6 add six more concerns
(target, resilience, fallback, affinity, selection, signals). A single inline row
cannot carry that. **Recommendation: keep the table as the list view and add a
full-height drawer editor with sections**, introduced at step 1 and extended
thereafter, so each step adds a section rather than redesigning the surface.

---

### Step 0 — Unify the matcher

| Repo | Work |
|---|---|
| **aether-shared** | New shared Go module `go-aiqg-matcher`: matcher struct, strict parse, evaluate, canonicalise. Single source of truth. |
| **aiqg-dashboard-be** | Replace `internal/handlers/route_match.go` with the shared library; keep write-time validation. One-shot idempotent migration rewriting 9 stored cohorts (`url_path` substring → `regexp.QuoteMeta`). |
| **tas-llm-router** | Replace the cohort matcher in `pkg/aiqg/experiments` with the shared library. |
| **aiqg-ui** | Extract `MatcherEditor` from `RoutingManager`; reuse it in the Experiments cohort editor so both surfaces offer the same fields and validation. Add inline regex validation and a **“test matcher”** affordance that shows which recent requests a matcher would select. |

**Acceptance** — one evaluator; 9 cohorts rewritten; second implementation
deleted; a replayed traffic sample selects identically pre/post.

**Risk** — the migration is one-way. Mitigated by 0 running experiments and a
pre-migration export.

---

### Step 1 — `target` end to end

| Repo | Work |
|---|---|
| **aiqg-dashboard-be** | `bundleResolveResponse` returns `target` from `route_rule.provider_override`; record which rung set it. |
| **tas-llm-router** | Read `target`; pin provider/model before selection; stamp `target_source` on the event. |
| **Data** | Two event fields → Timescale columns. |
| **aiqg-ui** | **New drawer editor** (see above), section 1 *Matcher*, section 2 *Route to* — provider select, optional model, and an explicit “no override” state. `matcherSummary` gains the target. Live Monitor and Traffic Explorer show which rung chose the provider. |

**Acceptance** — a live request carries `source=route_rule` **and** lands on the
rule’s provider. **Gate:** this must be demonstrated on live traffic before
step 2 (§10).

---

### Step 2 — `health` + `budgets`

| Repo | Work |
|---|---|
| **tas-llm-router** | Per-target circuit state in Redis so replicas share it; ejection on consecutive errors or error-rate; half-open single-probe restore; retry budget accounted as a ratio of live requests; `429` → backoff honouring `retry-after`, no ejection. |
| **aiqg-dashboard-be** | `health` + `budgets` in resolve; CRUD and validation on the route rule. |
| **aiqg-ui** | Drawer section 3 *Resilience*: eject-after, window, half-open delay, 429 policy, retry ratio, max attempts — each with a plain-language explanation of the failure it prevents. **New Provider Health panel on Live Monitor**: per-provider state (healthy / ejected / half-open), last transition, and reason. Operationally this is the most important new surface in the whole plan — an ejection nobody can see is indistinguishable from an outage. |

**Acceptance** — induced 5xx ejects within the window and restores half-open; a
429 backs off without ejection; retries never exceed the ratio under load.

---

### Step 3 — `fallback.chain` + `constraints`

| Repo | Work |
|---|---|
| **aiqg-dashboard-be** | `chain` JSONB on route rule; tenant-level `constraints`; write-time validation of every chain entry against constraints and the provider catalogue. |
| **tas-llm-router** | Walk the chain on eligible failures only; stamp chain position and the reason it advanced. |
| **aiqg-ui** | Drawer section 4 *Fallback*: ordered chain builder — add, remove, drag-reorder, provider+model pickers, inline validation showing *why* an entry is rejected. Tenant **Constraints editor** (deny vendors, require zero-retention) in Governance. Errors page gains a fallback breakdown: how often, to what, and why. |

**Acceptance** — the chain walks in order on induced failure; a denied vendor is
rejected at write time, not discovered at failover.

---

### Step 4 — `affinity` + `epoch` + cache-control pass-through

| Repo | Work |
|---|---|
| **tas-llm-router** | Affinity store in Redis keyed `(tenant, conversation, epoch)` → provider/model; epoch from `cachePrefixHash` + idle bucket; **pass client `cache_control` through** (closes #100). |
| **aiqg-dashboard-be** | `affinity` in resolve. |
| **aiqg-ui** | Drawer section 5 *Affinity*: key source, scope, TTL, on-break behaviour. Caching settings page gains a **Provider affinity** card. Traffic Explorer shows, per request, whether affinity held, and when an epoch incremented and why (prefix change vs idle expiry). |

**Acceptance** — vendor cache-read share measurably rises on a repeated-prefix
flow; epoch increments on prefix change and on idle beyond TTL.

---

### Step 5 — `selection` + `switching`

| Repo | Work |
|---|---|
| **aiqg-dashboard-be** | Scheduled job building the verbosity table per `(model, workflow)` from events, with sample floor, decay window and staleness marking; served on resolve. |
| **tas-llm-router** | `expected_cost` strategy; `weighted` for canary; hysteresis — minimum improvement, dwell timer, warm-cache handicap. |
| **aiqg-ui** | Drawer section 6 *Selection*: strategy picker with an explanation of each, weights editor for canary, switching thresholds. **New “Model economics” report** — measured verbosity per model × workflow, cost per request, and the verbosity budget against actual, so the routing decision is legible rather than a black box. Staleness and low-sample states shown explicitly. |

**Acceptance** — expected-cost differs from list-price ranking on a known case;
no flapping under a synthetic price oscillation; abstention below the sample
floor is visible in the UI.

---

### Step 6 — `signals` (CLEAR-gated routing)

| Repo | Work |
|---|---|
| **Prerequisite** | Demo-flow / synthetic attribution stamped on events, so test traffic can be excluded. |
| **aiqg-dashboard-be** | Aggregate CLEAR per `(model, workflow, tenant)` excluding synthetic; staleness; serve as `signals`. |
| **tas-llm-router** | Filter the candidate set by floors *before* selection; stamp which candidates were filtered and on which dimension. |
| **aiqg-ui** | Drawer section 7 *Quality gates*: minimum efficacy, maximum assurance severity, sample floor, staleness, behaviour on insufficient data. **CLEAR Scorecard gains a “routing eligibility” view** — which models currently pass or fail this tenant’s floors, on what evidence, with sample counts. Traffic Explorer shows “candidate excluded by signals” as a first-class outcome. |

**Acceptance** — the assurance gate drops a candidate on evidence; synthetic
traffic is excluded from aggregates; thin data is a no-op.

---

### Step 7 — `limits`

Depends on the model registry (tas-llm-router #2–#6).

**UI** — registry admin view: models with status (active / deprecated /
unavailable), context windows, pricing and last sync; per-route limit overrides
that may only lower a provider’s advertised window.

---

### Step 8 — `enforcement` (Stage 4.1)

| Repo | Work |
|---|---|
| **tas-llm-router** | Apply rule actions at decision points B and C; fail-open/closed per configuration; stamp the enforcement outcome and the mode that applied. |
| **aiqg-dashboard-be** | `enforcement.mode` in resolve; validation that an enforcing bundle has at least one non-`log` rule. |
| **aiqg-ui** | **Enforcement toggle per bundle with a dry-run diff** — *“if this bundle had been enforcing over the last 7 days, N requests would have been blocked and M redacted; here they are.”* This is the single most important UI in the plan: it converts enforcement from an act of faith into a reviewable decision. Rule editor shows per-rule impact counts. Security report separates *would-block* from *blocked*. |

**Acceptance** — a `block` rule blocks; an enforcing bundle containing only `log`
rules is rejected; the dry-run diff matches actual enforcement when enabled.

---

### Step 9 — AIR Ops suggestion stage

| Repo | Work |
|---|---|
| **aiqg-dashboard-be** | Suggestion engine evaluating computable conditions over existing events (cost, quality, policy classes per §10.4); suggestion store with evidence and provenance. |
| **aiqg-ui** | **New “Recommendations” surface** — an inbox of suggestions, each showing the condition, the evidence, and the affected traffic, with two actions: *run as experiment* (creates a dry-run experiment pre-filled from the suggestion) and *draft policy* (opens the bundle editor pre-filled). AI-generated explanations render as drafts with provenance shown. Nothing applies without review. |

**Acceptance** — a detected condition produces a suggestion carrying its
evidence; accepting one creates a `dry_run` experiment; no suggestion can
activate a change without human approval.

---

### Sequencing rationale

- **Health before chains** — a chain without cooldown fails faster in a loop.
- **`switching` with `selection`** — an expected-cost router without hysteresis is
  the flapping cost machine of §3.3.
- **Synthetic attribution before signals** — scoring a vendor on our own test
  harness is worse than not scoring it.
- **Enforcement late** — it is the only step that changes a customer-visible
  outcome, and its dry-run diff depends on measurement already being trusted.
- **Suggestions last** — a suggestion engine is only as good as the signals
  beneath it.

### UI work summary

| Surface | Change | Step |
|---|---|---|
| `MatcherEditor` (new, shared) | extracted, reused by routing + experiments | 0 |
| Route rule **drawer editor** (new) | 7 sections added progressively | 1–6 |
| Live Monitor | **Provider Health panel** | 2 |
| Governance | tenant **Constraints editor** | 3 |
| Errors report | fallback breakdown | 3 |
| Caching settings | provider affinity card | 4 |
| Traffic Explorer | affinity state, epoch changes, signal exclusions | 4, 6 |
| Reports | **Model economics** (new) | 5 |
| CLEAR Scorecard | **routing eligibility** view | 6 |
| Registry admin (new) | models, status, windows, pricing | 7 |
| Policies | **enforcement dry-run diff** | 8 |
| **Recommendations** (new) | suggestion inbox → experiment or policy draft | 9 |

## 8. Decisions taken

| decision | date | rationale |
|---|---|---|
| Composite CLEAR is **not** a routing input | 2026-08-19 | Weights are a presentation choice; coverage varies 60–100% by model; equal weighting trades a dollar against a quality point |
| Matcher unification is a **rewrite**, not dual-write | 2026-08-19 | 0 of 9 experiments running; the window closes when experiments carry real traffic |
| RE2 regex over substring | 2026-08-19 | Substring cannot express anchoring; regex can express substring |
| Hedging **off** by default | 2026-08-19 | Doubles cost, guarantees a cache miss |
| 429 is **backoff**, not unhealthy | 2026-08-19 | A throttled provider is healthy; `retry-after` says when |
| Experiments may override **`model` only** | 2026-08-19 | A test must not relax a compliance policy |

---

## 9. Open questions

1. **Does an experiment's model swap survive a fallback?** A fallback that leaves
   the experiment also breaks affinity, so the answer is probably "leave, and
   record both".
2. **Is `provider_override` a hard pin or a preference?** Suggested: a pin, with
   `fallback.chain` the only sanctioned escape — configuration rather than policy.
3. **Whose affinity key wins** when a tenant sets `key_source: conversation` and
   the caller supplies `prompt_cache_key`? Proposed: the caller's.
4. **Does `time_window` belong in this pass**, while the matcher is open?
5. **Is `workflow_type`'s doc wrong, or its code?** The design says MVP must
   reject it; the implementation matches on it.
6. **Where does the verbosity table live** — dashboard-be (gateway stays
   stateless) or the gateway (fresher, but two services disagree about price)?
7. **Does `switching.dwell` apply across a fallback?** A failover is not a price
   decision, so probably not — otherwise an outage pins you to a degraded provider.

---

## 10. Risks

- **Route-rule matching is barely exercised** — one tenant, one match-all rule,
  proven via a direct resolver call. Gated at step 2.
- **Enforcement raises the cost of a matcher bug** from invisible to
  account-wide. Strict decoding and write-time validation (aiqg-dashboard-be
  #123) are prerequisites.
- **Affinity and cost-optimisation are in genuine tension.** The contract makes
  the trade explicit rather than choosing globally — but a tenant can configure a
  combination worse than either alone, and the UI must say so.
- **Expected-cost routing optimises for brevity if unguarded** — a terser model
  may simply be answering less.
- **Verbosity is workload-specific and drifts.** A factor measured on
  `single_turn_qa` says nothing about `rag`, and a prompt change moves it
  overnight. Needs a decay window and a staleness alarm.
- **Routing on CLEAR creates a one-way door unless exploration is explicit.** A
  model routed away from stops being measured and can never recover. The
  experiment runner must be the designated explorer, and its traffic excluded
  from the aggregates it feeds.
- **This adds surface to a component that is already three systems.** Mitigated
  by every field being optional; the residual risk is that "optional" becomes
  "undocumented default nobody understands".

---

## Appendix A — Prior art

Surveyed 2026-08-19: single-pass web research against primary docs. **Not**
3-vote verified to the standard of `AIQG_COMPETITIVE_LANDSCAPE.md`.

| product | selection | failure handling | affinity |
|---|---|---|---|
| **LiteLLM** | shuffle, least-busy, usage, latency, cost — per model group | per-deployment **cooldown**; in-group weighted failover, then cross-group `fallbacks`; `order` tiers | — |
| **Portkey** | weighted `loadbalance`; `conditional` on metadata | fallback, retries, circuit breakers, timeouts | — |
| **OpenRouter** | sort by price/throughput/latency; inverse-square price weighting | deprioritises a provider with an outage in the last 30s | — |
| **Kong AI Gateway** | provider + semantic routing | gateway retry/breaker | — |
| **Cloudflare AI Gateway** | weighted LB | fallback across providers *and API keys* | — |
| **Envoy AI Gateway** | prioritised `backendRefs` | primary → next healthy fallback | — |
| **Bifrost** | priority list | per-provider retry budget, then next in chain | — |
| **Vercel AI Gateway** | per-request provider priority list | retries + fallback | — |
| **llm-d / vLLM stack** | scores replicas by **prefix-match length** vs load | k8s health | **prefix-cache-aware** |
| **RouteLLM / Not Diamond / Martian** | learned router on *predicted* quality per query | escalation cascade | — |

Four convergences we lack: per-target cooldown, ordered failover tiers,
token-based rate limiting, and compliance as a routing input.

## Appendix B — The load-balancer canon

| mechanism | applies? |
|---|---|
| Passive health / outlier detection | ✅ directly — the missing cooldown |
| Circuit breaking, retry budgets | ✅ especially the retry cap; this storm is billed |
| Priority levels / locality failover | ✅ the fallback chain, properly modelled |
| P2C + Peak EWMA | ✅ routes on observed, not configured, performance |
| Ring hash / Maglev | ✅ the affinity primitive |
| Slow start | ↻ reinterpreted: a cold *prompt cache* |
| Connection draining | ↻ reinterpreted: *conversation* draining |
| Active health checks | ~ weak — says nothing about *your* key being throttled |
| Session persistence | ✅ but for entirely different reasons (§5.5) |

**Where the analogy breaks:** the unit of work is not a connection (32 vs 3,324
input tokens); **429 is not 5xx**; failure is slow *and billed*, which raises the
value of retry budgets; and targets are stateful in a way HTTP backends are
deliberately not.

## Appendix C — Measurements and method

All figures from our own systems on 2026-08-18/19.

| figure | source |
|---|---|
| Token profiles, verbosity by model | `aiqg.event_metrics` by model/workflow, `token_accounting` |
| CLEAR coverage and means | `aiqg.event_metrics` → `clear_scores`, n = 2,042 scored events |
| Cache write/read multipliers | Anthropic prompt-caching pricing (1.25× write, 0.10× read) |
| Prefix size 3,324 tokens | measured mean input for the `rag` workflow |
| Experiment estate (9 / 18 / 0) | `aiqg.experiment`, `aiqg.experiment_variant` |
| Prompt-cache routing gains | published benchmarks on self-hosted vLLM replicas (external) |

**Known contamination:** efficacy figures include this session's verification
traffic, which truncated at `max_tokens` 5 and 64 and therefore scored 60. Any
production use of efficacy must exclude synthetic traffic first.

## Appendix D — Related documentation

- [[route-rule]] · [[policy-bundle]] · [[policy-rule]] · [[experiment]] · [[extraction-policy]]
- `tas-llm-router` #2–#6 (model registry) · #100 (`cache_control` dropped)
- `AIQG_COMPETITIVE_LANDSCAPE.md` — scoring and inline-security competitive set
- `AIQG_ROUTING_COMPETITIVE.md` — routing-specific competitive analysis
- `AIQG_ROUTING_CAPABILITIES.md` — outward-facing capability overview
