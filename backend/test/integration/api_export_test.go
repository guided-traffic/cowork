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
)

// docs/adr/0044 D1, D5: the canonical Markdown with its ETag, every call
// recorded, behind the ticket's predicate.
func TestMarkdownExport(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Ship the export", func(b *apigen.TicketCreate) {
		b.Body, b.Assignee = ptr("## Current state\n\nNothing yet."), &e.MemberA
	}))
	e.ask(t, member, tk, apigen.QuestionCreate{Question: "Which format?", Recommendation: ptr("v1")})
	decodeAttachment(t, e.uploadTo(t, member, tk, "notes.txt", "text/plain", []byte("plain notes\n"), nil, ""))
	tk = e.walk(t, member, tk, toAnalysed, toDecided, toInProgress, toDone)
	path := fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), tk.Number)

	res := e.s.do(t, member, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/markdown; charset=utf-8", res.Header.Get("Content-Type"))
	assert.Equal(t, fmt.Sprintf(`"%d"`, tk.Version), res.Header.Get("ETag"))
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	doc := string(raw)
	today := time.Now().UTC().Format(time.DateOnly)
	for _, line := range []string{
		"key: " + tk.Key, "title: Ship the export", "type: task", "state: done", "progress: 100",
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
	assert.EqualValues(t, 2, n, "one act per call")

	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	secretPath := fmt.Sprintf("%s/%d/markdown", e.projectTickets("ALPHA"), secret.Number)
	assertProblem(t, e.s.do(t, caller{Token: e.tk.ViewerA}, http.MethodGet, secretPath, nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	assert.Equal(t, http.StatusOK, e.s.do(t, caller{Token: e.tk.ViewerA}, http.MethodGet, path, nil).StatusCode, "any role reads")
}
