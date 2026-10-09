---
id: T86
title: the GitHub webhook the owner dropped is still built and released
state: done
severity: low
security: none
urgency: later        # rule 4: off in every tenant without a secret; the owner starts using cowork first
effort: M
filed-from: the owner's answer of 2026-10-09 ("das Tool braucht keine MR oder Branches tracken")
opened: 2026-10-09
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0
---

## Current state

The owner dropped the GitHub webhook before its trial
([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
Status): cowork tracks no pull requests, no branches and no pushes. The code built on 2026-10-06 is
in the tree and in every release from 0.9.0 to 0.12.0, off in each tenant that has no webhook
secret, and the UI still offers the secret in the tenant's settings.

## Required changes

None. Built: the webhook is removed; migration 41's tables leave in a later contract migration (ADR 0028).

