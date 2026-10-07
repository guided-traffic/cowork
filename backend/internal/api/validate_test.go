package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// A query parameter that breaks its schema is named by its failure alone, as
// a body's field is (docs/adr/0047 D2): the JSON Schema 2020-12 validator a
// 3.1 document uses writes the resource it compiles every schema under and
// the value's location before the failure, and neither reaches the client. A
// parameter is one entry, a repeated one's failing values in its message.
func TestAParametersMessageIsItsFailureAlone(t *testing.T) {
	h := documentHandler(t)
	tickets := "/api/v1/tenants/acme/projects/WEB/tickets"
	for name, c := range map[string]struct {
		target   string
		expected []problem.FieldError
	}{
		"a minimum": {tickets + "?limit=0", []problem.FieldError{{Pointer: "query:limit", Message: "minimum: got 0, want 1"}}},
		"an enum": {tickets + "?page=1&per_page=7",
			[]problem.FieldError{{Pointer: "query:per_page", Message: "value must be one of 25, 50, 100"}}},
		"a pattern": {"/api/v1/tenants/acme/time-entries?project=web",
			[]problem.FieldError{{Pointer: "query:project", Message: "'web' does not match pattern '^[A-Z][A-Z0-9]{1,9}$'"}}},
		"a repeated parameter": {tickets + "?state=" + strings.Repeat("a", 33) + "&state=done&state=" + strings.Repeat("b", 40),
			[]problem.FieldError{{Pointer: "query:state", Message: "maxLength: got 33, want 32; maxLength: got 40, want 32"}}},
		// The invalidParameter example of components/responses.yaml.
		"two parameters": {tickets + "?page=0&per_page=7",
			[]problem.FieldError{{Pointer: "query:page", Message: "minimum: got 0, want 1"},
				{Pointer: "query:per_page", Message: "value must be one of 25, 50, 100"}}},
	} {
		r := httptest.NewRequest(http.MethodGet, c.target, nil)
		route, params, err := h.router.FindRoute(r)
		require.NoError(t, err, name)
		perr := h.validateRequest(r, route, params)
		require.NotNil(t, perr, name)
		assert.Equal(t, problem.ValidationFailed, perr.Code, name)
		assert.Equal(t, c.expected, perr.Errors, name)
		for _, e := range perr.Errors {
			assert.NotContains(t, e.Message, "example.com", name)
			assert.False(t, strings.ContainsAny(e.Message, "\r\n"), "%s: %q", name, e.Message)
		}
	}
}
