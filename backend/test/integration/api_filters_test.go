//go:build integration

package integration

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

func filtersPath(slug string, id ...uuid.UUID) string {
	p := "/api/v1/tenants/" + slug + "/filters"
	if len(id) > 0 {
		p += "/" + id[0].String()
	}
	return p
}

// saveFilter creates a filter as c in the tenant and requires the 201.
func (e ticketEnv) saveFilter(t *testing.T, c caller, slug string, body apigen.SavedFilterCreate) apigen.SavedFilter {
	t.Helper()
	res := e.s.do(t, c, http.MethodPost, filtersPath(slug), body)
	require.Equal(t, http.StatusCreated, res.StatusCode, "%v", res.Status)
	f := decode[apigen.SavedFilter](t, res)
	assert.Equal(t, strconv.Quote(strconv.Itoa(f.Version)), res.Header.Get("ETag"))
	assert.Equal(t, filtersPath(slug, f.Id), res.Header.Get("Location"))
	return f
}

// savedFilters lists the filters c sees in the tenant, by name.
func (e ticketEnv) savedFilters(t *testing.T, c caller, slug string) map[string]apigen.SavedFilter {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, filtersPath(slug), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	out := map[string]apigen.SavedFilter{}
	for _, f := range decode[apigen.SavedFilterList](t, res).Items {
		out[f.Name] = f
	}
	return out
}

func filterNames(m map[string]apigen.SavedFilter) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	return out
}

func strs(v ...string) *[]string { return &v }

// docs/adr/0018 D5: a saved filter is a person's own, optionally shared with
// the tenant, which shows its owner; only its owner changes it, with
// If-Match, and every change is a recorded act; nothing crosses a tenant.
func TestSavedFiltersAreAPersonsAndOptionallyTheTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, viewer, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.ViewerA}, caller{Token: e.tk.Both}

	mine := e.saveFilter(t, member, e.SlugA, apigen.SavedFilterCreate{Name: " mine ",
		Parameters: apigen.SavedFilterParameters{State: strs("filed", "!blocked"), Assignee: strs("me"), IncludeTerminal: ptr(true)}})
	assert.Equal(t, "mine", mine.Name, "the name is trimmed")
	assert.Equal(t, e.MemberA, mine.Owner.Id)
	assert.False(t, mine.Shared)
	assert.Equal(t, []string{"me"}, *mine.Parameters.Assignee, "me stays me: whoever applies the filter")
	assert.Empty(t, mine.Warnings)
	team := e.saveFilter(t, member, e.SlugA, apigen.SavedFilterCreate{Name: "team", Shared: ptr(true),
		Parameters: apigen.SavedFilterParameters{Severity: strs("high", "critical")}})
	own := e.saveFilter(t, viewer, e.SlugA, apigen.SavedFilterCreate{Name: "the viewer's", Parameters: apigen.SavedFilterParameters{}})
	assert.Equal(t, e.ViewerA, own.Owner.Id, "any role saves its own")

	assert.ElementsMatch(t, []string{"mine", "team"}, filterNames(e.savedFilters(t, member, e.SlugA)))
	seen := e.savedFilters(t, viewer, e.SlugA)
	assert.ElementsMatch(t, []string{"team", "the viewer's"}, filterNames(seen), "a private filter is its owner's alone")
	assert.Equal(t, e.MemberA, seen["team"].Owner.Id, "a shared filter shows its owner")
	assert.ElementsMatch(t, []string{"team"}, filterNames(e.savedFilters(t, both, e.SlugA)))
	assertProblem(t, e.s.do(t, viewer, http.MethodGet, filtersPath(e.SlugA, mine.Id), nil), http.StatusNotFound, "not_found")
	assert.Equal(t, http.StatusOK, e.s.do(t, viewer, http.MethodGet, filtersPath(e.SlugA, team.Id), nil).StatusCode)

	// Another tenant reaches nothing of them, and its own filters stay its own.
	assertProblem(t, e.s.do(t, caller{Token: e.tk.MemberB}, http.MethodGet, filtersPath(e.SlugA), nil), http.StatusNotFound, "not_found")
	inB := e.saveFilter(t, both, e.SlugB, apigen.SavedFilterCreate{Name: "in B", Shared: ptr(true), Parameters: apigen.SavedFilterParameters{}})
	assert.NotContains(t, filterNames(e.savedFilters(t, both, e.SlugA)), "in B")
	assertProblem(t, e.s.do(t, both, http.MethodGet, filtersPath(e.SlugA, inB.Id), nil), http.StatusNotFound, "not_found")
	assert.ElementsMatch(t, []string{"in B"}, filterNames(e.savedFilters(t, caller{Token: e.tk.MemberB}, e.SlugB)))

	readToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Scope: domain.ScopeRead})
	require.NoError(t, err)
	assertProblem(t, e.s.do(t, caller{Token: readToken}, http.MethodPost, filtersPath(e.SlugA),
		apigen.SavedFilterCreate{Name: "x", Parameters: apigen.SavedFilterParameters{}}), http.StatusForbidden, "insufficient_scope")
	assert.Equal(t, http.StatusOK, e.s.do(t, caller{Token: readToken}, http.MethodGet, filtersPath(e.SlugA), nil).StatusCode)

	// Only the owner changes and deletes; another's shared filter is 403, a
	// private one 404.
	etag := func(f apigen.SavedFilter) string { return strconv.Quote(strconv.Itoa(f.Version)) }
	patch := apigen.SavedFilterPatch{Name: ptr("renamed")}
	assertProblem(t, e.s.do(t, viewer, http.MethodPatch, filtersPath(e.SlugA, team.Id), patch, "If-Match", etag(team)),
		http.StatusForbidden, "forbidden")
	assertProblem(t, e.s.do(t, viewer, http.MethodDelete, filtersPath(e.SlugA, team.Id), nil), http.StatusForbidden, "forbidden")
	assertProblem(t, e.s.do(t, viewer, http.MethodPatch, filtersPath(e.SlugA, mine.Id), patch, "If-Match", etag(mine)),
		http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, member, http.MethodPatch, filtersPath(e.SlugA, team.Id), patch), http.StatusPreconditionRequired,
		"precondition_required")
	stale := assertProblem(t, e.s.do(t, member, http.MethodPatch, filtersPath(e.SlugA, team.Id), patch, "If-Match", `"7"`),
		http.StatusPreconditionFailed, "precondition_failed")
	assert.Equal(t, "team", stale["errors"].([]any)[0].(map[string]any)["current"])

	res := e.s.do(t, member, http.MethodPatch, filtersPath(e.SlugA, team.Id),
		apigen.SavedFilterPatch{Name: ptr("renamed"), Shared: ptr(false)}, "If-Match", etag(team))
	require.Equal(t, http.StatusOK, res.StatusCode)
	renamed := decode[apigen.SavedFilter](t, res)
	assert.Equal(t, "renamed", renamed.Name)
	assert.False(t, renamed.Shared)
	assert.Equal(t, team.Version+1, renamed.Version)
	assert.NotContains(t, filterNames(e.savedFilters(t, viewer, e.SlugA)), "renamed", "an unshared filter leaves the others' list")
	unchanged := e.s.do(t, member, http.MethodPatch, filtersPath(e.SlugA, team.Id), apigen.SavedFilterPatch{Shared: ptr(false)},
		"If-Match", etag(renamed))
	require.Equal(t, http.StatusOK, unchanged.StatusCode)
	assert.Equal(t, renamed.Version, decode[apigen.SavedFilter](t, unchanged).Version, "a patch that changes nothing raises nothing")

	e.send(t, member, http.StatusNoContent, http.MethodDelete, filtersPath(e.SlugA, team.Id), nil)
	assertProblem(t, e.s.do(t, member, http.MethodGet, filtersPath(e.SlugA, team.Id), nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, member, http.MethodDelete, filtersPath(e.SlugA, team.Id), nil), http.StatusNotFound, "not_found")
	for action, n := range map[string]int{"created": 1, "updated": 1, "deleted": 1} {
		assert.Equal(t, n, scalar[int](t, `SELECT count(*) FROM audit_events WHERE entity_type = 'saved_filter'
			AND entity_id = $1 AND action = $2 AND actor_user_id = $3`, team.Id, action, e.MemberA), action)
	}
	assert.JSONEq(t, `{"name": "renamed", "shared": false}`, scalar[string](t, `SELECT after::text FROM audit_events
		WHERE entity_id = $1 AND action = 'updated'`, team.Id))
}

// docs/adr/0049 D4, D6, D7: a saved filter takes the lists' parameters and
// refuses what they refuse; read again it warns of a value that no longer
// holds; another person's filter that names what the reader cannot see is
// shown without its parameters (docs/adr/0065 D5).
func TestSavedFilterParametersAreTheListsParameters(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}

	bad := assertProblem(t, e.s.do(t, member, http.MethodPost, filtersPath(e.SlugA), apigen.SavedFilterCreate{Name: "bad",
		Parameters: apigen.SavedFilterParameters{State: strs("nonsense"), Assignee: strs("somebody")}}),
		http.StatusBadRequest, "validation_failed")
	errs := bad["errors"].([]any)
	pointers := make([]string, 0, len(errs))
	for _, fe := range errs {
		pointers = append(pointers, fe.(map[string]any)["pointer"].(string))
	}
	assert.ElementsMatch(t, []string{"/parameters/state", "/parameters/assignee"}, pointers)
	assertProblem(t, e.s.do(t, member, http.MethodPost, filtersPath(e.SlugA),
		`{"name": "typo", "parameters": {"stat": ["filed"]}}`), http.StatusBadRequest, "validation_failed")
	assertProblem(t, e.s.do(t, member, http.MethodPost, filtersPath(e.SlugA),
		apigen.SavedFilterCreate{Name: "  ", Parameters: apigen.SavedFilterParameters{}}), http.StatusBadRequest, "validation_failed")

	// A vocabulary that changed under a stored filter is a warning, not an
	// empty list.
	kept := e.saveFilter(t, member, e.SlugA, apigen.SavedFilterCreate{Name: "kept", Parameters: apigen.SavedFilterParameters{State: strs("filed")}})
	require.NoError(t, f.Exec(e.ctx, `UPDATE saved_filters SET parameters = '{"state": ["filed", "triaged"]}' WHERE id = $1`, kept.Id))
	warned := e.savedFilters(t, member, e.SlugA)["kept"]
	require.Len(t, warned.Warnings, 1)
	assert.Equal(t, "state", warned.Warnings[0].Parameter)
	assert.Contains(t, warned.Warnings[0].Message, "triaged")
	renamed := e.s.do(t, member, http.MethodPatch, filtersPath(e.SlugA, kept.Id), apigen.SavedFilterPatch{Name: ptr("kept, renamed")},
		"If-Match", strconv.Quote(strconv.Itoa(warned.Version)))
	require.Equal(t, http.StatusOK, renamed.StatusCode, "a rename leaves the parameters as they are, an old value included")
	assert.Len(t, decode[apigen.SavedFilter](t, renamed).Warnings, 1)

	// A restricted project, a confidential ticket and a deleted one: the
	// owner reads the filter whole, the others without its parameters.
	secret, err := f.Project(e.ctx, e.A, "SECRET", "Secret")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", secret))
	hidden := e.file(t, admin, "ALPHA", task("hidden"))
	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET confidential = true WHERE id = $1", hidden.Id))
	gone := e.file(t, admin, "ALPHA", task("gone"))
	open := e.file(t, admin, "ALPHA", task("open"))
	for name, params := range map[string]apigen.SavedFilterParameters{
		"restricted":   {Project: strs("!SECRET")},
		"confidential": {Parent: strs(short(hidden))},
		"deleted":      {Parent: strs(short(gone))},
		"visible":      {Parent: strs(short(open)), Project: strs("ALPHA")},
	} {
		e.saveFilter(t, admin, e.SlugA, apigen.SavedFilterCreate{Name: name, Shared: ptr(true), Parameters: params})
	}
	e.send(t, admin, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", gone.Number), nil)

	others := e.savedFilters(t, member, e.SlugA)
	for _, name := range []string{"restricted", "confidential", "deleted"} {
		assert.True(t, others[name].Redacted, name)
		assert.Equal(t, apigen.SavedFilterParameters{}, others[name].Parameters, name)
		assert.Empty(t, others[name].Warnings, name)
		assert.Equal(t, e.AdminA, others[name].Owner.Id, "the name and the owner stay")
	}
	assert.False(t, others["visible"].Redacted)
	assert.Equal(t, []string{short(open)}, *others["visible"].Parameters.Parent)

	owners := e.savedFilters(t, admin, e.SlugA)
	for _, name := range []string{"restricted", "confidential", "visible"} {
		assert.False(t, owners[name].Redacted, name)
		assert.Empty(t, owners[name].Warnings, "the owner sees what their filter names: %s", name)
	}
	assert.False(t, owners["deleted"].Redacted)
	require.Len(t, owners["deleted"].Warnings, 1, "the owner is told their filter names a ticket that is gone")
	assert.Equal(t, "parent", owners["deleted"].Warnings[0].Parameter)
}

// docs/adr/0045 D3, D4: saving a filter takes an Idempotency-Key — an agent's
// must — and a repetition replays the first answer.
func TestSavingAFilterIsIdempotent(t *testing.T) {
	e := newTicketEnv(t)
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	body := apigen.SavedFilterCreate{Name: "by the agent", Parameters: apigen.SavedFilterParameters{Urgency: strs("now")}}
	assertProblem(t, e.s.do(t, agent, http.MethodPost, filtersPath(e.SlugA), body), http.StatusBadRequest, "idempotency_key_required")
	key := uuid.Must(uuid.NewV7()).String()
	first := e.s.do(t, agent, http.MethodPost, filtersPath(e.SlugA), body, "Idempotency-Key", key)
	require.Equal(t, http.StatusCreated, first.StatusCode)
	second := e.s.do(t, agent, http.MethodPost, filtersPath(e.SlugA), body, "Idempotency-Key", key)
	require.Equal(t, http.StatusCreated, second.StatusCode)
	assert.Equal(t, decode[apigen.SavedFilter](t, first).Id, decode[apigen.SavedFilter](t, second).Id)
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM saved_filters WHERE tenant_id = $1 AND name = 'by the agent'`, e.A))
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_events WHERE entity_type = 'saved_filter' AND action = 'created'
		AND agent = 'claude-code/opus/s1' AND tenant_id = $1`, e.A), "an agent's filter is marked as its act")
	assertProblem(t, e.s.do(t, agent, http.MethodPost, filtersPath(e.SlugA), apigen.SavedFilterCreate{Name: "other",
		Parameters: apigen.SavedFilterParameters{}}, "Idempotency-Key", key), http.StatusUnprocessableEntity, "idempotency_mismatch")
}

// docs/adr/0021 D6: inside the tenant's own transaction the policies of
// migration 33 hold a person to their own filters and the shared ones — a
// query that forgot its owner reads, changes and deletes nobody else's.
func TestTheSavedFilterPoliciesHoldAPersonToTheirOwn(t *testing.T) {
	e := newTicketEnv(t)
	e.saveFilter(t, caller{Token: e.tk.MemberA}, e.SlugA, apigen.SavedFilterCreate{Name: "private", Parameters: apigen.SavedFilterParameters{}})
	e.saveFilter(t, caller{Token: e.tk.MemberA}, e.SlugA, apigen.SavedFilterCreate{Name: "shared", Shared: ptr(true),
		Parameters: apigen.SavedFilterParameters{}})
	asViewer := func(sql string) (int64, error) {
		conn, err := pgx.Connect(e.ctx, env.RuntimeURL)
		require.NoError(t, err)
		defer func() { _ = conn.Close(e.ctx) }()
		tx, err := conn.Begin(e.ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(e.ctx) }()
		_, err = tx.Exec(e.ctx, "SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)",
			e.A.String(), e.ViewerA.String())
		require.NoError(t, err)
		tag, err := tx.Exec(e.ctx, sql)
		return tag.RowsAffected(), err
	}
	n, err := asViewer(`SELECT name FROM saved_filters`)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "the shared one, not the private one")
	n, err = asViewer(`UPDATE saved_filters SET name = 'taken over'`)
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = asViewer(`DELETE FROM saved_filters`)
	require.NoError(t, err)
	assert.Zero(t, n)
	_, err = asViewer(`INSERT INTO saved_filters (tenant_id, owner_id, name) VALUES ('` + e.A.String() + `', '` + e.MemberA.String() + `', 'forged')`)
	assert.ErrorContains(t, err, "row-level security", "nobody files a filter in another person's name")
}
