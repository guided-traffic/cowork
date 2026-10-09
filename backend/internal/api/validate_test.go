package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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

// tripwire is a body that notes whether anything read it.
type tripwire struct{ read bool }

func (b *tripwire) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }
func (b *tripwire) Close() error             { return nil }

// docs/adr/0039 D2, docs/adr/0046 D4: what a body is — and so which limit holds
// it and whether the document validates it — is the operation's, as the API
// document declares it, never the Content-Type the request names. Every
// operation that declares a body refuses one of a type it does not declare
// with 415 before a byte is read: a JSON body sent as multipart, a multipart
// one sent to a JSON route, one without a type. A declared type passes, with
// its parameters, and a request without a body is not looked at.
func TestABodyIsTheTypeTheOperationDeclares(t *testing.T) {
	h := documentHandler(t)
	h.opts.MaxJSONBody = 1 << 20
	h.opts.AttachmentMaxBytes = 10 << 20
	send := func(op *openapi3.Operation, contentType string) (*problem.Error, bool) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		body := &tripwire{}
		r.Body = body
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		return h.limitBody(httptest.NewRecorder(), r, op), body.read
	}
	operations := 0
	for _, path := range h.doc.Paths.InMatchingOrder() {
		for _, op := range h.doc.Paths.Value(path).Operations() {
			if op.RequestBody == nil {
				continue
			}
			operations++
			multipart := declaresMultipart(op)
			wrong := "multipart/form-data; boundary=x"
			if multipart {
				wrong = "application/json"
			}
			for _, contentType := range []string{wrong, "", "text/plain"} {
				perr, read := send(op, contentType)
				require.NotNil(t, perr, "%s with %q", op.OperationID, contentType)
				assert.Equal(t, problem.UnsupportedMediaType, perr.Code, "%s with %q", op.OperationID, contentType)
				assert.False(t, read, "%s with %q: refused before a byte is read", op.OperationID, contentType)
			}
			for contentType := range op.RequestBody.Value.Content {
				if multipart {
					contentType += "; boundary=x"
				} else {
					contentType += "; charset=utf-8"
				}
				perr, _ := send(op, contentType)
				assert.Nil(t, perr, "%s with %q", op.OperationID, contentType)
			}
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Header.Set("Content-Type", wrong)
			assert.Nil(t, h.limitBody(httptest.NewRecorder(), r, op), "%s without a body", op.OperationID)
		}
	}
	assert.Greater(t, operations, 30, "the document's operations with a body")

	r := httptest.NewRequest(http.MethodPost, "/auth/local", strings.NewReader(`{"username":"ada","password":"x"}`))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	route, params, err := h.router.FindRoute(r)
	require.NoError(t, err)
	assert.False(t, requestInput(r, route, params).Options.ExcludeRequestBody,
		"a multipart Content-Type exempts no body from the validation of a JSON route")
	upload := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/projects/WEB/tickets/1/attachments", strings.NewReader(""))
	route, params, err = h.router.FindRoute(upload)
	require.NoError(t, err)
	assert.True(t, requestInput(upload, route, params).Options.ExcludeRequestBody, "an upload is its handler's to read")
}
