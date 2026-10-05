package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// EOE Unravel end to end: a counter moves the targeted spell off the stack,
// and a CHAINED sub-ability then reads Targeted$CastTotalManaSpent. The move
// zeroes the spell's live cast captures (CR 400.7), so without the
// resolution-start snapshot the read is 0 and Unravel's "less than its mana
// value" gate wrongly fires. This test casts the target spell for REAL mana
// (never hand-stamping ManaSpent) so it proves the pay-time capture survives
// the counter through the effects.Resolve entry snapshot.

// unravelProbeSrc counters the targeted spell, then places X charge counters
// on my artifact where X is the mana that was spent to cast the countered
// spell. The chained PutCounter runs AFTER the Counter's move, so only the
// snapshot can answer X.
func unravelProbeSrc() string {
	return "Name:Unravel Probe\nManaCost:0\nTypes:Instant\n" +
		"A:SP$ Counter | TargetType$ Spell | ValidTgts$ Card | SubAbility$ DBPut | SpellDescription$ Counter it.\n" +
		"SVar:DBPut:DB$ PutCounter | Choices$ Artifact.YouCtrl | CounterType$ CHARGE | CounterNum$ X\n" +
		"SVar:X:Targeted$CastTotalManaSpent\n" +
		"Oracle:x\n"
}

// spendReaderSrc is a battlefield permanent whose face reads the
// TRIGGER-relative spend spelling. Its presence forces the engine's pay-time
// cast-spend capture (rules.paymentPlanBoardSpendReaderOut), so the target
// spell's Object.ManaSpent is really stamped by the real cast -- exactly the
// board a real game has when any spend-reading permanent is out. It is not
// itself the reader under test; the probe reads the value through the
// Targeted$ snapshot.
func spendReaderSrc() string {
	return "Name:Spend Reader\nTypes:Creature\nPT:1/1\n" +
		"SVar:Z:TriggeredCard$CastTotalManaSpent\nOracle:x\n"
}

// TestTargetedCastTotalManaSpentSeesPaidSpendAfterCounter is the end-to-end
// proof: a seven-mana spell is cast for real, countered by the probe before it
// resolves, and the chained read places seven charge counters.
func TestTargetedCastTotalManaSpentSeesPaidSpendAfterCounter(t *testing.T) {
	t.Parallel()
	probe := card(t, unravelProbeSrc())
	artifact := card(t, "Name:Spend Catcher\nTypes:Artifact\nOracle:x\n")
	reader := card(t, spendReaderSrc())
	// {5}{R}{R} = 7 mana, so the spent total is a distinctive 7.
	target := card(t, "Name:Big Spell\nManaCost:5 R R\nTypes:Sorcery\nOracle:x\n")
	e, _ := tokenReplGameSeats(t, 947, []*cards.Card{probe, artifact, reader, target}, nil)
	probeID := moveSeededCard(t, e, 0, probe, state.ZHand)
	artifactID := moveSeededCard(t, e, 0, artifact, state.ZBattlefield)
	readerID := moveSeededCard(t, e, 0, reader, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZHand)

	// Precondition: the spend reader really is on the battlefield, so the
	// pay-time capture gate below is genuinely open.
	if o := e.G.Obj(readerID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: spend reader = %+v, want battlefield", e.G.Obj(readerID))
	}

	// Seat 0 casts the seven-mana spell for real; the engine pays its cost and
	// returns priority to seat 0 with the spell on the stack.
	addMana(t, e, 0, "RRRRRRR")
	submitChoices(t, e, castOptionFor(t, e, targetID).Index)
	// Precondition: the target spell is on the stack with a real captured
	// spend of 7 (the pay-time capture, not a hand-stamp).
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: target spell = %+v, want on the stack", e.G.Obj(targetID))
	}
	if got := e.G.Obj(targetID).ManaSpent; got != 7 {
		t.Fatalf("precondition: target spell ManaSpent = %d, want 7 (paid {5}{R}{R})", got)
	}
	if o := e.G.Obj(artifactID); o == nil || o.Zone != state.ZBattlefield || o.Counter("CHARGE") != 0 {
		t.Fatalf("precondition: artifact = %+v, want a clean battlefield Artifact", e.G.Obj(artifactID))
	}

	// Seat 0 responds with the probe, targeting its own stack spell before
	// priority passes into resolution. The probe costs 0.
	submitChoices(t, e, castModeOption(t, e, probeID, ""))
	targetObject(t, e, targetID)
	passUntilStackEmpty(t, e, 30)

	// Precondition: the counter really happened -- the target left the stack
	// and its live spend was zeroed by the move.
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZGraveyard || o.ManaSpent != 0 {
		t.Fatalf("precondition: countered spell = %+v, want graveyard with live ManaSpent 0", e.G.Obj(targetID))
	}
	if got := e.G.Obj(artifactID).Counter("CHARGE"); got != 7 {
		t.Fatalf("artifact CHARGE = %d, want 7 (the mana really spent to cast the countered spell)", got)
	}
}
