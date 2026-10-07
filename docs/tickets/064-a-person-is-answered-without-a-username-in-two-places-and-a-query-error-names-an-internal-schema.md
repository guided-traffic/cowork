---
id: T64
title: a person is answered without a username in two places and a query error names an internal schema
state: filed
severity: low         # an API client reads a person without a name and a message with a URL; nothing breaks
security: none
threat:
urgency: now          # rule 1: a measured-false statement in tracked files — Person.username of the API document
effort: S
filed-from: writing the examples of the API document (T62), 2026-10-06
opened: 2026-10-07
decided:
done:
---

## Current state

Writing the examples of the API document found three answers that say less, or other, than the
document promises; the examples show what the handlers answer.

1. **The person who set a ticket's horizon is answered by the id alone.** `horizonSetView`
   ([`backend/internal/api/tickets.go:98`](../../backend/internal/api/tickets.go)) writes
   `horizon_set.by` as `{id, username: null, display_name: ""}`: the ticket row carries
   `urgency_override_by` and neither the setter's username nor their name. For a local account the
   document's `Person.username` promises the username ("null for a person of the identity
   provider"), and `display_name`, a required field, is answered empty for everyone. Every answer
   that carries a ticket carries it — a ticket, the lists, the person-level lists. The tests read the
   id only ([`api_horizon_test.go:40`](../../backend/test/integration/api_horizon_test.go), `:110`,
   `:162`; [`transitions_test.go:381`](../../backend/internal/api/transitions_test.go)).
2. **A new link names its maker without a username.** `LinkTickets` answers `201` with
   `created_by` built from the principal, `Username: nullableString(nil)`
   ([`backend/internal/api/links.go:175`](../../backend/internal/api/links.go)), so a local
   account's username is `null`; the `200` of the same link made again, and the list, read the person
   and answer the username. The test reads the id only
   ([`api_links_test.go:63`](../../backend/test/integration/api_links_test.go)).
3. **A query parameter's error message names the validator's internal schema.** A parameter that
   breaks its schema is an `errors[]` entry whose message is `reasonOf(reqErr)`
   ([`backend/internal/api/validate.go:199`](../../backend/internal/api/validate.go), `reasonOf` at `:270`): the
   reason the JSON Schema 2020-12 validator writes for a 3.1 document, `jsonschema validation failed
   with 'https://example.com/schema.json#'\n- at '': minimum: got 0, want 1` for `limit=0` — the
   resource every schema is compiled under (kin-openapi v0.149.0,
   `openapi3/schema_jsonschema_validator.go:43`), a newline and an empty location before the failure.
   Read on 2026-10-07 for a minimum (`limit=0`), an enum (`per_page=7`) and a pattern (`project=web`
   on the tenant's time list). A body's failure reads clean: `schemaFields` and `locateReason`
   take the failure out of the validator's text.

Neither the UI nor `cowork-mcp` reads `horizon_set.by` or a link's `created_by` (read 2026-10-07):
who meets the first two is a client of the API that shows them, and the third every client that
shows a parameter's error.

**Why no security class.** The URL in the third is a constant of the validator library, the same
in every installation and for every request; it names no host, path, file, setting or secret of
cowork, and the rest of the message is the failure of the value the client sent itself. No
principal learns anything from it: it is the quality of a message, not a disclosure.

## Required changes

1. **`horizon_set.by` names the person**, as `reporter` does: the queries that read a ticket row
   read the setter's username and display name beside `urgency_override_by`, and `horizonSetView`
   builds the person with `personView`. Test: in
   [`api_horizon_test.go`](../../backend/test/integration/api_horizon_test.go), a local account and a
   person of the identity provider each set a horizon, and the ticket, the project's list and
   `GET /api/v1/me/next` answer `horizon_set.by` as `GET /api/v1/me` names that person.
2. **A new link names its maker as the list does**: the `201` of `linkTickets` reads the person as
   the `200` does. Test: in [`api_links_test.go`](../../backend/test/integration/api_links_test.go),
   a local account links two tickets, and the `201`, the `200` of the same link made again and the
   list answer the same `created_by`, the username included.
3. **A parameter's message is its failure alone**: the validator's resource and location are taken
   out of the reason, as `locateReason` takes them out of a body's, so `limit=0` answers `minimum: got
   0, want 1` at `query:limit`. Test: a unit test in `internal/api` over the document handler, as
   `TestATurnIsHeldToTheDocument` validates a turn: for a minimum, an enum and a pattern, no message
   carries `example.com` or a newline.
4. **The examples follow**: the `Ticket` example of `components/schemas.yaml` names Ada Lovelace in
   `horizon_set.by`, the `Problem` response may show a parameter's failure, and
   [docs/developer/api.md](../developer/api.md#examples) drops its note on `horizon_set.by`.

## Related

- T62 — the examples of the API document, which found these.
