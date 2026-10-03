package rules

// W3 step 2a(choose): the dual-run tests for the choose family's converted
// ask sites (effects/choose.go, choose_control.go, choose_direction.go,
// changetext.go, changecombatants.go, life.go, attach.go, clone.go, copy.go,
// copypermanent.go, cipher.go). Each scenario resolves on legacy and on the
// tape kernel and must stay byte-identical with its asks served from the
// tape (tapeConverted / tapeDual).

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// tapeChooseCase is one synthetic sorcery, the fixture permanents seat 0
// puts onto the battlefield first, and the minimum tape-served answers.
type tapeChooseCase struct {
	name, src string
	seats     int
	board     []string
	served    int64
}

func tapeChooseRun(t *testing.T, seed uint64, tc tapeChooseCase) {
	t.Helper()
	src := "Name:" + tc.name + "\nManaCost:U\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
	srcs := []string{src, ptResumeBearSrc, ptResumeAngelSrc}
	seats := tc.seats
	if seats == 0 {
		seats = 2
	}
	_, st := tapeDual(t, seats, seed, func(t *testing.T, e *Engine) {
		for _, n := range tc.board {
			moveByName(t, e, 0, n, state.ZBattlefield)
		}
		tapeCastAndResolve(t, e, tc.name, "U")
	}, srcs...)
	if st.Served < tc.served || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the converted asks were not served from the tape: %+v", tc.name, st)
	}
}

const tapeGainSVar = "SVar:DBGain:DB$ GainLife | LifeAmount$ 2"

var bothCreatures = []string{"ParentLink Bear", "ParentLink Angel"}

func TestTapeConvertChooseValues(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Even Odd", src: "A:SP$ ChooseEvenOdd | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Number", src: "A:SP$ ChooseNumber | Max$ 5 | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Type", src: "A:SP$ ChooseType | Type$ Creature | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Card Type", src: "A:SP$ ChooseType | Type$ Card | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Direction", src: "A:SP$ ChooseDirection | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Safe", seats: 3,
			src: "A:SP$ ChooseNumber | ValidTgts$ Opponent | Defined$ TargetedAndYou | Min$ 1 | Max$ 3 | Secretly$ True | MatchedAbility$ DBGainA | UnmatchedAbility$ DBGainB\n" +
				"SVar:DBGainA:DB$ GainLife | LifeAmount$ 1\nSVar:DBGainB:DB$ GainLife | LifeAmount$ 3", served: 2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13000+uint64(i), tc) })
	}
}

func TestTapeConvertChoice(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Choose Card", board: bothCreatures,
			src: "A:SP$ ChooseCard | Choices$ Creature | Mandatory$ True | SubAbility$ DBPump\n" +
				"SVar:DBPump:DB$ Pump | Defined$ ChosenCard | NumAtt$ 2", served: 1},
		{name: "Tape Each Chooses Card", seats: 3, board: bothCreatures,
			src: "A:SP$ ChooseCard | Defined$ Player | Choices$ Creature | AllCards$ True | Amount$ 1 | RememberChosen$ True | SubAbility$ DBGain\n" + tapeGainSVar, served: 3},
		{name: "Tape Choose Player", seats: 3,
			src: "A:SP$ ChoosePlayer | Defined$ You | Choices$ Player | SubAbility$ DBGain\n" +
				"SVar:DBGain:DB$ GainLife | LifeAmount$ 2 | Defined$ ChosenPlayer", served: 1},
		{name: "Tape Each Chooses Player", seats: 3,
			src: "A:SP$ ChoosePlayer | Defined$ Player | Choices$ Player.Opponent | SubAbility$ DBGain\n" + tapeGainSVar, served: 3},
		{name: "Tape Choose Source", board: bothCreatures,
			src: "A:SP$ ChooseSource | Choices$ Card | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Gain Choice", board: bothCreatures,
			src: "A:SP$ GainControl | Choices$ Creature | NewController$ You | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Redistribute", seats: 3,
			src: "A:SP$ SetLife | PlayerChoices$ Player | ChoiceAmount$ Any | ChoicePrompt$ Choose players | Redistribute$ True", served: 2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13100+uint64(i), tc) })
	}
}

func TestTapeConvertAttachCloneCopy(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Clone Choice", board: bothCreatures,
			src: "A:SP$ Clone | Choices$ Creature | ValidTgts$ Creature | TgtPrompt$ Select target creature | Duration$ UntilEndOfTurn", served: 1},
		{name: "Tape Clone Maybe", board: bothCreatures,
			src: "A:SP$ Clone | Choices$ Creature | ChoiceOptional$ True | ValidTgts$ Creature | Optional$ True | Duration$ UntilEndOfTurn", served: 1},
		{name: "Tape Change Text", board: bothCreatures,
			src: "A:SP$ ChangeText | ValidTgts$ Creature | ChangeTypeWord$ ChooseCreatureType ChooseCreatureType | Duration$ Permanent", served: 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13200+uint64(i), tc) })
	}
}
