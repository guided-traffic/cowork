---
id: T24
title: older ADRs still say that one record amends another, against the owner's rule that every amendment is made in place in the record it changes
state: done
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix (docs/adr/README.md, "Keeping them current")
effort: S
filed-from: the owner's rule in docs/adr/README.md, "Keeping them current"
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-06
shipped: docs/adr — ADR 0066 D7 and D8 point to ADR 0043 D3 and D4, ADR 0011 D2 and ADR 0042 D2, their old text struck; every amended record names in its Status the owner's answer that changed it; ADR 0002 D1, ADR 0053 and ADR 0011's Consequences marked in place; ADR 0016, 0033, 0043, 0054, 0056, 0066 and 0075 say what the other record holds; the index rows say "D… amended <date>"; every record's Status and its index row name the same amendments, ADR 0050 and 0067 in their Status, ADR 0012, 0042, 0052 and 0058 in their rows
---

## Current state

Every rule an older record stated as an amendment of another stands in the record it changes,
whose `Status` names the owner's answer that changed it and cites the other record as context
([docs/adr/README.md, "Keeping them current"](../adr/README.md#keeping-them-current), which
says so):

- [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
  D7 and D8 are struck and point to ADR 0043 D3 and D4, ADR 0011 D2 and ADR 0042 D2. ADR 0011 D2
  carries the rest of D8 — an agent changes only an answer an agent recorded, a person always
  their own, the ticket shows the answer as recorded by the agent — as `mayAnswer` in
  [`api/questions.go`](../../backend/internal/api/questions.go) and the question's line in
  [`ticket-detail.html`](../../frontend/src/app/features/ticket/ticket-detail.html) build it;
  ADR 0011's Consequences mark the line that said the agent records nothing.
- ADR 0002 D1 carries its struck `DEVELOPER.md` and ADR 0053 the mark of its line on live
  updates; the line's wording before 2026-10-01 is in no commit, so nothing old is struck there.
- ADR 0016, 0033, 0043, 0054, 0056, 0066 and 0075 say what the other record holds; ADR 0043 says
  that ADR 0014 had no agent rule to amend and names the capability ADR 0014 D2 and D3 cite.
- Each record whose text beyond its `Status` changed records that as amended 2026-10-06 with "no
  rule changes" — ADR 0002, 0011, 0016, 0033, 0043, 0053, 0054, 0066 and 0075; a record whose
  `Status` alone changed records nothing more.
- Every record's `Status` and its index row name the same amendments: the `Status` of ADR 0050
  records D1 made concrete 2026-10-05 and that of ADR 0067 D2, D4 and D5 amended 2026-10-04, as
  their rows had them; the rows of ADR 0012 (D1 sharpened 2026-10-01), ADR 0042 (D1 amended
  2026-10-04 for `place_ticket`), ADR 0052 (D6's budget settled 2026-10-05, no longer open) and
  ADR 0058 (the Context and D2 amended 2026-10-06) name what their `Status` records.

`grep -n -E "Amendment to ADR|amended by|is amended|are amended|this record amends|amended here" docs/adr/*.md`
on 2026-10-06 finds no cross-amendment claim, only: ADR 0009 line 181 and ADR 0021 line 242, their
own amendment paths; ADR 0042 line 167 ("the tool set is amended") and ADR 0076 line 30 (its own
title), each about itself; ADR 0076 line 315, its list of records amended in place; ADR 0066 lines
115, 126 and 134, the struck text of D7 and D8; and the rule itself on line 38 of the README.

The dates of every record's `Status` compared with its index row, all 76 records — every date the
`Status` names after "amended", "added", "made concrete", "settled", "marked" or "superseded" is in
the row, and every date of the row is in the `Status` — differ only for ADR 0058, whose "amended
2026-10-01 the same day" the row says as "amended the same day". A stricter pattern, only the
dates after "amended" or "marked" on both sides, flags twelve more, read by hand: each says the
same amendment in other words ("D9 added", "and on 2026-10-05", "the same day"). The clauses of a row
were compared with its `Status` for the records this change touched and the ones a pattern
flagged, not for all 76.

Code and pages that cite ADR 0066 D8 for recording an answer (`api/questions.go`, the OpenAPI
document and its generated code, migrations 10 and 27, the `record_answer` tool,
[docs/developer/domain.md](../developer/domain.md)) and ADR 0066 D7 (`api/repositories.go`) still
resolve: D7 and D8 name where the rule stands.

## Required changes

None.
