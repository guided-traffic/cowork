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
	return "/api/v1/tenants/" + slug + "/projects/" + project + "/tickets/" + strconv.Itoa(number)
}

// fileIn files a ticket as c in the tenant's project and requires the 201.
func (e ticketEnv) fileIn(t *testing.T, c caller, slug, project string, body apigen.TicketCreate) apigen.Ticket {
	t.Helper()
	res := e.s.do(t, c, http.MethodPost, "/api/v1/tenants/"+slug+"/projects/"+project+"/tickets", body)
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
	assert.Equal(t, apigen.TenantRef{Slug: e.SlugA, Name: "Tenant A"}, entry.Tenant)
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
	assert.Equal(t, e.SlugB, all.Items[0].Tenant.Slug)
	assert.Equal(t, inA.Key, all.Items[1].Ticket.Key)
	assert.Equal(t, e.SlugA, all.Items[1].Tenant.Slug)
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
	assert.Equal(t, e.SlugB, now.Items[0].Tenant.Slug)
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
	return e.openStreamAt(t, e.s, c, "/api/v1/tenants/"+slug+"/events?me=true", "")
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
// when it opens and when the inbox changes, and carries the questions asked of
// the person in their other tenants, without an id — and nothing of another
// person, of a tenant the person left, of a project restricted away from them
// or of a confidential ticket they cannot see.
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
	assert.Empty(t, m.ID, "another tenant's event moves no replay point")
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 2, unreadOf(t, m))

	// What it must not carry: a question asked of another person; the acts of
	// questions asked of the person on a ticket of a project restricted away
	// from them and on a confidential ticket. The stream judges its events in
	// order, so a sentinel in A says each was judged while the person still
	// belonged to B.
	e.send(t, memberB, http.StatusCreated, http.MethodPost, b1Path+"/questions", map[string]any{"question": "you?", "asked_of": other})
	e.send(t, memberB, http.StatusOK, http.MethodPatch, h1Path+"/questions/1", map[string]any{"question": "hidden, edited?"}, "If-Match", `"1"`)
	e.send(t, memberB, http.StatusOK, http.MethodPatch, c1Path+"/questions/1", map[string]any{"question": "secret, edited?"}, "If-Match", `"1"`)
	a1 := e.fileIn(t, admin, e.SlugA, "ALPHA", task("a1", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	before, sentinel := me.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, a1.Key, eventKey(t, sentinel))
	assert.NotEmpty(t, sentinel.ID, "the stream's own tenant keeps its ids")
	for _, m := range before {
		assert.NotEqual(t, "question.changed", m.Event, "nothing of another person, a hidden project or a confidential ticket: %v", m)
	}
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 3, unreadOf(t, m), "A's assignment beside B's two")

	// Nor, once the person left B, the act of a question asked of them there.
	require.NoError(t, f.Exec(e.ctx, "DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2", e.B, e.Both))
	e.send(t, memberB, http.StatusOK, http.MethodPatch, b1Path+"/questions/1", map[string]any{"question": "after leaving?"}, "If-Match", `"1"`)
	a2 := e.fileIn(t, admin, e.SlugA, "ALPHA", task("a2", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	before, sentinel = me.until(t, func(m sse) bool { return m.Event == "ticket.changed" })
	assert.Equal(t, a2.Key, eventKey(t, sentinel))
	for _, m := range before {
		assert.NotEqual(t, "question.changed", m.Event, "nothing of a tenant left: %v", m)
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
// person in another tenant still arrives without an id; the heartbeat, an hour
// here, plays no part.
func TestAPersonLevelStreamRefiltersAndKeepsItsPersonsEvents(t *testing.T) {
	e := newTicketEnv(t)
	srv := newAPI(t, func(o *api.Options) { o.Heartbeat = time.Hour })
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	me := e.openStreamAt(t, srv, both, "/api/v1/tenants/"+e.SlugA+"/events?me=true", "")
	first, ok := me.next(t, 5*time.Second)
	require.True(t, ok)
	assert.Equal(t, 0, unreadOf(t, first))

	res := srv.do(t, admin, http.MethodPost, "/api/v1/tenants/"+e.SlugA+"/projects", map[string]any{"key": "LATE", "name": "Late"})
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
	assert.Empty(t, m.ID, "another tenant's event moves no replay point")
	_, m = me.until(t, func(m sse) bool { return m.Event == "inbox.changed" })
	assert.Equal(t, 2, unreadOf(t, m))
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
