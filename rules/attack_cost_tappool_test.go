package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestHollowWarriorAttackDeclarationTapPool is the wire-contract regression
// for the declaration-dependent tap pool. Hollow Warrior's
// `CantAttackUnless ... Cost$ tapXType<1/Creature.!attacking>` is payable only
// by tapping a creature NOT declared as attacking, so a declaration that
// commits every other attacker leaves the obligation no candidate. Before the
// fix the KAttackers option list published only CostTaps, so
// Decision.Validate accepted the all-three declaration while the engine's
// board-aware validateAttackers rejected it -- a decision that looked legal
// from the offered wire but failed on submission.
//
// The test drives the REAL corpus card (mshCorpusCard) with two vanilla
// creatures and asserts the published pool, the wire rejection of the
// pool-exhausting declaration, and the acceptance (and payment) of the
// declaration that leaves a candidate.
func TestHollowWarriorAttackDeclarationTapPool(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	warrior := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Hollow Warrior"))
	bear1 := onBoardReady(t, e, 0, "Name:Test Bear One\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear2 := onBoardReady(t, e, 0, "Name:Test Bear Two\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: all three creatures are on the battlefield in the zone
	// the rule reads (seat 0's), and all are untapped.
	for _, id := range []state.ObjID{warrior, bear1, bear2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is not on the battlefield: %+v", id, o)
		}
		if o.Controller != 0 {
			t.Fatalf("precondition: object %d is not controlled by seat 0", id)
		}
		if o.Tapped {
			t.Fatalf("precondition: object %d is already tapped", id)
		}
	}
	// PRECONDITION: the charge is a single tap obligation and the candidate
	// pool is nonempty without the declaration (three untapped
	// Creature.!attacking permanents).
	ch := e.attackPairCharge(warrior, 1, 0)
	if len(ch.taps) != 1 || ch.taps[0].n != 1 || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want one tap of 1", ch)
	}
	if got := len(e.blockTapCandidates(0, ch.taps[0], nil)); got != 3 {
		t.Fatalf("precondition: pool without exclusions = %d, want 3", got)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no attackers decision: %+v", d)
	}
	// PRECONDITION: the engine published the pool, and the values the test
	// compares really differ (all three consumes 3 of a 3-pool owing 1, the
	// two-creature declaration consumes 2).
	if d.ChargeTapPool != 3 {
		t.Fatalf("published ChargeTapPool = %d, want 3", d.ChargeTapPool)
	}
	wopt := findAttackOption(d, warrior, 1)
	b1opt := findAttackOption(d, bear1, 1)
	b2opt := findAttackOption(d, bear2, 1)
	if wopt == nil || b1opt == nil || b2opt == nil {
		t.Fatalf("missing offered pairs for the three creatures: %+v", d.Options)
	}
	if wopt.CostTaps != 1 || wopt.TapPoolCost != 1 {
		t.Fatalf("warrior option = %+v, want CostTaps 1 and TapPoolCost 1", wopt)
	}
	if b1opt.CostTaps != 0 || b1opt.TapPoolCost != 1 || b2opt.CostTaps != 0 || b2opt.TapPoolCost != 1 {
		t.Fatalf("bear options must each consume one pool candidate: %+v / %+v", b1opt, b2opt)
	}
	all := []int{wopt.Index, b1opt.Index, b2opt.Index}
	leave := []int{wopt.Index, b1opt.Index}

	// The wire validator rejects the declaration that consumes the last
	// candidate and accepts the one that leaves one.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: all}); err == nil {
		t.Fatal("Decision.Validate accepted the pool-exhausting declaration")
	} else if !strings.Contains(err.Error(), "tap obligation") {
		t.Fatalf("Decision.Validate error = %v, want the tap-pool rejection", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: leave}); err != nil {
		t.Fatalf("Decision.Validate rejected the legal declaration that leaves a candidate: %v", err)
	}
	// The engine's own validator agrees, through the shared rule.
	if err := e.validateAttackers(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: all}); err == nil {
		t.Fatal("validateAttackers accepted the pool-exhausting declaration")
	}
	if err := e.validateAttackers(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: leave}); err != nil {
		t.Fatalf("validateAttackers rejected the legal declaration: %v", err)
	}

	// The legal declaration submits, pays the obligation from the one
	// uncommitted creature, and commits both attackers.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: leave}); err != nil {
		t.Fatalf("submit legal declaration: %v", err)
	}
	if !e.G.Obj(bear2).Tapped {
		t.Fatal("the tap obligation did not tap the one uncommitted creature")
	}
	if o := e.G.Obj(warrior); o == nil || !o.IsAttacking {
		t.Fatal("Hollow Warrior was not declared attacking")
	}
	if o := e.G.Obj(bear1); o == nil || !o.IsAttacking {
		t.Fatal("the chosen Bear was not declared attacking")
	}
	if o := e.G.Obj(bear2); o == nil || o.IsAttacking {
		t.Fatal("the tap candidate must not also be attacking")
	}
	drainCombatPriority(t, e)
}
