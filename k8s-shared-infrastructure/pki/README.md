# TAS internal root CA

`tas-root-ca.crt` is the **public** certificate of the CA that signs every
internal `*.tas.scharber.com` host. Install it once and those hosts verify
normally — no `-k`, no `--insecure`, no `verify=False`.

```
subject       O = TAS, OU = Infrastructure, CN = TAS Root CA
valid         2025-11-18 → 2035-11-16
sha256        71:FC:96:D4:E4:CD:46:AD:03:F9:FF:81:B5:27:7F:48:
              CC:0E:A2:F6:B4:11:FA:0B:12:28:4D:25:59:25:E3:D9
```

Check that fingerprint before installing. It is the only thing that
distinguishes this file from any other self-signed CA someone hands you.

## Why this file exists

The cluster runs **two** issuers, and both are correct:

| issuer | signs | trusted by clients? |
|---|---|---|
| `letsencrypt-prod` | the public `*.air-ops.net` hosts | yes, publicly |
| `tas-ca-issuer` | every internal `*.tas.scharber.com` host | **only if you install this file** |

Nothing is broken in the cluster. All four ClusterIssuers are `Ready`, the
certificates are healthy, and cert-manager renews the leaves on schedule (they
run ~90 days; this CA runs to 2035, so **this file is stable** and is not
something you re-copy at each renewal).

The gap was distribution. Measured from a workstation on 2026-09-22 and again
on 2026-10-11, unchanged:

```
$ curl https://prometheus.tas.scharber.com
curl: (60) SSL certificate problem: unable to get local issuer certificate
```

`prometheus`, `grafana`, `loki`, `registry-api` and `aether` all failed
identically, and with `--cacert` all five answer **302, 302, 404, 200, 200** —
real TLS, correct chain. So the routing was never the problem (this is what
closed OPS-15, which had diagnosed it as one).

**Why that matters more than the inconvenience.** When every host fails to
verify, everyone reaches for `-k`, and a *genuine* certificate failure becomes
indistinguishable from the normal state. It also invites the real version of
that shortcut in code: `agent-builder` set `InsecureSkipVerify: true` on the
JWKS fetch it validates every token against (SEC-28) — the obvious workaround
when the CA is not in the trust store, and not fixable until it is.

The piecemeal version of this fix was already in place for exactly one client:
`/etc/docker/certs.d/registry-api.tas.scharber.com/ca.crt` has held a
**byte-identical** copy of this certificate since 2025-11-18, which is why
`docker push` works while `curl` does not.

## Install it

**System-wide (Debian/Ubuntu/WSL)** — needs root, so run it yourself:

```bash
sudo cp tas-root-ca.crt /usr/local/share/ca-certificates/tas-root-ca.crt
sudo update-ca-certificates
```

That covers curl, wget, git, and anything using OpenSSL's default store.
Verify:

```bash
curl -sS -o /dev/null -w '%{http_code}\n' https://prometheus.tas.scharber.com   # 302, no -k
```

**Without root**, point individual tools at the file:

```bash
export SSL_CERT_FILE=$PWD/tas-root-ca.crt        # curl, OpenSSL CLI
export REQUESTS_CA_BUNDLE=$PWD/tas-root-ca.crt   # python requests, httpx
export NODE_EXTRA_CA_CERTS=$PWD/tas-root-ca.crt  # node, npm
export GIT_SSL_CAINFO=$PWD/tas-root-ca.crt       # git over https
curl --cacert $PWD/tas-root-ca.crt https://loki.tas.scharber.com/ready
```

Python's `ssl` module does not read `REQUESTS_CA_BUNDLE`; for stdlib
`urllib`/`http.client` pass the file explicitly:

```python
ctx = ssl.create_default_context(cafile="tas-root-ca.crt")
```

**Docker**, per registry host (the mechanism already in use):

```bash
sudo install -D -m644 tas-root-ca.crt \
  /etc/docker/certs.d/registry-api.tas.scharber.com/ca.crt
```

Note `/etc/docker/daemon.json` currently lists `192.168.68.74:30500` under
`insecure-registries`. That covers the NodePort address only, not the
hostname, and is independent of this file.

**Browsers** keep their own stores: Firefox needs it under
Settings → Privacy & Security → Certificates → View Certificates → Authorities →
Import. Chrome on Linux uses the NSS store (`certutil -d sql:$HOME/.pki/nssdb
-A -t "C,," -n "TAS Root CA" -i tas-root-ca.crt`).

## Re-exporting it

This file is a copy of the `tls.crt` key of the `tas-root-ca-secret` secret in
the `cert-manager` namespace:

```bash
kubectl get secret tas-root-ca-secret -n cert-manager \
  -o jsonpath='{.data.tls\.crt}' | base64 -d > tas-root-ca.crt
```

Only ever export `tls.crt`. **`tls.key` in that secret is the CA's private
key**: it signs every internal certificate, it stays in the cluster, and it must
never be copied here or anywhere else. A root CA's public certificate is meant
to be distributed widely — that is its whole function — so this file carries no
secret and is committed deliberately.

Refs: OPS-36 (this), OPS-15 (closed into it), SEC-28, OPS-7
