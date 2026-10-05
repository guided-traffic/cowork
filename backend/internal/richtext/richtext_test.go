package richtext

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// The ticket of the tests and its attachments: a PNG the text may show, and
// what another ticket's path would name.
var (
	png     = uuid.MustParse("0199a3c2-1d2e-7f00-8000-0000000000a1")
	other   = uuid.MustParse("0199a3c2-1d2e-7f00-8000-0000000000b2")
	pngPath = "/api/v1/tenants/acme/projects/COW/tickets/12/attachments/" + png.String() + "/content"
	images  = Images{png: pngPath}
)

// allowList is the sanitiser's allow-list as a fixture (docs/adr/0011 D6): the
// elements a rendered text may hold, each with the attributes it may carry. A
// change here is a change to the security page that names it
// (docs/security/rendered-text.md).
var allowList = map[string][]string{
	"p": nil, "br": nil, "hr": nil, "h1": nil, "h2": nil, "h3": nil, "h4": nil, "h5": nil, "h6": nil,
	"blockquote": nil, "ul": nil, "ol": {"start"}, "li": nil, "pre": nil, "code": nil, "em": nil, "strong": nil,
	"del": nil, "table": nil, "thead": nil, "tbody": nil, "tr": nil, "th": {"align"}, "td": {"align"},
	"a":   {"href", "title", "rel", "target"},
	"img": {"src", "alt", "title"},
}

// docs/adr/0011 D6: the Markdown people write renders as HTML.
func TestMarkdownRenders(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"paragraphs and emphasis":              {"A *b* **c** ~~d~~ `e`\n\nnext", "<p>A <em>b</em> <strong>c</strong> <del>d</del> <code>e</code></p>\n<p>next</p>"},
		"headings carry no id":                 {"## Current state", "<h2>Current state</h2>"},
		"lists":                                {"- one\n- two\n\n3. three", "<ul>\n<li>one</li>\n<li>two</li>\n</ul>\n<ol start=\"3\">\n<li>three</li>\n</ol>"},
		"code block":                           {"```go\nfmt.Println(\"<b>\")\n```", "<pre><code>fmt.Println(&#34;&lt;b&gt;&#34;)\n</code></pre>"},
		"quote and rule":                       {"> quoted\n\n---", "<blockquote>\n<p>quoted</p>\n</blockquote>\n<hr>"},
		"table with alignment":                 {"| a | b |\n|:--|--:|\n| 1 | 2 |", "<table>\n<thead>\n<tr>\n<th align=\"left\">a</th>\n<th align=\"right\">b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td align=\"left\">1</td>\n<td align=\"right\">2</td>\n</tr>\n</tbody>\n</table>"},
		"a link":                               {"[docs](https://example.com/a?b=1)", `<p><a href="https://example.com/a?b=1" rel="noopener noreferrer nofollow" target="_blank">docs</a></p>`},
		"a relative link":                      {"[COW-12](/t/acme/tickets/COW-12)", `<p><a href="/t/acme/tickets/COW-12" rel="noopener noreferrer nofollow" target="_blank">COW-12</a></p>`},
		"an autolink":                          {"see <https://example.com>", `<p>see <a href="https://example.com" rel="noopener noreferrer nofollow" target="_blank">https://example.com</a></p>`},
		"a bare address":                       {"see www.example.com now", `<p>see <a href="http://www.example.com" rel="noopener noreferrer nofollow" target="_blank">www.example.com</a> now</p>`},
		"an e-mail address":                    {"<ada@example.com>", `<p><a href="mailto:ada@example.com" rel="noopener noreferrer nofollow" target="_blank">ada@example.com</a></p>`},
		"an attachment image":                  {"![shot](" + pngPath + ")", `<p><img src="` + pngPath + `" alt="shot"></p>`},
		"an attachment by its URL on any host": {"![shot](https://cowork.example" + pngPath + ")", `<p><img src="` + pngPath + `" alt="shot"></p>`},
		"angle brackets in prose are text":     {"returns Vec<String> or Option<T>", "<p>returns Vec&lt;String&gt; or Option&lt;T&gt;</p>"},
		"empty":                                {"   \n", ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, HTML(c.in, images))
		})
	}
}

// docs/adr/0011 D6, docs/adr/0016 D7: hostile input reaches the browser as
// text or not at all — no script, no inline handler, no raw HTML, no
// javascript: or data: address, no image from elsewhere.
func TestHostileInputIsDefused(t *testing.T) {
	cases := map[string]struct {
		in string
		// want is the whole output where it is exact.
		want string
	}{
		"a script tag":                      {in: "<script>alert(1)</script>", want: "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>"},
		"a script inline":                   {in: "a <script>alert(1)</script> b", want: "<p>a &lt;script&gt;alert(1)&lt;/script&gt; b</p>"},
		"an event handler":                  {in: `<img src=x onerror=alert(1)>`, want: "<p>&lt;img src=x onerror=alert(1)&gt;</p>"},
		"an inline event handler":           {in: `x <a href="https://e.com" onclick="alert(1)">y</a>`, want: `<p>x &lt;a href=&#34;https://e.com&#34; onclick=&#34;alert(1)&#34;&gt;y&lt;/a&gt;</p>`},
		"a style element":                   {in: "<style>body{display:none}</style>", want: "<p>&lt;style&gt;body{display:none}&lt;/style&gt;</p>"},
		"an iframe":                         {in: `<iframe src="https://evil.example"></iframe>`, want: `<p>&lt;iframe src=&#34;https://evil.example&#34;&gt;&lt;/iframe&gt;</p>`},
		"an svg":                            {in: `<svg onload=alert(1)>`, want: "<p>&lt;svg onload=alert(1)&gt;</p>"},
		"a comment":                         {in: "<!-- hidden -->", want: "<p>&lt;!-- hidden --&gt;</p>"},
		"a javascript link":                 {in: "[click](javascript:alert(1))", want: "<p>click</p>"},
		"a javascript link in case":         {in: "[click](JaVaScRiPt:alert(1))", want: "<p>click</p>"},
		"an entity-encoded javascript link": {in: "[click](&#106;avascript:alert(1))", want: "<p>click</p>"},
		"a javascript autolink":             {in: "<javascript:alert(1)>", want: "<p>javascript:alert(1)</p>"},
		"a vbscript link":                   {in: "[click](vbscript:msgbox)", want: "<p>click</p>"},
		"a data link":                       {in: "[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)", want: "<p>click</p>"},
		"a file link":                       {in: "[click](file:///etc/passwd)", want: "<p>click</p>"},
		"a data image":                      {in: "![x](data:image/png;base64,iVBORw0KGgo=)", want: "<p>x</p>"},
		"a remote image":                    {in: "![logo](https://tracker.example/pixel.png)", want: `<p><a href="https://tracker.example/pixel.png" rel="noopener noreferrer nofollow" target="_blank">logo</a></p>`},
		"a protocol-relative image":         {in: "![logo](//tracker.example/pixel.png)", want: `<p><a href="//tracker.example/pixel.png" rel="noopener noreferrer nofollow" target="_blank">logo</a></p>`},
		"another ticket's attachment": {
			in:   "![x](/api/v1/tenants/acme/projects/COW/tickets/13/attachments/" + other.String() + "/content)",
			want: `<p><a href="/api/v1/tenants/acme/projects/COW/tickets/13/attachments/` + other.String() + `/content" rel="noopener noreferrer nofollow" target="_blank">x</a></p>`,
		},
		"an image that names a query": {
			in:   "![x](" + pngPath + "?a=1)",
			want: `<p><img src="` + pngPath + `" alt="x"></p>`,
		},
		"an image with a javascript address":   {in: "![x](javascript:alert(1))", want: "<p>x</p>"},
		"an image inside a link":               {in: "[![x](https://e.com/a.png)](https://e.com)", want: `<p><a href="https://e.com" rel="noopener noreferrer nofollow" target="_blank">x</a></p>`},
		"an attribute break-out in a title":    {in: `[a](https://e.com "x\" onmouseover=\"alert(1)")`, want: `<p><a href="https://e.com" title="x&#34; onmouseover=&#34;alert(1)" rel="noopener noreferrer nofollow" target="_blank">a</a></p>`},
		"an attribute break-out in an address": {in: `[a](<https://e.com/"onmouseover="alert(1)>)`, want: `<p><a href="https://e.com/%22onmouseover=%22alert(1)" rel="noopener noreferrer nofollow" target="_blank">a</a></p>`},
		"a nested trick":                       {in: "<scr<script>ipt>alert(1)</scr</script>ipt>", want: "<p>&lt;scr&lt;script&gt;ipt&gt;alert(1)&lt;/scr&lt;/script&gt;ipt&gt;</p>"},
		"a raw block of html":                  {in: "<div onclick=\"alert(1)\">\n<b>x</b>\n</div>", want: "<p>&lt;div onclick=&#34;alert(1)&#34;&gt;\n&lt;b&gt;x&lt;/b&gt;\n&lt;/div&gt;</p>"},
		"a form":                               {in: `<form action="https://evil.example"><input name=q></form>`, want: `<p>&lt;form action=&#34;https://evil.example&#34;&gt;&lt;input name=q&gt;&lt;/form&gt;</p>`},
		"a base element":                       {in: `<base href="https://evil.example/">`, want: `<p>&lt;base href=&#34;https://evil.example/&#34;&gt;</p>`},
		"a meta refresh":                       {in: `<meta http-equiv="refresh" content="0;url=https://evil.example">`, want: `<p>&lt;meta http-equiv=&#34;refresh&#34; content=&#34;0;url=https://evil.example&#34;&gt;</p>`},
		"a link reference to javascript":       {in: "[x][1]\n\n[1]: javascript:alert(1)", want: "<p>x</p>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := HTML(c.in, images)
			assert.Equal(t, c.want, out)
			assertAllowed(t, out)
		})
	}
}

// The second line holds on its own: HTML that reaches the sanitiser keeps only
// what the allow-list names, whatever the renderer let through.
func TestTheSanitiserHoldsTheAllowList(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"a script":             {`<p>a<script>alert(1)</script></p>`, `<p>a</p>`},
		"an event handler":     {`<p onclick="alert(1)">a</p>`, `<p>a</p>`},
		"a javascript address": {`<a href="javascript:alert(1)" rel="noopener noreferrer nofollow" target="_blank">a</a>`, `<a rel="noopener noreferrer nofollow" target="_blank">a</a>`},
		"a data address":       {`<a href="data:text/html,x">a</a>`, `a`},
		"a link without rel":   {`<a href="https://e.com">a</a>`, `<a href="https://e.com" rel="nofollow noreferrer">a</a>`},
		"a rel of its own":     {`<a href="https://e.com" rel="opener">a</a>`, `<a href="https://e.com" rel="nofollow noreferrer">a</a>`},
		"a target of its own":  {`<a href="https://e.com" target="_top">a</a>`, `<a href="https://e.com" rel="nofollow noreferrer">a</a>`},
		"a remote image":       {`<img src="https://tracker.example/p.png" alt="x">`, `<img alt="x">`},
		"an image's handler":   {`<img src="` + pngPath + `" onerror="alert(1)">`, `<img src="` + pngPath + `">`},
		"an id and a class":    {`<p id="x" class="y" style="color:red">a</p>`, `<p>a</p>`},
		"an iframe":            {`<iframe src="https://e.com"></iframe>a`, `a`},
		"an svg with a script": {`<svg><script>alert(1)</script></svg>a`, `a`},
		"a math namespace":     {`<math><mtext><img src=x onerror=alert(1)></mtext></math>`, ``},
		// The tokenizer reads a noscript's content as text, as a browser with
		// scripts does, and what follows its end is held to the list.
		"a noscript break-out": {`<noscript><p title="</noscript><img src=x onerror=alert(1)>">`, `&#34;&gt;`},
		"a template":           {`<template><img src=x onerror=alert(1)></template>a`, `a`},
		"an unknown alignment": {`<td align="justify">a</td>`, `<td>a</td>`},
		"a style element":      {`<style>p{}</style>a`, `a`},
		"an object":            {`<object data="x"></object>a`, `a`},
	} {
		t.Run(name, func(t *testing.T) {
			out := policy.Sanitize(c.in)
			assert.Equal(t, c.want, out)
			assertAllowed(t, out)
		})
	}
}

// docs/adr/0011 D6: whatever the text, the output holds only the allow-list's
// elements and attributes, every link carries the rel, and every image is an
// attachment's.
func TestEveryOutputKeepsToTheAllowList(t *testing.T) {
	var corpus strings.Builder
	corpus.WriteString("# h\n\n| a | b |\n|:-:|--|\n| 1 | 2 |\n\n1. x\n2. y\n\n> q\n\n```\nc\n```\n\n")
	corpus.WriteString("[a](https://e.com \"t\") <https://e.com> www.e.com ![i](" + pngPath + " \"t\") ![r](https://e.com/x.png)\n\n")
	corpus.WriteString("<b onclick=x>b</b> <img src=x onerror=y> [j](javascript:x) ![d](data:image/png;base64,AA==)\n")
	out := HTML(corpus.String(), images)
	assertAllowed(t, out)
	assert.Contains(t, out, "<table>", "the corpus reaches the table")
	assert.Contains(t, out, `<img src="`+pngPath+`" alt="i" title="t">`)
}

// A pathological text renders in bounded time: nesting and delimiter runs do
// not make the request hang (docs/adr/0039 D1).
func TestPathologicalInputRendersQuickly(t *testing.T) {
	for name, in := range map[string]string{
		"nested quotes":    strings.Repeat(">", 10000) + " x",
		"nested lists":     strings.Repeat("- ", 5000) + "x",
		"open brackets":    strings.Repeat("[", 50000),
		"emphasis runs":    strings.Repeat("*a _b ", 20000),
		"unclosed tags":    strings.Repeat("<a ", 20000),
		"backtick runs":    strings.Repeat("`", 50000) + "x",
		"a long body":      strings.Repeat("word ", 40000),
		"link definitions": strings.Repeat("[a]: /x\n", 10000) + strings.Repeat("[a] ", 10000),
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			out := HTML(in, images)
			assert.Less(t, time.Since(start), 5*time.Second)
			assertAllowed(t, out)
		})
	}
}

// assertAllowed parses the output as the browser would and fails on any
// element or attribute the allow-list does not name, a link without the rel,
// and an image that is not an attachment's.
func assertAllowed(t *testing.T, out string) {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(out))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			require.ErrorIs(t, z.Err(), io.EOF)
			return
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			assert.NotEqual(t, html.CommentToken, tt, "no comment passes")
			continue
		}
		tok := z.Token()
		allowed, ok := allowList[tok.Data]
		if !assert.True(t, ok, "element %q is not on the allow-list: %s", tok.Data, out) {
			continue
		}
		attrs := map[string]string{}
		for _, a := range tok.Attr {
			assert.Contains(t, allowed, a.Key, "attribute %q of %q is not on the allow-list: %s", a.Key, tok.Data, out)
			attrs[a.Key] = a.Val
		}
		switch tok.Data {
		case "a":
			if href, ok := attrs["href"]; ok {
				assert.True(t, allowedLink(href), "link to %q", href)
				assert.Contains(t, attrs["rel"], "noreferrer")
				assert.Contains(t, attrs["rel"], "nofollow")
				lower := strings.ToLower(href)
				assert.False(t, strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:"), href)
			}
		case "img":
			if src, ok := attrs["src"]; ok {
				assert.Regexp(t, attachmentContent, src, "an image only from an attachment's path")
			}
		}
	}
}
