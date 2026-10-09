package cards

// Task agent-20261009T091513Z-3c5153cc: the chain-aware mana-ability
// classifier (IsManaAbilitySA). A Forge "AB$ ChooseColor | SubAbility$
// DBMana" head is an activated mana ability by CR 605.1a -- no target, could
// add mana as it resolves -- but the head's own API word is not Mana, so the
// head-word-only classifier left the whole chain on the ordinary activated
// path. These leaves pin the classifier and both membership lists on the
// real corpus: exactly the four chain carriers classify, the 27 other
// ChooseColor cards stay ordinary, and the compiled span matches the textual
// fallback.

import (
	"path/filepath"
	"slices"
	"testing"
)

// manaChainHeads are the corpus cards whose AB$ ChooseColor head reaches a
// DB$ Mana sub-ability. Measured at the 2026-10-09 pin: 31 files carry
// AB$ ChooseColor; exactly these four chain to DB$ Mana.
var manaChainHeads = []string{
	"Foraging Wickermaw",
	"Nykthos, Shrine to Nyx",
	"Nyx Lotus",
	"Rhystic Cave",
}

func openCardsTestCorpus(t *testing.T) *Registry {
	t.Helper()
	reg, err := SharedCorpus(filepath.Join("..", ".cards"))
	if err != nil {
		t.Skipf("corpus unavailable: %v", err)
	}
	return reg
}

// chooseColorHead returns the first activated ChooseColor ability on f.
func chooseColorHead(f *Face) *SA {
	for _, a := range f.Abilities {
		if a != nil && a.IsActivated() && a.API == "ChooseColor" {
			return a
		}
	}
	return nil
}

func TestManaChainClassifierClassifiesTheCorpusChainHeads(t *testing.T) {
	reg := openCardsTestCorpus(t)
	for _, name := range manaChainHeads {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s missing from the corpus", name)
		}
		f := c.Faces[0]
		head := chooseColorHead(f)
		if head == nil {
			t.Fatalf("precondition: %s has no activated ChooseColor ability", name)
		}
		sub := ManaChainProduction(head)
		if sub == nil || sub.API != "Mana" || sub.Kind != "DB" {
			t.Fatalf("precondition: %s's ChooseColor head does not reach a DB$ Mana: sub=%+v", name, sub)
		}
		if !IsManaAbilitySA(head) {
			t.Errorf("IsManaAbilitySA(%s's ChooseColor head) = false, want true", name)
		}
		if !slices.Contains(f.ManaAbilities(), head) {
			t.Errorf("%s's ChooseColor head is absent from Face.ManaAbilities()", name)
		}
	}
}

// TestManaChainCompiledListMatchesTheTextualFallback pins the two
// classification sites to the same predicate: the compiled ManaAbilityIDs
// span Face.ManaAbilities returns and the unbounded textual walk must list
// the same abilities, in the same order, for every chain carrier.
func TestManaChainCompiledListMatchesTheTextualFallback(t *testing.T) {
	reg := openCardsTestCorpus(t)
	for _, name := range manaChainHeads {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s missing from the corpus", name)
		}
		f := c.Faces[0]
		compiled := f.ManaAbilities()
		textual := f.textualManaAbilities()
		if len(compiled) != len(textual) {
			t.Fatalf("%s: compiled ManaAbilities lists %d, textual lists %d", name, len(compiled), len(textual))
		}
		for i := range compiled {
			if compiled[i] != textual[i] {
				t.Fatalf("%s: compiled[%d]=%q, textual[%d]=%q", name, i, compiled[i].Line, i, textual[i].Line)
			}
		}
	}
}

// TestManaChainNykthosListsBothAbilitiesWithoutDoubleCounting: Nykthos
// carries a plain AB$ Mana AND the ChooseColor chain; both are mana
// abilities, and each appears exactly once (the span arithmetic in
// compiled_catalog pairs the append pass with the count pass).
func TestManaChainNykthosListsBothAbilitiesWithoutDoubleCounting(t *testing.T) {
	reg := openCardsTestCorpus(t)
	c, ok := reg.Lookup("Nykthos, Shrine to Nyx")
	if !ok {
		t.Fatalf("precondition: Nykthos missing from the corpus")
	}
	f := c.Faces[0]
	head := chooseColorHead(f)
	if head == nil {
		t.Fatalf("precondition: Nykthos has no activated ChooseColor ability")
	}
	plain := 0
	for _, a := range f.Abilities {
		if a != nil && a.IsActivated() && a.API == "Mana" {
			plain++
		}
	}
	if plain != 1 {
		t.Fatalf("precondition: Nykthos has %d plain AB$ Mana abilities, want 1", plain)
	}
	mas := f.ManaAbilities()
	if len(mas) != 2 {
		t.Fatalf("Nykthos ManaAbilities = %d entries (%v), want the plain Mana plus the chain", len(mas), saLines(mas))
	}
	n := 0
	for _, a := range mas {
		if a == head {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Nykthos chain head appears %d times in ManaAbilities, want exactly once", n)
	}
}

// TestManaChainNonManaChooseColorStaysOrdinary is the negative population:
// the other ChooseColor cards chain to Animate/Effect/DestroyAll bodies, not
// a DB$ Mana, so the classifier and both lists must leave them alone.
func TestManaChainNonManaChooseColorStaysOrdinary(t *testing.T) {
	reg := openCardsTestCorpus(t)
	for _, name := range []string{"Kavu Chameleon", "Alchor's Tomb", "Root Greevil"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s missing from the corpus", name)
		}
		f := c.Faces[0]
		head := chooseColorHead(f)
		if head == nil {
			t.Fatalf("precondition: %s has no activated ChooseColor ability", name)
		}
		if ManaChainProduction(head) != nil {
			t.Fatalf("precondition: %s's ChooseColor head reaches a DB$ Mana; pick a negative fixture", name)
		}
		if IsManaAbilitySA(head) {
			t.Errorf("IsManaAbilitySA(%s's ChooseColor head) = true, want false (no DB$ Mana in the chain)", name)
		}
		if slices.Contains(f.ManaAbilities(), head) {
			t.Errorf("%s's non-mana ChooseColor head is listed in Face.ManaAbilities()", name)
		}
	}
}

// TestManaChainClassifierEdgeShapes pins the predicate's edges: nil, an
// unactivated SA, and a head-word Mana ability.
func TestManaChainClassifierEdgeShapes(t *testing.T) {
	if IsManaAbilitySA(nil) {
		t.Error("IsManaAbilitySA(nil) = true, want false")
	}
	if IsManaAbilitySA(&SA{Kind: "ST", API: "ChooseColor"}) {
		t.Error("a static ChooseColor SA classified as a mana ability")
	}
	if !IsManaAbilitySA(&SA{Kind: "AB", API: "Mana"}) {
		t.Error("AB$ Mana not classified as a mana ability")
	}
	if !IsManaAbilitySA(&SA{Kind: "AB", API: "ManaReflected"}) {
		t.Error("AB$ ManaReflected not classified as a mana ability")
	}
}

func saLines(sas []*SA) []string {
	out := make([]string, 0, len(sas))
	for _, sa := range sas {
		if sa == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, sa.API)
	}
	return out
}
