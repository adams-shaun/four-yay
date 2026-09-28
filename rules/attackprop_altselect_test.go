// The attack/block prop payment windows must resolve the EXACT alternative a
// tap answer selected. Round 1 widened the window to one option per priceable
// ability but identified every option only by its Obj: a permanent with two
// free abilities producing DIFFERENT quantities (one {C}, one {C}{C}) had its
// budget counted at the maximum (2), a {2} charge was offered, and selecting
// the second option still activated the FIRST ability -- the payer came up
// short and the declaration aborted. The window now stores the posed tap list
// and resolves the chosen option's Index, and each option's label names its
// production, so the alternatives are distinguishable on the wire and the
// handler activates the ability that was selected.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// unequalDualLandScript is a land with TWO free-to-tap mana abilities whose
// productions differ in quantity: one {C}, one {C}{C}. One tap yields exactly
// one of them -- never their sum.
func unequalDualLandScript(t testing.TB) *cards.Card {
	return card(t, "Name:Unequal Vale\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ C | Oracle:x\n"+
		"A:AB$ Mana | Cost$ T | Produced$ C C | Oracle:x\n")
}

// attackPropSeatUnequal is attackPropSeatDual's shape with the payer's mana
// supplied by one unequalDualLandScript land.
func attackPropSeatUnequal(t *testing.T, propName string) (*Engine, state.ObjID) {
	t.Helper()
	cfg := Config{Seed: 719, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	onBoardCard(t, e, 0, mshCorpusCard(t, propName))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	onBoardCard(t, e, 1, unequalDualLandScript(t))
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	return e, bear
}

// TestAttackPropPaysTheSelectedAlternative pins the end-to-end unequal-output
// fix: Ghostly Prison charges {2}, the payer's only source is one land whose
// two free abilities make {C} and {C}{C}, so the budget is the MAX (2) and the
// pair is offered; selecting the SECOND option must activate the {C}{C}
// ability and commit the attack. Before the fix the answer resolved the first
// alternative sharing the Obj ({C}), the payer came up short, the window found
// no source left (the land is tapped) and ABORTED the declaration.
func TestAttackPropPaysTheSelectedAlternative(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeatUnequal(t, "Ghostly Prison")

	// PRECONDITION: the one land offers two alternatives with UNEQUAL units,
	// the labels distinguish them on the wire, the budget is the max (2) --
	// which is why a {2} charge is offered at all -- and the pool is empty so
	// the window is the only route.
	sources := e.attackManaSources(1)
	if len(sources) != 2 || sources[0].units != 1 || sources[1].units != 2 {
		t.Fatalf("precondition: attackManaSources(1) = %+v, want two alts with units 1 and 2", sources)
	}
	if got := e.attackBudget(1); got != 2 {
		t.Fatalf("precondition: attackBudget(1) = %d, want 2 (the max alternative)", got)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0", got)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("a land whose max alternative reaches {2} was not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged attack: %v", err)
	}
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) != 2 ||
		pay.Options[0].Kind != "attack_mana" {
		t.Fatalf("expected the two-option attack payment window, got %+v", pay)
	}
	if pay.Options[0].Label == pay.Options[1].Label {
		t.Errorf("precondition: the two alternatives' labels are identical (%q) -- the payer cannot tell them apart; continuing to expose the resolution defect",
			pay.Options[0].Label)
	}
	if !strings.Contains(pay.Options[1].Label, "{C}{C}") {
		t.Errorf("precondition: second option's label %q does not name its {C}{C} production", pay.Options[1].Label)
	}
	// Select the SECOND alternative: the {C}{C} ability.
	if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{1}}); err != nil {
		t.Fatalf("tap the land for {C}{C}: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying {2} = %d, want 0 (the tax was paid, not stranded)", got)
	}
	if o := e.G.Obj(bear); !o.IsAttacking {
		t.Fatal("selecting the {C}{C} alternative left the declaration aborted: the attack was never committed")
	}
	drainCombatPriority(t, e)
}

// TestBlockPropPaysTheSelectedAlternative pins the same identity rule on the
// block side: Qal Sisma Behemoth charges {2} to block, the defender's only
// source is the unequal land, and selecting the {C}{C} option must resolve
// THAT ability and commit the block. Before the fix the answer resolved the
// first alternative ({C}), the window found no source left, cleared itself
// silently, and the block never committed.
func TestBlockPropPaysTheSelectedAlternative(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	qal := onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth"))
	land := onBoardCard(t, e, 0, unequalDualLandScript(t))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	attackSeat0(t, e, bear)

	// PRECONDITION: the static prices the pair at exactly {2}, the one land
	// offers two alternatives with UNEQUAL units, and the pool is empty so
	// the window is the only route.
	if got := e.blockPairCharge(qal, bear).mana; got != 2 {
		t.Fatalf("precondition: blockPairCharge = %d, want 2", got)
	}
	sources := e.attackManaSources(0)
	if len(sources) != 2 || sources[0].units != 1 || sources[1].units != 2 {
		t.Fatalf("precondition: attackManaSources(0) = %+v, want two alts with units 1 and 2", sources)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0", got)
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("affordable pair not offered although the {C}{C} alternative reaches {2}")
	}
	opt := findBlockOption(d, qal, bear)
	if opt == nil {
		t.Fatalf("pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged block: %v", err)
	}
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) != 2 ||
		pay.Options[0].Kind != "block_mana" {
		t.Fatalf("expected the two-option block payment window, got %+v", pay)
	}
	if pay.Options[0].Label == pay.Options[1].Label {
		t.Errorf("precondition: the two alternatives' labels are identical (%q); continuing to expose the resolution defect",
			pay.Options[0].Label)
	}
	// Select the SECOND alternative: the {C}{C} ability.
	if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{1}}); err != nil {
		t.Fatalf("tap the land for {C}{C}: %v", err)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {2} block cost = %d, want 0", got)
	}
	if o := e.G.Obj(land); !o.Tapped {
		t.Fatal("the unequal land was not tapped as the payment")
	}
	blocked := false
	for _, b := range e.G.Obj(bear).BlockedBy {
		if b == qal {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("selecting the {C}{C} alternative left the block uncommitted (BlockedBy %v)", e.G.Obj(bear).BlockedBy)
	}
}
