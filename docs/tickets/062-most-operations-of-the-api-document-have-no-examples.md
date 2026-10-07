---
id: T62
title: most operations of the API document have no examples
state: in-progress    # filed -> analysed -> decided -> in-progress -> done | dropped
severity: low         # a client reads the shapes from the schemas alone; nothing breaks
security: none
threat:
urgency: later        # rule 4: a decided fix — the rule and its test exist, the last item waits on T57
effort: XS
blocked-by: T57
filed-from: ADR 0046 D6, whose examples stood on a few operations only, the night of 2026-10-06
opened: 2026-10-06
decided: 2026-10-06
done:
---

## Current state

Every operation of the API document has its request and response examples
([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D6): the 109 of the
129 operations that have a body of their own carry an example for each body — but the bytes of an
attachment's download, which have none, and its upload, which describes its parts instead —, and
every operation's errors share the three examples of the shared `Problem` response. Where an example is written — a
response's on the schema it names in `components/schemas.yaml`, a request's on the operation, an
answer the schema's example cannot stand for on the operation — and the one world they share are
[docs/developer/api.md](../developer/api.md#examples). Two unit tests over the bundled document,
[`backend/api/examples_test.go`](../../backend/api/examples_test.go), hold it:
`TestEveryBodyHasAnExample` names each body without an example by operation, method, path, status
and media type, and `TestEveryExampleValidates` names each example that breaks its schema.

The import and the export of phase 6 (T57) add operations on a branch of their own, which is not
integrated with this one. Their bodies have no examples there; once both branches meet,
`TestEveryBodyHasAnExample` names each of them and `make test-unit` fails until they have one.

## Required changes

1. **The examples of phase 6's import and export**, when T57's branch is integrated: each request
   body an example on its operation; each answer a schema of `components/schemas.yaml` that carries
   an `example` — a new schema gets one, a new list an anchor to its entity's — or an example of its
   own where the schema's cannot stand for what the operation answers; an upload of the import
   names its parts in its description if it has no example. Then `make generate` and, in
   `backend/`, `go test ./api/` until both tests pass. This is the ticket's last item: nothing else
   is left to extract, since ADR 0046 and api.md state the rule.

## Not verified

How a viewer of the served document — Swagger UI, Redoc — shows a response whose example stands on
its schema rather than on its media type was not looked at. What was verified: kin-openapi
validates every example, and neither generator writes one into its output (`make generate-check`,
`make frontend-generate-check`).

## Related

- T57 — the import and the export of phase 6, whose operations need the examples.
- T58 — phase 7, whose child this is.
