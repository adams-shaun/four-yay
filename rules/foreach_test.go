package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestForEachObjectReentrySafety pins the re-entry property of forEachObject
// (Task A2). forEachObject snapshots each zone into a scratch buffer before
// walking it, because fn may move objects between zones (a trigger match
// putting something on the stack), and the depth-0 walk reuses the shared
// e.foreachBuf so it settles at the largest zone and stops allocating. If fn
// itself reaches forEachObject again -- directly, or through emit ->
// checkTriggers -- while the outer walk is mid-range, a single shared buffer
// would be a correctness bug: the inner walk would overwrite the outer's
// snapshot and the outer loop would read clobbered ids for the rest of that
// zone. The depth guard hands any re-entrant call its own private buffer, so
// the outer walk is never disturbed.
//
// This test forces exactly that re-entry: the first element of seat 0's four-
// object hand triggers a nested forEachObject, i.e. the outer walk is partway
// through a multi-element zone when the inner walk runs. A preceding warm-up
// walk grows e.foreachBuf to the largest zone's capacity, so both the outer
// hand snapshot and the inner walk's appends share ONE backing array -- the
// exact collision a plain shared buffer exhibits -- which makes the corruption
// deterministic rather than dependent on append's growth coincidences. To
// mutation-test it, remove the `if e.foreachDepth > 1 { buf = nil }` guard
// (and the writeback guard) in trigger_match.go so the re-entrant call reuses
// the shared buffer: the inner walk then overwrites indices 1..3 of the outer
// hand range, the outer loop reads those clobbered ids, and this test fails
// on the seen/want mismatch.
func TestForEachObjectReentrySafety(t *testing.T) {
	t.Parallel()
	g := state.NewGame([]string{"a", "b"})
	add := func(p state.PlayerID) state.ObjID { return g.AddObject(nil, p).ID }
	// Zone sizes: seat 1's battlefield is the largest (drives the warm-up
	// buffer capacity) and seat 0's four-object hand is where the re-entry
	// fires mid-range.
	p0lib := []state.ObjID{add(0), add(0)}
	p0hand := []state.ObjID{add(0), add(0), add(0), add(0)}
	p0bf := []state.ObjID{add(0), add(0)}
	p1lib := []state.ObjID{add(1)}
	p1hand := []state.ObjID{add(1)}
	p1bf := []state.ObjID{add(1), add(1), add(1), add(1), add(1), add(1), add(1), add(1)}
	g.SetZone(state.ZLibrary, 0, p0lib)
	g.SetZone(state.ZHand, 0, p0hand)
	g.SetZone(state.ZBattlefield, 0, p0bf)
	g.SetZone(state.ZLibrary, 1, p1lib)
	g.SetZone(state.ZHand, 1, p1hand)
	g.SetZone(state.ZBattlefield, 1, p1bf)

	e := &Engine{G: g}

	// Expected depth-0 scan order: seat 0 then seat 1, zones in Zone's own
	// order, positions within each zone. The stack is empty and visited only
	// on the first living seat, so it contributes nothing.
	want := append([]state.ObjID(nil), p0lib...)
	want = append(want, p0hand...)
	want = append(want, p0bf...)
	want = append(want, p1lib...)
	want = append(want, p1hand...)
	want = append(want, p1bf...)

	// Warm-up: grow e.foreachBuf to the largest zone's capacity with a
	// no-op depth-0 walk so the test walk below and its nested walk share a
	// single big backing array (deterministic corruption under a shared
	// buffer).
	e.forEachObject(func(state.ObjID) {})

	// Test walk: re-enter exactly once, on the first element of seat 0's
	// hand -- so the outer walk is mid-zone (four elements) when the inner
	// walk runs, the exact window a shared buffer would clobber.
	reenterID := p0hand[0]
	once := true
	var seen []state.ObjID
	var innerCount int
	e.forEachObject(func(id state.ObjID) {
		if once && id == reenterID {
			once = false
			// Inner walk: the re-entry the guard defends against.
			e.forEachObject(func(state.ObjID) { innerCount++ })
		}
		seen = append(seen, id)
	})

	if innerCount != len(want) {
		t.Fatalf("re-entrant inner walk visited %d objects, want %d", innerCount, len(want))
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("outer walk corrupted by re-entry:\n got %v\nwant %v", seen, want)
	}
	if e.foreachDepth != 0 {
		t.Fatalf("foreachDepth left at %d after walks (defer unwind lost)", e.foreachDepth)
	}
}

// TestForEachObjectCloneOwnsScratch pins that Engine.Clone does not share the
// forEachObject scratch buffer (foreachBuf/foreachDepth) between the original
// and the clone (Task A2). The brief's concern: a cloned engine that walks
// while the original walks would corrupt both. A clone is taken at an intent
// boundary -- never mid-walk, so foreachDepth is zero -- and sharing the
// buffer would still be a bug for a future concurrent pair. The structural
// invariant is what makes sharing impossible: Clone deliberately leaves both
// fields zero, so the clone owns a private buffer it grows itself and there
// is nothing shared to corrupt. This test pins that invariant directly: warm
// the original's buffer (proving there is a buffer that COULD be shared),
// clone, and assert the clone's own buffer starts fresh and independent while
// the original's keeps working.
func TestForEachObjectCloneOwnsScratch(t *testing.T) {
	t.Parallel()
	g := state.NewGame([]string{"a"})
	var ids []state.ObjID
	for i := 0; i < 8; i++ {
		ids = append(ids, g.AddObject(nil, 0).ID)
	}
	g.SetZone(state.ZBattlefield, 0, ids)
	want := append([]state.ObjID(nil), ids...)

	// Engine fields Clone() itself reads (G/L/rng); the rest stay zero and
	// are fine for a no-pending-decision, no-suspended-resolution engine --
	// exactly the shape a clone-boundary engine is in.
	orig := &Engine{G: g, L: events.NewLog(1), rng: newRNG(1)}
	// Warm the original's shared (per-Engine) buffer so there is an actual
	// buffer a careless Clone could have shared.
	orig.forEachObject(func(state.ObjID) {})
	// The walk ends with the last (empty) zone snapshotted, so len is 0; what
	// is carried forward is the CAPACITY, which append(buf[:0], ...) reuses.
	if cap(orig.foreachBuf) == 0 {
		t.Fatal("warm-up did not grow the original's scratch buffer")
	}

	clone := orig.Clone()
	// The two must never share a snapshot: the clone's buffer must start nil
	// (so it grows its own), and must not be the original's slice header.
	if clone.foreachBuf != nil {
		t.Fatalf("Clone shared the original's scratch buffer: clone.foreachBuf=%v", clone.foreachBuf)
	}
	if clone.foreachDepth != 0 {
		t.Fatalf("Clone carried foreachDepth=%d (must be zero at a clone boundary)", clone.foreachDepth)
	}
	if cap(orig.foreachBuf) == 0 {
		t.Fatal("the original's scratch buffer was clobbered or never kept")
	}

	// Both still walk the full expected scan independently.
	var cloneSeen, origSeen []state.ObjID
	clone.forEachObject(func(id state.ObjID) { cloneSeen = append(cloneSeen, id) })
	orig.forEachObject(func(id state.ObjID) { origSeen = append(origSeen, id) })
	if !reflect.DeepEqual(cloneSeen, want) {
		t.Fatalf("clone's walk wrong: got %v want %v", cloneSeen, want)
	}
	if !reflect.DeepEqual(origSeen, want) {
		t.Fatalf("original's walk wrong: got %v want %v", origSeen, want)
	}
}
