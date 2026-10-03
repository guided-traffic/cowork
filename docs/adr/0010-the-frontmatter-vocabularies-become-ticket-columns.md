# ADR 0010: The Frontmatter Vocabularies Become Ticket Columns, and Urgency Is Derived With a Reasoned, Expiring Override

## Status

Accepted, amended 2026-10-02 (D3: the rule set v1 over the facts cowork holds, and the agent
sentence that ADR 0043 D4 replaced) and 2026-10-03 (D3: an override holds until it is
withdrawn or replaced, and a person may leave its reason out). Date: 2026-09-29. Decided by
the owner as the answer to the catalog question "which frontmatter fields become
first-class?": all of them, with the vocabularies the ticket rules already define. The
amendment of D3 is the owner's answer of 2026-10-02 to the question which rules the
derivation can evaluate: three of the five rows of the tickets page read facts cowork does not
hold (a branch, tracked files, whether a trigger is live, whether a fix is cheap), and no row
is a default. The owner chose a rule set over the facts cowork holds with `later` as its
default, over new fact columns and over urgency set by the person.

The amendment of 2026-10-03 is the owner's answer to two questions put when the backlog was
grouped by urgency and the board given a column per state for the `now` tickets
([ADR 0018](0018-the-views-of-the-first-release.md) D1). What does a block do to an override?
Rule set v1 derives no blocked ticket to `now`, so an override that ended on every block left
the board's `blocked` column empty and sent an unblocked ticket back to `later`; the owner
chose an override that holds, over a board that shows `in-progress` and `blocked` tickets
whatever their urgency, and over keeping the expiry. Does a drag between the urgency groups
need a reason? The owner chose a reason a person may leave out and an agent must give, over a
required reason and over a generated one.

**Built** (phase 2, 2026-10-02): D1–D3 — the columns and enums (migration 8), the threat rule
as a CHECK and in the API, rule set v1 ([`DeriveUrgency`](../../backend/internal/domain/ticket.go)),
the override and its end. D4's `found-in` link exists; D5 arrives with the importer. The
amendment of 2026-10-03 is not built yet: the override still ends on an input change and
still requires a reason.

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
~~and it **expires** — when an input of the derivation changes, the rule runs again, the
override is dropped, and the timeline says so.~~ *(Amended 2026-10-03:)* and it **holds until
a person or an agent withdraws it or sets another** — when an input of the derivation changes,
the rule runs again and the derived value beside the override changes, and the override
stays. *(Amended 2026-10-03:)* The reason is required of an agent and optional for a person:
a drag between the backlog's urgency groups is a person's daily planning move
([ADR 0018](0018-the-views-of-the-first-release.md) D1), and the act records who moved the
ticket from which value to which either way. ~~An agent may not override.~~ *(Amended
2026-10-02: an agent overrides with the `override-urgency` capability of
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4, which replaced this sentence on 2026-10-01 without marking it here.)*

*(Added 2026-10-02.)* **Rule set v1**, first match, top down; the stored rule names the
version, so a later rule set can tell its rows apart:

| Rule | Urgency | Stored rule |
|---|---|---|
| The ticket is `blocked` with the kind `release` ("is gated on it", [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D2) | `release` | `v1:release-block` |
| The ticket is `blocked` with the kind `decision`, `human` or `product` | `icebox` | `v1:icebox-block` |
| An open ticket of type `decision` `blocks` it | `icebox` | `v1:icebox-decision` |
| None of the above | `later` | `v1:default` |

`now`, `next` and "gates the release" are reached through the ~~reasoned~~ override only: the
facts the tickets page's rows read for them — a branch, tracked files, a live trigger, a
release ticket — are not columns of a ticket, and "gates the release" needs a way to know
which ticket is the release that no record gives. The inputs of v1 are the ticket's state,
its block kind, and the state and type of the tickets that `block` it. *(Made concrete
2026-10-02: "an input of the derivation changes" is a change of what v1 tells apart — whether
the ticket is blocked, the block kind while it is, whether an open `decision` blocks it; a
transition from `filed` to `analysed` changes none of them and leaves an override standing.
When one changes, the derivation runs in the same transaction, ~~the override ends with an
`overridden` act on that ticket — the reason "an input of the urgency derivation changed",
attributed to the act that changed the input —~~ and a value re-derived from a link or another
ticket's change leaves the ticket's version alone.)* *(Amended 2026-10-03: the override stays
when an input changes; only the derived value and its rule change.)*

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
- ~~D3's expiry means an override never silently outlives the facts it was made under; it
  also means a person may have to restate it after a fact changes. That is the point.~~
  *(Amended 2026-10-03:)* An override outlives a block and a link change on purpose. Every
  row of rule set v1 that is not its default says that the ticket waits — on a release, on a
  decision, on a person, on a product call — not that it matters less; the derived value
  beside the override keeps saying so, and the board keeps a blocked `now` ticket in its
  `blocked` column, where waiting is meant to be seen
  ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)). A stale
  override does not hide: every `now` and `release` ticket stays on the board.
- *(Added 2026-10-03.)* An override set by a person without a reason says only who set it and
  when; the reason is worth writing where the call is not obvious from the ticket.

## Alternatives Considered

- **A minimal core (severity, effort) and the rest as labels or body text.** Loses the
  cross-tenant filters, the urgency derivation and the machine-readable security class the
  confidentiality mechanism needs. Lost.
- **Urgency computed only, no override.** Cleaner, but the rule table has a row that is a
  human judgement ("needs a product call") and facts change outside the ticket; without an
  override, severity would be bent to move urgency. Lost.
- **A free-text `blocked-by` column.** Already placed by ADR 0009 D2 as the reason of the
  `blocked` transition; not a column of the ticket.
- **An override that expires on every input change** — the rule until 2026-10-03. Since rule
  set v1 derives no blocked ticket to `now`, it emptied the board's `blocked` column and sent
  every unblocked ticket back to `later`, to be dragged forward again. Lost.
- **A board that shows `in-progress` and `blocked` tickets whatever their urgency**, with the
  expiry kept. No change to the model, but a blocked `now` ticket would still leave its backlog
  group and come back from a block as `later` while in progress. Lost.
- **A reason required of a person on every drag between the urgency groups**, and **a reason
  generated from the drag** ("moved from later to next"). The first turns daily planning into
  a form; the second fills the field with words that say nothing the act does not already
  record. Lost.
- **A separate planning field beside `urgency`.** Two fields for one judgement, and urgency
  set by the person under another name. Not offered.

## Residual risks

- The derivation reads links ("gated on a release", "blocked by a decision"); until the
  links record exists, the rule set is implemented over the columns alone and says which rows
  it cannot yet evaluate.
- The vocabularies were written for one project's tickets; a second tenant may want a
  `severity` scale of its own. Not offered; if it comes up, it is a question, not a setting.
- *(Added 2026-10-03.)* A later rule set that derives importance from facts — a live security
  finding as `now` — would be outranked by a standing override set before that fact. Whether
  an override then yields to such a row is a question for the amendment that brings the row.

## References

- [docs/tickets/README.md](../tickets/README.md) — the vocabularies, the security scale and the urgency rule table
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) — the fields that became transitions
- [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) — the types the columns apply to
