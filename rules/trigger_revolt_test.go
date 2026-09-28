package rules

// The trigger-level Revolt$ gate (the trig-phase-revolt brief): Revolt$ was
// read on the REPLACEMENT path (rules/replacement.go's Revolt$ case) but the
// trigger path ignored it, so a T: line carrying `Revolt$ True` fired whether
// or not a permanent you controlled left the battlefield this turn. The gate
// now lives in triggerConditionHoldsAs (the shared CR 603.4 condition walk,
// so every trigger mode is covered, not just Phase), beside the bare
// `Condition$ Revolt` spelling, both reading the same Engine.revoltThisTurn
// scan the replacement path reads. The Count$Revolt.<yes>.<no> branch head
// (effects/count.go) and effects' bare Condition$ Revolt gate delegate to the
// same predicate through the effects.Host RevoltHolds bridge, so all four
// spellings answer identically.
//
// The fixtures drive the REAL compiled corpus cards (never a re-written
// copy): Aid from the Cowl (the Phase end-step carrier the brief names),
// Decommission (the corpus's only bare Condition$ Revolt line) and Lifecraft
// Cavalry (the Count$Revolt.1.0 etbCounter gate). Aid's own Dig body carries
// the separately-ledgered primary LibraryPosition$ approximation -- firing,
// not resolving, is what the Aid gate asserts.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const revoltBearSrc = "Name:Revolt Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"
const revoltKeySrc = "Name:Revolt Key\nTypes:Artifact\nOracle:x\n"

// leaveForRevolt moves a battlefield permanent to its owner's graveyard with
// a logged MoveZone -- exactly the event revoltThisTurn's scan reads.
func leaveForRevolt(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
		t.Fatalf("revolt fixture %d in zone %s, want battlefield", id, z)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
}

// TestAidFromTheCowlRevoltGatesTheEndStepTrigger is the brief's named card:
// the end-step trigger queues only after a permanent seat 0 controlled left
// the battlefield this turn, and the window resets at the next TurnChange.
func TestAidFromTheCowlRevoltGatesTheEndStepTrigger(t *testing.T) {
	t.Parallel()
	e, _, aid := gateFixture(t, 931, "Aid from the Cowl", revoltBearSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: aid, From: state.ZHand, To: state.ZBattlefield})
	bear := gateMoveFromLibrary(t, e, "Revolt Bear", state.ZBattlefield)

	// End step with nothing left the battlefield this turn: the Revolt$
	// gate fails and the trigger never queues.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if n := queuedPhaseTriggers(e, aid); n != 0 {
		t.Fatalf("end step without revolt queued %d triggers, want 0", n)
	}
	e.pendingTriggers = nil

	// A permanent seat 0 controlled leaves the battlefield: the same end
	// step now queues exactly one trigger.
	leaveForRevolt(t, e, bear)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if n := queuedPhaseTriggers(e, aid); n != 1 {
		t.Fatalf("end step after a leave queued %d triggers, want 1", n)
	}
	e.pendingTriggers = nil

	// The next turn starts a fresh revolt window: nothing has left yet, so
	// the end step stays silent again.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if n := queuedPhaseTriggers(e, aid); n != 0 {
		t.Fatalf("next turn's end step queued %d triggers, want 0 (revolt window reset)", n)
	}
}

// TestAirdropAeronautsRevoltGatesTheETBTrigger pins the same gate on the
// ChangesZone mode (the corpus's other 12 Revolt$ carriers are ETBs): the
// enters trigger fires only when revolt already held as it entered.
func TestAirdropAeronautsRevoltGatesTheETBTrigger(t *testing.T) {
	t.Parallel()
	e, _, aero := gateFixture(t, 936, "Airdrop Aeronauts", revoltBearSrc)
	bear := gateMoveFromLibrary(t, e, "Revolt Bear", state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: aero, From: state.ZHand, To: state.ZBattlefield})
	if n := queuedTriggersFor(e, aero); n != 0 {
		t.Fatalf("ETB without revolt queued %d triggers, want 0", n)
	}
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: aero, From: state.ZBattlefield, To: state.ZHand})
	leaveForRevolt(t, e, bear)
	e.emit(events.Event{Kind: events.MoveZone, Obj: aero, From: state.ZHand, To: state.ZBattlefield})
	if n := queuedTriggersFor(e, aero); n != 1 {
		t.Fatalf("ETB after a leave queued %d triggers, want 1", n)
	}
}

// queuedTriggersFor counts the triggers queued (pending, not yet placed) for
// one source object -- the phase harness's counter without the Phase-only
// name, so an ETB gate can use it too.
func queuedTriggersFor(e *Engine, id state.ObjID) int {
	return queuedPhaseTriggers(e, id)
}

// revoltCastFixture builds a two-seat game at Main 1 of turn 1 (seat 0
// starting): seat 0's deck carries the REAL corpus Decommission plus the
// authored Revolt Bear, seat 1's the authored Revolt Key. Both protagonists
// are bridged to their hands (the gateFixture discipline, replay-safe), the
// bear to seat 0's battlefield and the key to seat 1's. The key's OWNER is
// seat 1 by construction, so destroying it can never turn the CASTER's
// revolt on through the post-move controller read: a control-changed
// permanent resets to its owner when it leaves the battlefield, so the
// no-revolt arm below needs the leave to be an opponent's permanent.
func revoltCastFixture(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	dec := choiceCorpusCard(t, "Decommission")
	deck0 := append([]*cards.Card{dec, card(t, revoltBearSrc)}, mountainDeck(t, 38)...)
	deck1 := append([]*cards.Card{card(t, revoltKeySrc)}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, deck1}})
	e := New(cfg)
	e.Advance()
	decID := findInZones(t, e, 0, "Decommission")
	if inZone(e, state.ZLibrary, 0, decID) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: decID, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	toMain1(t, e)
	keyID := findInZones(t, e, 1, "Revolt Key")
	if keyID == 0 {
		t.Fatal("Revolt Key not in seat 1's hand or library")
	}
	from := state.ZLibrary
	if inZone(e, state.ZHand, 1, keyID) {
		from = state.ZHand
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: keyID, From: from, To: state.ZBattlefield})
	bearID := findInZones(t, e, 0, "Revolt Bear")
	if bearID == 0 {
		t.Fatal("Revolt Bear not in seat 0's hand or library")
	}
	from = state.ZLibrary
	if inZone(e, state.ZHand, 0, bearID) {
		from = state.ZHand
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: from, To: state.ZBattlefield})
	return e, cfg, decID, keyID, bearID
}

// castDecommissionAtTheKey drives a real Decommission cast (2W) targeting the
// named battlefield permanent, resolving the stack.
func castDecommissionAtTheKey(t *testing.T, e *Engine, dec, key state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "WWW")
	submitChoices(t, e, castOptMode(t, castOptions(t, e), dec, "").Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask missing: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == key {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Revolt Key not offered as a target: %+v", d)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
}

// TestDecommissionBareRevoltCondition pins the corpus's only bare
// `Condition$ Revolt` line: the chained DB$ GainLife gains its 3 life only
// when a permanent the caster controlled left the battlefield this turn,
// while the destroy half always resolves. The no-revolt arm destroys an
// OPPONENT-controlled artifact (control handed over with a logged
// ControlChange) so the destroy's own zone change cannot turn revolt on for
// the caster -- destroying one of your own permanents legitimately would.
func TestDecommissionBareRevoltCondition(t *testing.T) {
	t.Parallel()
	t.Run("no revolt", func(t *testing.T) {
		e, cfg, dec, key, _ := revoltCastFixture(t, 932)
		castDecommissionAtTheKey(t, e, dec, key)
		if z := e.G.Obj(key).Zone; z != state.ZGraveyard {
			t.Fatalf("key zone %s, want graveyard (the destroy half still resolves)", z)
		}
		if got := e.G.Players[0].Life; got != 20 {
			t.Fatalf("life %d, want 20 (no seat-0-controlled leave this turn, no gain)", got)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("revolt", func(t *testing.T) {
		e, cfg, dec, key, bear := revoltCastFixture(t, 933)
		leaveForRevolt(t, e, bear)
		castDecommissionAtTheKey(t, e, dec, key)
		if z := e.G.Obj(key).Zone; z != state.ZGraveyard {
			t.Fatalf("key zone %s, want graveyard", z)
		}
		if got := e.G.Players[0].Life; got != 23 {
			t.Fatalf("life %d, want 23 (revolt held, +3)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// TestLifecraftCavalryCountRevoltGatesTheEtbCounters pins the
// Count$Revolt.<yes>.<no> branch head through the card's real etbCounter
// gate: two +1/+1 counters when a permanent left the battlefield this turn,
// none otherwise (the pre-fix read degraded the unimplemented head to a
// fail-open gate that always applied both counters).
func TestLifecraftCavalryCountRevoltGatesTheEtbCounters(t *testing.T) {
	t.Parallel()
	t.Run("no revolt", func(t *testing.T) {
		e, cfg, cav := gateFixture(t, 934, "Lifecraft Cavalry")
		e.emit(events.Event{Kind: events.MoveZone, Obj: cav, From: state.ZHand, To: state.ZBattlefield})
		if n := counterN(e.G.Obj(cav), "P1P1"); n != 0 {
			t.Fatalf("P1P1 counters %d, want 0 (no leave this turn)", n)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("revolt", func(t *testing.T) {
		e, cfg, cav := gateFixture(t, 935, "Lifecraft Cavalry", revoltBearSrc)
		bear := gateMoveFromLibrary(t, e, "Revolt Bear", state.ZBattlefield)
		leaveForRevolt(t, e, bear)
		e.emit(events.Event{Kind: events.MoveZone, Obj: cav, From: state.ZHand, To: state.ZBattlefield})
		if n := counterN(e.G.Obj(cav), "P1P1"); n != 2 {
			t.Fatalf("P1P1 counters %d, want 2 (revolt holds)", n)
		}
		replayCheck(t, e, cfg)
	})
}
