---
id: T78
title: a JSON body sent as multipart/form-data skips the API document's validation and the JSON body limit
state: done
severity: medium
security: boundary
threat: any client, anonymous on POST /auth/local and on the webhook, sends a JSON body with Content-Type multipart/form-data; the request is not validated against the API document and its bound becomes the attachment maximum plus 64 KiB (about 10 MiB by default, unbounded when that is 0), read whole by the webhook before its signature check; a login's password and a change's current password are then held to no length
urgency: next          # rule 3: severity medium, live
effort: S
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: the body limit and the validation follow what the operation declares, and an undeclared content type is 415 before the body is read
---

## Current state

`limitBody` and `ExcludeRequestBody` decide "multipart" from the request's header, not from the
operation (validate.go:43-53, 104); the strict handler decodes JSON whatever the type (apigen
api.gen.go:58686); kin-openapi skips the body then (openapi3filter/validate_request.go:93, v0.149.0).

## Required changes

1. Take the multipart limit and the validation's exclusion only for operations that declare multipart —
   GitHub's webhook among those that do not: its handler reads the whole body and computes the HMAC
   before it answers 401 (github.go:90-100), so without a secret anybody makes it read ten megabytes;
   refuse an undeclared content type with `415` before reading; tests for the login, the webhook and one
   JSON write with a multipart header.
