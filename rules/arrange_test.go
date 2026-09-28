package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// arrangeSrc is Ponder's shape without the MayShuffle$ and draw: a sorcery
// that rearranges the top three cards of its controller's library and does
// nothing else, so the only observable effect under test is the reorder.
const arrangeSrc = "Name:ArrangeMe\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3\nOracle:x\n"

// arrangeFixture builds an engine whose seat 0 can cast arrangeSrc, with the
// mana to do so, and returns (engine, config, fixture-id).
func arrangeFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := newFixtureDeck(t, seed, arrangeSrc)
	addMana(t, e, 0, "U")
	return e, cfg, id
}

// countLibraryOrder reports how many LibraryOrder events the log carries.
func countLibraryOrder(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.LibraryOrder {
			n++
		}
	}
	return n
}

// arrangeDecision casts arrangeSrc and returns the pending KArrange decision.
func arrangeDecision(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected a pending KArrange decision, got %+v", d)
	}
	return d
}

// TestArrangePosesDecisionAndSuspends is the Ruling J0/J2 leaf: resolving a
// RearrangeTopOfLibrary | NumCards$ 3 against a host that CAN ask yields a
// pending KArrange decision with 3 options, Min == Max == 3, and the
// resolution suspended -- the spell stays on the stack, the asking effect has
// returned, and nothing before the ask re-runs until the answer arrives.
func TestArrangePosesDecisionAndSuspends(t *testing.T) {
	t.Parallel()
	e, _, id := arrangeFixture(t, 100)
	d := arrangeDecision(t, e, id)
	if d.Min != 3 || d.Max != 3 {
		t.Fatalf("Min/Max = %d/%d, want 3/3 (a full reorder is a permutation over the top N)", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %d, want 3", len(d.Options))
	}
	if d.Player != 0 {
		t.Fatalf("decision player = %d, want 0 (the library owner)", d.Player)
	}
	if !e.Suspended() {
		t.Fatal("resolution not suspended: the asking effect must return and leave the spell on the stack")
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("no stack object under suspension: the spell must still be resolving")
	}
}

// TestArrangeAnswerReordersLibrary is the reorder leaf: answering [2,0,1]
// puts the library in exactly that order on top, leaves the untouched
// remainder beneath the top 3, and emits exactly one events.LibraryOrder
// carrying the full new order. It also runs the log-only replay check, so a
// reorder that bypassed the event log would be caught.
func TestArrangeAnswerReordersLibrary(t *testing.T) {
	t.Parallel()
	e, cfg, id := arrangeFixture(t, 102)
	d := arrangeDecision(t, e, id)
	top := []state.ObjID{d.Options[0].Obj, d.Options[1].Obj, d.Options[2].Obj}
	libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)

	submitChoices(t, e, 2, 0, 1)

	libAfter := e.G.Zone(state.ZLibrary, 0)
	wantTop := []state.ObjID{top[2], top[0], top[1]}
	for i := 0; i < 3; i++ {
		if libAfter[i] != wantTop[i] {
			t.Fatalf("library top[%d] = %v, want %v (answer [2,0,1] must put option 2 then 0 then 1 on top)", i, libAfter[i], wantTop[i])
		}
	}
	for i := 3; i < len(libBefore); i++ {
		if libAfter[i] != libBefore[i] {
			t.Fatalf("library card %d (index %d) changed: %v -> %v; the remainder beneath the top N must be untouched", i, i, libBefore[i], libAfter[i])
		}
	}
	if n := countLibraryOrder(e); n != 1 {
		t.Fatalf("LibraryOrder events = %d, want exactly 1", n)
	}
	// The single event carries the complete new library.
	for _, ev := range e.L.Events {
		if ev.Kind != events.LibraryOrder {
			continue
		}
		if ev.Player != 0 || !ev.Secret {
			t.Fatalf("LibraryOrder event must name the library owner and be Secret: %+v", ev)
		}
		if len(ev.IDs) != len(libAfter) {
			t.Fatalf("LibraryOrder carried %d ids, want the full %d-card library", len(ev.IDs), len(libAfter))
		}
		for i := range libAfter {
			if ev.IDs[i] != libAfter[i] {
				t.Fatalf("LibraryOrder carries %v, live library is %v", ev.IDs, libAfter)
			}
		}
	}
	replayCheck(t, e, cfg)
}

// TestArrangeHonoursAnswerOrderNotSorted is the leaf that catches a handler
// which builds pile A from a map, a sorted set, or any structure that
// discards the answer's order. The same three cards are chosen in both
// games (same seed yields the same initial top 3), so both answers pick the
// SAME SET; a correct handler must yield DIFFERENT top orders, and the two
// must be permutations of one another.
func TestArrangeHonoursAnswerOrderNotSorted(t *testing.T) {
	t.Parallel()
	orderFor := func(seed uint64, choices ...int) []state.ObjID {
		e, _, id := arrangeFixture(t, seed)
		d := arrangeDecision(t, e, id)
		// Record the set actually offered, so the two runs can be shown to
		// operate on the same cards.
		_ = d
		submitChoices(t, e, choices...)
		lib := e.G.Zone(state.ZLibrary, 0)
		return append([]state.ObjID(nil), lib[:3]...)
	}
	a := orderFor(103, 2, 1, 0)
	b := orderFor(103, 0, 1, 2)
	if equalObjs(a, b) {
		t.Fatalf("[2,1,0] and [0,1,2] produced the SAME library order %v; the handler must honour the answer's order, not sort it", a)
	}
	if !sameObjSet(a, b) {
		t.Fatalf("%v and %v are not the same three cards; the two runs must start from the same seed", a, b)
	}
}

func equalObjs(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameObjSet(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestArrangeReplaysByteIdentically is the Ruling J2 leaf: a match containing
// an answered arrange replays byte-identically. replayCheck rebuilds the game
// from the log alone (no Engine), so a reorder that mutated state without a
// LibraryOrder event -- or an event that did not actually set the order --
// would produce a reconstructed library that diverges from the live one.
func TestArrangeReplaysByteIdentically(t *testing.T) {
	t.Parallel()
	e, cfg, id := arrangeFixture(t, 105)
	d := arrangeDecision(t, e, id)
	submitChoices(t, e, d.Options[2].Index, d.Options[0].Index, d.Options[1].Index)
	replayCheck(t, e, cfg)
}
