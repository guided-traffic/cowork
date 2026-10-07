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
filed-from: docs/planning/project-plan.md phase 7, converted by ADR 0074 D2
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

Children:

- T59 — metrics ([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md))
- T60 — the inbound GitHub webhook, on trial ([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md))
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
2. **The security pages reviewed against the code once more**, after the children landed: every
   statement of `docs/security/` checked against the tree, every `H-<n>` still true or closed.
3. **The phase verification:** the release workflow has produced a tagged image and a chart index
   (every release does); an upgrade from the previous release has run in a cluster — a kind cluster
   with PostgreSQL and MinIO, the previous release's chart installed and upgraded to the new one,
   in both migration modes; and every `H-<n>` in `docs/security/` is closed or explicitly accepted
   by the owner — the owner's act, which only he can perform.
4. **1.0**, once every question of every open ticket is answered.

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

**Answer:** _open_

## Related

- T55 — phase 6, which started the same night
- T26 — phase 3, still open for the owner's reviews
