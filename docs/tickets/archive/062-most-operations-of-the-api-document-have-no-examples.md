---
id: T62
title: most operations of the API document have no examples
state: done
severity: low         # a client reads the shapes from the schemas alone; nothing breaks
security: none
threat:
urgency: later        # rule 4: a decided fix — ADR 0046 D6
effort: M
filed-from: ADR 0046 D6, whose examples stood on a few operations only, the night of 2026-10-06
opened: 2026-10-06
decided: 2026-10-06
done: 2026-10-07
shipped: request and response examples on all 134 operations of the API document — on the schema an answer names, on the operation for a request and for an answer the schema's cannot stand for, three on the shared problem response — held by TestEveryBodyHasAnExample and TestEveryExampleValidates in backend/api/examples_test.go
---

## Current state

Every operation of the API document has its examples
([ADR 0046](../../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D6), the import and
the export of phase 6 included: the 112 operations whose bodies take an example carry one for each,
the 3 that answer bytes — an attachment's download and the two export archives — need none, and the
other 19 have no body but the shared `Problem` response, whose three examples every operation's
errors share. The two uploads, an attachment's and an import's dry run, describe their parts
instead of an example. Where an example is written and the one world the examples share are
[docs/developer/api.md](../../developer/api.md#examples), the rule ADR 0046 D6; two unit tests over
the bundled document, [`backend/api/examples_test.go`](../../../backend/api/examples_test.go), hold
it — `TestEveryBodyHasAnExample` names each body without an example by operation, method, path,
status and media type, `TestEveryExampleValidates` each example that breaks its schema, with
`format: uuid` checked as the server checks it, which the bundler does not.

Verified on 2026-10-07, on the branch that carries phase 6 and phase 7: `make generate-check
test-unit lint cyclo`, `make test-integration` and `make frontend-generate-check` pass; before the
examples, `TestEveryBodyHasAnExample` failed and named 118 bodies of phase 7's document, and an id in
no shape, a stream line that is no JSON, an upload whose description drops a part and an answer
without an example, each put into the bundled document by hand, failed the tests with their place
named. The two reports of `ImportJob` are what `internal/importer` makes of the upload of the
example, the Markdown export and the context what `internal/markdown` writes for `acme/WEB-42`,
byte for byte. The examples change neither generated client; a request body's description, which
an upload carries, reaches the Angular client's comments.

## Required changes

None.

## Not verified

How a viewer of the served document — Swagger UI, Redoc — shows a response whose example stands on
its schema rather than on its media type was not looked at; kin-openapi and both generators read it
as described above.

## Related

- T57 — the import and the export of phase 6, whose operations got their examples here.
- T58 — phase 7, whose child this is.
- T64 — what writing the examples found in the answers of the API.
