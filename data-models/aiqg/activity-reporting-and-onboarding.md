# AIQG Activity Reporting & Activation Onboarding

---

**Metadata**

```yaml
service: aiqg-dashboard-be (capture + workers + report/preview API + email) + aiqg-ui (admin Activity view + My Activity / Next-Steps) + tas-llm-router (already emits the request events we aggregate)
models: SessionEvent (dashboard logins), NotificationLog (sent-email ledger), NotificationPref (per-account opt-in/unsubscribe)
databases:
  - aiqg.session_event (Postgres, aiqg db) — NEW
  - aiqg.notification_log (Postgres, aiqg db) — NEW
  - aiqg.notification_pref (Postgres, aiqg db) — NEW
  - aiqg.event_metrics (Timescale) — REUSED for request activity
reuses: internal/email (SMTP sender), internal/digest (worker + template pattern), internal/store/reports.go (snapshots), aiqg.audit_log (completion signals)
version: 0.1.0
last_updated: 2026-08-12
status: designed (decisions locked with John 2026-08-12); not yet implemented
plan_ref: CLAUDE-PLANS-BACKLOG.md Plan #15
```

---

## 1. Overview

### Purpose

Two coupled capabilities, driven by two real problems John raised:

1. **"People say they're using the system, but I expect they're not."**
   A **daily activity report** — delivered to John/admins by email *and* rendered in-app — that shows the ground truth of platform usage: logins, requests processed, cost, and website activity, at **account grain**, plus an **activation funnel** that makes "claimed vs actual" usage obvious at a glance.

2. **"Some people just don't know how to use the system"** (setting the proxy base URL, the auth header, or even running the existing curl command).
   An **activation onboarding engine** that watches each account's real activity and **automatically emails the right next step** — e.g. a customer who created an account but never sent a request gets a walkthrough with their base URL, `TAS-Auth` header, and a copy-paste curl; a customer who just sent their first request gets a congrats + "what to do next."

The feature is deliberately **additive** and reuses the existing digest/email/worker infrastructure (see §3). ~70% of the plumbing already exists.

### Decisions locked (2026-08-12, with John)

1. **Report audience — BOTH.** (a) An **admin platform-wide** daily digest for John (all accounts). (b) **Per-user personal** digests later (opt-in). The admin report is P1; per-user is P3.
2. **Onboarding trigger — ACTIVITY-TRIGGERED.** Emails fire off real milestones (or their absence), not a fixed calendar drip. See the milestone matrix (§6).
3. **Login capture — BOTH.** Add lightweight **dashboard middleware** capture now (P1), and a **Keycloak event listener** later (P3) for auth forensics (failed logins, etc.).
4. **Delivery — EMAIL + IN-APP together.** Reuse SMTP for email; add dashboard views rendering the same data.

### The identity subtlety that shapes the whole design: two different "users"

There are two distinct notions of "user" in AIQG, and conflating them produces wrong numbers:

| | **Dashboard user** | **Gateway user** |
|---|---|---|
| Who | The human who logs into aiqg-ui | Whoever the customer's app declares |
| Identity | Keycloak `sub` (authoritative) | `user_id` = **pseudonymous baggage** value set by the calling app |
| Emits | Logins / website activity | LLM request events |
| Captured today? | **No** (AIQG UI auths against Keycloak directly; the aether-be `LastLoginAt` is a different product on Neo4j) | **Yes** — Timescale `aiqg.event_metrics` |

**Consequence:** the reliable grain for "is this customer actually using AIQG?" is the **account / tenant**, measured by request volume in Timescale. That is what the admin report and the activation engine key off. Per-(baggage-)user is a *drill-down within an account*, never the primary axis. Onboarding is addressed to the **dashboard user(s)** of an account (email from the JWT/account).

### Not this

- Not a replacement for the existing **weekly CLEAR digest** (`internal/digest`) — that stays. This adds a *daily activity/usage* lens (volume, logins, activation), where the weekly digest is a *quality* lens (CLEAR composite, cost drift).
- Not a new analytics store — request activity is already in Timescale (`event_metrics`); we add only the login/notification bookkeeping tables.

---

## 2. Data model

Three new tables in the `aiqg` Postgres db (additive migrations, next free numbers after `018_provider_credentials.sql`). Request activity is **reused** from Timescale `aiqg.event_metrics` (no new table).

### 2.1 `aiqg.session_event` — dashboard login/session capture (NEW)

One row the first time a given dashboard user is seen in a UTC day (deduped), stamped by new middleware on JWT validation. This is the P1 "add login capture" mechanism.

```sql
CREATE TABLE aiqg.session_event (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    aiqg_account_id UUID,
    keycloak_sub    TEXT NOT NULL,          -- authoritative dashboard-user id
    email           TEXT,                   -- from JWT claim, for reporting/onboarding
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    seen_count      INTEGER NOT NULL DEFAULT 1,  -- requests observed that day
    user_agent      TEXT,
    client_ip       TEXT,
    day             DATE NOT NULL,          -- UTC day bucket for dedupe
    UNIQUE (tenant_id, keycloak_sub, day)   -- one row per user per day
);
CREATE INDEX idx_session_event_day    ON aiqg.session_event (day);
CREATE INDEX idx_session_event_tenant ON aiqg.session_event (tenant_id, day);
```

Capture = `INSERT ... ON CONFLICT (tenant_id, keycloak_sub, day) DO UPDATE SET last_seen_at=now(), seen_count=seen_count+1`. Cheap (one upsert per request, hot row cached). Gives **logins/day, DAU/WAU, last-seen, days-since-first-seen**. (A "login" here = "active dashboard session that day," which is the operationally useful signal; the Keycloak listener in P3 adds true auth-event fidelity + failed logins.)

### 2.2 `aiqg.notification_log` — sent-email ledger / idempotency (NEW)

Guarantees each activity-triggered email fires **once** per account per milestone (the weekly digest already self-dedupes via `last_sent_at`; this covers the new lifecycle sends and the admin digest).

```sql
CREATE TABLE aiqg.notification_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aiqg_account_id UUID,                   -- NULL for the platform admin digest
    tenant_id       UUID,
    recipient       TEXT NOT NULL,
    kind            TEXT NOT NULL,          -- 'admin_daily' | 'activation.<milestone>' | 'personal_daily'
    dedupe_key      TEXT NOT NULL,          -- e.g. 'activation.no_request_3d:<account>' or 'admin_daily:2026-08-12'
    channel         TEXT NOT NULL DEFAULT 'email',
    status          TEXT NOT NULL,          -- 'sent' | 'failed' | 'skipped'
    error           TEXT,
    sent_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (dedupe_key)                      -- the idempotency guarantee
);
CREATE INDEX idx_notification_log_account ON aiqg.notification_log (aiqg_account_id, kind);
```

The worker inserts with `ON CONFLICT (dedupe_key) DO NOTHING` **before** sending; only proceeds if it won the insert (claim-then-send), so concurrent/replayed ticks never double-send. Failed sends flip `status='failed'` so a retry policy can re-attempt (or the row is deleted to allow one retry — see §7).

### 2.3 `aiqg.notification_pref` — per-account opt-in + unsubscribe (NEW)

CAN-SPAM hygiene once we email customers. Admin digest to John is exempt (internal ops).

```sql
CREATE TABLE aiqg.notification_pref (
    aiqg_account_id     UUID NOT NULL,
    keycloak_sub        TEXT NOT NULL,
    activation_emails   BOOLEAN NOT NULL DEFAULT true,   -- lifecycle nudges
    personal_digest     BOOLEAN NOT NULL DEFAULT false,  -- opt-in (P3)
    unsubscribe_token   TEXT NOT NULL,                   -- random; one-click unsubscribe link
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (aiqg_account_id, keycloak_sub)
);
```

### 2.4 Reused: `aiqg.event_metrics` (Timescale) — request activity

No change. Per-account daily rollups come from the existing hypertable / continuous aggregates:

```sql
-- Per-account activity for a day (admin report + activation signals)
SELECT tenant_id, aiqg_account_id,
       count(*)                                   AS requests,
       count(*) FILTER (WHERE status='success')   AS ok,
       sum(total_tokens)                          AS tokens,
       sum(total_cost_usd)                        AS cost_usd,
       max(time)                                  AS last_request_at,
       count(DISTINCT user_id)                    AS distinct_gateway_users
FROM aiqg.event_metrics
WHERE time >= $1 AND time < $2
GROUP BY tenant_id, aiqg_account_id;
```

"First request ever" per account (activation milestone) = `min(time)` over all-time, or existence check; cached per account to avoid full scans.

---

## 3. Reuse map (what we build on)

| Need | Existing component | File |
|---|---|---|
| Send email (HTML+text MIME, TLS, graceful-disable) | `email.Sender` / `email.New(cfg)` | `internal/email/sender.go` |
| Scheduled worker (ticker, ctx, config-gate, shutdown, `shouldFire`/`MarkSent`) | digest worker pattern | `internal/digest/worker.go`, `cmd/server/main.go` |
| Email template (embedded Go template → HTML+plain) | digest template pattern | `internal/digest/template.go`, `email.tmpl.html` |
| Report snapshot persistence + preview compute | `ReportStore` | `internal/store/reports.go`, `internal/handlers/reports.go` |
| Completion signals for "next steps" | `aiqg.audit_log` + table existence | `internal/store/{tokens,provider_credentials,policies}.go` |
| Request activity aggregation | Timescale client | `internal/timescale/client.go` |
| SMTP config | `SMTPConfig` | `internal/config/config.go` |

---

## 4. Components (all in aiqg-dashboard-be + aiqg-ui)

### 4.1 Session-capture middleware (NEW, P1)
Runs inside the authenticated `/api/v1` group (after `AuthRequired`, so claims are present). Fire-and-forget upsert into `session_event` keyed on `(tenant, sub, UTC-day)`. Adds ~one cheap upsert per authenticated request; errors are logged, never block the request.

### 4.2 Admin daily digest worker (NEW, P1)
Copies the digest worker pattern. Once per day (configurable hour UTC): builds the platform-wide report (§5), emails John (recipient(s) from config), and writes an in-app snapshot (reuse `report_snapshot` with a new `kind='admin_daily'`, or a dedicated `daily_activity_snapshot`). Idempotent via `notification_log` dedupe_key `admin_daily:<date>`.

### 4.3 Activation engine worker (NEW, P2)
Once per day: for each active account, compute its milestone state (from `session_event` + Timescale + `token`/`provider_credential`/`policy_bundle`/`audit_log`), pick the highest-priority *unsent* nudge (§6), claim it in `notification_log`, and send. Respects `notification_pref.activation_emails` + unsubscribe.

### 4.4 Keycloak event listener (NEW, P3)
Consume Keycloak `LOGIN` / `LOGIN_ERROR` events (event SPI export → webhook or Kafka) into `session_event` (or a `auth_event` sibling) for authoritative auth history + failed-login forensics. Deferred; the middleware covers the reporting need in P1.

### 4.5 API endpoints (NEW)
```
GET /api/v1/activity/admin/daily            # platform-wide report (admin role) — powers in-app admin view + email
GET /api/v1/activity/accounts?window=1d     # per-account activity table (admin role)
GET /api/v1/activity/me                     # this user's activity + milestone/next-steps state
GET /api/v1/activity/me/next-steps          # checklist state (done/undone per milestone)
POST /api/v1/notifications/unsubscribe      # token-based, unauthenticated one-click
GET/PUT /api/v1/notifications/preferences   # per-user opt-in
```
Admin endpoints gated by a realm role (reuse the `ProviderCredentialAdminRole` gating pattern / a new `AIQG_ADMIN_ROLE`).

### 4.6 UI (NEW, aiqg-ui)
- **Admin ▸ Activity** page: the activation funnel + sortable per-account table (account · logins · requests · cost · last-seen · status flag) + trend sparkline. Same data as the email.
- **My Activity + Next Steps**: a personal activity summary and a checklist (issue token → add key → send first request → set a policy) reading milestone state; each item deep-links to the relevant Settings/Quickstart flow (Quickstart already exists at `QuickstartPage.tsx`).

---

## 5. Admin daily report spec (the reality-check)

Ordered to answer "who's actually using it" first:

1. **Activation funnel** (last 24h + cumulative): `signed up → issued API token → added a key (BYOK) → sent ≥1 request → active (≥N req/day)`. Counts + conversion %.
2. **Headline tiles:** new signups (24h), total requests (24h), total cost (24h), active accounts, and the smoking-gun metric **"logged in but never sent a request"** (accounts with dashboard sessions but zero gateway traffic).
3. **Per-account table:** account / display name · logins(24h) · **requests(24h)** · tokens · cost · last-request-seen · days-since-signup · **status flag**:
   - 🟢 **active** — sent requests recently
   - 🟡 **set up, idle** — has token/key but no recent requests
   - 🔴 **signed up, never sent** — the accounts to reach out to (and the activation engine auto-nudges)
4. **Website activity:** dashboard sessions per account (from `session_event`) + top pages (from Loki `{namespace="aiqg", source="frontend"}`, best-effort, P3 enrichment for per-user).

---

## 6. Activation engine — milestone → email matrix

Evaluated daily per account; highest-priority *unsent* row fires (one email/account/day max). `dedupe_key` in parentheses.

| Priority | Trigger condition | Email content | dedupe_key |
|---|---|---|---|
| 1 | Signed up ≥1 day ago, **no API token** | "Create your API token" + deep link to Settings ▸ API Tokens | `activation.no_token:<account>` |
| 2 | Token issued, **no request** after 2–3 days | **"Send your first request"** — base URL, `TAS-Auth` header, copy-paste **curl** + Python (mirrors Quickstart) | `activation.no_request:<account>` |
| 3 | **First request succeeded** | "🎉 You're live" + next: add your own provider key / set a policy | `activation.first_request:<account>` |
| 4 | Active, **no BYOK key** | "Use your own provider key (BYOK)" | `activation.no_byok:<account>` |
| 5 | Active, **no policy bundle** | "Set your first policy / quality gate" | `activation.no_policy:<account>` |
| 6 | Was active, **silent ≥7 days** | Re-engagement + "need help?" | `activation.dormant:<account>:<week>` |

Milestone data sources: `session_event` (signed-up/last-seen), `aiqg.token` (token issued), Timescale `event_metrics` (first/last request), `provider_credential` (BYOK), `policy_bundle` (policy), `audit_log` (corroboration). Every send is also written to `audit_log` (as the digest worker does).

The **#2 email is the heart of John's second goal** — it hands the exact proxy base URL, auth header, and curl to the people who "don't know how to use the system."

---

## 7. Idempotency, preferences, deliverability

- **Claim-then-send:** insert `notification_log` (`ON CONFLICT (dedupe_key) DO NOTHING`); send only if the insert won. Guarantees exactly-once per milestone even across worker restarts / multiple replicas.
- **Retry:** a failed send marks `status='failed'`; a bounded retry (next tick, capped attempts) re-attempts; success updates the row. (Alternative: delete the row on failure to permit one clean retry — decide at impl.)
- **Preferences / unsubscribe:** customer emails respect `notification_pref.activation_emails` and carry a one-click unsubscribe link (token → `POST /notifications/unsubscribe`). The **admin digest to John is internal ops and exempt**.
- **SMTP disabled → no-op:** `email.New` already returns a `disabledSender`; workers log and skip (same as digest today). In-app views still work.

---

## 8. Config / feature flags

```
ACTIVITY_ADMIN_DIGEST_ENABLED   (default false)         # gate P1 admin worker
ACTIVITY_ADMIN_DIGEST_RECIPIENTS (comma-sep emails)     # John + admins
ACTIVITY_ADMIN_DIGEST_HOUR_UTC  (default 13)
ACTIVATION_ENGINE_ENABLED       (default false)         # gate P2 worker
ACTIVATION_NO_REQUEST_DAYS      (default 3)             # tuning for milestone #2
AIQG_ADMIN_ROLE                 (realm role for admin activity endpoints)
# Reuses existing SMTP_* + TIMESCALE_DSN + DIGEST_DASHBOARD_URL
```
Both workers follow the existing gate pattern (PG + Timescale + SMTP present, plus the enable flag), so shipping the code with flags off is safe.

---

## 9. Phasing

- **P1 — Visibility (highest value to John):** `session_event` + capture middleware; per-account daily rollup query; **admin daily digest email + in-app Admin ▸ Activity page** (funnel + per-account table). Delivers the "who's actually using it" truth.
- **P2 — Activation engine:** `notification_log` + `notification_pref`; milestone worker + email templates (the curl/setup nudges); in-app **Next Steps** checklist.
- **P3 — Polish:** per-user opt-in personal digest; unsubscribe flow; Keycloak event listener (auth forensics / failed logins); website-activity per-user enrichment (stamp Keycloak `sub` on `POST /api/v1/logs`).

---

## 10. Open questions

1. **Admin digest recipients** — config list, or a role-based "all users with `aiqg-admin`"? (Default: config list, simplest.)
2. **"Active" threshold** — requests/day to count an account 🟢. (Default: ≥1 request in 24h; tune later.)
3. **Snapshot storage** — reuse `report_snapshot` (`kind='admin_daily'`) vs a dedicated `daily_activity_snapshot`. (Lean: reuse.)
4. **Website-activity attribution** — is `POST /api/v1/logs` authenticated? If yes, stamp `sub`/`tenant` for per-user UI activity in P3; if not, keep session-grain.
5. **Retry policy** for failed sends — bounded retry vs delete-to-retry (§7).

---

## 11. Related documentation

- [provider-credentials.md](./provider-credentials.md) — BYOK; a key "next step" the activation engine nudges toward.
- [report-snapshot.md](./report-snapshot.md) — snapshot store this reuses.
- [response-event.md](./response-event.md) / [aggregated-metrics.md](./aggregated-metrics.md) — the request-activity source (Timescale `event_metrics`).
- [audit-log-entry.md](./audit-log-entry.md) — completion-signal source for milestones.
- [account.md](./account.md) — account model (signup timestamp, tenant mapping).
