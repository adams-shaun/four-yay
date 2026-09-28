package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMillikinMillCostMovesTopLibraryCardBeforeMana(t *testing.T) {
	t.Parallel()
	millikin, ok := testutil.CorpusRegistry(t).Lookup("Millikin")
	if !ok {
		t.Fatal("corpus missing Millikin")
	}
	e := handEngine(t, millikin)
	source := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZHand, To: state.ZBattlefield})
	e.G.Obj(source).SummonSick = false
	libCard := e.G.Zone(state.ZLibrary, 0)[0]
	if e.G.Obj(libCard) == nil || e.G.Obj(libCard).Zone != state.ZLibrary {
		t.Fatal("precondition: top library card is not in the library")
	}
	if len(millikin.Faces) == 0 || len(millikin.Faces[0].Abilities) == 0 {
		t.Fatal("precondition: Millikin has no face ability")
	}
	var manaAbility = millikin.Faces[0].Abilities[0]
	parsed := ParseCost(manaAbility.Params["Cost"])
	if manaAbility.API != "Mana" || len(parsed.Mill) != 1 || parsed.Mill[0].N != 1 {
		t.Fatalf("precondition: Millikin ability is not the expected Mill<1> mana ability: %+v", manaAbility)
	}
	e.resolveManaAbility(0, source, manaAbility, false)
	if e.G.Obj(libCard).Zone != state.ZGraveyard {
		t.Fatalf("Millikin did not mill the top card: zone=%s", e.G.Obj(libCard).Zone)
	}
	if e.G.Players[0].Pool[state.MC] != 1 {
		t.Fatalf("Millikin did not add mana after milling: pool=%+v", e.G.Players[0].Pool)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API Mill" {
			t.Fatal("Millikin used the unimplemented Mill path")
		}
	}
}
