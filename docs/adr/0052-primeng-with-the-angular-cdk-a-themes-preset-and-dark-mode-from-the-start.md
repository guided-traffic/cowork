# ADR 0052: PrimeNG With the Angular CDK, a `@primeuix/themes` Preset, and Dark Mode From the Start — System Scheme by Default, a Per-Person Override

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"component library?": PrimeNG, over Angular Material (the recommendation), over Tailwind
with headless components, and over Material with Tailwind utilities; the owner's condition:
dark mode is supported. The rules of D3–D7 were put to the owner with the decision and not
objected to.

Verified on 2026-10-01 against the npm registry: `primeng` 22.1.2 declares
`@angular/core ^22.1.0` and `@angular/cdk ^22.1.0` as peers; `@primeuix/themes` 3.0.1;
`primeicons` 8.0.2; `@angular/cdk` 22.2.1. PrimeNG's major has tracked Angular's since
version 19, which answers the lag concern raised against it.

**Not built.** The frontend is a shell without a component library.

## Context

The views of [ADR 0018](0018-the-views-of-the-first-release.md) need two drag-and-drop
boards, tables with page numbers ([ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md)),
forms that show field errors ([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md)
`errors[]`), a five-step slider ([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)),
dialogs, menus, date ranges for the time report and a light and a dark theme. Both
toolchains track the newest release ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D9), so the library must move with Angular's majors. The owner does not find Material's look
appealing; PrimeNG offers a broader widget set — a full data table with paginator, tree,
chips, slider, date picker, dialogs, toasts — with a theming system built on design tokens
and presets.

## Decision

**D1 — PrimeNG is the component library, the Angular CDK the behaviour layer.** PrimeNG
(same major as Angular) for widgets; `@angular/cdk` — a peer PrimeNG requires anyway — for
`DragDrop` (both boards, columns and swimlanes through `cdkDropListGroup`), virtual scroll
on long streams, and the `A11y` utilities. Angular Material is not installed.

**D2 — Theming is `@primeuix/themes` with one preset** (Aura unless the owner picks another
when the first screens exist), configured through `providePrimeNG({ theme: { preset,
options } })`; cowork's palette is a `definePreset` over it with a primary colour and the
semantic colours for severity and security class, so the badges of ADR 0018 D1 are tokens,
not hard-coded colours.

**D3 — Dark mode from the first screen.** `options.darkModeSelector` is a class on `<html>`
(`.app-dark`); the application sets it from a three-way preference — *system*, *light*,
*dark* — stored per person (browser storage, and in the person's settings once those exist),
default *system*, which follows `prefers-color-scheme` and updates live when the OS switches.
Every component, every custom style and every badge colour is defined for both schemes
through the preset's tokens; a style that only works in one scheme is a defect.

**D4 — Icons are PrimeIcons, self-hosted,** installed from the package; the UI makes no
request to an external CDN, which keeps the content-security policy of the security pages
tight.

**D5 — Standalone imports per component;** no library-wide module. Component styles live in
SCSS beside the component and use PrimeNG's CSS variables (`var(--p-…)`), never literal
colours.

**D6 — Density and chrome.** PrimeNG's compact variants where they exist; a work tool, not
a landing page. The bundle budget in `angular.json` is raised to 1 MiB warning, 1.5 MiB
error for the initial bundle once the library is in, and measured against the production
build in CI.

**D7 — Renovate moves PrimeNG, `@primeuix/themes` and PrimeIcons in the "Angular" group**
([`renovate.json`](../../renovate.json)), so the library's major rises with Angular's in one
change and the peer range is checked by the install.

## Consequences

- The broad widget set covers the first release's views without a second library: data
  table with paginator and column filters, tree for the prerequisite view, slider, chips,
  dialogs, toasts, menus, date picker; the boards are CDK with PrimeNG cards.
- Dark mode is not a later feature but a token discipline from the first component; the
  Playwright tier ([ADR 0003](0003-test-and-ci-policy.md)) runs its smoke paths in both
  schemes.
- A third-party library with its own release cadence sits in the critical path of every
  Angular major; the version alignment since v19 and the Renovate group are what makes that
  acceptable, and the status above records the check.
- The `.app-dark` class approach, not a media query alone, is what makes the per-person
  override possible.

## Alternatives Considered

- **Angular Material with the CDK** — the recommendation. First-party, same release train;
  the owner does not want its look, and theming it away from "Material" is work. Lost by the
  owner's decision.
- **Tailwind with headless components.** Full visual control; slider, date picker, table
  paginator, dialogs and accessibility all built by hand, the most expensive route for an XL
  phase 3. Lost.
- **Material plus Tailwind utilities.** Two styling systems, two sources for dark mode. Lost.

## Residual risks

- PrimeNG's theming moved from SCSS themes to tokens at version 18; the current system is
  what this record relies on, and a future change of that scale is a migration the Angular
  group would surface.
- A preset chosen before the first screens is a guess; D2 names Aura as the default and
  leaves the pick to the owner at the first review.

## References

- [ADR 0018](0018-the-views-of-the-first-release.md) — the views to build
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D9 — the newest-release policy the version alignment serves
- [ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D2, [ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D4 — field errors and paged tables the widgets render
- [`frontend/package.json`](../../frontend/package.json), [`renovate.json`](../../renovate.json) — where the library and its group live
