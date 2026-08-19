# AIQG Routing Decision — unified resolve contract

---

**Metadata**

```yaml
service: aiqg-dashboard-be + tas-llm-router
model: AIQGRoutingDecision
database: PostgreSQL (aiqg schema)
version: 0.4.0
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
including a first-class treatment of **affinity** (§6), the switching costs that
make affinity economic (§7), session identity (§8), cache-key design (§9), and
CLEAR measurements fed back as routing inputs (§10).

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

## 4. Selection strategy — expected cost, not list price

Today: one global strategy (`cost-optimized` | `round-robin` | `performance`)
chosen at process start. Two things are wrong with it, and the second is worse
than the first.

### 4.1 Cost routing currently optimises the wrong 8%

`EstimateCost` in both providers estimates output as **`max_tokens` — the
ceiling** (`anthropic/provider.go:312`, `openai/provider.go`), falling back to a
literal 100 when unset. So two models with the same `max_tokens` price
identically on output no matter how differently they actually behave.

That would be a rounding error if output were cheap. It is not. Measured on our
own traffic, output is **90–99% of the bill**:

| model (single_turn_qa) | n | avg in | avg out | $/req | output share |
|---|---|---|---|---|---|
| `claude-opus-4-6` | 33 | 31.0 | **452.4** | 0.034395 | 99% |
| `claude-haiku-4-5` | 1258 | 32.8 | **61.6** | 0.000273 | 90% |
| `gpt-4o-mini` | 48 | 13.9 | **48.0** | 0.000031 | 93% |

Same workflow, near-identical input, and a **7.3× verbosity spread**. Cost
routing today optimises the 8% it can see and guesses the 92% that decides the
answer.

### 4.2 The verbosity budget rule

A cheaper model only wins if its extra verbosity stays inside its price
advantage. For output-dominated traffic that reduces to a rule worth stating
plainly:

> **A model's verbosity budget is its output-price ratio.**
> Model B beats model A while `out_B/out_A < p_out_A/p_out_B`.

Applied to the measured data, against `claude-haiku-4-5`:

| candidate | may be … more verbose | actually is | verdict |
|---|---|---|---|
| `gpt-4o-mini` | **6.7×** | 0.78× | wins comfortably — break-even at **451** output tokens, 9.4× its current length |
| `claude-opus-4-6` | 0.05× | 7.34× | loses by ~126×, as its list price already implies |

The useful case is the first one: `gpt-4o-mini` has ~9× of verbosity headroom
before the cheaper list price stops being a cheaper bill. A model with a 10%
price advantage has 10% of headroom, and one bad prompt template erases it.

### 4.3 Proposal

```jsonc
"selection": {
  "strategy": "expected_cost",                   // cost | expected_cost | latency | p2c_ewma | weighted | pinned
  "weights":  {"anthropic": 90, "openai": 10}    // weighted only — canary lives here
}
```

`expected_cost` prices a candidate as
`in_tokens × p_in + E[out_tokens | model, workflow_type] × p_out`, where the
expectation is measured from our own response events rather than assumed. It
**abstains** — falling back to today's `max_tokens` estimate — below a sample
floor per (model, workflow), because a verbosity factor off three requests is
noise.

Two guards this needs, and they are not optional:

- **Verbosity is not quality.** A terser model may simply be answering less.
  Expected-cost routing without a quality floor optimises for brevity, which is
  why it pairs with CLEAR efficacy scores rather than replacing them.
- **`max_tokens` is a ceiling, not an estimate, and must stay in the estimate as
  a cap.** A model that would run long gets truncated, and the truncated cost is
  what you actually pay.

Adopting Portkey's observation explicitly: **canary is load balancing with a
different intent.** A 90/10 weight split and a canary rollout are the same
mechanism; what differs is who watches the result — the experiment runner's job,
which already exists.

`p2c_ewma` is proposed as the eventual default over `performance`: it routes on
observed behaviour with O(1) selection and sheds traffic from a degrading
provider before it fails outright.

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

## 7. Switching costs — why "cheaper" can be more expensive

Affinity has an economic mirror image that no list-price comparison shows:
**moving a request to a different provider throws away a warm cache and makes
you pay to rebuild it.**

Anthropic prices the cache explicitly — a write costs **1.25×** base input, a
read **0.10×**. So abandoning a warm prefix and establishing it elsewhere costs
`(1.25 − 0.10) × p_in × prefix_tokens` as a one-off, before any per-request
saving starts accruing.

For our measured RAG prefix of 3,324 tokens at Haiku input pricing, that penalty
is **$0.00306**, against a cached request that costs **$0.000486**. How many
further requests inside the TTL window it takes to repay:

| per-request saving | requests needed to break even |
|---|---|
| 5% | **126** |
| 10% | **63** |
| 25% | 25 |
| 50% | 13 |
| 80% | 8 |

The default cache TTL is **five minutes**. A 10% cheaper route therefore has to
attract 63 further requests from the same conversation inside five minutes
before it is actually cheaper. For most traffic it will not, and the "saving"
is a loss.

Two consequences:

1. **Small price advantages should not move traffic at all when a cache is
   warm.** This is flap damping, and the load-balancer canon already has the
   shape for it — a minimum improvement threshold plus a dwell time.
2. **Flapping costs twice.** Switching back re-warms the original. A cost
   strategy that chases a moving price without hysteresis pays the penalty in
   both directions.

```jsonc
"switching": {
  "min_improvement_pct": 25,     // ignore anything smaller while a cache is warm
  "dwell": "60s",                // minimum time on a target before another move
  "warm_cache_bias_pct": 15      // handicap applied to challengers while warm
}
```

These numbers are deliberately conservative defaults, not tuning: 25% is the
point at which the table above repays inside a plausible five-minute burst.

---

## 8. Session identity — when has a session *really* changed?

Long sessions wander across topics, so "same conversation" is a poor proxy for
"same routing context". But the three things one might mean by *changed* have
three different answers, and only one of them involves topics at all.

| question | correct signal | topic drift relevant? |
|---|---|---|
| Is the vendor's prompt cache still warm? | **TTL expiry + stable-prefix change** | **No** |
| Is our semantic cache entry valid? | per-request similarity + L2 guards | n/a — C4 has no session concept |
| Is it safe to switch models? | **topic drift** | **Yes — as a seam, not a break** |

### 8.1 Prompt-cache warmth is topic-independent

A three-hour session covering eight topics keeps its vendor cache the whole time,
provided the stable prefix (system prompt + tool schemas) does not change and
requests stay inside the TTL. The cache is keyed on **prefix bytes**, not
meaning. Invalidating affinity because the subject changed would discard a warm
cache for no reason — an expensive mistake dressed as diligence.

### 8.2 Session epoch — a cheap, deterministic identity

```
epoch  = (stable_prefix_hash, idle_bucket)
key    = (conversation_id, epoch)
```

The epoch increments when either:

- the **stable prefix changes** — the system prompt or tool set was edited, so
  the vendor cache is cold regardless of what we do; or
- the **gap since the previous request exceeds `affinity.ttl`** — the cache has
  expired, so affinity is free and costs nothing to abandon.

No embeddings, no thresholds, no per-turn inference. `cachePrefixHash` already
computes the first component. Idle-bucketing gives the second for the price of a
timestamp comparison.

### 8.3 Topic drift is for finding a seam, not for invalidating a cache

The one place semantic drift genuinely helps inverts the usual framing. A model
switch is most noticeable *mid-topic* — voice, formatting and refusal behaviour
change under the user's feet. At a topic boundary it is nearly invisible.

So drift detection is not a cache signal; it is a **scheduling** signal for
`on_break: prefer_same` — the moment at which a deferred switch can finally be
taken. That makes it strictly optional: costing an embedding per turn to find a
politer moment to re-route is worth it only for long interactive sessions, and
the failure mode of skipping it is mild.

**Anti-pattern, stated explicitly**: do not use topic drift to invalidate
prompt-cache affinity. The two are unrelated, and conflating them throws away
warm caches while leaving genuinely stale pins in place.

---

## 9. Cache key specification

Three caches sit in this path, with three different keys and three different
invalidation rules. They are currently specified in three places and interact in
ways nothing documents.

| cache | key today | TTL | store |
|---|---|---|---|
| **C1 exact response** | sha256(tenant, vendor, model, full messages, temperature, seed, …) | 10m | `redis-shared`, `aiqg:cache:{tenant}:*` |
| **C4 semantic** | embedding(`lastUserText`) + Scope(tenant, model, scoring_version) | 30m | `redis-semcache`, `aiqg:scache:{tenant}:*` |
| **vendor prompt cache** | not ours — the vendor hashes our prefix bytes | 5m / 1h | vendor side; probed via `cachePrefixHash` |

Six rules the keys should obey:

1. **Include what changes the answer; exclude what changes only the route.**
   Vendor and model change the answer, so they belong in the key — with the
   consequence that a provider switch busts *our* C1 as well as the vendor's
   cache. That is a second cost stacked on §7's, and it should be counted there
   rather than discovered.
2. **Cross-model reuse is an opt-in tier, not a key change.** The tempting fix —
   drop `model` from the key — silently serves an Opus answer to a Haiku request,
   which corrupts attribution and makes CLEAR scores incomparable. If wanted, it
   should be a *second* lookup, tenant opt-in, stamped
   `cache_state=cross_model_hit` so the reporting stays honest.
3. **Normalise before hashing.** Key order in tool schemas, trailing whitespace,
   and `content` given as a string versus a one-element array all produce
   different bytes for identical requests. Canonical JSON ordering and trimmed
   whitespace, or the cache fragments for reasons no operator can see.
4. **Exclude routing-only fields**: retry counters, request ids, timestamps,
   `TAS-*` headers. Anything the router adds must not enter the key, or every
   retry becomes a miss.
5. **Reduction must be deterministic, or it fragments every key at once.**
   Payload reduction rewrites the request; if identical input reduces differently
   on two calls, C1 misses *and* the vendor prefix misses. Reduction is currently
   query-anchored and therefore stable per call, but the invariant is implicit —
   it should be stated and tested, because it is load-bearing for two caches.
6. **The prefix key covers the stable span only** — system prompt plus tool
   schemas plus leading context, never the trailing user turn. Include the turn
   and every request is a new key, which is the failure mode that makes prefix
   probes look useless.

One deferred optimisation, noted so it is not rediscovered: two requests
differing only in `max_tokens` could share an entry when the stored answer is
shorter than the new cap, since `max_tokens` truncates rather than changes the
answer. Correct, but not worth the special-casing until the simpler rules land.

---

## 10. CLEAR as a routing input

We measure the thing nobody routes on. LiteLLM, Portkey, OpenRouter, Kong and
Cloudflare all select on price, latency and liveness. **None of them route on
observed quality or observed compliance outcomes**, because none of them compute
either. We compute both per request and then throw the result at a dashboard.

Closing that loop is the strongest differentiator available here — and the
measurements below say the signal is not yet fit for it, in three specific ways
that are all fixable.

### 10.1 What the data actually says

Per-model CLEAR, all traffic to date:

| model | n | efficacy cov. | reliability cov. | efficacy | assurance | reliability | **composite** |
|---|---|---|---|---|---|---|---|
| `claude-haiku-4-5` | 1864 | 88% | 88% | 70.2 | 92.6 | 100.0 | 91.3 |
| `gpt-4o-mini` | 114 | 96% | 100% | 87.9 | 100.0 | 100.0 | 97.6 |
| `claude-opus-4-6` | 33 | 100% | 100% | **100.0** | **100.0** | **100.0** | **67.0** |
| `gpt-4o` | 5 | 60% | 60% | 86.7 | 100.0 | 100.0 | 98.4 |

Three hazards fall straight out of it.

**1 — Composite is not comparable across models.** `weights_applied` is
`equal-weight-non-nil` on **100%** of 2,042 scored events, so composite
renormalises over whichever dimensions happened to be present. Coverage ranges
from **60% to 100% depending on the model**. A composite averaged over two
dimensions and one averaged over five are different quantities wearing the same
name, and ranking candidates by it compares them anyway.

**2 — Composite is dominated by cost.** `claude-opus-4-6` scores **100 on
efficacy, assurance and reliability** and lands at **67 composite**. The only
things that can drag it there are cost and latency. So "route by composite" is
approximately "route by cost, with the quality dimensions along for the ride" —
and equal weighting means one dollar trades 1:1 against one quality point, which
is not a trade any tenant has actually asked for.

**3 — The signal is contaminated by our own traffic.** Efficacy is
finish-reason-only in the MVP: `stop`/`tool_calls` → 100, **`length` → 60**,
`content_filter` → 0. Haiku's 70.2 is depressed because this session's
verification probes ran with `max_tokens` of 5 and 64 and truncated. Routing on
that today would penalise a model for our test harness. Any use of efficacy for
routing has to exclude synthetic traffic — which means the demo-flow attribution
gap (per-flow identity is not stamped on events) is a prerequisite, not a
nice-to-have.

### 10.2 Design rules

**Route on dimensions, never on composite.** Composite is a reporting artifact
whose weights are a presentation choice. Routing needs named thresholds against
named dimensions, so a change in dashboard weighting cannot silently re-route
production traffic.

**Two loops at two timescales, and do not conflate them.**

| loop | signal | horizon | decides |
|---|---|---|---|
| **fast** | 5xx, timeouts, 429 | sub-second | eject / retry / fall back (§10 health) |
| **slow** | CLEAR aggregates per (model, workflow, tenant) | minutes–days | which candidates are eligible at all |

CLEAR reliability is scored *after* the response and is useless for ejection;
raw 5xx counting is useless for quality. Each loop needs its own signal.

**Floors and gates, not an optimisation target.** CLEAR dimensions filter the
candidate set; cost and latency then choose among the survivors. That
lexicographic shape avoids the 1:1 dollar-versus-quality trade that equal
weighting imposes, and it degrades safely: with no qualifying data the filter is
a no-op and behaviour is exactly today's.

```jsonc
"signals": {
  "min_efficacy":           70,          // filter, not an objective
  "max_assurance_severity": "medium",    // any high/critical drops the candidate
  "min_samples":            200,         // per (model, workflow) before the filter binds
  "max_staleness":          "24h",
  "exclude_synthetic":      true,        // demo/experiment traffic must not score a vendor
  "on_insufficient_data":   "ignore"     // ignore | prefer_measured | fail
}
```

**Assurance is the dimension worth routing on first.** It is already bucketed on
*worst* severity rather than count — the code's own reasoning is that "one
unauthorized disclosure invalidates otherwise perfect performance" — which makes
it a natural hard gate rather than a score to average. It also complements
`constraints` (§11) neatly: constraints express compliance *as declared*,
assurance expresses compliance *as observed*. A vendor whose outputs keep
tripping high-severity findings for a tenant should leave that tenant's
candidate set on evidence, not only on policy.

### 10.3 Two failure modes this creates

**Exploration collapse.** Routing away from a model stops generating data about
it, so its score freezes at the moment it fell out of favour and it can never
recover — a one-way door built out of a moving average. The fix is already
built: **the experiment runner is the designated explorer.** It does sticky
cohort assignment with traffic caps and auto-stop guardrails, which is exactly a
bandit's exploration arm. Routing exploits; experiments explore; neither should
learn from the other's traffic without knowing which it was.

**Goodhart.** Efficacy today is finish-reason; when the judged sub-metrics land
it becomes a model-scored quantity, and routing to maximise a judge's score
selects for judge-pleasing answers. Using floors rather than maximisation blunts
this — a floor is satisfied, not chased — and assurance is comparatively
resistant because Gatekeeper findings are adversarial rather than preferential.

---

## 11. Health, budgets and hedging

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

## 12. The contract

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
  "selection": { "strategy": "expected_cost", "weights": null },
  "switching": { "min_improvement_pct": 25, "dwell": "60s", "warm_cache_bias_pct": 15 },
  "cache":     { "cross_model_reuse": false },   // opt-in; stamps cache_state=cross_model_hit
  "signals":   { "min_efficacy": 70, "max_assurance_severity": "medium",
                 "min_samples": 200, "max_staleness": "24h",
                 "exclude_synthetic": true, "on_insufficient_data": "ignore" },
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

## 13. Composition order

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

### 13.1 Precedence

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

## 14. Validation Rules

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
8. `selection.strategy = expected_cost` requires a measured verbosity factor for
   the (model, workflow) pair above the sample floor; below it the router falls
   back to the `max_tokens` estimate and says so on the event, rather than
   pricing on three data points.
9. `switching.min_improvement_pct` may not be 0 while `affinity` is set — a zero
   threshold means every price tick abandons a warm cache, which §7 shows is a
   loss below ~25%.
10. `cache.cross_model_reuse = true` requires the tenant to have acknowledged
    that CLEAR scores become incomparable across the reused entries.

---

## 15. Gap analysis

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
| Expected-cost (verbosity-aware) routing | — *nobody does this well* | prices output at `max_tokens`, the ceiling | `selection.expected_cost` |
| Switch hysteresis / flap damping | Envoy dwell + slow start | none — cost strategy may move on any delta | `switching` |
| Session identity for affinity | LB cookie rotation | none | `epoch` (§8) |
| Cache-key normalisation | standard practice | ad hoc per cache | §9 rules |
| **Quality/compliance-outcome routing** | **nobody — none of them measure it** | computed per request, used only for dashboards | `signals` (§10) |
| Quality-based routing | RouteLLM cascades | none | *out of scope — noted* |

Two are deliberately **not** proposed. Token-based rate limiting belongs with
quotas and spend governance, not routing. Quality-based routing (RouteLLM-style
weak/strong cascades) changes *which answer the customer gets*, and belongs behind
the same gate as enforcement.

---

## 16. Migration Strategy

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

**Step 4 — `affinity` + `epoch` (§8) + the §9 key rules.** Pairs with #100
(`cache_control` pass-through); neither is worth much alone. Start at prefix-hash
(tier 2). The epoch is a prerequisite, not a follow-up — affinity without an
expiry signal is a pin that never releases.

**Step 5 — `selection`** (weights first, then `expected_cost`, then `p2c_ewma`).
`expected_cost` needs only a verbosity table built from events we already emit,
so it can land before `p2c_ewma`, which needs step 2's health data. Ship
`switching` in the same step: an `expected_cost` router without hysteresis is
precisely the flapping cost machine §7 describes.

**Step 5b — `signals` (§10).** Needs three fixes first, in order: stop routing
on composite, exclude synthetic traffic from the aggregates (which needs demo-flow
attribution on events), and land per-dimension sample floors. Assurance first —
it is already a hard-gate shape.

**Step 6 — `limits`.** Waits on the model registry (#2–#6).

**Step 7 — `enforcement`.** Stage 4.1, last: the only stage that changes a
customer-visible outcome, and it should sit on a contract already exercised.

Every step is additive per build-vs-reuse §1.2.

---

## 17. Open Questions

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
6. **Is `cross_model_reuse` ever acceptable?** It raises hit rate and destroys
   score comparability. My instinct is no by default and yes for a tenant that
   explicitly values cost over measurement — but it may be simpler to decline it
   entirely than to explain what the scores then mean.
7. **Where does the verbosity table live?** Computed in dashboard-be from events
   and shipped on the decision keeps the gateway stateless; computed in the
   gateway is fresher but makes two services disagree about price.
8. **Should composite ever be routable**, with explicit weights per tenant, or is
   the lexicographic floors-then-cost shape the whole answer? Weights are what a
   customer asks for and what makes a dollar trade against a quality point.
9. **Does `switching.dwell` apply across a fallback?** A failover is not a price
   decision, so it probably should not count against the dwell timer — otherwise
   an outage pins you to a degraded provider.

---

## 18. Risks

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
- **Expected-cost routing optimises for brevity if left unguarded.** A terser
  model may simply be answering less. This is why it is specified alongside a
  CLEAR efficacy floor rather than as a standalone strategy — and why the
  verbosity factor abstains below a sample floor instead of guessing.
- **Verbosity is workload-specific and drifts.** A factor measured on
  `single_turn_qa` says nothing about `rag`, and a prompt-template change can
  move it overnight. The table needs a decay window and a staleness alarm, or it
  becomes a confidently wrong price list.
- **Routing on CLEAR creates a one-way door unless exploration is explicit.** A
  model routed away from stops being measured and can never recover. The
  experiment runner has to be the designated explorer, and its traffic has to be
  excluded from the aggregates it feeds — otherwise exploitation contaminates
  exploration and the loop closes on itself.
- **This adds surface to a component that is already three systems.** Mitigation:
  every field is optional and absent means today's behaviour. The residual risk is
  that "optional" becomes "undocumented default nobody understands".

---

## 19. Related Documentation

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
