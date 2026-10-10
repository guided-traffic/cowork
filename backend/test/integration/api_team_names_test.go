//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// A tenant is called a team on every surface, and the names before stay in
// /api/v1 for one release, deprecated, behaving as they did (docs/adr/0005
// D1, docs/adr/0023 D1, docs/adr/0046 D7). The tests of this file hold the
// pairs: a team path and its twin under the family before, a parameter and a
// property by either name, an archive written before the rename.

// teamTwin is a request editor that sends a request of the generated client —
// which names the team family, the twins being left out of it — to its
// deprecated twin in the family before.
func teamTwin(_ context.Context, req *http.Request) error {
	if rest, ok := strings.CutPrefix(req.URL.Path, "/api/v1/teams"); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
		req.URL.Path, req.URL.RawPath = "/api/v1/tenants"+rest, ""
	}
	return nil
}

// accept is a request editor that asks for a media type.
func accept(mediaType string) apigen.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Accept", mediaType)
		return nil
	}
}

// twinClient is the generated client acting as c on the twins.
func (s apiServer) twinClient(t *testing.T, c caller) *apigen.ClientWithResponses {
	t.Helper()
	cl, err := apigen.NewClientWithResponses(s.URL, apigen.WithRequestEditorFn(c.editor), apigen.WithRequestEditorFn(teamTwin))
	require.NoError(t, err)
	return cl
}

// answer is what a request was answered, as a team path and its twin are
// compared: the status, the headers a client reads, and the body without the
// request id — the one value that differs from request to request. A
// problem's instance names the team path on both.
type answer struct {
	Status  int
	Headers map[string]string
	Body    any
}

func answerOf(t *testing.T, res *http.Response) answer {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	a := answer{Status: res.StatusCode, Headers: map[string]string{}, Body: string(body)}
	for _, h := range []string{"Content-Type", "Content-Disposition", "Cache-Control", "ETag", "Location"} {
		a.Headers[h] = res.Header.Get(h)
	}
	if strings.Contains(res.Header.Get("Content-Type"), "json") {
		var v any
		require.NoError(t, json.Unmarshal(body, &v), string(body))
		if m, ok := v.(map[string]any); ok {
			delete(m, "request_id")
		}
		a.Body = v
	}
	return a
}

// readJSON decodes an answer's body as JSON, whatever its status.
func readJSON(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	var v map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&v))
	return v
}

// fieldOf is the pointer of the first field a 400 validation_failed names.
func fieldOf(t *testing.T, res *http.Response) string {
	t.Helper()
	body := assertProblem(t, res, http.StatusBadRequest, "validation_failed")
	errs, ok := body["errors"].([]any)
	require.True(t, ok, "%v", body)
	require.NotEmpty(t, errs)
	return errs[0].(map[string]any)["pointer"].(string)
}

// docs/adr/0023 D1, docs/adr/0046 D7: an operation answers on its twin in the
// family before exactly as on its team path — the status, the headers and the
// body, a problem with its instance included —, a read and a write alike; and
// what a write through a twin did is the team path's to read. The generated
// client is sent to the twins by a request editor; the server's clock stands
// still, so that two reads answer alike to the byte.
func TestATwinAnswersAsItsTeamPath(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	e := ticketEnv{world: w, tk: tk, s: newAPI(t, withLogin, withClock(newClock())), ctx: context.Background()}
	ctx := e.ctx
	member, admin := caller{Token: tk.MemberA}, caller{Token: tk.AdminA}
	seen := e.file(t, member, "ALPHA", task("Seen on both paths"))
	require.Equal(t, http.StatusCreated, e.book(t, member, seen, 30, "2026-10-01").StatusCode())

	alike := func(name string, c caller, send func(cl apigen.ClientInterface) (*http.Response, error)) {
		t.Helper()
		onTeam, err := send(e.s.client(t, c))
		require.NoError(t, err, name)
		onTwin, err := send(e.s.twinClient(t, c))
		require.NoError(t, err, name)
		require.True(t, strings.HasPrefix(onTwin.Request.URL.Path, "/api/v1/tenants/"), "%s went to %s", name, onTwin.Request.URL.Path)
		require.True(t, strings.HasPrefix(onTeam.Request.URL.Path, "/api/v1/teams/"), "%s went to %s", name, onTeam.Request.URL.Path)
		assert.Equal(t, answerOf(t, onTeam), answerOf(t, onTwin), "%s answers alike on its twin", name)
	}
	alike("the team", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.GetTeam(ctx, e.SlugA)
	})
	alike("the team's projects", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.ListProjects(ctx, e.SlugA, &apigen.ListProjectsParams{})
	})
	alike("the team's tickets", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.ListTeamTickets(ctx, e.SlugA, &apigen.ListTeamTicketsParams{})
	})
	alike("a ticket", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.GetTicket(ctx, e.SlugA, "ALPHA", seen.Number)
	})
	alike("the team's search", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.SearchTeam(ctx, e.SlugA, &apigen.SearchTeamParams{Q: "Seen"})
	})
	alike("the time report as CSV", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.TimeReport(ctx, e.SlugA, &apigen.TimeReportParams{GroupBy: ptr(apigen.TimeReportParamsGroupByProject)}, accept("text/csv"))
	})
	alike("the dashboard", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.GetDashboard(ctx, e.SlugA, &apigen.GetDashboardParams{})
	})
	alike("the team's export", admin, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.ExportTeam(ctx, e.SlugA)
	})
	alike("a ticket that is not there", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.GetTicket(ctx, e.SlugA, "ALPHA", 999)
	})
	alike("another team", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.GetTeam(ctx, e.SlugB)
	})
	alike("a filing the document refuses", member, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.CreateTicket(ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("Refused", func(b *apigen.TicketCreate) {
			b.Severity = "grave"
		}))
	})
	alike("a change without If-Match", admin, func(cl apigen.ClientInterface) (*http.Response, error) {
		return cl.UpdateTeam(ctx, e.SlugA, &apigen.UpdateTeamParams{}, apigen.TeamPatch{Name: ptr("Unchanged")})
	})

	// A filing answers alike but for what makes the ticket another one; its
	// Location names the team family either way.
	filed := func(cl apigen.ClientInterface) (map[string]any, string) {
		res, err := cl.CreateTicket(ctx, e.SlugA, "ALPHA", &apigen.CreateTicketParams{}, task("Filed on both paths"))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, res.StatusCode)
		body := readJSON(t, res)
		location := res.Header.Get("Location")
		assert.Equal(t, ticketPath(e.SlugA, "ALPHA", int(body["number"].(float64))), location)
		for _, key := range []string{"id", "key", "number", "created_at", "opened_at", "updated_at"} {
			delete(body, key)
		}
		return body, location
	}
	onTeam, _ := filed(e.s.client(t, member))
	onTwin, location := filed(e.s.twinClient(t, member))
	assert.Equal(t, onTeam, onTwin, "a filing answers alike on its twin")
	assert.True(t, strings.HasPrefix(location, "/api/v1/teams/"), location)

	// What a write through a twin did, the team path reads.
	current, err := e.s.client(t, admin).GetTeamWithResponse(ctx, e.SlugA)
	require.NoError(t, err)
	renamed, err := e.s.twinClient(t, admin).UpdateTeamWithResponse(ctx, e.SlugA,
		&apigen.UpdateTeamParams{IfMatch: ptr(current.HTTPResponse.Header.Get("ETag"))}, apigen.TeamPatch{Name: ptr("Team A, renamed")})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, renamed.StatusCode(), string(renamed.Body))
	after, err := e.s.client(t, member).GetTeamWithResponse(ctx, e.SlugA)
	require.NoError(t, err)
	assert.Equal(t, "Team A, renamed", after.JSON200.Name)
	assert.Equal(t, renamed.HTTPResponse.Header.Get("ETag"), after.HTTPResponse.Header.Get("ETag"))
}

// docs/adr/0005 D1, docs/adr/0046 D7: a person-level list is narrowed to a
// team by ?team= or by ?tenant=, its name before, alike — on each of the six
// operations that take it —; both together are refused at query:tenant,
// whatever their values; and the narrowing by either name answers a team the
// person does not belong to like no team.
func TestAPersonLevelListTakesTheTeamByEitherName(t *testing.T) {
	e := newTicketEnv(t)
	both := caller{Token: e.tk.Both}
	inA := e.file(t, caller{Token: e.tk.MemberA}, "ALPHA", task("Narrowed by either name", func(b *apigen.TicketCreate) {
		b.Assignee = &e.Both
	}))
	e.fileIn(t, caller{Token: e.tk.MemberB}, e.SlugB, "BETA", task("Narrowed by either name in B", func(b *apigen.TicketCreate) {
		b.Assignee = &e.Both
	}))
	asked := e.ask(t, caller{Token: e.tk.MemberA}, inA, apigen.QuestionCreate{Question: "Which name?", AskedOf: &e.Both})
	require.Equal(t, http.StatusCreated, asked.StatusCode(), string(asked.Body))

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/me/inbox", nil},
		{http.MethodGet, "/api/v1/me/next", nil},
		{http.MethodGet, "/api/v1/me/assigned", nil},
		{http.MethodGet, "/api/v1/me/decisions", nil},
		{http.MethodGet, "/api/v1/me/search?q=Narrowed", nil},
		{http.MethodPut, "/api/v1/me/inbox/read", map[string]any{"through": uuid.Must(uuid.NewV7())}},
	} {
		sep := "?"
		if strings.Contains(c.path, "?") {
			sep = "&"
		}
		byTeam := e.s.do(t, both, c.method, c.path+sep+"team="+e.SlugA, c.body)
		byTenant := e.s.do(t, both, c.method, c.path+sep+"tenant="+e.SlugA, c.body)
		require.Equal(t, http.StatusOK, byTeam.StatusCode, c.path)
		require.Equal(t, http.StatusOK, byTenant.StatusCode, c.path)
		teamBody, tenantBody := answerOf(t, byTeam), answerOf(t, byTenant)
		assert.Equal(t, teamBody, tenantBody, "%s narrows alike by either name", c.path)
		raw, err := json.Marshal(teamBody.Body)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), e.SlugB, "%s is narrowed to the team", c.path)
		for _, q := range []string{"team=" + e.SlugA + "&tenant=" + e.SlugA, "team=" + e.SlugA + "&tenant=" + e.SlugB} {
			assert.Equal(t, "query:tenant", fieldOf(t, e.s.do(t, both, c.method, c.path+sep+q, c.body)), "%s%s%s", c.path, sep, q)
		}
	}
	for _, name := range []string{"team", "tenant"} {
		assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, "/api/v1/me/assigned?"+name+"="+e.SlugB, nil),
			http.StatusNotFound, "not_found")
	}
	assert.Equal(t, "query:project", fieldOf(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/next?project=ALPHA", nil)),
		"a project needs its team, by either name")
	assert.Equal(t, http.StatusOK, e.s.do(t, both, http.MethodGet, "/api/v1/me/next?tenant="+e.SlugA+"&project=ALPHA", nil).StatusCode)
}

// docs/adr/0005 D1, docs/adr/0046 D7: a token is restricted to a team by team
// or by tenant, its name before, alike; both together must name the same slug,
// else 400 at /team; a team the person cannot reach is "no such team" at the
// property that named it; and the answers name the restriction by both names,
// the old one the same value.
func TestATokenIsRestrictedToATeamByEitherName(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["both"], testPassword)
	create := func(body map[string]any) *http.Response {
		body["name"] = uniqueSlug("token")
		return b.request(http.MethodPost, "/api/v1/me/tokens", body)
	}
	for name, body := range map[string]map[string]any{
		"team":                    {"scope": "read", "team": w.SlugA, "project": "ALPHA"},
		"tenant, the name before": {"scope": "read", "tenant": w.SlugA, "project": "ALPHA"},
		"both, the same slug":     {"scope": "read", "team": w.SlugA, "tenant": w.SlugA, "project": "ALPHA"},
	} {
		res := create(body)
		require.Equal(t, http.StatusCreated, res.StatusCode, name)
		created := readJSON(t, res)
		assert.Equal(t, w.SlugA, created["restricted_team"], name)
		assert.Equal(t, w.SlugA, created["restricted_tenant"], "%s: the name before repeats it", name)
		assert.Equal(t, "ALPHA", created["restricted_project"], name)
		token := caller{Token: created["token"].(string)}
		assert.Equal(t, http.StatusOK, s.do(t, token, http.MethodGet, "/api/v1/teams/"+w.SlugA+"/projects/ALPHA", nil).StatusCode, name)
		assertProblem(t, s.do(t, token, http.MethodGet, "/api/v1/teams/"+w.SlugB, nil), http.StatusNotFound, "not_found")
		own := readJSON(t, s.do(t, token, http.MethodGet, "/api/v1/me/token", nil))
		assert.Equal(t, w.SlugA, own["restricted_team"], name)
		assert.Equal(t, w.SlugA, own["restricted_tenant"], name)
	}
	assert.Equal(t, "/team", fieldOf(t, create(map[string]any{"scope": "read", "team": w.SlugA, "tenant": w.SlugB})),
		"two different slugs")
	assert.Equal(t, "/tenant", fieldOf(t, create(map[string]any{"scope": "read", "tenant": uniqueSlug("nobody")})),
		"no such team, at the name that named it")
	assert.Equal(t, "/project", fieldOf(t, create(map[string]any{"scope": "read", "project": "ALPHA"})),
		"a project needs its team")
	unrestricted := readJSON(t, create(map[string]any{"scope": "read"}))
	assert.Nil(t, unrestricted["restricted_team"])
	assert.Nil(t, unrestricted["restricted_tenant"])

	list := readJSON(t, b.get("/api/v1/me/tokens"))
	items := list["items"].([]any)
	require.Len(t, items, 4)
	for _, it := range items {
		tok := it.(map[string]any)
		assert.Equal(t, tok["restricted_team"], tok["restricted_tenant"], "a listed token names its team by both names")
	}
}

// docs/adr/0005 D1, docs/adr/0046 D7: every answer that names a team names it
// by team and by tenant, its name before, the same value: the memberships, the
// person-level lists and the search, the tokens of the team and a token's own
// answer, the repository lookup's bindings and proposal, and the export's
// manifest.
func TestEveryAnswerNamesTheTeamByBothNames(t *testing.T) {
	e := newTicketEnv(t)
	member, both, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.Both}, caller{Token: e.tk.AdminA}
	named := e.file(t, member, "ALPHA", task("Named by both names", func(b *apigen.TicketCreate) { b.Assignee = &e.Both }))
	asked := e.ask(t, member, named, apigen.QuestionCreate{Question: "Both names?", AskedOf: &e.Both})
	require.Equal(t, http.StatusCreated, asked.StatusCode(), string(asked.Body))

	sameTeam := func(what string, item any) {
		t.Helper()
		m, ok := item.(map[string]any)
		require.True(t, ok, "%s: %v", what, item)
		team, ok := m["team"].(map[string]any)
		require.True(t, ok, "%s names its team: %v", what, m)
		assert.NotEmpty(t, team["slug"], what)
		assert.NotEmpty(t, team["name"], what)
		assert.Equal(t, m["team"], m["tenant"], "%s: tenant repeats team", what)
	}
	me := readJSON(t, e.s.do(t, both, http.MethodGet, "/api/v1/me", nil))
	memberships := me["memberships"].([]any)
	require.Len(t, memberships, 2)
	for _, m := range memberships {
		sameTeam("a membership", m)
	}
	for _, path := range []string{"/api/v1/me/inbox", "/api/v1/me/assigned", "/api/v1/me/next", "/api/v1/me/decisions", "/api/v1/me/search?q=Named"} {
		items := readJSON(t, e.s.do(t, both, http.MethodGet, path, nil))["items"].([]any)
		require.NotEmpty(t, items, path)
		for _, it := range items {
			sameTeam(path, it)
		}
	}

	restricted, _, err := fixtures(t).Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, TenantID: e.A})
	require.NoError(t, err)
	own := readJSON(t, e.s.do(t, caller{Token: restricted}, http.MethodGet, "/api/v1/me/token", nil))
	assert.Equal(t, e.SlugA, own["restricted_team"])
	assert.Equal(t, e.SlugA, own["restricted_tenant"])
	tokens := readJSON(t, e.s.do(t, admin, http.MethodGet, "/api/v1/teams/"+e.SlugA+"/tokens", nil))["items"].([]any)
	seen := map[any]bool{}
	for _, it := range tokens {
		tok := it.(map[string]any)
		require.Contains(t, tok, "restricted_team")
		assert.Equal(t, tok["restricted_team"], tok["restricted_tenant"], "a member's token names its team by both names")
		seen[tok["restricted_team"]] = true
	}
	assert.True(t, seen[e.SlugA], "the restricted token is among them")
	assert.True(t, seen[nil], "and an unrestricted one, whose team is null under both names")

	const remote = "git@github.com:acme/named.git"
	bound, err := e.s.client(t, member).BindRepositoryWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.BindRepositoryParams{IdempotencyKey: newKey()},
		apigen.RepositoryBind{Remote: remote})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, bound.StatusCode(), string(bound.Body))
	lookup := readJSON(t, e.s.do(t, member, http.MethodGet, "/api/v1/me/repositories/lookup?remote="+url.QueryEscape(remote), nil))
	bindings := lookup["bindings"].([]any)
	require.Len(t, bindings, 1)
	sameTeam("a binding", bindings[0])
	unbound := readJSON(t, e.s.do(t, member, http.MethodGet, "/api/v1/me/repositories/lookup?remote="+url.QueryEscape("git@github.com:acme/unbound.git"), nil))
	proposal, ok := unbound["proposal"].(map[string]any)
	require.True(t, ok, "%v", unbound)
	assert.Equal(t, e.SlugA, proposal["team"])
	assert.Equal(t, proposal["team"], proposal["tenant"], "the proposal's tenant repeats its team")
	assert.Len(t, proposal["teams"], 1)
	assert.Equal(t, proposal["teams"], proposal["tenants"], "the proposal's tenants repeat its teams")
	assert.Equal(t, "only-tenant", proposal["reason"], "the reason keeps the name before (docs/adr/0005 D1)")

	_, _, files := e.exportOf(t, member, "ALPHA")
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(files["manifest.json"], &manifest))
	assert.Equal(t, e.SlugA, manifest["team"])
	assert.Equal(t, e.SlugA, manifest["tenant"], "written for the importers of the release before")
}

// docs/adr/0005 D1, docs/adr/0046 D7, docs/adr/0017 D10: group_by=team sums
// the team in one row keyed team, under the CSV's column team; group_by=tenant,
// its name before, does as it did: keyed tenant, under the column tenant.
func TestTheTimeReportSumsTheTeamByTheNameItIsAskedBy(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	booked := e.file(t, member, "ALPHA", task("Booked twice"))
	require.Equal(t, http.StatusCreated, e.book(t, member, booked, 30, "2026-10-01").StatusCode())
	require.Equal(t, http.StatusCreated, e.book(t, member, booked, 45, "2026-10-02").StatusCode())
	for _, name := range []string{"team", "tenant"} {
		path := "/api/v1/teams/" + e.SlugA + "/time-report?group_by=" + name
		report := decode[apigen.TimeReport](t, e.s.do(t, member, http.MethodGet, path, nil))
		assert.Equal(t, apigen.TimeReportGroupBy(name), report.GroupBy)
		require.Len(t, report.Items, 1, name)
		assert.Equal(t, name, report.Items[0].Key)
		assert.Equal(t, "", report.Items[0].Label)
		assert.Equal(t, 75, report.Items[0].Minutes)
		assert.Equal(t, 75, report.TotalMinutes)
		res := e.s.do(t, member, http.MethodGet, path, nil, "Accept", "text/csv")
		require.Equal(t, http.StatusOK, res.StatusCode)
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		assert.Equal(t, name+",label,minutes\n"+name+",,75\n", string(body), name)
	}
}

// docs/adr/0051 D4, docs/adr/0005 D1: an archive written before a tenant was
// called a team names the team in its manifest by tenant alone; it is
// imported as it always was — the manifest read and reported, every ticket
// created, the links inside it made.
func TestAnArchiveWrittenBeforeTheTeamIsImported(t *testing.T) {
	e := newTicketEnv(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}
	_, err := fixtures(t).Project(e.ctx, e.A, "COPY", "Copy")
	require.NoError(t, err)
	first := e.file(t, member, "ALPHA", task("Written before"))
	second := e.file(t, member, "ALPHA", task("Linked before"))
	require.Equal(t, http.StatusCreated, e.link(t, admin, first, apigen.LinkTypeRelatesTo, second).StatusCode)

	_, _, files := e.exportOf(t, admin, "ALPHA")
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(files["manifest.json"], &manifest))
	require.Equal(t, e.SlugA, manifest["team"])
	delete(manifest, "team")
	files["manifest.json"] = mustJSON(t, manifest)
	parts := make([]namedFile, 0, len(files))
	for name, body := range files {
		parts = append(parts, namedFile{name: name, body: body})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })

	created := e.dryRun(t, admin, "COPY", namedFile{name: "before.tar.gz", body: tarGzOf(t, parts)})
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	job := created.JSON201
	assert.Equal(t, "the manifest of an export of "+e.SlugA+"/ALPHA (docs/adr/0051 D4)", reported(t, job, "manifest.json").Reason.MustGet())
	assert.Equal(t, 2, job.Summary.Create)
	assert.Zero(t, job.Summary.Error)
	executed := e.execute(t, admin, "COPY", job.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))

	_, _, after := e.exportOf(t, admin, "COPY")
	assert.Equal(t, 2, manifestOf(t, after).Tickets)
	var links []apigen.ExportLink
	require.NoError(t, json.Unmarshal(after["links.json"], &links))
	assert.Len(t, links, 1, "the link inside the archive is made")
}
