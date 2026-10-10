package store

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	createTablePattern = regexp.MustCompile(`(?m)^CREATE TABLE (\w+) \(`)
	tenantColumnRe     = regexp.MustCompile(`(?m)^\s+tenant_id\s+uuid`)
)

// namedTables are the tables docs/adr/0021 D6 names one by one: each has a
// policy of its own instead of tenant_isolation, because its rows are read
// across tenants by the person they belong to (memberships, tokens, the
// person's sessions, local account and chat capabilities, the person's
// installation-level audit rows) or have no tenant at all (the login's
// attempts and locks).
var namedTables = map[string]bool{
	"tenants":           true,
	"users":             true,
	"memberships":       true,
	"tokens":            true,
	"idempotency_keys":  true,
	"audit_events":      true,
	"sessions":          true,
	"local_accounts":    true,
	"login_attempts":    true,
	"login_locks":       true,
	"chat_capabilities": true,
}

// Every table the migrations create is protected in the migration set itself,
// without a database (docs/adr/0021 Consequences, docs/adr/0027 D7): row-level
// security enabled and forced, at least one policy, a grant to the runtime
// role, and the canonical tenant_isolation policy on every table that carries
// a tenant_id outside the named list. tenants comes from 000001 and is
// protected by 000002.
func TestEveryTableHasItsPolicyAndGrant(t *testing.T) {
	all := allMigrations(t)
	tables := createTablePattern.FindAllStringSubmatch(all, -1)
	require.NotEmpty(t, tables)

	for _, m := range tables {
		table := m[1]
		t.Run(table, func(t *testing.T) {
			assert.True(t, strings.Contains(all, "ALTER TABLE "+table+" ENABLE ROW LEVEL SECURITY;"), "row-level security not enabled")
			assert.True(t, strings.Contains(all, "ALTER TABLE "+table+" FORCE ROW LEVEL SECURITY;"), "row-level security not forced")
			assert.True(t, regexp.MustCompile(`CREATE POLICY \w+ ON `+table+`\b`).MatchString(all), "no policy")
			assert.True(t, regexp.MustCompile(`GRANT [A-Z, ()a-z_']+ ON `+table+` TO %I`).MatchString(all), "no grant to the runtime role")
			if !namedTables[table] {
				body := tableBody(t, all, table)
				require.True(t, tenantColumnRe.MatchString(body), "not on the named list of docs/adr/0021 D6, so it must carry tenant_id")
				assert.True(t, strings.Contains(all, "CREATE POLICY tenant_isolation ON "+table+"\n"+
					"    USING (tenant_id = app_tenant_id())\n"+
					"    WITH CHECK (tenant_id = app_tenant_id());"), "no canonical tenant_isolation policy")
			}
		})
	}
}

// A migration that lifts the force of row-level security for a backfill of
// its own restores it later in the same file: the file runs as one
// transaction, so no committed state leaves a table unforced
// (docs/adr/0021 D1). The test above would not see a missing restore — an
// earlier migration's FORCE satisfies it.
func TestLiftedForceIsRestoredInTheSameMigration(t *testing.T) {
	noForce := regexp.MustCompile(`(?m)^ALTER TABLE (\w+) NO FORCE ROW LEVEL SECURITY;`)
	for name, body := range migrationBodies(t) {
		for _, m := range noForce.FindAllStringSubmatchIndex(body, -1) {
			table := body[m[2]:m[3]]
			assert.Contains(t, body[m[1]:], "ALTER TABLE "+table+" FORCE ROW LEVEL SECURITY;",
				"%s lifts the force on %s and does not restore it", name, table)
		}
	}
}

// A policy reads the request context only through the guarded functions or a
// NULLIF over the setting: on a pooled connection an ended transaction's
// setting reads ” and a bare cast raises instead of matching nothing
// (docs/adr/0021 D1, D3).
func TestPoliciesReadSettingsGuarded(t *testing.T) {
	for name, body := range migrationBodies(t) {
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, "current_setting('app.") {
				continue
			}
			guarded := strings.Contains(line, "NULLIF(current_setting('app.") ||
				strings.Contains(line, "current_setting('app.job', true) = ")
			assert.True(t, guarded, "%s: unguarded setting in %q", name, strings.TrimSpace(line))
		}
	}
}

// No cascade points into the audit record: a cascade deletes past both the
// grant and row-level security (docs/adr/0026 D3).
func TestNothingCascadesIntoTheAuditRecord(t *testing.T) {
	all := allMigrations(t)
	body := tableBody(t, all, "audit_events")
	assert.NotContains(t, body, "ON DELETE")
	assert.Contains(t, all, "GRANT SELECT, INSERT ON audit_events TO %I")
}

// A function that runs with its owner's rights is the one privileged code path
// of the schema (docs/adr/0026 D3): it resolves names on a path it fixes, so no
// object a caller creates stands in for one it names, and nobody but the
// runtime role may call it. And it sets app.crossing as its first statement —
// a crossing its kind, every other one the empty value — so that a value its
// caller left never reaches the owner role's crossing policies inside it
// (docs/adr/0021 D7 as made concrete 2026-10-10): a SET clause cannot carry a
// custom setting for an owner role that is no superuser, so the body does, and
// a crossing restores the value it found before each of its exits.
func TestEverySecurityDefinerFunctionIsFencedIn(t *testing.T) {
	all := allMigrations(t)
	definers := securityDefiners(t, all)
	require.NotEmpty(t, definers, "the purge's function is expected")
	for name, body := range definers {
		assert.Contains(t, all, "ALTER FUNCTION "+name+"(", "%s fixes no search_path", name)
		assert.Regexp(t, `ALTER FUNCTION `+name+`\([^)]*\) SET search_path = %I, pg_temp`, all, "%s: pg_temp must come last", name)
		assert.Contains(t, all, "REVOKE ALL ON FUNCTION "+name+"(", "%s is not revoked from PUBLIC", name)
		kind, ok := crossingKind(body)
		if !assert.True(t, ok, "%s: its first statement sets app.crossing — its kind, or '' for a function that crosses nothing", name) {
			continue
		}
		if kind == "" {
			continue
		}
		assert.Contains(t, body, crossingPrevious, "%s keeps the value it found in prev", name)
		statements := bodyStatements(body)
		for i, s := range statements {
			if returns(s) {
				assert.True(t, i > 0 && statements[i-1] == crossingRestore, "%s: a return without restoring app.crossing first: %q", name, s)
			}
		}
		require.NotEmpty(t, statements, name)
		last := len(statements) - 1
		assert.True(t, statements[last-1] == crossingRestore || (returns(statements[last-1]) && statements[last-2] == crossingRestore),
			"%s ends without restoring app.crossing", name)
	}
	assert.NotRegexp(t, `(?i)\bSET\s+app\.crossing\b`, all, "no SET clause names app.crossing: the function's body sets it")
}

// The statements a crossing function sets and restores app.crossing with.
const (
	crossingPrevious = "prev text := NULLIF(current_setting('app.crossing', true), '');"
	crossingRestore  = "PERFORM set_config('app.crossing', coalesce(prev, ''), true)"
)

var (
	definerPattern = regexp.MustCompile(`(?s)CREATE (?:OR REPLACE )?FUNCTION (\w+)\(([^)]*)\)(.*?)AS \$\$(.*?)\$\$;`)
	crossingFirst  = regexp.MustCompile(`^PERFORM set_config\('app\.crossing', '(\w*)', true\)$`)
)

// securityDefiners are the SECURITY DEFINER functions of the migrations, each
// by its last definition — a later CREATE OR REPLACE replaces the body.
func securityDefiners(t *testing.T, all string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range definerPattern.FindAllStringSubmatch(all, -1) {
		if strings.Contains(m[3], "SECURITY DEFINER") {
			out[m[1]] = m[4]
		} else {
			delete(out, m[1])
		}
	}
	return out
}

// crossingKind is the kind of crossing a function's first statement sets, ""
// for a function that crosses nothing; false when its first statement sets no
// crossing.
func crossingKind(body string) (string, bool) {
	statements := bodyStatements(body)
	if len(statements) == 0 {
		return "", false
	}
	m := crossingFirst.FindStringSubmatch(statements[0])
	if m == nil {
		return "", false
	}
	return m[1], true
}

// bodyStatements are the statements of a PL/pgSQL body after its BEGIN, each
// with its whitespace collapsed; the last is the body's END.
func bodyStatements(body string) []string {
	begin := regexp.MustCompile(`(?m)^BEGIN$`).FindStringIndex(body)
	if begin == nil {
		return nil
	}
	var out []string
	for _, s := range strings.Split(body[begin[1]:], ";") {
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// returns reports whether a statement leaves the function: a RETURN, not a
// RETURN QUERY or a RETURN NEXT, which add rows and go on.
func returns(statement string) bool {
	return statement == "RETURN" || (strings.HasPrefix(statement, "RETURN ") &&
		!strings.HasPrefix(statement, "RETURN QUERY") && !strings.HasPrefix(statement, "RETURN NEXT"))
}

// The crossing policies bind the owner role alone — the role that runs the
// migration and owns the crossing functions — and admit a crossing only by the
// kind a function set in app.crossing; no other policy names the setting, so
// the runtime role's queries are never widened (docs/adr/0021 D7 as made
// concrete 2026-10-10). DB.CheckCrossing counts them at the start of serve.
func TestTheCrossingPoliciesNameTheOwnerAlone(t *testing.T) {
	crossing := regexp.MustCompile(`(?s)EXECUTE format\('CREATE POLICY (\w+) ON (\w+) FOR (\w+) TO %I '\s*'(.*?)', (\w+)\);`)
	found := 0
	for name, body := range migrationBodies(t) {
		for _, m := range crossing.FindAllStringSubmatch(body, -1) {
			found++
			assert.Contains(t, m[1], "_crossing_", "%s: the policy %s of a crossing says so in its name", name, m[1])
			assert.Contains(t, m[4], "app_crossing()", "%s: %s admits a crossing by its kind", name, m[1])
			assert.Equal(t, "owner_role", m[5], "%s: %s is the owner role's", name, m[1])
			assert.Contains(t, body, "owner_role text := current_user;", "%s: the owner role is the role that runs the migration", name)
		}
		for _, stmt := range regexp.MustCompile(`(?ms)^(?:CREATE|ALTER) POLICY .*?;`).FindAllString(body, -1) {
			assert.NotContains(t, stmt, "app_crossing()", "%s: only the owner role's crossing policies read app.crossing: %s", name, stmt)
			assert.NotContains(t, stmt, "_crossing_", "%s: a crossing policy is created for the owner role alone: %s", name, stmt)
		}
	}
	assert.Equal(t, crossingPolicies, found, "DB.CheckCrossing counts the crossing policies the migrations create")
}

func migrationBodies(t *testing.T) map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(MigrationsFS(), ".")
	require.NoError(t, err)
	out := map[string]string{}
	for _, e := range entries {
		b, err := fs.ReadFile(MigrationsFS(), e.Name())
		require.NoError(t, err)
		out[e.Name()] = string(b)
	}
	return out
}

func allMigrations(t *testing.T) string {
	t.Helper()
	bodies := migrationBodies(t)
	names := make([]string, 0, len(bodies))
	for n := range bodies {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(bodies[n])
		b.WriteString("\n")
	}
	return b.String()
}

// tableBody returns the column list of a CREATE TABLE statement.
func tableBody(t *testing.T, all, table string) string {
	t.Helper()
	start := strings.Index(all, "CREATE TABLE "+table+" (")
	require.GreaterOrEqual(t, start, 0, "no CREATE TABLE %s", table)
	end := strings.Index(all[start:], "\n);")
	require.Greater(t, end, 0)
	return all[start : start+end]
}
