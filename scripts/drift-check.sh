#!/usr/bin/env bash
#
# drift-check.sh — does every kustomize tree still describe what is running?
#
# WHY THIS EXISTS (OPS-27)
# `kubectl diff -k` is the only thing that answers that question, and nothing
# ran it. Drift was found when somebody happened to look, which is how a
# committed image name could differ from production for months.
#
# Worse, the command lies by omission. When it cannot evaluate a tree at all —
# a CRD this cluster lacks, a namespace that does not exist, an object the API
# rejects — it exits 2 and prints NOTHING. That is indistinguishable from "no
# drift" to a human skimming output or a script that only reads stdout. This
# script exists because that mistake was actually made: a tree was reported
# clean in a session when it had checked nothing.
#
# So the rule here is: classify on the EXIT CODE, never on empty output.
#   0 -> clean      1 -> drift      >1 -> could not check
#
# EXPECTED FAILURES ARE NOT DEFECTS
# Several overlays target namespaces that exist only on a multi-environment
# cluster (aether-dev, tas-mcp-staging, ...). On this single node they can
# never be checked, and counting them as failures would leave a permanent
# non-zero number that people learn to ignore — the same way a noisy alert
# stops being an alert. They are classified SKIP and reported separately.
#
# OUTPUT
# A table on stdout, and four gauges pushed to pushgateway-shared so the answer
# survives the terminal it was printed in. Prometheus alerts on them (see
# monitoring.yaml, TASDrift*). Pushgateway is ClusterIP-only, so the push goes
# through a short-lived port-forward; this runs on the workstation because that
# is where the git checkouts are.
#
# EXIT: 0 if every tree is clean or skipped, 1 if anything drifted, 2 if any
# tree could not be checked. Safe to run by hand; --no-push skips the gateway.

set -uo pipefail

ROOT="${DRIFT_ROOT:-$HOME/eng/TAS}"
JOB="drift_check"
PUSH_SVC="svc/pushgateway-shared"
PUSH_NS="tas-shared"
PUSH_PORT=9091
DO_PUSH=1
[[ "${1:-}" == "--no-push" ]] && DO_PUSH=0

command -v kubectl >/dev/null || { echo "drift-check: kubectl not found" >&2; exit 2; }

# Trees that can never be evaluated here, with the reason. Listing them by
# path rather than by matching the error text keeps a genuine "namespace
# missing" regression visible instead of silently absorbed.
is_expected_skip() {
  case "$1" in
    */aether-be/deployments/overlays/*) return 0 ;;  # aether-dev/staging/production/testing
    */tas-mcp/k8s/base|*/tas-mcp/k8s/overlays/dev|*/tas-mcp/k8s/overlays/staging) return 0 ;;
    */tas-mcp/deployments/k8s) return 0 ;;
    *) return 1 ;;
  esac
}

mapfile -t DIRS < <(
  find "$ROOT" -maxdepth 5 -name kustomization.yaml \
    -not -path '*/node_modules/*' -not -path '*/.claude/*' \
    -not -path '*/mirror/*' -not -path '*/.docwt-*' 2>/dev/null |
  xargs -r -n1 dirname | sort -u
)

clean=0; drift=0; fail=0; skip=0
drift_list=(); fail_list=()

printf '%-52s %s\n' "TREE" "RESULT"
printf '%-52s %s\n' "----" "------"
for d in "${DIRS[@]}"; do
  rel="${d#"$ROOT"/}"
  err="$(timeout 120 kubectl diff -k "$d" 2>&1 >/dev/null)"; st=$?

  if [[ $st -eq 0 ]]; then
    clean=$((clean+1)); printf '%-52s %s\n' "$rel" "clean"
  elif [[ $st -eq 1 ]]; then
    drift=$((drift+1)); drift_list+=("$rel"); printf '%-52s %s\n' "$rel" "DRIFT"
  elif is_expected_skip "$d"; then
    skip=$((skip+1)); printf '%-52s %s\n' "$rel" "skip (namespace absent here)"
  else
    fail=$((fail+1)); fail_list+=("$rel")
    printf '%-52s %s\n' "$rel" "CANNOT CHECK :: $(echo "$err" | grep -v '^# Warning' | head -1 | cut -c1-70)"
  fi
done

echo
echo "clean=$clean drift=$drift cannot-check=$fail skipped=$skip (of ${#DIRS[@]} trees)"
((drift)) && { echo "drifted:"; printf '  %s\n' "${drift_list[@]}"; }
((fail))  && { echo "cannot check:"; printf '  %s\n' "${fail_list[@]}"; }

if [[ $DO_PUSH -eq 1 ]]; then
  # Every gauge is published on every run, including zeros. Pushgateway POST
  # replaces only the metric FAMILIES in the body, so a family that stops being
  # sent freezes at its last value forever instead of disappearing (the same
  # trap recorded in OPS-21).
  body=$(cat <<EOF
# HELP tas_drift_trees_total Number of kustomize trees examined.
# TYPE tas_drift_trees_total gauge
tas_drift_trees_total ${#DIRS[@]}
# HELP tas_drift_trees_drifted Trees whose rendered manifests differ from the cluster.
# TYPE tas_drift_trees_drifted gauge
tas_drift_trees_drifted $drift
# HELP tas_drift_trees_uncheckable Trees kubectl diff could not evaluate at all (excludes expected skips).
# TYPE tas_drift_trees_uncheckable gauge
tas_drift_trees_uncheckable $fail
# HELP tas_drift_last_success_timestamp_seconds Unix time of the last completed drift check.
# TYPE tas_drift_last_success_timestamp_seconds gauge
tas_drift_last_success_timestamp_seconds $(date +%s)
EOF
)
  kubectl port-forward -n "$PUSH_NS" "$PUSH_SVC" "$PUSH_PORT:9091" >/dev/null 2>&1 &
  pf=$!
  for _ in $(seq 1 20); do
    curl -sf -o /dev/null "http://localhost:$PUSH_PORT/-/ready" 2>/dev/null && break
    sleep 0.5
  done
  if printf '%s\n' "$body" | curl -sf --data-binary @- "http://localhost:$PUSH_PORT/metrics/job/$JOB" 2>/dev/null; then
    echo "published drift metrics to pushgateway"
  else
    echo "drift-check: WARNING could not publish metrics (the check itself still ran)" >&2
  fi
  kill $pf 2>/dev/null; wait $pf 2>/dev/null
fi

((fail))  && exit 2
((drift)) && exit 1
exit 0
