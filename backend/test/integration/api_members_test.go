//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/bootstrap"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/oidc"
	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// adminWorld is a world whose persons have local accounts, its tenant A's
// administrator in a session, an administrator's token besides, and an
// identity provider in the test's process whose persons the tests make.
type adminWorld struct {
	world
	s        apiServer
	names    map[string]string
	admin    *browser
	token    string
	issuer   string
	provider *oidc.Provider
}

func newAdminWorld(t *testing.T) adminWorld {
	t.Helper()
	w := newWorld(t)
	is := fakeissuer.Start(t)
	provider := fakeProvider(t, is)
	a := adminWorld{world: w, names: withAccounts(t, w), issuer: is.URL, provider: provider,
		s: newAPI(t, withLogin, withIdentity(provider, []string{"cowork-users"}, ""))}
	a.admin = a.s.browser(t)
	a.admin.mustLogin(a.names["adminA"], testPassword)
	plaintext, _, err := fixtures(t).Token(context.Background(), fixture.TokenSpec{UserID: w.AdminA, Scope: domain.ScopeAdmin})
	require.NoError(t, err)
	a.token = plaintext
	return a
}

func (a adminWorld) path(rest string) string { return "/api/v1/tenants/" + a.SlugA + rest }

// globalAdmin makes the tenant's administrator a global administrator as well,
// who alone makes a group mapping or changes its role (docs/adr/0030 D7).
func (a adminWorld) globalAdmin(t *testing.T) {
	t.Helper()
	require.NoError(t, fixtures(t).GlobalAdmin(context.Background(), a.AdminA))
}

// person makes a person of the identity provider with an address and groups.
func (a adminWorld) person(t *testing.T, email string, verified *bool, groups ...string) uuid.UUID {
	t.Helper()
	return providerPerson(t, fixtures(t), a.issuer, email, verified, groups)
}

func address(prefix string) string { return uniqueSlug(prefix) + "@example.com" }

func members(t *testing.T, b *browser, path string) map[uuid.UUID]apigen.Member {
	t.Helper()
	res := b.get(path + "?limit=200")
	require.Equal(t, http.StatusOK, res.StatusCode)
	out := map[uuid.UUID]apigen.Member{}
	for _, m := range decode[apigen.MemberList](t, res).Items {
		out[m.Person.Id] = m
	}
	return out
}

// docs/adr/0030 D3, docs/adr/0029 D5, docs/adr/0035 D5: an administrator grants
// a role to a person who exists — by the address the issuer asserted, compared
// without regard to case, an unverified one never, or by a local account's
// username — in a browser session only.
func TestAddMemberByAddressOrUsername(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	yes, no := true, false
	add := func(person, role string) *http.Response {
		return a.admin.request(http.MethodPost, a.path("/members"), map[string]string{"person": person, "role": role})
	}

	pat := address("pat")
	patID := a.person(t, pat, &yes, "g")
	res := add("  "+strings.ToUpper(pat)+" ", "member")
	require.Equal(t, http.StatusCreated, res.StatusCode)
	m := decode[apigen.Member](t, res)
	assert.Equal(t, patID, m.Person.Id)
	assert.False(t, m.Local)
	assert.True(t, m.Person.Username.IsNull())
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceGrant, Role: apigen.RoleMember}}, m.Origins)
	assertProblem(t, add(pat, "viewer"), http.StatusConflict, "grant_exists")

	unverified := address("unv")
	a.person(t, unverified, &no)
	assertProblem(t, add(unverified, "member"), http.StatusNotFound, "person_not_found")
	silent := address("silent")
	a.person(t, silent, nil)
	assertProblem(t, add(silent, "member"), http.StatusNotFound, "person_not_found") // the next test

	// The issuer made two persons of one address, as it does for an account it
	// re-created.
	shared := address("shared")
	a.person(t, shared, &yes)
	a.person(t, shared, &yes)
	assertProblem(t, add(shared, "member"), http.StatusConflict, "person_ambiguous")
	// A person of another issuer cannot log in, and is no one to grant to (m6).
	elsewhere := address("elsewhere")
	providerPerson(t, fixtures(t), "https://another-issuer.example.com", elsewhere, &yes, nil)
	assertProblem(t, add(elsewhere, "member"), http.StatusNotFound, "person_not_found")

	gone := address("gone")
	goneID := a.person(t, gone, &yes)
	require.NoError(t, fixtures(t).Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, goneID))
	assertProblem(t, add(gone, "member"), http.StatusNotFound, "person_not_found")
	assertProblem(t, add(address("nobody"), "member"), http.StatusNotFound, "person_not_found")

	local := add(a.names["memberB"], "viewer")
	require.Equal(t, http.StatusCreated, local.StatusCode, "a local account of another tenant, by its username")
	assert.True(t, decode[apigen.Member](t, local).Local)
	prefixed, err := fixtures(t).Person(ctx, uniqueSlug("pre"), "Prefixed")
	require.NoError(t, err)
	require.NoError(t, fixtures(t).Account(ctx, prefixed, testPassword, a.B, false))
	named := add("local:"+strings.ToUpper(usernameOf(t, prefixed)), "viewer")
	require.Equal(t, http.StatusCreated, named.StatusCode, "the identity's name, local:<username>, in any case")
	assert.Equal(t, usernameOf(t, prefixed), decode[apigen.Member](t, named).Person.Username.MustGet(), "the view holds the plain username")
	assertProblem(t, add("   ", "member"), http.StatusBadRequest, "validation_failed")

	assertProblem(t, a.s.do(t, caller{Token: a.token}, http.MethodPost, a.path("/members"), map[string]string{"person": pat, "role": "member"}),
		http.StatusForbidden, "session_required")
	member := a.s.browser(t)
	member.mustLogin(a.names["memberA"], testPassword)
	assertProblem(t, member.request(http.MethodPost, a.path("/members"), map[string]string{"person": silent, "role": "admin"}),
		http.StatusForbidden, "forbidden")

	list := members(t, member, a.path("/members"))
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceGrant, Role: apigen.RoleMember}}, list[patID].Origins,
		"every member reads the origins (docs/adr/0034 D7)")
	assert.True(t, list[a.MemberB].Local)
}

// docs/adr/0030 D3, COWORK_OIDC_EMAIL_TRUSTED (the owner's answer of
// 2026-10-04): by default an address finds a person only when the issuer
// marked it verified; with the setting, also when the issuer said nothing about
// it; never when it marked it unverified.
func TestAnAddressTheIssuerSaidNothingAboutIsTrustedOnlyWhenConfigured(t *testing.T) {
	a := newAdminWorld(t)
	yes, no := true, false
	silent, unverified, verified := address("silent"), address("unv"), address("ver")
	silentID := a.person(t, silent, nil)
	a.person(t, unverified, &no)
	a.person(t, verified, &yes)
	add := func(b *browser, person string) *http.Response {
		return b.request(http.MethodPost, a.path("/members"), map[string]string{"person": person, "role": "viewer"})
	}

	assertProblem(t, add(a.admin, silent), http.StatusNotFound, "person_not_found")
	assertProblem(t, add(a.admin, unverified), http.StatusNotFound, "person_not_found")
	require.Equal(t, http.StatusCreated, add(a.admin, verified).StatusCode)

	trusting := newAPI(t, withLogin, withIdentity(a.provider, []string{"cowork-users"}, ""),
		func(o *api.Options) { o.OIDC.EmailTrusted = true })
	admin := trusting.browser(t)
	admin.mustLogin(a.names["adminA"], testPassword)
	res := add(admin, silent)
	require.Equal(t, http.StatusCreated, res.StatusCode, "the issuer's silence is taken as its word")
	assert.Equal(t, silentID, decode[apigen.Member](t, res).Person.Id)
	assertProblem(t, add(admin, unverified), http.StatusNotFound, "person_not_found")
}

// docs/adr/0030 D3, docs/adr/0034 D1: a grant is set and removed by an
// administrator; setting is a session's, removing a token may do too; no
// change leaves the tenant without an administrator.
func TestGrantsAndTheLastAdministrator(t *testing.T) {
	a := newAdminWorld(t)
	grant := func(b *browser, person uuid.UUID, role string) *http.Response {
		return b.request(http.MethodPut, a.path("/members/"+person.String()+"/grant"), map[string]string{"role": role})
	}
	assertProblem(t, grant(a.admin, a.MemberB, "member"), http.StatusNotFound, "person_not_found")
	assertProblem(t, grant(a.admin, uuid.Must(uuid.NewV7()), "member"), http.StatusNotFound, "person_not_found")

	res := grant(a.admin, a.MemberA, "admin")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, apigen.RoleAdmin, decode[apigen.Member](t, res).Role)
	before := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'`, a.A)
	require.Equal(t, http.StatusOK, grant(a.admin, a.MemberA, "admin").StatusCode)
	assert.Equal(t, before, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'`, a.A),
		"repeating it changes nothing")
	assertProblem(t, a.s.do(t, caller{Token: a.token}, http.MethodPut, a.path("/members/"+a.MemberA.String()+"/grant"),
		map[string]string{"role": "viewer"}), http.StatusForbidden, "session_required")

	remove := a.path("/members/" + a.MemberA.String() + "/grant")
	assert.Equal(t, http.StatusNoContent, a.s.do(t, caller{Token: a.token}, http.MethodDelete, remove, nil).StatusCode,
		"removing a grant only takes access away: a token may")
	assert.Equal(t, http.StatusNoContent, a.s.do(t, caller{Token: a.token}, http.MethodDelete, remove, nil).StatusCode, "idempotent")
	assertProblem(t, a.admin.get("/api/v1/tenants/"+a.SlugA+"/members/"+a.MemberA.String()+"/grant"), http.StatusMethodNotAllowed, "method_not_allowed")

	assertProblem(t, grant(a.admin, a.AdminA, "member"), http.StatusConflict, "last_admin")
	assertProblem(t, a.admin.request(http.MethodDelete, a.path("/members/"+a.AdminA.String()+"/grant"), nil), http.StatusConflict, "last_admin")
	assert.Equal(t, "admin", scalar[string](t, `SELECT role::text FROM memberships WHERE tenant_id = $1 AND user_id = $2`, a.A, a.AdminA))
}

// docs/adr/0030 D2, D7: a mapping applies at once to every person whose groups
// hold it — made, changed, removed — and its writes are a session's, its
// removal a token's too.
func TestGroupMappingsDeriveAtOnce(t *testing.T) {
	a := newAdminWorld(t)
	a.globalAdmin(t)
	yes := true
	gA, gB := uniqueSlug("ga"), uniqueSlug("gb")
	p1 := a.person(t, address("p1"), &yes, gA)
	p2 := a.person(t, address("p2"), &yes, gA, gB)
	create := func(group, role string) *http.Response {
		return a.admin.request(http.MethodPost, a.path("/group-mappings"), map[string]string{"group": group, "role": role})
	}

	res := create(gA, "member")
	require.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Equal(t, `"1"`, res.Header.Get("ETag"))
	mapping := decode[apigen.GroupMapping](t, res)
	assert.Equal(t, a.path("/group-mappings/"+mapping.Id.String()), res.Header.Get("Location"))
	assert.False(t, mapping.IncludesCaller, "a local account holds no group")
	list := members(t, a.admin, a.path("/members"))
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceMapping, Role: apigen.RoleMember}}, list[p1].Origins)
	assert.Equal(t, apigen.RoleMember, list[p2].Role)
	assert.EqualValues(t, 2, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'
		AND action = 'created' AND reason = 'mapping' AND actor_system = 'system:identity-provider' AND actor_user_id IS NULL`, a.A))

	require.Equal(t, http.StatusCreated, create(gB, "admin").StatusCode)
	assert.Equal(t, apigen.RoleAdmin, members(t, a.admin, a.path("/members"))[p2].Role, "several mapped groups give the highest role")
	assertProblem(t, create(gA, "viewer"), http.StatusConflict, "mapping_exists")
	assertProblem(t, a.s.do(t, caller{Token: a.token}, http.MethodPost, a.path("/group-mappings"), map[string]string{"group": "x", "role": "member"}),
		http.StatusForbidden, "session_required")
	assertProblem(t, create(" padded", "member"), http.StatusBadRequest, "validation_failed")

	patch := a.path("/group-mappings/" + mapping.Id.String())
	assertProblem(t, a.admin.request(http.MethodPatch, patch, map[string]string{"role": "viewer"}), http.StatusPreconditionRequired, "precondition_required")
	assertProblem(t, a.admin.request(http.MethodPatch, patch, map[string]string{"role": "viewer"}, withHeader("If-Match", `"9"`)),
		http.StatusPreconditionFailed, "precondition_failed")
	changed := a.admin.request(http.MethodPatch, patch, map[string]string{"role": "viewer"}, withHeader("If-Match", `"1"`))
	require.Equal(t, http.StatusOK, changed.StatusCode)
	assert.Equal(t, `"2"`, changed.Header.Get("ETag"))
	assert.Equal(t, apigen.RoleViewer, members(t, a.admin, a.path("/members"))[p1].Role, "changed at once")

	page := decode[apigen.GroupMappingList](t, a.admin.get(a.path("/group-mappings?limit=1")))
	require.Len(t, page.Items, 1)
	require.False(t, page.NextCursor.IsNull())
	rest := decode[apigen.GroupMappingList](t, a.admin.get(a.path("/group-mappings?limit=1&cursor="+page.NextCursor.MustGet())))
	require.Len(t, rest.Items, 1)
	assert.NotEqual(t, page.Items[0].Group, rest.Items[0].Group)
	member := a.s.browser(t)
	member.mustLogin(a.names["memberA"], testPassword)
	assertProblem(t, member.get(a.path("/group-mappings")), http.StatusForbidden, "forbidden")

	var gBID uuid.UUID
	for _, m := range append(page.Items, rest.Items...) {
		if m.Group == gB {
			gBID = m.Id
		}
	}
	assert.Equal(t, http.StatusNoContent, a.s.do(t, caller{Token: a.token}, http.MethodDelete, a.path("/group-mappings/"+gBID.String()), nil).StatusCode,
		"removing a mapping only takes access away: a token may")
	assert.Equal(t, apigen.RoleViewer, members(t, a.admin, a.path("/members"))[p2].Role, "the next mapped group's role")
	require.Equal(t, http.StatusNoContent, a.admin.request(http.MethodDelete, patch, nil).StatusCode)
	require.Equal(t, http.StatusNoContent, a.admin.request(http.MethodDelete, patch, nil).StatusCode, "idempotent")
	list = members(t, a.admin, a.path("/members"))
	assert.NotContains(t, list, p1)
	assert.NotContains(t, list, p2)
}

// docs/adr/0030 D7: only a global administrator who administers the tenant
// makes a mapping or changes its role. Another administrator of the tenant is
// refused before anything is written — no mapping, no record, no membership,
// no idempotency key — and still removes a mapping, which only takes access
// away; a global administrator who does not administer the tenant is refused
// as well.
func TestOnlyAGlobalAdministratorMapsAGroup(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	yes := true
	group := uniqueSlug("everyone")
	person := a.person(t, address("everyone"), &yes, group)
	key := uuid.NewString()
	create := func(b *browser) *http.Response {
		return b.request(http.MethodPost, a.path("/group-mappings"), map[string]string{"group": group, "role": "admin"},
			withHeader("Idempotency-Key", key))
	}
	records := func() int64 { return scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, a.A) }
	mappings := func() int64 { return scalar[int64](t, `SELECT count(*) FROM group_mappings WHERE tenant_id = $1`, a.A) }

	before := records()
	refused := assertProblem(t, create(a.admin), http.StatusForbidden, "forbidden")
	assert.Equal(t, "mapping a group needs a global administrator who administers the tenant", refused["detail"])
	assert.Zero(t, mappings(), "no mapping")
	assert.Equal(t, before, records(), "no record")
	assert.NotContains(t, members(t, a.admin, a.path("/members")), person, "nobody joined")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM idempotency_keys WHERE key = $1`, key), "no key kept")

	// A global administrator who is a member of the tenant, not its administrator.
	require.NoError(t, fixtures(t).GlobalAdmin(ctx, a.MemberA))
	global := a.s.browser(t)
	global.mustLogin(a.names["memberA"], testPassword)
	assertProblem(t, create(global), http.StatusForbidden, "forbidden")
	assert.Zero(t, mappings())

	require.Equal(t, http.StatusOK, a.admin.request(http.MethodPut, a.path("/members/"+a.MemberA.String()+"/grant"),
		map[string]string{"role": "admin"}).StatusCode)
	res := create(global)
	require.Equal(t, http.StatusCreated, res.StatusCode, "a global administrator who administers the tenant")
	mapping := decode[apigen.GroupMapping](t, res)
	assert.Equal(t, apigen.RoleAdmin, members(t, a.admin, a.path("/members"))[person].Role)

	patch := a.path("/group-mappings/" + mapping.Id.String())
	before = records()
	refused = assertProblem(t, a.admin.request(http.MethodPatch, patch, map[string]string{"role": "viewer"}, withHeader("If-Match", `"1"`)),
		http.StatusForbidden, "forbidden")
	assert.Equal(t, "changing a group mapping needs a global administrator who administers the tenant", refused["detail"])
	assert.Equal(t, "admin", scalar[string](t, `SELECT role::text FROM group_mappings WHERE id = $1`, mapping.Id), "the role stays")
	assert.EqualValues(t, 1, scalar[int32](t, `SELECT version FROM group_mappings WHERE id = $1`, mapping.Id))
	assert.Equal(t, before, records(), "no record")
	assert.Equal(t, apigen.RoleAdmin, members(t, a.admin, a.path("/members"))[person].Role, "the membership as it was")

	changed := global.request(http.MethodPatch, patch, map[string]string{"role": "viewer"}, withHeader("If-Match", `"1"`))
	require.Equal(t, http.StatusOK, changed.StatusCode)
	assert.Equal(t, apigen.RoleViewer, members(t, a.admin, a.path("/members"))[person].Role)

	require.Equal(t, http.StatusNoContent, a.admin.request(http.MethodDelete, patch, nil).StatusCode,
		"removing a mapping only takes access away: any administrator of the tenant may")
	assert.Zero(t, mappings())
	assert.NotContains(t, members(t, a.admin, a.path("/members")), person)
}

// docs/adr/0034 D3, D4, docs/adr/0050 D3: a restricted project is its
// administrators' and its list's; restricting needs If-Match and a session,
// the list's entries a session, their removal a token too.
func TestProjectRestrictionAndAccessList(t *testing.T) {
	a := newAdminWorld(t)
	tokens := issueTokens(t, a.world)
	project := a.path("/projects/ALPHA")
	restrict := func(on bool, opts ...reqOpt) *http.Response {
		return a.admin.request(http.MethodPut, project+"/restriction", map[string]bool{"restricted": on}, opts...)
	}
	assertProblem(t, restrict(true), http.StatusPreconditionRequired, "precondition_required")
	assertProblem(t, a.s.do(t, caller{Token: a.token}, http.MethodPut, project+"/restriction", map[string]bool{"restricted": true}, "If-Match", `"1"`),
		http.StatusForbidden, "session_required")
	tag := a.admin.get(project).Header.Get("ETag")
	res := restrict(true, withHeader("If-Match", tag))
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.True(t, decode[apigen.Project](t, res).Restricted)
	tag = res.Header.Get("ETag")

	assertProblem(t, a.s.do(t, caller{Token: tokens.MemberA}, http.MethodGet, project, nil), http.StatusNotFound, "not_found")
	entry := project + "/access/" + a.MemberA.String()
	res = a.admin.request(http.MethodPut, entry, map[string]string{"role": "viewer"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	e := decode[apigen.ProjectAccessEntry](t, res)
	assert.Equal(t, a.MemberA, e.Person.Id)
	assert.Equal(t, apigen.ProjectAccessRoleViewer, e.Role)
	assert.Equal(t, http.StatusOK, a.s.do(t, caller{Token: tokens.MemberA}, http.MethodGet, project, nil).StatusCode, "on the list")
	assertProblem(t, a.s.do(t, caller{Token: tokens.MemberA}, http.MethodPatch, project, map[string]string{"name": "x"}, "If-Match", tag),
		http.StatusForbidden, "forbidden")
	assertProblem(t, a.admin.request(http.MethodPut, project+"/access/"+a.MemberB.String(), map[string]string{"role": "viewer"}),
		http.StatusNotFound, "person_not_found")
	assertProblem(t, a.s.do(t, caller{Token: a.token}, http.MethodPut, entry, map[string]string{"role": "member"}),
		http.StatusForbidden, "session_required")
	require.Equal(t, http.StatusOK, a.admin.request(http.MethodPut, entry, map[string]string{"role": "member"}).StatusCode)
	listed := decode[apigen.ProjectAccessList](t, a.admin.get(project+"/access"))
	require.Len(t, listed.Items, 1)
	assert.Equal(t, apigen.ProjectAccessRoleMember, listed.Items[0].Role)

	assert.Equal(t, http.StatusNoContent, a.s.do(t, caller{Token: a.token}, http.MethodDelete, entry, nil).StatusCode,
		"taking a person off the list only takes access away: a token may")
	assert.Equal(t, http.StatusNoContent, a.s.do(t, caller{Token: a.token}, http.MethodDelete, entry, nil).StatusCode, "idempotent")
	assertProblem(t, a.s.do(t, caller{Token: tokens.MemberA}, http.MethodGet, project, nil), http.StatusNotFound, "not_found")

	require.Equal(t, http.StatusOK, restrict(false, withHeader("If-Match", tag)).StatusCode)
	assert.Equal(t, http.StatusOK, a.s.do(t, caller{Token: tokens.MemberA}, http.MethodGet, project, nil).StatusCode, "open again")
	assert.EqualValues(t, 2, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE entity_id = $1 AND action = 'updated'
		AND after ? 'restricted'`, a.ProjectA))
}

type membershipData struct {
	PersonID  *uuid.UUID `json:"person_id"`
	ProjectID *uuid.UUID `json:"project_id"`
	MappingID *uuid.UUID `json:"mapping_id"`
}

func nextMembership(t *testing.T, s *stream) (membershipData, bool) {
	t.Helper()
	for {
		m, ok := s.next(t, 3*time.Second)
		if !ok {
			return membershipData{}, false
		}
		if m.Event != "membership.changed" {
			continue
		}
		var d membershipData
		require.NoError(t, json.Unmarshal([]byte(m.Data), &d))
		return d, true
	}
}

// docs/adr/0054 D2, D3: membership.changed reaches its audience with keys only:
// a grant every member, a mapping the administrators, an access entry the
// administrators and the person it names.
func TestMembershipEventsReachTheirAudience(t *testing.T) {
	a := newAdminWorld(t)
	a.globalAdmin(t)
	tokens := issueTokens(t, a.world)
	var env ticketEnv
	admin := env.openStream(t, a.s, caller{Token: tokens.AdminA}, a.SlugA, "")
	member := env.openStream(t, a.s, caller{Token: tokens.MemberA}, a.SlugA, "")
	viewer := env.openStream(t, a.s, caller{Token: tokens.ViewerA}, a.SlugA, "")
	yes := true

	person := a.person(t, address("evt"), &yes)
	require.Equal(t, http.StatusCreated, a.admin.request(http.MethodPost, a.path("/members"),
		map[string]string{"person": scalar[string](t, `SELECT email FROM users WHERE id = $1`, person), "role": "viewer"}).StatusCode)
	for name, s := range map[string]*stream{"admin": admin, "member": member, "viewer": viewer} {
		d, ok := nextMembership(t, s)
		require.True(t, ok, name)
		require.NotNil(t, d.PersonID, name)
		assert.Equal(t, person, *d.PersonID, name)
		assert.Nil(t, d.ProjectID)
		assert.Nil(t, d.MappingID)
	}

	res := a.admin.request(http.MethodPost, a.path("/group-mappings"), map[string]string{"group": uniqueSlug("nobodys"), "role": "member"})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	d, ok := nextMembership(t, admin)
	require.True(t, ok)
	require.NotNil(t, d.MappingID)
	assert.Equal(t, decode[apigen.GroupMapping](t, res).Id, *d.MappingID)

	require.Equal(t, http.StatusOK, a.admin.request(http.MethodPut, a.path("/projects/ALPHA/access/"+a.MemberA.String()),
		map[string]string{"role": "viewer"}).StatusCode)
	for name, s := range map[string]*stream{"admin": admin, "member": member} {
		d, ok := nextMembership(t, s)
		require.True(t, ok, name)
		require.NotNil(t, d.PersonID, name)
		require.NotNil(t, d.ProjectID, name)
		assert.Equal(t, a.MemberA, *d.PersonID)
		assert.Equal(t, a.ProjectA, *d.ProjectID)
	}
	_, ok = nextMembership(t, viewer)
	assert.False(t, ok, "the viewer hears of neither the mapping nor the access entry")
}

// docs/adr/0035 D2: every audit row written for a request carries the keyed
// hash of the client's address — the same address, the same hash — and a
// job's rows carry none.
func TestAuditRowsCarryTheSourceHash(t *testing.T) {
	ctx := context.Background()
	a := newAdminWorld(t)
	require.Equal(t, http.StatusOK, a.admin.request(http.MethodPut, a.path("/members/"+a.MemberA.String()+"/grant"),
		map[string]string{"role": "admin"}).StatusCode)
	require.Equal(t, http.StatusOK, a.admin.request(http.MethodPut, a.path("/members/"+a.MemberA.String()+"/grant"),
		map[string]string{"role": "member"}).StatusCode)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(DISTINCT source_hash) FROM audit_events WHERE tenant_id = $1
		AND entity_type = 'membership' AND octet_length(source_hash) = 32`, a.A), "two acts of one client, one hash")
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'
		AND source_hash IS NULL`, a.A))
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE action = 'logged_in' AND actor_user_id = $1
		AND octet_length(source_hash) = 32`, a.AdminA), "the login's too")

	_, err := openRuntime(t).ExpireIdempotencyKeys(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE actor_system LIKE 'system:%-expiry'
		AND source_hash IS NOT NULL`), "a job's rows have none")
	assert.EqualValues(t, 0, scalar[int64](t, `SELECT count(*) FROM audit_events WHERE source_hash IS NOT NULL
		AND before::text || after::text LIKE '%127.0.0.1%'`), "the address is in no row")
}

// docs/adr/0032 D6 (amended 2026-10-04): with an administrator group, the
// bootstrap tenant is seeded with its mapping and needs no local
// administrator; the group's members administer it from their first login.
func TestBootstrapSeedsTheAdministratorGroupsMapping(t *testing.T) {
	ctx := context.Background()
	iso := newIsolated(t)
	logger := (&recordingLogger{}).logger()
	p := bootstrap.Params{TenantSlug: "boot", TenantName: "Boot", AdminGroup: "cowork-admins"}
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, p, logger))
	require.NoError(t, bootstrap.Sync(ctx, iso.DB, p, logger), "a second start changes nothing")
	tenant := scalar2[uuid.UUID](t, iso, `SELECT id FROM tenants WHERE slug = 'boot'`)
	assert.Equal(t, "admin", scalar2[string](t, iso, `SELECT role::text FROM group_mappings WHERE tenant_id = $1 AND group_name = 'cowork-admins'`, tenant))
	assert.EqualValues(t, 0, audit(t, iso, `entity_type = 'membership'`), "no grant: there is no local administrator")
	assert.EqualValues(t, 1, audit(t, iso, `entity_type = 'group_mapping' AND action = 'created' AND actor_system = 'system:bootstrap'`))
	n, err := iso.F.QueryCount(ctx, `SELECT count(*) FROM memberships`)
	require.NoError(t, err)
	assert.Zero(t, n)

	s := newAPI(t, withLogin, iso.option, devGate(dexProvider(t)))
	ada := s.browser(t)
	require.Equal(t, "/", ada.oidcLogin("ada@example.com", "/").Header.Get("Location"))
	me := decode[apigen.Me](t, ada.get("/api/v1/me"))
	require.Len(t, me.Memberships, 1)
	assert.Equal(t, apigen.RoleAdmin, me.Memberships[0].Role, "the administrator group administers the bootstrap tenant")
	assert.Equal(t, "boot", me.Memberships[0].Tenant.Slug)
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceMapping, Role: apigen.RoleAdmin}}, me.Memberships[0].Origins)
}
