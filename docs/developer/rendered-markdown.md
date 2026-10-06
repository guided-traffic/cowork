# Rendered Markdown

How the Markdown people and agents write — a ticket's body, a comment, a question's options and its
answer — becomes the HTML the browser shows: the package that renders and sanitises it, the routes
and fields that answer it, and the components that show it. The decisions are [ADR 0011] D6 (rendered
Markdown is sanitised on the server) and [ADR 0016] D7 (an image only of the ticket's own raster
attachments); the security perspective — what it defends against and what it leaves open — is
[docs/security/rendered-markdown.md](../security/rendered-markdown.md). Read against the tree on
2026-10-05.

It is not the canonical Markdown of a ticket: that is [`internal/markdown`](../../backend/internal/markdown/),
which *writes* Markdown for the export and the context ([markdown-grammar.md](markdown-grammar.md));
`internal/richtext` *reads* Markdown and writes HTML.

## The package

[`internal/richtext`](../../backend/internal/richtext/richtext.go): `HTML(src, images)` — one pure
function of the text and the images it may show, safe for concurrent use.

| Step | Does |
|---|---|
| parse | goldmark with CommonMark, the GFM table (alignment as an `align` attribute, `TableCellAlignAttribute`, which both sanitisers keep), strikethrough and linkify extensions; no task lists (their `<input>` would be one element more on the list), no heading ids |
| `rewrite` | walks the tree once: an image whose address names a raster attachment of the ticket gets that attachment's path (`attachmentImage`, by the id in `attachmentContent`'s path on any host); any other image becomes a `Link` to its address with the image's text, or — inside a link — its text alone; every link and autolink whose address, its character references resolved as the renderer resolves them, has a scheme other than `http`, `https`, `mailto` is replaced by its text (`unwrap`); every other link gets `rel` `LinkRel` and `target="_blank"` |
| render | goldmark's HTML renderer, with `rawAsText` registered above it (priority 100 against the renderer's 1000 — the lower number wins) for `RawHTML` and `HTMLBlock`: the source of raw HTML written escaped, a block as one paragraph; the renderer's default would drop it and with it every word in angle brackets, `Vec<String>` included |
| sanitise | `policy`, a bluemonday policy built once (`newPolicy`): the elements and attributes of the allow-list and nothing else, the URL schemes, relative URLs, `nofollow` and `noreferrer` required, `img` `src` held to `attachmentContent` |

`Images` maps an attachment's id to the path of its bytes. The API builds it per ticket with
`imagesOf` in [`api/rendered.go`](../../backend/internal/api/rendered.go): `ListTicketImages` reads the
attachments of the tickets through their predicate, and `domain.InlineAttachment` keeps the raster
ones, the types delivered inline. A comment's attachment is its ticket's too, so a comment may show
an image uploaded to the ticket or to another comment of it.

**The allow-list is a test fixture** ([ADR 0011] D6): `allowList` in
[`richtext_test.go`](../../backend/internal/richtext/richtext_test.go), against which every output of
the tests is checked by parsing it as a browser would (`assertAllowed`). Adding an element or an
attribute is a change of `newPolicy`, of the fixture and of the security page in the same change, and
it has to stay inside what Angular's sanitiser keeps (`VALID_ELEMENTS`, `VALID_ATTRS` of
`@angular/core`), or the browser removes it again.

## The libraries

| Library | Version | Why |
|---|---|---|
| [goldmark](https://github.com/yuin/goldmark) | v1.8.6 (2026-09-03) | CommonMark-compliant, the GFM extensions as options, actively maintained (commits in 2026-09), raw HTML omitted unless asked for and dangerous URLs dropped by its own renderer — a safe default to build on —, and an AST that can be rewritten before rendering; the renderer of Hugo and Gitea |
| [bluemonday](https://github.com/microcosm-cc/bluemonday) | v1.0.27 (2024-07-04) | the allow-list sanitiser of Go: policies per element and attribute, a tokenizer of `golang.org/x/net/html`, URL scheme checks and the `rel` requirements; the pairing with goldmark is what Gitea renders untrusted Markdown with. Released rarely — v1.0.27 is the newest tag, the last commit is of 2025-04 — as a feature-complete library is; Renovate and govulncheck watch it |

Rejected: `gomarkdown/markdown` and `blackfriday` v2 — not CommonMark, the second unmaintained —
and a sanitiser of cowork's own over `x/net/html`, which would be security code to own where a
maintained one exists. Both chosen libraries are the newest stable versions on 2026-10-05.

## Where the HTML is answered

| What | Where | Why there |
|---|---|---|
| A ticket's body | `GET …/{number}/body` (`GetTicketBody`), `{"body","body_html","version"}` with the ticket's `ETag` | a route of its own: the ticket lists carry every row's Markdown, and rendering a page of bodies is work no list needs |
| A comment's text | `body_html` beside `body` on every comment the API answers (`renderedComment`), `null` once withdrawn; a mention is plain `@Name` text beside the comment's `mentions`, the ids, so the rendering knows nothing of it and links nobody ([domain.md](domain.md#comments-and-the-activity-list); `TestMarkdownRenders`, `TestAMentionTellsThePersonAndMakesThemAWatcher`) | a comment is read in its thread a page at a time; the images are read once per page |
| A question's options and answer | `options_html` and `answer_html` beside `options` and `answer` on every question (`questionView`), the open decisions of `/me/decisions` included (`imagesOf` per tenant's page) | likewise |

The context document, the Markdown export, the revisions of a comment and the MCP tools stay
Markdown: they are read by models and people as text. A stored answer an `Idempotency-Key` replays
carries the HTML as it was rendered when the answer was stored.

## In the browser

[`RenderedText`](../../frontend/src/app/shared/rendered-text.ts), `<app-rendered-text [html]>`, binds
the HTML to `[innerHTML]` as a string — Angular's sanitiser runs over it; nothing calls
`bypassSecurityTrust…` — and styles it with the preset's tokens. Its styles are not encapsulated,
because `[innerHTML]` content carries no attribute of the component's; every rule is scoped under
`app-rendered-text`. The page's parts ([frontend.md](frontend.md#the-detail-page)):

- **The body**: `TicketRelations.body` loads `GET …/body` for the version of the ticket the page shows
  (`TicketRelations.version`, which the page sets from the cache) and again on a `ticket.changed` of
  the ticket that moves no version — an upload, whose image the body may show. `TicketBody` shows the
  rendering only while its `body` is the body the cache holds; otherwise — loading, failed, an older
  body — the body shows as text, never as markup. The editor stays Markdown.
- **A comment**: `CommentItem` shows `body_html`, or the text where an answer has none.
- **A question**: the detail page shows `options_html` under *Options:* and `answer_html` under
  *Answer:*; the recommendation stays text.

## Tests

| Test | Holds |
|---|---|
| `TestMarkdownRenders`, `TestHostileInputIsDefused`, `TestTheSanitiserHoldsTheAllowList`, `TestEveryOutputKeepsToTheAllowList`, `TestPathologicalInputRendersQuickly` ([`richtext_test.go`](../../backend/internal/richtext/richtext_test.go)) | the Markdown that renders; script tags, handlers, `javascript:`, `data:`, `vbscript:` and `file:` addresses, character-reference tricks, raw HTML, remote and foreign images, attribute break-outs, nested tags — each one's exact output; the policy on its own against HTML the renderer never makes; every output against the allow-list; nesting and delimiter runs in bounded time |
| `TestTextsAreRenderedAndSanitisedOnTheServer` ([`api_rendered_test.go`](../../backend/test/integration/api_rendered_test.go)) | the body's route and `ETag`; the ticket's raster image shown, an SVG, another ticket's image and a remote one as links; a comment's and a question's HTML, `null` once withdrawn and without an answer; the open decisions with the ticket's images; a ticket the caller cannot see `404` |
| [`rendered-text.spec.ts`](../../frontend/src/app/shared/rendered-text.spec.ts), `ticket-body.spec.ts`, `ticket-relations.spec.ts`, `ticket-detail.spec.ts`, `comment-item.spec.ts` | Angular's sanitiser stays on; the body shows the rendering only of the body shown; the rendering loads for the version and on an upload |
| [`rendered.spec.ts`](../../frontend/e2e/rendered.spec.ts) (end-to-end) | in Chromium and WebKit, both schemes, behind the shell's content-security policy: a body's heading, table, code and link with its `rel` and `target`, the ticket's own PNG from its attachment's path and loaded, a raw `<img>` with a handler and a `javascript:` link as text with nothing run, and no refusal of the policy ([testing.md](testing.md#end-to-end-tests)) |

Not verified: how the rendered text looks in either scheme on a real screen — the end-to-end tier
asserts its markup and compares no picture of it.

[ADR 0011]: ../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md
[ADR 0016]: ../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md
