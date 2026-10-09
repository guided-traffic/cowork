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
// version, the document itself (docs/adr/0046 D5), the schema of a
// repository's binding file (docs/adr/0066 D4), what the login page offers
// and the logins themselves — the local one and the identity provider's two
// browser navigations (docs/adr/0033 D8, docs/adr/0031 D1, docs/adr/0029 D1)
// —, and GitHub's webhook, whose credential is its signature (docs/adr/0071
// D2, D3).
var public = map[string]bool{
	"getVersion": true, "getOpenAPI": true, "getCoworkYamlSchema": true, "getAuthOptions": true,
	"loginLocal": true, "loginOidc": true, "oidcCallback": true, "receiveGitHubWebhook": true,
}

// signed are the public writes whose credential is a signature over their body,
// which their handler verifies: GitHub's webhook (docs/adr/0071 D3). GitHub
// sends no Origin, and a signature no browser can make needs no CSRF defence,
// so they are the public writes without the origin check (docs/adr/0037 D5).
var signed = map[string]string{"receiveGitHubWebhook": "github"}

// openQuery are the operations that take query parameters the document does
// not name: the identity provider's callback, to which the issuer may add its
// own (docs/adr/0029 D1). Every other operation refuses an unknown parameter
// (docs/adr/0049 D4).
var openQuery = map[string]bool{"oidcCallback": true}

// recordedRead are the reads that record an act, data leaving the system
// (docs/adr/0026 D5): a session's request for one of them is held to the
// installation's own pages by Sec-Fetch-Site (docs/adr/0026 D5 as amended
// 2026-10-07).
var recordedRead = map[string]bool{
	"downloadAttachment": true, "exportTicket": true, "exportTicketContext": true, "exportProject": true, "exportTenant": true,
}

// sessionOnly are the operations a personal access token cannot call: it
// answers `403 session_required` (docs/adr/0035 D5, docs/adr/0033 D1, D4, D5,
// docs/adr/0005 D5, docs/adr/0031 D4, docs/adr/0030 D2, D3, docs/adr/0034 D3).
// The document says so with a single requirement, `sessionCookie`; every other
// operation takes either credential. What a leaked token must not be able to
// make — a token, a tenant, an account, a password the administrator knows, a
// role, a mapping, a person's way into a restricted project — outlives its
// revocation; what only takes access away stays open to a token. A turn of
// the chat acts with the person's session, and so does stopping one; an agent
// with a token has the MCP server (docs/adr/0076, docs/adr/0040). Choosing the
// chat's capabilities gives the person's agent access, which a token does not
// give (docs/adr/0043 D5). The list of every tenant is a global
// administrator's view across the installation's clients, which a token of
// theirs does not get (docs/adr/0034 D2). Purging a deleted ticket is the one
// irreversible act on a ticket, which a leaked token must not make either
// (docs/adr/0024 D7 as amended 2026-10-05). The tenant's GitHub webhook secret
// lets whoever holds it write links into the tenant after a leaked token's
// revocation; revoking it only takes access away (docs/adr/0071 D1). Removing
// the orphaned objects of a consistency check is irreversible like a purge
// (docs/adr/0059 D4). Unlocking a local account undoes its lockout, which a
// leaked token could do between guesses until the lockout held never
// (docs/adr/0035 D5 as amended 2026-10-07).
var sessionOnly = map[string]bool{
	"logout": true, "changeMyPassword": true, "createMyToken": true, "createTenant": true, "listTenants": true,
	"createAccount": true, "resetAccountPassword": true, "unlockAccount": true,
	"addMember": true, "setMemberGrant": true, "createGroupMapping": true, "updateGroupMapping": true,
	"setProjectRestriction": true, "setProjectAccess": true, "runChatTurn": true, "stopChatTurns": true,
	"setMyChat": true, "purgeTicket": true, "createGitHubSecret": true, "removeOrphanedObjects": true,
}

// The document is part of the security documentation (docs/adr/0046 D8):
// every operation has an id, a security requirement — either credential, the
// session cookie alone where a token may not call, or an explicit empty one on
// the public operations — and the problem response for its errors
// (docs/adr/0047 D1). A public route that writes is origin-checked, because
// no session carries the CSRF check for it (docs/adr/0037 D5) — but a signed
// one, whose signature is its credential —, and only the identity provider's
// callback takes query parameters it does not declare.
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
				by, isSigned := signed[op.OperationID]
				if method != http.MethodGet && !isSigned {
					assert.Equal(t, true, op.Extensions["x-cowork-origin-check"], "%s is a public write and is origin-checked", where)
				}
				if isSigned {
					assert.Equal(t, by, op.Extensions["x-cowork-signed"], "%s says who signs it", where)
					assert.Nil(t, op.Extensions["x-cowork-origin-check"], "%s is signed, not origin-checked", where)
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
			open, _ := op.Extensions["x-cowork-open-query"].(bool)
			assert.Equal(t, openQuery[op.OperationID], open, "%s takes unknown query parameters only where the document allows it", where)
			_, isSigned := op.Extensions["x-cowork-signed"]
			assert.Equal(t, signed[op.OperationID] != "", isSigned, "%s is signed only where the test names it", where)
			recorded, _ := op.Extensions["x-cowork-recorded-read"].(bool)
			assert.Equal(t, recordedRead[op.OperationID], recorded, "%s is a recorded read only where the test names it", where)
			if recorded {
				assert.Equal(t, http.MethodGet, method, "%s is a read", where)
			}
		}
	}
	for id := range sessionOnly {
		assert.Contains(t, ids, id, "the session-only operation %s exists", id)
	}
	assert.Greater(t, len(ids), 50, "the document has the phase's operations")
}
