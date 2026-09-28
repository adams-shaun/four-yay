package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestMustBlockAggregateBudget keeps two distinct required creatures and
// attackers, each block individually payable but both together unaffordable.
// The fixture combines Watchdog's MustBlock and Qal Sisma Behemoth's printed
// CantBlockUnless shapes; flying makes the second attacker's pairing unique.
func TestMustBlockAggregateBudget(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	priced := strings.Replace(pricedMustBlockSrc, "Cost$ 3", "Cost$ 2", 1)
	ground := onBoardCard(t, e, 0, card(t, priced))
	flying := onBoardCard(t, e, 0, card(t, strings.Replace(priced, "PT:1/2\n", "PT:1/2\nK:Flying\n", 1)))
	bear := onBoardCard(t, e, 1, card(t, "Name:Test Ground Attacker\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	bird := onBoardCard(t, e, 1, card(t, "Name:Test Flying Attacker\nManaCost:1 U\nTypes:Creature Bird\nPT:2/2\nK:Flying\nOracle:x\n"))
	attackSeat0(t, e, bear, bird)
	floatMana(t, e, 0, "RR")

	// Both required blockers are on the battlefield; each has a distinct
	// legal attacker, and the sum {4} exceeds the available {2}.
	for _, b := range []state.ObjID{ground, flying} {
		if e.G.Obj(b).Zone != state.ZBattlefield {
			t.Fatalf("precondition: required blocker %d not on battlefield", b)
		}
	}
	if !e.canBlock(ground, bear) || e.canBlock(ground, bird) || !e.canBlock(flying, bird) {
		t.Fatalf("precondition: ground and flying pair abilities do not differ as expected")
	}
	if e.blockPairCharge(ground, bear).mana != 2 || e.blockPairCharge(flying, bird).mana != 2 || e.blockManaBudget(0) != 2 {
		t.Fatalf("precondition: pair prices or aggregate budget wrong")
	}
	d := askBlockersFresh(t, e)
	if d == nil || d.MaxSum != 2 {
		t.Fatalf("precondition: no {2}-budget block decision: %+v", d)
	}
	first, second := findBlockOption(d, ground, bear), findBlockOption(d, flying, bird)
	if first == nil || second == nil || first.Value != 2 || second.Value != 2 {
		t.Fatalf("precondition: distinct {2} pairs not offered: %+v", d.Options)
	}
	if first.Required && second.Required {
		t.Fatalf("both {2} pairs required under a {2} total budget: %+v", d.Options)
	}
	if !first.Required && !second.Required {
		t.Fatalf("neither individually affordable required block marked: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{first.Index}}); err != nil {
		t.Fatalf("affordable maximum declaration rejected: %v", err)
	}
	if !blockCommitted(t, e, bear, ground) {
		t.Fatalf("affordable block did not commit: %v", e.G.Obj(bear).BlockedBy)
	}
}

// TestMustBlockAggregateBudgetBotAnswerNeverLivelocks runs the actual bot
// answer through Submit under the same aggregate-budget constraint.
func TestMustBlockAggregateBudgetBotAnswerNeverLivelocks(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	priced := strings.Replace(pricedMustBlockSrc, "Cost$ 3", "Cost$ 2", 1)
	ground := onBoardCard(t, e, 0, card(t, priced))
	flying := onBoardCard(t, e, 0, card(t, strings.Replace(priced, "PT:1/2\n", "PT:1/2\nK:Flying\n", 1)))
	bear := onBoardCard(t, e, 1, card(t, "Name:Test Ground Attacker\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	bird := onBoardCard(t, e, 1, card(t, "Name:Test Flying Attacker\nManaCost:1 U\nTypes:Creature Bird\nPT:2/2\nK:Flying\nOracle:x\n"))
	attackSeat0(t, e, bear, bird)
	floatMana(t, e, 0, "RR")
	d := askBlockersFresh(t, e)
	if d == nil || d.MaxSum != 2 || findBlockOption(d, ground, bear) == nil || findBlockOption(d, flying, bird) == nil {
		t.Fatalf("precondition: missing two separately affordable required pairs: %+v", d)
	}
	bot := newTestBot(7)
	in := bot.answer(e, d)
	if err := e.Submit(in); err != nil {
		t.Fatalf("bot's own answer %v rejected (livelock): %v", in.Choices, err)
	}
	if e.Pending() == d {
		t.Fatal("bot answer did not consume blockers decision")
	}
}
