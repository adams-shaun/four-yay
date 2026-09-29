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
//   - A call through a function-typed value (a parameter, local, struct
//     field or package-level var of signature type) is INDIRECT: it edges to
//     every function VALUE that flowed into that value. Slots are keyed on
//     the OBJECT, so a value stored in one function and invoked from another
//     (a struct field or package var) resolves, and value flows propagate
//     slot-to-slot to a fixpoint, so a callback passed through a CHAIN of
//     parameters is visible at the slot the innermost function calls.
//     Passing a value to a SAME-PACKAGE callee is not itself a call: the
//     callee invokes it only where its own body calls the parameter, which
//     the indirect pass records.
//   - FAIL CLOSED on higher-order calls that cannot be resolved, in TWO ways:
//     (a) a named same-package function or method value passed as an ARGUMENT
//     to an external or value callee may be invoked by it, so the caller gets
//     an edge to it; (b) an indirect call through a slot that may hold an
//     untraceable value -- assigned from a map lookup, type assertion or
//     external call, taint-propagated through parameter chains, or never
//     bound in-package at all -- edges to every same-package function value
//     whose signature is assignable to the slot. A function literal passed as
//     an argument is already an edge from the node that defines it.
//   - A method selection (x.M) is a DIRECT call to M, never a func-typed slot,
//     so an external method call is not mistaken for an indirect call.
//   - A function value assigned to a func-typed local is tracked; when the
//     local is called, the node edges to the value. A func-typed local
//     initialised from a call expression (the ranker factories
//     removalRanker/effectRanker) edges to the FUNCTION THAT PRODUCED it,
//     whose returned-literal body is an edge from itself.
//   - A function literal lexically inside a node is an edge from that node,
//     so an escaping or immediately-invoked closure remains reachable.
//
// This can add false edges (sound, never misses a same-package call), which
// is why the allowlist in the caller names the one real cycle. Indexed
// function values, interface dispatch (including resolved local declarations
// and unresolved foreign interface selections), and factory-produced values
// fail closed to signature-compatible local functions.
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
	// valueTypes records each function-value node's callable signature (a
	// method value's without its receiver) so the fail-closed resolution below
	// can edge only to values that could actually occupy a slot.
	valueTypes := map[string]types.Type{}
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
	// isInterfaceMethod reports whether o declares an interface method: its
	// signature has a receiver whose underlying type is an interface. A call
	// to (or a bound value of) such a declaration is DYNAMIC -- it dispatches
	// to whichever concrete method the dynamic receiver provides -- so it must
	// never be treated as a direct call to the bodiless declaration node.
	isInterfaceMethod := func(o types.Object) bool {
		fn, ok := o.(*types.Func)
		if !ok {
			return false
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() == nil {
			return false
		}
		_, isIface := sig.Recv().Type().Underlying().(*types.Interface)
		return isIface
	}
	// valueNode resolves a function-VALUE expression to a node name: a
	// named same-package function/method (bare or method value) or a literal.
	// A bound INTERFACE method value (i.run) is not a value that belongs to
	// any one declaration, so it is deliberately not resolved here: returning
	// false makes the caller treat the slot as untraceable and fail closed to
	// the assignable implementations, rather than flowing the edge to the
	// bodiless declaration node.
	valueNode := func(e ast.Expr) (string, bool) {
		switch v := e.(type) {
		case *ast.Ident:
			if n, ok := funcObjName(info.Uses[v]); ok {
				if t := info.TypeOf(v); t != nil {
					valueTypes[n] = t
				}
				return n, true
			}
		case *ast.SelectorExpr:
			if sel, ok := info.Selections[v]; ok && sel.Obj() != nil {
				if isInterfaceMethod(sel.Obj()) {
					return "", false
				}
				if n, ok := funcObjName(sel.Obj()); ok {
					if t := info.TypeOf(v); t != nil {
						valueTypes[n] = t
					}
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
	// slotOf returns a stable flow key for a func-typed object. It keys on the
	// OBJECT itself, not the enclosing function: a struct field or
	// package-level var assigned in one function and called from another still
	// shares one slot, and a call site resolves to exactly the slot its
	// caller bound. The object's declared type is recorded for the
	// assignability-filtered fail-closed below.
	slotIDs := map[types.Object]string{}
	slotType := map[string]types.Type{}
	slotCount := 0
	slotOf := func(_ string, o types.Object) (string, bool) {
		if o == nil {
			return "", false
		}
		if _, ok := o.Type().Underlying().(*types.Signature); !ok {
			return "", false
		}
		if id, ok := slotIDs[o]; ok {
			return id, true
		}
		slotCount++
		id := fmt.Sprintf("slot#%d:%s", slotCount, o.Name())
		slotIDs[o] = id
		slotType[id] = o.Type()
		return id, true
	}
	// slotOfExpr resolves an expression that IS a func-typed variable, field
	// or parameter to its slot key. A method selection (x.M) is a DIRECT call
	// to M, not a value sitting in a func-typed slot, so it is excluded: only a
	// bare variable/parameter or a struct FIELD access is an indirect call
	// target.
	slotOfExpr := func(e ast.Expr) (string, bool) {
		switch v := e.(type) {
		case *ast.Ident:
			return slotOf("", info.Uses[v])
		case *ast.SelectorExpr:
			if sel, ok := info.Selections[v]; ok && sel.Obj() != nil {
				if sel.Kind() != types.FieldVal {
					return "", false
				}
				return slotOf("", sel.Obj())
			}
			return "", false
		}
		return "", false
	}
	// isFuncLit reports whether e is a function literal. A literal is already
	// an edge from the node that defines it, so it is not also treated as a
	// flowed value at an argument or assignment site.
	isFuncLit := func(e ast.Expr) bool {
		_, ok := e.(*ast.FuncLit)
		return ok
	}
	// assignSlot resolves the destination of an assignment to a func-typed
	// slot: a bare variable/parameter or a struct field.
	assignSlot := func(lhs ast.Expr, enclosing string) (string, bool) {
		switch v := lhs.(type) {
		case *ast.Ident:
			o := info.Defs[v]
			if o == nil {
				o = info.Uses[v]
			}
			return slotOf(enclosing, o)
		case *ast.SelectorExpr:
			if sel, ok := info.Selections[v]; ok && sel.Obj() != nil {
				return slotOf(enclosing, sel.Obj())
			}
		}
		return "", false
	}
	flows := map[string]map[string]bool{}     // slot -> function values assigned to it
	slotFrom := map[string]map[string]bool{}  // slot -> other slots whose values it may receive
	tainted := map[string]bool{}              // slot may hold a value we could not trace
	indirect := map[string]map[string]bool{}  // owner -> slot called indirectly
	dynamicCalls := map[string][]types.Type{} // unresolved callable signatures
	recordFlow := func(slot, target string) {
		if flows[slot] == nil {
			flows[slot] = map[string]bool{}
		}
		flows[slot][target] = true
	}
	recordSlotFlow := func(dest, src string) {
		if slotFrom[dest] == nil {
			slotFrom[dest] = map[string]bool{}
		}
		slotFrom[dest][src] = true
	}
	// bindArg records how a call argument feeds a callee parameter slot: a
	// function value, another slot, or an untraceable source.
	bindArg := func(paramSlot string, arg ast.Expr) {
		if isFuncLit(arg) {
			return
		}
		if vn, ok := valueNode(arg); ok {
			recordFlow(paramSlot, vn)
			return
		}
		if src, ok := slotOfExpr(arg); ok {
			recordSlotFlow(paramSlot, src)
			return
		}
		if call, ok := arg.(*ast.CallExpr); ok {
			if fn := calleeObj(call.Fun); fn != nil {
				if vn, ok := funcObjName(fn); ok {
					recordFlow(paramSlot, vn)
					return
				}
			}
		}
		tainted[paramSlot] = true
	}

	var walkNode func(nodeName string, body *ast.BlockStmt)
	walkNode = func(nodeName string, body *ast.BlockStmt) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				ln := litName(x.Pos())
				add(nodeName, ln)
				valueTypes[ln] = info.TypeOf(x)
				walkNode(ln, x.Body)
				return false
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i >= len(x.Lhs) {
						continue
					}
					slot, ok := assignSlot(x.Lhs[i], nodeName)
					if !ok {
						continue
					}
					if vn, ok := valueNode(rhs); ok {
						recordFlow(slot, vn)
						continue
					}
					if src, ok := slotOfExpr(rhs); ok {
						recordSlotFlow(slot, src)
						continue
					}
					// A call result assigned to a func-typed slot: attribute
					// it to the producing function, whose returned literal is
					// an edge from itself.
					if call, ok := rhs.(*ast.CallExpr); ok {
						if fn := calleeObj(call.Fun); fn != nil {
							if vn, ok := funcObjName(fn); ok {
								recordFlow(slot, vn)
								// A factory may return any function value compatible
								// with this slot. Keep the producer edge, but force
								// the eventual indirect call to fail closed too.
								tainted[slot] = true
								continue
							}
						}
					}
					// Untraceable source (map lookup, type assertion, external
					// call) -- the slot may hold anything of its type.
					tainted[slot] = true
				}
			case *ast.CallExpr:
				// Indexed function values (map/slice elements) have no object
				// slot to resolve. Conservatively include signature-compatible
				// same-package function values.
				if _, indexed := x.Fun.(*ast.IndexExpr); indexed {
					if funType := info.TypeOf(x.Fun); funType != nil {
						if _, ok := funType.Underlying().(*types.Signature); ok {
							dynamicCalls[nodeName] = append(dynamicCalls[nodeName], funType)
						}
					}
				}
				// The callee may be a direct same-package call, an indirect
				// call through a function-typed value, or an external call.
				if fn := calleeObj(x.Fun); fn != nil {
					if vn, ok := funcObjName(fn); ok {
						add(nodeName, vn)
						// Interface dispatch is not represented by the edge to the
						// bodiless interface-method declaration. This is keyed on the
						// RESOLVED OBJECT, not the selection receiver: a promoted
						// (embedded) interface method, or one reached through a
						// pointer, has a concrete expression receiver while the
						// declaration still dispatches dynamically. Add every
						// signature-compatible concrete implementation.
						if isInterfaceMethod(fn) {
							dynamicCalls[nodeName] = append(dynamicCalls[nodeName], callableType(fn))
						}
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
								paramSlot, ok := slotOf(vn, sig.Params().At(i))
								if !ok {
									continue
								}
								bindArg(paramSlot, arg)
							}
						}
					}
				} else {
					// A method selected from a foreign interface cannot resolve
					// through calleeObj, but its receiver selection still identifies
					// dynamic dispatch. Keep this complementary receiver-keyed arm
					// only for unresolved calls; same-package interface declarations
					// are handled above by their resolved object, including promoted
					// methods whose expression receiver is concrete.
					if selExpr, ok := x.Fun.(*ast.SelectorExpr); ok {
						if sel := info.Selections[selExpr]; sel != nil && sel.Kind() == types.MethodVal {
							if _, isInterface := sel.Recv().Underlying().(*types.Interface); isInterface {
								if funType := info.TypeOf(x.Fun); funType != nil {
									dynamicCalls[nodeName] = append(dynamicCalls[nodeName], funType)
								}
							}
						}
					}
					// FAIL CLOSED on higher-order calls we cannot resolve: a
					// named same-package function or method passed as an argument
					// to an external or value callee may be invoked by it. (A
					// literal is already an edge from this node.)
					for _, arg := range x.Args {
						if isFuncLit(arg) {
							continue
						}
						if av, ok := valueNode(arg); ok {
							add(nodeName, av)
						}
					}
					// An indirect call through a func-typed value: a parameter,
					// local or struct field.
					if slot, ok := slotOfExpr(x.Fun); ok {
						if indirect[nodeName] == nil {
							indirect[nodeName] = map[string]bool{}
						}
						indirect[nodeName][slot] = true
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
			valueTypes[name] = callableType(info.Defs[fd.Name])
			walkNode(name, fd.Body)
		}
	}
	// Propagate slot-to-slot value flows to a fixpoint, so a function value
	// passed through a chain of callback parameters is visible at the slot
	// the innermost function actually calls. Taint propagates the same way: a
	// parameter fed an untraceable value taints everything downstream.
	changed := true
	for changed {
		changed = false
		for dest, srcs := range slotFrom {
			for src := range srcs {
				for target := range flows[src] {
					if flows[dest] == nil {
						flows[dest] = map[string]bool{}
					}
					if !flows[dest][target] {
						flows[dest][target] = true
						changed = true
					}
				}
				if tainted[src] && !tainted[dest] {
					tainted[dest] = true
					changed = true
				}
				for through := range slotFrom[src] {
					if slotFrom[dest] == nil {
						slotFrom[dest] = map[string]bool{}
					}
					if !slotFrom[dest][through] {
						slotFrom[dest][through] = true
						changed = true
					}
				}
			}
		}
	}
	// Resolve indirect calls: edge to every value that flowed into the slot.
	// FAIL CLOSED when the slot may hold an untraceable value, or has no
	// tracked flow at all (a parameter whose caller is outside the package,
	// or a field never assigned in-package): edge to every same-package
	// function value, so no recursion can hide behind a source the flow
	// analysis could not see.
	for owner, slots := range indirect {
		for slot := range slots {
			for target := range flows[slot] {
				add(owner, target)
			}
			if tainted[slot] || len(flows[slot]) == 0 {
				for fn, ft := range valueTypes {
					if st := slotType[slot]; st != nil && ft != nil && types.AssignableTo(ft, st) {
						add(owner, fn)
					}
				}
			}
		}
	}

	// Resolve dynamic indexed/interface calls after every local function and
	// literal has contributed its callable type.
	for owner, signatures := range dynamicCalls {
		for _, sig := range signatures {
			for fn, ft := range valueTypes {
				if ft != nil && types.AssignableTo(ft, sig) {
					add(owner, fn)
				}
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

// callableType returns the callable signature of a function object, stripping
// a method receiver so a method value compares assignable to a plain
// func-typed slot.
func callableType(o types.Object) types.Type {
	fn, ok := o.(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	if sig.Recv() == nil {
		return sig
	}
	return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
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
// and function-value flows (arguments, method values, func-typed locals,
// struct fields and package-level vars), propagating value flows and taint
// slot-to-slot to a fixpoint, and fails closed to the assignable same-package
// function values for any indirect call through a slot it could not fully
// trace. It can add false edges but never miss a same-package name or a
// value-mediated callback, including indexed map/slice values, interface
// dispatch (resolved local declarations are keyed on the method object;
// unresolved foreign interface calls use the interface receiver selection),
// and factory-returned values. It still does NOT follow a function value that
// escapes through goroutines (botpolicy starts none) or reflection (botpolicy
// uses none). The regression probes
// TestPolicyCallGraphCatchesFunctionValueRecursion,
// TestPolicyCallGraphCatchesIndirectEvaluatorRecursion and
// TestPolicyCallGraphCatchesDynamicEvaluatorRecursion pin the callback and
// dynamic-dispatch holes this guard closes.
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
// buildProbeGraph type-checks one probe source file and returns its call
// graph. The probes share the source importer so the package of each probe is
// self-contained.
func buildProbeGraph(t *testing.T, src string) policyCallGraph {
	t.Helper()
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
	return buildPolicyCallGraph(fset, []*ast.File{af}, info, "probe")
}

func TestPolicyCallGraphCatchesFunctionValueRecursion(t *testing.T) {
	const src = `package probe

func walk(f func()) { f() }

func score() { walk(score) }

func leaf() {}

func run() { walk(leaf) }
`
	g := buildProbeGraph(t, src)

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

// TestPolicyCallGraphCatchesIndirectEvaluatorRecursion covers the CLASS the
// one-hop callback probe left open: recursion reached through an indirect call
// whose value flow passes through more than one function or through storage
// the one-hop pass did not follow. Each case must yield an SCC containing
// score, and its acyclic control must not.
func TestPolicyCallGraphCatchesIndirectEvaluatorRecursion(t *testing.T) {
	cyclic := map[string]struct {
		src  string
		want []string
	}{
		"two-callback chain": {
			src: `package probe
func a(f func()) { f() }
func b(f func()) { a(f) }
func score() { b(score) }
`,
			want: []string{"a", "b", "score"},
		},
		"struct field": {
			src: `package probe

type holder struct{ f func() }

func (h holder) run() { h.f() }

func score() {
	var h holder
	h.f = score
	h.run()
}
`,
			want: []string{"holder.run", "score"},
		},
		"package var": {
			src: `package probe

var g func()

func walk() { g() }

func score() {
	g = score
	walk()
}
`,
			want: []string{"score", "walk"},
		},
		"method value": {
			src: `package probe

type h struct{}

func (h) walk(f func()) { f() }

func score() {
	var x h
	x.walk(score)
}
`,
			want: []string{"h.walk", "score"},
		},
	}
	acyclic := map[string]string{
		"two-callback no callback": `package probe
func a(f func()) {}
func b(f func()) { a(f) }
func run() { b(leaf) }
func leaf() {}
`,
		"struct field never called back": `package probe

type holder struct{ f func() }

func (h holder) run() { h.f() }

func score() {
	var h holder
	h.f = leaf
	h.run()
}
func leaf() {}
`,
		"package var no callback": `package probe

var g func()

func walk() { g() }

func score() {
	g = leaf
	walk()
}
func leaf() {}
`,
	}

	for name, tc := range cyclic {
		t.Run("cycle/"+name, func(t *testing.T) {
			g := buildProbeGraph(t, tc.src)
			want := fmt.Sprint(tc.want)
			var found bool
			for _, scc := range g.sccs {
				if fmt.Sprint(scc) == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("indirect recursion %s not detected: sccs=%s self=%v edges=%v", want, formatSCCs(g.sccs), g.selfLoops, g.edges)
			}
		})
	}
	for name, src := range acyclic {
		t.Run("acyclic/"+name, func(t *testing.T) {
			g := buildProbeGraph(t, src)
			// Precondition: the probe is non-trivial -- there is at least one
			// closure/indirect call path to follow. run or score must be a
			// caller of something.
			if len(g.edges) == 0 {
				t.Fatalf("probe produced no edges; precondition failed: %v", g.edges)
			}
			for _, scc := range g.sccs {
				for _, n := range scc {
					if strings.Contains(n, "score") || n == "run" || strings.Contains(n, "walk") {
						t.Fatalf("false cycle on the acyclic control: %s", formatSCCs(g.sccs))
					}
				}
			}
			if len(g.selfLoops) != 0 {
				t.Fatalf("acyclic control gained self-recursion: %v", g.selfLoops)
			}
		})
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
