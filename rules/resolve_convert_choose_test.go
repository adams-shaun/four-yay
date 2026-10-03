package rules

// W3 step 2a(choose): the dual-run tests for the choose family's converted
// ask sites (effects/choose.go, choose_control.go, choose_direction.go,
// changetext.go, changecombatants.go, life.go, attach.go, clone.go, copy.go,
// copypermanent.go, cipher.go). Each scenario resolves on legacy and on the
// tape kernel and must stay byte-identical with its asks served from the
// tape (tapeConverted / tapeDual).

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
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

func tapeChooseRun(t *testing.T, seed uint64, tc tapeChooseCase) {
	t.Helper()
	tapeChooseRunWith(t, seed, tc)
}

func tapeChooseRunWith(t *testing.T, seed uint64, tc tapeChooseCase, extra ...string) {
	t.Helper()
	src := "Name:" + tc.name + "\nManaCost:U\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
	srcs := append([]string{src, ptResumeBearSrc, ptResumeAngelSrc}, extra...)
	seats := tc.seats
	if seats == 0 {
		seats = 2
	}
	var kinds []string
	_, st := tapeDual(t, seats, seed, func(t *testing.T, e *Engine) {
		for _, n := range tc.board {
			moveByName(t, e, 0, n, state.ZBattlefield)
		}
		kinds = tapeChooseCastResolve(t, e, tc.name)
	}, srcs...)
	if st.Served < tc.served || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the converted asks were not served from the tape: %+v", tc.name, st)
	}
	if tc.kind != "" && !slices.Contains(kinds, tc.kind) {
		t.Fatalf("%s: no %q ask was posed (posed %v)", tc.name, tc.kind, kinds)
	}
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

func TestTapeConvertChooseValues(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Even Odd", kind: "chooseevenodd", src: "A:SP$ ChooseEvenOdd | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Number", kind: "choosenumber", src: "A:SP$ ChooseNumber | Max$ 5 | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Type", kind: "choosetype", src: "A:SP$ ChooseType | Type$ Creature | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Card Type", kind: "choosetype", src: "A:SP$ ChooseType | Type$ Card | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Direction", kind: "choosedirection", src: "A:SP$ ChooseDirection | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Safe", kind: "choosenumbermulti", seats: 3,
			src: "A:SP$ ChooseNumber | ValidTgts$ Opponent | Defined$ TargetedAndYou | Min$ 1 | Max$ 3 | Secretly$ True | MatchedAbility$ DBGainA | UnmatchedAbility$ DBGainB\n" +
				"SVar:DBGainA:DB$ GainLife | LifeAmount$ 1\nSVar:DBGainB:DB$ GainLife | LifeAmount$ 3", served: 2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13000+uint64(i), tc) })
	}
}

func TestTapeConvertChoice(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Choose Card", kind: "choice", board: bothCreatures,
			src: "A:SP$ ChooseCard | Choices$ Creature | Mandatory$ True | SubAbility$ DBPump\n" +
				"SVar:DBPump:DB$ Pump | Defined$ ChosenCard | NumAtt$ 2", served: 1},
		{name: "Tape Each Chooses Card", kind: "choice", seats: 3, board: bothCreatures,
			src: "A:SP$ ChooseCard | Defined$ Player | Choices$ Creature | AllCards$ True | Amount$ 1 | RememberChosen$ True | SubAbility$ DBGain\n" + tapeGainSVar, served: 3},
		{name: "Tape Choose Player", kind: "choice", seats: 3,
			src: "A:SP$ ChoosePlayer | Defined$ You | Choices$ Player | SubAbility$ DBGain\n" +
				"SVar:DBGain:DB$ GainLife | LifeAmount$ 2 | Defined$ ChosenPlayer", served: 1},
		{name: "Tape Each Chooses Player", kind: "choice", seats: 3,
			src: "A:SP$ ChoosePlayer | Defined$ Player | Choices$ Player.Opponent | SubAbility$ DBGain\n" + tapeGainSVar, served: 3},
		{name: "Tape Choose Source", kind: "choice", board: bothCreatures,
			src: "A:SP$ ChooseSource | Choices$ Card | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Gain Choice", kind: "choice", board: bothCreatures,
			src: "A:SP$ GainControl | Choices$ Creature | NewController$ You | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Redistribute", kind: "choice", seats: 3,
			src: "A:SP$ SetLife | PlayerChoices$ Player | ChoiceAmount$ Any | ChoicePrompt$ Choose players | Redistribute$ True", served: 2},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13100+uint64(i), tc) })
	}
}

func TestTapeConvertAttachCloneCopy(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Clone Choice", kind: "clone_choice", board: bothCreatures,
			src: "A:SP$ Clone | Choices$ Creature | ValidTgts$ Creature | TgtPrompt$ Select target creature | Duration$ UntilEndOfTurn", served: 1},
		{name: "Tape Clone Maybe", kind: "clone_choice", board: bothCreatures,
			src: "A:SP$ Clone | Choices$ Creature | ChoiceOptional$ True | ValidTgts$ Creature | Optional$ True | Duration$ UntilEndOfTurn", served: 1},
		{name: "Tape Change Text", kind: "changetext", board: bothCreatures,
			src: "A:SP$ ChangeText | ValidTgts$ Creature | ChangeTypeWord$ ChooseCreatureType ChooseCreatureType | Duration$ Permanent", served: 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13200+uint64(i), tc) })
	}
}

const tapeSwordSrc = "Name:Tape Sword\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n"

const tapeAxeSrc = "Name:Tape Axe\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n"

func TestTapeConvertAttach(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Attach Player", kind: "attach_player_choice", seats: 3,
			src: "A:SP$ Attach | Object$ Valid Equipment.YouCtrl | PlayerChoices$ Player", board: []string{"Tape Sword"}, served: 1},
		{name: "Tape Attach Dest", kind: "attach_choice", board: append([]string{"Tape Sword"}, bothCreatures...),
			src: "A:SP$ Attach | Object$ Valid Equipment.YouCtrl | Choices$ Creature.YouCtrl | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Attach Object", kind: "attach_choice", board: append([]string{"Tape Sword", "Tape Axe"}, bothCreatures...),
			src: "A:SP$ Attach | Optional$ True | Choices$ Equipment.YouCtrl | Defined$ Valid Creature.YouCtrl+namedParentLink Bear | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
		{name: "Tape Attach Maybe", kind: "attach_optional", board: append([]string{"Tape Sword"}, bothCreatures...),
			src: "A:SP$ Attach | Optional$ True | Object$ Valid Equipment.YouCtrl | Defined$ Valid Creature.YouCtrl | SubAbility$ DBGain\n" + tapeGainSVar, served: 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.board = append([]string(nil), tc.board...)
			tapeChooseRunWith(t, 13300+uint64(i), tc, tapeSwordSrc, tapeAxeSrc)
		})
	}
}

func TestTapeConvertCopyCipherVariant(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Copy Maybe", kind: "copy_optional",
			src: "A:SP$ GainLife | LifeAmount$ 1 | SubAbility$ DBCopy\nSVar:DBCopy:DB$ CopySpellAbility | Defined$ Parent | Optional$ True", served: 1},
		{name: "Tape Cipher", kind: "cipher", board: bothCreatures,
			src: "A:SP$ GainLife | LifeAmount$ 2\nK:Cipher", served: 1},
		{name: "Tape Inniaz", kind: "choice", board: bothCreatures,
			src: "A:SP$ GainControlVariant | AllValid$ Creature | ChangeController$ ChooseFromPlayerToTheirRight", served: 1},
		{name: "Tape Succession", kind: "choice", seats: 3, board: bothCreatures,
			src: "A:SP$ ChooseDirection | SubAbility$ DBGainControl\nSVar:DBGainControl:DB$ GainControlVariant | AllValid$ Creature | ChangeController$ ChooseNextPlayerInChosenDirection", served: 2},
		{name: "Tape Clone Elect", kind: "clone", board: bothCreatures,
			src: "A:SP$ Clone | Defined$ Valid Creature.namedParentLink Angel | CloneTarget$ Valid Creature.namedParentLink Bear | Optional$ True", served: 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { tapeChooseRun(t, 13400+uint64(i), tc) })
	}
}

const tapeBoltSrc = "Name:Tape Bolt\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ 1\nOracle:x\n"

const tapeRedirectSrc = "Name:Tape Redirect\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ ChangeTargets | TargetType$ Spell | ValidTgts$ Card | TgtPrompt$ Select target spell\nOracle:x\n"

// A ChangeTargets redirect ("choice" via effChangeTargets): a targeted
// instant on the stack, then the redirect cast over it.
func TestTapeConvertChangeTargets(t *testing.T) {
	var kinds []string
	_, st := tapeDual(t, 2, 13501, func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
		moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
		addMana(t, e, 0, "RU")
		for _, name := range []string{"Tape Bolt", "Tape Redirect"} {
			submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, name)).Index)
			for i := 0; i < 20; i++ {
				d := e.Pending()
				if d == nil || d.Kind == decision.KPriority {
					break
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
					t.Fatalf("cast-time %s: %v", d.Kind, err)
				}
			}
		}
		kinds = nil
		for i := 0; i < 400; i++ {
			d := e.Pending()
			if d == nil || e.G.Over || (d.Kind == decision.KPriority && len(e.G.Stack) == 0) {
				break
			}
			if d.Kind == decision.KPriority {
				submitChoices(t, e, tapePassIndex(d))
				continue
			}
			kinds = append(kinds, d.ResumeKind)
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
	}, tapeBoltSrc, tapeRedirectSrc, ptResumeBearSrc, ptResumeAngelSrc)
	if st.Served < 1 || st.LegacySwitch != 0 || st.Aborts != 0 || !slices.Contains(kinds, "choice") {
		t.Fatalf("the redirect ask was not served from the tape: %+v (posed %v)", st, kinds)
	}
}

const tapePendantSrc = "Name:Tape Pendant\nManaCost:U\nTypes:Artifact\n" +
	"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBChooseOpp | Layer$ Control | Description$ x\n" +
	"SVar:DBChooseOpp:DB$ ChoosePlayer | Defined$ You | Choices$ Player.Opponent | SubAbility$ MoveToPlay\n" +
	"SVar:MoveToPlay:DB$ ChangeZone | Hidden$ True | Origin$ All | Destination$ Battlefield | Defined$ ReplacedCard | GainControl$ ChosenPlayer | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearChosenPlayer$ True\nOracle:x\n"

// A permanent spell's own Moved replacement asks while the spell is still on
// the stack (Pendant of Prosperity): the body moves it off the stack, and the
// resolution's completion must still return priority to the active player
// with the passes reset (CR 117.3b) -- on the legacy resume as on the tape.
func TestTapeOwnMovedReplacementPriorityReset(t *testing.T) {
	for _, tape := range []bool{false, true} {
		e, _ := tapeFixture(t, 3, 13601, tape, tapePendantSrc)
		addMana(t, e, 0, "U")
		submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Pendant")).Index)
		asked := false
		for i := 0; i < 50; i++ {
			d := e.Pending()
			if d == nil {
				t.Fatal("no decision")
			}
			if d.Kind == decision.KPriority {
				if len(e.G.Stack) == 0 {
					break
				}
				submitChoices(t, e, tapePassIndex(d))
				continue
			}
			asked = asked || d.ResumeKind == "choice"
			submitChoices(t, e, tapePick(d)...)
		}
		d := e.Pending()
		if !asked || d == nil || d.Kind != decision.KPriority || d.Player != e.G.Active || len(e.G.Stack) != 0 {
			t.Fatalf("tape=%v: after the replacement-body ask the resolution must end with priority to the active player (asked=%v, pending %+v, active %d)", tape, asked, d, e.G.Active)
		}
	}
	tapeDual(t, 3, 13601, func(t *testing.T, e *Engine) { tapeCastAndResolve(t, e, "Tape Pendant", "U") }, tapePendantSrc)
}
