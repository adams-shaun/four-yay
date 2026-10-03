package archtest

import (
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

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
