package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/adr/0023 D1, docs/adr/0046 D7: a request to a twin under the family
// before is answered as its team path — the family itself and every path
// under it rewritten, the query kept —; the team family, another family and a
// path that only begins like the family before are left as they are; and the
// request as sent, which the log middleware holds, keeps its path.
func TestATwinIsAnsweredAsItsTeamPath(t *testing.T) {
	for name, c := range map[string]struct{ target, path, escaped, query string }{
		"the family itself": {"/api/v1/tenants", "/api/v1/teams", "", ""},
		"a path under it": {"/api/v1/tenants/acme/projects/COW/tickets?state=filed&limit=5", "/api/v1/teams/acme/projects/COW/tickets", "",
			"state=filed&limit=5"},
		"a trailing slash": {"/api/v1/tenants/", "/api/v1/teams/", "", ""},
		// The client's escaping of the rest is kept; an escaped family is
		// read as the family.
		"an escaped segment":    {"/api/v1/tenants/acme/projects/C%4FW", "/api/v1/teams/acme/projects/COW", "/api/v1/teams/acme/projects/C%4FW", ""},
		"an escaped family":     {"/api/v1/%74enants/acme", "/api/v1/teams/acme", "", ""},
		"the team family":       {"/api/v1/teams/acme?team=acme", "/api/v1/teams/acme", "", "team=acme"},
		"another family":        {"/api/v1/me/inbox?tenant=acme", "/api/v1/me/inbox", "", "tenant=acme"},
		"a family like it":      {"/api/v1/tenantsX/acme", "/api/v1/tenantsX/acme", "", ""},
		"a family that ends it": {"/api/v1/tenants-old", "/api/v1/tenants-old", "", ""},
	} {
		r := httptest.NewRequest(http.MethodGet, c.target, nil)
		sent := r.URL.String()
		got := asTeamPath(r)
		escaped := c.escaped
		if escaped == "" {
			escaped = c.path
		}
		assert.Equal(t, c.path, got.URL.Path, name)
		assert.Equal(t, escaped, got.URL.EscapedPath(), name)
		assert.Equal(t, c.query, got.URL.RawQuery, name)
		assert.Equal(t, sent, r.URL.String(), "%s: the request as sent keeps its path", name)
		assert.Equal(t, r.Context(), got.Context(), name)
	}
}

// Every twin of the document is routed as its team path: the rewrite reaches
// every path of the family before, so that no twin's own operation is ever
// routed — the boundary, the maps of operations and the handler are the team
// path's alone (docs/adr/0023 D1, D5).
func TestNoTwinIsEverRouted(t *testing.T) {
	h := documentHandler(t)
	param := regexp.MustCompile(`\{[^}]+\}`)
	twins := 0
	for _, path := range h.doc.Paths.InMatchingOrder() {
		if path != tenantFamily && !strings.HasPrefix(path, tenantFamily+"/") {
			continue
		}
		team := teamFamily + strings.Replace(strings.TrimPrefix(path, tenantFamily), "{tenant}", "{team}", 1)
		require.NotNil(t, h.doc.Paths.Value(team), "%s has a team path", path)
		for method := range h.doc.Paths.Value(path).Operations() {
			twins++
			r := httptest.NewRequest(method, param.ReplaceAllString(path, "x1"), nil)
			route, params, err := h.router.FindRoute(asTeamPath(r))
			require.NoError(t, err, "%s %s", method, path)
			assert.Equal(t, team, route.Path, "%s %s", method, path)
			assert.False(t, slices.Contains(route.Operation.Tags, "tenants"), "%s %s is routed to its twin", method, path)
			if strings.Contains(path, "{tenant}") {
				assert.Contains(t, params, "team", "%s %s passes the boundary by its team", method, path)
			}
		}
	}
	assert.Greater(t, twins, 100, "every twin is walked")
}
