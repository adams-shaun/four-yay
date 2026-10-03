package rules

// Restores effects/library_position_absent_test.go on the kernel: a hidden
// library-to-library search with no LibraryPosition$ puts the found card on
// TOP of its owner's library (Forge's default position 0) with one Secret
// LibraryOrder and no degradation Note -- on a synthetic search and on the
// real Knowledge Exploitation, whose declined may-cast leaves the card in
// the library and whose closing DBShuffle runs after the placement.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestSearchAbsentLibraryPositionIsTop(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	found := kr2Put(t, e, 0, kr2Src(t, "Name:Found\nTypes:Sorcery\nOracle:x\n"), state.ZLibrary, false)
	rest := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	rest = rest[:len(rest)-1]
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Seeker",
		"A:SP$ ChangeZone | Origin$ Library | Destination$ Library | ChangeType$ Sorcery | Shuffle$ False"), state.ZHand, false)
	d := kr2Want(t, kr2Cast(t, e, 0, spell), "search")
	from := len(e.L.Events)
	if d = kr2Answer(t, e, d, kr2ObjIdx(t, d, found)); d != nil {
		t.Fatalf("the answered search posed another ask: %+v", d)
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) != len(rest)+1 || lib[0] != found {
		t.Fatalf("library = %v, want Found (%d) on top (absent LibraryPosition$ = TOP)", lib, found)
	}
	for i, id := range rest {
		if lib[i+1] != id {
			t.Fatalf("library below Found changed at %d: %v, want %v", i, lib[1:], rest)
		}
	}
	sawOrder := false
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Note && ev.Obj == spell {
			t.Fatalf("unexpected Note %+v (the absent default must not be loud)", ev)
		}
		if ev.Kind == events.LibraryOrder && ev.Player == 0 && ev.Secret && len(ev.IDs) > 0 && ev.IDs[0] == found {
			sawOrder = true
		}
	}
	if !sawOrder {
		t.Fatal("no Secret LibraryOrder put Found on top")
	}
}

func TestKnowledgeExploitationSearchPlacesChosenOnTop(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	found := kr2Put(t, e, 1, kr2Src(t, "Name:Found\nManaCost:R\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 1\nOracle:x\n"), state.ZLibrary, true)
	g1 := kr2Put(t, e, 1, kr2Src(t, "Name:G1\nManaCost:G\nTypes:Creature\nPT:1/1\nOracle:x\n"), state.ZLibrary, true)
	g0 := kr2Put(t, e, 1, kr2Src(t, "Name:G0\nManaCost:G\nTypes:Creature\nPT:1/1\nOracle:x\n"), state.ZLibrary, true)
	ke := kr2Put(t, e, 0, kr2Corpus(t, "Knowledge Exploitation"), state.ZHand, false)
	d := kr2Cast(t, e, 0, ke)
	if d != nil && d.Kind == decision.KTarget {
		d = kr2Answer(t, e, d, kr2PlayerIdx(t, d, 1))
	}
	d = kr2Want(t, d, "search")
	if len(d.Options) != 1 || d.Options[0].Obj != found {
		t.Fatalf("search offered %+v, want only the instant/sorcery Found", d.Options)
	}
	from := len(e.L.Events)
	d = kr2Want(t, kr2Answer(t, e, d, d.Options[0].Index), "play")
	if d.Player != 0 {
		t.Fatalf("may-cast posed to seat %d, want the searching seat 0", d.Player)
	}
	kr2ObjIdx(t, d, found)
	orderAt := -1
	for i, ev := range e.L.Events[from:] {
		if ev.Kind == events.LibraryOrder && ev.Player == 1 && ev.Secret && len(ev.IDs) >= 3 &&
			ev.IDs[0] == found && ev.IDs[1] == g0 && ev.IDs[2] == g1 {
			orderAt = from + i
		}
		if ev.Kind == events.PutOnStack && ev.Obj == found {
			t.Fatal("Found was cast before the may-cast was answered")
		}
	}
	if orderAt < 0 {
		t.Fatal("no Secret LibraryOrder put Found on top of seat 1's library before the may-cast")
	}
	if lib := e.G.Zone(state.ZLibrary, 1); lib[0] != found {
		t.Fatalf("seat 1 library = %v, want Found on top at the may-cast", lib[:3])
	}
	// Decline the may-cast: Found stays in the library and the shuffle
	// follows the placement.
	if d = kr2Answer(t, e, d); d != nil {
		t.Fatalf("unexpected ask after the declined may-cast: %+v", d)
	}
	if z := e.G.Obj(found).Zone; z != state.ZLibrary {
		t.Fatalf("declined may-cast still moved Found to %s", z)
	}
	shuffled := false
	for _, ev := range e.L.Events[orderAt+1:] {
		if ev.Kind == events.Shuffle && ev.Player == 1 {
			shuffled = true
		}
	}
	if !shuffled {
		t.Fatal("the closing DBShuffle did not run after the LibraryOrder placement")
	}
}
