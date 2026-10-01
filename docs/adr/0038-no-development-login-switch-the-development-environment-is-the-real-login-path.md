# ADR 0038: No Development Login Switch — the Development Environment Runs the Real Login Path, With a Minimal Dex, the Local Administrator, and Fixture Identities

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "local
development login?": no development-only authentication code, over a `COWORK_DEV_LOGIN`
switch and over a test-only build tag. The rules of D3–D5 were put to the owner with the
question and not objected to.

**Not built.** No compose file, no `make dev-up`, no fixture configuration.

## Context

[ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md)
D3 made a minimal Dex the test and development issuer; [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
gave every installation an optional local administrator from configuration. Between the two
there is no login case left that a switch would serve: the smallest development start is
PostgreSQL plus one environment variable, the full one is three containers. A switch that
creates a session without authentication is a code path one misconfiguration away from
production, and tests that run through it do not test the login.

## Decision

**D1 — cowork has no development-only authentication code.** No `COWORK_DEV_LOGIN`, no
build tag, no fake session. Development, tests and production go through the same resolver
([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6).

**D2 — Two development starts, both real.** The small one: `make postgres-up` and `make run`
with `COWORK_LOCAL_ADMIN_USERNAME` and `_PASSWORD` set — the local administrator logs in and
creates the first tenant. The full one: `make dev-up` starts PostgreSQL 18, MinIO
([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md))
and the minimal Dex from one `compose.yaml` at the repository root, and `make run` reads
`.env.dev`.

**D3 — The fixture identities live in the Dex configuration and are documented.** Static
users with passwords and groups that together cover the test matrix of the earlier records:
a global administrator (in `COWORK_ADMIN_GROUP`), a tenant administrator, a member and a
viewer of tenant A, a member of tenant B, one person in both tenants, and one person who
passes the gate but is in no mapped group (the grant case). Their names, groups and
passwords are listed in [docs/developer/testing.md](../developer/testing.md) when the file
exists; the passwords are visibly development-only (`dev-only-…`).

**D4 — `.env.dev` carries the development configuration and nothing secret.** Issuer, client
id and secret of the fixture Dex, `COWORK_OIDC_ALLOWED_GROUPS`, `COWORK_ADMIN_GROUP`, a
bootstrap tenant, the local administrator, the MinIO endpoint and keys — all values that
are meaningless outside `localhost`. The compose file says in its first lines that it binds
to `127.0.0.1` and is for development only.

**D5 — The integration tier uses the same services as service containers** with the same
configuration files, so what a developer logs in with is what CI logs in with.

## Consequences

- No authentication bypass exists in the binary; an audit finds none.
- The full development environment is three containers. The small one is one container and
  one variable, by courtesy of ADR 0032.
- The fixture set of D3 is a test asset with a shape (seven identities) that every
  two-identity, two-tenant, three-role test draws from; changing it is changing tests.
- `make dev-up` and `make dev-down` join the Makefile; `compose.yaml` and the Dex
  configuration join the repository; the operations page points developers at them and
  operators away from them.

## Alternatives Considered

- **A `COWORK_DEV_LOGIN` switch.** `make run` and logged in; a code path that removes
  authentication and tests that never exercise the login. Lost.
- **A test-only build tag that compiles the switch into test binaries only.** The production
  binary is clean; two binaries with different authentication behaviour, for a convenience
  the local administrator already provides. Lost.

## Residual risks

- Three containers are a heavier first run for a new contributor; D2's small start is the
  answer, and the developer pages show it first.
- Fixture passwords in the repository are, by D4, development-only and obviously named; a
  scanner will still flag them, and the repository's secret-scanning configuration allow-lists
  the fixture file by path.

## References

- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D3 — the minimal Dex
- [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) — the local administrator that makes the small start possible
- [ADR 0004](0004-cowork-is-a-team-product.md) — the two-identity test rule the fixtures serve
- [ADR 0003](0003-test-and-ci-policy.md) D2, D3 — the integration tier and "nothing is skipped"
