package rules

import (
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This synthetic grant touches three layers, deliberately emitted in a
// different order from active()'s sorted list. Its nested slices must stay
// freshly owned even when the outer ContinuousEffect storage is reused.
const staticBufferGrantSrc = "Name:Buffer grant\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | AddToughness$ 1 | AddKeyword$ Flying | AddTypes$ Bird\nOracle:x\n"

// Allocating a fresh outer slice after every event must fail this test.
// Keeping the old pointer live makes address reuse by the allocator impossible.
func TestStaticEffectsReusesBackingAcrossEvents(t *testing.T) {
	e := layerEngine(t)
	onBoardGrant(t, e, 0, staticBufferGrantSrc)
	bear := onBoardGrant(t, e, 0, creatureSrc("Buffer bear"))
	e.active()
	if len(e.staticContinuous) != 3 {
		t.Fatalf("static effects = %d, want 3", len(e.staticContinuous))
	}
	backing := &e.staticContinuous[0]
	keywords := e.staticContinuous[1].AddKeywords
	types := e.staticContinuous[2].AddTypes
	if len(keywords) != 1 || keywords[0] != "Flying" || len(types) != 1 || types[0] != "Bird" {
		t.Fatalf("grant payloads = %v / %v, want [Flying] / [Bird]", keywords, types)
	}
	if &e.activeBuf[0] == backing || e.activeBuf[0].Layer != LType || e.staticContinuous[0].Layer != LPT {
		t.Fatal("active sorting must use distinct storage and leave static scan order untouched")
	}
	epoch := e.staticEpoch
	// A static-quiet kind (layercache.go's staticQuietKinds, ClockTick among
	// them) only re-stamps a gate-free memo, so force a real rebuild with a
	// kind outside that set whose fold cannot change the scan.
	seq := e.staticBuildSeq
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...)
	e.emit(events.Event{Kind: events.Shuffle, Player: 1, IDs: lib})
	e.active()
	if e.staticEpoch <= epoch || e.staticEpoch != len(e.L.Events) || e.staticBuildSeq == seq {
		t.Fatal("a non-quiet event did not rebuild the static memo")
	}
	if &e.staticContinuous[0] != backing {
		t.Error("event-backed rebuild allocated new static effect storage")
	}
	if &e.staticContinuous[1].AddKeywords[0] == &keywords[0] || &e.staticContinuous[2].AddTypes[0] == &types[0] {
		t.Error("rebuild reused nested keyword/type storage; only the outer slice may be reused")
	}
	if keywords[0] != "Flying" || types[0] != "Bird" {
		t.Fatal("rebuild mutated previously borrowed keyword/type payloads")
	}
	d := e.Derived(bear)
	if d.Power != 3 || d.Toughness != 3 || !slices.Contains(d.Keywords, "Flying") || !slices.Contains(d.Types, "Bird") {
		t.Fatalf("rebuilt grant produced %+v, want 3/3 Flying Bird", d)
	}
}

// A reslice without clearing leaves departed sources and their payloads
// reachable. Dropping capacity on the empty board also loses warm reuse.
func TestStaticEffectsClearsDroppedSlotsAndRetainsCapacity(t *testing.T) {
	e := layerEngine(t)
	first := onBoardGrant(t, e, 0, staticBufferGrantSrc)
	second := onBoardGrant(t, e, 1, staticBufferGrantSrc)
	e.active()
	if len(e.staticContinuous) != 6 {
		t.Fatalf("initial static effects = %d, want 6", len(e.staticContinuous))
	}
	storage := e.staticContinuous[:cap(e.staticContinuous)]
	checkStorage := func(wantLen int) {
		t.Helper()
		if len(e.staticContinuous) != wantLen || cap(e.staticContinuous) != len(storage) {
			t.Fatalf("static len/cap = %d/%d, want %d/%d", len(e.staticContinuous), cap(e.staticContinuous), wantLen, len(storage))
		}
		if &e.staticContinuous[:cap(e.staticContinuous)][0] != &storage[0] {
			t.Error("shrinking/empty/regrowing static effects replaced the backing array")
		}
		for i := wantLen; i < len(storage); i++ {
			if !reflect.DeepEqual(storage[i], ContinuousEffect{}) {
				t.Errorf("obsolete static slot %d retains a departed effect", i)
			}
		}
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: first, From: state.ZBattlefield, To: state.ZGraveyard})
	e.active()
	checkStorage(3)
	for _, ce := range e.staticContinuous {
		if ce.Source != second || ce.Controller != 1 {
			t.Fatalf("surviving static effect has source/controller %d/%d, want %d/1", ce.Source, ce.Controller, second)
		}
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: second, From: state.ZBattlefield, To: state.ZGraveyard})
	e.active()
	checkStorage(0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: second, From: state.ZGraveyard, To: state.ZBattlefield})
	e.active()
	checkStorage(3)
	for _, ce := range e.staticContinuous {
		if ce.Source != second || ce.Controller != 1 || ce.Timestamp != e.G.Obj(second).Timestamp {
			t.Fatalf("regrown static effect kept stale identity: %+v", ce)
		}
	}
}

// Sharing writable static storage through Clone lets either branch's next
// rebuild silently rewrite the other branch's source/controller membership.
func TestStaticEffectsCloneOwnsBacking(t *testing.T) {
	e := layerEngine(t)
	source := onBoardGrant(t, e, 0, staticBufferGrantSrc)
	e.active()
	parentEffects := slices.Clone(e.staticContinuous)
	c := e.Clone()
	// The clone carries the parent's memo (same board, same log head) in its
	// OWN outer storage, keyed to its own registry version.
	if c.staticEpoch != e.staticEpoch || c.staticVersion != c.continuousVersion ||
		len(c.staticContinuous) != 3 || &c.staticContinuous[0] == &e.staticContinuous[0] {
		t.Fatal("clone did not carry the parent's static memo in its own storage")
	}
	c.active()
	if len(c.staticContinuous) != 3 || !reflect.DeepEqual(c.staticContinuous, parentEffects) {
		t.Fatal("clone did not keep the same three static effects")
	}
	if &c.staticContinuous[0] == &e.staticContinuous[0] || &c.activeBuf[0] == &c.staticContinuous[0] {
		t.Fatal("clone shares writable static storage with its parent or active list")
	}
	cloneBacking := &c.staticContinuous[0]
	c.emit(events.Event{Kind: events.ControlChange, Obj: source, Player: 1})
	c.active()
	if &c.staticContinuous[0] != cloneBacking {
		t.Error("clone's event-backed rebuild allocated new static storage")
	}
	for _, ce := range c.staticContinuous {
		if ce.Controller != 1 {
			t.Fatalf("clone retained old controller %d after ControlChange", ce.Controller)
		}
	}
	if !reflect.DeepEqual(e.staticContinuous, parentEffects) {
		t.Fatal("clone rebuild rewrote the parent's static effects or payloads")
	}
	c.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZGraveyard})
	c.active()
	e.emit(events.Event{Kind: events.ClockTick})
	e.active()
	if len(c.staticContinuous) != 0 || !reflect.DeepEqual(e.staticContinuous, parentEffects) {
		t.Fatal("independently rebuilt branches did not retain their own static membership")
	}
}

var staticEffectsBufferSink []ContinuousEffect

// Eight numeric-only effects currently grow a new outer slice four times.
// Compare against the same sorted-list rebuild without a static rescan so
// sort.SliceStable's allocation cost is not mistaken for static storage.
func TestStaticEffectsWarmRebuildAllocationBudget(t *testing.T) {
	// The static zone skip's verify mode re-runs the unskipped scan into
	// fresh storage, which is not the production path this budget measures.
	prevStatic := staticZoneSkipVerify
	staticZoneSkipVerify = false
	defer func() { staticZoneSkipVerify = prevStatic }()
	e := layerEngine(t)
	for range 8 {
		onBoardGrant(t, e, 0, "Name:Numeric buffer grant\nTypes:Enchantment\n"+
			"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | AddToughness$ 1\nOracle:x\n")
	}
	e.active()
	if len(e.staticContinuous) != 8 {
		t.Fatalf("static effects = %d, want 8", len(e.staticContinuous))
	}
	versionOnly := testing.AllocsPerRun(100, func() {
		e.EndOfTurnCleanup()
		staticEffectsBufferSink = e.active()
	})
	withStaticScan := testing.AllocsPerRun(100, func() {
		e.EndOfTurnCleanup()
		// Force a genuine static rebuild without charging this measurement
		// for event creation, hashing, trigger checks or log growth. The
		// event-driven test above exercises real invalidation.
		e.staticEpoch = -1
		staticEffectsBufferSink = e.active()
	})
	t.Logf("version-only rebuild = %.0f allocations; static rescan = %.0f", versionOnly, withStaticScan)
	if extra := withStaticScan - versionOnly; extra > 1 {
		t.Fatalf("warm static rescan added %.0f allocations, want at most 1 for unchanged seat traversal", extra)
	}
}

// TestStaticMemoRestampsAcrossQuietEvents pins staticQuietKinds' admission:
// on a gate-free, state-read-free board a quiet event (a tap, a step change)
// re-stamps the static memo instead of rescanning, while the same events on a
// board whose scan saw a continuous gate rescan as before. layerInertVerify
// (on in this binary) re-checks every re-stamp against a fresh scan.
func TestStaticMemoRestampsAcrossQuietEvents(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		restamp bool
	}{
		{"gate-free", staticBufferGrantSrc, true},
		{"gated", "Name:Gated grant\nTypes:Enchantment\n" +
			"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | IsPresent$ Creature.YouCtrl\nOracle:x\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			onBoardGrant(t, e, 0, tc.src)
			bear := onBoardGrant(t, e, 0, creatureSrc("Quiet bear"))
			e.active()
			seq := e.staticBuildSeq
			e.emit(events.Event{Kind: events.Tap, Obj: bear})
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepDraw})
			e.active()
			if e.staticEpoch != len(e.L.Events) {
				t.Fatal("static memo not brought up to the log head")
			}
			if restamped := e.staticBuildSeq == seq; restamped != tc.restamp {
				t.Fatalf("re-stamped = %v, want %v (gated %v, state-read %v)", restamped, tc.restamp, e.staticMemoGated, e.staticMemoStateRead)
			}
		})
	}
}

// TestStaticMemoMoveAdmission pins staticMoveCold: on a quiet board a
// static-free creature's zone move re-stamps the memo, while moving the
// static's own source rescans (and drops its effects).
func TestStaticMemoMoveAdmission(t *testing.T) {
	e := layerEngine(t)
	lord := onBoardGrant(t, e, 0, staticBufferGrantSrc)
	bear := onBoardGrant(t, e, 0, creatureSrc("Moving bear"))
	e.active()
	seq := e.staticBuildSeq
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.active()
	if e.staticBuildSeq != seq || len(e.staticContinuous) != 3 {
		t.Fatalf("static-free move: rebuilt %v, %d effects; want a re-stamp keeping 3", e.staticBuildSeq != seq, len(e.staticContinuous))
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: lord, From: state.ZBattlefield, To: state.ZGraveyard})
	e.active()
	if e.staticBuildSeq == seq || len(e.staticContinuous) != 0 {
		t.Fatalf("source move: rebuilt %v, %d effects; want a rescan leaving none", e.staticBuildSeq != seq, len(e.staticContinuous))
	}
}
