package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestHasKeywordLandwalkParameter(t *testing.T) {
	g := state.NewGame([]string{"you"})
	islandID := addKeywordPredicateFixture(t, g, "Landwalk:Island")
	mountainID := addKeywordPredicateFixture(t, g, "Landwalk:Mountain")
	island, mountain := g.Obj(islandID), g.Obj(mountainID)
	if island == nil || island.Zone != state.ZBattlefield || !island.Face().HasKeyword("Landwalk") {
		t.Fatal("precondition: battlefield fixture does not carry Landwalk")
	}
	if mountain == nil || mountain.Zone != state.ZBattlefield || !mountain.Face().HasKeyword("Landwalk") {
		t.Fatal("precondition: comparison fixture does not carry Landwalk")
	}
	islandParam, islandHasParam := island.Face().KeywordParam("Landwalk")
	mountainParam, mountainHasParam := mountain.Face().KeywordParam("Landwalk")
	if !islandHasParam || !mountainHasParam || islandParam == mountainParam {
		t.Fatalf("precondition: Landwalk parameters = %q/%q (present %v/%v), want different present parameters", islandParam, mountainParam, islandHasParam, mountainHasParam)
	}
	spec := "Creature.hasKeywordLandwalk:Island"
	unknown := UnknownPredicates(spec)
	if len(unknown) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want no unknowns", spec, unknown)
	}
	gotIsland := MatchesSpec(g, spec, islandID, 0)
	gotMountain := MatchesSpec(g, spec, mountainID, 0)
	if !gotIsland || gotMountain || gotIsland == gotMountain {
		t.Fatalf("Landwalk:Island results for Islandwalk/Mountainwalk = %v/%v, want true/false", gotIsland, gotMountain)
	}
}

func TestHasKeywordEnchantParameter(t *testing.T) {
	g := state.NewGame([]string{"you"})
	creatureID := addKeywordPredicateFixture(t, g, "Enchant:Creature.YouCtrl:creature you control")
	auraID := addKeywordPredicateFixture(t, g, "Enchant:Aura")
	creatureAura, otherAura := g.Obj(creatureID), g.Obj(auraID)
	if creatureAura == nil || creatureAura.Zone != state.ZBattlefield || !creatureAura.Face().HasKeyword("Enchant") {
		t.Fatal("precondition: battlefield fixture does not carry Enchant")
	}
	if otherAura == nil || otherAura.Zone != state.ZBattlefield || !otherAura.Face().HasKeyword("Enchant") {
		t.Fatal("precondition: comparison fixture does not carry Enchant")
	}
	creatureParam, creatureHasParam := creatureAura.Face().KeywordParam("Enchant")
	auraParam, auraHasParam := otherAura.Face().KeywordParam("Enchant")
	if !creatureHasParam || !auraHasParam || creatureParam == auraParam {
		t.Fatalf("precondition: Enchant parameters = %q/%q (present %v/%v), want different present parameters", creatureParam, auraParam, creatureHasParam, auraHasParam)
	}
	spec := "Creature.hasKeywordEnchant:Creature"
	unknown := UnknownPredicates(spec)
	if len(unknown) != 0 {
		t.Fatalf("UnknownPredicates(%q) = %v, want no unknowns", spec, unknown)
	}
	gotCreature := MatchesSpec(g, spec, creatureID, 0)
	gotAura := MatchesSpec(g, spec, auraID, 0)
	if !gotCreature || gotAura || gotCreature == gotAura {
		t.Fatalf("Enchant:Creature results for Creature/Aura parameters = %v/%v, want true/false", gotCreature, gotAura)
	}
}

func TestUnknownKeywordPredicateParameterizedFailsClosed(t *testing.T) {
	g := state.NewGame([]string{"you"})
	id := addKeywordPredicateFixture(t, g, "Landwalk:Island")
	o := g.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.Face().HasKeyword("Landwalk") {
		t.Fatal("precondition: battlefield fixture does not carry Landwalk")
	}
	spec := "Creature.withHexproof:Ward"
	unknown := UnknownPredicates(spec)
	if len(unknown) != 1 || unknown[0] != "withHexproof:Ward" {
		t.Fatalf("UnknownPredicates(%q) = %v, want [withHexproof:Ward]", spec, unknown)
	}
	if MatchesSpec(g, spec, id, 0) {
		t.Fatalf("unsupported parameterized predicate %q matched", spec)
	}
}
