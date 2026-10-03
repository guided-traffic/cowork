# ADR 0055: The UI Is English Only, Dates and Numbers Follow the Browser Locale, Times Are UTC in the API, and i18n Is Not Configured

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"languages?": English only with i18n unconfigured, over English with i18n markers, over
German and English at runtime, and over English now with runtime i18n as the named
amendment path. The rules of D3–D4 were put to the owner with the question and not objected
to.

**Partly built.** The shell's few strings are English literals; no locale configuration. *(Phase
2, 2026-10-02:)* D3 and D4 hold for the API — timestamps in RFC 3339 UTC, the vocabularies spelt
as the API spells them, the Markdown export's dates as UTC dates.

## Context

Code, documentation and configuration of this repository are English by rule
([ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
D11), and so are the vocabularies a person sees in the UI — `in-progress`, `boundary`,
`icebox` — which the API, the export and the import keep in English
([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md), [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)).
No tenant has asked for another language. Angular's build-time i18n produces one bundle per
locale, which the nginx container would have to serve under per-locale paths — at odds with
the single-origin, path-routed shape of [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
and [ADR 0023](0023-the-tenant-is-in-the-path.md) D4. Marking every string for a translation
nobody needs is discipline without a user.

## Decision

**D1 — The UI is English, and only English.** Strings are literals in templates; no
`i18n` attributes, no `$localize`, no `ng extract-i18n` in the build.

**D2 — Dates, times and numbers follow the browser.** `LOCALE_ID` is taken from
`navigator.language` at start and registered with Angular's locale data on demand; `DatePipe`
and `DecimalPipe` render with it; the browser's time zone is used for display. No date
format is hard-coded.

**D3 — The API speaks UTC.** Every timestamp is RFC 3339 with `Z`; the frontend converts
for display and sends UTC back. The export's frontmatter dates are dates (`2026-10-01`), not
timestamps, as the Markdown tickets have them.

**D4 — Vocabulary values are shown as the API spells them,** with a tooltip that explains
the value; they are not translated, so what a person sees is what the export, the import,
the filters and the MCP tools say.

**D5 — No amendment path is pre-chosen.** Should a tenant require another language, the
choice between build-time and runtime i18n is made then, against the shape of the frontend
at that time.

## Consequences

- Nothing to configure, extract or maintain; one bundle, one path family.
- A later localisation means adding markers to every template — a mechanical, tool-assisted
  task, and the price of not doing it now.
- A German user sees `01.10.2026` and `1.234,5` from D2 while reading English labels; that
  is the intended mix.

## Alternatives Considered

- **English with i18n markers from the start.** Later a translation file instead of
  template edits; marking every string now for no reader, and Angular's build-time path
  would still produce a bundle per locale. Lost.
- **German and English at runtime.** A German UI for the owner; a translation file from day
  one, a dependency, and English vocabularies that are either translated against the API or
  shown mixed. Lost.
- **English now, runtime i18n named as the amendment path** — the recommendation. The owner
  chose not to pre-decide the later mechanism. Lost in that one respect; D5 says so.

## Residual risks

- D2's locale data is loaded for the browser's language; an unusual locale falls back to
  `en-US` formatting with a console note, never an error.

## References

- [ADR 0002](0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md) D11 — English everywhere
- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md), [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) — the vocabularies and the export dates
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3, [ADR 0023](0023-the-tenant-is-in-the-path.md) D4 — the shape build-time i18n would have fought
