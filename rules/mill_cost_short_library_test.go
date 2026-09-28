package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMillikinMillCostIsPayableWithEmptyLibrary pins CR 701.13a: mill N
// moves all remaining cards, including zero cards, rather than making a cost
// unpayable when the library is shorter than N.
func TestMillikinMillCostIsPayableWithEmptyLibrary(t *testing.T) {
	t.Parallel()
	millikin, ok := testutil.CorpusRegistry(t).Lookup("Millikin")
	if !ok {
		t.Fatal("corpus missing Millikin")
	}
	e := handEngine(t, millikin)
	source := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZHand, To: state.ZBattlefield})
	e.G.Obj(source).SummonSick = false
	e.G.SetZone(state.ZLibrary, 0, nil)
	if e.G.Obj(source) == nil || e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: Millikin is not on the battlefield")
	}
	if len(e.G.Zone(state.ZLibrary, 0)) != 0 {
		t.Fatal("precondition: library is not empty")
	}
	if len(millikin.Faces) == 0 || len(millikin.Faces[0].Abilities) == 0 {
		t.Fatal("precondition: Millikin has no face ability")
	}
	manaAbility := millikin.Faces[0].Abilities[0]
	parsed := ParseCost(manaAbility.Params["Cost"])
	if manaAbility.API != "Mana" || len(parsed.Mill) != 1 || parsed.Mill[0].N != 1 {
		t.Fatalf("precondition: Millikin ability is not the expected Mill<1> mana ability: %+v", manaAbility)
	}
	if !e.manaAbilityPayable(0, source, manaAbility) {
		t.Fatal("Millikin must be payable with an empty library")
	}
	e.resolveManaAbility(0, source, manaAbility, false)
	if e.G.Players[0].Pool[state.MC] != 1 || !e.G.Obj(source).Tapped {
		t.Fatalf("empty-library Millikin activation did not complete: pool=%+v tapped=%v", e.G.Players[0].Pool, e.G.Obj(source).Tapped)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API Mill" {
			t.Fatal("Millikin used the unimplemented Mill path")
		}
	}
}
