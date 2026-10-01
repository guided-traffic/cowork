# ADR 0010: The Frontmatter Vocabularies Become Ticket Columns, and Urgency Is Derived With a Reasoned, Expiring Override

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question "which
frontmatter fields become first-class?": all of them, with the vocabularies the ticket rules
already define.

**Not built.** No `tickets` table exists.

## Context

The Markdown tickets carry a frontmatter whose fields are not metadata but method: a
severity scale defined as "impact if never fixed", a security class defined by threat model
rather than by gut feeling, an urgency derived from a rule table, an effort size. Earlier
records placed the state and the transition data ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)).
What remained was whether the rest stays machine-readable — filterable across every tenant a
person belongs to, derivable, importable one to one — or becomes labels and prose.

## Decision

**D1 — Six columns with fixed vocabularies, on every ticket regardless of type:**

| Column | Values | Meaning |
|---|---|---|
| `severity` | `critical`, `high`, `medium`, `low`, `cosmetic` | impact if never fixed — for a `feature`, the impact of never building it |
| `security` | `live`, `boundary`, `hardening`, `none` | the threat-model class of the tickets page |
| `threat` | text | required when `security` is not `none`; names the guarantee or principal, verb and target |
| `urgency` | `now`, `release`, `next`, `later`, `icebox` | derived (D3) |
| `effort` | `XS`, `S`, `M`, `L` | size, not time |
| `opened_at` | timestamp | creation, or the imported `opened:` date |

**D2 — `threat` is validated with `security`.** A ticket with `security` other than `none`
and an empty `threat` is refused; a ticket with `security: none` carries no `threat`.

**D3 — `urgency` is derived and may be overridden with a reason.** The derivation is the rule
table of the tickets page (first match, top down), applied over the ticket's own columns and
its links; the matched rule is stored with the value. A person may override the value with a
reason; the override is a recorded act with its actor, it is shown beside the derived value,
and it **expires** — when an input of the derivation changes, the rule runs again, the
override is dropped, and the timeline says so. An agent may not override.

**D4 — Two fields dissolve into other records.** `filed-from` becomes a `found-in` link when
it names a ticket, and a free-text note otherwise; `publication-accepted` becomes part of the
confidentiality mechanism that replaces the file-name embargo.

**D5 — The importer maps one to one** and refuses a value outside the vocabulary rather than
guessing; the report names the field and the ticket.

## Consequences

- Filters and sorts across every tenant a person belongs to work on these columns: every
  `security: live` finding, every `urgency: now`, every `effort: XS` for a quiet hour.
- A new value in a vocabulary is a migration and a UI change, not a setting — intended, as
  with the types.
- The board can colour by severity and badge by security class; the backlog's computed
  priority (its own record) has severity and urgency as inputs.
- D3's expiry means an override never silently outlives the facts it was made under; it
  also means a person may have to restate it after a fact changes. That is the point.

## Alternatives Considered

- **A minimal core (severity, effort) and the rest as labels or body text.** Loses the
  cross-tenant filters, the urgency derivation and the machine-readable security class the
  confidentiality mechanism needs. Lost.
- **Urgency computed only, no override.** Cleaner, but the rule table has a row that is a
  human judgement ("needs a product call") and facts change outside the ticket; without an
  override, severity would be bent to move urgency. Lost.
- **A free-text `blocked-by` column.** Already placed by ADR 0009 D2 as the reason of the
  `blocked` transition; not a column of the ticket.

## Residual risks

- The derivation reads links ("gated on a release", "blocked by a decision"); until the
  links record exists, the rule set is implemented over the columns alone and says which rows
  it cannot yet evaluate.
- The vocabularies were written for one project's tickets; a second tenant may want a
  `severity` scale of its own. Not offered; if it comes up, it is a question, not a setting.

## References

- [docs/tickets/README.md](../tickets/README.md) — the vocabularies, the security scale and the urgency rule table
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) — the fields that became transitions
- [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) — the types the columns apply to
