package oraclegen

// Ordering tests for CharmCombinations after the repeat-mode fix: within each
// pick count the repeat-free combinations must come first, in Choices$ order,
// so the audit tries a legal all-distinct plan before the C(n+k-1,k) repeating
// ones. For a Charm without CanRepeatModes$ the enumeration is unchanged.

import (
	"testing"
)

// charmCombinationRepeats reports whether one combination picks any mode twice.
func charmCombinationRepeats(c CharmCombination) bool {
	seen := map[string]bool{}
	for _, m := range c.Modes {
		if seen[m.Label()] {
			return true
		}
		seen[m.Label()] = true
	}
	return false
}

// TestCharmCombinationsRepeatFreeFirst pins Unite the Coalition's enumeration:
// five picks from five repeatable modes is C(5+5-1,5) = 126 ordered
// multisets, and every repeat-free combination precedes every repeated one.
func TestCharmCombinationsRepeatFreeFirst(t *testing.T) {
	reg := censusRegistry(t)
	card, ok := reg.Lookup("Unite the Coalition")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("Unite the Coalition missing from corpus")
	}
	face := card.Faces[0]
	var charmFound bool
	for _, ab := range face.Abilities {
		if ab.Kind == "SP" && ab.API == "Charm" {
			charmFound = true
			if ab.Params["CharmNum"] != "5" || ab.Params["CanRepeatModes"] != "True" {
				t.Fatalf("corpus charm parameters changed: %+v", ab.Params)
			}
		}
	}
	if !charmFound {
		t.Fatal("precondition: Unite the Coalition has no Charm ability")
	}

	combos := CharmCombinations(face)
	if len(combos) != 126 {
		t.Fatalf("got %d combinations, want 126 (C(9,5))", len(combos))
	}
	if len(combos[0].Modes) != 5 {
		t.Fatalf("first combination has %d modes, want 5", len(combos[0].Modes))
	}
	if charmCombinationRepeats(combos[0]) {
		t.Fatalf("first combination repeats a mode: %+v", combos[0].Modes)
	}
	firstRepeat := -1
	for i, c := range combos {
		if charmCombinationRepeats(c) {
			firstRepeat = i
			break
		}
	}
	if firstRepeat < 0 {
		t.Fatal("precondition: a repeat-mode charm enumerated no repeated combination")
	}
	for i := 0; i < firstRepeat; i++ {
		if charmCombinationRepeats(combos[i]) {
			t.Fatalf("combination %d repeats before the first repeated one at %d", i, firstRepeat)
		}
	}
}

// TestCharmCombinationsNonRepeatOrderUnchanged pins a non-repeat Charm's
// enumeration to strictly increasing Choices$ positions: the first
// combination is the first k modes in order, the last is the last k, and no
// combination repeats. The pick counts of a non-repeat charm never take the
// repeat walk, so this is the historical ordering.
func TestCharmCombinationsNonRepeatOrderUnchanged(t *testing.T) {
	reg := censusRegistry(t)
	card, ok := reg.Lookup("Ashling's Command")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("Ashling's Command missing from corpus")
	}
	face := card.Faces[0]
	modes := CharmModes(face)
	if len(modes) != 4 {
		t.Fatalf("precondition: got %d modes, want 4", len(modes))
	}
	combos := CharmCombinations(face)
	// Two picks from four distinct modes: C(4,2) = 6.
	if len(combos) != 6 {
		t.Fatalf("got %d combinations, want 6", len(combos))
	}
	for i, c := range combos {
		if charmCombinationRepeats(c) {
			t.Fatalf("non-repeat charm enumerated a repeated combination at %d: %+v", i, c.Modes)
		}
	}
	for i := 0; i < 2; i++ {
		if combos[0].Modes[i].Label() != modes[i].Label() {
			t.Fatalf("first combination mode %d = %q, want %q", i, combos[0].Modes[i].Label(), modes[i].Label())
		}
		if combos[len(combos)-1].Modes[i].Label() != modes[len(modes)-2+i].Label() {
			t.Fatalf("last combination mode %d = %q, want %q", i, combos[len(combos)-1].Modes[i].Label(), modes[len(modes)-2+i].Label())
		}
	}
}
