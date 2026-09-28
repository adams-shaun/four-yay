package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPoolTriggerMatchesImprintedCardLeavingExile is the regression for the
// DISTINCT-source ChangesZone route (the review round-2 finding): the trigger
// lives on a battlefield permanent (Knowledge Pool's shape — the measured 22
// `Origin$ Exile` carriers, Mimic Vat among them) and fires on a DIFFERENT
// object's departure from exile:
//
//	T:Mode$ ChangesZone | Origin$ Exile | Destination$ Any |
//	ValidCard$ Card.IsImprinted | Execute$ DBForget
//
// The trigger's source is the pool, not the moving card, so the
// `source == ev.Obj` LKI branch does not apply, and the move never left the
// battlefield, so `leftBattlefield` does not either — before the fix the
// ValidCard$ Card.IsImprinted check ran against the LIVE object, which the
// MoveZone fold has already placed in the destination zone, where the CR
// 607.2a exile-only association is dead. The departure must be judged against
// the event's LKI snapshot, still in exile.
//
// The test drives the real dispatch (emit → checkTriggers → the registered
// ChangesZone matcher), not just the matcher directly, and pins both the
// positive and the bystander negative.
func TestPoolTriggerMatchesImprintedCardLeavingExile(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)

	pool := onBoardCard(t, e, 0, card(t, `Name:Knowledge Vat
Types:Artifact
T:Mode$ ChangesZone | Origin$ Exile | Destination$ Any | ValidCard$ Card.IsImprinted | Execute$ TrigForget
SVar:TrigForget:DB$ Pump | Defined$ TriggeredCard | ForgetImprinted$ TriggeredCard
Oracle:probe
`))
	linked := e.G.AddObject(card(t, "Name:Imprinted Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	linked = e.G.Obj(linked.ID)
	linked.Zone = state.ZExile
	e.G.SetZone(state.ZExile, linked.Owner, []state.ObjID{linked.ID})
	bystander := e.G.AddObject(card(t, "Name:Unlinked Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	bystander = e.G.Obj(bystander.ID)
	bystander.Zone = state.ZExile
	e.G.SetZone(state.ZExile, bystander.Owner, []state.ObjID{bystander.ID})

	// The association lives on the SOURCE (pool.Imprinted names the linked
	// card) — the ordinary imprint shape, unlike the round-1 test's artificial
	// self-imprint.
	poolObj := e.G.Obj(pool)
	poolObj.Imprinted = []state.ObjID{linked.ID}

	// Preconditions: the two objects are distinct, the pool is in the zone the
	// default TriggerZones$ reads, the linked card is in the zone the
	// association's liveness rule reads, and the link really runs pool→card.
	if pool == linked.ID || linked.ID == bystander.ID {
		t.Fatalf("precondition: pool %d, linked %d and bystander %d must be distinct", pool, linked.ID, bystander.ID)
	}
	if poolObj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: pool zone=%v, want battlefield", poolObj.Zone)
	}
	if linked.Zone != state.ZExile {
		t.Fatalf("precondition: linked zone=%v, want exile", linked.Zone)
	}
	if len(poolObj.Imprinted) != 1 || poolObj.Imprinted[0] != linked.ID {
		t.Fatalf("precondition: pool Imprinted=%v, want exactly [%d]", poolObj.Imprinted, linked.ID)
	}
	lki := linked.CloneDeep()
	if lki.Zone != state.ZExile {
		t.Fatalf("precondition: LKI snapshot zone=%v, want exile (the pre-move snapshot)", lki.Zone)
	}

	before := len(e.pendingTriggers)
	ev := e.emit(events.Event{Kind: events.MoveZone, Obj: linked.ID, From: state.ZExile,
		To: state.ZHand, Player: linked.Owner})
	live := e.G.Obj(linked.ID)
	if live == nil || live.Zone != state.ZHand {
		t.Fatalf("precondition: the move must have folded: live zone %+v", live)
	}
	if live.Zone == lki.Zone {
		t.Fatalf("precondition: live zone %v and LKI zone %v must actually differ", live.Zone, lki.Zone)
	}
	if len(e.pendingTriggers)-before != 1 {
		t.Fatalf("queued triggers = %d, want exactly the pool's departure trigger", len(e.pendingTriggers)-before)
	}
	pt := e.pendingTriggers[len(e.pendingTriggers)-1]
	if pt.Source != pool {
		t.Fatalf("queued trigger Source=%d, want the pool %d", pt.Source, pool)
	}
	if pt.Ctx.LKI == nil || pt.Ctx.LKI.ID != linked.ID || pt.Ctx.LKI.Zone != state.ZExile {
		t.Fatalf("queued trigger must carry the linked card's exile LKI, got %+v", pt.Ctx.LKI)
	}
	_ = ev

	// A bystander leaving exile is not in the pool's association: the same
	// departure must queue nothing.
	beforeB := len(e.pendingTriggers)
	e.emit(events.Event{Kind: events.MoveZone, Obj: bystander.ID, From: state.ZExile,
		To: state.ZHand, Player: bystander.Owner})
	if got := len(e.pendingTriggers) - beforeB; got != 0 {
		t.Fatalf("bystander departure queued %d triggers, want 0", got)
	}

	// A card moving INTO exile is not an Origin$ Exile departure: the
	// imprinted card itself moving exile→battlefield by some later effect
	// would fire, but a fresh card exiled from hand must not (the pool's
	// Origin$ Exile gate narrows ev.From before any ValidCard read).
	third := e.G.AddObject(card(t, "Name:Freshly Exiled\nTypes:Sorcery\nOracle:x\n"), 0)
	third = e.G.Obj(third.ID)
	third.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{third.ID})
	beforeC := len(e.pendingTriggers)
	e.emit(events.Event{Kind: events.MoveZone, Obj: third.ID, From: state.ZHand,
		To: state.ZExile, Player: 0})
	if got := len(e.pendingTriggers) - beforeC; got != 0 {
		t.Fatalf("an exile-arrival queued %d triggers, want 0 (Origin$ Exile gates it)", got)
	}
	if e.G.Obj(third.ID).Zone != state.ZExile {
		t.Fatal("precondition: the third card must have folded into exile")
	}
}
