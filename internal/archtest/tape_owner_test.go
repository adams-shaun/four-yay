package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestTapeStateOwnedOnlyByTheResolutionKernel is the kernel-era successor of
// TestResumeStateOwnedOnlyByTheResolutionMachinery (ruling T21-e's static
// half for the mid-resolution ask). The legacy Engine.resume frame is gone:
// every mid-resolution ask is now the resolution kernel's (rules/resolve), and
// its per-engine state is the Engine.tape field (a resolve.Kernel whose
// fields are unexported, so package rules can only drive it through its
// methods). What rules CAN still do is overwrite the field wholesale -- and a
// wholesale write anywhere but the three owners below silently drops a posed
// checkpoint or shares a live run between engines, which is exactly a
// log-only-replay bug.
//
// The allow-list is the exact census; both directions are pinned: a writer
// not on the list fails, and a listed function that no longer writes fails,
// so a new writer is a deliberate, reviewed allow-list edit.
//
// Named limits (a syntactic census, not a type-checked one): a write counts
// when any selector in the assignment target's chain is named `tape`, except
// rules/unless_payment.go's unrelated `unlessPayment.tape bool` (a chain whose
// `tape` hangs off a selector named `unlessPayment`, or an assignment of the
// bool literals true/false). An alias bound from e.tape and assigned through
// would defeat it, as would a whole-engine `*e = ...` copy (the one today,
// resolveBoard.Restore's, re-installs the live kernel on the next line, and
// is itself a listed writer).
func TestTapeStateOwnedOnlyByTheResolutionKernel(t *testing.T) {
	allowed := map[string]string{
		"cloneFieldsEngineResolveKernel": "a snapshot clone starts from tape.ForClone(): the posed checkpoint shared, nothing in flight (rules/clone_gen.go, generated from the clone tags)",
		"(*Engine).entryPreview":         "a speculative entry preview carries no kernel state, so its private asks never join the live run (rules/entry_counters.go)",
		"(*resolveBoard).Restore":        "the kernel's own checkpoint restore: the live kernel and epoch survive the in-place copy of S0 (rules/resolve_board.go)",
	}
	writers := kr9TapeFieldWriters(t)
	for fn, sites := range writers {
		if _, ok := allowed[fn]; ok {
			continue
		}
		for _, site := range sites {
			t.Errorf("%s: %s overwrites the engine's resolution-kernel state (Engine.tape); only the clone, "+
				"the entry preview and the kernel's own restore may (ruling T21-e: a posed checkpoint must survive)",
				site, fn)
		}
	}
	for fn := range allowed {
		if _, ok := writers[fn]; !ok {
			t.Errorf("allow-list entry %s is stale: it writes no Engine.tape; prune it or the write moved (ruling T21-e)", fn)
		}
	}
	named := make([]string, 0, len(writers))
	for fn := range writers {
		named = append(named, fn)
	}
	sort.Strings(named)
	for _, fn := range named {
		t.Logf("tape writer %s at %s", fn, strings.Join(writers[fn], ", "))
	}
}

// kr9TapeFieldWriters walks the non-test rules/ sources (recursively) and
// returns every function assigning through a `tape` selector, keyed by the
// receiver-qualified function name, with the sites it writes.
func kr9TapeFieldWriters(t *testing.T) map[string][]string {
	t.Helper()
	dir := filepath.Join("..", "..", "rules")
	files := goSourcesUnder(t, dir)
	if len(files) == 0 {
		t.Fatalf("no rules sources found under %s (cwd %s)", dir, mustCwd())
	}
	out := map[string][]string{}
	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		rel, err := filepath.Rel(filepath.Join("..", ".."), file)
		if err != nil {
			t.Fatalf("rel %s: %v", file, err)
		}
		rel = filepath.ToSlash(rel)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn := funcQualName(fd)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, lhs := range as.Lhs {
					sel, ok := kr9TapeSelectorInChain(lhs)
					if !ok {
						continue
					}
					if len(as.Rhs) == len(as.Lhs) {
						if id, isID := as.Rhs[i].(*ast.Ident); isID && (id.Name == "true" || id.Name == "false") {
							continue
						}
					}
					out[fn] = append(out[fn], fmt.Sprintf("%s:%d", rel, fset.Position(sel.Pos()).Line))
				}
				return true
			})
		}
	}
	return out
}

// kr9TapeSelectorInChain reports whether a selector in the assignment
// target's chain is the engine's `tape` (not unlessPayment's bool flag).
func kr9TapeSelectorInChain(expr ast.Expr) (ast.Expr, bool) {
	for {
		sel, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return nil, false
		}
		if sel.Sel.Name == "tape" {
			if parent, ok := sel.X.(*ast.SelectorExpr); ok && parent.Sel.Name == "unlessPayment" {
				return nil, false
			}
			return sel, true
		}
		expr = sel.X
	}
}
