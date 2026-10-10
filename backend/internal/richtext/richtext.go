// Package richtext turns the Markdown a person or an agent wrote — a ticket's
// body, a comment, a question's options and answer — into the HTML another
// person's browser shows (docs/adr/0011 D6, docs/adr/0016 D7). It is not the
// canonical Markdown of a ticket, which internal/markdown writes; it reads
// what people wrote and makes it safe to show.
//
// Two steps, each enough on its own for what it guards. goldmark parses the
// text and renders HTML from the tree, after the tree is rewritten: raw HTML is
// shown as the text it is, never as markup; a link keeps only an http, https or
// mailto address or a relative one, opens in a new tab and carries
// rel="noopener noreferrer nofollow"; an image shows only when it names a
// raster attachment of the same ticket, and is then served from that
// attachment's own path — any other image becomes a link to its address, so
// nothing is fetched from elsewhere. bluemonday then holds the HTML to an
// allow-list of elements and attributes, the same rules again: whatever the
// first step let through by mistake, the second removes.
//
// What the parser reads is bounded (bounds.go): a text longer than the longest
// the API takes is not parsed at all but shown as written, escaped, in one
// preformatted block; below that, how deep blocks nest and how many emphasis,
// link and raw HTML markers are read as such are bounded per text.
package richtext

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Images are the raster attachments a text may show, by id, each with the
// path the backend delivers it at: the attachments of the text's own ticket
// whose type is shown inline (docs/adr/0016 D5, D7).
type Images map[uuid.UUID]string

// LinkRel is the rel every rendered link carries: no access to the page that
// opened it, no Referer, and no endorsement of the address.
const LinkRel = "noopener noreferrer nofollow"

// linkSchemes are the schemes a link may have; a link without one is
// relative to the installation.
var linkSchemes = map[string]bool{"http": true, "https": true, "mailto": true}

// attachmentContent is the path of an attachment's bytes, the only source an
// image may have; the id is what names the attachment, which then shows from
// the path Images gives it. The path is under the team family, or under the
// family before a tenant was called a team, which the texts written before
// name for good (docs/adr/0005 D1, docs/adr/0016 D7).
var attachmentContent = regexp.MustCompile(
	`^/api/v1/(?:teams|tenants)/[^/]+/projects/[^/]+/tickets/[0-9]+/attachments/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/content$`)

var (
	markdown = goldmark.New(
		// goldmark's parsers with the bounds of bounds.go; the strikethrough
		// of GitHub Flavored Markdown is among the inline parsers, so its
		// renderer is added below rather than by its extension.
		goldmark.WithParser(parser.NewParser(
			parser.WithBlockParsers(blockParsers()...),
			parser.WithInlineParsers(inlineParsers()...),
			parser.WithParagraphTransformers(paragraphTransformers()...),
		)),
		goldmark.WithExtensions(
			// Tables with their alignment as an attribute, not a style, which
			// the sanitiser and the browser's own sanitiser keep.
			extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
			extension.Linkify,
		),
		goldmark.WithRendererOptions(
			// Raw HTML is shown as text: the renderer's default drops it, and
			// with it every word between angle brackets, Vec<String> included.
			renderer.WithNodeRenderers(util.Prioritized(rawAsText{}, 100),
				util.Prioritized(extension.NewStrikethroughHTMLRenderer(), 500)),
		),
	)
	policy = newPolicy()
)

// HTML renders src as Markdown and sanitises the result. A text without
// visible content renders as the empty string, and a text longer than
// MaxLength as written, as plain text.
func HTML(src string, images Images) string {
	if utf8.RuneCountInString(src) > MaxLength {
		return plain(src)
	}
	source := []byte(src)
	doc := markdown.Parser().Parse(text.NewReader(source))
	rewrite(doc, source, images)
	var out bytes.Buffer
	if err := markdown.Renderer().Render(&out, source, doc); err != nil {
		// Rendering into memory does not fail; should it, nothing of the
		// text is shown rather than a part of it.
		return ""
	}
	return strings.TrimSpace(policy.Sanitize(out.String()))
}

// rewrite holds links and images to the rules of the package before anything
// is rendered.
func rewrite(doc ast.Node, source []byte, images Images) {
	var links, pictures []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindLink, ast.KindAutoLink:
			links = append(links, n)
		case ast.KindImage:
			pictures = append(pictures, n)
		}
		return ast.WalkContinue, nil
	})
	for _, n := range pictures {
		img := n.(*ast.Image)
		if path, ok := attachmentImage(img.Destination, images); ok {
			img.Destination = []byte(path)
			continue
		}
		if insideLink(img) {
			// A link cannot hold another: the image's text stands in.
			unwrap(img, source)
			continue
		}
		// Not a raster attachment of this ticket: a link to the address,
		// which nothing loads unless a person follows it.
		link := ast.NewLink()
		link.Destination, link.Title = img.Destination, img.Title
		for c := img.FirstChild(); c != nil; {
			next := c.NextSibling()
			link.AppendChild(link, c)
			c = next
		}
		img.Parent().ReplaceChild(img.Parent(), img, link)
		links = append(links, link)
	}
	for _, n := range links {
		destination := linkDestination(n, source)
		if !allowedLink(destination) {
			unwrap(n, source)
			continue
		}
		n.SetAttributeString("rel", LinkRel)
		n.SetAttributeString("target", "_blank")
	}
}

// insideLink says whether a node is part of a link's text.
func insideLink(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == ast.KindLink {
			return true
		}
	}
	return false
}

// linkDestination is the address a link or an autolink points to, its
// character references resolved as the renderer resolves them.
func linkDestination(n ast.Node, source []byte) string {
	if a, ok := n.(*ast.AutoLink); ok {
		if a.AutoLinkType == ast.AutoLinkEmail {
			return "mailto:" + string(a.URL(source))
		}
		return string(util.URLEscape(a.URL(source), false))
	}
	return string(util.URLEscape(n.(*ast.Link).Destination, true))
}

// allowedLink says whether a link may point at the address: an http, https or
// mailto URL, or one relative to the installation.
func allowedLink(destination string) bool {
	u, err := url.Parse(strings.TrimSpace(destination))
	if err != nil {
		return false
	}
	return u.Scheme == "" || linkSchemes[strings.ToLower(u.Scheme)]
}

// attachmentImage is the path an image is shown from, when its address names a
// raster attachment of the ticket — by its path, on this installation's host
// or written with another — and false otherwise.
func attachmentImage(destination []byte, images Images) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(string(util.URLEscape(destination, true))))
	if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	m := attachmentContent.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false
	}
	path, ok := images[uuid.MustParse(m[1])]
	return path, ok
}

// unwrap replaces a link or an image by what it shows: its text, or for an
// autolink the address as text.
func unwrap(n ast.Node, source []byte) {
	parent := n.Parent()
	if a, ok := n.(*ast.AutoLink); ok {
		parent.ReplaceChild(parent, n, ast.NewString(a.Label(source)))
		return
	}
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}

// rawAsText renders raw HTML, inline and as a block, as the text it is.
type rawAsText struct{}

func (rawAsText) RegisterFuncs(r renderer.NodeRendererFuncRegisterer) {
	r.Register(ast.KindRawHTML, renderRawInline)
	r.Register(ast.KindHTMLBlock, renderRawBlock)
}

func renderRawInline(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		segments := n.(*ast.RawHTML).Segments
		for i := range segments.Len() {
			segment := segments.At(i)
			_, _ = w.Write(util.EscapeHTML(segment.Value(source)))
		}
	}
	return ast.WalkSkipChildren, nil
}

func renderRawBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block := n.(*ast.HTMLBlock)
	var raw []byte
	lines := block.Lines()
	for i := range lines.Len() {
		line := lines.At(i)
		raw = append(raw, line.Value(source)...)
	}
	if block.HasClosure() {
		raw = append(raw, block.ClosureLine.Value(source)...)
	}
	_, _ = w.WriteString("<p>")
	_, _ = w.Write(util.EscapeHTML(bytes.TrimRight(raw, "\r\n")))
	_, _ = w.WriteString("</p>\n")
	return ast.WalkSkipChildren, nil
}

// newPolicy is the allow-list: the elements the Markdown above renders and
// nothing else, each with the attributes it needs. It is a subset of what
// Angular's own sanitiser keeps, so the browser removes nothing more.
func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "ul", "ol", "li",
		"pre", "code", "em", "strong", "del", "table", "thead", "tbody", "tr", "th", "td")
	p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|center|right)$`)).OnElements("th", "td")

	p.AllowAttrs("href", "title").OnElements("a")
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^` + LinkRel + `$`)).OnElements("a")
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireParseableURLs(true)
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)

	// An image only from the path of an attachment's bytes.
	p.AllowAttrs("src").Matching(attachmentContent).OnElements("img")
	p.AllowAttrs("alt", "title").OnElements("img")
	return p
}
