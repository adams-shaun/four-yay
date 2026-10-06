package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// An imprint association is not itself a Defined$ Imprinted object while its
// card remains in the library (CR 607.2a). Only the paired deferred return
// from Green Sun's DigMultiple may consume its still-in-library pile.
func TestUnpairedImprintedLibraryFetchKeepsExileGate(t *testing.T) {
	_, ability, vars := corpusRiderSA(t, "Green Sun's Twilight", "")
	returnSA := cards.ResolveSVar(vars, "RestBottom")
	if ability == nil || ability.API != "DigMultiple" || returnSA == nil ||
		returnSA.Params["Defined"] != "Imprinted" || returnSA.Params["Origin"] != "Library" ||
		returnSA.Params["RandomOrder"] != "True" || returnSA.Params["NoShuffle"] != "True" {
		t.Fatalf("precondition: corpus paired return = %+v / %+v", ability, returnSA)
	}
	h := newHost(t, 2)
	source := h.g.AddObject(mkCard(t, "Name:Unrelated Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	imprinted := h.g.AddObject(mkCard(t, "Name:Imprinted\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	tail := h.g.AddObject(mkCard(t, "Name:Tail\nTypes:Sorcery\nOracle:x\n"), 0).ID
	original := []state.ObjID{imprinted, tail}
	h.g.SetZone(state.ZLibrary, 0, original)
	h.Emit(events.Event{Kind: events.Imprint, Obj: source, IDs: []state.ObjID{imprinted}})
	if h.g.Obj(imprinted).Zone != state.ZLibrary || imprinted == tail ||
		!reflect.DeepEqual(h.g.Obj(source).Imprinted, []state.ObjID{imprinted}) ||
		!reflect.DeepEqual(h.g.Zone(state.ZLibrary, 0), original) {
		t.Fatalf("precondition: non-exiled imprint association and distinct untouched tail: source=%+v library=%v", h.g.Obj(source), h.g.Zone(state.ZLibrary, 0))
	}
	// Even an exactly matching return body and a named continuation are not
	// enough: no DigMultiple picked a Remembered card for this source.
	ctx := &Ctx{Source: source, Controller: 0, SVars: vars}
	if got, ok := knownDefinedTargets(h, ctx, "Imprinted"); !ok || len(got) != 0 {
		t.Fatalf("precondition: ordinary CR 607.2a gate = %v, known=%v; want empty", got, ok)
	}
	effChangeZone(h, ctx, returnSA)
	if !reflect.DeepEqual(h.g.Zone(state.ZLibrary, 0), original) {
		t.Fatalf("unpaired fetch changed library to %v, want %v", h.g.Zone(state.ZLibrary, 0), original)
	}
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone || ev.Kind == events.LibraryOrder || ev.Kind == events.Shuffle {
			t.Fatalf("unpaired Imprinted fetch bypassed exile gate: %+v", ev)
		}
	}
}

func TestUnrelatedImprintedLibraryFetchKeepsExileGate(t *testing.T) {
	_, _, vars := corpusRiderSA(t, "Moonlight Bargain", "")
	sa := cards.ResolveSVar(vars, "DBChangeZone")
	if sa == nil || sa.API != "ChangeZone" || sa.Params["Defined"] != "Imprinted" ||
		sa.Params["Origin"] != "Library" || sa.Params["Destination"] != "Graveyard" {
		t.Fatalf("precondition: Moonlight Bargain library fetch = %+v", sa)
	}
	h := newHost(t, 2)
	source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	card := h.g.AddObject(mkCard(t, "Name:Library Card\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{card})
	h.Emit(events.Event{Kind: events.Imprint, Obj: source, IDs: []state.ObjID{card}})
	ctx := &Ctx{Source: source, Controller: 0, SVars: vars}
	if h.g.Obj(card).Zone != state.ZLibrary || !reflect.DeepEqual(h.g.Obj(source).Imprinted, []state.ObjID{card}) {
		t.Fatalf("precondition: association in library = %+v / %+v", h.g.Obj(card), h.g.Obj(source))
	}
	if targets, known := knownDefinedTargets(h, ctx, "Imprinted"); !known || len(targets) != 0 {
		t.Fatalf("precondition: ordinary exile gate targets=%v known=%v", targets, known)
	}
	// Call the library mover directly: Moonlight's surrounding RepeatEach binds
	// its own iteration subject, whereas this test deliberately has none.
	if !moveDefinedLibraryObjects(h, ctx, sa, ChangeZoneOf(sa), state.ZGraveyard) {
		t.Fatal("precondition: library fetch did not reach its handler")
	}
	if h.g.Obj(card).Zone != state.ZLibrary {
		t.Fatalf("unrelated fetch moved non-exiled imprint to %s", h.g.Obj(card).Zone)
	}
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone || ev.Kind == events.Shuffle {
			t.Fatalf("unrelated library fetch bypassed exile gate: %+v", ev)
		}
	}
}
