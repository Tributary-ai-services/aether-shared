# AIQG Routing Decision — unified resolve contract

---

**Metadata**

```yaml
service: aiqg-dashboard-be + tas-llm-router
model: AIQGRoutingDecision
database: PostgreSQL (aiqg schema)
version: 0.2.0
last_updated: 2026-08-19
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
contract.

This note proposes one resolve contract that returns a **routing decision**
rather than a policy bundle, fixes the order in which the three systems compose,
collapses the two matcher languages into one (§5), and — new in v0.2 — adopts the
parts of the load-balancer canon that survive contact with LLM traffic (§3–§4),
including a first-class treatment of **affinity** (§6).

It is a contract change, not a feature: the point is to settle the seam before
enforcement (Stage 4.1), fallback policy and the model registry are each built
against the current bundle-only shape and then have to be reworked.

### 1.1 What already exists (audited 2026-08-18)

| capability | state | evidence |
|---|---|---|
| Policy bundle resolution | **live** | `source=route_rule` proven for tenant `65ea3247`; precedence explicit→route_rule→tenant_active→default |
| Policy **enforcement** | **not built** | `pkg/aiqg/policy`: *"Phase 4.0 — observation-only … takes no enforcement action yet"*; no `block`/`redact` handling anywhere |
| A/B model testing | **live, separate surface** | `experiments.NewResolver` wired at `server.go:513/545`; override axes `model` + params |
| Provider fallback | **live, global only** | `internal/routing/router.go` sets `FallbackUsed`; `fallback_enabled` is process-wide config |
| Per-rule provider steering | **dead column** | `route_rule.provider_override` stored, CRUD'd, validated — never returned, never read |
| Context-window / vendor config | **hardcoded** | `anthropic/provider.go:88` = 200000, `openai/provider.go:88` = 128000 |
| Model registry | **unbuilt** | tas-llm-router #2–#6 all open; #5 *is* registry-driven fallback |
| Vendor prompt-cache control | **measurement only** | `cachePrefixHash` probe exists; #100 — *"router drops client `cache_control`; vendor prompt caching unreachable"* |
| `time_window` matcher | **unbuilt** | designed in route-rule.md, absent from code |
| `workflow_type` matcher | **drift** | design says MVP must *reject* it; implementation accepts and matches on it |

Two documentation drifts to correct: route-rule.md §10.2 places matcher
evaluation in `tas-llm-router/internal/policy/resolver.go` (it is in
`aiqg-dashboard-be/internal/handlers/route_match.go`), and §10.4 describes Neo4j
migrations (route rules are PostgreSQL).

---

## 2. Prior art — how other LLM routers model this

Surveyed 2026-08-19. The point is not feature parity; it is to find where the
field has converged, because convergence usually marks a real constraint.

| product | selection | failure handling | affinity | notable |
|---|---|---|---|---|
| **LiteLLM** | `simple-shuffle`, `least-busy`, `usage-based`, `latency-based`, `cost-based`; per-model-group strategy | per-deployment **cooldown** after `allowed_fails`; retry within the group (weighted failover) before escalating to cross-group `fallbacks`; `order` tiers 1..n | — | Cooldown isolates one deployment, not the group — healthy peers keep serving |
| **Portkey** | `loadbalance` with weights; `conditional` routing on metadata/params | `fallback` strategy, automatic retries, **circuit breakers**, request timeouts | — | **Canary testing is load balancing with a different intent**; semantic cache ~0.95 threshold, claims 30–50% hit rates |
| **OpenRouter** | `provider.sort` = price \| throughput \| latency; default weights by **inverse square of price** | **deprioritises any provider with an outage in the last 30s**; `allow_fallbacks`, `order`, `only`, `ignore` | — | Routing constrained by **data policy** (`data_collection`, ZDR) — compliance as a routing input |
| **Kong AI Gateway** | provider routing, **semantic routing** | standard gateway retry/breaker | — | **Token-based** rate limiting, not request-based |
| **Cloudflare AI Gateway** | weighted load balancing | automatic fallback across providers *and API keys* | — | Per-consumer **token** limits; SaaS-only — fails data-residency needs |
| **llm-d / vLLM production stack / Gateway API Inference Extension** | scheduler scores replicas by **prefix-match length** against load | standard k8s health | **prefix-cache-aware, KV-event-aware** | The only tier that treats the *cache* as the thing being balanced |
| **RouteLLM** (research) | learned router: weak vs strong model per query | escalation cascade | — | ~85% cost cut at ~95% of GPT-4 quality, 14% of queries to the strong model |

Four things the field has converged on that we do not have:

1. **Per-deployment cooldown / outlier ejection.** Everyone isolates a failing
   target rather than failing the whole group. We have neither — a provider that
   starts erroring is retried at full rate on every subsequent request.
2. **Ordered failover tiers, not a single fallback flag.** LiteLLM `order`,
   OpenRouter `order`, Envoy priority levels. Ours is a boolean.
3. **Token-based rate limiting.** Kong and Cloudflare both meter tokens, because
   a request is not a unit of load in this domain.
4. **Compliance as a routing input.** OpenRouter routes on data policy. This is
   the one where we are *ahead* conceptually — our bundles already carry
   compliance intent — but behind in wiring, because the resolver cannot express
   "never this vendor for this tenant" as a routing constraint.

---

## 3. The load-balancer canon

What twenty-five years of L4/L7 balancing already solved, and whether it applies.

| mechanism | what it does | applies to LLM routing? |
|---|---|---|
| **Active health checks** | periodic synthetic probe | ⚠️ weakly — a synthetic call says nothing about whether *your key* is throttled |
| **Passive health / outlier detection** | eject on consecutive 5xx, consecutive gateway failures, or **success-rate deviation from peers** | ✅ **directly** — this is the missing cooldown |
| **Circuit breaking** | cap concurrent connections, pending requests, **retries** | ✅ especially the retry cap |
| **Retry budgets** | retries capped as a *ratio* of active requests rather than a fixed count per request | ✅ **critical** — an LLM retry storm costs money, not just capacity |
| **Priority levels / locality failover** | tiered target sets; spill to the next tier only when the current one is unhealthy | ✅ this is the fallback chain, properly modelled |
| **P2C + Peak EWMA** | pick 2 at random, choose the better by latency EWMA weighted by outstanding requests; O(1), re-aggregated ~100ms | ✅ routes on *observed* rather than *configured* performance |
| **Ring hash / Maglev** | consistent hashing so membership change reshuffles minimally | ✅ the affinity primitive (§6) |
| **Slow start / warm-up** | ramp traffic to a newly healthy target | ⚠️ reinterpreted: a **cold prompt cache** is the LLM analogue of a cold target |
| **Connection draining** | finish in-flight work before removing a target | ⚠️ reinterpreted as **conversation draining** — §6.3 |
| **Session persistence** | cookie / source-IP / consistent-hash affinity | ✅ but for entirely different reasons — §6 |

### 3.1 Where the analogy breaks

Copying the canon wholesale would be wrong. Four differences matter:

1. **The unit of work is not a connection.** Requests differ in cost by two
   orders of magnitude (32 input tokens vs 3,324 — measured, §11). Least-*request*
   balancing assumes requests are fungible. Kong and Cloudflare metering tokens is
   the field's acknowledgement of this.
2. **Health is multi-dimensional, and 429 is not 5xx.** A rate-limited provider is
   perfectly healthy — just not for us, right now, and typically per-key rather
   than globally. Outlier detection keyed on 5xx alone would miss the most common
   real failure. `retry-after` is a *scheduling* signal, not a health one.
3. **Failure is slow and expensive.** A failed LLM call burns seconds and real
   money, which inverts the usual retry calculus and makes **retry budgets** and
   **hedging** more consequential here, not less.
4. **Targets are stateful in a way HTTP backends are not.** A provider holds a
   prompt/KV cache keyed to your prefix. Web backends are deliberately stateless
   so any target will do; here, *which* target you pick changes cost and latency
   by multiples. That is the crux of §6.

---

## 4. Selection strategy

Today: one global strategy (`cost-optimized` | `round-robin` | `performance`)
chosen at process start. Proposed: strategy is part of the decision, so a tenant
or a route can differ, with `weights` for canary and progressive rollout.

```jsonc
"selection": {
  "strategy": "cost",                            // cost | latency | p2c_ewma | weighted | pinned
  "weights":  {"anthropic": 90, "openai": 10}    // weighted only — canary lives here
}
```

Adopting Portkey's observation explicitly: **canary is load balancing with a
different intent.** A 90/10 weight split and a canary rollout are the same
mechanism, so they should not be two features. What differs is who watches the
result — the experiment runner's job, which already exists.

`p2c_ewma` is proposed as the eventual default over `performance`, because it
routes on observed behaviour with O(1) selection and moves traffic off a
degrading provider before it fails outright.

---

## 5. One matcher, not two

Route rules and experiment cohorts match on overlapping fields with **different
semantics for the same key**:

| field | route rule | experiment cohort |
|---|---|---|
| `url_path` | **RE2 regex** | **substring** |
| `model`, `source_app`, `workflow_type` | list, OR-within, case-insensitive | list, OR-within |
| `vendor` | supported | absent |
| `customer_header_match` | supported | absent |
| `time_window` | reserved, unbuilt | absent |

Two operators writing "the same" matcher against the two surfaces select
different traffic. `url_path: "/v1/chat"` matches by substring in a cohort and by
regex in a rule — they agree here and diverge the moment anyone writes `.` or `*`.

### 5.1 Canonical matcher

```jsonc
{
  "url_path":     "^/v1/chat",         // RE2 regex, matched with MatchString
  "model":        ["gpt-4o"],          // lists: OR within, case-insensitive
  "vendor":       ["openai"],
  "source_app":   ["checkout"],
  "workflow_type":["rag"],
  "customer_header_match": {"header_name": "X-Team", "regex_value": "^eng$"}
}
```

An absent or empty field is **no constraint**; set fields AND across; lists OR
within; a malformed regex **fails closed**; unknown fields are **rejected**.

RE2 over substring, deliberately: substring cannot express anchoring, so a cohort
keyed on `/v1/chat` also claims `/v1/chat_admin`. Regex can express substring;
substring cannot express regex.

### 5.2 Migration is a rewrite, not a dual-write

Measured 2026-08-18: **9 experiments** (7 `archived`, 1 `dry_run`, 1 `draft`),
**18 variants**, **0 running**. Nothing serves traffic through a cohort matcher,
so stored cohorts convert in place — `url_path` substring →
`regexp.QuoteMeta(substring)`, preserving today's behaviour exactly rather than
guessing at intent. Archived experiments migrate too; they are read for
historical attribution.

If this lands after experiments carry real traffic, this section is void and
route-rule.md §10.3's dual-write path applies instead.

### 5.3 The evaluator is a shared library, not a copy

Route matching runs in dashboard-be at resolve time; cohort matching runs in the
gateway at routing time. A duplicated matcher is exactly how the two semantics
diverged. `tas-llm-router` already consumes `aether-shared/go-events` and
`Gatekeeper` as sibling modules, so the precedent exists.

---

## 6. Affinity — "session stickiness", disambiguated

Stickiness in a web tier exists for one reason: server-side session state. In LLM
routing the term covers **three unrelated needs**, with different keys, different
scopes, and different costs when broken. Conflating them is why our current
design has none of them.

### 6.1 Prompt-cache affinity — an economic mechanism, not a correctness one

Vendors hold a cache keyed to the prompt prefix:

- **Anthropic** — explicit `cache_control` breakpoints, **5-minute default TTL**,
  extendable to 1 hour; cache *writes* carry a 25% surcharge (2× at 1h), reads are
  heavily discounted.
- **OpenAI** — implicit, engages above ~1,024 tokens; **routes on a hash of roughly
  the first 256 tokens**, and `prompt_cache_key` is documented as a *routing hint*
  that steers a request toward a machine likely to already hold the state.

That second point is the striking one: OpenAI has effectively exposed a
**consistent-hash affinity key** at the API boundary. The vendor is doing ring-hash
routing internally and letting the caller influence the ring.

The self-hosted analogue quantifies what breaking affinity costs. Against vLLM
replicas with prefix caching, replacing round-robin with cache-aware routing has
been measured at up to **57× better time-to-first-token and 2× throughput** on an
8-pod deployment, and ~**108%** throughput improvement in DigitalOcean's inference
gateway. Round-robin does not fail — it silently recomputes the prefill every time.

**Directly relevant to us**: issue #100 records that the gateway *drops client
`cache_control`*, making vendor prompt caching unreachable. Affinity and
cache-control pass-through are therefore one feature, not two — and today we have
neither, while `cachePrefixHash` already computes exactly the key the decision
would need.

Implementation tiers, in the order the field recommends adopting them:

| tier | key | gets you |
|---|---|---|
| 1 — session affinity | user / conversation id | works for single-tenant chat; reshuffles when membership changes |
| 2 — **prefix-hash** | hash of the first N stable tokens (system prompt, tools) | most of the gain, no custom infrastructure — **the recommended starting point** |
| 3 — KV-event-aware | live per-replica cache state | most precise; only meaningful for self-hosted replicas |

For a gateway in front of *commercial* APIs, tier 2 is the ceiling, and it means:
**keep a conversation on the same vendor and model, and preserve the prefix
byte-for-byte.** Both are routing decisions.

### 6.2 Assignment affinity — already solved, worth naming

Experiments already bucket stickily on `key_source` = `conversation | user | flow |
principal | ip | request`, with a `salt` to decorrelate experiments. That is
consistent hashing under another name, and it is the one affinity we have.

The contract should reuse this vocabulary rather than invent a second one:
`affinity.key_source` and the experiment's `assignment.key_source` should be the
same enum, evaluated by the same code.

### 6.3 Conversation coherence — a product constraint, not an optimisation

If a fallback silently swaps models mid-conversation, the assistant's voice,
formatting and refusal behaviour change mid-thread. No load-balancer concept
covers this, because no web backend has a personality.

This is the LLM reading of **connection draining**: a chain that is fine for a
fresh request may be wrong at turn 12. Hence `on_break`:

```jsonc
"affinity": {
  "key_source": "conversation",   // conversation | user | flow | principal | ip | none
  "scope":      "vendor+model",   // vendor | vendor+model | deployment
  "ttl":        "5m",             // align with the vendor's cache TTL
  "on_break":   "prefer_same"     // prefer_same | allow_switch | fail
}
```

`ttl` defaulting to 5m is not arbitrary — it matches Anthropic's default cache
lifetime, so affinity expires when the thing it protects expires.

---

## 7. Health, budgets and hedging

The canon's resilience primitives, translated. All optional; absent means today's
behaviour.

```jsonc
"health": {
  "eject_after":     {"consecutive_errors": 5, "error_rate_pct": 50},
  "ejection_window": "30s",       // cf. OpenRouter deprioritising on a 30s outage window
  "half_open_after": "60s",       // probe one request before restoring
  "treat_429_as":    "backoff"    // backoff | unhealthy — a throttle is not an outage
},
"budgets": {
  "retry_ratio_pct":  20,         // retries as a share of active requests, not a per-request count
  "max_attempts":     2,
  "hedge_after_ms":   null        // null = off; a hedge DOUBLES cost, so it is opt-in
}
```

Three deliberate choices:

- **`treat_429_as: backoff` is the default.** Ejecting a provider because your key
  is throttled removes capacity you would get back in seconds, and `retry-after`
  says exactly when.
- **Retry budget as a ratio, not a count.** Envoy's guidance is that fixed
  per-request retries turn a partial outage into a full one. Here the blast radius
  is billed.
- **Hedging is off by default and should stay that way.** A duplicate request to a
  second provider halves tail latency and *doubles* cost — defensible for a short
  interactive call, indefensible for a 3,324-token RAG call. It also fights §6.1:
  a hedge to a second vendor is a guaranteed cache miss.

---

## 8. The contract

```jsonc
// POST /internal/policy/resolve → 200
{
  // ---- existing, unchanged ----
  "bundle_id":   "6da56444-…",
  "bundle_name": "Compliance",
  "source":      "route_rule",       // explicit | route_rule | tenant_active | default
  "reduction":   { /* extraction policy */ },

  // ---- new, all optional ----
  "target":    { "provider": "anthropic", "model": null, "source": "route_rule" },
  "selection": { "strategy": "cost", "weights": null },
  "fallback":  { "chain": [ {"provider":"anthropic","model":"claude-haiku-4-5-20251001"},
                            {"provider":"openai","model":"gpt-4o-mini"} ],
                 "on": ["vendor_error","timeout","context_overflow"] },
  "affinity":  { "key_source":"conversation", "scope":"vendor+model",
                 "ttl":"5m", "on_break":"prefer_same" },
  "health":    { "eject_after":{"consecutive_errors":5}, "ejection_window":"30s",
                 "half_open_after":"60s", "treat_429_as":"backoff" },
  "budgets":   { "retry_ratio_pct":20, "max_attempts":2, "hedge_after_ms":null },
  "limits":    { "max_context_window":200000, "max_output_tokens":4096 },
  "constraints": { "deny_vendors":["…"], "require_zdr":false },
  "enforcement": { "mode":"observe" }   // Stage 4.1; absent = observe-only
}
```

`constraints` applies the OpenRouter lesson to our own strength: bundles already
encode compliance intent, and a vendor a tenant may not use should be expressible
as a routing constraint rather than discovered at review time. It is also what
makes "fallback never crosses a policy boundary" (§9) enforceable rather than
aspirational.

---

## 9. Composition order

**Resolution decides, experiments overlay, the router executes, enforcement
applies.** Each stage may only narrow or replace what the previous produced, and
each stamps its identity on the event.

```
1. RESOLVE   (dashboard-be)  matcher → {bundle, target, selection, fallback,
                                        affinity, health, budgets, limits, constraints}
2. OVERLAY   (gateway)       a running experiment may replace target.model / params
3. EXECUTE   (gateway)       affinity → selection → attempt → health/budgets → fallback.chain
4. ENFORCE   (gateway, 4.1)  bundle rules applied per enforcement.mode
```

- **An experiment outranks a route rule on `model`, and only on `model`.** A test
  may not quietly relax a compliance policy — which is why the unwired
  `policy_bundle_id` experiment axis stays unwired until enforcement exists.
- **Fallback never crosses a policy boundary.** Every chain entry is validated
  against `constraints` at write time, not discovered at failover time.
- **Affinity outranks selection.** A warm cache is worth more than a marginally
  cheaper provider: check affinity first, then apply the strategy to what remains.
- **Global strategy is the floor.** No `target` and no `selection` means exactly
  today's behaviour.

### 9.1 Precedence

| decision | set by | beaten by |
|---|---|---|
| policy bundle | explicit header → route rule → tenant active → default | nothing |
| provider/model target | route rule `provider_override` | a running experiment's `override.model` |
| affinity | route rule | expiry (`ttl`) or `on_break: allow_switch` |
| selection strategy | route rule, else global config | affinity |
| params | caller | experiment `override.params` |
| context window / output cap | model registry, then `limits` | nothing |
| fallback chain | route rule | `constraints` (a denied vendor is removed from the chain) |

---

## 10. Validation Rules

1. `target.provider` must name a configured, enabled provider for the tenant.
2. Every `fallback.chain` entry must be a valid `(provider, model)` pair and must
   satisfy `constraints` — rejected at write time.
3. A chain may not begin with the target (that is a retry, not a fallback) and may
   not repeat an entry.
4. `limits.max_context_window` may only **lower** the provider's advertised window.
   Raising it converts a clean pre-flight rejection into a vendor error.
5. `affinity.ttl` may not exceed the target vendor's cache lifetime — beyond it,
   affinity costs routing freedom and buys nothing.
6. `budgets.hedge_after_ms` requires `max_attempts ≥ 2`, and is rejected when
   `affinity.scope` is set, because a hedge to another vendor is a guaranteed cache
   miss.
7. `enforcement.mode = enforce` requires at least one bundle rule with a non-`log`
   action, so "enforcing" never silently means "doing nothing".

---

## 11. Gap analysis

| capability | field norm | us today | proposed |
|---|---|---|---|
| Ordered failover tiers | LiteLLM `order`, Envoy priority | boolean `fallback_enabled` | `fallback.chain` |
| Per-target cooldown | LiteLLM cooldown, Envoy outlier detection | **none** | `health` |
| Retry budget | Envoy ratio-based | fixed attempts | `budgets.retry_ratio_pct` |
| Rate-limit-aware routing | 429 ≠ outage | 429 treated as a failure | `treat_429_as` |
| Observed-performance selection | P2C + Peak EWMA | static config strategy | `selection.p2c_ewma` |
| Weighted / canary split | Portkey, Cloudflare | experiments only | `selection.weights` |
| Prompt-cache affinity | prefix-hash routing; `prompt_cache_key` | **none** (#100: `cache_control` dropped) | `affinity` |
| Token-based limiting | Kong, Cloudflare | request-based | *out of scope — noted* |
| Compliance-constrained routing | OpenRouter data policy | bundles carry intent, routing ignores it | `constraints` |
| Quality-based routing | RouteLLM cascades | none | *out of scope — noted* |

Two are deliberately **not** proposed. Token-based rate limiting belongs with
quotas and spend governance, not routing. Quality-based routing (RouteLLM-style
weak/strong cascades) changes *which answer the customer gets*, and belongs behind
the same gate as enforcement.

---

## 12. Migration Strategy

**Step 0 — unify the matcher (§5).** Extract the shared evaluator, rewrite the 9
stored cohorts, delete the second implementation. First, because every later step
adds fields to a matcher.

**Step 1 — wire `provider_override` end to end** as `target.provider`. One field
that already has schema, store, API and UI; converts a dead column into proof that
resolution can steer execution.

**Step 2 — `health` + `budgets`.** Deliberately before fallback chains: a chain
without cooldown just fails faster in a loop, and these two need no new routing
semantics, only bookkeeping around the existing call.

**Step 3 — `fallback.chain` + `constraints`.** Chains are only safe once
constraints can express which vendors a tenant may not reach.

**Step 4 — `affinity`.** Pairs with #100 (`cache_control` pass-through); neither is
worth much alone. Start at prefix-hash (tier 2).

**Step 5 — `selection`** (weights first, then `p2c_ewma`). Needs step 2's health
data to be worth anything.

**Step 6 — `limits`.** Waits on the model registry (#2–#6).

**Step 7 — `enforcement`.** Stage 4.1, last: the only stage that changes a
customer-visible outcome, and it should sit on a contract already exercised.

Every step is additive per build-vs-reuse §1.2.

---

## 13. Open Questions

1. **Does an experiment's model swap survive a fallback?** Attribution says leave
   the experiment and mark it abandoned; continuity says stay. Now sharper: a
   fallback that leaves the experiment *also* breaks affinity, so the answer is
   probably "leave, and record both".
2. **Is `provider_override` a hard pin or a preference?** A pin plus an outage
   means failure; a preference lets global strategy silently contradict an explicit
   tenant instruction. Suggested resolution: a pin, with `fallback.chain` as the
   only sanctioned escape — which makes the answer configuration rather than policy.
3. **Whose affinity key wins** when a tenant sets `key_source: conversation` but the
   caller supplies `prompt_cache_key`? Proposed: the caller's, since they know their
   prefix structure; ours is the default when they say nothing.
4. **Does `time_window` belong in this pass**, while the matcher is already open?
5. **Should `workflow_type` be formally promoted** — is the doc wrong, or the code?

---

## 14. Risks

- **Route-rule matching is barely exercised.** One production tenant has a rule, it
  is match-all, and matching was proven by calling the resolver directly rather than
  through live traffic. A live end-to-end match carrying `source=route_rule` gates
  step 1.
- **The matcher unification breaks stored cohorts.** Accepted: 0 of 9 experiments
  are running. The window closes the moment experiments carry real traffic.
- **Enforcement raises the cost of a matcher bug** from invisible to account-wide.
  Strict decoding and write-time validation (aiqg-dashboard-be #123) are
  prerequisites, not niceties.
- **Affinity and cost-optimisation are in genuine tension.** Affinity pins traffic
  to a vendor that may not be cheapest; cost routing shreds prompt caches. The
  contract makes the trade explicit rather than choosing globally — but a tenant can
  configure a combination worse than either alone, and the UI has to say so.
- **This adds surface to a component that is already three systems.** Mitigation:
  every field is optional and absent means today's behaviour. The residual risk is
  that "optional" becomes "undocumented default nobody understands".

---

## 15. Related Documentation

- [[route-rule]] — matcher schema, resolution order, Phase-2 placeholders
- [[policy-bundle]] · [[policy-rule]] — bundle contents and rule actions
- [[experiment]] — cohort matcher, override axes, guardrails, sticky assignment
- [[extraction-policy]] — the `reduction` block already carried on resolve
- `tas-llm-router` #2–#6 — model registry (source of `limits`, registry-driven fallback)
- `tas-llm-router` #100 — client `cache_control` dropped; blocks §6.1

### External references

- Envoy — [outlier detection](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/outlier), [circuit breaking](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/circuit_breaking), [retry budgets / transient failures](https://www.envoyproxy.io/docs/envoy/latest/faq/load_balancing/transient_failures), [Peak EWMA](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/contrib/load_balancing_policies/peak_ewma/peak_ewma)
- [LiteLLM routing & load balancing](https://docs.litellm.ai/docs/routing) — strategies, cooldowns, `order` tiers
- [Portkey conditional routing](https://docs1.portkey.ai/docs/product/ai-gateway/conditional-routing) · [canary testing](https://github.com/Portkey-AI/docs/blob/v2/product/ai-gateway-streamline-llm-integrations/canary-testing.md)
- [OpenRouter provider routing](https://openrouter.ai/docs/guides/routing/provider-selection) · [prompt caching](https://openrouter.ai/docs/guides/best-practices/prompt-caching)
- [vLLM production-stack prefix-aware routing](https://docs.vllm.ai/projects/production-stack/en/latest/use_cases/prefix-aware-routing.html) · [llm-d prefix-cache-aware routing](https://llm-d.ai/docs/architecture/advanced/kv-management/prefix-cache-aware-routing)
- [TrueFoundry — KV cache routing: why standard load balancers break prefix caching](https://www.truefoundry.com/blog/kv-cache-routing-why-standard-load-balancers-break-prefix-caching-and-how-to-fix-it)
- [HAProxy — load balancing, affinity, persistence, sticky sessions](https://www.haproxy.com/blog/load-balancing-affinity-persistence-sticky-sessions-what-you-need-to-know)
- [RouteLLM / cost-aware cascades](https://arxiv.org/pdf/2606.27457)
