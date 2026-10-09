package effects

import "testing"

// TestSharesNameWithSelfMatchesTheSource pins the general object matcher's
// `sharesNameWith Self` referent: Self is the filter's own source
// (SpecContext.Source), so Marvin, Murderous Mimic's
// `GainsAbilitiesOf$ Creature.YouCtrl+!sharesNameWith Self` admits a
// creature you control whose name differs from Marvin and excludes the
// source itself. Before the referent was classified the whole token stayed
// unknown and failed closed, so the grant matched nothing.
func TestSharesNameWithSelfMatchesTheSource(t *testing.T) {
	g, id := board(t)
	src := g.Obj(id["myBear"])
	other := g.Obj(id["myFlier"])
	if src == nil || other == nil {
		t.Fatal("precondition: the board's two creatures are missing")
	}
	if src.Face().Name == other.Face().Name {
		t.Fatalf("precondition: %q and %q share a name, so the exclusion cannot be told apart", src.Face().Name, other.Face().Name)
	}

	if !MatchesSpecFrom(g, "Creature.YouCtrl+!sharesNameWith Self", other.ID, 0, src.ID) {
		t.Fatalf("a differently-named creature you control did not match !sharesNameWith Self (source %s, candidate %s)", src.Face().Name, other.Face().Name)
	}
	if MatchesSpecFrom(g, "Creature.YouCtrl+!sharesNameWith Self", src.ID, 0, src.ID) {
		t.Fatalf("the source %s matched its own !sharesNameWith Self exclusion", src.Face().Name)
	}
	if !MatchesSpecFrom(g, "Creature.YouCtrl+sharesNameWith Self", src.ID, 0, src.ID) {
		t.Fatalf("the source %s did not match the positive sharesNameWith Self", src.Face().Name)
	}
	if MatchesSpecFrom(g, "Creature.YouCtrl+sharesNameWith Self", other.ID, 0, src.ID) {
		t.Fatalf("a differently-named creature matched the positive sharesNameWith Self")
	}

	if un := UnknownPredicates("Creature.YouCtrl+!sharesNameWith Self"); len(un) != 0 {
		t.Fatalf("UnknownPredicates(sharesNameWith Self) = %v, want empty", un)
	}
}
