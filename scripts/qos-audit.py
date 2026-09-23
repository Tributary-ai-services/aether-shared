#!/usr/bin/env python3
"""
qos-audit.py - does every manifest obey CLAUDE.md's resource policy?

WHY THIS EXISTS (OPS-37)
MEMORY-RIGHTSIZING-PLAN.md names the `k8s-qos-auditor` skill as where to start:
"Running it across the whole tree is the fastest way to turn this plan into a
concrete diff." Nobody had run it. This is that sweep, made repeatable - and
made honest, because the naive reading of the policy produces a report nobody
can act on.

THREE FILTERS THAT TURNED 55 FINDINGS INTO 32 REAL ONES (measured 2026-09-23):

  1. Dead trees are excluded. Four scanned trees describe no running workload -
     the OPS-29 orphan, a plan for a future cluster, a tree whose namespace does
     not exist here, and one declaring ns `default`. Auditing them yields
     findings nobody can act on, which is how a report stops being read.

  2. A thing that TALKS to a datastore is not a datastore. postgres-exporter
     scrapes Postgres; neo4j-mcp proxies Neo4j; kafka-create-topics runs against
     Kafka. Matching the stateful exception on the name alone made 11 of the
     first sweep's findings bogus, each demanding Guaranteed QoS for a process
     holding no state at all.

  3. The justification comment must sit immediately above its own `resources:`
     key. Searching the whole file credits one workload's comment to every other
     workload in the same manifest.

A FINDING IS NOT A LICENCE TO CUT. Requests govern scheduling, limits govern
survival, and a workload measured in an idle window looks flat because nothing
ran - not because it is safe to shrink. See the tier-B note in the plan.

USAGE
  python3 scripts/qos-audit.py

Exit status is always 0: this reports, it does not gate. Gating needs required
status checks to exist first - see aether-shared#95.
"""
import os,sys,yaml,re,glob
ROOT='/home/jscharber/eng/TAS'
KINDS={'Deployment','StatefulSet','DaemonSet','Job','CronJob'}
STATEFUL=re.compile(r'postgres|pgbouncer|postgresql|neo4j|kafka|zookeeper|keycloak|loki',re.I)
files=set()
for pat in ('**/k8s/**','**/k8s-shared-infrastructure/**','**/manifests/**'):
    for ext in ('yaml','yml'):
        files|=set(glob.glob(f'{ROOT}/{pat}/*.{ext}',recursive=True))
files={f for f in files if '/node_modules/' not in f and '/mirror/' not in f and '/.docwt-' not in f and '/archive/' not in f}
# Trees that describe no running workload. Auditing them produces findings
# nobody can act on, which is how a report stops being read.
DEAD=(
  ROOT+'/k8s-shared-infrastructure/',        # OPS-29 orphan; superseded by aether-shared/
  ROOT+'/tas-ops-planning/',                 # a plan for a FUTURE cluster
  ROOT+'/tas-mcp/deployments/k8s/',          # namespace absent here (drift-check skips it)
  ROOT+'/aether/k8s/',                       # declares ns `default`; live frontend is in aether-be
)
files={f for f in files if not f.startswith(DEAD)}
def qos(c):
    r=(c.get('resources') or {}).get('requests') or {}
    l=(c.get('resources') or {}).get('limits') or {}
    if not r and not l: return 'BestEffort'
    keys=set(r)|set(l)
    if r and l and all(r.get(k)==l.get(k) for k in keys): return 'Guaranteed'
    return 'Burstable'
JUSTIFY=re.compile(r'profiled|derived|p95|re-derive|maxmemory|deliberate|Guaranteed QoS',re.I)
def has_comment(path,name):
    """A justification must sit in the comment block IMMEDIATELY above a
    `resources:` key -- file-level matching credits one manifest's comment to
    every workload in the file."""
    try: lines=open(path).read().split('\n')
    except Exception: return False
    for i,l in enumerate(lines):
        if not l.strip().startswith('resources:'): continue
        j=i-1; block=[]
        while j>=0 and lines[j].strip().startswith('#'):
            block.append(lines[j]); j-=1
        if block and JUSTIFY.search('\n'.join(block)): return True
    return False
rows=[];parse_err=0
for f in sorted(files):
    try: docs=list(yaml.safe_load_all(open(f)))
    except Exception: parse_err+=1; continue
    for d in docs:
        if not isinstance(d,dict) or d.get('kind') not in KINDS: continue
        name=(d.get('metadata') or {}).get('name','?')
        spec=d.get('spec') or {}
        tpl=spec.get('template') or (spec.get('jobTemplate',{}).get('spec',{}).get('template') if d.get('kind')=='CronJob' else None) or {}
        pod=(tpl.get('spec') or {})
        cs=(pod.get('containers') or [])
        if not cs: continue
        tiers=[qos(c) for c in cs]
        order={'BestEffort':0,'Burstable':1,'Guaranteed':2}
        pod_qos=min(tiers,key=lambda t:order[t])
        imgs=' '.join(str(c.get('image','')) for c in cs)
        # `postgres-exporter` scrapes Postgres; `neo4j-mcp` proxies Neo4j. Neither
        # holds state, so the stateful-infra exception must not capture them --
        # naive name matching made 11 of the first sweep's 55 findings bogus.
        NOT_A_STORE=re.compile(r'-(exporter|mcp|backup|init|migrate|ui|client|proxy)(-|$)',re.I)
        # A Job/CronJob is ephemeral by definition -- `kafka-create-topics` runs
        # against Kafka, it is not Kafka. The stateful exception cannot apply.
        st=bool((STATEFUL.search(name) or STATEFUL.search(imgs))
                and not NOT_A_STORE.search(name)
                and d.get('kind') not in ('Job','CronJob'))
        gpu=any('nvidia.com/gpu' in str((c.get('resources') or {})) for c in cs)
        rel=os.path.relpath(f,ROOT)
        if st and pod_qos!='Guaranteed':
            rows.append(('FAIL',rel,d['kind'],name,pod_qos,'Guaranteed','stateful infra must be Guaranteed'))
        elif not st and not gpu and pod_qos!='BestEffort' and not has_comment(f,name):
            rows.append(('FAIL',rel,d['kind'],name,pod_qos,'BestEffort or profiling comment','declares resources with no profiling comment'))
        else:
            rows.append(('PASS',rel,d['kind'],name,pod_qos,'','' ))
fails=[r for r in rows if r[0]=='FAIL']
print(f"k8s-qos-auditor — {len(files)} files scanned, {len(fails)} findings\n")
for r in fails:
    print(f"FAIL  {r[1]}  {r[2]}/{r[3]}  {r[4]} -> {r[5]}\n      {r[6]}")
print(f"\n{len(rows)-len(fails)} pass, {len(fails)} fail, {parse_err} files not parsed")
