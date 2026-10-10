package apispec

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// public are the operations that answer without a credential: the build's
// version, the document itself (docs/adr/0046 D5), the schema of a
// repository's binding file (docs/adr/0066 D4), what the login page offers
// and the logins themselves — the local one and the identity provider's two
// browser navigations (docs/adr/0033 D8, docs/adr/0031 D1, docs/adr/0029 D1).
var public = map[string]bool{
	"getVersion": true, "getOpenAPI": true, "getCoworkYamlSchema": true, "getAuthOptions": true,
	"loginLocal": true, "loginOidc": true, "oidcCallback": true,
}

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
	"downloadAttachment": true, "exportTicket": true, "exportTicketContext": true, "exportProject": true, "exportTeam": true,
}

// sessionOnly are the operations a personal access token cannot call: it
// answers `403 session_required` (docs/adr/0035 D5, docs/adr/0033 D1, D4, D5,
// docs/adr/0005 D5, docs/adr/0031 D4, docs/adr/0030 D2, D3, docs/adr/0034 D3).
// The document says so with a single requirement, `sessionCookie`; every other
// operation takes either credential. What a leaked token must not be able to
// make — a token, a team, an account, a password the administrator knows, a
// role, a mapping, a person's way into a restricted project — outlives its
// revocation; what only takes access away stays open to a token. A turn of
// the chat acts with the person's session, and so does stopping one; an agent
// with a token has the MCP server (docs/adr/0076, docs/adr/0040). Choosing the
// chat's capabilities gives the person's agent access, which a token does not
// give (docs/adr/0043 D5). The list of every team is a global
// administrator's view across the installation's teams, which a token of
// theirs does not get (docs/adr/0034 D2). Purging a deleted ticket is the one
// irreversible act on a ticket, which a leaked token must not make either
// (docs/adr/0024 D7 as amended 2026-10-05). Removing the orphaned objects of a
// consistency check is irreversible like a purge (docs/adr/0059 D4). Unlocking a
// local account undoes its lockout, which a leaked token could do between
// guesses until the lockout held never (docs/adr/0035 D5 as amended 2026-10-07).
var sessionOnly = map[string]bool{
	"logout": true, "changeMyPassword": true, "createMyToken": true, "createTeam": true, "listTeams": true,
	"createAccount": true, "resetAccountPassword": true, "unlockAccount": true,
	"addMember": true, "setMemberGrant": true, "createGroupMapping": true, "updateGroupMapping": true,
	"setProjectRestriction": true, "setProjectAccess": true, "runChatTurn": true, "stopChatTurns": true,
	"setMyChat": true, "purgeTicket": true, "removeOrphanedObjects": true,
}

// The document is part of the security documentation (docs/adr/0046 D8):
// every operation has an id, a security requirement — either credential, the
// session cookie alone where a token may not call, or an explicit empty one on
// the public operations — and the problem response for its errors
// (docs/adr/0047 D1). A public route that writes is origin-checked, because
// no session carries the CSRF check for it (docs/adr/0037 D5), and only the
// identity provider's callback takes query parameters it does not declare.
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
			if isTwin(path) {
				// A twin is declared as the path it stands for, which this loop
				// checks; TestTheTenantPathsAreDeprecatedTwinsOfTheTeamPaths holds
				// the twin to it.
				continue
			}

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
			open, _ := op.Extensions["x-cowork-open-query"].(bool)
			assert.Equal(t, openQuery[op.OperationID], open, "%s takes unknown query parameters only where the document allows it", where)
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

// The deprecated path family of the rename of a tenant to a team (docs/adr/0005
// D1, docs/adr/0023 D1): tools/specbundle writes a twin under oldFamily for
// every path under newFamily, for one release (docs/adr/0046 D7).
const (
	newFamily = "/api/v1/teams"
	oldFamily = "/api/v1/tenants"
)

// renamed are the operations the rename renamed, by the operationId their twin
// keeps — the one the release before served them under.
var renamed = map[string]string{
	"listTeams": "listTenants", "createTeam": "createTenant", "getTeam": "getTenant", "updateTeam": "updateTenant",
	"exportTeam": "exportTenant", "searchTeam": "searchTenant", "listTeamTickets": "listTenantTickets",
	"listTeamTime": "listTenantTime", "listTeamTokens": "listTenantTokens", "revokeTeamToken": "revokeTenantToken",
}

func isTwin(path string) bool {
	return path == oldFamily || strings.HasPrefix(path, oldFamily+"/")
}

// teamPath is the path a twin stands for.
func teamPath(twin string) string {
	return newFamily + strings.Replace(strings.TrimPrefix(twin, oldFamily), "{tenant}", "{team}", 1)
}

// Every path under /api/v1/teams has its twin under /api/v1/tenants until the
// release that removes the pair, and a twin is the path it stands for under the
// name before: the same operations, each deprecated and tagged `tenants`, with
// the same parameters but the deprecated `tenant` for `team`, the same body, the
// same answers, the same security and the same marks — and the operationId of
// the release before where the rename renamed it (cowork-mcp of that release
// checks it at its start, docs/adr/0040 D5).
func TestTheTenantPathsAreDeprecatedTwinsOfTheTeamPaths(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(Document)
	require.NoError(t, err)

	teams, twins := 0, 0
	for _, path := range doc.Paths.InMatchingOrder() {
		switch {
		case path == newFamily || strings.HasPrefix(path, newFamily+"/"):
			teams++
			assert.NotNil(t, doc.Paths.Value(oldFamily+strings.Replace(strings.TrimPrefix(path, newFamily), "{team}", "{tenant}", 1)),
				"%s has its deprecated twin", path)
		case isTwin(path):
			twins++
			item := doc.Paths.Value(path)
			team := doc.Paths.Value(teamPath(path))
			require.NotNil(t, team, "the twin %s stands for a team path", path)
			assert.Equal(t, comparable(t, team.Parameters), comparable(t, item.Parameters), "%s takes the parameters of its team path", path)
			require.Len(t, item.Operations(), len(team.Operations()), "%s has the operations of its team path", path)
			for method, op := range item.Operations() {
				where := method + " " + path
				original := team.GetOperation(strings.ToUpper(method))
				require.NotNil(t, original, "%s stands for an operation of its team path", where)
				assert.True(t, op.Deprecated, "%s is deprecated", where)
				assert.Equal(t, []string{"tenants"}, op.Tags, "%s is tagged as a twin", where)
				want := original.OperationID + "Deprecated"
				if old, ok := renamed[original.OperationID]; ok {
					want = old
				}
				assert.Equal(t, want, op.OperationID, "%s keeps the operationId of the release before", where)
				assert.Equal(t, comparable(t, original.Parameters), comparable(t, op.Parameters), "%s takes the parameters of %s", where, original.OperationID)
				assert.Equal(t, comparable(t, original.RequestBody), comparable(t, op.RequestBody), "%s takes the body of %s", where, original.OperationID)
				assert.Equal(t, comparable(t, original.Responses), comparable(t, op.Responses), "%s answers as %s", where, original.OperationID)
				assert.Equal(t, comparable(t, original.Security), comparable(t, op.Security), "%s takes the credentials of %s", where, original.OperationID)
				assert.Equal(t, comparable(t, original.Extensions), comparable(t, op.Extensions), "%s carries the marks of %s", where, original.OperationID)
			}
		}
	}
	assert.Greater(t, teams, 70, "the team paths are in the document")
	assert.Equal(t, teams, twins, "every team path has one twin and every twin stands for one")
	for id := range renamed {
		assert.Contains(t, operationIDs(doc), id, "the renamed operation %s exists", id)
	}
}

// comparable is v as JSON, with the deprecated path parameter of a twin read as
// the team's.
func comparable(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return strings.ReplaceAll(string(b), "#/components/parameters/TenantSlug", "#/components/parameters/TeamSlug")
}

func operationIDs(doc *openapi3.T) []string {
	var ids []string
	for _, path := range doc.Paths.InMatchingOrder() {
		for _, op := range doc.Paths.Value(path).Operations() {
			ids = append(ids, op.OperationID)
		}
	}
	return ids
}

// oldNames are the names before of the rename and the names that replace them
// (docs/adr/0005 D1).
var oldNames = map[string]string{"tenant": "team", "tenants": "teams", "restricted_tenant": "restricted_team"}

// Every name before of the rename is deprecated, and stands beside the name
// that replaces it, until the release that removes it (docs/adr/0046 D7): a
// parameter `tenant` beside `team` in the same place, a property `tenant`,
// `tenants` or `restricted_tenant` beside `team`, `teams` or `restricted_team`
// in the same schema. Outside the twins, no path names `{tenant}`.
func TestEveryTenantNameIsDeprecatedBesideItsTeamName(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(Document)
	require.NoError(t, err)

	for _, path := range doc.Paths.InMatchingOrder() {
		if !isTwin(path) {
			assert.NotContains(t, path, "{tenant}", "%s names the team, not the tenant", path)
		}
		item := doc.Paths.Value(path)
		for method, op := range item.Operations() {
			where := method + " " + path
			params := append(openapi3.Parameters{}, item.Parameters...)
			params = append(params, op.Parameters...)
			for _, p := range params {
				if p.Value == nil || p.Value.Name != "tenant" {
					continue
				}
				assert.True(t, p.Value.Deprecated, "the parameter tenant of %s is deprecated", where)
				if p.Value.In == openapi3.ParameterInQuery {
					assert.NotNil(t, params.GetByInAndName(openapi3.ParameterInQuery, "team"), "%s takes team beside tenant", where)
				}
			}
		}
	}
	names := make([]string, 0, len(doc.Components.Schemas))
	for name := range doc.Components.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		schema := doc.Components.Schemas[name].Value
		for _, part := range append(openapi3.SchemaRefs{{Value: schema}}, schema.AllOf...) {
			if part.Value == nil {
				continue
			}
			for prop, ref := range part.Value.Properties {
				replacement, old := oldNames[prop]
				if !old {
					continue
				}
				assert.True(t, ref.Value != nil && ref.Value.Deprecated, "%s.%s is deprecated", name, prop)
				_, beside := part.Value.Properties[replacement]
				assert.True(t, beside, "%s has %s beside %s", name, replacement, prop)
			}
		}
		assert.NotContains(t, name, "Tenant", "the schema %s names the team, not the tenant", name)
	}
}
