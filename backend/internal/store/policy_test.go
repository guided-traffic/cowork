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
// person's installation-level audit rows) or have no tenant at all.
var namedTables = map[string]bool{
	"tenants":          true,
	"users":            true,
	"memberships":      true,
	"tokens":           true,
	"idempotency_keys": true,
	"audit_events":     true,
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
