package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A CR 722.3c copy is exempt from cessation only while it remains the
// castable copy created directly in exile. Once cast and resolved, it has
// left the stack and CR 707.10a requires it to cease.
func TestPreparedExileCopyCeasesAfterCast(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 918, []string{"Cheerful Osteomancer // Raise Dead"}, []string{sosBearSrc}, nil)
	osteomancer := findAndMoveToHand(t, e, 0, "Cheerful Osteomancer")
	addMana(t, e, 0, "BBBB")
	submitChoices(t, e, castOptionFor(t, e, osteomancer).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(osteomancer); o == nil || o.Zone != state.ZBattlefield || !o.Prepared {
		t.Fatalf("precondition: Osteomancer must be prepared on the battlefield, got %+v", o)
	}
	bear := addToGraveyard(t, e, 0, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: Raise Dead needs a creature card in the graveyard, got %+v", o)
	}
	e.priorityRound()

	var copyID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.IsCopy && o.Face() != nil && o.Face().Name == "Raise Dead" {
			copyID = id
		}
	}
	if copyID == 0 {
		t.Fatal("precondition: no prepared Raise Dead copy in exile")
	}
	if cp := e.G.Obj(copyID); cp == nil || cp.Zone != state.ZExile || !cp.PreparedExileCopy() {
		t.Fatalf("precondition: direct-from-exile copy should be exempt before casting: %+v", cp)
	}

	addMana(t, e, 0, "B")
	castIndex := -1
	for _, opt := range castOptions(t, e) {
		if opt.Obj == copyID {
			castIndex = opt.Index
		}
	}
	if castIndex < 0 {
		t.Fatal("prepared Raise Dead copy has no cast option")
	}
	submitChoices(t, e, castIndex)
	if cp := e.G.Obj(copyID); cp == nil || cp.Zone != state.ZStack || cp.PreparedExileCopy() {
		t.Fatalf("precondition: cast prepared copy should be on stack and no longer exempt: %+v", cp)
	}
	sosDrain(t, e, bear, 30)
	if cp := e.G.Obj(copyID); cp == nil || cp.Zone != state.ZCeased {
		t.Fatalf("resolved prepared spell copy zone = %v, want ceased", zoneOf(cp))
	}
	replayCheck(t, e, cfg)
}

func TestPreparedExileCopyCeasesWhenSourceLeavesBattlefield(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 919, []string{"Cheerful Osteomancer // Raise Dead"}, []string{sosBearSrc}, nil)
	osteomancer := findAndMoveToHand(t, e, 0, "Cheerful Osteomancer")
	addMana(t, e, 0, "BBBB")
	submitChoices(t, e, castOptionFor(t, e, osteomancer).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(osteomancer); o == nil || o.Zone != state.ZBattlefield || !o.Prepared {
		t.Fatalf("precondition: Osteomancer must be prepared on the battlefield, got %+v", o)
	}
	bear := addToGraveyard(t, e, 0, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: Raise Dead needs a creature card in the graveyard, got %+v", o)
	}
	e.priorityRound()

	var copyID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.IsCopy && o.Face() != nil && o.Face().Name == "Raise Dead" {
			copyID = id
		}
	}
	if copyID == 0 {
		t.Fatal("precondition: no prepared Raise Dead copy in exile")
	}
	if cp := e.G.Obj(copyID); cp == nil || cp.PreparedSource != osteomancer || !cp.PreparedExileCopy() {
		t.Fatalf("precondition: exile copy must be linked to the prepared source: %+v", cp)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: osteomancer, From: state.ZBattlefield, To: state.ZGraveyard})
	if cp := e.G.Obj(copyID); cp == nil || cp.PreparedSource != 0 || cp.PreparedExileCopy() {
		t.Fatalf("prepared-source departure must retire copy provenance: %+v", cp)
	}
	e.checkStateBased()
	if cp := e.G.Obj(copyID); cp == nil || cp.Zone != state.ZCeased {
		t.Fatalf("orphaned prepared spell copy zone = %v, want ceased", zoneOf(cp))
	}
	replayCheck(t, e, cfg)
}
