//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// overseer is a global administrator with a local account and no role in any
// tenant of the world, logged in in a browser, with an admin-scope token
// besides (docs/adr/0034 D2).
type overseer struct {
	world
	s       apiServer
	names   map[string]string
	id      uuid.UUID
	browser *browser
	token   string
}

func newOverseer(t *testing.T) overseer {
	t.Helper()
	ctx := context.Background()
	w := newWorld(t)
	o := overseer{world: w, names: withAccounts(t, w), s: newAPI(t, withLogin)}
	o.id = globalAdministrator(t, "overseer")
	o.browser = o.s.browser(t)
	o.browser.mustLogin(usernameOf(t, o.id), testPassword)
	token, _, err := fixtures(t).Token(ctx, fixture.TokenSpec{UserID: o.id, Scope: domain.ScopeAdmin})
	require.NoError(t, err)
	o.token = token
	return o
}

// globalAdministrator makes a global administrator with a local account that
// no tenant manages, as the local administrator of the configuration is.
func globalAdministrator(t *testing.T, prefix string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	f := fixtures(t)
	id, err := f.Person(ctx, uniqueSlug(prefix), "Global "+prefix)
	require.NoError(t, err)
	require.NoError(t, f.GlobalAdmin(ctx, id))
	require.NoError(t, f.Account(ctx, id, testPassword, uuid.Nil, false))
	return id
}

func tenantPath(slug, rest string) string { return "/api/v1/teams/" + slug + rest }

// selfGrant is the grant of a role to the browser's own person.
func selfGrant(b *browser, slug string, person uuid.UUID, role string, opts ...reqOpt) *http.Response {
	return b.request(http.MethodPut, tenantPath(slug, "/members/"+person.String()+"/grant"), map[string]string{"role": role}, opts...)
}

// docs/adr/0034 D2, docs/adr/0023 D5: a global administrator without a role in
// a tenant reaches its administration — the tenant and its settings, the
// members, the group mappings — and nothing else of it: every other route of
// the tenant answers exactly like an unknown tenant. The routes come from the
// document, so a new route family is held to it the day it exists. A token of
// the same person, an agent-marked session and a person who is not a global
// administrator reach nothing.
func TestAGlobalAdministratorWithoutARoleSeesTheAdministrationOnly(t *testing.T) {
	o := newOverseer(t)
	b := o.browser

	res := b.get(tenantPath(o.SlugA, ""))
	require.Equal(t, http.StatusOK, res.StatusCode, "the tenant and its settings")
	assert.Equal(t, "Team A", decode[apigen.Team](t, res).Name)
	list := members(t, b, tenantPath(o.SlugA, "/members"))
	require.Contains(t, list, o.AdminA)
	assert.Equal(t, apigen.RoleAdmin, list[o.AdminA].Role)
	assert.NotContains(t, list, o.id, "the overseer is no member")
	for id, m := range list {
		assert.True(t, m.Email.IsNull(), "no address for the overseer: %s", id)
	}
	require.NoError(t, fixtures(t).Exec(context.Background(),
		`INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, $2, 'member')`, o.A, uniqueSlug("watched")))
	mappings := b.get(tenantPath(o.SlugA, "/group-mappings"))
	require.Equal(t, http.StatusOK, mappings.StatusCode, "the group mappings")
	assert.Len(t, decode[apigen.GroupMappingList](t, mappings).Items, 1)

	strip := func(res *http.Response) map[string]any {
		body := problemBody(t, res)
		delete(body, "instance")
		delete(body, "request_id")
		return body
	}
	reached := map[string]bool{}
	for _, r := range teamRoutes(t) {
		var body any
		if r.method != http.MethodGet && r.method != http.MethodDelete {
			body = map[string]any{}
		}
		overseen := b.request(r.method, r.at(o.SlugA), body)
		if overseen.StatusCode != http.StatusNotFound {
			reached[r.method+" "+r.rest()] = true
			continue
		}
		unknown := b.request(r.method, r.at("no-such-tenant-9"), body)
		require.Equal(t, http.StatusNotFound, unknown.StatusCode, "%s %s", r.method, r.path)
		assert.Equal(t, strip(unknown), strip(overseen), "%s %s answers the overseer like no tenant", r.method, r.path)
	}
	grantRoute := ""
	for route := range reached {
		if strings.HasPrefix(route, "PUT /members/") {
			grantRoute = route
		}
	}
	assert.Equal(t, map[string]bool{"GET ": true, "GET /members": true, "GET /group-mappings": true, grantRoute: true}, reached,
		"the tenant, its members, its mappings and the grant, nothing else")
	require.NotEmpty(t, grantRoute)
	for _, rest := range []string{"/projects", "/tickets", "/time-entries", "/audit", "/accounts", "/events", "/chat",
		"/projects/ALPHA/tickets/1/attachments"} {
		assertProblem(t, b.get(tenantPath(o.SlugA, rest)), http.StatusNotFound, "not_found")
	}

	assertProblem(t, o.s.do(t, caller{Token: o.token}, http.MethodGet, tenantPath(o.SlugA, "/members"), nil),
		http.StatusNotFound, "not_found")
	assertProblem(t, b.get(tenantPath(o.SlugA, "/members"), withHeader("X-Cowork-Agent", "chat/model/conversation")),
		http.StatusNotFound, "not_found")
	assertProblem(t, b.get(tenantPath(o.SlugA, ""), withHeader("X-Cowork-Agent", "chat/model/conversation")),
		http.StatusNotFound, "not_found")

	stranger := o.s.browser(t)
	stranger.mustLogin(o.names["memberB"], testPassword)
	assertProblem(t, stranger.get(tenantPath(o.SlugA, "/members")), http.StatusNotFound, "not_found")
	assertProblem(t, stranger.get(tenantPath(o.SlugA, "")), http.StatusNotFound, "not_found")
	assertProblem(t, selfGrant(stranger, o.SlugA, o.MemberB, "admin"), http.StatusNotFound, "not_found")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1 AND user_id = $2`, o.A, o.MemberB))
}

// docs/adr/0034 D2, docs/adr/0035 D5, docs/adr/0026: a global administrator
// grants themselves a role in a tenant in which they hold none — a marked
// grant like any other, recorded in the tenant with them as its actor and
// announced to its members — in a browser session only, never to anybody
// else; afterwards the tenant answers them as it answers any administrator.
func TestAGlobalAdministratorGrantsThemselvesARole(t *testing.T) {
	o := newOverseer(t)
	b := o.browser
	tk := issueTokens(t, o.world)
	var env ticketEnv
	memberStream := env.openStream(t, o.s, caller{Token: tk.MemberA}, o.SlugA, "")
	records := func() int64 {
		return scalar[int64](t, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND entity_type = 'membership'`, o.A)
	}
	before := records()

	assertProblem(t, o.s.do(t, caller{Token: o.token}, http.MethodPut, tenantPath(o.SlugA, "/members/"+o.id.String()+"/grant"),
		map[string]string{"role": "admin"}), http.StatusForbidden, "session_required")
	assertProblem(t, selfGrant(b, o.SlugA, o.id, "admin", withHeader("X-Cowork-Agent", "chat/model/conversation")),
		http.StatusForbidden, "agent_forbidden")
	assertProblem(t, selfGrant(b, o.SlugA, o.MemberA, "admin"), http.StatusForbidden, "forbidden")
	assertProblem(t, b.request(http.MethodPost, tenantPath(o.SlugA, "/members"),
		map[string]string{"person": usernameOf(t, o.id), "role": "admin"}), http.StatusNotFound, "not_found")
	assert.Equal(t, before, records(), "nothing recorded")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1 AND user_id = $2`, o.A, o.id))

	res := selfGrant(b, o.SlugA, o.id, "admin")
	require.Equal(t, http.StatusOK, res.StatusCode)
	m := decode[apigen.Member](t, res)
	assert.Equal(t, o.id, m.Person.Id)
	assert.Equal(t, apigen.RoleAdmin, m.Role)
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceGrant, Role: apigen.RoleAdmin}}, m.Origins)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events
		WHERE tenant_id = $1 AND actor_user_id = $2 AND entity_type = 'membership' AND action = 'created'
		  AND after->>'user' = $3 AND after->>'role' = 'admin' AND after->>'source' = 'grant'`, o.A, o.id, o.id.String()),
		"the tenant's record names the administrator as the actor of their own grant")
	d, ok := nextMembership(t, memberStream)
	require.True(t, ok, "the tenant's members hear of it")
	require.NotNil(t, d.PersonID)
	assert.Equal(t, o.id, *d.PersonID)

	require.Equal(t, http.StatusOK, selfGrant(b, o.SlugA, o.id, "admin").StatusCode, "repeating it changes nothing")
	assert.Equal(t, before+1, records())
	projects := b.get(tenantPath(o.SlugA, "/projects"))
	require.Equal(t, http.StatusOK, projects.StatusCode, "the tenant's work, as its administrator")
	items := decode[apigen.ProjectList](t, projects).Items
	keys := make([]string, 0, len(items))
	for _, p := range items {
		keys = append(keys, p.Key)
	}
	assert.Contains(t, keys, "ALPHA")
	assert.Equal(t, http.StatusOK, b.get(tenantPath(o.SlugA, "/tickets")).StatusCode)
	assert.Equal(t, http.StatusOK, b.get(tenantPath(o.SlugA, "/audit")).StatusCode)
	withAddresses := members(t, b, tenantPath(o.SlugA, "/members"))
	assert.Equal(t, apigen.RoleAdmin, withAddresses[o.id].Role)
	assert.Equal(t, http.StatusOK, o.s.do(t, caller{Token: o.token}, http.MethodGet, tenantPath(o.SlugA, "/members"), nil).StatusCode,
		"a role reaches the person's token too")
	assertProblem(t, b.get(tenantPath(o.SlugB, "/projects")), http.StatusNotFound, "not_found")
}

// docs/adr/0034 D1, D2, docs/security/identity-provider.md H-29,
// docs/security/local-accounts.md H-32: a tenant left without an administrator
// who can log in is given one again by a global administrator's grant to
// themselves, which takes no administrator away and so meets no last_admin —
// in any role they pick — after which the tenant's acts are an
// administrator's again.
func TestAStrandedTenantIsRecoveredByTheSelfGrant(t *testing.T) {
	ctx := context.Background()
	o := newOverseer(t)
	member := o.s.browser(t)
	member.mustLogin(o.names["memberA"], testPassword)
	require.NoError(t, fixtures(t).Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, o.AdminA))
	assertProblem(t, member.request(http.MethodPut, tenantPath(o.SlugA, "/members/"+o.MemberA.String()+"/grant"),
		map[string]string{"role": "admin"}), http.StatusForbidden, "forbidden")

	watcher := globalAdministrator(t, "watcher")
	w := o.s.browser(t)
	w.mustLogin(usernameOf(t, watcher), testPassword)
	viewed := selfGrant(w, o.SlugA, watcher, "viewer")
	require.Equal(t, http.StatusOK, viewed.StatusCode, "a grant that takes nobody's role away is never last_admin")
	assert.Equal(t, apigen.RoleViewer, decode[apigen.Member](t, viewed).Role)

	require.Equal(t, http.StatusOK, selfGrant(o.browser, o.SlugA, o.id, "admin").StatusCode)
	promoted := o.browser.request(http.MethodPut, tenantPath(o.SlugA, "/members/"+o.MemberA.String()+"/grant"),
		map[string]string{"role": "admin"})
	require.Equal(t, http.StatusOK, promoted.StatusCode, "the tenant's administration acts again")
	assert.Equal(t, apigen.RoleAdmin, decode[apigen.Member](t, promoted).Role)
	require.Equal(t, http.StatusNoContent, o.browser.request(http.MethodDelete, tenantPath(o.SlugA, "/members/"+o.id.String()+"/grant"), nil).StatusCode,
		"the overseer steps back once the tenant has an administrator of its own")
	assert.Zero(t, scalar[int64](t, `SELECT count(*) FROM memberships WHERE tenant_id = $1 AND user_id = $2`, o.A, o.id))
	assert.Equal(t, http.StatusOK, o.browser.get(tenantPath(o.SlugA, "/members")).StatusCode, "and oversees it as before")
}

// docs/adr/0034 D2 (the owner's answer of 2026-10-04): a global administrator
// who holds a role below admin in a tenant — a viewer, in a tenant left without
// an administrator who can log in — raises their own grant to admin, recorded
// as the change of the grant and announced to the members, after which the
// tenant's administration is theirs. A viewer who is no global administrator
// cannot, the raise reaches nobody else's grant, and a token and an agent are
// refused it.
func TestAGlobalAdministratorWithALowerRoleRaisesTheirOwnGrant(t *testing.T) {
	ctx := context.Background()
	o := newOverseer(t)
	tk := issueTokens(t, o.world)
	require.NoError(t, fixtures(t).Member(ctx, o.A, o.id, domain.RoleViewer))
	require.NoError(t, fixtures(t).Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, o.AdminA))
	var env ticketEnv
	memberStream := env.openStream(t, o.s, caller{Token: tk.MemberA}, o.SlugA, "")
	grantOf := func(person uuid.UUID) string {
		return scalar[string](t, `SELECT role::text FROM memberships WHERE tenant_id = $1 AND user_id = $2 AND source = 'grant'`, o.A, person)
	}

	viewer := o.s.browser(t)
	viewer.mustLogin(o.names["viewerA"], testPassword)
	assertProblem(t, selfGrant(viewer, o.SlugA, o.ViewerA, "admin"), http.StatusForbidden, "forbidden")
	assert.Equal(t, "viewer", grantOf(o.ViewerA), "a viewer who is no global administrator stays one")
	assertProblem(t, selfGrant(o.browser, o.SlugA, o.MemberA, "admin"), http.StatusForbidden, "forbidden")
	assertProblem(t, o.s.do(t, caller{Token: o.token}, http.MethodPut, tenantPath(o.SlugA, "/members/"+o.id.String()+"/grant"),
		map[string]string{"role": "admin"}), http.StatusForbidden, "session_required")
	assertProblem(t, selfGrant(o.browser, o.SlugA, o.id, "admin", withHeader("X-Cowork-Agent", "chat/model/conversation")),
		http.StatusForbidden, "agent_forbidden")
	assert.Equal(t, "viewer", grantOf(o.id))

	res := selfGrant(o.browser, o.SlugA, o.id, "admin")
	require.Equal(t, http.StatusOK, res.StatusCode)
	m := decode[apigen.Member](t, res)
	assert.Equal(t, apigen.RoleAdmin, m.Role)
	assert.Equal(t, []apigen.MembershipOrigin{{Source: apigen.MembershipSourceGrant, Role: apigen.RoleAdmin}}, m.Origins)
	assert.EqualValues(t, 1, scalar[int64](t, `SELECT count(*) FROM audit_events
		WHERE tenant_id = $1 AND actor_user_id = $2 AND entity_type = 'membership' AND action = 'updated'
		  AND before->>'role' = 'viewer' AND after->>'role' = 'admin'`, o.A, o.id),
		"the tenant's record names the administrator as the actor of their own raise")
	d, ok := nextMembership(t, memberStream)
	require.True(t, ok, "the tenant's members hear of it")
	require.NotNil(t, d.PersonID)
	assert.Equal(t, o.id, *d.PersonID)

	assert.Equal(t, http.StatusOK, o.browser.get(tenantPath(o.SlugA, "/audit")).StatusCode, "an administrator's read")
	promoted := o.browser.request(http.MethodPut, tenantPath(o.SlugA, "/members/"+o.MemberA.String()+"/grant"),
		map[string]string{"role": "admin"})
	require.Equal(t, http.StatusOK, promoted.StatusCode, "the tenant's administration acts again")
}

// docs/adr/0034 D2: a global administrator lists every tenant of the
// installation with the role they hold in each, null where none, a page at a
// time; nobody else does, and no token, and no agent.
func TestOnlyAGlobalAdministratorListsEveryTenant(t *testing.T) {
	o := newOverseer(t)
	all := func(b *browser) map[string]apigen.TeamSummary {
		out := map[string]apigen.TeamSummary{}
		next := ""
		for {
			path := "/api/v1/teams?limit=200"
			if next != "" {
				path += "&cursor=" + url.QueryEscape(next)
			}
			res := b.get(path)
			require.Equal(t, http.StatusOK, res.StatusCode)
			page := decode[apigen.TeamSummaryList](t, res)
			for _, item := range page.Items {
				out[item.Slug] = item
			}
			if page.NextCursor.IsNull() {
				return out
			}
			next = page.NextCursor.MustGet()
		}
	}
	tenants := all(o.browser)
	require.Contains(t, tenants, o.SlugA)
	require.Contains(t, tenants, o.SlugB)
	assert.Equal(t, "Team A", tenants[o.SlugA].Name)
	assert.True(t, tenants[o.SlugA].Role.IsNull(), "no role in A")
	require.Equal(t, http.StatusOK, selfGrant(o.browser, o.SlugA, o.id, "member").StatusCode)
	tenants = all(o.browser)
	assert.Equal(t, apigen.RoleMember, tenants[o.SlugA].Role.MustGet())
	assert.True(t, tenants[o.SlugB].Role.IsNull())

	first := o.browser.get("/api/v1/teams?limit=1")
	require.Equal(t, http.StatusOK, first.StatusCode)
	page := decode[apigen.TeamSummaryList](t, first)
	require.Len(t, page.Items, 1)
	require.False(t, page.NextCursor.IsNull(), "more than one tenant: a next page")
	second := decode[apigen.TeamSummaryList](t, o.browser.get("/api/v1/teams?limit=1&cursor="+url.QueryEscape(page.NextCursor.MustGet())))
	require.Len(t, second.Items, 1)
	assert.Less(t, page.Items[0].Slug, second.Items[0].Slug, "by slug")

	member := o.s.browser(t)
	member.mustLogin(o.names["adminA"], testPassword)
	assertProblem(t, member.get("/api/v1/teams"), http.StatusForbidden, "forbidden")
	assertProblem(t, o.s.do(t, caller{Token: o.token}, http.MethodGet, "/api/v1/teams", nil), http.StatusForbidden, "session_required")
	assertProblem(t, o.browser.get("/api/v1/teams", withHeader("X-Cowork-Agent", "chat/model/conversation")),
		http.StatusForbidden, "agent_forbidden")
	assertProblem(t, o.s.do(t, caller{}, http.MethodGet, "/api/v1/teams", nil), http.StatusUnauthorized, "unauthenticated")
}

// docs/adr/0034 D2, docs/adr/0021 D6 (migration 26): row-level security shows a
// global administrator every tenant, admits their grant to themselves in any
// role and the change of their own grant's role in the tenant; it opens nothing
// else — with no tenant set, their transaction reads
// no tenant's members, mappings, projects, tickets, time, attachments,
// comments or audit rows, and a deactivated one reads no tenant they do not
// belong to and changes no grant.
func TestPoliciesOfTheGlobalAdministratorsReach(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	f := fixtures(t)
	boss, err := f.Person(ctx, uniqueSlug("boss"), "Boss")
	require.NoError(t, err)
	require.NoError(t, f.GlobalAdmin(ctx, boss))
	_, _, err = f.Ticket(ctx, w.A, w.ProjectA, w.AdminA, "Seen by nobody outside")
	require.NoError(t, err)
	require.NoError(t, f.Exec(ctx, `INSERT INTO group_mappings (tenant_id, group_name, role) VALUES ($1, $2, 'member')`, w.A, uniqueSlug("g")))
	global := ctxOf(boss, uuid.Nil)

	tenants := `SELECT count(*) FROM tenants WHERE id IN ($1, $2)`
	assert.EqualValues(t, 2, count(t, global, tenants, w.A, w.B), "every tenant")
	assert.EqualValues(t, 1, count(t, ctxOf(w.MemberA, uuid.Nil), tenants, w.A, w.B), "a member their own")
	assert.EqualValues(t, 0, count(t, settings{}, tenants, w.A, w.B))
	for table, sql := range map[string]string{
		"memberships":    `SELECT count(*) FROM memberships WHERE tenant_id = $1`,
		"group_mappings": `SELECT count(*) FROM group_mappings WHERE tenant_id = $1`,
		"projects":       `SELECT count(*) FROM projects WHERE tenant_id = $1`,
		"tickets":        `SELECT count(*) FROM tickets WHERE tenant_id = $1`,
		"time_entries":   `SELECT count(*) FROM time_entries WHERE tenant_id = $1`,
		"attachments":    `SELECT count(*) FROM attachments WHERE tenant_id = $1`,
		"comments":       `SELECT count(*) FROM comments WHERE tenant_id = $1`,
		"audit_events":   `SELECT count(*) FROM audit_events WHERE tenant_id = $1`,
	} {
		assert.EqualValues(t, 0, count(t, global, sql, w.A), "%s stays closed outside the tenant's transaction", table)
	}
	assert.Positive(t, count(t, ctxOf(w.AdminA, w.A), `SELECT count(*) FROM tickets WHERE tenant_id = $1`, w.A), "the ticket exists")

	grant := `INSERT INTO memberships (tenant_id, user_id, role, source) VALUES ($1, $2, $3, $4)`
	affects(t, 1, ctxOf(boss, w.B), grant, w.B, boss, "viewer", "grant")
	affects(t, 1, ctxOf(boss, w.B), grant, w.B, boss, "member", "grant")
	affects(t, 1, global, grant, w.B, boss, "admin", "grant")
	denied(t, ctxOf(boss, w.B), grant, w.B, w.MemberA, "viewer", "grant")
	denied(t, ctxOf(boss, w.B), grant, w.B, boss, "viewer", "mapping")
	denied(t, ctxOf(w.MemberA, w.B), grant, w.B, w.MemberA, "viewer", "grant")

	// Their own grant's role inside the tenant, raised: the global administrator's, nobody else's.
	require.NoError(t, f.Member(ctx, w.B, boss, domain.RoleViewer))
	raise := `UPDATE memberships SET role = 'admin', version = version + 1 WHERE tenant_id = $1 AND user_id = $2 AND source = 'grant'`
	affects(t, 1, ctxOf(boss, w.B), raise, w.B, boss)
	affects(t, 0, ctxOf(boss, w.B), raise, w.B, w.MemberB)
	affects(t, 0, ctxOf(boss, w.A), raise, w.B, boss)
	affects(t, 0, ctxOf(w.MemberB, w.B), raise, w.B, w.MemberB)

	require.NoError(t, f.Exec(ctx, `UPDATE users SET deactivated_at = now() WHERE id = $1`, boss))
	affects(t, 0, ctxOf(boss, w.B), raise, w.B, boss)
	assert.EqualValues(t, 1, count(t, global, tenants, w.A, w.B), "a deactivated global administrator reads only the tenant they belong to")
	denied(t, ctxOf(boss, w.A), grant, w.A, boss, "viewer", "grant")
}
