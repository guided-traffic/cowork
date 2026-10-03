# Repository layout

Where things are, as a tree. The per-package responsibilities are
[package-map.md](package-map.md); the shape of the system is [architecture.md](architecture.md).
Read against the tree on 2026-10-02.

```
cowork/
├── backend/                    # Go module github.com/guided-traffic/cowork/backend
│   ├── api/                    # the API document, the contract (package apispec)
│   │   ├── openapi.yaml        # the root: info, security, paths → one file per path family
│   │   ├── meta.yaml, me.yaml, tenants.yaml, tickets.yaml, questions.yaml,
│   │   │   comments.yaml, time.yaml, attachments.yaml, events.yaml
│   │   ├── components/         # schemas, parameters, responses, headers; problem-codes.yaml (generated)
│   │   ├── oapi-codegen.yaml   # the generator's configuration
│   │   ├── openapi.gen.json    # the bundle (generated), embedded and served
│   │   └── embed.go
│   ├── cmd/cowork/             # the binary: serve, migrate, version
│   ├── internal/
│   │   ├── api/                # the pipeline and one handler per operation
│   │   │   └── apigen/         # oapi-codegen output: server interface, models, client (generated)
│   │   ├── auth/               # tokens, the principal and agent mark, authorization
│   │   ├── config/             # COWORK_* environment variables → Config
│   │   ├── domain/             # vocabularies, keys, urgency, transitions, attachment types
│   │   ├── events/             # the event hub of one replica
│   │   ├── httpserver/         # health, request id, request log, recovery, server lifecycle
│   │   ├── markdown/           # the Markdown export, grammar v1
│   │   │   └── testdata/       # golden files
│   │   ├── problem/            # the problem code catalogue and the RFC 9457 body
│   │   ├── requestid/          # the request id in the context
│   │   ├── storage/            # the S3 client for the attachments
│   │   └── store/              # transaction wrappers, roles check, list builder, locks, jobs, NOTIFY/LISTEN
│   │       ├── migrations/     # NNNNNN_<name>.up.sql, embedded; no down files
│   │       ├── queries/        # read/ and write/: the SQL, one file per aggregate
│   │       ├── readq/          # sqlc output for queries/read (generated)
│   │       └── writeq/         # sqlc output for queries/write (generated)
│   ├── test/
│   │   ├── fixture/            # rows written past row-level security, for tests and dev-seed
│   │   ├── devseed/            # make dev-seed
│   │   └── integration/        # build tag `integration`; needs PostgreSQL 18 and an S3 server
│   ├── tools/
│   │   ├── problemdoc/         # writes the problem-code enum and the README table
│   │   └── specbundle/         # bundles api/ into openapi.gen.json
│   ├── sqlc.yaml               # readq/writeq from the migrations and queries/
│   ├── Containerfile           # golang:1.27.1-alpine → distroless nonroot
│   └── .golangci.yml
├── frontend/                   # Angular 22 workspace, project "frontend"
│   ├── src/app/                # the shell and core/ services
│   ├── nginx/default.conf.template  # the container's nginx configuration, four substituted variables
│   ├── proxy.conf.json         # ng serve → backend :8080 for /api, /healthz, /readyz
│   ├── Containerfile           # node:26-alpine build → nginxinc/nginx-unprivileged
│   └── eslint.config.js
├── deploy/helm/cowork/         # the chart: backend (with the migrate init container) + frontend; ci/*-values.yaml
├── hack/                       # verify-release-tooling.mjs; verify-phase-2.sh + verify_phase_2.py
├── docs/
│   ├── adr/                    # decisions
│   ├── developer/              # contributor entry point: layout, package map, architecture, subsystems, build, testing, CI, checklists, conventions
│   ├── operations/             # installation, runtime
│   ├── security/               # one page per perspective
│   ├── tickets/                # work lists (+ archive/); rules in its README
│   └── planning/               # project plan, workflow plan, the consumed question catalog — transitional
├── .github/workflows/          # release.yml (Test and Release), build.yml (Release Docker & Helm), renovate.yml
├── Makefile                    # every target; bin/ and coverage/ land here
├── renovate.json, .releaserc.json, package.json (semantic-release)
├── CLAUDE.md, README.md, SECURITY.md, LICENSE
```

What is generated — `api/openapi.gen.json`, `api/components/problem-codes.yaml`,
`internal/api/apigen/`, `internal/store/readq/`, `internal/store/writeq/` and the problem-code
table in the root `README.md` — is committed, written only by `make generate`, and checked by
`make generate-check` ([build-test-lint.md](build-test-lint.md#generated-code)).
