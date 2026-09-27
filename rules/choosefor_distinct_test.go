package rules

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestChooseForValuesAreDistinct parses the package's own sources and
// evaluates every chooseFor constant (explicit literals and iota blocks). The
// values are hand-numbered across many files, so two flows can silently
// claim the same number -- handleChoose then routes one flow's answers to the
// other (a Morph turn-up flow at 44 once swallowed the CR 704.5j legend-rule
// answers and livelocked a bot game). Every chooseFor constant must be
// distinct.
func TestChooseForValuesAreDistinct(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	type decl struct {
		expr ast.Expr
		iota int64
	}
	fset := token.NewFileSet()
	decls := map[string]decl{}
	var names []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			var typ ast.Expr
			var vals []ast.Expr
			for iota, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if vs.Type != nil || len(vs.Values) > 0 {
					typ, vals = vs.Type, vs.Values
				}
				id, ok := typ.(*ast.Ident)
				if !ok || id.Name != "chooseFor" || len(vals) != len(vs.Names) {
					continue
				}
				for i, n := range vs.Names {
					decls[n.Name] = decl{vals[i], int64(iota)}
					names = append(names, n.Name)
				}
			}
		}
	}
	if len(names) < 30 {
		t.Fatalf("found only %d chooseFor constants; the scan is broken", len(names))
	}
	var eval func(x ast.Expr, iota int64, depth int) (int64, bool)
	eval = func(x ast.Expr, iota int64, depth int) (int64, bool) {
		if depth > len(names) {
			return 0, false
		}
		switch x := x.(type) {
		case *ast.BasicLit:
			return constant.Int64Val(constant.MakeFromLiteral(x.Value, x.Kind, 0))
		case *ast.Ident:
			if x.Name == "iota" {
				return iota, true
			}
			d, ok := decls[x.Name]
			if !ok {
				return 0, false
			}
			return eval(d.expr, d.iota, depth+1)
		case *ast.ParenExpr:
			return eval(x.X, iota, depth)
		case *ast.BinaryExpr:
			a, ok1 := eval(x.X, iota, depth)
			b, ok2 := eval(x.Y, iota, depth)
			if ok1 && ok2 && x.Op == token.ADD {
				return a + b, true
			}
			if ok1 && ok2 && x.Op == token.SUB {
				return a - b, true
			}
		}
		return 0, false
	}
	sort.Strings(names)
	seen := map[int64]string{}
	for _, n := range names {
		v, ok := eval(decls[n].expr, decls[n].iota, 0)
		if !ok {
			t.Fatalf("cannot evaluate chooseFor constant %s", n)
		}
		if prev, dup := seen[v]; dup {
			t.Errorf("chooseFor value %d is claimed by both %s and %s", v, prev, n)
		}
		seen[v] = n
	}
}
