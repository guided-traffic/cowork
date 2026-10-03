# ADR 0053: Signals and Injectable Services Are the Frontend State; `resource()` Loads, an Entity Cache Keeps Values and ETags, No Store Framework

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "state
management?": Angular signals and services, over NgRx SignalStore, over classic NgRx, and
over a query-cache library. The rules of D4–D7 were put to the owner with the question and
not objected to.

**Partly built** (phase 3, 2026-10-03): D1 with `SessionService`, `ProjectsService`,
`TicketsService`, `EventStreamService` and `ThemeService` (the board, inbox and filter services
arrive with their views), D2 (`EntityCache` keyed by the ticket's canonical key, the `ETag`
derived from the version as [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D2 defines it), D3, D4 (a tenant switch clears the ticket cache), D5 (`ProblemService`; the
`401` redirect arrives with the login), D6, D7 —
[`frontend/src/app/core/`](../../frontend/src/app/core/).

## Context

The frontend is Angular 22, zoneless, signals-first; the API client is generated
([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D3); every overwriting
write needs the entity's `ETag` ([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D6); the boards, the detail page and the lists show the same tickets and must agree after a
write; the inbox counter and the live-update question need a place to live. Angular 20 and
later ship `resource()` and `httpResource()`, which cover loading state, errors and reload
natively; a store framework over that is a dependency in the Angular release train
([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D9) and an abstraction over what the platform does, for roughly ten entity types and no
offline requirement.

## Decision

**D1 — State lives in injectable services, as signals.** One service per domain —
`SessionService` (person, tenants, roles, token capabilities), `TicketsService`,
`BoardService`, `InboxService`, `FiltersService`, `ThemeService` — with `signal` and
`computed` for state, `resource()` / `httpResource()` for loads, `toSignal` where a stream
exists. Components hold only local UI state (an open dialog, a drag in flight).

**D2 — An `EntityCache<T>` is the single source for entities shown in more than one
place.** A map from id to `{ value, etag, loadedAt }`; the detail page, the boards and the
lists read the same entry, a successful write updates it, and the cache hands the `ETag` to
the client for `If-Match` (ADR 0050 D6). Lists hold ids and read values through the cache.

**D3 — No store framework.** Not NgRx, not SignalStore, not a query-cache library. If a
feature outgrows D1 — a complex undo, offline edits — NgRx SignalStore is the amendment,
scoped to that feature.

**D4 — Tenant-scoped state is cleared on tenant switch** ([ADR 0023](0023-the-tenant-is-in-the-path.md)
D4): the caches and lists of one tenant never bleed into another's pages; the
`SessionService` owns the switch and the services react to it.

**D5 — Errors are translated in one place.** A `ProblemService` turns the API's problem
details ([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md)) into a
toast, a form's field errors (`errors[]` by pointer), a `412` merge prompt, or a redirect to
login on `401`; components never parse a problem body.

**D6 — Browser storage holds preferences only:** the theme choice ([ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md)
D3), the last chosen tenant, collapsed panels. No entity data, no token, no session
([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md)).

**D7 — Tests.** Services are tested with `TestBed` and `provideHttpClientTesting`
against the generated client; components against mocked services; `await fixture.whenStable()`
settles signals in the zoneless test bed, as the shell's tests already do.

## Consequences

- Nothing to learn beyond Angular itself; Angular DevTools show the signals; no actions,
  reducers or effects.
- D2 is the one piece of structure that is not free: a small, tested class that every
  service with shared entities uses, and the place the `ETag` discipline is enforced.
- D4 adds a lifecycle hook to every tenant-scoped service; a forgotten one shows another
  tenant's cached card for a moment — the integration of two identities in the Playwright
  tier walks a tenant switch for that reason.
- Live updates ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md))
  arrive as events that name a key; the owning service calls `reload()` on the affected
  `resource()` or refreshes the cache entry; polling on a timer is the fallback the same
  services run when the stream is down.

## Alternatives Considered

- **NgRx SignalStore.** Less boilerplate than classic NgRx, entity features, DevTools; a
  dependency that has lagged Angular majors by weeks, and an abstraction over `resource()`
  for ten entity types. Kept as the amendment of D3.
- **Classic NgRx.** Maximal traceability and time travel; five times the code, zone-era
  patterns in a zoneless app, team conventions for one person. Lost.
- **A query-cache library (TanStack Query for Angular).** Caching, invalidation and polling
  ready-made; an experimental adapter, a second loading abstraction beside `resource()`, its
  own cadence. Lost; D2 and `reload()` are the half that is needed.

## Residual risks

- Discipline replaces structure: a component that starts holding domain state is a review
  finding, not a compile error. The package layout (`core/` for services, `features/` for
  pages) makes the line visible.
- `resource()` and `httpResource()` are young APIs; their shape may move in an Angular
  major, which the Angular Renovate group surfaces with the rest.

## References

- [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D3 — the generated client the services call
- [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D6 — the `ETag` the cache carries
- [ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) — the problem details `ProblemService` translates
- [ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D3 — the one preference in browser storage
- [`frontend/src/app/app.ts`](../../frontend/src/app/app.ts), [`app.spec.ts`](../../frontend/src/app/app.spec.ts) — the pattern already in the shell
