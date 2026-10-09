---
id: T32
title: the tenant has no attachment quota
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided and built; the owner reviews the result
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done: 2026-10-09
shipped: built and released in phase 3, before 0.8.0
---

## Current state

The browser uploads to a comment of the person's own, shows a raster attachment as a preview —
each load a recorded download — and corrects a time entry over its version, with its earlier
values on request ([`frontend.md`](../developer/frontend.md#the-detail-page)).

The per-tenant quota of ADR 0016 D6 is built on this branch, Q1 answered (a), enforce, on the
recommendation, the owner reviewing the result
([ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D6 and its residual risks, [ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2, amended 2026-10-05):

- `COWORK_ATTACHMENT_TENANT_QUOTA`, a size, `0` — none — by default
  ([`config.go`](../../backend/internal/config/config.go)), in the chart
  `backend.config.attachmentTenantQuota`. The default is none because no figure suits every
  installation, a single tenant's bound is the bucket, and an upgrade must not start refusing
  uploads; [runtime.md](../operations/runtime.md#the-tenants-attachment-quota) tells an installation
  of several tenants to set it.
- Where it is set, an upload takes the tenant's quota lock (`cowu`, before the ticket's), sums every
  attachment of the tenant and refuses a file that does not fit with `409 attachment_quota` before a
  row or an object exists ([`attachments.go`](../../backend/internal/api/attachments.go)
  `lockQuota`, `withinQuota`). A deleted ticket's files count until the purge removes them: they
  occupy the bucket until then, so a deletion frees no quota and a purge does.
- The usage for the tenant's administrators: `GET /api/v1/tenants/{tenant}/attachment-usage`, with a
  weak `ETag` and `304`, shown on the tenant's settings page with a meter and read again on an upload
  or a purge in the tenant
  ([`tenant-settings.ts`](../../frontend/src/app/features/tenant/tenant-settings.ts)).
- Tests: `TestTheTenantAttachmentQuota` (a confidential ticket's file counts; the refusal names no
  sum; tenant B's uploads ignore tenant A's usage and B's usage shows B's alone; the usage is the
  administrators'; another tenant's administrator gets `404`) and
  `TestSimultaneousUploadsKeepTheTenantQuota` (eight uploads to eight tickets at once, three fit),
  `TestADeletedTicketsFilesCountAgainstTheQuotaUntilThePurge`, `TestTheAttachmentUsageAnswersAWeakETag`;
  `config_test.go`; `tenant-settings.spec.ts`.

The README reference (variable, chart value, route, code), [storage.md](../developer/storage.md),
[data-access.md](../developer/data-access.md), [api.md](../developer/api.md),
[frontend.md](../developer/frontend.md), [runtime.md](../operations/runtime.md) and
[attachments.md](../security/attachments.md) (H-10 rewritten) carry it.

## Required changes

None. The owner reviews it in use ("Lass mich doch erstmal anfangen das Tool zu verwenden", 2026-10-09): T55 holds the review of the built pages as one item, and what he wants changed becomes a ticket of its own.

## Not verified

The settings page's usage has not been looked at in a browser; `make dev` was not run for this work.
