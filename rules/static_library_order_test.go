package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A library reorder (LibraryOrder, Shuffle) re-stamps a quiet static memo
// when no effect can come from that library (staticLibraryOrderCold), and
// rescans once a library object's static could function there. The rules
// test binary's layerInertVerify rescans every re-stamp and panics on a
// difference.
func TestStaticMemoAcrossALibraryReorder(t *testing.T) {
	e := layerEngine(t)
	onBoardGrant(t, e, 0, staticBufferGrantSrc)
	e.active()
	reorder := func(kind events.Kind) {
		lib := slices.Clone(e.G.Zone(state.ZLibrary, 1))
		slices.Reverse(lib)
		e.emit(events.Event{Kind: kind, Player: 1, IDs: lib})
		e.active()
	}
	for _, kind := range []events.Kind{events.LibraryOrder, events.Shuffle} {
		seq := e.staticBuildSeq
		reorder(kind)
		if e.staticBuildSeq != seq || e.staticEpoch != len(e.L.Events) {
			t.Fatalf("%v of a cold library rescanned the static memo", kind)
		}
	}
	// A card whose static functions from the library makes the library's
	// order an input of the scan.
	o := e.G.AddObject(card(t, "Name:Library lord\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | EffectZone$ Library | Description$ x\nOracle:x\n"), 1)
	o.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 1, append(slices.Clone(e.G.Zone(state.ZLibrary, 1)), o.ID))
	e.emit(events.Event{Kind: events.Exert, Obj: o.ID}) // a non-quiet event: rescan with it
	e.active()
	seq := e.staticBuildSeq
	reorder(events.Shuffle)
	if e.staticBuildSeq == seq {
		t.Fatal("a reorder of a library holding a static-hot object re-stamped the static memo")
	}
}
