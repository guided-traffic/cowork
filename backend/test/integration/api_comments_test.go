//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

func (e ticketEnv) comment(t *testing.T, c caller, tk apigen.Ticket, text string) apigen.Comment {
	t.Helper()
	params := &apigen.AddCommentParams{}
	if c.Agent != "" {
		params.IdempotencyKey = newKey()
	}
	res, err := e.s.client(t, c).AddCommentWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, params, apigen.CommentWrite{Body: text})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	return *res.JSON201
}

func (e ticketEnv) commentPath(tk apigen.Ticket, id uuid.UUID) string {
	return fmt.Sprintf("%s/%d/comments/%s", e.projectTickets(tk.Project), tk.Number, id)
}

func (e ticketEnv) editComment(t *testing.T, c caller, tk apigen.Ticket, cm apigen.Comment, text string) *http.Response {
	t.Helper()
	return e.s.do(t, c, http.MethodPatch, e.commentPath(tk, cm.Id), map[string]any{"body": text}, "If-Match", strconv.Quote(strconv.Itoa(cm.Version)))
}

func (e ticketEnv) thread(t *testing.T, c caller, tk apigen.Ticket, query string) apigen.CommentList {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, fmt.Sprintf("%s/%d/comments?%s", e.projectTickets(tk.Project), tk.Number, query), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	var list apigen.CommentList
	require.NoError(t, json.NewDecoder(res.Body).Decode(&list))
	return list
}

func (e ticketEnv) activity(t *testing.T, c caller, tk apigen.Ticket) (apigen.ActivityList, string) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, fmt.Sprintf("%s/%d/activity", e.projectTickets(tk.Project), tk.Number), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var list apigen.ActivityList
	require.NoError(t, json.Unmarshal(raw, &list))
	return list, string(raw)
}

func bodies(list apigen.CommentList) []string {
	out := make([]string, 0, len(list.Items))
	for _, c := range list.Items {
		out = append(out, c.Body.MustGet())
	}
	return out
}

// docs/adr/0015 D1, D3: a flat thread, oldest first or reversed; an edit keeps
// the previous text; withdrawal hides the text everywhere and keeps the entry.
func TestCommentThread(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Discussed"))
	first := e.comment(t, member, tk, "first")
	e.comment(t, member, tk, "second")
	e.comment(t, member, tk, "third")

	assert.Equal(t, []string{"first", "second", "third"}, bodies(e.thread(t, member, tk, "")))
	assert.Equal(t, []string{"third", "second", "first"}, bodies(e.thread(t, member, tk, "order=desc")))
	page := e.thread(t, member, tk, "limit=2")
	assert.Equal(t, []string{"first", "second"}, bodies(page))
	rest := e.thread(t, member, tk, "limit=2&cursor="+page.NextCursor.MustGet())
	assert.Equal(t, []string{"third"}, bodies(rest))
	assertProblem(t, e.s.do(t, member, http.MethodGet, fmt.Sprintf("%s/%d/comments?order=desc&cursor=%s", e.projectTickets("ALPHA"), tk.Number,
		page.NextCursor.MustGet()), nil), http.StatusBadRequest, "invalid_cursor")

	res := e.editComment(t, member, tk, first, "first, corrected")
	require.Equal(t, http.StatusOK, res.StatusCode)
	var edited apigen.Comment
	require.NoError(t, json.NewDecoder(res.Body).Decode(&edited))
	assert.True(t, edited.Edited)
	revs, err := e.s.client(t, member).ListCommentRevisionsWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, first.Id, &apigen.ListCommentRevisionsParams{})
	require.NoError(t, err)
	require.Len(t, revs.JSON200.Items, 1)
	assert.Equal(t, "first", revs.JSON200.Items[0].Body)

	w := e.s.do(t, member, http.MethodPut, e.commentPath(tk, first.Id)+"/withdrawal", nil)
	require.Equal(t, http.StatusOK, w.StatusCode)
	var withdrawn apigen.Comment
	require.NoError(t, json.NewDecoder(w.Body).Decode(&withdrawn))
	assert.True(t, withdrawn.Withdrawn)
	assert.True(t, withdrawn.Body.IsNull())
	assert.Equal(t, http.StatusOK, e.s.do(t, member, http.MethodPut, e.commentPath(tk, first.Id)+"/withdrawal", nil).StatusCode, "idempotent")
	assert.Equal(t, http.StatusConflict, e.editComment(t, member, tk, withdrawn, "back").StatusCode, "a withdrawn comment is not edited")

	thread := e.thread(t, member, tk, "")
	assert.Len(t, thread.Items, 3, "the entry stays")
	assert.True(t, thread.Items[0].Body.IsNull())
	revs, err = e.s.client(t, member).ListCommentRevisionsWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, first.Id, &apigen.ListCommentRevisionsParams{})
	require.NoError(t, err)
	assert.Empty(t, revs.JSON200.Items, "no history once withdrawn")
	_, raw := e.activity(t, member, tk)
	assert.NotContains(t, raw, "first, corrected")
	assert.NotContains(t, raw, `"first"`)
}

// docs/adr/0015 D3, D4 with two identities, a plain and a flagged token each.
func TestWhoChangesAComment(t *testing.T) {
	e := newTicketEnv(t)
	member, both, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}, caller{Token: e.tk.AdminA}
	memberAgent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/a"}
	f := fixtures(t)
	bothToken, _, err := f.Token(e.ctx, fixtureAgent(e.Both))
	require.NoError(t, err)
	bothAgent := caller{Token: bothToken, Agent: "claude-code/opus/b"}
	tk := e.file(t, member, "ALPHA", task("Edits"))

	byMember := e.comment(t, member, tk, "by the member")
	byMemberAgent := e.comment(t, memberAgent, tk, "by the member's agent")
	byBoth := e.comment(t, both, tk, "by both")
	assert.Equal(t, "claude-code/opus/a", byMemberAgent.Agent.MustGet())

	for _, c := range []struct {
		name    string
		who     caller
		comment apigen.Comment
		status  int
	}{
		{"a person edits their own", member, byMember, http.StatusOK},
		{"a person edits their agent's", member, byMemberAgent, http.StatusOK},
		{"a person does not edit another's", member, byBoth, http.StatusForbidden},
		{"an agent does not edit its person's own", memberAgent, byMember, http.StatusForbidden},
		{"an agent does not edit another person's agent's", bothAgent, byMemberAgent, http.StatusForbidden},
		{"an administrator never edits", admin, byBoth, http.StatusForbidden},
	} {
		cur := e.s.do(t, member, http.MethodGet, e.commentPath(tk, c.comment.Id), nil)
		var now apigen.Comment
		require.NoError(t, json.NewDecoder(cur.Body).Decode(&now))
		res := e.editComment(t, c.who, tk, now, "edited: "+c.name)
		assert.Equal(t, c.status, res.StatusCode, c.name)
	}
	res := e.editComment(t, memberAgent, tk, e.getComment(t, member, tk, byMemberAgent.Id), "the agent edits its own kind")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, http.StatusForbidden, e.s.do(t, both, http.MethodPut, e.commentPath(tk, byMember.Id)+"/withdrawal", nil).StatusCode)
	assertProblem(t, e.s.do(t, caller{Token: e.tk.AdminAWrite}, http.MethodPut, e.commentPath(tk, byBoth.Id)+"/withdrawal", nil),
		http.StatusForbidden, "insufficient_scope")
	assert.Equal(t, http.StatusOK, e.s.do(t, admin, http.MethodPut, e.commentPath(tk, byBoth.Id)+"/withdrawal", nil).StatusCode,
		"an administrator withdraws, with admin scope")
	assert.Equal(t, http.StatusForbidden, e.s.do(t, caller{Token: e.tk.ViewerA}, http.MethodPost,
		fmt.Sprintf("%s/%d/comments", e.projectTickets("ALPHA"), tk.Number), map[string]any{"body": "viewer"}).StatusCode)
}

func (e ticketEnv) getComment(t *testing.T, c caller, tk apigen.Ticket, id uuid.UUID) apigen.Comment {
	t.Helper()
	res, err := e.s.client(t, c).GetCommentWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode())
	return *res.JSON200
}

// docs/adr/0015 D2: an act points at the comment written with it, and the
// comment names the acts it explains.
func TestExplainingComments(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Explained"))

	res := e.move(t, member, tk, apigen.Transition{From: tk.State, To: apigen.TicketStateAnalysed, Comment: ptr("analysed: the export path")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	patched := e.patch(t, member, tk, apigen.TicketPatch{Severity: ptr(apigen.SeverityHigh), Comment: ptr("customers hit it daily")})
	require.Equal(t, http.StatusOK, patched.StatusCode(), string(patched.Body))
	etag := strconv.Quote(strconv.Itoa(patched.JSON200.Version))
	body, err := e.s.client(t, member).ReplaceTicketBodyWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number,
		&apigen.ReplaceTicketBodyParams{IfMatch: &etag}, apigen.TicketBodyReplace{Body: "## Current state", Comment: ptr("rewrote the summary")})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, body.StatusCode(), string(body.Body))

	thread := e.thread(t, member, tk, "")
	require.Len(t, thread.Items, 3)
	for i, want := range []apigen.AuditAction{apigen.AuditActionTransitioned, apigen.AuditActionUpdated, apigen.AuditActionUpdated} {
		assert.Equal(t, []apigen.AuditAction{want}, thread.Items[i].Explains, thread.Items[i].Body.MustGet())
	}
	list, _ := e.activity(t, member, tk)
	explained := map[apigen.AuditAction]int{}
	for _, a := range list.Items {
		if !a.ExplainedByComment.IsNull() {
			explained[a.Action]++
		}
	}
	assert.Equal(t, map[apigen.AuditAction]int{apigen.AuditActionTransitioned: 1, apigen.AuditActionUpdated: 2}, explained)
}

// docs/adr/0015 D6: the activity is the record without time entries and
// without data leaving the system; an act naming a ticket the reader cannot
// see shows without its payload (docs/adr/0065 D4).
func TestActivityProjection(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	tk := e.file(t, member, "ALPHA", task("Visible"))
	secret := e.file(t, member, "ALPHA", task("Hidden source", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	require.Equal(t, http.StatusCreated, e.link(t, member, secret, apigen.LinkTypeBlocks, tk).StatusCode)
	for _, row := range []struct{ entity, action string }{{"attachment", "downloaded"}, {"ticket", "exported"}, {"time_entry", "booked"}} {
		require.NoError(t, f.Exec(e.ctx, `INSERT INTO audit_events (tenant_id, actor_user_id, entity_type, ticket_id, action)
			VALUES ($1, $2, $3, $4, $5)`, e.A, e.MemberA, row.entity, tk.Id, row.action))
	}

	list, raw := e.activity(t, member, tk)
	actions := make([]apigen.AuditAction, 0, len(list.Items))
	for _, a := range list.Items {
		actions = append(actions, a.Action)
	}
	assert.Equal(t, []apigen.AuditAction{apigen.AuditActionCreated, apigen.AuditActionLinked}, actions)
	assert.Contains(t, raw, secret.Key, "a reader who sees both ends reads the link")
	vlist, vraw := e.activity(t, viewer, tk)
	require.Len(t, vlist.Items, 2)
	assert.True(t, vlist.Items[1].Redacted)
	assert.True(t, vlist.Items[1].After.IsNull())
	assert.NotContains(t, vraw, secret.Key, "the hidden end's key never reaches the viewer")
	assertProblem(t, e.s.do(t, viewer, http.MethodGet, fmt.Sprintf("%s/%d/activity", e.projectTickets("ALPHA"), secret.Number), nil),
		http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, fmt.Sprintf("%s/%d/comments", e.projectTickets("ALPHA"), tk.Number), nil),
		http.StatusNotFound, "not_found")

	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	key := newKey()
	for range 2 {
		res, err := e.s.client(t, agent).AddCommentWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, &apigen.AddCommentParams{IdempotencyKey: key},
			apigen.CommentWrite{Body: "once"})
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, res.StatusCode())
	}
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM comments WHERE ticket_id = $1 AND body = 'once'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "a keyed comment is written once")
}
