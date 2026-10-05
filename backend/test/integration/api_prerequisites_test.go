//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// tree reads a ticket's prerequisite tree as c — up for the dependents — and
// requires the 200.
func (e ticketEnv) tree(t *testing.T, c caller, tk apigen.Ticket, up bool, cursor *string, limit int) apigen.PrerequisiteTree {
	t.Helper()
	res := e.treeOf(t, c, tk, up, cursor, limit)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200
}

func (e ticketEnv) treeOf(t *testing.T, c caller, tk apigen.Ticket, up bool, cursor *string, limit int) *apigen.ListPrerequisitesResponse {
	t.Helper()
	params := &apigen.ListPrerequisitesParams{Cursor: cursor}
	if up {
		params.Direction = ptr(apigen.ListPrerequisitesParamsDirectionUp)
	}
	if limit > 0 {
		params.Limit = &limit
	}
	res, err := e.s.client(t, c).ListPrerequisitesWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, params)
	require.NoError(t, err)
	return res
}

// shape writes a tree's nodes as "depth title", a repeated node marked "(again)".
func shape(nodes []apigen.PrerequisiteNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		line := fmt.Sprintf("%d %s", n.Depth, n.Title)
		if n.Repeated {
			line += " (again)"
		}
		out = append(out, line)
	}
	return out
}

func (e ticketEnv) blocks(t *testing.T, from, to apigen.Ticket) {
	t.Helper()
	res := e.link(t, caller{Token: e.tk.AdminA}, from, apigen.LinkTypeBlocks, to)
	require.Equal(t, http.StatusCreated, res.StatusCode)
}

// docs/adr/0012 D6: the tickets that block a ticket, what blocks those, and
// so on, depth first in the order they were filed; read upward, its
// dependents. A ticket under two others stands in full once and once more as
// a repeated leaf; the open ones are counted once each, the done and dropped
// ones settled; the page is cut by a cursor, the count is the whole tree's.
func TestPrerequisiteTree(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	root := e.file(t, member, "ALPHA", task("root"))
	a := e.file(t, member, "ALPHA", task("a", func(b *apigen.TicketCreate) { b.Assignee = &e.AdminA }))
	b := e.file(t, member, "ALPHA", task("b"))
	c := e.file(t, member, "ALPHA", task("c"))
	d := e.file(t, member, "ALPHA", task("d"))
	x := e.file(t, member, "ALPHA", task("x"))
	for _, l := range [][2]apigen.Ticket{{a, root}, {b, root}, {c, a}, {d, a}, {d, b}, {x, c}} {
		e.blocks(t, l[0], l[1])
	}
	dropped := e.move(t, member, b, apigen.Transition{From: b.State, To: apigen.TicketStateDropped, Reason: ptr("not needed")})
	require.Equal(t, http.StatusOK, dropped.StatusCode(), string(dropped.Body))
	e.walk(t, member, a, toAnalysed, toDecided, toInProgress)

	down := e.tree(t, member, root, false, nil, 0)
	assert.Equal(t, []string{"1 a", "2 c", "3 x", "2 d", "1 b", "2 d (again)"}, shape(down.Items))
	assert.Equal(t, 4, down.Open, "a, c, d and x, each once; b is dropped")
	assert.True(t, down.NextCursor.IsNull())
	first := down.Items[0]
	assert.Equal(t, a.Key, first.Key)
	assert.Equal(t, apigen.TicketStateInProgress, first.State)
	assigned, err := first.Assignee.Get()
	require.NoError(t, err)
	assert.Equal(t, e.AdminA, assigned.Id)
	assert.False(t, first.Settled)
	assert.True(t, down.Items[4].Settled, "b is dropped")
	assert.Equal(t, apigen.TicketStateDropped, down.Items[4].State)

	up := e.tree(t, member, d, true, nil, 0)
	assert.Equal(t, []string{"1 a", "2 root", "1 b", "2 root (again)"}, shape(up.Items))
	assert.Equal(t, 2, up.Open, "a and root; b is dropped")
	assert.Equal(t, []string{"1 c", "2 a", "3 root"}, shape(e.tree(t, member, x, true, nil, 0).Items))
	assert.Empty(t, e.tree(t, member, x, false, nil, 0).Items, "nothing blocks x")

	var pages [][]string
	var cursor *string
	for {
		page := e.tree(t, member, root, false, cursor, 2)
		pages = append(pages, shape(page.Items))
		assert.Equal(t, 4, page.Open, "every page counts the whole tree")
		next, err := page.NextCursor.Get()
		if err != nil {
			break
		}
		cursor = &next
	}
	assert.Equal(t, [][]string{{"1 a", "2 c"}, {"3 x", "2 d"}, {"1 b", "2 d (again)"}}, pages)

	half := e.tree(t, member, root, false, nil, 2)
	next, err := half.NextCursor.Get()
	require.NoError(t, err)
	res := e.treeOf(t, member, root, true, &next, 2)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode(), "a cursor of the prerequisites does not page the dependents")
	assert.Equal(t, apigen.ProblemCodeInvalidCursor, res.ApplicationproblemJSONDefault.Code)
	res = e.treeOf(t, member, a, false, &next, 2)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode(), "nor another ticket's tree")

	// The context shows each prerequisite once (docs/adr/0044 D2).
	_, doc := e.contextOf(t, member, root, "")
	section := doc[strings.Index(doc, "## Prerequisites"):]
	section = section[:strings.Index(section, "\n## ")]
	assert.Contains(t, section, "4 of 5 open.")
	assert.Equal(t, 1, strings.Count(section, d.Key), "d once, though it blocks a and b")
}

// docs/adr/0065 D5, docs/adr/0034 D4: the walk never passes a ticket the
// caller cannot see — a confidential one, one of a restricted project — so the
// ticket and whatever lies only behind it are absent, uncounted, without a
// placeholder; who sees them sees the whole tree.
func TestPrerequisiteTreeLeavesOutWhatTheCallerCannotSee(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}
	secret, err := f.Project(e.ctx, e.A, "SECRET", "Secret")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", secret))

	root := e.file(t, member, "ALPHA", task("root"))
	seen := e.file(t, member, "ALPHA", task("seen"))
	confidential := e.file(t, admin, "ALPHA", task("confidential", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("a finding")
	}))
	behindConfidential := e.file(t, member, "ALPHA", task("behind the confidential one"))
	restricted := e.file(t, admin, "SECRET", task("restricted"))
	behindRestricted := e.file(t, member, "ALPHA", task("behind the restricted one"))
	alsoSeen := e.file(t, member, "ALPHA", task("behind both and behind seen"))
	for _, l := range [][2]apigen.Ticket{
		{seen, root}, {confidential, root}, {restricted, root},
		{behindConfidential, confidential}, {behindRestricted, restricted},
		{alsoSeen, confidential}, {alsoSeen, seen},
	} {
		e.blocks(t, l[0], l[1])
	}

	memberTree := e.tree(t, member, root, false, nil, 0)
	assert.Equal(t, []string{"1 seen", "2 behind both and behind seen"}, shape(memberTree.Items),
		"what lies only behind a hidden ticket is absent; what is reached through a visible one is not")
	assert.Equal(t, 2, memberTree.Open)
	for _, n := range memberTree.Items {
		for _, hidden := range []apigen.Ticket{confidential, restricted, behindConfidential, behindRestricted} {
			assert.NotEqual(t, hidden.Key, n.Key)
		}
	}

	adminTree := e.tree(t, admin, root, false, nil, 0)
	assert.Equal(t, []string{
		"1 seen", "2 behind both and behind seen",
		"1 confidential", "2 behind the confidential one", "2 behind both and behind seen (again)",
		"1 restricted", "2 behind the restricted one",
	}, shape(adminTree.Items), "an administrator sees the whole tree")
	assert.Equal(t, 6, adminTree.Open)

	assert.Empty(t, e.tree(t, member, behindConfidential, true, nil, 0).Items,
		"read upward, the walk stops at the confidential ticket and never reaches root")
	assert.Equal(t, []string{"1 confidential", "2 root"}, shape(e.tree(t, admin, behindConfidential, true, nil, 0).Items))
	assert.Empty(t, e.tree(t, member, behindRestricted, true, nil, 0).Items)

	assertProblem(t, e.s.do(t, member, http.MethodGet,
		fmt.Sprintf("%s/%d/prerequisites", e.projectTickets("ALPHA"), confidential.Number), nil),
		http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, member, http.MethodGet,
		fmt.Sprintf("%s/%d/prerequisites?direction=up", e.projectTickets("SECRET"), restricted.Number), nil),
		http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet,
		fmt.Sprintf("%s/%d/prerequisites", e.projectTickets("ALPHA"), root.Number), nil),
		http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, member, http.MethodGet,
		fmt.Sprintf("%s/%d/prerequisites?direction=sideways", e.projectTickets("ALPHA"), root.Number), nil),
		http.StatusBadRequest, "validation_failed")
}

// The walk keeps each link once per depth and never each path: a layered
// graph of forty tickets and a hundred and eighty links, which has 5^8 paths
// to the ticket, answers the tree and the context at once. The context's walk
// before the route enumerated every path and took eleven seconds here.
func TestPrerequisiteTreeCostsItsLinksNotItsPaths(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	root := e.file(t, member, "ALPHA", task("root"))
	previous := []uuid.UUID{root.Id}
	for layer := range 8 {
		current := make([]uuid.UUID, 0, 5)
		for i := range 5 {
			id, _, err := f.Ticket(e.ctx, e.A, e.ProjectA, e.MemberA, fmt.Sprintf("layer %d, %d", layer, i))
			require.NoError(t, err)
			current = append(current, id)
			for _, target := range previous {
				require.NoError(t, f.Exec(e.ctx,
					"INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by) VALUES ($1, 'blocks', $2, $3, $4)",
					e.A, id, target, e.MemberA))
			}
		}
		previous = current
	}

	start := time.Now()
	tree := e.tree(t, member, root, false, nil, 200)
	assert.Equal(t, 40, tree.Open, "every ticket of the eight levels, each once")
	res, doc := e.contextOf(t, member, root, "")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, doc, "40 of 40 open.")
	assert.Less(t, time.Since(start), 3*time.Second)
}
