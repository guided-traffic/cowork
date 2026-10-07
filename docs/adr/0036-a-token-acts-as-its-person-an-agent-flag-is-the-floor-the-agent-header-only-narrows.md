# ADR 0036: A Token Acts as Its Person; an Agent Flag on the Token Is the Floor, and the Agent Header Refines the Record and Only Ever Narrows

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "whom
does a token act as, and how is an agent marked?": the person is the actor, an agent flag set
at token creation is what the server trusts, an `X-Cowork-Agent` header refines the audit
record and may mark a request as an agent's but never un-mark one — over the flag alone,
over the header alone, and over service accounts. The additional rules of D5–D8 were put to
the owner with the question and explicitly confirmed.

**Partly built** (phase 2, 2026-10-02): D1–D5 and D7 — the token's person as the actor, the
flag and the header that only narrows ([`principal.go`](../../backend/internal/auth/principal.go)),
`unknown-agent`, the `write` ceiling as a CHECK on `tokens`, and the agent restrictions on
every marked request. D6's timeline arrives with the UI; D8 needs nothing.

Amended 2026-10-04 for the chat in the UI
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D2), provisionally with that record (D3: the header narrows a browser session's request too; D7:
a session the header marks is refused what only a session does), and built the same day —
[`api/session.go`](../../backend/internal/api/session.go) `authenticateSession`, `sessionRules` in
[`api/api.go`](../../backend/internal/api/api.go); `TestTheAgentHeaderOnASession`,
`TestAnAgentSessionIsRefusedWhatOnlyASessionDoes`. Amended again on 2026-10-04 by the owner's
answers recorded in ADR 0076, and built the same day (D3: a session's request the header marks holds
the capabilities its person chose for the chat,
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5, not every capability; the chat's `+confirmed` mark is gone with its proposals).

Amended 2026-10-04 by the owner's rule "it must always be distinguishable whether a change to a
ticket was made by the user themselves or by an agent with the user's token, so a model or agent
can never act unnoticed in the user's name; such a change always carries a small robot or agent
icon", and the owner's choice that **every act made through a token is marked** — with its agent
mark where the request is an agent's, otherwise as made through the token, by its name — while a
plain token keeps its person's authority (D6; D1 stands). Before it, a plain token whose requests
carried no header acted unmarked as its person in every view of a ticket; only the tenant's audit
view told it, by the row's `token_id`. Built the same day: the token's id and name on every
row that shows an act on a ticket
([migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql),
`actToken` and `tokenMarkView` in [`api/tickets.go`](../../backend/internal/api/tickets.go),
`tokenName` in [`store/tx.go`](../../backend/internal/store/tx.go)), `token` on the API's acts, and
the mark in the UI ([`shared/agent-mark.ts`](../../frontend/src/app/shared/agent-mark.ts));
`TestEveryActThroughATokenIsMarkedWithIt`. With it D6 is built: the activity, the comments, the
questions and the files show the agent and the token, the time entries the token. Amended again the
same day by the owner, by the same rule, and built the same day: the filing of a ticket beside its
reporter and a stake beside its holder carry the agent mark and the token too ([migration 28](../../backend/internal/store/migrations/000028_filing_and_stake_marks.up.sql)), the context
document of [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
and the session start's summary of the MCP server name a person's act through a token, and the
tenant's audit view names the token beside its id.

Made concrete 2026-10-06 (Consequences: the model of the MCP server's mark is the one Claude Code
names to the `SessionStart` hook,
[ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)
D5 as amended), and built the same day. Made concrete again the same day with ADR 0067 D5's
`PostModelSwitch` hook, on the recommendation of an open question the owner has not answered
(Consequences: after a switch, the one Claude Code names to that hook), and built the same day.

## Context

[ADR 0004](0004-cowork-is-a-team-product.md) D1 made the agent act through a token bound to
a person; [ADR 0035](0035-personal-access-tokens.md) D5 ruled out tokens without a human
owner. What remained was how a request is known to be an agent's — because the agent rules
(the next record) forbid an agent certain acts, and a forbiddance the agent can switch off by
omitting a header is no rule. The server can trust only what the person set in a session; the
header is useful for the record (which model, which session) and must not be able to widen
anything.

## Decision

**D1 — The actor of every act through a token is the token's person.** The audit row's
`actor_user_id` is the person ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D1); the timeline names the person.

**D2 — The agent flag is set at token creation, by the person, and is immutable.** A flagged
token is an agent for every request it makes, with or without a header. A token that should
stop being an agent's is revoked and replaced.

**D3 — The header `X-Cowork-Agent: <name>/<model>/<session>` refines the record and only
narrows.** On a flagged token it fills the audit row's `agent` field; on an unflagged token
it marks that request as an agent's act (a script choosing to be bound by the agent rules).
No header, and no header value, makes a flagged token's request a person's. The header is
validated — three slash-separated parts, each one to sixty-four printable characters — and a
malformed header is refused with `400`, never silently ignored. *(Amended 2026-10-04: the header
is read on a browser session's request too, and narrows it as it narrows an unflagged token's: the
request becomes an agent's with ~~every capability
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4's full set)~~ *(amended again 2026-10-04)* the capabilities its person chose for the chat in the
UI, the default where the person chose none
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5), held to the hard-off list and every agent rule, and its acts record the person as the actor,
the mark, the capability set and no token. A malformed header is refused there as well. The chat
in the UI marks every tool call it makes with the person's session so — `chat/<model>/<conversation>`~~,
and `chat/<model>/<conversation>+confirmed` for a call the person ran from the chat's proposal~~ —
and the session itself is the person's in every request without the header. Like every value of
the header, the mark is the client's word: the record keeps it, nothing verifies it
([docs/security/chat.md](../security/chat.md#h-39) H-39). A person who sends the header on a
request of their own session is held to the same set: the header narrows, never widens.)*

**D4 — A flagged token without a header is recorded as `unknown-agent`.** The record never
has a hole where the agent should be.

**D5 — An agent token's scope is at most `write`.** `admin` scope is refused at creation for
a flagged token: administration — members, mappings, deletion, locks — is a person's work.

**D6 — ~~The timeline and the comments show the agent.~~ Every act made through a token is shown
as made through it, every act of an agent shows the agent, and only the person's own browser
session acts unmarked.** *(Amended 2026-10-04 by the owner.)* ~~An act reads "Hans (via Claude
Code)" with the model on hover; a comment written through a flagged token carries the agent mark
([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3, D4). An
`unknown-agent` shows as "via an agent".~~ Beside the agent mark, the token an act came through is
recorded with the act — its id, and its name as the token has it, copied when the act is written,
so that a reader who may not read the token's row
([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D6) reads the name and
a revoked or expired token's acts keep it — on the audit row
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1), on a comment and
each edit of it (ADR 0015 D3), on a file
([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D1), on a question as it was asked and its answer as it was recorded
([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2), and on a time
entry and each correction of it
([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D6), which no agent makes and the activity leaves out, *(amended again 2026-10-04 by the owner)* on
the ticket as it was filed, beside its reporter (`reporter_agent`, `reporter_token`), and on a stake
as it was last set, beside its holder
([ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D1). Never the token's
secret, its hash or a part of either. The API answers each such act with
`token`, `{id, name}`, and `null` for a browser session — a question with `asked_by_token` and
`answered_by_token`, a ticket with `reporter_token` beside `reporter_agent`. The UI marks the act with the agent icon: where the request was an agent's,
the agent's name, with the whole mark and the token in the tooltip; where a token made it and no
agent, `token <name>` and "Done through the token <name> in this person's name"; for the person's
own browser session, nothing. An `unknown-agent` shows as "unknown agent". Whoever reads the act
reads the token's name — a member reading a ticket, the names of another person's tokens its acts
came through; the owner accepts that, and the token page and its creation form say so. *(Amended
again 2026-10-04:)* What names an act's person for an agent — the context document
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D2) and the summary `session_start` writes — names a person's act through a token as well, `through
the token <name>`, where it names an agent's act `via <agent>`; the tenant's audit view shows the
token's name beside its id ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D6). The mark is attribution, not authority: a plain token keeps its person's authority (D1) and
meets the agent rules only where the header marks its request (D3, D7).

**D7 — The agent restrictions apply to every request marked by D2 or D3.** What they are is
the next record's; this record settles that they attach to the marking, not to the person.
*(Amended 2026-10-04: a session's request the header marks is refused, with `403 agent_forbidden`,
every operation only a session may call — creating a token, changing a password, giving access,
a turn of the chat ([ADR 0035](0035-personal-access-tokens.md) D5) — after the CSRF check and
before anything else: what only a session does is a person's act, never an agent's.)*

**D8 — No service accounts.** A non-human owner (a CI job, a bot of its own) is not provided;
when one is needed it is a record of its own with its own accountability rule, not an
unflagged token in someone's name.

## Consequences

- A person who both scripts and runs an agent holds two tokens: one plain, one flagged. The
  token page says which is which.
- The agent cannot escalate by omission: forgetting the header costs it the model name in the
  record, nothing else.
- The audit record distinguishes four cases cleanly: person in a session, person through a
  plain token, agent through a flagged token with a header, agent without a header. *(Amended
  2026-10-04: and a fifth, an agent in the person's session — the chat — with the mark and no
  token.)* *(Amended 2026-10-04 by the owner: every view of an act on a ticket tells the five
  apart as well, D6.)*
- A token's name is no longer its person's alone: whoever reads an act made through it reads the
  name (D6). A token is named for what it does, not with what must not be read.
- The MCP server (its own record) sends the header on every request as a matter of course,
  with the model and a session id it generates. *(Made concrete 2026-10-06: the model is the one
  Claude Code names to the `SessionStart` hook in the server's project directory
  ([ADR 0067](0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md)
  D5 as amended) — MCP does not tell a server its model —, and `unknown` while none is recorded;
  the session id is the server's own. Made concrete again the same day: after a switch, the model
  is the one Claude Code names to the `PostModelSwitch` hook in that directory.)*

## Alternatives Considered

- **The flag alone.** Trustworthy; a script that wants to bind itself to the agent rules
  without a flagged token could not. Lost to D3's narrowing header.
- **The header alone.** One token for everything; the agent decides whether it is an agent,
  and an omitted header makes Claude a person with `decided` and `done`. Lost.
- **Service accounts.** Clean separation in the member list; nobody is accountable for the
  account, and attribution ends at the bot. Lost to D8.

## Residual risks

- D3's "only narrows" is a rule the resolver enforces; a test asserts that no header value
  removes the flag's effect.
- D4's `unknown-agent` rows are poorer records; the MCP server's always-on header keeps them
  rare.
- D6 marks what is written from migrations 27 and 28 on. An audit row written before names its
  token's id and not its name, and the activity shows such an act as made through a token it
  cannot name; a comment, file, question, time entry, filing or stake written before carries no
  mark, and an act of a plain token there reads as its person's — the activity and the tenant's
  audit view, filtered by token, still tell it, except for time, which the activity leaves out.
  Nothing is backfilled: the audit record is not rewritten
  ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D3).
- A link's creator and an urgency override's setter are answered by the API without a mark of
  their own, an agent's as much as a token's; no view of the UI shows them, and their acts are
  marked in the activity ([docs/security/tokens.md](../security/tokens.md#h-50) H-50).
- An image rolled back to the release before migration 27
  ([ADR 0028](0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4) writes
  no mark on these rows, and an answer or a stake it changes keeps the mark the earlier write
  recorded.

## References

- [ADR 0035](0035-personal-access-tokens.md) — the token, its scopes, its owner
- [ADR 0004](0004-cowork-is-a-team-product.md) D1 — the agent acts for a person
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 — `actor_user_id`, `agent`, `token_id`
- [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3, D4 — the agent mark on comments
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D6 — the role an agent inherits
- [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) D2 — the chat, whose tool calls the header marks on a session
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D2, [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D1, [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) D6 — the rows D6 marks besides the audit row and the comments
- [migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql), [migration 28](../../backend/internal/store/migrations/000028_filing_and_stake_marks.up.sql), [`api/tickets.go`](../../backend/internal/api/tickets.go) `actAgent`, `actToken`, `tokenMarkView`, [`markdown/context.go`](../../backend/internal/markdown/context.go) `via`, [`tools/start.go`](../../backend/internal/tools/start.go) `actLine`, [`shared/agent-mark.ts`](../../frontend/src/app/shared/agent-mark.ts) — D6 as built
