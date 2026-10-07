package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every background job takes the advisory lock of RunJob under a key of its
// own: two jobs that shared a key would keep each other from running whenever
// their hours met (docs/developer/data-access.md#jobs). The test reads every
// RunJob call of the backend's packages — a literal key, or a constant of the
// caller's package — and refuses a key two calls share.
func TestEveryJobHoldsALockKeyOfItsOwn(t *testing.T) {
	type call struct{ job, where string }
	keys := map[int64]call{}
	consts := map[string]map[string]int64{} // package directory → constant → value
	var calls []struct {
		dir, where, job string
		key             ast.Expr
	}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ValueSpec:
				for i, name := range n.Names {
					if i < len(n.Values) {
						if lit, ok := n.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
							v, _ := strconv.ParseInt(lit.Value, 0, 64)
							if consts[dir] == nil {
								consts[dir] = map[string]int64{}
							}
							consts[dir][name.Name] = v
						}
					}
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "RunJob" || len(n.Args) != 4 {
					return true
				}
				calls = append(calls, struct {
					dir, where, job string
					key             ast.Expr
				}{dir, fset.Position(n.Pos()).String(), exprText(n.Args[1]), n.Args[2]})
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(calls), 8, "every job of the backend: the seven of runJobs and the bootstrap")
	for _, c := range calls {
		var key int64
		switch k := c.key.(type) {
		case *ast.BasicLit:
			key, err = strconv.ParseInt(k.Value, 0, 64)
			require.NoError(t, err, c.where)
		case *ast.Ident:
			v, ok := consts[c.dir][k.Name]
			require.True(t, ok, "%s: the lock key %s is no integer constant of the package", c.where, k.Name)
			key = v
		default:
			t.Fatalf("%s: the lock key is neither a literal nor a constant", c.where)
		}
		if other, taken := keys[key]; taken {
			t.Errorf("lock key %d is taken twice: %s at %s and %s at %s", key, other.job, other.where, c.job, c.where)
		}
		keys[key] = call{c.job, c.where}
	}
}

// exprText is an argument as it reads in the source, for a failure's message.
func exprText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Value
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	}
	return "?"
}
