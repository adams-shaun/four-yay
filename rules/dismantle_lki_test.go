package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDismantleDestroysCounteredTargetAndPlacesCounters is the ordinary
// destruction path for Dismantle (the destructible counterpart of
// TestDismantleCounterTypeChoiceContinuesAfterDeterministicRecipient, which
// deliberately uses an indestructible target). Dismantle's Destroy moves the
// targeted artifact to the graveyard before DBPutCounter evaluates BOTH
// `ConditionDefined$ Targeted | ConditionPresent$ Card.HasCounters` and
// `X:Targeted$CardCounters.ALL`; both must read the target's
// last-known-information counters (CR 608.2b/h), which the engine now
// captures at the start of resolution (Ctx.TargetCountersLKI). The test
// proves the target really was destroyed (so the counters are gone from the
// live object) AND two counters of the chosen kind landed on the caster's
// recipient -- i.e. the gate was met and the amount survived the move.
func TestDismantleDestroysCounteredTargetAndPlacesCounters(t *testing.T) {
	t.Parallel()
	dismantle := mustCorpusCardT(t, "Dismantle")
	// The target has NO indestructible: it is destroyed, so only LKI can
	// carry its counters into the chained PutCounter.
	target := card(t, "Name:Dismantled Relic\nTypes:Artifact\nOracle:x\n")
	recipient := card(t, "Name:Remaining Relic\nTypes:Artifact\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 923, []*cards.Card{dismantle, recipient}, []*cards.Card{target})
	dismantleID := moveSeededCard(t, e, 0, dismantle, state.ZHand)
	targetID := moveSeededCard(t, e, 1, target, state.ZBattlefield)
	recipientID := moveSeededCard(t, e, 0, recipient, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 2})
	e.pending = nil

	// Preconditions the rule reads: the counter-carrying artifact really is on
	// the battlefield with exactly two counters, and the recipient has none
	// (so a passing assertion cannot come from a live target).
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 2 {
		t.Fatalf("precondition: Dismantle target = %+v, want battlefield Artifact with two P1P1", o)
	}
	if o := e.G.Obj(recipientID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 0 || o.Counter("CHARGE") != 0 {
		t.Fatalf("precondition: recipient = %+v, want clean battlefield Artifact", o)
	}

	addMana(t, e, 0, "RRR")
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == dismantleID {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("precondition: Dismantle cast option absent: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	targetObject(t, e, targetID)

	// The deterministic recipient (the one Artifact.YouCtrl) leaves only the
	// counter-type choice, exactly as the indestructible-target case does.
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_kind" || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("Dismantle must continue to the kind ask after the target is destroyed: %+v", d)
	}
	// The target is already gone, so the gate and the amount were read from
	// the pre-move snapshot, not the live object.
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: destroyed target = %+v, want in the graveyard before the kind answer", o)
	}
	if o := e.G.Obj(targetID); o != nil && o.Counter("P1P1") != 0 {
		t.Fatalf("precondition: destroyed target P1P1 = %d, want the live counters cleared by the move", o.Counter("P1P1"))
	}
	charge := optionIndexByLabel(d, "CHARGE")
	if charge < 0 {
		t.Fatalf("Dismantle kind options = %+v, want CHARGE", d.Options)
	}
	submitChoices(t, e, charge)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(recipientID).Counter("CHARGE"); got != 2 {
		t.Fatalf("recipient CHARGE = %d, want 2 from the destroyed target's counters", got)
	}
	if got := e.G.Obj(recipientID).Counter("P1P1"); got != 0 {
		t.Fatalf("recipient P1P1 = %d, want 0 after choosing CHARGE", got)
	}
	replayCheck(t, e, cfg)
}

// TestChainReadsTargetCountersChangedEarlierInResolution is the
// departure-boundary regression: the CR 608.2b/h look-back must read the
// counters a target carried IMMEDIATELY BEFORE its zone change, not the
// resolution-start snapshot. The probe spell adds one +1/+1 counter to its
// targeted artifact (which already carries two), destroys it, then --
// chained, exactly Dismantle's DBPutCounter shape -- puts X charge counters
// on the caster's other artifact, with X read from
// Targeted$CardCounters.ALL. The correct amount is THREE; the stale
// resolution-start snapshot would size TWO.
func TestChainReadsTargetCountersChangedEarlierInResolution(t *testing.T) {
	t.Parallel()
	probe := card(t, "Name:Counter Reap\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ PutCounter | ValidTgts$ Artifact.YouDontCtrl | CounterType$ P1P1 | CounterNum$ 1 | SubAbility$ DBDestroy\n"+
		"SVar:DBDestroy:DB$ Destroy | Defined$ Targeted | SubAbility$ DBPut\n"+
		"SVar:DBPut:DB$ PutCounter | Choices$ Artifact.YouCtrl | CounterType$ CHARGE | CounterNum$ X | ConditionDefined$ Targeted | ConditionPresent$ Card.HasCounters\n"+
		"SVar:X:Targeted$CardCounters.ALL\nOracle:x\n")
	target := card(t, "Name:Counted Relic\nTypes:Artifact\nOracle:x\n")
	recipient := card(t, "Name:Spare Relic\nTypes:Artifact\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 924, []*cards.Card{probe, recipient}, []*cards.Card{target})
	probeID := moveSeededCard(t, e, 0, probe, state.ZHand)
	targetID := moveSeededCard(t, e, 1, target, state.ZBattlefield)
	recipientID := moveSeededCard(t, e, 0, recipient, state.ZBattlefield)
	e.emit(events.Event{Kind: events.CounterChange, Obj: targetID, Counter: "P1P1", Amount: 2})
	addMana(t, e, 0, "") // toMain1 + priorityRound; the probe costs 0

	// Preconditions the rule reads: the target really is an opposing
	// battlefield artifact with exactly two P1P1 (the chain adds ONE more and
	// THEN destroys it), and the recipient has no counters -- so a passing
	// assertion cannot come from a live target or a pre-existing recipient
	// counter.
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 || o.Counter("P1P1") != 2 {
		t.Fatalf("precondition: target = %+v, want opponent battlefield Artifact with two P1P1", o)
	}
	if o := e.G.Obj(recipientID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 0 || o.Counter("CHARGE") != 0 {
		t.Fatalf("precondition: recipient = %+v, want clean battlefield Artifact", o)
	}

	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == probeID {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("precondition: probe cast option absent: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	targetObject(t, e, targetID)
	passUntilStackEmpty(t, e, 30)

	// The target was destroyed (after the chain added its own counter), so
	// only the departure-boundary look-back can size the placement.
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("postcondition: target = %+v, want in the graveyard", o)
	}
	if got := e.G.Obj(recipientID).Counter("CHARGE"); got != 3 {
		t.Fatalf("recipient CHARGE = %d, want 3 (the target's counters immediately before destruction: 2 + the chain's own 1)", got)
	}
	if got := e.G.Obj(recipientID).Counter("P1P1"); got != 0 {
		t.Fatalf("recipient P1P1 = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}
