# AIQG Policy Rule

---

**Metadata**

```yaml
service: aiqg-dashboard-be (CRUD) + tas-llm-router (evaluator)
model: AIQGPolicyRule
database: Neo4j
node_label: AIQGPolicyRule
version: 1.0.0
last_updated: 2026-05-31
status: planned (new node label; non-breaking addition)
spec_refs: source-spec-v0.2.md §3.5.1, §3.5.2, §3.10
plan_ref: build-vs-reuse.md §2.5, §2.10, §7.5
```

---

## 1. Overview

### Purpose
A `Policy Rule` is the **atomic unit of policy** composed into a [[policy-bundle]]. Each rule encodes a single `condition → action` pairing: a JsonLogic boolean expression evaluated against the request/response context, paired with a typed action (sample / tag / block / redact / transform / route_override / audit / rate_limit) and its parameters. Rules are the smallest re-usable knob the AI Quality Gateway exposes; bundles are merely ordered, named compositions of rules.

### Ownership
- **Owning service**: `aiqg-dashboard-be` — CRUD of `:AIQGPolicyRule` nodes, validation of `condition` and `action_params` schemas, tenant-scoped authorization (`aiqg:policy:edit` Keycloak role)
- **Evaluator**: `tas-llm-router/internal/policy` — resolves the active bundle from the route matcher, walks `INCLUDES_RULE.order` in priority order, evaluates each rule's `condition`, executes its `action`, records the matched rule id into [[request-event]] / [[response-event]] / [[audit-log-entry]]
- **Read-only consumers**: `aiqg-ui` (policy editor, Phase 2), Gatekeeper rule-pack loader (for `tag` rules whose action emits compliance tags)

### Storage Surfaces
| Surface | Role |
|---|---|
| Neo4j `:AIQGPolicyRule` node | Source of truth — definition, lineage, lifecycle |
| `Gatekeeper/configs/rules/aiqg_starter_rules.yaml` | NEW additive YAML file shipping the 11 starter rules (per build-vs-reuse §1.2 non-breaking constraint — net-new file, no existing pack touched) |
| Redis `rule:{id}:condition_compiled` | Compiled JsonLogic cache, TTL'd per rule version |
| Kafka `tas.aiqg.findings.v1` | When a rule fires it emits a finding event (consumed by Spark + audit) |

### Lifecycle Summary
Starter rules ship as YAML and are loaded into Neo4j on first boot of `aiqg-dashboard-be`. Tenant-custom rules are created via the dashboard (Phase 2), versioned via `:DERIVED_FROM` edges (rule edits never mutate — they create a new node). `enabled=false` soft-disables without deletion; rules are never hard-deleted while referenced by a non-archived bundle.

### Key Characteristics
- **Atomic + composable** — a rule does one thing; a bundle composes many
- **JsonLogic conditions** — sandbox-safe, NOT arbitrary code; complex matching escapes to Hyperscan rule packs
- **8 action types** — `sample`, `tag`, `block`, `redact`, `transform` (Phase 2), `route_override` (Phase 2), `audit`, `rate_limit` (Phase 2)
- **Shareable across bundles** — many-to-many via `:INCLUDES_RULE` edge with `order` priority
- **Dry-run capable** — `dry_run_default=true` rules evaluate but don't enforce (per spec §3.5.2); overridden by per-request `TAS-Dry-Run` header
- **Versioned lineage** — `:DERIVED_FROM` edges preserve audit lineage when a tenant clones a starter rule for customization
- **Non-breaking**: net-new node label, net-new YAML file, additive evaluator package inside tas-llm-router

---

## 2. Schema Definition

### Storage
- **Database**: Neo4j (same instance as `aether-be` — `neo4j.aether-be.svc.cluster.local:7687`)
- **Label**: `AIQGPolicyRule`
- **Migration impact**: additive only — new label + new edges; existing labels, properties, and relationships are unchanged

### Properties

| Property | Type | Required | Description |
|---|---|---|---|
| `id` | UUID (string) | yes | Primary key. Unique across all tenants. Generated server-side at create time. |
| `tenant_id` | UUID (string) | no | Foreign key to [[account]]'s `tenant_id`. **NULL means "starter rule" available to all tenants**; non-NULL means tenant-custom. |
| `name` | string | yes | Stable identifier. snake_case. Unique per tenant (or globally for starter rules where `tenant_id IS NULL`). |
| `description` | string | yes | Human-readable explanation surfaced in the dashboard policy editor. |
| `rule_type` | enum | yes | One of: `sample`, `tag`, `block`, `redact`, `transform`, `route_override`, `audit`, `rate_limit`. See §2.1 for semantics. |
| `condition` | JSONB | yes | JsonLogic boolean expression evaluated against request/response context. See §2.2. |
| `action_params` | JSONB | yes | Rule-type-specific parameters. Schema-validated per `rule_type` (see §2.3). |
| `severity` | enum | yes | `info`, `warn`, `critical`. Drives downstream alerting in [[audit-log-entry]] / Prometheus / [[response-event]]. |
| `enabled` | bool | yes | Soft-disable without deletion. Default `true`. |
| `dry_run_default` | bool | yes | When `true`, rule defaults to "evaluate but don't enforce" mode (per spec §3.5.2). Overridden by per-request `TAS-Dry-Run` header. Default `false`. |
| `is_starter` | bool | yes | `true` for rules shipped in starter bundles via `aiqg_starter_rules.yaml`. Used to prevent edits and to display the "TAS Starter" badge in the UI. Default `false`. |
| `created_at` | timestamp | yes | ISO-8601 UTC. Immutable after creation. |
| `updated_at` | timestamp | yes | ISO-8601 UTC. Updated only via clone-and-supersede (see §5). |
| `created_by_user_id` | UUID (string) | yes | Keycloak `sub` claim of the creating user. For starter rules: well-known `00000000-0000-0000-0000-000000000000`. |

### 2.1 `rule_type` Enum — Action Semantics

| `rule_type` | What it does | Phase |
|---|---|---|
| `sample` | Sets sampling decisions for this request (e.g., `sample_llm_judge_5pct`). Decision recorded on [[request-event]]. | MVP |
| `tag` | Applies one or more tags to the captured event (e.g., `tag_workflow_rag`). Tags flow into [[tag-set]] and feed CLEAR scoring + Gatekeeper findings. | MVP |
| `block` | Refuses to forward the request. Returns a 4xx (default 403) to the client and writes an [[audit-log-entry]]. `severity` MUST be `critical`. | MVP |
| `redact` | Calls Databunker tokenize on matched fields before forwarding. **Reuses existing tokenizer** per build-vs-reuse §2.11 — no new redaction engine. | MVP |
| `transform` | Mutates the payload (e.g., compression, context-window trim). Phase 2. | Phase 2 |
| `route_override` | Picks a different vendor/model than the default (e.g., downgrade to a cheaper model for low-value workflows). Phase 2. | Phase 2 |
| `audit` | Sets audit-retention level for this request (`retention_days`, `include_payload`). | MVP |
| `rate_limit` | Rejects request if customer is over quota. Phase 2. | Phase 2 |

### 2.2 `condition` — JsonLogic

Conditions are stored as JsonLogic JSON. The evaluator package compiles them on first use, caches the compiled form in Redis (keyed by rule id + version hash), and re-evaluates on every matching request.

Available context variables (the JsonLogic `var` operand):

| Path | Source |
|---|---|
| `vendor` | `openai` / `anthropic` / ... (resolved by router) |
| `model` | resolved model name |
| `tags` | array of tags applied so far in the rule chain |
| `workflow_type` | from [[workflow-classification]] |
| `request_structure.*` | full [[request-structure]] (system, user, history, tools, context_block_total_tokens, ...) |
| `response_structure.*` | full [[response-structure]] (for response-side rules) |
| `usage.*` | from [[token-accounting]] |
| `severity_seen` | highest severity of rules already fired |
| `request_event.headers.*` | normalized customer headers (post-TAS-* strip) |
| `account.region` | from [[account]] |
| `tenant_id` | active tenant |

Example conditions:

```json
// Match all RAG requests
{"==": [{"var": "request_structure.workflow_type"}, "rag"]}
```

```json
// Match bloated OpenAI RAG (large context block on openai)
{"and": [
  {">": [{"var": "request_structure.context_block_total_tokens"}, 10000]},
  {"==": [{"var": "vendor"}, "openai"]}
]}
```

```json
// Match when a PII tag has already been applied earlier in the chain
{"in": ["pii:email", {"var": "tags"}]}
```

```json
// Match all requests (used by sample_llm_judge_5pct)
true
```

### 2.3 `action_params` — Schema by Rule Type

Each `rule_type` has its own JSON Schema (loaded by Gatekeeper's rule loader). Validated on bundle activation; malformed `action_params` block bundle activation.

```yaml
# sample
{ rate: 0.05, scope: "llm_judge", stratified_by: "workflow_type" }

# tag
{ tags: ["workflow:rag", "antipattern:context_bloat"] }

# block
{ status: 403, message: "blocked by production_strict bundle" }

# redact
{ pii_types: ["email", "ssn", "phone", "credit_card"], strategy: "tokenize" }

# audit
{ retention_days: 90, include_payload: true, log_all_tags: true }

# transform (Phase 2)
{ operation: "context_trim", max_tokens: 8000, strategy: "tail" }

# route_override (Phase 2)
{ target_vendor: "openai", target_model: "gpt-4o-mini" }

# rate_limit (Phase 2)
{ rpm: 60, tpm: 90000, scope: "tenant" }
```

---

## 3. Fields Reference

See §2 for the table. Notes:

- **`id`** — UUID v7 preferred (sortable by creation time) but v4 accepted
- **`tenant_id` NULL semantics** — starter rules are scoped globally; tenant-custom rules are isolated by `tenant_id` (Cypher queries always filter on it where non-NULL)
- **`name` uniqueness** — Neo4j composite uniqueness constraint on `(tenant_id, name)`. Starter rules (where `tenant_id IS NULL`) have a global unique constraint on `name`.
- **`severity` for block** — enforced at create/update: if `rule_type=block` and `severity != "critical"`, validation rejects with 400. This guarantees blocked requests always alert.
- **`is_starter` immutability** — once `true`, cannot be flipped to `false` (and vice versa). Custom rules cloned from starters set `is_starter=false` on the clone, `:DERIVED_FROM` preserves the lineage.

---

## 4. Validation Rules

Enforced by `aiqg-dashboard-be/internal/services/policy_rule_service.go`:

| Rule | Check |
|---|---|
| Name format | `name` matches `^[a-z][a-z0-9_]*$` (snake_case, lowercase, alphanumeric + underscore, starts with letter) |
| Name length | `1 ≤ len(name) ≤ 64` |
| Rule type | `rule_type ∈ {sample, tag, block, redact, transform, route_override, audit, rate_limit}` |
| Condition parses | `condition` must parse as valid JsonLogic; reject on **bundle activation** (not on rule create — allows draft-then-fix workflow) if any rule has a malformed condition |
| Action params schema | `action_params` validated against the JSON Schema for the rule's `rule_type` |
| Severity for block | `rule_type=block` requires `severity=critical` |
| Tenant isolation | non-starter rules MUST have `tenant_id` set; starter rules MUST have `tenant_id IS NULL` and `is_starter=true` |
| Phase-2 rule types | Creating a rule with `rule_type ∈ {transform, route_override, rate_limit}` returns 501 Not Implemented in MVP |
| Description length | `10 ≤ len(description) ≤ 1000` |
| Referenced redact pii_types | Each entry must be a Databunker-known PII type; unknown types reject |
| Sample rate bounds | `0 < action_params.rate ≤ 1.0` |

Compile-time validation of starter YAML happens in CI:

```bash
cd Gatekeeper
go test ./configs/rules -run TestAIQGStarterRulesValid
```

---

## 5. Relationships

### Neo4j Edges

```cypher
// Rule belongs to one or more bundles (ordered)
(:AIQGPolicyBundle)-[:INCLUDES_RULE {order: integer}]->(:AIQGPolicyRule)

// Versioned lineage — clone-and-supersede
(:AIQGPolicyRule)<-[:DERIVED_FROM]-(:AIQGPolicyRule)

// Audit trail — which rules fired on which request
(:RequestEvent)-[:MATCHED_RULE {fired_at: timestamp, dry_run: bool}]->(:AIQGPolicyRule)
```

- **`INCLUDES_RULE.order`** — integer; lower values evaluated first; `block` rules short-circuit the chain
- **`DERIVED_FROM`** — set when a tenant clones a starter or another tenant rule. The old node stays; `updated_at` and a new `id` on the clone. Provides audit lineage and the ability to compare versions.
- **`MATCHED_RULE`** — written by tas-llm-router after every request that fired a rule (regardless of dry-run). Forms the audit log query surface.

### Cross-Service References
- [[policy-bundle]] — composes rules
- [[route-rule]] — selects which bundle is active for a request; doesn't reference rules directly
- [[audit-log-entry]] — written by `block`, `redact`, `transform` actions
- [[tag-set]] — populated by `tag` rules (emits `policy:*`, `quality:*`, `antipattern:*`, `nist:*`, `clear:*` tags)
- [[request-event]] — records `matched_rule_ids` array
- [[response-event]] — records `matched_rule_ids` array (for response-side rules)
- [[account]] — tenant-custom rules scoped by `tenant_id`

---

## 6. Lifecycle & State Machines

### Rule Lifecycle (no formal state machine — driven by `enabled` + edge presence)

| State | Definition |
|---|---|
| **Draft** | `enabled=false`, no bundle references this rule yet |
| **Active** | `enabled=true`, at least one non-archived bundle includes this rule via `INCLUDES_RULE` |
| **Disabled** | `enabled=false` after having been active. Does not evaluate. Does not appear in audit. Edges remain. |
| **Superseded** | A newer rule with `(:NewRule)-[:DERIVED_FROM]->(:OldRule)` exists. Old rule may still be active in older bundle versions. |
| **Orphaned** | `enabled=true` but no `INCLUDES_RULE` references. Doesn't evaluate (no bundle path). Surfaced in the dashboard as cleanup-candidate. |

### Transition Rules
- Rule edits NEVER mutate in place — they create a new rule node + `:DERIVED_FROM` edge; the bundle is then re-pointed to the new rule via a bundle version bump
- `enabled` can be toggled in place (cheap soft-disable)
- A rule cannot be hard-deleted while any `:AIQGPolicyBundle` with `status != "archived"` references it via `:INCLUDES_RULE`
- Starter rules (`is_starter=true`) cannot be edited or disabled; tenants must clone them first (`:DERIVED_FROM`)

### Bundle-Activation Evaluation Order
When a [[policy-bundle]] is activated, the evaluator:
1. Loads all rules included via `:INCLUDES_RULE` ordered by `order` ascending
2. Validates every rule's `condition` parses as JsonLogic (rejects activation on any failure)
3. Validates every rule's `action_params` against the schema for its `rule_type`
4. Compiles conditions and caches in Redis under `rule:{id}:condition_compiled` (TTL 1 hour, invalidated on rule version bump)

---

## 7. API Examples

### Starter Rule Library — Full Definitions

These 11 rules ship in `Gatekeeper/configs/rules/aiqg_starter_rules.yaml` and are loaded into Neo4j on first boot of `aiqg-dashboard-be`. **NEW additive file** — no existing rule pack is touched (build-vs-reuse §1.2).

#### 1. `sample_llm_judge_5pct` — Default 5% LLM-judge sampling
```yaml
name: sample_llm_judge_5pct
description: "Sample 5% of requests for LLM-as-judge evaluation, stratified by workflow type"
rule_type: sample
condition: true
action_params:
  rate: 0.05
  scope: llm_judge
  stratified_by: workflow_type
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

#### 2. `sample_llm_judge_10pct` — Higher-rate variant
```yaml
name: sample_llm_judge_10pct
description: "Sample 10% of requests for LLM-as-judge evaluation, stratified by workflow type"
rule_type: sample
condition: true
action_params: { rate: 0.10, scope: llm_judge, stratified_by: workflow_type }
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

#### 3. `sample_llm_judge_1pct` — Low-rate (high-volume) variant
```yaml
name: sample_llm_judge_1pct
description: "Sample 1% of requests for LLM-as-judge evaluation (high-volume customers)"
rule_type: sample
condition: true
action_params: { rate: 0.01, scope: llm_judge, stratified_by: workflow_type }
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

#### 4. `strict_validity_check` — Tag validity failures
```yaml
name: strict_validity_check
description: "Tag responses whose structural-validity target was set but not satisfied as quality:validity_failed + clear:efficacy_marginal"
rule_type: tag
condition:
  and:
    - { "!=": [{ "var": "response_structure.structural_validity_target" }, "none"] }
    - { "==": [{ "var": "response_structure.structural_validity_passed" }, false] }
action_params:
  tags: ["quality:validity_failed", "clear:efficacy_marginal"]
severity: warn
enabled: true
dry_run_default: false
is_starter: true
```

#### 5. `structural_validity_only` — Lighter validity tagger
```yaml
name: structural_validity_only
description: "Tag responses with quality:validity_failed only when structural-validity target was explicitly set and not satisfied (no CLEAR signal)"
rule_type: tag
condition:
  and:
    - { "!=": [{ "var": "response_structure.structural_validity_target" }, "none"] }
    - { "==": [{ "var": "response_structure.structural_validity_passed" }, false] }
action_params:
  tags: ["quality:validity_failed"]
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

#### 6. `pii_detect_input` — Tag input-side PII findings
```yaml
name: pii_detect_input
description: "When Gatekeeper PII scanner finds PII in the request payload, tag pii:* and nist:privacy_enhanced:violation"
rule_type: tag
condition:
  some:
    - { "var": "request_event.gatekeeper_findings" }
    - { "==": [{ "var": "category" }, "pii"] }
action_params:
  tags: ["pii:detected", "nist:privacy_enhanced:violation"]
severity: warn
enabled: true
dry_run_default: false
is_starter: true
```

#### 7. `pii_detect_output` — Same, response side
```yaml
name: pii_detect_output
description: "When Gatekeeper PII scanner finds PII in the response payload, tag pii:* and nist:privacy_enhanced:violation"
rule_type: tag
condition:
  some:
    - { "var": "response_event.gatekeeper_findings" }
    - { "==": [{ "var": "category" }, "pii"] }
action_params:
  tags: ["pii:detected", "nist:privacy_enhanced:violation"]
severity: warn
enabled: true
dry_run_default: false
is_starter: true
```

#### 8. `pii_tokenize_input` — Redact input PII via Databunker
```yaml
name: pii_tokenize_input
description: "Tokenize PII (email, ssn, phone, credit_card) in the request payload using Databunker before forwarding to the vendor"
rule_type: redact
condition:
  some:
    - { "var": "request_event.gatekeeper_findings" }
    - { "==": [{ "var": "category" }, "pii"] }
action_params:
  pii_types: ["email", "ssn", "phone", "credit_card"]
  strategy: tokenize
severity: warn
enabled: true
dry_run_default: false
is_starter: true
```

#### 9. `audit_full` — Long-retention audit with payload
```yaml
name: audit_full
description: "Audit-log every matched event with full payload and 90-day retention"
rule_type: audit
condition: true
action_params:
  retention_days: 90
  include_payload: true
  log_all_tags: true
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

#### 10. `audit_summary` — Short-retention audit, no payload
```yaml
name: audit_summary
description: "Audit-log every matched event with metadata only (no payload) and 30-day retention"
rule_type: audit
condition: true
action_params:
  retention_days: 30
  include_payload: false
  log_all_tags: false
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

#### 11. `payload_retain_sampled` — Retain payload only on sampled requests
```yaml
name: payload_retain_sampled
description: "Retain the full payload only when the request has been selected for sampling (LLM-judge or otherwise); 7-day retention"
rule_type: audit
condition:
  in: ["sampled", { "var": "tags" }]
action_params:
  retain_payload_when: sampled
  retention_days: 7
  include_payload: true
severity: info
enabled: true
dry_run_default: false
is_starter: true
```

### Cypher — Clone a Starter Rule for Tenant Customization
```cypher
MATCH (starter:AIQGPolicyRule {name: "sample_llm_judge_5pct", is_starter: true})
CREATE (clone:AIQGPolicyRule {
  id: randomUUID(),
  tenant_id: $tenant_id,
  name: "sample_llm_judge_5pct_custom",
  description: "Same as TAS starter, but stratify by customer header X-App-Tier",
  rule_type: starter.rule_type,
  condition: starter.condition,
  action_params: '{"rate": 0.05, "scope": "llm_judge", "stratified_by": "request_event.headers.x-app-tier"}',
  severity: starter.severity,
  enabled: true,
  dry_run_default: false,
  is_starter: false,
  created_at: datetime(),
  updated_at: datetime(),
  created_by_user_id: $keycloak_sub
})
CREATE (clone)-[:DERIVED_FROM]->(starter)
RETURN clone
```

### Cypher — List All Rules in an Active Bundle, Ordered
```cypher
MATCH (b:AIQGPolicyBundle {id: $bundle_id, status: "active"})
      -[r:INCLUDES_RULE]->
      (rule:AIQGPolicyRule {enabled: true})
RETURN rule, r.order AS priority
ORDER BY r.order ASC
```

### Cypher — Find All Tenant Customizations of a Starter
```cypher
MATCH (clone:AIQGPolicyRule)-[:DERIVED_FROM*1..]->(starter:AIQGPolicyRule {name: $starter_name, is_starter: true})
WHERE clone.tenant_id IS NOT NULL
RETURN clone.tenant_id, clone.name, clone.id
```

### API — POST /api/v1/policy-rules
```json
POST /api/v1/policy-rules HTTP/1.1
Authorization: Bearer <keycloak-jwt>
Content-Type: application/json

{
  "name": "block_bloated_rag_openai",
  "description": "Refuse OpenAI RAG requests whose context block exceeds 10,000 tokens",
  "rule_type": "block",
  "condition": {
    "and": [
      { "==": [{ "var": "request_structure.workflow_type" }, "rag"] },
      { ">": [{ "var": "request_structure.context_block_total_tokens" }, 10000] },
      { "==": [{ "var": "vendor" }, "openai"] }
    ]
  },
  "action_params": {
    "status": 413,
    "message": "Context block exceeds 10K tokens; trim before retrying"
  },
  "severity": "critical",
  "enabled": true,
  "dry_run_default": true
}
```

Response `201 Created`:
```json
{
  "id": "0190a3d0-7c8a-7b2e-9d8c-3f1b6c2a9e54",
  "tenant_id": "0190a000-...",
  "name": "block_bloated_rag_openai",
  "is_starter": false,
  "created_at": "2026-05-31T18:42:00Z",
  ...
}
```

### Go Pseudocode — JsonLogic Evaluation in the Hot Path
```go
// tas-llm-router/internal/policy/evaluator.go (additive package)
func (e *Evaluator) Evaluate(ctx context.Context, bundle *Bundle, req *Request) ([]MatchedRule, error) {
    matched := make([]MatchedRule, 0, len(bundle.Rules))
    for _, rule := range bundle.Rules { // already sorted by INCLUDES_RULE.order
        if !rule.Enabled {
            continue
        }
        compiled := e.cache.Get(rule.ID) // Redis-backed
        if compiled == nil {
            compiled = jsonlogic.MustCompile(rule.Condition)
            e.cache.Set(rule.ID, compiled, 1*time.Hour)
        }
        if !compiled.Apply(req.Context()) {
            continue
        }
        dryRun := rule.DryRunDefault || req.Header.Get("TAS-Dry-Run") == "1"
        if err := e.execute(ctx, rule, req, dryRun); err != nil {
            return matched, err
        }
        matched = append(matched, MatchedRule{ID: rule.ID, DryRun: dryRun})
        if rule.RuleType == RuleTypeBlock && !dryRun {
            return matched, ErrBlockedByPolicy // short-circuit
        }
    }
    return matched, nil
}
```

---

## 8. Cross-Service Integration

| Service | How it interacts with `:AIQGPolicyRule` |
|---|---|
| `aiqg-dashboard-be` | Owns CRUD; serves `/api/v1/policy-rules/*`; validates schemas; enforces Keycloak role `aiqg:policy:edit` |
| `tas-llm-router` | Loads rules at bundle-resolution time (per [[route-rule]] match); evaluates conditions in hot path; executes actions; records matched rule ids in [[request-event]] / [[response-event]] |
| Gatekeeper | Loads `aiqg_starter_rules.yaml` on boot (additive file); for `tag` rules whose action_params reference NIST AI RMF / CLEAR tag namespaces, these are the same tag taxonomy emitted by other Gatekeeper rule packs |
| Databunker | Called by `redact` rules via existing `pkg/tokenize/databunker.go` integration — **no new redaction engine** (build-vs-reuse §2.11) |
| `aiqg-ui` | Phase 2 — policy editor surfaces rule listings, clone-from-starter, dry-run preview |
| `tas-spark-jobs/aiqg_aggregator` | Reads matched rule ids from [[request-event]] / [[response-event]] streams; rolls up per-rule fire counts into TimescaleDB for the dashboard |
| Kafka `tas.aiqg.findings.v1` | When a rule fires it emits a finding event (rule id, severity, dry-run flag, tenant id) for the audit + alerting pipeline |
| Loki | Rule-fire log lines tagged with `rule_id`, `rule_name`, `severity`, `dry_run` for LogQL-based investigation |

---

## 9. Performance Considerations

| Concern | Mitigation |
|---|---|
| Hot-path latency budget per rule | Compiled JsonLogic + Redis cache + sorted bundle: target < 50µs per rule on the gateway thread |
| Bundle size ceiling | Recommend ≤50 rules per bundle. Surface this in the dashboard's bundle editor (Phase 2) as a soft warning at 30, hard cap at 50. Beyond this the per-request evaluation cost dominates. |
| Cache invalidation | Rule version bump increments a per-rule version counter; Redis key includes that counter; old compiled forms naturally TTL out |
| Short-circuit on block | First `block` rule that fires terminates evaluation — keeps p99 latency bounded when block rules fire early |
| Compilation cost | One-time per rule version; amortized across millions of requests. Cold path is `O(rule_count)` on first request after deploy. |
| Cardinality on `MATCHED_RULE` edges | High-volume tenants generate millions of edges/day. Edges TTL'd after 90 days via a nightly Neo4j job; aggregated counts live in TimescaleDB. |
| Condition complexity | JsonLogic AST depth limit of 32 enforced at validation; complex matching escapes to Hyperscan rule packs (much faster for regex-heavy patterns) |

---

## 10. Security Considerations

| Concern | Mitigation |
|---|---|
| Arbitrary code execution via condition | **JsonLogic only — NOT eval, NOT a scripting language. Sandbox-safe by design.** No I/O, no network, no filesystem, no reflection. |
| Privilege escalation via tenant rule creation | Custom rule creation requires Keycloak role `aiqg:policy:edit` (per-tenant). Cross-tenant access blocked by `tenant_id` filter in repository layer. |
| Starter rule tampering | `is_starter=true` rules are immutable; mutations rejected at validation. Tenants must clone via `:DERIVED_FROM`. |
| Redact rule misconfiguration leaking PII | `redact` rules invoke Databunker which has its own access control + audit; the PII type list is validated against Databunker's registered tokenizers. |
| Block rule accidentally taking down traffic | `dry_run_default: true` recommended for new block rules. Per-request `TAS-Dry-Run` header lets operators flip enforcement back on per-request for canary testing. Audit log surfaces every fire (dry-run included). |
| Audit log retention compliance | `audit` rule's `retention_days` validated against tenant's [[account]] data-retention policy; longest setting wins. |
| Sensitive data in `action_params` | Schema rejects unknown keys; freeform string values (e.g., `block.message`) sanitized before being surfaced in 4xx responses to avoid info leak. |

---

## 11. Migration Strategies

### Initial Seed
On first boot of `aiqg-dashboard-be`, a one-shot job (`cmd/seed-starter-rules`) reads `Gatekeeper/configs/rules/aiqg_starter_rules.yaml` and creates the 11 starter `:AIQGPolicyRule` nodes with `tenant_id IS NULL` and `is_starter=true`. Idempotent — re-running is a no-op.

### Starter Rule Versioning
Starter rule changes ship via YAML; new versions are added with a `version` suffix in the name (e.g., `sample_llm_judge_5pct_v2`); the old node is preserved. Tenants stay pinned to the version their bundle references until they explicitly upgrade by re-cloning. This is the same backward-compat pattern as [[policy-bundle]] versioning.

### Tenant Custom Rule Migration
- Adding new rule fields → ADDITIVE only (new property defaulted server-side). Existing rules continue to evaluate.
- Adding new `rule_type` enum value → ADDITIVE only. Existing rules untouched. Evaluator gains a new `case` arm.
- Deprecating a `rule_type` → mark rules `enabled=false` in a migration script + emit a dashboard banner; never hard-delete.

### Non-Breaking Constraint Compliance
Per build-vs-reuse §1.2, this entire model is additive: new Neo4j label, new YAML file, new evaluator package inside tas-llm-router. No existing rule pack, schema, topic, or endpoint is modified.

---

## 12. Common Patterns

### Pattern A — Clone & Customize a Starter
Tenant decides the default 5% sampling is too noisy. They clone `sample_llm_judge_5pct` → `sample_llm_judge_5pct_lowvol` with a condition filtering out low-volume workflows. The dashboard surfaces this as "Customized from TAS starter `sample_llm_judge_5pct`".

### Pattern B — Stage a Block Rule via Dry-Run
1. Author rule with `dry_run_default: true`
2. Include in active bundle
3. Watch the audit log for ~24 hours — did the rule fire on traffic that should NOT be blocked?
4. If clean, edit bundle to flip `dry_run_default: false` (creates a new rule version via `:DERIVED_FROM`)
5. If noisy, refine the condition without changing the bundle

### Pattern C — Layered Tagging → Scoring Chain
Several `tag` rules fire in order, each adding a tag (`workflow:rag`, `antipattern:context_bloat`, `quality:validity_failed`). The CLEAR scorer downstream reads the cumulative tag set and computes composite efficacy/cost scores per [[response-event]].

### Pattern D — Escape Hatch via `TAS-Policy` Header
For one-off debugging, a developer sets `TAS-Policy: development_lenient` on a request. The route-matched bundle is bypassed; only the rules in `development_lenient` evaluate. The override is itself audit-logged on the request for the platform team's review.

### Pattern E — Hyperscan Escape for Complex Matching
JsonLogic can't easily express "match any of 200 known prompt-injection regexes." Instead, route the request through a Gatekeeper Hyperscan rule pack (much more expressive) which emits tags; then a `tag` rule with condition `{"some": [..., {"==": [{"var": "category"}, "prompt_injection"]}]}` reacts to the tag set.

---

## 13. Error Handling

| Failure mode | Behavior |
|---|---|
| Malformed `condition` at bundle activation | Bundle activation rejected with 400 + the offending rule id. Active bundles keep evaluating; only the activation transaction fails. |
| Compiled condition cache miss (Redis down) | Fall back to in-process compile + LRU cache. Latency hit ~10ms first request per rule per replica. Logged at WARN. |
| `redact` rule, Databunker unreachable | If `severity=critical`, fail the request with 503. Otherwise log + skip the redaction + tag with `policy:redact_failed`. Configurable per [[account]] `failure_policy`. |
| `block` rule fires but downstream config wants pass-through | Per spec §3.5.2 resolution order, per-request `TAS-Policy` header can override the route-attached bundle. The override itself is audit-logged. |
| Rule fires but action engine panics | Recovered at the request handler boundary; request continues without the action; tag `policy:rule_panic` applied; alert raised on `aiqg_rule_panic_total` Prometheus counter |
| Bundle references a rule that has been hard-deleted (shouldn't happen — guarded) | Bundle activation fails with 500 (consistency violation); ops alert raised |
| Phase-2 `rule_type` used in MVP | Validation rejects with 501 Not Implemented + a link to the roadmap |

---

## 14. Known Issues & Limitations

- **JsonLogic expressivity ceiling** — Some patterns (deeply nested array reductions, multi-step regex matching) are awkward in JsonLogic. **Documented escape**: complex matching uses `tag` rules driven by Hyperscan rule packs (`Gatekeeper/configs/rules/aiqg_*_antipatterns.yaml`), which are far more expressive and faster.
- **Performance ceiling at ~50 rules/bundle** — Beyond this the per-request evaluation cost dominates. Surface this in the dashboard's bundle editor (Phase 2) as a soft warning at 30, hard cap at 50.
- **No "rule groups" abstraction** — Tenants who want to enable/disable several related rules at once must manage them individually or compose via [[policy-bundle]]. Phase 3 may introduce explicit "Rule Group" objects if usage demands.
- **Starter rule edits require a TAS platform release** — Starter rule YAML lives in the Gatekeeper repo; updates require a CI cut. Tenants needing faster iteration must clone-and-customize.
- **Phase-2 rule types stubbed** — `transform`, `route_override`, `rate_limit` are reserved in the enum but return 501 in the MVP. Validation accepts the rule shape; execution rejects.
- **No multi-tenant rule sharing** — A tenant cannot share a custom rule with another tenant directly. They would have to publish it as a starter (which requires TAS platform involvement). Acceptable for MVP; revisit if customer demand surfaces.

---

## Cross-References

- [[policy-bundle]] — composes rules into named, versioned collections
- [[route-rule]] — matches incoming requests to the right bundle
- [[audit-log-entry]] — immutable record of every rule fire (incl. dry-run)
- [[tag-set]] — populated by `tag` rules (`policy:*`, `quality:*`, `antipattern:*`, `nist:*`, `clear:*` tags)
- [[request-event]] — records `matched_rule_ids` array (request-side rules)
- [[response-event]] — records `matched_rule_ids` array (response-side rules)
- [[account]] — tenant-custom rules scoped by `tenant_id`; failure-policy + retention defaults
- [[workflow-classification]] — provides `workflow_type` for JsonLogic conditions
- [[request-structure]] / [[response-structure]] — JsonLogic variable surface

---

## Changelog

| Version | Date | Author | Notes |
|---|---|---|---|
| 1.0.0 | 2026-05-31 | TAS Platform | Initial spec draft — `:AIQGPolicyRule` Neo4j node, 11 starter rules in `Gatekeeper/configs/rules/aiqg_starter_rules.yaml` (new additive file), 8 action types (4 MVP + 4 Phase-2), JsonLogic condition surface, `:INCLUDES_RULE` / `:DERIVED_FROM` / `:MATCHED_RULE` edges. Non-breaking per build-vs-reuse §1.2. |
