---
id: T79
title: a page on a sibling host of the same site makes the recorded reads in a person's name without a click
state: done
severity: low
security: boundary
threat: a page on another host of the same registrable domain embeds an image whose address is one of the five recorded reads (an attachment, a ticket's Markdown or context, a project's or the tenant's export); the browser sends the SameSite=Lax cookie with a same-site subresource request, so each view records acts in the person's name and makes the server build whole archives
urgency: later         # decided 2026-10-07, waits for the build
effort: S
blocked-by:
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-09
shipped: 0.13.0: a session's request to the five reads that record an act is refused when Sec-Fetch-Site says same-site or cross-site
---

## Current state

The cookie is `SameSite=Lax` (session.go:180-184); same-site subresource requests carry it; `no-store`
keeps nothing cached (api.go:303); the tenant export builds every project per request
(exports.go:105-130). csrf.md:122 says an image from another site does not carry the cookie, true only
across sites.

## Open questions

### Q1: How are the recorded reads held to the installation's own pages?

- **(a) `Sec-Fetch-Site: same-origin` required for a session's request** on the five recorded reads (a browser sends it; a script elsewhere cannot forge it), keeping the inline images of rendered Markdown, which are same-origin.
- **(b) The custom header on every session request**, reads included — breaks inline images (`…/attachments/{id}/content`).
- **(c) Leave it**, named as part of H-22.

Recommended: **(a)** — closes the sibling host without breaking what the page loads itself.

**Answer:** (a) — the owner, 2026-10-07, as made concrete when it was put to him: on the five
recorded reads a session's request with `Sec-Fetch-Site` `same-site` or `cross-site` is refused with
`403`; `same-origin` (the UI and the inline images of rendered Markdown) and `none` (the address bar,
a bookmark) pass, and so does a request without the header (an older browser). It closes the
cross-site link of H-22 as well; a direct link to one of these API addresses from another site stops
working. The amendment of ADR 0026 D5 and H-22's page land with the fix, in the same change.
