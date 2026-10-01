# Release Tracking

**Status:** design, not built. Proposed 2026-10-01.
**Depends on:** the Versioning & Release Policy in the root `CLAUDE.md` (adopted 2026-10-01).

## What this answers

Three questions, none of which has an answer today:

1. **Which releases have been built?** The registry lists tags, but a tag does not say what commit it came from, what config was baked into it, or whether it was ever deployed.
2. **What is in production right now?** `kubectl` says which image a Deployment references. It does not say whether that was intended, who put it there, or whether the manifest in git agrees.
3. **What has been promoted, where, and when?** Nothing records this. Promotions happen via `kubectl set image` and leave no trace beyond a ReplicaSet revision number with no annotation.

The design is shaped by what actually went wrong, not by what a release system usually has. Every check in §6 maps to an incident that has already happened here.

## 1. Why the obvious model is wrong

**A release is not keyed on a service.** `tas-llm-router` runs `aiqg-v5.75` and `aiqg-v5.88` *simultaneously* — the `llm-router` and `llm-router-aiqg` Deployments. `aiqg-ui` runs `0.2.92` internally and `0.2.93_airops` publicly. A schema with one "current version" per service cannot represent the cluster as it exists, so the unit is a **deployment target**: a specific container in a specific Deployment in a specific namespace.

**A version does not identify an artifact.** Two images can share a version and differ in baked configuration. On 2026-09-30 the public `aiqg-ui` build omitted `VITE_KEYCLOAK_URL`, pointed the browser at the wrong Keycloak, and every authenticated request returned `401 token_invalid` — the whole public UI. The version looked like a routine increment. Any record keyed on version alone would have called that deploy clean. **Configuration is part of artifact identity**, so it is hashed and recorded.

**Provenance is the thing most often missing.** `aether-be` has exactly one tag in the registry: `latest`. `tas-llm-router` production has run images built from a local branch that was never pushed. This session ended with `aiqg-dashboard-be:0.4.0-rc88` in production, built from two commits that exist only on one laptop. "What code is in production" is currently unanswerable for several services, and that is the most valuable thing to fix.

## 2. The model

Three nouns. Only the first two are stored; current state is derived (§4).

### Artifact — an image that exists

```json
{
  "repo": "aiqg-ui",
  "version": "0.2.93",
  "variant": "airops",
  "tag": "0.2.93_airops",
  "digest": "sha256:1094730cde946339cd08db1525266cd5a6813cd354e7e56e98aebecb7458502b",
  "git_sha": "2c8818b9f...",
  "git_remote_ref": "refs/heads/fix/panel-truthfulness",
  "git_pushed": false,
  "config_sha256": "4f1a...",
  "config": {
    "VITE_DASHBOARD_API_URL": "",
    "VITE_KEYCLOAK_URL": "https://auth.air-ops.net",
    "VITE_KEYCLOAK_REALM": "aether",
    "VITE_KEYCLOAK_CLIENT_ID": "aiqg-ui",
    "VITE_AIQG_GATEWAY_URL": "https://gateway.air-ops.net"
  },
  "built_at": "2026-09-30T18:02:11Z",
  "built_by": "john@workstation"
}
```

`git_pushed` is recorded **at build time** and re-checked by the reconciler, because it is the field that decays: a commit that was unpushed when built may be pushed later, and a branch that existed may be deleted. `config` is stored in full as well as hashed — the hash detects a change, the body tells you which key moved, which is the difference between "something differs" and the one-line diff that identified the 2026-09-30 outage.

### Target — a place something runs

```yaml
# releases/targets.yaml — the registry of deployable places
environments:
  production:
    cluster: tas-k8s
    description: the single-node k3s cluster, internal hostnames
    targets:
      - { namespace: aiqg,           deployment: aiqg-dashboard-be,        container: aiqg-dashboard-be, repo: aiqg-dashboard-be }
      - { namespace: aiqg,           deployment: aiqg-ui,                  container: aiqg-ui,           repo: aiqg-ui }
      - { namespace: tas-llm-router, deployment: llm-router,               container: llm-router,        repo: tas-llm-router }
      - { namespace: tas-llm-router, deployment: llm-router-aiqg,          container: llm-router,        repo: tas-llm-router }

  airops:
    cluster: tas-k8s
    description: >
      the public Cloudflare-tunnel edge. Same cluster, different Deployments and
      different baked config, which is exactly why it is a separate environment
      and not a copy of production.
    targets:
      - { namespace: aiqg, deployment: aiqg-dashboard-be-public, container: aiqg-dashboard-be, repo: aiqg-dashboard-be }
      - { namespace: aiqg, deployment: aiqg-ui-public,           container: aiqg-ui,           repo: aiqg-ui, expect_variant: airops }

  # Declared, deliberately empty. drift-check.sh already classifies the
  # aether-be and tas-mcp overlays for these as SKIP because the namespaces do
  # not exist on a single node; the reconciler reuses that classification
  # rather than inventing a second meaning for "missing".
  dev: { cluster: tas-k8s, targets: [] }
  test: { cluster: tas-k8s, targets: [] }
  staging: { cluster: tas-k8s, targets: [] }
```

`container` is explicit because `kubectl set image '*='` clobbers init containers — a trap already recorded for the router. `expect_variant` lets the reconciler assert that the public UI is running an `airops` build, which is the single check that would have caught the auth outage.

### Promotion — an append-only event

```json
{
  "at": "2026-09-30T17:25:11Z",
  "env": "airops",
  "namespace": "aiqg",
  "deployment": "aiqg-ui-public",
  "repo": "aiqg-ui",
  "from": { "tag": "0.2.91_airops", "digest": "sha256:aa31..." },
  "to":   { "tag": "0.2.93_airops", "digest": "sha256:1094..." },
  "by": "john",
  "method": "release.sh promote",
  "reason": "provider-health panel read via control plane",
  "rolled_back_at": null
}
```

A rollback is a promotion, not a deletion — it appends a new record and stamps `rolled_back_at` on the one it reverses. The log must read as what happened, including the mistakes; 2026-09-30 had a promotion and a rollback 20 minutes apart and both belong in the history.

## 3. Storage

```
aether-shared/releases/
  targets.yaml                  # the target registry (§2)
  artifacts/<repo>.jsonl        # append-only, one line per build
  promotions.jsonl              # append-only, one line per promotion
  scripts/release.sh            # build + promote; the only sanctioned path
  scripts/release-check.sh      # reconciler (§6)
```

**JSONL, append-only, in git.** Appending never conflicts the way editing a shared YAML list does, git supplies history and attribution for free, and the files are greppable without tooling. No new database: the registry, git, and the cluster are already three sources of truth about what exists, and a fourth store that must be kept in agreement is how you get a fifth disagreement.

## 4. Current state is derived, never stored

"The current production release" is **the last promotion for that target**, filtered to not-rolled-back. It is not a field anybody edits.

This is deliberate. The existing hand-maintained record of what is deployed — the image pins in each repo's `k8s/deployment.yaml` — went stale within an hour of this session's deploy:

| target | live | manifest |
|---|---|---|
| `aiqg-dashboard-be` | `0.4.0-rc88` | `0.4.0-rc87` |
| `aiqg-ui-public` | `0.2.93_airops` | `0.2.91-airops3` |

A `kubectl apply` would have rolled production backwards. Any design where a human must remember to update a "current" field reproduces that, so there is no such field.

The manifests still need to agree — that is what `drift-check.sh` is for, and §6 reports it rather than replacing it.

## 5. The write path

`release.sh` exists because an unenforced convention is the thing that failed. It is not a wrapper for convenience; it is the only place that knows how to produce a correct artifact record.

```bash
release.sh build  <repo> [--variant airops]     # build, label, push, append artifact
release.sh promote <repo> <version> <env>        # set image BY DIGEST, append promotion
release.sh rollback <env> <deployment>           # promote the previous not-rolled-back artifact
release.sh status  [<env>]                       # derived current state (§4)
```

`build` refuses to proceed when:

- the version in the repo does not parse as semver, or the tag mapping violates the policy (`-variant` instead of `_variant`)
- the working tree is dirty — a `git_sha` that does not describe the bytes being built is worse than no record
- `--variant` is given but the build-arg set is identical to the default build, which means the variant arg was dropped (**this is the 2026-09-30 check**)

`promote` sets the image **by digest**, not by tag, because this repo's own `docker-push` pushes `:latest` alongside the version tag, so any tag can be re-pointed afterwards. It warns — and continues — when `git_pushed` is false, naming the commit that exists only locally.

## 6. The reconciler

`release-check.sh`, modelled on `drift-check.sh` and reusing its discipline: **classify on the exit code, never on empty output**, report SKIP separately from failure, and push gauges to `pushgateway-shared` so the answer outlives the terminal.

| check | state | the incident it comes from |
|---|---|---|
| live image ≠ last promotion | `DRIFT` | — |
| live image ≠ repo manifest pin | `MANIFEST_DRIFT` | 2026-09-30: all four targets, apply would roll back |
| live image has no artifact record | `UNKNOWN_PROVENANCE` | hand `kubectl set image`, no trace |
| artifact `git_sha` not on any remote | `UNPUSHED` | `rc88` in production from a laptop-only commit |
| tag is floating (`latest`, branch, date) | `FLOATING` | `aether-be/deeplake-api`, `tas-mcp-servers/kafka-mcp`; OPS-14's 22-day blackout |
| target's variant ≠ `expect_variant` | `WRONG_VARIANT` | 2026-09-30 auth outage |
| same version, different `config_sha256` across targets | `CONFIG_DIVERGENCE` | `airops4` vs `airops5` differed in one env var |
| namespace absent | `SKIP` | dev/test/staging on a single node |

Gauges: `tas_release_targets_total`, `..._ok`, `..._drift`, `..._unknown_provenance`, `..._floating`, `..._unchecked`. Alert on `unknown_provenance > 0` and `unpushed > 0` in production — those two mean nobody can say what is running.

**Read the gauges in pairs.** A rising drift count can mean a target became *checkable*, not that anything drifted — the same trap already documented for `drift-check.sh`'s ratchets.

## 7. The honest alternative: Argo CD

Argo CD is this problem's off-the-shelf answer, and the cluster already runs Argo Workflows and Argo Events, so the operational familiarity is there. It reconciles declared-versus-live continuously, shows per-app sync status, and keeps deployment history — which is §4 and most of §6 for free, and better than a shell script will do.

What it does **not** give, and what motivated this design:

- **provenance.** Argo CD tracks git-to-cluster. It does not know which commit produced an image, nor whether that commit was ever pushed. `UNKNOWN_PROVENANCE` and `UNPUSHED` are outside its model.
- **config identity.** Two images with the same tag and different baked build args are identical to Argo CD. That is the 2026-09-30 outage, undetected.
- **a build ledger.** "Which releases have been built but never deployed" is not a question it answers.

So the two are complements, not competitors, and the sensible split is:

> **Argo CD owns declared-versus-live. The ledger owns build provenance and promotion history.**

Adopting Argo CD would delete §4–§6's drift checks and shrink `release.sh` to `build`. That is a smaller system and a larger migration — 24 repos' manifests into App definitions on a single node that is already at ~67% memory requests. Recommended as the next step after the ledger proves the data is worth having, not before.

## 8. Build order

1. `targets.yaml` by introspecting the cluster, plus `release-check.sh` in **report-only** mode. Zero process change, and it immediately prints the four manifest drifts, two floating tags and the unpushed `rc88` — i.e. it pays for itself before anything else is built.
2. `release.sh build` with the OCI labels and the artifact ledger. Backfill only what is verifiable; a guessed `git_sha` is worse than a null one.
3. `release.sh promote` by digest, and the promotion log.
4. Gauges and alerts.
5. Revisit Argo CD (§7).

Step 1 is useful alone, which is the test of whether the order is right.

## 9. Open questions

- **Does `config` belong in git in full?** It is not secret today — the Vite values are public hostnames recoverable from any served bundle — but a future build arg might not be. Options: store only the hash, or allowlist the keys recorded.
- **Does the ledger cover non-image releases?** Grafana dashboards, policy bundles and Keycloak realm config are all promoted between environments and all suffer the same amnesia.
- **Who may promote?** Today anyone with `kubectl`. The ledger records intent but enforces nothing; enforcement would mean removing direct `set image` access, which is a bigger conversation than tracking.
- **Scope is 26 deployment targets across 7 namespaces, drawing on 16 of the registry's 24 repos** (counted 2026-10-01 over Deployments, StatefulSets and DaemonSets referencing `registry-api`). This design models all of them and proposes wiring `aiqg` and `tas-llm-router` first — the services with real release cadence and the ones every incident above came from. The 8 registry repos with no live target are their own finding: either dead images or undeployed work, and the ledger would say which.
