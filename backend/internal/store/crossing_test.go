package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tenantSwitches are the functions that bind a transaction to a team: the
// wrappers' settings, a job's work in a team, the identity provider's acts in
// a team, and an act recorded in another team's record
// (docs/adr/0021 D3, D7). Nothing else names app.tenant_id.
var tenantSwitches = []string{"setContext", "inTenant", "flushIn", "RecordElsewhere"}

// Only the crossing functions of the migrations cross a team, and only by the
// setting their bodies set (docs/adr/0021 D7 as made concrete 2026-10-10): no
// Go file of the backend and no query file names app.crossing, and app.tenant_id
// is named by the few functions that bind a transaction to a team.
func TestOnlyTheCrossingFunctionsCross(t *testing.T) {
	queries, err := filepath.Glob("queries/*/*.sql")
	require.NoError(t, err)
	for _, f := range queries {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "app.crossing", "%s names the crossings' setting", f)
	}
	err = filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, isLit := n.(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					return true
				}
				where := fset.Position(lit.Pos()).String()
				assert.NotContains(t, lit.Value, "app.crossing", "%s names the crossings' setting", where)
				if strings.Contains(lit.Value, "app.tenant_id") {
					assert.True(t, ok && slices.Contains(tenantSwitches, fn.Name.Name),
						"%s binds a transaction to a team outside %v", where, tenantSwitches)
				}
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)
}

// Every crossing that reads a ticket's head decides what the caller sees of
// it — ticket_sight — and leaves a deleted ticket out; every walk answers yes
// or no; and every other crossing is listed here with what it returns, so a
// new one is decided on, not slipped in. The store's list of crossing
// functions, which DB.CheckCrossing reads the catalog for, is the same.
func TestEveryCrossingFunctionDecidesSight(t *testing.T) {
	answersNoHead := map[string]string{
		"refresh_derived":         "writes the derived progress columns and answers a count",
		"relations_elsewhere":     "answers where an act is recorded in another team; the store shows the caller none of it",
		"end_relations_elsewhere": "ends a purged ticket's relations and answers where the purge records its acts",
		"end_team_relations":      "ends a team's relations and answers where the acts are recorded",
	}
	all := allMigrations(t)
	var crossings []string
	for name, body := range securityDefiners(t, all) {
		kind, ok := crossingKind(body)
		require.True(t, ok, name)
		if kind == "" {
			continue
		}
		crossings = append(crossings, name)
		head := regexp.MustCompile(`(?s)CREATE (?:OR REPLACE )?FUNCTION ` + name + `\([^)]*\)\s*RETURNS boolean`)
		switch kind {
		case "head":
			assert.Contains(t, body, "ticket_sight(", "%s reads a head without deciding the caller's sight", name)
			assert.Contains(t, body, "deleted_at IS NULL", "%s reads a head without leaving the deleted out", name)
			assert.Contains(t, body, "app_is_member()", "%s reads for a caller who holds no role in the team", name)
		case "walk":
			assert.True(t, head.MatchString(all), "%s walks a graph and answers yes or no only", name)
		default:
			assert.Contains(t, answersNoHead, name, "%s crosses as %q: list it with what it returns", name, kind)
		}
	}
	slices.Sort(crossings)
	listed := slices.Clone(crossingFunctions)
	slices.Sort(listed)
	assert.Equal(t, listed, crossings, "DB.CheckCrossing reads the catalog for every crossing function")
}
