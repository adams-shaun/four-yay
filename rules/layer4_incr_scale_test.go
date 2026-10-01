package rules

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The whole-emit-path benchmark for the cardfuzz bigboard class is
// BenchmarkEmitTokenCreateOnLargeBoard in emit_scale_test.go (added on main by
// the trigger-walk ticket, seed 6181111140895991800). It is the emit-path
// witness the brief asks for, so this file does not declare a second copy: the
// two pins below cover the layer-4 refresh in isolation, which is the residual
// linear term this ticket removed.

// The layer-4 derived-type table's incremental rebuild (layer4types.go):
// scaling pins. The table is refreshed after
// every emitted non-inert event while a layer-4 type effect is live, so its
// per-event cost -- before the incremental rebuild, two whole-board passes
// (the staticsMayChangeTypes face probe and the walk's candidate scan) -- is
// paid once per event for as long as the effect lives. The cardfuzz bigboard
// class (seed 6181111140895991800: Krenko doubling thousands of Goblin tokens
// beside a crewed Clown Car) made every emitted event O(battlefield) there.
//
// Both pins here use the same fixture shape as layer4types_selfonly_test.go's
// selfOnlyBoard (n vanilla goblins plus a permanent carrying a registered
// `Affected$ Card.Self` layer-4 effect): every live layer-4 effect is
// self-only, so the incremental rebuild must re-walk only the candidate set,
// never the board.

// incrBoard is the emit-path fixture: n vanilla goblin tokens on seat 0's
// battlefield plus one permanent (car) carrying a registered `Affected$
// Card.Self` layer-4 type effect, and the id of the last token (tap) to
// emit events against. AddContinuous arms layer4InPool, so every emitted
// event afterwards refreshes the derived-type table.
func incrBoard(t testing.TB, n int) (e *Engine, car, tap state.ObjID) {
	t.Helper()
	// The pins emit tens of thousands of events, so the livelock watcher's
	// default thresholds (a repeating cycle, a no-progress runaway run)
	// would abort the measurement loop; the same opt-out an embedder uses
	// keeps the loop alive. Nothing else about the engine differs.
	e = New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:     [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		LoopGuard: &LoopGuard{Disabled: true}}))
	for i := 0; i < n; i++ {
		tap = onBoard(t, e, 0, fmt.Sprintf("Name:Goblin %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
	}
	car = onBoard(t, e, 0, "Name:Car\nTypes:Artifact Vehicle\nPT:4/4\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: car, Controller: 0, Affects: "Card.Self", Layer: LType,
		UntilEOT: true, AddTypes: []string{"Artifact", "Creature"}})
	return e, car, tap
}

// incrBoardOK asserts the fixture's preconditions: the table maintenance is
// armed, the effect source is a live battlefield permanent, the live layer-4
// shape is self-only with exactly that one source, and the table actually
// carries the source's granted type (a vacuous fixture would make every
// assertion below vacuous too).
func incrBoardOK(t testing.TB, e *Engine, car state.ObjID) {
	t.Helper()
	e.refreshDerivedTypes()
	if !e.layer4InPool {
		t.Fatal("fixture precondition: layer4InPool is not armed by the registered layer-4 effect")
	}
	if o := e.G.Obj(car); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: effect source %d is not a battlefield permanent", car)
	}
	srcs, selfOnly := e.layer4SelfOnlySources(nil)
	if !selfOnly || len(srcs) != 1 || srcs[0] != car {
		t.Fatalf("fixture precondition: layer4SelfOnlySources = %v, %v; want [%d], true", srcs, selfOnly, car)
	}
	table := e.EffectiveTypes()
	if len(table) != 1 || table[0].ID != car {
		t.Fatalf("fixture precondition: derived-type table = %v; want exactly the source's entry", table)
	}
	if !containsFold(table[0].Types, "Creature") {
		t.Fatalf("fixture precondition: the source's entry %v is missing the self-granted Creature", table[0].Types)
	}
}

// emitRefreshPair emits a +1/-1 counter pair on one battlefield token: each
// is an emitted event that writes an object field inside events.Apply and is
// not derived-quiet (a counter is object-local), so each one moves the table
// key and drives a real refresh of the derived-type table -- O(1) board
// change per event, so the per-event refresh cost is measured (or counted)
// alone. (A Tap/Untap pair no longer does: those are derived-quiet, and the
// refresh reuses the table outright -- TestLayer4TableReusedAcrossQuietEvents.)
func emitRefreshPair(e *Engine, tap state.ObjID) {
	emitRefresh(e, tap, 1)
	emitRefresh(e, tap, -1)
}

// emitRefresh emits one counter change on tap: an incremental refresh.
func emitRefresh(e *Engine, tap state.ObjID, n int32) {
	e.emit(events.Event{Kind: events.CounterChange, Obj: tap, Counter: "P1P1", Amount: n})
}

// engineIntField reads an unexported engine counter by name. The counters the
// pins read (typesIncrBuilds, typesVisited) exist only in the engine that
// implements the incremental rebuild; reading them through reflection keeps
// this file compilable against an engine without the fix, so the pins FAIL
// there with a real message instead of a compile error.
func engineIntField(t testing.TB, e *Engine, name string) int {
	t.Helper()
	v := reflect.ValueOf(e).Elem().FieldByName(name)
	if !v.IsValid() {
		t.Fatalf("rules.Engine has no %s field: the incremental layer-4 rebuild (layer4types.go) is not implemented", name)
	}
	return int(v.Int())
}

// TestLayer4TableRefreshWorkIsNotPerObject is the deterministic scaling pin,
// in the TestPotentialManaStaticScanIsNotPerObject shape. It asserts on the
// objects the layer-4 build EXAMINED (typesVisited), not on wall clock: the
// table's own work must be the maintained candidate set, not the board. The
// whole-board build sets typesVisited to len(e.G.Objs); an incremental build
// sets it to the candidate count, which is bounded by the fixture (one source
// plus at most the tapped token). The pin fails on the pre-incremental code,
// where every refresh rescans the board and typesVisited would track len(Objs).
func TestLayer4TableRefreshWorkIsNotPerObject(t *testing.T) {
	t.Parallel()
	visited := func(n int) int {
		e, car, tap := incrBoard(t, n)
		incrBoardOK(t, e, car)
		// Precondition: the whole-board build examined the whole board, so a
		// counter that never moves cannot masquerade as an incremental pass.
		if full := engineIntField(t, e, "typesVisited"); full != len(e.G.Objs) {
			t.Fatalf("whole-board build at n=%d examined %d objects, want len(Objs)=%d", n, full, len(e.G.Objs))
		}
		// One non-inert emit refreshes the table through the ordinary path.
		emitRefresh(e, tap, 1)
		return engineIntField(t, e, "typesVisited")
	}
	small := visited(500)
	large := visited(4000)
	if small == 0 || large == 0 {
		t.Fatalf("pin precondition: no objects examined (small=%d large=%d)", small, large)
	}
	if large != small {
		t.Fatalf("the layer-4 build examines work proportional to the board: %d objects at 500 goblins, %d at 4000 -- an incremental build must examine only its candidate set", small, large)
	}
}

// TestLayer4TableRefreshTakesTheIncrementalPath pins, deterministically and
// without timing, that the per-event refresh after a whole-board build takes
// the incremental path: K emit PAIRS (2K non-inert events) advance the
// incremental-build counter exactly 2K times (a whole-board fallback is a
// conservative direction the engine is allowed to take, but the ordinary
// self-only emit path must not need one), and the table the incremental
// builds produce is still exactly the full walk's (the test binary's
// layer4PrecheckVerify already re-derives it on every incremental build; the
// explicit compare here is the same check with the verify flag read out in
// the open).
func TestLayer4TableRefreshTakesTheIncrementalPath(t *testing.T) {
	t.Parallel()
	e, car, tap := incrBoard(t, 4000)
	incrBoardOK(t, e, car)
	before := engineIntField(t, e, "typesIncrBuilds")
	const K = 32
	for i := 0; i < K; i++ {
		emitRefreshPair(e, tap)
	}
	got := engineIntField(t, e, "typesIncrBuilds") - before
	if got != 2*K {
		t.Fatalf("after %d emit pairs (%d non-inert events) the table took %d incremental rebuilds (and %d whole-board fallbacks) -- the ordinary emit path must be incremental every time", K, 2*K, got, 2*K-got)
	}
	table := e.EffectiveTypes()
	full := e.buildDerivedTypesFull(nil)
	if len(table) != len(full) || (len(table) > 0 && !reflect.DeepEqual(table, full)) {
		t.Fatalf("incremental table %v != full walk %v", table, full)
	}
	if len(table) == 0 || table[0].ID != car {
		t.Fatalf("fixture precondition failed after the emits: table %v lost the source's entry", table)
	}
}

// TestLayer4TableReusedAcrossQuietEvents pins the derived-quiet reuse
// (typesQuietReuse): a Tap/Untap pair writes nothing the table, its candidate
// slice or the statics probe read, so the self-only table is restamped
// without a rebuild -- and it is still exactly the full walk's.
func TestLayer4TableReusedAcrossQuietEvents(t *testing.T) {
	t.Parallel()
	e, car, tap := incrBoard(t, 500)
	incrBoardOK(t, e, car)
	builds := e.typesIncrBuilds
	for i := 0; i < 8; i++ {
		e.emit(events.Event{Kind: events.Tap, Obj: tap})
		e.emit(events.Event{Kind: events.Untap, Obj: tap})
	}
	if e.typesIncrBuilds != builds || e.typesEpoch != len(e.L.Events) {
		t.Fatalf("quiet events rebuilt the table (builds %d -> %d, epoch %d of %d)", builds, e.typesIncrBuilds, e.typesEpoch, len(e.L.Events))
	}
	if got, want := e.EffectiveTypes(), e.buildDerivedTypesFull(nil); !reflect.DeepEqual(got, want) || len(got) != 1 || got[0].ID != car {
		t.Fatalf("reused table %v, want full table %v", got, want)
	}
	// A non-quiet event still rebuilds.
	emitRefresh(e, tap, 1)
	if e.typesIncrBuilds != builds+1 {
		t.Fatalf("a counter change did not rebuild (builds %d -> %d)", builds, e.typesIncrBuilds)
	}
}
