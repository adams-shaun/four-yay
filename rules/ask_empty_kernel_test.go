package rules

// Restored from effects/ask_empty_test.go (W3 legacy removal): the
// optional direct-library-fetch continuations, now answered through the
// resolution kernel on a real engine.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const kr0ForestSrc = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"

// TestBucolicRanchBottomContinuationKernel: Bucolic Ranch's real optional
// DBChangeZone2 poses a yes/no before moving anything; accepting it puts the
// actual top card of the library on the bottom.
func TestBucolicRanchBottomContinuationKernel(t *testing.T) {
	t.Parallel()
	ranch := kr0Corpus(t, "Bucolic Ranch")
	bottom := kr0SVar(t, ranch, "DBChangeZone2")
	e := kr0Engine(t, 2)
	src := kr0Place(t, e, 0, ranch, state.ZBattlefield)
	ids := kr0Library(t, e, 0, kr0ForestSrc, 3)
	start := len(e.L.Events)
	d := kr0Run(t, e, bottom, func() *effects.Ctx { return &effects.Ctx{Source: src, Controller: 0} }, nil)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "defined_library_optional" {
		t.Fatalf("DBChangeZone2 posed %+v, want an optional direct-fetch decision", d)
	}
	if n := kr0Count(kr0Since(e, start), events.MoveZone); n != 0 {
		t.Fatalf("DBChangeZone2 moved %d card(s) before its answer", n)
	}
	kr0Answer(t, e, kr0Kind(t, d, "yes"))
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) != len(ids) || lib[len(lib)-1] != ids[0] {
		t.Fatalf("accepted DBChangeZone2 library = %v, want top %d on bottom", lib, ids[0])
	}
	var moved, ordered bool
	for _, ev := range kr0Since(e, start) {
		moved = moved || ev.Kind == events.MoveZone && ev.Obj == ids[0]
		ordered = ordered || ev.Kind == events.LibraryOrder
	}
	if !moved || !ordered {
		t.Fatalf("accepted DBChangeZone2 events = %v, want top-card move and library order", kr0Since(e, start))
	}
}

// TestDefinedLibraryOptionalDeclineLeavesTheFetchListAloneKernel: Kenessos's
// real DBBottom continuation offers yes/no; declining neither moves nor
// shuffles nor reorders the remembered library card.
func TestDefinedLibraryOptionalDeclineLeavesTheFetchListAloneKernel(t *testing.T) {
	t.Parallel()
	kenessos := kr0Corpus(t, "Kenessos, Priest of Thassa")
	bottom := kr0SVar(t, kenessos, "DBBottom")
	e := kr0Engine(t, 2)
	src := kr0Place(t, e, 0, kenessos, state.ZBattlefield)
	id := kr0Library(t, e, 0, "Name:Sea Monster\nTypes:Creature\nPT:1/1\nOracle:x\n", 1)[0]
	start := len(e.L.Events)
	d := kr0Run(t, e, bottom, func() *effects.Ctx {
		return &effects.Ctx{Source: src, Controller: 0, Remembered: []state.Target{{Obj: id}}}
	}, nil)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "defined_library_optional" {
		t.Fatalf("optional direct fetch asked %+v, want defined_library_optional KChoose", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("optional direct fetch options = %+v, want yes/no", d.Options)
	}
	if n := kr0Count(kr0Since(e, start), events.MoveZone); n != 0 {
		t.Fatalf("optional direct fetch moved before its answer")
	}
	kr0Answer(t, e, kr0Kind(t, d, "no"))
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("declined DBBottom left card %+v, want it in the library", o)
	}
	for _, ev := range kr0Since(e, start) {
		if ev.Kind == events.MoveZone || ev.Kind == events.Shuffle || ev.Kind == events.LibraryOrder {
			t.Fatalf("declined DBBottom emitted %v, want no move or reorder", ev)
		}
	}
}
