# AIQG Token

---

**Metadata**

```yaml
service: aiqg-dashboard-be
model: Token
database: PostgreSQL (shared instance, schema `aiqg`)
table: aiqg.token
version: 1.2.0
last_updated: 2026-11-20
status: implemented (migration 001; scopes + expires_at added in 036, aiqg-dashboard-be#177; owner_sub + owner_email added in 052, aiqg-dashboard-be#255)
go_struct: aiqg-dashboard-be/internal/models/token.go
```

---

## 1. Overview

### Purpose
`aiqg.token` holds the customer-facing bearer tokens, `tas_qg_live_` followed by 40 hex characters. A customer puts one in an SDK to send traffic through the gateway. Since migration 036, a token can also carry **data scopes** that let it read the tenant's own data through the customer data MCP server (Plan #20).

This document was referenced as `[[token]]` from `account.md` before it existed. It was written when 036 changed the model, from the schema and the code rather than from earlier design notes.

### Ownership
- **Owning service:** `aiqg-dashboard-be` (issue, list, revoke, resolve).
- **Writers:** a dashboard user with a Keycloak JWT carrying `tenant_id`, through `POST /api/v1/account/tokens`. Any authenticated tenant user can mint a token today; there is no admin gate.
- **Read-only consumers:**
  - `tas-llm-router`, through `POST /internal/auth/validate`. It ignores scopes.
  - `DataPlaneAuth` on `POST /api/v1/feedback`. It ignores scopes.
  - `aiqg-data-mcp`, the customer data MCP server. It **requires** a data scope.

### Security properties
- The plaintext is returned exactly once, on issuance. Only its SHA-256 is stored.
- SHA-256 without stretching is deliberate. A token carries 160 random bits, and stretching protects low-entropy secrets, not keys.
- `token_prefix` (the first 16 characters) is kept so the UI can show which token is which without showing the secret.

---

## 2. Schema Definition

```sql
CREATE TABLE aiqg.token (
    token_id        UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    aiqg_account_id UUID NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,       -- hex SHA-256 of the plaintext
    token_prefix    TEXT NOT NULL,              -- first 16 chars, for display
    source_app      TEXT NOT NULL DEFAULT '',
    label           TEXT NOT NULL DEFAULT '',
    suspended       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    scopes          TEXT[] NOT NULL DEFAULT '{}',   -- 036
    expires_at      TIMESTAMPTZ,                    -- 036; NULL = never
    owner_sub       TEXT,                           -- 052; NULL = unowned
    owner_email     TEXT                            -- 052; display only
);
-- Partial indexes on (tenant_id) and (token_hash), both WHERE revoked_at IS NULL.
```

---

## 3. Fields Reference

| Field | Type | Notes |
|---|---|---|
| `token_id` | UUID | Random v4, generated in Go. |
| `tenant_id` | UUID | From the issuer's JWT, never from the request body. Compared as lowercase canonical text against TimescaleDB's TEXT `tenant_id`. |
| `aiqg_account_id` | UUID | Currently the tenant ID as a placeholder (`handlers/tokens.go`). |
| `token_hash` | TEXT | Hex SHA-256 of the plaintext; the lookup key. |
| `token_prefix` | TEXT | Display only. |
| `source_app` | TEXT | Free text set at issuance; stamped onto feedback rows. |
| `label` | TEXT | Human-readable name. |
| `suspended` | BOOLEAN | A suspended token resolves but is refused with 403. |
| `revoked_at` | TIMESTAMPTZ | Set by `DELETE /api/v1/account/tokens/:id`. A revoked token never resolves. |
| `last_used_at` | TIMESTAMPTZ | Best-effort update on every resolve. |
| `scopes` | TEXT[] | Data-access scopes. **Empty means a gateway and feedback token that cannot read data.** Every token minted before 036 has `{}`. |
| `expires_at` | TIMESTAMPTZ | NULL means no expiry. A token past this instant resolves as not found, exactly like a revoked one. |
| `owner_sub` | TEXT | Keycloak `sub` of the person who issued the token, from their JWT. The authenticated "who" behind every request made with it (Plan #22 P1). NULL means **unowned**: minted before the audit log, by SQL, or by a system job (the SDLC labeling worker). The demo-flow and audit-ask ephemeral tokens are owned by the person who ran them. |
| `owner_email` | TEXT | The issuer's email, for display on the token list only. **Never** returned by `/internal/auth/validate` and never put on an event (design §12). |

### Owner backfill (migration 052)

052 set `owner_sub` and `owner_email` from the `token.issued` row in `aiqg.audit_log`, matching `resource_id = token_id` **and** the tenant, never overwriting an existing owner. On 2026-11-20 that was 3 of 644 tokens, the only ones issued through the API since auditing began. The rest stay unowned.

### Scopes

| Scope | Grants |
|---|---|
| `data:usage` | Usage and cost tools (requests, cost, tokens, latency, breakdowns). |
| `data:quality` | Quality and policy tools (Phase 1, not yet built). |
| `data:events` | Per-request event listing (Phase 2). |
| `data:identity` | Widens the others to include `user_id` and `principal_id`. Grants nothing on its own. |

The valid set is enforced in Go (`models.ValidTokenScopes`), not in SQL, so adding a scope needs no migration.

---

## 4. Validation Rules (issuance)

- An unknown scope is rejected with `400 invalid_scopes`.
- `data:identity` without another data scope is rejected.
- Scopes are de-duplicated and sorted before storage, so the stored value and the audit row are stable.
- `expires_in_days` must be between 0 (never) and 365.
- Omitting `scopes` and `expires_in_days` gives exactly the pre-036 token.
- `team_id` (Plan #22 5.5) places the token in a team at issue. **Since AIQG-101 (aiqg-dashboard-be 0.45.0) it is required** whenever the org structure is on, so no new token is unassigned:
  - Missing, blank, archived or not a team of the org: `400 team_required`. An org with no teams at all: `409 org_has_no_teams` (create one in Settings ▸ Organization first).
  - The caller must be a member or manager of that team, an org owner or admin, or a platform operator. Otherwise `403 placement_forbidden`. (Members were added by AIQG-101; moving an existing token still needs a manager.)
  - The placement row starts at the token's `created_at`. It and its `org.token.placed` audit commit in the token's own transaction, so a failed placement leaves no token.
  - With `ORG_STRUCTURE_ENABLED` off, a `team_id` is `400 org_structure_disabled`, and tokens are issued without one as before.
- Tokens AIQG mints for itself (demo-flow runs, SDLC Mode C ticks, audit asks) are placed in the org's default team (`aiqg.org_default_team`, `GET/PUT /api/v1/org/default-team`, owners/admins, audited `org.default_team.set`). With no usable default they are refused `team_required` like a person who picks none.
- Tokens issued before AIQG-101 with no team keep working (validation is unchanged) and are never placed automatically; org owners/admins place them from Settings ▸ Organization ▸ Place these tokens, and get one deduplicated inbox message (`org.tokens.unplaced`) while any remain.

---

## 5. Lifecycle

```
issued ──(DELETE)──> revoked          never resolves again
   │
   ├──(expires_at passes)──> expired  resolves as not found
   │
   └──(suspended = true)──> suspended resolves, refused with 403
```

---

## 6. API

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/api/v1/account/tokens` | JWT | Active tokens for the tenant, newest first. `scopes` is always an array, never null. `team_id` is the placement in effect now (absent = unplaced). **D11 (SEC-47, 0.44.1):** `owner_sub` / `owner_email` are returned only to `aiqg-admin` (never in a switched org), org owners/admins, managers of the token's team or a team above it, and the owner; anyone else gets the token with `owner_hidden: true` and no owner fields. |
| `POST` | `/api/v1/account/tokens` | JWT | Body `{label, source_app, scopes?, expires_in_days?, team_id}`. `team_id` is required with the org structure on (§4, AIQG-101). 201 with `plaintext_bearer`, shown once, and `team_id`. |
| `DELETE` | `/api/v1/account/tokens/:id` | JWT | Tenant-scoped: 404 for another tenant's token. |
| `POST` | `/internal/auth/validate` | Internal-Auth | Body `{bearer}`. 200 `{token_id, tenant_id, aiqg_account_id, source_app, suspended, label?, owner_sub?}`, 401 unknown, 403 suspended, 500 resolver down. `label` and `owner_sub` (052) are omitted when empty. |

---

## 7. Cross-Service Integration

- **Gateway (`tas-llm-router`)** resolves tokens through `/internal/auth/validate` and does not read `scopes`. A data scope never changes what a token can do on the gateway. It stamps `tas_auth_token_id` and `token_owner_sub` on the response event, and the aggregator copies them to `aiqg.event_metrics.token_id` / `owner_sub` (TimescaleDB migration 007).
- **Customer data MCP server (`aiqg-data-mcp`)** resolves the token directly, then:
  - 401 when it is unknown, revoked or expired;
  - 403 when it is suspended or carries no `data:*` scope;
  - 503 when the store is unreachable, so the failure is retryable.

  It pins the token's `tenant_id` for each TimescaleDB transaction and reads only the `aiqg_mcp` views, as the `aiqg_mcp_reader` role. See `k8s-shared-infrastructure/timescaledb-migrations/005_aiqg_mcp_read_layer.sql`.

---

## 8. Related Documentation

- [[account]] — the account a token belongs to.
- [[response-feedback]] — the other data-plane consumer of these tokens.
- Customer data MCP server design (Plan #20): https://claude.ai/code/artifact/ee8f2315-4ff5-4cdd-8095-7fee5c7d0b43
