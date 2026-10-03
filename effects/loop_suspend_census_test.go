package effects

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// loopSuspendKnownDrops names the functions whose loop still returns on a
// body suspension WITHOUT reporting its cursor to the host, so the
// iterations after the asking one are dropped. It is a ratchet: a new
// function with the shape fails the census, and an entry that now reports
// (or no longer loops) is stale and fails too.
var loopSuspendKnownDrops = map[string]bool{
	// The NoCall$ outcome repetition (resolveOutcome: a branch run once per
	// win/loss when it does not read X) returns on a suspended call and
	// drops the calls after it, and the lose branch with them.
	"effFlipCoin": true,
}

// TestLoopBodySuspensionReportsItsCursor is the class census for the
// dropped-iterations defect (a non-optional Repeat whose body asked returned
// without SuspendRepeatBody, so its remaining iterations never ran): in the
// non-test effects sources, every `for` loop that runs a sub-ability through
// Resolve and then returns on h.Suspended() must, in that suspension branch
// report a loop continuation through an h.Suspend*
// method other than the plain SuspendContinuation/SuspendUnless (those
// re-enter Sub, never the loop). The resumed loop then re-enters itself at
// the next iteration instead of falling through to its SubAbility$. A
// function is flagged when ANY of its loops has the shape, so one listed
// function must be fixed entirely before its entry can go.
func TestLoopBodySuspensionReportsItsCursor(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	loops, repeatSites, repeatGuarded := 0, 0, false
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					var body *ast.BlockStmt
					switch l := n.(type) {
					case *ast.ForStmt:
						body = l.Body
					case *ast.RangeStmt:
						body = l.Body
					default:
						return true
					}
					for i, st := range body.List {
						if !isResolveCall(st) || i+1 >= len(body.List) {
							continue
						}
						ifs, ok := body.List[i+1].(*ast.IfStmt)
						if !ok || !isSuspendedCheck(ifs.Cond) || !endsInReturn(ifs.Body) {
							continue
						}
						loops++
						if !reportsLoopFrame(ifs.Body) {
							found[fd.Name.Name] = true
						}
						if fd.Name.Name == "effRepeat" {
							repeatSites++
							if !topLevelCall(ifs.Body, "SuspendRepeatBody") {
								repeatGuarded = true
							}
						}
					}
					return true
				})
			}
		}
	}
	if loops == 0 {
		t.Fatal("no Resolve-then-suspend loop found: the census is measuring nothing")
	}
	var bad, stale []string
	for fn := range found {
		if !loopSuspendKnownDrops[fn] {
			bad = append(bad, fn)
		}
	}
	for fn := range loopSuspendKnownDrops {
		if !found[fn] {
			stale = append(stale, fn)
		}
	}
	sort.Strings(bad)
	sort.Strings(stale)
	for _, fn := range bad {
		t.Errorf("%s: a loop returns on its body's suspension without reporting its cursor (h.Suspend*): the remaining iterations are dropped", fn)
	}
	for _, fn := range stale {
		t.Errorf("%s is listed in loopSuspendKnownDrops but no longer drops: remove it", fn)
	}
	// effRepeat's body suspension must report UNCONDITIONALLY: the defect
	// this census exists for was exactly an `if optional` guard around the
	// report, which a conditional-report shape would still accept.
	if repeatSites != 1 {
		t.Errorf("effRepeat has %d Resolve-then-suspend sites, want 1: re-derive this pin", repeatSites)
	}
	if repeatGuarded {
		t.Error("effRepeat must call h.SuspendRepeatBody unconditionally when its body suspends, for every Repeat, optional or not")
	}
	t.Logf("%d Resolve-then-suspend loop sites, %d known drops", loops, len(loopSuspendKnownDrops))
}

// isResolveCall reports whether st is a bare `Resolve(...)` call statement.
func isResolveCall(st ast.Stmt) bool {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "Resolve"
}

// isSuspendedCheck reports whether cond is `h.Suspended()` (any receiver).
func isSuspendedCheck(cond ast.Expr) bool {
	call, ok := cond.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Suspended"
}

func endsInReturn(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}

// reportsLoopFrame reports whether the suspension branch calls an h.Suspend*
// method that records a loop re-entry frame. The call may sit under a
// condition: a loop whose suspended iteration was its last owes no frame
// (effFlipCoin's per-flip cursor), and the census does not judge that
// condition, only that the branch can report at all.
func reportsLoopFrame(b *ast.BlockStmt) bool {
	reports := false
	ast.Inspect(b, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if strings.HasPrefix(name, "Suspend") && name != "SuspendContinuation" && name != "SuspendUnless" {
			reports = true
		}
		return true
	})
	return reports
}

// topLevelCall reports whether the block calls the named method as one of its
// own statements (not under a further condition).
func topLevelCall(b *ast.BlockStmt, name string) bool {
	for _, st := range b.List {
		es, ok := st.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			return true
		}
	}
	return false
}
