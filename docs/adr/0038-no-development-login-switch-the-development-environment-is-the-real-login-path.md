# ADR 0038: No Development Login Switch — the Development Environment Runs the Real Login Path, With a Minimal Dex, the Local Administrator, and Fixture Identities

## Status

Accepted, amended 2026-10-02 (D2 until a login exists; D6, D7 added). Date: 2026-10-01.
Decided by the owner as the answer to the catalog question "local development login?": no
development-only authentication code, over a `COWORK_DEV_LOGIN` switch and over a test-only
build tag. The rules of D3–D5 were put to the owner with the question and not objected to.

The amendment of 2026-10-02 is the owner's answer to the question of how the first person,
tenant and token exist before any login does, which no record answered: a test-only fixture
over the administrative database connection (D6, D7), over moving the local administrator's
login — sessions, CSRF, lockout — into phase 2 of the plan (the recommendation), and over a
bootstrap token from configuration.

**Partly built** (phase 2, 2026-10-02): D6 and D7 — the fixture package
[`backend/test/fixture`](../../backend/test/fixture/) over the administrative connection (a test
holds the binary free of it) and `make dev-seed`; D2's small start is `make postgres-up`,
`make dev-seed` and `make run`, with `make minio-up` for attachments. The compose file,
`make dev-up`, the local administrator and Dex arrive with the login.

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
`.env.dev`. *(Amended 2026-10-02: until the login of [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)
exists, the small start is `make postgres-up`, `make dev-seed` (D7) and `make run`, and the
seeded token is the credential.)*

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

**D6 — Until a login exists, persons, tenants, memberships and tokens come from a test-only
fixture over the administrative connection.** *(Added 2026-10-02.)* No route creates a
person, a tenant or a token before the login does: [ADR 0035](0035-personal-access-tokens.md)
D5 (a token is created by its person in a session) and
[ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D5 (a tenant
by a global administrator) stand unchanged, and their routes arrive with the sessions. The
integration tier creates what it needs through a fixture package under `backend/test/`,
which writes over the administrative connection of `COWORK_TEST_DATABASE_URL`, past
row-level security. The package is not reachable from `backend/cmd/cowork`; a test asserts
that the binary's dependency graph does not contain it. A token the fixture creates has the
format of ADR 0035 D1 and is resolved by the product's resolver like any other; nothing in
the binary knows how it was made. Until the login exists, no installation can create a
person, a tenant or a token except by writing to its database with the administrative
credential; the operations page says so.

**D7 — `make dev-seed` is the development entry to D6.** *(Added 2026-10-02.)* Against the
database of `make postgres-up` it creates a person, a tenant, an `admin` membership and a
token through the same fixture and prints the token once. It is a development step, never
an installation step; the README's fast start shows it as such.

## Consequences

- No authentication bypass exists in the binary; an audit finds none.
- The full development environment is three containers. The small one is one container and
  one variable, by courtesy of ADR 0032.
- The fixture set of D3 is a test asset with a shape (seven identities) that every
  two-identity, two-tenant, three-role test draws from; changing it is changing tests.
- `make dev-up` and `make dev-down` join the Makefile; `compose.yaml` and the Dex
  configuration join the repository; the operations page points developers at them and
  operators away from them.
- *(Added 2026-10-02.)* Until the login exists, Claude works with a seeded token in
  development and in the tests only; an installation of such a release has no way to issue
  a token through cowork. The UI cannot log in with a token either, because the browser
  never holds one (ADR 0035 D7), so the UI needs the login first.

## Alternatives Considered

- **A `COWORK_DEV_LOGIN` switch.** `make run` and logged in; a code path that removes
  authentication and tests that never exercise the login. Lost.
- **A test-only build tag that compiles the switch into test binaries only.** The production
  binary is clean; two binaries with different authentication behaviour, for a convenience
  the local administrator already provides. Lost.
- *(2026-10-02, for D6.)* **Moving the local administrator's login into the phase that
  builds the API** — the recommendation: ADR 0032 D1–D3, D5, D7, the sessions of ADR 0031
  without the groups snapshot, ADR 0033 D3, D6, D7 for that one account, the CSRF check of
  ADR 0037, tenant and token creation. It would have made the API usable on a real
  installation, at the cost of one large ticket and the login surface two phases early.
  Lost to the smaller surface.
- *(2026-10-02, for D6.)* **A bootstrap token from configuration.** A long-lived credential
  in a Secret and a second creation path beside the person's own, which ADR 0035's and
  ADR 0032's alternatives had already rejected. Lost.

## Residual risks

- Three containers are a heavier first run for a new contributor; D2's small start is the
  answer, and the developer pages show it first.
- Fixture passwords in the repository are, by D4, development-only and obviously named; a
  scanner will still flag them, and the repository's secret-scanning configuration allow-lists
  the fixture file by path.
- *(Added 2026-10-02.)* D6's fixture writes past row-level security with the administrative
  credential, so whoever holds that credential can mint a token for any person. The same
  person can already read and write every row, so the fixture adds no privilege; the tokens
  security page names it.
- *(Added 2026-10-02.)* A seeded token is real: `make dev-seed` pointed at a database that is
  not a development one yields a token that works there. The target defaults to the
  container of `make postgres-up`.

## References

- [ADR 0029](0029-standard-oidc-with-a-configurable-groups-claim-tested-against-a-minimal-dex.md) D3 — the minimal Dex
- [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) — the local administrator that makes the small start possible
- [ADR 0004](0004-cowork-is-a-team-product.md) — the two-identity test rule the fixtures serve
- [ADR 0003](0003-test-and-ci-policy.md) D2, D3 — the integration tier and "nothing is skipped"
- [ADR 0035](0035-personal-access-tokens.md) D1, D5, D7 — the token format, its creation in a session, the browser that never holds one (D6)
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D5 — tenants by a global administrator (D6)
