# Workload Model Fit — UI design

**Date:** 2026-09-05 · **Status:** design, nothing built · **Scope:** `aiqg-ui` only. Companion to [`model-fit.md`](./model-fit.md), which deferred UI deliberately.

> **The question every screen here answers.** Not *"what did the model do?"* but *"do I believe this number enough to change my routing on it?"* Every surface below is therefore judged on whether it makes an argument checkable, not on whether it looks complete.

---

## 1. Three principles this inherits, and does not get to re-litigate

`aiqg-ui` has already settled the hard parts of how this kind of screen behaves. All three are load-bearing here, and all three are quoted from the code rather than invented for this document.

**Absence is rendered as loudly as presence.** `ModelEconomicsPage.tsx`: *"Today almost nothing clears the sample floor… A page that showed only the rows it had would look like a working system with a small table."* The class space will spend its first weeks mostly unmeasured, so this is not a nicety — it is the difference between a screen that is honest on day one and one that is misleading precisely when a customer is deciding whether to trust it.

**A draft is not an action, and the missing button is the feature.** `Recommendations.tsx`: *"There is no apply button here, and the absence is deliberate — a system that can silently act on its own advice is a system nobody can hand to a compliance officer."* The promotion draft (§2.5) is the one place this gets tested, because it is the first surface where accepting advice *does* change routing.

**Two kinds of nothing are different facts.** `Recommendations.tsx` again: *"'Nothing to recommend' and 'three of five checks could not run' are different facts, and only one of them means things are fine."* Applied here: a class with no verdict because it is still collecting is not a class with no verdict because nothing separates.

---

## 2. Five surfaces

Four extend pages that already exist. One is new. Nothing is greenfield.

### 2.1 Workloads — the class space *(new page, `/workloads`, Analysis section)*

**Where and why there.** Analysis, after Traffic Explorer. Analysis is where an operator goes to understand *their own traffic*; Reports are periodic artifacts about it. A class space is a description of the traffic, so it belongs beside the Explorer that shows the traffic itself.

**What it must show, per class:**

| Column | Why it is not optional |
|---|---|
| Label + description | A class nobody can name cannot be approved as a route rule (`model-fit.md` §4.2) |
| Origin — `global` / `declared` / `clustered` / `residual_variance` | Provenance is the difference between a hypothesis and a finding |
| Status — proposed / testing / promoted / merged / retired | The lifecycle *is* the argument |
| Evidence | `confirmed on your traffic, 12 Aug, n = 4,200` **or** `from the global prior — never tested on your traffic` |
| Volume + share of spend | A class too small to matter is a distraction even when real |
| Time-to-verdict, when testing | The honest answer to "when will I know?" |

**The state that matters most is the one with no evidence.** A `global`-origin class with no local `separation_evidence` must read visibly differently from a confirmed one — not a subtler shade of the same row. The backend design commits to this in prose (*"the UI must be able to say 'this class came from the global prior and has never been tested on your traffic'"*); this is where that promise is kept or broken.

**What it must not do.** It must not sort promoted classes to the top and let proposals fall off the fold — the proposals are where the next experiment comes from. And it must not show a count of classes as a progress metric; more classes is not better, and §4.2's merge pressure runs both ways.

**Reuses:** the tri-state rendering vocabulary from `ModelEconomicsPage` (measured-and-steering / below-floor / stale) maps almost one-to-one onto (promoted / testing / untested-global).

### 2.2 Retro-labelling — inside Traffic Explorer *(extends `/traffic`)*

**This is not a view. It is how ground truth enters the system**, and it is the surface the whole plan leans on (§4).

The Explorer is Wireshark-shaped — capture, display filter, packet list, detail — and already has a filter DSL (`parseFilter`) plus an AI-assisted filter builder (`FilterAssistDialog`). Retro-labelling is one step on the end of a flow the operator already performs:

```
filter traffic  →  "Name this as a workload"  →  system learns a rule that reproduces the selection
                →  shows precision / recall against the selection  →  accept, adjust, or abandon
```

**The precision/recall readout is the product, not a diagnostic.** If no rule reproduces the operator's selection, that *is* the finding — the segment is not structurally distinguishable, and they should see why (which features came closest, and what would separate it). A labelling flow that silently accepts an unlearnable segment produces a class that can never be classified inline, which fails later and further away.

**What it must not do.** It must not require the operator to understand the feature vector. They name what they know — *"our nightly contract-extraction job"* — and the system does the fitting.

**Reuses:** the existing filter DSL as the segment definition; `FilterAssistDialog`'s dialog pattern for the naming step.

### 2.3 Active learning — a bounded ask *(extends `Recommendations`)*

*"Label these 20 requests and we can tell these two workloads apart."* It belongs in the recommendations inbox because that is already the one surface that **comes to the operator** rather than waiting to be found — `Recommendations.tsx` says so in its own header.

Three rules: the ask is **bounded** (a count, not a queue that never empties), it **states what the answer buys** (which two classes get separable), and it is **declinable without penalty** — an unlabelled system must degrade to the global prior, not to a nag.

### 2.4 Model fit — extend Model Economics *(extends `/reports/model-economics`)*

Not a new page. That page already exists to make the routing decision checkable, and already draws the distinction that matters: *"'Cheaper per token' and 'cheaper per request' are different claims, and only the second is the bill."*

The change is one dimension: today it is `(model, workflow)`; it becomes `(model, workload_class)`, with a class selector. Plus three additions:

- **Evidence tier** on every row — `proxy_live` / `imported_trace` / `offline_corpus`, since only the first may gate a live decision.
- **Outcome-signal rates with their coverage**, never a rate alone. A model that proposes no edits has a perfect edit-applied rate.
- **`NULL` rendered as "not observed", never as `0`.** The backend keeps these distinct at some cost; a UI that prints `0%` for an unmeasured rate throws that away at the last step and produces a confident lie.

**The audit's "orphaned page" finding is now stale** — `nav.ts:89` lists Model economics under Reports. Worth noting so nobody re-fixes it.

### 2.5 Candidate suggestions and the promotion draft *(extends `Recommendations` + `/experiments`)*

A candidate suggestion carries the projected saving, the **powered** sample size, and a time-to-verdict — or an abstention naming what is missing. Accepting it prefills `ExperimentWizard` with cohort = the class, control = incumbent, variant = candidate, in `dry_run`.

The **promotion draft** is the first surface in this system where accepting advice changes routing, and it is where the "drafts, never actions" principle gets its real test. It shows: the rendered route-rule patch, the dry-run diff, the verdict and its evidence frozen at the time it was produced, and who applied it. A human with the admin role applies.

> **Blocker, inherited.** `AIQG_ROUTING_UI_COVERAGE.md` gap #1: **every advanced route-rule lever is create-only.** The existing-rules table edits priority and enabled and nothing else, even though `PATCH /route-rules/:id` accepts every field. A promotion draft that must modify a live rule therefore has nowhere to land, and today the only path is delete-and-recreate — which loses the rule's identity and its audit trail. **This must be fixed before P5, and it is worth fixing regardless**; it is the single biggest gap in the routing UI already.

---

## 3. What each phase can honestly show

The strong temptation is to build the Workloads page first because it is the most legible. That would produce a screen that renders an untested global prior for weeks, which is exactly the failure mode §1 exists to prevent.

| Phase | Backend delivers | UI can honestly show | Do not build yet |
|---|---|---|---|
| **P0** | features + seed classes + settled classification | nothing customer-facing. Validation output is a confusion matrix in the PR, not a page | the Workloads page — every class would read `untested` |
| **P1** | coding adapter + fit table on imported traces | **Model Economics gains the class dimension**, tier-labelled `imported_trace`. First real answer to "where does our coding spend go" | anything implying these gate routing |
| **P2** | candidate generation + suggestions | candidates in `Recommendations`, with abstentions | a promote button |
| **P3** | shadow at class granularity | verdicts on the Experiments page; **shadow cost now measured**, not modelled (tas-llm-router#190) | — |
| **P3.5** | labelling API + segment-rule learner | **Retro-labelling in Traffic Explorer** + active-learning asks | — |
| **P4** | discovery + separation test | **Workloads page** — now it has splits, merges and evidence to show | — |
| **P5** | promotion draft | the draft + dry-run diff + audited apply | auto-apply, in any form |

Two consequences worth stating plainly. **The first UI work is an extension, not a page** — P1 adds a dimension to a page that exists. And **the Workloads page is P4**, because until the separation test runs it has nothing to say that a static list of seed classes could not say more honestly.

---

## 4. The load-bearing surface

Of the five, **retro-labelling is the only one that is not optional**, because it is not a view.

P3.5's exit criterion is *the first confusion matrix measured on a customer's own traffic rather than a public corpus.* That measurement requires ground truth from the customer. Ground truth from the customer requires a way for them to give it. There is no other path — no amount of clustering produces a label, and validating against SWE-chat measures our corpus, not their traffic.

It is also the commercial argument in the design that the UI actually delivers: **retro-labelling requires no change to the customer's application** — no header, no SDK upgrade, no redeploy. That claim is made in `model-fit.md` §4.10 and can only be honoured by this screen.

---

## 5. Corrections to the backend design

**Prices are already published, and the UI already fetches them.** `model-fit.md` §4.6 says *"the gateway must publish prices"* as a required change. It already does: `GET /v1/capabilities` returns `input_cost_per_1k` and `output_cost_per_1k` per model, and `dashboardApi.fetchModelPrices` reads exactly that — Model Economics already joins backend tokens against gateway prices client-side.

So the `/v1/pricing` decision stands, but for a narrower and more precise reason than recorded: **not to expose prices, but to version and effective-date them**, so a cost delta approved last month is recomputable when prices move. The backend design should be amended to say so rather than implying prices are unavailable today.

---

## 6. Open questions

1. **Is Workloads a page or a tab of Model Economics?** A separate page risks two places to reason about the same partition; a tab risks burying the class space inside a report. Leaning separate, under Analysis, because the class space describes traffic rather than reporting on it — but decide once there is a real class list to look at.
2. **Where does a class's *spend* come from on the Workloads page?** Joining fit rows to prices client-side duplicates Model Economics' join. Probably a backend rollup, but it is not needed before P4.
3. **How is an unlearnable segment presented?** The precision/recall readout has to explain a negative result without teaching feature engineering. Needs a real example from P3.5 data before it can be designed.
4. **Does the promotion draft live on the Experiments page or in Policies?** The verdict is on Experiments; the thing being changed is a route rule in Policies. Following the change rather than the evidence probably wins, but this depends on the create-only fix in §2.5.
