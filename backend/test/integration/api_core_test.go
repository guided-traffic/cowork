//go:build integration

package integration

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

func TestVersionAndDocumentAreServedUnauthenticated(t *testing.T) {
	s := newAPI(t)
	cl := s.client(t, caller{})
	ctx := context.Background()

	v, err := cl.GetVersionWithResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, v.StatusCode())
	assert.Equal(t, "9.9.9-test", v.JSON200.Version)

	doc := s.do(t, caller{}, http.MethodGet, "/api/v1/openapi.json", nil)
	require.Equal(t, http.StatusOK, doc.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(doc.Body).Decode(&body))
	assert.Equal(t, "3.1.0", body["openapi"])
	assert.Equal(t, "9.9.9-test", body["info"].(map[string]any)["version"], "info.version is the backend version (docs/adr/0046 D5)")
}

func TestUnknownRoutesAndMethods(t *testing.T) {
	s := newAPI(t)
	assertProblem(t, s.do(t, caller{}, http.MethodGet, "/api/v1/nothing", nil), http.StatusNotFound, "not_found")
	res := s.do(t, caller{}, http.MethodDelete, "/api/v1/version", nil)
	assertProblem(t, res, http.StatusMethodNotAllowed, "method_not_allowed")
	assert.Equal(t, "GET", res.Header.Get("Allow"))
	head := s.do(t, caller{}, http.MethodHead, "/api/v1/version", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, head.StatusCode, "the API document declares no HEAD")
	assert.Equal(t, "GET", head.Header.Get("Allow"), "and Allow does not offer it")
}

// docs/adr/0035 D4, D6, D9: a missing, malformed or unknown token is 401
// unauthenticated; an expired or revoked one says so, and its use is recorded.
func TestAuthentication(t *testing.T) {
	w := newWorld(t)
	s := newAPI(t)
	f := fixtures(t)
	ctx := context.Background()
	revoked, revokedID, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, Revoked: true})
	require.NoError(t, err)
	expired, _, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, ExpiresAt: time.Now().Add(-time.Hour)})
	require.NoError(t, err)

	for name, c := range map[string]caller{
		"none":      {},
		"malformed": {Token: "not-a-token"},
		"unknown":   {Token: "cwk_" + strings.Repeat("A", 43)},
	} {
		t.Run(name, func(t *testing.T) {
			res := s.do(t, c, http.MethodGet, "/api/v1/me", nil)
			assertProblem(t, res, http.StatusUnauthorized, "unauthenticated")
			assert.Equal(t, `Bearer realm="cowork"`, res.Header.Get("WWW-Authenticate"))
		})
	}
	assertProblem(t, s.do(t, caller{Token: revoked}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")
	assertProblem(t, s.do(t, caller{Token: expired}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_expired")
	assertProblem(t, s.do(t, caller{Token: revoked}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")

	n, err := f.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE token_id = $1 AND action = 'refused'`, revokedID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "two refusals within the hour are one recorded act (docs/adr/0035 D9)")

	// A token in a query parameter is not a token (docs/adr/0035 D7); the
	// parameter is unknown to the route as well.
	res := s.do(t, caller{}, http.MethodGet, "/api/v1/me?access_token="+revoked, nil)
	assertProblem(t, res, http.StatusUnauthorized, "unauthenticated")
}

// docs/adr/0036 D3: a malformed X-Cowork-Agent is refused, never ignored.
func TestMalformedAgentHeaderIsRefused(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	res := s.do(t, caller{Token: tk.MemberA, Agent: "claude-code"}, http.MethodGet, "/api/v1/me", nil)
	body := assertProblem(t, res, http.StatusBadRequest, "validation_failed")
	errs := body["errors"].([]any)
	assert.Equal(t, "header:X-Cowork-Agent", errs[0].(map[string]any)["pointer"])
}

func TestMeListsThePersonsTenants(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	me, err := s.client(t, caller{Token: tk.Both}).GetMeWithResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, me.StatusCode(), string(me.Body))
	assert.Equal(t, w.Both, me.JSON200.Id)
	slugs := make([]string, 0, len(me.JSON200.Memberships))
	for _, m := range me.JSON200.Memberships {
		slugs = append(slugs, m.Tenant.Slug)
		assert.Equal(t, apigen.RoleMember, m.Role)
	}
	assert.ElementsMatch(t, []string{w.SlugA, w.SlugB}, slugs)
}

// docs/adr/0035 D3: a token restricted to a tenant is invalid elsewhere — on
// the person's own routes it sees its tenant's membership and itself only,
// and revokes no other token.
func TestARestrictedTokenSeesItsTenantAndItselfOnly(t *testing.T) {
	w := newWorld(t)
	f := fixtures(t)
	ctx := context.Background()
	s := newAPI(t)
	plain, plainID, err := f.Token(ctx, fixture.TokenSpec{UserID: w.Both})
	require.NoError(t, err)
	narrow, narrowID, err := f.Token(ctx, fixture.TokenSpec{UserID: w.Both, TenantID: w.A})
	require.NoError(t, err)
	cl := s.client(t, caller{Token: narrow})

	me, err := cl.GetMeWithResponse(ctx)
	require.NoError(t, err)
	require.Len(t, me.JSON200.Memberships, 1, "the other tenant's name stays hidden")
	assert.Equal(t, w.SlugA, me.JSON200.Memberships[0].Tenant.Slug)

	list, err := cl.ListMyTokensWithResponse(ctx, &apigen.ListMyTokensParams{})
	require.NoError(t, err)
	require.Len(t, list.JSON200.Items, 1)
	assert.Equal(t, narrowID, list.JSON200.Items[0].Id)

	other := s.do(t, caller{Token: narrow}, http.MethodDelete, "/api/v1/me/tokens/"+plainID.String(), nil)
	assertProblem(t, other, http.StatusNotFound, "not_found")
	assert.Equal(t, http.StatusOK, s.do(t, caller{Token: plain}, http.MethodGet, "/api/v1/me", nil).StatusCode, "the other token still works")
	assert.Equal(t, http.StatusNoContent, s.do(t, caller{Token: narrow}, http.MethodDelete, "/api/v1/me/tokens/"+narrowID.String(), nil).StatusCode,
		"it revokes itself")
}

// The plan's proof: a token of tenant A cannot see tenant B, and the refusal
// is indistinguishable from an unknown tenant (docs/adr/0023 D5,
// docs/adr/0047 D5). Every route family under a tenant is asked.
func TestATokenOfOneTenantCannotSeeAnother(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	f := fixtures(t)
	restrictedToA, _, err := f.Token(context.Background(), fixture.TokenSpec{UserID: w.Both, TenantID: w.A})
	require.NoError(t, err)

	routes := []string{"", "/members", "/audit", "/projects", "/projects/BETA", "/projects/BETA/archive"}
	for _, route := range routes {
		for name, c := range map[string]caller{"member of A": {Token: tk.MemberA}, "token restricted to A": {Token: restrictedToA}} {
			t.Run(name+" "+route, func(t *testing.T) {
				method := http.MethodGet
				if strings.HasSuffix(route, "/archive") {
					method = http.MethodPut
				}
				refused := s.do(t, c, method, "/api/v1/tenants/"+w.SlugB+route, nil)
				unknown := s.do(t, c, method, "/api/v1/tenants/no-such-tenant-x"+route, nil)
				rb := assertProblem(t, refused, http.StatusNotFound, "not_found")
				ub := assertProblem(t, unknown, http.StatusNotFound, "not_found")
				for _, k := range []string{"type", "title", "status", "detail", "code"} {
					assert.Equal(t, ub[k], rb[k], "the refusal must not tell the tenant exists (%s)", k)
				}
			})
		}
	}
	// The person in both tenants reaches B with a token that is not restricted.
	res := s.do(t, caller{Token: tk.Both}, http.MethodGet, "/api/v1/tenants/"+w.SlugB, nil)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestValidationRefusesWhatTheDocumentDoesNotDescribe(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	c := caller{Token: tk.AdminA}

	body := assertProblem(t, s.do(t, c, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/projects?sort=key", nil), http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "query:sort", body["errors"].([]any)[0].(map[string]any)["pointer"], "an unknown parameter is refused (docs/adr/0049 D4)")

	body = assertProblem(t, s.do(t, c, http.MethodPost, "/api/v1/tenants/"+w.SlugA+"/projects", map[string]any{"key": "lower", "name": "x", "extra": 1}), http.StatusBadRequest, "validation_failed")
	pointers := map[string]bool{}
	for _, e := range body["errors"].([]any) {
		pointers[e.(map[string]any)["pointer"].(string)] = true
	}
	assert.True(t, pointers["/key"], "the key breaks its pattern: %v", pointers)

	assertProblem(t, s.do(t, c, http.MethodGet, "/api/v1/tenants/Not_A_Slug", nil), http.StatusNotFound, "not_found")
	assertProblem(t, s.do(t, c, http.MethodPost, "/api/v1/tenants/"+w.SlugA+"/projects", "{not json"), http.StatusBadRequest, "validation_failed")
}

func TestBodyLimit(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t, func(o *api.Options) { o.MaxJSONBody = 64 })
	res := s.do(t, caller{Token: tk.AdminA}, http.MethodPost, "/api/v1/tenants/"+w.SlugA+"/projects",
		map[string]any{"key": "BIG", "name": strings.Repeat("x", 200)})
	assertProblem(t, res, http.StatusRequestEntityTooLarge, "payload_too_large")
}

// docs/adr/0035 D6, docs/adr/0043 D3: revocation is immediate and recorded; a
// token may always revoke itself, another token of the person needs write
// scope, and an agent may revoke only the token it holds.
func TestRevokingTokens(t *testing.T) {
	w := newWorld(t)
	s := newAPI(t)
	f := fixtures(t)
	ctx := context.Background()
	mint := func(spec fixture.TokenSpec) (string, string) {
		p, id, err := f.Token(ctx, spec)
		require.NoError(t, err)
		return p, id.String()
	}
	readTok, _ := mint(fixture.TokenSpec{UserID: w.MemberA, Scope: "read"})
	writeTok, _ := mint(fixture.TokenSpec{UserID: w.MemberA})
	agentTok, agentID := mint(fixture.TokenSpec{UserID: w.MemberA, Agent: true})
	_, victimID := mint(fixture.TokenSpec{UserID: w.MemberA, Name: "victim"})
	_, otherPersonsID := mint(fixture.TokenSpec{UserID: w.MemberB})

	assertProblem(t, s.do(t, caller{Token: readTok}, http.MethodDelete, "/api/v1/me/tokens/"+victimID, nil), http.StatusForbidden, "insufficient_scope")
	body := assertProblem(t, s.do(t, caller{Token: agentTok}, http.MethodDelete, "/api/v1/me/tokens/"+victimID, nil), http.StatusForbidden, "agent_forbidden")
	assert.Equal(t, "hard-off: token administration", body["detail"])
	assertProblem(t, s.do(t, caller{Token: writeTok}, http.MethodDelete, "/api/v1/me/tokens/"+otherPersonsID, nil), http.StatusNotFound, "not_found")

	require.Equal(t, http.StatusNoContent, s.do(t, caller{Token: writeTok}, http.MethodDelete, "/api/v1/me/tokens/"+victimID, nil).StatusCode)
	require.Equal(t, http.StatusNoContent, s.do(t, caller{Token: writeTok}, http.MethodDelete, "/api/v1/me/tokens/"+victimID, nil).StatusCode, "revoking twice changes nothing")
	require.Equal(t, http.StatusNoContent, s.do(t, caller{Token: agentTok}, http.MethodDelete, "/api/v1/me/tokens/"+agentID, nil).StatusCode, "an agent revokes the token it holds")
	assertProblem(t, s.do(t, caller{Token: agentTok}, http.MethodGet, "/api/v1/me", nil), http.StatusUnauthorized, "token_revoked")

	n, err := f.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'revoked' AND tenant_id IS NULL`, victimID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	list, err := s.client(t, caller{Token: writeTok}).ListMyTokensWithResponse(ctx, &apigen.ListMyTokensParams{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, list.StatusCode(), string(list.Body))
	states := map[string]apigen.TokenState{}
	for _, tok := range list.JSON200.Items {
		states[tok.Id.String()] = tok.State
	}
	assert.Equal(t, apigen.TokenStateRevoked, states[victimID], "a revoked token stays listed")
	assert.Equal(t, apigen.TokenStateRevoked, states[agentID])
}

// docs/adr/0035 D6: revoking stays idempotent when the revocations race —
// each of several simultaneous ones answers 204, and the act is recorded once.
func TestSimultaneousRevocationsAnswerAlike(t *testing.T) {
	w := newWorld(t)
	s := newAPI(t)
	f := fixtures(t)
	ctx := context.Background()
	writeTok, _, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA})
	require.NoError(t, err)
	_, victim, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, Name: "victim"})
	require.NoError(t, err)
	cl := s.client(t, caller{Token: writeTok})

	codes := simultaneously(times(16, func() int {
		res, err := cl.RevokeMyTokenWithResponse(ctx, victim)
		if err != nil {
			return 0
		}
		return res.StatusCode()
	})...)
	for i, code := range codes {
		assert.Equal(t, http.StatusNoContent, code, "revocation %d", i)
	}
	count, err := f.QueryCount(ctx, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'revoked'`, victim.String())
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestTenantSettings(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	admin := s.client(t, caller{Token: tk.AdminA})

	got, err := admin.GetTenantWithResponse(ctx, w.SlugA)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode(), string(got.Body))
	assert.True(t, got.JSON200.MembersCreateProjects, "members create projects by default (docs/adr/0034 D9)")
	etag := got.HTTPResponse.Header.Get("ETag")
	require.NotEmpty(t, etag)

	patch := apigen.TenantPatch{MembersCreateProjects: ptr(false)}
	res, err := admin.UpdateTenantWithResponse(ctx, w.SlugA, &apigen.UpdateTenantParams{}, patch)
	require.NoError(t, err)
	assert.Equal(t, http.StatusPreconditionRequired, res.StatusCode(), "an overwriting write needs If-Match (docs/adr/0050 D3)")

	res, err = admin.UpdateTenantWithResponse(ctx, w.SlugA, &apigen.UpdateTenantParams{IfMatch: ptr(`"99"`)}, patch)
	require.NoError(t, err)
	require.Equal(t, http.StatusPreconditionFailed, res.StatusCode())
	assert.Equal(t, etag, res.HTTPResponse.Header.Get("ETag"), "a 412 names the current version")
	require.NotNil(t, res.ApplicationproblemJSONDefault)
	require.NotNil(t, res.ApplicationproblemJSONDefault.Errors)
	assert.Equal(t, "/members_create_projects", (*res.ApplicationproblemJSONDefault.Errors)[0].Pointer)

	res, err = admin.UpdateTenantWithResponse(ctx, w.SlugA, &apigen.UpdateTenantParams{IfMatch: &etag}, patch)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.False(t, res.JSON200.MembersCreateProjects)
	assert.NotEqual(t, etag, res.HTTPResponse.Header.Get("ETag"))

	member := s.client(t, caller{Token: tk.MemberA})
	denied, err := member.UpdateTenantWithResponse(ctx, w.SlugA, &apigen.UpdateTenantParams{IfMatch: &etag}, patch)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, denied.StatusCode())
	writeAdmin, err := s.client(t, caller{Token: tk.AdminAWrite}).UpdateTenantWithResponse(ctx, w.SlugA, &apigen.UpdateTenantParams{IfMatch: &etag}, patch)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, writeAdmin.StatusCode(), "an administration act needs admin scope")
	assert.Equal(t, "insufficient_scope", string(writeAdmin.ApplicationproblemJSONDefault.Code))
}

func TestAuditViewForAdministrators(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	created, err := s.client(t, caller{Token: tk.AgentA, Agent: "claude-code/opus/s1"}).CreateProjectWithResponse(ctx, w.SlugA,
		&apigen.CreateProjectParams{IdempotencyKey: newKey()}, apigen.ProjectCreate{Key: "AUD", Name: "Audited"})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))

	denied, err := s.client(t, caller{Token: tk.MemberA}).ListAuditWithResponse(ctx, w.SlugA, &apigen.ListAuditParams{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, denied.StatusCode())

	audit, err := s.client(t, caller{Token: tk.AdminA}).ListAuditWithResponse(ctx, w.SlugA, &apigen.ListAuditParams{
		Action: &[]apigen.AuditAction{apigen.AuditActionCreated}})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, audit.StatusCode(), string(audit.Body))
	require.NotEmpty(t, audit.JSON200.Items)
	row := audit.JSON200.Items[0]
	assert.Equal(t, "project", row.EntityType)
	person, err := row.Actor.Person.Get()
	require.NoError(t, err)
	assert.Equal(t, w.MemberA, person.Id, "the actor is the token's person (docs/adr/0036 D1)")
	agent, err := row.Agent.Get()
	require.NoError(t, err)
	assert.Equal(t, "claude-code/opus/s1", agent)

	res := s.do(t, caller{Token: tk.AdminA}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/audit", nil, "Accept", "text/csv")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/csv", res.Header.Get("Content-Type"))
	records, err := csv.NewReader(res.Body).ReadAll()
	require.NoError(t, err)
	require.Greater(t, len(records), 1)
	assert.Equal(t, "id", records[0][0])
}

func TestCursorPagination(t *testing.T) {
	w := newWorld(t)
	tk := issueTokens(t, w)
	s := newAPI(t)
	ctx := context.Background()
	admin := s.client(t, caller{Token: tk.AdminA})
	for _, key := range []string{"PA", "PB", "PC"} {
		res, err := admin.CreateProjectWithResponse(ctx, w.SlugA, &apigen.CreateProjectParams{}, apigen.ProjectCreate{Key: key, Name: key})
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	}
	var keys []string
	var cursor *string
	for range 10 {
		res, err := admin.ListProjectsWithResponse(ctx, w.SlugA, &apigen.ListProjectsParams{Limit: ptr(2), Cursor: cursor})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		for _, p := range res.JSON200.Items {
			keys = append(keys, p.Key)
		}
		next, err := res.JSON200.NextCursor.Get()
		if err != nil {
			break
		}
		cursor = &next
	}
	assert.Equal(t, []string{"ALPHA", "PA", "PB", "PC"}, keys)

	first, err := admin.ListProjectsWithResponse(ctx, w.SlugA, &apigen.ListProjectsParams{Limit: ptr(1)})
	require.NoError(t, err)
	next, err := first.JSON200.NextCursor.Get()
	require.NoError(t, err)
	tampered := next[:len(next)-2] + "AA"
	assertProblem(t, s.do(t, caller{Token: tk.AdminA}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/projects?cursor="+tampered, nil), http.StatusBadRequest, "invalid_cursor")
	assertProblem(t, s.do(t, caller{Token: tk.AdminA}, http.MethodGet, "/api/v1/tenants/"+w.SlugA+"/members?cursor="+next, nil), http.StatusBadRequest, "invalid_cursor")
}
