---
id: T24
title: older ADRs still say that one record amends another, against the owner's rule that every amendment is made in place in the record it changes
state: filed
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix (docs/adr/README.md, "Keeping them current")
effort: S
filed-from: the owner's rule in docs/adr/README.md, "Keeping them current"
opened: 2026-10-02
decided:
done:
---

## Current state

[docs/adr/README.md, "Keeping them current"](../adr/README.md#keeping-them-current): no ADR
amends, supersedes, overrides or invalidates another; when an answer touches several records,
each is amended in place and says why. Records written before the rule that carry an
"Amendment to ADR NNNN" section keep it "until a change of its own folds it into the records it
amends" — this ticket is that change. Found with
`grep -n -E "Amendment to ADR|amended by|is amended|are amended|this record amends|amended here" docs/adr/*.md`
(ADR 0009's and ADR 0021 D7's own amendment paths match too and are not targets):

- [ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
  D7 ("Amendment to ADR 0043: `create-project` is a selectable capability") and D8 ("Amendment
  to ADR 0011 D2 and ADR 0043 D3", which also says "ADR 0042 D2 is amended"); its Status ("both
  amend earlier records (D7, D8)") and its References ("amended by D7 and D8"). The rules already
  stand in place in [ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
  D3, D4, [ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
  D2, [ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md) D2 and
  [ADR 0006](../adr/0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
  D3, each citing ADR 0066.
- Status lines in the form "amended … by ADR NNNN": ADR 0042 and ADR 0043.
- ADR 0043 D4's table ("ADR 0013 D4 amended by this"), its Consequences ("Those records are
  amended in place by reference to this one", which also names
  [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) — a record with no agent
  rule to amend) and its References ("amended by D4").
- [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)'s
  Consequences ("are amended to 'pushed, polled as fallback'") and References ("amended by this
  record") about ADR 0020 D4 and ADR 0053; ADR 0053 carries no in-place mark for the line that
  changed.
- [ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
  D7 ("ADR 0011 D6 is amended accordingly") and its References ("the sanitiser this record
  amends"); [ADR 0033](../adr/0033-local-accounts-are-created-by-administrators-never-by-registration.md)
  D3 ("ADR 0032 D3 is amended accordingly") and its References ("amended here");
  [ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)'s
  Status ("ADR 0003 D2's row is amended to point here");
  [ADR 0075](../adr/0075-developer-documentation-lives-in-docs-developer-and-the-general-standard-says-so.md)'s
  References ("the homes table, amended").
- Index rows of docs/adr/README.md that say "amended by ADR NNNN": 0001, 0002, 0006, 0011, 0013,
  0020, 0032, 0042, 0043, 0053.

## Required changes

1. Each amended record states its rule in place, with the date and the reason — the owner's
   answer that changed it — and cites the other record as context only. Where the text already
   stands in place (ADR 0003 D2, 0006 D3, 0011 D2 and D6, 0013 D4, 0020 D4, 0032 D3, 0042 D2,
   0043 D3 and D4), only the "by ADR NNNN" wording of its Status changes; ADR 0053 gets the
   in-place mark of its live-update line.
2. The amending records drop the claim: ADR 0066 D7 and D8 become references to the records that
   now carry the rule, the move marked in place; the sentences and References lines of ADR 0043,
   0054, 0016, 0033, 0056 and 0075 say what the other record now holds, not that this one amends
   it; ADR 0043's claim about ADR 0014 is corrected.
3. The index rows say "D… amended <date>" instead of "amended by ADR NNNN".
4. Verification: the grep above finds no cross-amendment claim, and every record's Status agrees
   with its index row.
