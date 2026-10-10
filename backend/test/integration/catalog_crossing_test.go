//go:build integration

package integration

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The functions the database holds after the migrations keep the crossing
// rules whatever the migration files look like — a function quoted otherwise,
// written in lowercase, qualified by its schema or made SECURITY DEFINER by an
// ALTER FUNCTION, which the file lint of policy_test.go does not read: every
// SECURITY DEFINER function is plpgsql and resolves names on the schema and
// then pg_temp; declares nothing that runs a query before its first
// statement; and sets app.crossing as that first statement, to its kind or to
// the empty string. A crossing restores the value it found before every RETURN
// and at its end. No function's settings name app.crossing (docs/adr/0021 D7
// as made concrete 2026-10-10). The file lint stays the fast check; this one
// reads what PostgreSQL holds.
func TestEverySecurityDefinerInTheCatalogPinsTheCrossing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, env.OwnerURL)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	rows, err := conn.Query(ctx, `SELECT p.oid::regprocedure::text, l.lanname, p.prosecdef, p.prosrc,
		coalesce(p.proconfig, '{}'::text[]), current_schema()
		FROM pg_proc p JOIN pg_language l ON l.oid = p.prolang
		WHERE p.pronamespace = to_regnamespace(current_schema())`)
	require.NoError(t, err)
	type function struct {
		Name, Language string
		Definer        bool
		Source         string
		Config         []string
		Schema         string
	}
	functions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[function])
	require.NoError(t, err)
	definers := 0
	for _, f := range functions {
		if f.Definer {
			definers++
		}
		for _, problem := range catalogCrossingProblems(f.Language, f.Definer, f.Source, f.Config, f.Schema) {
			t.Errorf("%s: %s", f.Name, problem)
		}
	}
	assert.GreaterOrEqual(t, definers, 13, "the twelve crossings and the purge's function are SECURITY DEFINER")

	// A function a later statement makes SECURITY DEFINER, quoted otherwise
	// than the migrations quote theirs, is read as the catalog holds it.
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "CREATE FUNCTION crossing_probe() RETURNS integer LANGUAGE plpgsql AS $fn$ BEGIN RETURN 1; END $fn$")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "ALTER FUNCTION crossing_probe() SECURITY DEFINER")
	require.NoError(t, err)
	var probe function
	require.NoError(t, tx.QueryRow(ctx, `SELECT p.oid::regprocedure::text, l.lanname, p.prosecdef, p.prosrc,
		coalesce(p.proconfig, '{}'::text[]), current_schema()
		FROM pg_proc p JOIN pg_language l ON l.oid = p.prolang WHERE p.proname = 'crossing_probe'`).Scan(
		&probe.Name, &probe.Language, &probe.Definer, &probe.Source, &probe.Config, &probe.Schema))
	assert.NotEmpty(t, catalogCrossingProblems(probe.Language, probe.Definer, probe.Source, probe.Config, probe.Schema),
		"a definer made by ALTER FUNCTION that sets no app.crossing is refused")

	// The check refuses what the file lint does not see.
	pinned := "\nDECLARE\n    prev text := NULLIF(current_setting('app.crossing', true), '');\nBEGIN\n" +
		"    PERFORM set_config('app.crossing', 'head', true);\n"
	restore := "    PERFORM set_config('app.crossing', coalesce(prev, ''), true);\n"
	path := []string{"search_path=public, pg_temp"}
	for name, c := range map[string]struct {
		language string
		source   string
		config   []string
		refused  bool
	}{
		"a pinned crossing":  {"plpgsql", pinned + restore + "    RETURN 1;\nEND\n", path, false},
		"lowercase keywords": {"plpgsql", strings.ToLower(pinned) + strings.ToLower(restore) + "    return 1;\nend\n", path, false},
		"a query in a declaration before the pin": {"plpgsql",
			"\nDECLARE\n    prev text := NULLIF(current_setting('app.crossing', true), '');\n    n integer := (SELECT count(*) FROM tickets);\nBEGIN\n" +
				"    PERFORM set_config('app.crossing', 'head', true);\n" + restore + "    RETURN n;\nEND\n", path, true},
		"a return inside an IF without the restore": {"plpgsql",
			pinned + "    IF true THEN RETURN 0; END IF;\n" + restore + "    RETURN 1;\nEND\n", path, true},
		"no restore at the end": {"plpgsql", pinned + "    RETURN 1;\nEND\n", path, true},
		"no pin first":          {"plpgsql", "\nBEGIN\n    RETURN 1;\nEND\n", path, true},
		"a SQL function":        {"sql", "SELECT 1", path, true},
		"no fixed search_path":  {"plpgsql", pinned + restore + "    RETURN 1;\nEND\n", nil, true},
		"pg_temp first":         {"plpgsql", pinned + restore + "    RETURN 1;\nEND\n", []string{"search_path=pg_temp, public"}, true},
		"app.crossing in the settings": {"plpgsql", pinned + restore + "    RETURN 1;\nEND\n",
			[]string{"search_path=public, pg_temp", "app.crossing=head"}, true},
	} {
		problems := catalogCrossingProblems(c.language, true, c.source, c.config, "public")
		assert.Equal(t, c.refused, len(problems) > 0, "%s: %v", name, problems)
	}
}

var (
	catalogComment  = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	catalogBegin    = regexp.MustCompile(`(?i)\bBEGIN\b`)
	catalogPin      = regexp.MustCompile(`(?i)^PERFORM set_config\('app\.crossing', '(\w*)', true\)$`)
	catalogRestore  = regexp.MustCompile(`(?i)PERFORM set_config\('app\.crossing', coalesce\(prev, ''\), true\)$`)
	catalogPrevious = regexp.MustCompile(`(?i)^prev text := NULLIF\(current_setting\('app\.crossing', true\), ''\)$`)
	catalogConstant = regexp.MustCompile(`(?i)^(?:-?\d+|'[^']*'|NULL|true|false)$`)
	catalogInitial  = regexp.MustCompile(`(?i)\s*(?::=|=|\bDEFAULT\b)\s*`)
	catalogReturn   = regexp.MustCompile(`(?i)\bRETURN\b(\s+\w+)?`)
	catalogDeclare  = regexp.MustCompile(`(?i)^\s*(?:#variable_conflict\s+\w+\s*)?(?:DECLARE\s+)?`)
	catalogGoesOn   = regexp.MustCompile(`(?i)^\s+(QUERY|NEXT)$`)
	catalogEnd      = regexp.MustCompile(`(?i)^END\b`)
)

// catalogCrossingProblems are what a function the catalog holds breaks of the
// crossing rules; none for a function that is no SECURITY DEFINER and names
// app.crossing in no setting.
func catalogCrossingProblems(language string, definer bool, source string, config []string, schema string) []string {
	var problems []string
	path := false
	for _, c := range config {
		if strings.HasPrefix(c, "app.crossing=") {
			problems = append(problems, "a setting names app.crossing: "+c)
		}
		if c == "search_path="+schema+", pg_temp" {
			path = true
		}
	}
	if !definer {
		return problems
	}
	if !path {
		problems = append(problems, "no fixed search_path with pg_temp last: "+strings.Join(config, " "))
	}
	if language != "plpgsql" {
		return append(problems, "a SECURITY DEFINER function in "+language+", which cannot set app.crossing first")
	}
	text := catalogComment.ReplaceAllString(source, "")
	begin := catalogBegin.FindStringIndex(text)
	if begin == nil {
		return append(problems, "no BEGIN")
	}
	hasPrevious := false
	for _, d := range catalogStatements(text[:begin[0]]) {
		d = strings.TrimSpace(catalogDeclare.ReplaceAllString(d, ""))
		if catalogPrevious.MatchString(d) {
			hasPrevious = true
			continue
		}
		if parts := catalogInitial.Split(d, 2); len(parts) == 2 && !catalogConstant.MatchString(strings.TrimSpace(parts[1])) {
			problems = append(problems, "a declaration that runs before app.crossing is set: "+d)
		}
	}
	statements := catalogStatements(text[begin[1]:])
	if len(statements) == 0 {
		return append(problems, "an empty body")
	}
	m := catalogPin.FindStringSubmatch(statements[0])
	if m == nil {
		return append(problems, "the first statement sets no app.crossing: "+statements[0])
	}
	if m[1] == "" {
		return problems
	}
	if !hasPrevious {
		problems = append(problems, "the value app.crossing had is not kept in prev")
	}
	for i, s := range statements {
		for _, r := range catalogReturn.FindAllStringSubmatchIndex(s, -1) {
			if r[2] >= 0 && catalogGoesOn.MatchString(s[r[2]:r[3]]) {
				continue
			}
			if r[0] != 0 || i == 0 || !catalogRestore.MatchString(statements[i-1]) {
				problems = append(problems, "a RETURN without restoring app.crossing right before it: "+s)
			}
		}
	}
	last := len(statements) - 1
	if !catalogEnd.MatchString(statements[last]) || last < 1 {
		return append(problems, "the body does not end in END")
	}
	if !catalogRestore.MatchString(statements[last-1]) && !(catalogReturn.MatchString(statements[last-1]) && last >= 2 &&
		catalogRestore.MatchString(statements[last-2])) {
		problems = append(problems, "the body ends without restoring app.crossing")
	}
	return problems
}

// catalogStatements are the statements of a piece of PL/pgSQL, split at their
// semicolons, each with its whitespace collapsed.
func catalogStatements(text string) []string {
	var out []string
	for _, s := range strings.Split(text, ";") {
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			out = append(out, s)
		}
	}
	return out
}
