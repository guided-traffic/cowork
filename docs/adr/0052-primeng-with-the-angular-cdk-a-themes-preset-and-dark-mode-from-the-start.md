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

Amended 2026-10-03 (D2: the primary colour and the dark surfaces come from the logo, and the
preset carries the badge accents and the page's layers; D8: the logo; D9: the license). The
check of 2026-10-01 read the peers and missed the license: verified on 2026-10-03 against the
packages, `primeng` 22.0.0 and later, `primeicons` 8 and `@primeuix/themes` 3 are under the
*PrimeUI License* (since 2026-07-15), while `primeng` 21.1.10 is still MIT and requires Angular
21. The owner decided D9 — the free Community License, over Angular Material and over PrimeNG 21
on Angular 21 — and D2's colours and D8's logo, which follow the owner's reference image.

**Partly built** (phase 3, 2026-10-03): D1 (PrimeNG; the CDK is installed, its drag and drop
arrives with the boards), D2, D3, D4, D5, D6 (the budget raised), D7 (the Renovate group), D8
and D9 (the key's paths; the key itself is the owner's to register) — [`frontend/src/app/theme/`](../../frontend/src/app/theme/),
[`frontend/src/app/brand/`](../../frontend/src/app/brand/).

Amended 2026-10-04 with the chat in the UI
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D6): D4, the shell's content-security policy exists, and the production build inlines no critical
CSS for it; D6, the budget as measured against it. ~~**The bundle budget's warning is open:**
`make frontend-build` on 2026-10-04 measured the initial bundle at 1,016.52 kB raw (223.53 kB
estimated transfer) and warns `bundle initial exceeded maximum budget` — `angular.json`'s
`"maximumWarning": "1mb"` warns at 1,000,000 bytes, as the warning's own numbers show, while D6
names 1 MiB (1,048,576 bytes), under which the bundle stays. Neither was changed; which number
holds is open.~~ *(Settled 2026-10-05, built on the recommendation, the owner reviewing the
result:)* `angular.json` follows D6 in bytes — `"maximumWarning": "1048576b"`, `"maximumError":
"1572864b"` —, since `"1mb"` meaning 1,000,000 bytes was the builder's unit and not a decision; the
build warns again once the initial bundle passes 1 MiB.

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
not hard-coded colours. *(Amended 2026-10-03: the primary scale is the logo's violet
`#6e51eb` as 500 with the logo's ink `#160941` as 950; the dark surfaces are a near-black tinted
toward the ink, the light ones Aura's slate. The preset carries the accents of every badge value
— severity, security class and state — as `light-dark()` pairs that pass WCAG AA as text in both
schemes, the brand's gradient, ink and glow, and the page's layers (ground, panel, border).
`@primeuix/themes` 3 resolves its own tokens with `light-dark()` and `color-scheme`, which D3's
class sets.)*

**D3 — Dark mode from the first screen.** `options.darkModeSelector` is a class on `<html>`
(`.app-dark`); the application sets it from a three-way preference — *system*, *light*,
*dark* — stored per person (browser storage, and in the person's settings once those exist),
default *system*, which follows `prefers-color-scheme` and updates live when the OS switches.
Every component, every custom style and every badge colour is defined for both schemes
through the preset's tokens; a style that only works in one scheme is a defect.

**D4 — Icons are PrimeIcons, self-hosted,** installed from the package; the UI makes no
request to an external CDN, which keeps the content-security policy of the security pages
tight. *(Amended 2026-10-04: the policy exists — nginx sends it with the shell, the bundles and
the icons ([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D6): every source `'self'`, and `style-src 'self' 'unsafe-inline'`, because PrimeNG and Angular
write `<style>` elements at run time. For it the production build inlines no critical CSS
(`"inlineCritical": false` in [`angular.json`](../../frontend/angular.json)): the inliner loads
the rest of the stylesheet through an inline `onload` handler, which `script-src 'self'` refuses.
The stylesheet is then a render-blocking file of its own. The PrimeUI license of D9 is checked in
the page, offline, and needs no other source.)*

**D5 — Standalone imports per component;** no library-wide module. Component styles live in
SCSS beside the component and use PrimeNG's CSS variables (`var(--p-…)`), never literal
colours.

**D6 — Density and chrome.** PrimeNG's compact variants where they exist; a work tool, not
a landing page. The bundle budget in `angular.json` is raised to 1 MiB warning, 1.5 MiB
error for the initial bundle once the library is in, and measured against the production
build in CI. *(Amended 2026-10-04: `angular.json` writes `"1mb"` and `"1.5mb"`, which the
builder reads in thousands, not as MiB — the warning fires at 1,000,000 bytes, as its own numbers
show; the initial bundle measured 1,016.52 kB on 2026-10-04 and the warning stands — Status.)*
*(Amended 2026-10-05: `angular.json` writes the two limits in bytes, 1,048,576 and 1,572,864.)*

**D7 — Renovate moves PrimeNG, `@primeuix/themes` and PrimeIcons in the "Angular" group**
([`renovate.json`](../../renovate.json)), so the library's major rises with Angular's in one
change and the peer range is checked by the install.

**D8 — The logo** *(added 2026-10-03)*. The style of the owner's reference: a border in the
gradient from teal over violet, purple, magenta and coral to amber and gold (`#70e6ce`,
`#6e51eb`, `#a63cd8`, `#d233b8`, `#db6a68`, `#e7a666`, `#f2d672`) around the deep ink
`#160941`, a white glyph, a soft violet glow. The mark is a rounded square in that style, the
wordmark the reference's pill with the sparkle and the name. In the UI both are CSS — the
`padding-box`/`border-box` gradient of the reference — not SVG gradients, whose `url(#…)` a
`<base href>` breaks; the favicon set (`favicon.svg`, `favicon.ico`, `apple-touch-icon.png`) is
generated from the mark ([`hack/icons.py`](../../hack/icons.py)) and served revalidated, never
immutable, because its names do not change. The glyph is the **board spark** — three board
columns and the spark that works them — picked by the owner on the design preview on
2026-10-03, over the twin sparkles of the reference and a c with the spark in its opening; the
top bar carries the pill large (42 px in a 68 px bar), at the owner's word.

**D9 — PrimeNG, PrimeIcons and `@primeuix/themes` are used under the PrimeUI Community
License** *(added 2026-10-03)*. The owner registers the key and confirms the eligibility the
license asks for each year. The key never enters the repository — the license forbids
publishing it for others' use: `ng build` and `ng serve` receive it through `--define
PRIMEUI_LICENSE` from the environment variable `PRIMEUI_LICENSE` or the untracked file
`.dev/primeui-license` ([`primeui-define.mjs`](../../frontend/scripts/primeui-define.mjs)); the
image build takes it as the BuildKit secret `primeui_license`, in no layer and no build argument,
and the release workflow passes the repository secret `PRIMEUI_LICENSE`. The key may appear in
the bundle (the vendor's terms). A build without a key works and shows PrimeNG's license notice;
a fork needs a key of its own.

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
- *(Added 2026-10-03.)* The vendor moved the library from MIT to a commercial license with a
  community tier once, in July 2026. If the tier's terms change again, the library question is
  open again; the token discipline of D5 keeps the colours, but every PrimeNG component would
  have to be replaced. The Community License is self-certified: whether the owner qualifies is
  the owner's statement, not something this record verified.
- *(Added 2026-10-03.)* Builds without the key — every fork, the CI frontend job, the scan job's
  image — show the license notice. That is the vendor's mechanism, not a defect.

## References

- [ADR 0018](0018-the-views-of-the-first-release.md) — the views to build
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D9 — the newest-release policy the version alignment serves
- [ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D2, [ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D4 — field errors and paged tables the widgets render
- [`frontend/package.json`](../../frontend/package.json), [`renovate.json`](../../renovate.json) — where the library and its group live
