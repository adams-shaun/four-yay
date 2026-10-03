package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDisableTriggersMatchesCauseBeforeQueue pins the printed DisableTriggers
// static at the shared trigger matcher (rules/disable_triggers.go): an
// enter-the-battlefield trigger carried by the PERMANENT THAT ENTERS does not
// queue while a matching static is on the battlefield, an Artifact cause the
// static's ValidCause$ does not name still queues, and removing the static
// lets the same creature's ETB queue again. The two cause types make the test
// unable to pass by matching everything.
func TestDisableTriggersMatchesCauseBeforeQueue(t *testing.T) {
	e := newSeats(t, 2)
	staticCard := card(t, "Name:Cause Gate\nTypes:Artifact\n"+
		"S:Mode$ DisableTriggers | ValidCause$ Creature | ValidMode$ ChangesZone | Destination$ Battlefield\nOracle:x\n")
	etb := "T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | " +
		"TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this enters, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	creature := card(t, "Name:Entering Creature\nTypes:Creature\n"+etb+"Oracle:x\n")
	artifact := card(t, "Name:Entering Artifact\nTypes:Artifact\n"+etb+"Oracle:x\n")

	addHand := func(c *cards.Card, p state.PlayerID) state.ObjID {
		t.Helper()
		o := e.G.AddObject(c, p)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
		return o.ID
	}
	gate := e.G.AddObject(staticCard, 0)
	gate.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), gate.ID))
	creatureID := addHand(creature, 1)
	artifactID := addHand(artifact, 1)

	if len(e.activeStatics("DisableTriggers")) != 1 {
		t.Fatalf("precondition: DisableTriggers static from %d is not active", gate.ID)
	}
	if creatureID == artifactID || gate.ID == creatureID {
		t.Fatalf("precondition: fixture objects share an ObjID")
	}
	// The ValidCause$ spec must distinguish the two entering objects, or the
	// Artifact half below would pass vacuously.
	sc := e.specCtx(gate.ID, 0)
	if !e.matchesSpec("Creature", creatureID, sc) || e.matchesSpec("Creature", artifactID, sc) {
		t.Fatalf("precondition: ValidCause$ Creature does not distinguish %d from %d", creatureID, artifactID)
	}

	moveIn := func(id state.ObjID) int {
		t.Helper()
		before := len(e.pendingTriggers) + len(e.G.Stack)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
		return len(e.pendingTriggers) + len(e.G.Stack) - before
	}

	// Live control first: with the static present an Artifact cause (not named
	// by ValidCause$ Creature) MUST still queue, so a later zero on the
	// creature is the gate's doing rather than a harness that never fires.
	if n := moveIn(artifactID); n != 1 {
		t.Fatalf("Artifact cause queued %d trigger(s), want 1 (ValidCause$ Creature must not match it)", n)
	}
	if o := e.G.Obj(artifactID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: artifact cause did not enter: %+v", o)
	}
	if n := moveIn(creatureID); n != 0 {
		t.Fatalf("Creature cause queued %d trigger(s) despite DisableTriggers", n)
	}
	if o := e.G.Obj(creatureID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature cause did not enter: %+v", o)
	}

	// Removing the static must let a creature's ETB queue, proving the zero
	// above was the static and not a harness that never fires.
	e.emit(events.Event{Kind: events.MoveZone, Obj: gate.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.activeStatics("DisableTriggers"); len(got) != 0 {
		t.Fatalf("precondition: static still active after leaving: %d", len(got))
	}
	againID := addHand(creature, 1)
	if n := moveIn(againID); n != 1 {
		t.Fatalf("without the static the entering creature's ETB queued %d trigger(s), want 1", n)
	}
	if o := e.G.Obj(againID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: second creature did not enter: %+v", o)
	}
}
