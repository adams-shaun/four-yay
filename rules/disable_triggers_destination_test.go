package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestDisableTriggersDestinationList(t *testing.T) {
	e := newSeats(t, 2)
	staticCard := card(t, "Name:Destination List Gate\nTypes:Artifact\n"+
		"S:Mode$ DisableTriggers | ValidCause$ Creature | ValidMode$ ChangesZone | Destination$ Graveyard,Exile\nOracle:x\n")
	trigger := "T:Mode$ ChangesZone | Origin$ Any | Destination$ Any | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this changes zones, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	creature := card(t, "Name:Zone Trigger Creature\nTypes:Creature\n"+trigger+"Oracle:x\n")

	gate := parkObj(t, e, staticCard, 0, state.ZBattlefield)
	if got := e.activeStatics("DisableTriggers"); len(got) != 1 || got[0].Source != gate {
		t.Fatalf("precondition: active DisableTriggers statics = %+v, want gate %d", got, gate)
	}
	sv := e.activeStatics("DisableTriggers")[0]
	code, ok := sv.ParamCode(cards.PKDestination)
	if !ok {
		t.Fatal("precondition: Destination$ has no compiled parameter")
	}
	dest := effects.Destination(code)
	if !dest.Admits(state.ZGraveyard) || !dest.Admits(state.ZExile) || dest.Admits(state.ZBattlefield) {
		t.Fatalf("precondition: compiled destination %v must admit Graveyard and Exile but not Battlefield", dest)
	}

	// Establish that this exact trigger fixture fires for a non-listed zone;
	// subsequent zero counts therefore exercise the active static.
	control := parkObj(t, e, creature, 1, state.ZHand)
	if n := countQueued(e, control, state.ZHand, state.ZBattlefield); n != 1 {
		t.Fatalf("control transition to Battlefield queued %d triggers, want 1", n)
	}
	for _, to := range []state.Zone{state.ZGraveyard, state.ZExile} {
		id := parkObj(t, e, creature, 1, state.ZHand)
		if n := countQueued(e, id, state.ZHand, to); n != 0 {
			t.Errorf("transition to %v queued %d triggers, want 0 under Destination$ Graveyard,Exile", to, n)
		}
	}
}
