// Shape guards on cards.GrantedKeywordAbility, the layer-6 AddKeyword$ grant
// synthesizer (CR 613.1f). The granted body must be byte-identical to the
// printed expansion's SA, so the anti-drift guard compares the two
// constructions for every head in scope.
package cards

import (
	"maps"
	"testing"
)

// printedExpandedSA parses a card that prints the given K: line and returns
// the single ability expandKeywords minted from it.
func printedExpandedSA(t *testing.T, line string) *SA {
	t.Helper()
	src := "Name:Test Carrier\nManaCost:2\nTypes:Artifact\nK:" + line + "\nOracle:x\n"
	c, diags := ParseBytes("test.txt", []byte(src))
	if len(diags) > 0 {
		t.Fatalf("parse diagnostics for %q: %+v", line, diags)
	}
	c.Faces[0].expandKeywords()
	if len(c.Faces[0].Abilities) != 1 {
		t.Fatalf("%q printed expansion produced %d abilities, want 1", line, len(c.Faces[0].Abilities))
	}
	return c.Faces[0].Abilities[0]
}

// TestGrantedKeywordAbilityShapes pins the dispatcher's totality and its
// anti-drift property: each in-scope head returns the right body, and the
// granted body's parameters equal the printed expansion's for the same line;
// every other head returns nil (fail closed).
func TestGrantedKeywordAbilityShapes(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"Cycling:1 U", "TypeCycling:Sliver:3", "Saddle:2", "Crew:1"} {
		granted := GrantedKeywordAbility(line)
		if granted == nil {
			t.Fatalf("GrantedKeywordAbility(%q) = nil, want a body", line)
		}
		if granted.Params["KeywordLine"] != line {
			t.Fatalf("GrantedKeywordAbility(%q) KeywordLine = %q, want the line", line, granted.Params["KeywordLine"])
		}
		printed := printedExpandedSA(t, line)
		if granted.API != printed.API || granted.Kind != printed.Kind {
			t.Fatalf("%q granted = %s/%s, printed = %s/%s", line, granted.Kind, granted.API, printed.Kind, printed.API)
		}
		if !maps.Equal(granted.Params, printed.Params) {
			t.Fatalf("%q granted params drifted from the printed expansion:\n granted %v\n printed %v", line, granted.Params, printed.Params)
		}
	}
	// Fail closed on heads that mint no activated ability this build models.
	for _, line := range []string{"Flying", "Equip:1", "Reconfigure:2", "Level up:1"} {
		if got := GrantedKeywordAbility(line); got != nil {
			t.Fatalf("GrantedKeywordAbility(%q) = %+v, want nil (fail closed)", line, got)
		}
	}
	// The cycling-only wrapper keeps its old contract.
	if GrantedCyclingAbility("Saddle:2") != nil {
		t.Fatal("GrantedCyclingAbility must stay cycling-only")
	}
	if GrantedCyclingAbility("Cycling:2") == nil {
		t.Fatal("GrantedCyclingAbility lost the cycling arm")
	}
}
