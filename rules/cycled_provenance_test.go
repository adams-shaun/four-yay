package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the Cycled-trigger provenance fix (AGENTS.md "Known
// approximations" row: "**Cycled** matches only a printed-`K:Cycling` face's
// cost-discard; layer-granted cycling misses and another card's cost
// false-fires"). The trigger now matches the CYCLING ABILITY recorded on the
// discard (events.DiscardCostCycling), never the moved card's printed face:
//
//   - a discard tagged as a cycling ability's cost fires, even when the card
//     prints no Cycling (the granted-cycling direction: any cycling ability,
//     printed or layer-granted, tags its own discard);
//   - a bare cost discard of a card that DOES print Cycling does not fire (the
//     false-fire direction: an unrelated ability's discard cost is not a
//     cycle).
//
// The row is FROZEN and is not edited here; the sibling ticket
// cli-20260923T060000Z-trig-attackerblocked deletes it once every sub-shape
// lands.

// cycledSelfTriggerSrc is a card whose ONLY cycling relationship is the
// Mode$ Cycled trigger; no cycling keyword is printed, so the old
// printed-keyword matcher could never fire it.
const cycledSelfTriggerSrc = "Name:Provenance Cycler\nManaCost:U\nTypes:Instant\nOracle:x\n" +
	"T:Mode$ Cycled | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When you cycle CARDNAME, draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"

// cycledPrintedSrc prints K:Cycling and carries the same self trigger.
const cycledPrintedSrc = "Name:Printed Cycler\nManaCost:U\nTypes:Instant\nK:Cycling:U\nOracle:x\n" +
	"T:Mode$ Cycled | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When you cycle CARDNAME, draw a card.\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n"

// TestCycledFiresForAProvenanceTaggedDiscard is the granted-cycling direction:
// a discard tagged as a cycling ability's cost fires Mode$ Cycled on a card
// that prints NO Cycling. The precondition asserts the fixture really has no
// printed keyword, so the assertion cannot pass by the old printed-face path.
func TestCycledFiresForAProvenanceTaggedDiscard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, cycledSelfTriggerSrc))
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Face() == nil {
		t.Fatal("precondition: fixture card has no face")
	} else if o.Face().HasKeyword("Cycling") {
		t.Fatal("precondition: fixture must NOT print Cycling for this direction to mean anything")
	}

	handBefore := len(e.G.Zone(state.ZHand, 0))
	e.emit(events.DiscardCostCycling(id, "Cycling"))
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition: tagged discard left the card in %s, want graveyard", got)
	}
	if got := len(e.pendingTriggers); got != 1 {
		t.Fatalf("pendingTriggers after the tagged discard = %d, want 1 (a tagged cost discard is a cycle)", got)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	// The discard left the hand (-1) and the trigger drew a card (+1).
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("hand after the cycled draw = %d, want %d", got, handBefore)
	}
}

// TestCycledIgnoresABareCostDiscardOfAPrintedCycler is the false-fire
// direction: a bare events.DiscardCost of a card that PRINTS Cycling is an
// unrelated cost discard and must not queue Mode$ Cycled. The precondition
// asserts the fixture prints Cycling and the bare event carries no cycling
// cause; the second engine proves the matcher is live by firing the same card
// on a properly tagged discard.
func TestCycledIgnoresABareCostDiscardOfAPrintedCycler(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, cycledPrintedSrc))
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Face() == nil {
		t.Fatal("precondition: fixture card has no face")
	} else if !o.Face().HasKeyword("Cycling") {
		t.Fatal("precondition: fixture must print Cycling, else the bare discard is trivially inert")
	}
	bare := events.DiscardCost(id)
	if kw, tagged := events.IsCyclingDiscard(bare); tagged {
		t.Fatalf("precondition: a bare DiscardCost carried cycling cause %q", kw)
	}
	e.emit(bare)
	if got := len(e.pendingTriggers); got != 0 {
		t.Fatalf("a bare cost discard of a printed cycler queued %d triggers, want 0", got)
	}

	// The handler is live: exactly the same card, with the cycling cause on
	// the discard, fires the trigger.
	live := handEngine(t, card(t, cycledPrintedSrc))
	liveID := live.G.Zone(state.ZHand, 0)[0]
	live.emit(events.DiscardCostCycling(liveID, "Cycling"))
	if got := len(live.pendingTriggers); got != 1 {
		t.Fatalf("tagged discard on the same fixture queued %d triggers, want 1 (matcher must be live)", got)
	}
}

// TestCycledIgnoresADiscardCostPaidForADifferentAbility is the end-to-end
// false-fire: a real corpus card that prints Cycling is discarded to pay a
// DIFFERENT ability's cost, and a real battlefield Cycled trigger (Valiant
// Rescuer, "Whenever you cycle another card for the first time each turn")
// must not fire. A genuine tagged cycle of the same kind then fires it, so the
// no-fire assertion cannot pass because the trigger is unregistered.
func TestCycledIgnoresADiscardCostPaidForADifferentAbility(t *testing.T) {
	t.Parallel()
	rescuer := mshCorpusCard(t, "Valiant Rescuer")
	aven := mshCorpusCard(t, "Windcaller Aven")
	tusker := mshCorpusCard(t, "Warped Tusker")
	e := handEngine(t, aven, tusker)
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 2 {
		t.Fatalf("precondition: fixture hand has %d cards, want 2", len(hand))
	}
	avenID, tuskerID := hand[0], hand[1]
	if !e.G.Obj(avenID).Face().HasKeyword("Cycling") || !e.G.Obj(tuskerID).Face().HasKeyword("Cycling") {
		t.Fatal("precondition: both fixture cards must print Cycling")
	}
	rescuerID := onBoardCard(t, e, 0, rescuer)
	outlet := onBoard(t, e, 0, "Name:Discard Outlet\nTypes:Artifact\nA:AB$ Draw | Cost$ Discard<1/Card> | NumCards$ 1\nOracle:x\n")

	var opt decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == outlet {
			opt = o
			break
		}
	}
	if opt.Kind != "ability" {
		t.Fatal("precondition: the discard-outlet activation was not offered")
	}
	e.beginActivation(0, opt)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("precondition: discard cost decision = %+v, want a KChoose", d)
	}
	avenIdx := -1
	for _, o := range d.Options {
		if o.Obj == avenID {
			avenIdx = o.Index
		}
	}
	if avenIdx < 0 {
		t.Fatalf("precondition: discard chooser did not offer the cycling card: %+v", d.Options)
	}
	submitChoices(t, e, avenIdx)
	if got := e.G.Obj(avenID).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition: cost discard left Windcaller Aven in %s, want graveyard", got)
	}
	// The printed cycler was discarded as this ability's cost, so NO Cycled
	// trigger may queue: only the outlet ability stands on the stack.
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("pendingTriggers after the unrelated cost discard = %d, want 0", len(e.pendingTriggers))
	}
	if got := len(e.G.Stack); got != 1 {
		t.Fatalf("stack after the unrelated cost discard = %d, want 1 (only the outlet ability)", got)
	}
	// A genuine tagged cycle of another card fires the same trigger, proving
	// the matcher is live on this board. Both Warped Tusker's own Card.Self
	// trigger and Valiant Rescuer's Card.Other trigger queue, so asserting the
	// Rescuer is among the sources proves the false-fire axis was a real
	// rejection rather than an unregistered trigger.
	rescuers := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == rescuerID {
			rescuers++
		}
	}
	if rescuers != 0 {
		t.Fatalf("precondition: Valiant Rescuer already queued %d triggers before the tagged cycle", rescuers)
	}
	e.emit(events.DiscardCostCycling(tuskerID, "Cycling"))
	rescuers = 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == rescuerID {
			rescuers++
		}
	}
	if rescuers != 1 {
		t.Fatalf("a tagged cycle of another card queued %d Valiant Rescuer triggers, want 1 (matcher must be live)", rescuers)
	}
}

// TestCyclingProvenanceSurvivesAReplacementRedirect pins the CR 614.6 carry:
// a replacement that substitutes a different move for a cycling cost discard
// keeps the cycling cause on the substituted move (events.CarryAction), so a
// Mode$ Cycled trigger still sees the redirected discard as a cycle. Without
// the carry the substituted move's Counter is empty and the trigger misses.
func TestCyclingProvenanceSurvivesAReplacementRedirect(t *testing.T) {
	t.Parallel()
	const obj state.ObjID = 7
	cyc := events.DiscardCostCycling(obj, "Cycling")
	if kw, ok := events.IsCyclingDiscard(cyc); !ok || kw != "Cycling" {
		t.Fatalf("precondition: source event is not a cycling discard: ok=%v kw=%q", ok, kw)
	}
	marker := events.ActionMarker(cyc)
	// A replacement body's substituted move: same object, same origin, no
	// marker or cause of its own.
	sub := events.Event{Kind: events.MoveZone, Obj: obj, From: state.ZHand, To: state.ZExile}
	if sub.Counter != "" {
		t.Fatal("precondition: the substituted move already carries a cause")
	}
	got := events.CarryAction(marker, obj, sub)
	kw, ok := events.IsCyclingDiscard(got)
	if !ok || kw != "Cycling" {
		t.Fatalf("redirected cycling discard lost its cause: ok=%v kw=%q counter=%q", ok, kw, got.Counter)
	}
}
