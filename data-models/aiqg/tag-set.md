# AIQG Tag Set

---

**Metadata**

```yaml
service: gatekeeper (tag application) + aiqg-dashboard-be (storage/query)
model: TagSet
database: PostgreSQL (TimescaleDB) — `tags` JSONB column on request/response event tables
version: 1.0.0
last_updated: 2026-05-31
status: planned (additive — extends existing Gatekeeper tag mechanism)
spec_refs: source-spec-v0.2.md §2.4 (Assurance), §3.10 (match-once-tag-many)
plan_ref: build-vs-reuse.md §2.4 (Assurance/NIST mapping), §2.10 (Gatekeeper rule-pack reuse), §2.12 (CLEAR computation)
```

---

## 1. Overview

### Purpose
The `TagSet` is the **unified tag taxonomy** applied to request and response events by the Gatekeeper Hyperscan scanner running AIQG-specific rule packs. Tags are flat strings with a category prefix that record what was detected, classified, or computed for a given event. They are the lowest-cost, most queryable signal in AIQG: every dashboard tile, alert, scoring input, and audit search is — at the data layer — a `tags @>` query.

### Ownership
- **Tag producers**:
  - **Gatekeeper** (`pkg/scanner/`) — runs Hyperscan rule packs against request/response payloads and emits tags via "match once, tag many" (`pkg/attest/cache.go`)
  - **aiqg-dashboard-be** — applies derived tags after CLEAR scoring (`clear:*`) and policy evaluation (`policy:*`)
  - **tas-llm-router** — applies workflow-classification tags (`workflow:*`) from request metadata
- **Tag consumers**:
  - **aiqg-dashboard-be** (reports, drill-downs, search)
  - **tas-spark-jobs/aiqg_aggregator** (rollups into [[aggregated-metrics]])
  - **aiqg-ui** (filters, tag clouds, Top-N panels)
  - **Alerting** (Grafana / Prometheus rules over tag-frequency materialized views)

### Lifecycle Summary
Request-side tags are written at request receipt; response-side tags are written at response close. Once written to an event row, tags are **immutable** — corrections come via new event versions, not in-place rewrites. Categories may be deprecated but never renamed (see Migration).

### Key Characteristics
- **Flat strings** with category prefix — no nesting, no objects, no key-value pairs
- **Match-once-tag-many** (spec §3.10) — a single Hyperscan pass applies all matched tags
- **GIN-indexed JSONB** array on both [[request-event]] and [[response-event]]
- **Reuses existing Gatekeeper rule packs** (build-vs-reuse §2.10) — `compliance:`, `injection:`, `pii:` categories already exist and are not modified
- **NIST AI RMF aligned** — `nist:*` tags map 1:1 to the seven trustworthiness characteristics (build-vs-reuse §2.4)
- **HMAC-attested scan cache** — tag application uses Gatekeeper's existing security model

---

## 2. Schema Definition

### Storage
- **Database**: PostgreSQL with TimescaleDB extension (same instance as other AIQG event tables)
- **Column**: `tags JSONB NOT NULL DEFAULT '[]'::jsonb` — string array
- **Tables**: [[request-event]] and [[response-event]] both carry a `tags` column
- **Index**: GIN index on `tags` per table (required for `@>` containment queries)

### Tag Categories

The taxonomy is partitioned by **category prefix**. Each prefix has a single owning rule pack or computation source.

| Prefix | Category | Example tag values | Source rule pack |
|---|---|---|---|
| `quality:` | Quality assessment | `quality:validity_passed`, `quality:hedge_high`, `quality:refusal`, `quality:groundedness_low` | `aiqg_output_antipatterns.yaml` |
| `workflow:` | Workflow classification | `workflow:rag`, `workflow:agentic`, `workflow:single_turn_qa`, `workflow:tool_chain`, `workflow:summarization` | `aiqg_workflows.yaml` |
| `policy:` | Policy match | `policy:payload_reduce_eligible`, `policy:pii_detected`, `policy:over_token_budget`, `policy:dry_run` | bundle-specific (set by [[policy-rule]] application) |
| `nist:` | NIST AI RMF trustworthiness | `nist:safe`, `nist:secure_resilient`, `nist:privacy_enhanced`, `nist:accountable_transparent`, `nist:valid_reliable`, `nist:explainable_interpretable`, `nist:fair_bias_managed` — applied when a request/response satisfies (positive tag) or violates (negative tag like `nist:privacy_enhanced:violation`) the characteristic | `aiqg_clear_assurance.yaml` + existing NIST AI RMF pack |
| `clear:` | CLEAR dimension flag | `clear:assurance_pass`, `clear:assurance_fail`, `clear:efficacy_marginal`, `clear:cost_fail`, `clear:latency_pass`, `clear:reliability_fail` | computed by `pkg/clear/` per build-vs-reuse §2.12 |
| `compliance:` | Existing Gatekeeper compliance | `compliance:hipaa`, `compliance:gdpr`, `compliance:pci_dss`, `compliance:sox` | reused from existing Gatekeeper rule packs (build-vs-reuse §2.10) — non-breaking |
| `injection:` | Existing Gatekeeper injection detection | `injection:prompt_jailbreak`, `injection:sql`, `injection:control_chars` | reused |
| `pii:` | Existing Gatekeeper PII detection | `pii:email`, `pii:ssn`, `pii:credit_card`, `pii:phone`, `pii:name` | reused |
| `antipattern:` | New AIQG anti-patterns | `antipattern:context_bloat`, `antipattern:stale_context`, `antipattern:fragmented_chunks`, `antipattern:conflicting_instructions`, `antipattern:hedge_dense`, `antipattern:tool_def_ambiguous` | `aiqg_context_antipatterns.yaml` + `aiqg_prompt_antipatterns.yaml` + `aiqg_output_antipatterns.yaml` |
| `behavioral:` | Behavioral signals | `behavioral:retry`, `behavioral:abandonment`, `behavioral:conversation_continue`, `behavioral:tool_loop_step` | `aiqg_behavioral_signals.yaml` |

### NIST AI RMF Mapping (Detailed)

Per spec §2.4, the full mapping from NIST AI RMF trustworthiness characteristics to AIQG `nist:*` tags:

| NIST Characteristic | AIQG `nist:*` tag | What triggers it |
|---|---|---|
| Safe | `nist:safe` (pass) / `nist:safe:violation` | output content scan against prohibited categories |
| Secure and resilient | `nist:secure_resilient` / `:violation` | prompt-injection detection, jailbreak attempt classification |
| Privacy-enhanced | `nist:privacy_enhanced` / `:violation` | PII in input/output, tokenization compliance |
| Accountable and transparent | `nist:accountable_transparent` | per-request audit trail present in [[audit-log-entry]] |
| Valid and reliable | `nist:valid_reliable` | output passed [[response-structure]].structural_validity_passed AND clear:reliability not failing |
| Explainable and interpretable | `nist:explainable_interpretable` | optional — set when grounded with citations |
| Fair with harmful bias managed | `nist:fair_bias_managed` | optional — set when output passes bias-detection rule pack |

---

## 3. Fields Reference

A tag is a single string; there are no sub-fields. However, the **string itself** follows a structured grammar:

```
<category>:<value>[:<qualifier>]
```

| Position | Required | Description | Example |
|---|---|---|---|
| `category` | Yes | One of the prefixes from §2 | `nist` |
| `value` | Yes | Category-specific identifier | `privacy_enhanced` |
| `qualifier` | No | Modifier such as severity, polarity, or sub-type | `violation` |

Full example: `nist:privacy_enhanced:violation`.

The `tags` column itself is a JSONB array of these strings, e.g.:

```json
["workflow:rag", "quality:validity_passed", "nist:safe", "nist:valid_reliable"]
```

---

## 4. Validation Rules

### Naming Conventions

| Rule | Requirement |
|---|---|
| Case | All tags lowercase |
| Charset | `[a-z0-9_:]+` only |
| Structure | `category:value` or `category:value:qualifier` (max 3 colon-separated segments) |
| Max length | 64 characters per tag |
| Uniqueness | Tags within a single event's array are deduplicated at write time |
| Category whitelist | Categories must be defined in §2; new categories require schema update + rule-pack release |

### Validation Enforcement
- **Producer-side**: Gatekeeper rule packs are linted in CI against the category whitelist (`pkg/scanner/tag_lint.go`)
- **Database-side**: CHECK constraint optional (Phase 2) — `CHECK (jsonb_typeof(tags) = 'array')` is enforced; per-element regex validation is deferred to producer
- **Rejection policy**: Invalid tag strings emitted by a misconfigured rule pack are **dropped with a warning log** rather than failing the event write (availability over strict validation)

---

## 5. Relationships

The tag set itself is not a node — it is an **attribute** of event rows. Relationships are conceptual:

| From | To | Relationship | Cardinality |
|---|---|---|---|
| [[request-event]] | TagSet | `HAS_TAGS` (column) | 1:1 (array of N tags) |
| [[response-event]] | TagSet | `HAS_TAGS` (column) | 1:1 (array of N tags) |
| [[policy-rule]] | TagSet | applies `policy:*` tags | 1:N |
| [[aggregated-metrics]] | TagSet | rolls up tag counts | N:N (via materialized view) |
| [[audit-log-entry]] | TagSet | logs tag application events | N:N |
| [[workflow-classification]] | TagSet | emits `workflow:*` tags | 1:N |

---

## 6. Lifecycle & State Machines

Tags do not have their own state machine; they follow the lifecycle of the event they are attached to.

### Tag Write Timing

| Phase | Tags written | Producer |
|---|---|---|
| **Request receipt** | `workflow:*`, `policy:*` (input-eligible), `antipattern:context_*`, `antipattern:prompt_*`, `pii:*` (input), `injection:*` (input), `compliance:*` (input) | Gatekeeper + tas-llm-router |
| **Response close** | `quality:*`, `antipattern:output_*`, `behavioral:*`, `clear:*`, `nist:*`, `pii:*` (output), `compliance:*` (output) | Gatekeeper + aiqg-dashboard-be |

### Immutability
Once written, tags are **never modified in place**. If a downstream re-scan classifies an event differently, a new event version is created (see [[request-event]] / [[response-event]] versioning), and the original tags remain for audit reproducibility.

### Deprecation
Tags may be deprecated but never renamed (see §10 Migration). Deprecated tags continue to exist on historical events; new events do not receive them.

---

## 7. API Examples

### Query: events tagged with a specific NIST violation
```sql
SELECT id, tenant_id, occurred_at, tags
FROM aiqg.response_events
WHERE tenant_id = $1
  AND occurred_at >= NOW() - INTERVAL '7 days'
  AND tags @> '["nist:privacy_enhanced:violation"]'::jsonb
ORDER BY occurred_at DESC
LIMIT 100;
```

### Query: top antipattern tags for a tenant (last 7 days)
```sql
SELECT tag, COUNT(*) AS cnt
FROM aiqg.response_events,
     LATERAL jsonb_array_elements_text(tags) AS tag
WHERE tenant_id = $1
  AND occurred_at >= NOW() - INTERVAL '7 days'
  AND tag LIKE 'antipattern:%'
GROUP BY tag
ORDER BY cnt DESC
LIMIT 10;
```

### Query: weekly NIST violation count for Assurance scoring
```sql
SELECT
  date_trunc('week', occurred_at) AS week,
  COUNT(*) FILTER (WHERE tag LIKE 'nist:%:violation') AS violations,
  COUNT(*) AS total_events
FROM aiqg.response_events,
     LATERAL jsonb_array_elements_text(tags) AS tag
WHERE tenant_id = $1
  AND occurred_at >= NOW() - INTERVAL '90 days'
GROUP BY week
ORDER BY week DESC;
```

### REST: tag-filtered event search (aiqg-dashboard-be)
```
GET /api/v1/events?tags=workflow:rag,clear:assurance_fail&since=7d
Authorization: Bearer <tas_qg_live_*>

Response:
{
  "events": [
    {
      "id": "evt_01HXY...",
      "occurred_at": "2026-05-31T14:22:00Z",
      "tags": ["workflow:rag", "antipattern:fragmented_chunks", "clear:assurance_fail", "nist:valid_reliable:violation"]
    }
  ],
  "total": 47,
  "next_cursor": "..."
}
```

---

## 8. Cross-Service Integration

| Service | How it interacts with tags |
|---|---|
| **Gatekeeper** | Primary tag producer; runs Hyperscan rule packs; uses `pkg/attest/cache.go` for "match once, tag many" |
| **tas-llm-router** | Emits `workflow:*` tags from request metadata; consumes `policy:*` tags to make routing decisions |
| **aiqg-dashboard-be** | Computes `clear:*` and a subset of `nist:*` tags after scoring; serves tag-filtered queries |
| **tas-spark-jobs/aiqg_aggregator** | Reads tag arrays; produces `tag_frequency_daily` materialized view; feeds [[aggregated-metrics]] |
| **aiqg-ui** | Renders tag clouds, filters, and Top-N panels; tag categories drive UI color coding |
| **Alerting (Grafana)** | Threshold rules over `tag_frequency_daily` for `nist:*:violation`, `antipattern:*`, `clear:*_fail` |

### Day-1 Trustworthiness Section
The Day-1 customer dashboard's Trustworthiness section is driven by counts of `nist:*:violation` tags rolled up per characteristic per week.

---

## 9. Performance Considerations

### Indexing
- **GIN index** on `tags` JSONB column on both event tables is **required** for any `tags @> ARRAY['nist:safe']`-style query. Without it, queries fall back to full scan and become unusable at production scale.
  ```sql
  CREATE INDEX idx_request_events_tags ON aiqg.request_events USING GIN (tags);
  CREATE INDEX idx_response_events_tags ON aiqg.response_events USING GIN (tags);
  ```

### Materialized View for Top-N
For dashboards showing top tags by frequency, an inline `jsonb_array_elements_text` unnest per query is expensive. Use:
```sql
CREATE MATERIALIZED VIEW aiqg.tag_frequency_daily AS
SELECT
  tenant_id,
  date_trunc('day', occurred_at) AS day,
  tag,
  COUNT(*) AS cnt
FROM aiqg.response_events,
     LATERAL jsonb_array_elements_text(tags) AS tag
GROUP BY tenant_id, day, tag;

CREATE UNIQUE INDEX ON aiqg.tag_frequency_daily (tenant_id, day, tag);
```
Refresh: `REFRESH MATERIALIZED VIEW CONCURRENTLY aiqg.tag_frequency_daily;` (hourly via aiqg_aggregator Spark job).

### Write Cost
- Average tag array size: 4–8 entries per event
- JSONB write overhead: negligible vs row insertion cost
- Gatekeeper "match once, tag many" amortizes Hyperscan cost across the full rule pack — single scan emits all matching tags

---

## 10. Migration Strategies

### Adding a New Tag Category
1. Update this schema doc (§2 table) with the new category prefix and source rule pack
2. Update Gatekeeper rule-pack release with the new category whitelisted in `pkg/scanner/tag_lint.go`
3. Deploy rule pack; new tags begin appearing on new events
4. Update aiqg-ui category color mapping if customer-facing
5. **No backfill** — historical events do not receive new categories

### Deprecating a Tag
1. Mark in §15 Known Issues with deprecation date
2. Stop emitting in the next rule-pack release
3. Leave existing tags on historical rows in place (audit reproducibility)
4. Downstream alerts/dashboards must update their filters; the tag never disappears from old data

### What You Must Not Do
- **DO NOT rename a tag** — it is a breaking change for every dashboard, alert, and customer integration that filters by tag. Add a new tag and deprecate the old one instead.
- **DO NOT remove a category** — same reason.
- **DO NOT mutate existing tag arrays** — corrections come via new event versions.

---

## 11. Common Patterns

### Pattern: Clean RAG response
```json
["workflow:rag", "quality:validity_passed", "nist:safe", "nist:valid_reliable"]
```

### Pattern: PII leak in response
```json
["workflow:single_turn_qa", "pii:email", "nist:privacy_enhanced:violation", "clear:assurance_fail"]
```

### Pattern: Prompt injection attempt
```json
["injection:prompt_jailbreak", "nist:secure_resilient:violation"]
```

### Pattern: Retry of bad output
```json
["behavioral:retry", "antipattern:context_bloat", "clear:cost_fail"]
```

### Pattern: Compound NIST violations driving Trustworthiness scoring
```sql
-- Count nist:*:violation per tenant per week (drives Assurance dimension)
SELECT
  tenant_id,
  date_trunc('week', occurred_at) AS week,
  COUNT(*) FILTER (WHERE tag LIKE 'nist:%:violation') AS violations
FROM aiqg.response_events,
     LATERAL jsonb_array_elements_text(tags) AS tag
GROUP BY tenant_id, week;
```

### Pattern: "What's drifting" tenant report
```sql
-- Top 10 antipattern tags by frequency for a tenant (drives "what's drifting" section)
SELECT tag, COUNT(*) AS cnt
FROM aiqg.response_events,
     LATERAL jsonb_array_elements_text(tags) AS tag
WHERE tenant_id = $1
  AND occurred_at >= NOW() - INTERVAL '30 days'
  AND tag LIKE 'antipattern:%'
GROUP BY tag
ORDER BY cnt DESC
LIMIT 10;
```

---

## 12. Error Handling

| Failure mode | Handling | Owner |
|---|---|---|
| Rule pack emits invalid tag string | Drop tag with warning log; event write succeeds | Gatekeeper |
| Tag array exceeds reasonable size (>64 tags) | Truncate at 64, log warning | Gatekeeper |
| Tag write fails (DB error) | Event write fails (tags are part of the event row) — retried by upstream | aiqg-dashboard-be ingestor |
| Tag GIN index missing | Queries time out; surface in slow-query log; alert on missing index | DBA / aiqg-dashboard-be |
| Rule pack version skew across replicas | Tag distributions drift temporarily; downstream alerts must be threshold-based (see §15) | SRE |
| Hyperscan over-matching for short patterns | Suppressed via Gatekeeper's existing `confidence_boosting` and `validation_rules` mechanism (per Gatekeeper survey) | Gatekeeper rule-pack owner |

---

## 13. Testing Strategies

### Unit Tests
- Rule pack lint: every emitted tag matches the category whitelist and regex
- Tag dedup: producer never emits the same tag twice in one array
- Tag length: no tag exceeds 64 chars

### Integration Tests
- Golden fixtures: known input payloads → expected tag arrays (per category)
- Match-once-tag-many: one Hyperscan scan emits all matching categories in one pass
- GIN index: `tags @> '["nist:safe"]'::jsonb` returns expected rows under EXPLAIN ANALYZE (index used)

### Contract Tests
- aiqg-dashboard-be tag-filter API: returns only events matching ALL requested tags (AND semantics)
- aiqg_aggregator: `tag_frequency_daily` view counts match raw `jsonb_array_elements_text` count

### Performance Tests
- 10M-row event table, p95 < 50ms for `tags @>` containment query on tenant-scoped slice
- Materialized view refresh completes in < 5 min hourly

---

## 14. Related Documentation

- [[request-event]] — owns the request-side `tags` column
- [[response-event]] — owns the response-side `tags` column
- [[policy-rule]] — applies `policy:*` tags as part of rule evaluation
- [[aggregated-metrics]] — rolls up tag distributions per tenant/period
- [[audit-log-entry]] — logs tag application events
- [[workflow-classification]] — emits `workflow:*` tags
- [[response-structure]] — `structural_validity_passed` feeds `nist:valid_reliable`
- `source-spec-v0.2.md` §2.4 (Assurance dimension), §3.10 (match-once-tag-many)
- `build-vs-reuse.md` §2.4 (Assurance/NIST mapping), §2.10 (Gatekeeper rule-pack reuse), §2.12 (CLEAR computation)
- Gatekeeper `pkg/attest/cache.go` — "match once, tag many" implementation
- Gatekeeper `pkg/scanner/tag_lint.go` — category whitelist enforcement

---

## 15. Known Issues

- **Hyperscan over-matching for short patterns** — tag application uses Gatekeeper's existing `confidence_boosting` and `validation_rules` mechanism to suppress false positives (per Gatekeeper survey). Short-pattern rules (< 6 chars) must declare boost rules in the YAML pack.
- **Tag-set churn risk** — each new rule-pack version can shift tag distributions; downstream alerts should be **threshold-based** (e.g., "violations > 5x baseline"), **not absolute-count** ("violations > 100"). Document churn windows when shipping a new pack.
- **No per-tag ACL** — any reader of an event row sees the full tag array. Sensitive classifications (e.g., specific PII subtypes) should not be encoded in tag values that leak via dashboards. Use generic `pii:email` not `pii:email:john@example.com`.
- **Deprecation registry not yet implemented** — Phase 2 will add a `aiqg.deprecated_tags` table tracking deprecation date and replacement tag. Until then, deprecated tags are tracked only in this doc.

---

## Security

- Tags carry **no sensitive payload data** — only metadata about what was detected
- Tag application uses Gatekeeper's existing **HMAC-attested scan cache** (`pkg/attest/cache.go`) — same security model as existing TAS services
- Tag values are safe to surface in customer-facing dashboards (per §15, do not encode raw PII in tag values)
- Tag-filtered queries enforce tenant scoping via `tenant_id = $1` predicate; tag containment alone is never a query authorization signal

---

## Changelog

- **v1.0.0 — 2026-05-31** — initial spec draft — TAS Platform
