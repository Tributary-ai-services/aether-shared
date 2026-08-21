# Routing measurements — what the data actually said

**Status:** living record · **First written:** 2026-08-21 · **Companion to:** [`routing-decision.md`](./routing-decision.md)

Every measurement here was taken against live production data before the
corresponding feature was written, and each one changed the design. They are
recorded together because a number that changes a decision is worth more than
the decision it changed: the decision can be re-derived from the number, but not
the reverse.

Re-run these before trusting any of the defaults below. Several are already
known to be fragile.

---

## 1. Our own probe traffic poisoned the verbosity table

**Measured 2026-08-21, 30-day window, `aiqg.event_metrics`.**

Every `gpt-4o-mini` event in production came from gateway verification probes:

| model | workflow | source_app | n | avg out | max out |
|---|---|---|---|---:|---:|
| gpt-4o-mini | single_turn_qa | `claude code` | 9 | 3 | 8 |
| gpt-4o-mini | single_turn_qa | `step1-gate` | 3 | 1 | 1 |
| gpt-4o-mini | single_turn_qa | `other-app` | 1 | 1 | 1 |

All thirteen sent `max_tokens: 12` and asked for the single word "ok". The
`step1-gate` and `other-app` rows are the routing **step 1** acceptance rule and
its control — created while verifying that work.

**Why it matters.** The same model's real output on the same workflow measures
**~48 tokens** (§3.2 of the design doc). A verbosity table built naively from
these events would price `gpt-4o-mini` roughly **16× cheaper than reality**, and
an `expected_cost` router would send it everything — a measurement-driven router
made *worse* than the guess it replaced.

**What changed.** Synthetic exclusion became a prerequisite rather than a
refinement, with two mechanisms: `TAS-Synthetic` **mark-at-source** (exact,
survives renaming) and a shared **source-app denylist** as the interim for rows
predating the marking. The event records *which* fired, so the denylist's
contribution can be watched as it shrinks. **If the denylist is still growing a
year from now, mark-at-source was never adopted.**

---

## 2. Nearly half of all responses are truncated, and truncation is not verbosity

**Measured 2026-08-21, 30-day window.**

| finish_reason | n | avg completion tokens |
|---|---:|---:|
| `stop` | 248 | 123 |
| **`length`** | **226** | **144** |
| `tool_calls` | 4 | 54 |
| `end_turn` | 3 | 9 |

**47% of events hit `max_tokens`.** For those, `completion_tokens` is **our own
cap**, not the model's natural verbosity — so including them measures our
settings rather than the model. Note the direction: truncated responses average
*higher* (144 vs 123), which means the tighter we cap, the more verbose every
model would appear.

**What changed.** Natural verbosity is measured from completed responses only;
`max_tokens` re-enters at estimate time as a **cap**, because a truncated answer
is still an answer you paid for.

---

## 3. The verbosity table is empty in practice, and that is the honest state

**Measured 2026-08-21**, 7-day window with all exclusions applied
(synthetic, truncated, failed):

| model | workflow | n | mean out | sd |
|---|---|---:|---:|---:|
| claude-haiku-4-5 | single_turn_qa | 10 | 212 | 90 |
| claude-haiku-4-5 | (none) | 8 | 297 | 45 |
| claude-haiku-4-5 | summarization | 4 | 315 | 13 |

Against a sample floor of **100**, nothing qualifies. Over 30 days one cell
does — `claude-haiku-4-5 / single_turn_qa`, n=412.

**So `expected_cost` abstains on all live traffic today.** That is the correct
behaviour, not a defect to tune away: routing money on a handful of requests is
how a measurement-driven router becomes worse than the guess it replaced.

**The window is a real tradeoff, deliberately left unresolved.** 7 days keeps a
model's behaviour change visible; 30 days is the only way to clear the floor at
current volume. The default is 7 days and configurable, rather than quietly
widened to make the table look populated.

**Consequence for verification.** Step 5's acceptance criterion — *"expected-cost
differs from list-price ranking on a known case"* — can only be demonstrated
against a seeded or labelled dataset, not live traffic. It is asserted in tests
using real published prices and the measured Opus/Haiku figures.

---

## 4. The prompt-cache P0 probe has too little data to pick a default

**Measured 2026-08-20, 30-day window, Loki.**

A naive count of lines matching `"prompt-cache probe"` returns **72**. Only
**16** are measurements — the other 56 are the **startup banner**, which is
emitted once per pod boot. Counting those gives a wrong denominator and a
meaninglessly low reuse rate.

Of the 16 real measurements:

- 2 showed prefix reuse (12.5%)
- mean cacheable prefix **613 tokens**, against a per-model minimum of
  **1,024–4,096** tokens
- only 2 measurements reached 1,024 tokens at all

**Reading.** This is §10.1's third case — *most traffic has no cacheable span at
all* — not evidence that caching would not pay.

**What changed.** The prompt-cache default is `passthrough`, not `auto`. Nowhere
near enough evidence to justify the gateway rewriting every caller's
breakpoints.

**Known gap, unfixed.** The probe records `prefix_seen` and `prompt_tokens` but
nothing distinguishing *"no cacheable span existed"* from *"a span existed and
did not recur"*. Those read identically and mean opposite things. **Fix this
before anyone uses probe data to decide on `auto`.**

---

## 5. Where the verbosity table lives — open question 6, settled

**dashboard-be measures the TOKENS; the gateway applies the PRICES.**

The gateway cannot query Timescale on the hot path, and must not: a routing
decision that depends on an analytics database being up is a routing decision
that fails when analytics does. So the table is computed on a schedule and
shipped through `resolve`.

The question worried about *"two services disagreeing about price"*. Splitting it
this way means only **one** service knows prices — the one computing cost — so
there is nothing to disagree about. The gateway reads prices from its own
capability matrix, which is where they already live.

The table rides on resolve **only when a rule asks for `expected_cost`**;
attaching it universally would add weight to every response for a feature almost
no rule uses.

---

## Re-running these

```sql
-- 1 & 3: verbosity with every exclusion
SELECT model, COALESCE(NULLIF(workflow,''),'(none)') wf, COUNT(*) n,
       ROUND(AVG(completion_tokens)) mean_out,
       ROUND(COALESCE(STDDEV_SAMP(completion_tokens),0)) sd
  FROM aiqg.event_metrics
 WHERE time > NOW() - INTERVAL '30 days' AND status='success' AND completion_tokens > 0
   AND COALESCE(finish_reason,'') NOT IN ('length')
   AND COALESCE((raw->'data'->>'synthetic')::boolean,false) = false
 GROUP BY 1,2 ORDER BY n DESC;

-- 2: truncation share
SELECT COALESCE(NULLIF(finish_reason,''),'(none)'), COUNT(*), ROUND(AVG(completion_tokens))
  FROM aiqg.event_metrics
 WHERE time > NOW() - INTERVAL '30 days' AND completion_tokens > 0
 GROUP BY 1 ORDER BY 2 DESC;
```

For 4, filter Loki on `prefix_seen != ""` — **not** on the message text, or the
startup banner inflates the denominator.
