# Development credentials

Every username, password, key and token the development environment and the test tiers use, with
the file that sets it. **All of them are development-only.** They are public in this repository and
protect nothing; never point an installation at them, and never reuse one
([ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D4). The containers publish their ports on `CONTAINER_BIND`, `127.0.0.1` by default, and the
backend of `make dev` listens on `127.0.0.1:8080`, so none of this is reachable from the network the
machine is on.

The values here are read from the files named in each row; when a file changes, this page changes
with it.

## Signing in to the UI under `make dev`

`make dev` serves the UI on `https://localhost:4200` and offers two ways in
([build-test-lint.md](build-test-lint.md), [frontend.md](frontend.md#the-development-loop)).

| Way in | Username | Password | Who that is in cowork | Set in |
|---|---|---|---|---|
| The form | `dev` | `dev-only-cowork` | the configuration's local administrator ([ADR 0032](../adr/0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) D1): a global administrator, and the administrator of the tenant `dev` by the seed's grant | [`hack/dev.sh`](../../hack/dev.sh) `ADMIN_USER`, `ADMIN_PASSWORD`, passed as `COWORK_LOCAL_ADMIN_USERNAME` and `COWORK_LOCAL_ADMIN_PASSWORD`; `COWORK_DEV_ADMIN` and `COWORK_DEV_ADMIN_PASSWORD` change both |
| *Sign in with Dex* | `ada@example.com` | `dev-only-dex` | in `cowork-admins` and `cowork-users`: a global administrator, by the administrator group | [`hack/dex/config.yaml`](../../hack/dex/config.yaml) |
| *Sign in with Dex* | `bob@example.com` | `dev-only-dex` | in `cowork-users` and `team-red`: a member of `dev`, by the demo data's mapping `team-red` → `member` | same |
| *Sign in with Dex* | `cyd@example.com` | `dev-only-dex` | in `cowork-users` only: behind the gate, in no tenant until somebody grants one | same |
| *Sign in with Dex* | `dan@example.com` | `dev-only-dex` | in `team-red` only: outside the gate, refused at the sign-in | same |

The gate behind these rows is `make dev`'s: `COWORK_OIDC_ALLOWED_GROUPS=cowork-users` and
`COWORK_ADMIN_GROUP=cowork-admins` ([`hack/dev.sh`](../../hack/dev.sh)). What each Dex user stands
for in the tests is [testing.md](testing.md#the-identity-provider-in-the-tests).

After a sign-in with Dex the browser remembers it, and once that session ends the login page sends
the first input to Dex's form by itself — once per tab, until the tab has a session again
([frontend.md](frontend.md#the-login-page)). To switch to `dev`, come back from Dex's form, or sign
out first: a sign-out forgets the remembered sign-in.

The seed also makes a second person, `sam`, an administrator of `dev` without a local account: it
fills the member lists and cannot sign in.

## What `make dev` keeps in `.dev/`

`.dev/` is untracked (`.gitignore`) and only its owner can open it (mode `700`).

| File | What it is | Made by |
|---|---|---|
| `.dev/token` | a plain token of `dev` with `admin` scope, named `dev-seed`, valid ninety days; the demo data is written with it. Every run of `make dev` or `make dev-seed` makes another one, and the earlier ones stay valid until they expire or are revoked on the token page | [`backend/test/devseed`](../../backend/test/devseed/main.go) |
| `.dev/session-key` | the backend's `COWORK_SESSION_KEY`, 32 random bytes made once, so that list cursors and the login throttle outlive a restart | [`hack/dev.sh`](../../hack/dev.sh) |
| `.dev/primeui-license` | the owner's PrimeUI Community License key, one line, or `PRIMEUI_LICENSE` in the environment; it is never in the repository ([ADR 0052](../adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D9) | put there by hand |

The dev server's proxy ([`frontend/proxy.conf.mjs`](../../frontend/proxy.conf.mjs)) holds no
credential: the browser signs in, and the session cookie passes through it.

## The containers

| Service | Endpoint | Credentials | Set in |
|---|---|---|---|
| PostgreSQL 18 (`cowork-postgres`) | `127.0.0.1:5432` | superuser `postgres` / `postgres`; the owner role `cowork_owner` / `cowork_owner`, which owns the database `cowork` and runs the migrations; the runtime role `cowork_app` / `cowork_app`, which the backend serves as | [`Makefile`](../../Makefile) `postgres-up`, `DEV_DATABASE_URL`, `DEV_DATABASE_OWNER_URL`, `DEV_ADMIN_URL` |
| PostgreSQL 18 that serves TLS (`cowork-postgres-tls`) | `127.0.0.1:5433` | superuser `postgres` / `postgres`; the private authority and the server's key made in the container at its first start, the authority's key gone once it issued the certificate; the authority's certificate copied to `bin/cowork-postgres-tls-ca.crt` | [`Makefile`](../../Makefile) `postgres-tls-up`, `TEST_DATABASE_TLS_URL`; [`hack/postgres-tls/entrypoint.sh`](../../hack/postgres-tls/entrypoint.sh) |
| PGSTY Silo (`cowork-minio`) | `127.0.0.1:9000` | root user `cowork` / `cowork-secret`, which the backend uses as its access key; the bucket of `make dev` is `cowork-dev` | [`Makefile`](../../Makefile) `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`; [`hack/dev.sh`](../../hack/dev.sh) `BUCKET` |
| Dex (`cowork-dex`) | issuer `http://localhost:5556/dex` | the client `cowork` with the secret `cowork-dev-dex-secret`, whose redirect URIs are `https://localhost:4200/auth/callback`, the integration tests' `http://cowork.test/auth/callback` and the end-to-end tier's `https://localhost:18443/auth/callback`; the users above | [`hack/dex/config.yaml`](../../hack/dex/config.yaml); the secret again in [`hack/dev.sh`](../../hack/dev.sh) `DEX_CLIENT_SECRET` |
| LM Studio (on the machine, optional) | `http://localhost:1234/v1` | none: `make dev` configures the chat's provider `lmstudio` without an API key | [`hack/dev.sh`](../../hack/dev.sh) `chat_env` |

Dex stores its users as bcrypt hashes; the password of each is the comment at the top of
[`hack/dex/config.yaml`](../../hack/dex/config.yaml). Dex keeps nothing between starts.

## The test tiers

| Where | Credentials | Set in |
|---|---|---|
| Integration tier, the database | `COWORK_TEST_DATABASE_URL`, by default the superuser `postgres` / `postgres` of `make postgres-up`; the run creates the roles `cowork_it_owner` and `cowork_it_app`, each with its name as its password, and the database `cowork_it_<unix-nanoseconds>`, which it drops at the end | [`Makefile`](../../Makefile) `TEST_DATABASE_URL`; [`backend/test/integration/main_test.go`](../../backend/test/integration/main_test.go) |
| Integration tier, the database that serves TLS | `COWORK_TEST_DATABASE_TLS_URL`, by default the superuser `postgres` / `postgres` of `make postgres-tls-up`, and its authority's certificate in `COWORK_TEST_DATABASE_TLS_CA`; the test creates the same two roles there and the database `cowork_it_tls_<unix-nanoseconds>`, which it drops at the end | [`Makefile`](../../Makefile) `TEST_ENV`; [`backend/test/integration/database_tls_test.go`](../../backend/test/integration/database_tls_test.go) |
| Integration tier, the object storage | `COWORK_TEST_S3_ACCESS_KEY_ID` / `COWORK_TEST_S3_SECRET_ACCESS_KEY`, by default Silo's `cowork` / `cowork-secret`; the bucket `cowork-it-<unix-nanoseconds>`, emptied and removed at the end | [`Makefile`](../../Makefile) `TEST_ENV` |
| Integration tier, Dex | the client secret `cowork-dev-dex-secret` and the users' password `dev-only-dex`, as above | [`backend/test/integration/oidc_helpers_test.go`](../../backend/test/integration/oidc_helpers_test.go) |
| Integration tier, the fake issuer | the client `cowork` with the secret `fake-issuer-secret`, an issuer the tests run in their own process | [`backend/test/fakeissuer`](../../backend/test/fakeissuer/) |
| Integration tier, the stub model | the API key `stub-key-0123456789`, which the stub checks only where a test asks it to | [`backend/test/stubllm`](../../backend/test/stubllm/stubllm.go) |
| `make verify-phase-2` | the database `cowork_verify` and the bucket `cowork-verify`, made fresh; a random server key per run; a token `make dev-seed` makes in that database | [`hack/verify-phase-2.sh`](../../hack/verify-phase-2.sh) |
| End-to-end tier, the local administrator | `e2e-admin` / `e2e-only-cowork`, the backend's `COWORK_LOCAL_ADMIN_*`: a global administrator, and the administrator of the tenants it makes: `e2e`, `e2e-other` and `e2e-visual` per run, and an `e2e-d<random>` per run of the dashboard's path | [`hack/e2e.sh`](../../hack/e2e.sh) `ADMIN_USER`, `ADMIN_PASSWORD` (`COWORK_E2E_ADMIN`, `COWORK_E2E_ADMIN_PASSWORD` change both); the suite reads the same variables in [`support/identities.ts`](../../frontend/e2e/support/identities.ts) |
| End-to-end tier, what the suite makes | local accounts `e2e-<random>` with temporary and chosen passwords `e2e-only-<random>`; the administrator's token `e2e-seed`, `admin` scope, one day, made per run and handed to the workers in `COWORK_E2E_TOKEN`; the administrator's session cookie in `frontend/e2e/.auth/admin.json`, which is ignored | [`support/identities.ts`](../../frontend/e2e/support/identities.ts), [`global-setup.ts`](../../frontend/e2e/global-setup.ts) |
| End-to-end tier, Dex | a container of its own, `cowork-e2e-dex`, issuer `http://localhost:5557/dex`, from `hack/dex/config.yaml`: the client secret and the users' password `dev-only-dex` as above; the suite signs in as `bob@example.com` | [`hack/dex/config.yaml`](../../hack/dex/config.yaml), [`hack/e2e.sh`](../../hack/e2e.sh) `DEX_CLIENT_SECRET` |
| End-to-end tier, the database and the bucket | the container `cowork-e2e-postgres`: superuser `postgres` / `postgres`, the roles `cowork_owner` and `cowork_app`, each with its name as its password, the database `cowork_e2e`; the container `cowork-e2e-minio`: root keys `cowork-e2e` / `cowork-e2e-secret`, the bucket `cowork-e2e`; both go with the stack | [`hack/e2e.sh`](../../hack/e2e.sh) |
| End-to-end tier, the server key and the TLS certificate | a server key drawn with `openssl rand` per run; a self-signed certificate for `localhost` and its key, made per run in a temporary directory, copied into the stand-in and deleted when the script ends — never stored | [`hack/e2e.sh`](../../hack/e2e.sh) |
| Chart CI values | placeholders that only `make helm-template` renders, such as the inline local administrator `admin` / `change-me-before-use` and the inline database URLs with `cowork_app` / `cowork_owner` | [`deploy/helm/cowork/ci/`](../../deploy/helm/cowork/ci/) |

## Changing one

A value changes in the file of its row, and then here. A password of a Dex user is changed as a new
bcrypt hash in [`hack/dex/config.yaml`](../../hack/dex/config.yaml) — the comment above the users
names the password — and `make dex-down dex-up` loads it. The local administrator's password must
meet `COWORK_PASSWORD_MIN_LENGTH`, 12 by default and never below 8, or the backend refuses to start
([ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md) D3).
