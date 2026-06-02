# AI Quality Gateway — Build vs. Reuse Mapping

**Source spec:** [source-spec-v0.2.md](./source-spec-v0.2.md) (AI Quality Gateway v0.2, Stakeholder Review Draft)
**Status:** Initial survey complete. Ready for architecture spec drafting.
**Last updated:** 2026-05-31 (§7 decisions stamped; revised to extend tas-llm-router in place rather than fork)

This document maps every AIQG capability called out in the v0.2 spec to one of three states:

- **REUSE** — capability already exists in TAS; AIQG wires it in
- **EXTEND** — capability mostly exists; AIQG adds a thin layer
- **BUILD** — net-new component AIQG must create

The intent is to make the smallest possible new build that delivers the Day-1 diagnostic report.

---

## 1. Executive Summary

| Layer | Reuse | Extend | Build |
|---|---|---|---|
| Scanning / rule packs / actions / Kafka stream | **Gatekeeper** (80%) | NIST AI RMF → CLEAR tag mapping | Workflow-classification rule pack |
| LLM proxy + routing + provider SDKs | **tas-llm-router** (extend in place — no new repo) | Per-chunk timing instrumentation; per-request Path A mode; AIQG event schema; TAS-\* header taxonomy; payload reduction (Phase 2) | New `pkg/clear`, `pkg/aiqg/*` packages **inside tas-llm-router** |
| Dashboard backend (Gin, Neo4j, Keycloak, logging, metrics, OpenAPI) | **aether-be** patterns (copy verbatim) | Domain repos for AIQG models | Day-1 report generator; TimescaleDB client |
| Dashboard frontend (React 19 + Vite + Redux + Tailwind + Keycloak) | **aether** patterns + 5 shared packages | Theme tweaks for AIQG branding | New `aiqg-ui` repo with AIQG-specific screens |
| Identity / tenancy | **Keycloak** realm `aether`; **Neo4j** User/Space nodes | Add `aiqg_account_id` attribute to Space | AIQG Account node |
| Event aggregation | **tas-spark-jobs** Structured Streaming template | Adapt for AIQG topics | New `aiqg_events_aggregator` job |
| Hot analytics storage | **PostgreSQL + TimescaleDB** (already in stack) | — | **Decision needed**: stick with TimescaleDB (existing stack) or add ClickHouse (scale, but new infra) — see §7.3 |
| Metrics / dashboards / logs | **Prometheus + Grafana + Alloy + Loki** | Add AIQG namespaces to Alloy | 4 AIQG Grafana dashboards + alert rules |
| Data models | **14-section template**; existing User/Space/ChatRequest models adopted | 5 existing model docs updated | 16 new AIQG model docs (see §8) |

**Bottom line: ~75% of AIQG is wiring existing TAS components together. The proxy/gateway capability is added by extending `tas-llm-router` in place (no new repo). The two genuinely new repos are `aiqg-dashboard-be` (a thin domain layer copying aether-be patterns) and `aiqg-ui` (the customer-facing dashboard). Plus one Spark aggregation job.**

### 1.1 Why extend tas-llm-router rather than fork

tas-llm-router already provides 90% of the AIQG proxy shell: OpenAI/Anthropic streaming providers, routing/failover, Gatekeeper scanning hook, CloudEvents publisher to Kafka, auth middleware. Forking it would mean maintaining two copies of provider integrations forever. The genuine differences (per-chunk timing, Path A pass-through auth, AIQG event schema, workflow classification, CLEAR scoring, TAS-\* header taxonomy) are features, not a different architecture.

The auth-mode collision — tas-llm-router stores vendor keys today, AIQG requires pass-through with no storage — is resolved per-request: if the inbound request carries `TAS-Auth` **and** a customer `Authorization` header, the proxy enters **Path A mode** (pass through unchanged, never persist). Otherwise it falls back to the existing internal-routing behavior. One service, two modes selected by headers. Deployment can run the same binary as multiple replicas with different ingress URLs (`gateway.aiqg.tas.io` vs. internal cluster DNS) if operational separation is desired, but the codebase stays single.

### 1.2 Non-breaking-change constraint

**Hard rule for the AIQG extension: no existing exported function signatures, struct fields, JSON/proto schemas, Kafka topics, HTTP endpoints, config keys, or CloudEvent types may change shape.** All AIQG work must be additive. Existing callers — `tas-agent-builder`, `aether-be`, `audimodal`, `llm-invocation`, and any out-of-tree consumers — must keep building and running with no changes.

Implications for each at-risk surface:

| Surface | Risk | Resolution |
|---|---|---|
| `internal/types/responses.ChatChunk` | Adding a `ReceivedAt` field changes JSON marshaling and breaks positional struct literals | **Don't extend the type.** Stamp chunk timestamps into a sidecar `internal/instrumentation.TimingCollector` keyed by request context (`context.Value(timingKey)`) — invisible to existing consumers |
| `internal/providers/{openai,anthropic}/provider.go` method signatures | Adding a `bearerOverride` parameter breaks the `LLMProvider` interface | Pass the per-request bearer override via `context.Context` value, read inside the provider's existing implementation. Interface unchanged |
| `internal/security/auth.go` validation | Changing the token regex breaks existing TAS API keys | Additive recognition: try existing validators first; only attempt `tas_qg_live_*` parsing if all prior validators reject. Existing keys continue to validate unchanged |
| `internal/pricing` / `EstimateCost()` | Changing the function body to use actuals would surprise existing routing callers | Leave `EstimateCost()` untouched (returns pre-request estimate). Add a new `ActualCost(usage Usage) Cost` function read after the response completes |
| CloudEvent types `com.tas.activity.llm.{request,response}` | Adding AIQG fields would change the wire payload | **Don't touch.** Define new `com.tas.aiqg.request.v1` / `com.tas.aiqg.response.v1` as separate types on separate topics (`tas.aiqg.*`). Emission is additive — existing events keep flowing unchanged |
| Kafka topics `tas.activity.*`, `tas.compliance.*` | Existing Spark jobs subscribe by exact name | **Don't touch.** AIQG uses new topics `tas.aiqg.request.v1`, `tas.aiqg.response.v1`, `tas.aiqg.findings.v1` |
| HTTP endpoints `/v1/chat/completions`, `/v1/openai/*`, `/v1/anthropic/*`, `/v1/providers`, `/v1/health`, `/metrics` | Existing callers rely on these | **Don't touch.** AIQG management endpoints live under a new prefix `/aiqg/v1/*` or are exposed only through `aiqg-dashboard-be` |
| Config struct shape (`config.Config` and nested) | Renaming or restructuring breaks existing deployments | Add new nested struct `Config.AIQG` containing all AIQG settings. Empty/zero value means AIQG mode is disabled — behavior identical to today |
| Prometheus metric names | Existing dashboards/alerts subscribe by name | **Don't rename.** New AIQG metrics use a `aiqg_*` prefix; existing `llm_router_*` metrics keep emitting unchanged |
| Existing rule packs in `Gatekeeper/configs/rules/` | Modifying any existing pack could change findings for current users | **Don't touch.** AIQG ships net-new YAML files (`aiqg_*.yaml`) only |
| `internal/events.Publisher` interface | Adding new methods breaks anything implementing it | Use the existing `Publish(ctx, event)` method to publish the new AIQG event types (they're just CloudEvents with new `type` field). No interface change |

This constraint is enforced by code review and by keeping the AIQG extension on a feature branch until existing-caller smoke tests pass.

---

## 2. Component Mapping by AIQG Spec Section

### 2.1 §3.1 — Onboarding (env-var redirection)
**State: EXTEND tas-llm-router ingress + BUILD token issuance in dashboard**

Customer sets `OPENAI_BASE_URL=https://gateway.aiqg.tas.io/openai/v1` / `ANTHROPIC_BASE_URL=...` + `TAS-Auth` header. This is a **new ingress URL routed to tas-llm-router** (with a customer-facing TLS cert, no internal-cluster DNS). The customer-facing tokens (`tas_qg_live_...`) are issued by the new `aiqg-dashboard-be` and validated by the existing tas-llm-router auth middleware.

- **Extend** `tas-llm-router/internal/security/auth.go` to recognize the `tas_qg_live_*` token format and resolve to an AIQG account
- **Build** ingress: new K8s `Ingress` resource pointing `gateway.aiqg.tas.io` at the existing `tas-llm-router` service
- **Reuse** token-format conventions from aether-be Keycloak/API patterns

---

### 2.2 §3.2 — Path A Authentication
**State: EXTEND tas-llm-router (per-request mode)**

tas-llm-router today **stores** vendor keys in config (`OPENAI_API_KEY` env var). AIQG's Path A requires vendor keys to flow through the customer's `Authorization` header **and never be stored**. Both modes coexist in the same binary, selected per-request:

- If request has `TAS-Auth: tas_qg_live_*` **and** an inbound `Authorization: Bearer sk-...` header → **Path A**: validate TAS-Auth, forward the `Authorization` header unchanged to the vendor, do not persist anything from it
- Otherwise → fall back to existing behavior (use stored vendor key from config)

Implementation:
- **Additively extend** `tas-llm-router/internal/security/auth.go` — recognize `tas_qg_live_*` tokens as an additional code path; existing TAS API-key validation is untouched and tried first
- **Pass per-request bearer override via `context.Context`** — no change to `LLMProvider` interface or provider method signatures. A new `internal/auth/bearer_context.go` exposes `WithBearer(ctx, token)` and `BearerFromContext(ctx)`; the existing provider implementations read it inside their bodies and prefer it over the config key when present
- **Add** middleware that, on AIQG-mode requests, calls `WithBearer(ctx, customerAuthHeader)` before forwarding
- **Add new** middleware path for `TAS-Upstream-Authorization` (per-request key override for multi-tenant customers) — wires through the same context mechanism
- **Defer** Path B dashboard credential storage to Phase 2 (would be a different mode; trivial extension once Path A ships)

---

### 2.3 §3.3 — Streaming-Native Architecture
**State: EXTEND tas-llm-router providers (90% there, the missing 10% is the entire AIQG value proposition)**

tas-llm-router has functional OpenAI/Anthropic streaming via `StreamCompletion() chan *types.ChatChunk` but **no per-chunk timing capture**.

Required AIQG instrumentation (all new, added inside tas-llm-router):
- DNS resolution timestamp
- TLS handshake completion
- TCP connect time
- Request forwarded (`timestamp_forwarded`)
- TTFB (first byte received from vendor)
- TTFT (first token in stream)
- Inter-token latency (per chunk)
- Last chunk timestamp
- Total stream duration
- Gateway ingress + egress overhead

**Implementation (in tas-llm-router) — all additive, no existing type or signature changes:**
- Add `internal/instrumentation/httptrace.go` — wraps the vendor HTTP client with `net/http/httptrace.ClientTrace` to capture `DNSDone`, `ConnectDone`, `TLSHandshakeDone`, `GotConn`, `GotFirstResponseByte`. The wrapped client is injected via existing HTTP-client provider config; no interface change.
- Add `internal/instrumentation/timing.go` — defines `TimingCollector` and stores one per request in `context.Value(timingKey)`. **The `ChatChunk` type is NOT extended.** Chunk timestamps are stamped into the collector keyed by request context.
- Modify the **body** of `internal/providers/openai/provider.go:StreamCompletion()` and `internal/providers/anthropic/provider.go:StreamCompletion()` to call `timing.StampChunk(ctx)` on each `stream.Recv()` (4–6 line change per provider, no signature change, no struct change). If no collector is attached to the context (non-AIQG callers), the stamp is a no-op.
- Timing snapshot is read at request close via `timing.FromContext(ctx).Snapshot()` and included in the AIQG CloudEvent payload only

---

### 2.4 §3.4 — Multi-Endpoint Coverage
**State: REUSE + EXTEND**

| Endpoint | Status |
|---|---|
| chat/completions, messages | **REUSE** tas-llm-router providers |
| embeddings | **BUILD** in MVP (high-volume cost tracking; no streaming) |
| image generation | **BUILD** Phase 2 |
| audio TTS/STT | **BUILD** Phase 2 |
| files / fine-tuning | Phase 3+ |
| tool-use loops | **EXTEND** Phase 2 — conversation threading layer atop chat |
| batch APIs | Deferred per §5.5 |

---

### 2.5 §3.5 — Policy as Headers / Routes
**State: BUILD inside tas-llm-router**

Header taxonomy (`TAS-Auth`, `TAS-Policy`, `TAS-Policy-Bundle`, `TAS-Workflow`, `TAS-Upstream-Authorization`, `TAS-Trace`, `TAS-Dry-Run`):
- **Add** `tas-llm-router/internal/middleware/aiqg_headers.go` — parses + strips all TAS-\* headers before forwarding
- **Reuse** Gatekeeper's policy bundle pattern (`configs/rules/*.yaml`) as the storage model for named bundles
- **Add** `tas-llm-router/internal/policy/resolver.go` — route-matcher engine (URL + source app + header + workflow + time-of-day → bundle resolution)
- **Build new** the dashboard policy editor in `aiqg-ui` (Phase 2)
- **Reuse** Gatekeeper's dry-run pattern (already supports `tag-only` mode in action engine)

---

### 2.6 §3.6 — Thin Client SDK
**State: BUILD (Phase 3 only — not MVP)**

Defer entirely.

---

### 2.7 §3.7 — Per-Request Capture
**State: EXTEND tas-llm-router CloudEvents publisher**

tas-llm-router today publishes `com.tas.activity.llm.request` and `com.tas.activity.llm.response` via `internal/events/publisher.go`. These lack:
- Latency-decomposition timestamps (see §2.3)
- Workflow classification
- Tag set (Gatekeeper-style quality/policy/NIST tags)
- Sampling decision
- Active-mode action attribution

**Plan:**
- **Add** `tas-llm-router/internal/events/aiqg_v1.go` — new event types `com.tas.aiqg.request.v1` and `com.tas.aiqg.response.v1` extending the existing CloudEvents 1.0 envelope
- **Reuse** `pkg/stream/envelope.go` from Gatekeeper for CloudEvents wrapping (already a dependency)
- **Reuse** Kafka producer config (snappy compression, batching) from existing `internal/events/publisher.go`
- **Emit conditionally** — only when the request is in AIQG mode (per §2.2). Internal-routing traffic keeps emitting `com.tas.activity.llm.*` unchanged

---

### 2.8 §3.8 — Sampling Strategy
**State: BUILD inside tas-llm-router**

100% deterministic checks (token counting, schema validation, Hyperscan tagging) — trivial via Gatekeeper's existing scanner. LLM-as-judge sampling (5–10% small, 1% large) — entirely new:
- **Add** `tas-llm-router/internal/sampling/stratified.go` — stratified sampler (by workflow / customer / anomaly history)
- **Reuse** tas-llm-router's own provider client for judge calls (the proxy uses its own internal routing path to call a judge model — clean dogfooding)
- **Add** `tas-llm-router/internal/judging/prompts/*.tmpl` — judge prompt library

---

### 2.9 §3.9 — Workflow Classification
**State: BUILD inside tas-llm-router (rule pack lives in Gatekeeper config dir)**

Six workflow types (Q&A, RAG, agentic, summarization, code-gen, classification/extraction).
- **Add** `Gatekeeper/configs/rules/aiqg_workflows.yaml` — new rule pack detecting workflow via request shape (delimiters, tool definitions, conversation length, etc.)
- **Reuse** Gatekeeper's Hyperscan engine to run it (zero added latency; the tas-llm-router → Gatekeeper integration already exists)
- **Reuse** the tag attachment mechanism for classifier output
- **Add** `tas-llm-router/internal/workflow/classifier.go` — thin wrapper that calls the Gatekeeper scanner with the AIQG workflow rule pack and writes the result to the request context

---

### 2.10 §3.10 — Gatekeeper Rule-Pack Reuse
**State: REUSE (the central architectural lever)**

What ships today and is directly reusable:
- PII (15+ types), HIPAA, GDPR, SOX, PCI-DSS, CCPA, SOC2, ISO27001, NIST CSF, **NIST AI RMF**, EU AI Act
- Injection detection (SQL, XSS, prompt injection)
- Action engine: block / redact / tokenize / alert / log / webhook / quarantine
- HMAC-attested "match once, tag many" caching
- Kafka streaming to per-framework topics with CloudEvents envelopes
- Databunker PII tokenization

What AIQG adds as new rule packs:
- `aiqg_workflows.yaml` (workflow classifier — see §2.9)
- `aiqg_context_antipatterns.yaml` (fragmented chunks, stale dates, bloat, contradictions)
- `aiqg_prompt_antipatterns.yaml` (conflicting instructions, missing escape conditions)
- `aiqg_output_antipatterns.yaml` (hedge phrases, refusal patterns, leakage)
- `aiqg_behavioral_signals.yaml` (retry markers, abandonment, session continuation)
- `aiqg_clear_assurance.yaml` (NIST AI RMF → CLEAR Assurance dimension scoring)

---

### 2.11 §3.11 — Privacy & Data Handling
**State: REUSE**

- Default no-payload retention: **build new** in gateway (matches default; trivial)
- Sampled payload retention: **reuse** Gatekeeper's existing payload-retain pattern
- PII tokenization: **reuse** `pkg/tokenize/databunker.go` verbatim
- Regional residency: **build new** account-level region selection (data plane already supports it via deployment)
- Customer-owned exports: **build new** dashboard API endpoint

---

### 2.12 §2.1–§2.5 — CLEAR Dimension Measurement
**State: BUILD inside tas-llm-router (with heavy reuse of upstream signals)**

The CLEAR composite scoring engine is net-new. Inputs come from:
- **Cost** — token accounting from response usage fields + vendor pricing tables. **Do not modify existing `EstimateCost()`** (callers depend on it returning a pre-request estimate). Add a new `pkg/clear/cost.ActualCost(usage Usage, pricing PricingTable) Cost` function read after the response completes
- **Latency** — the new chunk-timing instrumentation (§2.3)
- **Efficacy** — Gatekeeper's structural validity scanner + new hedge-phrase / refusal rule packs
- **Assurance** — Gatekeeper's existing compliance findings, mapped to CLEAR Assurance via new rule pack
- **Reliability** — Phase 2 only (requires conversation threading)

**Add to tas-llm-router**:
- `pkg/clear/` scorer package (CNA, CPS, SCR, PAS, pass@k formulas)
- `pkg/clear/cost_decomposer.go` — direct payload waste / induced output waste / genuine post-model waste split
- `pkg/clear/thresholds.go` — Healthy / Marginal / Failing per dimension
- `pkg/clear/composite.go` — composite weighting (per §6.2 open question — default to equal weights for MVP)

Scoring runs at request close inside tas-llm-router (decision §7.2) and ships as part of the AIQG CloudEvent payload — Spark aggregates pre-computed scores, never re-derives them.

---

### 2.13 §4 — UI Flow (8 screens)
**State: BUILD `aiqg-ui` repo from aether patterns**

| Screen | Reuse from aether | Build new |
|---|---|---|
| 1 — Landing | layout + Tailwind theme | marketing copy + hero |
| 2 — Account Creation | AuthContext, authSlice, Keycloak flow | region selector, ToS UX |
| 3 — Quickstart | Modal primitive, code blocks | token display + provider-specific snippet generator |
| 4 — Pre-Report Dashboard | Card primitives, Recharts | progress component, live signal cards |
| 5 — Day-1 Report | layout + Recharts | CLEAR grid, cost-destruction breakdown, latency waterfall, NIST mapping section |
| 6 — Ongoing Dashboard | Sidebar, Card, table primitives | trend charts, workflow-type table |
| 7 — Route Policy Editor (Phase 2) | Modal, Form primitives | route-rule cards, bundle picker, dry-run toggle |
| 8 — Settings | Modal, form primitives | retention + region + team panels |

---

## 3. Backend Service Topology

```
   ┌─────────────────────────────────────────────────────────────────────┐
   │  Customer Application (any language, any framework)                 │
   │     OPENAI_BASE_URL=https://gateway.aiqg.tas.../openai/v1           │
   │     header: TAS-Auth: <customer token>                              │
   └──────────────────────┬──────────────────────────────────────────────┘
                          │ HTTPS, SSE streaming
                          │   (new ingress: gateway.aiqg.tas.io)
                          ▼
   ┌─────────────────────────────────────────────────────────────────────┐
   │  tas-llm-router  (EXISTING — extended in place)                     │
   │                                                                     │
   │  Existing capabilities (reused):                                    │
   │   - OpenAI + Anthropic streaming providers                          │
   │   - Routing / failover / health checks                              │
   │   - Gatekeeper scanning hook (pkg/scan, pkg/action, pkg/attest)     │
   │   - CloudEvents Kafka publisher                                     │
   │   - Auth middleware (TAS API-key validation)                        │
   │                                                                     │
   │  New (added inside this repo):                                      │
   │   - Per-chunk timing capture (net/http/httptrace)                   │
   │   - Path A auth mode (per-request, by TAS-Auth header presence)     │
   │   - TAS-* header taxonomy + dry-run mode                            │
   │   - Workflow classifier (uses new Gatekeeper rule pack)             │
   │   - pkg/clear — CLEAR composite scorer (CNA/CPS/SCR/PAS)            │
   │   - Cost decomposer (direct/induced/genuine waste)                  │
   │   - Stratified LLM-as-judge sampler                                 │
   │   - AIQG CloudEvent types → tas.aiqg.{request,response}.v1          │
   └────┬───────────────────────────────────────────────────┬────────────┘
        │ forwarded request (with customer key OR stored)   │ Kafka
        ▼                                                   ▼
   ┌──────────────┐                            ┌───────────────────────────┐
   │ Vendor APIs  │                            │ aiqg_aggregator (Spark)   │
   │ (OpenAI,     │                            │ - Subscribes tas.aiqg.*   │
   │  Anthropic)  │                            │ - Rolls 1m/1h windows     │
   └──────────────┘                            │ - Writes TimescaleDB      │
                                               │   (postgres-shared)       │
                                               └───────────────┬───────────┘
                                                               │
   ┌─────────────────────────────────────────────────────────────────────┐
   │  aiqg-dashboard-be  (NEW Go repo, copies aether-be patterns)        │
   │  - Gin + Keycloak JWT (realm: aether) + Neo4j (account, policy)     │
   │  - Reads aggregates from TimescaleDB                                │
   │  - Day-1 report generation                                          │
   │  - Issues tas_qg_live_* tokens (validated by tas-llm-router)        │
   │  - Frontend log ingestion (POST /api/v1/logs)                       │
   │  - Prometheus /metrics, /health/{live,ready}                        │
   └──────────────────────┬──────────────────────────────────────────────┘
                          │ HTTPS + JWT
                          ▼
   ┌─────────────────────────────────────────────────────────────────────┐
   │  aiqg-ui  (NEW React repo, copies aether patterns)                  │
   │  - React 19 + Vite + Redux Toolkit + Tailwind v4                    │
   │  - Keycloak realm "aether"                                          │
   │  - Candidate shared packages: @tas/auth-keycloak, @tas/api-client,  │
   │      @tas/logging-client, @tas/ui-primitives, @tas/theme-provider   │
   └─────────────────────────────────────────────────────────────────────┘

   Shared infra (already deployed): Kafka, Redis, PostgreSQL+TimescaleDB,
   Neo4j, Loki, Prometheus, Grafana, Alloy, Keycloak, Databunker, MinIO
```

**Total new repos: 2** (`aiqg-dashboard-be`, `aiqg-ui`). The gateway capability is an extension of `tas-llm-router`.

---

## 4. Service-by-Service Plan

### 4.1 Extend existing `tas-llm-router` (Go)

**Purpose:** The streaming-native LLM proxy capability. **No new repo.** All AIQG gateway features land as additive packages in `tas-llm-router`, gated by per-request mode detection (§2.2).

**Existing surface kept unchanged:**
- `internal/providers/{openai,anthropic}/` — provider clients
- `internal/routing/router.go` — routing strategies + failover
- `internal/gatekeeper/` — Gatekeeper integration (pre-flight scan, finding stream)
- `internal/security/auth.go` — TAS API-key validation (extended; not replaced)
- `internal/events/publisher.go` — CloudEvents publisher (extended with AIQG event types)
- `cmd/llm-router/main.go` — entrypoint (no change to bootstrap)

**New code added to tas-llm-router (proposed paths):**
- `internal/instrumentation/httptrace.go` — DNS/TLS/TTFB capture via `net/http/httptrace`
- `internal/instrumentation/timing.go` — `RequestTiming` struct + chunk timestamp aggregator
- `internal/middleware/aiqg_headers.go` — parses + strips TAS-* headers
- `internal/security/aiqg_auth.go` — recognizes `tas_qg_live_*` tokens, dispatches Path A
- `internal/policy/resolver.go` — route-matcher engine for bundle resolution
- `internal/workflow/classifier.go` — calls Gatekeeper scanner with `aiqg_workflows.yaml`
- `internal/sampling/stratified.go` — LLM-as-judge sampler
- `internal/judging/prompts/*.tmpl` — judge prompt templates
- `pkg/clear/` — CLEAR scorers (CNA, CPS, SCR, PAS, pass@k), cost decomposer, thresholds, composite weighter
- `internal/events/aiqg_v1.go` — `com.tas.aiqg.request.v1` / `com.tas.aiqg.response.v1` types
- `configs/aiqg/` — AIQG-specific deployment overlays (timeouts, route defaults, ingress hints)

**New code added to Gatekeeper (proposed paths):**
- `configs/rules/aiqg_workflows.yaml`
- `configs/rules/aiqg_context_antipatterns.yaml`
- `configs/rules/aiqg_prompt_antipatterns.yaml`
- `configs/rules/aiqg_output_antipatterns.yaml`
- `configs/rules/aiqg_behavioral_signals.yaml`
- `configs/rules/aiqg_clear_assurance.yaml` (NIST AI RMF → CLEAR Assurance mapping)

**Existing types and interfaces are NOT modified.** Per §1.2: chunk timing is captured into a sidecar `instrumentation.TimingCollector` keyed by request context; per-request bearer override flows through `context.Context`; provider method signatures and the `ChatChunk` struct stay exactly as they are today. AIQG-only behavior activates only when an AIQG-mode context value is set.

**Infra:** Kafka (existing), Redis (existing), Databunker (existing). No new dependencies.

**Deployment topology:** the same binary may run as two K8s Deployments behind two Ingresses — one internal-cluster (current behavior, stored vendor keys), one external (`gateway.aiqg.tas.io`, Path A only via config flag forcing AIQG mode). Or one Deployment behind both Ingresses with per-request mode resolution. Operational choice; the codebase is single.

---

### 4.2 New repo: `aiqg-dashboard-be` (Go)

**Purpose:** Dashboard API for the `aiqg-ui` frontend. Account/policy management, report generation, metrics queries.

**Copies verbatim from aether-be:**
- `cmd/server/main.go` bootstrapping (config → logger → DI → graceful shutdown)
- `internal/middleware/auth.go` Keycloak JWT (realm "aether")
- `internal/logger/logger.go` zap setup
- `internal/handlers/logging.go` frontend-log ingestion `POST /api/v1/logs`
- `internal/handlers/health.go` `/health/{live,ready}`
- `internal/metrics/metrics.go` Prometheus
- `internal/config/config.go` env+`.env` pattern
- Repository pattern for Neo4j
- Swag annotations (install `swag` this time — see §4.5)

**New code:**
- `internal/repos/account_repo.go` — AIQG Account Neo4j CRUD
- `internal/repos/policy_repo.go` — policy bundles + route rules CRUD
- `internal/services/report_service.go` — Day-1 report assembly
- `internal/clients/timescale_client.go` — aggregates query client
- `internal/clients/kafka_consumer.go` — drift alert consumer

**Infra:** Neo4j (existing), TimescaleDB on postgres-shared (existing), Keycloak (existing).

---

### 4.3 New repo: `aiqg-ui` (React 19 + TypeScript)

**Purpose:** The customer-facing dashboard. Maps to §4 of the spec (8 screens).

**Copies verbatim from aether (or via shared packages — see §6):**
- Vite + TypeScript + Tailwind v4 + PostCSS config
- Redux Toolkit store layout (subset of slices needed)
- Keycloak `AuthContext` + `authSlice` (realm "aether")
- `aetherApi`-style fetch client → renamed `aiqgApi`
- `services/logging.ts` frontend logger (batched, sendBeacon)
- `ui/Button`, `ui/Modal`, `ui/Tooltip`, `ui/TabButton`, skeleton loaders
- `ThemeContext.jsx` — keep one preset, drop the rest
- ESLint config

**New code:**
- `pages/Landing.tsx`
- `pages/Quickstart.tsx` — token display + per-language code snippet (Python/Node/Go/curl/Ruby)
- `pages/PreReportDashboard.tsx` — progress + live signals
- `pages/Day1Report.tsx` — CLEAR grid + cost-destruction breakdown + latency waterfall + NIST mapping (the screenshot-able artifact)
- `pages/Dashboard.tsx` — ongoing dashboard with workflow breakdown
- `pages/PolicyEditor.tsx` — Phase 2
- `pages/Settings.tsx`
- `components/clear/CLEARGrid.tsx`
- `components/clear/CostDestructionBreakdown.tsx`
- `components/clear/LatencyWaterfall.tsx`
- `components/clear/NistMapping.tsx`
- `store/slices/{account,reports,policies,routes,metrics}Slice.ts`

**Decision needed (§6):** which patterns to extract as shared packages vs. copy-paste for v1. Recommendation: copy-paste for MVP, refactor to shared packages once both repos have shipped.

---

### 4.4 Extend `tas-spark-jobs`

**Purpose:** Roll per-request AIQG events into 1m / 5m / 1h / 1d aggregates feeding the dashboard.

**Reuses:**
- `jobs/events_aggregator/main.py` structure — Structured Streaming, CloudEvents schema, S3A checkpoint, staging-table upsert
- `schema.py` CloudEvents 1.0 base

**New code:**
- `jobs/aiqg_aggregator/main.py` — subscribes to `tas.aiqg.request.v1` / `tas.aiqg.response.v1`
- `jobs/aiqg_aggregator/schema.py` — AIQG-extended schema
- `jobs/aiqg_aggregator/clear_scoring.py` — composite computation (delegates to the same `pkg/clear` logic? — open question; could keep all scoring in `aiqg-gateway` and have Spark only aggregate)
- TimescaleDB DDL — hypertables: `aiqg_requests`, `aiqg_metrics_1m`, `aiqg_metrics_1h`, continuous aggregates configured

**Decision:** §7.3 — TimescaleDB scales to ~50K events/min/customer with continuous aggregates. For MVP this is comfortable. If a customer crosses 100K events/min sustained, revisit ClickHouse (deferred).

---

### 4.5 Extend `shared-monitoring`

**Purpose:** Grafana dashboards + Prometheus alert rules for AIQG SLOs.

**New artifacts:**
- `grafana/dashboards/aiqg-clear-composite.json` — CLEAR scores by workflow × period
- `grafana/dashboards/aiqg-latency-decomposition.json` — DNS/TLS/TTFB/TTFT/inter-token waterfall by route × model
- `grafana/dashboards/aiqg-cost-destruction.json` — $ by category (direct/induced/genuine) × workflow × top-N customers
- `grafana/dashboards/aiqg-drift-detection.json` — rolling-window deltas, anomaly markers
- `prometheus/alerts/aiqg-slos.yml` — gateway overhead > 50ms p99; CLEAR composite drop > 10%/h; cost anomaly > 20%

**Alloy config update:** `aether-shared/k8s-shared-infrastructure/logging/alloy-configmap.yaml` — add namespaces `aiqg-prod`, `aiqg-gateway`, `aiqg-dashboard-be` to the keep regex.

---

### 4.6 Update existing CLAUDE.md files

Per the data-models survey, five existing docs need cross-service updates — **all additive, none changes existing schema shape**:
- `keycloak/users/user-model.md` — document `aiqg_account_id` as an *optional* Keycloak custom attribute (Keycloak attributes are name-keyed; adding one does not change the user schema for callers that don't read it)
- `aether-be/nodes/space.md` — annotative note: 1:1 mapping to AIQG account exists where the account row references the Space's `tenant_id`. No new properties or relationships on the Space node itself
- `tas-llm-router/request-format.md` — annotative note: AIQG mode captures the full `ChatRequest` payload into a separate CloudEvent. The `ChatRequest` schema itself is unchanged
- `cross-service/flows/` — new file `aiqg-request-lifecycle.md` (new doc, no edits to existing flows)
- `cross-service/mappings/id-mapping-chain.md` — append AIQG provisioning chain (additive section, existing chains untouched)

---

## 5. Infrastructure Dependencies

| Component | Status | Action |
|---|---|---|
| Kafka | Deployed (`kafka-shared`) | Add topics `tas.aiqg.request.v1`, `tas.aiqg.response.v1`, `tas.aiqg.findings.v1` |
| Redis | Deployed | Reuse Gatekeeper attestation cache |
| PostgreSQL + TimescaleDB | Deployed | Add `aiqg` database with hypertables |
| Neo4j | Deployed (`neo4j.aether-be.svc`) | Add labels `AIQGAccount`, `PolicyBundle`, `RouteRule` |
| Keycloak (realm `aether`) | Deployed | Add clients `aiqg-dashboard-be` (confidential), `aiqg-ui` (public) |
| Databunker | Deployed | Reuse for PII tokenization |
| Loki + Alloy | Deployed | Add AIQG namespaces to Alloy keep regex |
| Prometheus | Deployed | Add AIQG scrape jobs, alert rules |
| Grafana | Deployed | Add 4 dashboards |
| MinIO | Deployed | Use existing buckets for Spark checkpoints + sampled payload retention |
| Spark | Deployed (`tas-spark-jobs`) | Add new aggregator job |
| **ClickHouse** | **NOT deployed** | **Deferred** — TimescaleDB sufficient for MVP scale (see §7.3) |

**No net-new infrastructure required for MVP.**

---

## 6. Shared Frontend Packages (Recommendation)

Per the aether frontend survey, extract these from `aether/` after `aiqg-ui` v1 ships (not before — premature extraction risks coupling):

| Package | Source (aether) | Used by |
|---|---|---|
| `@tas/auth-keycloak` | `src/store/slices/authSlice.js`, `src/contexts/AuthContext.jsx`, `src/services/tokenStorage.js` | aether + aiqg-ui |
| `@tas/api-client` | `src/services/aetherApi.js` (core fetch wrapper, interceptors) | aether + aiqg-ui |
| `@tas/logging-client` | `src/services/logging.ts` | aether + aiqg-ui |
| `@tas/ui-primitives` | `src/components/ui/{Button,Modal,Tooltip,TabButton,StatusBadge}.jsx` + skeletons | aether + aiqg-ui |
| `@tas/theme-provider` | `src/context/ThemeContext.jsx` + Tailwind v4 base config | aether + aiqg-ui |

**MVP approach:** copy the code into `aiqg-ui` v1. Refactor to shared packages in Phase 2 once both apps have proven the surface area.

---

## 7. Decisions

All decisions resolved 2026-05-31. Recorded here as the source of truth for downstream architecture specs. Each decision lists rationale and the main tradeoff so future revisits can be reasoned about.

### 7.1 Hot analytics store — **DECIDED: TimescaleDB**

**Date:** 2026-05-31

**Resolution:** Use TimescaleDB on the existing `postgres-shared` cluster. Add a new `aiqg` database with hypertables for raw events and continuous aggregates for 1m / 5m / 1h / 1d windows.

**Rationale:** Already the destination of `tas-spark-jobs/events_aggregator`. Adding ClickHouse contradicts the "use existing infrastructure stack" constraint. Hypertables + continuous aggregates handle ~50K events/min/customer; MVP pilot customer profile is well below that.

**Tradeoff:** At 100K+ events/min/customer sustained, raw-event query latency degrades. Mitigation: pre-aggregate aggressively (only the 1m hypertable ever sees raw event volume); dashboard queries hit pre-aggregated tables. Re-platforming to ClickHouse later is a clean Spark sink swap.

**Trigger to revisit:** any single customer crosses 30K events/min sustained for a week, or aggregate query p95 latency on the dashboard exceeds 500ms.

---

### 7.2 CLEAR scoring location — **DECIDED: gateway-side (Go)**

**Date:** 2026-05-31

**Resolution:** Compute CLEAR composite inside `tas-llm-router` at request close. The `pkg/clear/` package emits final scores as part of the AIQG CloudEvent payload. Spark aggregates pre-computed scores; it does not re-derive them.

**Rationale:** One scoring code path. Day-1 report numbers are deterministic regardless of late-arriving events. Avoids Python/Go dialect sprawl on the same formulas.

**Tradeoff:** Re-scoring with a new CLEAR formula requires a gateway redeploy + period re-run.

**Mitigation:** Every emitted score carries a `scoring_version` field. If historical re-scoring becomes valuable, build a one-off Spark job that reads raw signals from the events table and computes new scores tagged with the new version.

---

### 7.3 Path A enforcement — **DECIDED: strict**

**Date:** 2026-05-31

**Resolution:** The customer-facing ingress (`gateway.aiqg.tas.io`) returns `401 Unauthorized` for any request missing **either** `TAS-Auth: tas_qg_live_*` **or** a customer `Authorization: Bearer ...` header. No fallback to stored keys on the external ingress.

Internal callers (tas-agent-builder, aether-be, audimodal, llm-invocation) keep using the internal ingress + stored vendor keys exactly as today — they never carry a `TAS-Auth` header, so they never trigger AIQG mode.

**Rationale:** "We never hold your keys" is one of the strongest competitive differentiators per spec §6.1.13. It pre-empts the LiteLLM-style supply-chain compromise risk. A marketing claim we can defend technically.

**Tradeoff:** Customers who paste curl examples without the required headers get 401s on first try.

**Mitigation:** The `aiqg-ui` quickstart screen generates copy-paste-ready code snippets per language (Python / Node / Go / curl / Ruby) that always include both headers. A diagnostic 401 response body explains exactly which header is missing.

---

### 7.4 Shared package extraction timing — **DECIDED: copy-paste for MVP, extract after v1**

**Date:** 2026-05-31

**Resolution:** `aiqg-ui` v1 copies the relevant files from `aether/` directly into its own `src/`. No `@tas/*` npm packages or monorepo workspaces are created for the MVP. Refactor to shared packages once both apps have shipped v1 and the genuinely-shared surface area is proven.

**Rationale:** Premature abstraction risks coupling a shared package to aether's needs before aiqg-ui's actual requirements are known. Extracting before the second use is stable is the canonical anti-pattern.

**Tradeoff:** Short-term duplication; bug fixes need to land twice for the window between aiqg-ui ship and shared-package extraction.

**Mitigation:** Copied files keep their original filenames and relative paths (`src/services/logging.ts`, `src/contexts/AuthContext.jsx`, etc.) so the eventual extraction is a mechanical `git mv` plus light cleanup. Annotate each copied file's header with `// SOURCE: aether/src/.../<file> @ <commit>` to make drift visible.

---

### 7.5 Spec §6 inherited open-question triage

**Date:** 2026-05-31

The AIQG v0.2 spec leaves 14 questions for stakeholders. Most are GTM/positioning and don't block the architecture. Four block the architecture spec and are resolved here with MVP defaults; the remainder are deferred to the appropriate phase.

**Architecture-blocking (resolved with MVP defaults):**

| Spec § | Question | Resolution |
|---|---|---|
| §6.2.15 | CLEAR composite weighting | **Equal weight (0.2 each dimension)** — CLEAR's published default. `account.scoring_weights` schema field reserved for Phase 2 customer-configurable weighting. |
| §6.2.16 | Threshold calibration (Healthy / Marginal / Failing) | **Use the spec's published thresholds**: Healthy ≥75 / Marginal 50–74 / Failing <50 for Cost, Latency, Efficacy, Reliability. Assurance stricter at Healthy ≥90 / Marginal 75–89 / Failing <75 (asymmetric consequence per spec §2.4). Recalibrate against real customer data after 10 paying customers, or any threshold demonstrates >20% misclassification rate against operator judgment, whichever first. |
| §6.3.18 | Policy bundle taxonomy | **MVP ships 4 starter bundles**: `production_strict`, `development_lenient`, `pii_strip`, `audit_full`. PCI/HIPAA/SOC2/GDPR vertical bundles deferred to Phase 3 compliance work. Customer-defined bundles deferred to Phase 3. |
| §6.3.19 | Route matcher expressiveness for v1 | **MVP matchers**: URL path + source identifier (which TAS-Auth token sent it) + customer-header value match. Workflow-type and time-window matchers deferred to Phase 2 (they depend on fine-grained workflow classification, itself a Phase 2 build). |

**Deferred (not architecture-blocking):**

| Spec § | Question | Phase / status |
|---|---|---|
| §6.1.11 | Pricing model (free with cap vs. trial-then-paid) | GTM. Resolve before public launch, not before MVP build |
| §6.1.12 | Upsell aggressiveness into broader TAS platform | GTM |
| §6.1.13 | "We never hold your keys" as marketing headline | Position locked by §7.3 above; marketing copy is GTM |
| §6.1.14 | OSS for the thin client SDK | Phase 3; revisit when SDK design is close to ship |
| §6.2.17 | Input Quality as future CLEAR sixth dimension | Methodology research; not implementation-blocking |
| §6.3.20 | Multi-tenant policy management RBAC | Phase 3 |
| §6.3.21 | Stored-key (Path B) introduction timing | Phase 2; trivial extension of the Path A mechanism |
| §6.4.22 | Customer acquisition motion / GTM owner | GTM |
| §6.4.23 | Conversion mechanism from self-service to higher tiers | GTM |
| §6.4.24 | Cannibalization risk vs. broader TAS platform | GTM |

---

## 8. Data Models — Files to Author Under `aiqg/`

Per the data-models survey, 16 new docs are required. All follow the mandatory 14-section template (Overview → Schema → Relationships → Validation → Lifecycle → Examples → Cross-Service → Tenancy → Performance → Security → Migration → Issues → Related → Changelog).

| File | Purpose |
|---|---|
| `account.md` | AIQG Account (1:1 Space, retention, region, quotas) |
| `request-event.md` | Per-request capture envelope |
| `response-event.md` | Per-response capture envelope |
| `token-accounting.md` | input/output/cached/tool tokens + vendor pricing |
| `event-timestamps.md` | DNS/TLS/TTFB/TTFT/inter-token/last-chunk/complete |
| `request-structure.md` | system/user/history/tools/context blocks snapshot |
| `response-structure.md` | response text/tool_calls/finish/logprobs/validity |
| `inferred-labels.md` | workflow_type, retry_of_previous, abandonment, hedge |
| `tag-set.md` | quality + policy + NIST AI RMF tags |
| `policy-bundle.md` | named, versioned policy collections |
| `policy-rule.md` | individual rule definitions |
| `route-rule.md` | URL/header/source/workflow/time → bundle matchers |
| `audit-log-entry.md` | immutable policy-application audit |
| `aggregated-metrics.md` | rolled-up CLEAR scores per workflow/route/account |
| `report-snapshot.md` | frozen Day-1 + periodic reports |
| `workflow-classification.md` | the six-type taxonomy + detection signals |

Plus updates to 5 existing docs (§4.6 above).

---

## 9. Phasing — Mapping to Spec §5

### MVP (Phase 1) — what ships
- `tas-llm-router` (extended): streaming OpenAI + Anthropic chat/messages with Path A auth mode, chunk-timing capture, TAS-\* headers, workflow classifier, CLEAR scorer, AIQG events to Kafka
- 3 CLEAR dims measured fully (Cost, Latency, Assurance via Gatekeeper)
- 2 CLEAR dims heuristic (Efficacy = structural validity + hedge; Reliability = consistency proxy)
- Coarse workflow classification (chat / embeddings / images / audio)
- `aiqg-dashboard-be`: account, settings, report endpoints
- `aiqg-ui`: screens 1–5 + screen 8 (Settings, no route editor)
- `tas-spark-jobs/aiqg_aggregator`: 1m / 1h rollups to TimescaleDB
- 4 Grafana dashboards (read-only for internal ops)
- 6 AIQG rule packs in Gatekeeper

### Phase 2
- Payload reduction (request transformation inside `tas-llm-router`)
- Route policy editor (UI screen 7)
- Full Efficacy + Reliability via conversation threading
- Fine-grained 6-type workflow taxonomy
- Bedrock + Vertex support
- Multi-endpoint expansion (images, audio quality measurement)
- Drift alerting
- Path B (stored vendor keys) as enterprise opt-in

### Phase 3
- Thin-client SDK (Python first)
- Outcome webhook integration
- Custom rule packs (customer-authored)
- Eval set management
- Compliance-vertical bundles (PCI/HIPAA/SOC2/GDPR)
- RBAC + approval workflows

---

## 10. Wire-Compat Checklist

Concrete gates the AIQG extension must pass before merging to `main`. Each item is a runnable check, owned by CI or a named team member. None of these tests should require new dependencies — they all exist or can be added trivially to the current `make test` / `make ci` pipelines.

The principle: **a binary built from the AIQG feature branch, deployed with no AIQG-specific config and no AIQG-tagged requests, must behave exactly like today's binary on every observable surface.**

### 10.1 HTTP endpoint contracts (tas-llm-router)

For each existing endpoint, replay a canned request fixture and diff the response shape (status, headers, body schema) against the same fixture replayed against `main`:

| Endpoint | Fixture | Owner |
|---|---|---|
| `POST /v1/chat/completions` (non-streaming) | `testdata/contract/chat_basic.json` | `make test-contract` |
| `POST /v1/chat/completions?stream=true` | `testdata/contract/chat_stream.json` | `make test-contract` |
| `GET /v1/providers` | `testdata/contract/providers_list.json` | `make test-contract` |
| `GET /v1/providers/{name}` | `testdata/contract/provider_detail.json` | `make test-contract` |
| `GET /v1/health` | `testdata/contract/health.json` | `make test-contract` |
| `GET /v1/health/{name}` | `testdata/contract/health_provider.json` | `make test-contract` |
| `GET /v1/capabilities` | `testdata/contract/capabilities.json` | `make test-contract` |
| `POST /v1/routing/decision` | `testdata/contract/routing_decision.json` | `make test-contract` |
| `GET /health` | exact-match `{"status":"ok"}` | `make test-contract` |

**New `Makefile` target proposed: `make test-contract` runs `go test ./internal/contract/...` which spins up the server, hits each endpoint, and JSON-diffs against the fixtures.** Fixtures are recorded once from `main` and committed.

### 10.2 Provider interface stability

Compile-time guard that nothing new was added to `LLMProvider`:

```go
// /internal/providers/contract_test.go
package providers_test

import (
    "testing"
    "github.com/Tributary-ai-services/tas-llm-router/internal/providers"
    "github.com/Tributary-ai-services/tas-llm-router/internal/providers/openai"
    "github.com/Tributary-ai-services/tas-llm-router/internal/providers/anthropic"
)

func TestLLMProviderInterfaceStability(t *testing.T) {
    // these var declarations fail to compile if signatures drift
    var _ providers.LLMProvider = (*openai.Provider)(nil)
    var _ providers.LLMProvider = (*anthropic.Provider)(nil)
}
```

Plus a **method-count assertion** via reflection that fails if the interface gains a method:

```go
func TestLLMProviderMethodCount(t *testing.T) {
    iface := reflect.TypeOf((*providers.LLMProvider)(nil)).Elem()
    if got, want := iface.NumMethod(), 6; got != want {
        t.Fatalf("LLMProvider gained methods: have %d, want %d (frozen for AIQG extension)", got, want)
    }
}
```

### 10.3 Struct shape stability

Snapshot the JSON marshaling of every public type and assert byte-equality. Run on every PR:

```go
// /internal/types/snapshot_test.go
func TestChatChunkJSONShape(t *testing.T) {
    chunk := types.ChatChunk{ID: "x", Created: 1, Model: "m"}
    got, _ := json.Marshal(chunk)
    want := `{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":null}`
    if string(got) != want { t.Fatalf("ChatChunk shape changed: %s", got) }
}
```

Cover at minimum: `ChatChunk`, `ChatRequest`, `ChatResponse`, `Message`, `ContentPart`, `ToolCall`, `Usage`, `CostEstimate`, `RoutingDecision`, `RouterMetadata`, `ProviderHealth`, `ModelInfo`.

### 10.4 CloudEvent payload stability

Existing event types must keep their exact payload schema. Verify by emitting a fixture and validating against the current JSON schema:

| Event type | Topic | Schema fixture |
|---|---|---|
| `com.tas.activity.llm.request` | `tas.activity.llm` | `testdata/events/activity_llm_request.schema.json` |
| `com.tas.activity.llm.response` | `tas.activity.llm` | `testdata/events/activity_llm_response.schema.json` |

Test: build a request fixture, run it through the extended binary in non-AIQG mode, capture the published event, validate against the committed JSON schema. **Adding fields is a regression.**

New AIQG event types (`com.tas.aiqg.request.v1`, `com.tas.aiqg.response.v1`) have their own fixtures and schemas — these are net-new and do not constrain the existing ones.

### 10.5 Kafka topic continuity

Verify the existing topic set is unchanged:

```bash
# Before AIQG extension
kubectl exec -n tas-shared kafka-shared-0 -- \
  kafka-topics.sh --bootstrap-server localhost:9092 --list > /tmp/topics-main.txt

# After AIQG extension (in feature-branch environment)
kubectl exec -n tas-shared kafka-shared-0 -- \
  kafka-topics.sh --bootstrap-server localhost:9092 --list > /tmp/topics-aiqg.txt

# Must show: only ADDITIONS (tas.aiqg.*), no DELETIONS or RENAMES
diff /tmp/topics-main.txt /tmp/topics-aiqg.txt
```

### 10.6 Spark job continuity

`tas-spark-jobs/events_aggregator` subscribes to `tas.(activity|compliance).*`. Run the job unchanged against a feature-branch deployment for 24h and verify:

- Input event rate matches baseline (±5%)
- TimescaleDB `events` table row count growth matches baseline
- No deserialization errors logged in Spark
- No `events_aggregator` restarts

This is a smoke test, owned by the data-platform team. Pass criterion documented in `tas-spark-jobs/test/AIQG-COMPAT.md` (new file).

### 10.7 Internal-caller smoke tests

Each downstream service must build and pass its own test suite against an AIQG-extended `tas-llm-router` image (no code change in the caller):

| Caller | Test command | Notes |
|---|---|---|
| `tas-agent-builder` | `make test && make test-integration` | calls `/v1/chat/completions` via internal DNS |
| `aether-be` | `make test && make test-integration` | calls via document-AI features |
| `audimodal` | `make test` | document analysis path |
| `llm-invocation` | `make test` | the generic client library |

CI pipeline proposal: a `tas-llm-router` PR that touches `internal/providers/**`, `internal/types/**`, `internal/security/auth.go`, `internal/events/**`, or `cmd/**` triggers the downstream test matrix as a required check.

### 10.8 Prometheus metric set

Existing metric names + label sets must be byte-stable. Scrape `/metrics` from both binaries, sort, and diff:

```bash
curl -s http://main-binary:8085/metrics    | grep -E '^(llm_router|http|go|process)_' | sort > /tmp/metrics-main.txt
curl -s http://aiqg-binary:8085/metrics    | grep -E '^(llm_router|http|go|process)_' | sort > /tmp/metrics-aiqg.txt
diff /tmp/metrics-main.txt /tmp/metrics-aiqg.txt
# Expected: only additions of aiqg_* metrics; no llm_router_* renames or removals
```

Owned by SRE. Run as part of `make test-contract`.

### 10.9 Config compatibility

A deployment using the **existing** `config.yaml` (no `aiqg:` block, no AIQG env vars) must boot, accept traffic, and route exactly as `main`:

```bash
# CI test in /home/jscharber/eng/TAS/tas-llm-router/configs/test/
go run cmd/llm-router/main.go --config configs/config.example.yaml
# Wait for /v1/health to return 200; assert no AIQG features active in logs
curl -s http://localhost:8085/v1/health | jq -e '.aiqg_mode == null or .aiqg_mode == false'
```

The AIQG config struct (`Config.AIQG`) must have a zero value that disables every AIQG feature — instrumentation overhead, event emission, classification, scoring. CI asserts this via a fuzzed config test.

### 10.10 Existing rule-pack output stability (Gatekeeper)

The 10+ existing rule packs (`pii.yaml`, `hipaa.yaml`, `gdpr.yaml`, ...) must produce identical findings against a canned input corpus before and after the AIQG additions:

```bash
# Replay a corpus of 1000 representative payloads through the scanner
go test -tags=hyperscan ./pkg/scan/regression_test.go -run TestExistingRulePackStability
# Compares finding sets (sorted by pattern_id, location) against committed baseline JSON
```

New AIQG rule packs (`aiqg_*.yaml`) only emit findings when explicitly enabled in the scanner profile, so they cannot pollute existing pack outputs.

### 10.11 Publisher interface stability

The Gatekeeper `pkg/stream.Streamer` and tas-llm-router `events.Publisher` interfaces must not gain methods. Same compile-time + reflection check as §10.2:

```go
func TestPublisherInterfaceStability(t *testing.T) {
    iface := reflect.TypeOf((*events.Publisher)(nil)).Elem()
    if got, want := iface.NumMethod(), 2; got != want {
        t.Fatalf("Publisher gained methods: have %d, want %d", got, want)
    }
}
```

### 10.12 Gate summary

| Check | Where it runs | Required for merge |
|---|---|---|
| 10.1 HTTP endpoint contracts | `make test-contract` | ✓ |
| 10.2 LLMProvider interface compile-guard + count | `go test ./internal/providers/...` | ✓ |
| 10.3 Struct JSON shape snapshots | `go test ./internal/types/...` | ✓ |
| 10.4 CloudEvent schema validation | `make test-contract` | ✓ |
| 10.5 Kafka topic diff | Manual / staging deploy | ✓ |
| 10.6 Spark job 24h soak | Data-platform team | ✓ |
| 10.7 Downstream caller tests | CI matrix | ✓ |
| 10.8 Prometheus metric diff | `make test-contract` | ✓ |
| 10.9 Default-config boot test | `make test` | ✓ |
| 10.10 Gatekeeper rule-pack regression | `go test -tags=hyperscan ./pkg/scan/...` | ✓ |
| 10.11 Publisher interface guards | `go test ./internal/events/...` | ✓ |

A PR that touches any file in tas-llm-router or Gatekeeper that this checklist names must include passing results for all twelve items before review.

---

## 11. Next Steps

1. **Stakeholder sign-off on the build-vs-reuse mapping** (this doc)
2. **Resolve decisions in §7** — TimescaleDB vs. ClickHouse; CLEAR scoring location; Path A enforcement
3. **Author the 16 data-model docs under `aiqg/`** (~2 days)
4. **Draft architecture specs** for the AIQG work:
   - `tas-llm-router/docs/AIQG-EXTENSION.md` (new file in existing repo describing AIQG additions)
   - `aiqg-dashboard-be/ARCHITECTURE.md` (new repo)
   - `aiqg-ui/ARCHITECTURE.md` (new repo)
5. **Draft OpenAPI** for `aiqg-dashboard-be` and the new AIQG-mode endpoints exposed by `tas-llm-router`
6. **Update existing docs** (§4.6) with cross-service AIQG references
7. **Branch & scaffold** the two new repos + AIQG feature branch on `tas-llm-router` (do NOT push to main per [CLAUDE.md branching rule](../../../CLAUDE.md))
