# Aether on the MCP proxy (Plan #22 C.6)

Plan #22 P4-Config, work package C.6, and backlog item AETHER-DBHUB-1. Code: aether-be `feat/mcp-via-proxy`. This page holds the contract: the flag, the org token, the `Database` node properties, the API errors, the migration job and the rollout.

## Why

Before C.6, aether-be called every MCP server at its own URL. The SQL console sent every query to dbhub, which is stock bytebase and never reads a per-call connection. Whatever host a user had typed, the SQL ran against dbhub's only source, `tas-postgres` (AETHER-DBHUB-1). That source ran as the `tasuser` superuser until SEC-39.

AIQG now holds per-tenant connections (C.4), and the tas-mcp proxy injects them on each call. C.6 moves Aether onto that path. Both products then read one store, and the console queries the org's own database.

## The flag

| Variable | Default | Meaning |
|---|---|---|
| `AETHER_MCP_VIA_PROXY` | `false` | Turns on the proxy path. Off means every direct path, including dbhub, works exactly as before. |
| `AETHER_MCP_PROXY_URL` | `http://prod-tas-mcp-http.tas-mcp-prod.svc.cluster.local:8082` | The proxy's HTTP base URL. |
| `AETHER_MCP_PROXY_TIMEOUT_SECONDS` | `30` | The timeout for each proxy call. |
| `AETHER_MCP_ORG_TOKENS_FILE` | empty | The path of the org-token file, a mounted Secret (see the next section). With the flag on and this empty, every proxied call is refused with 503. |
| `AETHER_MCP_PROXY_POSTGRES_SERVER_ID` | `postgres-mcp` | The proxy id of the console's server. |
| `AETHER_MCP_PROXY_SERVER_IDS` | the built-in map | `aether-id=proxy-id,...`. An Aether server with no entry keeps its direct path. |

The built-in map is `mcp-postgres→postgres-mcp`, `mcp-neo4j→neo4j-mcp`, `mcp-minio→minio-mcp`, `mcp-kafka→kafka-mcp`, `mcp-grafana→grafana-mcp`, `mcp-context7→context7-mcp`, `mcp-sequential-thinking→sequential-thinking-mcp`, `mcp-paper-search→paper-search-mcp`, `mcp-assembler→assembler-mcp` and `mcp-podcast→podcast-mcp`.

## The org token

The proxy takes the caller's tenant from a `TAS-Auth` gateway token (`tas_qg_live_*`), which it resolves through `/internal/auth/validate`. It never takes the tenant from the request body. aiqg-dashboard-be stores only the hashes of tokens, and no AIQG endpoint issues a token to a service, so aether-be cannot work out an org's token by itself.

**As built, an operator issues one gateway token per linked org** in AIQG (Settings ▸ Tokens, source app `aether`) and adds it to a Secret, which aether-be mounts at `AETHER_MCP_ORG_TOKENS_FILE`:

```json
{
  "orgs": {
    "<Aether organization id>": {
      "aiqg_tenant_id": "<AIQG tenant uuid the token was issued in>",
      "token": "tas_qg_live_..."
    }
  }
}
```

- The file is re-read when its modification time or size changes, so rotating the Secret needs no restart.
- A file that does not parse, or an entry that is not a gateway token, refuses every org with 503. The error never quotes a token.
- The token is never logged, and the `OrgToken` type never formats it.
- Only organizations are mapped. A personal space, or an organization-type space with no owning `Organization` (no `HAS_SPACE` edge), gets **403** `mcp_org_space_required`. A token is never guessed.
- `aiqg_tenant_id` is informational. aether-be cannot check it, because it holds no `/internal/auth/validate` credential. If an entry maps org A to tenant B's token, A's console queries B's connection. Issuing and filing the token is a two-person step.

**This is a long-lived secret for each org, and aether-be holds all of them.** The cleaner mechanisms, neither of which exists yet, are:

1. **An Aether delegate token on the proxy.** The proxy would honor `TAS-MCP-On-Behalf-Of-Tenant` from a narrow service token that may only invoke, the way it does for the admin token today. aiqg-dashboard-be would resolve the org link, so aether-be would hold one secret instead of one per org. The admin token cannot be reused for this, because it also writes the registry and reads every tenant.
2. **A short-lived token endpoint in AIQG.** aether-be would present its service token and an org id and receive a token for that org's tenant, valid for minutes.

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
| The proxy returns 403: no stored connection (SEC-38), a disallowed tool, another tenant's server, or a suspended account | 403 | `mcp_proxy_forbidden` |
| The proxy returns 503: the connection could not be resolved | 503 | — |
| `PUT /databases/:id` changes host, port, database, credentials, SSL mode or protocol on a shared connection | 409 | `connection_managed_in_aiqg` |

A shared connection is served even when Aether last recorded its status as `Pending`.

## The MCP tool routes with the flag on

The routes are `GET /mcp/servers/:id/tools`, `POST /mcp/invoke` and `POST /mcp/servers/:id/test-connection`.

- A server in the map goes through the proxy with the org's token. The proxy applies the tenant's `allowed_tools`.
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

1. Merge aether-be. The flag is off, so nothing changes.
2. For each linked org: issue a gateway token in AIQG with source app `aether`, and add it to Secret `aether-mcp-org-tokens` (key `tokens.json`) in `aether-be`.
3. Mount the Secret read-only and set `AETHER_MCP_ORG_TOKENS_FILE` on the `aether-backend` Deployment. Leave the flag off.
4. Decide on egress: either move the org's database behind a service in `tas-mcp-servers`, or widen `MCP_TENANT_CLUSTER_NAMESPACES` / the allowlist on **both** the proxy and aiqg-dashboard-be, together with the NetworkPolicy.
5. Run the migration job as a dry run, read the plan, then run it with `-apply`. Rerun it to confirm every row shows `already_shared`.
6. Set `AETHER_MCP_VIA_PROXY=true` and roll aether-backend by digest.
7. Verify. Run one console query from a TAS-org space. It appears in Loki on `{namespace="tas-mcp-prod"}` as a `postgres-mcp` call attributed to the TAS tenant, and its rows come from the org's database, not `tas_shared`. A query from a personal space returns 403. A connection that was not migrated returns 409.
8. After one release, remove the direct dbhub console path and the flag.

**Rollback:** set the flag to `false` and roll the deployment. Nothing was deleted, so the direct paths work as they did before.
