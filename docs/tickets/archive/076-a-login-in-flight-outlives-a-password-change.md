---
id: T76
title: a login that read the old password hash commits its session after the password change that was to end every other session
state: done
severity: low
security: boundary
threat: a holder of the old password keeps logins in flight while the person changes the password or the operator rotates the local administrator's Secret, and keeps a session the change was meant to end
urgency: later         # rule 4: a known fix; a narrow window
effort: XS
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: a login makes no session when the password changed after it was verified
---

## Current state

`LookupLogin` reads the hash outside any lock (login.go:154, 163); `RecordLoginAttempt` and
`CreateSession` do not check it again (store/login.go:177-210; store/sessions.go:114-132); the change
(me.go:438-442) and the synchronisation's `setPassword` (bootstrap.go:229-245) take no lock the login
takes.

## Required changes

1. In the transaction that makes the session, `SELECT password_hash … FOR SHARE` from `local_accounts`
   and refuse when it differs from the hash verified; a test that interleaves a login and a change.
