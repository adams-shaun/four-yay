package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// layering_chars_test.go pins rules/chars, the L4 characteristic-computation
// package of the rules-engine lasagna (spec 2026-10-03 §3, W5 step E4): the
// CR 613 layer walk, read through the chars.Board interface package rules
// implements. Its rows live in their own file (not TestDependencyOrderHolds'
// slice) so the W5 extraction steps running in parallel do not edit the same
// lines; the check is the same transitive one.

// charsImports is the closed set of module packages rules/chars may import
// directly: the L0/L1 vocabulary (cards, state) and the L3 filter and count
// grammar (effects) its Affected$ binding and CDA evaluators use. Anything
// else -- rules, an L5 subsystem, the resolution kernel, a bot layer --
// would invert the layering.
//
// To widen it, add the package here with a sentence arguing it sits below
// L4; rules and its subsystem packages never belong in this list.
var charsImports = map[string]bool{
	module + "/cards":   true,
	module + "/state":   true,
	module + "/effects": true,
}

// TestCharsImportsStayBelowRules pins rules/chars' direct module imports to
// charsImports, and forbids the transitive edges the layering rules out:
// rules itself, the L5 subsystem packages above it (combat, pay, trigmatch),
// the L6 resolution kernel and the bot layer. It skips until the package
// exists.
func TestCharsImportsStayBelowRules(t *testing.T) {
	const sub = module + "/rules/chars"
	pkgs := packages(t)
	p, ok := pkgs[sub]
	if !ok {
		t.Skip("rules/chars is not built yet")
	}
	var bad []string
	for imp := range p.imports {
		if !strings.HasPrefix(imp, module+"/") {
			continue // the standard library (go.mod has no requires)
		}
		if !charsImports[imp] {
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	for _, imp := range bad {
		t.Errorf("%s imports %s; the characteristics layer may import only %v "+
			"(internal/archtest/layering_chars_test.go charsImports)", sub, imp, sortedKeys(charsImports))
	}
	for _, to := range []string{
		module + "/rules",
		module + "/rules/combat",
		module + "/rules/pay",
		module + "/rules/trigmatch",
		module + "/rules/resolve",
		module + "/botpolicy",
	} {
		if p.deps[to] {
			t.Errorf("%s depends on %s (transitively); the dependency order forbids it", sub, to)
		}
	}
}

// charsBoardMethods is the method count of chars.Board, the read-only
// interface rules/chars reads the game through (rules/chars/board.go). It is
// a SHRINK-ONLY ratchet on the pattern of combatBoardMethods: measured 8 when
// the package landed (W5 step E4). A change that removes a method lowers the
// constant in the same commit; NEVER raise it -- derive a new fact from an
// existing method (Game, Active, Matches) instead of widening the interface.
const charsBoardMethods = 8

// TestCharsBoardOnlyShrinks counts chars.Board's methods (embedded
// interfaces are counted by name and fail: Board declares every method
// itself, so its size is visible here).
func TestCharsBoardOnlyShrinks(t *testing.T) {
	dir := filepath.Join("..", "..", "rules", "chars")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Skip("rules/chars is not built yet")
	}
	n, found := 0, false
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok || ts.Name.Name != "Board" {
					continue
				}
				found = true
				for _, m := range it.Methods.List {
					if len(m.Names) == 0 {
						t.Errorf("chars.Board embeds %v; declare its methods directly so this ratchet sees them", m.Type)
						continue
					}
					n += len(m.Names)
				}
			}
		}
	}
	if !found {
		t.Fatal("rules/chars declares no Board interface; the ratchet would run vacuously")
	}
	switch {
	case n > charsBoardMethods:
		t.Errorf("chars.Board has %d methods, above the frozen ceiling of %d. This is a SHRINK-ONLY "+
			"ratchet: derive the fact from an existing Board method instead of adding one. A raised "+
			"constant is a MAJOR review finding.", n, charsBoardMethods)
	case n < charsBoardMethods:
		t.Logf("chars.Board has %d methods, below the ceiling of %d: lower charsBoardMethods in "+
			"internal/archtest/layering_chars_test.go to %d in the same commit.", n, charsBoardMethods, n)
	default:
		t.Logf("chars.Board: %d methods (at ceiling)", n)
	}
}
