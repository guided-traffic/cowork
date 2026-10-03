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

// tenantRoute is one operation of the API document under a tenant, with its
// path parameters filled in but the tenant's. sessionOnly marks the operations
// that take the session cookie alone: a token is refused on them before any
// tenant is looked at (docs/adr/0035 D5).
type tenantRoute struct {
	method, path string
	sessionOnly  bool
}

// tenantRoutes walks the document for every operation under /tenants/{tenant}
// and fills in the other path parameters, so a new route family is covered the
// day it exists. The tenant stays `{tenant}`.
func tenantRoutes(t *testing.T) []tenantRoute {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(apispec.Document)
	require.NoError(t, err)
	params := map[string]string{
		"{project}": "BETA", "{number}": "1", "{question}": "1", "{type}": "blocks", "{other}": "BETA-2",
		"{key}": "BETA-1", "{username}": "somebody",
	}
	paths := doc.Paths.InMatchingOrder()
	sort.Strings(paths)
	var routes []tenantRoute
	for _, path := range paths {
		if !strings.Contains(path, "{tenant}") {
			continue
		}
		for method, op := range doc.Paths.Value(path).Operations() {
			url := path
			for k, v := range params {
				url = strings.ReplaceAll(url, k, v)
			}
			for _, id := range []string{"{comment}", "{entry}", "{attachment}", "{token_id}"} {
				url = strings.ReplaceAll(url, id, uuid.Must(uuid.NewV7()).String())
			}
			sessionOnly := op.Security != nil && len(*op.Security) == 1
			if sessionOnly {
				_, sessionOnly = (*op.Security)[0]["sessionCookie"]
			}
			routes = append(routes, tenantRoute{method: method, path: url, sessionOnly: sessionOnly})
		}
	}
	return routes
}

// Every route under a tenant refuses a person of another tenant with the
// very answer an unknown tenant gets (docs/adr/0023 D5, docs/adr/0005 D3):
// the refusal is asserted, not the absence of data. The routes come from the
// document, so a new route family is covered the day it exists.
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
	routes := tenantRoutes(t)
	for _, r := range routes {
		var body any
		if r.method != http.MethodGet && r.method != http.MethodDelete {
			body = map[string]any{}
		}
		other := e.s.do(t, member, r.method, strings.ReplaceAll(r.path, "{tenant}", e.SlugB), body)
		unknown := e.s.do(t, member, r.method, strings.ReplaceAll(r.path, "{tenant}", "no-such-tenant-9"), body)
		// A route that takes a session only refuses a token before it looks at the
		// tenant, so there is nothing to tell the two apart; the session variant of
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
		assert.JSONEq(t, string(bj), string(aj), "%s %s answers another tenant like no tenant", r.method, r.path)
	}
	assert.Greater(t, len(routes), 40, "the tenant's routes are all walked")
}
