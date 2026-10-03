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

// Shrouded Lore's shape (cardfuzz dual run, seed 11101, census game
// 177156192444407068, exposed once its UnlessCost$ election was served
// from the tape): a ChooseCard recorded without an answered re-entry (here
// AtRandom$) left its pick in Ctx.Choice, the ANSWER channel, so the next
// ChooseCard on the same Ctx read a stale answer at its entry, skipped
// Forge's setChosenCards replacement and accumulated: Defined$ ChosenCard
// then named every card chosen so far instead of the last choice's.
const legacyStaleChoiceSrc = "Name:Tape Twice Chosen\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ ChooseCard | Defined$ You | Amount$ 1 | AtRandom$ True | Choices$ Land.YouOwn | ChoiceZone$ Library | SubAbility$ PickAgain\n" +
	"SVar:PickAgain:DB$ ChooseCard | Defined$ You | Amount$ 1 | AtRandom$ True | Choices$ Land.YouOwn | ChoiceZone$ Library | SubAbility$ Fetch\n" +
	"SVar:Fetch:DB$ ChangeZone | Defined$ ChosenCard | Origin$ Library | Destination$ Hand\nOracle:x\n"

// Rhystic Circle's shape (cardfuzz -tape, seed 11101, game
// 281516253186263535): an UnlessCost$ with several payers in front of a
// choosing body. The legacy re-entry rebuilt its Ctx with ChoiceTarget set
// to the answered election's ResumeTarget -- the PAYER index -- so after the
// second payer declined the body's own chooser walk started at chooser 1 of
// 1 and chose nothing. The payer cursor belongs to the unless gate
// (Ctx.UnlessNext); the body starts its own walk at zero.
const legacyUnlessCursorSrc = "Name:Tape Rhystic Pick\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ ChooseCard | Defined$ You | Amount$ 1 | Choices$ Land.YouOwn | ChoiceZone$ Library | UnlessCost$ 1 | UnlessPayer$ Player | SubAbility$ Fetch\n" +
	"SVar:Fetch:DB$ ChangeZone | Defined$ ChosenCard | Origin$ Library | Destination$ Hand\nOracle:x\n"
