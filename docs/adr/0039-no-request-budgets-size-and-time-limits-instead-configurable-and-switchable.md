# ADR 0039: No Request Budgets in the First Release — Size and Time Limits Instead, Each Configurable and Switchable Off; the Audit Record and Revocation Are the Response to Abuse

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "rate
limits and abuse?": no request budgets beyond the login limits, size and time limits in
their place, over budgets per token, per tenant, and over a budget shipped switched off. The
owner's condition: every limit is configurable and can be switched off. The inbox collapse
rule of D5 was put to the owner with the question and not objected to; it is configurable
and switchable like the rest.

Amended 2026-10-02 (D2: the event stream is exempt from the request timeout, and the timeout
bounds reading the body; D3: how the chart sizes nginx, and that nginx answers its own limits
as problem bodies). An event stream lives
for an hour ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1, D6, D9); a request timeout would cut it every thirty seconds.

**Partly built** (phase 2, 2026-10-02): D1–D4 — the five limits of D2 in
[`config.go`](../../backend/internal/config/config.go) and the request pipeline, nginx sized by
the chart. D5 arrives with the inbox, D6 with the local login.

## Context

Login attempts are already limited ([ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md)
D6). After login, the realistic abuse is not an attacker but an agent in a loop: a session
that comments fifty times a second or files tickets until a quota stops it. A request budget
guesses numbers for that case before it has happened and trips legitimate work — a bulk
import, a busy session — in the meantime. What protects a process regardless of intent is a
bound on what one request may cost: its size and its duration. Every act carries its token
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1), and a token
is revoked in one act ([ADR 0035](0035-personal-access-tokens.md) D6); that is the response
to a loop once it is seen.

## Decision

**D1 — No request budget per token, session or tenant in the first release.** Reads and
writes are not counted. A budget is an amendment with numbers taken from the audit record,
not guessed.

**D2 — Limits on what one request may cost, each a configuration value, each disabled by
`0`:**

| Variable | Default | Effect when exceeded |
|---|---|---|
| `COWORK_MAX_JSON_BODY` | `1MiB` | `413` with a JSON error |
| `COWORK_ATTACHMENT_MAX_BYTES` | `10MiB` ([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D6) | `413` before bytes are stored |
| `COWORK_REQUEST_TIMEOUT` | `30s` | the handler's context is cancelled; `504` with a JSON error |
| `COWORK_MAX_PAGE_SIZE` | `200` | a larger `limit` is clamped, not refused |
| `COWORK_MAX_QUERY_LENGTH` | `256` | a longer search query answers `400` |

A value of `0` disables that limit; the operations page says that a disabled body limit lets
one request hold unbounded memory and that `0` belongs in no production values file.
*(Added 2026-10-02:)* the event stream of ADR 0054 is exempt from `COWORK_REQUEST_TIMEOUT`; its
heartbeat bounds an idle stream instead. The timeout is a context deadline that rolls the
transaction back, never a buffering handler, and a read deadline on the request body, lifted
once the body is read: a body that trickles in fails at the deadline.

**D3 — The frontend proxy is sized above the backend's limits.** nginx's
`client_max_body_size` is set from the attachment maximum plus headroom and its proxy
timeouts above `COWORK_REQUEST_TIMEOUT`, so a limit is always the backend's JSON error and
never nginx's default page. The chart renders both from the same values. *(Made concrete
2026-10-02:)* the image substitutes `NGINX_CLIENT_MAX_BODY_SIZE` and `NGINX_PROXY_READ_TIMEOUT`
(image defaults `11m` and `40s`); the chart sets the body size to the larger of the JSON and
the attachment maximum, rounded up to MiB, plus one MiB, and the read timeout to the request
timeout plus ten seconds; a backend `0` becomes no body limit and an hour. A body above
nginx's own limit, a backend nginx cannot reach and a backend that does not answer in time
get static problem bodies from nginx — without `instance` and `request_id`
([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D6); the backend's
own errors pass through, never intercepted. The event stream's location has no buffering and
an hour's read timeout.

**D4 — Abuse is answered by the record and by revocation.** The per-token view of the audit
record ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D6)
shows what a token did and how fast; a tenant administrator revokes a token or ends a
person's sessions; nothing is automatic.

**D5 — The inbox collapses bursts, configurably.** More than `COWORK_INBOX_COLLAPSE_THRESHOLD`
(default 20; `0` disables) events on one ticket by one actor within
`COWORK_INBOX_COLLAPSE_WINDOW` (default five minutes) become one inbox line — "23 changes by
Hans (via Claude Code)" — so a loop does not drown the watchers before anyone sees it
([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)).

**D6 — The login limits of ADR 0033 D6 become configurable the same way:** attempts per
account and window, attempts per source address and minute, each disabled by `0`; the
defaults stay as that record set them.

## Consequences

- Nothing to calibrate blind; no `429` for legitimate sessions; a single request cannot
  exhaust the backend by size or duration unless an operator switched the limit off.
- A loop is visible in the audit record and the inbox collapse within minutes and ended by
  one revocation; it is not prevented in advance. The owner accepts that for the first
  release.
- Seven configuration values more, all with defaults; the README's table grows in the change
  that builds them, and the chart's values carry them.
- D2's `0` semantics are a sharp tool; the chart's default values file never sets one, and
  `helm lint` with the `ci/` values keeps it that way.

## Alternatives Considered

- **A write budget per token and session** (for instance 600 per minute, `429` with
  `Retry-After`). Stops a loop before damage; guesses numbers, trips bulk imports, and per
  replica without shared state it is a net, not a guarantee. Deferred to an amendment with
  measured numbers.
- **Budgets per tenant.** Fairness between clients on one instance; multi-tenant instances
  are the minority ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
  D6) and no instance has the problem. Lost.
- **A budget shipped switched off** (`COWORK_WRITE_BUDGET_PER_MINUTE`, default `0`). The
  reaction time of a budget without a deploy; dead code by default. Lost; the amendment is
  an hour's work when the audit asks for it.

## Residual risks

- D1 by name: a loop runs until seen. The collapse of D5 and the per-token audit view are
  what make "seen" quick.
- D2's `0` in a production values file removes a protection silently; the operations page and
  the values comments say so, nothing enforces it.

## References

- [ADR 0033](0033-local-accounts-are-created-by-administrators-never-by-registration.md) D6 — the login limits, made configurable here
- [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D6 — the attachment maximum
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D6, [ADR 0035](0035-personal-access-tokens.md) D6 — the per-token record and revocation
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) — the inbox the collapse protects
- [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template) — where D3's sizes land
