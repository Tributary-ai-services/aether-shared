# AIQG Extraction Policy (`PolicyBundle.Spec` shape)

---

**Metadata**

```yaml
service: aiqg-dashboard-be (stores/serves) + tas-llm-router (consumes)
model: ExtractionPolicy  (the `reduction` block of PolicyBundle.Spec)
database: PostgreSQL — aiqg.policy_bundle.spec (JSONB, already exists)
version: 1.0.0 (Contract v1 — authored for Plan #7 Phase 2)
last_updated: 2026-06-17
status: authored (Phase 0); consumed in Phase 2 (resolver + gateway), edited in the wizard (Phase 2 UI)
related: policy-bundle.md, policy-rule.md, route-rule.md, token-accounting.md, experiment.md
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #7 Appendix A
```

---

## 1. Overview

### Purpose
An **Extraction Policy** is the `reduction` block of a [[policy-bundle]]'s opaque `spec` (JSONB). It configures the **Gatekeeper payload-reduction pipeline** — chunking, relevance/top-K filtering, and SLM rewrite — that the gateway can run on a request's context *before* the vendor call, turning the **projected** cost savings (CLEAR v0.2 — see [[token-accounting]] §3.4) into **measured** savings.

Phase 1 ships projected savings on 100% of traffic (always-on heuristic, no extraction). This spec is what Phase 2 reads to actually *run* extraction — policy-gated, per-bundle, in `shadow` (sampled, measured-not-applied) or `active` (applied) mode.

### Ownership
- **Stored by** `aiqg-dashboard-be` — `aiqg.policy_bundle.spec` (the column already exists; today it's persisted opaque, not parsed).
- **Served by** the resolver: `POST /internal/policy/resolve` returns the resolved bundle; Phase 2 adds the parsed `reduction` config to the response.
- **Consumed by** `tas-llm-router` — replaces the hardcoded `EnableExtraction=false` (`internal/gatekeeper/gatekeeper.go`) with the resolved policy; `Gatekeeper/pkg/extract` runs the configured steps.
- **Edited by** `aiqg-ui` — an extraction editor in the policy-bundle wizard (Phase 2 UI). The bundle `spec` is currently never shown.

---

## 2. Schema

The `reduction` block (other `spec` keys are reserved for future bundle-level config):

```jsonc
{
  "reduction": {
    "mode": "shadow",            // off | projected | shadow | active
    "min_tokens": 8000,          // skip extraction below this prompt size → Gatekeeper MinContentSize
    "sample_rate": 0.05,         // fraction of eligible traffic run through the REAL extractor (shadow)
    "steps": {
      "chunking":  { "enabled": true,  "size": 512, "overlap": 50 },
      "relevance": { "enabled": true,  "threshold": 0.30, "top_k": 100, "top_k_ratio": 0.30, "embed_model": "all-MiniLM-L6-v2" },
      "slm":       { "enabled": false, "model": "phi-3.5-mini", "max_tokens": 4096 }
    }
  }
}
```

### 2.1 Field reference

| Field | Type | Description |
|---|---|---|
| `mode` | enum | `off` (no extraction; projected savings still computed). `projected` (Phase 1 default — heuristic only, the implicit state when no spec). `shadow` (run the real extractor on `sample_rate` of traffic, **record** measured reduction + quality, but **do not** apply it to the vendor call — safe calibration). `active` (apply the reduction — real token drop). |
| `min_tokens` | int | Don't extract below this prompt token count — small prompts have nothing to gain. Maps to Gatekeeper `MinContentSize`. |
| `sample_rate` | float 0–1 | In `shadow` mode, the fraction of eligible requests routed through the real extractor (the rest stay projected-only). Ignored in `active`. |
| `steps.chunking` | object | **Tuning parameter, not a savings lever** (overlap duplicates bytes → ~0 standalone savings). `size`/`overlap` shape how relevance/SLM operate. |
| `steps.relevance` | object | Relevance / top-K context filtering. `threshold` (cosine cutoff), `top_k` / `top_k_ratio` (cap retained chunks), `embed_model`. The primary real reducer. |
| `steps.slm` | object | Small-language-model rewrite/compression of retained context. `model`, `max_tokens`. The secondary reducer (low projected confidence until measured). |

### 2.2 Validation
- `mode` ∈ {off, projected, shadow, active}; absent `spec`/`reduction` ⇒ treated as `projected` (Phase 1 behavior).
- `sample_rate` required and >0 when `mode=shadow`; ignored otherwise.
- At least one of `relevance`/`slm` `enabled` when `mode ∈ {shadow, active}` (chunking alone saves ~nothing).
- Each method independently configurable + comparable in an [[experiment]] (variants set `override.extraction = <this spec>`).

---

## 3. Lifecycle

`off → projected (default) → shadow (sampled, measured) → active`, promoted deliberately:
1. **projected** (Phase 1, always-on): heuristic savings on every request; nothing runs.
2. **shadow** (Phase 2): real extractor on `sample_rate`; emits `actual_reduction_*` + quality deltas (Contract v2 fields) for projected-vs-actual calibration; vendor call unchanged.
3. **experiment** ([[experiment]]): A/B an extraction method as a variant — measured savings **and** quality delta side-by-side — to decide the cost×quality tradeoff.
4. **active** (Phase 4): apply the validated config; real token reduction; drift/spend-cap alerts watch it.

This mirrors the experiment `status` machine (the real lifecycle in code); route rules today carry only an `enabled` flag (no mode) — see [[route-rule]].

---

## 4. Relationships
- Lives inside [[policy-bundle]] `spec.reduction`; a bundle is targeted at traffic via [[route-rule]]s (shipped) and/or the explicit `TAS-Policy-Bundle` header.
- Produces the **measured** counterparts of the **projected** [[token-accounting]] §3.4 fields (Contract v2: `actual_reduction_{relevance,slm}_usd`, quality deltas).
- Compared per-method in an [[experiment]] before promotion to `active`.

---

## 5. Status / not-yet-built
Authored as the Phase 0 contract. **Not yet consumed:** the resolver doesn't parse `spec.reduction` and the gateway still hardcodes `EnableExtraction=false`; the wizard doesn't render an extraction editor. Those land in Plan #7 Phase 2.
