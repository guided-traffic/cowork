---
id: T45
title: the login page follows a return path with a control character to another site after a local login
state: done
severity: medium
security: boundary
threat: a person who can send a cowork link to a member makes the browser leave cowork for a page of their choice right after the member typed a correct password, as a phishing step ("session expired, sign in again")
urgency: next         # rule 3: severity medium, trigger live since 0.2.0, fixed in 0.3.0
effort: XS
blocked-by:
filed-from: the phase-4 frontend review of 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done: 2026-10-04
shipped: 0.3.0 — the login page takes only a path without control characters and backslashes, at most 2048 bytes, as its way back, the rule of the backend
---

## Current state

`safeReturn` in [`login.ts`](../../frontend/src/app/features/auth/login.ts) (released in 0.2.0)
accepts any value that starts with `/` and not with `//` or `/\`. A browser drops tab, newline and
carriage return from a URL before parsing it, so `/login?return=%2F%09%2Fevil.example%2Flogin`
yields the return path `/\t/evil.example/login`, which `window.location.assign` turns into
`https://evil.example/login` after a correct local login (verified with Node's WHATWG URL parser
for `%09`, `%0A`, `%0D` and `%09%5C`). The provider login is not affected: the backend's
`safeReturnTo` refuses control characters and backslashes.

## Required changes

1. `safeReturn` follows the backend's rule: no character below 0x20, no 0x7f, no backslash,
   at most 2048 characters; anything else is `/`.
2. `login.spec.ts` cases for `%09`, `%0A`, `%0D`, `%09%5C` and a backslash — failing before the fix.
3. On the merge of the fixing pull request: `state: done`, `shipped:`, then the file is renamed
   without the `local_` prefix and archived (the embargo ends with the fix).
