package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task movecounter1: api:MoveCounter end to end on the REAL corpus carriers.
// Before this task `DB$ MoveCounter` / `AB$ MoveCounter` was unregistered, so
// every such line was one "unimplemented API MoveCounter" Note and moved
// nothing -- and the ACTIVATED carriers (Weapon Rack, Diamond City,
// Explorer's Cache) were not even offered. These leaves drive the real
// compiled SAs: the origin's count falls, the destination's rises, and the
// CounterChange pair is on the event stream. Every fixture mutation goes
// through e.emit, so each game stays replay-verified.

// moveCounterEngine deals a corpus-only game whose seat 0 library holds the
// named fixtures, drives to seat 0's Main1, and returns the engine.
func moveCounterEngine(t *testing.T, reg *cards.Registry, fixtures ...string) (*Engine, Config) {
	t.Helper()
	e, cfg := proliferateEngine(t, reg, fixtures...)
	return e, cfg
}

// TestSpikeCannibalMovesAllPlusOneCountersOntoItself drives the real Spike
// Cannibal: `ValidSource$ Creature | Defined$ Self | CounterType$ P1P1 |
// CounterNum$ All` -- the sweep origin, the Define$ Self destination and the
// deterministic All amount (no ask).
func TestSpikeCannibalMovesAllPlusOneCountersOntoItself(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := moveCounterEngine(t, reg, "Spike Cannibal", "Grizzly Bears")

	bearA := putNamedOnBattlefield(t, e, "Grizzly Bears")
	bearB := putNamedOnBattlefield(t, e, "Grizzly Bears")
	e.priorityRound()
	putCountersOn(t, e, 0, "Grizzly Bears", "P1P1", 2) // lands on the first bear
	// Tag the two bears by id: putCountersOn targets the first match, so put
	// the second bear's counter by id to be unambiguous.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearB, Counter: "P1P1", Amount: 1})
	e.priorityRound()

	spike := putNamedOnBattlefield(t, e, "Spike Cannibal")
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bearA).Counter("P1P1"); got != 0 {
		t.Fatalf("bear A P1P1 = %d, want 0 (swept)", got)
	}
	if got := e.G.Obj(bearB).Counter("P1P1"); got != 0 {
		t.Fatalf("bear B P1P1 = %d, want 0 (swept)", got)
	}
	// Spike entered with 1 (K:etbCounter:P1P1:1). Every OTHER creature loses
	// its counters and Spike gains their total 2 + 1 = 3, so it ends at
	// 1 + 3 = 4: the whole board's counters are on Spike. (Spike's own
	// counter is a move from Spike to Spike -- a null move, no event pair.)
	if got := e.G.Obj(spike).Counter("P1P1"); got != 4 {
		t.Fatalf("Spike Cannibal P1P1 = %d, want 4 (the whole board's counters)", got)
	}

	// The event stream carries the real CounterChange events, not just the
	// post-state: -2 on bear A, -1 on bear B, +2 and +1 onto Spike -- and no
	// null self-move pair on Spike (no ±4 event on it at all).
	sawMinusTwo, sawPlusTwo, sawMinusOne, sawPlusOne, sawSelfPair := false, false, false, false, false
	for _, ev := range e.L.Events {
		if ev.Kind != events.CounterChange || ev.Counter != "P1P1" {
			continue
		}
		if ev.Obj == bearA && ev.Amount == -2 {
			sawMinusTwo = true
		}
		if ev.Obj == bearB && ev.Amount == -1 {
			sawMinusOne = true
		}
		if ev.Obj == spike {
			switch ev.Amount {
			case 2:
				sawPlusTwo = true
			case 1:
				sawPlusOne = true
			case 4, -4:
				sawSelfPair = true // the old null self-move's signature
			}
		}
	}
	if sawSelfPair {
		t.Fatalf("null self-move pair emitted on Spike (a ±4 CounterChange)")
	}
	if !sawMinusTwo || !sawMinusOne || !sawPlusTwo || !sawPlusOne {
		t.Fatalf("CounterChange events missing: -2 bearA=%v, -1 bearB=%v, +2 Spike=%v, +1 Spike=%v",
			sawMinusTwo, sawMinusOne, sawPlusTwo, sawPlusOne)
	}
}

// TestAetherbornMarauderMovesAnyNumberOntoItself drives the real Aetherborn
// Marauder: `ValidSource$ Permanent.YouCtrl+Other+counters_GE1_P1P1 |
// Defined$ Self | CounterType$ P1P1 | CounterNum$ Any` -- the COUNTERS_GE1
// filter sweep, the Define$ Self destination, and the real any-number ask.
func TestAetherbornMarauderMovesAnyNumberOntoItself(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := moveCounterEngine(t, reg, "Aetherborn Marauder", "Grizzly Bears")

	bear := putNamedOnBattlefield(t, e, "Grizzly Bears")
	e.priorityRound()
	putCountersOn(t, e, 0, "Grizzly Bears", "P1P1", 3)
	e.priorityRound()

	marauder := putNamedOnBattlefield(t, e, "Aetherborn Marauder")
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "move_counter" {
		t.Fatalf("decision = %+v, want the CounterNum$ Any amount ask (ResumeKind move_counter)", d)
	}
	if d.Player != 0 {
		t.Fatalf("ask player = %d, want the controller 0", d.Player)
	}
	// The offer list is 0..the origin's held count (here 3, so 0..3). Answer
	// the maximum to move everything the sweep admits.
	best := -1
	bestAmount := -1
	for i, o := range d.Options {
		if o.Amount > bestAmount {
			bestAmount = o.Amount
			best = i
		}
	}
	if best < 0 || bestAmount != 3 {
		t.Fatalf("amount options = %+v, want 0..3", d.Options)
	}
	submitChoices(t, e, d.Options[best].Index)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("bear P1P1 = %d, want 0 (all three moved)", got)
	}
	if got := e.G.Obj(marauder).Counter("P1P1"); got != 3 {
		t.Fatalf("Aetherborn Marauder P1P1 = %d, want 3", got)
	}
	sawMinus, sawPlus := false, false
	for _, ev := range e.L.Events {
		if ev.Kind != events.CounterChange || ev.Counter != "P1P1" {
			continue
		}
		if ev.Obj == bear && ev.Amount == -3 {
			sawMinus = true
		}
		if ev.Obj == marauder && ev.Amount == 3 {
			sawPlus = true
		}
	}
	if !sawMinus || !sawPlus {
		t.Fatalf("CounterChange pair missing: -3 on bear=%v, +3 on Marauder=%v", sawMinus, sawPlus)
	}
}

// TestWeaponRackAbilityIsOfferedAndMovesACounter pins the activated
// `Source$ Self | ValidTgts$ Creature` shape: registering the API is what
// makes the ability appear in the offered options at all (before this task the
// offer gate skipped it and Weapon Rack's second line was dead), and the
// chosen activation moves one counter from the Rack onto the target creature.
func TestWeaponRackAbilityIsOfferedAndMovesACounter(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := moveCounterEngine(t, reg, "Weapon Rack", "Grizzly Bears")

	rack := putNamedOnBattlefield(t, e, "Weapon Rack")
	bear := putNamedOnBattlefield(t, e, "Grizzly Bears")
	e.priorityRound()
	passUntilStackEmpty(t, e, 60)
	// Weapon Rack enters with three +1/+1 counters (K:etbCounter:P1P1:3).
	if got := e.G.Obj(rack).Counter("P1P1"); got != 3 {
		t.Fatalf("Weapon Rack P1P1 = %d, want 3 (etbCounter)", got)
	}

	// The {T} ability must be OFFERED. The priority decision's options carry
	// an "activate" kind anchored to the source; find the Rack's.
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending priority decision at Main1")
	}
	activate := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == rack {
			activate = o.Index
		}
	}
	if activate < 0 {
		t.Fatalf("Weapon Rack's MoveCounter ability is not offered: %+v", d.Options)
	}

	// Activate, then answer the target ask with the bear.
	submitChoices(t, e, activate)
	td := passUntilNonPriority(t, e, 60)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("decision = %+v, want the target ask for the Rack's ability", td)
	}
	target := -1
	for _, o := range td.Options {
		if o.Obj == bear {
			target = o.Index
		}
	}
	if target < 0 {
		t.Fatalf("bear not offered as a target: %+v", td.Options)
	}
	submitChoices(t, e, target)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(rack).Counter("P1P1"); got != 2 {
		t.Fatalf("Weapon Rack P1P1 = %d, want 2 (one moved off)", got)
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("bear P1P1 = %d, want 1 (one landed)", got)
	}
	sawMinus, sawPlus := false, false
	for _, ev := range e.L.Events {
		if ev.Kind != events.CounterChange || ev.Counter != "P1P1" {
			continue
		}
		if ev.Obj == rack && ev.Amount == -1 {
			sawMinus = true
		}
		if ev.Obj == bear && ev.Amount == 1 {
			sawPlus = true
		}
	}
	if !sawMinus || !sawPlus {
		t.Fatalf("CounterChange pair missing: -1 on Rack=%v, +1 on bear=%v", sawMinus, sawPlus)
	}
}

// TestNestingGroundsSubTargetReceivesTheMove drives the real Nesting
// Grounds: the {1},{T} root is a Pump whose target is the ORIGIN (Source$
// ParentTarget) and whose DBMove sub carries its own ValidTgts$ -- the shape
// the generic mvts1 pre-ask asks. The sub's OWN chosen target (the second
// bear) must be the destination, never the root's target (the PickedTargets
// convention): with the pre-fix code the -1 and +1 both landed on the root's
// target and cancelled (land 1->1, bear 0->0).
func TestNestingGroundsSubTargetReceivesTheMove(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := moveCounterEngine(t, reg, "Nesting Grounds", "Grizzly Bears")
	grounds := putNamedOnBattlefield(t, e, "Nesting Grounds")
	bearA := putNamedOnBattlefield(t, e, "Grizzly Bears")
	bearB := putNamedOnBattlefield(t, e, "Grizzly Bears")
	e.priorityRound()
	putCountersOn(t, e, 0, "Grizzly Bears", "P1P1", 1) // the first bear is the root's target
	e.priorityRound()

	// Activate the {1},{T} Pump: fund the generic, pick the ability option.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision at Main1")
	}
	activate := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == grounds {
			activate = o.Index
		}
	}
	if activate < 0 {
		t.Fatalf("Nesting Grounds' Pump ability is not offered: %+v", d.Options)
	}
	submitChoices(t, e, activate)

	// The root Pump's target ask: the first bear.
	td := passUntilNonPriority(t, e, 60)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("decision = %+v, want the root's target ask", td)
	}
	root := -1
	for _, o := range td.Options {
		if o.Obj == bearA {
			root = o.Index
		}
	}
	if root < 0 {
		t.Fatalf("bear A not offered as the root's target: %+v", td.Options)
	}
	submitChoices(t, e, root)

	// The sub DBMove's own "another target permanent": announced as the
	// ability is ACTIVATED (CR 602.2b / 601.2c), right after the root's
	// target and before the cost is paid -- the second bear.
	kd := castSubAsk(t, e)
	sub := -1
	for _, o := range kd.Options {
		if o.Obj == bearB {
			sub = o.Index
		}
	}
	if sub < 0 {
		t.Fatalf("bear B not offered as the sub's target: %+v", kd.Options)
	}
	submitChoices(t, e, sub)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bearA).Counter("P1P1"); got != 0 {
		t.Fatalf("root target (origin) P1P1 = %d, want 0", got)
	}
	if got := e.G.Obj(bearB).Counter("P1P1"); got != 1 {
		t.Fatalf("sub's own target P1P1 = %d, want 1 (the destination)", got)
	}
	replayCheck(t, e, cfg)
}

// TestNestingGroundsAnyKindWithOwnTargetDrains pins the movecounter1
// livelock fix end to end: a MoveCounter sub with its OWN ValidTgts$ AND a
// CounterType$ Any kind pick (Nesting Grounds' DBMove, an origin holding two
// kinds) must drain -- before the fix the answered kind was lost to the
// re-entry's fresh Ctx, the tgts pre-ask re-fired, and the two asks
// alternated forever (measured by the reviewer: move_counter_kind:38,
// tgts:39 over 80 drive steps, the stack never drained).
func TestNestingGroundsAnyKindWithOwnTargetDrains(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := moveCounterEngine(t, reg, "Nesting Grounds", "Grizzly Bears")
	grounds := putNamedOnBattlefield(t, e, "Nesting Grounds")
	bearA := putNamedOnBattlefield(t, e, "Grizzly Bears")
	bearB := putNamedOnBattlefield(t, e, "Grizzly Bears")
	e.priorityRound()
	putCountersOn(t, e, 0, "Grizzly Bears", "P1P1", 1)
	putCountersOn(t, e, 0, "Grizzly Bears", "CHARGE", 1) // a second kind on the same origin
	e.priorityRound()

	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	e.priorityRound()
	d := e.Pending()
	activate := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == grounds {
			activate = o.Index
		}
	}
	if activate < 0 {
		t.Fatalf("Pump ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, activate)

	td := passUntilNonPriority(t, e, 60)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("decision = %+v, want the root's target ask", td)
	}
	root := -1
	for _, o := range td.Options {
		if o.Obj == bearA {
			root = o.Index
		}
	}
	submitChoices(t, e, root)

	kd := castSubAsk(t, e)
	sub := -1
	for _, o := range kd.Options {
		if o.Obj == bearB {
			sub = o.Index
		}
	}
	submitChoices(t, e, sub)

	// The CounterType$ Any kind pick, posed by the sub's body AFTER the
	// target answer. Pick the SECOND offered kind (CHARGE) to prove the
	// answer is honoured on the re-entry that follows it.
	kk := passUntilNonPriority(t, e, 60)
	if kk == nil || kk.Kind != decision.KChoose || kk.ResumeKind != "move_counter_kind" {
		t.Fatalf("decision = %+v, want the move_counter_kind ask", kk)
	}
	charge := -1
	for _, o := range kk.Options {
		if o.Label == "CHARGE" {
			charge = o.Index
		}
	}
	if charge < 0 {
		t.Fatalf("CHARGE not offered as a kind: %+v", kk.Options)
	}
	submitChoices(t, e, charge)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(bearA).Counter("CHARGE"); got != 0 {
		t.Fatalf("origin CHARGE = %d, want 0 (the answered kind moved)", got)
	}
	if got := e.G.Obj(bearB).Counter("CHARGE"); got != 1 {
		t.Fatalf("destination CHARGE = %d, want 1", got)
	}
	if got := e.G.Obj(bearA).Counter("P1P1"); got != 1 {
		t.Fatalf("origin P1P1 = %d, want 1 (a different kind was chosen: untouched)", got)
	}
	replayCheck(t, e, cfg)
}

// TestForgottenAncientSweepDistributesAndConserves drives the real
// Forgotten Ancient: `Source$ Self | ValidDefined$ Creature.Other |
// CounterType$ P1P1 | CounterNum$ Any` at an upkeep. The any-number answer
// is DISTRIBUTED across the sweep's destinations (CR 122.5: a move never
// mints) -- 2 over two other creatures is 1 and 1, the total on the board
// is conserved. The pre-fix code gave EACH destination the whole moved set
// (measured: FA 4 -> 2 but bearA 2 AND bearB 2 -- total 6 from 4).
func TestForgottenAncientSweepDistributesAndConserves(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := moveCounterEngine(t, reg, "Forgotten Ancient", "Grizzly Bears")
	fa := putNamedOnBattlefield(t, e, "Forgotten Ancient")
	bearA := putNamedOnBattlefield(t, e, "Grizzly Bears")
	bearB := putNamedOnBattlefield(t, e, "Grizzly Bears")
	e.priorityRound()
	putCountersOn(t, e, 0, "Forgotten Ancient", "P1P1", 4)
	e.priorityRound()

	// FA's upkeep trigger is OptionalDecider$ You; seat 0's next upkeep is
	// two global turns away (two seats).
	driveToStep(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("decision = %+v, want Forgotten Ancient's optional upkeep trigger ask", d)
	}
	submitChoices(t, e, 0) // yes
	d = passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "move_counter" {
		t.Fatalf("decision = %+v, want the CounterNum$ Any amount ask", d)
	}
	two := -1
	for _, o := range d.Options {
		if o.Amount == 2 {
			two = o.Index
		}
	}
	if two < 0 {
		t.Fatalf("no amount-2 option: %+v", d.Options)
	}
	submitChoices(t, e, two)
	passUntilStackEmpty(t, e, 60)

	if got := e.G.Obj(fa).Counter("P1P1"); got != 2 {
		t.Fatalf("Forgotten Ancient P1P1 = %d, want 2 (4 minus the answered 2)", got)
	}
	if got := e.G.Obj(bearA).Counter("P1P1"); got != 1 {
		t.Fatalf("bearA P1P1 = %d, want 1 (its share of the distribution)", got)
	}
	if got := e.G.Obj(bearB).Counter("P1P1"); got != 1 {
		t.Fatalf("bearB P1P1 = %d, want 1 (its share of the distribution)", got)
	}
	total := e.G.Obj(fa).Counter("P1P1") + e.G.Obj(bearA).Counter("P1P1") + e.G.Obj(bearB).Counter("P1P1")
	if total != 4 {
		t.Fatalf("board total P1P1 = %d, want 4 (CR 122.5: conserved)", total)
	}
	replayCheck(t, e, cfg)
}
