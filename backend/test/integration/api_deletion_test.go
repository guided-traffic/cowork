//go:build integration

package integration

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/storage"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// short is a ticket's key inside its tenant, <PROJECT>-<number>.
func short(tk apigen.Ticket) string { return tk.Project + "-" + strconv.Itoa(tk.Number) }

// binPath is a tenant's bin, or a ticket in it.
func binPath(slug string, key ...string) string {
	p := "/api/v1/tenants/" + slug + "/deleted-tickets"
	if len(key) > 0 {
		p += "/" + key[0]
	}
	return p
}

// bin reads a tenant's bin as c and returns its keys.
func (e ticketEnv) bin(t *testing.T, c caller, slug string) []apigen.DeletedTicket {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, binPath(slug), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	return decode[apigen.DeletedTicketList](t, res).Items
}

func binKeys(items []apigen.DeletedTicket) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

// deletedScene is a ticket with everything that can hang off it or point at
// it: a parent, a child, a comment, a question asked of a member, a file, a
// booking, a stake, a ticket it blocks, one blocked on it by kind, a relates
// link, an assignment that told the assignee — and the persons who look.
type deletedScene struct {
	ticketEnv
	admin, member, viewer, both caller
	parent, gone, child         apigen.Ticket
	blocked, waiting, related   apigen.Ticket
	attachment                  uuid.UUID
}

func newDeletedScene(t *testing.T) deletedScene {
	t.Helper()
	e := newTicketEnv(t)
	s := deletedScene{ticketEnv: e, admin: caller{Token: e.tk.AdminA}, member: caller{Token: e.tk.MemberA},
		viewer: caller{Token: e.tk.ViewerA}, both: caller{Token: e.tk.Both}}
	s.parent = e.file(t, s.member, "ALPHA", task("the parent"))
	s.gone = e.file(t, s.admin, "ALPHA", task("the wrong paste with another client's data", func(b *apigen.TicketCreate) {
		b.Parent, b.Assignee, b.Effort = ptr(short(s.parent)), &e.MemberA, apigen.EffortL
	}))
	s.child = e.file(t, s.member, "ALPHA", task("its child", func(b *apigen.TicketCreate) { b.Parent = ptr(short(s.gone)) }))
	s.blocked = e.file(t, s.member, "ALPHA", task("blocked by it"))
	require.Equal(t, http.StatusCreated, e.link(t, s.member, s.gone, apigen.LinkTypeBlocks, s.blocked).StatusCode)
	s.related = e.file(t, s.member, "ALPHA", task("related to it"))
	require.Equal(t, http.StatusCreated, e.link(t, s.member, s.related, apigen.LinkTypeRelatesTo, s.gone).StatusCode)
	s.waiting = e.file(t, s.member, "ALPHA", task("waits on it"))
	moved := e.move(t, s.member, s.waiting, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateBlocked,
		Reason: ptr("needs the other one"), Block: &apigen.BlockSet{Kind: apigen.BlockKindTicket, Ticket: ptr(short(s.gone))}})
	require.Equal(t, http.StatusOK, moved.StatusCode(), string(moved.Body))
	s.waiting = *moved.JSON200

	e.send(t, s.member, http.StatusCreated, http.MethodPost, ticketPath(e.SlugA, "ALPHA", s.gone.Number)+"/comments",
		apigen.CommentWrite{Body: "the secret is in here"})
	asked := e.ask(t, s.admin, s.gone, apigen.QuestionCreate{Question: "Which client is this?", AskedOf: &e.MemberA})
	require.Equal(t, http.StatusCreated, asked.StatusCode(), string(asked.Body))
	s.attachment = decodeAttachment(t, e.uploadTo(t, s.member, s.gone, "shot.png", "image/png", pngBytes, nil, "")).Id
	require.Equal(t, http.StatusCreated, e.book(t, s.member, s.gone, 45, "2026-10-01").StatusCode())
	e.send(t, s.member, http.StatusCreated, http.MethodPut, ticketPath(e.SlugA, "ALPHA", s.gone.Number)+"/interest",
		apigen.InterestSet{Weight: apigen.InterestWeightWatch})
	s.gone = e.reread(t, s.admin, s.gone)
	return s
}

func (e ticketEnv) reread(t *testing.T, c caller, tk apigen.Ticket) apigen.Ticket {
	t.Helper()
	res := e.get(t, c, tk.Project, tk.Number)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200
}

// routesOf are the routes of a ticket and under it, as a reader would call
// them: every one must answer 404 for a deleted ticket.
func routesOf(slug string, tk apigen.Ticket) []string {
	base := ticketPath(slug, tk.Project, tk.Number)
	return []string{base, base + "/comments", base + "/questions", base + "/questions/1", base + "/links",
		base + "/attachments", base + "/activity", base + "/interest", base + "/time-entries", base + "/prerequisites",
		base + "/prerequisites?direction=up", base + "/markdown", base + "/context",
		"/api/v1/tickets/" + slug + "/" + short(tk)}
}

func (e ticketEnv) treeKeys(t *testing.T, c caller, tk apigen.Ticket, query string) ([]string, int) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, ticketPath(e.SlugA, tk.Project, tk.Number)+"/prerequisites"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	tree := decode[apigen.PrerequisiteTree](t, res)
	keys := make([]string, 0, len(tree.Items))
	for _, n := range tree.Items {
		keys = append(keys, n.Key)
	}
	return keys, tree.Open
}

func (e ticketEnv) reportMinutes(t *testing.T, c caller) int {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/tenants/"+e.SlugA+"/time-report?from=2026-09-01&to=2026-10-31", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	return decode[apigen.TimeReport](t, res).TotalMinutes
}

// docs/adr/0024 D1, D3, docs/adr/0065 D5's reading: a deleted ticket answers
// like a missing one everywhere — its routes, every list, the trees, the links,
// the person-level lists, the inbox, the context, the time report and the acts
// that name it — but the tenant administrators' bin; a restoration brings
// all of it back.
func TestADeletedTicketAnswersLikeAMissingOne(t *testing.T) {
	s := newDeletedScene(t)
	e := s.ticketEnv
	key := s.gone.Key
	stream := e.openStream(t, e.s, s.member, e.SlugA, "")

	require.Equal(t, 1, e.reread(t, s.member, s.blocked).OpenPrerequisites)
	beforeParent := e.reread(t, s.member, s.parent)
	require.True(t, beforeParent.ProgressDerived)
	minutesBefore := e.reportMinutes(t, s.admin)
	require.GreaterOrEqual(t, minutesBefore, 45)
	assigned, _ := e.assigned(t, s.member, "")
	require.Contains(t, assigned, key)
	decided, _ := e.decisions(t, s.member, "")
	require.Contains(t, decided, key+" Q1")
	require.Contains(t, reasonsAbout(e.inbox(t, s.member, ""), key), "assigned")

	e.send(t, s.admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", s.gone.Number), nil)
	_, ok := stream.until(t, func(m sse) bool { return m.Event == "ticket.changed" && strings.Contains(m.Data, `"kind":"deleted"`) })
	assert.Equal(t, key, eventKey(t, ok), "the deletion is published to whoever could see the ticket")

	for _, who := range map[string]caller{"admin": s.admin, "member": s.member, "viewer": s.viewer} {
		for _, path := range routesOf(e.SlugA, s.gone) {
			assertProblem(t, e.s.do(t, who, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
		}
		assert.NotContains(t, e.titles(t, who, e.projectTickets("ALPHA"), "include_terminal=true"), s.gone.Title)
		assert.NotContains(t, e.titles(t, who, e.tenantTickets(), "include_terminal=true"), s.gone.Title)
		assert.Empty(t, e.titles(t, who, e.tenantTickets(), "q=wrong+paste"), "full text leaves it out")
		assert.NotContains(t, e.titles(t, who, e.projectTickets("ALPHA"), "parent=ALPHA-"+strconv.Itoa(s.gone.Number)), s.child.Title,
			"a deleted parent matches nothing, as a missing one")
	}
	for _, write := range []struct{ method, path string }{
		{http.MethodPatch, ""}, {http.MethodPut, "/body"}, {http.MethodPost, "/transitions"}, {http.MethodPost, "/comments"},
		{http.MethodPut, "/interest"}, {http.MethodPut, "/rank"}, {http.MethodDelete, ""},
	} {
		res := e.s.do(t, s.admin, write.method, ticketPath(e.SlugA, "ALPHA", s.gone.Number)+write.path, map[string]any{})
		assert.Contains(t, []int{http.StatusNotFound, http.StatusBadRequest, http.StatusPreconditionRequired}, res.StatusCode, write)
		if res.StatusCode != http.StatusNotFound {
			continue
		}
		assert.Equal(t, "not_found", problemBody(t, res)["code"])
	}
	assertProblem(t, e.link(t, s.member, s.blocked, apigen.LinkTypeRelatesTo, s.gone), http.StatusNotFound, "not_found")

	blocked := e.reread(t, s.member, s.blocked)
	assert.Zero(t, blocked.OpenPrerequisites, "a deleted blocker is counted nowhere")
	assert.NotContains(t, e.links(t, s.member, s.blocked), "blocked by "+key)
	assert.NotContains(t, e.links(t, s.member, s.related), "relates to "+key)
	tree, open := e.treeKeys(t, s.member, s.blocked, "")
	assert.NotContains(t, tree, key)
	assert.Zero(t, open)
	assert.Empty(t, e.titles(t, s.member, e.projectTickets("ALPHA"), "blocked=true"), "nothing is blocked by a deleted ticket")
	waiting := e.reread(t, s.member, s.waiting)
	require.True(t, waiting.Block.IsSpecified() && !waiting.Block.IsNull())
	block := waiting.Block.MustGet()
	assert.Equal(t, apigen.BlockKindTicket, block.Kind)
	assert.True(t, block.Ticket.IsNull(), "the block names no ticket the reader cannot see")
	child := e.reread(t, s.member, s.child)
	assert.True(t, child.Parent.IsNull(), "a child shows its deleted parent as hidden")
	parent := e.reread(t, s.member, s.parent)
	assert.False(t, parent.ProgressDerived, "the parent's stages leave its deleted child out")
	assert.Equal(t, beforeParent.Version, parent.Version, "a derived change raises no version")
	acts, _ := e.activity(t, s.member, s.related)
	linked := 0
	for _, act := range acts.Items {
		if act.Action == apigen.AuditActionLinked {
			linked++
			assert.True(t, act.Redacted, "an act that names the deleted ticket is shown without its payload")
		}
	}
	assert.Equal(t, 1, linked)
	ctxRes, doc := e.contextOf(t, s.member, s.blocked, "")
	require.Equal(t, http.StatusOK, ctxRes.StatusCode)
	assert.NotContains(t, doc, short(s.gone))

	assigned, _ = e.assigned(t, s.member, "")
	assert.NotContains(t, assigned, key)
	decided, _ = e.decisions(t, s.member, "")
	assert.NotContains(t, decided, key+" Q1")
	inbox := e.inbox(t, s.member, "")
	assert.Empty(t, reasonsAbout(inbox, key))
	assert.Less(t, e.reportMinutes(t, s.admin), minutesBefore, "its time leaves the report")
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM tickets WHERE id = $1 AND deleted_at IS NULL`, s.gone.Id))

	items := e.bin(t, s.admin, e.SlugA)
	require.Equal(t, []string{key}, binKeys(items), "the administrators' bin is where it exists")
	assert.Equal(t, e.AdminA, items[0].DeletedBy.Id)
	assert.Equal(t, s.gone.Title, items[0].Title)
	assert.Equal(t, items[0].DeletedAt.Add(30*24*time.Hour), items[0].PurgeAt)
	assertProblem(t, e.s.do(t, s.admin, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", s.gone.Number), nil),
		http.StatusNotFound, "not_found")
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'deleted'
		AND actor_user_id = $2`, s.gone.Id, e.AdminA), "the deletion is recorded")
	assert.Equal(t, 7, e.file(t, s.member, "ALPHA", task("after")).Number, "the deleted ticket's number stays taken")

	res := e.s.do(t, s.admin, http.MethodPut, binPath(e.SlugA, short(s.gone))+"/restore", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	restored := decode[apigen.Ticket](t, res)
	assert.Equal(t, key, restored.Key)
	assert.Greater(t, restored.Version, s.gone.Version)
	assert.Equal(t, strconv.Quote(strconv.Itoa(restored.Version)), res.Header.Get("ETag"))
	_, ok = stream.until(t, func(m sse) bool { return m.Event == "ticket.changed" && strings.Contains(m.Data, `"kind":"restored"`) })
	assert.Equal(t, key, eventKey(t, ok))
	for _, path := range routesOf(e.SlugA, s.gone) {
		assert.Equal(t, http.StatusOK, e.s.do(t, s.member, http.MethodGet, path, nil).StatusCode, path)
	}
	assert.Equal(t, 1, e.reread(t, s.member, s.blocked).OpenPrerequisites, "its links come back")
	assert.Contains(t, e.links(t, s.member, s.related), "relates to "+key)
	assert.True(t, e.reread(t, s.member, s.parent).ProgressDerived, "the parent counts it again")
	assert.Equal(t, e.SlugA+"/"+short(s.gone), e.reread(t, s.member, s.child).Parent.MustGet())
	assigned, _ = e.assigned(t, s.member, "")
	assert.Contains(t, assigned, key)
	assert.Equal(t, minutesBefore, e.reportMinutes(t, s.admin))
	assert.Contains(t, reasonsAbout(e.inbox(t, s.member, ""), key), "assigned")
	assert.Empty(t, e.bin(t, s.admin, e.SlugA))
	assertProblem(t, e.s.do(t, s.admin, http.MethodPut, binPath(e.SlugA, short(s.gone))+"/restore", nil),
		http.StatusNotFound, "not_found")
}

// docs/adr/0024 D7, docs/adr/0043 D3: deleting, restoring and purging are a
// tenant administrator's acts with admin scope, never an agent's; the bin is
// the administrators' to read; another tenant's never answers but as an
// unknown tenant.
func TestDeletionIsATenantAdministratorsActNeverAnAgents(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	tk := e.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("to delete"))
	path := ticketPath(e.SlugA, "ALPHA", tk.Number)

	for name, c := range map[string]struct {
		who  caller
		code string
	}{
		"member":                 {caller{Token: e.tk.MemberA}, "forbidden"},
		"viewer":                 {caller{Token: e.tk.ViewerA}, "forbidden"},
		"administrator, write":   {caller{Token: e.tk.AdminAWrite}, "insufficient_scope"},
		"administrator's agent":  {caller{Token: e.tk.AdminA, Agent: "claude-code/opus/s1"}, "agent_forbidden"},
		"member's agent token":   {caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}, "forbidden"},
		"another tenant's token": {caller{Token: e.tk.MemberB}, "not_found"},
	} {
		status := http.StatusForbidden
		if c.code == "not_found" {
			status = http.StatusNotFound
		}
		body := assertProblem(t, e.s.do(t, c.who, http.MethodDelete, path, nil), status, c.code)
		if c.code == "agent_forbidden" {
			assert.Equal(t, "hard-off: deleting, restoring or purging", body["detail"], name)
		}
	}
	e.send(t, admin, http.StatusNoContent, http.MethodDelete, path, nil)

	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, binPath(e.SlugA), nil), http.StatusForbidden, "forbidden")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, binPath(e.SlugA), nil), http.StatusNotFound, "not_found")
	assert.Equal(t, http.StatusOK, e.s.do(t, caller{Token: e.tk.AdminAWrite}, http.MethodGet, binPath(e.SlugA), nil).StatusCode,
		"reading the bin takes read scope")
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		p := binPath(e.SlugA, short(tk))
		if method == http.MethodPut {
			p += "/restore"
		}
		assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, method, p, nil), http.StatusForbidden, "forbidden")
		assertProblem(t, e.s.do(t, caller{Token: e.tk.AdminAWrite}, method, p, nil), http.StatusForbidden, "insufficient_scope")
		assertProblem(t, e.s.do(t, caller{Token: e.tk.AdminA, Agent: "claude-code/opus/s1"}, method, p, nil),
			http.StatusForbidden, "agent_forbidden")
		assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, method, p, nil), http.StatusNotFound, "not_found")
	}

	// Tenant B's administrator finds nothing of A's bin in B's, and neither a
	// live ticket nor an unknown key is in a bin.
	adminB, err := f.Person(e.ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(e.ctx, e.B, adminB, domain.RoleAdmin))
	tokenB, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: adminB, Scope: domain.ScopeAdmin})
	require.NoError(t, err)
	assert.Empty(t, e.bin(t, caller{Token: tokenB}, e.SlugB))
	assertProblem(t, e.s.do(t, caller{Token: tokenB}, http.MethodPut, binPath(e.SlugB, short(tk))+"/restore", nil),
		http.StatusNotFound, "not_found")
	live := e.file(t, admin, "ALPHA", task("live"))
	assertProblem(t, e.s.do(t, admin, http.MethodPut, binPath(e.SlugA, short(live))+"/restore", nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, admin, http.MethodDelete, binPath(e.SlugA, short(live)), nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, admin, http.MethodDelete, binPath(e.SlugA, "ALPHA-999"), nil), http.StatusNotFound, "not_found")
	assert.Equal(t, http.StatusOK, e.get(t, admin, "ALPHA", live.Number).StatusCode(), "a refused purge leaves a live ticket alone")
}

// docs/adr/0024 D2, docs/adr/0026 D3: the purge removes the ticket and what
// belongs only to it, the attachment objects included; its audit rows keep
// the key, the actor and the act with their content emptied; its children
// become roots, a block on it waits on its key; the key stays taken.
func TestThePurgeRemovesTheTicketAndKeepsItsAuditRows(t *testing.T) {
	s := newDeletedScene(t)
	e := s.ticketEnv
	key := s.gone.Key
	stream := e.openStream(t, e.s, s.admin, e.SlugA, "")
	e.send(t, s.admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", s.gone.Number), nil)
	require.Positive(t, scalar[int](t, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND after IS NOT NULL`, s.gone.Id))
	waitingVersion := e.reread(t, s.member, s.waiting).Version

	e.send(t, s.admin, http.StatusNoContent, http.MethodDelete, binPath(e.SlugA, short(s.gone)), nil)
	_, ok := stream.until(t, func(m sse) bool { return m.Event == "ticket.changed" && strings.Contains(m.Data, `"kind":"purged"`) })
	assert.Equal(t, key, eventKey(t, ok))

	for table, column := range map[string]string{"tickets": "id", "comments": "ticket_id", "questions": "ticket_id",
		"attachments": "ticket_id", "time_entries": "ticket_id", "ticket_interest": "ticket_id", "notifications": "ticket_id"} {
		assert.Zero(t, scalar[int](t, `SELECT count(*) FROM `+table+` WHERE `+column+` = $1`, s.gone.Id), table)
	}
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM ticket_links WHERE source_id = $1 OR target_id = $1`, s.gone.Id))
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM comment_revisions r WHERE NOT EXISTS (SELECT 1 FROM comments c WHERE c.id = r.comment_id)`))
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action <> 'purged'
		AND (before IS NOT NULL OR after IS NOT NULL OR reason IS NOT NULL OR note IS NOT NULL)`, s.gone.Id),
		"the audit rows keep no content")
	assert.Positive(t, scalar[int](t, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND ticket_key = $2
		AND action = 'commented'`, s.gone.Id, key), "the rows keep the key, the actor and the act")
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'purged'
		AND actor_user_id = $2 AND ticket_key = $3`, s.gone.Id, e.AdminA, key))
	assert.JSONEq(t, `{"attachments":1,"children":1,"comments":1,"interest":1,"links":3,"notifications":3,"questions":1,"time_entries":1}`,
		scalar[string](t, `SELECT after::text FROM audit_events WHERE ticket_id = $1 AND action = 'purged'`, s.gone.Id),
		"the purge records what it removed, counted, never what it said")
	_, _, err := testStorage(t).Get(e.ctx, storage.Key(e.A, s.attachment))
	assert.ErrorIs(t, err, storage.ErrMissing, "the attachment's object is gone")

	child := e.reread(t, s.member, s.child)
	assert.True(t, child.Parent.IsNull())
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM tickets WHERE id = $1 AND parent_id IS NOT NULL`, s.child.Id))
	waiting := e.reread(t, s.member, s.waiting)
	block := waiting.Block.MustGet()
	assert.Equal(t, apigen.BlockKindExternal, block.Kind, "the block waits on the purged key as text")
	assert.Equal(t, key, block.ExternalRef.MustGet())
	assert.Equal(t, waitingVersion+1, waiting.Version)
	assert.Equal(t, apigen.TicketStateBlocked, waiting.State)
	assert.Empty(t, e.bin(t, s.admin, e.SlugA))
	for _, path := range routesOf(e.SlugA, s.gone) {
		assertProblem(t, e.s.do(t, s.admin, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	}
	assertProblem(t, e.s.do(t, s.admin, http.MethodPut, binPath(e.SlugA, short(s.gone))+"/restore", nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, s.admin, http.MethodDelete, binPath(e.SlugA, short(s.gone)), nil), http.StatusNotFound, "not_found")
	assert.Equal(t, 7, e.file(t, s.member, "ALPHA", task("after the purge")).Number, "the purged key is never handed out again")
}

// docs/adr/0024 D2, docs/adr/0027 D5: thirty days after the deletion the job
// purges the ticket in its tenant, as system:ticket-purge, and leaves younger
// deletions and live tickets alone.
func TestThePurgeJobPurgesAfterThirtyDays(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin := caller{Token: e.tk.AdminA}
	old := e.file(t, admin, "ALPHA", task("deleted long ago"))
	young := e.file(t, admin, "ALPHA", task("deleted yesterday"))
	live := e.file(t, admin, "ALPHA", task("live"))
	file := decodeAttachment(t, e.uploadTo(t, admin, old, "shot.png", "image/png", pngBytes, nil, ""))
	for _, tk := range []apigen.Ticket{old, young} {
		e.send(t, admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", tk.Number), nil)
	}
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET deleted_at = now() - interval '31 days' WHERE id = $1`, old.Id))
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET deleted_at = now() - interval '1 day' WHERE id = $1`, young.Id))

	purged, err := openRuntime(t).PurgeDeletedTickets(e.ctx, time.Now())
	require.NoError(t, err)
	keys := make([]string, 0, len(purged))
	for _, p := range purged {
		keys = append(keys, p.Key)
		if p.Key == old.Key {
			assert.Equal(t, e.A, p.TenantID)
			assert.Equal(t, []uuid.UUID{file.Id}, p.Attachments, "the caller removes the objects after the commit")
		}
	}
	assert.Contains(t, keys, old.Key)
	assert.NotContains(t, keys, young.Key)
	assert.Zero(t, scalar[int](t, `SELECT count(*) FROM tickets WHERE id = $1`, old.Id))
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM tickets WHERE id = $1`, young.Id))
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM tickets WHERE id = $1 AND deleted_at IS NULL`, live.Id))
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'purged'
		AND actor_system = 'system:ticket-purge' AND tenant_id = $2 AND ticket_key = $3`, old.Id, e.A, old.Key),
		"the job's act is in the ticket's tenant")
	assert.Equal(t, []string{young.Key}, binKeys(e.bin(t, admin, e.SlugA)))

	again, err := openRuntime(t).PurgeDeletedTickets(e.ctx, time.Now())
	require.NoError(t, err)
	for _, p := range again {
		assert.NotEqual(t, old.Key, p.Key, "a second run finds nothing more of it")
	}
}

// docs/adr/0024 D2, docs/adr/0026 D3, docs/adr/0021 D1: outside the purge no
// delete reaches a ticket or what belongs only to it — a forgotten WHERE
// removes nothing —, the purge reaches only a deleted ticket's rows, and the
// owner's function that empties audit rows refuses everything else.
func TestThePurgePoliciesHoldEveryDeleteToTheBin(t *testing.T) {
	e := newTicketEnv(t)
	admin := caller{Token: e.tk.AdminA}
	live := e.file(t, admin, "ALPHA", task("live"))
	e.send(t, admin, http.StatusCreated, http.MethodPost, ticketPath(e.SlugA, "ALPHA", live.Number)+"/comments",
		apigen.CommentWrite{Body: "stays"})
	deleted := e.file(t, admin, "ALPHA", task("deleted"))
	e.send(t, admin, http.StatusCreated, http.MethodPost, ticketPath(e.SlugA, "ALPHA", deleted.Number)+"/comments",
		apigen.CommentWrite{Body: "goes"})
	e.send(t, admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", deleted.Number), nil)

	// Each statement runs as the runtime role in a transaction of its own,
	// bound to the tenant and the administrator, the job named or not, and
	// rolled back: what it could reach is what counts.
	exec := func(job, sql string, args ...any) (int64, error) {
		conn, err := pgx.Connect(e.ctx, env.RuntimeURL)
		require.NoError(t, err)
		defer func() { _ = conn.Close(e.ctx) }()
		tx, err := conn.Begin(e.ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(e.ctx) }()
		_, err = tx.Exec(e.ctx, "SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true), "+
			"set_config('app.job', $3, true)", e.A.String(), e.AdminA.String(), job)
		require.NoError(t, err)
		tag, err := tx.Exec(e.ctx, sql, args...)
		return tag.RowsAffected(), err
	}
	n, err := exec("", `DELETE FROM comments WHERE tenant_id = $1`, e.A)
	require.NoError(t, err)
	assert.Zero(t, n, "outside the purge no comment goes")
	n, err = exec("", `DELETE FROM tickets WHERE tenant_id = $1 AND deleted_at IS NOT NULL`, e.A)
	require.NoError(t, err)
	assert.Zero(t, n, "outside the purge no ticket goes, a deleted one included")
	n, err = exec("ticket-purge", `DELETE FROM comments WHERE tenant_id = $1`, e.A)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "inside the purge only the deleted ticket's comment goes")
	n, err = exec("ticket-purge", `DELETE FROM tickets WHERE tenant_id = $1 AND id = $2`, e.A, live.Id)
	require.NoError(t, err)
	assert.Zero(t, n, "inside the purge a live ticket stays")

	_, err = exec("", `SELECT purge_ticket_audit($1)`, deleted.Id)
	assert.ErrorContains(t, err, "inside the purge", "the function refuses outside the purge")
	_, err = exec("ticket-purge", `SELECT purge_ticket_audit($1)`, live.Id)
	assert.ErrorContains(t, err, "no deleted ticket", "the function refuses a live ticket")
	_, err = exec("", `UPDATE audit_events SET after = NULL WHERE ticket_id = $1`, deleted.Id)
	assert.ErrorContains(t, err, "permission denied", "the runtime role still cannot rewrite an audit row")
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM comments WHERE ticket_id = $1`, live.Id))
}

// docs/adr/0027 conventions: two administrators deleting or purging the same
// ticket at once — one wins, the other answers as a later request would.
func TestSimultaneousDeletionsAndPurgesAnswerAlike(t *testing.T) {
	e := newTicketEnv(t)
	admin := caller{Token: e.tk.AdminA}
	for round := range 4 {
		tk := e.file(t, admin, "ALPHA", task("race "+strconv.Itoa(round)))
		send := func(method, path string) func() int {
			return func() int {
				req, _ := http.NewRequest(method, e.s.URL+path, nil)
				_ = admin.editor(context.Background(), req)
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					return 0
				}
				_ = res.Body.Close()
				return res.StatusCode
			}
		}
		codes := simultaneously(times(3, send(http.MethodDelete, ticketPath(e.SlugA, "ALPHA", tk.Number)))...)
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusNoContent, http.StatusNotFound, http.StatusNotFound}, codes, "deletions")
		codes = simultaneously(append(times(2, send(http.MethodDelete, binPath(e.SlugA, short(tk)))),
			send(http.MethodPut, binPath(e.SlugA, short(tk))+"/restore"))...)
		assert.Equal(t, 1, countOf(codes, http.StatusNoContent)+countOf(codes, http.StatusOK), "one purge or the restoration wins: %v", codes)
		assert.NotContains(t, codes, http.StatusInternalServerError, "%v", codes)
	}
}

func countOf(codes []int, code int) int {
	n := 0
	for _, c := range codes {
		if c == code {
			n++
		}
	}
	return n
}
