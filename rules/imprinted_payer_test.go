package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// UnlessPayer$ ImprintedController on the corpus's two RepeatEach carriers.
// Forge's UseImprinted$ binds each iteration's current subject as
// "Imprinted"; the engine binds it on the iteration context (Ctx.RepeatSubject)
// and carries it through a resumed ask.

// TestImprintedControllerHeroism drives Heroism's real activation ("For each
// attacking red creature, prevent all combat damage that would be dealt by
// that creature this turn unless its controller pays {2}{R}."): after the
// white-creature sacrifice cost is paid, the loop's iteration over the
// attacking red creature offers the {2}{R} to THAT CREATURE'S controller
// (seat 1), not to Heroism's controller — the payer the ImprintedController
// selector names. Paying charges it; the decline branch keeps their pool.
func TestImprintedControllerHeroism(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		pay       bool
		wantRed   int32
		wantColor int32
	}{
		{"pays", true, 0, 0},
		{"declines", false, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			e := stealEngine(t, 742)
			onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Heroism"))
			knight := onBoard(t, e, 0, "Name:Knight\nTypes:Creature\nManaCost:W\nPT:2/2\nOracle:x\n")
			goblin := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Goblin Piledriver"))
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{goblin}})
			addMana(t, e, 1, "CCR") // the payer's {2}{R}, for the pay branch
			e.askPriority(0)
			submitChoices(t, e, abilityOption(t, e, heroismID(t, e), 0).Index)
			// Pay the Sac<1/Creature.White> cost by burying the Knight.
			answerSacrifice(t, e, "Knight")
			if e.G.Obj(knight).Zone != state.ZGraveyard {
				t.Fatalf("knight zone = %v, want sacrificed as the activation cost", e.G.Obj(knight).Zone)
			}
			// The iteration's unless-pay ask goes to the goblin's controller,
			// once the priority rounds let the ability resolve.
			d := passUntilNonPriority(t, e, 8)
			if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
				t.Fatalf("pending = %+v, want the ImprintedController unless-pay ask for seat 1", d)
			}
			answerUnlessPay(t, e, tc.pay)
			if got := e.G.Players[1].Pool[state.MR]; got != tc.wantRed {
				t.Fatalf("seat 1 red pool = %d, want %d", got, tc.wantRed)
			}
			if got := e.G.Players[1].Pool[state.MC]; got != tc.wantColor {
				t.Fatalf("seat 1 colourless pool = %d, want %d", got, tc.wantColor)
			}
		})
	}
}

// heroismID finds the Heroism enchantment on seat 0's battlefield.
func heroismID(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Heroism" {
			return id
		}
	}
	t.Fatal("Heroism not on seat 0's battlefield")
	return 0
}

// TestImprintedControllerStenchOfEvil drives Stench of Evil's real damage
// chain ("Destroy all Plains. For each land destroyed this way, Stench of
// Evil deals 1 damage to that land's controller unless they pay {2}."): the
// RepeatEach over the destroyed plains binds each as Imprinted, and DBDmg's
// Defined$ ImprintedController names the destroyed land's controller — the
// same player the UnlessPayer$ asks.
func TestImprintedControllerStenchOfEvil(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		pay        bool
		wantPool   int32
		wantDamage bool
	}{
		{"pays", true, 0, false},
		{"declines", false, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			e := handEngine(t, mustCorpusCard(t, reg, "Stench of Evil"))
			plain := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Plains"))
			addMana(t, e, 0, "BBBB")
			addMana(t, e, 1, "CC") // the payer's {2}, for the pay branch
			e.askPriority(0)
			submitChoices(t, e, passToCast(t, e, handIDsByFace(e)["Stench of Evil"]))
			d := passUntilNonPriority(t, e, 8)
			if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
				t.Fatalf("pending = %+v, want the destroyed plains' controller unless-pay ask for seat 1", d)
			}
			life := e.G.Players[1].Life
			wantLife := life
			if tc.wantDamage {
				wantLife = life - 1
			}
			answerUnlessPay(t, e, tc.pay)
			if e.G.Obj(plain).Zone != state.ZGraveyard {
				t.Fatalf("plains zone = %v, want destroyed", e.G.Obj(plain).Zone)
			}
			if got := e.G.Players[1].Life; got != wantLife {
				t.Fatalf("seat 1 life = %d, want %d", got, wantLife)
			}
			if got := e.G.Players[1].Pool[state.MC]; got != tc.wantPool {
				t.Fatalf("seat 1 colourless pool = %d, want %d", got, tc.wantPool)
			}
		})
	}
}

// answerSacrificeNeedsKChoose guards the assumption the heroism test leans
// on: the Sac<1/Creature.White> activation cost is a real KChoose when two
// white creatures exist, so the cost cannot silently take the first.
func TestImprintedControllerHeroismCostIsAChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 742)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Heroism"))
	onBoard(t, e, 0, "Name:Knight\nTypes:Creature\nManaCost:W\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Cleric\nTypes:Creature\nManaCost:W\nPT:1/1\nOracle:x\n")
	goblin := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Goblin Piledriver"))
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{goblin}})
	e.askPriority(0)
	submitChoices(t, e, abilityOption(t, e, heroismID(t, e), 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want the two-white-creature sacrifice cost ask", d)
	}
}
