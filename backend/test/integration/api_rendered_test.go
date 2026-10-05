//go:build integration

package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// docs/adr/0011 D6, docs/adr/0016 D7: the server renders and sanitises the
// body, a comment, a question's options and its answer. Hostile Markdown
// reaches the reader as text; an image shows only when it is a raster
// attachment of the same ticket, from that attachment's own path; any other
// image is a link that loads nothing. The body has a route of its own,
// whose ETag is the ticket's; a withdrawn comment has no HTML.
func TestTextsAreRenderedAndSanitisedOnTheServer(t *testing.T) {
	e := newTicketEnv(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	tk := e.file(t, member, "ALPHA", task("Rendered"))
	other := e.file(t, member, "ALPHA", task("Another"))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)
	png := decodeAttachment(t, e.uploadTo(t, member, tk, "shot.png", "image/png", pngBytes, nil, ""))
	svg := decodeAttachment(t, e.uploadTo(t, member, tk, "logo.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), nil, ""))
	foreign := decodeAttachment(t, e.uploadTo(t, member, other, "theirs.png", "image/png", pngBytes, nil, ""))

	body := strings.Join([]string{
		"## State",
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"[run](javascript:alert(1)) [data](data:text/html;base64,PHNjcmlwdD4=) [docs](https://example.com)",
		"![shot](" + png.ContentUrl + ") ![logo](" + svg.ContentUrl + ") ![theirs](" + foreign.ContentUrl + ")",
		"![pixel](https://tracker.example/p.png) returns Vec<String>",
	}, "\n\n")
	etag := strconv.Quote(strconv.Itoa(tk.Version))
	e.send(t, member, http.StatusOK, http.MethodPut, path+"/body", map[string]any{"body": body}, "If-Match", etag)

	res := e.s.do(t, viewer, http.MethodGet, path+"/body", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, strconv.Quote(strconv.Itoa(tk.Version+1)), res.Header.Get("ETag"), "the ticket's ETag")
	got := decode[apigen.TicketBody](t, res)
	assert.Equal(t, body, got.Body, "the Markdown as written")
	assert.Equal(t, tk.Version+1, got.Version)
	html := got.BodyHtml
	assert.Contains(t, html, "<h2>State</h2>")
	assert.Contains(t, html, "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>", "raw HTML is text")
	assert.Contains(t, html, "<p>&lt;img src=x onerror=alert(1)&gt;</p>")
	assert.Contains(t, html, "<p>run data "+
		`<a href="https://example.com" rel="noopener noreferrer nofollow" target="_blank">docs</a></p>`,
		"a javascript: or data: link is its text")
	assert.Contains(t, html, `<img src="`+png.ContentUrl+`" alt="shot">`, "the ticket's own raster image shows")
	assert.Contains(t, html, `<a href="`+svg.ContentUrl+`" rel="noopener noreferrer nofollow" target="_blank">logo</a>`,
		"an SVG is no raster image: a link")
	assert.Contains(t, html, `<a href="`+foreign.ContentUrl+`" rel="noopener noreferrer nofollow" target="_blank">theirs</a>`,
		"another ticket's image: a link")
	assert.Contains(t, html, `<a href="https://tracker.example/p.png" rel="noopener noreferrer nofollow" target="_blank">pixel</a>`,
		"an image from elsewhere loads nothing")
	assert.Contains(t, html, "returns Vec&lt;String&gt;")
	for _, never := range []string{"<script", "onerror=\"", "javascript:", "data:", "<img src=\"https://", "<svg"} {
		assert.NotContains(t, html, never)
	}
	assert.Equal(t, 1, strings.Count(html, "<img "), "one image: the ticket's raster attachment")

	// A comment and a question carry their HTML beside the Markdown.
	res = e.s.do(t, member, http.MethodPost, path+"/comments", map[string]any{"body": "**done** ![shot](" + png.ContentUrl + ") <b onclick=x>"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	c := decode[apigen.Comment](t, res)
	assert.Equal(t, `<p><strong>done</strong> <img src="`+png.ContentUrl+`" alt="shot"> &lt;b onclick=x&gt;</p>`, c.BodyHtml.MustGet())
	list := decode[apigen.CommentList](t, e.s.do(t, viewer, http.MethodGet, path+"/comments", nil))
	require.Len(t, list.Items, 1)
	assert.Equal(t, c.BodyHtml.MustGet(), list.Items[0].BodyHtml.MustGet())
	e.send(t, member, http.StatusOK, http.MethodPut, path+"/comments/"+c.Id.String()+"/withdrawal", nil)
	gone := decode[apigen.Comment](t, e.s.do(t, viewer, http.MethodGet, path+"/comments/"+c.Id.String(), nil))
	assert.True(t, gone.BodyHtml.IsNull(), "a withdrawn comment has no text, rendered or not")

	res = e.s.do(t, member, http.MethodPost, path+"/questions", map[string]any{
		"question": "Ship it?", "options": "- *yes*\n- [no](javascript:alert(1))", "asked_of": e.MemberA})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	q := decode[apigen.Question](t, res)
	assert.Equal(t, "<ul>\n<li><em>yes</em></li>\n<li>no</li>\n</ul>", q.OptionsHtml)
	assert.True(t, q.AnswerHtml.IsNull(), "no answer, no HTML")
	answered := decode[apigen.Question](t, e.s.do(t, member, http.MethodPut, path+"/questions/1/answer",
		map[string]any{"answer": "Yes, see <https://example.com>"}))
	assert.Equal(t, `<p>Yes, see <a href="https://example.com" rel="noopener noreferrer nofollow" target="_blank">https://example.com</a></p>`,
		answered.AnswerHtml.MustGet())

	res = e.s.do(t, member, http.MethodPost, path+"/questions", map[string]any{
		"question": "Which one?", "options": "![shot](" + png.ContentUrl + ")", "asked_of": e.Both})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	decisions := decode[apigen.DecisionList](t, e.s.do(t, caller{Token: e.tk.Both}, http.MethodGet, "/api/v1/me/decisions", nil))
	require.Len(t, decisions.Items, 1)
	assert.Equal(t, `<p><img src="`+png.ContentUrl+`" alt="shot"></p>`, decisions.Items[0].Question.OptionsHtml,
		"the person-level list renders with the ticket's images")

	// The body is the ticket's: one that the caller cannot see is not found.
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path+"/body", nil), http.StatusNotFound, "not_found")
}
