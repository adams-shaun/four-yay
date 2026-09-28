package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the RESUME carriage of the resolution-start stack-kind
// snapshot the SpellTargeted count ref reads (task
// agent-20260920T120233Z-357a4f24, round t2). effects.Resolve captures which
// object targets were spells at Ctx.TargetSpellLKI entry; a mid-resolution ask
// suspends the chain, and the resumed Ctx is rebuilt from the stack object's
// targets whose live zone a completed Counter has already changed. Without
// carryonto the resumePoint (rules' targetSpellLKI) the resumed read answers
// zero, exactly the failure Gale's Redirection's roll ask and Press the
// Enemy's optional-cast ask would hit.

// spellMVResumeProbeSrc is the authored probe: counter the targeted spell,
// then pose a ChooseColor ask (a real suspension), then place X charge
// counters on my artifact where X is the countered spell's mana value read
// through SpellTargeted$CardManaCostLKI. The ask sits BETWEEN the move and the
// read, so only the resumed chain's snapshot can answer X.
func spellMVResumeProbeSrc() string {
	return "Name:Spell Kind Probe\nManaCost:0\nTypes:Instant\n" +
		"A:SP$ Counter | TargetType$ Spell | ValidTgts$ Card | SubAbility$ DBPick | SpellDescription$ Counter it.\n" +
		"SVar:DBPick:DB$ ChooseColor | SubAbility$ DBPut\n" +
		"SVar:DBPut:DB$ PutCounter | Choices$ Artifact.YouCtrl | CounterType$ CHARGE | CounterNum$ X\n" +
		"SVar:X:SpellTargeted$CardManaCostLKI\n" +
		"Oracle:x\n"
}

// TestSpellTargetedSurvivesMidResolutionAsk is the resumed-chain proof: the
// targeted spell is countered BEFORE the ChooseColor suspension and read only
// AFTER the resume, so the counters placed prove the snapshot crossed the ask.
func TestSpellTargetedSurvivesMidResolutionAsk(t *testing.T) {
	t.Parallel()
	probe := card(t, spellMVResumeProbeSrc())
	artifact := card(t, "Name:Charge Catcher\nTypes:Artifact\nOracle:x\n")
	enemy := card(t, "Name:Enemy Bomb\nManaCost:3 R\nTypes:Sorcery\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 941, []*cards.Card{probe, artifact}, []*cards.Card{enemy})
	probeID := moveSeededCard(t, e, 0, probe, state.ZHand)
	artifactID := moveSeededCard(t, e, 0, artifact, state.ZBattlefield)
	enemyID := moveSeededCard(t, e, 1, enemy, state.ZHand)

	// Seat 1's spell is on the stack; seat 0 gets priority to counter it.
	e.emit(events.Event{Kind: events.PutOnStack, Obj: enemyID, Player: 1, From: state.ZHand, To: state.ZStack})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "") // the probe costs 0; funds nothing and re-asks priority

	// Preconditions the rule reads: the enemy spell really is a cmc-4 stack
	// spell, the artifact is a clean battlefield permanent with no counters,
	// and the probe's own mana value (0) differs from the target's -- so the
	// placed count cannot come from the source, or from a pre-existing counter.
	if o := e.G.Obj(enemyID); o == nil || o.Zone != state.ZStack || o.Face() == nil || o.Face().Cmc() != 4 {
		t.Fatalf("precondition: enemy spell = %+v, want a cmc-4 stack spell", e.G.Obj(enemyID))
	}
	if o := e.G.Obj(artifactID); o == nil || o.Zone != state.ZBattlefield || o.Counter("CHARGE") != 0 {
		t.Fatalf("precondition: artifact = %+v, want a clean battlefield Artifact", e.G.Obj(artifactID))
	}
	if o := e.G.Obj(probeID); o == nil || o.Face() == nil || o.Face().Cmc() != 0 {
		t.Fatalf("precondition: probe = %+v, want mana value 0 (distinct from the target's 4)", e.G.Obj(probeID))
	}

	submitChoices(t, e, castModeOption(t, e, probeID, ""))
	targetObject(t, e, enemyID)

	// The Counter has already moved the spell and the chain is parked on the
	// ChooseColor ask: this is the suspension whose resume must still know the
	// target was a spell.
	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosecolor" {
		t.Fatalf("want the ChooseColor suspension after the counter: %+v", d)
	}
	if o := e.G.Obj(enemyID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: countered spell = %+v, want moved to the graveyard before the resume", e.G.Obj(enemyID))
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Obj(artifactID).Counter("CHARGE"); got != 4 {
		t.Fatalf("artifact CHARGE = %d, want 4 (the countered spell's mana value read after the resume)", got)
	}
	replayCheck(t, e, cfg)
}
