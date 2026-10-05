//go:build integration

package integration

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// assigned reads "assigned to me" as c, with a query, and returns the keys.
func (e ticketEnv) assigned(t *testing.T, c caller, query string) ([]string, apigen.MyTicketList) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/me/assigned"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	l := decode[apigen.MyTicketList](t, res)
	keys := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		assert.Equal(t, it.Tenant.Slug+"/", it.Ticket.Key[:len(it.Tenant.Slug)+1], "the tenant beside the key is the key's")
		keys = append(keys, it.Ticket.Key)
	}
	return keys, l
}

// decisions reads "open decisions" as c, with a query, and returns
// "<key> Q<n>" per item.
func (e ticketEnv) decisions(t *testing.T, c caller, query string) ([]string, apigen.DecisionList) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/me/decisions"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	l := decode[apigen.DecisionList](t, res)
	out := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, it.Ticket.Key+" Q"+strconv.Itoa(it.Question.Number))
	}
	return out, l
}

// walk follows a person-level list one item per page and returns the keys.
func walk(t *testing.T, read func(query string) ([]string, *string)) []string {
	t.Helper()
	var all []string
	query := "?limit=1"
	for range 20 {
		keys, next := read(query)
		all = append(all, keys...)
		if next == nil {
			return all
		}
		query = "?limit=1&cursor=" + *next
	}
	t.Fatal("the cursor never ended")
	return nil
}

// docs/adr/0018 D3, docs/adr/0023 D2, docs/adr/0014 D5 as it stands until the
// score: "assigned to me" holds the person's open tickets across their tenants,
// each beside its tenant, in the order of the tenant's slug, the project's key
// and the project's rank; done and dropped tickets, another person's, and a
// project restricted away from the person are absent; a tenant narrows it, and
// a restricted token reads its tenant or its project only.
func TestAssignedToMeAcrossTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	toBoth := func(b *apigen.TicketCreate) { b.Assignee = &e.Both }

	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	first := e.fileIn(t, admin, e.SlugA, "ALPHA", task("first", toBoth))
	second := e.fileIn(t, admin, e.SlugA, "ALPHA", task("second", toBoth))
	e.fileIn(t, admin, e.SlugA, "ALPHA", task("the member's", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	done := e.fileIn(t, admin, e.SlugA, "ALPHA", task("done", toBoth))
	e.send(t, admin, http.StatusOK, http.MethodPost, ticketPath(e.SlugA, "ALPHA", done.Number)+"/transitions",
		map[string]any{"from": "filed", "to": "done", "note": "verified by hand"})
	inGamma := e.fileIn(t, admin, e.SlugA, "GAMMA", task("gamma", toBoth))
	e.fileIn(t, admin, e.SlugA, "HIDDEN", task("hidden", toBoth))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("in B", toBoth))
	// The second before the first in the project's rank.
	e.send(t, admin, http.StatusOK, http.MethodPut, ticketPath(e.SlugA, "ALPHA", second.Number)+"/rank", map[string]any{"before": first.Number})
	// GAMMA restricted with the person on its list, HIDDEN restricted without.
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id IN ($1, $2)", gamma, hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')",
		e.A, gamma, e.Both))

	want := []string{second.Key, first.Key, inGamma.Key, inB.Key}
	keys, list := e.assigned(t, both, "")
	assert.Equal(t, want, keys)
	assert.Equal(t, apigen.TenantRef{Slug: e.SlugA, Name: "Tenant A"}, list.Items[0].Tenant)
	assert.Equal(t, apigen.TenantRef{Slug: e.SlugB, Name: "Tenant B"}, list.Items[3].Tenant)
	assert.Equal(t, "second", list.Items[0].Ticket.Title, "the whole ticket")

	assert.Equal(t, want, walk(t, func(query string) ([]string, *string) {
		keys, l := e.assigned(t, both, query)
		next, err := l.NextCursor.Get()
		if err != nil {
			return keys, nil
		}
		return keys, &next
	}), "the cursor walks the same order, one per page")

	onlyB, _ := e.assigned(t, both, "?tenant="+e.SlugB)
	assert.Equal(t, []string{inB.Key}, onlyB)
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/assigned?tenant=no-such-tenant", nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, "/api/v1/me/assigned?tenant="+e.SlugA, nil),
		http.StatusNotFound, "not_found")
	other, _ := e.assigned(t, caller{Token: e.tk.MemberA}, "")
	assert.Len(t, other, 1, "the member's own, nothing of the person's")

	tenantToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})
	require.NoError(t, err)
	byTenant, _ := e.assigned(t, caller{Token: tenantToken}, "")
	assert.Equal(t, []string{inB.Key}, byTenant)
	projectToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.A, ProjectID: e.ProjectA})
	require.NoError(t, err)
	byProject, _ := e.assigned(t, caller{Token: projectToken}, "")
	assert.Equal(t, []string{second.Key, first.Key}, byProject)
}

// docs/adr/0018 D3, docs/adr/0011 D2: "open decisions" holds the open
// questions asked of the person and those open in their tenants, on tickets
// they see, across their tenants, beside the tenant and the ticket; a question
// asked of another person, an answered one and one on a project restricted
// away from the person are absent; the order is the tenant's slug, the
// project's key, the ticket's rank and the question's number.
func TestOpenDecisionsAcrossTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	ask := func(c caller, slug, project string, number int, body map[string]any) {
		e.send(t, c, http.StatusCreated, http.MethodPost, ticketPath(slug, project, number)+"/questions", body)
	}
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	first := e.fileIn(t, admin, e.SlugA, "ALPHA", task("first"))
	second := e.fileIn(t, admin, e.SlugA, "ALPHA", task("second"))
	inHidden := e.fileIn(t, admin, e.SlugA, "HIDDEN", task("hidden"))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("in B"))

	ask(admin, e.SlugA, "ALPHA", first.Number, map[string]any{"question": "asked of the person", "asked_of": e.Both})
	ask(admin, e.SlugA, "ALPHA", first.Number, map[string]any{"question": "asked of the member", "asked_of": e.MemberA})
	ask(admin, e.SlugA, "ALPHA", first.Number, map[string]any{"question": "open in the tenant"})
	ask(admin, e.SlugA, "ALPHA", second.Number, map[string]any{"question": "answered", "asked_of": e.Both})
	e.send(t, both, http.StatusOK, http.MethodPut, ticketPath(e.SlugA, "ALPHA", second.Number)+"/questions/1/answer", map[string]any{"answer": "done"})
	ask(admin, e.SlugA, "ALPHA", second.Number, map[string]any{"question": "on the second", "asked_of": e.Both})
	ask(admin, e.SlugA, "HIDDEN", inHidden.Number, map[string]any{"question": "on a hidden project", "asked_of": e.Both})
	ask(memberB, e.SlugB, "BETA", inB.Number, map[string]any{"question": "open in B"})
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))
	// The second before the first in the project's rank.
	e.send(t, admin, http.StatusOK, http.MethodPut, ticketPath(e.SlugA, "ALPHA", second.Number)+"/rank", map[string]any{"before": first.Number})

	want := []string{second.Key + " Q2", first.Key + " Q1", first.Key + " Q3", inB.Key + " Q1"}
	got, list := e.decisions(t, both, "")
	assert.Equal(t, want, got)
	assert.Equal(t, e.SlugB, list.Items[3].Tenant.Slug)
	assert.Equal(t, "open in B", list.Items[3].Question.Question)
	assert.Equal(t, apigen.QuestionStatusOpen, list.Items[0].Question.Status)

	assert.Equal(t, want, walk(t, func(query string) ([]string, *string) {
		got, l := e.decisions(t, both, query)
		next, err := l.NextCursor.Get()
		if err != nil {
			return got, nil
		}
		return got, &next
	}), "the cursor walks the same order, one per page")

	onlyA, _ := e.decisions(t, both, "?tenant="+e.SlugA)
	assert.Equal(t, want[:3], onlyA)
	member, _ := e.decisions(t, caller{Token: e.tk.MemberA}, "")
	assert.Equal(t, []string{first.Key + " Q2", first.Key + " Q3"}, member, "the member's own and the tenant's open one")
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/decisions?limit=1&cursor=tampered", nil), http.StatusBadRequest, "invalid_cursor")
}
