---
id: T75
title: the per-address login throttle can be outrun by parallel requests, and the password change is not throttled where the lockout is off
state: done
severity: medium
security: boundary
threat: an anonymous client sends POST /auth/local in parallel bursts; every request of a burst reads the same count before any attempt row exists, so about a thousand guesses fit in one request timeout instead of COWORK_LOGIN_ADDRESS_LIMIT a minute — password spraying across usernames without tripping the per-user lock; and the holder of a stolen session guesses the current password through PUT /api/v1/me/password at hash speed where COWORK_LOGIN_MAX_FAILURES is 0
urgency: next          # rule 3: severity medium, live
effort: S
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-09
done: 2026-10-09
shipped: 0.13.0: a password attempt is counted against its address under a lock before it is hashed, and the password change takes the address throttle
---

## Current state

`throttled` reads the count in a read-only transaction (login.go:150, 209; store/login.go:108-116);
the attempt row is inserted after the Argon2 computation (store/login.go:232/247/261); nothing
serializes per address. `ChangeMyPassword` never calls `throttled` (me.go:409-433), and with the lockout
off nothing locks (store/login.go:265-267).

## Required changes

1. Reserve the attempt before hashing: count and insert under an advisory lock on the address hash in one
   transaction, then record the outcome on that row; a test with parallel logins from one address.
2. The password change takes the address throttle, or a per-account cap independent of the lockout switch.
