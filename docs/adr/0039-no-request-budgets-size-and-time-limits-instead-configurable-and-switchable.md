# ADR 0039: No Request Budgets in the First Release — Size and Time Limits Instead, Each Configurable and Switchable Off; the Audit Record and Revocation Are the Response to Abuse

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "rate
limits and abuse?": no request budgets beyond the login limits, size and time limits in
their place, over budgets per token, per tenant, and over a budget shipped switched off. The
owner's condition: every limit is configurable and can be switched off. The inbox collapse
rule of D5 was put to the owner with the question and not objected to; it is configurable
and switchable like the rest. Confirmed by the owner 2026-10-08 for phase 7, whose plan made
per-token limits conditional on the audit record asking for them: it does not, and no per-token
limit is built (D1).

Amended 2026-10-02 (D2: the event stream is exempt from the request timeout, and the timeout
bounds reading the body; D3: how the chart sizes nginx, and that nginx answers its own limits
as problem bodies). An event stream lives
for an hour ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1, D6, D9); a request timeout would cut it every thirty seconds.

**Partly built** (phase 2, 2026-10-02): D1–D4 — the five limits of D2 in
[`config.go`](../../backend/internal/config/config.go) and the request pipeline, ~~nginx sized by
the chart~~ *(since 2026-10-04 the controller's sizes documented and printed by the chart's
notes, D3)*. D5 arrives with the inbox.

**Built** (phase 3, 2026-10-03): D6 with the local login — `COWORK_LOGIN_MAX_FAILURES` and
`COWORK_LOGIN_ADDRESS_LIMIT`, in the chart `auth.local.maxFailures` and `auth.local.addressLimit`.

Amended 2026-10-04 for the chat in the UI
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) D7),
provisionally with that record (D2: a turn's limits; D3: the turn's stream behind nginx), and built
the same day ([`config/chat.go`](../../backend/internal/config/chat.go),
[`api/chat.go`](../../backend/internal/api/chat.go)). Amended again on 2026-10-04 by the owner's
answers recorded in ADR 0076 (D2: no decisions in a turn's body; the person's stop of their turns, on
the replica that answers it), built the same day. Amended 2026-10-05 with the answer on the tenant's
attachment quota recorded in
[ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D6 — enforce, over report only — built on the recommendation, the owner reviewing the result (D2:
the quota is a limit of the table, `0` for none, and the one whose default is `0`). Amended on 2026-10-04 by the owner's decision on
the routing recorded in
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3
(D3: the frontend proxies nothing, so the proxy sized above the backend's limits is the Ingress
controller, whose limits the installation sets and the chart documents; built the same day). Amended 2026-10-06 with the import of
[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D7 (D2: its
variable joins the table; D3: the controller's body limit covers its upload), built the same day
([`config.go`](../../backend/internal/config/config.go), `limitBody` in
[`validate.go`](../../backend/internal/api/validate.go), the chart's `cowork.ingressBodySize`). Fixed,
not configured: at most 10,000 files an upload (`importer.MaxFiles`) and one import at a time per
replica, a dry run or an execution (`importSlot` in
[`imports.go`](../../backend/internal/api/imports.go)); both bound what one request holds in memory,
neither is a budget, and D1 stands. Amended 2026-10-09 (D2: the body limit is the operation's, as
the API document declares its body, and a body of a type the operation does not declare is `415`
before it is read).

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
| `COWORK_ATTACHMENT_TENANT_QUOTA` *(added 2026-10-05)* | `0`, none (ADR 0016 D6) | `409 attachment_quota` before bytes are stored |
| `COWORK_MAX_IMPORT_BYTES` *(added 2026-10-06)* | `50MiB` ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D7) | `413` — an import's upload whose body, or whose files together, are larger; the dry run stores nothing |
| `COWORK_REQUEST_TIMEOUT` | `30s` | the handler's context is cancelled; `504` with a JSON error |
| `COWORK_MAX_PAGE_SIZE` | `200` | a larger `limit` is clamped, not refused |
| `COWORK_MAX_QUERY_LENGTH` | `256` | a longer search query answers `400` |

A value of `0` disables that limit; the operations page says that a disabled body limit lets
one request hold unbounded memory and that `0` belongs in no production values file. *(Amended
2026-10-09, made concrete by the fix of the security review of 2026-10-07: which of the body limits
holds a request is the operation's, as the API document declares its body — the attachment's or
the import's for an operation that takes a multipart upload, `COWORK_MAX_JSON_BODY` for every other
—, never the request's `Content-Type`; a body of a type the operation does not declare is `415`
before it is read ([`api/validate.go`](../../backend/internal/api/validate.go) `limitBody`).)* *(Added
2026-10-05: the tenant's attachment quota is no bound on what one request costs but on what a
tenant keeps, and its default is `0` — no quota — because no figure suits every installation and an
upgrade must not start refusing uploads; an installation of several tenants sets it, which the
operations page says.)*
*(Added 2026-10-02:)* the event stream of ADR 0054 is exempt from `COWORK_REQUEST_TIMEOUT`; its
heartbeat bounds an idle stream instead. The timeout is a context deadline that rolls the
transaction back, never a buffering handler, and a read deadline on the request body, lifted
once the body is read: a body that trickles in fails at the deadline. *(Added 2026-10-04: a turn of
the chat is exempt as well — the request timeout bounds reading its body, and the turn has limits
of its own, each a variable and each disabled by `0`: `COWORK_CHAT_TURN_TIMEOUT` (`5m`; past it the
turn's stream ends with the `error` event `timeout`), `COWORK_CHAT_MAX_STEPS` (`8` calls of the model
a turn; the turn then ends and a new message goes on) and `COWORK_CHAT_TURNS_PER_PERSON` (`2` turns
of one person at once on one replica; one more is `429 chat_busy`). A turn's body is held to
`COWORK_MAX_JSON_BODY` like any. Fixed, not configured: the conversation's shape in the API document
— at most 400 messages, a text of at most 100,000 characters, 32 tool calls a message, ~~32
decisions~~ —, a tool's answer clipped to 16,000 characters for the model and 2,000 for the person,
a comment after ten seconds of silence, and the gateway's bounds on a provider: two minutes to
begin an answer, ninety seconds of silence, a line of 1 MiB, a stream of 32 MiB, a body of 8 MiB,
an answer's text of 256 KiB, a call's arguments of 64 KiB, 64 calls an answer, 4096 tokens an
answer. The count of turns is a bound on what runs at once, not a budget: D1 stands.)* *(Amended
2026-10-04 by the owner's answers on the chat, ADR 0076: the decisions are gone with the proposals;
a person stops their running turns at once with `DELETE …/chat/turns`, which ends them on the
replica that answers it, as the count of turns is that replica's.)*

**D3 — ~~The frontend proxy is sized above the backend's limits.~~ The Ingress controller is
sized above the backend's limits** *(amended 2026-10-04 by the owner, ADR 0001 D3; the rule it
replaces is struck through below)*. The controller stands in front of the backend, and the chart
does not know which controller it is, so its limits are the installation's to set in the
controller's own configuration, and the chart documents them (the `ingress` comment of
[`values.yaml`](../../deploy/helm/cowork/values.yaml),
[docs/operations/installation.md](../operations/installation.md#expose-it)) and prints them in its
notes for the values given: a body limit of at least ~~the larger of the JSON and the attachment
maximum~~ the largest of the JSON, the attachment and the import maximum *(amended 2026-10-06 with
the import's upload, [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D7)*, rounded up to MiB, plus one MiB (~~`11m`~~ `51m` with the defaults; none when any of them is
`0`), and a
read timeout of at least the request timeout plus ten seconds (`40` seconds; an hour when it is
`0`), so a limit is the backend's problem body with its request id. What the controller answers
itself — its own `413` above its limit, its `502` or `503` without a ready backend pod, its `504`
past its timeout — is its page, not a problem body
([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D6 as amended). The two
streams need no setting of their own on an nginx-based controller: the backend answers both with
`X-Accel-Buffering: no`, which nginx obeys, and each sends something at least every twenty seconds,
which keeps it inside any read timeout above that.

~~nginx's
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
an hour's read timeout. *(Added 2026-10-04: a turn of the chat has no location of its own; it
passes `/api/` with the read timeout above, unbuffered because the backend answers it with
`X-Accel-Buffering: no`, and kept within the read timeout by its comment every ten seconds.)*~~

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
defaults stay as that record set them. *(Built 2026-10-03: `COWORK_LOGIN_MAX_FAILURES`, default
5, and `COWORK_LOGIN_ADDRESS_LIMIT`, default 20, each disabled by `0`; the window of fifteen
minutes and the address minute are fixed, not configured.)*

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
- ~~`frontend/nginx/default.conf.template` — where D3's sizes land~~ *(until 2026-10-04)*;
  [`deploy/helm/cowork/values.yaml`](../../deploy/helm/cowork/values.yaml) (`ingress`) and
  [`templates/NOTES.txt`](../../deploy/helm/cowork/templates/NOTES.txt) — where D3's sizes are
  documented and printed
