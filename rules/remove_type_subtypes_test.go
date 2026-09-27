package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestRemoveTypeCreatureRemovesCreatureSubtypeFromFilters(t *testing.T) {
	purph := corpusCard(t, "Purphoros, God of the Forge")
	e := layerEngine(t)
	id := onBoardCard(t, e, 0, purph)
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken: Purphoros zone = %v, want ZBattlefield", got)
	}
	printed := e.G.Obj(id).Face().Types
	if !hasTypeWord(printed, "Creature") || !hasTypeWord(printed, "God") {
		t.Fatalf("precondition broken: printed Purphoros types = %v, want Creature and God", printed)
	}
	types := e.Derived(id).Types
	if hasTypeWord(types, "Creature") || hasTypeWord(types, "God") {
		t.Fatalf("low-devotion Purphoros types = %v, want neither Creature nor its creature subtype God", types)
	}
	sc := e.specCtx(0, 0)
	sc.DerivedTypes = append(sc.DerivedTypes, effects.ObjectTypes{ID: id, Types: types})
	if effects.MatchesSpecCtx(e.G, "God", id, sc) {
		t.Fatalf("low-devotion Purphoros still matches God with derived types %v", types)
	}
	if effects.MatchesSpecCtx(e.G, "Creature", id, sc) {
		t.Fatalf("low-devotion Purphoros unexpectedly matches Creature with derived types %v", types)
	}
}

func TestRemoveTypePlaneswalkerRemovesWalkerSubtype(t *testing.T) {
	e := layerEngine(t)
	walker := card(t, "Name:Type Strip Walker\nTypes:Legendary Planeswalker Jace\nLoyalty:4\n"+
		"S:Mode$ Continuous | Affected$ Card.Self | RemoveType$ Planeswalker\nOracle:x\n")
	id := onBoardCard(t, e, 0, walker)
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken: walker zone = %v, want ZBattlefield", got)
	}
	printed := e.G.Obj(id).Face().Types
	if !hasTypeWord(printed, "Planeswalker") || !hasTypeWord(printed, "Jace") {
		t.Fatalf("precondition broken: printed walker types = %v", printed)
	}
	types := e.Derived(id).Types
	if hasTypeWord(types, "Planeswalker") || hasTypeWord(types, "Jace") {
		t.Fatalf("RemoveType$ Planeswalker left its subtype in derived types: %v", types)
	}
	if !hasTypeWord(types, "Legendary") {
		t.Fatalf("RemoveType$ Planeswalker removed unrelated supertype: %v", types)
	}
}
