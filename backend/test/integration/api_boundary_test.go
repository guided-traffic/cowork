//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apispec "github.com/guided-traffic/cowork/backend/api"
)

// teamRoute is one operation of the API document under a team — under the
// team family, or under its deprecated twin in the family before
// (docs/adr/0023 D1) — with its path parameters filled in but the team's,
// which stays param. sessionOnly marks the operations that take the session
// cookie alone: a token is refused on them before any team is looked at
// (docs/adr/0035 D5).
type teamRoute struct {
	method, path, param string
	sessionOnly         bool
}

// at is the route's path with slug as its team.
func (r teamRoute) at(slug string) string { return strings.ReplaceAll(r.path, r.param, slug) }

// rest is the route's path after its team: the same for a team path and its
// twin.
func (r teamRoute) rest() string {
	_, rest, _ := strings.Cut(r.path, r.param)
	return rest
}

// teamRoutes walks the document for every operation under /teams/{team} and
// every twin under /tenants/{tenant}, and fills in the other path
// parameters, so a new route family is covered the day it exists, under both
// names. The team stays its parameter.
func teamRoutes(t *testing.T) []teamRoute {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(apispec.Document)
	require.NoError(t, err)
	params := map[string]string{
		"{project}": "BETA", "{number}": "1", "{question}": "1", "{type}": "blocks", "{other}": "BETA-2",
		"{key}": "BETA-1", "{username}": "somebody",
	}
	// One id per parameter for the whole walk, so that a team path and its
	// twin are filled in alike.
	for _, id := range []string{"{comment}", "{entry}", "{attachment}", "{token_id}", "{person_id}", "{mapping_id}"} {
		params[id] = uuid.Must(uuid.NewV7()).String()
	}
	paths := doc.Paths.InMatchingOrder()
	sort.Strings(paths)
	var routes []teamRoute
	for _, path := range paths {
		param := ""
		switch {
		case strings.Contains(path, "{team}"):
			param = "{team}"
		case strings.Contains(path, "{tenant}"):
			param = "{tenant}"
		default:
			continue
		}
		for method, op := range doc.Paths.Value(path).Operations() {
			url := path
			for k, v := range params {
				url = strings.ReplaceAll(url, k, v)
			}
			sessionOnly := op.Security != nil && len(*op.Security) == 1
			if sessionOnly {
				_, sessionOnly = (*op.Security)[0]["sessionCookie"]
			}
			routes = append(routes, teamRoute{method: method, path: url, param: param, sessionOnly: sessionOnly})
		}
	}
	return routes
}

// countByParam counts the routes of each family: {team}'s and its twins'.
func countByParam(routes []teamRoute) map[string]int {
	n := map[string]int{}
	for _, r := range routes {
		n[r.param]++
	}
	return n
}

// Every route under a team refuses a person of another team with the very
// answer an unknown team gets (docs/adr/0023 D5, docs/adr/0005 D3): the
// refusal is asserted, not the absence of data. The routes come from the
// document, so a new route family is covered the day it exists — on its team
// path and on its twin under the family before (docs/adr/0023 D1).
func TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	strip := func(t *testing.T, res *http.Response) map[string]any {
		t.Helper()
		body := problemBody(t, res)
		delete(body, "instance")
		delete(body, "request_id")
		return body
	}
	routes := teamRoutes(t)
	for _, r := range routes {
		var body any
		if r.method != http.MethodGet && r.method != http.MethodDelete {
			body = map[string]any{}
		}
		other := e.s.do(t, member, r.method, r.at(e.SlugB), body)
		unknown := e.s.do(t, member, r.method, r.at("no-such-tenant-9"), body)
		// A route that takes a session only refuses a token before it looks at the
		// team, so there is nothing to tell the two apart; the session variant of
		// this test walks those routes into the boundary.
		want := http.StatusNotFound
		if r.sessionOnly {
			want = http.StatusForbidden
		}
		require.Equal(t, want, other.StatusCode, "%s %s", r.method, r.path)
		require.Equal(t, want, unknown.StatusCode, "%s %s", r.method, r.path)
		a, b := strip(t, other), strip(t, unknown)
		aj, _ := json.Marshal(a)
		bj, _ := json.Marshal(b)
		assert.JSONEq(t, string(bj), string(aj), "%s %s answers another team like no team", r.method, r.path)
	}
	n := countByParam(routes)
	assert.Greater(t, n["{team}"], 40, "the team's routes are all walked")
	assert.Equal(t, n["{team}"]-1, n["{tenant}"], "and every twin, the resolver aside, which has none")
}
