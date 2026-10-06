# What text from other people can do in a reader's browser

How the Markdown that people and agents write — a ticket's body, a comment, a question's options and
its answer — becomes HTML in another person's browser, which rules hold it there, and what that
leaves open, as built on 2026-10-05
([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6,
[ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D7). The files those texts may show are [attachments.md](attachments.md); the policy the shell sends
with the page is [trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy); what
the same text can make a model do is [chat.md](chat.md#h-38). How the rendering is built is
[docs/developer/rendered-markdown.md](../developer/rendered-markdown.md).

## Three lines

A text is written by one person or agent and read by another, of the same tenant, who may be an
administrator; whatever it holds is foreign input to the reader's session.

```
 writer (person, agent)           backend                                  reader's browser
┌──────────────────────┐  PUT …   ┌──────────────────────────────────┐ GET ┌─────────────────────────┐
│ Markdown, any bytes  │ ───────► │ stored as written                │ ──► │ [innerHTML] as a string:│
└──────────────────────┘          │ rendered on every read:          │     │ Angular's sanitiser     │
                                  │ 1. goldmark, the tree rewritten  │     │ (never bypassed)        │
                                  │ 2. bluemonday, the allow-list    │     │ the shell's policy:     │
                                  └──────────────────────────────────┘     │ script-src 'self'       │
                                                                           └─────────────────────────┘
```

1. **The renderer** ([`internal/richtext`](../../backend/internal/richtext/richtext.go), `HTML`):
   [goldmark](https://github.com/yuin/goldmark) v1.8.6 parses the text as CommonMark with tables,
   strikethrough and bare addresses as links, and the tree is rewritten before anything is rendered
   (`rewrite`): raw HTML — a block or a tag inside a line — is written out as the text it is, escaped,
   never as markup (`rawAsText`); a link or an autolink keeps an `http`, `https` or `mailto` address
   or one without a scheme, and any other — `javascript:`, `data:`, `vbscript:`, `file:`, written in
   any case or with character references — loses its link and keeps its text; every link left gets
   `rel="noopener noreferrer nofollow"` and `target="_blank"`. goldmark escapes every text and every
   attribute value it writes.
2. **The sanitiser** ([bluemonday](https://github.com/microcosm-cc/bluemonday) v1.0.27,
   `newPolicy`) parses that HTML as a browser's tokenizer does and keeps only what the allow-list
   below names, with every link's address held to the same schemes, `nofollow` and `noreferrer`
   required on every link, and an image's source held to the path of an attachment's bytes. It is the
   second line: whatever the renderer let through by mistake, it removes.
3. **The browser.** The page binds the HTML to `[innerHTML]` as a plain string
   ([`RenderedText`](../../frontend/src/app/shared/rendered-text.ts)), so Angular's own sanitiser
   runs over it once more; nothing in the UI calls `bypassSecurityTrust…`. The server's allow-list is
   a subset of what Angular keeps, so this line removes nothing the first two let through on purpose,
   and stands alone should they ever let through more. Behind it, the shell's policy refuses inline
   scripts and every request to another origin.

What reaches the reader's page is therefore never a script, an event handler, a style, a frame, a
form, an object, a `<base>` or a `<meta>`, an `id` or a `class`, and never a request to another host.
The unit tests feed both lines hostile input — script tags, handlers, `javascript:` and `data:`
addresses in links and images, raw HTML, remote images, attribute break-outs, nested and namespace
tricks, a `<noscript>` break-out — and hold every output to the allow-list
([`richtext_test.go`](../../backend/internal/richtext/richtext_test.go)); the integration tier does
the same through the API ([`api_rendered_test.go`](../../backend/test/integration/api_rendered_test.go)),
and the frontend's tests that Angular's sanitiser stays on
([`rendered-text.spec.ts`](../../frontend/src/app/shared/rendered-text.spec.ts)). In a real browser,
behind the shell's policy, the end-to-end tier shows a body with headings, a table, code, a link, the
ticket's own PNG loaded from its attachment's path, a raw `<img>` with a handler and a
`javascript:` link: both hostile lines stay text, neither runs, and the policy refuses nothing, in
Chromium and WebKit ([`rendered.spec.ts`](../../frontend/e2e/rendered.spec.ts)).

## The allow-list

The allow-list is a test fixture (`allowList` in `richtext_test.go`, ADR 0011 D6): a change to it is
a change to this page.

| Element | Attributes |
|---|---|
| `p`, `br`, `hr`, `h1`–`h6`, `blockquote`, `ul`, `li`, `pre`, `code`, `em`, `strong`, `del`, `table`, `thead`, `tbody`, `tr` | none |
| `ol` | `start`, an integer |
| `th`, `td` | `align`: `left`, `center`, `right` |
| `a` | `href` (`http`, `https`, `mailto` or relative), `title`, `rel` (exactly `noopener noreferrer nofollow`), `target` (exactly `_blank`) |
| `img` | `src` (the path of an attachment's bytes, nothing else), `alt`, `title` |

Headings carry no `id`: an `id` in a text would let it name, and clobber, elements of the page.

## Images

An image shows only when its address names a raster attachment — PNG, JPEG, GIF, WebP, the types
delivered inline (ADR 0016 D5) — **of the same ticket**, by the path of its bytes on any host; its
source is then rewritten to that attachment's own path on this installation, so the browser asks
nothing of another origin. The ticket's raster attachments are read through the ticket's visibility
predicate ([`api/rendered.go`](../../backend/internal/api/rendered.go) `imagesOf`). Any other image —
an SVG, another ticket's attachment, an address elsewhere, a `data:` image — becomes a link to its
address with the image's text, which nothing loads until a person follows it; a link inside a link
keeps its text alone. An image shown is a download: the browser fetches it with the reader's
session, and every `200` is recorded as `downloaded` in the tenant's audit record — the
reader's opening of a text with an image is in the record their administrators read, as a preview's
is ([attachments.md](attachments.md#delivery-makes-the-browser-treat-the-bytes-as-data)).

## Links

A link opens in a new tab, `noopener` takes the new tab's handle on the page away, `noreferrer` keeps
the page's address from the target, `nofollow` endorses nothing. A relative link points into the
installation; following one is a top-level `GET` with the reader's session cookie (`SameSite=Lax`
admits it), and a `GET` changes nothing: every write of a session is another method and must pass the
CSRF check ([csrf.md](csrf.md)). A `GET` of the API that a link names is read with the reader's
rights, and one of an attachment's bytes is recorded as the reader's download.

## What this does not cover

<a id="h-51"></a>
### H-51 — A link's text can name one address and lead to another

Live in every text. `[https://cowork.example/login](https://evil.example/login)` shows one address
and opens another; a text can also imitate the look of the page within the allow-list — a heading, a
table. The renderer does not compare a link's text with its target and marks no link as leading
elsewhere. What a reader has: the target in the browser's status bar on hover and focus, a new tab
that cannot reach back into cowork (`noopener`), and no `Referer`. Mitigation: treat a link in a
ticket like a link in an e-mail of a colleague's.

<a id="h-52"></a>
### H-52 — The Markdown is not sanitised; only its rendering is

Live for every client but the UI. The API stores and answers the body, a comment, a question's
options and its answer as written — `body`, `options`, `answer` beside `body_html`, `options_html`,
`answer_html` — and so do the canonical Markdown, the context document, the MCP tools and the chat's
tools. A client that renders that Markdown itself — another UI, a script that writes it into a page,
a tool that shows a model's output as HTML — gets none of the rules on this page. The UI renders only
the server's HTML, and the chat's panel shows a model's text as text ([chat.md](chat.md#what-the-panel-shows)).
Mitigation for whoever builds such a client: render the `…_html` fields, not the Markdown.

### What the rendering costs

A text is rendered on every read that answers it — the body on its own route, never in a list, and a
comment or a question on every page of them. A text is at most 200,000 characters (a body) or
100,000 (a comment, the options, an answer), and the unit tests hold pathological nesting and
delimiter runs to a bounded time; beyond that, `COWORK_REQUEST_TIMEOUT` ends a request that renders
too long. No cache keeps a rendering.

### The libraries' own flaws

Both lines are third-party code. A flaw in goldmark's parser or in bluemonday's policy would reach
every reader; govulncheck runs over the backend in CI ([ci-and-release.md](../developer/ci-and-release.md)),
Renovate moves both, and Angular's sanitiser and the shell's policy stand behind them. bluemonday's
releases are rare — v1.0.27 of 2024-07-04 is its newest — which is the cadence of a library that is
feature-complete, not a fix this page can make.
