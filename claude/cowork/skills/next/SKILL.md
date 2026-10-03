---
name: next
description: Pick the next cowork ticket to work on in this repository. Use when the person asks what to do next or which ticket to take.
---

Refresh where the session stands with the cowork `session_start` tool, then:

1. A ticket of the person in progress: name it — key, title, state, open questions — and ask whether to go on with it.
2. None: show the candidates it lists, top first, with key, title, state and effort, and let the person choose. Do not choose for them.
3. On the person's choice: read it with `get_ticket`. If it is unassigned, assign it to the person through `api` — `GET /api/v1/me` for their id, then `PATCH` the ticket with `{"assignee": "<id>"}` and the `If-Match` of its version — and move it forward with `transition` until it is `in-progress`. A refusal such as `agent_forbidden` means that step is the person's: say so and stop.
4. Every commit of the work carries the strings `get_ticket` gives.
