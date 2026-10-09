---
id: T58
title: phase 7 (hardening and 1.0) — an installation cannot yet be left running unattended
state: in-progress
severity: high
security: hardening
threat: the phase would additionally cover an installation whose failures nobody sees (no metrics, no alerts, no consistency check after a restore) and whose operator cannot follow a tested upgrade or restore procedure
urgency: release      # rule 2: gates the release of 1.0
effort: M
blocked-by:
filed-from: phase 7 of the project plan, converted by ADR 0074 D2
opened: 2026-10-06
decided: 2026-10-06
done:
---

## Current state

The family ticket of phase 7
([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2). **Goal:** an installation the owner would leave running unattended. The phase started on
2026-10-06 with phase 6, on the owner's word to build every remaining phase of the plan in one
night; this ticket consumed the phase's section of the project plan, the plan's last. **By the
owner's rule of 2026-10-06, 1.0 is not released while any question of any open ticket is
unanswered** — semantic-release cuts it from a commit marked as breaking, and no commit of this
phase carries that mark.

Already in place before the phase: the release workflow publishes the images to Docker Hub and the
chart into its Helm repository on every release
([ADR 0061](../adr/0061-images-are-published-to-docker-hub-the-runners-secrets-and-pages-are-verified.md),
[docs/developer/ci-and-release.md](../developer/ci-and-release.md)). The workflow ran for every tag
from `v0.1.0` to `v0.7.0` and failed once, for `v0.3.0` (the `cowork-mcp` binaries; `v0.3.1`
followed); the chart index lists every version from 0.1.0 to 0.7.0 (read 2026-10-06).

**The upgrade from the previous release ran in a cluster on 2026-10-07**, from 0.8.0 to 0.9.0, the
newest published pair, with the chart from the Helm repository and the images from Docker Hub: a
kind cluster of its own (kind v0.32.0, Kubernetes v1.36.1), Helm v3.21.3, PostgreSQL 18.6 from
[`deploy/examples/cloudnative-pg-cluster.yaml`](../../deploy/examples/cloudnative-pg-cluster.yaml)
under CloudNativePG 1.30.1 — applied as it is but for its password, one `Cluster` of three instances
per release — and one MinIO pod of the image `make minio-up` runs (RELEASE.2026-09-22T19-25-18Z),
with a bucket and a bucket-scoped key per release made by
[`deploy/examples/minio-bucket.sh`](../../deploy/examples/minio-bucket.sh). Every credential was an
`existingSecret`: the owner's the URL CloudNativePG writes, the runtime role's a URL Secret of its
own (0.8.0 reads URLs only), the server key 32 random bytes, a local administrator. Two releases,
each installed at 0.8.0 and upgraded with `--reset-then-reuse-values`:

- **`migrations.mode` at its default:** the new pod's `migrate` init container applied the one
  pending migration (`database schema is current`, version 41, applied 1;
  `cowork_migrations_schema_version 41`, dirty 0), and the old pod stopped once the new one was
  ready. The upgrade took 7 seconds.
- **`--set migrations.mode=job`:** the hook Job applied the migration and ran the bootstrap
  (`the bootstrap ran after the migration`), was complete a second before the first new backend pod
  was created, and was deleted once it had succeeded; the Deployment has no `migrate` init container
  any more. The upgrade took 10 seconds.

In both, what was written on 0.8.0 — a tenant, a project, two tickets, a comment and an attachment —
read the same on 0.9.0, the attachment byte for byte; a session made on 0.8.0 still authenticated; a
new login worked and a ticket was filed. A further upgrade from 0.9.0 to 0.9.0 with `--reuse-values`
changed nothing on either release: the same manifest, the same pods, and in job mode a Job that
applied nothing. Both releases were then switched to the chart values the CloudNativePG example
names — each role read as components, the location from its ConfigMap — and their migration run and
server connected with `sslmode` `require` and served. The images are published for linux/amd64
only, which the cluster's arm64 node cannot pull: they were loaded into it and ran under the host's
emulation. One further finding of the run, in the CloudNativePG example, is with the owner and not
described here until he has classified it.

The security pages were read against the code once more on 2026-10-07, after every child had
landed: what they said otherwise was corrected, and the thirty gaps the code has and no page named
are H-78 to H-107, each in the page of its mechanism. What the review found beyond hardening was
handled as its own tickets.

Children:

- T59 — metrics ([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md))
- T86 — the removal of the inbound GitHub webhook, which the owner dropped before its trial ([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md))
- T61 — the chart's remaining references, the example manifests and the migration hook Job
  ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
  D1–D4, [ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md)
  D2, D4–D6)
- T62 — request and response examples on every operation of the API document
  ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D6)
- T63 — the consistency check, the restore procedure and the operations pages for backups and
  upgrades ([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
  D4–D6)

The tenant Markdown export the plan named here is built with phase 6's export (T57).

## Required changes

1. **The children.**
2. **The phase verification.** Done: the release workflow produces a tagged image and a chart index
   with every release — the index lists every version from 0.1.0 to 0.9.0, and Docker Hub serves
   both images of 0.8.0 and 0.9.0 (read 2026-10-07); an upgrade from the previous release has run in
   a kind cluster with PostgreSQL and MinIO, the previous release's chart installed and upgraded to
   the new one, in both migration modes — 0.8.0 to 0.9.0 ([Current state](#current-state)). Left:
   every `H-<n>` in `docs/security/` closed or explicitly accepted by the owner — the owner's act,
   which only he can perform. Not run: an upgrade from 0.9.0 to the releases after it, 0.10.0 to 0.13.0
   (migrations 42 to 45); job mode in a cluster with every credential from an `existingSecret` — a
   `helm install` where the Job migrates an empty database and makes the local administrator and the
   bootstrap tenant before a pod starts, an upgrade where the Job runs before the new pods and is
   deleted once it succeeded, a failing migration that fails the release and keeps the Job for its
   log, and an upgrade of an earlier installation with `--reuse-values` and with
   `--reset-then-reuse-values` — recorded in ADR 0057's Status; and the walk-through of phase 3 by
   hand: an administrator files a ticket assigned to a local account, which sees it in "assigned to
   me" and in its inbox, moves it and closes it, two identities, through the UI alone.
3. **1.0**, once every question of every open ticket is answered.

## Open questions

### Q1: Does the audit record ask for per-token rate limits?

The plan made per-token rate limits conditional: "if the audit log asks for them".
[ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D1 and D4 decided against request budgets — abuse is answered by the per-token view of the audit
record and by revocation, nothing automatic. The evidence the plan waits for is in the audit record
of the owner's installation, which only he can read.

- **(a) No limits** (recommended, and what is built): the decision of ADR 0039 stands; the per-token
  view of the tenant's audit page shows what a token did and how fast, and an administrator revokes.
  Nothing to build.
- **(b) A per-token limit**, a token bucket per token id in the backend, configurable and off by
  default ([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
  D2's pattern): an amendment of ADR 0039 D1, a limiter shared across replicas or one per replica,
  and a problem code for the refusal.

Recommended: **(a)**, unless the owner's audit record shows a token that a limit would have
stopped before an administrator could: a limit built without such a case guesses its numbers.

**Answer:** (a) — the owner, 2026-10-08. No per-token limits; ADR 0039 records the confirmation.

## Related

- T55 — phase 6, which started the same night
- T26 — phase 3, closed; its walk-through by hand is item 2 here
