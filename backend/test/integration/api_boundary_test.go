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

// Every route under a tenant refuses a person of another tenant with the
// very answer an unknown tenant gets (docs/adr/0023 D5, docs/adr/0005 D3):
// the refusal is asserted, not the absence of data. The routes come from the
// document, so a new route family is covered the day it exists.
func TestEveryTenantRouteRefusesAnotherTenantLikeNoTenant(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	doc, err := openapi3.NewLoader().LoadFromData(apispec.Document)
	require.NoError(t, err)

	params := map[string]string{
		"{project}": "BETA", "{number}": "1", "{question}": "1", "{type}": "blocks", "{other}": "BETA-2",
		"{key}": "BETA-1",
	}
	strip := func(t *testing.T, res *http.Response) map[string]any {
		t.Helper()
		body := problemBody(t, res)
		delete(body, "instance")
		delete(body, "request_id")
		return body
	}
	paths := doc.Paths.InMatchingOrder()
	sort.Strings(paths)
	families := 0
	for _, path := range paths {
		if !strings.Contains(path, "{tenant}") {
			continue
		}
		for method := range doc.Paths.Value(path).Operations() {
			url := path
			for k, v := range params {
				url = strings.ReplaceAll(url, k, v)
			}
			for _, id := range []string{"{comment}", "{entry}", "{attachment}", "{token_id}"} {
				url = strings.ReplaceAll(url, id, uuid.Must(uuid.NewV7()).String())
			}
			var body any
			if method != http.MethodGet && method != http.MethodDelete {
				body = map[string]any{}
			}
			other := e.s.do(t, member, method, strings.ReplaceAll(url, "{tenant}", e.SlugB), body)
			unknown := e.s.do(t, member, method, strings.ReplaceAll(url, "{tenant}", "no-such-tenant-9"), body)
			require.Equal(t, http.StatusNotFound, other.StatusCode, "%s %s", method, path)
			require.Equal(t, http.StatusNotFound, unknown.StatusCode, "%s %s", method, path)
			a, b := strip(t, other), strip(t, unknown)
			aj, _ := json.Marshal(a)
			bj, _ := json.Marshal(b)
			assert.JSONEq(t, string(bj), string(aj), "%s %s answers another tenant like no tenant", method, path)
			families++
		}
	}
	assert.Greater(t, families, 40, "the tenant's routes are all walked")
}
