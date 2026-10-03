# Repository layout

Where things are, as a tree. The per-package responsibilities are
[package-map.md](package-map.md); the shape of the system is [architecture.md](architecture.md).
Read against the tree on 2026-10-04.

```
cowork/
├── backend/                    # Go module github.com/guided-traffic/cowork/backend
│   ├── api/                    # the API document, the contract (package apispec)
│   │   ├── openapi.yaml        # the root: info, security, paths → one file per path family
│   │   ├── meta.yaml, auth.yaml, me.yaml, tenants.yaml, accounts.yaml, members.yaml, tickets.yaml,
│   │   │   questions.yaml, comments.yaml, time.yaml, attachments.yaml, events.yaml, repositories.yaml
│   │   ├── cowork-yaml.schema.json  # the schema of a repository's .cowork.yaml, served
│   │   ├── components/         # schemas, parameters, responses, headers; problem-codes.yaml (generated)
│   │   ├── oapi-codegen.yaml   # the generator's configuration
│   │   ├── openapi.gen.json    # the bundle (generated), embedded and served
│   │   └── embed.go
│   ├── cmd/cowork/             # the binary: serve, migrate, version
│   ├── cmd/cowork-mcp/         # the MCP server for Claude Code, its hooks and subcommands
│   ├── internal/
│   │   ├── api/                # the pipeline and one handler per operation
│   │   │   └── apigen/         # oapi-codegen output: server interface, models, client (generated)
│   │   ├── auth/               # tokens, sessions, passwords, the principal and agent mark, authorization, sealing
│   │   ├── bootstrap/          # the local administrator and the bootstrap tenant, synchronised at start
│   │   ├── config/             # COWORK_* environment variables → Config
│   │   ├── domain/             # vocabularies, keys, urgency, transitions, attachment types
│   │   ├── events/             # the event hub of one replica
│   │   ├── httpserver/         # health, request id, request log, recovery, server lifecycle
│   │   ├── markdown/           # the Markdown export, grammar v1, and the context document
│   │   │   └── testdata/       # golden files
│   │   ├── mcpcli/             # cowork-mcp's command line: serve, the hooks, token check, lookup
│   │   ├── mcpserver/          # the tool catalogue over the MCP Go SDK
│   │   ├── oidc/               # the OpenID Connect relying party: discovery, the code, the ID token, the refresh
│   │   ├── problem/            # the problem code catalogue and the RFC 9457 body
│   │   ├── requestid/          # the request id in the context
│   │   ├── storage/            # the S3 client for the attachments
│   │   ├── tools/              # the tool catalogue, transport-free; the session start and the binding
│   │   └── store/              # transaction wrappers, roles check, list builder, locks, jobs, NOTIFY/LISTEN
│   │       ├── migrations/     # NNNNNN_<name>.up.sql, embedded; no down files
│   │       ├── queries/        # read/ and write/: the SQL, one file per aggregate
│   │       ├── readq/          # sqlc output for queries/read (generated)
│   │       └── writeq/         # sqlc output for queries/write (generated)
│   ├── test/
│   │   ├── fixture/            # rows written past row-level security, for tests and dev-seed
│   │   ├── devseed/            # make dev-seed
│   │   ├── fakeissuer/         # an OpenID Connect issuer in the test's process, for what Dex cannot do
│   │   └── integration/        # build tag `integration`; needs PostgreSQL 18, an S3 server and Dex
│   ├── tools/
│   │   ├── problemdoc/         # writes the problem-code enum and the README table
│   │   └── specbundle/         # bundles api/ into openapi.gen.json
│   ├── sqlc.yaml               # readq/writeq from the migrations and queries/
│   ├── Containerfile           # golang:1.27.1-alpine → distroless nonroot
│   └── .golangci.yml
├── frontend/                   # Angular 22 workspace, project "frontend"
│   ├── src/app/                # api/ (generated), brand/, theme/, core/, layout/, features/, shared/, dev/ (frontend.md)
│   ├── public/                 # favicon.svg, favicon.ico, apple-touch-icon.png
│   ├── scripts/primeui-define.mjs   # the PrimeUI license key → ng build/serve --define
│   ├── ng-openapi-gen.json     # the client generator's configuration
│   ├── nginx/default.conf.template  # the container's nginx configuration, four substituted variables
│   ├── proxy.conf.mjs          # ng serve → backend :8080 for /api, /auth, /healthz, /readyz; holds no credential
│   ├── Containerfile           # node:26-alpine build → nginxinc/nginx-unprivileged
│   └── eslint.config.js
├── claude/cowork/              # the Claude Code plugin: MCP server entry, hooks, skills /next /ticket /question /done
├── .claude-plugin/             # marketplace.json: the repository as a Claude Code plugin marketplace
├── deploy/helm/cowork/         # the chart: backend (with the migrate init container) + frontend; ci/*-values.yaml
├── hack/                       # dev.sh + dev_demo.py (make dev); verify-release-tooling.mjs; verify-phase-2.sh + verify_phase_2.py
│   └── dex/config.yaml         # the development and test issuer: one client, four users; credentials development-only
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
