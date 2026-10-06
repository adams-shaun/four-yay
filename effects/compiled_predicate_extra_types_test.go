package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestCompiledPredicateExtraTypesAware guards the layer walk's fast path: a
// compiled type predicate must answer the SAME derived type set the textual
// oracle reads through SpecContext.ExtraTypes, so the sidecar can stay
// installed for a derived-characteristics match (rules/layers.go
// matchesWithTypes) instead of being cleared for the whole walk.
//
// The carrier is a printed Land whose ExtraTypes has been made a Creature --
// exactly the layer-4 state a manifested Forest under Maskwood Nexus reaches
// (CR 708.5 base {Creature} plus an AddAllCreatureTypes$ grant). Before the
// fix matchesCompiledBase called hasType, which reads the printed face and
// returned a definite PredicateNo for `Creature.YouCtrl`, bypassing the
// ExtraTypes-aware textual oracle.
func TestCompiledPredicateExtraTypesAware(t *testing.T) {
	g, ids := board(t)
	land := g.Obj(ids["myLand"])
	if land == nil {
		t.Fatal("precondition: myLand must be in the game")
	}
	// The rule reads the object in its zone and asks for a type the printed
	// face does NOT carry; assert both so the test cannot pass vacuously.
	if land.Zone != state.ZBattlefield {
		t.Fatalf("precondition: myLand zone = %v, want battlefield", land.Zone)
	}
	if hasType(land, "Creature") {
		t.Fatal("precondition: myLand must print no Creature type")
	}

	ps := CompilePredicatePrograms([]string{"Creature.YouCtrl", "Creature.Goblin"})

	// The derived type set the layer walk binds: a Creature that is also
	// controlled by You, plus a Goblin subtype for the term path.
	sc := SpecContext{
		You:               0,
		Source:            ids["myBear"],
		PredicatePrograms: ps,
		ExtraTypes:        []string{"Creature", "Goblin", "Land"},
		ExtraTypesOwner:   ids["myLand"],
	}

	if got := ps.Evaluate("Creature.YouCtrl", g, land, sc); got != PredicateYes {
		t.Fatalf("compiled Creature.YouCtrl over ExtraTypes = %v, want PredicateYes", got)
	}
	if !MatchesSpecCtx(g, "Creature.YouCtrl", ids["myLand"], sc) {
		t.Fatal("sidecar-backed MatchesSpecCtx(Creature.YouCtrl) = false, want true")
	}
	if got := ps.Evaluate("Creature.Goblin", g, land, sc); got != PredicateYes {
		t.Fatalf("compiled Creature.Goblin over ExtraTypes = %v, want PredicateYes", got)
	}
	if !MatchesSpecCtx(g, "Creature.Goblin", ids["myLand"], sc) {
		t.Fatal("sidecar-backed MatchesSpecCtx(Creature.Goblin) = false, want true")
	}

	// Negative control 1: the same printed Land with NO derived Creature in
	// ExtraTypes stays a definite PredicateNo -- the fix must not turn the
	// compiled path into a wildcard.
	noCreature := sc
	noCreature.ExtraTypes = []string{"Land"}
	if got := ps.Evaluate("Creature.YouCtrl", g, land, noCreature); got != PredicateNo {
		t.Fatalf("compiled Creature.YouCtrl without a derived Creature = %v, want PredicateNo", got)
	}
	if MatchesSpecCtx(g, "Creature.YouCtrl", ids["myLand"], noCreature) {
		t.Fatal("MatchesSpecCtx(Creature.YouCtrl) without a derived Creature = true, want false")
	}

	// Negative control 2: the derived Creature is controlled by the OPPONENT,
	// so YouCtrl must fail even though the type is derived.
	oppCtrl := sc
	oppCtrl.You = 1
	if got := ps.Evaluate("Creature.YouCtrl", g, land, oppCtrl); got != PredicateNo {
		t.Fatalf("compiled Creature.YouCtrl against an opponent-controlled object = %v, want PredicateNo", got)
	}
	if MatchesSpecCtx(g, "Creature.YouCtrl", ids["myLand"], oppCtrl) {
		t.Fatal("MatchesSpecCtx(Creature.YouCtrl) against an opponent-controlled object = true, want false")
	}
}
