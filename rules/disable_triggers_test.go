package rules

import (
	"strings"
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

// moveObj parks c in p's named zone and returns its id.
func parkObj(t *testing.T, e *Engine, c *cards.Card, p state.PlayerID, z state.Zone) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = z
	e.G.SetZone(z, p, append(e.G.Zone(z, p), o.ID))
	return o.ID
}

// TestDisableTriggersValidCardScopesSource pins the ValidCard$ parameter: it
// scopes WHICH permanent's abilities are suppressed by matching the trigger's
// SOURCE, distinct from ValidCause$ which matches the event object. Elesh
// Norn's shape (Permanent.OppCtrl+inZoneBattlefield) suppresses only
// permanents controlled by the static controller's opponents.
func TestDisableTriggersValidCardScopesSource(t *testing.T) {
	e := newSeats(t, 2)
	// The static sits under p0 and suppresses creatures NOT controlled by p0.
	staticCard := card(t, "Name:Opp Only Gate\nTypes:Artifact\n"+
		"S:Mode$ DisableTriggers | ValidCause$ Creature | ValidMode$ ChangesZone,ChangesZoneAll | Destination$ Battlefield | ValidCard$ Permanent.OppCtrl+inZoneBattlefield\nOracle:x\n")
	etb := "T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this enters, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	creature := card(t, "Name:ETB Creature\nTypes:Creature\n"+etb+"Oracle:x\n")

	gate := parkObj(t, e, staticCard, 0, state.ZBattlefield)
	oppID := parkObj(t, e, creature, 1, state.ZHand)
	ownID := parkObj(t, e, creature, 0, state.ZHand)

	if len(e.activeStatics("DisableTriggers")) != 1 {
		t.Fatalf("precondition: static from %d is not active", gate)
	}
	// The scoped source spec must distinguish p1's permanent (suppressed)
	// from p0's (kept). Probe with battlefield-resident TRIGGERLESS objects,
	// because the spec's inZoneBattlefield term cannot distinguish hand cards
	// and a probe with a trigger would itself queue on later entries.
	probe := card(t, "Name:Probe Permanent\nTypes:Creature\nOracle:x\n")
	probeOpp := parkObj(t, e, probe, 1, state.ZBattlefield)
	probeOwn := parkObj(t, e, probe, 0, state.ZBattlefield)
	sc := e.specCtx(gate, 0)
	if !e.matchesSpec("Permanent.OppCtrl+inZoneBattlefield", probeOpp, sc) ||
		e.matchesSpec("Permanent.OppCtrl+inZoneBattlefield", probeOwn, sc) {
		t.Fatalf("precondition: ValidCard$ OppCtrl does not distinguish %d from %d", probeOpp, probeOwn)
	}
	moveIn := func(id state.ObjID) int {
		before := len(e.pendingTriggers) + len(e.G.Stack)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
		return len(e.pendingTriggers) + len(e.G.Stack) - before
	}
	if n := moveIn(oppID); n != 0 {
		t.Fatalf("opponent's creature queued %d trigger(s), want 0 under ValidCard$ OppCtrl", n)
	}
	if n := moveIn(ownID); n != 1 {
		t.Fatalf("controller's own creature queued %d trigger(s), want 1 (ValidCard$ excludes it)", n)
	}
}

// TestDisableTriggersReadsOriginAndDestination pins the zone-transition
// parameters on the death shape (Hushbringer's second line): Origin$
// Battlefield | Destination$ Graveyard suppresses a creature's dies trigger
// but must NOT suppress its enter trigger (different destination).
func TestDisableTriggersReadsOriginAndDestination(t *testing.T) {
	e := newSeats(t, 2)
	staticCard := card(t, "Name:Death Gate\nTypes:Artifact\n"+
		"S:Mode$ DisableTriggers | ValidCause$ Creature | ValidMode$ ChangesZone,ChangesZoneAll | Origin$ Battlefield | Destination$ Graveyard | Secondary$ True\nOracle:x\n")
	etb := "T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this enters, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	dies := "T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this dies, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	etbCreature := card(t, "Name:ETB Creature\nTypes:Creature\n"+etb+"Oracle:x\n")
	dying := card(t, "Name:Dying Creature\nTypes:Creature\n"+dies+"Oracle:x\n")

	if gate := parkObj(t, e, staticCard, 0, state.ZBattlefield); len(e.activeStatics("DisableTriggers")) != 1 {
		t.Fatalf("precondition: static from %d is not active", gate)
	}
	enterID := parkObj(t, e, etbCreature, 1, state.ZHand)
	dyingID := parkObj(t, e, dying, 1, state.ZBattlefield)

	// The death shape's destination is Graveyard, so the enter trigger (To
	// Battlefield) must survive; assert the reason on the static's own gates.
	if n := countQueued(e, enterID, state.ZHand, state.ZBattlefield); n != 1 {
		t.Fatalf("enter trigger queued %d, want 1 (Destination$ Graveyard must not match an enter)", n)
	}
	if n := countQueued(e, dyingID, state.ZBattlefield, state.ZGraveyard); n != 0 {
		t.Fatalf("dies trigger queued %d, want 0 under Origin$ Battlefield/Destination$ Graveyard", n)
	}
}

// countQueued emits one MoveZone from->to for id and returns how many triggers
// queued (pending or on the stack).
func countQueued(e *Engine, id state.ObjID, from, to state.Zone) int {
	before := len(e.pendingTriggers) + len(e.G.Stack)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
	return len(e.pendingTriggers) + len(e.G.Stack) - before
}

// TestDisableTriggersUnreadParameterFailsOpen pins the fail-closed contract: a
// DisableTriggers line carrying a parameter this build does not read (the
// Static$ shape below) suppresses NOTHING, fires a loud unmodelled Note
// exactly once, and leaves the trigger firing.
func TestDisableTriggersUnreadParameterFailsOpen(t *testing.T) {
	e := newSeats(t, 2)
	staticCard := card(t, "Name:Unread Gate\nTypes:Artifact\n"+
		"S:Mode$ DisableTriggers | Secondary$ True | Static$ True | ValidCard$ Creature.OppCtrl+inZoneBattlefield\nOracle:x\n")
	etb := "T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ When this enters, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"
	creature := card(t, "Name:ETB Creature\nTypes:Creature\n"+etb+"Oracle:x\n")

	if gate := parkObj(t, e, staticCard, 0, state.ZBattlefield); len(e.activeStatics("DisableTriggers")) != 1 {
		t.Fatalf("precondition: static from %d is not active", gate)
	}
	id := parkObj(t, e, creature, 1, state.ZHand)
	if got := disableTriggersUnread(e.activeStatics("DisableTriggers")[0]); len(got) != 1 || got[0] != "Static" {
		t.Fatalf("precondition: unread params = %v, want [Static]", got)
	}
	if n := countQueued(e, id, state.ZHand, state.ZBattlefield); n != 1 {
		t.Fatalf("unread-parameter static suppressed the trigger (queued %d, want 1)", n)
	}
	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unmodelled DisableTriggers parameters: Static") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("%d unmodelled DisableTriggers notes, want exactly 1; log=%+v", notes, e.L.Events)
	}
}
