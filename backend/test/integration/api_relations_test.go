//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// The relations across projects and teams through the API (docs/adr/0005 D3,
// docs/adr/0008 D2, docs/adr/0012 D2–D7, docs/adr/0017 D3, docs/adr/0034 D4,
// docs/adr/0065 D5), proved for identities holding a role in one team, both or
// neither: a head and nothing more across a team, the placeholder where the
// reader may not see, a key the person does not read refused as a missing
// one, who may set a relation, the derived progress, the walks, the done act,
// and the restricted tokens.

// relEnv is a ticket environment with two more persons: a viewer of A who is a
// member of B, and an administrator of B.
type relEnv struct {
	ticketEnv
	f                    *fixture.DB
	ViewerAB, AdminB     uuid.UUID
	tkViewerAB, tkAdminB string
}

func newRelEnv(t *testing.T) relEnv {
	t.Helper()
	ctx := context.Background()
	e := relEnv{ticketEnv: newTicketEnv(t), f: fixtures(t)}
	var err error
	e.ViewerAB, err = e.f.Person(ctx, uniqueSlug("viewer-ab"), "Viewer of A, member of B")
	require.NoError(t, err)
	require.NoError(t, e.f.Member(ctx, e.A, e.ViewerAB, domain.RoleViewer))
	require.NoError(t, e.f.Member(ctx, e.B, e.ViewerAB, domain.RoleMember))
	e.AdminB, err = e.f.Person(ctx, uniqueSlug("admin-b"), "Administrator of B")
	require.NoError(t, err)
	require.NoError(t, e.f.Member(ctx, e.B, e.AdminB, domain.RoleAdmin))
	e.tkViewerAB = e.mint(t, fixture.TokenSpec{UserID: e.ViewerAB})
	e.tkAdminB = e.mint(t, fixture.TokenSpec{UserID: e.AdminB, Scope: domain.ScopeAdmin})
	return e
}

func (e relEnv) mint(t *testing.T, spec fixture.TokenSpec) string {
	t.Helper()
	plaintext, _, err := e.f.Token(context.Background(), spec)
	require.NoError(t, err)
	return plaintext
}

// fileIn files a ticket in a project of a team as c and requires the 201.
func (e relEnv) fileIn(t *testing.T, c caller, team, project string, body apigen.TicketCreate) apigen.Ticket {
	t.Helper()
	res := e.createIn(t, c, team, project, body)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	return *res.JSON201
}

func (e relEnv) createIn(t *testing.T, c caller, team, project string, body apigen.TicketCreate) *apigen.CreateTicketResponse {
	t.Helper()
	params := &apigen.CreateTicketParams{}
	if c.Agent != "" {
		params.IdempotencyKey = newKey()
	}
	res, err := e.s.client(t, c).CreateTicketWithResponse(e.ctx, team, project, params, body)
	require.NoError(t, err)
	return res
}

// patchIn changes a ticket of a team as c, over the version it was read at.
func (e relEnv) patchIn(t *testing.T, c caller, team string, tk apigen.Ticket, body apigen.TicketPatch) *apigen.UpdateTicketResponse {
	t.Helper()
	etag := strconv.Quote(strconv.Itoa(tk.Version))
	res, err := e.s.client(t, c).UpdateTicketWithResponse(e.ctx, team, tk.Project, tk.Number, &apigen.UpdateTicketParams{IfMatch: &etag}, body)
	require.NoError(t, err)
	return res
}

// readIn reads a ticket of a team as c and requires the 200.
func (e relEnv) readIn(t *testing.T, c caller, team string, tk apigen.Ticket) apigen.Ticket {
	t.Helper()
	res, err := e.s.client(t, c).GetTicketWithResponse(e.ctx, team, tk.Project, tk.Number)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200
}

// rawBody reads a path as c and answers its body, requiring the 200.
func (e relEnv) rawBody(t *testing.T, c caller, path string) []byte {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, path, nil)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode, string(body))
	return body
}

// relations lists a ticket's relations of a kind as c.
func (e relEnv) relations(t *testing.T, c caller, team string, tk apigen.Ticket, kind string) []apigen.Relation {
	t.Helper()
	var list apigen.RelationList
	require.NoError(t, json.Unmarshal(e.rawBody(t, c, ticketPathOf(team, tk)+"/relations?kind="+kind), &list))
	return list.Items
}

func ticketPathOf(team string, tk apigen.Ticket) string {
	return "/api/v1/teams/" + team + "/projects/" + tk.Project + "/tickets/" + strconv.Itoa(tk.Number)
}

// shortOf is a ticket's key within its team, <PROJECT>-<number>.
func shortOf(tk apigen.Ticket) string { return tk.Project + "-" + strconv.Itoa(tk.Number) }

// headKeys are the fields of a head, every one of them and nothing more
// (docs/adr/0005 D3).
var headKeys = []string{"team", "key", "title", "type", "state", "placeholder", "readable"}

// transitionIn moves a ticket of a team through the states as c.
func (e relEnv) transitionIn(t *testing.T, c caller, team string, tk apigen.Ticket, body apigen.Transition) *apigen.TransitionTicketResponse {
	t.Helper()
	res, err := e.s.client(t, c).TransitionTicketWithResponse(e.ctx, team, tk.Project, tk.Number, &apigen.TransitionTicketParams{}, body)
	require.NoError(t, err)
	return res
}

// toInProgress moves a filed ticket of a team to in-progress as c.
func (e relEnv) toInProgress(t *testing.T, c caller, team string, tk apigen.Ticket) apigen.Ticket {
	t.Helper()
	for _, step := range [][2]apigen.TicketState{{apigen.TicketStateFiled, apigen.TicketStateAnalysed},
		{apigen.TicketStateAnalysed, apigen.TicketStateDecided}, {apigen.TicketStateDecided, apigen.TicketStateInProgress}} {
		res := e.transitionIn(t, c, team, tk, apigen.Transition{From: step[0], To: step[1]})
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		tk = *res.JSON200
	}
	return tk
}

// A reader of team B's ticket reads team A's parent by its head and nothing
// more: the team, the key, the title, the type and the state — A's body, its
// assignee, its progress and its id absent from every byte of the answers
// (docs/adr/0005 D3, docs/adr/0008 D2).
func TestAParentInAnotherTeamShowsItsHeadOnly(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent of A", func(c *apigen.TicketCreate) {
		c.Body = ptr("SECRET-BODY-OF-A")
		c.Assignee = &e.MemberA
		c.Effort = apigen.EffortM
	}))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child of B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	assert.Equal(t, parent.Key, child.Parent.MustGet())

	for _, path := range []string{ticketPathOf(e.SlugB, child), ticketPathOf(e.SlugB, child) + "/relations?kind=parent"} {
		body := e.rawBody(t, memberB, path)
		for _, hidden := range []string{"SECRET-BODY-OF-A", parent.Id.String(), e.MemberA.String()} {
			assert.NotContains(t, string(body), hidden, "%s shows more of the parent than its head", path)
		}
	}
	var raw map[string]any
	require.NoError(t, json.Unmarshal(e.rawBody(t, memberB, ticketPathOf(e.SlugB, child)), &raw))
	head, ok := raw["parent_head"].(map[string]any)
	require.True(t, ok, "the parent's head: %v", raw["parent_head"])
	assert.ElementsMatch(t, headKeys, slices.Collect(maps.Keys(head)))
	assert.Equal(t, map[string]any{"slug": e.SlugA, "name": "Team A"}, head["team"])
	assert.Equal(t, parent.Key, head["key"])
	assert.Equal(t, "The parent of A", head["title"])
	assert.Equal(t, "task", head["type"])
	assert.Equal(t, "filed", head["state"])
	assert.Equal(t, false, head["placeholder"])
	assert.Equal(t, false, head["readable"], "a member of B alone does not open it")
	assert.Equal(t, parent.Key, raw["parent"])

	var list map[string]any
	require.NoError(t, json.Unmarshal(e.rawBody(t, memberB, ticketPathOf(e.SlugB, child)+"/relations?kind=parent"), &list))
	items := list["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.ElementsMatch(t, []string{"kind", "link", "head"}, slices.Collect(maps.Keys(item)))
	assert.ElementsMatch(t, headKeys, slices.Collect(maps.Keys(item["head"].(map[string]any))))

	assert.True(t, e.readIn(t, both, e.SlugB, child).ParentHead.MustGet().Readable, "a member of both teams opens it")
	children := e.relations(t, memberA, e.SlugA, parent, "child")
	require.Len(t, children, 1)
	assert.Equal(t, child.Key, children[0].Head.Key.MustGet(), "the parent's readers read the child's head")
	assert.False(t, children[0].Head.Readable)
}

// A confidential parent the reader may not see is `<team> [Confidential]`, in
// another team and in the reader's own (docs/adr/0065 D5 as amended
// 2026-10-10), while the lists, the search and the dashboard of its team still
// leave it out.
func TestAConfidentialRelationShowsThePlaceholder(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, memberA, memberB := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}
	secret := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("The confidential parent", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassBoundary, ptr("a member of team B reads the payroll")
	}))
	require.True(t, secret.Confidential)
	require.True(t, secret.Assignee.IsNull(), "a confidential ticket without an assignee")
	childA := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("A child of A", func(c *apigen.TicketCreate) { c.Parent = ptr(secret.Key) }))
	childB := e.fileIn(t, memberB, e.SlugB, "BETA", task("A child of B"))
	// Nobody of B may set it — no member of B reads the parent —; the relation
	// is made past the API to show what it looks like.
	require.NoError(t, e.f.Exec(ctx, "UPDATE tickets SET parent_id = $1 WHERE id = $2", secret.Id, childB.Id))

	for name, read := range map[string]apigen.Ticket{
		"a member of the child's team":                    e.readIn(t, memberB, e.SlugB, childB),
		"a member of the parent's own team, not admitted": e.readIn(t, memberA, e.SlugA, childA),
	} {
		assert.True(t, read.Parent.IsNull(), name)
		head := read.ParentHead.MustGet()
		assert.True(t, head.Placeholder, name)
		assert.False(t, head.Readable, name)
		assert.Equal(t, "Team A", head.Team.Name, name)
		assert.True(t, head.Key.IsNull() && head.Title.IsNull() && head.Type.IsNull() && head.State.IsNull(), name)
	}
	assert.True(t, e.readIn(t, adminA, e.SlugA, childA).ParentHead.MustGet().Readable, "its team's administrator reads it")

	assert.NotContains(t, e.titles(t, memberA, e.projectTickets("ALPHA"), ""), secret.Title, "the list leaves it out")
	search := e.rawBody(t, memberA, "/api/v1/teams/"+e.SlugA+"/search?q="+url.QueryEscape("confidential parent"))
	assert.NotContains(t, string(search), secret.Key, "the search leaves it out")
	dashboard := e.rawBody(t, memberA, "/api/v1/teams/"+e.SlugA+"/dashboard")
	assert.NotContains(t, string(dashboard), secret.Key, "the dashboard leaves it out")
	assert.Contains(t, string(e.rawBody(t, adminA, "/api/v1/teams/"+e.SlugA+"/dashboard")), secret.Key)
}

// problemOf decodes a problem body and drops the fields every answer has of
// its own: the request id and, given, more.
func problemOf(t *testing.T, raw []byte, drop ...string) map[string]any {
	t.Helper()
	var p map[string]any
	require.NoError(t, json.Unmarshal(raw, &p), string(raw))
	delete(p, "request_id")
	for _, d := range drop {
		delete(p, d)
	}
	return p
}

// A key the person does not read is refused exactly as a key that names
// nothing — the same status, code and body but its request id (and, on a
// link, the path it names) —, whatever the reason: no such team, a team the
// person holds no role in, a token restricted to another team, no such
// project, a project restricted from them, no such number, a deleted ticket,
// a confidential one they are not admitted to (docs/adr/0008 D2,
// docs/adr/0012 D2).
func TestAnUnreadableKeyAnswersAsAMissingOne(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	plain := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("plain"))
	secret := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("secret", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("it leaks")
	}))
	gone := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("deleted"))
	require.NoError(t, e.f.Exec(ctx, "UPDATE tickets SET deleted_at = now(), deleted_by = $1 WHERE id = $2", e.AdminA, gone.Id))
	gamma, err := e.f.Project(ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	require.NoError(t, e.f.Exec(ctx, "UPDATE projects SET restricted = true WHERE id = $1", gamma))
	_, restricted, err := e.f.Ticket(ctx, e.A, gamma, e.AdminA, "restricted")
	require.NoError(t, err)
	child := e.fileIn(t, both, e.SlugB, "BETA", task("child of B"))
	teamToken := caller{Token: e.mint(t, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})}

	type miss struct {
		c         caller
		team, key string
	}
	cases := map[string]miss{
		"no such team":                           {both, "no-such-team", "ALPHA-1"},
		"a team the person holds no role in":     {memberB, e.SlugA, shortOf(plain)},
		"a token restricted to another team":     {teamToken, e.SlugA, shortOf(plain)},
		"no such project":                        {both, e.SlugA, "NOPE-1"},
		"a project restricted from the person":   {both, e.SlugA, fmt.Sprintf("GAMMA-%d", restricted)},
		"no such number":                         {both, e.SlugA, "ALPHA-99999"},
		"a deleted ticket":                       {both, e.SlugA, shortOf(gone)},
		"a confidential ticket, not admitted to": {both, e.SlugA, shortOf(secret)},
	}

	parentMiss := func(c caller, key string) map[string]any {
		t.Helper()
		res := e.patchIn(t, c, e.SlugB, child, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(key)})
		require.Equal(t, http.StatusBadRequest, res.StatusCode(), "%s: %s", key, res.Body)
		return problemOf(t, res.Body)
	}
	filingMiss := func(c caller, key string) map[string]any {
		t.Helper()
		res := e.createIn(t, c, e.SlugB, "BETA", task("filed", func(b *apigen.TicketCreate) { b.Parent = ptr(key) }))
		require.Equal(t, http.StatusBadRequest, res.StatusCode(), "%s: %s", key, res.Body)
		return problemOf(t, res.Body)
	}
	linkMiss := func(c caller, team, key string) map[string]any {
		t.Helper()
		res := e.s.do(t, c, http.MethodPut, ticketPathOf(e.SlugB, child)+"/links/blocks/"+team+"/"+key, nil)
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s/%s: %s", team, key, body)
		return problemOf(t, body, "instance")
	}
	baseParent := parentMiss(both, e.SlugB+"/BETA-99999")
	assert.Equal(t, "validation_failed", baseParent["code"])
	assert.Equal(t, "no such ticket", baseParent["detail"])
	baseFiling := filingMiss(both, e.SlugB+"/BETA-99999")
	baseLink := linkMiss(both, e.SlugB, "BETA-99999")
	assert.Equal(t, "not_found", baseLink["code"])
	assert.Equal(t, "no such ticket", baseLink["detail"])
	for name, m := range cases {
		key := m.team + "/" + m.key
		assert.Equal(t, baseParent, parentMiss(m.c, key), "the parent: %s", name)
		assert.Equal(t, baseFiling, filingMiss(m.c, key), "a filing's parent: %s", name)
		assert.Equal(t, baseLink, linkMiss(m.c, m.team, m.key), "a link's other end: %s", name)
	}

	// Inside the team the short form answers the same: a project restricted
	// from the person, or a confidential ticket, is a missing one.
	source := e.fileIn(t, both, e.SlugA, "ALPHA", task("source in A"))
	short := func(other string) map[string]any {
		t.Helper()
		res := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugA, source)+"/links/relates-to/"+other, nil)
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, res.StatusCode, "%s: %s", other, body)
		return problemOf(t, body, "instance")
	}
	baseShort := short("ALPHA-99999")
	assert.Equal(t, baseShort, short(fmt.Sprintf("GAMMA-%d", restricted)), "a project restricted from the person")
	assert.Equal(t, baseShort, short(shortOf(secret)), "a confidential ticket, not admitted to")

	res := e.patchIn(t, both, e.SlugB, child, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(plain.Key)})
	require.Equal(t, http.StatusOK, res.StatusCode(), "a key the person reads is a parent: %s", res.Body)
}

// Setting a parent or a link takes a member of the child's, or the source's,
// team who reads the other end — a viewer of the other team suffices —; a
// member of the child's team alone is refused as for a missing key, and a
// viewer of the other team writes nothing there (docs/adr/0008 D2,
// docs/adr/0012 D2).
func TestAViewerOfTheParentsTeamSetsIt(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, viewerAB := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tkViewerAB}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))

	child := e.fileIn(t, viewerAB, e.SlugB, "BETA", task("A child in B"))
	res := e.patchIn(t, viewerAB, e.SlugB, child, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(parent.Key)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, parent.Key, res.JSON200.Parent.MustGet())
	filed := e.fileIn(t, viewerAB, e.SlugB, "BETA", task("Filed under A", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	assert.Equal(t, parent.Key, filed.Parent.MustGet())
	link := e.s.do(t, viewerAB, http.MethodPut, ticketPathOf(e.SlugB, child)+"/links/relates-to/"+e.SlugA+"/"+shortOf(parent), nil)
	assert.Equal(t, http.StatusCreated, link.StatusCode)

	alone := e.fileIn(t, memberB, e.SlugB, "BETA", task("A child of a member of B alone"))
	res = e.patchIn(t, memberB, e.SlugB, alone, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(parent.Key)})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode())
	assert.Equal(t, http.StatusBadRequest, e.createIn(t, memberB, e.SlugB, "BETA",
		task("x", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) })).StatusCode())
	link = e.s.do(t, memberB, http.MethodPut, ticketPathOf(e.SlugB, alone)+"/links/relates-to/"+e.SlugA+"/"+shortOf(parent), nil)
	assert.Equal(t, http.StatusNotFound, link.StatusCode)

	// The viewer writes nothing in A: the act on A's ticket needs a member.
	res = e.patchIn(t, viewerAB, e.SlugA, parent, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(child.Key)})
	assert.Equal(t, http.StatusForbidden, res.StatusCode())
}

// A parent's derived progress counts its children in other teams, written
// through the crossing, its version unchanged; a child's deletion, its
// restoration and its move take it out and back (docs/adr/0017 D3 as amended
// 2026-10-10).
func TestDerivedProgressCountsAChildInAnotherTeam(t *testing.T) {
	e := newRelEnv(t)
	memberA, both, adminB := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}, caller{Token: e.tkAdminB}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent"))
	childA := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A child of A", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	res := e.patchIn(t, memberA, e.SlugA, childA, apigen.TicketPatch{Progress: ptr(50)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	childB := e.fileIn(t, both, e.SlugB, "BETA", task("A child of B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))

	before := e.readIn(t, memberA, e.SlugA, parent)
	assert.Equal(t, 25, before.Progress, "the mean of 50 and 0, both S")
	res = e.patchIn(t, both, e.SlugB, childB, apigen.TicketPatch{Progress: ptr(100)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	childB = *res.JSON200
	after := e.readIn(t, memberA, e.SlugA, parent)
	assert.Equal(t, 75, after.Progress, "the child of B counts")
	assert.True(t, after.ProgressDerived)
	assert.Equal(t, before.Version, after.Version, "the derived write moves no version")

	del := e.s.do(t, adminB, http.MethodDelete, ticketPathOf(e.SlugB, childB), nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)
	assert.Equal(t, 50, e.readIn(t, memberA, e.SlugA, parent).Progress, "a deleted child counts no more")
	restore := e.s.do(t, adminB, http.MethodPut, "/api/v1/teams/"+e.SlugB+"/deleted-tickets/"+url.PathEscape(shortOf(childB))+"/restore", nil)
	require.Equal(t, http.StatusOK, restore.StatusCode)
	assert.Equal(t, 75, e.readIn(t, memberA, e.SlugA, parent).Progress, "restored, it counts again")
	childB = e.readIn(t, both, e.SlugB, childB)
	res = e.patchIn(t, both, e.SlugB, childB, apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, 50, e.readIn(t, memberA, e.SlugA, parent).Progress, "moved away, it counts no more")
}

// A writer of team B moves the derived progress of a parent of team A and
// nothing else of it: when B's child moves away, the parent shows its own
// stages as its own team left them — never the last derived value seeded into
// them —, its version and its updated_at unmoved (docs/adr/0017 D3 as made
// concrete 2026-10-10).
func TestAChildOfAnotherTeamNeverRewritesItsParentsOwnProgress(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent of A"))
	res := e.patchIn(t, memberA, e.SlugA, parent, apigen.TicketPatch{ProgressRefinement: ptr(20)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	own := *res.JSON200
	require.Equal(t, 20, own.ProgressRefinement)

	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child of B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	res = e.patchIn(t, memberB, e.SlugB, child, apigen.TicketPatch{ProgressRefinement: ptr(95)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	child = *res.JSON200
	derived := e.readIn(t, memberA, e.SlugA, parent)
	assert.True(t, derived.ProgressDerived)
	assert.Equal(t, 95, derived.ProgressRefinement, "the child of B counts")

	res = e.patchIn(t, memberB, e.SlugB, child, apigen.TicketPatch{Parent: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	after := e.readIn(t, memberA, e.SlugA, parent)
	assert.False(t, after.ProgressDerived)
	assert.Equal(t, 20, after.ProgressRefinement, "the stage a member of A set, not the value B's child left")
	assert.Equal(t, own.Version, after.Version, "no version moved")
	assert.Equal(t, own.UpdatedAt, after.UpdatedAt, "the write of team B leaves the parent's updated_at")
	stored, err := e.f.QueryCount(e.ctx, `SELECT progress_refinement FROM tickets WHERE id = $1`, parent.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 20, stored, "the parent's own column is untouched")
}

// The parent's team hears of the derived change on its stream, as the event of
// the kind derived — a stream that does not see the parent's project hears
// nothing of it —, for a child of another team and for one of its own, whose
// act names the child alone (docs/adr/0017 D3, docs/adr/0054 D2 as made
// concrete 2026-10-10, D3).
func TestTheDerivedProgressReachesTheParentsTeamStream(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, memberA, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent"))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child of B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	gamma, err := e.f.Project(ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	require.NoError(t, e.f.Exec(ctx, "UPDATE projects SET restricted = true WHERE id = $1", gamma))
	hiddenID, hiddenNumber, err := e.f.Ticket(ctx, e.A, gamma, e.AdminA, "A parent in a restricted project")
	require.NoError(t, err)
	hiddenChild := e.fileIn(t, both, e.SlugB, "BETA", task("Another child of B"))
	require.NoError(t, e.f.Exec(ctx, "UPDATE tickets SET parent_id = $1 WHERE id = $2", hiddenID, hiddenChild.Id))

	memberStream := e.openStream(t, e.s, memberA, e.SlugA, "")
	adminStream := e.openStream(t, e.s, adminA, e.SlugA, "")
	res := e.patchIn(t, both, e.SlugB, child, apigen.TicketPatch{Progress: ptr(40)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	m, ok := memberStream.next(t, 5*time.Second)
	require.True(t, ok, "the parent's team hears of it")
	assert.Equal(t, "ticket.changed", m.Event)
	assert.Equal(t, parent.Key, eventKey(t, m))
	assert.Contains(t, m.Data, `"kind":"derived"`)
	_, ok = adminStream.next(t, 5*time.Second)
	require.True(t, ok)

	res = e.patchIn(t, both, e.SlugB, hiddenChild, apigen.TicketPatch{Progress: ptr(40)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	m, ok = adminStream.next(t, 5*time.Second)
	require.True(t, ok, "an administrator sees the restricted project")
	assert.Equal(t, fmt.Sprintf("%s/GAMMA-%d", e.SlugA, hiddenNumber), eventKey(t, m))
	m, ok = memberStream.next(t, time.Second)
	assert.False(t, ok, "a member the project is restricted from hears nothing: %v", m)

	own := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A parent with a child of its own team"))
	ownChild := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A child of A", func(c *apigen.TicketCreate) { c.Parent = ptr(own.Key) }))
	for {
		if _, ok := memberStream.next(t, time.Second); !ok {
			break
		}
	}
	res = e.patchIn(t, memberA, e.SlugA, ownChild, apigen.TicketPatch{Progress: ptr(60)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	heard := map[string]string{}
	for {
		m, ok := memberStream.next(t, 2*time.Second)
		if !ok {
			break
		}
		heard[eventKey(t, m)] = m.Data
	}
	require.Contains(t, heard, own.Key, "the parent of the same team is announced: %v", heard)
	assert.Contains(t, heard[own.Key], `"kind":"derived"`)
	assert.Contains(t, heard, ownChild.Key, "the child's act as before")
}

// A parent cycle and a blocks cycle through two teams are refused
// (docs/adr/0008 D2, docs/adr/0012 D4).
func TestCyclesAcrossTeamsAreRefused(t *testing.T) {
	e := newRelEnv(t)
	both := caller{Token: e.tk.Both}
	a := e.fileIn(t, both, e.SlugA, "ALPHA", task("a"))
	b := e.fileIn(t, both, e.SlugB, "BETA", task("b", func(c *apigen.TicketCreate) { c.Parent = ptr(a.Key) }))
	res := e.patchIn(t, both, e.SlugA, a, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(b.Key)})
	require.Equal(t, http.StatusConflict, res.StatusCode(), string(res.Body))
	assert.Equal(t, "parent_cycle", string(res.ApplicationproblemJSONDefault.Code))

	link := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugA, a)+"/links/blocks/"+e.SlugB+"/"+shortOf(b), nil)
	require.Equal(t, http.StatusCreated, link.StatusCode)
	back := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugB, b)+"/links/blocks/"+e.SlugA+"/"+shortOf(a), nil)
	assertProblem(t, back, http.StatusConflict, "link_cycle")
}

// Two writers that cross the same two teams in opposite directions both
// finish: one installation-wide lock per graph, taken first, orders them — a
// cycle is refused, a disjoint pair is set, and nothing waits on the other or
// fails with a 500 (docs/developer/data-access.md#advisory-locks).
func TestTwoWritersCrossingTwoTeamsInOppositeDirectionsBothFinish(t *testing.T) {
	e := newRelEnv(t)
	both := caller{Token: e.tk.Both}
	cl := e.s.client(t, both)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	setParent := func(team string, child, parent apigen.Ticket) func() int {
		return func() int {
			etag := strconv.Quote(strconv.Itoa(child.Version))
			res, err := cl.UpdateTicketWithResponse(ctx, team, child.Project, child.Number, &apigen.UpdateTicketParams{IfMatch: &etag},
				apigen.TicketPatch{Parent: nullable.NewNullableWithValue(parent.Key)})
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
	}
	setBlocks := func(team string, source, target apigen.Ticket, targetTeam string) func() int {
		return func() int {
			res, err := cl.LinkTicketToWithResponse(ctx, team, source.Project, source.Number, apigen.LinkTypeBlocks, targetTeam, shortOf(target))
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
	}
	for round := range 5 {
		x := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("x%d", round)))
		y := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("y%d", round)))
		codes := simultaneously(setParent(e.SlugA, x, y), setParent(e.SlugB, y, x))
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusOK, http.StatusConflict}, codes, "round %d: the cyclic pair", round)

		p := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("p%d", round)))
		q := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("q%d", round)))
		r := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("r%d", round)))
		s := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("s%d", round)))
		codes = simultaneously(setParent(e.SlugA, p, q), setParent(e.SlugB, s, r))
		assert.Equal(t, []int{http.StatusOK, http.StatusOK}, codes, "round %d: a disjoint pair", round)

		u := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("u%d", round)))
		v := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("v%d", round)))
		codes = simultaneously(setBlocks(e.SlugA, u, v, e.SlugB), setBlocks(e.SlugB, v, u, e.SlugA))
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusCreated, http.StatusConflict}, codes, "round %d: the cyclic blocks pair", round)
	}
	require.NoError(t, ctx.Err(), "no writer waited on the other")
}

// Two people setting the same link at once — a relates-to from both ends,
// across teams and inside one, or the same directed link twice — both get the
// answer for the link that now exists, whichever end stored it: one 201 and
// one 200, never a 500. One row stands, with one act on each ticket
// (docs/adr/0045 D1, docs/adr/0012 D1, D3).
func TestALinkSetAtOnceFromBothEndsIsOneLink(t *testing.T) {
	e := newRelEnv(t)
	both := caller{Token: e.tk.Both}
	cl := e.s.client(t, both)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	canonical := func(team string, source apigen.Ticket, typ apigen.LinkType, otherTeam string, other apigen.Ticket) func() int {
		return func() int {
			res, err := cl.LinkTicketToWithResponse(ctx, team, source.Project, source.Number, typ, otherTeam, shortOf(other))
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
	}
	short := func(team string, source apigen.Ticket, typ apigen.LinkType, other apigen.Ticket) func() int {
		return func() int {
			res, err := cl.LinkTicketsWithResponse(ctx, team, source.Project, source.Number, typ, shortOf(other))
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
	oneLink := func(what string, round int, a, b apigen.Ticket, codes []int) {
		t.Helper()
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusOK, http.StatusCreated}, codes, "round %d: %s", round, what)
		assert.EqualValues(t, 1, count(`SELECT count(*) FROM ticket_links
			WHERE (source_id = $1 AND target_id = $2) OR (source_id = $2 AND target_id = $1)`, a.Id, b.Id),
			"round %d: %s is one row", round, what)
		for _, tk := range []apigen.Ticket{a, b} {
			assert.EqualValues(t, 1, count(`SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'linked'`, tk.Id),
				"round %d: %s is one act on %s", round, what, tk.Key)
		}
	}
	for round := range 8 {
		a := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("a%d", round)))
		b := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("b%d", round)))
		oneLink("a relates-to across teams from both ends", round, a, b, simultaneously(
			canonical(e.SlugA, a, apigen.LinkTypeRelatesTo, e.SlugB, b), canonical(e.SlugB, b, apigen.LinkTypeRelatesTo, e.SlugA, a)))

		c := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("c%d", round)))
		d := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("d%d", round)))
		oneLink("a relates-to inside the team from both ends", round, c, d, simultaneously(
			short(e.SlugA, c, apigen.LinkTypeRelatesTo, d), short(e.SlugA, d, apigen.LinkTypeRelatesTo, c)))

		f := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("f%d", round)))
		g := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("g%d", round)))
		oneLink("a blocks link across teams twice", round, f, g, simultaneously(
			times(2, canonical(e.SlugA, f, apigen.LinkTypeBlocks, e.SlugB, g))...))

		h := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("h%d", round)))
		i := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("i%d", round)))
		oneLink("a found-in link inside the team twice", round, h, i, simultaneously(
			times(2, short(e.SlugA, h, apigen.LinkTypeFoundIn, i))...))
	}
	require.NoError(t, ctx.Err(), "no writer waited on the other for long")
}

// Two people setting a ticket's parent at once, with the same If-Match: one
// write wins and the other is told the ticket moved on — 412, never a 500 —,
// and the parent is set once (docs/adr/0050 D5, docs/adr/0008 D2).
func TestAParentSetTwiceAtOnceIsSetOnce(t *testing.T) {
	e := newRelEnv(t)
	both := caller{Token: e.tk.Both}
	cl := e.s.client(t, both)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for round := range 8 {
		parent := e.fileIn(t, both, e.SlugA, "ALPHA", task(fmt.Sprintf("parent%d", round)))
		child := e.fileIn(t, both, e.SlugB, "BETA", task(fmt.Sprintf("child%d", round)))
		etag := strconv.Quote(strconv.Itoa(child.Version))
		set := func() int {
			res, err := cl.UpdateTicketWithResponse(ctx, e.SlugB, child.Project, child.Number, &apigen.UpdateTicketParams{IfMatch: &etag},
				apigen.TicketPatch{Parent: nullable.NewNullableWithValue(parent.Key)})
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
		codes := simultaneously(set, set)
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusOK, http.StatusPreconditionFailed}, codes, "round %d", round)
		now := e.readIn(t, both, e.SlugB, child)
		assert.Equal(t, parent.Key, now.Parent.MustGet(), "round %d", round)
		assert.Equal(t, child.Version+1, now.Version, "round %d: one write", round)
	}
	require.NoError(t, ctx.Err())
}

// Closing a parent and its child at once, where the parent also blocks the
// child, finishes both — across teams and inside one. The child's done moves
// its rank and so locks its row; the parent's done tells the child's watchers,
// whose notifications reference that row; the child's refresh of its parent
// waits on the parent's row. Before the rank's unique index left the cleared
// rank out, the child's row was locked as for a change of its key, the
// notifications' reference waited on it, and one of the two failed as a
// deadlock (docs/adr/0014 D2: the rank is unique among the ranked tickets).
func TestClosingAParentAndTheChildItBlocksAtOnceFinishesBoth(t *testing.T) {
	e := newRelEnv(t)
	memberA, both, adminA, adminB := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}, caller{Token: e.tk.AdminA},
		caller{Token: e.tkAdminB}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	done := func(c caller, team string, tk apigen.Ticket, override bool) func() int {
		body := apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDone, Note: ptr("verified")}
		if override {
			body.OverridePrerequisites, body.Reason = ptr(true), ptr("closed with its parent")
		}
		return func() int {
			res, err := e.s.client(t, c).TransitionTicketWithResponse(ctx, team, tk.Project, tk.Number, &apigen.TransitionTicketParams{}, body)
			if err != nil {
				return 0
			}
			return res.StatusCode()
		}
	}
	for round := range 12 {
		for _, across := range []bool{true, false} {
			team, project, reporter := e.SlugA, "ALPHA", adminA
			if across {
				team, project, reporter = e.SlugB, "BETA", adminB
			}
			parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task(fmt.Sprintf("parent %d %t", round, across)))
			child := e.fileIn(t, reporter, team, project, task(fmt.Sprintf("child %d %t", round, across)))
			res := e.patchIn(t, both, team, child, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(parent.Key)})
			require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
			link := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugA, parent)+"/links/blocks/"+team+"/"+shortOf(child), nil)
			require.Equal(t, http.StatusCreated, link.StatusCode)
			codes := simultaneously(done(both, team, child, true), done(memberA, e.SlugA, parent, false))
			assert.Equal(t, []int{http.StatusOK, http.StatusOK}, codes, "round %d, across teams %t", round, across)
		}
	}
	require.NoError(t, ctx.Err())
}

// A node of the prerequisite tree that the reader sees by its head shows its
// team, key, title, type and state and nothing more: not the state a blocked
// ticket came from, which a reader of the ticket reads (docs/adr/0005 D3,
// docs/adr/0012 D6 as amended 2026-10-10).
func TestAHeadInThePrerequisiteTreeShowsNoBlockedFrom(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	root := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Waits in A"))
	pre := e.fileIn(t, memberB, e.SlugB, "BETA", task("Blocked in B"))
	link := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugB, pre)+"/links/blocks/"+e.SlugA+"/"+shortOf(root), nil)
	require.Equal(t, http.StatusCreated, link.StatusCode)
	res := e.transitionIn(t, memberB, e.SlugB, pre, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateBlocked,
		Reason: ptr("waits on a vendor"), Block: &apigen.BlockSet{Kind: apigen.BlockKindExternal}})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))

	node := func(c caller) apigen.PrerequisiteHeadNode {
		t.Helper()
		var tree apigen.PrerequisiteHeadTree
		require.NoError(t, json.Unmarshal(e.rawBody(t, c, ticketPathOf(e.SlugA, root)+"/prerequisite-tree"), &tree))
		require.Len(t, tree.Items, 1)
		return tree.Items[0]
	}
	head := node(memberA)
	assert.False(t, head.Head.Readable, "a member of A alone reads the head")
	assert.Equal(t, apigen.TicketStateBlocked, head.Head.State.MustGet())
	assert.True(t, head.BlockedFrom.IsNull(), "a head shows not where the blocked ticket stood")
	read := node(both)
	assert.True(t, read.Head.Readable)
	assert.Equal(t, apigen.TicketStateFiled, read.BlockedFrom.MustGet(), "a reader of the ticket reads it")
}

// done is refused over an open prerequisite in another team whose state the
// closer reads in its head, unless a person overrides with a reason; an agent
// cannot; a placeholder neither shows nor refuses (docs/adr/0012 D7 as
// amended 2026-10-10).
func TestDoneIsRefusedOverAnOpenPrerequisiteInAnotherTeam(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, memberA, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	agent := caller{Token: e.mint(t, fixture.TokenSpec{UserID: e.Both, Agent: true}), Agent: "claude-code/test/7f3a"}
	pre := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The prerequisite in A"))
	blocked := e.fileIn(t, memberB, e.SlugB, "BETA", task("Blocked in B"))
	link := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugA, pre)+"/links/blocks/"+e.SlugB+"/"+shortOf(blocked), nil)
	require.Equal(t, http.StatusCreated, link.StatusCode)
	assert.Equal(t, 1, e.readIn(t, memberB, e.SlugB, blocked).OpenPrerequisites, "a member of B alone counts the head")

	blocked = e.toInProgress(t, both, e.SlugB, blocked)
	done := apigen.Transition{From: apigen.TicketStateInProgress, To: apigen.TicketStateDone, Note: ptr("verified")}
	res := e.transitionIn(t, both, e.SlugB, blocked, done)
	require.Equal(t, http.StatusConflict, res.StatusCode(), string(res.Body))
	refusal := res.ApplicationproblemJSONDefault
	assert.Equal(t, "open_prerequisites", string(refusal.Code))
	require.NotNil(t, refusal.Errors)
	assert.Contains(t, (*refusal.Errors)[0].Message, pre.Key+": The prerequisite in A")

	over := done
	over.OverridePrerequisites, over.Reason = ptr(true), ptr("the change in A is not needed after all")
	res = e.transitionIn(t, agent, e.SlugB, blocked, over)
	assert.Equal(t, http.StatusForbidden, res.StatusCode(), "an agent cannot override")
	res = e.transitionIn(t, both, e.SlugB, blocked, over)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))

	secret := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("A confidential prerequisite", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("it leaks")
	}))
	quiet := e.fileIn(t, memberB, e.SlugB, "BETA", task("Blocked by a confidential ticket"))
	require.NoError(t, e.f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'blocks', $2, $3, $4)`, e.A, secret.Id, quiet.Id, e.AdminA))
	assert.Zero(t, e.readIn(t, memberB, e.SlugB, quiet).OpenPrerequisites, "a placeholder is never counted")
	quiet = e.toInProgress(t, memberB, e.SlugB, quiet)
	res = e.transitionIn(t, memberB, e.SlugB, quiet, done)
	assert.Equal(t, http.StatusOK, res.StatusCode(), "a placeholder does not refuse done: %s", res.Body)
}

// A project restricted from a member shows its ticket's head at the end of a
// relation, as to a person outside the team; every other surface keeps the
// restriction (docs/adr/0034 D4 as made concrete 2026-10-10).
func TestARestrictedProjectShowsTheHead(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	memberA := caller{Token: e.tk.MemberA}
	gamma, err := e.f.Project(ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	require.NoError(t, e.f.Exec(ctx, "UPDATE projects SET restricted = true WHERE id = $1", gamma))
	id, number, err := e.f.Ticket(ctx, e.A, gamma, e.AdminA, "In a restricted project")
	require.NoError(t, err)
	child := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A child"))
	require.NoError(t, e.f.Exec(ctx, "UPDATE tickets SET parent_id = $1 WHERE id = $2", id, child.Id))

	head := e.readIn(t, memberA, e.SlugA, child).ParentHead.MustGet()
	assert.False(t, head.Placeholder)
	assert.False(t, head.Readable, "a head the member does not open")
	assert.Equal(t, fmt.Sprintf("%s/GAMMA-%d", e.SlugA, number), head.Key.MustGet())
	assert.Equal(t, "In a restricted project", head.Title.MustGet())
	res := e.s.do(t, memberA, http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/projects/GAMMA/tickets/%d", e.SlugA, number), nil)
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "the ticket itself stays behind the restriction")
}

// A token restricted to one team — or to one of its projects — reads every
// other team's tickets by their heads, even of a team its person belongs to
// (docs/adr/0035 D3, docs/adr/0005 D3).
func TestARestrictedTokenReadsAStrangersHeads(t *testing.T) {
	e := newRelEnv(t)
	memberA, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	assert.True(t, e.readIn(t, both, e.SlugB, child).ParentHead.MustGet().Readable)
	for name, token := range map[string]fixture.TokenSpec{
		"restricted to team B":       {UserID: e.Both, TenantID: e.B},
		"restricted to project BETA": {UserID: e.Both, TenantID: e.B, ProjectID: e.ProjectB},
	} {
		head := e.readIn(t, caller{Token: e.mint(t, token)}, e.SlugB, child).ParentHead.MustGet()
		assert.False(t, head.Readable, name)
		assert.Equal(t, parent.Key, head.Key.MustGet(), name)
	}
}

// When a ticket reaches done or dropped, the watchers of the tickets it blocks
// in other teams are told as in its own team: an act on the blocked ticket in
// its own team's record names the prerequisite in its refs alone — its
// activity entry redacted for every reader there, no head of the prerequisite
// stored, so nothing of it outlives a later confidential flag or a purge —,
// and a closer who holds no role in that team is recorded as system:relation,
// with no token or agent mark (docs/adr/0012 D5 as made concrete 2026-10-10,
// docs/adr/0026 D1).
func TestTheWatchersOfAnotherTeamAreToldWhenAPrerequisiteSettles(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, memberA, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	toldOf := func(blocked apigen.Ticket) apigen.InboxEntry {
		t.Helper()
		var inbox apigen.InboxList
		require.NoError(t, json.Unmarshal(e.rawBody(t, memberB, "/api/v1/me/inbox"), &inbox))
		for _, n := range inbox.Items {
			if n.Reason == apigen.InboxReasonBlockerClosed && n.Ticket.Key == blocked.Key {
				return n
			}
		}
		t.Fatalf("the blocked ticket's watcher is told: %+v", inbox.Items)
		return apigen.InboxEntry{}
	}
	stored := func(blocked, pre apigen.Ticket) (actor string, token, after bool) {
		t.Helper()
		require.NoError(t, e.f.QueryRow(ctx, `SELECT coalesce(actor_system, actor_user_id::text), token_id IS NOT NULL, after IS NOT NULL
			FROM audit_events WHERE tenant_id = $1 AND ticket_id = $2 AND action = 'prerequisite_settled' AND $3 = ANY (refs)`,
			e.B, blocked.Id, pre.Id).Scan(&actor, &token, &after))
		return actor, token, after
	}
	settle := func(c caller, pre, blocked apigen.Ticket) {
		t.Helper()
		link := e.s.do(t, both, http.MethodPut, ticketPathOf(e.SlugA, pre)+"/links/blocks/"+e.SlugB+"/"+shortOf(blocked), nil)
		require.Equal(t, http.StatusCreated, link.StatusCode)
		res := e.transitionIn(t, c, e.SlugA, pre, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDone, Note: ptr("done")})
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	}

	pre := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The prerequisite in A"))
	blocked := e.fileIn(t, memberB, e.SlugB, "BETA", task("Blocked in B"))
	settle(memberA, pre, blocked)
	told := toldOf(blocked)
	assert.Equal(t, apigen.AuditActionPrerequisiteSettled, told.Act.Action)
	assert.True(t, told.Act.Redacted, "the act names a ticket of another team")
	assert.True(t, told.Blocker.IsNull(), "the blocked ticket's relations name the prerequisite, the inbox does not")
	assert.Equal(t, "system:relation", told.Act.ActorSystem.MustGet(), "the closer holds no role in B")
	assert.True(t, told.Act.Actor.IsNull())
	assert.True(t, told.Act.Token.IsNull())
	actor, token, after := stored(blocked, pre)
	assert.Equal(t, "system:relation", actor)
	assert.False(t, token, "no token of the closer in B's record")
	assert.False(t, after, "no head of the prerequisite in B's record")

	byBoth := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A prerequisite a member of both closes"))
	alsoBlocked := e.fileIn(t, memberB, e.SlugB, "BETA", task("Also blocked in B"))
	settle(both, byBoth, alsoBlocked)
	actor, _, _ = stored(alsoBlocked, byBoth)
	assert.Equal(t, e.Both.String(), actor, "a closer who holds a role in B is the actor there")

	secret := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("A confidential prerequisite", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("it leaks")
	}))
	quiet := e.fileIn(t, memberB, e.SlugB, "BETA", task("Blocked by a confidential ticket"))
	require.NoError(t, e.f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'blocks', $2, $3, $4)`, e.A, secret.Id, quiet.Id, e.AdminA))
	res := e.transitionIn(t, adminA, e.SlugA, secret, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped, Reason: ptr("not needed")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	told = toldOf(quiet)
	assert.True(t, told.Blocker.IsNull(), "a confidential prerequisite is named by no key")
	assert.True(t, told.Act.Redacted)
	assert.NotContains(t, fmt.Sprint(told), "confidential prerequisite")
	assert.True(t, strings.HasSuffix(told.Ticket.Key, shortOf(quiet)))
}
