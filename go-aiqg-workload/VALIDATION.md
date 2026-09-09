# Seed class space — validation against SWE-chat

**Date:** 2026-09-06 · **Corpus:** `SALT-NLP/SWE-chat` (5,851 real coding sessions from Claude Code, Codex and Gemini CLI; ODC-BY) · **Unit:** labelled prompt segment · **Artifact:** `seed-1`, extractor `fx-1`

P0's exit criterion is a published confusion matrix against labelled intents, abstentions included. This is it, including the parts that do not flatter the classifier.

## What was compared

SWE-chat labels `prompt_intent` on user prompts. A *segment* is one labelled prompt plus every turn until the next one — so a segment aggregates all the tool calls the agent made in service of that prompt. Expected mapping follows the taxonomy's Axis-1:

| `prompt_intent` | expected class | note |
|---|---|---|
| `understand` | `code.discovery` | |
| `refactor` | `code.modification` | |
| `create new code` | `code.modification` | generation folds in — not separable |
| `test` | `code.execution` | validation not separable from execution |
| `git` | `code.execution` | |
| `debug`, `other`, `connect`, `review` | — | no class expected |

## Two numbers, and only one of them means anything

**Unconditioned: 19.9%** agreement across all 60,979 labelled segments. This number is **not a measurement of the classifier**. 44.2% of labelled segments contain no tool call at all, and 40.3% carry no token counts — the corpus's per-turn usage fields are documented as sparse. A tool-family classifier cannot classify a segment with no tools, and token rules cannot fire on absent tokens.

**Conditioned on tool-bearing segments: 33.9%** (7,226 / 21,300). This is the honest headline, and it is not good.

## The matrix

| `prompt_intent` | n | top predictions | expected class hit |
|---|---:|---|---:|
| `git` | 6,164 | execution 74% · unclassified 21% | **73.7%** |
| `other` | 6,664 | unclassified 40% · execution 33% | — |
| `debug` | 5,656 | unclassified 43% · execution 26% · discovery 23% | — |
| `create new code` | 5,386 | unclassified 56% · execution 17% | **7.3%** |
| `understand` | 4,983 | unclassified 30% · execution 29% · discovery 29% | **28.6%** |
| `refactor` | 3,160 | unclassified 55% · discovery 16% · modification 15% | **14.6%** |
| `test` | 1,607 | unclassified 48% · execution 25% | **25.3%** |
| `connect` | 414 | unclassified 36% · execution 31% | — |

## The finding: dilution, not absence

Splitting "is the expected family *present*" from "is it *dominant*" locates the problem exactly:

| intent | expected family present | present at ≥60% |
|---|---:|---:|
| `git` | 93.3% | 73.7% |
| `refactor` | 78.6% | 14.6% |
| `test` | 73.6% | 25.3% |
| `create new code` | 65.4% | 7.3% |
| `understand` | 62.1% | 28.6% |

**The signal is there. The threshold is wrong for the unit.** 78.6% of `refactor` segments contain an edit; only 14.6% are 60% edits. A prompt segment is a *mixture* of activities — read, grep, run, edit — so a dominance threshold tuned for a single turn dilutes to nothing when aggregated over a whole prompt.

Three consequences, all actionable:

1. **The deployment unit is a turn, not a segment.** The gateway classifies one request. SWE-chat's per-prompt labels are therefore not the classifier's native unit, and this validation measures a granularity mismatch alongside accuracy. It is still worth having: it establishes the family signal exists in 62–93% of segments.
2. **Segment/session archetype needs a different rule than turn classification** — either a per-turn roll-up (plurality of turn classes) or a precedence rule over families present ("any edit ⇒ modification"), which matches the taxonomy's own claim that Modification is the dominant coding-agent action. Not a threshold tweak.
3. **`git → execution` at 73.7% is the one clean mapping**, and it is clean because git is done through exactly one family. Everything else is done through several.

## What this does not show

It does not show the classifier is accurate at its native granularity — no per-turn labelled corpus exists, which is precisely the gap customer labelling (P3.5) exists to fill. It does not validate the non-coding archetypes at all: SWE-chat is coding-only. And the personas are simulated (Vague Requester, Mind Changer, Expert Nitpicker), so this is a benchmark, not organic production traffic.

## Reproducing

```
go test . -run TestSWEChatConfusionMatrix -v \
  SWECHAT=/path/to/swechat_segments.jsonl
```

The test skips without the corpus. Segments are extracted from the parquet with the DuckDB query in this repo's PR discussion; the corpus itself is gated (free HF account) and is not vendored here.

---

# Coding outcome adapter — first fit table from real traces

**Date:** 2026-09-07 · **Corpus:** 52 local Claude Code transcripts, 15,283 assistant turns · **Adapter:** `coding-1`

The P1 question was *"which coding tasks does our own spend go to, and where do they fail?"* This is the first answer. Aggregated by class, ordered by output tokens — the 5×-priced side of the bill.

| class | turns | output tokens | % of output | out/turn | outcome rates (coverage) |
|---|---:|---:|---:|---:|---|
| unclassified | 8,334 | 11,858,086 | **59.3%** | 1,423 | repair 0.67 (n=6) · correction 0.98 (n=8,084) |
| code.execution | 4,773 | 4,319,484 | 21.6% | 905 | **command_exit 0.98 (n=4,773)** · repair 1.00 (n=82) |
| code.modification | 1,281 | 2,260,532 | 11.3% | 1,765 | **edit_applied 0.98 (n=1,281)** · repair 0.67 (n=28) |
| code.orchestration | 262 | 1,086,056 | 5.4% | **4,145** | correction 0.96 (n=257) |
| code.discovery | 494 | 359,640 | 1.8% | 728 | repair 1.00 (n=1) |
| conversation | 126 | 105,252 | 0.5% | 835 | — |
| summarization | 13 | 3,891 | 0.0% | 299 | — |

## Three findings

**1. `user_correction` is saturated and cannot gate anything.** It scores 0.96–1.00 in every single class over n=8,084 observations. An input with no variance cannot drive a decision — which is *exactly* the defect that makes CLEAR Efficacy unusable (`stop` and `tool_calls` both score 100), reproduced in a signal this design added to fix it. It was already the weakest of the five by construction; it is now measured weak, and it should be reported for coverage but never gate.

**2. `repair_loop` is coverage-starved by construction.** It fires only after a failure, and failures are rare — 2% — so it collected n=28 on modification and n=6 on unclassified against 15,283 turns. It is a real signal about a rare event, not a routine one, and treating it as a per-class rate will mislead. It belongs in an incident view, not a fit table.

**3. `edit_applied` and `command_exit` are the two that work** — full coverage on their classes (n=1,281 and n=4,773) at 0.98. The 2% failure rate is small but it is the discriminating quantity: 2% versus 6% is a threefold difference in rework, and it is measurable per model. These two carry the objective-quality claim; the other three do not.

## The routing finding

**Orchestration turns cost 4,145 output tokens each — 4.6× an execution turn and 2.3× a modification turn** — while being only 1.7% of turns. Output is the 5×-priced side, so a small number of delegation turns carries 5.4% of the output bill. That is a class worth routing deliberately, and it is invisible in any `(model, workflow)` table because all of it collapses into `agentic`.

**And 59.3% of output spend is unclassified.** The abstention rate now has a cost attached rather than a turn count, which is a much sharper statement of what labelling and discovery are for: the majority of the bill sits in traffic the structural vector cannot place.

## Caveats

Single developer, near-single model, so cross-model discrimination is untested — that needs the gateway or a second model's traces. The 0.98 rates are one model's competence, not a benchmark. `session_completed` is absent from the table because these sessions commit through a shell in ways the commit cue sees inconsistently; it needs work before it is trustworthy.

---

# Segment roll-up — choosing the rule by measurement

**Date:** 2026-09-07 · **Corpus:** SWE-chat, 34,036 segments re-extracted at per-turn granularity

The earlier validation found the turn threshold was wrong for the segment unit and said the fix was a roll-up rather than a tuned threshold. Rather than pick one, both candidates were implemented and measured on the same labelled data.

| `prompt_intent` | n | flat (one observation) | plurality | **precedence** |
|---|---:|---:|---:|---:|
| `git` | 6,164 | 73.7% | **83.4%** | 67.2% |
| `create new code` | 5,386 | 7.3% | 18.2% | **65.4%** |
| `understand` | 4,983 | 28.6% | **43.2%** | 21.2% |
| `refactor` | 3,160 | 14.6% | 25.4% | **78.6%** |
| `test` | 1,607 | 25.3% | **43.3%** | 21.7% |
| **overall** | **21,300** | **33.9%** | **45.9%** | **54.2%** |

**Both roll-ups beat the flat approach decisively** — 33.9% → 45.9% / 54.2%. The unit mismatch was the dominant error, exactly as the earlier validation argued.

**Precedence wins overall and is not uniformly better.** It gains enormously on edit intents (refactor 25.4% → 78.6%, create 18.2% → 65.4%) and loses on non-edit ones (git 83.4% → 67.2%), because an agent that edits incidentally while running commands has its segment renamed a modification. It is chosen anyway, for two reasons: it is the taxonomy's own principle (Modification is the dominant coding-agent action), and modification is the class a routing decision most needs to get right.

**A gated hybrid was built and lost.** Precedence-above-a-minimum-share was measured at four thresholds and beat by pure precedence at every one:

| min share | 0.15 | 0.25 | 0.35 | 0.50 |
|---|---:|---:|---:|---:|
| overall | 53.9% | 52.6% | 48.7% | 46.8% |

It was removed rather than left as a knob someone could turn on. A configuration option that measured worse is a liability, not flexibility.

## What this changes

Turn classification keeps its strict threshold — the segment problem was never a reason to loosen it. Segment and session archetype use `RollUp(..., RollUpPrecedence)`, and carry **classified coverage as their confidence**, so an archetype resting on one classified turn in ten says so.

---

# Rule learner — validated against SWE-chat, and it refuses

**Date:** 2026-09-08 · **Corpus:** SWE-chat, 60,979 labelled segments · **Learner:** greedy conjunction, max 3 conditions, floors precision ≥ 0.8 / recall ≥ 0.5

The learner turns a labelled *selection* into a *predicate*, because a set cannot classify tomorrow's requests. It was run against every SWE-chat intent with ≥400 segments, treating that intent's segments as the selection and everything else as negatives.

| selection | n | precision | recall | F1 | usable | closest rule |
|---|---:|---:|---:|---:|---|---|
| `other` | 15,980 | 0.33 | 0.68 | 0.45 | **no** | readonly share ≤ 0 and share exec ≤ 0.84 |
| `understand` | 11,618 | 0.27 | 0.61 | 0.38 | **no** | tool count ≤ 2 and depth ≥ 6 and share edit… |
| `git` | 8,595 | 0.48 | 0.45 | 0.47 | **no** | share exec ≥ 0.66 and depth ≤ 49 |
| `debug` | 8,097 | 0.22 | 0.47 | 0.30 | **no** | readonly share ≥ 0.12 and share other ≤ 0 |
| `create new code` | 7,896 | 0.22 | 0.43 | 0.29 | **no** | tool count ≥ 3 and share exec ≤ 0.66 |
| `refactor` | 5,724 | 0.20 | 0.26 | 0.22 | **no** | dominant family is edit |
| `test` | 2,509 | 0.08 | 0.21 | 0.11 | **no** | tool count ≥ 9 and share edit ≤ 0.31 |
| `connect` | 558 | 0.03 | 0.07 | 0.04 | **no** | share search ≥ 0.33 and share exec ≤ 0 |

**Not one is learnable.** The best is `git` at F1 0.47, and it still fails both floors.

## This is the learner working, not failing

A learner that always returns something is worse than one that admits defeat, because the failure then surfaces months later as a class that never behaved as its name implied. Every row above returns the closest approximation *and* a sentence saying it is not good enough — which is the designed behaviour and the reason `Usable` exists as a separate field from the rule itself.

It is also consistent with everything measured before it: 33.9% seed-classifier agreement on tool-bearing segments, and the present-vs-dominant split showing the expected family present in 62–93% of segments but dominant in far fewer. Three independent methods now agree that **structure alone does not reproduce semantic intent at segment granularity.**

## What it does not show — and this matters for the product

**SWE-chat is the hard case, not the representative one.** Its labels are *semantic*: what the user was asking for. Every segment is a coding session in the same harness, so the workloads differ by intent while looking structurally alike. That is close to the worst case for a structural learner.

The customer case is different in kind. An operator naming *"our nightly contract-extraction job"* is selecting on `source_app`, a path, or a schedule — attributes that are **structurally distinct by construction**, because different applications genuinely send differently shaped traffic. SWE-chat cannot test that case at all: it has no source-app dimension.

So this result is a **lower bound**. It says: a rule learned from tool structure alone will not reconstruct a semantic intent. It does not say the learner will fail on an operational segment, and it should not be quoted as if it did.

## The design change it points to

The feature vector deliberately excludes attribution — `source_app`, path, principal — because those are customer data that must never reach pooled Tier-C aggregates. That exclusion is right for the *shared* layer and **wrong for local rule learning**: within a tenant, those fields never leave, and they are exactly the discriminative features an operator's filter selects on.

The follow-up is therefore a split the design does not currently make: **poolable features** (structural, shareable as centroids) versus **tenant-local features** (attribution, usable for learning, never shared). The learner should see both; Tier C should see only the first. Recorded here rather than built, because it should be designed against a real customer segment rather than guessed at.
