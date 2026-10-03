---
name: question
description: Put one open decision on the current cowork ticket to the person as a question. Use whenever a decision belongs to the person rather than to you.
argument-hint: [what to decide]
---

Before asking:

1. Read the ticket with `get_ticket`. If a question on it is still open, put that one to the person first instead of a new one: one question at a time.
2. Research the options against the code and the repository's decision records. Each option must make sense here, with its cost and its consequence named.

Then call `open_question` with:

- `question`: one question, understandable on its own;
- `options`: the context and the options, as Markdown;
- `recommendation`: the option you recommend and the specific reason for it;
- `asked_of`: `me`, unless the person names someone else.

Show the person the question, the options and the recommendation, and wait. Do not build on an unanswered question. When the person answers in chat, write their words down with `record_answer` — never an answer they did not give. A decision that rules the repository belongs in its decision records as well, once answered.
