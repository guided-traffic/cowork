# ADR 0057: Migrations Run on Pod Start by Default; a Helm Hook Job Is the Switchable Alternative; `serve` Refuses to Run Against a Stale Schema

## Status

Accepted, amended 2026-10-02 (D1, D2: the owner credential of ADR 0021 D2). Date: 2026-10-01.
Decided by the owner as the answer to the catalog question "where
do migrations run in production?": both modes in the chart with on-start as the default, over
on-start alone, over a hook Job alone as the only mode, and over a hook Job alone. The rules
of D4–D7 were put to the owner with the question and not objected to. The amendment of
2026-10-02 follows from the mandatory owner role of
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2: the
serving container never holds the owner credential.

**Partly built** (phase 2, 2026-10-02): D1 as amended — the chart's `migrate` init container
with the owner credential and the serving container with `COWORK_MIGRATE_ON_START=false` — and
D3's stale-schema refusal in `serve`. Not built: D2's `migrations.mode` and hook Job, D4's
bootstrap, D5's second values file per mode, D6's notes.

## Context

The skeleton migrates on start and can be told not to; migrations only move forward and
never remove what the previous release reads ([ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)
D3), which makes a rolling update safe while old pods still run. For the common
single-tenant installation ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D6) that is one `helm upgrade` and nothing else. Some installations want more: a migration
role separate from the runtime role, exactly one migration run per rollout, or a GitOps
flow where the schema change is a visible step. The binary already supports that path; what
was missing was the chart's half and a guard against pods serving a schema they do not know.

## Decision

**D1 — Default: migrations run at pod start.** `migrations.mode: onStart` (the chart's
default) leaves `COWORK_MIGRATE_ON_START=true`; every backend replica runs the migration
under the advisory lock before it listens; the startup probe covers the duration
([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D8). Nothing changes for an installation that says nothing. *(Amended 2026-10-02: in the chart
the on-start run is an init container of the backend pod that runs `cowork migrate` with the
owner credential of ADR 0021 D2; the serving container holds the runtime credential alone and
runs with `COWORK_MIGRATE_ON_START=false`. Every replica still migrates under the advisory
lock before it listens; the migration's duration delays the pod's start instead of running
under the startup probe. `cowork serve` migrates by itself only where it is given the owner
URL — a development convenience, never the chart's way.)*

**D2 — Alternative: a Helm hook Job.** `migrations.mode: job` renders a Job with the same
image and `args: ["migrate"]`, annotated `pre-install,pre-upgrade`, `hook-weight: "-10"`,
`hook-delete-policy: before-hook-creation,hook-succeeded`, and sets
`COWORK_MIGRATE_ON_START=false` on the Deployment. ~~The Job reads the database URL from the
same Secret as the pods, or from `migrations.job.existingSecret` when the installation uses
a separate owner role for DDL.~~ *(Amended 2026-10-02: the Job reads the owner credential of
[ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3; the separate owner role is no longer optional.)* A failed Job fails the release before any new pod starts.

**D3 — `serve` refuses to run against a stale schema.** Whatever the mode, the backend
compares the embedded migration set with the database's version at start; with pending
migrations and `COWORK_MIGRATE_ON_START=false` it exits 1 with
`pending migrations: N; run the migration job (or set COWORK_MIGRATE_ON_START=true)`. A pod
never serves a schema older than its binary expects.

**D4 — Bootstrap follows the migration** ([ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md)):
in `onStart` mode it runs in the pod after the migration; in `job` mode `cowork migrate`
performs it after the schema step, under the same lock, with the same configuration.

**D5 — Both modes are rendered and tested.** `deploy/helm/cowork/ci/` carries a values file
per mode; `make helm-lint` and `make helm-template` cover both; the end-to-end tier
([ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md))
runs on `onStart`, and an integration test exercises D3's refusal.

**D6 — GitOps notes in the operations page.** ArgoCD and Flux translate Helm hooks into sync
phases; the page names the annotations each needs (`argocd.argoproj.io/hook`, Flux's
`spec.install.remediation` / `upgrade` behaviour) so that `job` mode works outside plain
Helm.

**D7 — A dirty schema is repaired by a person in either mode** (ADR 0001 D5); neither the
pod nor the Job repairs it.

## Consequences

- The default installation stays one command; the installation that wants a separate DDL
  role or one run per rollout flips one value and provides one Secret.
- D3 turns a misconfiguration (job mode, job not run) into a loud failure instead of a pod
  that serves and fails on the first query of a new column.
- One Job template, one value, one more `ci/` file, one more start-up check in `serve`.
- Rolling updates stay safe in both modes because ADR 0028 D3 is what makes them safe, not
  the mode.

## Alternatives Considered

- **On-start only, as built.** Correct for the common case; no answer for role separation
  or GitOps flows that want the schema step visible. Lost as the only mode, kept as the
  default.
- **A hook Job as the only mode.** Exactly one run per rollout and a separate role for all;
  hook lifecycle, GitOps translation and a second Secret for the installation that has one
  client and no such needs. Lost.

## Residual risks

- Helm hooks and GitOps controllers differ in detail; D6's notes are written from the
  controllers' documentation and verified only when an installation uses them.
- A long backfill in `onStart` mode blocks every new pod until it completes and may exceed
  the startup probe; ADR 0028 D3 keeps backfills additive, and a migration expected to take
  minutes is run in `job` mode with the probe untouched.

## References

- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D5, D8 — the on-start run and the startup probe
- [ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D3 — what makes rolling updates safe in both modes
- [ADR 0032](0032-bootstrap-from-helm-values-a-local-administrator-synced-from-a-secret-and-an-init-state-for-administrators-only.md) — the bootstrap that follows the migration
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D2 — the role constraints the owner role must respect
- [`deploy/helm/cowork/`](../../deploy/helm/cowork/), [`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) — where the mode and the refusal land
