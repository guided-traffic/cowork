---
id: T61
title: the chart takes no component keys, no config maps, no examples and no migration Job
state: in-progress
severity: medium
security: hardening
threat: an authority for the database's certificate (Q3) would additionally cover a principal who intercepts the connection between the backend and the database inside the cluster network, where `sslmode` `require` encrypts it without verifying the server
urgency: release      # rule 2: the Job's first run in a cluster gates the release that ships it
effort: S
blocked-by: decision
filed-from: the build of ADR 0057 D2–D6 and ADR 0058 D1–D4 in phase 7
opened: 2026-10-06
decided:
done:
---

## Current state

What ADR 0057 and ADR 0058 decide is built and documented, each detail the records left open
marked "made concrete by the implementer" in their `Status` and decisions:

- each database role is a URL or its components, composed in
  [`config/database.go`](../../backend/internal/config/database.go); the chart reads them by
  configurable keys, the location optionally from a ConfigMap
  ([`_helpers.tpl`](../../deploy/helm/cowork/templates/_helpers.tpl) `cowork.databaseSource`), and
  the storage's endpoint, bucket, region and path style from `storage.existingConfigMap`;
- `migrations.mode: job` renders the hook Job
  [`migrate-job.yaml`](../../deploy/helm/cowork/templates/migrate-job.yaml), which runs
  `cowork migrate` with `COWORK_MIGRATE_BOOTSTRAP=true` and takes `existingSecret` references only;
- [`deploy/examples/`](../../deploy/examples/) holds a CloudNativePG cluster, a MinIO Tenant and the
  `mc` commands of a bucket-scoped key, checked by `make examples-lint` in the `helm` job;
- the manifests of every earlier `ci/` values file render as before, byte for byte, with the new
  defaults and with the values of 0.7.0 that `helm upgrade --reuse-values` keeps (Helm 3.21.3 and
  4.3.0); `ci/components-values.yaml` and `ci/migrations-job-values.yaml` render both modes;
  [`command_test.go`](../../backend/test/integration/command_test.go) runs the built binary as the
  Job and the serving container run it.

What is open: three decisions the records do not settle, below — the first two built with the
recommended option, the third recommended for a change of its own — and the Job's first run in a
cluster.

## Required changes

Independent of the open questions:

1. Run job mode in a cluster, with every credential from an `existingSecret`: a `helm install`,
   where the Job migrates an empty database and leaves the local administrator and the bootstrap
   tenant before a pod starts; an upgrade, where the Job runs before the new pods and is deleted
   once it succeeded; a migration that fails — a dirty schema — failing the release with the Job
   kept for its log; the upgrade of a 0.7.0 installation that sets nothing new, with
   `--reuse-values` and with `--reset-then-reuse-values`, leaving every object as it was. Record what
   was run in ADR 0057's `Status`.
2. The `helm` job's first run on a runner: `make examples-lint` installs kubeconform with
   `go install` and fetches the operators' CustomResourceDefinitions, so it needs Go — the job sets
   it up now — and the network.

Depends on the answers:

- Q1 (b): a variable that keeps `cowork serve` from the bootstrap, set by the chart in job mode; the
  local administrator's Secret out of the serving container in that mode; the recovery of a leaked
  administrator password in [installation.md](../operations/installation.md#the-local-administrator)
  and [local-accounts.md H-20](../security/local-accounts.md) rewritten per mode; ADR 0057 D4 amended.
- Q2 (b) or (c): ADR 0058 D1 amended, the example files and the `Makefile`'s pinned versions changed,
  the operations page's table with them.
- Q3 (a) or (b): ADR 0058 D3 gains the database's authority; `database.tls.caConfigMap` and its key,
  mounted read-only into the init container, the Job and the serving container; the backend reads it
  for both pools; a test against a PostgreSQL that serves TLS.

3. **The database's authority** (Q3, answered b): `database.tls.caConfigMap` and `keys.ca`, mounted
   as the storage's authority is, and one variable the backend applies to the runtime pool, the
   owner pool and the migration run; the integration tier gains a PostgreSQL that serves TLS and a
   test that `verify-full` holds against the private authority and refuses a server outside it;
   README reference, trust-boundaries.md (H-78 closed) and ADR 0058 D3's row marked built, in the
   same change.

4. **PGSTY Silo in place of MinIO** (Q2, answered c): `deploy/examples/minio-tenant.yaml` and
   `minio-bucket.sh` give way to a values file for Silo's Helm chart (`helm/silo` of
   [pgsty/silo](https://github.com/pgsty/silo) at a pinned release tag; Silo publishes no Helm
   repository) and an `mcli` script for the bucket, the access key and the bucket-scoped policy, run
   once against the store of `make minio-up`; `make examples-lint` renders the chart at that tag with
   the example's values and checks the output with kubeconform, `MINIO_OPERATOR_VERSION` and the
   Tenant CRD schema leave the Makefile; `MINIO_IMAGE` becomes `pgsty/silo` at a pinned release,
   with its Renovate comment, and the integration and end-to-end tiers pass against it; the
   operations page, the developer pages and the README name Silo. Whether the targets keep the name
   `minio-up` is decided with the build.

## Open questions

### Q1: Does `cowork serve` keep its own bootstrap in job mode?

ADR 0057 D4 says that in job mode `cowork migrate` performs the bootstrap after the schema step; it
does not say whether the serving pods stop running it.

- **(a) Both run it** — built. The Job synchronises the local administrator and the bootstrap tenant
  before a pod starts; each pod's start finds it in step and changes nothing. Cost: the local
  administrator's password stays in the serving container's environment, as in `onStart` mode, and
  the synchronisation runs twice per upgrade. Gain: rotating the Secret and restarting the pods ends
  a leaked password in both modes — the recovery the operations page gives — and no new variable.
- **(b) The Job alone.** A new variable keeps `cowork serve` from the bootstrap, and the chart sets it
  in job mode. Gain: the serving container no longer holds the local administrator's password. Cost:
  a rotated Secret reaches the account only at the next install or upgrade, so the documented
  recovery — rotate and restart — silently does nothing in one mode and a leaked password stays
  valid; a variable and two recovery procedures to document.

**Recommended: (a).** What (b) takes out of the serving process is small beside what that process
holds already — the runtime role, which reads and writes every tenant's rows, and the server key —,
while (b) turns the recovery of a leaked administrator password into a step that does nothing in one
of two modes.

**Answer:** (a) — the owner, 2026-10-07. Both run it; ADR 0057 D4 records it as confirmed.

### Q2: Do the MinIO examples stay, now that MinIO's projects are archived?

ADR 0058 D1 asks for a MinIO Operator `Tenant` and `mc` commands. Checked on 2026-10-06: the
repositories of the MinIO Operator, the MinIO server and `mc` are archived on GitHub, and
`minio/minio`, the server image the operator defaults to, can no longer be pulled from Docker Hub or
quay.io; the Tenant example names Chainguard's build, which the integration tier runs and which was
not tried with the operator.

- **(a) Keep both as decided**, marked archived on every page that names them — built. Cost: two
  examples for software that gets no fix.
- **(b) Keep the `mc` commands, drop the Tenant.** The commands work against any MinIO-compatible
  administration API and were run against the MinIO of `make minio-up`. Cost: ADR 0058 D1 amended;
  no example for an operator-managed store.
- **(c) Replace both with another S3 store's example** — a maintained operator, chosen by the owner;
  candidates were not looked into here. Cost: a new example, its CRDs in `make examples-lint`,
  ADR 0058 D1 amended.

**Recommended: (a)** until the owner names the store to replace MinIO with: nothing an installation
may copy disappears unasked, and every place that names the examples says that MinIO gets no fix.

**Answer:** (c), with the store named — the owner, 2026-10-09: "Wir wechseln auf
https://github.com/pgsty/silo". Read as the switch of every MinIO server cowork names, the example
and the test tiers' container alike — made concrete when the answer was recorded, open to the
owner's objection; ADR 0058 D1 and D2 as amended; the build is required change 4.

### Q3: Should the chart take an authority for the database's certificate?

The chart mounts no authority for the database: a role's `sslmode` can be `require` — encrypted, the
server unverified — but not `verify-ca` or `verify-full` against a private authority such as
CloudNativePG's own. That holds for the URL as for the components; ADR 0058's residual risks and
[trust-boundaries.md](../security/trust-boundaries.md) say so.

- **(a) Now, in this ticket**: `database.tls.caConfigMap` and its key, as the storage's, and a
  variable the backend applies to both pools. Cost: a security-relevant interface no record decides,
  built without the owner's word, and not provable in the integration tier, whose PostgreSQL serves
  no TLS.
- **(b) As a change of its own**, after an amendment of ADR 0058 D3 that adds the database's row, with
  a PostgreSQL that serves TLS in the tests. Cost: the gap stays until then.
- **(c) Never**: an installation that needs a verified server brings a database whose certificate
  chains to an authority of the image's system pool. Cost: CloudNativePG's default certificates
  cannot be verified.

**Recommended: (b).** The interface touches the TLS configuration of both pools and the migration run,
and only a test against a server that serves TLS proves it; it is worth deciding first, not building
unasked.

**Answer:** (b) — the owner, 2026-10-07. ADR 0058 D3 carries the database's authority row, marked
not built; the build is required change 3.

## Not verified

- The Job in a cluster: the hook's order against the release's resources, its deletion policy, a
  failed Job failing the release — required change 1.
- The notes for Argo CD and Flux on the operations page: written from their documentation (Argo CD
  v3.5.3, helm-controller v1.6.5), run with neither; whether Flux's `spec.upgrade.preserveValues`
  takes the new chart's defaults.
- The CloudNativePG and Tenant examples: never applied; the Tenant's image never run by the operator.
- Renovate's regex manager on the new `Makefile` lines (`KUBECONFORM_VERSION`, `CNPG_VERSION`,
  `MINIO_OPERATOR_VERSION`): the pattern matches them, no Renovate run has confirmed it.
