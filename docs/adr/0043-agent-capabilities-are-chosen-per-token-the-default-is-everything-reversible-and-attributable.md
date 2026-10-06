# ADR 0043: Agent Capabilities Are Chosen per Token, the Default Is Everything That Is Reversible and Attributable, and a Short Hard-Off List Stays

## Status

Accepted, amended 2026-10-01 (D3, D4: `create-project` and `record-answer` become selectable
capabilities; creating a project and recording a person's answer leave the hard-off list — the
owner's grant in the answer to the catalog question "repository binding?", whose context
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
records), amended 2026-10-02 (D4:
`create-project` follows the tenant setting of
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D9 instead of the `admin` role), amended 2026-10-04 (D4: `rank` and `override-urgency` cover a filing's place and
horizon, with the horizon of ADR 0010 D3 as amended that day), amended 2026-10-05 (D4: `override-urgency` is
named `set-horizon`, the API following the word horizon as ADR 0010 D1 records it; the old name is taken
until a later release rewrites the stored sets), amended 2026-10-06 (D4: that release's contract — the
stored sets rewritten to `set-horizon`, the old name refused on input and dropped on read, as ADR 0010 D1
records it; the checks of the stored sets keep the old name until a release after it, so that an image
rollback to the release before stays safe, [ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D3),
amended 2026-10-06 once more (D4's `interest` row, the Consequences and the References: they say
what [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D4 holds instead
of amending it, and that [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) had no
agent rule for this record to amend, by the owner's rule that every amendment is made in place in the record it changes;
no rule changes), amended 2026-10-06 a third time (the note on what is built of D3: the saved filter
an agent saves, changes, shares and deletes is its person's own, while a tenant administrator's
unshare or deletion of another person's shared filter — the owner's answer recorded in
[ADR 0018](0018-the-views-of-the-first-release.md) D5 that day — is an administration act and
hard-off by D3 as it stands; no rule changes),
amended 2026-10-03 (D4: `close` covers both ways to `done`
of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5 — the write
that fills the last progress stage and done by hand — an agent's only from `in-progress` or
`review`; the owner's answer when the progress stages were made to close a ticket, since an
agent may otherwise set progress). Decided by the owner as the
answer to the catalog question "what may an agent do without a human?": not a fixed list of agent limits but a set of capabilities
the person chooses when creating an agent token, with every selectable capability switched on
by default — over fixed server-side limits (the recommendation had been "no `decided`, no
`done`"), over a conservative default, and over a mandatory choice at creation. The hard-off
list of D3 and the rules of D5–D7 were put to the owner with the decision and not objected
to.

**Built** (phase 2, 2026-10-02) for the acts that exist: D1, D2, D3, D4 and D5 — the capability
set on the token, the baseline, the hard-off list and the capabilities checked by
[`auth.Authorize`](../../backend/internal/auth/authorize.go) on every marked request — `rank`
on a move in the rank since 2026-10-03 and on adopting the score with the score (since 2026-10-05,
`sortProjectRank` in [`api/score.go`](../../backend/internal/api/score.go)), `rank` and
`override-urgency` on a filing's place and horizon since 2026-10-04 (`filing.capabilities` in
[`tickets.go`](../../backend/internal/api/tickets.go)),
`create-project`'s repository binding with that binding (since 2026-10-04 on binding and
unbinding a repository) — the set recorded on each act. D4's amendment of 2026-10-03 is built (2026-10-03): `close` on done by hand and on the
`PATCH` that fills the last progress stage, refused with `agent_forbidden` outside `in-progress`
and `review` ([`mayClose`](../../backend/internal/api/transitions.go)); without `close` that
`PATCH` is refused whole and the stage keeps its value. D6 is built (2026-10-04): `GET /api/v1/me/token` answers the token a request presents with
the request's mark — whether it is an agent's, the agent recorded, the capabilities it holds —
and `cowork-mcp` reads it at start into its tool descriptions; `finish_work` makes the
furthest move the token may — `done` with `close` from `in-progress` or `review`, else
`review` from `in-progress` — and says what remains for a person. The token page (phase 3, 2026-10-03) offers the nine
switches with the full and assisted shortcuts, and all of them off — the baseline only — which
the API takes as an empty list; only a list left out is every capability. Acts no record lists are open to agents
— reassigning a confidential ticket, removing a `blocks` link, backward moves and reopens
(since 2026-10-03 the withdrawal of a done by hand and the lower stage that reopens among them),
removing a stake, editing a question, editing a project — until a review after experience.
Amended 2026-10-04 (D5: a session the agent header marks, and the chat in the UI, which is one —
provisionally, with [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md))
and built the same day. Amended again on 2026-10-04 by the owner's answers to the chat's open
questions, recorded in ADR 0076, and built the same day (D5: the person chooses the chat's
capabilities from the nine of D4 — by default every one but `decide`, `close`, `drop` and
`record-answer` —, a session the agent header marks holds that set and no longer every capability,
and the chat proposes nothing: every call runs at once).
*(2026-10-05:)* D3's "deleting, restoring or purging anything" is built for tickets as the hard-off rule
`deleting, restoring or purging` ([`api/deletion.go`](../../backend/internal/api/deletion.go)): an
agent-marked request — a token's or the chat's — that deletes, restores or purges a ticket is
`403 agent_forbidden`. Saving, changing, sharing and deleting ~~a saved filter~~ *(2026-10-06:)* its
person's own saved filter is an act no record lists and is open to agents until the review after
experience; a tenant administrator's unshare or deletion of another person's shared filter
(ADR 0018 D5 as amended 2026-10-06) is an administration act, refused to an agent as
`hard-off: administration`.
*(2026-10-05:)* D4's amendment of 2026-10-05 is built, its expand half: `set-horizon` is the
capability's name in `auth.AllCapabilities`, `auth.DefaultChatCapabilities`, the tool descriptions,
the chat's instructions and the UI's nine switches; `auth.Canonical` reads `override-urgency` as
`set-horizon` wherever a set comes in — a token's, the chat's, a token being made, a chat set being
chosen — so a request holds `set-horizon` whichever name its set was stored with, a new set is
stored as `set-horizon` followed by `override-urgency` until the contract, so that the release
before still grants it after a rollback (`auth.Stored`; built on the recommendation, 2026-10-05, the
owner reviewing the result), and every answer names `set-horizon`
([`principal.go`](../../backend/internal/auth/principal.go), `capabilitiesView` in
[`token.go`](../../backend/internal/api/token.go)); `GET /api/v1/me/token` answers
`override-urgency` after `set-horizon` in the request's set, which a `cowork-mcp` of the release
before reads to describe its tools (`requestCapabilities`); migration 37 lets the checks of
`tokens.capabilities` and `chat_capabilities` take both names and rewrites no row. ~~Not built: the
contract, a later release's — a migration that rewrites every stored set to `set-horizon` and
drops `override-urgency` from both checks, and the old name gone from the API's `Capability` and
from `/me/token`.~~
*(2026-10-06:)* D4's amendment of 2026-10-06 is built, the contract:
[migration 38](../../backend/internal/store/migrations/000038_horizon_names_only.up.sql) rewrites every
`override-urgency` of `tokens.capabilities` and `chat_capabilities` to `set-horizon`, each name once in
the order it was first named; the API's `Capability` has nine values, a set sent with
`override-urgency` is `400`, `GET /api/v1/me/token` answers the request's set as it is, and the code
that wrote the old name is gone (`auth.Stored`, `requestCapabilities`). Both checks still take
`override-urgency`, which release 0.5 writes beside `set-horizon` in every set it stores after an image
rollback; `auth.Canonical` drops it wherever a set is read — a token's, the chat's, every answer —,
which loses nothing, since 0.5 writes it only beside `set-horizon`. Not built: the narrowing, a release
after the one that ships migration 38 — a migration that rewrites the stored sets again, for what a
rollback wrote in between, and then drops `override-urgency` from both checks.

## Context

[ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
made the agent flag something the person sets on the token in a session and the server
therefore trusts. The owner generalised that: if the person sets the flag, the person can
set *what* the flagged token may do — two tokens for two levels of trust instead of one
global rule. Earlier records had already placed several acts beyond any agent, each for a
reason of its own: a question is answered by a person because the entity exists so that a
person decides ([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D2); an agent never deletes ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7) and never books time ([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D6); the prerequisite refusal is overridden by a person ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)
D7); an agent token has no `admin` scope (ADR 0036 D5). Those are about accountability and
irreversibility, not about trust in an agent, and they stay.

## Decision

**D1 — An agent token carries a capability set, chosen by the person at creation,
immutable afterwards.** A different set is a new token. The set never grants more than the
person's role in the tenant ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D6) or the token's scope ([ADR 0035](0035-personal-access-tokens.md) D3).

**D2 — The baseline every agent token has** (with `write` scope): create tickets, replace
the body, comment, create links, ask questions, set progress, register `watch`, and the
transitions `filed → analysed`, `decided → in-progress`, into `blocked` and back.

**D3 — The hard-off list: acts no agent token can be given.** ~~Answering a question~~
*(amended 2026-10-01: recording a person's answer is the `record-answer` capability, ADR
0066 D8; the decision stays the person's)*; deleting, restoring or purging anything; booking
time; overriding the prerequisite refusal on `done`; every administration act — members,
mappings, grants, tokens, ~~projects~~ *(amended 2026-10-01: creating a project and binding a
repository is the `create-project` capability, ADR 0066 D7; archiving, restricting and
deleting projects stay here)*, tenants, time-period locks; `admin` scope. Opening any of
these is an amendment of the record that closed it, not of this one.

**D4 — The selectable capabilities, each on by default:**

| Capability | Grants |
|---|---|
| `decide` | the transition `analysed → decided` |
| `close` | the transition `→ done` (the verification note stays mandatory; open prerequisites still refuse, and the agent cannot override); *(amended 2026-10-03)* both ways to `done` of ADR 0009 D5 — the write that fills the last progress stage and done by hand — and only from `in-progress` or `review`, so that `close` never stands in for `decide`; without it, that write is refused and the stage keeps its value; *(decided 2026-10-04 by the owner)* an open question of the ticket does not hold `close` back — the question stays open on the done ticket |
| `drop` | the transition `→ dropped` with a reason |
| `rank` | moving the rank and adopting the score ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)); *(amended 2026-10-04)* naming a filing's place in its horizon (ADR 0014 D2) |
| ~~`override-urgency`~~ `set-horizon` *(renamed 2026-10-05, [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1: ~~the API takes the old name as the new until a later release drops it; a set stored with it keeps it, which the release before reads, until that release rewrites it — [ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D3~~; amended 2026-10-06: the old name is refused on input and dropped on read; the checks take it until a later release, for an image rollback to the release before)* | a reasoned urgency override ([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3); *(amended 2026-10-04)* the ticket's horizon, which the override now is — set on a ticket with a reason, or named at its filing when it is not `later` |
| `interest` | `need` and `urgent` interest, not only `watch` ([ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D4, which names this capability) |
| `upload` | uploading attachments ([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)) |
| `create-project` *(added 2026-10-01, ADR 0066 D7)* | creating a project and binding a repository, where the person ~~is tenant `admin`~~ *(amended 2026-10-02)* may create projects (ADR 0034 D9), with `write` scope |
| `record-answer` *(added 2026-10-01, ADR 0066 D8)* | recording and updating an answer the person gave, marked as recorded by the agent |

The token page shows the nine switches and two shortcuts: **full** (all on, the default)
and **assisted** (`decide`, `close`, `rank`, `create-project` and `record-answer` off).

**D5 — Enforcement is the API's, on every request marked as an agent's** (ADR 0036 D2, D3).
A refused act answers `403` with the code `agent_forbidden` and the name of the missing
capability or the hard-off rule; the audit row of every agent act records the capabilities
the token had. *(Amended 2026-10-04 for the chat in the UI,
[ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md),
provisionally with it: a browser session's request that the header marks (ADR 0036 D3 as amended)
~~holds every capability, as a plain token's with the header,~~ and meets the hard-off list and the
handlers' agent rules like any agent; its audit row records ~~the full set~~ the set it holds. The
chat is such a session. ~~It may do whatever an agent with every capability may — no person chooses
less for it yet — and before they run it proposes to the person the acts a person owes a reason, a
note or a decision for: a move to `decided`, `done`, `dropped` or `blocked`, a backward move, a
reopen, the withdrawal of a done, the progress write that closes or reopens a ticket,
`finish_work`, `record_answer` and `create_project`, and every write once its conversation has read
a confidential ticket. A proposal is the chat's own gate in front of the API's, not a capability:
the API still decides what the call may do when the person runs it.~~)*
*(Amended again 2026-10-04 by the owner's answers recorded in ADR 0076, and built the same day:)*
**the person chooses the chat's capabilities.** A browser session's request that the header marks
holds the capabilities its person gave the chat — the nine of D4, chosen with `PUT /api/v1/me/chat`
in a browser session only (a token is `403 session_required`, a session the header marks `403
agent_forbidden`: what the chat may do is access, and the chat never widens its own), read with `GET
/api/v1/me/chat` — and a person who never chose holds the default: every capability but `decide`,
`close` and `drop`, which the owner keeps a person's, and `record-answer`, because with nothing
waiting for the person a text the model read could otherwise record an answer in the person's
name. The set is read on every marked request of the session
([`authenticateSession`](../../backend/internal/api/session.go), `auth.DefaultChatCapabilities`),
so a change reaches a running turn at its next call; it lives in `chat_capabilities`, one row per
person that only the person reads and writes
([migration 24](../../backend/internal/store/migrations/000024_chat_capabilities.up.sql)), and a
change is the person's recorded act. The chat proposes nothing: every call runs at once, and the
capabilities and the API's rules are the limit — an act that needs a capability the person did not
give is `403 agent_forbidden` naming it, an answer the model reads. The tool descriptions and the
instructions the chat gives the model name the capabilities it holds, as D6 does for a token.

**D6 — The MCP server reads the token's capabilities at start** (`GET /api/v1/me/token`)
and puts them into its tool descriptions, so the model knows before calling what this token
can do; `finish_work` ([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md))
performs the furthest transition the capabilities allow and reports what remains for a
person.

**D7 — Nothing here changes what a person may do.** A person's acts are bounded by role and
scope alone.

## Consequences

- The owner's agent does everything reversible and attributable by default, and the audit
  marks each act as the agent's; a person who wants a human gate before `decided` or `done`
  unticks two switches.
- The earlier records' agent lines become capability names: ADR 0013 D4's "an agent may only
  `watch`" reads, in place since 2026-10-01, "`need` and `urgent` as well when its token carries
  the `interest` capability". ADR 0014 had no agent rule to change: the acts on the rank are an
  agent's with `rank` because D4 grants them, and ADR 0014 names the capability where it speaks
  of an agent — a filing's place (D2) and the sort by the score (D3).
- Two persons, two trust levels, same product: a client tenant can hand its agent an
  "assisted" token while the owner runs "full".
- D3 keeps five acts human for every installation; a request to open one is a conversation
  about that record, not a switch.

## Alternatives Considered

- **Fixed limits: no `decided`, no `done` for any agent** — the recommendation. Every rule
  and every result signed by a person; the owner judged the gate unnecessary for his own
  work and preferred to choose per token. Lost.
- **A conservative default ("assisted"), full by opt-in.** The same mechanism with the other
  default; the owner's working mode is the default. Lost.
- **A mandatory choice at creation.** Friction at every token for a decision most people make
  once. Lost.
- **Opening the hard-off list too.** Would make an agent able to answer its own questions,
  delete, book time; those records exist for reasons unrelated to agent trust. Not offered.

## Residual risks

- A "full" token does close tickets on its own verification note; the audit shows it as the
  agent's act, and the owner accepted that the second pair of eyes is optional.
- *(Added 2026-10-05.)* Until the contract, every capability set stored since the rename carries
  `override-urgency` beside `set-horizon`, which the release before reads, so that an image rollback
  ([ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4) keeps
  the act for an agent made after the upgrade; the cost is the redundant name in those rows, which
  the contract's rewrite removes. *(Amended 2026-10-06:)* An image rollback to 0.5 over migration 38 is
  safe: the checks still take `override-urgency`, which 0.5 writes beside `set-horizon` into every
  agent token it makes and every chat set it stores, and 0.5 reads everything the migration leaves.
  Once the image goes forward again, this release reads such a set without the old name
  (`auth.Canonical`) and holds `set-horizon` from it. What the window costs: a saved filter 0.5
  stores with the key `urgency` — which 0.5 still takes — loses that condition under this release,
  which reads the filter as if it named no horizon, until the migration of the later release
  rewrites it to `horizon`. Read from the code of 0.5.1, not run.
- Capabilities multiply the test matrix: each switch has an allowed and a refused test in the
  integration tier, with the fixture identities.

## References

- [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) — the flag this generalises
- [ADR 0035](0035-personal-access-tokens.md) D3 — scope never exceeds the person
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D7, [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) D6, [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D7 — the hard-off list's sources
- [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D4, [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D2, D3 — the stakes and the rank, whose agent acts D4's `interest` and `rank` grant
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the tools that read the capabilities
