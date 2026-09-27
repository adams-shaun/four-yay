// blocktag1: the block-direction charge's mana half must not withhold the
// declaration's own committed blockers. Declaring a block never taps a
// creature (CR 509.1), so a blocker that carries a CantBlockUnless tax (and
// is itself the mana source that can pay it) is a legitimate payment source,
// exactly as the payment window (askNextBlockPay, taps-only) and the
// published blockManaBudget already treat it. Before the split, validation
// and the offer gate passed the committed blockers into combatChargeAffordable
// and the mana half subtracted them along with the plan's tap reservations,
// so a legal declaration payable by tapping a blocker was rejected while
// Decision.MaxSum still said payable -- the bot's own answer then crashed the
// table (the reported defect). The obligation half still withholds them:
// see TestHollowWarriorBlockTapsAnUntappedNonblocker and
// TestTapObligationCannotAlsoPayTheMana.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// raysOn attaches the real corpus Oppressive Rays to bearer with a logged
// events.Attach (never a bare field write, so the layer static reads the
// aura's `ValidCard$ Creature.AttachedBy`).
func raysOn(t *testing.T, e *Engine, p state.PlayerID, bearer state.ObjID) state.ObjID {
	t.Helper()
	rays := onBoardCard(t, e, p, mshCorpusCard(t, "Oppressive Rays"))
	e.emit(events.Event{Kind: events.Attach, Obj: rays, IDs: []state.ObjID{bearer}})
	if got := e.G.Obj(rays).AttachedTo; got != bearer {
		t.Fatalf("precondition: Oppressive Rays %d not attached to %d (AttachedTo %d)", rays, bearer, got)
	}
	return rays
}

// llanowarOn places the real corpus Llanowar Elves ({T}: Add {G}) able to
// block (summoning sickness never bars blocking, CR 509.1) and able to use
// its mana ability (a summoning-sick creature cannot tap for {T}, so the
// sickness is cleared as if it had been under its controller since turn 1).
func llanowarOn(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	id := onBoardCard(t, e, p, mshCorpusCard(t, "Llanowar Elves"))
	e.G.Obj(id).SummonSick = false
	return id
}

// TestBlockTaxBlockerOwnManaIsARealSource pins the fix end to end on the
// filing card: Oppressive Rays ({3} CantBlockUnless) on a lone Llanowar Elves
// that is the defender's only mana source, with {2} floating. The pair is
// offered, the bot's own answer is accepted, and the charge settles by
// tapping the blocker itself.
func TestBlockTaxBlockerOwnManaIsARealSource(t *testing.T) {
	e := threeSeatEngine(t)
	// Oppressive Rays lies on the battlefield under seat 0, attached to the
	// defender's only creature. (An aura's controller controls the aura; the
	// static scopes by AttachedBy, so the bearer's controller pays.)
	elves := llanowarOn(t, e, 0)
	raysOn(t, e, 0, elves)
	wurm := onBoardReady(t, e, 1, "Name:Craw Wurm\nManaCost:4 G G\nTypes:Creature Wurm\nPT:6/4\nOracle:x\n")
	floatMana(t, e, 0, "RR")
	attackSeat0(t, e, wurm)

	// PRECONDITIONS: the bearer is on the battlefield and untapped, the aura
	// is attached where the static reads, the charge is exactly {3}, the pool
	// holds exactly {2}, and the Elves is the ONLY mana source -- so the
	// blocker's own mana is the difference between payable and not.
	if o := e.G.Obj(elves); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Elves not an untapped battlefield object: %+v", o)
	}
	ch := e.blockPairCharge(elves, wurm)
	if ch.mana != 3 || ch.life != 0 || len(ch.taps) != 0 || ch.unpriceable {
		t.Fatalf("precondition: blockPairCharge = %+v, want a clean {3} mana tax", ch)
	}
	if e.G.Players[0].Pool.Total() != 2 {
		t.Fatalf("precondition: pool = %d, want 2", e.G.Players[0].Pool.Total())
	}
	srcs := e.attackManaSources(0)
	if len(srcs) != 1 || srcs[0].id != elves {
		t.Fatalf("precondition: mana sources = %+v, want exactly the blocker %d", srcs, elves)
	}
	if e.blockManaBudget(0) != 3 {
		t.Fatalf("precondition: blockManaBudget = %d, want 3 (the {2} pool + the blocker's {G})", e.blockManaBudget(0))
	}

	// (a) The pair IS offered. At main the mana half withheld the blocker, so
	// only the {2} pool remained and the {3} pair was dropped.
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("blockers decision missing although the pair is payable")
	}
	if d.MaxSum != 3 {
		t.Fatalf("published MaxSum = %d, want the {3} budget", d.MaxSum)
	}
	opt := findBlockOption(d, elves, wurm)
	if opt == nil {
		t.Fatalf("the blocker's own-mana pair is not offered: %+v", d.Options)
	}
	if opt.Value != 3 {
		t.Fatalf("offered pair Value = %d, want 3", opt.Value)
	}

	// (b)/(c) Submit the block the way a client that read MaxSum would, then
	// settle the payment window: the charge is payable ONLY by tapping the
	// blocker itself. At main validateBlockers rejected this very declaration
	// while MaxSum advertised it as payable.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("the blocker's own-mana declaration was rejected although MaxSum says payable: %v", err)
	}
	tapped := false
	for i := 0; i < 16; i++ {
		p := e.Pending()
		if p == nil || p.Kind != decision.KChoose {
			break
		}
		paid := false
		for _, o := range p.Options {
			if o.Kind == "block_mana" && o.Obj == elves {
				if err := e.Submit(decision.Intent{Seq: p.Seq, Player: p.Player, Choices: []int{o.Index}}); err != nil {
					t.Fatalf("tapping the blocker for its own tax rejected: %v", err)
				}
				tapped = true
				paid = true
				break
			}
		}
		if !paid {
			t.Fatalf("the payment window never offered the blocker (%d) as a block_mana source: %+v", elves, p.Options)
		}
	}
	if !tapped {
		t.Fatal("the payment window posed no block_mana ask at all")
	}

	// (c) The charge settled by tapping the blocker, the block committed, and
	// the blocker remained in combat.
	if !e.G.Obj(elves).Tapped {
		t.Fatal("the blocker itself was not tapped to pay its own block tax")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the paid block = %d, want 0 ({2} floating + the Elves' {G})", got)
	}
	blocked := false
	for _, b := range e.G.Obj(wurm).BlockedBy {
		if b == elves {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("the paid block never committed (Wurm.BlockedBy = %v)", e.G.Obj(wurm).BlockedBy)
	}
	if o := e.G.Obj(elves); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the blocker left play: %+v", o)
	}

	// (b, bot arm) A bot that reads its block answer off the published fields
	// (LegalBlockChoices -> Submit) must not have that answer rejected on the
	// same board. The board is made lethal so the heuristics actually choose
	// the block; the assertion is that whatever the bot returns is accepted.
	t.Run("bot answer accepted", func(t *testing.T) {
		e := threeSeatEngine(t)
		elves := llanowarOn(t, e, 0)
		raysOn(t, e, 0, elves)
		wurm := onBoardReady(t, e, 1, "Name:Craw Wurm\nManaCost:4 G G\nTypes:Creature Wurm\nPT:6/4\nOracle:x\n")
		floatMana(t, e, 0, "RR")
		e.G.Players[0].Life = 1
		attackSeat0(t, e, wurm)

		d := askBlockersFresh(t, e)
		if d == nil || findBlockOption(d, elves, wurm) == nil {
			t.Fatalf("precondition: the taxed pair is not offered: %+v", d)
		}
		bot := newTestBot(11)
		sawBlock := false
		for i := 0; i < 16; i++ {
			d := e.Pending()
			if d == nil || e.G.Step != state.StepDeclareBlockers || d.Kind == decision.KPriority {
				break
			}
			in := bot.answer(e, d)
			if d.Kind == decision.KBlockers && len(in.Choices) > 0 {
				sawBlock = true
			}
			if err := e.Submit(in); err != nil {
				t.Fatalf("bot's own %v answer %v rejected by the engine (never consumed: livelock): %v", d.Kind, in.Choices, err)
			}
		}
		if !sawBlock {
			t.Fatal("the bot never chose the block on the lethal board, so the no-rejection arm is vacuous")
		}
	})
}

// TestBlockTaxTwoBlockersOnePaysTheOthersTax pins the whole-declaration half:
// blocker A (an untaxed Llanowar Elves, the only mana source) pays blocker
// B's Oppressive Rays {3} tax. The per-pair offer for B succeeds at main
// (only B was withheld there), but validateBlockers withheld EVERY committed
// blocker from the mana half, so the same declaration the offer list showed
// was rejected on submit. The fix lets the declaration settle by tapping A.
func TestBlockTaxTwoBlockersOnePaysTheOthersTax(t *testing.T) {
	e := threeSeatEngine(t)
	elves := llanowarOn(t, e, 0) // untaxed, the mana source
	bear := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	raysOn(t, e, 0, bear)
	wurm1 := onBoardReady(t, e, 1, "Name:Craw Wurm\nManaCost:4 G G\nTypes:Creature Wurm\nPT:6/4\nOracle:x\n")
	wurm2 := onBoardReady(t, e, 1, "Name:Craw Wurm\nManaCost:4 G G\nTypes:Creature Wurm\nPT:6/4\nOracle:x\n")
	floatMana(t, e, 0, "RR")
	attackSeat0(t, e, wurm1, wurm2)

	// PRECONDITIONS: B carries the {3} tax, A is the lone source, the pool is
	// {2}, and the two charges (A's free, B's {3}) sum to exactly the budget.
	if ch := e.blockPairCharge(elves, wurm1); !ch.zero() || ch.unpriceable {
		t.Fatalf("precondition: A's pair charge = %+v, want free", ch)
	}
	if ch := e.blockPairCharge(bear, wurm2); ch.mana != 3 || ch.unpriceable {
		t.Fatalf("precondition: B's pair charge = %+v, want a clean {3}", ch)
	}
	srcs := e.attackManaSources(0)
	if len(srcs) != 1 || srcs[0].id != elves {
		t.Fatalf("precondition: mana sources = %+v, want exactly A (%d)", srcs, elves)
	}
	if e.G.Players[0].Pool.Total() != 2 || e.blockManaBudget(0) != 3 {
		t.Fatalf("precondition: pool %d, budget %d, want 2 and 3", e.G.Players[0].Pool.Total(), e.blockManaBudget(0))
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("blockers decision missing although the declaration is payable")
	}
	optA := findBlockOption(d, elves, wurm1)
	optB := findBlockOption(d, bear, wurm2)
	if optA == nil || optB == nil {
		t.Fatalf("the two-blocker declaration is not offered: %+v", d.Options)
	}

	// Submit the whole declaration (A blocks wurm1, B blocks wurm2). At main
	// validateBlockers withheld both blockers from the mana half and rejected
	// it; with the fix the {3} tax is payable by tapping A.
	chosen := []int{optA.Index, optB.Index}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: chosen}); err != nil {
		t.Fatalf("two-blocker declaration rejected although its tax is payable by tapping A: %v", err)
	}
	for i := 0; i < 16; i++ {
		p := e.Pending()
		if p == nil || p.Kind != decision.KChoose {
			break
		}
		paid := false
		for _, o := range p.Options {
			if o.Kind == "block_mana" && o.Obj == elves {
				if !strings.Contains(o.Label, "Llanowar") {
					t.Fatalf("block_mana option label = %q, want the Elves", o.Label)
				}
				index := o.Index
				if err := e.Submit(decision.Intent{Seq: p.Seq, Player: p.Player, Choices: []int{index}}); err != nil {
					t.Fatalf("tapping A for B's tax rejected: %v", err)
				}
				paid = true
				break
			}
		}
		if !paid {
			t.Fatalf("the payment window never offered A (%d) as a block_mana source: %+v", elves, p.Options)
		}
	}
	if !e.G.Obj(elves).Tapped {
		t.Fatal("A was not tapped to pay B's block tax")
	}
	for _, pair := range [][2]state.ObjID{{wurm1, elves}, {wurm2, bear}} {
		found := false
		for _, b := range e.G.Obj(pair[0]).BlockedBy {
			if b == pair[1] {
				found = true
			}
		}
		if !found {
			t.Fatalf("block did not commit: %d.BlockedBy = %v, want %d", pair[0], e.G.Obj(pair[0]).BlockedBy, pair[1])
		}
	}
}
