---
name: done
description: Finish the current cowork ticket with a verification note. Use when the work of a ticket is complete and verified.
argument-hint: [key]
---

Finish the ticket `$ARGUMENTS` — or, without one, the session's active ticket — only when the work is verified:

1. Write the verification note from what actually ran in this session: the command or the check, what it ran against, and its result, such as "make test-integration passed against PostgreSQL 18.6". No note, no finish; a claim nobody ran is no note.
2. If the body is behind the work, record the final state with `record_state`.
3. Call `finish_work` with the note. It closes the ticket when this token may, otherwise moves it to review; tell the person what it says remains for them.
4. Do the extraction it asks for in this repository now: a decision into a decision record, behaviour into the developer or operations pages, a gap into the security pages. No file of the repository cites the ticket; commits carry its key.
5. Commit with the strings it gives.
