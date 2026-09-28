package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestImprintedChangesZoneLKIBranchMatchesLeavingExile drives the actual
// zone-change matcher's LKI branch (rules/trigmatch_zone.go,
// zoneChangeMatchesWithCapture) for a `ValidCard$ Card.IsImprinted` spec.
//
// The branch runs when the trigger's source is the moved object
// (`source == ev.Obj`, here a registration on the imprinted card itself) or the
// move left the battlefield. In both cases the matcher hands the predicate the
// event's LKI snapshot -- the object as it was a moment before the move, still
// in exile. The predicate must judge the imprint association against that
// candidate; before the fix it re-read the live object, now in the destination
// zone, and never matched.
func TestImprintedChangesZoneLKIBranchMatchesLeavingExile(t *testing.T) {
	t.Parallel()
	e := goadGrantEngine(t)

	// The imprinted card is the trigger's own source (a delayed/self
	// registration) and lives in its own imprint association -- exactly the
	// shape the branch's `source == ev.Obj` condition admits.
	linked := e.G.AddObject(card(t, "Name:Imprinted Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	linked = e.G.Obj(linked.ID)
	linked.Zone = state.ZExile
	e.G.SetZone(state.ZExile, linked.Owner, []state.ObjID{linked.ID})
	linked.Imprinted = []state.ObjID{linked.ID}

	bystander := e.G.AddObject(card(t, "Name:Bystander Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	bystander = e.G.Obj(bystander.ID)
	bystander.Zone = state.ZExile
	e.G.SetZone(state.ZExile, bystander.Owner, []state.ObjID{bystander.ID})

	tr := cards.Trigger{Mode: "ChangesZone", Params: map[string]string{
		"Origin":      "Exile",
		"Destination": "Any",
		"ValidCard":   "Card.IsImprinted",
	}}

	// Preconditions: the source carries a real self-imprint link and is in the
	// zone the actual rule reads; the live and LKI zones will actually differ.
	if len(linked.Imprinted) != 1 || linked.Imprinted[0] != linked.ID {
		t.Fatalf("precondition: linked Imprinted=%v, want exactly [%d]", linked.Imprinted, linked.ID)
	}
	if linked.Zone != state.ZExile {
		t.Fatalf("precondition: linked zone=%v, want exile", linked.Zone)
	}
	lki := linked.CloneDeep()
	if lki.Zone != state.ZExile {
		t.Fatalf("precondition: LKI zone=%v, want exile (the pre-move snapshot)", lki.Zone)
	}

	ev := e.emit(events.Event{Kind: events.MoveZone, Obj: linked.ID, From: state.ZExile,
		To: state.ZHand, Player: linked.Owner})
	live := e.G.Obj(linked.ID)
	if live == nil || live.Zone != state.ZHand {
		t.Fatalf("precondition: the live object must be in the destination zone for the move to have folded: %+v", live)
	}
	if live.Zone == lki.Zone {
		t.Fatalf("precondition: live zone %v and LKI zone %v must actually differ", live.Zone, lki.Zone)
	}

	if !e.zoneChangeMatches(tr, linked.ID, ev, &lki) {
		t.Fatal("ChangesZone ValidCard$ Card.IsImprinted did not match an imprinted card leaving exile against its LKI")
	}

	// An unrelated card is not in the source's imprint association. Its own
	// trigger source is itself, so the association is its own (empty) list.
	bystanderLKI := bystander.CloneDeep()
	ev2 := e.emit(events.Event{Kind: events.MoveZone, Obj: bystander.ID, From: state.ZExile,
		To: state.ZHand, Player: bystander.Owner})
	if e.G.Obj(bystander.ID).Zone == bystanderLKI.Zone {
		t.Fatal("precondition: bystander live and LKI zones must differ")
	}
	if e.zoneChangeMatches(tr, bystander.ID, ev2, &bystanderLKI) {
		t.Error("ChangesZone ValidCard$ Card.IsImprinted matched a bystander that was never imprinted")
	}
}
