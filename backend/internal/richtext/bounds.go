package richtext

import (
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The bounds of a rendering (docs/adr/0011 D6). A text is rendered on every
// read that answers it, so what the parser reads of a text as Markdown is
// bounded: the length of the text, how deep its blocks nest, how many markers
// it reads as emphasis, links and raw HTML, and how far a link's text and
// destination reach — the constructs whose parsing otherwise grows faster than
// the text. Below the bounds a text renders as CommonMark does; beyond them a
// marker is text.
const (
	// MaxLength is the longest text, in characters, that renders as
	// Markdown: a ticket's body, the longest text the API takes. A longer
	// text is shown as written, as plain text.
	MaxLength = 200_000
	// maxNesting is how deep block quotes and lists nest inside each other,
	// counted in blocks from the document; a marker deeper is text.
	maxNesting = 32
	// maxDelimiters is how many runs of *, _ and ~ a text's rendering reads
	// as emphasis and strikethrough.
	maxDelimiters = 2000
	// maxLinkOpeners is how many [ and ![ a text's rendering reads as the
	// opening of a link or an image.
	maxLinkOpeners = 1000
	// maxLinkText is how far, in bytes, a closing bracket may follow the
	// opener it closes; one further away is text, and the opener stays open.
	maxLinkText = 4096
	// maxDestination is how far, in bytes, the parser looks for the end of an
	// inline link's destination on its line; one that does not end within it
	// is no destination.
	maxDestination = 4096
	// maxRawHTML is how many comments, processing instructions,
	// declarations and CDATA sections — the raw HTML read up to a marker of
	// its end — a text's rendering reads as raw HTML, which is shown as text
	// either way.
	maxRawHTML = 250
	// maxDefinitionLines is how many lines a paragraph may have for the link
	// reference definitions at its start to be read as such.
	maxDefinitionLines = 1000
)

// The state of a rendering in the parser's context of the text: the counts of
// the bounded constructs, and the openers of links still open.
var (
	delimiters  = parser.NewContextKey()
	linkOpeners = parser.NewContextKey()
	openLinks   = parser.NewContextKey()
	rawHTML     = parser.NewContextKey()
)

// plain is a text beyond MaxLength as it was written, never parsed: escaped,
// in one preformatted block.
func plain(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	return "<pre>" + html.EscapeString(src) + "</pre>"
}

// blockParsers are goldmark's, with block quotes and lists held to
// maxNesting.
func blockParsers() []util.PrioritizedValue {
	ps := parser.DefaultBlockParsers()
	for i, p := range ps {
		switch p.Value {
		case parser.NewBlockquoteParser(), parser.NewListParser():
			ps[i].Value = nested{p.Value.(parser.BlockParser)}
		}
	}
	return ps
}

// inlineParsers are goldmark's and the strikethrough of GitHub Flavored
// Markdown, with emphasis, strikethrough, links and raw HTML each held to its
// bounds.
func inlineParsers() []util.PrioritizedValue {
	ps := append(parser.DefaultInlineParsers(), util.Prioritized(extension.NewStrikethroughParser(), 500))
	for i, p := range ps {
		ip, _ := p.Value.(parser.InlineParser)
		switch p.Value {
		case parser.NewEmphasisParser(), extension.NewStrikethroughParser():
			ps[i].Value = counted{InlineParser: ip, key: delimiters, limit: maxDelimiters}
		case parser.NewLinkParser():
			ps[i].Value = links{counted{InlineParser: ip, key: linkOpeners, limit: maxLinkOpeners, counts: opensLink}}
		case parser.NewRawHTMLParser():
			ps[i].Value = counted{InlineParser: ip, key: rawHTML, limit: maxRawHTML, counts: readsToMarker}
		}
	}
	return ps
}

// paragraphTransformers are goldmark's, with the reader of link reference
// definitions held to maxDefinitionLines.
func paragraphTransformers() []util.PrioritizedValue {
	ps := parser.DefaultParagraphTransformers()
	for i, p := range ps {
		if p.Value == parser.LinkReferenceParagraphTransformer {
			ps[i].Value = definitions{parser.LinkReferenceParagraphTransformer}
		}
	}
	return ps
}

// definitions reads the link reference definitions at the start of a
// paragraph of at most maxDefinitionLines lines; a longer paragraph keeps
// them as its text.
type definitions struct {
	parser.ParagraphTransformer
}

func (d definitions) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	if node.Lines().Len() > maxDefinitionLines {
		return
	}
	d.ParagraphTransformer.Transform(node, reader, pc)
}

// nested is a block quote's or a list's parser that opens no block deeper
// than maxNesting: the marker is then read as what it would be without it,
// mostly a paragraph's text.
type nested struct {
	parser.BlockParser
}

func (n nested) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	depth := 0
	for p := parent; p != nil; p = p.Parent() {
		depth++
	}
	if depth > maxNesting {
		return nil, parser.NoChildren
	}
	return n.BlockParser.Open(parent, reader, pc)
}

// counted is an inline parser that reads at most limit of its constructs in a
// text, counted under key; counts says which triggers count, every one when
// it is nil. Beyond the limit the trigger is text.
type counted struct {
	parser.InlineParser
	key    parser.ContextKey
	limit  int
	counts func(line []byte) bool
}

func (c counted) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	if line, _ := block.PeekLine(); c.counts == nil || c.counts(line) {
		n, _ := pc.Get(c.key).(int)
		if n >= c.limit {
			return nil
		}
		pc.Set(c.key, n+1)
	}
	return c.InlineParser.Parse(parent, block, pc)
}

// CloseBlock hands the end of a block to a parser that keeps state over it —
// the link parser does, and turns the openers no bracket closed into text.
func (c counted) CloseBlock(parent ast.Node, block text.Reader, pc parser.Context) {
	if cb, ok := c.InlineParser.(parser.CloseBlocker); ok {
		cb.CloseBlock(parent, block, pc)
	}
}

// readsToMarker reports whether the raw HTML parser's trigger starts what it
// reads up to a marker of its end — <!-- -->, <? ?>, <!X >, <![CDATA[ ]]> —
// rather than a tag, which its grammar ends.
func readsToMarker(line []byte) bool {
	return len(line) > 1 && (line[1] == '!' || line[1] == '?')
}

// opensLink reports whether the link parser's trigger opens a link or an
// image.
func opensLink(line []byte) bool {
	return len(line) > 0 && (line[0] == '[' || len(line) > 1 && line[0] == '!' && line[1] == '[')
}

// links is goldmark's link parser with its openers counted and its closing
// brackets held to the bounds. goldmark's closing bracket takes the last
// opener of its block, whether it makes a link or not, and reads back to it:
// links keeps the same openers, by where they start, and hands a closing
// bracket on only within maxLinkText of the last one. Of an inline link's
// destination that does not end within maxDestination, the parser sees the
// line end right after the opening parenthesis, and does what it does with
// every link that does not parse: its brackets are text, unless a reference
// defines its label.
type links struct {
	counted
}

func (l links) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, seg := block.PeekLine()
	open, _ := pc.Get(openLinks).([]int)
	switch {
	case opensLink(line):
		n := l.counted.Parse(parent, block, pc)
		if n != nil {
			pc.Set(openLinks, append(open, seg.Start))
		}
		return n
	case len(line) == 0 || line[0] != ']' || len(open) == 0:
		return l.counted.Parse(parent, block, pc)
	case seg.Start-open[len(open)-1] > maxLinkText:
		return nil
	}
	pc.Set(openLinks, open[:len(open)-1])
	if len(line) > 1 && line[1] == '(' && !destinationEnds(line[2:]) {
		return l.counted.Parse(parent, cutLine{Reader: block, stop: seg.Start + 2}, pc)
	}
	return l.counted.Parse(parent, block, pc)
}

// CloseBlock ends a block, whose openers goldmark turns into text.
func (l links) CloseBlock(parent ast.Node, block text.Reader, pc parser.Context) {
	pc.Set(openLinks, nil)
	l.counted.CloseBlock(parent, block, pc)
}

// destinationEnds reports whether the destination after an inline link's
// "](" ends within maxDestination bytes as goldmark's parser reads it: after
// spaces, the <…> form at its >, any other at a space or at a ) that closes no
// ( of its own, a backslash escaping the punctuation after it. A rest of the
// line no longer than the bound ends within it whatever it holds.
func destinationEnds(rest []byte) bool {
	start := 0
	for start < len(rest) && util.IsSpace(rest[start]) {
		start++
	}
	window := rest[start:]
	if len(window) <= maxDestination {
		return true
	}
	window = window[:maxDestination]
	angle, opened := window[0] == '<', 0
	for i := 0; i < len(window); i++ {
		switch c := window[i]; {
		case c == '\\' && i+1 < len(window) && util.IsPunct(window[i+1]):
			i++
		case angle:
			if c == '>' {
				return true
			}
		case c == '(':
			opened++
		case c == ')':
			if opened == 0 {
				return true
			}
			opened--
		case util.IsSpace(c):
			return true
		}
	}
	return false
}

// cutLine is a reader whose current line ends at the source offset stop for
// whoever peeks it.
type cutLine struct {
	text.Reader
	stop int
}

func (r cutLine) PeekLine() ([]byte, text.Segment) {
	line, seg := r.Reader.PeekLine()
	if n := r.stop - seg.Start; line != nil && n < len(line) {
		n = max(n, 0)
		return line[:n], seg.WithStop(seg.Start + n)
	}
	return line, seg
}
