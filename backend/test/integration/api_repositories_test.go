//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

const agentHeader = "claude-code/opus/s1"

// repoEnv is the shape the bindings and the lookup are tested on: a world
// with its tokens and a running API.
type repoEnv struct {
	world
	tk  tokens
	s   apiServer
	ctx context.Context
}

func newRepoEnv(t *testing.T) repoEnv {
	t.Helper()
	w := newWorld(t)
	return repoEnv{world: w, tk: issueTokens(t, w), s: newAPI(t), ctx: context.Background()}
}

func (e repoEnv) bind(t *testing.T, c caller, tenant, project string, body apigen.RepositoryBind) *apigen.BindRepositoryResponse {
	t.Helper()
	res, err := e.s.client(t, c).BindRepositoryWithResponse(e.ctx, tenant, project, &apigen.BindRepositoryParams{IdempotencyKey: newKey()}, body)
	require.NoError(t, err)
	return res
}

func (e repoEnv) lookup(t *testing.T, c caller, path string, remotes ...string) apigen.RepositoryLookup {
	t.Helper()
	params := &apigen.LookupRepositoryParams{Remote: remotes}
	if path != "" {
		params.Path = &path
	}
	res, err := e.s.client(t, c).LookupRepositoryWithResponse(e.ctx, params)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	return *res.JSON200
}

// docs/adr/0066 D1, D5, D6: a binding by the normalised identity, idempotent
// over it, the remote kept as last given and without credentials; another
// project of the tenant is refused, named only to who sees it.
func TestBindingARepository(t *testing.T) {
	e := newRepoEnv(t)
	member := caller{Token: e.tk.MemberA}

	res := e.bind(t, member, e.SlugA, "ALPHA", apigen.RepositoryBind{Remote: "https://user:secret@github.com/acme/alpha.git"})
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	bound := *res.JSON201
	assert.Equal(t, "github.com/acme/alpha", bound.Identity)
	assert.Equal(t, "", bound.Path)
	assert.Equal(t, "https://github.com/acme/alpha.git", bound.Remote, "the credentials are never stored")
	assert.Equal(t, "/api/v1/tenants/"+e.SlugA+"/projects/ALPHA/repositories/"+bound.Id.String(), *res.Headers201.Location)

	res = e.bind(t, member, e.SlugA, "ALPHA", apigen.RepositoryBind{Remote: "https://github.com/acme/alpha.git"})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, bound.Id, res.JSON200.Id, "binding again changes nothing")

	res = e.bind(t, member, e.SlugA, "ALPHA", apigen.RepositoryBind{Remote: "git@github.com:acme/alpha.git"})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, "git@github.com:acme/alpha.git", res.JSON200.Remote, "the last original form is kept")

	acts, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_id = $1", bound.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, acts, "linked once, updated once for the new form")
	n, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_id = $1 AND after::text LIKE '%secret%'", bound.Id)
	require.NoError(t, err)
	assert.Zero(t, n, "no act carries the credentials")

	_, err = fixtures(t).Project(e.ctx, e.A, "OTHER", "Other")
	require.NoError(t, err)
	res = e.bind(t, member, e.SlugA, "OTHER", apigen.RepositoryBind{Remote: "ssh://git@github.com:22/acme/alpha/"})
	require.Equal(t, http.StatusConflict, res.StatusCode())
	assert.Equal(t, "repository_bound", string(res.ApplicationproblemJSONDefault.Code))
	assert.Contains(t, *res.ApplicationproblemJSONDefault.Detail, e.SlugA+"/ALPHA", "a project the caller sees is named")

	require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", e.ProjectA))
	res = e.bind(t, member, e.SlugA, "OTHER", apigen.RepositoryBind{Remote: "git@github.com:acme/alpha.git"})
	require.Equal(t, http.StatusConflict, res.StatusCode())
	assert.NotContains(t, *res.ApplicationproblemJSONDefault.Detail, "ALPHA", "a project the caller cannot see is not named")

	res = e.bind(t, member, e.SlugA, "OTHER", apigen.RepositoryBind{Remote: "git@github.com:acme/alpha.git", Path: ptr("services/a")})
	require.Equal(t, http.StatusCreated, res.StatusCode(), "a sub-directory of a monorepo is a binding of its own")
	assert.Equal(t, "services/a", res.JSON201.Path)

	res = e.bind(t, member, e.SlugA, "OTHER", apigen.RepositoryBind{Remote: "/srv/git/local.git"})
	require.Equal(t, http.StatusBadRequest, res.StatusCode())
	assert.Equal(t, "/remote", (*res.ApplicationproblemJSONDefault.Errors)[0].Pointer)
	res = e.bind(t, member, e.SlugA, "OTHER", apigen.RepositoryBind{Remote: "git@github.com:acme/x.git", Path: ptr("../up")})
	require.Equal(t, http.StatusBadRequest, res.StatusCode())
	assert.Equal(t, "/path", (*res.ApplicationproblemJSONDefault.Errors)[0].Pointer)
}

// docs/adr/0043 D4, docs/adr/0034 D9: binding is the act of creating a
// project — write scope, a member while the tenant allows it, an agent with
// create-project and an Idempotency-Key — and unbinding the same act.
func TestWhoBindsAndUnbindsRepositories(t *testing.T) {
	e := newRepoEnv(t)
	body := apigen.RepositoryBind{Remote: "git@github.com:acme/who.git"}

	res := e.bind(t, caller{Token: e.tk.ViewerA}, e.SlugA, "ALPHA", body)
	assert.Equal(t, http.StatusForbidden, res.StatusCode(), "a viewer binds nothing")
	res = e.bind(t, caller{Token: e.tk.AssistedAgentA, Agent: agentHeader}, e.SlugA, "ALPHA", body)
	require.Equal(t, http.StatusForbidden, res.StatusCode())
	assert.Equal(t, "missing capability: create-project", *res.ApplicationproblemJSONDefault.Detail)
	assertProblem(t, e.s.do(t, caller{Token: e.tk.AgentA}, http.MethodPost, "/api/v1/tenants/"+e.SlugA+"/projects/ALPHA/repositories",
		map[string]any{"remote": "git@github.com:acme/who.git"}), http.StatusBadRequest, "idempotency_key_required")
	res = e.bind(t, caller{Token: e.tk.MemberB}, e.SlugA, "ALPHA", body)
	assert.Equal(t, http.StatusNotFound, res.StatusCode(), "another tenant's token finds no tenant")

	res = e.bind(t, caller{Token: e.tk.AgentA, Agent: agentHeader}, e.SlugA, "ALPHA", body)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	id := res.JSON201.Id
	agent, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_id = $1 AND agent = $2 AND action = 'linked'", id, agentHeader)
	require.NoError(t, err)
	assert.EqualValues(t, 1, agent, "the act is the agent's")

	require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE tenants SET members_create_projects = false WHERE id = $1", e.A))
	member := e.s.client(t, caller{Token: e.tk.MemberA})
	un, err := member.UnbindRepositoryWithResponse(e.ctx, e.SlugA, "ALPHA", id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, un.StatusCode(), "a member binds and unbinds only while the tenant lets members create projects")
	admin := e.s.client(t, caller{Token: e.tk.AdminAWrite})
	un, err = admin.UnbindRepositoryWithResponse(e.ctx, e.SlugA, "ALPHA", id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, un.StatusCode())
	un, err = admin.UnbindRepositoryWithResponse(e.ctx, e.SlugA, "ALPHA", id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, un.StatusCode(), "unbinding is idempotent")
	acts, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'unlinked'", id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, acts)

	list, err := admin.ListRepositoriesWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.ListRepositoriesParams{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, list.StatusCode())
	assert.Empty(t, list.JSON200.Items)
}

// docs/adr/0048: the list pages like every list, and a restricted project's
// bindings are as invisible as the project.
func TestListingRepositories(t *testing.T) {
	e := newRepoEnv(t)
	admin := caller{Token: e.tk.AdminAWrite}
	for _, name := range []string{"one", "two", "three"} {
		res := e.bind(t, admin, e.SlugA, "ALPHA", apigen.RepositoryBind{Remote: "git@github.com:acme/" + name + ".git"})
		require.Equal(t, http.StatusCreated, res.StatusCode())
	}
	cl := e.s.client(t, caller{Token: e.tk.ViewerA})
	limit := 2
	first, err := cl.ListRepositoriesWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.ListRepositoriesParams{Limit: &limit})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, first.StatusCode(), string(first.Body))
	require.Len(t, first.JSON200.Items, 2)
	assert.Equal(t, "github.com/acme/one", first.JSON200.Items[0].Identity)
	cursor := first.JSON200.NextCursor.MustGet()
	second, err := cl.ListRepositoriesWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.ListRepositoriesParams{Limit: &limit, Cursor: &cursor})
	require.NoError(t, err)
	require.Len(t, second.JSON200.Items, 1)
	assert.Equal(t, "github.com/acme/three", second.JSON200.Items[0].Identity)

	require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", e.ProjectA))
	hidden, err := cl.ListRepositoriesWithResponse(e.ctx, e.SlugA, "ALPHA", &apigen.ListRepositoriesParams{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, hidden.StatusCode(), "a restricted project's bindings are as invisible as the project")
}

// docs/adr/0066 D2, D6: the lookup spans the person's tenants, the first
// remote with a binding decides, the most specific sub-directory wins, and
// several bindings are the data error they are.
func TestLookingUpARepository(t *testing.T) {
	e := newRepoEnv(t)
	res := e.bind(t, caller{Token: e.tk.AdminAWrite}, e.SlugA, "ALPHA", apigen.RepositoryBind{Remote: "git@github.com:acme/app.git"})
	require.Equal(t, http.StatusCreated, res.StatusCode())
	res = e.bind(t, caller{Token: e.tk.AdminAWrite}, e.SlugA, "ALPHA", apigen.RepositoryBind{Remote: "git@github.com:acme/mono.git", Path: ptr("services/a")})
	require.Equal(t, http.StatusCreated, res.StatusCode())

	both := caller{Token: e.tk.Both}
	got := e.lookup(t, both, "", "https://github.com/acme/app")
	require.Equal(t, apigen.RepositoryLookupStatusBound, got.Status)
	require.Len(t, got.Bindings, 1)
	assert.Equal(t, e.SlugA, got.Bindings[0].Tenant.Slug)
	assert.Equal(t, "ALPHA", got.Bindings[0].Project.Key)
	assert.False(t, got.Proposal.IsSpecified() && !got.Proposal.IsNull(), "a bound repository has no proposal")

	got = e.lookup(t, both, "", "git@github.com:me/app-fork.git", "git@github.com:acme/app.git")
	assert.Equal(t, apigen.RepositoryLookupStatusBound, got.Status, "a later remote binds when the first has no binding")
	require.Len(t, got.Remotes, 2)
	assert.Equal(t, "github.com/me/app-fork", got.Remotes[0].Identity.MustGet())

	got = e.lookup(t, both, "services/a/cmd", "git@github.com:acme/mono.git")
	assert.Equal(t, apigen.RepositoryLookupStatusBound, got.Status, "a sub-directory binding covers what is below it")
	got = e.lookup(t, both, "services/b", "git@github.com:acme/mono.git")
	assert.Equal(t, apigen.RepositoryLookupStatusUnbound, got.Status, "and nothing beside it")

	res = e.bind(t, caller{Token: e.tk.MemberB}, e.SlugB, "BETA", apigen.RepositoryBind{Remote: "git@github.com:acme/app.git"})
	require.Equal(t, http.StatusCreated, res.StatusCode(), "another tenant binds the same repository")
	got = e.lookup(t, both, "", "git@github.com:acme/app.git")
	assert.Equal(t, apigen.RepositoryLookupStatusAmbiguous, got.Status, "a person in both sees two bindings (docs/adr/0066 D6)")
	assert.Len(t, got.Bindings, 2)
	got = e.lookup(t, caller{Token: e.tk.MemberA}, "", "git@github.com:acme/app.git")
	assert.Equal(t, apigen.RepositoryLookupStatusBound, got.Status, "a person in one tenant sees that tenant's binding only")

	restricted, _, err := fixtures(t).Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})
	require.NoError(t, err)
	got = e.lookup(t, caller{Token: restricted}, "", "git@github.com:acme/app.git")
	require.Equal(t, apigen.RepositoryLookupStatusBound, got.Status, "a token restricted to a tenant searches that tenant only")
	assert.Equal(t, e.SlugB, got.Bindings[0].Tenant.Slug)

	require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", e.ProjectA))
	got = e.lookup(t, caller{Token: e.tk.MemberA}, "", "git@github.com:acme/app.git")
	assert.Equal(t, apigen.RepositoryLookupStatusUnbound, got.Status, "a restricted project's binding is as invisible as the project")

	got = e.lookup(t, both, "", "/srv/local.git")
	assert.Equal(t, apigen.RepositoryLookupStatusUnbound, got.Status)
	assert.True(t, got.Remotes[0].Identity.IsNull(), "a remote that names no host has no identity")
	assert.True(t, got.Proposal.IsNull())
	assert.Contains(t, got.ProposalUnavailable.MustGet(), "no remote names a host")

	res2 := e.s.do(t, both, http.MethodGet, "/api/v1/me/repositories/lookup?remote=x&path=..%2Fup", nil)
	assertProblem(t, res2, http.StatusBadRequest, "validation_failed")
}

// docs/adr/0066 D2: the proposal — the only tenant the caller may create
// projects in, the tenant that binds repositories under the same owner, or
// the list to choose from — with a key free in each, and none where the
// caller may create nothing.
func TestTheLookupProposesAProject(t *testing.T) {
	e := newRepoEnv(t)
	ctx := e.ctx
	f := fixtures(t)

	got := e.lookup(t, caller{Token: e.tk.MemberA}, "", "git@github.com:acme/valkey-operator.git")
	require.Equal(t, apigen.RepositoryLookupStatusUnbound, got.Status)
	p := got.Proposal.MustGet()
	assert.Equal(t, apigen.RepositoryProposalReasonOnlyTenant, p.Reason)
	assert.Equal(t, e.SlugA, p.Tenant.MustGet())
	assert.Equal(t, "valkey-operator", p.Name)
	assert.Equal(t, "github.com/acme/valkey-operator", p.Identity)
	require.Len(t, p.Tenants, 1)
	assert.Equal(t, "VO", p.Tenants[0].Key)

	_, err := f.Project(ctx, e.A, "VO", "Taken")
	require.NoError(t, err)
	got = e.lookup(t, caller{Token: e.tk.MemberA}, "", "git@github.com:acme/valkey-operator.git")
	assert.Equal(t, "VO2", got.Proposal.MustGet().Tenants[0].Key, "a number is appended on collision")

	got = e.lookup(t, caller{Token: e.tk.Both}, "", "git@github.com:acme/valkey-operator.git")
	p = got.Proposal.MustGet()
	assert.Equal(t, apigen.RepositoryProposalReasonChoose, p.Reason, "two tenants and no hint: the person chooses")
	assert.True(t, p.Tenant.IsNull())
	assert.Len(t, p.Tenants, 2)

	res := e.bind(t, caller{Token: e.tk.MemberB}, e.SlugB, "BETA", apigen.RepositoryBind{Remote: "git@github.com:acme/sibling.git"})
	require.Equal(t, http.StatusCreated, res.StatusCode())
	got = e.lookup(t, caller{Token: e.tk.Both}, "", "git@github.com:acme/valkey-operator.git")
	p = got.Proposal.MustGet()
	assert.Equal(t, apigen.RepositoryProposalReasonRemoteOwner, p.Reason, "the tenant that binds the owner's other repositories")
	assert.Equal(t, e.SlugB, p.Tenant.MustGet())

	for label, c := range map[string]caller{
		"a viewer":          {Token: e.tk.ViewerA},
		"an assisted agent": {Token: e.tk.AssistedAgentA, Agent: agentHeader},
	} {
		got = e.lookup(t, c, "", "git@github.com:acme/new-thing.git")
		assert.True(t, got.Proposal.IsNull(), label)
		assert.Contains(t, got.ProposalUnavailable.MustGet(), "none of your tenants", label)
	}
	scoped, _, err := f.Token(ctx, fixture.TokenSpec{UserID: e.MemberA, TenantID: e.A, ProjectID: e.ProjectA})
	require.NoError(t, err)
	got = e.lookup(t, caller{Token: scoped}, "", "git@github.com:acme/new-thing.git")
	assert.Contains(t, got.ProposalUnavailable.MustGet(), "restricted to one project")
}

// docs/adr/0066 D3, D5: create_project creates the project and binds the
// repository in one recorded act, and is idempotent over the remote.
func TestCreatingAProjectForARepository(t *testing.T) {
	e := newRepoEnv(t)
	cl := e.s.client(t, caller{Token: e.tk.AgentA, Agent: agentHeader})
	create := func(key, remote string) *apigen.CreateProjectResponse {
		res, err := cl.CreateProjectWithResponse(e.ctx, e.SlugA, &apigen.CreateProjectParams{IdempotencyKey: newKey()},
			apigen.ProjectCreate{Key: key, Name: "valkey-operator", Repository: &apigen.RepositoryBind{Remote: remote}})
		require.NoError(t, err)
		return res
	}
	res := create("VO", "git@github.com:acme/valkey-operator.git")
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	created := res.JSON201.Id
	acts, err := fixtures(t).QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'created'
		AND after->>'repository' = 'github.com/acme/valkey-operator'`, created)
	require.NoError(t, err)
	assert.EqualValues(t, 1, acts, "one act creates the project and binds the repository")

	res = create("VO2", "https://github.com/acme/valkey-operator")
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, created, res.JSON200.Id, "the same identity answers with the existing project")
	assert.Equal(t, "VO", res.JSON200.Key)
	n, err := fixtures(t).QueryCount(e.ctx, "SELECT count(*) FROM projects WHERE tenant_id = $1 AND key = 'VO2'", e.A)
	require.NoError(t, err)
	assert.Zero(t, n, "nothing was created twice")

	got := e.lookup(t, caller{Token: e.tk.MemberA}, "", "git@github.com:acme/valkey-operator.git")
	assert.Equal(t, apigen.RepositoryLookupStatusBound, got.Status)

	require.NoError(t, fixtures(t).Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", created))
	res = create("VO3", "git@github.com:acme/valkey-operator.git")
	require.Equal(t, http.StatusConflict, res.StatusCode(), "a binding in a project the caller cannot see")
	assert.Equal(t, "repository_bound", string(res.ApplicationproblemJSONDefault.Code))
	assert.Equal(t, "/repository/remote", (*res.ApplicationproblemJSONDefault.Errors)[0].Pointer)

	assisted := e.s.client(t, caller{Token: e.tk.AssistedAgentA, Agent: agentHeader})
	refused, err := assisted.CreateProjectWithResponse(e.ctx, e.SlugA, &apigen.CreateProjectParams{IdempotencyKey: newKey()},
		apigen.ProjectCreate{Key: "NOPE", Name: "x", Repository: &apigen.RepositoryBind{Remote: "git@github.com:acme/nope.git"}})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, refused.StatusCode())
}

// docs/adr/0043 D6, docs/adr/0070 D2: the token a request presents and what
// it makes of the request.
func TestTheTokenOfTheRequest(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	tk := issueTokens(t, w)
	s := newAPI(t, withLogin)
	ctx := context.Background()

	get := func(c caller) apigen.CurrentToken {
		res, err := s.client(t, c).GetMyTokenWithResponse(ctx)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		return *res.JSON200
	}
	plain := get(caller{Token: tk.MemberA})
	assert.False(t, plain.Agent)
	assert.False(t, plain.Request.Agent)
	assert.True(t, plain.Request.AgentMark.IsNull())
	assert.Empty(t, plain.Request.Capabilities)
	assert.Equal(t, apigen.ScopeWrite, plain.Scope)

	marked := get(caller{Token: tk.MemberA, Agent: agentHeader})
	assert.False(t, marked.Agent, "the flag stays the token's")
	assert.True(t, marked.Request.Agent, "the header marks the request")
	assert.Equal(t, agentHeader, marked.Request.AgentMark.MustGet())
	// Every capability — nine — and the deprecated name of set-horizon beside it, which a
	// cowork-mcp of the release before looks for (docs/adr/0043 D4 as amended 2026-10-05).
	assert.Len(t, marked.Request.Capabilities, 10, "a plain token the header marks holds every capability")

	assisted := get(caller{Token: tk.AssistedAgentA})
	assert.True(t, assisted.Agent)
	assert.Equal(t, "unknown-agent", assisted.Request.AgentMark.MustGet())
	assert.Equal(t, []apigen.Capability{"drop", "set-horizon", "interest", "upload"}, assisted.Capabilities)
	assert.Equal(t, []apigen.Capability{"drop", "set-horizon", "override-urgency", "interest", "upload"},
		assisted.Request.Capabilities, "the request's set, the name before after set-horizon")
	assert.NotContains(t, assisted.Request.Capabilities, apigen.CapabilityClose)

	scoped, id, err := fixtures(t).Token(ctx, fixture.TokenSpec{UserID: w.MemberA, TenantID: w.A, ProjectID: w.ProjectA})
	require.NoError(t, err)
	restricted := get(caller{Token: scoped})
	assert.Equal(t, id, restricted.Id)
	assert.Equal(t, w.SlugA, restricted.RestrictedTenant.MustGet())
	assert.Equal(t, "ALPHA", restricted.RestrictedProject.MustGet())

	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	res := b.get("/api/v1/me/token")
	assertProblem(t, res, http.StatusNotFound, "not_found")
}

// docs/adr/0066 D4: the schema of .cowork.yaml, served without a credential.
func TestTheBindingFileSchemaIsServed(t *testing.T) {
	s := newAPI(t)
	res := s.do(t, caller{}, http.MethodGet, "/api/v1/schemas/cowork-yaml.json", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	schema := decode[map[string]any](t, res)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"tenant", "project", "path", "url"} {
		assert.Contains(t, props, key)
	}
	assert.Equal(t, []any{"tenant", "project"}, schema["required"])
}

// The lookup takes the remotes as given and sends nothing back but their
// sanitised form: a credential in a remote never leaves in an answer.
func TestTheLookupShowsNoCredential(t *testing.T) {
	e := newRepoEnv(t)
	q := url.Values{"remote": {"https://ghp_secret@github.com/acme/app.git"}}
	res := e.s.do(t, caller{Token: e.tk.MemberA}, http.MethodGet, "/api/v1/me/repositories/lookup?"+q.Encode(), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	got := decode[apigen.RepositoryLookup](t, res)
	assert.Equal(t, "https://github.com/acme/app.git", got.Remotes[0].Remote)
	assert.False(t, strings.Contains(got.Proposal.MustGet().Remote, "secret"))
}
