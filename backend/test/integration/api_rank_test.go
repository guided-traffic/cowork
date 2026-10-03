//go:build integration

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// rankMove sends a move in the rank as c.
func (e ticketEnv) rankMove(t *testing.T, c caller, tk apigen.Ticket, body apigen.TicketRankSet) *apigen.MoveTicketRankResponse {
	t.Helper()
	res, err := e.s.client(t, c).MoveTicketRankWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, body)
	require.NoError(t, err)
	return res
}

// place moves tk directly after, or before, other and requires the 200.
func (e ticketEnv) place(t *testing.T, c caller, tk apigen.Ticket, after bool, other apigen.Ticket) apigen.Ticket {
	t.Helper()
	res := e.rankMove(t, c, tk, side(after, other.Number))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200
}

func side(after bool, number int) apigen.TicketRankSet {
	if after {
		return apigen.TicketRankSet{After: &number}
	}
	return apigen.TicketRankSet{Before: &number}
}

// rankKeys reads the keys of a project's tickets by number, past the
// predicate; an unranked ticket has none.
func rankKeys(t *testing.T, f *fixture.DB, project uuid.UUID) map[int]string {
	t.Helper()
	rows, err := f.Query(context.Background(), "SELECT number, rank FROM tickets WHERE project_id = $1 AND rank IS NOT NULL", project)
	require.NoError(t, err)
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var n int
		var k string
		require.NoError(t, rows.Scan(&n, &k))
		out[n] = k
	}
	require.NoError(t, rows.Err())
	return out
}

// rankOf reads one ticket's key past the predicate; nil when it has none. No
// answer of the API shows a key (docs/adr/0014 D2).
func rankOf(t *testing.T, f *fixture.DB, id uuid.UUID) *string {
	t.Helper()
	var k *string
	require.NoError(t, f.QueryRow(context.Background(), "SELECT rank FROM tickets WHERE id = $1", id).Scan(&k))
	return k
}

// keyOf reads one ticket's key past the predicate and requires one.
func keyOf(t *testing.T, f *fixture.DB, id uuid.UUID) string {
	t.Helper()
	k := rankOf(t, f, id)
	require.NotNil(t, k, "the ticket is ranked")
	return *k
}

// listed reads a project's list as c, every page of it with the cursor.
func (e ticketEnv) listed(t *testing.T, c caller, project, query string, limit int) []apigen.Ticket {
	t.Helper()
	var out []apigen.Ticket
	cursor := ""
	for range 100 {
		q := query + "&limit=" + strconv.Itoa(limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		res := e.s.do(t, c, http.MethodGet, e.projectTickets(project)+"?"+q, nil)
		require.Equal(t, http.StatusOK, res.StatusCode, q)
		var page apigen.TicketList
		require.NoError(t, json.NewDecoder(res.Body).Decode(&page))
		out = append(out, page.Items...)
		if page.NextCursor.IsNull() {
			return out
		}
		cursor = page.NextCursor.MustGet()
	}
	t.Fatal("the cursor never ended")
	return nil
}

func titlesOf(list []apigen.Ticket) []string {
	out := make([]string, 0, len(list))
	for _, tk := range list {
		out = append(out, tk.Title)
	}
	return out
}

func rankedActs(t *testing.T, f *fixture.DB, id uuid.UUID) int64 {
	t.Helper()
	n, err := f.QueryCount(context.Background(), "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'ranked'", id)
	require.NoError(t, err)
	return n
}

// docs/adr/0014 D2: a filed ticket joins its project's rank at the bottom, and
// the project's list is the rank's order.
func TestRankNewTicketsLandAtTheBottom(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	a := e.file(t, member, "ALPHA", task("a"))
	b := e.file(t, member, "ALPHA", task("b"))
	c := e.file(t, member, "ALPHA", task("c"))
	for _, tk := range []apigen.Ticket{a, b, c} {
		assert.True(t, domain.ValidRank(keyOf(t, f, tk.Id)), "a filed ticket is ranked")
	}
	assert.Less(t, keyOf(t, f, a.Id), keyOf(t, f, b.Id))
	assert.Less(t, keyOf(t, f, b.Id), keyOf(t, f, c.Id))

	e.place(t, member, c, false, a)
	d := e.file(t, member, "ALPHA", task("d"))
	assert.Equal(t, []string{"c", "a", "b", "d"}, e.titles(t, member, e.projectTickets("ALPHA"), ""),
		"a move reorders the list, and a filing lands at the bottom after it")
	assert.Equal(t, 1, d.Version, "filing is no move")
}

// docs/adr/0014 D2, docs/adr/0050 D1, D4: after and before reorder the list
// with one recorded act that raises the version and is published; a ticket
// that already sits there answers unchanged, with no act and no event.
func TestRankMoves(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	a := e.file(t, member, "ALPHA", task("a"))
	b := e.file(t, member, "ALPHA", task("b"))
	c := e.file(t, member, "ALPHA", task("c"))
	d := e.file(t, member, "ALPHA", task("d"))
	s := e.openStream(t, e.s, member, e.SlugA, "")

	res := e.rankMove(t, member, d, side(true, a.Number))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	moved := *res.JSON200
	assert.Equal(t, d.Version+1, moved.Version, "a move raises the version")
	assert.Equal(t, strconv.Quote(strconv.Itoa(moved.Version)), res.HTTPResponse.Header.Get("ETag"))
	assert.Less(t, keyOf(t, f, a.Id), keyOf(t, f, d.Id))
	assert.Less(t, keyOf(t, f, d.Id), keyOf(t, f, b.Id))
	assert.Equal(t, []string{"a", "d", "b", "c"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))

	m, ok := s.next(t, time.Second)
	require.True(t, ok, "the move is published")
	assert.Equal(t, "ticket.changed", m.Event)
	assert.JSONEq(t, fmt.Sprintf(`{"key":%q,"version":%d,"kind":"ranked"}`, d.Key, moved.Version), m.Data)

	var noBefore bool
	var after map[string]any
	var refs []string
	var actor uuid.UUID
	require.NoError(t, f.QueryRow(e.ctx, `SELECT before IS NULL, after, refs::text[], actor_user_id FROM audit_events
		WHERE ticket_id = $1 AND action = 'ranked'`, d.Id).Scan(&noBefore, &after, &refs, &actor))
	assert.True(t, noBefore, "the act holds no key")
	assert.Equal(t, map[string]any{"after": a.Key}, after, "only the neighbour")
	assert.Equal(t, []string{a.Id.String()}, refs, "the neighbour is a reference: who cannot see it reads the act without its payload")
	assert.Equal(t, e.MemberA, actor)

	moved2 := e.place(t, member, b, false, a)
	assert.Equal(t, []string{"b", "a", "d", "c"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))
	_, ok = s.next(t, time.Second)
	require.True(t, ok)

	for _, noop := range []struct {
		tk    apigen.Ticket
		after bool
		other apigen.Ticket
	}{{moved, true, a}, {moved2, false, a}} {
		key := keyOf(t, f, noop.tk.Id)
		res := e.rankMove(t, member, noop.tk, side(noop.after, noop.other.Number))
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		assert.Equal(t, noop.tk.Version, res.JSON200.Version, "a ticket that already sits there is unchanged")
		assert.Equal(t, key, keyOf(t, f, noop.tk.Id))
		assert.Equal(t, int64(1), rankedActs(t, f, noop.tk.Id), "and nothing is recorded")
	}
	_, ok = s.next(t, 300*time.Millisecond)
	assert.False(t, ok, "nor published")

	e.place(t, member, b, true, c)
	assert.Equal(t, []string{"a", "d", "c", "b"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "after the last ticket")
	e.place(t, member, c, false, a)
	assert.Equal(t, []string{"c", "a", "d", "b"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "before the first ticket")
}

// docs/adr/0014 D1, docs/adr/0043 D4, docs/adr/0065 D5: the refusals of a move.
func TestRankRefusals(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}
	a := e.file(t, member, "ALPHA", task("a"))
	b := e.file(t, member, "ALPHA", task("b"))
	path := fmt.Sprintf("%s/%d/rank", e.projectTickets("ALPHA"), a.Number)

	for _, body := range []map[string]any{{}, {"after": b.Number, "before": b.Number}} {
		assertProblem(t, e.s.do(t, member, http.MethodPut, path, body), http.StatusBadRequest, "validation_failed")
	}
	res := e.rankMove(t, member, a, side(true, a.Number))
	require.Equal(t, http.StatusBadRequest, res.StatusCode(), "the ticket itself")
	assert.Equal(t, "/after", (*res.ApplicationproblemJSONDefault.Errors)[0].Pointer)

	secret := e.file(t, admin, "ALPHA", task("secret", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	missing := e.s.do(t, member, http.MethodPut, path, map[string]any{"before": 999})
	hidden := e.s.do(t, member, http.MethodPut, path, map[string]any{"before": secret.Number})
	gone, unseen := assertProblem(t, missing, http.StatusBadRequest, "validation_failed"), assertProblem(t, hidden, http.StatusBadRequest, "validation_failed")
	assert.Equal(t, gone["errors"], unseen["errors"], "a neighbour the caller cannot see is one that does not exist")
	assert.Equal(t, gone["detail"], unseen["detail"])
	assertProblem(t, e.s.do(t, member, http.MethodPut, fmt.Sprintf("%s/%d/rank", e.projectTickets("ALPHA"), secret.Number),
		map[string]any{"after": a.Number}), http.StatusNotFound, "not_found")

	done := e.walk(t, member, e.file(t, member, "ALPHA", task("done")), toAnalysed, toDecided, toInProgress, toDone)
	require.Nil(t, rankOf(t, f, done.Id), "a done ticket has no rank")
	res = e.rankMove(t, member, done, side(true, a.Number))
	require.Equal(t, http.StatusConflict, res.StatusCode())
	assert.Equal(t, "state_conflict", string(res.ApplicationproblemJSONDefault.Code))
	res = e.rankMove(t, member, a, side(false, done.Number))
	require.Equal(t, http.StatusConflict, res.StatusCode(), "nor is it a neighbour")
	assert.Equal(t, "/before", (*res.ApplicationproblemJSONDefault.Errors)[0].Pointer)
	assert.Equal(t, "done", (*res.ApplicationproblemJSONDefault.Errors)[0].Current.MustGet())

	refused := e.rankMove(t, caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}, a, side(true, b.Number))
	require.Equal(t, http.StatusForbidden, refused.StatusCode())
	assert.Equal(t, "agent_forbidden", string(refused.ApplicationproblemJSONDefault.Code))
	assert.Equal(t, "missing capability: rank", *refused.ApplicationproblemJSONDefault.Detail)
	readToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Scope: domain.ScopeRead})
	require.NoError(t, err)
	readOnly := e.rankMove(t, caller{Token: readToken}, a, side(true, b.Number))
	require.Equal(t, http.StatusForbidden, readOnly.StatusCode())
	assert.Equal(t, "insufficient_scope", string(readOnly.ApplicationproblemJSONDefault.Code))
	viewer := e.rankMove(t, caller{Token: e.tk.ViewerA}, a, side(true, b.Number))
	require.Equal(t, http.StatusForbidden, viewer.StatusCode())
	assert.Equal(t, "forbidden", string(viewer.ApplicationproblemJSONDefault.Code))
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodPut, path, map[string]any{"after": b.Number}),
		http.StatusNotFound, "not_found")
	assert.Equal(t, int64(0), rankedActs(t, f, a.Id), "no refusal records a move")

	agent := e.place(t, caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}, a, true, b)
	assert.Equal(t, []string{"b", "a", "secret"}, e.titles(t, admin, e.projectTickets("ALPHA"), ""), "an agent with rank moves")
	var mark string
	require.NoError(t, f.QueryRow(e.ctx, "SELECT agent FROM audit_events WHERE ticket_id = $1 AND action = 'ranked'", agent.Id).Scan(&mark))
	assert.Equal(t, "claude-code/opus/s1", mark)
}

// docs/adr/0014 D1: done and dropped take the rank away; a reopen ranks the
// ticket at the bottom of its project.
func TestRankLeavesWithDoneAndReturnsWithAReopen(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	a := e.file(t, member, "ALPHA", task("a"))
	b := e.file(t, member, "ALPHA", task("b"))
	c := e.file(t, member, "ALPHA", task("c"))
	d := e.file(t, member, "ALPHA", task("d"))

	b = e.walk(t, member, b, toAnalysed, toDecided, toInProgress)
	key := keyOf(t, f, b.Id)
	blocked := e.move(t, member, b, apigen.Transition{From: b.State, To: apigen.TicketStateBlocked, Reason: ptr("waits on a person"),
		Block: &apigen.BlockSet{Kind: apigen.BlockKindHuman}})
	require.Equal(t, http.StatusOK, blocked.StatusCode(), string(blocked.Body))
	assert.Equal(t, key, keyOf(t, f, b.Id), "a blocked ticket keeps its rank")
	b = *e.move(t, member, *blocked.JSON200, apigen.Transition{From: apigen.TicketStateBlocked, To: apigen.TicketStateInProgress}).JSON200
	b = e.walk(t, member, b, toDone)
	assert.Nil(t, rankOf(t, f, b.Id), "done takes the rank away")
	dropped := e.move(t, member, c, apigen.Transition{From: c.State, To: apigen.TicketStateDropped, Reason: ptr("not needed")})
	require.Equal(t, http.StatusOK, dropped.StatusCode(), string(dropped.Body))
	assert.Nil(t, rankOf(t, f, c.Id), "so does dropped")

	assert.Equal(t, []string{"a", "d"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))
	assert.Equal(t, []string{"a", "d", "b", "c"}, e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"),
		"the unranked follow the ranked, by number")

	e.place(t, member, d, false, a)
	reopened := e.move(t, member, b, apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateInProgress, Reason: ptr("the fix regressed")})
	require.Equal(t, http.StatusOK, reopened.StatusCode(), string(reopened.Body))
	require.NotNil(t, rankOf(t, f, b.Id), "the withdrawal of a done by hand ranks the ticket")
	assert.Equal(t, []string{"d", "a", "b"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "at the bottom")
	reopened = e.move(t, member, c, apigen.Transition{From: apigen.TicketStateDropped, To: apigen.TicketStateFiled, Reason: ptr("needed")})
	require.Equal(t, http.StatusOK, reopened.StatusCode(), string(reopened.Body))
	assert.Equal(t, []string{"d", "a", "b", "c"}, e.titles(t, member, e.projectTickets("ALPHA"), ""), "so does a reopen")
}

// Concurrent moves in one project, with filings and reopens among them, are
// ordered by its rank lock: every open ticket ends with a key of its own, and
// the list is the keys' order.
func TestConcurrentRankMoves(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tks := make([]apigen.Ticket, 0, 8)
	for i := range 8 {
		tks = append(tks, e.file(t, member, "ALPHA", task(fmt.Sprintf("t%d", i))))
	}
	done := make([]apigen.Ticket, 0, 2)
	for i := range 2 {
		done = append(done, e.walk(t, member, e.file(t, member, "ALPHA", task(fmt.Sprintf("r%d", i))), toAnalysed, toDecided, toInProgress, toDone))
	}
	cl := e.s.client(t, member)
	var sends []func() int
	for i := range 20 {
		r := rand.New(rand.NewPCG(uint64(i), 14))
		moved := r.IntN(len(tks))
		other := (moved + 1 + r.IntN(len(tks)-1)) % len(tks)
		body := side(r.IntN(2) == 0, tks[other].Number)
		sends = append(sends, func() int {
			res, err := cl.MoveTicketRankWithResponse(e.ctx, e.SlugA, "ALPHA", tks[moved].Number, body)
			if err != nil {
				return 0
			}
			return res.StatusCode()
		})
	}
	for i := range 4 {
		sends = append(sends, func() int {
			res, err := cl.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task(fmt.Sprintf("n%d", i)))
			if err != nil || res.StatusCode() != http.StatusCreated {
				return 0
			}
			return http.StatusOK
		})
	}
	for _, d := range done {
		sends = append(sends, func() int {
			res, err := cl.TransitionTicketWithResponse(e.ctx, e.SlugA, "ALPHA", d.Number, &apigen.TransitionTicketParams{},
				apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateInProgress, Reason: ptr("again")})
			if err != nil {
				return 0
			}
			return res.StatusCode()
		})
	}
	for _, code := range simultaneously(sends...) {
		assert.Equal(t, http.StatusOK, code)
	}

	keys := rankKeys(t, f, e.ProjectA)
	require.Len(t, keys, len(tks)+len(done)+4, "every open ticket is ranked")
	seen := map[string]bool{}
	for _, k := range keys {
		assert.True(t, domain.ValidRank(k), k)
		assert.False(t, seen[k], "the key %s is held once", k)
		seen[k] = true
	}
	numbers := make([]int, 0, len(keys))
	for n := range keys {
		numbers = append(numbers, n)
	}
	slices.SortFunc(numbers, func(x, y int) int { return strings.Compare(keys[x], keys[y]) })
	pages := e.listed(t, member, "ALPHA", "", 3)
	listed := make([]int, 0, len(pages))
	for _, tk := range pages {
		listed = append(listed, tk.Number)
	}
	assert.Equal(t, numbers, listed, "the list is the keys' order")
}

// docs/adr/0014 D2, docs/adr/0065 D4: a key is computed over every ticket of
// the project, so a confidential ticket the mover cannot see keeps its key and
// its place, and no key is handed out twice. Whether a move writes is decided
// over the tickets the mover sees: a ticket that sits next to its neighbour
// with a hidden one between them answers unchanged, as it would without it.
func TestRankAroundAHiddenTicket(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}
	a := e.file(t, member, "ALPHA", task("a"))
	hidden := e.file(t, admin, "ALPHA", task("hidden", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	b := e.file(t, member, "ALPHA", task("b"))
	c := e.file(t, member, "ALPHA", task("c"))
	key := keyOf(t, f, hidden.Id)
	assert.Equal(t, []string{"a", "b", "c"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))

	c = e.place(t, member, c, true, a)
	assert.Less(t, keyOf(t, f, a.Id), keyOf(t, f, c.Id))
	assert.Less(t, keyOf(t, f, c.Id), key, "c lands between a and the ticket the mover cannot see")
	assert.Equal(t, []string{"a", "c", "hidden", "b"}, e.titles(t, admin, e.projectTickets("ALPHA"), ""))

	for _, noop := range []struct {
		name  string
		tk    apigen.Ticket
		after bool
		other apigen.Ticket
	}{{"b after c", b, true, c}, {"c before b", c, false, b}} {
		before := keyOf(t, f, noop.tk.Id)
		res := e.rankMove(t, member, noop.tk, side(noop.after, noop.other.Number))
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		assert.Equal(t, noop.tk.Version, res.JSON200.Version, "%s: already there for the mover, the hidden ticket between them", noop.name)
		assert.Equal(t, before, keyOf(t, f, noop.tk.Id), noop.name)
		assert.Equal(t, int64(1), rankedActs(t, f, c.Id)+rankedActs(t, f, b.Id), "%s: nothing is recorded", noop.name)
	}

	assert.Equal(t, key, *rankOf(t, f, hidden.Id), "the hidden ticket keeps its key")
	keys := rankKeys(t, f, e.ProjectA)
	seen := map[string]bool{}
	for _, k := range keys {
		assert.False(t, seen[k], "the key %s is held once", k)
		seen[k] = true
	}
	assert.Equal(t, []string{"a", "c", "b"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))
	assert.Equal(t, []string{"a", "c", "hidden", "b"}, e.titles(t, admin, e.projectTickets("ALPHA"), ""))
}

// docs/adr/0014 D2: a key is computed over tickets the caller may not see, so
// no answer shows one — not a ticket, not a list, not the act of a move, not
// a cursor, which carries its position sealed.
func TestRankKeyIsNeverShown(t *testing.T) {
	e := newTicketEnv(t)
	member, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}
	a := e.file(t, member, "ALPHA", task("a"))
	e.file(t, admin, "ALPHA", task("hidden", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	b := e.file(t, member, "ALPHA", task("b"))
	ticket := func(number int) string { return fmt.Sprintf("%s/%d", e.projectTickets("ALPHA"), number) }
	decode := func(name string, res *http.Response, into any) {
		t.Helper()
		require.Less(t, res.StatusCode, http.StatusMultipleChoices, name)
		require.NoError(t, json.NewDecoder(res.Body).Decode(into), name)
	}
	for name, res := range map[string]*http.Response{
		"a filing": e.s.do(t, member, http.MethodPost, e.projectTickets("ALPHA"), task("c")),
		"a ticket": e.s.do(t, member, http.MethodGet, ticket(a.Number), nil),
		"a move":   e.s.do(t, member, http.MethodPut, ticket(a.Number)+"/rank", map[string]any{"after": b.Number}),
		"a reopen": e.s.do(t, member, http.MethodPost, ticket(e.walk(t, member, e.file(t, member, "ALPHA", task("d")), toAnalysed,
			toDecided, toInProgress, toDone).Number)+"/transitions", map[string]any{"from": "done", "to": "in-progress", "reason": "again"}),
	} {
		var body map[string]any
		decode(name, res, &body)
		assert.NotContains(t, body, "rank", name)
	}

	position := regexp.MustCompile(`^[0-9A-Za-z]*\.[0-9]+$`)
	cursor := ""
	for range 10 {
		q := "?limit=1&include_terminal=true"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		var page struct {
			Items      []map[string]any `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		}
		decode("a page", e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA")+q, nil), &page)
		for _, it := range page.Items {
			assert.NotContains(t, it, "rank", "a listed ticket")
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
		payload, _, _ := strings.Cut(cursor, ".")
		readable, err := base64.RawURLEncoding.DecodeString(payload)
		require.NoError(t, err)
		var fields struct {
			After string `json:"a"`
		}
		require.NoError(t, json.Unmarshal(readable, &fields))
		assert.NotRegexp(t, position, fields.After, "the cursor's position is sealed, not <key>.<number>")
	}

	var acts struct {
		Items []struct {
			Action string         `json:"action"`
			Before map[string]any `json:"before"`
			After  map[string]any `json:"after"`
		} `json:"items"`
	}
	decode("the activity", e.s.do(t, member, http.MethodGet, ticket(a.Number)+"/activity", nil), &acts)
	var ranked int
	for _, act := range acts.Items {
		if act.Action != "ranked" {
			continue
		}
		ranked++
		assert.Nil(t, act.Before, "the act holds no key")
		assert.Equal(t, map[string]any{"after": b.Key}, act.After, "only the neighbour")
	}
	assert.Equal(t, 1, ranked)
}

// docs/adr/0028 D3: a ticket the previous release filed has no key; the
// project's list shows it after the ranked ones by number, and the next write
// that hands out a key ranks it there first. A move that turns out to change
// nothing writes none of those keys either.
func TestRankRanksTicketsAnOlderReleaseFiled(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	old := func(title string) apigen.Ticket {
		_, n, err := f.Ticket(e.ctx, e.A, gamma, e.MemberA, title)
		require.NoError(t, err)
		res := e.get(t, member, "GAMMA", n)
		require.Equal(t, http.StatusOK, res.StatusCode())
		require.Nil(t, rankOf(t, f, res.JSON200.Id))
		return *res.JSON200
	}
	old("o1")
	o2 := old("o2")
	old("o3")
	assert.Equal(t, []string{"o1", "o2", "o3"}, e.titles(t, member, e.projectTickets("GAMMA"), ""), "unranked, by number")

	e.file(t, member, "GAMMA", task("n4"))
	keys := rankKeys(t, f, gamma)
	require.Len(t, keys, 4, "the filing ranked the older tickets first")
	for n := 1; n < 4; n++ {
		assert.Less(t, keys[n], keys[n+1], "in number order, the new one at the bottom")
	}

	o5 := old("o5")
	assert.Equal(t, []string{"o1", "o2", "o3", "n4", "o5"}, e.titles(t, member, e.projectTickets("GAMMA"), ""))
	assert.Equal(t, []string{"o1", "o2", "o3", "n4", "o5"}, titlesOf(e.listed(t, member, "GAMMA", "", 2)),
		"a cursor crosses from the ranked to the unranked")
	e.place(t, member, o2, true, o5)
	assert.Equal(t, []string{"o1", "o3", "n4", "o5", "o2"}, e.titles(t, member, e.projectTickets("GAMMA"), ""))

	o6, o7 := old("o6"), old("o7")
	res := e.rankMove(t, member, o7, side(true, o6.Number))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, o7.Version, res.JSON200.Version, "o7 already sits directly after o6")
	assert.Nil(t, rankOf(t, f, o6.Id), "the keys the move would have given are rolled back with it")
	assert.Nil(t, rankOf(t, f, o7.Id))
	assert.Equal(t, int64(0), rankedActs(t, f, o7.Id))
}

// docs/adr/0048 D1, D2, D5: the project's list pages through the ranked and
// the unranked tickets with the cursor and with numbered pages alike.
func TestRankListPaging(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	p1 := e.file(t, member, "ALPHA", task("p1"))
	p2 := e.file(t, member, "ALPHA", task("p2"))
	e.file(t, member, "ALPHA", task("p3"))
	p4 := e.file(t, member, "ALPHA", task("p4"))
	e.place(t, member, p4, false, p1)
	e.walk(t, member, p2, toAnalysed, toDecided, toInProgress, toDone)
	for _, title := range []string{"f5", "f6"} {
		_, _, err := f.Ticket(e.ctx, e.A, e.ProjectA, e.MemberA, title)
		require.NoError(t, err)
	}

	all := []string{"p4", "p1", "p3", "p2", "f5", "f6"}
	for _, limit := range []int{1, 2, 4} {
		assert.Equal(t, all, titlesOf(e.listed(t, member, "ALPHA", "include_terminal=true", limit)), "limit %d", limit)
	}
	assert.Equal(t, []string{"p4", "p1", "p3", "f5", "f6"}, titlesOf(e.listed(t, member, "ALPHA", "", 2)))
	assert.Equal(t, all, e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true&page=1&per_page=25"))
}

// docs/adr/0014 D1, docs/adr/0028 D3: the previous release's done and dropped
// leave the key in the column. The list reads a key on a done or dropped
// ticket as none — it follows the open tickets by number, on every page — and
// a move decides over the open tickets alone. A ticket that goes done or
// dropped between the read of the unranked tickets and its first key, which
// done and dropped do not wait for, stays without one.
func TestRankIgnoresAKeyLeftOnADoneTicket(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	p1 := e.file(t, member, "ALPHA", task("p1"))
	p2 := e.file(t, member, "ALPHA", task("p2"))
	p3 := e.file(t, member, "ALPHA", task("p3"))
	p4 := e.file(t, member, "ALPHA", task("p4"))
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET state = 'done', done_at = now() WHERE id = $1`, p2.Id))
	require.NotNil(t, rankOf(t, f, p2.Id), "closed the way the previous release closes")

	all := []string{"p1", "p3", "p4", "p2"}
	assert.Equal(t, all, e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"),
		"the done ticket follows the open ones by number")
	for _, limit := range []int{1, 2} {
		assert.Equal(t, all, titlesOf(e.listed(t, member, "ALPHA", "include_terminal=true", limit)), "limit %d", limit)
	}
	res := e.rankMove(t, member, p3, side(true, p1.Number))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, p3.Version, res.JSON200.Version, "p3 sits directly after p1 among the open tickets")
	assert.Equal(t, int64(0), rankedActs(t, f, p3.Id))

	// Byte order, the "C" collation, through the rule as well: capitals
	// before small letters.
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET rank = 'a' WHERE id = $1`, p1.Id))
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET rank = 'B' WHERE id = $1`, p4.Id))
	all = []string{"p4", "p3", "p1", "p2"}
	assert.Equal(t, all, e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"))
	assert.Equal(t, all, titlesOf(e.listed(t, member, "ALPHA", "include_terminal=true", 1)))

	_, n, err := f.Ticket(e.ctx, e.A, e.ProjectA, e.MemberA, "old")
	require.NoError(t, err)
	old := e.get(t, member, "ALPHA", n).JSON200
	require.NoError(t, f.Exec(e.ctx, `UPDATE tickets SET state = 'dropped' WHERE id = $1`, old.Id))
	ctx := as(e.MemberA)
	var left *string
	_, err = openRuntime(t).Mutate(ctx, e.A, func(w *store.Writer) error {
		if err := w.RankUnrankedTicket(ctx, writeq.RankUnrankedTicketParams{TenantID: e.A, ID: old.Id, Rank: "zz"}); err != nil {
			return err
		}
		st, err := w.GetTicketRank(ctx, writeq.GetTicketRankParams{TenantID: e.A, ID: old.Id})
		if err != nil {
			return err
		}
		left = st.Rank
		return store.ErrNoChange
	})
	require.ErrorIs(t, err, store.ErrNoChange)
	assert.Nil(t, left, "a ticket dropped since it was read as unranked gets no key")
}
