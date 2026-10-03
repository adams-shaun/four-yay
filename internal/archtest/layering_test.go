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

// costVocabularyImports is the closed set of module packages rules/cost may
// import directly (the standard library is always allowed). rules/cost is the
// L2 leaf of the rules-engine lasagna (spec 2026-10-03 §3, W5 step E1): the
// Cost/CostPart vocabulary, cost parsing and cost formatting that every rules
// subsystem shares. It reads only the L1 state vocabulary (state.Mana,
// state.Zone, state.ObjID); anything wider -- effects, events, rules or a rules
// subsystem package -- would make the leaf depend on the layers that are meant
// to depend on it.
//
// To widen it, add the package here with a sentence arguing it is at or below
// L1 (cards, state); a package above L2 never belongs in this list.
var costVocabularyImports = map[string]bool{
	module + "/state": true,
}

// TestCostVocabularyIsALeaf pins rules/cost's direct module imports to
// costVocabularyImports. It skips until the package exists, so the row lands
// before the package (spec §9: "archtest rows for the new package land
// first").
func TestCostVocabularyIsALeaf(t *testing.T) {
	const leaf = module + "/rules/cost"
	p, ok := packages(t)[leaf]
	if !ok {
		t.Skip("rules/cost is not built yet")
	}
	var bad []string
	for imp := range p.imports {
		if !strings.HasPrefix(imp, module+"/") {
			continue // the standard library (go.mod has no requires)
		}
		if !costVocabularyImports[imp] {
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	for _, imp := range bad {
		t.Errorf("%s imports %s; the cost vocabulary is an L2 leaf and may import only %v "+
			"(internal/archtest/layering_test.go costVocabularyImports)", leaf, imp, sortedKeys(costVocabularyImports))
	}
}

// trigmatchImports is the closed set of module packages rules/trigmatch may
// import directly (the standard library is always allowed). rules/trigmatch is
// the L5 trigger-matching layer of the rules-engine lasagna (spec 2026-10-03
// §3, W5 step E3): the Mode$ registry and the per-mode matchers, reading the
// game through trigmatch.Board. It reads the L0/L1 vocabulary (cards, state,
// events), the L2 cost vocabulary and the L3 effects filter and count
// evaluators (matchesSpec's grammar, MatchesPlayerSpec, EvalCountOK).
//
// To widen it, add the package with a sentence arguing it is below L5 (L4
// rules/chars at most). rules, a sibling L5 package (pay, combat) or the L6
// resolution kernel never belongs here.
var trigmatchImports = map[string]bool{
	module + "/cards":      true,
	module + "/state":      true,
	module + "/events":     true,
	module + "/effects":    true,
	module + "/rules/cost": true,
}

// TestTrigmatchImportsStayBelowL5 pins rules/trigmatch's direct module
// imports to trigmatchImports.
func TestTrigmatchImportsStayBelowL5(t *testing.T) {
	const pkg = module + "/rules/trigmatch"
	p, ok := packages(t)[pkg]
	if !ok {
		t.Skip("rules/trigmatch is not built yet")
	}
	var bad []string
	for imp := range p.imports {
		if !strings.HasPrefix(imp, module+"/") {
			continue // the standard library (go.mod has no requires)
		}
		if !trigmatchImports[imp] {
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	for _, imp := range bad {
		t.Errorf("%s imports %s; the trigger-matching layer is L5 and may import only %v "+
			"(internal/archtest/layering_test.go trigmatchImports)", pkg, imp, sortedKeys(trigmatchImports))
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

// combatImports is the closed set of module packages rules/combat may import
// directly. rules/combat is an L5 subsystem package of the rules-engine
// lasagna (spec 2026-10-03 §3, W5 step E5): attack and block legality, read
// through the combat.Board interface package rules implements. It reads the
// L0/L1 vocabulary (cards, state, events) and the L3 filter and restriction
// grammar (effects); anything else -- rules, a sibling subsystem, decision or
// a bot layer -- would invert the layering.
//
// To widen it, add the package here with a sentence arguing it sits below
// L5; rules and its other subsystem packages never belong in this list.
var combatImports = map[string]bool{
	module + "/cards":   true,
	module + "/state":   true,
	module + "/events":  true,
	module + "/effects": true,
}

// TestCombatImportsStayBelowRules pins rules/combat's direct module imports
// to combatImports. It skips until the package exists.
func TestCombatImportsStayBelowRules(t *testing.T) {
	const sub = module + "/rules/combat"
	p, ok := packages(t)[sub]
	if !ok {
		t.Skip("rules/combat is not built yet")
	}
	var bad []string
	for imp := range p.imports {
		if !strings.HasPrefix(imp, module+"/") {
			continue // the standard library (go.mod has no requires)
		}
		if !combatImports[imp] {
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	for _, imp := range bad {
		t.Errorf("%s imports %s; the combat subsystem may import only %v "+
			"(internal/archtest/layering_test.go combatImports)", sub, imp, sortedKeys(combatImports))
	}
}

// combatBoardMethods is the method count of combat.Board, the read-only
// interface rules/combat reads the game through (rules/combat/board.go). It
// is a SHRINK-ONLY ratchet on the pattern of internal/codeshape's: measured
// 17 when the package landed (W5 step E5). A change that removes a method
// lowers the constant in the same commit; NEVER raise it -- derive a new fact
// from an existing method (Game, Keywords, MatchesSpec) instead of widening
// the interface.
const combatBoardMethods = 17

// TestCombatBoardOnlyShrinks counts combat.Board's methods (embedded
// interfaces are counted by name and fail: Board declares every method
// itself, so its size is visible here).
func TestCombatBoardOnlyShrinks(t *testing.T) {
	dir := filepath.Join("..", "..", "rules", "combat")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Skip("rules/combat is not built yet")
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
						t.Errorf("combat.Board embeds %v; declare its methods directly so this ratchet sees them", m.Type)
						continue
					}
					n += len(m.Names)
				}
			}
		}
	}
	if !found {
		t.Fatal("rules/combat declares no Board interface; the ratchet would run vacuously")
	}
	switch {
	case n > combatBoardMethods:
		t.Errorf("combat.Board has %d methods, above the frozen ceiling of %d. This is a SHRINK-ONLY "+
			"ratchet: derive the fact from an existing Board method instead of adding one. A raised "+
			"constant is a MAJOR review finding.", n, combatBoardMethods)
	case n < combatBoardMethods:
		t.Logf("combat.Board has %d methods, below the ceiling of %d: lower combatBoardMethods in "+
			"internal/archtest/layering_test.go to %d in the same commit.", n, combatBoardMethods, n)
	default:
		t.Logf("combat.Board: %d methods (at ceiling)", n)
	}
}
