package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestRiddlesInTheDarkRememberedPeekMovesBothPiles(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Riddles in the Dark", "Grizzly Bears", "Island", "Swamp", "Mountain")
	cardID := searchMoveByName(t, e, "Riddles in the Dark", state.ZHand)
	wantNames := []string{"Grizzly Bears", "Island", "Swamp", "Mountain"}
	var reveal []state.ObjID
	for _, name := range wantNames {
		var found state.ObjID
		for _, zone := range []state.Zone{state.ZLibrary, state.ZHand} {
			for _, id := range e.G.Zone(zone, 0) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && o.Face().Name == name {
					found = id
					break
				}
			}
			if found != 0 {
				break
			}
		}
		if found == 0 {
			t.Fatalf("fixture card %q missing from library/hand", name)
		}
		if e.G.Obj(found).Zone == state.ZHand {
			e.emit(events.Event{Kind: events.MoveZone, Obj: found, From: state.ZHand, To: state.ZLibrary})
		}
		reveal = append(reveal, found)
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	order := append([]state.ObjID(nil), reveal...)
	for _, id := range lib {
		if !fofContainsID(reveal, id) {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order})
	if len(reveal) != 4 || e.G.Obj(reveal[0]).Zone != state.ZLibrary || e.G.Obj(reveal[3]).Zone != state.ZLibrary {
		t.Fatalf("precondition: reveal cards not in library: %+v", reveal)
	}
	addMana(t, e, 0, "WWUUBBRRGG")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == cardID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Riddles in the Dark not castable: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = passUntilNonPriority(t, e, 40)
	if d != nil && d.ResumeKind == "look_ack" {
		submitChoices(t, e, d.Options[0].Index)
		d = passUntilNonPriority(t, e, 40)
	}
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "twopiles_split" {
		t.Fatalf("resolution ask = %+v, want remembered TwoPiles split", d)
	}
	if d.Player != 0 || len(d.Options) != 4 {
		t.Fatalf("split ask player/options = %d/%d, want caster/4", d.Player, len(d.Options))
	}
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	d = e.Pending()
	if d == nil || d.ResumeKind != "twopiles_pick" || d.Player != 1 {
		t.Fatalf("pile pick = %+v, want opponent chooser", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	for _, id := range []state.ObjID{reveal[0], reveal[2]} {
		if z := e.G.Obj(id).Zone; z != state.ZHand {
			t.Fatalf("chosen pile object %d zone=%s, want hand", id, z)
		}
	}
	for _, id := range []state.ObjID{reveal[1], reveal[3]} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("unchosen pile object %d zone=%s, want graveyard", id, z)
		}
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle {
			t.Fatalf("Riddles pile bodies shuffled: %+v", e.L.Events[start:])
		}
	}
	replayCheck(t, e, cfg)
}
