package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// trigBearSrc carries one printed trigger, so a TriggerPush naming it mints
// a real face-less ability object.
const trigBearSrc = "Name:Trig bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
	"T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ x\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"

// TestDerivedSeqSpansPushesAndObjectLocalEvents pins derived_transparent.go's
// push and object-local admissions: under local effects a TriggerPush (the
// arena grows by one face-less ability object) keeps derivedSeq and the next
// walk is served the earlier entry; a CounterChange on a non-source keeps it
// too but retires that object's entry, which re-derives with its counter;
// a CounterChange on an effect's source moves derivedSeq. derivedMemoVerify
// (on in this binary) recomputes every served entry.
func TestDerivedSeqSpansPushesAndObjectLocalEvents(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	lord := onBoard(t, e, 0, "Name:Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddPower$ 1 | Description$ x\nOracle:x\n")
	bear := onBoard(t, e, 0, trigBearSrc)
	walk := func() int32 {
		e.beginDerivedMemo()
		defer e.endDerivedMemo()
		return e.Derived(bear).Power
	}
	if p := walk(); p != 3 {
		t.Fatalf("bear power %d, want 3", p)
	}
	seq := e.derivedSeq
	objs := len(e.G.Objs)
	e.emit(events.Event{Kind: events.TriggerPush, Player: 0, Obj: bear, Amount: 0})
	if len(e.G.Objs) != objs+1 {
		t.Fatal("precondition: the TriggerPush minted no ability object")
	}
	if p := walk(); p != 3 {
		t.Fatalf("bear power after the push %d, want 3", p)
	}
	if e.derivedSeq != seq {
		t.Fatalf("a face-less TriggerPush under local effects moved derivedSeq %d -> %d", seq, e.derivedSeq)
	}
	if m := e.derivedMemo.at(bear); m == nil || m.seq != seq {
		t.Fatal("the bear's entry was not served across the push")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	if p := walk(); p != 4 {
		t.Fatalf("bear power after its +1/+1 counter %d, want 4 (stale entry served)", p)
	}
	if e.derivedSeq != seq {
		t.Fatalf("a counter on a non-source moved derivedSeq %d -> %d", seq, e.derivedSeq)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: lord, Counter: "P1P1", Amount: 1})
	_ = walk()
	if e.derivedSeq == seq {
		t.Fatal("a counter on an effect's source kept derivedSeq")
	}
}

// TestSBAQuietSpansAPush pins sbaquiet.go's push admission: a TriggerPush
// after a quiet pass loop keeps the skip (the arena grew by exactly the one
// face-less ability object it minted).
func TestSBAQuietSpansAPush(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	bear := onBoard(t, e, 0, trigBearSrc)
	e.emit(events.Event{Kind: events.Note, Text: "settle"})
	e.checkStateBased()
	if !e.sbaQuietNow() {
		t.Fatal("precondition: a pass loop that applied nothing should arm the quiet key")
	}
	e.emit(events.Event{Kind: events.TriggerPush, Player: 0, Obj: bear, Amount: 0})
	if !e.sbaQuietNow() {
		t.Fatal("a face-less TriggerPush should keep the quiet key")
	}
	e.emit(events.Event{Kind: events.Scry, Player: 0})
	if !e.sbaQuietNow() {
		t.Fatal("a pure marker should keep the quiet key")
	}
}

// TestDerivedSeqSpansBattlefieldMoves pins derived_transparent.go's any-move
// admission: a static-free creature entering and leaving the battlefield
// under local effects keeps derivedSeq (moving derivedBFSeq), retiring only
// the mover (derivedMemoVerify, on in this binary, recomputes every entry
// the walks are served); the lord's own departure moves derivedSeq.
func TestDerivedSeqSpansBattlefieldMoves(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	lord := onBoard(t, e, 0, "Name:Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddPower$ 1 | Description$ x\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	co := e.G.AddObject(card(t, "Name:Cub\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"), 0)
	co.Zone = state.ZHand
	cub := co.ID
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), cub))
	e.staticEpoch, e.activeEpoch = -1, -1
	e.emit(events.Event{Kind: events.Note, Text: "settle"})
	walk := func(id state.ObjID) int32 {
		e.beginDerivedMemo()
		defer e.endDerivedMemo()
		_ = e.Derived(bear)
		return e.Derived(id).Power
	}
	if p := walk(cub); p != 2 {
		t.Fatalf("cub power in hand %d, want 2", p)
	}
	seq, bf := e.derivedSeq, e.derivedBFSeq
	e.emit(events.Event{Kind: events.MoveZone, Obj: cub, From: state.ZHand, To: state.ZBattlefield})
	if p := walk(cub); p != 2 {
		t.Fatalf("cub power on the battlefield %d, want 2", p)
	}
	if m := e.derivedMemo.at(cub); m == nil || m.ep != len(e.L.Events) {
		t.Fatal("the cub was not re-derived after its entry")
	}
	if e.derivedSeq != seq || e.derivedBFSeq == bf {
		t.Fatalf("a static-free battlefield entry: derivedSeq %d -> %d (want kept), derivedBFSeq %d -> %d (want moved)", seq, e.derivedSeq, bf, e.derivedBFSeq)
	}
	if m := e.derivedMemo.at(bear); m == nil || m.seq != seq {
		t.Fatal("the bear's entry was not kept across the cub's entry")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cub, From: state.ZBattlefield, To: state.ZGraveyard})
	if p := walk(cub); p != 2 {
		t.Fatalf("cub power in the graveyard %d, want 2", p)
	}
	if e.derivedSeq != seq {
		t.Fatalf("a static-free battlefield departure moved derivedSeq %d -> %d", seq, e.derivedSeq)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: lord, From: state.ZBattlefield, To: state.ZGraveyard})
	if p := walk(bear); p != 2 {
		t.Fatalf("bear power after the lord left %d, want 2", p)
	}
	if e.derivedSeq == seq {
		t.Fatal("the lord's departure kept derivedSeq")
	}
}

// TestDerivedAnyMoveRefusesAMutatedPile pins derivedAnyMoveOK: a mutated
// pile leaving the battlefield moves its under-card with no event of its
// own, so the zone ledger outgrows the logged moves and the rebuild is not
// transparent.
func TestDerivedAnyMoveRefusesAMutatedPile(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddPower$ 1 | Description$ x\nOracle:x\n")
	pile := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	mo := e.G.AddObject(card(t, "Name:Mutant\nManaCost:1 G\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"), 0)
	mo.Zone = state.ZHand
	mutant := mo.ID
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), mutant))
	e.staticEpoch, e.activeEpoch = -1, -1
	e.emit(events.Event{Kind: events.Mutate, Obj: pile, IDs: []state.ObjID{mutant}, Text: "under", Amount: 1})
	if len(e.G.Obj(pile).MergedCards) != 1 {
		t.Fatal("precondition: the mutate did not merge")
	}
	e.beginDerivedMemo()
	_ = e.Derived(pile)
	e.endDerivedMemo()
	seq := e.derivedSeq
	e.emit(events.Event{Kind: events.MoveZone, Obj: pile, From: state.ZBattlefield, To: state.ZGraveyard})
	e.active()
	if e.derivedSeq == seq {
		t.Fatal("a mutated pile's departure (its under-card moved silently) kept derivedSeq")
	}
}

// TestSBAQuietSpansAPlainEntry pins sbaQuietEntry: a vanilla creature
// entering keeps the quiet skip (sbaQuietVerify, on in this binary, runs the
// pass loop anyway and panics if it would have applied anything); a 0/0
// creature or a legendary permanent entering drops it.
func TestSBAQuietSpansAPlainEntry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		quiet     bool
	}{
		{"vanilla", "Name:Cub\nManaCost:G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n", true},
		{"zero toughness", "Name:Husk\nManaCost:G\nTypes:Creature Bear\nPT:0/0\nOracle:x\n", false},
		{"legendary", "Name:Lady\nManaCost:G\nTypes:Legendary Creature Bear\nPT:1/1\nOracle:x\n", false},
	} {
		e := newSeats(t, 2)
		co := e.G.AddObject(card(t, tc.src), 0)
		co.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), co.ID))
		e.staticEpoch, e.activeEpoch = -1, -1
		e.emit(events.Event{Kind: events.Note, Text: "settle"})
		e.checkStateBased()
		if !e.sbaQuietNow() {
			t.Fatalf("%s: precondition: the quiet key is not armed", tc.name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: co.ID, From: state.ZHand, To: state.ZBattlefield})
		if got := e.sbaQuietNow(); got != tc.quiet {
			t.Fatalf("%s: quiet after the entry = %v, want %v", tc.name, got, tc.quiet)
		}
		e.checkStateBased()
	}
}
