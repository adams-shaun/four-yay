package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// Whiskervale Forerunner's shape (cardfuzz dual run, seed 11101, game
// 12242075152789232691): a ChooseCard binding, an Optional$ fetch of the
// ChosenCard that asks, then subs whose Condition* gate reads the chosen
// card through the Card.ChosenCard predicate. The legacy re-entry after the
// optional confirm rebuilt its Ctx without the chain's chosen binding, so
// Card.ChosenCard failed closed, the "not on the battlefield" gate passed
// and the "it went to hand" leg ran beside the battlefield move. The answer
// must resume with the chosen binding the first pass held, as the kernel's
// re-execution does.
const legacyChosenAcrossAskSrc = "Name:Tape Forerunner Peek\nManaCost:W\nTypes:Sorcery\n" +
	"A:SP$ PeekAndReveal | Defined$ You | PeekAmount$ 3 | NoReveal$ True | RememberPeeked$ True | SubAbility$ PickOne\n" +
	"SVar:PickOne:DB$ ChooseCard | Defined$ You | Amount$ 1 | Choices$ Card.IsRemembered | ChoiceZone$ Library | SubAbility$ ToField\n" +
	"SVar:ToField:DB$ ChangeZone | Origin$ Library | Destination$ Battlefield | Defined$ ChosenCard | Optional$ True | SubAbility$ GainOff\n" +
	"SVar:GainOff:DB$ GainLife | LifeAmount$ 3 | ConditionDefined$ Remembered | ConditionPresent$ Card.ChosenCard+inZoneBattlefield | ConditionCompare$ EQ0 | SubAbility$ GainOn\n" +
	"SVar:GainOn:DB$ GainLife | LifeAmount$ 1 | ConditionDefined$ Remembered | ConditionPresent$ Card.ChosenCard+inZoneBattlefield | ConditionCompare$ EQ1 | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearChosenCard$ True | ClearRemembered$ True\nOracle:x\n"

// legacyChosenScenario casts the fixture and accepts the optional fetch.
func legacyChosenScenario(t *testing.T, e *Engine) {
	t.Helper()
	addMana(t, e, 0, "W")
	id := fixtureInHand(t, e, "Tape Forerunner Peek")
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 50; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		pick := []int{0}
		if d.Min > 1 {
			pick = tapePick(d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: pick}); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
}

func TestLegacyResumeKeepsChosenBindingAcrossAsk(t *testing.T) {
	for _, tape := range []bool{false, true} {
		e, _ := tapeFixture(t, 2, 33417, tape, legacyChosenAcrossAskSrc)
		life := e.G.Players[0].Life
		legacyChosenScenario(t, e)
		if got := e.G.Players[0].Life - life; got != 1 {
			t.Fatalf("tape=%v: the chosen card went to the battlefield, so only the EQ1 leg gains (1); gained %d", tape, got)
		}
	}
	tapeDual(t, 2, 33417, legacyChosenScenario, legacyChosenAcrossAskSrc)
}
