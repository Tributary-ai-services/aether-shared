# AIQG Cache Keys and Session Identity

---

**Metadata**

```yaml
service: tas-llm-router (+ aiqg-dashboard-be for configuration)
version: 1.0.0
last_updated: 2026-08-19
status: proposed — §2 documents current behaviour, §3 and §5 propose changes
plan_ref: routing-decision.md §5.6 (session epoch), §5.9 (cache keys), §7 steps 4–5
```

---

## 1. Why these two are one document

Cache keys and session identity look unrelated and are not. Three caches sit in
the request path, and the third — the vendor's own prompt cache — is keyed on
content *we* choose to send and reachable only if successive requests in a
conversation land on the same provider. That makes provider affinity a cache-key
concern, and it makes "when did the session change" the question that decides
when affinity may be released.

Getting either wrong is expensive in a way that does not announce itself. A
fragmented key does not error; it silently misses and the bill goes up. Affinity
released too early does not error; it silently re-warms a cache that was already
paid for.

---

## 2. Current key construction

### 2.1 C1 — exact-match response cache

**Implementation:** `pkg/aiqg/responsecache.KeyHash()`.
**Storage:** `redis-shared`, key `aiqg:cache:{tenant}:{sha256}`. **TTL 10 min.**

The hash covers a JSON signature of:

| Field | In key | Why |
|---|---|---|
| tenant | ✅ | isolation boundary; also the Redis namespace segment |
| vendor, model | ✅ | different models produce different answers |
| full message array | ✅ | the request itself |
| temperature, top_p, max_tokens | ✅ | sampling parameters change the answer |
| frequency/presence penalty, stop, seed | ✅ | ditto |
| response_format | ✅ | changes the shape of the answer |
| tools, functions | ✅ | changes what the model may do |
| scoring version | ✅ | see §2.4 — arguably wrong |
| **experiment variant** | ✅ | prevents a variant's answers leaking into control |

Tenant appears **both** in the hash and as a key segment. That is deliberate
redundancy: the segment makes `PurgeTenant` a prefix scan (right-to-be-forgotten,
tenant deletion), and the hash makes a cross-tenant read impossible even if a
caller constructs a key by hand. The `Cache` interface takes tenant and hash as
separate arguments so a caller *cannot express* a cross-tenant read.

**Eligibility** is decided separately by `Decide()`: streaming responses,
tool-bearing requests, and requests that are neither seeded nor
zero-temperature are refused, because none of them is reproducible.

### 2.2 C4 — semantic response cache

**Implementation:** `pkg/aiqg/semcache`, `Scope{TenantID, Model, ScoringVersion}`.
**Storage:** `redis-semcache` (RediSearch), `aiqg:scache:{tenant}:*`. **TTL 30 min.**

The embedded text is `lastUserText(messages)` — **the most recent user-role
message only**. Everything else in the request is scope or guard material, not
embedding material.

| Component | Role |
|---|---|
| embedding of last user message | L1 vector similarity search |
| `Scope{tenant, model, scoring_version}` | tag filter; re-verified by the L2 scope guard |
| stored prompt text | input to the L2 lexical guards |
| stored response + CLEAR score | what is served on a hit |

The consequence of embedding only the last user turn is worth stating plainly,
because it explains a limitation elsewhere: **conversation context is invisible to
C4**. Two identical questions asked in different conversations are the same key.
This is correct for stateless Q&A and wrong for a follow-up like "and the second
one?", which is why the deterministic guards exist and why RAG traffic with
context in the user turn caches poorly (every request is unique).

### 2.3 Vendor prompt cache (probe only today)

**Implementation:** `cachePrefixHash()` in `internal/server/server.go`.

Covers the **stable span**: all text `system` messages concatenated with a `\x00`
separator (so `["ab","c"]` and `["a","bc"]` differ), plus a signature over every
tool and function — name, description and parameter schema.

It deliberately excludes the trailing user turn. Including it would make every
request a distinct prefix and the probe would never match, which is the failure
mode that makes prefix instrumentation look useless.

Today this is **measurement only**. The gateway does not pass client
`cache_control` through (tas-llm-router #100), so the vendor cache cannot
currently be populated deliberately.

### 2.4 Two properties of the current design worth challenging

**Scoring version in the C1 key.** A scoring-version bump invalidates every
cached response, even though scoring does not change the answer — only how the
answer is scored. The argument for including it is that a stored entry carries its
score, and serving it under a new version would report a stale score. The argument
against is that a scoring change flushes a cache for a reason unrelated to
correctness of content.

*Recommendation:* keep it for now, and revisit by storing the score separately
from the response so the entry survives a re-score. Flagged rather than changed,
because it is a small, self-healing cost (10-minute TTL) and changing it touches
the score-reporting path.

**`max_tokens` in the C1 key.** `max_tokens` truncates; it does not alter the
answer's content. Two requests differing only in `max_tokens` could share an entry
when the stored answer is shorter than the new cap. Correct, but a special case;
deferred until the normalisation rules below are in place.

---

## 3. Proposed key-construction rules

Six rules. The first four are normalisation; the last two are policy.

### 3.1 Normalise before hashing

The current hash is over `json.Marshal` of a Go struct. Struct field order is
deterministic and Go sorts map keys, so the *encoding* is stable — but the
*inputs* are not. Four sources of fragmentation, each producing different bytes
for a semantically identical request:

| Source | Example | Fix |
|---|---|---|
| Content shape | `"hello"` vs `[{"type":"text","text":"hello"}]` | canonicalise single-text content to its string form before hashing |
| Insignificant whitespace | trailing newline on a system prompt | trim trailing whitespace per message |
| Tool ordering | same three tools declared in a different order | sort tools by name before hashing |
| Absent vs zero | `temperature` omitted vs `0` | already handled by `omitempty` + pointers; keep pointers, never default them before hashing |

Tool sorting deserves a caveat: tool *order* can matter to some models' behaviour.
Sorting for the **cache key** while preserving the caller's order **in the request**
is the correct split — the key asks "is this the same question", not "is this the
same bytes".

### 3.2 Version the key

Add a key-schema version to the hashed signature and to the Redis key:

```
aiqg:cache:v2:{tenant}:{sha256}
```

Without it, a normalisation change means old entries remain readable under new
rules — semantically stale but structurally valid. With it, a version bump makes
old entries unreachable and they expire naturally. The TTL is short enough that
this costs almost nothing and removes a class of subtle bug entirely.

### 3.3 Exclude routing-only fields

Nothing the router adds may enter the key: retry counters, attempt numbers,
request identifiers, timestamps, `TAS-*` headers, trace or span identifiers. The
test is simple — **if two requests differ only in that field, would the answer
differ?** If not, it is not key material.

This is not currently a bug (the signature is an explicit allow-list, which is the
right shape) but it must remain an allow-list. A future "hash the whole request"
convenience would make every retry a miss.

### 3.4 Reduction must be deterministic — an invariant, not an aspiration

Payload reduction rewrites the request before it reaches the vendor. If identical
input reduced differently on two calls, **C1 misses and the vendor prefix misses
simultaneously**.

Reduction is currently query-anchored and therefore stable per call, so the
property holds — but it holds by accident of implementation rather than by
contract. It should be:

- stated as an invariant in the reducer's package documentation;
- covered by a test that reduces the same input twice and asserts byte equality;
- and, because reduction happens *after* the C1 lookup, verified not to change
  what a subsequent identical request hashes to.

### 3.5 Cross-model reuse is a second lookup, never a key change

The tempting optimisation is to drop `model` from the key so answers are shared
across models. It should not be done that way. Dropping `model` silently serves a
frontier model's answer to a request that asked for a cheap one, which corrupts
cost attribution and makes CLEAR scores incomparable across the reused entries.

If wanted, it is a **separate, opt-in lookup**:

```jsonc
"cache": { "cross_model_reuse": false }   // tenant opt-in
```

performed only after the model-scoped lookup misses, and stamped
`cache_state=cross_model_hit` so reporting stays honest. Default off. The tenant
must acknowledge that scores become incomparable across reused entries.

### 3.6 Key the prefix on the stable span only

Already correct in `cachePrefixHash`; stated here so it survives future edits. The
prefix key covers system messages and tool schemas. It must never include the
trailing user turn.

---

## 4. Session identity

### 4.1 What identifies a session today

Identity is assembled in `experimentIdentity()` and already supports six keys,
with documented fallbacks:

| Key | Source | Fallback |
|---|---|---|
| `conversation` | `TAS-Conversation-Id` header | W3C baggage `session.id` |
| `user` | baggage `user.id` | — |
| `flow` | `TAS-Flow-Id` | trace id |
| `principal` | token's source app | token id |
| `ip` | client IP | — |
| `request` | response event id | — (no stickiness) |

This is the enum affinity should reuse. Inventing a second vocabulary for
"affinity key" when the experiment runner already has one — with the same
fallbacks and the same edge cases — would guarantee they drift.

### 4.2 The problem with "conversation" alone

A conversation identifier is stable for hours. A vendor prompt cache lives for
five minutes by default. Pinning a provider for the life of a conversation
therefore holds an affinity long after the thing it protects has expired, which
costs routing freedom and buys nothing.

The naive fix — release affinity when the topic changes — is wrong, and the reason
is the crux of this section. **The vendor cache is keyed on prefix bytes, not
meaning.** A three-hour session covering eight topics keeps its cache the whole
time, provided the system prompt and tool set are unchanged and requests stay
inside the TTL. Topic drift neither warms nor cools it.

### 4.3 The session epoch

```
epoch  = (stable_prefix_hash, idle_bucket)
key    = (tenant, conversation_id, epoch)
```

The epoch increments when either component changes:

**`stable_prefix_hash`** — exactly the value `cachePrefixHash` already computes
(system messages + tool schemas). When it changes, the system prompt or tool set
was edited, so the vendor cache is cold regardless of routing. Affinity to the old
provider is worthless and should be released.

**`idle_bucket`** — `floor(now / affinity.ttl)` advanced whenever the gap since
the previous request in this conversation exceeds `affinity.ttl`. When it
advances, the vendor cache has expired, so affinity costs nothing to abandon.

Both signals are already available: the first is computed today, the second needs
only a timestamp comparison. **No embeddings, no thresholds, no inference.**

### 4.4 Storage

| Aspect | Choice | Rationale |
|---|---|---|
| Store | Redis (shared across gateway replicas) | routing decisions must be consistent across replicas |
| Key | `aiqg:affinity:{tenant}:{conversation}:{epoch}` | tenant-segmented for purge, epoch-segmented so a new epoch is a new key rather than a mutation |
| Value | `{provider, model, established_at}` | small and fixed-size |
| TTL | `affinity.ttl`, refreshed on read | the record expires with the cache it protects; no sweeper needed |
| Size | one small record per active conversation | bounded by concurrency, not by history |

Making the epoch part of the key rather than a field means epoch advance requires
no read-modify-write and no invalidation — the old key simply expires.

**Privacy.** The affinity record stores identifiers and a hash, never content. The
conversation identifier is supplied by the caller; the prefix hash is a SHA of
prompt text and is not reversible. Nothing in this store is a data-retention
concern beyond the identifiers already present on events.

### 4.5 Lifecycle

```
request arrives
  │
  ├─ derive conversation id (header → baggage → none)
  │     └─ none? → no affinity; route normally
  │
  ├─ compute stable_prefix_hash
  ├─ read last-seen timestamp for (tenant, conversation)
  ├─ idle > ttl?  → advance idle_bucket
  ├─ prefix changed? → new stable_prefix_hash
  │
  ├─ look up aiqg:affinity:{tenant}:{conv}:{epoch}
  │     ├─ hit  → prefer that provider (subject to health §7 and constraints)
  │     └─ miss → route normally, then record the chosen provider
  │
  └─ on completion: refresh TTL
```

### 4.6 Edge cases, and what each does

| Case | Behaviour | Why |
|---|---|---|
| No conversation identifier | no affinity; route normally | most single-shot API traffic; guessing would be worse |
| Provider ejected by health | affinity yields to health | a warm cache on a broken provider is worthless |
| Provider denied by constraints | affinity yields to constraints | compliance outranks economics, always |
| Experiment claims the request | experiment's model override wins; affinity applies to the *provider* where it still can | an experiment varies the model deliberately |
| Fallback fires | affinity is broken and re-established on the fallback target | the original is unreachable by definition |
| Retry of the same request | same epoch, same affinity | a retry is not a new turn |
| Gateway replica restart | no effect | state is in Redis, not in process |
| Streaming request | epoch computed at request start, unchanged mid-stream | a stream is one turn |

### 4.7 `on_break` — the product control

When affinity cannot be honoured, three behaviours are configurable:

| Value | Meaning | Use |
|---|---|---|
| `prefer_same` *(default)* | try the affine provider first; fall through silently if unavailable | almost all traffic |
| `allow_switch` | affinity is a hint; selection may override on a large enough improvement | cost-sensitive batch work |
| `fail` | if the affine provider is unavailable, fail rather than switch | strict reproducibility requirements |

`fail` exists for a narrow case — a workload that must not silently change model
mid-conversation, where a visible error is preferable to a subtly different
answer. It should be rare, and the UI should say so.

### 4.8 Where topic drift legitimately belongs

Not in cache invalidation, and not in affinity release. Its only sound use is
**scheduling a switch that has already been decided**: a model change is jarring
mid-topic and nearly invisible at a topic boundary, so a deferred switch may wait
for a drift signal to take effect.

That makes it strictly optional — it costs an embedding per turn to find a politer
moment, and the failure mode of skipping it is that a user notices a tonal shift.
Worth it for long interactive sessions; not worth it for API traffic.

**Anti-pattern, stated so it is not rediscovered:** using topic drift to invalidate
prompt-cache affinity discards warm caches for no benefit while leaving genuinely
stale pins in place, because the signal is uncorrelated with what it is being used
to decide.

---

## 5. Interaction between the three caches

| Change | C1 | C4 | Vendor prefix |
|---|---|---|---|
| Different tenant | miss | miss | n/a |
| Different model | miss | miss (scope) | miss |
| Different provider | miss | miss (scope) | **miss — and re-warm is billed** |
| Any message byte differs | miss | may hit (last user turn only) | hit if the *prefix* is unchanged |
| Reduction output differs | miss | may hit | miss |
| Scoring version bump | miss | miss | unaffected |
| System prompt edited | miss | may hit | **miss** — and the epoch advances |
| Idle > 5 min | may hit (10m TTL) | may hit (30m TTL) | **expired** — epoch advances |

Two rows carry most of the operational consequence. **Changing provider misses
all three** and additionally pays the vendor's cache-write surcharge — which is
why switching needs hysteresis. And **editing a system prompt** invalidates the
vendor prefix for every conversation using it simultaneously, which is worth
knowing before a prompt is edited at 09:00 on a Monday.

---

## 6. Test plan

| Test | Asserts |
|---|---|
| Content shape canonicalisation | `"hi"` and `[{"type":"text","text":"hi"}]` produce one key |
| Tool ordering | same tools in different order produce one key |
| Whitespace | trailing newline does not fragment |
| Routing fields excluded | two requests differing only in retry count produce one key |
| Reduction determinism | same input reduced twice is byte-identical |
| Key version | a version bump makes old entries unreachable, not misread |
| Cross-tenant | a hand-constructed key cannot read another tenant's entry |
| Epoch — prefix change | editing the system prompt advances the epoch |
| Epoch — idle | a gap beyond TTL advances the epoch |
| Epoch — topic change | a topic change with a stable prefix **does not** advance the epoch |
| Affinity yields | ejected or constraint-denied providers are not selected despite affinity |
| Affinity on retry | a retry keeps the same epoch |

The third epoch test is the important one: it encodes the decision that topic
drift is not a cache signal, so a future change that "helpfully" adds drift-based
invalidation fails a test that explains why.

---

## 7. Related

- [[routing-decision]] §5.6, §5.9, §7 steps 4–5
- [[extraction-policy]] — reduction determinism invariant
- `tas-llm-router` #100 — client `cache_control` dropped; blocks deliberate vendor-cache population
