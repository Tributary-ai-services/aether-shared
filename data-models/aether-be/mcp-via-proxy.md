# Aether on the MCP proxy (Plan #22 C.6)

Plan #22 P4-Config, work package C.6, and backlog item AETHER-DBHUB-1. Code: aether-be `feat/mcp-via-proxy`. This page holds the contract: the flag, the delegate credential and its verify endpoint, the `Database` node properties, the API errors, the migration job and the rollout.

## Why

Before C.6, aether-be called every MCP server at its own URL. The SQL console sent every query to dbhub, which is stock bytebase and never reads a per-call connection. Whatever host a user had typed, the SQL ran against dbhub's only source, `tas-postgres` (AETHER-DBHUB-1). That source ran as the `tasuser` superuser until SEC-39.

AIQG now holds per-tenant connections (C.4), and the tas-mcp proxy injects them on each call. C.6 moves Aether onto that path. Both products then read one store, and the console queries the org's own database.

## The flag

| Variable | Default | Meaning |
|---|---|---|
| `AETHER_MCP_VIA_PROXY` | `false` | Turns on the proxy path. Off means every direct path, including dbhub, works exactly as before. |
| `AETHER_MCP_PROXY_URL` | `http://prod-tas-mcp-http.tas-mcp-prod.svc.cluster.local:8082` | The proxy's HTTP base URL. |
| `AETHER_MCP_PROXY_TIMEOUT_SECONDS` | `30` | The timeout for each proxy call. |
| `AETHER_MCP_DELEGATE_TOKEN_FILE` | empty | The path of the delegate credential, a mounted Secret (see the next section). It is re-read when it changes. |
| `AETHER_MCP_DELEGATE_TOKEN` | empty | The same credential from the environment, used only when no file is set. With the flag on and neither one set, every proxied call is refused with 503. |
| `AETHER_MCP_PROXY_POSTGRES_SERVER_ID` | `postgres-mcp` | The proxy id of the console's server. |
| `AETHER_MCP_PROXY_SERVER_IDS` | the built-in map | `aether-id=proxy-id,...`. An Aether server with no entry keeps its direct path. |

The built-in map is `mcp-postgres→postgres-mcp`, `mcp-neo4j→neo4j-mcp`, `mcp-minio→minio-mcp`, `mcp-kafka→kafka-mcp`, `mcp-grafana→grafana-mcp`, `mcp-context7→context7-mcp`, `mcp-sequential-thinking→sequential-thinking-mcp`, `mcp-paper-search→paper-search-mcp`, `mcp-assembler→assembler-mcp` and `mcp-podcast→podcast-mcp`.

## The delegate credential (AIQG-88)

The proxy normally takes the caller's tenant from a `TAS-Auth` gateway token. aether-be holds no tenant's token, and AIQG cannot issue one to a service. The first build of #76 read one long-lived gateway token per org from a file. John replaced that on 2026-11-23 (AIQG-88): aether-be now holds **one** service credential that the proxy accepts only for tool invoke, and it names the org and user on each call. The proxy checks both against AIQG before it injects anything.

```
POST {AETHER_MCP_PROXY_URL}/api/v1/federation/servers/{id}/invoke
TAS-MCP-Delegate-Token:    <the delegate credential>
TAS-MCP-On-Behalf-Of-Org:  <Aether organization id of the caller's space>
TAS-MCP-On-Behalf-Of-User: <the caller's Keycloak sub>
```

- **Who it acts for.** The org comes from the space context: `(o:Organization)-[:HAS_SPACE]->(space)`, or the org id itself in the legacy flow. The user is the JWT `sub`.
  - A personal space, an organization-type space with no owning Organization, or a request with no user is refused with **403** before any call.
  - aether-be never names a tenant and never sends `TAS-Auth`.
- **What the proxy checks.** It calls aiqg-dashboard-be `POST /internal/mcp/delegation/verify` (see below). It refuses an unlinked org, a tenant that does not match the link, and a user who is not a member of the org. It fails closed with 503 when AIQG cannot answer. Only then is the call attributed to the tenant. The tool-call event records `delegated_by=aether-be` and `user_id=<sub>`.
- **What the credential cannot do.** On any other route it gets 403, including registry writes, reads, broadcast and events. Only `tools/list` and `tools/call` run with it. The proxy refuses at startup a credential shaped like a gateway token, so it can never pass as `TAS-Auth` at the proxy or at the LLM gateway.
- **Where it lives.**
  - On aether-be: Secret `aether-mcp-delegate-token` (key `token`) in `aether-be`, mounted at `AETHER_MCP_DELEGATE_TOKEN_FILE`. The file is re-read when it changes, so rotation needs no restart. `AETHER_MCP_DELEGATE_TOKEN` is used only when no file is set. The value is never logged or formatted.
  - On the proxy: the same value as Secret `tas-mcp-delegate-token` (key `token`) in `tas-mcp-prod`.
- **What a compromise of aether-be still allows.** It can call tools as any linked org, but only as a user who is a member of that org in AIQG's copy of Aether's membership. Every call is attributed to that user. It cannot register servers, read other tenants' configuration, or send LLM traffic.

### Verify endpoint (aiqg-dashboard-be)

`POST /internal/mcp/delegation/verify` sits behind `MCP_DELEGATION_VERIFY_ENABLED` (default off). The header `Internal-Auth` must carry `MCP_DELEGATION_VERIFY_TOKEN`, which is a token of its own: if it equals `INTERNAL_AUTH_TOKEN` or `MCP_CONNECTIONS_RESOLVE_TOKEN`, the route is not mounted. The Secret is `aiqg-mcp-delegation-verify-token` in `aiqg`, with the same value in `tas-mcp-prod`.

| Request | `{"aether_org_id": "...", "tenant_id": "<uuid, optional>", "user_sub": "..."}`. Either the org or the tenant is required, and `user_sub` is always required. |
|---|---|
| 200 | `{tenant_id, aether_org_id, user_sub, role, suspended}` |
| 403 `org_not_linked` | No live account is linked to the org, or the account is a personal one. |
| 403 `tenant_mismatch` | The org and the tenant were both named, and they are not linked to each other. |
| 403 `not_a_member` | The sub has no current `person` membership in the org, at org level or in any team (`team_member_history`, newest row ≤ now is `add`). |
| 400 `invalid_body` | The body is malformed. |
| 500 | The endpoint could not answer. The proxy fails closed and never caches this. |

A suspended account is answered 200 with `suspended: true`. The proxy then applies the suspended-account refusal it already uses for `TAS-Auth` callers.

Membership is only as current as AIQG's copy of Aether's membership, which arrives through `aether.org.changes.v1`. A member who was just added is refused until the consumer applies the change. A member who was just removed keeps access for up to the proxy's 60 s verify cache plus the consumer lag.

## Database node properties

The migration job adds three optional properties to a `Database` node and never removes any.

| Property | Type | Meaning |
|---|---|---|
| `mcp_server_id` | string | The proxy server the shared connection is for (`postgres-mcp`). |
| `mcp_connection_id` | string | The id of the `aiqg.mcp_connection` row holding this connection. |
| `mcp_migrated_at` | datetime | When the connection was first bound. |

The same properties are returned by `GET /api/v1/databases` and `GET /api/v1/databases/:id`, all three omitted when empty. Once `mcp_connection_id` is set and the flag is on, AIQG is the connection's source of truth. The host, port, database, username, password, SSL mode and protocol stored on the node and in the `db-credentials-*` Secret are left as they were, which allows a rollback.

## SQL console behavior with the flag on

The routes are `POST /databases/:id/query`, `POST /databases/:id/test`, `GET /databases/:id/schema`, `/tables`, `/tables/:table/columns` and saved-query execution.

- A PostgreSQL connection runs through the proxy on `postgres-mcp`'s `query` tool with the org's token. aether-be sends only `sql`. The proxy adds the tenant's stored connection.
- **dbhub is never called for SQL**, whatever the connection records.
- Schema, tables and columns are queried with `information_schema` and `pg_catalog` through the same tool, because server-postgres has no schema tool. A schema or table name must match `^[A-Za-z0-9_$ -]{1,63}$`, since the tool takes no bind parameters.
- Neo4j connections keep their direct Bolt path, which does not go through MCP.

| Condition | Status | `code` |
|---|---|---|
| No space context, a personal space, or an org space with no `Organization` | 403 | `mcp_space_required` / `mcp_org_space_required` |
| The org has no entry in the token file | 403 | `mcp_org_not_linked` |
| The token file is missing, unset or invalid | 503 | — |
| The connection has no `mcp_connection_id`, or one for another server | 409 | `connection_not_shared` |
| The database type is not `postgres` | 400 | — |
| Bind parameters were sent | 400 | — |
| The proxy returns 403: the org is not linked, the user is not a member, the credential is wrong, there is no stored connection (SEC-38), the tool is disallowed, the server is another tenant's, or the account is suspended | 403 | `mcp_proxy_forbidden` |
| The proxy returns 503: the connection could not be resolved | 503 | — |
| `PUT /databases/:id` changes host, port, database, credentials, SSL mode or protocol on a shared connection | 409 | `connection_managed_in_aiqg` |

A shared connection is served even when Aether last recorded its status as `Pending`.

## The MCP tool routes with the flag on

The routes are `GET /mcp/servers/:id/tools`, `POST /mcp/invoke` and `POST /mcp/servers/:id/test-connection`.

- A server in the map goes through the proxy with the delegate credential and the caller's org and sub. The proxy applies the tenant's `allowed_tools`.
- A caller-supplied `_connection` argument is removed before the call.
- A `connection_id` on `mcp-postgres` must name a `Database` in the caller's tenant that has `mcp_connection_id`, or the call gets 409. Nothing from that row is sent.
- A `connection_id` on any other server keeps the **direct** path. The proxy holds no connections for those servers. Sending Aether's credentials through the proxy as a tool argument would expose them to its scanner and its tool-call events, which redact only the values the proxy injected itself.
- Napkin keeps its own client. Brave Search, Firecrawl, Atlassian, Perplexity and Slack are not federated and keep their direct paths.
- Health checks in `GET /mcp/servers` are still direct.

## The migration job

`cmd/migrate-mcp-connections` in aether-be.

```bash
# Sign in to AIQG (the aiqg-ui client) as an owner or admin of the target org
# who also holds aiqg-platform-operator. The access token lasts 5 minutes.
export AIQG_BEARER='<access token>'            # read from the environment only
migrate-mcp-connections -aiqg-url http://aiqg-dashboard-be.aiqg.svc.cluster.local:8095            # dry run
migrate-mcp-connections -aiqg-url ... -apply                                                       # write
migrate-mcp-connections -aiqg-url ... -database-id <aether db id> -apply                           # when the org has several
```

It needs aether-be's Neo4j environment (`NEO4J_URI`, `NEO4J_USERNAME`, `NEO4J_PASSWORD`) and read access to the `db-credentials-*` Secrets in `tas-mcp-servers`. It uses the in-cluster service account, or `KUBECONFIG` when run outside the cluster.

What it does:

1. It reads `tenant_id` from the AIQG token. AIQG verifies the token and writes into that tenant, so the job cannot choose the destination.
2. It finds that tenant's org link (`GET /api/v1/admin/accounts/aether-links`). It refuses to run if the tenant is unlinked, is linked to a person rather than an org, or if the link's `aether_tenant_id` disagrees with the org's.
3. It lists the org's PostgreSQL `Database` nodes. These are the nodes in the legacy org space (`space_id` = the org id) and the nodes in any `Space` the org owns through `HAS_SPACE`, constrained to the org's Aether tenant.
4. It plans each one, prints the plan, and writes only with `-apply`. A write is `POST /api/v1/mcp/connections` with kind `database_dsn`, params `{argument: "_connection", format: "object"}`, the label `<name> (aether-db:<id>)` and the secret `postgres://user:pass@host:port/db?sslmode=…`. It then sets the three node properties.

| Action | Meaning |
|---|---|
| `create` | Will be copied (dry run), or was copied (`APPLIED=true`). |
| `bind` | AIQG already holds a connection with this node's marker, for example after a crash between the two writes. Only the id is recorded. |
| `already_shared` | Nothing to do. |
| `conflict` | The tenant already has an unrelated connection for the server. AIQG keeps **one per (tenant, server)**. |
| `ambiguous` | Several candidates and no `-database-id`. Nothing is copied. |
| `not_selected` | Another candidate is, or became, the shared one. |
| `no_credentials` | The login cannot be read. Add it in AIQG by hand. |
| `unsupported` | The host, user, database or SSL mode holds characters that postgres-mcp cannot carry (only `A-Za-z0-9._-` is allowed). |
| `stale_binding` | The node names a connection that AIQG no longer has. |
| `error` | AIQG refused the write, often on egress (see below), or the node write failed. Rerun to finish. |

The exit code is 0 when nothing needs an operator, 2 otherwise, and 1 when the run itself failed.

**Properties.** The job is idempotent, because a rerun recognizes the `aether-db:<id>` marker. It never deletes or edits Aether's copy or an existing AIQG connection. It never prints a password or a DSN. The node write sets the properties only on an unbound node, or on one already bound to the same connection.

**Egress.** AIQG and the proxy accept a stored DSN host only if it is a service in `MCP_TENANT_CLUSTER_NAMESPACES` (default `tas-mcp-servers`) or a host on the tenant's `MCP_TENANT_EGRESS_ALLOWLIST`. Prod sets neither on 2026-11-23, so a connection to `postgres-shared.tas-shared.svc.cluster.local`, or to any outside host, is refused until one of the two is widened. Widening either one is a reviewed deploy change on the proxy and aiqg-dashboard-be, together with the NetworkPolicy.

## One connection per server

`aiqg.mcp_connection` is keyed by `(tenant_id, server_id)` with a single live row (063, `mcp_connection_one_live`). Aether lets a space hold many PostgreSQL connections. Through the proxy, every Aether connection in an org reaches the same stored database, so only one of them can be the shared one. The other connections answer 409 `connection_not_shared` with the flag on. Removing that limit needs a C.4 change: several connections per `(tenant, server)`, chosen by an id the caller names and the proxy checks against the tenant.

## Data Sources page (not built)

The plan has Aether's Data Sources page read and write through the AIQG API. Four things block that, and none of them is code in aether:

1. **The `tenant_id` claim.** AIQG's `/api/v1` requires it. Its Keycloak mapper sits on the `aiqg-ui` client only, so Aether's browser token is answered with 403 `tenant_unprovisioned` (aiqg-dashboard-be README, "Where that claim comes from"). The mapper has to be added to `aether-frontend`, and the user attribute has to be set for every Aether user.
2. **CORS.** AIQG's `CORS_ALLOWED_ORIGINS` has to list Aether's origin.
3. **Roles.** `/api/v1/mcp/connections` serves only org owners and admins. An Aether member would see no connections.
4. **One connection per server**, as described in the previous section.

Until those are in place, the page keeps writing to aether-be. A connection with `mcp_connection_id` should be edited in AIQG (Settings ▸ Tools (MCP)), and aether-be returns 409 if someone tries to change where it points.

## Rollout

Every flag defaults to off, and each step below is inert until the next one.

1. **Merge** tas-mcp, aiqg-dashboard-be and aether-be. All three flags are off.
2. **Create the Secrets.** Generate two independent random values of 48 characters or more. Neither may start with `tas_qg_live_`.
   - The delegate credential goes in `tas-mcp-delegate-token` (key `token`) in `tas-mcp-prod`, and in `aether-mcp-delegate-token` (key `token`) in `aether-be`.
   - The verify token goes in `aiqg-mcp-delegation-verify-token` (key `token`) in `aiqg` and in `tas-mcp-prod`.
   - Record both values in aether-secrets.
3. **aiqg-dashboard-be first** (internal deployment only): set `MCP_DELEGATION_VERIFY_ENABLED=true` and `MCP_DELEGATION_VERIFY_TOKEN` from the Secret, then roll. Check:
   - the boot log says `mcp delegation verify mounted`;
   - the route answers 401 without the token;
   - from a throwaway curl pod in `tas-mcp-prod` (the proxy image has no shell tools), with the token, a TAS-org member's sub returns 200, a non-member's sub returns 403 `not_a_member`, and an unknown org returns 403 `org_not_linked`.
4. **tas-mcp second.** Set `MCP_DELEGATION_ENABLED=true`, `MCP_DELEGATE_TOKEN` and `MCP_DELEGATION_VERIFY_TOKEN` from the Secrets (`IDENTITY_ENABLED` and `MCP_TENANT_SERVERS` are already on), then roll by digest. Check:
   - the boot log says `delegation: enabled (tool invoke only)`;
   - `GET /api/v1/federation/servers` with the delegate header returns 403;
   - a delegated `tools/list` on `postgres-mcp` as a TAS member returns 200, and as a non-member returns 403;
   - `tas_mcp_delegation_decisions_total` moves.
5. **aether-be third**, still with `AETHER_MCP_VIA_PROXY=false`: mount `aether-mcp-delegate-token` read-only and set `AETHER_MCP_DELEGATE_TOKEN_FILE`.
6. **Egress (MCP-19)** for the org database's host, on the proxy and aiqg-dashboard-be together with the NetworkPolicy.
7. **Migration job:** a dry run, then `-apply`, then a rerun that shows `already_shared` on every row.
8. **Flag on:** set `AETHER_MCP_VIA_PROXY=true` and roll aether-backend by digest.
9. **Verify:**
   - A console query from a TAS-org space appears in `{namespace="tas-mcp-prod"}` and on Kafka as a `postgres-mcp` `tool_call` with `delegated_by=aether-be`, the TAS tenant and the user's sub. Its rows come from the org's database, not `tas_shared`.
   - A personal space returns 403.
   - A connection that was not migrated returns 409.
   - A user removed from the org is refused within about 60 s of the removal reaching AIQG.
10. After one release, remove the direct dbhub console path and the flag.

**Rollback:** set `AETHER_MCP_VIA_PROXY=false` and roll. The delegation flags can stay on, because nothing else uses them.
