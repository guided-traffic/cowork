//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// docs/adr/0034 D9: who creates projects is a tenant setting, members by
// default; a write act either way; an agent needs create-project.
func TestCreatingProjects(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	create := func(c caller, key string, idem bool) *apigen.CreateProjectResponse {
		params := &apigen.CreateProjectParams{}
		if idem {
			params.IdempotencyKey = newKey()
		}
		res, err := s.client(t, c).CreateProjectWithResponse(ctx, w.SlugA, params, apigen.ProjectCreate{Key: key, Name: key})
		require.NoError(t, err)
		return res
	}

	res := create(caller{Token: tk.MemberA}, "MEM", false)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	assert.Equal(t, "/api/v1/teams/"+w.SlugA+"/projects/MEM", *res.Headers201.Location)
	assert.Equal(t, `"1"`, *res.Headers201.ETag)

	res = create(caller{Token: tk.MemberA}, "MEM", false)
	assert.Equal(t, http.StatusConflict, res.StatusCode())
	assert.Equal(t, "project_key_taken", string(res.ApplicationproblemJSONDefault.Code))

	res = create(caller{Token: tk.ViewerA}, "VIEW", false)
	assert.Equal(t, http.StatusForbidden, res.StatusCode(), "a viewer creates nothing (docs/adr/0034 D8)")

	res = create(caller{Token: tk.AgentA}, "NOKEY", false)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode())
	assert.Equal(t, "idempotency_key_required", string(res.ApplicationproblemJSONDefault.Code), "an agent's POST needs a key (docs/adr/0045 D3)")

	res = create(caller{Token: tk.AssistedAgentA}, "ASSIST", true)
	assert.Equal(t, http.StatusForbidden, res.StatusCode())
	assert.Equal(t, "missing capability: create-project", *res.ApplicationproblemJSONDefault.Detail)

	res = create(caller{Token: tk.AgentA, Agent: "claude-code/opus/s1"}, "AGENT", true)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body), "an agent's write token creates a project (docs/adr/0066 D5)")

	// The tenant reserves project creation to its administrators.
	f := fixtures(t)
	require.NoError(t, f.Exec(ctx, "UPDATE tenants SET members_create_projects = false WHERE id = $1", w.A))
	res = create(caller{Token: tk.MemberA}, "LATE", false)
	assert.Equal(t, http.StatusForbidden, res.StatusCode())
	res = create(caller{Token: tk.AdminAWrite}, "ADMW", false)
	assert.Equal(t, http.StatusCreated, res.StatusCode(), "an administrator's write token creates projects")
}

// docs/adr/0045 D4: a repeated keyed POST replays the response and commits
// one act; a different body under the same key is 422.
func TestCreatingAProjectIsIdempotent(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	cl := s.client(t, caller{Token: tk.AgentA})
	key := newKey()

	first, err := cl.CreateProjectWithResponse(ctx, w.SlugA, &apigen.CreateProjectParams{IdempotencyKey: key}, apigen.ProjectCreate{Key: "IDEM", Name: "Once"})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, first.StatusCode(), string(first.Body))
	again, err := cl.CreateProjectWithResponse(ctx, w.SlugA, &apigen.CreateProjectParams{IdempotencyKey: key}, apigen.ProjectCreate{Key: "IDEM", Name: "Once"})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, again.StatusCode(), string(again.Body))
	assert.Equal(t, first.JSON201.Id, again.JSON201.Id)
	assert.Equal(t, *first.Headers201.ETag, *again.Headers201.ETag)

	other, err := cl.CreateProjectWithResponse(ctx, w.SlugA, &apigen.CreateProjectParams{IdempotencyKey: key}, apigen.ProjectCreate{Key: "IDEM2", Name: "Other"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, other.StatusCode())

	n, err := fixtures(t).QueryCount(ctx, "SELECT count(*) FROM audit_events WHERE idempotency_key = $1", *key)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

// docs/adr/0006 D4: archiving stays idempotent when the archivings race —
// each answers 200, and the act is recorded once.
func TestSimultaneousArchivingsAnswerAlike(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	admin := s.client(t, caller{Token: tk.AdminA})

	codes := simultaneously(times(16, func() int {
		res, err := admin.ArchiveProjectWithResponse(ctx, w.SlugA, "ALPHA")
		if err != nil {
			return 0
		}
		return res.StatusCode()
	})...)
	for i, code := range codes {
		assert.Equal(t, http.StatusOK, code, "archiving %d", i)
	}
	n, err := fixtures(t).QueryCount(ctx, "SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'project' AND action = 'archived'", w.A)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestUpdatingAndArchivingProjects(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	admin := s.client(t, caller{Token: tk.AdminA})

	got, err := admin.GetProjectWithResponse(ctx, w.SlugA, "ALPHA")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode(), string(got.Body))
	etag := got.HTTPResponse.Header.Get("ETag")

	upd, err := admin.UpdateProjectWithResponse(ctx, w.SlugA, "ALPHA", &apigen.UpdateProjectParams{IfMatch: &etag},
		apigen.ProjectPatch{Description: ptr("The first project")})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, upd.StatusCode(), string(upd.Body))
	assert.Equal(t, "The first project", upd.JSON200.Description)

	stale, err := admin.UpdateProjectWithResponse(ctx, w.SlugA, "ALPHA", &apigen.UpdateProjectParams{IfMatch: &etag},
		apigen.ProjectPatch{Name: ptr("Renamed")})
	require.NoError(t, err)
	require.Equal(t, http.StatusPreconditionFailed, stale.StatusCode())
	current := (*stale.ApplicationproblemJSONDefault.Errors)[0]
	assert.Equal(t, "/name", current.Pointer)
	assert.Equal(t, "Alpha", current.Current.MustGet(), "the 412 names the current value (docs/adr/0050 D5)")

	assisted := s.client(t, caller{Token: tk.AssistedAgentA, Agent: "claude-code/opus/s1"})
	refused, err := assisted.CreateProjectWithResponse(ctx, w.SlugA, &apigen.CreateProjectParams{IdempotencyKey: newKey()}, apigen.ProjectCreate{Key: "NOPE", Name: "No"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, refused.StatusCode(), "creating a project needs create-project")
	shown, err := assisted.GetProjectWithResponse(ctx, w.SlugA, "ALPHA")
	require.NoError(t, err)
	etag = shown.HTTPResponse.Header.Get("ETag")
	edited, err := assisted.UpdateProjectWithResponse(ctx, w.SlugA, "ALPHA", &apigen.UpdateProjectParams{IfMatch: &etag},
		apigen.ProjectPatch{Description: ptr("Edited by an agent")})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, edited.StatusCode(), "an agent edits a project without create-project: the baseline by the owner's decision (docs/adr/0043 D2)")

	agentArchive := s.do(t, caller{Token: tk.AdminAWrite, Agent: "claude-code/opus/s1"}, http.MethodPut, "/api/v1/teams/"+w.SlugA+"/projects/ALPHA/archive", nil)
	assertProblem(t, agentArchive, http.StatusForbidden, "insufficient_scope")

	archived, err := admin.ArchiveProjectWithResponse(ctx, w.SlugA, "ALPHA")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, archived.StatusCode(), string(archived.Body))
	assert.True(t, archived.JSON200.ArchivedAt.IsSpecified() && !archived.JSON200.ArchivedAt.IsNull())
	again, err := admin.ArchiveProjectWithResponse(ctx, w.SlugA, "ALPHA")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, again.StatusCode(), "archiving an archived project changes nothing")

	list, err := admin.ListProjectsWithResponse(ctx, w.SlugA, &apigen.ListProjectsParams{})
	require.NoError(t, err)
	assert.Empty(t, list.JSON200.Items, "an archived project leaves the default list (docs/adr/0006 D4)")
	list, err = admin.ListProjectsWithResponse(ctx, w.SlugA, &apigen.ListProjectsParams{IncludeArchived: ptr(true)})
	require.NoError(t, err)
	assert.Len(t, list.JSON200.Items, 1)

	n, err := fixtures(t).QueryCount(ctx, "SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'project' AND action IN ('updated', 'archived')", w.A)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n, "updated by the administrator and by the agent, archived")
}

// docs/adr/0034 D3: a restricted project is hidden from members not on its
// list and shown to administrators; docs/adr/0035 D3: a token restricted to a
// project sees that project only and nothing of the tenant beyond it.
func TestProjectVisibility(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	f := fixtures(t)
	ctx := context.Background()
	secret, err := f.Project(ctx, w.A, "SECRET", "Secret")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, "UPDATE projects SET restricted = true WHERE id = $1", secret))
	require.NoError(t, f.Exec(ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'viewer')", w.A, secret, w.ViewerA))

	keys := func(c caller) []string {
		res, err := s.client(t, c).ListProjectsWithResponse(ctx, w.SlugA, &apigen.ListProjectsParams{})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		out := make([]string, 0, len(res.JSON200.Items))
		for _, p := range res.JSON200.Items {
			out = append(out, p.Key)
		}
		return out
	}
	assert.Equal(t, []string{"ALPHA"}, keys(caller{Token: tk.MemberA}), "off the list: hidden")
	assert.Equal(t, []string{"ALPHA", "SECRET"}, keys(caller{Token: tk.ViewerA}), "on the list: shown")
	assert.Equal(t, []string{"ALPHA", "SECRET"}, keys(caller{Token: tk.AdminA}), "administrators see every project")
	assertProblem(t, s.do(t, caller{Token: tk.MemberA}, http.MethodGet, "/api/v1/teams/"+w.SlugA+"/projects/SECRET", nil), http.StatusNotFound, "not_found")

	projectToken, _, err := f.Token(ctx, fixture.TokenSpec{UserID: w.AdminA, TenantID: w.A, ProjectID: secret, Scope: "admin"})
	require.NoError(t, err)
	assert.Equal(t, []string{"SECRET"}, keys(caller{Token: projectToken}))
	assertProblem(t, s.do(t, caller{Token: projectToken}, http.MethodGet, "/api/v1/teams/"+w.SlugA+"/projects/ALPHA", nil), http.StatusNotFound, "not_found")
	assertProblem(t, s.do(t, caller{Token: projectToken}, http.MethodGet, "/api/v1/teams/"+w.SlugA+"/members", nil), http.StatusNotFound, "not_found")
	assertProblem(t, s.do(t, caller{Token: projectToken}, http.MethodGet, "/api/v1/teams/"+w.SlugA, nil), http.StatusNotFound, "not_found")

	_, inside, err := f.Ticket(ctx, w.A, secret, w.AdminA, "Inside")
	require.NoError(t, err)
	_, outside, err := f.Ticket(ctx, w.A, w.ProjectA, w.AdminA, "Outside")
	require.NoError(t, err)
	resolver := s.client(t, caller{Token: projectToken})
	resolved, err := resolver.ResolveTicketWithResponse(ctx, w.SlugA, fmt.Sprintf("SECRET-%d", inside))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resolved.StatusCode(), "the resolver answers a key of the token's project")
	resolved, err = resolver.ResolveTicketWithResponse(ctx, w.SlugA, fmt.Sprintf("ALPHA-%d", outside))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resolved.StatusCode(), "and no key outside it")
}
