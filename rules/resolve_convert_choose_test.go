package rules

// W3 step 2a(choose): the dual-run tests for the choose family's converted
// ask sites (effects/choose.go, choose_control.go, choose_direction.go,
// changetext.go, changecombatants.go, life.go, attach.go, clone.go, copy.go,
// copypermanent.go, cipher.go). Each scenario resolves on legacy and on the
// tape kernel and must stay byte-identical with its asks served from the
// tape (tapeConverted / tapeDual).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// tapeChooseCase is one synthetic sorcery, the fixture permanents seat 0
// puts onto the battlefield first, and the minimum tape-served answers.
type tapeChooseCase struct {
	name, src string
	seats     int
	board     []string
	served    int64
	kind      string // the ResumeKind the scenario must pose during resolution
}

// tapeChooseCastResolve is tapeCastAndResolve recording the ResumeKind of
// every decision posed while the stack resolves.
func tapeChooseCastResolve(t *testing.T, e *Engine, name string) []string {
	t.Helper()
	addMana(t, e, 0, "U")
	id := fixtureInHand(t, e, name)
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	var kinds []string
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return kinds
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return kinds
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		if d.ResumeKind != "" {
			kinds = append(kinds, d.ResumeKind)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("submit %s %v: %v", d.Kind, tapePick(d), err)
		}
	}
	t.Fatal("stack never drained")
	return nil
}

const tapeGainSVar = "SVar:DBGain:DB$ GainLife | LifeAmount$ 2"

var bothCreatures = []string{"ParentLink Bear", "ParentLink Angel"}

const tapeSwordSrc = "Name:Tape Sword\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n"

const tapeAxeSrc = "Name:Tape Axe\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n"

const tapeBoltSrc = "Name:Tape Bolt\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ 1\nOracle:x\n"

const tapeRedirectSrc = "Name:Tape Redirect\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ ChangeTargets | TargetType$ Spell | ValidTgts$ Card | TgtPrompt$ Select target spell\nOracle:x\n"

const tapePendantSrc = "Name:Tape Pendant\nManaCost:U\nTypes:Artifact\n" +
	"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBChooseOpp | Layer$ Control | Description$ x\n" +
	"SVar:DBChooseOpp:DB$ ChoosePlayer | Defined$ You | Choices$ Player.Opponent | SubAbility$ MoveToPlay\n" +
	"SVar:MoveToPlay:DB$ ChangeZone | Hidden$ True | Origin$ All | Destination$ Battlefield | Defined$ ReplacedCard | GainControl$ ChosenPlayer | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearChosenPlayer$ True\nOracle:x\n"
