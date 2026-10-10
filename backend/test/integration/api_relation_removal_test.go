//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// A relation that crosses teams is removed by a writer of either end, whatever
// they read of the other one, and recorded in both teams' records
// (docs/adr/0008 D2, docs/adr/0012 D2 as amended by the owner 2026-10-10): the
// remedy the team a relation lands on holds, whoever set it.

// childID is the id /relations gives the one child of a ticket, as c reads
// it.
func (e relEnv) childID(t *testing.T, c caller, team string, tk apigen.Ticket) string {
	t.Helper()
	rels := e.relations(t, c, team, tk, "child")
	require.Len(t, rels, 1, "the one child of %s", tk.Key)
	return rels[0].Id.MustGet()
}

// linkID is the id of the link a PUT of the canonical form made.
func linkID(t *testing.T, res *http.Response) string {
	t.Helper()
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var rel apigen.Relation
	require.NoError(t, json.NewDecoder(res.Body).Decode(&rel))
	return rel.Id.MustGet()
}

// actOf is who the latest act of the action on a ticket in a team's record
// was recorded as — a person's id or a system actor — and whether it carries
// an agent mark.
func (e relEnv) actOf(t *testing.T, team, ticket uuid.UUID, action string) (actor string, agent bool) {
	t.Helper()
	require.NoError(t, e.f.QueryRow(e.ctx, `SELECT coalesce(actor_system, actor_user_id::text), agent IS NOT NULL
		FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2 AND action = $3 ORDER BY id DESC LIMIT 1`,
		team, ticket, action).Scan(&actor, &agent), "an act %s on %s in the record", action, ticket)
	return actor, agent
}

// A member of A who holds no role in B removes, from A's side, a B ticket a
// viewer of A put under A's parent and a blocks link that viewer let a B ticket
// hold onto A's ticket, by the ids /relations gives, and a link B keeps by the
// other end's key: each act is recorded in both teams' records, the far one as
// system:relation. A's parent shows its own progress again, its child's version
// unmoved, and A's agent closes A's ticket, which nothing blocks any more.
func TestAWriterOfEitherEndRemovesARelationAcrossTeams(t *testing.T) {
	e := newRelEnv(t)
	memberA, viewerAB := caller{Token: e.tk.MemberA}, caller{Token: e.tkViewerAB}
	agentA := caller{Token: e.tk.AgentA, Agent: "claude-code/test/q7"}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))
	res := e.patchIn(t, memberA, e.SlugA, parent, apigen.TicketPatch{ProgressRefinement: ptr(20)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	parent = *res.JSON200
	child := e.fileIn(t, viewerAB, e.SlugB, "BETA", task("A child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	res = e.patchIn(t, viewerAB, e.SlugB, child, apigen.TicketPatch{ProgressRefinement: ptr(90)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	child = *res.JSON200
	work := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Work in A"))
	pre := e.fileIn(t, viewerAB, e.SlugB, "BETA", task("Blocks it from B"))
	blocks := linkID(t, e.s.do(t, viewerAB, http.MethodPut, ticketPathOf(e.SlugB, pre)+"/links/blocks/"+e.SlugA+"/"+shortOf(work), nil))
	near := e.fileIn(t, viewerAB, e.SlugB, "BETA", task("Relates to it from B"))
	linkID(t, e.s.do(t, viewerAB, http.MethodPut, ticketPathOf(e.SlugB, near)+"/links/relates-to/"+e.SlugA+"/"+shortOf(work), nil))

	require.True(t, e.readIn(t, memberA, e.SlugA, parent).ProgressDerived, "B's child counts in A's parent")
	work = e.toInProgress(t, memberA, e.SlugA, work)
	done := apigen.Transition{From: apigen.TicketStateInProgress, To: apigen.TicketStateDone, Note: ptr("verified")}
	closed, err := e.s.client(t, agentA).TransitionTicketWithResponse(e.ctx, e.SlugA, work.Project, work.Number,
		&apigen.TransitionTicketParams{IdempotencyKey: newKey()}, done)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, closed.StatusCode(), "A's agent cannot close over B's open prerequisite: %s", closed.Body)

	handle := e.childID(t, memberA, e.SlugA, parent)
	assert.NotContains(t, handle, child.Id.String(), "a child's relation id shows no ticket id")
	del := e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, parent)+"/children/"+handle, nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)
	links := e.relations(t, memberA, e.SlugA, work, "link")
	require.Len(t, links, 2)
	for _, l := range links {
		assert.Equal(t, apigen.RelationLinkDirectionIncoming, l.Link.MustGet().Direction, "B keeps both links")
		assert.Equal(t, l.Link.MustGet().Id.String(), l.Id.MustGet(), "a link's relation id is the link's")
	}
	del = e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, work)+"/links/"+blocks, nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)
	del = e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, work)+"/links/relates-to/"+e.SlugB+"/"+shortOf(near), nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)

	now := e.readIn(t, viewerAB, e.SlugB, child)
	assert.True(t, now.Parent.IsNull(), "the child of B is a root")
	assert.Equal(t, child.Version, now.Version, "the removal is a write on the parent: the child's version stays")
	own := e.readIn(t, memberA, e.SlugA, parent)
	assert.False(t, own.ProgressDerived, "the parent's progress is its own again")
	assert.Equal(t, 90, own.ProgressRefinement, "its own team's removal keeps the progress it showed (docs/adr/0017 D3)")
	assert.Equal(t, parent.Version, own.Version)
	assert.Empty(t, e.relations(t, memberA, e.SlugA, work, "link"))
	n, err := e.f.QueryCount(e.ctx, `SELECT count(*) FROM ticket_links WHERE target_id = $1`, work.Id)
	require.NoError(t, err)
	assert.Zero(t, n)

	actor, _ := e.actOf(t, e.A, parent.Id, "detached")
	assert.Equal(t, e.MemberA.String(), actor, "A's record: the parent lost its child, by the member of A")
	actor, _ = e.actOf(t, e.B, child.Id, "updated")
	assert.Equal(t, "system:relation", actor, "B's record: the child lost its parent, by nobody of B")
	for _, tk := range []apigen.Ticket{pre, near} {
		actor, _ = e.actOf(t, e.B, tk.Id, "unlinked")
		assert.Equal(t, "system:relation", actor, "B's record: %s lost its link", tk.Key)
	}
	n, err = e.f.QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2
		AND action = 'unlinked' AND actor_user_id = $3`, e.A, work.Id, e.MemberA)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "A's record: both links left A's ticket, by the member of A")
	var after string
	require.NoError(t, e.f.QueryRow(e.ctx, `SELECT after::text FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2
		AND action = 'updated' AND $3 = ANY (refs)`, e.B, child.Id, parent.Id).Scan(&after))
	assert.JSONEq(t, `{"parent": null}`, after, "B's record names A's parent in its refs alone")

	closed, err = e.s.client(t, agentA).TransitionTicketWithResponse(e.ctx, e.SlugA, work.Project, work.Number,
		&apigen.TransitionTicketParams{IdempotencyKey: newKey()}, done)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, closed.StatusCode(), "A's agent closes A's ticket: %s", closed.Body)
}

// A viewer of the ticket's team removes none of its relations, by any route:
// a removal is a write on the ticket (docs/adr/0008 D2, docs/adr/0012 D2 as
// amended 2026-10-10).
func TestAViewerCannotRemoveARelation(t *testing.T) {
	e := newRelEnv(t)
	memberA, viewerA, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	work := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Work in A"))
	pre := e.fileIn(t, both, e.SlugB, "BETA", task("Blocks it from B"))
	blocks := linkID(t, e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugB, pre)+"/links/blocks/"+e.SlugA+"/"+shortOf(work), nil))

	handle := e.childID(t, viewerA, e.SlugA, parent)
	for _, path := range []string{
		ticketPathOf(e.SlugA, parent) + "/children/" + handle,
		ticketPathOf(e.SlugA, work) + "/links/" + blocks,
		ticketPathOf(e.SlugA, work) + "/links/blocks/" + e.SlugB + "/" + shortOf(pre),
		ticketPathOf(e.SlugA, work) + "/links/blocks/" + shortOf(parent),
	} {
		assertProblem(t, e.s.do(t, viewerA, http.MethodDelete, path, nil), http.StatusForbidden, "forbidden")
	}
	assert.Equal(t, parent.Key, e.readIn(t, both, e.SlugB, child).Parent.MustGet(), "the child stays")
	assert.Len(t, e.relations(t, memberA, e.SlugA, work, "link"), 1, "the link stays")
}

// A member of the other end's team still removes from that end: the child's
// update of its parent and the source's removal of its link, each recorded in
// A's record as system:relation, the caller holding no role there
// (docs/adr/0008 D2, docs/adr/0012 D2, D3).
func TestAMemberOfTheOtherEndsTeamStillRemovesFromItsSide(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	work := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Work in A"))
	pre := e.fileIn(t, memberB, e.SlugB, "BETA", task("Blocks it from B"))
	blocks := linkID(t, e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugB, pre)+"/links/blocks/"+e.SlugA+"/"+shortOf(work), nil))

	res := e.patchIn(t, memberB, e.SlugB, child, apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.Parent.IsNull())
	actor, _ := e.actOf(t, e.A, parent.Id, "detached")
	assert.Equal(t, "system:relation", actor, "the parent of A hears of it in A's record")
	actor, _ = e.actOf(t, e.B, child.Id, "updated")
	assert.Equal(t, e.MemberB.String(), actor)
	assert.False(t, e.readIn(t, memberA, e.SlugA, parent).ProgressDerived)

	del := e.s.do(t, memberB, http.MethodDelete, ticketPathOf(e.SlugB, pre)+"/links/"+blocks, nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)
	actor, _ = e.actOf(t, e.A, work.Id, "unlinked")
	assert.Equal(t, "system:relation", actor)
	actor, _ = e.actOf(t, e.B, pre.Id, "unlinked")
	assert.Equal(t, e.MemberB.String(), actor)
}

// An agent removes a relation in its baseline (docs/adr/0043 D2): its act in
// its own team carries its mark, the far act in the other team none, recorded
// as system:relation.
func TestAnAgentRemovesARelationInItsBaseline(t *testing.T) {
	e := newRelEnv(t)
	memberA, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/test/q7"}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))

	res, err := e.s.client(t, assisted).RemoveTicketChildWithResponse(e.ctx, e.SlugA, parent.Project, parent.Number,
		e.childID(t, memberA, e.SlugA, parent))
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode(), string(res.Body))
	actor, agent := e.actOf(t, e.A, parent.Id, "detached")
	assert.Equal(t, e.MemberA.String(), actor)
	assert.True(t, agent, "the agent's act in its own team carries its mark")
	actor, agent = e.actOf(t, e.B, child.Id, "updated")
	assert.Equal(t, "system:relation", actor)
	assert.False(t, agent, "the far act carries no mark of the caller's")
}

// A relation that does not touch the ticket in the path answers exactly as one
// that does not exist — a link of another ticket of the team, a link another
// team keeps between two other tickets, a child of another parent, a child no
// more —, and removes nothing (docs/adr/0008 D2, docs/adr/0012 D2 as amended
// 2026-10-10). A link of the team is removed from its target as from its
// source.
func TestARelationThatDoesNotTouchTheTicketAnswersAsAMissingOne(t *testing.T) {
	e := newRelEnv(t)
	memberA, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	x := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The ticket in the path"))
	y := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Another ticket of A"))
	z := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A third ticket of A"))
	pre := e.fileIn(t, both, e.SlugB, "BETA", task("A ticket of B"))
	inTeam := linkID(t, e.s.do(t, memberA, http.MethodPut, ticketPathOf(e.SlugA, y)+"/links/blocks/"+e.SlugA+"/"+shortOf(z), nil))
	elsewhere := linkID(t, e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugB, pre)+"/links/blocks/"+e.SlugA+"/"+shortOf(z), nil))

	miss := func(res *http.Response) map[string]any {
		t.Helper()
		body := assertProblem(t, res, http.StatusNotFound, "not_found")
		delete(body, "request_id")
		delete(body, "instance")
		return body
	}
	missing := miss(e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, x)+"/links/"+uuid.NewString(), nil))
	assert.Equal(t, "no such link", missing["detail"])
	for name, id := range map[string]string{"a link of the team": inTeam, "a link another team keeps": elsewhere} {
		assert.Equal(t, missing, miss(e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, x)+"/links/"+id, nil)), name)
	}
	assert.Len(t, e.relations(t, memberA, e.SlugA, z, "link"), 2, "both links stand")

	del := e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, z)+"/links/"+inTeam, nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode, "a link of the team is removed from its target")
	for _, tk := range []apigen.Ticket{y, z} {
		actor, _ := e.actOf(t, e.A, tk.Id, "unlinked")
		assert.Equal(t, e.MemberA.String(), actor, "the act is on both tickets")
	}

	p1 := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("One parent"))
	p2 := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Another parent"))
	c1 := e.fileIn(t, both, e.SlugB, "BETA", task("A child of the one", func(c *apigen.TicketCreate) { c.Parent = ptr(p1.Key) }))
	c2 := e.fileIn(t, both, e.SlugB, "BETA", task("A child of the other", func(c *apigen.TicketCreate) { c.Parent = ptr(p2.Key) }))
	h1, h2 := e.childID(t, memberA, e.SlugA, p1), e.childID(t, memberA, e.SlugA, p2)
	noChild := miss(e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, p1)+"/children/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", nil))
	assert.Equal(t, "no such child", noChild["detail"])
	assert.Equal(t, noChild, miss(e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, p1)+"/children/"+h2, nil)),
		"a child of another parent")
	require.Equal(t, http.StatusNoContent, e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, p1)+"/children/"+h1, nil).StatusCode)
	assert.Equal(t, noChild, miss(e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, p1)+"/children/"+h1, nil)),
		"a child no more")
	assert.True(t, e.readIn(t, both, e.SlugB, c1).Parent.IsNull())
	assert.Equal(t, p2.Key, e.readIn(t, both, e.SlugB, c2).Parent.MustGet(), "the other parent's child stays")
}

// A relation removed from both ends at once ends once, with no 500: a link
// across teams and one inside a team, removed by their ids — one removal and
// one miss, one act on each ticket —, and a child of B under a parent of A,
// removed from the parent's side while B's member clears its parent — the
// removal that comes second finds the relation gone or is refused as stale,
// and never writes the parent back; one act on each end
// (docs/adr/0008 D2, docs/adr/0012 D2 as amended 2026-10-10, docs/adr/0050 D1).
func TestARelationRemovedFromBothEndsAtOnceEndsOnce(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	byID := func(c caller, team string, tk apigen.Ticket, id string) func() int {
		return func() int {
			res, err := e.s.client(t, c).RemoveTicketLinkWithResponse(ctx, team, tk.Project, tk.Number, uuid.MustParse(id))
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
	}
	count := func(sql string, args ...any) int64 {
		n, err := e.f.QueryCount(ctx, sql, args...)
		require.NoError(t, err)
		return n
	}
	endedOnce := func(what string, round int, codes []int, a, b apigen.Ticket) {
		t.Helper()
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusNoContent, http.StatusNotFound}, codes, "round %d: %s", round, what)
		assert.Zero(t, count(`SELECT count(*) FROM ticket_links WHERE source_id IN ($1, $2) OR target_id IN ($1, $2)`, a.Id, b.Id),
			"round %d: %s is gone", round, what)
		for _, tk := range []apigen.Ticket{a, b} {
			assert.EqualValues(t, 1, count(`SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'unlinked'`, tk.Id),
				"round %d: %s is one act on %s", round, what, tk.Key)
		}
	}
	for round := range 8 {
		pre := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("pre %d", round)))
		work := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("work %d", round)))
		id := linkID(t, e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugB, pre)+"/links/blocks/"+e.SlugA+"/"+shortOf(work), nil))
		endedOnce("a link across teams", round, simultaneously(byID(memberA, e.SlugA, work, id), byID(memberB, e.SlugB, pre, id)), pre, work)

		a1 := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("a1 %d", round)))
		a2 := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("a2 %d", round)))
		id = linkID(t, e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugA, a1)+"/links/found-in/"+e.SlugA+"/"+shortOf(a2), nil))
		endedOnce("a link inside the team", round, simultaneously(byID(memberA, e.SlugA, a1, id), byID(memberA, e.SlugA, a2, id)), a1, a2)

		parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task(fmt.Sprintf("parent %d", round)))
		child := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("child %d", round), func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
		handle := e.childID(t, memberA, e.SlugA, parent)
		etag := strconv.Quote(strconv.Itoa(child.Version))
		codes := simultaneously(func() int {
			res, err := e.s.client(t, memberA).RemoveTicketChildWithResponse(ctx, e.SlugA, parent.Project, parent.Number, handle)
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}, func() int {
			res, err := e.s.client(t, memberB).UpdateTicketWithResponse(ctx, e.SlugB, child.Project, child.Number,
				&apigen.UpdateTicketParams{IfMatch: &etag}, apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
			if err != nil {
				return 0
			}
			return res.StatusCode()
		})
		assert.Contains(t, []int{http.StatusNoContent, http.StatusNotFound}, codes[0], "round %d: the parent's side", round)
		assert.Contains(t, []int{http.StatusOK, http.StatusPreconditionFailed}, codes[1], "round %d: the child's side", round)
		assert.False(t, codes[0] == http.StatusNotFound && codes[1] == http.StatusPreconditionFailed, "round %d: one of them ends it", round)
		assert.True(t, e.readIn(t, both, e.SlugB, child).Parent.IsNull(), "round %d: the parent is not written back", round)
		assert.EqualValues(t, 1, count(`SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2 AND action = 'detached'`,
			e.A, parent.Id), "round %d: one act on the parent", round)
		assert.EqualValues(t, 1, count(`SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2 AND action = 'updated'
			AND after ? 'parent'`, e.B, child.Id), "round %d: one act on the child", round)

		other := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("other %d", round), func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
		handle = e.childID(t, memberA, e.SlugA, parent)
		etag = strconv.Quote(strconv.Itoa(other.Version))
		codes = simultaneously(func() int {
			res, err := e.s.client(t, memberA).RemoveTicketChildWithResponse(ctx, e.SlugA, parent.Project, parent.Number, handle)
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}, func() int {
			res, err := e.s.client(t, memberB).UpdateTicketWithResponse(ctx, e.SlugB, other.Project, other.Number,
				&apigen.UpdateTicketParams{IfMatch: &etag}, apigen.TicketPatch{Title: ptr(fmt.Sprintf("retitled %d", round))})
			if err != nil {
				return 0
			}
			return res.StatusCode()
		})
		assert.Equal(t, http.StatusNoContent, codes[0], "round %d: the parent's side removes the child", round)
		assert.Contains(t, []int{http.StatusOK, http.StatusPreconditionFailed}, codes[1], "round %d: the child's own change", round)
		assert.True(t, e.readIn(t, both, e.SlugB, other).Parent.IsNull(), "round %d: a change of the child never writes its parent back", round)
	}
	require.NoError(t, ctx.Err(), "no removal waited on the other for long")
}

// A removal from the parent's side moves no version of the child, and a
// patch over the version the child was read at before writes it without its
// parent: the server reads the parent as it stands, never the client's
// (docs/adr/0050 D1 as made concrete 2026-10-10). The act on a child of the
// parent's own team is the caller's.
func TestAPatchAfterARemovalLeavesTheParentGone(t *testing.T) {
	e := newRelEnv(t)
	memberA, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))
	child := e.fileIn(t, both, e.SlugA, "ALPHA", task("A child in A", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	read := e.readIn(t, both, e.SlugA, child)
	require.Equal(t, parent.Key, read.Parent.MustGet())

	del := e.s.do(t, memberA, http.MethodDelete, ticketPathOf(e.SlugA, parent)+"/children/"+e.childID(t, memberA, e.SlugA, parent), nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)
	actor, _ := e.actOf(t, e.A, child.Id, "updated")
	assert.Equal(t, e.MemberA.String(), actor, "the act on a child of the team is the caller's")

	now := e.readIn(t, both, e.SlugA, child)
	assert.True(t, now.Parent.IsNull())
	assert.Equal(t, read.Version, now.Version, "the removal is a write on the parent")
	res := e.patchIn(t, both, e.SlugA, read, apigen.TicketPatch{Title: ptr("A new title")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.Parent.IsNull(), "the parent is not written back")
}

// The compare-and-set of a patch covers the parent as the patch read it: a
// write whose parent a removal from the parent's side cleared since — which
// moves no version — writes nothing, and one over the parent as it stands
// writes (docs/adr/0050 D1 as made concrete 2026-10-10).
func TestAPatchComparesTheParentItRead(t *testing.T) {
	ctx := context.Background()
	c := newCrossing(t)
	f := fixtures(t)
	db := openRuntime(t)
	_, err := db.Mutate(as(c.MemberA), c.A, func(w *store.Writer) error {
		row, err := w.GetWrittenTicket(ctx, writeq.GetWrittenTicketParams{TenantID: c.A, ID: c.CA})
		require.NoError(t, err)
		require.NotNil(t, row.ParentID)
		require.NoError(t, f.Exec(ctx, "UPDATE tickets SET parent_id = NULL WHERE id = $1", c.CA))
		params := writeq.UpdateTicketFieldsParams{TenantID: c.A, ID: c.CA, Version: row.Version, Type: row.Type, Title: "taken over",
			Severity: row.Severity, Security: row.Security, Threat: row.Threat, Effort: row.Effort, ParentID: row.ParentID,
			ReadParentID: row.ParentID, AssigneeID: row.AssigneeID, Progress: row.Progress, ProgressRefinement: row.ProgressRefinement,
			ProgressReview: row.ProgressReview, Confidential: row.Confidential}
		_, err = w.UpdateTicketFields(ctx, params)
		assert.ErrorIs(t, err, pgx.ErrNoRows, "the parent the patch read is gone")
		params.ParentID, params.ReadParentID = nil, nil
		_, err = w.UpdateTicketFields(ctx, params)
		assert.NoError(t, err, "over the parent as it stands")
		w.Record(store.Event{EntityType: "seed", Action: "created"})
		return nil
	})
	require.NoError(t, err)
	var parent *uuid.UUID
	require.NoError(t, f.QueryRow(ctx, "SELECT parent_id FROM tickets WHERE id = $1", c.CA).Scan(&parent))
	assert.Nil(t, parent)
}
