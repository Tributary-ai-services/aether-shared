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
