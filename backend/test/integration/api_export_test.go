//go:build integration

package integration

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
)

// docs/adr/0044 D1, D5: the canonical Markdown with its ETag, every call
// recorded, behind the ticket's predicate; the state review and the three
// progress stages, and the note of the done act the stages made
// (docs/adr/0009 D5, docs/adr/0017 D2).
func TestMarkdownExport(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Ship the export", func(b *apigen.TicketCreate) {
		b.Body, b.Assignee = ptr("## Current state\n\nNothing yet."), &e.MemberA
	}))
	e.ask(t, member, tk, apigen.QuestionCreate{Question: "Which format?", Recommendation: ptr("v1")})
	decodeAttachment(t, e.uploadTo(t, member, tk, "notes.txt", "text/plain", []byte("plain notes\n"), nil, ""))
	tk = e.staged(t, member, e.walk(t, member, tk, toAnalysed, toDecided, toInProgress, toReview), 100, 100, 50)
	path := fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), tk.Number)

	inReview := e.s.do(t, member, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, inReview.StatusCode)
	raw, err := io.ReadAll(inReview.Body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "state: review\n")
	assert.Contains(t, string(raw), "progress-refinement: 100\nprogress: 100\nprogress-review: 50\n")
	closed := e.patch(t, member, tk, apigen.TicketPatch{ProgressReview: ptr(100), Note: ptr("go test ./... passed")})
	require.Equal(t, http.StatusOK, closed.StatusCode(), string(closed.Body))
	tk = *closed.JSON200

	res := e.s.do(t, member, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/markdown; charset=utf-8", res.Header.Get("Content-Type"))
	assert.Equal(t, fmt.Sprintf(`"%d"`, tk.Version), res.Header.Get("ETag"))
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	raw, err = io.ReadAll(res.Body)
	require.NoError(t, err)
	doc := string(raw)
	today := time.Now().UTC().Format(time.DateOnly)
	var username string
	require.NoError(t, f.QueryRow(e.ctx, "SELECT username FROM users WHERE id = $1", e.MemberA).Scan(&username))
	for _, line := range []string{
		"key: " + tk.Key, "title: Ship the export", "type: task", "state: done",
		"\nassignee: \"member-a <local:" + username + ">\"\n",
		"progress-refinement: 100\nprogress: 100\nprogress-review: 100\n",
		"opened: " + today, "done: " + today, "shipped: go test ./... passed", "attachments:\n  - notes.txt",
		"## Current state\n\nNothing yet.", "### Q1: Which format?", "**Recommendation:** v1", "**Answer:** _open_",
	} {
		assert.Contains(t, doc, line)
	}
	assert.True(t, strings.HasPrefix(doc, "---\nkey: "), doc)

	again := e.s.do(t, member, http.MethodGet, path, nil, "If-None-Match", res.Header.Get("ETag"))
	assert.Equal(t, http.StatusOK, again.StatusCode, "the document is never answered 304")
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'exported'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n, "one act per call")

	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	secretPath := fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), secret.Number)
	assertProblem(t, e.s.do(t, caller{Token: e.tk.ViewerA}, http.MethodGet, secretPath, nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	assert.Equal(t, http.StatusOK, e.s.do(t, caller{Token: e.tk.ViewerA}, http.MethodGet, path, nil).StatusCode, "any role reads")
}

// docs/adr/0044 D1: the export writes a person of the identity provider as
// `Name <oidc:<issuer>#<subject>>`, the stable key of docs/adr/0029 D5, read
// from the person's own row.
func TestMarkdownExportWritesAPersonOfTheIdentityProvider(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	yes := true
	name := uniqueSlug("ada")
	person := providerPerson(t, f, "https://login.example.com/dex", name+"@example.com", &yes, nil)
	require.NoError(t, f.Member(e.ctx, e.A, person, domain.RoleMember))
	tk := e.file(t, member, "ALPHA", task("Assigned to a person of the provider", func(b *apigen.TicketCreate) {
		b.Assignee = &person
	}))

	res := e.s.do(t, member, http.MethodGet, fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), tk.Number), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\nassignee: \""+name+" <oidc:https://login.example.com/dex#sub-"+person.String()+">\"\n")
}
