# Installation

cowork is installed with the Helm chart in [`deploy/helm/cowork/`](../../deploy/helm/cowork/).
A release is two Deployments — the backend (the API, the schema migration) and the frontend
(nginx with the Angular bundle, proxying `/api/` to the backend) — and it needs a PostgreSQL 18
(or newer) the backend can reach and a Secret with the connection URL. The chart brings no
database
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D8).

**There is no published chart repository and no published image yet.** The release workflow
that would publish them exists ([`.github/workflows/build.yml`](../../.github/workflows/build.yml))
and has not run; install from the checked-out tree and images you built with
`make docker-build` until the first release. The values reference is
[README.md, Helm chart values](../../README.md#helm-chart-values).

## The database credential

Create the Secret before the release. The key name is yours to choose; the default the chart
expects is `databaseUrl`.

```bash
kubectl -n cowork create secret generic cowork-database \
  --from-literal=databaseUrl='postgres://cowork:CHANGE-ME@postgres.cowork.svc:5432/cowork?sslmode=require'
```

The role in that URL owns the schema: migrations run under it on every start
([runtime.md](runtime.md#the-migration-run)), so it needs `CREATE` on the database. A split
into a migration role and a runtime role is not supported today.

`database.url` in the values renders the same Secret for you. Use it for a throw-away
installation only: the value is stored in plain text in the Helm release Secret and shown by
`helm get values`, and the chart prints a warning in its notes when it is set.

Any PostgreSQL 18 will do. On Kubernetes, a CloudNativePG cluster is the usual choice; its
`-app` Secret carries a `uri` key that can be handed to the chart directly with
`database.existingSecretKey=uri`. Not verified against a CloudNativePG cluster in this
repository; the key name is from that project's documentation.

## Install

```bash
helm upgrade --install cowork deploy/helm/cowork \
  --namespace cowork --create-namespace \
  --set backend.image.repository=your-registry/cowork-backend \
  --set frontend.image.repository=your-registry/cowork-frontend \
  --set backend.image.tag=0.1.0 --set frontend.image.tag=0.1.0 \
  --set database.existingSecret=cowork-database
```

What the release contains: the Deployments `<release>-cowork-backend` and
`<release>-cowork-frontend` (just `cowork-backend` and `cowork-frontend` when the release is
named `cowork`), a Service for each (backend on 8080, frontend on 80), one ServiceAccount
without an API token, and — only with `database.url` — the database Secret. No RBAC objects:
neither container talks to the Kubernetes API. The frontend pod gets `BACKEND_URL` set to the
backend Service by the chart.

Verify:

```bash
kubectl -n cowork rollout status deploy/cowork-backend deploy/cowork-frontend
kubectl -n cowork port-forward svc/cowork-frontend 8080:80 &
curl -s localhost:8080/healthz          # {"status":"ok"} — nginx itself
curl -s localhost:8080/api/v1/version   # proxied to the backend
open http://localhost:8080              # the UI shell, with the version in the footer
kubectl -n cowork port-forward svc/cowork-backend 8081:8080 &
curl -s localhost:8081/readyz           # {"status":"ready"} — the backend and its database
```

## Expose it

`ingress.enabled=true` renders a standard `networking.k8s.io/v1` Ingress that targets the
**frontend** Service; the frontend proxies `/api/` to the backend, so one rule covers the UI
and the API. Set `ingress.className`, the host and, for TLS, `ingress.tls` with a Secret your
certificate issuer fills. Set `backend.config.baseURL` to the public URL at the same time;
today nothing reads it, and the features that will (OIDC redirects, links in notifications)
need it to be right.

An installation that prefers path routing at the Ingress (`/api` straight to the backend
Service) can write that Ingress itself; both Services exist. Neither container terminates
TLS. Whatever you put in front — an Ingress controller, a mesh — terminates it; both pods
speak plain HTTP on 8080.

## Upgrade

```bash
helm upgrade cowork deploy/helm/cowork -n cowork --reuse-values \
  --set backend.image.tag=<new> --set frontend.image.tag=<new>
```

Both images carry the same version per release; move them together. The new backend pod
applies the pending schema migrations before it listens
([runtime.md](runtime.md#the-migration-run)). With `backend.replicaCount` above one, the pods
that start together serialise on a database advisory lock; the first applies, the rest find
the schema current. Rolling back an image to a version whose schema is older is **not** supported:
the migrations have `down` files, but nothing runs them automatically.

## Uninstall

```bash
helm uninstall cowork -n cowork
```

The release leaves the database untouched. A Secret you created yourself stays; the one the
chart rendered from `database.url` is removed with the release.

## Resources and scheduling

The backend defaults ask for 50m CPU and 128Mi memory and cap memory at 256Mi; the frontend
for 10m and 32Mi, capped at 64Mi. Both are idle at a fraction of that today and will be
revisited once there is a workload. `nodeSelector`, `tolerations` and `affinity` are per
component and passed through verbatim.
