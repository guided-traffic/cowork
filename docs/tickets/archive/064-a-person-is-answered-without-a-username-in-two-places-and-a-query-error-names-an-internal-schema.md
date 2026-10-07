---
id: T64
title: a person is answered without a username in two places and a query error names an internal schema
state: done
severity: low         # an API client reads a person without a name and a message with a URL; nothing breaks
security: none
threat:
urgency: now          # rule 1: a measured-false statement in tracked files — Person.username of the API document
effort: S
filed-from: writing the examples of the API document (T62), 2026-10-06
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: horizon_set.by and a new link's created_by name the person by username and display name as the reporter and the list of links do, and a parameter's errors[] message is its failure alone — held by TestTheHorizonSetNamesThePersonWhoSetIt, TestANewLinkNamesItsMakerAsTheListDoes, TestValidationRefusesWhatTheDocumentDoesNotDescribe and TestAParametersMessageIsItsFailureAlone
---

## Current state

Every answer names a person as the API document's `Person` promises, and a parameter's failure
reads like a body's ([ADR 0047](../../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D2):

1. **`horizon_set.by` names the person who set the horizon** as `reporter` names the reporter:
   `GetTicketByNumber`, `GetWrittenTicket` and the list builder join `users` for
   `urgency_override_by` beside the reporter and the assignee, under the same policy of `users` — a
   person who is no longer a member of the tenant has the id alone, as a reporter has —, and
   `horizonSetView` builds the person with `personView`
   ([`tickets.go`](../../../backend/internal/api/tickets.go)). A local account is answered with its
   username and display name, a person of the identity provider with the display name and `username:
   null`: on the write that sets the horizon, the ticket, the lists and the person-level lists. No
   migration.
2. **A new link names its maker as the list does**: `LinkTickets` reads the link it made back with
   `GetLink`, the query the `200` of the same link answers from
   ([`links.go`](../../../backend/internal/api/links.go)).
3. **A parameter's message is its failure alone**: `parameterError` in
   [`validate.go`](../../../backend/internal/api/validate.go) reads a parameter's failures as
   `bodyErrors` reads a body's — one entry for the parameter as before, at `query:<name>` or
   `header:<name>`, the failures of a repeated one's values joined by `; ` in its message — so
   `limit=0` answers `minimum: got 0, want 1` at `query:limit`, and no message carries the
   validator's resource or a newline.
4. **The examples follow**: the `Ticket` example names Ada Lovelace in `horizon_set.by`, the shared
   `Problem` response shows a parameter's failure as `invalidParameter`, and
   [docs/developer/api.md](../../developer/api.md) names the rule in the pipeline's validation step
   and no longer notes the id alone.

Verified on 2026-10-07, on the branch of the import and the export: without the fixes,
`TestTheHorizonSetNamesThePersonWhoSetIt`
([`api_horizon_test.go`](../../../backend/test/integration/api_horizon_test.go)) failed at every
place — the answer of the write, the ticket, the project's list, next for me — on an empty display
name and a null username, `TestANewLinkNamesItsMakerAsTheListDoes`
([`api_links_test.go`](../../../backend/test/integration/api_links_test.go)) at the `201` alone,
`TestValidationRefusesWhatTheDocumentDoesNotDescribe`
([`api_core_test.go`](../../../backend/test/integration/api_core_test.go)) and
`TestAParametersMessageIsItsFailureAlone`
([`validate_test.go`](../../../backend/internal/api/validate_test.go): a minimum, an enum, a
pattern, a repeated parameter and the two parameters of the `invalidParameter` example) on the
validator's text, and `TestTicketViewAnswersTheHorizon` on the person's names; with them, `make
generate-check test-unit lint cyclo gosec`, `make test-integration` and `make
frontend-generate-check frontend-test` pass. A header the document declares, an `Idempotency-Key`
that is no UUID, is answered by its failure alone at `header:Idempotency-Key`, read once by hand.
Neither generated client changes; the bundled document changes in its examples only.

## Required changes

None.

## Not verified

The cost of the third join of `users` on a ticket's read and on a page of a list was not measured;
it is a lookup by primary key per row, as the reporter's and the assignee's are. No test holds the
message of a header parameter.

## Related

- T62 — the examples of the API document, which found these.
