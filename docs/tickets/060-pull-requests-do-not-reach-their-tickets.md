---
id: T60
title: pull requests do not reach their tickets
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: built; what is left is the owner's trial and look
effort: L
blocked-by: human
filed-from: ADR 0071 (phase 7)
opened: 2026-10-06
decided: 2026-10-01
done:
---

## Current state

The inbound GitHub webhook of
[ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
is built, D1–D7, on trial; the details it left open were made concrete by the implementer on
2026-10-06 and are listed in the record's Status, open to the owner's objection.

- **The endpoint** `POST /api/v1/tenants/{tenant}/integrations/github/webhook`, public and signed
  (`x-cowork-signed: github`): the tenant and its sealed secret first — no secret, `404` like an
  unknown tenant —, the body bounded by `COWORK_MAX_JSON_BODY`, `X-Hub-Signature-256` in constant
  time before anything is parsed (`401 signature_invalid`), the delivery kept a day (a repetition
  `200`), then `pull_request` (opened, edited, synchronize, reopened, closed) and `push` to the
  default branch of a bound repository; everything taken answers `202`
  ([`api/github.go`](../../backend/internal/api/github.go),
  [`api/github_links.go`](../../backend/internal/api/github_links.go),
  [`internal/github`](../../backend/internal/github/)).
- **The secret**: made and rotated by a tenant administrator in a browser session (the eighteenth
  session-only operation), revoked by an administrator's `admin`-scope credential, never an agent's;
  sealed under a key derived from `COWORK_SESSION_KEY`, bound to the tenant
  ([`api/integrations.go`](../../backend/internal/api/integrations.go)).
- **What a ticket gains**: `ticket_pull_requests` (migration 42) under the ticket's predicate,
  `GET` and `DELETE …/tickets/{number}/pull-requests`, the acts `linked`, `merged`, `closed`,
  `reopened`, `updated` (left out of the activity) and `unlinked`, the inbox reason `merged`, the
  event `pull_request.changed`, a `## Pull requests` section in the context document; in the UI the
  card on the ticket page, the merge hint, the activity's and the inbox's lines, and the GitHub
  section of the tenant's settings.
- **The documentation**: [docs/operations/github.md](../operations/github.md),
  [docs/security/github-webhook.md](../security/github-webhook.md) with H-64 to H-67, and the
  developer pages.

**Verified** on 2026-10-07: `make lint cyclo gosec`, `make generate-check` and
`make frontend-generate-check frontend-test frontend-lint frontend-build` pass (the frontend: 120
files, 4205 tests). `make test-unit` passes but for the three store tests that hold the migration set
gapless (`TestMigrationFilesAreWellFormed`, `TestCountVersionsBetween`, `TestSchemaStatePending`),
and `make test-integration` passes but for the three that count the migrations applied
(`TestMigrateBringsFreshDatabaseToCurrentVersion`, `TestRankMigrationKeepsNumberOrder`,
`TestStagesMigrationBackfill`): versions 40 and 41 are missing until the work that brings them is
integrated, and all six pass with the migration numbered 40. The sixteen webhook tests of
[`api_github_test.go`](../../backend/test/integration/api_github_test.go) pass, over payloads rendered
from GitHub's documented shapes.

## Required changes

1. **The owner's trial on a real repository** (D1: the feature earns its place or goes): set the
   webhook up for one repository of a tenant as
   [docs/operations/github.md](../operations/github.md) says, work with it for a while, and decide —
   kept, ADR 0071's Status says so; dropped, the record is amended and the tables leave in a later
   release's migration ([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)).
2. **The owner looks at the UI** under `make dev`, in both schemes: the GitHub section of the
   tenant's settings with its secret dialog, the *Pull requests* card and the merge hint on a ticket,
   the activity's lines and the inbox's `merged` entry.
3. **The answers to the questions below**, each built or amended in ADR 0071 in the same change.

## Open questions

### Q1: What does a push to the default branch add to a ticket?

D5 reads keys from a pull request's commits, and D2 forbids fetching them: the commits reach cowork
only in the push that brings them to the default branch. What that push adds is not settled.

- **(a) A commit entry in the ticket's list of pull requests, its act in the activity, the merge
  hint — and no notification.** *Recommended and built:* a direct push to the default branch — the
  owner pushes documentation straight to `main` — shows on the ticket like a merge, at the cost of a
  commit beside its pull request after a squash merge, and of a commit per commit after a merge
  commit whose every commit carries the trailer. A pull request's merge tells the watchers already;
  telling them of its commit as well would tell them twice.
- (b) An act in the activity only: no entry, no hint. Less on the ticket; a direct push leaves no
  hint.
- (c) Nothing: pull requests only. A direct push never reaches the ticket.

**Answer:** _open_

### Q2: Does a person's removal of a wrong link stay, and may an agent remove one?

The Residual risks say a wrong link is removed by a person like any link. A plain delete would be
undone by the next delivery that names the ticket — every `synchronize` of an open pull request.

- **(a) The removal stays, and removing is in the agent baseline like any link's.** *Recommended
  and built:* the row stays marked removed, so no later delivery brings the link back; a member's
  act with `write` scope, recorded `unlinked`. Nothing brings a removed link back — not even a
  person —, which makes an agent's removal final: it loses information the audit record and GitHub
  keep, and nothing else.
- (b) The removal stays, and it is hard-off for agents. Nothing an injected text could make the chat
  do here; one more rule in ADR 0043 D3.
- (c) A plain delete: the next delivery that names the ticket links it again. A removal of an open
  pull request's link lasts until its next push.

**Answer:** _open_

### Q3: Does a key that leaves a pull request's title or body unlink the ticket?

A delivery links the tickets its texts name. When an edit takes a key out — the author saw the
wrong ticket named —, the link made before is not said to go.

- **(a) No: deliveries only add links, and a person removes a wrong one.** *Recommended and built:*
  a link once made is a fact of the ticket's history, and the removal of Q2 is there for a wrong
  one; it costs a person's act where the author's edit already said so.
- (b) Yes: the pull request's text is the truth of its own links — an edit that drops a key unlinks
  that ticket, recorded as the webhook's act. The author's correction is enough; a ticket can lose a
  link a person wanted, and a commit's link would follow other rules than a pull request's.

**Answer:** _open_

### Q4: When a pull request's body names full keys, are the short keys of its title read as well?

D5 reads the trailer first and the subject's short keys as the fallback; ADR 0068 D5 puts the full
key on the body's first line and the short key in the title, so both usually name the same ticket.

- **(a) The title only when the body names no key.** *Recommended and built:* the machine reads the
  full form (ADR 0068 D2) and the short one only where there is none, as D5 says; a title naming
  another ticket than the body is a mistake whose cost stays on the text that is not the convention.
- (b) Both, always: every key of the title and of the body links. Nothing a person wrote is passed
  over; a title's short key that meant another tenant's project of the same key cannot be told
  apart.

**Answer:** _open_

## Not verified

- **No delivery from GitHub itself has reached cowork.** The payloads are fixtures in the shape of
  GitHub's documentation, and the signature is checked against GitHub's documented example; a live
  webhook — its ping, a pull request, a push, a redelivery — is the trial of step 1.
- **The UI has not been looked at in a browser** — `make dev` was not run —, in neither scheme.
- An organisation's webhook, which sends the same events for every repository of the organisation,
  and a GitHub Enterprise Server were not tried.
- Whether GitHub signs a delivery it sends again from its log with the secret it holds then.
- **Migration 42 has not run after 40 and 41.** It restates the `tenants_read` policy of migration
  26 with the webhook's job added; a migration 40 or 41 that changes that policy as well must be
  merged into 42's statement, or the later one drops the other's clause.
