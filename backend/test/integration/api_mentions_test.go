//go:build integration

package integration

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// docs/adr/0015 D5 and docs/adr/0020 D2 as amended 2026-10-05: a comment names
// the persons it mentions in a list of ids beside its text. Each is told once
// — `mentioned`, not also `commented` — and watches the ticket while the
// comment stands; an edit that adds a person tells that person alone.
func TestAMentionTellsThePersonAndMakesThemAWatcher(t *testing.T) {
	e := newTicketEnv(t)
	admin, member, viewer, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}, caller{Token: e.tk.Both}
	tk := e.fileIn(t, admin, e.SlugA, "ALPHA", task("mentions"))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)

	res := e.s.do(t, member, http.MethodPost, path+"/comments",
		map[string]any{"body": "@Viewer and @Both, please look", "mentions": []uuid.UUID{e.ViewerA, e.Both}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	c := decode[apigen.Comment](t, res)
	assert.ElementsMatch(t, []uuid.UUID{e.ViewerA, e.Both}, c.Mentions)
	// A mention is plain @Name text beside the list of ids: the rendered body
	// shows the text as written, and links nobody (docs/adr/0011 D6).
	html, err := c.BodyHtml.Get()
	require.NoError(t, err)
	assert.Equal(t, "<p>@Viewer and @Both, please look</p>", html)
	assert.Equal(t, []string{"mentioned"}, reasonsAbout(e.inbox(t, viewer, ""), tk.Key), "told once, that they are mentioned")
	assert.Equal(t, []string{"mentioned"}, reasonsAbout(e.inbox(t, both, ""), tk.Key))
	assert.Equal(t, []string{"commented"}, reasonsAbout(e.inbox(t, admin, ""), tk.Key), "the reporter watches and hears the comment")
	assert.Empty(t, reasonsAbout(e.inbox(t, member, ""), tk.Key), "the author is told nothing")

	// A mentioned person watches: the next act on the ticket tells them.
	e.send(t, admin, http.StatusOK, http.MethodPost, path+"/transitions", map[string]any{"from": "filed", "to": "analysed"})
	assert.Equal(t, []string{"state_changed", "mentioned"}, reasonsAbout(e.inbox(t, viewer, ""), tk.Key))

	// An edit that keeps them tells nobody again; one that adds a person tells
	// that person; one that leaves the list out keeps it.
	commentPath := path + "/comments/" + c.Id.String()
	edit := func(c caller, version int, body map[string]any) apigen.Comment {
		t.Helper()
		res := e.s.do(t, c, http.MethodPatch, commentPath, body, "If-Match", strconv.Quote(strconv.Itoa(version)))
		require.Equal(t, http.StatusOK, res.StatusCode, "%v", res.Status)
		return decode[apigen.Comment](t, res)
	}
	c = edit(member, c.Version, map[string]any{"body": "@Viewer, @Both and @Admin, please look",
		"mentions": []uuid.UUID{e.ViewerA, e.Both, e.AdminA}})
	assert.Equal(t, []string{"mentioned", "commented"}, reasonsAbout(e.inbox(t, admin, ""), tk.Key), "the person the edit adds")
	assert.Equal(t, []string{"state_changed", "mentioned"}, reasonsAbout(e.inbox(t, viewer, ""), tk.Key), "nothing again")
	c = edit(member, c.Version, map[string]any{"body": "@Viewer, @Both and @Admin, please look again"})
	assert.ElementsMatch(t, []uuid.UUID{e.ViewerA, e.Both, e.AdminA}, c.Mentions, "an edit without the list keeps it")
	assert.Equal(t, "<p>@Viewer, @Both and @Admin, please look again</p>", c.BodyHtml.MustGet(), "the edit's text is rendered")

	// A person the list drops, and every person once the comment is withdrawn,
	// watches by it no more.
	c = edit(member, c.Version, map[string]any{"body": "@Both only", "mentions": []uuid.UUID{e.Both}})
	e.send(t, admin, http.StatusOK, http.MethodPost, path+"/transitions", map[string]any{"from": "analysed", "to": "decided"})
	assert.Equal(t, []string{"state_changed", "mentioned"}, reasonsAbout(e.inbox(t, viewer, ""), tk.Key), "dropped: no longer told")
	assert.Equal(t, []string{"state_changed", "state_changed", "mentioned"}, reasonsAbout(e.inbox(t, both, ""), tk.Key))
	e.send(t, member, http.StatusOK, http.MethodPut, commentPath+"/withdrawal", nil)
	withdrawn := decode[apigen.Comment](t, e.s.do(t, member, http.MethodGet, commentPath, nil))
	assert.Empty(t, withdrawn.Mentions, "a withdrawn comment shows no mentions")
	assert.True(t, withdrawn.BodyHtml.IsNull(), "nor any text")
	e.send(t, admin, http.StatusOK, http.MethodPost, path+"/transitions", map[string]any{"from": "decided", "to": "in-progress"})
	assert.Len(t, reasonsAbout(e.inbox(t, both, ""), tk.Key), 3, "withdrawn: no longer told")
}

// docs/adr/0015 D5 as amended 2026-10-05: each mention is checked like a
// question's asked_of — a member of the tenant who sees the ticket — and a
// refused mention is 400 at its place in the list and tells nobody.
func TestAMentionOfAPersonWhoCannotSeeTheTicketIsRefused(t *testing.T) {
	ctx := context.Background()
	e := newTicketEnv(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}
	tk := e.fileIn(t, admin, e.SlugA, "ALPHA", task("confidential", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	require.NoError(t, fixtures(t).Exec(ctx, `UPDATE tickets SET confidential = true WHERE id = $1`, tk.Id))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)
	before, err := fixtures(t).QueryCount(ctx, `SELECT count(*) FROM notifications WHERE ticket_id = $1`, tk.Id)
	require.NoError(t, err)

	for _, refused := range []struct {
		why      string
		mentions []uuid.UUID
		pointer  string
	}{
		{"a viewer of the tenant who cannot see the confidential ticket", []uuid.UUID{e.AdminA, e.ViewerA}, "/mentions/1"},
		{"a member of another tenant only", []uuid.UUID{e.MemberB}, "/mentions/0"},
		{"nobody at all", []uuid.UUID{uuid.Must(uuid.NewV7())}, "/mentions/0"},
	} {
		res := e.s.do(t, member, http.MethodPost, path+"/comments", map[string]any{"body": "look", "mentions": refused.mentions})
		body := assertProblem(t, res, http.StatusBadRequest, "validation_failed")
		fields, _ := body["errors"].([]any)
		require.Len(t, fields, 1, refused.why)
		assert.Equal(t, refused.pointer, fields[0].(map[string]any)["pointer"], refused.why)
	}
	after, err := fixtures(t).QueryCount(ctx, `SELECT count(*) FROM notifications WHERE ticket_id = $1`, tk.Id)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a refused mention tells nobody")
	comments, err := fixtures(t).QueryCount(ctx, `SELECT count(*) FROM comments WHERE ticket_id = $1`, tk.Id)
	require.NoError(t, err)
	assert.Zero(t, comments, "nor writes the comment")

	// An edit is checked for the persons it adds.
	res := e.s.do(t, member, http.MethodPost, path+"/comments", map[string]any{"body": "fine", "mentions": []uuid.UUID{e.AdminA}})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	c := decode[apigen.Comment](t, res)
	res = e.s.do(t, member, http.MethodPatch, path+"/comments/"+c.Id.String(),
		map[string]any{"body": "fine", "mentions": []uuid.UUID{e.AdminA, e.ViewerA}}, "If-Match", strconv.Quote(strconv.Itoa(c.Version)))
	assertProblem(t, res, http.StatusBadRequest, "validation_failed")

	// The list is unique and bounded by the document.
	res = e.s.do(t, member, http.MethodPost, path+"/comments", map[string]any{"body": "twice", "mentions": []uuid.UUID{e.AdminA, e.AdminA}})
	assertProblem(t, res, http.StatusBadRequest, "validation_failed")

	// An agent mentions as its person does, and a token's mention shows it.
	agentToken, _, err := fixtures(t).Token(ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true})
	require.NoError(t, err)
	res = e.s.do(t, caller{Token: agentToken, Agent: "claude-code/opus/1"}, http.MethodPost, path+"/comments",
		map[string]any{"body": "found it", "mentions": []uuid.UUID{e.AdminA}}, "Idempotency-Key", uuid.NewString())
	require.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Equal(t, "mentioned", reasonsAbout(e.inbox(t, admin, ""), tk.Key)[0])
}

// docs/adr/0024 D1, D3: a deleted ticket tells nobody of anything, its
// mentions included. A comment on it is a 404 and mentions nobody, and the act
// of a ticket that blocks it tells none of its watchers by mention — each
// recipient is held to their sight of the ticket, which a deletion ends for
// everybody (person_sees_ticket).
func TestAMentionOnADeletedTicketTellsNobody(t *testing.T) {
	e := newTicketEnv(t)
	admin, member, viewer := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	gone := e.fileIn(t, admin, e.SlugA, "ALPHA", task("deleted later"))
	blocker := e.fileIn(t, member, e.SlugA, "ALPHA", task("blocks it"))
	require.Equal(t, http.StatusCreated, e.link(t, member, blocker, apigen.LinkTypeBlocks, gone).StatusCode)
	path := ticketPath(e.SlugA, "ALPHA", gone.Number)
	e.send(t, member, http.StatusCreated, http.MethodPost, path+"/comments",
		map[string]any{"body": "@Viewer, look", "mentions": []uuid.UUID{e.ViewerA}})
	require.Equal(t, []string{"mentioned"}, reasonsAbout(e.inbox(t, viewer, ""), gone.Key))
	told := func() int {
		return scalar[int](t, `SELECT count(*) FROM notifications WHERE user_id = $1`, e.ViewerA)
	}
	before := told()

	e.send(t, admin, http.StatusNoContent, http.MethodDelete, path, nil)
	assertProblem(t, e.s.do(t, member, http.MethodPost, path+"/comments",
		map[string]any{"body": "@Viewer, still?", "mentions": []uuid.UUID{e.ViewerA}}), http.StatusNotFound, "not_found")
	moved := e.move(t, member, blocker, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped,
		Reason: ptr("not needed")})
	require.Equal(t, http.StatusOK, moved.StatusCode(), string(moved.Body))

	assert.Equal(t, before, told(), "neither the comment nor the blocker's close tells the mentioned watcher")
	assert.Empty(t, reasonsAbout(e.inbox(t, viewer, ""), gone.Key), "and the inbox leaves the deleted ticket out")
}
