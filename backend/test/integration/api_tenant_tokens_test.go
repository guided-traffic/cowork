//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// tenantTokens reads a tenant's token list as raw text and decoded, so a test
// can assert what is absent from the whole answer.
func tenantTokens(t *testing.T, res *http.Response) (string, map[string]apigen.MemberToken) {
	raw, list := tenantTokenList(t, res)
	byName := map[string]apigen.MemberToken{}
	for _, tok := range list.Items {
		byName[tok.Name] = tok
	}
	return raw, byName
}

func tenantTokenList(t *testing.T, res *http.Response) (string, apigen.MemberTokenList) {
	t.Helper()
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var list apigen.MemberTokenList
	require.NoError(t, json.Unmarshal(raw, &list))
	return string(raw), list
}

// docs/adr/0035 D5 as amended 2026-10-05: a tenant's administrators see the
// tokens that can act in the tenant — the members' unrestricted tokens and
// those restricted to it — and nothing of a token restricted to another tenant
// or of a person who is no member, not even its name; revoking one is a
// recorded act of the tenant, an unrestricted one ends in the person's other
// tenants too.
func TestTenantAdministratorsSeeAndRevokeTheTokensThatCanActInTheTenant(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	f := fixtures(t)
	mint := func(spec fixture.TokenSpec) (string, uuid.UUID) {
		plaintext, id, err := f.Token(ctx, spec)
		require.NoError(t, err)
		return plaintext, id
	}
	everywhere, everywhereID := mint(fixture.TokenSpec{UserID: a.Both, Name: "both-everywhere"})
	inA, inAID := mint(fixture.TokenSpec{UserID: a.Both, Name: "both-in-a", TenantID: a.A})
	_, alphaID := mint(fixture.TokenSpec{UserID: a.Both, Name: "both-alpha", TenantID: a.A, ProjectID: a.ProjectA})
	inB, inBID := mint(fixture.TokenSpec{UserID: a.Both, Name: "secret-name-of-b", TenantID: a.B})
	_, outsiderID := mint(fixture.TokenSpec{UserID: a.MemberB, Name: "outsider-of-a"})
	adminB, err := f.Person(ctx, uniqueSlug("admin-b"), "Admin B")
	require.NoError(t, err)
	require.NoError(t, f.Member(ctx, a.B, adminB, domain.RoleAdmin))
	adminBToken, _ := mint(fixture.TokenSpec{UserID: adminB, Scope: domain.ScopeAdmin})
	readToken, _ := mint(fixture.TokenSpec{UserID: a.AdminA, Scope: domain.ScopeRead})

	raw, all := tenantTokenList(t, a.admin.get(a.path("/tokens?limit=200")))
	_, seen := tenantTokens(t, a.admin.get(a.path("/tokens?limit=200")))
	for _, name := range []string{"both-everywhere", "both-in-a", "both-alpha"} {
		require.Contains(t, seen, name)
		assert.Equal(t, a.Both, seen[name].Person.Id)
	}
	assert.NotContains(t, raw, "secret-name-of-b", "a token restricted to another tenant never shows, not even its name")
	assert.NotContains(t, raw, inBID.String())
	assert.NotContains(t, raw, "outsider-of-a", "nor a token of a person who is no member")
	assert.NotContains(t, raw, outsiderID.String())
	assert.NotContains(t, raw, "cwk_", "no secret")
	assert.True(t, seen["both-everywhere"].RestrictedTeam.IsNull(), "an unrestricted token says so")
	slug, err := seen["both-in-a"].RestrictedTeam.Get()
	require.NoError(t, err)
	assert.Equal(t, a.SlugA, slug)
	key, err := seen["both-alpha"].RestrictedProject.Get()
	require.NoError(t, err)
	assert.Equal(t, "ALPHA", key)
	assert.Equal(t, alphaID, seen["both-alpha"].Id)

	// Tenant B's administrator sees B's share of the same person's tokens.
	rawB, seenB := tenantTokens(t, a.s.do(t, caller{Token: adminBToken}, http.MethodGet, "/api/v1/teams/"+a.SlugB+"/tokens?limit=200", nil))
	assert.Contains(t, seenB, "both-everywhere")
	assert.Contains(t, seenB, "secret-name-of-b")
	assert.Contains(t, seenB, "outsider-of-a")
	assert.NotContains(t, rawB, "both-in-a")
	assert.NotContains(t, rawB, "both-alpha")

	// Numbered pages count what the page shows.
	res := a.s.do(t, caller{Token: readToken}, http.MethodGet, a.path("/tokens?page=1&per_page=25"), nil)
	require.Equal(t, http.StatusOK, res.StatusCode, "an administrator's read token lists")
	numbered := decode[apigen.MemberTokenList](t, res)
	require.NotNil(t, numbered.Total)
	assert.Equal(t, len(all.Items), *numbered.Total)
	assert.Len(t, numbered.Items, len(all.Items))

	// Nobody but the tenant's administrators.
	assertProblem(t, a.s.do(t, caller{Token: a.tokenOf(t, a.MemberA)}, http.MethodGet, a.path("/tokens"), nil), http.StatusForbidden, "forbidden")
	assertProblem(t, a.s.do(t, caller{Token: a.tokenOf(t, a.ViewerA)}, http.MethodGet, a.path("/tokens"), nil), http.StatusForbidden, "forbidden")

	// Revoking: an administrator's act, a token of A's list only.
	revoke := func(c caller, id uuid.UUID) *http.Response {
		return a.s.do(t, c, http.MethodDelete, a.path("/tokens/"+id.String()), nil)
	}
	adminToken := caller{Token: a.token}
	assertProblem(t, revoke(adminToken, inBID), http.StatusNotFound, "not_found")
	assertProblem(t, revoke(adminToken, outsiderID), http.StatusNotFound, "not_found")
	assertProblem(t, revoke(adminToken, uuid.Must(uuid.NewV7())), http.StatusNotFound, "not_found")
	assertProblem(t, revoke(caller{Token: readToken}, inAID), http.StatusForbidden, "insufficient_scope")
	assertProblem(t, revoke(caller{Token: a.token, Agent: "claude-code/opus/1"}, inAID), http.StatusForbidden, "agent_forbidden")
	assertProblem(t, revoke(caller{Token: a.tokenOf(t, a.MemberA)}, inAID), http.StatusForbidden, "forbidden")
	assert.Equal(t, http.StatusOK, a.s.do(t, caller{Token: inB}, http.MethodGet, "/api/v1/teams/"+a.SlugB+"/projects", nil).StatusCode,
		"the token of another tenant is untouched")

	require.Equal(t, http.StatusNoContent, revoke(adminToken, inAID).StatusCode, "only takes access away: a token may")
	assertProblem(t, a.s.do(t, caller{Token: inA}, http.MethodGet, a.path("/projects"), nil), http.StatusUnauthorized, "token_revoked")
	require.Equal(t, http.StatusNoContent, revoke(adminToken, inAID).StatusCode, "revoking a revoked token changes nothing")
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events
		WHERE tenant_id = $1 AND entity_type = 'token' AND entity_id = $2 AND action = 'revoked' AND actor_user_id = $3`,
		a.A, inAID, a.AdminA), "a recorded act of the tenant, once")

	res = a.admin.request(http.MethodDelete, a.path("/tokens/"+everywhereID.String()), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode, "in a session too")
	assertProblem(t, a.s.do(t, caller{Token: everywhere}, http.MethodGet, "/api/v1/teams/"+a.SlugB+"/projects", nil),
		http.StatusUnauthorized, "token_revoked")
	assert.Equal(t, "true", scalar[string](t, `SELECT after->>'unrestricted' FROM audit_events
		WHERE tenant_id = $1 AND entity_id = $2 AND action = 'revoked'`, a.A, everywhereID), "the record says it reached beyond the tenant")
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'token'`, a.B),
		"tenant B's record holds nothing of A's acts")
	assert.Equal(t, a.AdminA, scalar[uuid.UUID](t, `SELECT revoked_by FROM tokens WHERE id = $1`, everywhereID))

	_, seen = tenantTokens(t, a.admin.get(a.path("/tokens?limit=200")))
	assert.Equal(t, apigen.TokenStateRevoked, seen["both-everywhere"].State, "a revoked token stays listed")

	// docs/adr/0054 D7: a page the client holds unchanged is a 304 to its weak
	// ETag; a revocation moves it.
	listed := a.s.do(t, adminToken, http.MethodGet, a.path("/tokens?page=1&per_page=25"), nil)
	require.Equal(t, http.StatusOK, listed.StatusCode)
	tag := listed.Header.Get("ETag")
	require.True(t, strings.HasPrefix(tag, `W/"`), tag)
	same := a.s.do(t, adminToken, http.MethodGet, a.path("/tokens?page=1&per_page=25"), nil, "If-None-Match", tag)
	assert.Equal(t, http.StatusNotModified, same.StatusCode)
	require.Equal(t, http.StatusNoContent, revoke(adminToken, alphaID).StatusCode)
	changed := a.s.do(t, adminToken, http.MethodGet, a.path("/tokens?page=1&per_page=25"), nil, "If-None-Match", tag)
	assert.Equal(t, http.StatusOK, changed.StatusCode, "the revocation moved the page")
}

// tokenOf mints a plain write token of a person of the world.
func (a adminWorld) tokenOf(t *testing.T, person uuid.UUID) string {
	t.Helper()
	plaintext, _, err := fixtures(t).Token(context.Background(), fixture.TokenSpec{UserID: person})
	require.NoError(t, err)
	return plaintext
}
