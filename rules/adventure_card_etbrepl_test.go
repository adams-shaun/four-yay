package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestETBReplacementFilterMysteriousPathlighter(t *testing.T) {
	t.Parallel()
	reg := freshCorpusRegistry(t, "m/mysterious_pathlighter.txt", "b/bonecrusher_giant_stomp.txt", "g/grizzly_bears.txt", "f/forest.txt", "m/mountain.txt", "p/plains.txt")
	e, cfg := etbreplEngine(t, reg, "Mysterious Pathlighter", "Bonecrusher Giant", "Grizzly Bears")
	pathlighterInHand := searchMoveByName(t, e, "Mysterious Pathlighter", state.ZLibrary)
	e.emit(events.Event{Kind: events.MoveZone, Obj: pathlighterInHand, Player: 0, From: state.ZLibrary, To: state.ZHand})
	castVanilla(t, e, "Mysterious Pathlighter", "CCW")
	var pathlighter state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mysterious Pathlighter" {
			pathlighter = id
		}
	}
	if pathlighter == 0 {
		t.Fatal("Pathlighter did not reach battlefield")
	}
	if n := counterCount(e.G.Obj(pathlighter), "P1P1"); n != 0 {
		t.Fatalf("Pathlighter self-counter = %d, want 0", n)
	}
	for _, tc := range []struct {
		name, mana string
		want       int32
	}{{"Bonecrusher Giant", "CCR", 1}, {"Grizzly Bears", "GG", 0}} {
		id := searchMoveByName(t, e, tc.name, state.ZHand)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand || o.Card == nil ||
			(o.Card.AlternateMode == "Adventure") != (tc.want == 1) {
			t.Fatalf("%s must start in hand with Adventure identity matching expected result", tc.name)
		}
		addMana(t, e, 0, tc.mana)
		idx := -1
		for _, option := range e.Pending().Options {
			if option.Kind == "cast" && option.Obj == id {
				idx = option.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no cast option for %s", tc.name)
		}
		submitChoices(t, e, idx)
		passUntilStackEmpty(t, e, 30)
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().IsCreature() {
			t.Fatalf("%s must be a battlefield creature after resolving; got %+v", tc.name, o)
		}
		if n := counterCount(o, "P1P1"); n != tc.want {
			t.Fatalf("%s P1P1 counters=%d, want %d", tc.name, n, tc.want)
		}
	}
	replayCheck(t, e, cfg)
}
