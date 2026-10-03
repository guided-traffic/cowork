//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// linkPath is the address of a link from tk, the source, to other.
func (e ticketEnv) linkPath(tk apigen.Ticket, typ apigen.LinkType, other apigen.Ticket) string {
	return e.projectTickets(tk.Project) + "/" + strconv.Itoa(tk.Number) + "/links/" + string(typ) + "/" + other.Project + "-" + strconv.Itoa(other.Number)
}

func (e ticketEnv) link(t *testing.T, c caller, tk apigen.Ticket, typ apigen.LinkType, other apigen.Ticket) *http.Response {
	t.Helper()
	return e.s.do(t, c, http.MethodPut, e.linkPath(tk, typ, other), nil)
}

// links reads a ticket's links as c: "name KEY" per link.
func (e ticketEnv) links(t *testing.T, c caller, tk apigen.Ticket) []string {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, e.projectTickets(tk.Project)+"/"+strconv.Itoa(tk.Number)+"/links", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	var list apigen.LinkList
	require.NoError(t, json.NewDecoder(res.Body).Decode(&list))
	out := make([]string, 0, len(list.Items))
	for _, l := range list.Items {
		out = append(out, l.Name+" "+l.Ticket.Key)
	}
	return out
}

// docs/adr/0012 D1–D4: typed directed links, read both ways, idempotent by
// address, recorded on both tickets, no self link, no blocks cycle.
func TestLinkingTickets(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	_, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	a := e.file(t, member, "ALPHA", task("a"))
	b := e.file(t, member, "ALPHA", task("b"))
	c := e.file(t, member, "ALPHA", task("c"))
	g := e.file(t, member, "GAMMA", task("g"))
	full := func(tk apigen.Ticket) string { return tk.Key }

	res := e.link(t, member, a, apigen.LinkTypeBlocks, b)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var l apigen.Link
	require.NoError(t, json.NewDecoder(res.Body).Decode(&l))
	assert.Equal(t, "blocks", l.Name)
	assert.Equal(t, full(b), l.Ticket.Key)
	assert.Equal(t, e.MemberA, l.CreatedBy.Id)
	assert.Equal(t, http.StatusOK, e.link(t, member, a, apigen.LinkTypeBlocks, b).StatusCode, "an existing link is success")
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM ticket_links WHERE source_id = $1", a.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	acts, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_type = 'link' AND action = 'linked' AND ticket_id IN ($1, $2)", a.Id, b.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, acts, "one act on each ticket, and none for the repeat")

	assert.Equal(t, []string{"blocks " + full(b)}, e.links(t, member, a))
	assert.Equal(t, []string{"blocked by " + full(a)}, e.links(t, member, b))

	require.Equal(t, http.StatusCreated, e.link(t, member, b, apigen.LinkTypeBlocks, c).StatusCode)
	for _, cycle := range [][2]apigen.Ticket{{b, a}, {c, a}} {
		res := e.link(t, member, cycle[0], apigen.LinkTypeBlocks, cycle[1])
		assertProblem(t, res, http.StatusConflict, "link_cycle")
	}
	assertProblem(t, e.link(t, member, a, apigen.LinkTypeBlocks, a), http.StatusBadRequest, "validation_failed")

	for _, typ := range []apigen.LinkType{apigen.LinkTypeDuplicates, apigen.LinkTypeFoundIn} {
		require.Equal(t, http.StatusCreated, e.link(t, member, a, typ, b).StatusCode, typ)
		require.Equal(t, http.StatusCreated, e.link(t, member, b, typ, a).StatusCode, "%s may form a cycle", typ)
	}
	require.Equal(t, http.StatusCreated, e.link(t, member, c, apigen.LinkTypeRelatesTo, a).StatusCode)
	assert.Equal(t, http.StatusOK, e.link(t, member, a, apigen.LinkTypeRelatesTo, c).StatusCode, "relates-to is stored once")
	assert.Contains(t, e.links(t, member, a), "relates to "+full(c))
	assert.Contains(t, e.links(t, member, c), "relates to "+full(a))

	require.Equal(t, http.StatusCreated, e.link(t, member, g, apigen.LinkTypeFoundIn, a).StatusCode, "links cross projects")
	assert.Contains(t, e.links(t, member, a), "found here "+full(g))
	assertProblem(t, e.s.do(t, member, http.MethodPut, e.projectTickets("ALPHA")+"/"+strconv.Itoa(a.Number)+"/links/blocks/BETA-1", nil),
		http.StatusNotFound, "not_found")
	beta, _, err := f.Ticket(e.ctx, e.B, e.ProjectB, e.MemberB, "in tenant B")
	require.NoError(t, err)
	err = f.Exec(e.ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'found-in', $2, $3, $4)`, e.A, a.Id, beta, e.MemberA)
	assert.ErrorContains(t, err, "violates foreign key constraint", "the schema refuses a link into another tenant (docs/adr/0012 D2)")

	del := e.s.do(t, member, http.MethodDelete, e.linkPath(a, apigen.LinkTypeBlocks, b), nil)
	assert.Equal(t, http.StatusNoContent, del.StatusCode)
	again := e.s.do(t, member, http.MethodDelete, e.linkPath(a, apigen.LinkTypeBlocks, b), nil)
	assert.Equal(t, http.StatusNoContent, again.StatusCode, "removing is idempotent")
	acts, err = f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_type = 'link' AND action = 'unlinked' AND ticket_id IN ($1, $2)", a.Id, b.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, acts)

	assert.Equal(t, http.StatusForbidden, e.link(t, caller{Token: e.tk.ViewerA}, a, apigen.LinkTypeBlocks, b).StatusCode)
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, e.projectTickets("ALPHA")+"/"+strconv.Itoa(a.Number)+"/links", nil),
		http.StatusNotFound, "not_found")
}

// Two concurrent blocks links in opposite directions cannot both pass the
// cycle check: the tenant's blocks lock orders them (docs/adr/0012 D4).
func TestConcurrentBlocksLinks(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	for round := range 5 {
		x := e.file(t, member, "ALPHA", task("x"+strconv.Itoa(round)))
		y := e.file(t, member, "ALPHA", task("y"+strconv.Itoa(round)))
		paths := []string{e.linkPath(x, apigen.LinkTypeBlocks, y), e.linkPath(y, apigen.LinkTypeBlocks, x)}
		codes := make([]int, 2)
		var wg sync.WaitGroup
		for i, path := range paths {
			wg.Go(func() {
				req, err := http.NewRequest(http.MethodPut, e.s.URL+path, nil)
				if err != nil {
					return
				}
				req.Header.Set("Authorization", "Bearer "+e.tk.MemberA)
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					return
				}
				_ = res.Body.Close()
				codes[i] = res.StatusCode
			})
		}
		wg.Wait()
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusCreated, http.StatusConflict}, codes, "round %d", round)
	}
}

// docs/adr/0010 D3: an open decision that blocks a ticket makes it icebox;
// the change of that input ends an override, with a row on the ticket's
// timeline; neither changes the ticket's version (docs/adr/0050 D1).
func TestLinksDeriveUrgency(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	work := e.file(t, member, "ALPHA", task("Build it"))
	decision := e.file(t, member, "ALPHA", task("Which database?", func(b *apigen.TicketCreate) { b.Type = apigen.TicketTypeDecision }))
	other := e.file(t, member, "ALPHA", task("Some task"))

	etag := strconv.Quote(strconv.Itoa(work.Version))
	ov, err := e.s.client(t, member).OverrideUrgencyWithResponse(e.ctx, e.SlugA, "ALPHA", work.Number,
		&apigen.OverrideUrgencyParams{IfMatch: &etag}, apigen.UrgencyOverrideSet{Value: apigen.UrgencyNow, Reason: "demo on Friday"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, ov.StatusCode(), string(ov.Body))
	version := ov.JSON200.Version

	require.Equal(t, http.StatusCreated, e.link(t, member, other, apigen.LinkTypeBlocks, work).StatusCode)
	got := e.get(t, member, "ALPHA", work.Number).JSON200
	assert.Equal(t, apigen.UrgencyNow, got.Urgency, "a task that blocks is no input of v1: the override stands")

	require.Equal(t, http.StatusCreated, e.link(t, member, decision, apigen.LinkTypeBlocks, work).StatusCode)
	got = e.get(t, member, "ALPHA", work.Number).JSON200
	assert.Equal(t, apigen.UrgencyIcebox, got.Urgency)
	assert.Equal(t, "v1:icebox-decision", got.UrgencyRule)
	assert.True(t, got.UrgencyOverride.IsNull(), "the input changed: the override ended")
	assert.Equal(t, version, got.Version, "a derived change leaves the version")
	var reason string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT reason FROM audit_events WHERE ticket_id = $1 AND action = 'overridden'
		ORDER BY id DESC LIMIT 1`, work.Id).Scan(&reason))
	assert.Equal(t, "an input of the urgency derivation changed", reason)

	assert.Equal(t, []string{"Build it"}, e.titles(t, member, e.tenantTickets(), "blocked=true"))
	assert.NotContains(t, e.titles(t, member, e.tenantTickets(), "blocked=false"), "Build it")

	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	del := e.s.do(t, agent, http.MethodDelete, e.linkPath(decision, apigen.LinkTypeBlocks, work), nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode, "an agent removes an open blocks link: the open gate")
	got = e.get(t, member, "ALPHA", work.Number).JSON200
	assert.Equal(t, apigen.UrgencyLater, got.Urgency)
	assert.Equal(t, "v1:default", got.UrgencyRule)
}

// docs/adr/0065 D4: a link whose other end the caller cannot see is absent,
// and linking to a ticket the caller cannot see is the 404 of a missing one.
func TestLinksToHiddenTickets(t *testing.T) {
	e := newTicketEnv(t)
	member, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}
	open := e.file(t, member, "ALPHA", task("Open"))
	secret := e.file(t, member, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassBoundary, ptr("tenant crossing")
	}))
	require.Equal(t, http.StatusCreated, e.link(t, member, secret, apigen.LinkTypeBlocks, open).StatusCode)
	assert.Equal(t, []string{"blocked by " + secret.Key}, e.links(t, member, open))
	assert.Empty(t, e.links(t, viewer, open), "the hidden end is absent")
	assert.Empty(t, e.titles(t, viewer, e.tenantTickets(), "blocked=true"), "a hidden blocker does not count for the viewer")

	both := caller{Token: e.tk.Both}
	assertProblem(t, e.link(t, both, open, apigen.LinkTypeRelatesTo, secret), http.StatusNotFound, "not_found")
}
