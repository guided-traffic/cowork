---
id: T60
title: pull requests do not reach their tickets
state: dropped
severity: low
security: none
threat:
urgency: icebox       # dropped by the owner on 2026-10-09
effort: L
blocked-by:
filed-from: ADR 0071 (phase 7)
opened: 2026-10-06
decided: 2026-10-01
done: 2026-10-09
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
- **What a ticket gains**: `ticket_pull_requests` (migration 41) under the ticket's predicate,
  `GET` and `DELETE …/tickets/{number}/pull-requests`, the acts `linked`, `merged`, `closed`,
  `reopened`, `updated` (left out of the activity) and `unlinked`, the inbox reason `merged`, the
  event `pull_request.changed`, a `## Pull requests` section in the context document; in the UI the
  card on the ticket page, the merge hint, the activity's and the inbox's lines, and the GitHub
  section of the tenant's settings.
- **The documentation**: [docs/operations/github.md](../operations/github.md),
  [docs/security/github-webhook.md](../security/github-webhook.md) with H-64 to H-67, and the
  developer pages.

**Verified** on 2026-10-07 on the branch that integrates it after migration 40, as migration 41:
`make generate-check test-unit lint cyclo gosec vuln`, `make test-integration` and
`make frontend-generate-check frontend-test frontend-lint frontend-build` pass — every test, the
sixteen webhook tests of [`api_github_test.go`](../../backend/test/integration/api_github_test.go)
among them, over payloads rendered from GitHub's documented shapes.

## Required changes

None. **Dropped by the owner on 2026-10-09, before the trial:** cowork tracks no pull requests, no
branches and no pushes ([ADR 0071](../adr/0071-an-inbound-signed-github-webhook-links-pull-requests-to-tickets-optional-and-on-trial.md)
Status). The removal of the code built on 2026-10-06 is T86.

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

**Answer:** lapsed — the owner dropped the webhook on 2026-10-09.

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

**Answer:** lapsed — the owner dropped the webhook on 2026-10-09.

### Q3: Does a key that leaves a pull request's title or body unlink the ticket?

A delivery links the tickets its texts name. When an edit takes a key out — the author saw the
wrong ticket named —, the link made before is not said to go.

- **(a) No: deliveries only add links, and a person removes a wrong one.** *Recommended and built:*
  a link once made is a fact of the ticket's history, and the removal of Q2 is there for a wrong
  one; it costs a person's act where the author's edit already said so.
- (b) Yes: the pull request's text is the truth of its own links — an edit that drops a key unlinks
  that ticket, recorded as the webhook's act. The author's correction is enough; a ticket can lose a
  link a person wanted, and a commit's link would follow other rules than a pull request's.

**Answer:** lapsed — the owner dropped the webhook on 2026-10-09.

### Q4: When a pull request's body names full keys, are the short keys of its title read as well?

D5 reads the trailer first and the subject's short keys as the fallback; ADR 0068 D5 puts the full
key on the body's first line and the short key in the title, so both usually name the same ticket.

- **(a) The title only when the body names no key.** *Recommended and built:* the machine reads the
  full form (ADR 0068 D2) and the short one only where there is none, as D5 says; a title naming
  another ticket than the body is a mistake whose cost stays on the text that is not the convention.
- (b) Both, always: every key of the title and of the body links. Nothing a person wrote is passed
  over; a title's short key that meant another tenant's project of the same key cannot be told
  apart.

**Answer:** lapsed — the owner dropped the webhook on 2026-10-09.

### Q5: Are the pull requests of authors outside the repository linked?

Since 0.12.0 the webhook links only pull requests whose `author_association` is `OWNER`, `MEMBER` or
`COLLABORATOR`: anyone can open a pull request against a public repository, and its title would reach
the ticket's context document, which Claude Code sessions and the chat read (ADR 0071 D4 as amended
2026-10-07).

- **(a) Only owners, members and collaborators** (built): an outside contributor's pull request stays
  unlinked until a maintainer pushes its commits to the default branch, which links them by their
  messages.
- **(b) Outside authors' pull requests linked without their title**: the link names the number, the
  state and the author, and the context document shows no text the author wrote.
- **(c) Every author, as before.**

Recommended: **(a)** — no text a stranger wrote reaches a model; (b) keeps the link at the cost of a
second rule in every surface that shows a pull request.

**Answer:** (a) — the owner, 2026-10-07. Only owners, members and collaborators; ADR 0071 D4 records it as confirmed.

## Not verified

- **No delivery from GitHub itself has reached cowork.** The payloads are fixtures in the shape of
  GitHub's documentation, and the signature is checked against GitHub's documented example; a live
  webhook — its ping, a pull request, a push, a redelivery — is the trial of step 1.
- **The UI has not been looked at in a browser** — `make dev` was not run —, in neither scheme.
- An organisation's webhook, which sends the same events for every repository of the organisation,
  and a GitHub Enterprise Server were not tried.
- Whether GitHub signs a delivery it sends again from its log with the secret it holds then.
- **The policy migration 41 restates.** It restates the `tenants_read` policy of migration 26 with
  the webhook's job added; a later migration that changes that policy must carry the webhook's clause
  in its statement, or it drops it. Migration 40 does not touch it.
