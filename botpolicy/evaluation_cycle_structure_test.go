package botpolicy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestNoUnguardedRecursionInPolicyCallGraph is the ACTIVE half of the L1
// evaluator cycle guard (design 2026-09-28 §7: "every evaluator that follows
// spell/ETB/recursion dependencies needs a path-local visiting bitset and an
// absolute budget").
//
// It resolves the ticket's premise mechanically instead of by prose: it
// builds the direct-call graph of botpolicy's PRODUCTION files with go/ast
// and asserts the only recursion reachable anywhere in the package is the
// allowlisted Clamp constraint repair (Clamp -> sameControllerChoices /
// setPropSharedChoices -> Clamp), bounded at three Clamp frames because each
// projection clears the very flag that would re-enter it.
//
// The premise it pins: NO evaluator in this package follows spell/ETB/
// recursion dependencies over other evaluations' scores -- castScore,
// cardWorth, abilityScore, rankOption and the combat arms read only the
// current Board. The sanctioned recursion entry for the day one lands is
// evaluateDependencies (botpolicy/evaluation_cycle.go): a dependency
// evaluator must express its graph as a []dependencyNode and let the
// depth-32 / 4096-node walker with the path-local visiting bitset do the
// recursion. If this test fails for you, that is its job:
//
//   - route the new dependency evaluation through evaluateDependencies
//     instead of Go call recursion, or
//   - extend the allowlist below ONLY with an explicit, documented bound
//     (the Clamp entry names its three-frame projection bound).
//
// Soundness limits, stated plainly: the walk follows direct syntactic calls
// (including calls made inside function literals, attributed to the
// enclosing named function) and conservatively resolves a call on an
// unknown local receiver by matching the method name across the package, so
// it can add false edges but never miss a same-package name. It does NOT
// follow recursion through function VALUES passed as arguments (today the
// package passes only leaf scorers that way: chooseLowest's cardWorth and
// the target ranker closures), through goroutines (botpolicy starts none)
// or through reflection (botpolicy uses none).
func TestNoUnguardedRecursionInPolicyCallGraph(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)

	type fn struct{ recv, name string }
	keyOf := func(f fn) string {
		if f.recv == "" {
			return f.name
		}
		return f.recv + "." + f.name
	}
	// pkgName is the package-qualified identity; bareName indexes by method
	// name so a call on an unknown local receiver resolves conservatively.
	pkgName := map[string]fn{}
	bareName := map[string][]fn{}
	edges := map[string]map[string]bool{}
	byName := func(name string) []fn { return bareName[name] }

	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	type pending struct {
		caller string
		sel    string
		pkg    string // "" = same-package candidate, "ext" = skip
	}
	var pend []pending
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		// Imported package identifiers, so a package-qualified call is never
		// mistaken for a same-package method name. Aliased imports use the
		// alias; unaliased ones use the last path element (the standard
		// library's versioned paths, math/rand/v2, are the only exception the
		// corpus needs and vN is stripped).
		imported := map[string]bool{}
		for _, imp := range af.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			base := path.Base(ipath)
			if imp.Name != nil {
				base = imp.Name.Name
			} else {
				for i := 0; i < 2; i++ {
					if len(base) > 1 && base[len(base)-2] == '/' && base[len(base)-1] >= '0' && base[len(base)-1] <= '9' {
						base = base[:len(base)-2]
					}
				}
			}
			if base == "." || base == "_" {
				continue
			}
			imported[base] = true
		}
		for _, decl := range af.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			recv := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				tt := fd.Recv.List[0].Type
				if star, ok := tt.(*ast.StarExpr); ok {
					tt = star.X
				}
				if id, ok := tt.(*ast.Ident); ok {
					recv = id.Name
				}
			}
			f := fn{recv, fd.Name.Name}
			k := keyOf(f)
			pkgName[k] = f
			bareName[f.name] = append(bareName[f.name], f)
			edges[k] = map[string]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch e := call.Fun.(type) {
				case *ast.Ident:
					// A bare call can only name a same-package function.
					pend = append(pend, pending{caller: k, sel: e.Name})
				case *ast.SelectorExpr:
					x, ok := e.X.(*ast.Ident)
					if !ok {
						return true // chained selector or composite; leaf shape
					}
					if imported[x.Name] {
						return true // package-qualified: outside this package
					}
					pend = append(pend, pending{caller: k, sel: e.Sel.Name, pkg: "?" + x.Name})
				default:
					return true
				}
				return true
			})
		}
	}
	// Resolve the pending calls after every function is registered, so call
	// order across files cannot hide an edge. A selector call resolves to
	// every same-package function carrying the name (conservative across
	// receiver types); an unknown name adds nothing.
	for _, p := range pend {
		for _, f := range byName(p.sel) {
			edges[p.caller][keyOf(f)] = true
		}
	}

	// Tarjan SCC over the whole production call graph.
	indexOf := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	counter := 0
	var sccs [][]string
	var visit func(v string)
	visit = func(v string) {
		indexOf[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range sortedKeys(edges[v]) {
			if _, seen := indexOf[w]; !seen {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && indexOf[w] < low[v] {
				low[v] = indexOf[w]
			}
		}
		if low[v] == indexOf[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				sort.Strings(scc)
				sccs = append(sccs, scc)
			}
		}
	}
	var keys []string
	for k := range edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, seen := indexOf[k]; !seen {
			visit(k)
		}
	}
	var selfLoops []string
	for _, k := range keys {
		if edges[k][k] {
			selfLoops = append(selfLoops, k)
		}
	}

	// The allowlist: exactly one multi-node SCC, the bounded Clamp repair,
	// and no self-recursion anywhere.
	want := []string{"Clamp", "sameControllerChoices", "setPropSharedChoices"}
	sort.Strings(want)
	if len(sccs) != 1 {
		t.Fatalf("botpolicy call graph recursion changed: found %d mutually recursive groups %v and self-recursion %v; the only sanctioned entry for recursion is evaluateDependencies (botpolicy/evaluation_cycle.go) -- route the new dependency evaluation through it, or extend the allowlist with an explicit bound",
			len(sccs), formatSCCs(sccs), selfLoops)
	}
	if fmt.Sprint(sccs[0]) != fmt.Sprint(want) {
		t.Fatalf("botpolicy call graph recursion changed: got cycle %v, want only the bounded Clamp repair %v; the only sanctioned entry for recursion is evaluateDependencies (botpolicy/evaluation_cycle.go) -- route the new dependency evaluation through it, or extend the allowlist with an explicit bound",
			sccs[0], want)
	}
	if len(selfLoops) != 0 {
		t.Fatalf("botpolicy call graph gained self-recursion: %v; the only sanctioned entry for recursion is evaluateDependencies (botpolicy/evaluation_cycle.go) -- route it through there, or extend the allowlist with an explicit bound", selfLoops)
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatSCCs(sccs [][]string) string {
	parts := make([]string, len(sccs))
	for i, scc := range sccs {
		parts[i] = "[" + strings.Join(scc, " <-> ") + "]"
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
