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
		assert.Equal(t, it.Team.Slug+"/", it.Ticket.Key[:len(it.Team.Slug)+1], "the tenant beside the key is the key's")
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

// myProjects reads the person's projects as c, with a query, and returns
// "<team>/<KEY>" per item.
func (e ticketEnv) myProjects(t *testing.T, c caller, query string) ([]string, apigen.MyProjectList) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/me/projects"+query, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	l := decode[apigen.MyProjectList](t, res)
	out := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, it.Team.Slug+"/"+it.Project.Key)
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

// docs/adr/0018 D3, docs/adr/0023 D2, docs/adr/0014 D5: "assigned to me" holds
// the person's open tickets across their tenants, each beside its tenant and
// its place in its project's rank, in the order of the score — the rank does
// not order it; done and dropped tickets, another person's, and a project
// restricted away from the person are absent; a tenant narrows it, and a
// restricted token reads its tenant or its project only.
func TestAssignedToMeAcrossTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	toBoth := func(b *apigen.TicketCreate) { b.Assignee = &e.Both }
	sev := func(s apigen.Severity) func(*apigen.TicketCreate) {
		return func(b *apigen.TicketCreate) { b.Severity = s }
	}

	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	first := e.fileIn(t, admin, e.SlugA, "ALPHA", task("first", toBoth))                             // 3 + 1
	second := e.fileIn(t, admin, e.SlugA, "ALPHA", task("second", toBoth, sev(apigen.SeverityHigh))) // 5 + 1
	e.fileIn(t, admin, e.SlugA, "ALPHA", task("the member's", func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	done := e.fileIn(t, admin, e.SlugA, "ALPHA", task("done", toBoth, sev(apigen.SeverityCritical)))
	e.send(t, admin, http.StatusOK, http.MethodPost, ticketPath(e.SlugA, "ALPHA", done.Number)+"/transitions",
		map[string]any{"from": "filed", "to": "done", "note": "verified by hand"})
	inGamma := e.fileIn(t, admin, e.SlugA, "GAMMA", task("gamma", toBoth, sev(apigen.SeverityLow))) // 1 + 1
	e.fileIn(t, admin, e.SlugA, "HIDDEN", task("hidden", toBoth, sev(apigen.SeverityCritical)))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("in B", toBoth, sev(apigen.SeverityCritical))) // 8 + 1
	// The first before the second in the project's rank, against the score.
	e.send(t, admin, http.StatusOK, http.MethodPut, ticketPath(e.SlugA, "ALPHA", first.Number)+"/rank", map[string]any{"before": second.Number})
	// GAMMA restricted with the person on its list, HIDDEN restricted without.
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id IN ($1, $2)", gamma, hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')",
		e.A, gamma, e.Both))

	want := []string{inB.Key, second.Key, first.Key, inGamma.Key}
	keys, list := e.assigned(t, both, "")
	assert.Equal(t, want, keys, "by score: 9, 6, 4, 2")
	assert.Equal(t, apigen.TeamRef{Slug: e.SlugB, Name: "Team B"}, list.Items[0].Team)
	assert.Equal(t, apigen.TeamRef{Slug: e.SlugA, Name: "Team A"}, list.Items[3].Team)
	assert.Equal(t, "second", list.Items[1].Ticket.Title, "the whole ticket")
	assert.Equal(t, []int{1, 2, 1, 1}, []int{list.Items[0].Place, list.Items[1].Place, list.Items[2].Place, list.Items[3].Place},
		"the place in the project's rank beside the score: the first stands before the second")

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
// away from the person are absent; the order is the score of the ticket, then
// the ticket and the question's number (docs/adr/0014 D5).
func TestOpenDecisionsAcrossTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	ask := func(c caller, slug, project string, number int, body map[string]any) {
		e.send(t, c, http.StatusCreated, http.MethodPost, ticketPath(slug, project, number)+"/questions", body)
	}
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	first := e.fileIn(t, admin, e.SlugA, "ALPHA", task("first")) // 3 + 1
	second := e.fileIn(t, admin, e.SlugA, "ALPHA", task("second", func(b *apigen.TicketCreate) { b.Severity = apigen.SeverityHigh }))
	inHidden := e.fileIn(t, admin, e.SlugA, "HIDDEN", task("hidden"))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("in B", func(b *apigen.TicketCreate) { b.Severity = apigen.SeverityLow }))

	ask(admin, e.SlugA, "ALPHA", first.Number, map[string]any{"question": "asked of the person", "asked_of": e.Both})
	ask(admin, e.SlugA, "ALPHA", first.Number, map[string]any{"question": "asked of the member", "asked_of": e.MemberA})
	ask(admin, e.SlugA, "ALPHA", first.Number, map[string]any{"question": "open in the tenant"})
	ask(admin, e.SlugA, "ALPHA", second.Number, map[string]any{"question": "answered", "asked_of": e.Both})
	e.send(t, both, http.StatusOK, http.MethodPut, ticketPath(e.SlugA, "ALPHA", second.Number)+"/questions/1/answer", map[string]any{"answer": "done"})
	ask(admin, e.SlugA, "ALPHA", second.Number, map[string]any{"question": "on the second", "asked_of": e.Both})
	ask(admin, e.SlugA, "HIDDEN", inHidden.Number, map[string]any{"question": "on a hidden project", "asked_of": e.Both})
	ask(memberB, e.SlugB, "BETA", inB.Number, map[string]any{"question": "open in B"})
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", hidden))

	want := []string{second.Key + " Q2", first.Key + " Q1", first.Key + " Q3", inB.Key + " Q1"}
	got, list := e.decisions(t, both, "")
	assert.Equal(t, want, got, "by the score of the ticket: 6, 4, 2")
	assert.Equal(t, e.SlugB, list.Items[3].Team.Slug)
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

// docs/adr/0023 D2 as amended 2026-10-10, docs/adr/0021 D5, docs/adr/0034 D3:
// the person's projects are every project they see in each of their teams,
// beside the team, by the team's slug and then the key — a restricted project
// only to the team's administrators and the people on its list, an archived
// one to nobody —; a team narrows the list, a restricted token reads its team
// or its project only, a team a global administrator holds no role in is not
// theirs, and a team the person left takes its projects with it at once.
func TestMyProjectsAcrossTeams(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	both := caller{Token: e.tk.Both}
	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	old, err := f.Project(e.ctx, e.A, "OLD", "Old")
	require.NoError(t, err)
	_, err = f.Project(e.ctx, e.B, "DELTA", "Delta")
	require.NoError(t, err)
	// GAMMA restricted with the person on its list, HIDDEN restricted without; OLD archived.
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id IN ($1, $2)", gamma, hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'viewer')",
		e.A, gamma, e.Both))
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET archived_at = now() WHERE id = $1", old))
	a, b := func(key string) string { return e.SlugA + "/" + key }, func(key string) string { return e.SlugB + "/" + key }

	want := []string{a("ALPHA"), a("GAMMA"), b("BETA"), b("DELTA")}
	keys, list := e.myProjects(t, both, "")
	assert.Equal(t, want, keys, "every team of the person, by its slug, then each project by its key")
	assert.Equal(t, apigen.TeamRef{Slug: e.SlugA, Name: "Team A"}, list.Items[1].Team)
	assert.Equal(t, "Gamma", list.Items[1].Project.Name, "the whole project")
	assert.True(t, list.Items[1].Project.Restricted)
	assert.Equal(t, apigen.TeamRef{Slug: e.SlugB, Name: "Team B"}, list.Items[3].Team)

	assert.Equal(t, want, walk(t, func(query string) ([]string, *string) {
		keys, l := e.myProjects(t, both, query)
		next, err := l.NextCursor.Get()
		if err != nil {
			return keys, nil
		}
		return keys, &next
	}), "the cursor walks the same order, one per page, across the teams")

	member, _ := e.myProjects(t, caller{Token: e.tk.MemberA}, "")
	assert.Equal(t, []string{a("ALPHA")}, member, "a restricted project is not listed to a member off its list")
	require.NoError(t, f.GlobalAdmin(e.ctx, e.AdminA))
	admin, _ := e.myProjects(t, caller{Token: e.tk.AdminA}, "")
	assert.Equal(t, []string{a("ALPHA"), a("GAMMA"), a("HIDDEN")}, admin,
		"an administrator sees every project of the team, and a global administrator nothing of a team without a role")

	onlyB, _ := e.myProjects(t, both, "?team="+e.SlugB)
	assert.Equal(t, want[2:], onlyB)
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/projects?team=no-such-team", nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, "/api/v1/me/projects?team="+e.SlugA, nil),
		http.StatusNotFound, "not_found")
	_, first := e.myProjects(t, both, "?limit=1")
	cursor, err := first.NextCursor.Get()
	require.NoError(t, err)
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/projects?limit=1&team="+e.SlugA+"&cursor="+cursor, nil),
		http.StatusBadRequest, "invalid_cursor")
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/projects?limit=1&cursor=tampered", nil), http.StatusBadRequest, "invalid_cursor")

	teamToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})
	require.NoError(t, err)
	byTeam, _ := e.myProjects(t, caller{Token: teamToken}, "")
	assert.Equal(t, want[2:], byTeam)
	projectToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.A, ProjectID: e.ProjectA})
	require.NoError(t, err)
	byProject, _ := e.myProjects(t, caller{Token: projectToken}, "")
	assert.Equal(t, []string{a("ALPHA")}, byProject)

	require.NoError(t, f.Exec(e.ctx, "DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2", e.B, e.Both))
	left, _ := e.myProjects(t, both, "")
	assert.Equal(t, want[:2], left, "nothing of a team the person left")
}
