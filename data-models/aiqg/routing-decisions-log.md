# Routing rewrite — decision log

**Status:** living record · **Covers:** steps 0–6 · **First written:** 2026-08-21
**Companions:** [`routing-decision.md`](./routing-decision.md) (the design) ·
[`routing-measurements.md`](./routing-measurements.md) (the evidence)

Decisions taken while building the routing rewrite, each with the reasoning and
**what would change it**. That last column is the point: a decision recorded
without its reversal condition is documentation, not a decision anyone can
review. Where a decision was made under a constraint that has since moved, say
so and revisit.

Several of these were close calls. They are marked ⚖️.

---

## 1. Failure taxonomy

### 1.1 Eject-eligibility and fallback-eligibility are separate axes

**Decision.** Two independent classifications of the same error, sharing no
type: `breaker.ClassifyError` (should this count against the provider?) and
`breaker.ClassifyFailure` (would a different provider do better?).

**Why.** They disagree constantly, and both disagreements matter:

| failure | ejects? | falls back? |
|---|---|---|
| 5xx / timeout | yes | yes |
| 429 rate limit | **no** — vendor is healthy, throttling | **yes** — another has capacity |
| context overflow | **no** — our request was too big | **yes** — a bigger window serves it |
| malformed / bad key | no | **no** — every provider rejects identically |

The context-overflow row is what a naive "4xx means don't try elsewhere" rule
gets exactly backwards. The malformed row matters because advancing there
multiplies one client error across every vendor and buries the real cause.

**What would change it.** A third consumer needing a different cut of the same
errors — at which point the taxonomy probably wants to be data rather than two
functions.

### 1.2 ⚖️ Breaker proceeds with nowhere to go; constraints fail

**Decision.** When a target is ejected and no alternative exists, the request
**proceeds**. When every provider is denied by constraints, the request
**fails**.

**Why.** Deliberately opposite postures for a similarly-shaped situation. An
ejection says *this target looks broken* — it may have recovered, and refusing
every request is worse than trying. A constraint says *this vendor must never be
used* — serving anyway is the breach the constraint exists to prevent.

**What would change it.** If ejections start being trusted enough that
proceeding is usually wrong, this is the first thing to flip. Watch for
ejected-and-proceeded requests that then fail anyway.

---

## 2. Pins, chains and precedence

### 2.1 ⚖️ `provider_override` is a pin, with the chain as its only escape

**Decision.** Settles open question 2. An unusable pin enters the fallback chain
at tier 1 rather than reverting to the configured strategy.

**Residue, deliberate.** With **no** chain configured, an unusable pin still
falls through to the strategy. Failing outright would turn a provider blip into
a tenant outage for every rule that has not adopted a feature that shipped
minutes earlier. The escape is recorded on the decision either way.

**What would change it.** Chains becoming the norm. Once most rules carry one,
this branch should narrow to an error. **Review when: >80% of rules with a
provider_override also have a chain.**

### 2.2 Affinity is last in precedence

**Decision.** `constraints → health → pin → affinity`.

**Why.** Affinity is an *economic* optimisation. A warm cache on a denied vendor
is a compliance breach; on an ejected provider it is worthless; and an operator
naming a provider outranks an inferred preference. The usable predicate runs
**before** a stored target is offered, so a bad provider is never proposed
rather than filtered afterwards.

**What would change it.** Nothing foreseeable. If this ordering is ever
inverted, the economics have been allowed to outrank correctness.

---

## 3. Scoping

### 3.1 Constraints are tenant-scoped; affinity is rule-scoped

**Decision.** Deliberately inverse.

**Why.** Constraints describe the **customer's obligations** — per-rule they
would be one forgotten rule away from being violated. Affinity describes a
**traffic pattern** — multi-turn agent flows want it, single-shot classification
traffic has no conversation to stick to.

**What would change it.** A tenant needing different compliance boundaries per
workload — real in principle (different data classifications through one
gateway), and it would make constraints a two-level thing rather than moving
them.

### 3.2 A rule's config REPLACES the gateway's, it does not merge

**Decision.** For affinity and prompt-cache mode.

**Why.** A merge makes "this rule turns the feature **off**" inexpressible — an
empty `key_source` would read as "inherit" and silently leave affinity on, the
opposite of what an operator writing an empty block means.

**What would change it.** If partial overrides become the common case, the fix
is an explicit `"inherit"` sentinel rather than switching to merge semantics.

---

## 4. Measurement

### 4.1 dashboard-be measures tokens; the gateway applies prices

**Decision.** Settles open question 6.

**Why.** The gateway cannot query Timescale on the hot path, and must not: a
routing decision that depends on an analytics database is one that fails when
analytics does. Splitting it this way means only **one** service knows prices —
the one computing cost — so there is nothing to disagree about.

**What would change it.** Prices becoming per-tenant (BYOK negotiated rates),
which would put pricing in the control plane and invert this.

### 4.2 Abstention is first-class and must be reported

**Decision.** Below the sample floor, `expected_cost` prices at `max_tokens` as
before **and records that it abstained**. Quality gates likewise report
abstained rather than passed.

**Why.** A router that silently reverts to the behaviour it was meant to
replace, while an operator believes the new one is running, is worse than one
that never shipped — nobody has a reason to look. "We abstained" and "we
measured and this is what we got" are different facts.

**What would change it.** Nothing. This is the load-bearing honesty property of
the whole measurement layer.

### 4.3 ⚖️ Sample floors are high enough to exclude everything today

**Decision.** Verbosity floor 100, signals floor 200. On current volume,
essentially nothing clears either.

**Why.** Routing money or excluding models on a handful of requests is how a
measurement-driven router becomes worse than the guess it replaced.

**What would change it.** Traffic volume. **Review when: any (model, workflow)
pair sustains >500 real requests in the measurement window** — at that point the
floors are doing real work rather than being a blanket no-op, and are worth
tuning per tenant.

### 4.4 ⚖️ The verbosity window is 7 days, which currently yields nothing

**Decision.** 7-day default, configurable. 30 days would produce one usable
cell; 7 produces none.

**Why.** Freshness — a model's behaviour change should surface rather than be
averaged away by a month of history. Widening it to make the table look
populated would be optimising the appearance of the feature.

**What would change it.** Evidence that model behaviour is stable over a month,
or a tenant with enough volume that 7 days clears the floor.

---

## 5. Synthetic traffic

### 5.1 Mark-at-source, with a denylist as interim

**Decision.** `TAS-Synthetic` is the mechanism; the source-app denylist covers
rows written before it existed. The event records **which fired**.

**Why.** Marking is exact and survives renaming; a denylist is a heuristic that
will rot. Recording which mechanism caught each event is what lets the
denylist's contribution be watched.

**What would change it.** **Review when: the denylist's share of synthetic
classifications is near zero** — then delete it. **If it is still growing a year
from now, mark-at-source was never adopted** and the reason needs finding.

### 5.2 Synthetic exclusion is on by default everywhere

**Decision.** For verbosity *and* quality aggregates.

**Why.** Measured: our probes made a model look ~16× cheaper **and** scored it
100 on efficacy and assurance. The biases point the **same way**, so they
compound rather than cancel — an unguarded router would send everything to
whatever we test with most.

**What would change it.** Nothing. A tenant may switch it off per rule, which is
their call to make explicitly.

---

## 6. Selection

### 6.1 Hysteresis ships with `expected_cost`, not after it

**Decision.** Same commit.

**Why.** An expected-cost router without hysteresis flaps, and flapping pays the
prompt-cache re-warm twice. At a measured 3,324-token RAG prefix a 5% saving
needs 126 further requests to repay one write. Shipping the strategy alone would
be shipping precisely the machine the design warns about.

**What would change it.** Nothing.

### 6.2 The warm-cache handicap, not just a threshold

**Decision.** The required improvement **rises** when a switch would discard a
warm cache, and the refusal reason says so.

**Why.** "Must be 25% better" is a number someone picked. "Must beat the
alternative plus the cache you are about to throw away" is the actual economics.
Naming it matters — someone who set 25%, sees 30%, and watches it refused would
otherwise conclude the router is broken.

**What would change it.** Measuring the re-warm cost directly per request, which
would replace the percentage with a real figure.

### 6.3 Weighted selection is deterministic, not random

**Decision.** Hash a stability key; sort providers before allocating bands.

**Why.** A 90/10 split that reshuffles every turn gives every conversation a 10%
chance of a cold cache on **every request**, instead of 10% of conversations
running on the canary. Sorting matters because Go randomises map iteration —
without it the same config reassigns its cohort on every process start.

**What would change it.** Nothing.

---

## 7. Quality gates

### 7.1 Floors and gates, never an optimisation target

**Decision.** CLEAR dimensions filter the candidate set **before** selection;
cost chooses among survivors. Lexicographic.

**Why.** This is the guard on `expected_cost`'s bias toward terseness — which
optimises for fewer tokens, not better answers. A quality *term inside* the cost
function would just be traded against; a floor cannot be.

**What would change it.** Nothing. Making quality tradeable against price is the
failure this prevents.

### 7.2 Dimensions, never composite

**Decision.** Gates read efficacy and assurance directly.

**Why.** Composite renormalises over whichever dimensions were present and is
dominated by cost — Opus scores 100 on every quality dimension and lands at 67.
Routing on it would let a **dashboard weighting change silently re-route
production traffic**.

**What would change it.** Nothing while composite is `equal-weight-non-nil`.

### 7.3 ⚖️ Thin data is a no-op by default

**Decision.** `on_insufficient_data: ignore`. Excluding is opt-in.

**Why.** A gate excluding every unmeasured candidate would, on a fresh tenant,
exclude **all** of them and fail every request. A quality control that takes the
service down when it has no evidence is worse than no control.

**What would change it.** A tenant for whom routing on unmeasured quality is
unacceptable — which is exactly what the opt-in is for.

---

## 8. Engineering constraints

### 8.1 RE2, not Hyperscan, in the matcher

**Decision.** Corrected from an earlier draft that argued from deployment
constraints.

**Why.** Argued from **expressiveness parity and problem shape**: both are
automata-based, neither supports backreferences or arbitrary lookaround, and
Hyperscan is *more* restrictive on anchors. The matcher's job does not need what
Hyperscan adds.

**Not precluded.** Hyperscan may still be added in the backend; it is already in
the proxy layer.

### 8.2 ⚖️ `go-aiqg-resilience` holds things that are not resilience

**Decision.** Affinity, selection and synthetic identification live in a module
named for resilience.

**Why.** It is in practice the shared **routing-decision contract**. A separate
module per concept would mean another sibling `replace` in two services and
another thing to keep in step, to buy a more accurate name. Renaming churns
every import.

**Accepted debt, stated in the package comment rather than hidden.**

**What would change it.** A fourth or fifth unrelated concept landing there —
at which point rename once, thoroughly.

### 8.3 Sibling `replace` requires a matching line in every build path

**Not a decision — a recurring failure worth a control.**

Adding a sibling `replace` to `go.mod` requires:

1. a **CI checkout** step (broke dashboard-be `main` after step 0),
2. a **Dockerfile `COPY`** (caught pre-build at step 5, gateway),
3. a **Makefile rsync AND a second Dockerfile `COPY`** (caught pre-build at
   step 5, dashboard-be — the rsync alone still failed).

Three instances, same cause: `go.mod` gives no hint those paths exist. All the
files now carry the warning, but **comments are not a control**.

**Recommended:** a CI job that adds a sibling replace to a scratch module and
asserts the image still builds — or simply builds the image on every PR.

---

## 9. Rollout posture

**Every new routing behaviour ships off by default** — `AIQG_BREAKER_ENABLED`,
`AIQG_AFFINITY_ENABLED`, per-rule strategies and gates unset.

**Why.** They change which provider serves a request. Deploying and enabling
should be two decisions, so a deploy can be verified for regressions before any
behaviour changes.

**What would change it.** Enough production evidence to make a feature the
sensible default — which is a per-feature judgement, not a blanket one.

---

## Open, unresolved

| # | Question | Blocked on |
|---|---|---|
| 1 | Does an experiment's model swap survive a fallback? | Step 6+ interaction |
| 3 | Whose affinity key wins when caller and tenant disagree? | Proposed: the caller's |
| 4 | Does `time_window` belong in the matcher? | Not started |
| 5 | Is `workflow_type`'s doc wrong, or its code? | Doc says MVP must reject; code matches on it |
| 7 | Does `switching.dwell` apply across a fallback? | Probably not — a failover is not a price decision |
| — | Errors-page fallback breakdown | Chain position/reason into Timescale |
| — | Prompt-cache `auto` placement engine (P2) | Probe blind spot (§4 of measurements) |
| — | Route-level `prompt_cache` config | Mode is header + global only |
