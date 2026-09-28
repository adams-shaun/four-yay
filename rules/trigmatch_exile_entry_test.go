package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestChangesZonePermanentYouCtrlStillMatchesEnteringFromExile ensures the
// exile-departure LKI path does not replace the live candidate for an ETB.
// Permanent requires the candidate to be on the battlefield, which is true of
// the post-move object and false of the exile LKI snapshot.
func TestChangesZonePermanentYouCtrlStillMatchesEnteringFromExile(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	source := onBoardCard(t, e, 0, card(t, `Name:Exile Entry Watcher
Types:Artifact
T:Mode$ ChangesZone | Origin$ Exile | Destination$ Battlefield | ValidCard$ Permanent.YouCtrl | Execute$ TrigProbe
SVar:TrigProbe:DB$ Pump | Defined$ TriggeredCard
Oracle:probe
`))
	entering := e.G.AddObject(card(t, "Name:Returning Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	entering = e.G.Obj(entering.ID)
	entering.Zone = state.ZExile
	e.G.SetZone(state.ZExile, entering.Owner, []state.ObjID{entering.ID})

	// Preconditions: the trigger source is live on the battlefield, its
	// candidate starts in exile, and Permanent's battlefield requirement
	// differs between the live post-move object and the LKI snapshot.
	if source == entering.ID || e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatalf("precondition: source %d must be distinct and on battlefield", source)
	}
	if entering.Zone != state.ZExile {
		t.Fatalf("precondition: entering candidate zone=%v, want exile", entering.Zone)
	}
	lki := entering.CloneDeep()
	if lki.Zone != state.ZExile || lki.Zone == state.ZBattlefield {
		t.Fatalf("precondition: LKI zone=%v must fail Permanent's battlefield requirement", lki.Zone)
	}

	before := len(e.pendingTriggers)
	ev := e.emit(events.Event{Kind: events.MoveZone, Obj: entering.ID, From: state.ZExile,
		To: state.ZBattlefield, Player: entering.Owner})
	live := e.G.Obj(entering.ID)
	if live == nil {
		t.Fatal("precondition: entering object disappeared after the move")
	}
	if live.Zone != state.ZBattlefield || live.Zone == lki.Zone {
		t.Fatalf("precondition: live zone=%v and LKI zone=%v must differ after entry", live.Zone, lki.Zone)
	}
	if got := len(e.pendingTriggers) - before; got != 1 {
		t.Fatalf("queued triggers = %d, want the Permanent.YouCtrl entry trigger (event %+v)", got, ev)
	}
	if got := e.pendingTriggers[len(e.pendingTriggers)-1].Source; got != source {
		t.Fatalf("queued trigger Source=%d, want watcher %d", got, source)
	}
}
