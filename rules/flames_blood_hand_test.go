package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestFlamesOfTheBloodHandPreventsOnlyRememberedLifeGain(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Flames of the Blood Hand"))
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil || o.Face().SpellAbility() == nil {
		t.Fatal("Flames has no compiled spell ability")
	}
	before := len(e.continuous)
	e.resolveAbility(source, 0, []state.Target{{IsPlayer: true, Player: 1}}, o.Face().SpellAbility(), o.Face().SVars)
	if len(e.continuous) <= before {
		t.Fatal("Flames did not register its Effect replacement")
	}
	var found bool
	for _, ce := range e.continuous[before:] {
		if ce.ReplacementEvent == "GainLife" && len(ce.RememberedPlayers) == 1 && ce.RememberedPlayers[0] == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("GainLife replacement did not capture the targeted player")
	}
	if e.G.Players[0].Life == e.G.Players[1].Life {
		t.Fatal("test requires distinct life totals")
	}
	life := e.G.Players[1].Life
	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 3})
	if e.G.Players[1].Life != life {
		t.Fatalf("remembered player's life = %d, want unchanged %d", e.G.Players[1].Life, life)
	}
	if got := e.L.Events[len(e.L.Events)-1].Text; got != "prevented: cannot gain life" {
		t.Fatalf("last event = %q, want canonical prevention Note", got)
	}
	if e.G.Players[0].Life == e.G.Players[1].Life {
		t.Fatal("comparison player must remain distinguishable")
	}
	other := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if e.G.Players[0].Life != other+3 {
		t.Fatalf("unremembered player's life = %d, want %d", e.G.Players[0].Life, other+3)
	}
	if len(e.L.Events) <= start {
		t.Fatal("life-gain events were not recorded")
	}
}
