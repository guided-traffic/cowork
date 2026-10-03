package apispec

import (
	"context"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// public are the operations that answer without a token: the build's
// version and the document itself (docs/adr/0046 D5).
var public = map[string]bool{"getVersion": true, "getOpenAPI": true}

// The document is part of the security documentation (docs/adr/0046 D8):
// every operation has an id, a security requirement — the bearer token, or
// an explicit empty one on the public operations — and the problem response
// for its errors (docs/adr/0047 D1).
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
			if public[op.OperationID] {
				assert.Empty(t, security, "%s is public and says so", where)
			} else {
				require.Len(t, security, 1, "%s declares the bearer token", where)
				_, bearer := security[0]["bearerToken"]
				assert.True(t, bearer, "%s declares the bearer token", where)
			}
			assert.NotNil(t, op.Responses.Default(), "%s answers its errors with the problem response", where)
			assert.NotEmpty(t, op.Tags, "%s has a tag", where)
		}
	}
	assert.Greater(t, len(ids), 50, "the document has the phase's operations")
}
