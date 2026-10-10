//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// ticketPath is a ticket's route in a tenant.
func ticketPath(slug, project string, number int) string {
	return "/api/v1/teams/" + slug + "/projects/" + project + "/tickets/" + strconv.Itoa(number)
}

// fileIn files a ticket as c in the tenant's project and requires the 201.
func (e ticketEnv) fileIn(t *testing.T, c caller, slug, project string, body apigen.TicketCreate) apigen.Ticket {
	t.Helper()
	res := e.s.do(t, c, http.MethodPost, "/api/v1/teams/"+slug+"/projects/"+project+"/tickets", body)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	return decode[apigen.Ticket](t, res)
}

// send sends a write as c and requires the status.
func (e ticketEnv) send(t *testing.T, c caller, status int, method, path string, body any, headers ...string) {
	t.Helper()
	res := e.s.do(t, c, method, path, body, headers...)
	if res.StatusCode != status {
		require.Equal(t, status, res.StatusCode, "%s %s: %v", method, path, problemBody(t, res))
	}
}

// inbox reads the person's inbox as c, with a query.
func (e ticketEnv) inbox(t *testing.T, c caller, query string) apigen.InboxList {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/me/inbox"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	return decode[apigen.InboxList](t, res)
}

// reasonsAbout lists the reasons of the entries about one ticket, newest first.
func reasonsAbout(l apigen.InboxList, key string) []string {
	var out []string
	for _, it := range l.Items {
		if it.Ticket.Key == key {
			out = append(out, string(it.Reason))
		}
	}
	return out
}

// docs/adr/0020 D2, D3: every event of the table creates a notification for
// its recipient, written with the act and rendering from it; the actor and
// the actor's agent are told nothing.
func TestTheEventsOfTheInboxTellTheirRecipients(t *testing.T) {
	e := newTicketEnv(t)
	admin, member, viewer, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}, caller{Token: e.tk.Both}
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/1"}

	// Filed with an assignee: the assignee is told; the filer is not.
	tk := e.fileIn(t, admin, e.SlugA, "ALPHA", task("assigned at filing", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)
	inbox := e.inbox(t, member, "")
	require.Equal(t, []string{"assigned"}, reasonsAbout(inbox, tk.Key))
	entry := inbox.Items[0]
	assert.Equal(t, apigen.TeamRef{Slug: e.SlugA, Name: "Team A"}, entry.Team)
	assert.Equal(t, apigen.AuditActionCreated, entry.Act.Action, "rendered from the filing")
	assert.Equal(t, "assigned at filing", entry.Ticket.Title)
	assert.False(t, entry.Read)
	assert.Equal(t, 1, inbox.Unread)
	assert.Empty(t, e.inbox(t, admin, "").Items, "the actor is told nothing")

	// A stake makes a watcher; a state change tells the watchers — the
	// assignee and the viewer with a stake, not the reporter who moved it.
	e.send(t, viewer, http.StatusCreated, http.MethodPut, path+"/interest", map[string]any{"weight": "watch"})
	e.send(t, admin, http.StatusOK, http.MethodPost, path+"/transitions", map[string]any{"from": "filed", "to": "analysed"})
	assert.Equal(t, []string{"state_changed", "assigned"}, reasonsAbout(e.inbox(t, member, ""), tk.Key))
	viewerInbox := e.inbox(t, viewer, "")
	assert.Equal(t, []string{"state_changed"}, reasonsAbout(viewerInbox, tk.Key))
	assert.Equal(t, apigen.AuditActionTransitioned, viewerInbox.Items[0].Act.Action)
	assert.Empty(t, e.inbox(t, admin, "").Items)

	// A comment tells the watchers; the member's own agent's comment tells the
	// member nothing, the others as any comment does.
	e.send(t, agent, http.StatusCreated, http.MethodPost, path+"/comments", map[string]any{"body": "an agent's note"}, "Idempotency-Key", uuid.NewString())
	assert.Equal(t, []string{"state_changed", "assigned"}, reasonsAbout(e.inbox(t, member, ""), tk.Key), "the person's agent is the person")
	assert.Equal(t, []string{"commented"}, reasonsAbout(e.inbox(t, admin, ""), tk.Key), "the reporter watches")
	commented := e.inbox(t, viewer, "").Items[0]
	assert.Equal(t, "commented", string(commented.Reason))
	assert.False(t, commented.Withdrawn)

	// A question asked of a person tells them; its answer tells the asker.
	e.send(t, admin, http.StatusCreated, http.MethodPost, path+"/questions", map[string]any{"question": "which?", "asked_of": e.Both})
	assert.Equal(t, []string{"asked"}, reasonsAbout(e.inbox(t, both, ""), tk.Key))
	e.send(t, both, http.StatusOK, http.MethodPut, path+"/questions/1/answer", map[string]any{"answer": "this one"})
	assert.Equal(t, []string{"answered", "commented"}, reasonsAbout(e.inbox(t, admin, ""), tk.Key))

	// An urgent stake on an assigned ticket tells the assignee; a watch does not.
	e.send(t, both, http.StatusCreated, http.MethodPut, path+"/interest", map[string]any{"weight": "urgent", "note": "a client waits"})
	assert.Equal(t, "urgent", reasonsAbout(e.inbox(t, member, ""), tk.Key)[0])

	// A reassignment tells the new assignee.
	e.send(t, admin, http.StatusOK, http.MethodPatch, path, map[string]any{"assignee": e.Both},
		"If-Match", strconv.Quote(strconv.Itoa(e.get(t, admin, "ALPHA", tk.Number).JSON200.Version)))
	assert.Equal(t, "assigned", reasonsAbout(e.inbox(t, both, ""), tk.Key)[0])

	// A ticket that blocks a watched one reaches dropped: the watchers of the
	// blocked ticket hear of it, about the ticket they watch, naming the blocker.
	blocker := e.fileIn(t, member, e.SlugA, "ALPHA", task("the blocker"))
	e.send(t, member, http.StatusCreated, http.MethodPut, ticketPath(e.SlugA, "ALPHA", blocker.Number)+"/links/blocks/ALPHA-"+strconv.Itoa(tk.Number), nil)
	e.send(t, member, http.StatusOK, http.MethodPost, ticketPath(e.SlugA, "ALPHA", blocker.Number)+"/transitions",
		map[string]any{"from": "filed", "to": "dropped", "reason": "not needed"})
	viewerInbox = e.inbox(t, viewer, "")
	require.Equal(t, "blocker_closed", string(viewerInbox.Items[0].Reason))
	assert.Equal(t, tk.Key, viewerInbox.Items[0].Ticket.Key, "about the ticket the viewer watches")
	b, err := viewerInbox.Items[0].Blocker.Get()
	require.NoError(t, err)
	assert.Equal(t, blocker.Key, b.Key)
	assert.Equal(t, apigen.TicketStateDropped, b.State)
}

// docs/adr/0020 D1, docs/adr/0021 D5, docs/adr/0023 D2: the inbox is the
// person's across their tenants, each entry naming its tenant, and nothing of
// another person's; a tenant narrows it; what the person no longer sees — a
// confidential ticket, a restricted project, a tenant they left — is absent
// and counts nowhere (docs/adr/0065 D5).
func TestTheInboxIsThePersonsAcrossTheirTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	inA := e.fileIn(t, admin, e.SlugA, "ALPHA", task("in A", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("in B", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))

	all := e.inbox(t, both, "")
	require.Len(t, all.Items, 2)
	assert.Equal(t, 2, all.Unread)
	assert.Equal(t, inB.Key, all.Items[0].Ticket.Key, "newest first, across tenants")
	assert.Equal(t, e.SlugB, all.Items[0].Team.Slug)
	assert.Equal(t, inA.Key, all.Items[1].Ticket.Key)
	assert.Equal(t, e.SlugA, all.Items[1].Team.Slug)
	assert.Empty(t, e.inbox(t, caller{Token: e.tk.MemberA}, "").Items, "another person's notifications")

	narrowed := e.inbox(t, both, "?tenant="+e.SlugA)
	require.Len(t, narrowed.Items, 1)
	assert.Equal(t, inA.Key, narrowed.Items[0].Ticket.Key)
	assert.Equal(t, 1, narrowed.Unread)
	strangerSlug := uniqueSlug("stranger")
	_, err := f.Tenant(e.ctx, strangerSlug, "Stranger")
	require.NoError(t, err)
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/inbox?tenant="+strangerSlug, nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/inbox?tenant=no-such-tenant", nil), http.StatusNotFound, "not_found")

	// One entry per page, the cursor holding the place.
	first := e.inbox(t, both, "?limit=1")
	require.Len(t, first.Items, 1)
	cursor, err := first.NextCursor.Get()
	require.NoError(t, err)
	second := e.inbox(t, both, "?limit=1&cursor="+cursor)
	require.Len(t, second.Items, 1)
	assert.Equal(t, inA.Key, second.Items[0].Ticket.Key)
	assert.True(t, second.NextCursor.IsNull())
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, "/api/v1/me/inbox?limit=1&cursor="+cursor, nil),
		http.StatusBadRequest, "invalid_cursor")

	// A token restricted to tenant B reads B's alone.
	plain, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})
	require.NoError(t, err)
	onlyB := e.inbox(t, caller{Token: plain}, "")
	require.Len(t, onlyB.Items, 1)
	assert.Equal(t, inB.Key, onlyB.Items[0].Ticket.Key)
	assert.Equal(t, 1, onlyB.Unread)

	// A ticket that turns confidential, a project restricted away, a tenant
	// left: gone from the list and from the count.
	watched := e.fileIn(t, admin, e.SlugA, "ALPHA", task("watched"))
	e.send(t, both, http.StatusCreated, http.MethodPut, ticketPath(e.SlugA, "ALPHA", watched.Number)+"/interest", map[string]any{"weight": "watch"})
	e.send(t, admin, http.StatusCreated, http.MethodPost, ticketPath(e.SlugA, "ALPHA", watched.Number)+"/comments", map[string]any{"body": "news"})
	require.Equal(t, []string{"commented"}, reasonsAbout(e.inbox(t, both, ""), watched.Key))
	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET confidential = true WHERE id = $1", watched.Id))
	afterFlag := e.inbox(t, both, "")
	assert.Empty(t, reasonsAbout(afterFlag, watched.Key), "a confidential ticket the person is neither administrator, assignee nor reporter of")
	assert.Equal(t, 2, afterFlag.Unread)

	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	inGamma := e.fileIn(t, admin, e.SlugA, "GAMMA", task("in gamma", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	require.Equal(t, []string{"assigned"}, reasonsAbout(e.inbox(t, both, ""), inGamma.Key))
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", gamma))
	afterRestriction := e.inbox(t, both, "")
	assert.Empty(t, reasonsAbout(afterRestriction, inGamma.Key), "a project restricted away")
	assert.Equal(t, 2, afterRestriction.Unread)

	require.NoError(t, f.Exec(e.ctx, "DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2", e.B, e.Both))
	afterLeaving := e.inbox(t, both, "")
	require.Len(t, afterLeaving.Items, 1, "a tenant the person left")
	assert.Equal(t, inA.Key, afterLeaving.Items[0].Ticket.Key)
	assert.Equal(t, 1, afterLeaving.Unread)
}

// docs/adr/0020 D6, docs/adr/0026 D1: a notification is marked read, one by
// one or every one up to the newest the person saw, each as the act read; a
// marking that changes nothing records nothing; another person's notification
// is not found; a read-scope token may not mark.
func TestMarkingNotificationsRead(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	for i := range 3 {
		e.fileIn(t, admin, e.SlugA, "ALPHA", task("a"+strconv.Itoa(i), func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	}
	e.fileIn(t, memberB, e.SlugB, "BETA", task("b", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	items := e.inbox(t, both, "").Items
	require.Len(t, items, 4)
	reads := func() int {
		return scalar[int](t, "SELECT count(*) FROM audit_events WHERE actor_user_id = $1 AND action = 'read'", e.Both)
	}

	one := items[1].Id.String()
	res := e.s.do(t, both, http.MethodPut, "/api/v1/me/inbox/"+one+"/read", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, 3, decode[apigen.InboxState](t, res).Unread)
	assert.Equal(t, 1, reads())
	res = e.s.do(t, both, http.MethodPut, "/api/v1/me/inbox/"+one+"/read", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, 3, decode[apigen.InboxState](t, res).Unread)
	assert.Equal(t, 1, reads(), "a notification read already records nothing")
	assert.True(t, e.inbox(t, both, "").Items[1].Read)

	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodPut, "/api/v1/me/inbox/"+one+"/read", nil),
		http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, both, http.MethodPut, "/api/v1/me/inbox/"+uuid.NewString()+"/read", nil), http.StatusNotFound, "not_found")
	reader, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, Scope: "read"})
	require.NoError(t, err)
	assertProblem(t, e.s.do(t, caller{Token: reader}, http.MethodPut, "/api/v1/me/inbox/"+one+"/read", nil),
		http.StatusForbidden, "insufficient_scope")

	// Every one up to the second newest — B's is the newest and stays unread.
	through := map[string]any{"through": items[1].Id}
	res = e.s.do(t, both, http.MethodPut, "/api/v1/me/inbox/read", through)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, 1, decode[apigen.InboxState](t, res).Unread)
	assert.Equal(t, 2, reads(), "one act, in the one tenant where something changed")
	now := e.inbox(t, both, "")
	assert.False(t, now.Items[0].Read)
	assert.Equal(t, e.SlugB, now.Items[0].Team.Slug)
	assert.Equal(t, 1, now.Unread)
	res = e.s.do(t, both, http.MethodPut, "/api/v1/me/inbox/read?tenant="+e.SlugB, map[string]any{"through": now.Items[0].Id})
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, 0, decode[apigen.InboxState](t, res).Unread)
	assert.Equal(t, 3, reads())
}

// docs/adr/0037 D1: the browser marks read with its session, and without the
// CSRF header it does not.
func TestABrowserMarksItsInboxRead(t *testing.T) {
	w := newWorld(t)
	s := newAPI(t, withLogin)
	names := withAccounts(t, w)
	e := ticketEnv{world: w, tk: issueTokens(t, w), s: s, ctx: context.Background()}
	e.fileIn(t, caller{Token: e.tk.AdminA}, e.SlugA, "ALPHA", task("for the browser", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	b := s.browser(t)
	b.mustLogin(names["both"], testPassword)
	list := decode[apigen.InboxList](t, b.get("/api/v1/me/inbox"))
	require.Len(t, list.Items, 1)
	path := "/api/v1/me/inbox/" + list.Items[0].Id.String() + "/read"
	assertProblem(t, b.request(http.MethodPut, path, nil, without("X-Requested-With")), http.StatusForbidden, "csrf")
	res := b.request(http.MethodPut, path, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, 0, decode[apigen.InboxState](t, res).Unread)
}

// docs/adr/0020 D6: a notification read more than ninety days ago goes; one
// read lately and one unread, however old, stay.
func TestReadNotificationsExpire(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	for i := range 3 {
		e.fileIn(t, admin, e.SlugA, "ALPHA", task("t"+strconv.Itoa(i), func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	}
	items := e.inbox(t, caller{Token: e.tk.MemberA}, "").Items
	require.Len(t, items, 3)
	require.NoError(t, f.Exec(e.ctx, "UPDATE notifications SET read_at = now() - interval '91 days' WHERE id = $1", items[0].Id))
	require.NoError(t, f.Exec(e.ctx, "UPDATE notifications SET read_at = now() - interval '89 days' WHERE id = $1", items[1].Id))
	require.NoError(t, f.Exec(e.ctx, "UPDATE notifications SET created_at = now() - interval '400 days' WHERE id = $1", items[2].Id))

	removed, err := openRuntime(t).ExpireNotifications(e.ctx, time.Now())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, removed, int64(1))
	left := e.inbox(t, caller{Token: e.tk.MemberA}, "").Items
	require.Len(t, left, 2)
	assert.Equal(t, items[1].Id, left[0].Id)
	assert.Equal(t, items[2].Id, left[1].Id)
	assert.GreaterOrEqual(t, scalar[int](t, `SELECT count(*) FROM audit_events WHERE actor_system = 'system:notification-expiry'
		AND entity_type = 'notifications' AND action = 'expired'`), 1, "the job records what it removed")
}

// docs/adr/0071 Status, docs/adr/0020 D2: a notification of the reason merged,
// which GitHub's webhook of the releases up to 0.12.0 made, stays in the
// database until a later contract migration but no answer names it — the
// document's reasons no longer hold it: it is out of the list, out of the
// count, and not found when it is marked read.
func TestAMergeNotificationOfTheRemovedWebhookIsLeftOut(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	e.fileIn(t, caller{Token: e.tk.AdminA}, e.SlugA, "ALPHA", task("merged once", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	member := caller{Token: e.tk.MemberA}
	items := e.inbox(t, member, "").Items
	require.Len(t, items, 1)
	var merged uuid.UUID
	require.NoError(t, f.QueryRow(e.ctx, `INSERT INTO notifications (tenant_id, user_id, ticket_id, audit_event_id, reason)
		SELECT tenant_id, user_id, ticket_id, audit_event_id, 'merged' FROM notifications WHERE id = $1 RETURNING id`,
		items[0].Id).Scan(&merged))

	after := e.inbox(t, member, "")
	require.Len(t, after.Items, 1)
	assert.Equal(t, items[0].Id, after.Items[0].Id)
	assert.Equal(t, 1, after.Unread)
	assertProblem(t, e.s.do(t, member, http.MethodPut, "/api/v1/me/inbox/"+merged.String()+"/read", nil),
		http.StatusNotFound, "not_found")
}

// docs/adr/0021 D6: a forgotten filter shows nobody another person's inbox —
// inside the tenant's own transaction, the restrictive policy admits the
// person's own notifications only.
func TestTheInboxPolicyHoldsAPersonToTheirOwn(t *testing.T) {
	e := newTicketEnv(t)
	e.fileIn(t, caller{Token: e.tk.AdminA}, e.SlugA, "ALPHA", task("for the member", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	db := openRuntime(t)
	count := func(person uuid.UUID) int {
		var n int
		require.NoError(t, db.InTenant(as(person), e.A, func(r *store.Reader) error {
			ids, err := r.ListInbox(e.ctx, readq.ListInboxParams{TenantID: e.A, UserID: e.MemberA, PageSize: 10})
			n = len(ids)
			return err
		}))
		return n
	}
	assert.Equal(t, 1, count(e.MemberA))
	assert.Zero(t, count(e.AdminA), "an administrator of the tenant asking for the member's inbox reads nothing")
}

// openMeStream subscribes c to the person-level stream on the tenant.
func (e ticketEnv) openMeStream(t *testing.T, c caller, slug string) *stream {
	t.Helper()
	return e.openStreamAt(t, e.s, c, "/api/v1/teams/"+slug+"/events?me=true", "")
}

// unreadOf reads an inbox.changed's count.
func unreadOf(t *testing.T, m sse) int {
	t.Helper()
	require.Equal(t, "inbox.changed", m.Event, m.Data)
	var d struct {
		Unread *int `json:"unread"`
	}
	require.NoError(t, json.Unmarshal([]byte(m.Data), &d))
	require.NotNil(t, d.Unread)
	return *d.Unread
}

// until reads the stream until a message matches, within five seconds,
// returning what came before.
func (s *stream) until(t *testing.T, match func(sse) bool) (before []sse, found sse) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, ok := s.next(t, time.Until(deadline))
		require.True(t, ok, "the awaited message never came; before it: %v", before)
		if match(m) {
			return before, m
		}
		before = append(before, m)
	}
}

// docs/adr/0054 D1, D2, D3: the person-level stream tells the unread count
// when it opens and when the inbox changes, and carries the events of the
// person's other tenants that each tenant's filter admits, with their ids — a
// question asked of the person or of another — and nothing of a tenant the
// person left, of a project restricted away from them or of a confidential
// ticket they cannot see.
func TestThePersonLevelStream(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	other, err := f.Person(e.ctx, uniqueSlug("other-b"), "Other B")
	require.NoError(t, err)
	require.NoError(t, f.Member(e.ctx, e.B, other, "member"))

	// Before the streams open: questions asked of the person on a ticket of a
	// project then restricted away from them, and on a ticket then made
	// confidential; the asker stays able to edit both.
	hidden, err := f.Project(e.ctx, e.B, "HIDDEN", "Hidden")
	require.NoError(t, err)
	h1 := e.fileIn(t, memberB, e.SlugB, "HIDDEN", task("h1"))
	h1Path := ticketPath(e.SlugB, "HIDDEN", h1.Number)
	e.send(t, memberB, http.StatusCreated, http.MethodPost, h1Path+"/questions", map[string]any{"question": "hidden?", "asked_of": e.Both})
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')",
		e.B, hidden, e.MemberB))
	c1 := e.fileIn(t, memberB, e.SlugB, "BETA", task("c1"))
	c1Path := ticketPath(e.SlugB, "BETA", c1.Number)
	e.send(t, memberB, http.StatusCreated, http.MethodPost, c1Path+"/questions", map[string]any{"question": "secret?", "asked_of": e.Both})
	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET confidential = true WHERE id = $1", c1.Id))

	me := e.openMeStream(t, both, e.SlugA)
	plain := e.openStream(t, e.s, both, e.SlugA, "")
	first, ok := me.next(t, 5*time.Second)
	require.True(t, ok)
	assert.Equal(t, 0, unreadOf(t, first), "the count when the stream opens, without what the person no longer sees")
	assert.Empty(t, first.ID, "a count carries no id")

	// An assignment in B reaches the stream on A as the new count.
	b1 := e.fileIn(t, memberB, e.SlugB, "BETA", task("b1", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	_, m := me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 1, unreadOf(t, m))

	// A question asked of the person in B: its event, without an id, and the count.
	b1Path := ticketPath(e.SlugB, "BETA", b1.Number)
	e.send(t, memberB, http.StatusCreated, http.MethodPost, b1Path+"/questions", map[string]any{"question": "yes?", "asked_of": e.Both})
	_, m = me.until(t, func(m sse) bool { return m.Event == "question.changed" })
	assert.Equal(t, b1.Key, eventKey(t, m))
	assert.NotEmpty(t, m.ID, "every event carries its id: a reconnect replays across the person's tenants")
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 2, unreadOf(t, m))

	// A question asked of another person in B is an event of B like any other.
	e.send(t, memberB, http.StatusCreated, http.MethodPost, b1Path+"/questions", map[string]any{"question": "you?", "asked_of": other})
	_, m = me.until(t, func(m sse) bool { return m.Event == "question.changed" })
	assert.Equal(t, b1.Key, eventKey(t, m))

	// What it must not carry: the acts of questions asked of the person on a
	// ticket of a project restricted away from them and on a confidential
	// ticket. The stream judges its events in order, so a sentinel in A says
	// each was judged while the person still belonged to B.
	e.send(t, memberB, http.StatusOK, http.MethodPatch, h1Path+"/questions/1", map[string]any{"question": "hidden, edited?"}, "If-Match", `"1"`)
	e.send(t, memberB, http.StatusOK, http.MethodPatch, c1Path+"/questions/1", map[string]any{"question": "secret, edited?"}, "If-Match", `"1"`)
	a1 := e.fileIn(t, admin, e.SlugA, "ALPHA", task("a1", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	before, sentinel := me.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, a1.Key, eventKey(t, sentinel))
	assert.NotEmpty(t, sentinel.ID, "the stream's own tenant keeps its ids")
	for _, m := range before {
		assert.NotEqual(t, "question.changed", m.Event, "nothing of a hidden project or a confidential ticket: %v", m)
	}
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 3, unreadOf(t, m), "A's assignment beside B's two")

	// Nor, once the person left B, the act of a question asked of them there: an
	// administrator of B removes their grant, and the stream hears that act —
	// the person's own — and nothing of B after it.
	adminB, err := f.Person(e.ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(e.ctx, e.B, adminB, "admin"))
	adminBToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: adminB, Scope: "admin"})
	require.NoError(t, err)
	e.send(t, caller{Token: adminBToken}, http.StatusNoContent, http.MethodDelete,
		"/api/v1/teams/"+e.SlugB+"/members/"+e.Both.String()+"/grant", nil)
	_, m = me.until(t, func(m sse) bool { return m.Event == "membership.changed" })
	assert.JSONEq(t, `{"team":"`+e.SlugB+`","tenant":"`+e.SlugB+`","person_id":"`+e.Both.String()+`"}`, m.Data,
		"tenant repeats team for one release (docs/adr/0005 D1)")
	e.send(t, memberB, http.StatusOK, http.MethodPatch, b1Path+"/questions/1", map[string]any{"question": "after leaving?"}, "If-Match", `"1"`)
	// Nor a later act of B that names the person: the removal of an access
	// entry they left behind.
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) SELECT tenant_id, id, $2, 'member' FROM projects WHERE tenant_id = $1 AND key = 'BETA'",
		e.B, e.Both))
	e.send(t, caller{Token: adminBToken}, http.StatusNoContent, http.MethodDelete,
		"/api/v1/teams/"+e.SlugB+"/projects/BETA/access/"+e.Both.String(), nil)
	a2 := e.fileIn(t, admin, e.SlugA, "ALPHA", task("a2", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	before, sentinel = me.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, a2.Key, eventKey(t, sentinel))
	for _, m := range before {
		assert.NotEqual(t, "question.changed", m.Event, "nothing of a tenant left: %v", m)
		assert.NotEqual(t, "membership.changed", m.Event, "nothing of a tenant left: %v", m)
	}
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 2, unreadOf(t, m), "A's two; B's are the tenant's the person left")

	plainBefore, _ := plain.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	for _, m := range plainBefore {
		assert.NotEqual(t, "inbox.changed", m.Event, "a stream without me tells no count")
		assert.NotEqual(t, "question.changed", m.Event, "nor another tenant's question")
	}
}

// docs/adr/0054 D1, D3: a person-level stream recomputes what it admits of its
// tenant on the act that changes it, as any stream does, and goes on telling
// its person's own events: a ticket filed in a project created after it opened
// arrives at once, with the count it changes, and a question asked of the
// person in another tenant still arrives, with its id; the heartbeat, an hour
// here, plays no part.
func TestAPersonLevelStreamRefiltersAndKeepsItsPersonsEvents(t *testing.T) {
	e := newTicketEnv(t)
	srv := newAPI(t, func(o *api.Options) { o.Heartbeat = time.Hour })
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	me := e.openStreamAt(t, srv, both, "/api/v1/teams/"+e.SlugA+"/events?me=true", "")
	first, ok := me.next(t, 5*time.Second)
	require.True(t, ok)
	assert.Equal(t, 0, unreadOf(t, first))

	res := srv.do(t, admin, http.MethodPost, "/api/v1/teams/"+e.SlugA+"/projects", map[string]any{"key": "LATE", "name": "Late"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	start := time.Now()
	late := e.fileIn(t, admin, e.SlugA, "LATE", task("late", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	_, m := me.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, late.Key, eventKey(t, m), "the ticket of a project created after the stream opened")
	assert.Less(t, time.Since(start), time.Second)
	assert.NotEmpty(t, m.ID)
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 1, unreadOf(t, m))

	b1 := e.fileIn(t, memberB, e.SlugB, "BETA", task("b1"))
	b1Path := ticketPath(e.SlugB, "BETA", b1.Number)
	e.send(t, memberB, http.StatusCreated, http.MethodPost, b1Path+"/questions", map[string]any{"question": "yes?", "asked_of": e.Both})
	_, m = me.until(t, func(m sse) bool { return m.Event == "question.changed" })
	assert.Equal(t, b1.Key, eventKey(t, m))
	assert.NotEmpty(t, m.ID, "every event carries its id")
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 2, unreadOf(t, m))
}

// docs/adr/0054 D1, D3, D5: the person-level stream opened on tenant A carries
// the events of B that B's filter admits — a ticket assigned to the person
// there within a second, with an hour's heartbeat — and follows the person
// into a tenant they are granted a role in at once; it carries nothing of a
// project of B hidden from them or a confidential ticket of B they cannot see;
// and a reconnect with the id of an event of A replays what B published
// meanwhile.
func TestThePersonLevelStreamSpansThePersonsTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	srv := newAPI(t, withLogin, func(o *api.Options) { o.Heartbeat = time.Hour })
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	hidden, err := f.Project(e.ctx, e.B, "HIDDEN", "Hidden")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')",
		e.B, hidden, e.MemberB))
	path := "/api/v1/teams/" + e.SlugA + "/events?me=true"
	me := e.openStreamAt(t, srv, both, path, "")
	_, _ = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })

	start := time.Now()
	assigned := e.fileIn(t, memberB, e.SlugB, "BETA", task("assigned in B", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	m, ok := me.next(t, time.Second)
	require.True(t, ok, "a ticket assigned to the person in B arrives on the stream opened on A within a second")
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, "ticket.changed", m.Event)
	assert.Equal(t, assigned.Key, eventKey(t, m))
	assert.NotEmpty(t, m.ID)
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 1, unreadOf(t, m))

	// Never: a ticket of a project of B hidden from the person, a confidential
	// ticket of B they cannot see. A sentinel in A ends the wait.
	notSeen := map[string]bool{}
	notSeen[e.fileIn(t, memberB, e.SlugB, "HIDDEN", task("hidden")).Key] = true
	notSeen[e.fileIn(t, memberB, e.SlugB, "BETA", task("secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	})).Key] = true
	sentinel := e.fileIn(t, admin, e.SlugA, "ALPHA", task("sentinel"))
	before, last := me.until(t, func(m sse) bool { return m.Event == "ticket.changed" && eventKey(t, m) == sentinel.Key })
	for _, m := range before {
		if m.Event == "ticket.changed" {
			assert.False(t, notSeen[eventKey(t, m)], "nothing hidden from the person: %v", m)
		}
	}

	// A reconnect with the id of A's sentinel replays what B published meanwhile.
	me.Close()
	missed := e.fileIn(t, memberB, e.SlugB, "BETA", task("while away"))
	again := e.openStreamAt(t, srv, both, path, last.ID)
	_, m = again.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, missed.Key, eventKey(t, m), "the replay merges the person's tenants")

	// Granted a role in a tenant C, the person's stream follows C at once.
	c, err := f.Tenant(e.ctx, uniqueSlug("team-c"), "Team C")
	require.NoError(t, err)
	slugC := scalar[string](t, `SELECT slug FROM tenants WHERE id = $1`, c)
	_, err = f.Project(e.ctx, c, "GAMMA", "Gamma")
	require.NoError(t, err)
	adminC, err := f.Person(e.ctx, uniqueSlug("admin-c"), "Admin C")
	require.NoError(t, err)
	require.NoError(t, f.Member(e.ctx, c, adminC, "admin"))
	require.NoError(t, f.Account(e.ctx, adminC, testPassword, c, false))
	adminCToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: adminC, Scope: "admin"})
	require.NoError(t, err)
	browser := srv.browser(t)
	browser.mustLogin(usernameOf(t, adminC), testPassword)
	res := browser.request(http.MethodPost, "/api/v1/teams/"+slugC+"/members", map[string]string{"person": usernameOf(t, e.Both), "role": "member"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	_, m = again.until(t, func(m sse) bool { return m.Event == "membership.changed" })
	assert.JSONEq(t, `{"team":"`+slugC+`","tenant":"`+slugC+`","person_id":"`+e.Both.String()+`"}`, m.Data, "the person hears their grant")
	start = time.Now()
	res = srv.do(t, caller{Token: adminCToken}, http.MethodPost, "/api/v1/teams/"+slugC+"/projects/GAMMA/tickets", task("in C"))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	inC := decode[apigen.Ticket](t, res)
	_, m = again.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, inC.Key, eventKey(t, m), "a tenant the person joined, without a heartbeat")
	assert.Less(t, time.Since(start), time.Second)
}

// docs/adr/0054 D3, D5: the heartbeat checks every membership a person-level
// stream follows — a change made in the database past the API, which no act
// announces: a tenant the person left is followed no more, one they joined is.
func TestTheHeartbeatChecksEveryMembershipOfThePersonLevelStream(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	srv := newAPI(t, func(o *api.Options) { o.Heartbeat = 100 * time.Millisecond })
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	me := e.openStreamAt(t, srv, both, "/api/v1/teams/"+e.SlugA+"/events?me=true", "")
	beats := func(n int) {
		t.Helper()
		for seen := 0; seen < n; {
			select {
			case m := <-me.Messages:
				if m.Comment == "heartbeat" {
					seen++
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no heartbeat")
			}
		}
	}

	require.NoError(t, f.Exec(e.ctx, "DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2", e.B, e.Both))
	c, err := f.Tenant(e.ctx, uniqueSlug("team-c"), "Team C")
	require.NoError(t, err)
	_, err = f.Project(e.ctx, c, "GAMMA", "Gamma")
	require.NoError(t, err)
	require.NoError(t, f.Member(e.ctx, c, e.Both, "member"))
	beats(2)

	left := e.fileIn(t, memberB, e.SlugB, "BETA", task("after leaving"))
	slugC := scalar[string](t, `SELECT slug FROM tenants WHERE id = $1`, c)
	joined := e.fileIn(t, both, slugC, "GAMMA", task("after joining"))
	sentinel := e.fileIn(t, admin, e.SlugA, "ALPHA", task("sentinel"))
	before, _ := me.until(t, func(m sse) bool { return m.Event == "ticket.changed" && eventKey(t, m) == sentinel.Key })
	keys := []string{}
	for _, m := range before {
		if m.Event == "ticket.changed" {
			keys = append(keys, eventKey(t, m))
		}
	}
	assert.NotContains(t, keys, left.Key, "a tenant the person left")
	assert.Contains(t, keys, joined.Key, "a tenant the person joined")
}

// docs/adr/0035 D3: a token restricted to a tenant hears nothing of another on
// its person-level stream, and counts its own tenant's notifications only.
func TestARestrictedTokensPersonLevelStreamStaysInItsTenant(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	memberB := caller{Token: e.tk.MemberB}
	onlyA, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.A})
	require.NoError(t, err)
	e.fileIn(t, memberB, e.SlugB, "BETA", task("b0", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))

	me := e.openMeStream(t, caller{Token: onlyA}, e.SlugA)
	first, ok := me.next(t, 5*time.Second)
	require.True(t, ok)
	assert.Equal(t, 0, unreadOf(t, first), "B's notification is outside the token's tenant")

	b1 := e.fileIn(t, memberB, e.SlugB, "BETA", task("b1"))
	e.send(t, memberB, http.StatusCreated, http.MethodPost, ticketPath(e.SlugB, "BETA", b1.Number)+"/questions",
		map[string]any{"question": "yes?", "asked_of": e.Both})
	a1 := e.fileIn(t, caller{Token: e.tk.AdminA}, e.SlugA, "ALPHA", task("a1"))
	before, sentinel := me.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, a1.Key, eventKey(t, sentinel))
	for _, m := range before {
		assert.NotEqual(t, "question.changed", m.Event)
		if m.Event == "inbox.changed" {
			assert.Equal(t, 0, unreadOf(t, m), "the question's notification is outside the token's tenant")
		}
	}
}
