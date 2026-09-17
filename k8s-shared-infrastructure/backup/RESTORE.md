# TAS restore runbook

Recovery procedures for the backups produced by the `tas-backup` CronJob.
Closes the "document recovery procedures / test backup restoration" half of
**OPS-1**.

Every procedure below was executed end-to-end on **2026-09-14** against the
backup set `20260914T203643Z`, restoring into throwaway pods and comparing the
result to production. The verification results are recorded at the bottom.

---

## 1. What a backup set contains

```
/backup/db/<TIMESTAMP>/
  postgres-shared.sql.gz         pg_dumpall  -- aiqg, audimodal, tas_shared + roles
  keycloak-db.sql.gz             pg_dumpall  -- the aether realm
  timescaledb-tas_events.sql.gz  pg_dump     -- hypertables: events, aiqg.event_metrics
  neo4j.cypher.gz                APOC export -- graph + indexes + constraints
  MANIFEST                       sha256 of each file
/backup/db/LATEST                the newest good timestamp
/backup/minio/current/           mirror of MinIO, minus loki-data + spark-checkpoints
/backup/minio/superseded/<TS>/   objects overwritten or deleted upstream
```

`db/.in-progress/` holds partially written sets. **Never restore from it** — a
set is renamed out of `.in-progress/` only after all four dumps have been
verified. If a directory is still there, that run died partway.

### Where the copies live

| Copy | Location | Holds | Retention |
|---|---|---|---|
| Local | PVC `tas-backups`, `/dev/sda1` | everything | 14 sets |
| Cloudflare R2 | `<r2-bucket>/db/`, `/minio/` | everything | 30 sets |
| Backblaze B2 | `<b2-bucket>/db/` | databases only | 90 sets |

The local copy is on the same physical disk as the data it protects. It covers
a bad migration or an accidental `DROP`; it does **not** cover losing the disk.
For disk loss, or the loss of the whole node, use R2. If the Cloudflare account
itself is unavailable, B2 is the break-glass copy — deliberately a different
vendor, because Cloudflare also fronts production via the `airops-edge` tunnel.

---

## 2. Pick a restore point and verify it

```bash
kubectl -n tas-shared run backup-inspect --image=rclone/rclone:1.68 \
  --restart=Never --overrides='
{"spec":{"containers":[{"name":"sh","image":"rclone/rclone:1.68",
"command":["/bin/sh","-c","sleep 3600"],
"volumeMounts":[{"name":"b","mountPath":"/backup"}]}],
"volumes":[{"name":"b","persistentVolumeClaim":{"claimName":"tas-backups"}}]}}'

kubectl -n tas-shared exec backup-inspect -- sh -c 'ls -1 /backup/db | sort'
kubectl -n tas-shared exec backup-inspect -- cat /backup/db/LATEST

# ALWAYS verify checksums before trusting a set.
TS=$(kubectl -n tas-shared exec backup-inspect -- cat /backup/db/LATEST)
kubectl -n tas-shared exec backup-inspect -- sh -c \
  "cd /backup/db/$TS && awk '/^  [0-9a-f]{64}/ {print \$1\"  \"\$2}' MANIFEST | sha256sum -c -"
# Expected output -- one OK per file:
#   ./keycloak-db.sql.gz: OK
#   ./neo4j.cypher.gz: OK
#   ./postgres-shared.sql.gz: OK
#   ./timescaledb-tas_events.sql.gz: OK
```

If a checksum fails, fall back to the previous set or pull from R2 rather than
restoring something corrupt.

### Pulling a set from offsite

**Everything offsite is encrypted** with rclone `crypt`. Object names in the
buckets are ciphertext, so browsing R2 or B2 in their consoles shows nothing
recognisable, and none of it is readable without the passphrase. Do not delete
"unrecognised" objects from those buckets; they are the backups.

You need three things, and in a real disaster the cluster may be gone, so keep
all three outside it:

| What | Where it lives |
|---|---|
| Passphrase (`crypt-password`, `crypt-salt`) | `aether-secrets/Backup-Crypt-Passphrase` **and a password manager**; in-cluster copy in `tas-shared/tas-backup-credentials` |
| R2 token | `aether-secrets/R2-Backup-Token`. The S3 Access Key ID is the token's **id**, and the Secret Access Key is the **SHA-256 of the token value**, so the token value alone is enough to rebuild both |
| B2 key | keyID + applicationKey, from wherever you stored them at creation |

This works from any machine with rclone. Define the remotes with env vars so
nothing lands in a config file:

```bash
export RCLONE_CONFIG_R2_TYPE=s3 RCLONE_CONFIG_R2_PROVIDER=Cloudflare RCLONE_CONFIG_R2_REGION=auto
export RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true      # token is bucket-scoped; CreateBucket is denied
export RCLONE_CONFIG_R2_ENDPOINT=https://93b0654e0bb9608676005a45cc10bfd8.r2.cloudflarestorage.com
read -rp  "R2 Access Key ID: "            RCLONE_CONFIG_R2_ACCESS_KEY_ID;     export RCLONE_CONFIG_R2_ACCESS_KEY_ID
read -rsp "R2 Secret Access Key: "        RCLONE_CONFIG_R2_SECRET_ACCESS_KEY; echo; export RCLONE_CONFIG_R2_SECRET_ACCESS_KEY

# The decrypting layer on top of the raw bucket
read -rsp "crypt-password: " CP; echo
read -rsp "crypt-salt: "     CS; echo
export RCLONE_CONFIG_R2C_TYPE=crypt RCLONE_CONFIG_R2C_REMOTE=R2:tas-backups
export RCLONE_CONFIG_R2C_PASSWORD="$(rclone obscure "$CP")" RCLONE_CONFIG_R2C_PASSWORD2="$(rclone obscure "$CS")"
unset CP CS

rclone lsf R2C:db/                                  # restore points, decrypted names
rclone copy R2C:db/<TIMESTAMP> ./restore/<TIMESTAMP> --progress
cd ./restore/<TIMESTAMP> && awk '/^  [0-9a-f]{64}/ {print $1"  "$2}' MANIFEST | sha256sum -c -
```

For B2, the break-glass copy (databases only), replace the R2 lines with
`RCLONE_CONFIG_B2_TYPE=b2`, `RCLONE_CONFIG_B2_ACCOUNT=<keyID>`,
`RCLONE_CONFIG_B2_KEY=<applicationKey>`, and wrap it as
`RCLONE_CONFIG_B2C_REMOTE=B2:<b2-bucket>` with the **same** passphrase.

If `rclone lsf R2C:db/` prints nothing while the raw `rclone lsf R2:tas-backups`
does list objects, **the passphrase is wrong**. Crypt skips names it cannot
decrypt instead of erroring. Check it before concluding the backups are gone.

The files you end up with are byte-identical to the local set (verified by
sha256 on 2026-09-17), so every per-engine procedure below applies unchanged.

---

## 3. PostgreSQL — `postgres-shared`

Holds `aiqg`, `audimodal` and `tas_shared`, plus the cluster's roles.

### 3a. Restore into a scratch instance first (tested)

Do this whenever you are not certain, and always before overwriting production.

```bash
kubectl -n tas-shared run restore-test-pg --image=postgres:15-alpine --restart=Never \
  --env=POSTGRES_PASSWORD=restoretest --env=PGDATA=/pgdata/data --overrides='
{"spec":{"containers":[{"name":"pg","image":"postgres:15-alpine",
"env":[{"name":"POSTGRES_PASSWORD","value":"restoretest"},{"name":"PGDATA","value":"/pgdata/data"}],
"volumeMounts":[{"name":"b","mountPath":"/backup","readOnly":true},{"name":"d","mountPath":"/pgdata"}]}],
"volumes":[{"name":"b","persistentVolumeClaim":{"claimName":"tas-backups"}},{"name":"d","emptyDir":{}}]}}'

kubectl -n tas-shared wait --for=condition=ready pod/restore-test-pg --timeout=180s

kubectl -n tas-shared exec restore-test-pg -- sh -c \
  "gunzip -c /backup/db/$TS/postgres-shared.sql.gz | psql -U postgres -d postgres -q"
```

Confirm it landed:

```bash
kubectl -n tas-shared exec restore-test-pg -- \
  psql -U postgres -d postgres -c '\l'
```

Compare row counts against production, per table:

```bash
Q="SELECT schemaname||'.'||relname||' '||n_live_tup FROM pg_stat_user_tables ORDER BY 1;"
for db in aiqg audimodal tas_shared; do
  kubectl -n tas-shared exec postgres-shared-0 -- psql -U tasuser -d $db -At -c "ANALYZE;"
  kubectl -n tas-shared exec postgres-shared-0 -- psql -U tasuser -d $db -At -c "$Q" | sort > /tmp/prod-$db
  kubectl -n tas-shared exec restore-test-pg -- psql -U postgres -d $db -At -c "ANALYZE;"
  kubectl -n tas-shared exec restore-test-pg -- psql -U postgres -d $db -At -c "$Q" | sort > /tmp/rest-$db
  echo "== $db"; diff /tmp/prod-$db /tmp/rest-$db && echo "   identical"
done
```

> **Expected difference:** `audimodal.public.tenant_usage_stats` is a
> *materialized view*. `pg_dump` emits `REFRESH MATERIALIZED VIEW` on restore,
> so the restored copy is recomputed from the base tables and will hold 9 rows
> while production currently holds 0 — production's view is stale, not the
> backup. Any other difference is worth investigating before you trust the set.

### 3b. Restore over production (NOT rehearsed — read first)

This clobbers live data and has not been rehearsed against the real
`postgres-shared`, because doing so would destroy production. Treat the command
list as reviewed-but-unproven and take a fresh dump before starting.

```bash
# 0. Fresh safety dump FIRST, whatever the emergency.
kubectl -n tas-shared create job --from=cronjob/tas-backup tas-backup-preflight

# 1. Stop writers (aether-be, audimodal, aiqg-dashboard-be, llm-router...).
kubectl -n aether-be scale deploy --all --replicas=0
kubectl -n aiqg scale deploy --all --replicas=0

# 2. pg_dumpall output is a full-cluster script: it DROPs and recreates each
#    database as it goes. Restore it as superuser against 'postgres'.
kubectl -n tas-shared exec -i postgres-shared-0 -- \
  psql -U tasuser -d postgres -v ON_ERROR_STOP=1 < postgres-shared.sql

# 3. Bring writers back and check application health, not just pod status.
kubectl -n aether-be scale deploy --all --replicas=1
```

---

## 4. Keycloak — `keycloak-db-shared`

The realm, users, clients, roles and credentials all live in this database.
There is no separate realm-JSON export; restoring the database restores the
`aether` realm.

```bash
kubectl -n tas-shared exec restore-test-pg -- sh -c \
  "gunzip -c /backup/db/$TS/keycloak-db.sql.gz | psql -U postgres -d postgres -q"

# Verify
for t in realm user_entity client keycloak_role user_role_mapping credential; do
  echo -n "$t prod="
  kubectl -n tas-shared exec keycloak-db-shared-0 -- psql -U keycloak -d keycloak -At -c "SELECT count(*) FROM $t;"
  echo -n "$t restored="
  kubectl -n tas-shared exec restore-test-pg -- psql -U postgres -d keycloak -At -c "SELECT count(*) FROM $t;"
done
```

**After restoring Keycloak over production, restart it** — it caches realm state
in memory and will serve the pre-restore realm until it is bounced:

```bash
kubectl -n tas-shared rollout restart deployment/keycloak-shared
```

Client secrets are restored with the database, so services do **not** need new
`KEYCLOAK_CLIENT_SECRET` values. If you restore an *older* realm than the
services were configured against, they will though — check `aether-be` and
`aiqg-dashboard-be` logs for `invalid_client` after a restore.

---

## 5. TimescaleDB — `timescaledb-shared`

TimescaleDB cannot be restored with a plain `psql` replay. The extension keeps
its own catalog (`hypertable`, `chunk`, `continuous_agg`) with circular foreign
keys — `pg_dump` warns about exactly these — and the restore must be bracketed
by `timescaledb_pre_restore()` / `timescaledb_post_restore()`.

```bash
kubectl -n tas-shared exec restore-test-ts -- sh -c "
  psql -U postgres -d postgres -q -c \"CREATE ROLE tas_events_rw LOGIN PASSWORD 'x';\"
  psql -U postgres -d postgres -q -c 'CREATE DATABASE tas_events OWNER tas_events_rw;'
  psql -U postgres -d tas_events -q -c 'CREATE EXTENSION IF NOT EXISTS timescaledb;'
  psql -U postgres -d tas_events -At -c 'SELECT timescaledb_pre_restore();'
  gunzip -c /backup/db/$TS/timescaledb-tas_events.sql.gz | psql -U postgres -d tas_events -q
  psql -U postgres -d tas_events -At -c 'SELECT timescaledb_post_restore();'
"
```

Skipping `pre_restore` produces a database whose tables exist but are **not
hypertables** — it looks restored and silently is not. Always verify:

```bash
kubectl -n tas-shared exec restore-test-ts -- psql -U postgres -d tas_events \
  -c "SELECT hypertable_schema, hypertable_name FROM timescaledb_information.hypertables;"
# Must list: aiqg.event_metrics and public.events
```

The restore pod must run the **same** TimescaleDB image as production
(`timescale/timescaledb:2.13.0-pg15`); a different extension version will
refuse the catalog.

---

## 6. Neo4j

Neo4j 5.15 **Community** has no online backup — `neo4j-admin database dump`
needs the database stopped and `neo4j-admin database backup` is Enterprise-only.
The backup is therefore a logical APOC export, which does carry indexes and
constraints as well as data.

The export is replayed with `cypher-shell`, not over HTTP: it uses the
`:begin` / `:commit` client commands, which only cypher-shell understands.

```bash
kubectl -n tas-shared exec restore-test-neo4j -- bash -c "
  gunzip -c /backup/db/$TS/neo4j.cypher.gz > /tmp/restore.cypher
  cypher-shell -u neo4j -p restoretest -a bolt://localhost:7687 --fail-fast -f /tmp/restore.cypher
"

# Verify against production
for q in 'MATCH (n) RETURN count(n)' 'MATCH ()-[r]->() RETURN count(r)' \
         'SHOW INDEXES YIELD name RETURN count(name)' \
         'SHOW CONSTRAINTS YIELD name RETURN count(name)'; do
  echo "$q"
  kubectl -n aether-be  exec neo4j-0 -c neo4j -- cypher-shell -u neo4j -p "$NEO4J_PW" \
    -a bolt://localhost:7687 --format plain "$q;"
  kubectl -n tas-shared exec restore-test-neo4j -- cypher-shell -u neo4j -p restoretest \
    -a bolt://localhost:7687 --format plain "$q;"
done
```

**Restoring over a non-empty graph will duplicate nodes.** The export uses
`CREATE`, not `MERGE`. To restore into the live instance, wipe it first:

```cypher
MATCH (n) DETACH DELETE n;
```

...and drop the existing indexes and constraints, since the script recreates
them and will fail on a name collision.

> **Why the export is base64-framed.** `cypher-shell --format plain` escapes
> embedded quotes as `\"` while leaving backslashes untouched, which is
> ambiguous to invert on values that contain escaped JSON — and several
> `Workflow.output` properties in this graph do. Base64 contains no character
> that plain format rewrites, so the export survives intact. If you ever change
> `dump-neo4j.sh` to drop the base64 step, re-run the property-level check in
> §8 before trusting it.

---

## 7. MinIO

MinIO is mirrored rather than snapshotted, so restoring is a copy back.
`superseded/<TS>/` holds anything that was overwritten or deleted upstream —
look there for a specific object that was lost rather than a whole-bucket event.

```bash
# Whole-bucket restore, from the local mirror
rclone copy /backup/minio/current/<bucket> MINIOSRC:<bucket> --progress

# ...or from offsite
rclone copy R2C:minio/current/<bucket> MINIOSRC:<bucket> --progress   # R2C = the crypt remote from §2

# A single object that was deleted upstream
rclone lsf /backup/minio/superseded/ --dirs-only         # find the run
rclone copy /backup/minio/superseded/<TS>/<bucket>/<key> MINIOSRC:<bucket>/<key>
```

`loki-data` and `spark-checkpoints` are **not** backed up. Loki chunks are
observability data with their own 30-day retention and Spark checkpoints
regenerate. Losing MinIO means losing recent log history — see OPS-20.

Integrity check of the mirror against the live source:

```bash
rclone check MINIOSRC: /backup/minio/current \
  --exclude "/loki-data/**" --exclude "/spark-checkpoints/**" --one-way
```

A handful of objects report "hash could not be checked" — those are multipart
uploads whose S3 ETag is a composite, not a plain MD5. Re-run with `--size-only`
to confirm them.

---

## 8. Verification record — 2026-09-14

Backup set `20260914T203643Z`, restored into throwaway pods, compared to live
production. This is the evidence for closing OPS-1's "restore never tested".

| Source | Result |
|---|---|
| `postgres-shared` | 3 databases, **55 tables**, row counts identical across all of them. The one difference, `tenant_usage_stats`, is a materialized view that `pg_dump` refreshes on restore — production's copy is stale. |
| `keycloak-db` | `aether` + `master` realms; **39 users, 18 clients, 86 roles, 85 role mappings, 36 credentials** — all exactly matching production. |
| `timescaledb` | Both hypertables restored **as hypertables**; `aiqg.event_metrics` **6,119 rows**, matching production exactly. |
| `neo4j` | **256 nodes, 374 relationships, 20 labels, 18 relationship types, 25 indexes, 9 constraints** — all matching. A `Workflow.output` property containing escaped JSON came back **byte-for-byte identical** (sha256 compared). |
| `minio` | **1,430 objects, 0 differences.** 1,427 verified by MD5; 3 multipart-ETag objects verified by size. |

**Not rehearsed:** restoring *over* production. Doing so would destroy the
thing being protected. §3b and §4 are reviewed but unproven — take a fresh
backup before you follow them.
