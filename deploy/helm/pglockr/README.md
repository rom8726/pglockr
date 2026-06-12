# pglockr Helm chart

Deploys one pglockr instance per target PostgreSQL cluster (the sidecar model).

```sh
helm install pglockr-mydb ./deploy/helm/pglockr \
  --set cluster.name=mydb \
  --set secret.data.PGLOCKR_DSN="postgres://pglockr_ro:***@db:5432/app?sslmode=disable" \
  --set secret.data.PGLOCKR_TOKEN="$(openssl rand -hex 24)"
```

Then `kubectl port-forward svc/pglockr-mydb-pglockr 8080:8080` and open the UI.

## Common values

| Key | Default | Purpose |
|-----|---------|---------|
| `cluster.name` | `default` | cluster label in UI/metrics |
| `image.repository` / `image.tag` | `ghcr.io/rom8726/pglockr` / appVersion | image |
| `secret.data` | DSN + admin token | env injected via `envFrom` (use `secret.existingSecret` in prod) |
| `auth.mode` | `token` | `token` or `proxy` |
| `auth.principals` | `[]` | token-mode principals `{name, role, tokenEnv}` |
| `auth.proxy` | `{}` | proxy-mode config (rendered into `auth.proxy`) |
| `persistence.enabled` | `false` | SQLite history/audit on a PVC (survives restarts) |
| `redaction.enabled` | `false` | mask query-text literals |
| `serviceMonitor.enabled` | `false` | Prometheus Operator scrape of `/metrics` |

## Secrets

By default the chart creates a Secret from `secret.data` (handy for a quick
start, but the values contain plaintext). For production set
`secret.existingSecret` to a Secret you manage out-of-band (External Secrets,
Vault, sealed-secrets) containing the same keys (`PGLOCKR_DSN`, token env vars,
`PGLOCKR_PROXY_SECRET`, …). They're injected wholesale via `envFrom`.

## Metrics

`/metrics` is unauthenticated (pglockr's own health/activity only). Scrape it via
`serviceMonitor.enabled=true`, or the pod's `prometheus.io/*` annotations.
