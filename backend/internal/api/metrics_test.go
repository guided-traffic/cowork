package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
)

// docs/adr/0060 D5: the API names every request's route by the pattern the
// document writes — /api/v1/tenants/{tenant}/projects/{project}/tickets/{number}
// — never by the tenant, the key or the number the client sent; a path the
// document does not hold is unmatched. Nothing here needs the database: the
// requests end before a handler would read it.
func TestTheRouteLabelIsTheDocumentsPattern(t *testing.T) {
	m := metrics.New()
	h, err := New(Options{SessionKey: make([]byte, 32), ValidateResponses: true, Metrics: m})
	require.NoError(t, err)
	send := func(method, target string, headers ...string) int {
		ctx, done := m.Request(context.Background(), method)
		r := httptest.NewRequest(method, target, nil).WithContext(ctx)
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		done(rec.Code)
		return rec.Code
	}
	assert.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/v1/teams/acme/projects/COW/tickets/42"))
	// A twin under the family before is recorded by its team path's pattern
	// (docs/adr/0023 D1).
	assert.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/v1/tenants/acme/projects/COW/tickets/42"))
	assert.Equal(t, http.StatusUnauthorized, send(http.MethodPost, "/api/v1/tenants/acme/projects/COW/tickets/42/comments",
		"Authorization", "Bearer not-a-token"))
	assert.Equal(t, http.StatusOK, send(http.MethodGet, "/api/v1/version"))
	assert.Equal(t, http.StatusSeeOther, send(http.MethodGet, "/auth/callback?code=c&state=s"))
	assert.Equal(t, http.StatusNotFound, send(http.MethodGet, "/api/v1/tenants/acme/nothing-here/42"))

	samples, err := m.Samples()
	require.NoError(t, err)
	for _, c := range []struct {
		route, method, status string
		count                 float64
	}{
		{"/api/v1/teams/{team}/projects/{project}/tickets/{number}", "GET", "401", 2},
		{"/api/v1/teams/{team}/projects/{project}/tickets/{number}/comments", "POST", "401", 1},
		{"/api/v1/version", "GET", "200", 1},
		{"/auth/callback", "GET", "303", 1},
		{metrics.Unmatched, "GET", "404", 1},
	} {
		assert.Equal(t, c.count, metrics.Sum(samples, "cowork_http_requests_total", "route", c.route, "method", c.method, "status", c.status), c)
	}
	for _, s := range samples {
		assert.NotContains(t, s.Labels["route"], "{tenant}", "a twin is recorded by its team path's pattern")
	}
	for _, s := range samples {
		route := s.Labels["route"]
		for _, instance := range []string{"acme", "COW", "42", "nothing-here"} {
			assert.False(t, strings.Contains(route, instance), "the route label %q holds what the client sent", route)
		}
	}
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_auth_token_refusals_total", "reason", "malformed"))
	assert.Equal(t, 1.0, metrics.Sum(samples, "cowork_auth_logins_total", "method", "oidc", "outcome", "failure"),
		"a callback without a provider is a failed login")
}
