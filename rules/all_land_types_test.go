package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func allLandTypesEngine(t *testing.T, reg *cards.Registry, names ...string) *Engine {
	t.Helper()
	deck := make([]*cards.Card, 0, 40)
	for _, name := range names {
		deck = append(deck, lookup(t, reg, name))
	}
	for len(deck) < 40 {
		deck = append(deck, lookup(t, reg, "Mountain"))
	}
	opponent := make([]*cards.Card, 40)
	for i := range opponent {
		opponent[i] = lookup(t, reg, "Mountain")
	}
	e := New(seatZeroStart(Config{
		Seed: 42, Names: []string{"land types", "opponent"},
		Decks: [][]*cards.Card{deck, opponent}, Tokens: reg.Tokens,
		NameUniverse: reg.Universe(),
	}))
	e.Advance()
	toMain1(t, e)
	return e
}

func TestAllBasicAndNonbasicLandTypes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := allLandTypesEngine(t, reg, "Dryad of the Ilysian Grove", "Planar Nexus", "Omo, Queen of Vesuva")

	dryad := moveByName(t, e, 0, "Dryad of the Ilysian Grove", state.ZBattlefield)
	planar := moveByName(t, e, 0, "Planar Nexus", state.ZBattlefield)
	omo := moveByName(t, e, 0, "Omo, Queen of Vesuva", state.ZBattlefield)
	land := onBoard(t, e, 0, "Name:Test Land\nTypes:Land\nOracle:x\n")

	// Dryad's static applies to every land we control, and CR 305.6 gives
	// those types their intrinsic mana abilities as part of the derived rules.
	landTypes := e.Derived(land).Types
	for _, want := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest"} {
		if !slices.Contains(landTypes, want) {
			t.Fatalf("Dryad-affected land types %v missing %s", landTypes, want)
		}
	}
	manaAbilities := e.availableManaAbilities(0, land)
	if len(manaAbilities) < 5 {
		t.Fatalf("Dryad-affected land offers %d mana abilities, want at least 5", len(manaAbilities))
	}
	var forestAbility *cards.SA
	for _, ability := range manaAbilities {
		if ability.Params["Produced"] == "G" {
			forestAbility = ability
			break
		}
	}
	if forestAbility == nil {
		t.Fatalf("Dryad-affected land abilities %v lack the Forest intrinsic", manaAbilities)
	}
	e.resolveManaAbility(0, land, forestAbility, false)
	if got := e.G.Players[0].Pool[state.MG]; got != 1 {
		t.Fatalf("tapping Dryad-affected land for its Forest ability produced %d green, want 1", got)
	}

	// Planar Nexus is itself a land with a characteristic-defining static.
	// Cave is corpus-derived (not one of the basic types or a test fixture).
	planarTypes := e.Derived(planar).Types
	if !slices.Contains(planarTypes, "Cave") {
		t.Fatalf("Planar Nexus types %v missing corpus land subtype Cave", planarTypes)
	}

	// Omo's static is gated by the Everything counter: before the counter,
	// this land must not gain the nonbasic land subtype; after its event it
	// must gain both land-type families.
	if got := e.Derived(land).Types; slices.Contains(got, "Cave") {
		t.Fatalf("land without an Everything counter unexpectedly has Cave: %v", got)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: land, Counter: "EVERYTHING", Amount: 1})
	omoLandTypes := e.Derived(land).Types
	if !slices.Contains(omoLandTypes, "Cave") || !slices.Contains(omoLandTypes, "Forest") {
		t.Fatalf("Omo-affected land types %v missing Cave and Forest", omoLandTypes)
	}
	if e.G.Obj(dryad).Zone != state.ZBattlefield || e.G.Obj(planar).Zone != state.ZBattlefield || e.G.Obj(omo).Zone != state.ZBattlefield {
		t.Fatal("test setup: Dryad, Planar Nexus, and Omo must be on the battlefield")
	}
}
