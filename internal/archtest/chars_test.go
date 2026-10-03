package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestNoTwoCharsReadsInOneStatement guards the lifetime contract of the
// characteristics query (effects.HostRead.Chars, rules' Engine.Chars): the
// returned *effects.Chars is valid only until the next Chars call, and Go
// leaves the order of a field load against a LATER call in the same
// expression unspecified. `textA, textB := h.Chars(a).Text, h.Chars(b).Text`
// compiled to two calls followed by two loads, both from b's record: the
// api:ExchangeTextBox capture swapped one box with itself. So no single
// statement may make two Chars calls; read each into its own statement.
// Scans rules/, effects/, botpolicy/ and view/ non-test files.
func TestNoTwoCharsReadsInOneStatement(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{"rules", "effects", "botpolicy", "view"} {
		for _, file := range goSourcesUnder(t, filepath.Join(root, dir)) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				var stmt ast.Node
				switch s := n.(type) {
				case *ast.AssignStmt, *ast.ReturnStmt, *ast.ExprStmt, *ast.DeclStmt:
					stmt = s
				case *ast.IfStmt:
					stmt = s.Cond
				default:
					return true
				}
				calls := 0
				ast.Inspect(stmt, func(m ast.Node) bool {
					if _, isLit := m.(*ast.FuncLit); isLit {
						return false // a closure body's statements are visited on their own
					}
					if call, ok := m.(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Chars" && len(call.Args) == 1 {
							calls++
						}
					}
					return true
				})
				if calls > 1 {
					t.Errorf("%s: %d Chars calls in one statement; the record is valid only until "+
						"the next Chars call, and Go does not order the first field load before the "+
						"second call. Read each into its own statement.", fset.Position(stmt.Pos()), calls)
				}
				return true
			})
		}
	}
}
