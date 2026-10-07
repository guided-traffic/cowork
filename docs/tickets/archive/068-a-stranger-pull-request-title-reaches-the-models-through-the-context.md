---
id: T68
title: the GitHub webhook links a pull request of any author, so a stranger's title reaches the models that read the ticket's context
state: done
severity: medium
security: boundary
threat: any GitHub user who can open a pull request in a public repository a tenant binds (fork pull requests are delivered) writes up to 500 characters of title into a ticket's context document, which Claude Code sessions (get_ticket, the session start) and the chat's provider read — text that steers an agent acting with the person's capabilities
urgency: now           # rule 1: fixed in the change that found it
effort: S
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-07
shipped: the webhook links only pull requests whose author_association is OWNER, MEMBER or COLLABORATOR (0.12.0)
---

## Current state

backend/internal/github/payload.go reads no `author_association`; a title ending in `(APP-12)` or a key
line in the body links it (github/keys.go:48-53) on `opened` and `edited` (payload.go:31); keys are
public through commit subjects (ADR 0068). The context document writes the pull requests' titles
(markdown/context.go:116-118, 129-146).

## Required changes

1. Link only pull requests whose `author_association` is `OWNER`, `MEMBER` or `COLLABORATOR`; a delivery
   of another author is `202` and changes nothing; tests with a fork's pull request. Recorded in ADR
   0071 as made concrete, open to the owner's objection, with the question for the owner in T60 once
   the fix is released (whether outside authors' pull requests should be linked without their titles).
2. docs/security/github-webhook.md, chat.md (H-38's writers), agent-client.md (H-34) name the rule.
