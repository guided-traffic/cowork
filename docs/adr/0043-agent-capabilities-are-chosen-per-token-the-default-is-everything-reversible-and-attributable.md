# ADR 0043: Agent Capabilities Are Chosen per Token, the Default Is Everything That Is Reversible and Attributable, and a Short Hard-Off List Stays

## Status

Accepted, amended 2026-10-01 by [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D7 and D8 (`create-project` and `record-answer` become selectable capabilities; creating a
project and recording a person's answer leave the hard-off list), amended 2026-10-02 (D4:
`create-project` follows the tenant setting of
[ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D9 instead of the `admin` role), amended 2026-10-03 (D4: `close` covers both ways to `done`
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
on a move in the rank since 2026-10-03 and on adopting the score with the score,
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
and built the same day.

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
| `rank` | moving the rank and adopting the score ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)) |
| `override-urgency` | a reasoned urgency override ([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3) |
| `interest` | `need` and `urgent` interest, not only `watch` ([ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D4 amended by this) |
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
holds every capability, as a plain token's with the header, and meets the hard-off list and the
handlers' agent rules like any agent; its audit row records the full set. The chat is such a
session: it may do whatever an agent with every capability may — no person chooses less for it yet
— and before they run it proposes to the person the acts a person owes a reason, a note or a
decision for: a move to `decided`, `done`, `dropped` or `blocked`, a backward move, a reopen, the
withdrawal of a done, the progress write that closes or reopens a ticket, `finish_work`,
`record_answer` and `create_project`, and every write once its conversation has read a confidential
ticket. A proposal is the chat's own gate in front of the API's, not a capability: the API still
decides what the call may do when the person runs it.)*

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
  `watch`" is now "unless the token has `interest`"; ADR 0014's rank rule likewise through
  `rank`. Those records are amended in place by reference to this one.
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
- Capabilities multiply the test matrix: each switch has an allowed and a refused test in the
  integration tier, with the fixture identities.

## References

- [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) — the flag this generalises
- [ADR 0035](0035-personal-access-tokens.md) D3 — scope never exceeds the person
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D7, [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) D6, [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D7 — the hard-off list's sources
- [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D4, [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) — amended by D4
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the tools that read the capabilities
