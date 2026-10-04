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
`TestAnAgentSessionIsRefusedWhatOnlyASessionDoes`.

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
request becomes an agent's with every capability
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4's full set), held to the hard-off list and every agent rule, and its acts record the person as
the actor, the mark, the capability set and no token. A malformed header is refused there as well.
The chat in the UI marks every tool call it makes with the person's session so —
`chat/<model>/<conversation>`, and `chat/<model>/<conversation>+confirmed` for a call the person ran
from the chat's proposal — and the session itself is the person's in every request without the
header. Like every value of the header, `+confirmed` is the client's word: the record keeps it,
nothing verifies it ([docs/security/chat.md](../security/chat.md#h-39) H-39).)*

**D4 — A flagged token without a header is recorded as `unknown-agent`.** The record never
has a hole where the agent should be.

**D5 — An agent token's scope is at most `write`.** `admin` scope is refused at creation for
a flagged token: administration — members, mappings, deletion, locks — is a person's work.

**D6 — The timeline and the comments show the agent.** An act reads "Hans (via Claude Code)"
with the model on hover; a comment written through a flagged token carries the agent mark
([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3, D4). An
`unknown-agent` shows as "via an agent".

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
  token.)*
- The MCP server (its own record) sends the header on every request as a matter of course,
  with the model and a session id it generates.

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

## References

- [ADR 0035](0035-personal-access-tokens.md) — the token, its scopes, its owner
- [ADR 0004](0004-cowork-is-a-team-product.md) D1 — the agent acts for a person
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 — `actor_user_id`, `agent`, `token_id`
- [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3, D4 — the agent mark on comments
- [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) D6 — the role an agent inherits
- [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) D2 — the chat, whose tool calls the header marks on a session
