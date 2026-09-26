package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Effect-created counter-placement replacement class (an SVar
// ReplacementEffects$ line naming Event$ AddCounter with a ReplaceWith$ body,
// registered live by effects/misc.go's effEffect) pinned on its sole corpus
// carrier: Brad Boimler, Eager Ensign. The trigger's TrigEffect is resolved
// exactly as the tap trigger does, then the counter events ride the engine's
// own CounterChange path so the dispatcher's AddCounter matcher and
// applyAddCounterReplacements intercept exactly where the live game's would.

// bradBoard seeds the real corpus Brad Boimler and an authored target creature
// on seat 0's battlefield and returns (engine, cfg, brad, target). The
// battlefields are asserted here so the replacement matcher's own
// ActiveZones$/Permanent.YouCtrl gates are never satisfied by accident.
func bradBoard(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	brad := tokenReplCorpusCard(t, "Brad Boimler, Eager Ensign")
	target := card(t, "Name:Counter Target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, cfg := tokenReplGame(t, seed, brad, target)
	bradID := moveSeededCard(t, e, 0, brad, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	if b := e.G.Obj(bradID); b == nil || b.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Brad not on seat 0's battlefield (zone %v)", b.Zone)
	}
	if g := e.G.Obj(targetID); g == nil || g.Zone != state.ZBattlefield {
		t.Fatalf("precondition: target not on seat 0's battlefield (zone %v)", g.Zone)
	}
	return e, cfg, bradID, targetID
}

// resolveBradTrigger resolves the real corpus card's TrigEffect DB$ Effect
// head exactly as its tap trigger executes; the CounterReplace replacement is
// registered from the card's own SVars, never an authored line.
func resolveBradTrigger(t *testing.T, e *Engine, brad state.ObjID) {
	t.Helper()
	face := e.G.Obj(brad).Face()
	effects.Resolve(e, &effects.Ctx{Source: brad, Controller: 0, SVars: face.SVars},
		replacementBodySA(face.SVars["TrigEffect"]))
}

// TestBradBoimlerEffectCreatedAddCounterReplacement is the filing card: after
// the trigger's Effect registers, a 1-counter placement on a creature Brad's
// controller controls becomes 2 (that many plus one, the body's
// ReplaceCount$CounterNum/Plus.1).
func TestBradBoimlerEffectCreatedAddCounterReplacement(t *testing.T) {
	e, cfg, bradID, targetID := bradBoard(t, 83)
	resolveBradTrigger(t, e, bradID)
	if hasNote(e, "continuous replacement unimplemented (CounterReplace)") {
		t.Fatal("Brad's CounterReplace registration fell through to the unimplemented Note")
	}
	if before := e.G.Obj(targetID).Counter("P1P1"); before != 0 {
		t.Fatalf("precondition: target starts with %d P1P1 counters, want 0", before)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 1})
	if got := e.G.Obj(targetID).Counter("P1P1"); got != 2 {
		t.Fatalf("Brad's trigger: 1 counter placed -> %d, want 2 (one extra of the kind)", got)
	}
	replayCheck(t, e, cfg)
}

// TestBradBoimlerEffectCreatedAddCounterIgnoresRemoval is the negative
// direction: a CounterChange that REMOVES counters (a negative amount) is
// never an AddCounter event, so the Effect leaves it alone. The starting
// counters are placed BEFORE the trigger resolves, so the removal is the only
// event the Effect could see.
func TestBradBoimlerEffectCreatedAddCounterIgnoresRemoval(t *testing.T) {
	e, cfg, bradID, targetID := bradBoard(t, 89)
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 3})
	if before := e.G.Obj(targetID).Counter("P1P1"); before != 3 {
		t.Fatalf("precondition: target has %d P1P1 counters before the removal, want 3", before)
	}
	resolveBradTrigger(t, e, bradID)
	if hasNote(e, "continuous replacement unimplemented (CounterReplace)") {
		t.Fatal("Brad's CounterReplace registration fell through to the unimplemented Note")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: -2})
	if got := e.G.Obj(targetID).Counter("P1P1"); got != 1 {
		t.Fatalf("removal under Brad's Effect: %d counters remain, want exactly 3-2 = 1 (never replaced)", got)
	}
	replayCheck(t, e, cfg)
}
