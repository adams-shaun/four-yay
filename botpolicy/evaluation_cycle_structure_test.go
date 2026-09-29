package botpolicy

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// policyCallGraph is the reachability graph of botpolicy's production
// functions. Nodes are named functions/methods ("Board.cardWorth") and
// function literals ("lit@file:line:col"); edges are calls that may be made
// while executing the source node.
type policyCallGraph struct {
	edges     map[string]map[string]bool
	sccs      [][]string
	selfLoops []string
}

// buildPolicyCallGraph builds the call graph of a type-checked package.
//
// It is TYPE-AWARE, not merely syntactic, so a recursive evaluator reached
// only through a function VALUE is still caught. botpolicy passes scorers as
// arguments (chooseLowest's worth parameter, the ranker closures); a syntactic
// pass sees only the direct calls at each site and misses walk<->score in
// `func walk(f func()) { f() }; func score() { walk(score) }`. The rules:
//
//   - Every call is attributed to its innermost enclosing node (a named
//     function/method, or the function literal itself).
//   - A call to a same-package named function/method is a direct edge.
//   - A call through a function-typed value (a parameter, local or struct
//     field of signature type) is INDIRECT: it edges to every function VALUE
//     that flowed into that value, tracked by the assignment and
//     argument-binding passes. Passing a value to a SAME-PACKAGE callee is
//     not itself a call: the callee invokes it only where its own body calls
//     the parameter, which the indirect pass records.
//   - FAIL CLOSED on higher-order calls that cannot be resolved: a named
//     same-package function or method value passed as an ARGUMENT to an
//     external or value callee may be invoked by it, so the caller gets an
//     edge to it. A function literal passed as an argument is already an edge
//     from the node that defines it.
//   - A function value assigned to a func-typed local is tracked; when the
//     local is called, the node edges to the value. A func-typed local
//     initialised from a call expression (the ranker factories
//     removalRanker/effectRanker) edges to the FUNCTION THAT PRODUCED it,
//     whose returned-literal body is an edge from itself.
//   - A function literal lexically inside a node is an edge from that node,
//     so an escaping or immediately-invoked closure remains reachable.
//
// This can add false edges (sound, never misses a same-package call), which
// is why the allowlist in the caller names the one real cycle.
func buildPolicyCallGraph(fset *token.FileSet, files []*ast.File, info *types.Info, pkgPath string) policyCallGraph {
	g := policyCallGraph{edges: map[string]map[string]bool{}}
	add := func(a, b string) {
		if a == "" || b == "" {
			return
		}
		if g.edges[a] == nil {
			g.edges[a] = map[string]bool{}
		}
		g.edges[a][b] = true
	}
	litName := func(pos token.Pos) string {
		return "lit@" + fset.Position(pos).String()
	}
	// funcObjName names a same-package *types.Func (method receiver-prefixed).
	funcObjName := func(o types.Object) (string, bool) {
		fn, ok := o.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
			return "", false
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			return "", false
		}
		if sig.Recv() == nil {
			return fn.Name(), true
		}
		rt := sig.Recv().Type()
		if p, ok := rt.(*types.Pointer); ok {
			rt = p.Elem()
		}
		if n, ok := rt.(*types.Named); ok {
			return n.Obj().Name() + "." + fn.Name(), true
		}
		return "", false
	}
	// valueNode resolves a function-VALUE expression to a node name: a
	// named same-package function/method (bare or method value) or a literal.
	valueNode := func(e ast.Expr) (string, bool) {
		switch v := e.(type) {
		case *ast.Ident:
			if n, ok := funcObjName(info.Uses[v]); ok {
				return n, true
			}
		case *ast.SelectorExpr:
			if sel, ok := info.Selections[v]; ok && sel.Obj() != nil {
				if n, ok := funcObjName(sel.Obj()); ok {
					return n, true
				}
			}
		case *ast.FuncLit:
			return litName(v.Pos()), true
		}
		return "", false
	}
	// calleeObj resolves a call's Fun to a same-package *types.Func: a bare
	// name, a method call, or a package method value. External calls and
	// calls through values resolve to nil.
	calleeObj := func(fun ast.Expr) *types.Func {
		if id, ok := fun.(*ast.Ident); ok {
			if fn, ok := info.Uses[id].(*types.Func); ok {
				if _, same := funcObjName(fn); same {
					return fn
				}
			}
			return nil
		}
		if sel, ok := fun.(*ast.SelectorExpr); ok {
			if s, ok := info.Selections[sel]; ok && s.Obj() != nil {
				if fn, ok := s.Obj().(*types.Func); ok {
					if _, same := funcObjName(fn); same {
						return fn
					}
				}
			}
		}
		return nil
	}
	// slotOf builds the flow key for a func-typed var/param object.
	slotOf := func(owner string, o types.Object) (string, bool) {
		if o == nil {
			return "", false
		}
		if _, ok := o.Type().Underlying().(*types.Signature); !ok {
			return "", false
		}
		return owner + "\x00" + o.Name(), true
	}
	flows := map[string]map[string]bool{}
	indirect := map[string]map[string]bool{}
	recordFlow := func(slot, target string) {
		if flows[slot] == nil {
			flows[slot] = map[string]bool{}
		}
		flows[slot][target] = true
	}

	var walkNode func(nodeName string, body *ast.BlockStmt)
	walkNode = func(nodeName string, body *ast.BlockStmt) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				ln := litName(x.Pos())
				add(nodeName, ln)
				walkNode(ln, x.Body)
				return false
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i >= len(x.Lhs) {
						continue
					}
					id, ok := x.Lhs[i].(*ast.Ident)
					if !ok {
						continue
					}
					o := info.Defs[id]
					if o == nil {
						o = info.Uses[id]
					}
					slot, ok := slotOf(nodeName, o)
					if !ok {
						continue
					}
					if vn, ok := valueNode(rhs); ok {
						recordFlow(slot, vn)
						continue
					}
					// A call result assigned to a func-typed slot: attribute
					// it to the producing function, whose returned literal is
					// an edge from itself.
					if call, ok := rhs.(*ast.CallExpr); ok {
						if fn := calleeObj(call.Fun); fn != nil {
							if vn, ok := funcObjName(fn); ok {
								recordFlow(slot, vn)
							}
						}
					}
				}
			case *ast.CallExpr:
				// The callee may be a direct same-package call or an indirect
				// call through a function-typed value.
				if fn := calleeObj(x.Fun); fn != nil {
					if vn, ok := funcObjName(fn); ok {
						add(nodeName, vn)
						// Bind arguments to the callee's func-typed params. The
						// callee invokes them only where its own body calls the
						// param, which the indirect pass records -- so passing a
						// value to a same-package callee is not itself a call.
						sig, _ := fn.Type().(*types.Signature)
						if sig != nil {
							for i, arg := range x.Args {
								if i >= sig.Params().Len() {
									break
								}
								slot, ok := slotOf(vn, sig.Params().At(i))
								if !ok {
									continue
								}
								if av, ok := valueNode(arg); ok {
									recordFlow(slot, av)
								}
							}
						}
					}
				} else {
					// FAIL CLOSED on higher-order calls we cannot resolve: a
					// named same-package function or method passed as an argument
					// to an external or value callee may be invoked by it. (A
					// literal is already an edge from this node.)
					for _, arg := range x.Args {
						if _, isLit := arg.(*ast.FuncLit); isLit {
							continue
						}
						if av, ok := valueNode(arg); ok {
							add(nodeName, av)
						}
					}
					if id, ok := x.Fun.(*ast.Ident); ok {
						// Indirect call through a func-typed parameter or local.
						if o := info.Uses[id]; o != nil {
							if slot, ok := slotOf(nodeName, o); ok {
								if indirect[nodeName] == nil {
									indirect[nodeName] = map[string]bool{}
								}
								indirect[nodeName][slot] = true
							}
						}
					} else if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
						// Indirect call through a func-typed struct field.
						if o := info.Uses[sel.Sel]; o != nil {
							if slot, ok := slotOf(nodeName, o); ok {
								if indirect[nodeName] == nil {
									indirect[nodeName] = map[string]bool{}
								}
								indirect[nodeName][slot] = true
							}
						}
					}
				}
			}
			return true
		})
	}

	for _, af := range files {
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
			name := fd.Name.Name
			if recv != "" {
				name = recv + "." + name
			}
			if g.edges[name] == nil {
				g.edges[name] = map[string]bool{}
			}
			walkNode(name, fd.Body)
		}
	}
	// Resolve indirect calls: edge to every value that flowed into the slot.
	for owner, slots := range indirect {
		for slot := range slots {
			for target := range flows[slot] {
				add(owner, target)
			}
		}
	}

	// Tarjan SCC + self-loops over the whole graph.
	indexOf := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	counter := 0
	var visit func(v string)
	visit = func(v string) {
		indexOf[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range sortedBoolKeys(g.edges[v]) {
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
				g.sccs = append(g.sccs, scc)
			}
		}
	}
	var keys []string
	for k := range g.edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, seen := indexOf[k]; !seen {
			visit(k)
		}
	}
	for _, k := range keys {
		if g.edges[k][k] {
			g.selfLoops = append(g.selfLoops, k)
		}
	}
	return g
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// typeCheckPolicy parses and type-checks a set of production files. The
// source importer is used rather than the gc exporter so the package's local
// dependencies (decision, state) resolve inside a fresh worktree.
func typeCheckPolicy(t *testing.T, fset *token.FileSet, paths []string, pkgPath string) ([]*ast.File, *types.Info) {
	t.Helper()
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		files = append(files, af)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(error) {}}
	if _, err := conf.Check(pkgPath, fset, files, info); err != nil {
		t.Fatalf("type-check %s: %v", pkgPath, err)
	}
	return files, info
}

// TestNoUnguardedRecursionInPolicyCallGraph is the ACTIVE half of the L1
// evaluator cycle guard (design 2026-09-28 §7: "every evaluator that follows
// spell/ETB/recursion dependencies needs a path-local visiting bitset and an
// absolute budget").
//
// It resolves the ticket's premise mechanically instead of by prose: it
// builds the TYPE-AWARE call graph of botpolicy's PRODUCTION files (including
// recursion mediated by function VALUES -- callbacks, method values, func-typed
// locals) and asserts the only recursion reachable anywhere in the package is
// the allowlisted Clamp constraint repair (Clamp -> sameControllerChoices /
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
// and function-value flows (arguments, method values, func-typed locals and
// struct fields) and conservatively resolves an unknown local receiver by
// matching the method name across the package, so it can add false edges but
// never miss a same-package name or a value-mediated callback. It still does
// NOT follow a function value that escapes through a package-level variable
// assigned in one function and called in another, through goroutines
// (botpolicy starts none) or through reflection (botpolicy uses none); the
// regression probe TestPolicyCallGraphCatchesFunctionValueRecursion pins the
// callback hole this guard closes.
func TestNoUnguardedRecursionInPolicyCallGraph(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files, info := typeCheckPolicy(t, fset, paths, "github.com/adams-shaun/gorge/botpolicy")
	g := buildPolicyCallGraph(fset, files, info, "github.com/adams-shaun/gorge/botpolicy")

	// The allowlist: exactly one multi-node SCC, the bounded Clamp repair,
	// and no self-recursion anywhere.
	want := []string{"Clamp", "sameControllerChoices", "setPropSharedChoices"}
	sort.Strings(want)
	if len(g.sccs) != 1 {
		t.Fatalf("botpolicy call graph recursion changed: found %d mutually recursive groups %s and self-recursion %v; the only sanctioned entry for recursion is evaluateDependencies (botpolicy/evaluation_cycle.go) -- route the new dependency evaluation through it, or extend the allowlist with an explicit bound",
			len(g.sccs), formatSCCs(g.sccs), g.selfLoops)
	}
	if fmt.Sprint(g.sccs[0]) != fmt.Sprint(want) {
		t.Fatalf("botpolicy call graph recursion changed: got cycle %v, want only the bounded Clamp repair %v; the only sanctioned entry for recursion is evaluateDependencies (botpolicy/evaluation_cycle.go) -- route the new dependency evaluation through it, or extend the allowlist with an explicit bound",
			g.sccs[0], want)
	}
	if len(g.selfLoops) != 0 {
		t.Fatalf("botpolicy call graph gained self-recursion: %v; the only sanctioned entry for recursion is evaluateDependencies (botpolicy/evaluation_cycle.go) -- route it through there, or extend the allowlist with an explicit bound", g.selfLoops)
	}
}

// TestPolicyCallGraphCatchesFunctionValueRecursion is the regression probe
// for the hole the syntactic pass had: recursion that reaches an evaluator
// only through a function VALUE passed as a callback produces no direct call
// edge from the callback's body, so it was invisible. The probe is the exact
// shape that broke the prior guard:
//
//	func walk(f func()) { f() }
//	func score()       { walk(score) }
//
// Combined with the direct edge score -> walk, the callback flow walk -> score
// closes the cycle. The control below (a callback that does NOT call back)
// must stay acyclic, proving the probe detects recursion rather than any
// callback.
func TestPolicyCallGraphCatchesFunctionValueRecursion(t *testing.T) {
	const src = `package probe

func walk(f func()) { f() }

func score() { walk(score) }

func leaf() {}

func run() { walk(leaf) }
`
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "probe.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(error) {}}
	if _, err := conf.Check("probe", fset, []*ast.File{af}, info); err != nil {
		t.Fatalf("type-check probe: %v", err)
	}
	g := buildPolicyCallGraph(fset, []*ast.File{af}, info, "probe")

	// Precondition: the two shapes are genuinely different -- score calls
	// walk, and walk reaches score only through walk's parameter.
	if !g.edges["score"]["walk"] {
		t.Fatalf("probe precondition failed: score does not call walk; edges=%v", g.edges)
	}
	// The callback cycle must be found.
	var found bool
	for _, scc := range g.sccs {
		if fmt.Sprint(scc) == fmt.Sprint([]string{"score", "walk"}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("callback recursion walk<->score not detected: sccs=%s self=%v; the guard must fail closed on function values passed as arguments", formatSCCs(g.sccs), g.selfLoops)
	}
	// The control: run -> walk -> leaf is acyclic (leaf never calls back).
	for _, scc := range g.sccs {
		for _, n := range scc {
			if strings.Contains(n, "leaf") || n == "run" {
				t.Fatalf("false cycle on the acyclic control: %s", formatSCCs(g.sccs))
			}
		}
	}
	if len(g.selfLoops) != 0 {
		t.Fatalf("probe gained self-recursion: %v", g.selfLoops)
	}
}

func formatSCCs(sccs [][]string) string {
	parts := make([]string, len(sccs))
	for i, scc := range sccs {
		parts[i] = "[" + strings.Join(scc, " <-> ") + "]"
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
