//go:build integration

package integration

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

func (e ticketEnv) contextOf(t *testing.T, c caller, tk apigen.Ticket, query string) (*http.Response, string) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, fmt.Sprintf("%s/%d/context%s", e.projectTickets(tk.Project), tk.Number, query), nil)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(raw)
}

// docs/adr/0044 D2, D5: the canonical document under its first line, then
// the links, the prerequisite tree, the last comments, the attachments and
// the last acts — what the caller cannot see absent — and every call
// recorded.
func TestTicketContext(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	agent := caller{Token: e.tk.AgentA, Agent: agentHeader}
	tk := e.file(t, member, "ALPHA", task("Ship the context", func(b *apigen.TicketCreate) {
		b.Body, b.Assignee = ptr("## Current state\n\nHalf done."), &e.MemberA
	}))
	blocker := e.file(t, member, "ALPHA", task("Write the queries", func(b *apigen.TicketCreate) { b.Assignee = &e.AdminA }))
	root := e.file(t, member, "ALPHA", task("Pick the format"))
	secret := e.file(t, caller{Token: e.tk.AdminA}, "ALPHA", task("Hidden prerequisite", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	related := e.file(t, member, "ALPHA", task("Related work"))
	for _, l := range []struct{ from, to apigen.Ticket }{{blocker, tk}, {root, blocker}, {secret, tk}} {
		res := e.link(t, caller{Token: e.tk.AdminA}, l.from, apigen.LinkTypeBlocks, l.to)
		require.Contains(t, []int{http.StatusCreated, http.StatusOK}, res.StatusCode)
	}
	require.Equal(t, http.StatusCreated, e.link(t, member, tk, apigen.LinkTypeRelatesTo, related).StatusCode)
	e.walk(t, member, root, toAnalysed, toDecided, toInProgress)
	e.comment(t, member, tk, "First thoughts.\n\n## Links\nnot a section")
	e.comment(t, agent, tk, "Recorded by the agent.")
	decodeAttachment(t, e.uploadTo(t, member, tk, "notes.txt", "text/plain", []byte("plain notes\n"), nil, ""))

	res, doc := e.contextOf(t, member, tk, "")
	require.Equal(t, http.StatusOK, res.StatusCode, doc)
	assert.Equal(t, "text/markdown; charset=utf-8", res.Header.Get("Content-Type"))
	assert.Empty(t, res.Header.Get("ETag"), "the context is not one entity")
	first, rest, _ := strings.Cut(doc, "\n")
	assert.Regexp(t, `^<!-- cowork: context of `+tk.Key+`, exported \S+ by \S.* — not an import format -->$`, first)
	assert.True(t, strings.HasPrefix(rest, "---\nkey: "+tk.Key+"\n"), "the canonical document follows")

	for _, want := range []string{
		"## Links\n\n",
		"- blocked by " + blocker.Key + " — Write the queries (filed, ",
		"- relates to " + related.Key + " — Related work (filed, unassigned)",
		"## Prerequisites\n\n2 of 2 open.\n\n- " + blocker.Key + " — Write the queries (filed, ",
		"  - " + root.Key + " — Pick the format (in-progress, unassigned, 0%)",
		"## Recent comments\n",
		"> First thoughts.\n>\n> ## Links\n> not a section\n",
		" via " + agentHeader + ", ",
		"> Recorded by the agent.\n",
		"## Attachments\n\n- notes.txt — text/plain; charset=utf-8, 12 B — /api/v1/teams/" + e.SlugA + "/projects/ALPHA/tickets/",
		"## Recent activity\n\n",
		"commented",
	} {
		assert.Contains(t, doc, want)
	}
	assert.NotContains(t, doc, secret.Key, "a confidential prerequisite the member cannot see is absent")
	assert.NotContains(t, doc, "Hidden prerequisite")
	sections := []string{"## Open questions", "## Links", "## Prerequisites", "## Recent comments", "## Attachments", "## Recent activity"}
	last := -1
	for _, s := range sections {
		i := strings.LastIndex(doc, "\n"+s+"\n")
		require.Positive(t, i, s)
		assert.Greater(t, i, last, "%s in its place", s)
		last = i
	}

	_, admins := e.contextOf(t, caller{Token: e.tk.AdminA}, tk, "")
	assert.Contains(t, admins, secret.Key, "an administrator sees the confidential prerequisite")

	_, short := e.contextOf(t, member, tk, "?comments=0&activity=0")
	assert.NotContains(t, short, "## Recent comments")
	assert.NotContains(t, short, "## Recent activity")
	_, one := e.contextOf(t, member, tk, "?comments=1&activity=1")
	assert.NotContains(t, one, "First thoughts", "only the last comment")
	assert.Contains(t, one, "Recorded by the agent.")

	_, viaAgent := e.contextOf(t, agent, tk, "")
	assert.Contains(t, strings.SplitN(viaAgent, "\n", 2)[0], "(via "+agentHeader+")")

	n, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'exported' AND after->>'format' = 'context v1'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 5, n, "every call is recorded")
	_, again := e.contextOf(t, member, tk, "")
	assert.NotContains(t, again[strings.Index(again, "## Recent activity"):], "exported", "the exports themselves are not activity")

	path := func(x apigen.Ticket) string {
		return fmt.Sprintf("%s/%d/context", e.projectTickets(x.Project), x.Number)
	}
	assertProblem(t, e.s.do(t, caller{Token: e.tk.ViewerA}, http.MethodGet, path(secret), nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, path(tk), nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, member, http.MethodGet, path(tk)+"?comments=101", nil), http.StatusBadRequest, "validation_failed")
}
