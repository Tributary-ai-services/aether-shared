# TAS backups

Nightly backup of every stateful data store in the TAS cluster. Closes
**OPS-1** ("No backups for PostgreSQL, Neo4j, the Keycloak realm or MinIO;
recovery procedure not written; restore never tested").

To recover something, go to **[RESTORE.md](RESTORE.md)**. This file is about
how the backup works and how to operate it.

## What is backed up

| Source | Method | Compressed | Why this method |
|---|---|---|---|
| `postgres-shared` | `pg_dumpall` | ~6.5 MB | Captures `aiqg`, `audimodal`, `tas_shared` **and cluster roles** in one artifact |
| `keycloak-db-shared` | `pg_dumpall` | ~63 KB | The `aether` realm lives in this database; there is no separate realm export |
| `timescaledb-shared` | `pg_dump` | ~1.2 MB | Hypertables `events` and `aiqg.event_metrics` |
| `neo4j` (aether-be) | APOC cypher export | ~57 KB | 5.15 **Community** has no online backup; APOC also captures indexes and constraints |
| `minio-shared` | `rclone sync` mirror | ~290 MB | Object storage is better mirrored than snapshotted |

Databases total under 8 MB per run, which is why hundreds of restore points fit
inside a 10 GB free tier.

### Deliberately excluded

`loki-data` (568 MB) and `spark-checkpoints` (1.1 MB) are skipped. Loki chunks
are observability data with their own 30-day retention; Spark checkpoints
regenerate. Between them they are two-thirds of MinIO's bulk and none of its
value in a disaster. The consequence — losing MinIO loses recent log history —
is tracked as **OPS-20**.

Exclusion is by name rather than by an include-list, so a **new** bucket is
backed up by default. That is the safe direction for a backup to be wrong in.

Not covered at all: Kafka topics, Zookeeper, Redis, Prometheus TSDB and the
container registry. All are either rebuildable or caches. Kubernetes Secrets
are not backed up here either — they belong to `aether-secrets`.

## Where the copies go

| Copy | Location | Holds | Keeps |
|---|---|---|---|
| Local | PVC `tas-backups` | everything | 14 sets |
| Cloudflare R2 | `tas-backups`, encrypted | everything | 30 sets |
| Backblaze B2 | *(not yet configured)*, encrypted | databases | 90 sets |

**The local copy is on `/dev/sda1` — the same physical disk as every PVC it
protects.** It covers a bad migration, a bad deploy or an accidental `DROP`. It
does not cover losing that disk. Disk-loss protection is the offsite copy, which
is why the job prints a loud warning while offsite is unconfigured.

**Offsite copies are always encrypted client-side** with rclone `crypt`. File
contents and names are ciphertext before they leave the cluster, and
`finalize.sh` refuses to upload at all if credentials are present but the
passphrase is not. Every upload is checked the same night with
`rclone cryptcheck`, so a wrong passphrase or a corrupt object fails the run
instead of turning up later during a restore. **The passphrase is the single
point of failure for offsite recovery.** It lives in the cluster, in
`aether-secrets/Backup-Crypt-Passphrase`, and should also be in a password
manager.

R2 and B2 are two vendors on purpose. Cloudflare already fronts production
through the `airops-edge` tunnel; if that account were ever lost, a
Cloudflare-only backup would go with it. B2 holds the databases as the
break-glass copy under a separate login. At 8 MB per run both stay inside their
free tiers indefinitely.

## Installing

```bash
kubectl apply -f 00-pvc.yaml -f 20-configmap-scripts.yaml -f 30-cronjob.yaml
```

`kustomization.yaml` is deliberately absent: `kubectl kustomize` does not build
in the parent directory (two `Namespace` resources collide under the namespace
transformer — that is **OPS-3**, pre-existing and unrelated). Plain
`kubectl apply -f` works and does not depend on that being fixed.

### Credentials

Copy `10-secret.example.yaml` somewhere outside the repo, fill it in, apply it,
and shred the copy. It needs:

- **Neo4j** — `aether-be`'s StatefulSet carries `NEO4J_AUTH` as a literal env
  value rather than a Secret, so the password has to be repeated here. That is
  tracked separately as **SEC-19**.
- **Cloudflare R2** — R2 → Overview → *Account Details* → **Manage** API Tokens →
  Create Account API token, *Object Read & Write*, restricted to `tas-backups`,
  TTL forever. Endpoint is `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`. A
  bucket-scoped token cannot call `CreateBucket`, which is why the CronJob sets
  `RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true`.
- **Backblaze B2** — Application Keys, scoped to the backup bucket; the keyID
  goes in `b2-account-id`. Set the bucket's lifecycle to **keep only the last
  version**, otherwise retention deletes just hide old sets and they keep
  counting toward the free tier.
- **Crypt passphrase** — `crypt-password` and `crypt-salt`, two long random
  strings. Keep a copy outside the cluster.

Once both are configured, set `OFFSITE_REQUIRED=true` in `30-cronjob.yaml` so
that losing an offsite target becomes a failed job rather than a warning
somebody has to read.

## Operating it

```bash
# Status and history
kubectl -n tas-shared get cronjob tas-backup
kubectl -n tas-shared get jobs -l job-name --sort-by=.metadata.creationTimestamp

# Run one now
kubectl -n tas-shared create job --from=cronjob/tas-backup tas-backup-manual-1

# Read a run (the interesting output is in the last container)
kubectl -n tas-shared logs job/tas-backup-manual-1 -c dump-databases
kubectl -n tas-shared logs job/tas-backup-manual-1 -c dump-neo4j
kubectl -n tas-shared logs job/tas-backup-manual-1 -c finalize
```

## Alerting (OPS-21)

Until 2026-09-18 nothing would have noticed if this CronJob stopped running.
There is no kube-state-metrics in this cluster, so
`kube_cronjob_status_last_successful_time` is not a series Prometheus can alert
on — and a backup that silently stops producing sets looks exactly like one
that is working. That is the same failure shape that hid a 13-day outage in
OPS-19 and 20 days of unsent digests in OPS-24: **silence read as success.**

A CronJob pod cannot be scraped — by the time Prometheus came looking it has
exited — so the run pushes its own outcome to `pushgateway-shared` and
Prometheus alerts on the timestamp going stale.

| metric | meaning |
|---|---|
| `tas_backup_last_success_timestamp_seconds` | from `db/LATEST`, so it is the restore point that actually exists on disk |
| `tas_backup_last_failure_timestamp_seconds` | set by finalize.sh's exit trap; `0` when the last run was clean |
| `tas_backup_offsite_last_success_timestamp_seconds{target}` | written only past `cryptcheck`, so it means "a readable copy is up there" |
| `tas_backup_local_restore_points` | complete sets held locally |
| `tas_backup_local_free_bytes` | free space on the backup volume |

| alert | fires when |
|---|---|
| `TASBackupStalled` (critical) | no successful run in 26h — one daily cycle plus the 2h `startingDeadlineSeconds` |
| `TASBackupMetricMissing` (critical) | the health metric itself is absent for 30m, so staleness is no longer being watched |
| `TASBackupRunFailed` (warning) | the last run failed after the dump stage — faster than waiting 26h |
| `TASBackupOffsiteStalled` (warning) | no verified offsite push in 26h, i.e. backups may be local-only |

Two things to know before touching this:

- **The PVC is the source of truth, not the gateway.** Every publish recomputes
  every gauge from `LATEST` and the marker files, so nothing has to be kept in
  sync and a re-publish can never invent a restore point.
- **Pushgateway POST replaces only the metric families in the body**, not the
  whole group — that is `PUT`, and busybox `wget` can send neither `PUT` nor
  `DELETE`. A family that stops being published freezes at its last value
  instead of disappearing, which is why every gauge is published on every run
  even when the answer is zero.

If `TASBackupMetricMissing` fires but backups are in fact fine (usually
`pushgateway-shared` lost its persistence file), re-publish without running a
backup:

```bash
kubectl delete job tas-backup-reseed-metrics -n tas-shared --ignore-not-found
kubectl apply -f 40-job-reseed-metrics.yaml
```

That mounts the same ReadWriteOnce PVC as the CronJob, so don't run it while a
backup is running.

## How it is put together

The three dump steps run as **initContainers**, which Kubernetes runs in order
and which abort the pod on the first failure. That buys two properties without
extra machinery: all four dumps come from roughly the same moment, so a restore
point is internally consistent; and a partial set can never be promoted.

```
initContainer dump-databases  (postgres:15-alpine)       -> 3 × .sql.gz
initContainer dump-neo4j      (neo4j:5.15-community)     -> neo4j.cypher.gz
container     finalize        (rclone/rclone:1.68)       -> MANIFEST, promote,
                                                            MinIO mirror, offsite,
                                                            retention
```

Dumps are written to `db/.in-progress/<TS>/` and renamed to `db/<TS>/` only
after every one has been verified. Anything still under `.in-progress/` is a
failed run, is never synced offsite, and is never restored from. That is what
makes "is this restore point good?" answerable rather than hopeful.

Two of the three images are what the servers themselves already run, so they are
resident on the node. `rclone` is pinned rather than floating — see **OPS-14**.

### Every dump is checked before it counts

Each dump must end with the completion marker its tool writes
(`PostgreSQL database cluster dump complete`, `:commit`, and so on) or the run
fails. This is not theoretical: `aether-be/backups/neo4j_backup_20250810_173830.cypher`
is **0 bytes**, and the reason OPS-1 stayed open for months is that nothing
noticed. A backup that fails loudly is worth more than one that succeeds quietly.

### Why the Neo4j export is base64-framed

`cypher-shell --format plain` escapes embedded quotes as `\"` but leaves
backslashes untouched, which is ambiguous to invert on values that themselves
contain escaped JSON — and several `Workflow.output` properties in this graph
do exactly that. Base64 contains no character that plain format will rewrite,
so the only unwrapping needed is stripping two quotes. Verified byte-for-byte
against production on 2026-09-14.

## Known gaps

- **On a fresh install the offsite series does not exist until the first
  successful run.** `tas_backup_offsite_last_success_timestamp_seconds` comes
  from a marker file that only a verified (`cryptcheck`-passed) push creates,
  and an absent series never alerts. An offsite failure in that window is still
  caught — it fails the run, so `TASBackupRunFailed` fires — but "offsite
  silently skipped" is not detectable until the first marker exists. Closed
  here on 2026-09-18 by running one backup by hand; do the same after any
  rebuild that loses the backup volume.
- **Restoring over production is unrehearsed.** §3b and §4 of RESTORE.md are
  reviewed but unproven, because proving them means destroying production.
- **The local copy shares a disk with the data.** Mitigated only by the offsite
  copy actually being configured.
