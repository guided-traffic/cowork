# Repository layout

Where things are, as a tree. The per-package responsibilities are
[package-map.md](package-map.md); the shape of the system is [architecture.md](architecture.md).
Read against the tree on 2026-10-01.

```
cowork/
├── backend/                    # Go module github.com/guided-traffic/cowork/backend
│   ├── cmd/cowork/             # the binary: serve, migrate, version
│   ├── internal/
│   │   ├── config/             # COWORK_* environment variables → Config
│   │   ├── httpserver/         # the handler (health, /api/v1, JSON 404/405) and the server lifecycle
│   │   └── store/              # pgx pool, golang-migrate runner
│   │       └── migrations/     # NNNNNN_<name>.up.sql, embedded; no down files
│   ├── test/integration/       # build tag `integration`; needs PostgreSQL 18
│   ├── Containerfile           # golang:1.27-alpine → distroless nonroot
│   └── .golangci.yml
├── frontend/                   # Angular 22 workspace, project "frontend"
│   ├── src/app/                # the shell and core/ services
│   ├── nginx/default.conf.template  # the container's nginx configuration
│   ├── proxy.conf.json         # ng serve → backend :8080, same shape as nginx
│   ├── Containerfile           # node:26-alpine build → nginxinc/nginx-unprivileged
│   └── eslint.config.js
├── deploy/helm/cowork/         # the chart: backend + frontend Deployments and Services; ci/*-values.yaml
├── hack/                       # verify-release-tooling.mjs
├── docs/
│   ├── adr/                    # decisions
│   ├── developer/              # contributor entry point: layout, package map, architecture, build, testing, CI, checklists, conventions
│   ├── operations/             # installation, runtime
│   ├── security/               # one page per perspective
│   ├── tickets/                # work lists (+ archive/); rules in its README
│   └── planning/               # question catalog, project plan, workflow plan — transitional
├── .github/workflows/          # release.yml (Test and Release), build.yml (Release Docker & Helm), renovate.yml
├── Makefile                    # every target; bin/ and coverage/ land here
├── renovate.json, .releaserc.json, package.json (semantic-release)
├── CLAUDE.md, README.md, SECURITY.md, LICENSE
```

The per-package responsibilities are the table in
[docs/developer/package-map.md](package-map.md).
