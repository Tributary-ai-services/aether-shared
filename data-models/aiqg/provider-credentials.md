# AIQG Provider Credentials (BYOK)

---

**Metadata**

```yaml
service: aiqg-dashboard-be (encrypted store + CRUD + /internal resolve) + tas-llm-router (resolve + inject) + aiqg-ui (Settings ▸ Provider Credentials)
model: ProviderCredential (account-scoped, encrypted-at-rest vendor API keys)
database: aiqg.provider_credential (Postgres, aiqg db on postgres-shared) — envelope-encrypted; aiqg.tenant_credential_policy (fallback toggle)
schema_location: aiqg-dashboard-be/internal/store/provider_credentials.go + migrations/018_provider_credentials.sql
version: 0.1.0
last_updated: 2026-08-04
status: designed (decisions locked with John 2026-08-04); not yet implemented
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #14
```

---

## 1. Overview

### Purpose

Let an AIQG customer **bring their own LLM provider API keys** (OpenAI, Anthropic, …), store them securely in AIQG, and have the gateway inject the right key per request — instead of passing a vendor key on every request (today's Path A requirement) or relying on TAS's pre-configured keys.

### The promise, refined: **"we never hold your keys — unless you bring your own"**

This is NOT a reversal of "we never hold your keys." It's an **explicit, opt-in exception** that keeps the promise intact for everyone who doesn't ask for it:

- **Default posture = never hold.** With no stored credential, TAS holds nothing: the customer either passes a vendor key per-request (`Authorization` / `TAS-Upstream-Authorization`, ephemeral, never persisted) or uses TAS's shared key. This is the recommended path and stays first-class.
- **Storing is a deliberate customer act.** "Bring your own" means the customer *chooses* to hand AIQG a key to hold, with a clear acknowledgment of what that means and how it's protected (§6). Until they do, the strict promise holds for them verbatim.
- **The promise is per-account, provable.** For any account that never stores a key, "we never hold your keys" is literally true and enforceable — an account can even **hard-disable** stored credentials (`stored_credentials_enabled = false`, §2) so no admin can accidentally opt them in.

So the design keeps three postures, in precedence order: **per-request key (never-hold, ephemeral) → stored key (opt-in, encrypted, "you brought your own") → TAS shared key (fallback, if allowed)**. The refined tagline is the product's north star and the UI copy (§8).

### Where the promise stays literally true: the data path never persists keys

Crucially, **"we never hold your keys" remains literally true for the request router and the Gatekeeper scanning path** — the parts that actually see your prompts and content. tas-llm-router **never persists a vendor key**: it holds one only transiently in memory for the duration of a single request to inject it upstream (exactly as any proxy forwards an `Authorization` header), then it's gone. Gatekeeper (inbound/outbound scanning, DLP, CLEAR) never touches the key at all.

BYOK storage lives entirely in the **control plane** — the aiqg-dashboard-be vault — which is a distinct service, trust boundary, and code path from the router/scanner. So the sensitive *data path* is stateless with respect to keys by construction; the only thing that stores a key is the vault, and only for a customer who explicitly brought their own. This is why the promise holds: **the router that reads your traffic still never holds your key; the vault only holds what you deliberately handed it, encrypted.** (This also motivates the §6 hardening where the gateway fetches the *wrapped* blob and decrypts in-process, so even the plaintext never crosses the control-plane→data-path wire.)

### Decisions locked (2026-08-04, with John)

1. **Encryption** — DB **envelope encryption** (AES-256-GCM), the KEK sourced from a k8s secret; plaintext decrypted only in-memory at request time; KEK **versioned** so a later KMS/Vault upgrade is non-breaking.
2. **Scope** — **account-level, admin-managed**: one set of provider keys per AIQG account, used by all that account's traffic. (Avoids the unresolved Keycloak↔gateway `user_id` mapping; per-user override is a future extension.)
3. **Fallback** — **per-tenant `allow_shared_fallback` toggle**: when no stored key exists for the routed provider, either fall back to TAS's pre-configured key (default, smooth onboarding) or hard-fail (BYOK-only tenants).

### Not this

- Not the TAS-Auth token store (`aiqg.token`) — those are **hashed** (one-way, verify-only). Vendor keys are **encrypted** (two-way, we must recover plaintext to call the vendor). Different mechanism, different table, different UI tab.

---

## 2. Data model

### `aiqg.provider_credential` (migration 018)

```sql
CREATE TABLE IF NOT EXISTS aiqg.provider_credential (
    id               UUID PRIMARY KEY,
    aiqg_account_id  UUID NOT NULL,              -- scope (account-level)
    tenant_id        UUID NOT NULL,              -- denormalized for resolve-by-tenant
    provider         TEXT NOT NULL,              -- openai | anthropic | … (whitelist)
    label            TEXT NOT NULL DEFAULT '',   -- human name ("prod openai")
    key_last4        TEXT NOT NULL,              -- masked display only
    -- envelope-encrypted payload (NEVER the plaintext):
    ciphertext       BYTEA NOT NULL,             -- AES-256-GCM(plaintext) under the DEK
    ct_nonce         BYTEA NOT NULL,
    wrapped_dek      BYTEA NOT NULL,             -- AES-256-GCM(DEK) under the KEK
    dek_nonce        BYTEA NOT NULL,
    kek_version      INT  NOT NULL,              -- which KEK wrapped the DEK
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    created_by       TEXT NOT NULL DEFAULT '',   -- keycloak sub
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at       TIMESTAMPTZ,
    last_used_at     TIMESTAMPTZ,
    UNIQUE (aiqg_account_id, provider, label)
);
CREATE INDEX IF NOT EXISTS provider_credential_resolve_idx
    ON aiqg.provider_credential (tenant_id, provider) WHERE enabled;
```

One enabled credential per `(account, provider)` is resolved by default; multiple labels allowed for rotation/staging (resolve picks the enabled one, newest `rotated_at`/`created_at`).

### `aiqg.tenant_credential_policy` (migration 018)

```sql
CREATE TABLE IF NOT EXISTS aiqg.tenant_credential_policy (
    tenant_id                 UUID PRIMARY KEY,
    allow_shared_fallback     BOOLEAN NOT NULL DEFAULT TRUE,  -- false = BYOK-only
    stored_credentials_enabled BOOLEAN NOT NULL DEFAULT TRUE, -- false = strict never-hold: the store API refuses to persist any key
    consented_at              TIMESTAMPTZ,                    -- when an admin first opted this account into storage
    consented_by              TEXT,                          -- keycloak sub who acknowledged
    updated_by                TEXT NOT NULL DEFAULT '',
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Absent row = default (`allow_shared_fallback = true`, `stored_credentials_enabled = true`, no consent yet). **`stored_credentials_enabled = false`** enforces the strict promise — the create/rotate API refuses to persist a key (409/403), so an account can *prove* "AIQG holds nothing of ours" and no admin can accidentally opt them in. **Consent gate**: the first successful `POST /account/credentials` for an account requires an explicit acknowledgment (§8) and stamps `consented_at`/`consented_by` (audited) — storing a key is a recorded, deliberate act, never a silent default.

---

## 3. Envelope encryption

**KEK** — a 32-byte key from `PROVIDER_CREDENTIAL_KEK` (hex, k8s secret), tagged with `PROVIDER_CREDENTIAL_KEK_VERSION` (int). The config may hold *multiple* versioned KEKs (`PROVIDER_CREDENTIAL_KEK_<n>`) so rotation can decrypt old records while writing new ones under the current version.

**Write** (store layer, `internal/store/provider_credentials.go`):
1. Generate a random 32-byte **DEK**.
2. `ciphertext = AES-256-GCM(plaintext_key, DEK, ct_nonce)`.
3. `wrapped_dek = AES-256-GCM(DEK, KEK[current], dek_nonce)`.
4. Store ciphertext/nonces/wrapped_dek/`kek_version`, `key_last4`. Zero the plaintext + DEK from memory.

**Read** (only in the `/internal/resolve` path): unwrap DEK with `KEK[kek_version]`, decrypt ciphertext, return plaintext (in-memory, never persisted/logged).

**KEK rotation** — re-wrap DEKs under the new KEK (cheap; no plaintext re-encrypt). **Credential rotation** — new plaintext → new DEK + ciphertext.

**Why envelope (vs KEK-direct):** KEK rotation only re-wraps small DEKs instead of re-encrypting every secret; and per-credential DEKs limit blast radius. Upgrading the KEK store to KMS/Vault later means wrapping the DEK with the KMS instead of a local KEK — the table shape is unchanged (`wrapped_dek` becomes a KMS-wrapped blob, `kek_version` → key ref).

---

## 4. API (aiqg-dashboard-be)

### Authed (Keycloak JWT, account-admin), sibling to `/account/tokens`

| Route | Behavior |
|---|---|
| `GET /api/v1/account/credentials` | List **masked** (`id, provider, label, key_last4, enabled, created_at, last_used_at`). Never plaintext. |
| `POST /api/v1/account/credentials` | Body `{provider, label, api_key}`. Validates provider whitelist + key format; encrypts; stores; returns masked metadata + `key_last4`. Plaintext accepted once, **never returned**. Audited. |
| `POST /api/v1/account/credentials/:id/test` | Server-side probe: decrypt + make a minimal provider call (e.g. list models) → `{ok, detail}`. Never echoes the key. |
| `PUT /api/v1/account/credentials/:id` | Rotate (new `api_key`) or toggle `enabled`/`label`. Audited. |
| `DELETE /api/v1/account/credentials/:id` | Delete. `204`. Audited. |
| `GET/PUT /api/v1/account/credential-policy` | Read/set `allow_shared_fallback` for the tenant. Admin-only. |

Requires an **account-admin** role check (new — tokens today are account-scoped but this gates a higher-value secret; enforce admin).

### Internal (Internal-Auth, in-cluster) — the gateway's resolver

`GET /internal/credentials/resolve?tenant=<id>&provider=<p>` → 
```jsonc
{ "found": true, "credential_id": "…", "api_key": "sk-…",   // DECRYPTED — in-cluster only
  "allow_shared_fallback": true }
// or: { "found": false, "allow_shared_fallback": false }
```
Mirrors `/internal/policy/resolve` + `/internal/cache-config` (same `InternalAuthRequired` gate). Returns the decrypted key **and** the tenant's fallback policy in one call (so the gateway can decide fallback without a second round-trip). Bumps `last_used_at`. ⚠ This is the one endpoint that returns a plaintext secret over in-cluster HTTP — see §6.

---

## 5. Gateway integration (tas-llm-router)

### 5.1 Relax the Path A auth gate

`internal/middleware/aiqg.go` (~L154): today `TAS-Auth` present + `Authorization` missing → reject. Change to: `Authorization` **optional** — a missing vendor key is no longer fatal at the gate; the effective-key resolution (§5.2) decides. (The gate still requires a valid `TAS-Auth`.)

### 5.2 Effective-key resolution + injection (the missing piece)

Today a code comment claims `TAS-Upstream-Authorization` is copied to the outbound `Authorization`, but **no code does it**. Build it, at `internal/server/server.go` (~L945, after `router.Route` → provider known), with this precedence:

```
1. per-request  TAS-Upstream-Authorization  (explicit BYOK-per-request; never-store path preserved)
2. per-request  Authorization               (raw customer key on the inbound request)
3. stored credential for (tenant, provider) via /internal/credentials/resolve
4. TAS pre-configured key                    — ONLY if allow_shared_fallback (from the resolve response)
   else → 402 provider_key_required (BYOK-only tenant, no key)
```

The resolved key is set as the upstream `Authorization` before the provider call; `StripFromOutbound` already removes the TAS-*/`TAS-Upstream-Authorization`/`gen_ai.*` headers. Resolution uses a short-TTL in-memory cache keyed by `(tenant, provider)` (mirror `pkg/aiqg/cacheconfig` HTTP loader, ~2s client timeout). New resolver package: `pkg/aiqg/credentials`.

### 5.3 Never-log discipline

`TAS-Upstream-Authorization` and `Authorization` are already flagged "raw, never logged." The resolved key inherits that: it is never placed on the event, never logged, never cached to disk. Only `credential_id` + `credential_source` (see §7) are recorded.

---

## 6. Security considerations

- **Data path is stateless re: keys** — the request router holds a key only transiently in-memory per request; Gatekeeper scanning never touches it. The vault (dashboard-be) is the *only* store, and only for opt-in keys. This is the structural basis of "we never hold your keys — unless you bring your own."
- **Strict opt-out** — `stored_credentials_enabled = false` makes the store API refuse to persist any key, so an account can enforce/prove the strict promise; no admin can silently opt them in.
- **Explicit consent** — first store per account requires an acknowledgment, stamped + audited (`consented_at`/`consented_by`). Holding a key is never a default; it's a recorded choice.
- **Blast radius** — per-credential DEKs; KEK compromise ≠ instant plaintext (still need the DB). KEK lives in a k8s secret (cluster-scoped); document rotation. KMS/Vault is the v2 hardening (§3).
- **Plaintext over in-cluster HTTP** (the `/internal/resolve` response) — protected today by `Internal-Auth` + NetworkPolicy (same trust boundary as token validation). Hardening: mTLS between gateway↔dashboard-be; or have the gateway hold the KEK and fetch only the wrapped blob (moves decrypt into the gateway — larger change, keeps plaintext off the wire). Note both; v1 = Internal-Auth + NetworkPolicy.
- **Masking** — only `provider` + `key_last4` ever leave the store to the UI. No GET-by-id-returns-plaintext route exists.
- **Audit** — create/rotate/delete/enable-disable + policy changes → `aiqg.audit` (existing `AuditStore`), actor sub/email, `credential_id`, never the value.
- **Least privilege** — the authed CRUD requires account-admin; the decrypt path is only reachable via `Internal-Auth`.
- **Test-probe** — the `/test` call must use a minimal, cheap, non-mutating provider endpoint and never echo the key or full error bodies that might contain it.

---

## 7. Attribution & events

Add to the response event (additive, `omitempty`): `credential_source` enum `upstream_header | stored | tas_shared` and, when stored, `credential_id`. Never the key. Lets the dashboard show "N% of traffic on your keys vs TAS keys" and per-credential usage — and pairs with the Spend Governor / cost work (whose spend is on whose key).

---

## 8. UI (aiqg-ui)

New **Settings ▸ Provider Credentials** tab (distinct from **API Tokens**): per-provider add (masked-on-save), rotate, enable/disable, delete; `key_last4` + "last used"; a **Test** button; the account's **BYOK-only** toggle (`allow_shared_fallback`); and a **"Don't store any keys"** strict toggle (`stored_credentials_enabled = false`).

Copy leads with the refined promise: **"We never hold your keys — unless you bring your own."** The tab explains the three postures (per-request ephemeral, stored opt-in, TAS shared) and states plainly that the request router + scanning never persist keys — only this vault does, only for keys you deliberately store, encrypted. The **first** stored key per account shows a one-time **consent acknowledgment** ("You're choosing to have AIQG hold this key. It's encrypted at rest and decrypted only in-memory to call the provider on your behalf. The router and Gatekeeper scanning never persist it.") — required to proceed, stamped to `consented_at`/`consented_by` + audit.

---

## 9. Phasing

- **Phase 0 — this spec** + `aiqg.provider_credential` / `tenant_credential_policy` data-model docs.
- **Phase 1 — store + crypto (dashboard-be)**: envelope crypto helper, `provider_credential` + `tenant_credential_policy` stores, migration 018, authed CRUD + `/test`, the **first-store consent gate** (stamp `consented_at`/`consented_by`) and **`stored_credentials_enabled=false` strict enforcement** (store refuses to persist), audit. KEK config + k8s secret. (No gateway change yet — keys storable but unused.)
- **Phase 2 — resolve + inject (gateway)**: `/internal/credentials/resolve`; `pkg/aiqg/credentials` resolver; relax the auth gate; effective-key precedence + injection at `server.go:945`; `credential_source`/`credential_id` on the event; fallback toggle honored.
- **Phase 3 — UI**: Provider Credentials tab + BYOK-only toggle + test button; attribution surfacing (your-keys vs TAS-keys).
- **Phase 4 — hardening (optional)**: KMS/Vault KEK backend; mTLS gateway↔dashboard-be; per-user override (needs the Keycloak↔gateway user_id mapping).

---

## 10. Testing

- Crypto: round-trip encrypt→decrypt; wrong-KEK fails; KEK-version rotation (re-wrap) preserves plaintext; nonce uniqueness.
- Store: masked list never contains ciphertext/plaintext; unique `(account, provider, label)`.
- Gateway precedence: header > stored > TAS; BYOK-only + no key → 402; fallback path uses TAS key only when `allow_shared_fallback`.
- Never-log: assert the key never appears in logs/events (regression guard like the existing token round-trip tests).
- `/test` probe: ok/failure paths, no key leakage in errors.

---

## 11. Related Documentation

- [[token-accounting]] — cost/usage the `credential_source` attribution pairs with
- `internal/middleware/aiqg.go` (Path A gate), `internal/server/server.go` (routing → provider), `pkg/aiqg/{tokens,policy,cacheconfig}` (resolver pattern this mirrors)
- `SPEND_GOVERNOR_DESIGN.md` — whose-key/whose-spend governance
