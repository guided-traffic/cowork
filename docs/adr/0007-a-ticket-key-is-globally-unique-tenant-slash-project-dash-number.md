# ADR 0007: A Ticket Key Is Globally Unique — `<tenant>/<PROJECT>-<number>` — and That Full Form Is Canonical Everywhere

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question "how is
a ticket addressed?": the globally unique form with the tenant in it, as the canonical
spelling everywhere, over the shorter `PROJECT-number` that would have collided across
tenants.

~~**Partly built**~~ **Built** *(whole since 2026-10-06, with D6)* (phase 2, 2026-10-02): D1–D4 — the key grammar
([`ParseTicketKey`](../../backend/internal/domain/ticket.go)), the counter row per project, the
full key in every response, the short form taken where the path fixes the tenant, numbers never
reused. ~~D5's imports arrive with the importer.~~ *(2026-10-06.)* D6 is built with the importer of
[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md): a ticket keeps the number of its file — `id: T<n>`, the name's
`NNN`, an export's key —, the project's sequence advances past the highest number an import brings
(`AdvanceTicketCounter`), and a `T<n>` in a repository's prose is rewritten to the full key of the
ticket the import creates ([ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md) D3). A number a ticket of the
project holds — a deleted one included — or a purged ticket held is a conflict of the dry run, so
D4 holds through an import.

## Context

A ticket's key is what people say, what commits and branches carry, what an LLM session is
handed at start and what an importer has to produce from the `id: T18` of the Markdown
tickets. [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) made
the tenant slug the immutable public identifier of a tenant and allowed a person to belong to
several tenants; [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md)
made the project key the namespace of ticket keys, unique within a tenant. A key without the
tenant is therefore unique only inside one tenant, and a person who works in several would
meet the same `VKO-12` twice — in the person-level lists, in chat, in a commit message read
out of context. The owner chose to pay for global uniqueness in length rather than in
ambiguity.

## Decision

**D1 — The canonical key of a ticket is `<tenant-slug>/<PROJECT-KEY>-<number>`.** Example:
`guided-traffic/VKO-12`. The tenant slug is the one of ADR 0005 D4
(`^[a-z0-9][a-z0-9-]{1,62}$`); the project key is `^[A-Z][A-Z0-9]{1,9}$` — upper case, no
hyphen, so the last `-` always separates the number; the number is a positive integer. The
grammar is unambiguous from left to right: the slash ends the tenant, the last hyphen ends
the project key.

**D2 — The full form is what cowork writes, everywhere.** In the UI, in every API response, in
exports, in notifications, in what the MCP server hands an LLM, in the commit and branch
convention of the workflow plan. A single-tenant installation writes it too: ADR 0005 D6
allows no special mode, and a key copied out of one installation into a chat, an e-mail or
another installation stays unambiguous.

**D3 — The short form `PROJECT-number` is accepted as input only where the tenant is already
fixed by the context** — inside a tenant-scoped API path, inside a tenant's pages, inside a
repository whose binding names the tenant — and is echoed back in full. It is never stored,
never emitted.

**D4 — The number is a per-project sequence that never reuses a value.** Not after a delete,
not after an archive, not after an import. The project key is unique within the tenant, the
tenant slug is unique within the installation, and the pair `(project, number)` is unique;
together the full key is unique within the installation and stable for the life of the
ticket.

**D5 — The key is the human identity; UUIDv7 remains the primary key.** The key is an
indexed, unique triple `(tenant_id, project_id, number)` on the ticket row; foreign keys use
the UUID. Renaming a project key or a tenant slug is not offered (ADR 0005 D4 for the slug; the
project key follows the same rule from this record on).

**D6 — An import keeps the numbers.** A Markdown ticket `id: T18` of a repository bound to
`guided-traffic/VKO` becomes `guided-traffic/VKO-18`, and the project's sequence is advanced
past the highest imported number, so references in the source repository's ADRs stay
correct with a mechanical rewrite.

## Consequences

- Every key is long: `guided-traffic/VKO-12` where `VKO-12` would have done inside the
  tenant. Commit scopes, branch names and chat get longer; the workflow record decides where
  the full form goes in a commit (scope or trailer).
- The grammar of D1 forbids a hyphen in the project key and a slash in the slug — the slug
  rule already does; the project key rule is new and is enforced at creation.
- The API can address a ticket by its full key in one segmentation (`{tenant}/{PROJECT}-{n}`)
  or by its path parts; the API record decides which routes exist. Both are lossless.
- GitHub's `#123` autolink never fires on a cowork key; the `/` and the letters keep it apart
  from issue references.
- The importer needs the project key of the target project before it can compute a single
  key; the import record has to ask for it up front.

## Alternatives Considered

- **`PROJECT-number`, tenant implied by context** — the recommendation. Shorter, GitHub-
  neutral, import-stable; unique only within a tenant, so the person-level lists and any key
  quoted outside its tenant would have needed a tenant column beside it. Lost by the owner's
  decision to make the key itself unambiguous.
- **A tenant-wide sequence `#123`.** Collides with GitHub's issue references in every pull
  request body, says nothing about the undertaking, and forces renumbering on import because
  two repositories both have a `T18`. Lost.
- **UUID only.** Not speakable, not greppable. Lost.

## Residual risks

- Length in commit messages is a cost the owner accepted; if it turns out unbearable the
  workflow record can choose a trailer over a scope without touching this record.
- Nothing is built; D1's grammar and D4's sequence are rules for the schema and the API to
  implement, and the unit tests of the key parser are the first place they will be checked.

## References

- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D4 — the tenant slug
- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) D2 — the project key as namespace
- [docs/tickets/README.md](../tickets/README.md) — the `id: T<n>` an import starts from
