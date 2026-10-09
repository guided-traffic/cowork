---
id: T81
title: a delivery dated in the future pins a pull request's link, and GitHub's genuine later deliveries change nothing
state: done
severity: low
security: boundary
threat: the holder of a leaked webhook secret sends a pull_request delivery with updated_at far in the future; every ticket's link to that pull request keeps the title, state and author it wrote, because an update requires source_updated_at at most the delivery's — a real merge is never shown or notified, and the pin outlives the secret's rotation
urgency: later         # rule 4: a known fix; the webhook is on trial and off by default
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: the GitHub webhook is removed
---

## Current state

The owner dropped the webhook on 2026-10-09 (ADR 0071 Status); its removal, T86, takes this finding
with it. Until that ships, the finding stands in every release that has the webhook, for a tenant
that set a secret.

payload.go:143 checks `updated_at` only for zero; write/github.sql:91 updates a link only from a
delivery not older than the stored one. A person's removal of a link is permanent (:52-61, 104-111).

## Required changes

1. Clamp `updated_at` and `merged_at` to the server's time plus a small skew; a genuine delivery
   overwrites a row dated in the future; a test with a future-dated delivery followed by a genuine one.
