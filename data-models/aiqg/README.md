# AI Quality Gateway (AIQG) — Data Model Hub

**Status:** MVP spec drafted 2026-05-31
**Product:** AI Quality Gateway — streaming-native LLM proxy + CLEAR-framework measurement + Day-1 diagnostic report
**Source spec:** [source-spec-v0.2.md](./source-spec-v0.2.md)
**Master plan:** [build-vs-reuse.md](./build-vs-reuse.md)

This directory contains the 16 data-model docs and the master plan for the AIQG product. Each model doc follows the standard TAS 14-section template.

---

## Documents

| Doc | Purpose |
|---|---|
| [build-vs-reuse.md](./build-vs-reuse.md) | Master plan: what's reused from existing TAS, what's built new, wire-compat checklist, all §7 decisions stamped |
| [source-spec-v0.2.md](./source-spec-v0.2.md) | Product spec (Mehta-CLEAR-anchored methodology and product definition, v0.2) |
| [account.md](./account.md) | AIQG Account (Neo4j) — 1:1 mapping to Space.tenant_id; region, retention, scoring weights, quotas |
| [request-event.md](./request-event.md) | Per-request capture envelope (TimescaleDB hypertable + CloudEvent) |
| [response-event.md](./response-event.md) | Per-response capture envelope paired with request-event |
| [token-accounting.md](./token-accounting.md) | Input/output/cached/tool tokens, vendor pricing, estimated vs. actual cost, three-category waste decomposition |
| [event-timestamps.md](./event-timestamps.md) | DNS/TLS/TTFB/TTFT/inter-token/last-chunk latency capture |
| [request-structure.md](./request-structure.md) | system/user/history/tools/context observational snapshot |
| [response-structure.md](./response-structure.md) | response text/tool calls/finish/logprobs/validity observational snapshot |
| [inferred-labels.md](./inferred-labels.md) | workflow_type, retry_of_previous, abandonment, hedge — heuristic labels |
| [tag-set.md](./tag-set.md) | quality/policy/NIST AI RMF/compliance tag taxonomy |
| [policy-bundle.md](./policy-bundle.md) | Named, versioned policy bundle collections (Neo4j) |
| [policy-rule.md](./policy-rule.md) | Individual rule definitions composed into bundles (Neo4j) |
| [route-rule.md](./route-rule.md) | URL/header/source/workflow/time matchers → policy bundle resolution (Neo4j) |
| [audit-log-entry.md](./audit-log-entry.md) | Immutable audit trail (TimescaleDB) |
| [aggregated-metrics.md](./aggregated-metrics.md) | CLEAR rollups per workflow/route/account, 1m/5m/1h/1d windows (TimescaleDB continuous aggregates) |
| [report-snapshot.md](./report-snapshot.md) | Frozen Day-1 + periodic report artifacts (PostgreSQL + MinIO) |
| [workflow-classification.md](./workflow-classification.md) | Six-type taxonomy + detection signals (Gatekeeper rule pack) |

---

## Service-side architecture docs (live in their own repos)

| Repo | Doc |
|---|---|
| `tas-llm-router` (extended in place) | [docs/AIQG-EXTENSION.md](../../../tas-llm-router/docs/AIQG-EXTENSION.md) |
| `aiqg-dashboard-be` (new repo) | [ARCHITECTURE.md](../../../aiqg-dashboard-be/ARCHITECTURE.md) |
| `aiqg-ui` (new repo) | [ARCHITECTURE.md](../../../aiqg-ui/ARCHITECTURE.md) |

---

## Cross-service flow

- [aiqg-request-lifecycle.md](../cross-service/flows/aiqg-request-lifecycle.md) — end-to-end trace of one request through ingress → router → vendor → Kafka → Spark → TimescaleDB → dashboard

---

## Additive updates landed in existing docs (no schemas changed)

- [keycloak/users/user-model.md](../keycloak/users/user-model.md) — added optional `aiqg_account_id` custom attribute note + AIQG cross-service note
- [aether-be/nodes/space.md](../aether-be/nodes/space.md) — added 1:1 AIQGAccount mapping note
- [tas-llm-router/request-format.md](../tas-llm-router/request-format.md) — added AIQG observational-capture note (ChatRequest schema unchanged)
- [cross-service/mappings/id-mapping-chain.md](../cross-service/mappings/id-mapping-chain.md) — appended AIQG provisioning chain + header conventions table

---

## Decisions (from build-vs-reuse §7, all stamped 2026-05-31)

| Decision | Resolution |
|---|---|
| Hot analytics store | **TimescaleDB** (existing stack); revisit if any customer crosses 30K events/min/min |
| CLEAR scoring location | **Gateway-side, Go**; `scoring_version` field enables historical re-scoring |
| Path A enforcement | **Strict** — 401 on customer-facing ingress if `TAS-Auth` or `Authorization` missing |
| Shared package extraction (`@tas/*`) | **Copy-paste for MVP**; extract after v1 ships when surface area is proven |
| Spec §6 inherited questions | 4 resolved with MVP defaults (CLEAR weights equal; published thresholds; 4 starter bundles; URL+source+header matchers); 10 deferred |

---

## Status

| Workstream | State |
|---|---|
| 16 data-model docs | ✅ all drafted |
| `tas-llm-router/docs/AIQG-EXTENSION.md` | ✅ drafted |
| `aiqg-dashboard-be/ARCHITECTURE.md` | ✅ drafted |
| `aiqg-ui/ARCHITECTURE.md` | ✅ drafted |
| Cross-service flow + 4 existing-doc updates | ✅ done |
| Repo scaffolding | ⏳ pending |
| Spec review | ⏳ pending stakeholder sign-off |
| Implementation kickoff | ⏳ pending |

---

**Maintainer:** TAS Platform Team
**Last updated:** 2026-05-31
