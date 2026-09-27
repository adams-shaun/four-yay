package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestKnowledgePoolImprintedDepartureLKI is the end-to-end regression for the
// DISTINCT-source ChangesZone departure path using the REAL corpus Knowledge
// Pool and its own printed trigger line:
//
//	T:Mode$ ChangesZone | Origin$ Exile | Destination$ Any | Static$ True |
//	ValidCard$ Card.IsImprinted | Execute$ DBForget
//
// The trigger lives on the pool artifact and fires on a DIFFERENT object's
// departure from exile. The MoveZone fold has already placed the moving card
// in the destination zone by trigger-check time, where the CR 607.2a
// exile-only imprint association is dead, so a live `Card.IsImprinted` read
// can never match: the departure must be judged against the event's LKI
// snapshot, still in exile. Earlier tests exercise an inline card
// (TestPoolTriggerMatchesImprintedCardLeavingExile) and the predicate alone
// against the corpus card (effects TestIsImprintedMatchesZoneChangeLKICandidate);
// this one drives the real printed trigger through the registered matcher and
// the real dispatch, and pins the entering-zone contract alongside it.
func TestKnowledgePoolImprintedDepartureLKI(t *testing.T) {
	poolCard := corpusAlternativeCard(t, "Knowledge Pool")
	bearsCard := corpusAlternativeCard(t, "Grizzly Bears")

	e := combatEngine(t)
	pool := onBoardCard(t, e, 0, poolCard)

	linked := e.G.AddObject(bearsCard, 0)
	linked = e.G.Obj(linked.ID)
	linked.Zone = state.ZExile
	e.G.SetZone(state.ZExile, linked.Owner, []state.ObjID{linked.ID})

	bystander := e.G.AddObject(bearsCard, 1)
	bystander = e.G.Obj(bystander.ID)
	bystander.Zone = state.ZExile
	e.G.SetZone(state.ZExile, bystander.Owner, []state.ObjID{bystander.ID})

	// The association lives on the SOURCE (the pool's Imprinted list names the
	// exiled Bear), the ordinary imprint shape and the one Knowledge Pool's
	// own Dig/Imprint$ True line produces.
	poolObj := e.G.Obj(pool)
	poolObj.Imprinted = []state.ObjID{linked.ID}

	// The trigger whose printed ValidCard$ is exactly Card.IsImprinted,
	// Origin$ Exile. Assert it parsed with those parameters so the test fails
	// loudly if the corpus face changes shape.
	var trigger *cards.Trigger
	for i := range poolObj.Face().Triggers {
		tr := &poolObj.Face().Triggers[i]
		if tr.Mode == "ChangesZone" && tr.Params["Origin"] == "Exile" &&
			strings.Contains(tr.Params["ValidCard"], "Card.IsImprinted") {
			trigger = tr
			break
		}
	}
	if trigger == nil {
		t.Fatal("precondition: Knowledge Pool's printed Origin$ Exile Card.IsImprinted trigger was not parsed")
	}

	// Preconditions: distinct objects, the pool in the zone its default
	// TriggerZones$ reads, the linked card in the zone the association's
	// liveness rule reads, and the link really runs pool->card.
	if pool == linked.ID || pool == bystander.ID || linked.ID == bystander.ID {
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

	// The LKI a zone-change trigger captures is the object as it was in exile.
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

	// Direct matcher route with the REAL printed trigger: the departure is
	// judged against the pre-move candidate, so the imprinted Bear leaving
	// exile matches.
	if !e.zoneChangeMatches(*trigger, pool, ev, &lki) {
		t.Fatal("Knowledge Pool's printed ChangesZone Card.IsImprinted trigger did not match an imprinted card leaving exile")
	}

	// Dispatch route: the same departure queues the pool's forget trigger,
	// carrying the exile LKI.
	queued := len(e.pendingTriggers) - before
	if queued != 1 {
		t.Fatalf("queued triggers = %d, want exactly the pool's departure trigger", queued)
	}
	pt := e.pendingTriggers[len(e.pendingTriggers)-1]
	if pt.Source != pool {
		t.Fatalf("queued trigger Source=%d, want the pool %d", pt.Source, pool)
	}
	if pt.Ctx.LKI == nil || pt.Ctx.LKI.ID != linked.ID || pt.Ctx.LKI.Zone != state.ZExile {
		t.Fatalf("queued trigger must carry the linked card's exile LKI, got %+v", pt.Ctx.LKI)
	}

	// A bystander leaving exile is not in the pool's association: the same
	// departure must queue nothing.
	lkiB := e.G.Obj(bystander.ID).CloneDeep()
	beforeB := len(e.pendingTriggers)
	evB := e.emit(events.Event{Kind: events.MoveZone, Obj: bystander.ID, From: state.ZExile,
		To: state.ZHand, Player: bystander.Owner})
	if liveB := e.G.Obj(bystander.ID); liveB == nil || liveB.Zone != state.ZHand || liveB.Zone == lkiB.Zone {
		t.Fatalf("precondition: the bystander must have folded into hand with an exile LKI that differs")
	}
	if got := len(e.pendingTriggers) - beforeB; got != 0 {
		t.Fatalf("bystander departure queued %d triggers, want 0", got)
	}
	// Belt and braces on the matcher too.
	if e.zoneChangeMatches(*trigger, pool, evB, &lkiB) {
		t.Error("the matcher matched a bystander that was never imprinted")
	}

	// Live entering-zone contract: an exile->battlefield move judged against
	// the LIVE entered object still matches (the new look-back must not make
	// an entering-zone predicate read the exile snapshot). Permanent requires
	// the candidate on the battlefield, which is true of the live object and
	// false of its exile LKI.
	entering := e.G.AddObject(bearsCard, 0)
	entering = e.G.Obj(entering.ID)
	entering.Zone = state.ZExile
	e.G.SetZone(state.ZExile, entering.Owner, []state.ObjID{entering.ID})
	entryTrigger := cards.Trigger{Mode: "ChangesZone", Params: map[string]string{
		"Origin":      "Exile",
		"Destination": "Battlefield",
		"ValidCard":   "Permanent.YouCtrl",
	}}
	entryLKI := entering.CloneDeep()
	evEnt := e.emit(events.Event{Kind: events.MoveZone, Obj: entering.ID, From: state.ZExile,
		To: state.ZBattlefield, Player: entering.Owner})
	if liveE := e.G.Obj(entering.ID); liveE == nil || liveE.Zone != state.ZBattlefield || liveE.Zone == entryLKI.Zone {
		t.Fatalf("precondition: the entering card must be on the battlefield with an exile LKI that differs")
	}
	if !e.zoneChangeMatches(entryTrigger, pool, evEnt, &entryLKI) {
		t.Error("an entering-zone Permanent.YouCtrl predicate must match the LIVE entered object, not the exile LKI")
	}
}
