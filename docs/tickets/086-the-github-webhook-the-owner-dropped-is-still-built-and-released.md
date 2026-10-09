---
id: T86
title: the GitHub webhook the owner dropped is still built and released
state: decided
severity: low
security: none
urgency: later        # rule 4: off in every tenant without a secret; the owner starts using cowork first
effort: M
filed-from: the owner's answer of 2026-10-09 ("das Tool braucht keine MR oder Branches tracken")
opened: 2026-10-09
decided: 2026-10-09
done:
---

## Current state

The owner dropped the GitHub webhook before its trial
([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
Status): cowork tracks no pull requests, no branches and no pushes. The code built on 2026-10-06 is
in the tree and in every release from 0.9.0 to 0.12.0, off in each tenant that has no webhook
secret, and the UI still offers the secret in the tenant's settings.

## Required changes

1. **Remove the feature**, in one change: the routes `POST …/integrations/github/webhook`,
   `GET …/integrations/github`, `POST` and `DELETE …/integrations/github/secret`,
   `GET …/tickets/{number}/pull-requests` and `DELETE …/pull-requests/{pull_request}` from the API
   document and their handlers (`api/github.go`, `api/github_links.go`, `api/integrations.go`,
   `api/pullrequests.go`), the parser `internal/github`, the job `github-delivery-expiry` (its lock
   key stays unused, the lock test keeps holding the others unique), the `merged` notification, the
   UI's GitHub section of the tenant settings, the ticket page's *Pull requests* card and merge hint,
   and their tests at every tier; the session-only operations drop from nineteen to eighteen and the
   unit test that counts them with them.
2. **The records and pages in the same change:** ADR 0020 D2, ADR 0031 D6, ADR 0035 D5, ADR 0037 D5
   and ADR 0043 D2 and D3 amended in place where they name the webhook; ADR 0071's Status says the
   code is removed; docs/operations/github.md and docs/security/github-webhook.md deleted with every
   link to them; the README's reference, CLAUDE.md's stack facts and the developer pages without
   the webhook.
3. **The tables stay** until a later release: migration 41's tables are read by the releases up to
   0.12.0, so a contract migration of the release after the removal drops them
   ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)).
