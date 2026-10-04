package rules

// Kernel-era restorations of the W3 step 2a(choose) dual-run tests. The
// legacy arm is gone; each scenario now resolves once on the resolution
// kernel, must pose its named ask, must have its answers served from the
// tape, and must replay byte-identically (replayCheck).

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// kr7Setup is one kernel scenario's table: seat 0's deck opens with seat0
// (moved to hand), seat 1's with seat1 (left where the deal put them), every
// other card a Mountain, and the token table tokens.
type kr7Setup struct {
	seats        int
	seed         uint64
	seat0, seat1 []*cards.Card
	tokens       map[string]*cards.Card
}

func kr7Build(t *testing.T, s kr7Setup) (*Engine, Config) {
	t.Helper()
	names := make([]string, s.seats)
	decks := make([][]*cards.Card, s.seats)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
		decks[i] = mountainDeck(t, 40)
	}
	decks[0] = append(append([]*cards.Card(nil), s.seat0...), mountainDeck(t, 40-len(s.seat0))...)
	if s.seats > 1 && len(s.seat1) > 0 {
		decks[1] = append(append([]*cards.Card(nil), s.seat1...), mountainDeck(t, 40-len(s.seat1))...)
	}
	tokens := s.tokens
	if tokens == nil {
		tokens = map[string]*cards.Card{}
	}
	cfg := seatZeroStart(Config{Seed: s.seed, Names: names, Decks: decks, Tokens: tokens})
	e := New(cfg)
	e.Advance()
	for _, f := range s.seat0 {
		name := f.Faces[0].Name
		inHand := false
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if o := e.G.Obj(id); o.Face().Name == name {
				inHand = true
			}
		}
		if !inHand {
			moveByName(t, e, 0, name, state.ZHand)
		}
	}
	return e, cfg
}

// kr7Fixture is the kernel-only tapeFixture: seat 0 holds the given
// fixture cards in hand over a Mountain deck.
func kr7Fixture(t *testing.T, seats int, seed uint64, srcs ...string) (*Engine, Config) {
	t.Helper()
	return kr7Build(t, kr7Setup{seats: seats, seed: seed, seat0: ctrCards(t, srcs...)})
}

// kr7Run builds s, runs scenario on the kernel, requires the replay to
// match, and returns the kernel counters the run moved.
func kr7Run(t *testing.T, s kr7Setup, scenario func(t *testing.T, e *Engine)) (*Engine, resolve.Stats) {
	t.Helper()
	e, cfg := kr7Build(t, s)
	before := resolve.ReadStats()
	scenario(t, e)
	st := resolve.ReadStats().Sub(before)
	replayCheck(t, e, cfg)
	t.Logf("kernel stats: %+v", st)
	return e, st
}

// kr7Dual is the kernel-only tapeDual (the name is kept from the dual-run
// era's call sites; there is one run now).
func kr7Dual(t *testing.T, seats int, seed uint64, scenario func(t *testing.T, e *Engine), srcs ...string) (*Engine, resolve.Stats) {
	t.Helper()
	return kr7Run(t, kr7Setup{seats: seats, seed: seed, seat0: ctrCards(t, srcs...)}, scenario)
}

func kr7DualTokens(t *testing.T, seats int, seed uint64, tokens map[string]*cards.Card, scenario func(t *testing.T, e *Engine), srcs ...string) (*Engine, resolve.Stats) {
	t.Helper()
	return kr7Run(t, kr7Setup{seats: seats, seed: seed, seat0: ctrCards(t, srcs...), tokens: tokens}, scenario)
}

func kr7Converted(t *testing.T, seats int, seed uint64, name, mana string, minServed int64, srcs ...string) resolve.Stats {
	t.Helper()
	_, st := kr7Dual(t, seats, seed, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, name, mana)
	}, srcs...)
	if st.Served < minServed || st.LegacySwitch != 0 || st.Aborts != 0 {
		t.Fatalf("%s: the asks were not served from the tape: %+v", name, st)
	}
	return st
}

func kr7ChooseRun(t *testing.T, seed uint64, tc tapeChooseCase) {
	t.Helper()
	kr7ChooseRunWith(t, seed, tc)
}

func kr7ChooseRunWith(t *testing.T, seed uint64, tc tapeChooseCase, extra ...string) {
	t.Helper()
	src := "Name:" + tc.name + "\nManaCost:U\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
	srcs := append([]string{src, ptResumeBearSrc, ptResumeAngelSrc}, extra...)
	seats := tc.seats
	if seats == 0 {
		seats = 2
	}
	var kinds []string
	_, st := kr7Dual(t, seats, seed, func(t *testing.T, e *Engine) {
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

func TestKernelAskChooseValues(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) { kr7ChooseRun(t, 13000+uint64(i), tc) })
	}
}

func TestKernelAskChoice(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) { kr7ChooseRun(t, 13100+uint64(i), tc) })
	}
}

func TestKernelAskAttachCloneCopy(t *testing.T) {
	cases := []tapeChooseCase{
		{name: "Tape Clone Choice", kind: "clone_choice", board: bothCreatures,
			src: "A:SP$ Clone | Choices$ Creature | ValidTgts$ Creature | TgtPrompt$ Select target creature | Duration$ UntilEndOfTurn", served: 1},
		{name: "Tape Clone Maybe", kind: "clone_choice", board: bothCreatures,
			src: "A:SP$ Clone | Choices$ Creature | ChoiceOptional$ True | ValidTgts$ Creature | Optional$ True | Duration$ UntilEndOfTurn", served: 1},
		{name: "Tape Change Text", kind: "changetext", board: bothCreatures,
			src: "A:SP$ ChangeText | ValidTgts$ Creature | ChangeTypeWord$ ChooseCreatureType ChooseCreatureType | Duration$ Permanent", served: 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { kr7ChooseRun(t, 13200+uint64(i), tc) })
	}
}

func TestKernelAskAttach(t *testing.T) {
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
			kr7ChooseRunWith(t, 13300+uint64(i), tc, tapeSwordSrc, tapeAxeSrc)
		})
	}
}

func TestKernelAskCopyCipherVariant(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) { kr7ChooseRun(t, 13400+uint64(i), tc) })
	}
}

// A ChangeTargets redirect ("choice" via effChangeTargets): a targeted
// instant on the stack, then the redirect cast over it.
func TestKernelAskChangeTargets(t *testing.T) {
	var kinds []string
	_, st := kr7Dual(t, 2, 13501, func(t *testing.T, e *Engine) {
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

// A permanent spell's own Moved replacement asks while the spell is still on
// the stack (Pendant of Prosperity): the body moves it off the stack, and the
// resolution's completion must still return priority to the active player
// with the passes reset (CR 117.3b) -- on the legacy resume as on the tape.
func TestKernelOwnMovedReplacementPriorityReset(t *testing.T) {
	{
		e, cfg := kr7Fixture(t, 3, 13601, tapePendantSrc)
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
			t.Fatalf("after the replacement-body ask the resolution must end with priority to the active player (asked=%v, pending %+v, active %d)", asked, d, e.G.Active)
		}
		replayCheck(t, e, cfg)
	}
	kr7Dual(t, 3, 13601, func(t *testing.T, e *Engine) { tapeCastAndResolve(t, e, "Tape Pendant", "U") }, tapePendantSrc)
}

// kr7Settle grants the priority window a probe-resolved resolution leaves
// unposted: a kernel probe (a test's direct resolveTop or e.probe) re-runs
// only its own function when its ask is answered, so the completion's
// priority grant -- and the state-based actions checked before it -- that a
// Submit-driven resolution performs is the test's to trigger.
func kr7Settle(e *Engine) {
	if e.Pending() == nil && !e.G.Over {
		e.priorityRound()
	}
}
