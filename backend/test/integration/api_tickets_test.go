//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// ticketEnv is a world with its tokens and a running API.
type ticketEnv struct {
	world
	tk  tokens
	s   apiServer
	ctx context.Context
}

func newTicketEnv(t *testing.T) ticketEnv {
	t.Helper()
	w := newWorld(t)
	return ticketEnv{world: w, tk: issueTokens(t, w), s: newAPI(t), ctx: context.Background()}
}

// task is a plain ticket body; edit adjusts it.
func task(title string, edit ...func(*apigen.TicketCreate)) apigen.TicketCreate {
	b := apigen.TicketCreate{Type: apigen.TicketTypeTask, Title: title, Severity: apigen.SeverityMedium,
		Security: apigen.SecurityClassNone, Effort: apigen.EffortS}
	for _, e := range edit {
		e(&b)
	}
	return b
}

// file creates a ticket as c and requires the 201; agents send a key.
func (e ticketEnv) file(t *testing.T, c caller, project string, body apigen.TicketCreate) apigen.Ticket {
	t.Helper()
	res := e.create(t, c, project, body)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	return *res.JSON201
}

func (e ticketEnv) create(t *testing.T, c caller, project string, body apigen.TicketCreate) *apigen.CreateTicketResponse {
	t.Helper()
	params := &apigen.CreateTicketParams{}
	if c.Agent != "" {
		params.IdempotencyKey = newKey()
	}
	res, err := e.s.client(t, c).CreateTicketWithResponse(e.ctx, e.SlugA, project, params, body)
	require.NoError(t, err)
	return res
}

func (e ticketEnv) get(t *testing.T, c caller, project string, number int) *apigen.GetTicketResponse {
	t.Helper()
	res, err := e.s.client(t, c).GetTicketWithResponse(e.ctx, e.SlugA, project, number)
	require.NoError(t, err)
	return res
}

func (e ticketEnv) patch(t *testing.T, c caller, tk apigen.Ticket, body apigen.TicketPatch) *apigen.UpdateTicketResponse {
	t.Helper()
	etag := strconv.Quote(strconv.Itoa(tk.Version))
	res, err := e.s.client(t, c).UpdateTicketWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number, &apigen.UpdateTicketParams{IfMatch: &etag}, body)
	require.NoError(t, err)
	return res
}

// titles lists a ticket list's titles as c, from a raw query string.
func (e ticketEnv) titles(t *testing.T, c caller, path, query string) []string {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, path+"?"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode, query)
	var list apigen.TicketList
	require.NoError(t, json.NewDecoder(res.Body).Decode(&list))
	out := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it.Title)
	}
	return out
}

func (e ticketEnv) tenantTickets() string { return "/api/v1/tenants/" + e.SlugA + "/tickets" }

func (e ticketEnv) projectTickets(project string) string {
	return "/api/v1/tenants/" + e.SlugA + "/projects/" + project + "/tickets"
}

// docs/adr/0007 D4, docs/adr/0022 D2: numbers are consecutive per project and
// independent across projects; a refused filing consumes no number.
func TestFilingTickets(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)

	first := e.create(t, member, "ALPHA", task("one"))
	require.Equal(t, http.StatusCreated, first.StatusCode(), string(first.Body))
	tk := *first.JSON201
	assert.Equal(t, 1, tk.Number)
	assert.Equal(t, e.SlugA+"/ALPHA-1", tk.Key)
	assert.Equal(t, "/api/v1/tenants/"+e.SlugA+"/projects/ALPHA/tickets/1", *first.Headers201.Location)
	assert.Equal(t, `"1"`, *first.Headers201.ETag)
	assert.Equal(t, apigen.TicketStateFiled, tk.State)
	assert.Equal(t, apigen.UrgencyLater, tk.Urgency, "rule set v1's default (docs/adr/0010 D3)")
	assert.Equal(t, "v1:default", tk.UrgencyRule)
	assert.Equal(t, e.MemberA, tk.Reporter.Id)
	assert.False(t, tk.Confidential)

	assert.Equal(t, 2, e.file(t, member, "ALPHA", task("two")).Number)
	assert.Equal(t, 1, e.file(t, member, "GAMMA", task("gamma one")).Number, "each project counts its own")

	refused := e.create(t, member, "ALPHA", task("refused", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberB }))
	require.Equal(t, http.StatusBadRequest, refused.StatusCode())
	assert.Equal(t, "/assignee", (*refused.ApplicationproblemJSONDefault.Errors)[0].Pointer)
	assert.Equal(t, 3, e.file(t, member, "ALPHA", task("three")).Number, "the refused filing consumed no number")

	viewer := e.create(t, caller{Token: e.tk.ViewerA}, "ALPHA", task("viewer"))
	assert.Equal(t, http.StatusForbidden, viewer.StatusCode())

	noKey, err := e.s.client(t, caller{Token: e.tk.AgentA}).CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("agent"))
	require.NoError(t, err)
	assert.Equal(t, "idempotency_key_required", string(noKey.ApplicationproblemJSONDefault.Code))
	agent := e.file(t, caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}, "ALPHA", task("by the agent"))
	assert.Equal(t, 4, agent.Number, "filing is in the agent baseline (docs/adr/0043 D2)")

	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET archived_at = now() WHERE id = $1", gamma))
	archived := e.create(t, member, "GAMMA", task("late"))
	require.Equal(t, http.StatusConflict, archived.StatusCode())
	assert.Equal(t, "project_archived", string(archived.ApplicationproblemJSONDefault.Code))

	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'ticket' AND action = 'created'", e.A)
	require.NoError(t, err)
	assert.EqualValues(t, 5, n)
}

// Concurrent filings get distinct consecutive numbers: the counter row's lock
// orders them (docs/adr/0022 D2).
func TestConcurrentFilingsGetDistinctNumbers(t *testing.T) {
	e := newTicketEnv(t)
	const n = 12
	cl := e.s.client(t, caller{Token: e.tk.MemberA})
	var wg sync.WaitGroup
	numbers := make(chan int, n)
	for i := range n {
		wg.Go(func() {
			res, err := cl.CreateTicketWithResponse(e.ctx, e.SlugA, "ALPHA",
				&apigen.CreateTicketParams{}, task(fmt.Sprintf("concurrent %d", i)))
			if err == nil && res.JSON201 != nil {
				numbers <- res.JSON201.Number
			}
		})
	}
	wg.Wait()
	close(numbers)
	var got []int
	for v := range numbers {
		got = append(got, v)
	}
	slices.Sort(got)
	want := make([]int, n)
	for i := range want {
		want[i] = i + 1
	}
	assert.Equal(t, want, got)
}

// docs/adr/0023 D3: the resolver answers the canonical route's body and ETag;
// a key of another tenant, or of nothing, is the same 404.
func TestResolvingTicketKeys(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("resolve me"))

	canonical := e.get(t, member, "ALPHA", tk.Number)
	require.Equal(t, http.StatusOK, canonical.StatusCode())
	resolved, err := e.s.client(t, member).ResolveTicketWithResponse(e.ctx, e.SlugA, "ALPHA-"+strconv.Itoa(tk.Number))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resolved.StatusCode(), string(resolved.Body))
	assert.Equal(t, string(canonical.Body), string(resolved.Body))
	assert.Equal(t, canonical.HTTPResponse.Header.Get("ETag"), resolved.HTTPResponse.Header.Get("ETag"))
	assert.NotEqual(t, http.StatusNotModified, canonical.StatusCode(), "an entity read never answers 304 (docs/adr/0050 D2)")

	for _, path := range []string{
		"/api/v1/tickets/" + e.SlugA + "/ALPHA-99",
		"/api/v1/tickets/" + e.SlugA + "/ALPHA-01",
		"/api/v1/tickets/" + e.SlugA + "/alpha-1",
		"/api/v1/tickets/" + e.SlugB + "/ALPHA-1",
		"/api/v1/tickets/" + e.SlugA + "/ALPHA-4294967297",
		e.projectTickets("ALPHA") + "/4294967297",
	} {
		assertProblem(t, e.s.do(t, member, http.MethodGet, path, nil), http.StatusNotFound, "not_found")
	}
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, "/api/v1/tickets/"+e.SlugA+"/ALPHA-1", nil),
		http.StatusNotFound, "not_found")
}

// docs/adr/0010 D2: a security class needs its threat, and none carries none.
func TestTheThreatRule(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	missing := e.create(t, member, "ALPHA", task("live", func(b *apigen.TicketCreate) { b.Security = apigen.SecurityClassLive }))
	require.Equal(t, http.StatusBadRequest, missing.StatusCode())
	assert.Equal(t, "/threat", (*missing.ApplicationproblemJSONDefault.Errors)[0].Pointer)
	extra := e.create(t, member, "ALPHA", task("none", func(b *apigen.TicketCreate) { b.Threat = ptr("nothing") }))
	require.Equal(t, http.StatusBadRequest, extra.StatusCode())

	tk := e.file(t, member, "ALPHA", task("hardening", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassHardening, ptr("defence in depth")
	}))
	res := e.patch(t, member, tk, apigen.TicketPatch{Security: ptr(apigen.SecurityClassNone)})
	require.Equal(t, http.StatusBadRequest, res.StatusCode(), "dropping the class must drop the threat too")
	res = e.patch(t, member, tk, apigen.TicketPatch{Security: ptr(apigen.SecurityClassNone), Threat: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.Threat.IsNull())
}

// docs/adr/0008 D2: a parent is a ticket of the same project, and no change
// closes a cycle — directly, over two steps, over n, or by two concurrent
// re-parentings.
func TestParentCycles(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	_, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	key := func(tk apigen.Ticket) string { return "ALPHA-" + strconv.Itoa(tk.Number) }

	a := e.file(t, member, "ALPHA", task("a"))
	b := e.file(t, member, "ALPHA", task("b", func(c *apigen.TicketCreate) { c.Parent = ptr(key(a)) }))
	c := e.file(t, member, "ALPHA", task("c", func(c *apigen.TicketCreate) { c.Parent = ptr(e.SlugA + "/" + key(b)) }))
	assert.Equal(t, e.SlugA+"/"+key(b), c.Parent.MustGet(), "the parent is shown by its full key")

	for _, target := range []apigen.Ticket{a, b, c} {
		res := e.patch(t, member, a, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(key(target))})
		require.Equal(t, http.StatusConflict, res.StatusCode(), "parent %s", target.Title)
		assert.Equal(t, "parent_cycle", string(res.ApplicationproblemJSONDefault.Code))
	}

	other := e.file(t, member, "GAMMA", task("elsewhere"))
	res := e.patch(t, member, a, apigen.TicketPatch{Parent: nullable.NewNullableWithValue("GAMMA-" + strconv.Itoa(other.Number))})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode(), "a parent of another project")
	res = e.patch(t, member, a, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(e.SlugB + "/BETA-1")})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode(), "a parent of another tenant")

	cl := e.s.client(t, member)
	for round := range 5 {
		x := e.file(t, member, "ALPHA", task(fmt.Sprintf("x%d", round)))
		y := e.file(t, member, "ALPHA", task(fmt.Sprintf("y%d", round)))
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i, pair := range [][2]apigen.Ticket{{x, y}, {y, x}} {
			wg.Go(func() {
				etag := strconv.Quote(strconv.Itoa(pair[0].Version))
				res, err := cl.UpdateTicketWithResponse(e.ctx, e.SlugA, "ALPHA", pair[0].Number,
					&apigen.UpdateTicketParams{IfMatch: &etag}, apigen.TicketPatch{Parent: nullable.NewNullableWithValue(key(pair[1]))})
				if err == nil {
					codes[i] = res.StatusCode()
				}
			})
		}
		wg.Wait()
		slices.Sort(codes)
		assert.Equal(t, []int{http.StatusOK, http.StatusConflict}, codes, "round %d: exactly one re-parenting wins", round)
	}
}

// docs/adr/0049: OR within a parameter, AND between them, ! negates, me is
// the caller, none the empty column; unknown parameters and values are
// refused, never ignored.
func TestTicketFilters(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	_, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)

	export := e.file(t, member, "ALPHA", task("Export drops attachments", func(b *apigen.TicketCreate) {
		b.Type, b.Severity, b.Body, b.Assignee = apigen.TicketTypeBug, apigen.SeverityHigh, ptr("the zip has no files"), &e.MemberA
	}))
	e.file(t, member, "ALPHA", task("Rename the Café board", func(b *apigen.TicketCreate) {
		b.Severity, b.Effort = apigen.SeverityLow, apigen.EffortM
	}))
	dark := e.file(t, member, "ALPHA", task("Dark mode", func(b *apigen.TicketCreate) {
		b.Type, b.Effort, b.Assignee = apigen.TicketTypeFeature, apigen.EffortL, &e.Both
		b.Security, b.Threat = apigen.SecurityClassHardening, ptr("glare")
	}))
	old := e.file(t, member, "ALPHA", task("Old work"))
	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET state = 'done', done_at = now() WHERE id = $1", old.Id))
	e.file(t, member, "GAMMA", task("Elsewhere", func(b *apigen.TicketCreate) { b.Type = apigen.TicketTypeQuestion }))
	e.file(t, member, "ALPHA", task("Child", func(b *apigen.TicketCreate) { b.Parent = ptr("ALPHA-" + strconv.Itoa(export.Number)) }))
	res := e.patch(t, member, dark, apigen.TicketPatch{Progress: ptr(50)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))

	all := e.tenantTickets()
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"", []string{"Child", "Elsewhere", "Dark mode", "Rename the Café board", "Export drops attachments"}},
		{"type=bug&type=feature", []string{"Dark mode", "Export drops attachments"}},
		{"type=task&severity=low", []string{"Rename the Café board"}},
		{"type=!task", []string{"Elsewhere", "Dark mode", "Export drops attachments"}},
		{"assignee=me", []string{"Export drops attachments"}},
		{"assignee=none&project=ALPHA", []string{"Child", "Rename the Café board"}},
		{"assignee=!me&project=ALPHA", []string{"Child", "Dark mode", "Rename the Café board"}},
		{"assignee=" + e.Both.String() + "&assignee=me", []string{"Dark mode", "Export drops attachments"}},
		{"reporter=me&effort=L", []string{"Dark mode"}},
		{"state=done", []string{"Old work"}},
		{"state=!filed", nil},
		{"include_terminal=true&project=ALPHA&type=task&parent=none", []string{"Old work", "Rename the Café board"}},
		{"project=GAMMA", []string{"Elsewhere"}},
		{"project=!GAMMA&type=!task", []string{"Dark mode", "Export drops attachments"}},
		{"q=attachments", []string{"Export drops attachments"}},
		{"q=files", []string{"Export drops attachments"}},
		{"q=cafe", []string{"Rename the Café board"}},
		{"progress_min=50", []string{"Dark mode"}},
		{"parent=ALPHA-" + strconv.Itoa(export.Number), []string{"Child"}},
		{"parent=!none", []string{"Child"}},
		{"parent=ALPHA-999", nil},
		{"opened_after=" + url.QueryEscape("2999-01-01T00:00:00Z"), nil},
		{"updated_before=" + url.QueryEscape("2000-01-01T00:00:00Z"), nil},
	} {
		got := e.titles(t, member, all, c.query)
		if c.want == nil {
			c.want = []string{}
		}
		assert.Equal(t, c.want, got, c.query)
	}

	project := e.titles(t, member, e.projectTickets("ALPHA"), "type=!feature")
	assert.Equal(t, []string{"Export drops attachments", "Rename the Café board", "Child"}, project, "a project's list is in filing order")

	for _, c := range []struct{ query, pointer string }{
		{"foo=1", "query:foo"},
		{"state=open", "query:state"},
		{"severity=!urgent", "query:severity"},
		{"assignee=bob", "query:assignee"},
		{"reporter=none", "query:reporter"},
		{"parent=alpha", "query:parent"},
		{"project=gamma", "query:project"},
		{"q=" + strings.Repeat("x", 257), "query:q"},
		{"opened_after=yesterday", "query:opened_after"},
	} {
		body := assertProblem(t, e.s.do(t, member, http.MethodGet, all+"?"+c.query, nil), http.StatusBadRequest, "validation_failed")
		errs, _ := body["errors"].([]any)
		require.NotEmpty(t, errs, c.query)
		assert.Equal(t, c.pointer, errs[0].(map[string]any)["pointer"], c.query)
	}
	body := assertProblem(t, e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA")+"?project=ALPHA", nil), http.StatusBadRequest, "validation_failed")
	assert.Contains(t, fmt.Sprint(body["errors"]), "query:project", "a project's list takes no project filter")
}

// docs/adr/0048: cursor pages on both lists, numbered pages with a total and
// a depth cap; docs/adr/0054 D7: a weak ETag and 304.
func TestTicketPaging(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	for i := 1; i <= 5; i++ {
		e.file(t, member, "ALPHA", task(fmt.Sprintf("t%d", i)))
	}
	cl := e.s.client(t, member)

	var seen []string
	params := &apigen.ListProjectTicketsParams{Limit: ptr(2)}
	for range 4 {
		res, err := cl.ListProjectTicketsWithResponse(e.ctx, e.SlugA, "ALPHA", params)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		for _, it := range res.JSON200.Items {
			seen = append(seen, it.Title)
		}
		assert.Nil(t, res.JSON200.Total, "a cursor page carries no total")
		if res.JSON200.NextCursor.IsNull() {
			break
		}
		params.Cursor = ptr(res.JSON200.NextCursor.MustGet())
	}
	assert.Equal(t, []string{"t1", "t2", "t3", "t4", "t5"}, seen)

	newest, err := cl.ListTenantTicketsWithResponse(e.ctx, e.SlugA, &apigen.ListTenantTicketsParams{Limit: ptr(2)})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, newest.StatusCode())
	assert.Equal(t, "t5", newest.JSON200.Items[0].Title)
	next, err := cl.ListTenantTicketsWithResponse(e.ctx, e.SlugA, &apigen.ListTenantTicketsParams{Limit: ptr(2), Cursor: ptr(newest.JSON200.NextCursor.MustGet())})
	require.NoError(t, err)
	assert.Equal(t, "t3", next.JSON200.Items[0].Title)
	foreign := e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA")+"?cursor="+url.QueryEscape(newest.JSON200.NextCursor.MustGet()), nil)
	assertProblem(t, foreign, http.StatusBadRequest, "invalid_cursor")

	numbered, err := cl.ListProjectTicketsWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.ListProjectTicketsParams{Page: ptr(2), PerPage: ptr(apigen.ListProjectTicketsParamsPerPage(25))})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, numbered.StatusCode(), string(numbered.Body))
	assert.Empty(t, numbered.JSON200.Items)
	assert.Equal(t, 5, *numbered.JSON200.Total)
	assert.Equal(t, 2, *numbered.JSON200.Page)
	first := e.titles(t, member, e.projectTickets("ALPHA"), "page=1&per_page=25")
	assert.Len(t, first, 5)
	e.titles(t, member, e.projectTickets("ALPHA"), "page=400&per_page=25")

	assertProblem(t, e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA")+"?page=401&per_page=25", nil), http.StatusBadRequest, "page_too_deep")
	for _, q := range []string{"page=1&cursor=abc", "page=1&limit=10", "per_page=25"} {
		assertProblem(t, e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA")+"?"+q, nil), http.StatusBadRequest, "validation_failed")
	}

	list := e.s.do(t, member, http.MethodGet, e.tenantTickets(), nil)
	require.Equal(t, http.StatusOK, list.StatusCode)
	etag := list.Header.Get("ETag")
	require.True(t, strings.HasPrefix(etag, `W/"`), etag)
	same := e.s.do(t, member, http.MethodGet, e.tenantTickets(), nil, "If-None-Match", etag)
	assert.Equal(t, http.StatusNotModified, same.StatusCode)
	assert.Equal(t, etag, same.Header.Get("ETag"))
	e.file(t, member, "ALPHA", task("t6"))
	changed := e.s.do(t, member, http.MethodGet, e.tenantTickets(), nil, "If-None-Match", etag)
	assert.Equal(t, http.StatusOK, changed.StatusCode)
	assert.NotEqual(t, etag, changed.Header.Get("ETag"))
}

// docs/adr/0065: live and boundary set the flag; a confidential ticket is
// visible to administrators, its assignee and its reporter only; nothing but
// an administrator's reasoned act lifts it, never an agent's.
func TestConfidentialTickets(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin, viewer, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}, caller{Token: e.tk.ViewerA}, caller{Token: e.tk.Both}
	secret := e.file(t, member, "ALPHA", task("Token leak", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("tokens in the log")
	}))
	require.True(t, secret.Confidential)
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'confidential_set'", secret.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	sees := func(c caller) bool {
		res := e.get(t, c, "ALPHA", secret.Number)
		listed := slices.Contains(e.titles(t, c, e.tenantTickets(), ""), "Token leak")
		assert.Equal(t, res.StatusCode() == http.StatusOK, listed, "the list and the read agree")
		if res.StatusCode() != http.StatusOK {
			assert.Equal(t, http.StatusNotFound, res.StatusCode())
			assert.Equal(t, "not_found", string(res.ApplicationproblemJSONDefault.Code))
		}
		return res.StatusCode() == http.StatusOK
	}
	assert.True(t, sees(admin), "administrator")
	assert.True(t, sees(member), "reporter")
	assert.False(t, sees(viewer), "another member")
	assert.False(t, sees(both), "another member")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, e.projectTickets("ALPHA")+"/"+strconv.Itoa(secret.Number), nil),
		http.StatusNotFound, "not_found")

	res := e.patch(t, member, secret, apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.Both)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, sees(both), "assignment admits the assignee (docs/adr/0065 D9)")
	assert.False(t, sees(viewer))

	res = e.patch(t, both, *res.JSON200, apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.ViewerA)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, e.ViewerA, res.JSON200.Assignee.MustGet().Id, "the writer is answered with what it wrote")
	assert.False(t, sees(both), "the former assignee is out")
	assert.True(t, sees(viewer), "the new assignee is in")

	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	res = e.patch(t, agent, *res.JSON200, apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.Both)})
	require.Equal(t, http.StatusOK, res.StatusCode(), "an agent reassigns a confidential ticket: the open gate")

	res = e.patch(t, member, *res.JSON200, apigen.TicketPatch{Security: ptr(apigen.SecurityClassNone), Threat: nullable.NewNullNullable[string]()})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.Confidential, "a change of the class never lifts the flag (docs/adr/0065 D3)")
	current := *res.JSON200

	set := func(c caller, confidential bool, reason *string, tk apigen.Ticket) *apigen.SetConfidentialResponse {
		etag := strconv.Quote(strconv.Itoa(tk.Version))
		r, err := e.s.client(t, c).SetConfidentialWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number,
			&apigen.SetConfidentialParams{IfMatch: &etag}, apigen.ConfidentialSet{Confidential: confidential, Reason: reason})
		require.NoError(t, err)
		return r
	}
	assert.Equal(t, http.StatusForbidden, set(member, false, ptr("fixed"), current).StatusCode())
	assert.Equal(t, "insufficient_scope", string(set(caller{Token: e.tk.AdminAWrite}, false, ptr("fixed"), current).ApplicationproblemJSONDefault.Code))
	hardOff := set(caller{Token: e.tk.AdminA, Agent: "claude-code/opus/s1"}, false, ptr("fixed"), current)
	assert.Equal(t, "hard-off: setting or lifting the confidential flag", *hardOff.ApplicationproblemJSONDefault.Detail)
	noReason := set(admin, false, nil, current)
	assert.Equal(t, http.StatusBadRequest, noReason.StatusCode())
	lifted := set(admin, false, ptr("the leak is fixed and rotated"), current)
	require.Equal(t, http.StatusOK, lifted.StatusCode(), string(lifted.Body))
	assert.False(t, lifted.JSON200.Confidential)
	assert.True(t, sees(caller{Token: e.tk.ViewerA}), "lifted: every member sees it")
	reason, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'confidential_lifted' AND reason = 'the leak is fixed and rotated'", secret.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, reason)

	plain := e.file(t, member, "ALPHA", task("Plain"))
	res = e.patch(t, member, plain, apigen.TicketPatch{Security: ptr(apigen.SecurityClassBoundary), Threat: nullable.NewNullableWithValue("tenant crossing")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.True(t, res.JSON200.Confidential, "a change to boundary sets the flag")
	lifted = set(admin, false, ptr("not a boundary after all"), *res.JSON200)
	require.Equal(t, http.StatusOK, lifted.StatusCode())
	res = e.patch(t, member, *lifted.JSON200, apigen.TicketPatch{Title: ptr("Plain again")})
	require.Equal(t, http.StatusOK, res.StatusCode())
	assert.False(t, res.JSON200.Confidential, "an unchanged class does not set again what an administrator lifted")
}

// docs/adr/0034 D3, D4: a restricted project's tickets are visible to the
// people on its list and to administrators only, and only they can be
// assigned.
func TestRestrictedProjectTickets(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	secret, err := f.Project(e.ctx, e.A, "SECRET", "Secret")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", secret))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')", e.A, secret, e.Both))

	tk := e.file(t, caller{Token: e.tk.AdminA}, "SECRET", task("Hidden work"))
	for _, c := range []struct {
		name    string
		caller  caller
		visible bool
	}{
		{"on the list", caller{Token: e.tk.Both}, true},
		{"administrator", caller{Token: e.tk.AdminA}, true},
		{"off the list", caller{Token: e.tk.MemberA}, false},
	} {
		res := e.get(t, c.caller, "SECRET", tk.Number)
		assert.Equal(t, c.visible, res.StatusCode() == http.StatusOK, c.name)
		assert.Equal(t, c.visible, slices.Contains(e.titles(t, c.caller, e.tenantTickets(), ""), "Hidden work"), c.name)
	}
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, e.projectTickets("SECRET"), nil), http.StatusNotFound, "not_found")

	res := e.patch(t, caller{Token: e.tk.Both}, tk, apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.MemberA)})
	require.Equal(t, http.StatusBadRequest, res.StatusCode(), "a member off the list cannot be assigned")
	res = e.patch(t, caller{Token: e.tk.Both}, tk, apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.Both)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
}

// docs/adr/0010 D3: the derived urgency, an override and its withdrawal; an
// agent needs override-urgency (docs/adr/0043 D4) and a reason, which the
// override shows.
func TestUrgencyOverride(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Escalated"))
	override := func(c caller, version int) *apigen.OverrideUrgencyResponse {
		etag := strconv.Quote(strconv.Itoa(version))
		res, err := e.s.client(t, c).OverrideUrgencyWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number,
			&apigen.OverrideUrgencyParams{IfMatch: &etag}, apigen.UrgencyOverrideSet{Value: apigen.UrgencyNow, Reason: ptr("a customer is down")})
		require.NoError(t, err)
		return res
	}

	narrow, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{"interest"}})
	require.NoError(t, err)
	refused := override(caller{Token: narrow, Agent: "claude-code/opus/s1"}, tk.Version)
	require.Equal(t, http.StatusForbidden, refused.StatusCode())
	assert.Equal(t, "missing capability: override-urgency", *refused.ApplicationproblemJSONDefault.Detail)

	res := override(caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}, tk.Version)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, apigen.UrgencyNow, res.JSON200.Urgency)
	assert.Equal(t, apigen.UrgencyLater, res.JSON200.UrgencyDerived)
	o := res.JSON200.UrgencyOverride.MustGet()
	assert.Equal(t, "a customer is down", o.Reason.MustGet())
	assert.Equal(t, e.MemberA, o.By.MustGet().Id)
	assert.Equal(t, tk.Version+1, res.JSON200.Version)

	etag := strconv.Quote(strconv.Itoa(res.JSON200.Version))
	back, err := e.s.client(t, member).WithdrawUrgencyOverrideWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number, &apigen.WithdrawUrgencyOverrideParams{IfMatch: &etag})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	assert.Equal(t, apigen.UrgencyLater, back.JSON200.Urgency)
	assert.True(t, back.JSON200.UrgencyOverride.IsNull())

	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'overridden'", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
}

// docs/adr/0050: an overwriting write needs If-Match, a stale one answers the
// current values, a change bumps the version and a no-op does not.
func TestEditingTickets(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("First title"))
	path := e.projectTickets("ALPHA") + "/" + strconv.Itoa(tk.Number)

	assertProblem(t, e.s.do(t, member, http.MethodPatch, path, map[string]any{"title": "x"}), http.StatusPreconditionRequired, "precondition_required")

	res := e.patch(t, member, tk, apigen.TicketPatch{Title: ptr("Second title")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, 2, res.JSON200.Version)
	assert.Equal(t, `"2"`, res.HTTPResponse.Header.Get("ETag"))
	again := e.patch(t, member, *res.JSON200, apigen.TicketPatch{Title: ptr("Second title")})
	require.Equal(t, http.StatusOK, again.StatusCode())
	assert.Equal(t, 2, again.JSON200.Version, "a change to nothing is no change")

	stale := e.patch(t, member, tk, apigen.TicketPatch{Title: ptr("Third title")})
	require.Equal(t, http.StatusPreconditionFailed, stale.StatusCode())
	cur := (*stale.ApplicationproblemJSONDefault.Errors)[0]
	assert.Equal(t, "/title", cur.Pointer)
	assert.Equal(t, "Second title", cur.Current.MustGet())
	empty := e.patch(t, member, tk, apigen.TicketPatch{Assignee: nullable.NewNullableWithValue(e.MemberA)})
	require.Equal(t, http.StatusPreconditionFailed, empty.StatusCode())
	cur = (*empty.ApplicationproblemJSONDefault.Errors)[0]
	assert.Equal(t, "/assignee", cur.Pointer)
	assert.True(t, cur.Current.IsSpecified() && cur.Current.IsNull(), "an empty field's current value is null (docs/adr/0050 D5)")

	etag := `"2"`
	body, err := e.s.client(t, member).ReplaceTicketBodyWithResponse(e.ctx, e.SlugA, "ALPHA", tk.Number,
		&apigen.ReplaceTicketBodyParams{IfMatch: &etag}, apigen.TicketBodyReplace{Body: "## Steps\n\n1. open"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, body.StatusCode(), string(body.Body))
	assert.Equal(t, 3, body.JSON200.Version)
	var before, after string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT before->>'body', after->>'body' FROM audit_events
		WHERE ticket_id = $1 AND action = 'updated' AND after ? 'body'`, tk.Id).Scan(&before, &after))
	assert.Equal(t, "", before)
	assert.Equal(t, "## Steps\n\n1. open", after, "the act keeps the previous and the new body (docs/adr/0011 D1)")

	agent := e.patch(t, caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}, *body.JSON200, apigen.TicketPatch{Severity: ptr(apigen.SeverityHigh)})
	require.Equal(t, http.StatusOK, agent.StatusCode(), "editing fields is the agent baseline")
	viewer := e.patch(t, caller{Token: e.tk.ViewerA}, *agent.JSON200, apigen.TicketPatch{Title: ptr("viewer")})
	assert.Equal(t, http.StatusForbidden, viewer.StatusCode())
	outsider := e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodPatch, path, map[string]any{"title": "x"}, "If-Match", `"4"`)
	assertProblem(t, outsider, http.StatusNotFound, "not_found")

	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET state = 'dropped' WHERE id = $1", tk.Id))
	dropped := e.patch(t, member, *agent.JSON200, apigen.TicketPatch{Progress: ptr(40)})
	require.Equal(t, http.StatusConflict, dropped.StatusCode(), "a stage is set in every state but dropped (docs/adr/0017 D2)")
	assert.Equal(t, "state_conflict", string(dropped.ApplicationproblemJSONDefault.Code))

	assignedEvents, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1", tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 4, assignedEvents, "created, two updates, the agent's update")
	var agentName string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT agent FROM audit_events WHERE ticket_id = $1 AND agent IS NOT NULL`, tk.Id).Scan(&agentName))
	assert.Equal(t, "claude-code/opus/s1", agentName)
}
