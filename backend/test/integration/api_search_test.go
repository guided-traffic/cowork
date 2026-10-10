//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// search reads a search as c — the tenant's when slug is set, the person's
// across their tenants otherwise — and returns the hits.
func (e ticketEnv) search(t *testing.T, c caller, slug, q, extra string) apigen.SearchHitList {
	t.Helper()
	path := "/api/v1/me/search"
	if slug != "" {
		path = "/api/v1/teams/" + slug + "/search"
	}
	res := e.s.do(t, c, http.MethodGet, path+"?q="+url.QueryEscape(q)+extra, nil)
	require.Equal(t, http.StatusOK, res.StatusCode, "%s %s: %v", slug, q, res.Status)
	return decode[apigen.SearchHitList](t, res)
}

// hitKeys are the keys of the hits, in their order.
func hitKeys(l apigen.SearchHitList) []string {
	keys := make([]string, 0, len(l.Items))
	for _, h := range l.Items {
		keys = append(keys, h.Key)
	}
	return keys
}

// snippetText is a hit's snippet as one text, the found words in brackets.
func snippetText(h apigen.SearchHit) string {
	var b strings.Builder
	for _, p := range h.Snippet {
		if p.Match {
			b.WriteString("[" + p.Text + "]")
		} else {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// docs/adr/0025 D3–D5, docs/adr/0018 D7: one hit per ticket at its best match,
// the title above the body above comments, questions and file names, a key
// above everything and a title by trigram below; where it matched and a
// snippet of that text; a withdrawn comment is not searched.
func TestSearchFindsAndRanksWithSnippets(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	inTitle := e.file(t, member, "ALPHA", task("Quokka migration plan", func(b *apigen.TicketCreate) { b.Body = ptr("Steps.") }))
	inBody := e.file(t, member, "ALPHA", task("Other", func(b *apigen.TicketCreate) {
		b.Body = ptr("The quokka sleeps under the desk all day, and nobody minds.")
	}))
	inComment := e.file(t, member, "ALPHA", task("Third"))
	e.send(t, member, http.StatusCreated, http.MethodPost, ticketPath(e.SlugA, "ALPHA", inComment.Number)+"/comments",
		map[string]any{"body": "Yesterday a quokka was seen near the rack."})
	inQuestion := e.file(t, member, "ALPHA", task("Fourth"))
	e.send(t, member, http.StatusCreated, http.MethodPost, ticketPath(e.SlugA, "ALPHA", inQuestion.Number)+"/questions",
		map[string]any{"question": "Does the quokka bite?"})
	inFile := e.file(t, member, "ALPHA", task("Fifth"))
	decodeAttachment(t, e.uploadTo(t, member, inFile, "quokka_portrait.png", "image/png", pngBytes, nil, ""))
	withdrawn := e.file(t, member, "ALPHA", task("Sixth"))
	res := e.s.do(t, member, http.MethodPost, ticketPath(e.SlugA, "ALPHA", withdrawn.Number)+"/comments",
		map[string]any{"body": "a quokka, said in passing"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	comment := decode[apigen.Comment](t, res)
	e.send(t, member, http.StatusOK, http.MethodPut,
		ticketPath(e.SlugA, "ALPHA", withdrawn.Number)+"/comments/"+comment.Id.String()+"/withdrawal", nil)

	hits := e.search(t, member, e.SlugA, "quokka", "")
	keys := hitKeys(hits)
	require.Len(t, keys, 5, "one hit per ticket, the withdrawn comment's not among them: %v", keys)
	assert.Equal(t, []string{inTitle.Key, inBody.Key}, keys[:2], "the title above the body above the rest")
	assert.ElementsMatch(t, []string{inComment.Key, inQuestion.Key, inFile.Key}, keys[2:])
	byKey := map[string]apigen.SearchHit{}
	for _, h := range hits.Items {
		byKey[h.Key] = h
		assert.Equal(t, apigen.TeamRef{Slug: e.SlugA, Name: "Team A"}, h.Team)
	}
	assert.Equal(t, apigen.SearchFoundInTicket, byKey[inTitle.Key].FoundIn)
	assert.Equal(t, "Quokka migration plan", byKey[inTitle.Key].Title)
	assert.Equal(t, apigen.TicketTypeTask, byKey[inTitle.Key].Type)
	assert.Equal(t, apigen.TicketStateFiled, byKey[inTitle.Key].State)
	assert.Equal(t, "Steps.", snippetText(byKey[inTitle.Key]), "a title's hit shows the body's beginning")
	assert.Contains(t, snippetText(byKey[inBody.Key]), "[quokka] sleeps under the desk")
	assert.Equal(t, apigen.SearchFoundInComment, byKey[inComment.Key].FoundIn)
	assert.NotNil(t, byKey[inComment.Key].Comment.MustGet(), "the comment is named")
	assert.Contains(t, snippetText(byKey[inComment.Key]), "a [quokka] was seen near the rack")
	assert.Equal(t, apigen.SearchFoundInQuestion, byKey[inQuestion.Key].FoundIn)
	assert.Equal(t, 1, byKey[inQuestion.Key].Question.MustGet(), "the question is named by its number")
	assert.Contains(t, snippetText(byKey[inQuestion.Key]), "Does the [quokka] bite")
	assert.Equal(t, apigen.SearchFoundInAttachment, byKey[inFile.Key].FoundIn)
	assert.Equal(t, "quokka_portrait.png", snippetText(byKey[inFile.Key]), "a file name is searched by its words and is its own snippet")
	assert.False(t, byKey[inFile.Key].Comment.IsSpecified() && !byKey[inFile.Key].Comment.IsNull(), "no comment for a file")

	// A key by its beginning, the tenant's slug with it or not; above every text.
	keyHits := e.search(t, member, e.SlugA, "alpha-"+strconv.Itoa(inTitle.Number), "")
	require.NotEmpty(t, keyHits.Items)
	assert.Equal(t, inTitle.Key, keyHits.Items[0].Key)
	assert.Equal(t, apigen.SearchFoundInKey, keyHits.Items[0].FoundIn)
	assert.Equal(t, []string{inBody.Key}, hitKeys(e.search(t, member, e.SlugA, inBody.Key, "")), "the canonical key")
	assert.Empty(t, e.search(t, member, e.SlugA, e.SlugB+"/ALPHA-"+strconv.Itoa(inBody.Number), "").Items,
		"a key that names another tenant names nothing here")

	// A half-typed word of a title by trigram.
	partial := e.search(t, member, e.SlugA, "quokk", "")
	assert.Equal(t, []string{inTitle.Key}, hitKeys(partial))
	assert.Equal(t, apigen.SearchFoundInTicket, partial.Items[0].FoundIn)

	// The cursor walks the same order one hit per page, and belongs to its query.
	var walked []string
	query := "&limit=1"
	for range 10 {
		page := e.search(t, member, e.SlugA, "quokka", query)
		walked = append(walked, hitKeys(page)...)
		next, err := page.NextCursor.Get()
		if err != nil {
			break
		}
		query = "&limit=1&cursor=" + url.QueryEscape(next)
	}
	assert.Equal(t, keys, walked)
	first := e.search(t, member, e.SlugA, "quokka", "&limit=1")
	cursor := url.QueryEscape(first.NextCursor.MustGet())
	assertProblem(t, e.s.do(t, member, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/search?q=desk&cursor="+cursor, nil),
		http.StatusBadRequest, "invalid_cursor")
	assertProblem(t, e.s.do(t, member, http.MethodGet, "/api/v1/me/search?q=quokka&cursor="+cursor, nil),
		http.StatusBadRequest, "invalid_cursor")

	// Nothing to find, or too much of it.
	body := assertProblem(t, e.s.do(t, member, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/search?q=%20%20", nil),
		http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "query:q", body["errors"].([]any)[0].(map[string]any)["pointer"])
	assertProblem(t, e.s.do(t, member, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/search?q="+strings.Repeat("a", 300), nil),
		http.StatusBadRequest, "validation_failed")
	assertProblem(t, e.s.do(t, member, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/search", nil),
		http.StatusBadRequest, "validation_failed")
	assert.Empty(t, e.search(t, member, e.SlugA, "!!!", "").Items, "a query of no word finds nothing and fails nothing")
}

// docs/adr/0025 D1, docs/adr/0034 D3, docs/adr/0065 D5, docs/adr/0021 D5: a
// hit — its snippet included — never crosses a tenant, a project
// restriction or the confidential rule, whichever text it is found in, and a
// restricted token searches its tenant or its project only.
func TestSearchNeverShowsWhatTheCallerCannotSee(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, member, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB},
		caller{Token: e.tk.Both}
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')",
		e.A, hidden, e.Both))

	open := e.fileIn(t, member, e.SlugA, "ALPHA", task("Zebracorn sighting", func(b *apigen.TicketCreate) {
		b.Body = ptr("Seen once, never again.")
	}))
	restricted := e.fileIn(t, admin, e.SlugA, "HIDDEN", task("Hidden", func(b *apigen.TicketCreate) {
		b.Body = ptr("A zebracorn in the restricted project, and the word narwhalrestricted.")
	}))
	confidential := e.fileIn(t, admin, e.SlugA, "ALPHA", task("Leak", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("credentials in a log")
		b.Body = ptr("The zebracorn credentials leaked.")
	}))
	require.True(t, confidential.Confidential)
	confidentialPath := ticketPath(e.SlugA, "ALPHA", confidential.Number)
	e.send(t, admin, http.StatusCreated, http.MethodPost, confidentialPath+"/comments",
		map[string]any{"body": "Rotate the narwhalcomment key at once."})
	e.send(t, admin, http.StatusCreated, http.MethodPost, confidentialPath+"/questions",
		map[string]any{"question": "Who saw the narwhalquestion dump?", "options": "narwhaloptions"})
	decodeAttachment(t, e.uploadTo(t, admin, confidential, "narwhalfile.png", "image/png", pngBytes, nil, ""))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("Zebracorn in B"))

	for _, c := range []struct {
		name   string
		caller caller
		want   []string
	}{
		{"a member sees the open ticket only", member, []string{open.Key}},
		{"on the restricted project's list", both, []string{open.Key, restricted.Key}},
		{"the administrator sees all of the tenant", admin, []string{open.Key, restricted.Key, confidential.Key}},
	} {
		got := hitKeys(e.search(t, c.caller, e.SlugA, "zebracorn", ""))
		assert.ElementsMatch(t, c.want, got, c.name)
		for _, h := range e.search(t, c.caller, e.SlugA, "zebracorn", "").Items {
			assert.Equal(t, e.SlugA, h.Team.Slug, "a tenant's search finds only the tenant's")
			if h.Key == open.Key {
				assert.Contains(t, snippetText(h), "Seen once, never again", "a snippet is its own ticket's text")
			}
		}
	}
	// What only hidden texts hold finds nothing for the member, in every kind of text.
	for _, word := range []string{"narwhalrestricted", "narwhalcomment", "narwhalquestion", "narwhaloptions", "narwhalfile"} {
		assert.Empty(t, e.search(t, member, e.SlugA, word, "").Items, word)
		assert.Empty(t, e.search(t, member, "", word, "").Items, word)
	}
	assert.Equal(t, apigen.SearchFoundInComment, e.search(t, admin, e.SlugA, "narwhalcomment", "").Items[0].FoundIn,
		"the administrator finds the confidential comment")
	assert.Equal(t, apigen.SearchFoundInAttachment, e.search(t, admin, e.SlugA, "narwhalfile", "").Items[0].FoundIn)
	assert.Empty(t, e.search(t, member, e.SlugA, "ALPHA-"+strconv.Itoa(confidential.Number), "").Items,
		"a confidential ticket is not found by its key")
	assert.Empty(t, e.search(t, member, e.SlugA, "HIDDEN-"+strconv.Itoa(restricted.Number), "").Items,
		"nor a restricted project's by its key")
	assert.Empty(t, e.search(t, member, e.SlugA, "Hidde", "").Items, "nor by trigram")

	// Another tenant is the boundary's 404, and the person-level search is a union of the person's.
	assertProblem(t, e.s.do(t, memberB, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/search?q=zebracorn", nil),
		http.StatusNotFound, "not_found")
	assert.Equal(t, []string{inB.Key}, hitKeys(e.search(t, memberB, "", "zebracorn", "")))
	union := e.search(t, both, "", "zebracorn", "")
	assert.ElementsMatch(t, []string{open.Key, restricted.Key, inB.Key}, hitKeys(union))
	for _, h := range union.Items {
		assert.True(t, strings.HasPrefix(h.Key, h.Team.Slug+"/"), "each hit names its own tenant")
	}
	assert.Equal(t, []string{inB.Key}, hitKeys(e.search(t, both, "", "zebracorn", "&tenant="+e.SlugB)))
	assertProblem(t, e.s.do(t, memberB, http.MethodGet, "/api/v1/me/search?q=zebracorn&tenant="+e.SlugA, nil),
		http.StatusNotFound, "not_found")
	walked := []string{}
	query := "&limit=1"
	for range 10 {
		page := e.search(t, both, "", "zebracorn", query)
		walked = append(walked, hitKeys(page)...)
		next, err := page.NextCursor.Get()
		if err != nil {
			break
		}
		query = "&limit=1&cursor=" + url.QueryEscape(next)
	}
	assert.Equal(t, hitKeys(union), walked, "the union's cursor walks its order across tenants")

	// A restricted token searches its tenant, or its project, alone.
	tenantToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})
	require.NoError(t, err)
	assert.Equal(t, []string{inB.Key}, hitKeys(e.search(t, caller{Token: tenantToken}, "", "zebracorn", "")))
	projectToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.A, ProjectID: e.ProjectA})
	require.NoError(t, err)
	assert.Equal(t, []string{open.Key}, hitKeys(e.search(t, caller{Token: projectToken}, e.SlugA, "zebracorn", "")))
	assert.Equal(t, []string{open.Key}, hitKeys(e.search(t, caller{Token: projectToken}, "", "zebracorn", "")))

	// An assignee joins the confidential ticket's circle, and finds it; the
	// admission is a session's act (docs/adr/0035 D5).
	etag := strconv.Quote(strconv.Itoa(confidential.Version))
	e.send(t, sessionOf(t, e.AdminA), http.StatusOK, http.MethodPatch, confidentialPath, map[string]any{"assignee": e.MemberA}, "If-Match", etag)
	assert.ElementsMatch(t, []string{open.Key, confidential.Key}, hitKeys(e.search(t, member, e.SlugA, "zebracorn", "")))
}
