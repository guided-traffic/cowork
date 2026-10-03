package apispec

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// public are the operations that answer without a credential: the build's
// version, the document itself (docs/adr/0046 D5), what the login page offers
// and the login itself (docs/adr/0033 D8, docs/adr/0031 D1).
var public = map[string]bool{"getVersion": true, "getOpenAPI": true, "getAuthOptions": true, "loginLocal": true}

// sessionOnly are the operations a personal access token cannot call: it
// answers `403 session_required` (docs/adr/0035 D5, docs/adr/0033 D1, D4, D5,
// docs/adr/0005 D5, docs/adr/0031 D4). The document says so with a single
// requirement, `sessionCookie`; every other operation takes either credential.
// What a leaked token must not be able to make — a token, a tenant, an
// account, a password the administrator knows — outlives its revocation.
var sessionOnly = map[string]bool{
	"logout": true, "changeMyPassword": true, "createMyToken": true, "createTenant": true,
	"createAccount": true, "resetAccountPassword": true,
}

// The document is part of the security documentation (docs/adr/0046 D8):
// every operation has an id, a security requirement — either credential, the
// session cookie alone where a token may not call, or an explicit empty one on
// the public operations — and the problem response for its errors
// (docs/adr/0047 D1). A public route that writes is origin-checked, because
// no session carries the CSRF check for it (docs/adr/0037 D5).
func TestEveryOperationIsDeclaredCompletely(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(Document)
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))

	paths := doc.Paths.InMatchingOrder()
	sort.Strings(paths)
	ids := map[string]string{}
	for _, path := range paths {
		for method, op := range doc.Paths.Value(path).Operations() {
			where := method + " " + path
			require.NotEmpty(t, op.OperationID, "%s has no operationId", where)
			if other, ok := ids[op.OperationID]; ok {
				t.Errorf("%s and %s share the operationId %s", where, other, op.OperationID)
			}
			ids[op.OperationID] = where

			security := doc.Security
			if op.Security != nil {
				security = *op.Security
			}
			switch {
			case public[op.OperationID]:
				assert.Empty(t, security, "%s is public and says so", where)
				if method != http.MethodGet {
					assert.Equal(t, true, op.Extensions["x-cowork-origin-check"], "%s is a public write and is origin-checked", where)
				}
			case sessionOnly[op.OperationID]:
				require.Len(t, security, 1, "%s takes a session only", where)
				_, session := security[0]["sessionCookie"]
				assert.True(t, session, "%s declares the session cookie alone", where)
			default:
				require.Len(t, security, 2, "%s takes either credential", where)
				_, bearer := security[0]["bearerToken"]
				_, session := security[1]["sessionCookie"]
				assert.True(t, bearer && session, "%s declares the bearer token and the session cookie", where)
			}
			assert.NotNil(t, op.Responses.Default(), "%s answers its errors with the problem response", where)
			assert.NotEmpty(t, op.Tags, "%s has a tag", where)
		}
	}
	for id := range sessionOnly {
		assert.Contains(t, ids, id, "the session-only operation %s exists", id)
	}
	assert.Greater(t, len(ids), 50, "the document has the phase's operations")
}
