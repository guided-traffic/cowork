# ADR 0071: An Inbound, Signed GitHub Webhook Links Pull Requests to Tickets and Notifies on Merge — Optional per Tenant, Nothing Outbound, No Automatic Transition, Built in Phase 7 on Trial

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"GitHub integration?": the inbound webhook, over nothing, over an automatic transition on
merge, and over an outbound integration with a GitHub credential in cowork — with the owner's
qualification that it is **not mandatory**: it is tried, and may be dropped if it does not
earn its place. The rules of D5–D8 were put to the owner with the question and not objected
to.

**Not built.** Phase 7.

## Context

[ADR 0068](0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md)
put the full ticket key into a commit trailer and the pull request body precisely so that a
machine could read it. [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5
says nothing leaves cowork; [ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md)
says cowork touches no repository; [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
D5 and D6 make `done` a person's or a capable agent's act with a verification note, and the
tickets page says "merged is not verification". What a ticket can gain without breaking any
of that is context: which pull requests belong to it, and the moment one is merged — as a
fact shown and notified, not as a state changed.

## Decision

**D1 — Optional, per tenant, off by default.** A tenant administrator creates a GitHub
webhook secret in the tenant's settings (shown once, rotatable, revocable); until one
exists the endpoint answers `404` for that tenant. The feature is **on trial**: if it does
not prove useful it is removed by amending this record, and nothing else depends on it.

**D2 — The endpoint is inbound only:** `POST /api/v1/tenants/{slug}/integrations/github/webhook`.
cowork holds no GitHub token and makes no call to GitHub; it is called.

**D3 — Signature first, then size, then dedup.** Every delivery is verified against the
tenant's secret with GitHub's `X-Hub-Signature-256` before the body is parsed; an invalid or
missing signature answers `401` without processing; the body is bounded by the JSON limit of
[ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2; the delivery id is kept for twenty-four hours and a repeat answers `200` without effect.

**D4 — Only `pull_request` (opened, edited, synchronize, closed) and `push` to the default
branch are processed;** every other event answers `202` and is discarded. The repository in
the payload (`repository.clone_url`) is normalised and resolved to a project by
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D1–D2; an unbound repository answers `202` and is discarded — no error back to GitHub, no
leak of what is bound.

**D5 — Keys are read from the trailer first, the subject suffix as fallback** (ADR 0068 D1,
D2), from the pull request's title and body and from its commits' messages; a pull request
may reference several tickets, and a key of another tenant is ignored.

**D6 — What a ticket gains:** a `## Pull requests` section (number, title, state, URL,
author, merged at) and a timeline entry when a pull request is opened, merged or closed.
**No state changes.** A merge creates a notification to the assignee and the watchers
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D2 gains the event) and a
hint on the ticket; a person or a capable agent moves the ticket.

**D7 — Setting the webhook up is the operator's step,** per repository in GitHub, with the
endpoint URL and the secret; the operations page shows it. No GitHub App, no organisation
installation.

**D8 — GitLab and others come through the same mechanism** with a payload adapter per
provider, each its own amendment, each optional.

## Consequences

- A ticket shows its pull requests and the moment of merge, with one small public endpoint
  per installation, HMAC-protected, idempotent, bounded — and no credential in cowork.
- Nothing automatic happens to a ticket; the verification rule holds, and "merged is not
  verification" stays true in cowork as it is in the tickets page.
- The trial framing is honest: the owner is not sure it is needed; D1 makes removal cheap.
- The pull-request table and the parser are the whole implementation; the notification
  event is one more row in ADR 0020 D2's table.

## Alternatives Considered

- **Nothing in the first release.** No surface, no setup; "merged, now move the ticket"
  stays manual and a ticket does not know its pull requests. Lost narrowly — the trial is the
  owner's compromise.
- **An automatic transition on merge** (`in-progress → done` with the pull request as the
  note). The tempting case; contradicts ADR 0009 D5 and the tickets page's rule, and a
  webhook would be the one actor without accountability. Lost.
- **An outbound integration** (comments on the pull request, a commit status when the
  trailer is missing). Visibility inside GitHub and enforcement of ADR 0068; a GitHub App or
  token inside cowork and outbound calls. Not now; its own record if the discipline of
  ADR 0068 proves insufficient.

## Residual risks

- A public endpoint, however small: D3's signature check is the whole defence, and the
  security page of integrations carries it with its `H-<n>` for what signature verification
  does not cover (a leaked secret).
- Keys read from free text can be wrong (a key mentioned, not meant); the section shows the
  source, and a wrong link is removed by a person like any link.

## References

- [ADR 0068](0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md) — the trailer and subject suffix the parser reads
- [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md) D1, D2 — the repository resolution
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5, D6, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D5 — why no state changes
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D2, D5, [ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md) — inbound only
- [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D2 — the body limit
