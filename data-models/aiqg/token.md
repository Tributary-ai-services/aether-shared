# AIQG Token

---

**Metadata**

```yaml
service: aiqg-dashboard-be
model: Token
database: PostgreSQL (shared instance, schema `aiqg`)
table: aiqg.token
version: 1.1.0
last_updated: 2026-10-13
status: implemented (migration 001; scopes + expires_at added in 036, aiqg-dashboard-be#177)
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
    expires_at      TIMESTAMPTZ                     -- 036; NULL = never
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
| `GET` | `/api/v1/account/tokens` | JWT | Active tokens for the tenant, newest first. `scopes` is always an array, never null. |
| `POST` | `/api/v1/account/tokens` | JWT | Body `{label, source_app, scopes?, expires_in_days?}`. 201 with `plaintext_bearer`, shown once. |
| `DELETE` | `/api/v1/account/tokens/:id` | JWT | Tenant-scoped: 404 for another tenant's token. |
| `POST` | `/internal/auth/validate` | Internal-Auth | Body `{bearer}`. 200, 401 unknown, 403 suspended, 500 resolver down. |

---

## 7. Cross-Service Integration

- **Gateway (`tas-llm-router`)** resolves tokens through `/internal/auth/validate` and does not read `scopes`. A data scope never changes what a token can do on the gateway.
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
