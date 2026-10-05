package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNoAbilitiesMuragandaPetroglyphsBuffsOnlyVanilla is the rules leaf for
// the NoAbilities predicate: Muraganda Petroglyphs' `S:Mode$ Continuous |
// Affected$ Creature.NoAbilities | AddPower$ 2 | AddToughness$ 2` reaches a
// printed-vanilla creature and never an ability-bearing one. Before the
// predicate existed the token failed closed, so the Affected$ spec matched
// nothing and the vanilla creature stayed 2/2 -- the assertion below fails by
// name on unmodified main.
//
// Real corpus cards only (the licensing rule: a corpus .txt is never inlined),
// driven through corpusEngine. Preconditions are asserted first so the test
// cannot pass vacuously: Petroglyphs really carries the Affected$ static, the
// Bear is really printed-vanilla, and the Elves really carries an activated
// ability.
func TestNoAbilitiesMuragandaPetroglyphsBuffsOnlyVanilla(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	petroglyphs := lookup(t, reg, "Muraganda Petroglyphs")
	bear := lookup(t, reg, "Grizzly Bears")
	elves := lookup(t, reg, "Llanowar Elves")

	// Preconditions: the static is on Petroglyphs' face and targets
	// Creature.NoAbilities; the two creatures carry the shapes the result
	// turns on.
	found := false
	for _, st := range petroglyphs.Faces[0].Statics {
		if st.Params["Affected"] == "Creature.NoAbilities" && st.Params["AddPower"] == "2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: Muraganda Petroglyphs has no Affected$ Creature.NoAbilities +2/+2 static")
	}
	if f := bear.Faces[0]; len(f.Keywords) != 0 || len(f.Statics) != 0 || len(f.Triggers) != 0 || len(f.Abilities) != 0 {
		t.Fatalf("precondition: Grizzly Bears is not printed-vanilla: %+v", f)
	}
	if f := elves.Faces[0]; len(f.Abilities) == 0 || f.Abilities[0].Kind != "AB" {
		t.Fatalf("precondition: Llanowar Elves carries no activated ability: %+v", f)
	}

	e := corpusEngine(t, reg, []*cards.Card{petroglyphs, bear, elves}, nil)
	petroglyphsID := moveByName(t, e, 0, "Muraganda Petroglyphs", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	elvesID := moveByName(t, e, 0, "Llanowar Elves", state.ZBattlefield)
	// Assert the fixture is really in the zone the layer scan reads.
	if e.G.Obj(petroglyphsID).Zone != state.ZBattlefield ||
		e.G.Obj(bearID).Zone != state.ZBattlefield ||
		e.G.Obj(elvesID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: fixture not all on the battlefield")
	}

	if got := e.Power(bearID); got != 4 || e.Toughness(bearID) != 4 {
		t.Fatalf("Grizzly Bears under Muraganda Petroglyphs = %d/%d, want 4/4 (NoAbilities must match a vanilla creature)",
			got, e.Toughness(bearID))
	}
	if got := e.Power(elvesID); got != 1 || e.Toughness(elvesID) != 1 {
		t.Fatalf("Llanowar Elves under Muraganda Petroglyphs = %d/%d, want 1/1 (it has an activated ability)",
			got, e.Toughness(elvesID))
	}
}
