package rules

// A suspended triggered ability resumes with the same Ctx its first pass had
// (spike S3, legacy defect 3).
//
// The dual-run fuzz found a Hero token wearing Ninja's Blades -- whose static
// grants "whenever this creature deals combat damage to a player, draw a
// card, then discard a card; that player loses life equal to the discarded
// card's mana value" -- losing 0 life instead of 2, 3 or 5 after the discard
// pick suspended the resolution. The pick and the RememberDiscarded$ rider
// both ran; what the resume lost was the grantor's SVar table. A granted
// trigger's body (and its SVar:X:Remembered$CardManaCost) lives on the
// GRANTOR's face, and resolveTop reads it from Engine.triggerLineSVars, but
// resumeResolution rebuilt the table from the recipient's face, where X is
// undefined.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const grantedDiscardDrainGrantor = "Name:Test Blades Grantor\nManaCost:2\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddTrigger$ CastTrig\n" +
	"SVar:CastTrig:Mode$ SpellCast | ValidCard$ Artifact | ValidActivatingPlayer$ You | Execute$ TrigDraw | TriggerZones$ Battlefield\n" +
	"SVar:TrigDraw:DB$ Draw | SubAbility$ DBDiscard\n" +
	"SVar:DBDiscard:DB$ Discard | Defined$ You | NumCards$ 1 | Mode$ TgtChoose | RememberDiscarded$ True | SubAbility$ DBLoseLife\n" +
	"SVar:DBLoseLife:DB$ LoseLife | Defined$ Opponent | LifeAmount$ X | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"SVar:X:Remembered$CardManaCost\nOracle:x\n"

// The recipient deliberately carries no X: the grantor's table must be read.
const grantedDiscardDrainRecipient = "Name:Test Blades Wearer\nManaCost:2\nTypes:Creature\nPT:1/1\nOracle:x\n"

const grantedDiscardDrainFodder = "Name:Test Three Drop\nManaCost:1 R R\nTypes:Creature\nPT:3/3\nOracle:x\n"

func TestGrantedTriggerKeepsGrantorSVarsAcrossDiscardAsk(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 7321, grantedDiscardDrainGrantor, grantedDiscardDrainRecipient,
		artifactSpellSrc, grantedDiscardDrainFodder)
	moveSeeded(t, e, 0, grantedDiscardDrainGrantor, state.ZBattlefield)
	moveSeeded(t, e, 0, grantedDiscardDrainRecipient, state.ZBattlefield)
	fodder := moveSeeded(t, e, 0, grantedDiscardDrainFodder, state.ZHand)
	moveSeeded(t, e, 0, artifactSpellSrc, state.ZHand)
	addMana(t, e, 0, "R")
	life := e.G.Players[1].Life
	castCardNow(t, e, "Test Trinket Spell")
	sawPick := false
	for i := 0; i < 40 && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while the stack resolves")
		}
		switch {
		case d.Kind == decision.KModes && d.ResumeKind == "discard":
			pick := -1
			for _, o := range d.Options {
				if o.Obj == fodder {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("the three-drop is not offered in the discard pick: %+v", d.Options)
			}
			sawPick = true
			submitChoices(t, e, pick)
		case d.Kind == decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			submitChoices(t, e, pass)
		default:
			submitChoices(t, e, 0)
		}
	}
	if !sawPick {
		t.Fatal("precondition: the granted trigger's discard never asked, so no suspension was exercised")
	}
	if z := e.G.Obj(fodder).Zone; z != state.ZGraveyard {
		t.Fatalf("the picked card zone = %s, want graveyard", z)
	}
	if got := life - e.G.Players[1].Life; got != 3 {
		t.Fatalf("opponent lost %d life, want the discarded card's mana value 3", got)
	}
	replayCheck(t, e, cfg)
}

// The Remembered member of the class, first half: an asking site that rides
// no ResumeRemembered of its own (ChooseColor here; 83 of the 120 asking
// sites in effects/) used to resume with the stack object's Remembered, so
// what the chain remembered BEFORE the ask was gone for the rest of it.
func TestChainRememberedSurvivesARideLessAsk(t *testing.T) {
	t.Parallel()
	src := "Name:Test Remember Then Ask\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Draw | NumCards$ 2 | RememberDrawn$ True | SubAbility$ DBColor\n" +
		"SVar:DBColor:DB$ ChooseColor | Defined$ You | SubAbility$ DBLose\n" +
		"SVar:DBLose:DB$ LoseLife | Defined$ Opponent | LifeAmount$ X\n" +
		"SVar:X:Remembered$Amount\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 7322, src)
	addMana(t, e, 0, "B")
	life := e.G.Players[1].Life
	d := castFixture(t, e, id, -1)
	if d == nil || d.ResumeKind != "choosecolor" {
		t.Fatalf("pending = %+v, want the mid-resolution ChooseColor ask", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := life - e.G.Players[1].Life; got != 2 {
		t.Fatalf("opponent lost %d life, want the two remembered draws (2)", got)
	}
	replayCheck(t, e, cfg)
}

// Second half: an enclosing loop that walked the SAME Ctx as the asking one
// (DB$ Branch resolves its arm in its caller's Ctx) continues, after the
// answered re-entry, with the Remembered that walk finished with -- not the
// stack object's rebuilt one.
func TestChainRememberedReachesTheEnclosingContinuation(t *testing.T) {
	t.Parallel()
	src := "Name:Test Remember Branch Ask\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Draw | NumCards$ 2 | RememberDrawn$ True | SubAbility$ DBBranch\n" +
		"SVar:DBBranch:DB$ Branch | BranchConditionSVar$ Y | BranchConditionSVarCompare$ GE1 | FalseSubAbility$ DBColor | TrueSubAbility$ DBColor | SubAbility$ DBLose\n" +
		"SVar:DBColor:DB$ ChooseColor | Defined$ You\n" +
		"SVar:DBLose:DB$ LoseLife | Defined$ Opponent | LifeAmount$ X\n" +
		"SVar:Y:Count$xPaid\nSVar:X:Remembered$Amount\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 7323, src)
	addMana(t, e, 0, "B")
	life := e.G.Players[1].Life
	d := castFixture(t, e, id, -1)
	if d == nil || d.ResumeKind != "choosecolor" {
		t.Fatalf("pending = %+v, want the mid-resolution ChooseColor ask", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := life - e.G.Players[1].Life; got != 2 {
		t.Fatalf("opponent lost %d life, want the two remembered draws (2)", got)
	}
	replayCheck(t, e, cfg)
}
