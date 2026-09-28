package rules

// The MustBlock requirement matcher must count its CR 509.1c maximum over
// exactly the (blocker, attacker) pairs the declare-blockers offer actually
// makes declarable -- admissiblePair (rules/combat.go), not canBlock alone.
// A requirement whose only pair the offer suppresses -- an attacker whose
// Min$ bound the defender cannot meet (minImpossible) or a block price above
// the pool -- must contribute nothing to the maximum, or validateBlockers
// rejects a declaration that satisfies every offered requirement (and the
// deterministic bot, which submits just such a declaration, wedges).
//
// Both leaves go through the real askBlockers offer and Submit validation:
// the priced fixture pair (CantBlockUnless$ Cost$ 3 on the MustBlock
// creature, pool empty) and the corpus pair (corpus Watchdog's stat:MustBlock
// against corpus Underworld Cerberus's Min$ 3). Each asserts its own
// preconditions: the priced pair really prices, the bound really suppresses,
// and every compared pair differs only in the suppressed dimension.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// pricedMustBlockSrc is a creature that is BOTH required to block (stat:
// MustBlock) and priced out of its block (stat:CantBlockUnless$ Cost$ 3) --
// the combination no corpus card carries, composed of the exact static shapes
// the corpus prints (Watchdog's MustBlock, Qal Sisma Behemoth's
// CantBlockUnless).
const pricedMustBlockSrc = "Name:Fixture Watchdog\nManaCost:3\n" +
	"Types:Artifact Creature Dog\nPT:1/2\n" +
	"S:Mode$ MustBlock | ValidCreature$ Card.Self | Description$ CARDNAME blocks each combat if able.\n" +
	"S:Mode$ CantBlockUnless | ValidCard$ Card.Self | Cost$ 3\n" +
	"Oracle:x\n"

// TestMustBlockIgnoresUnaffordableBlockPrice pins the cost half: with the
// pool empty, the required creature's block costs {3} and is never offered,
// so its requirement must not be counted -- declaring the free blocker is
// legal and commits, not a "0 of 1" rejection.
func TestMustBlockIgnoresUnaffordableBlockPrice(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	req := onBoardCard(t, e, 0, card(t, pricedMustBlockSrc))
	bear := onBoardCard(t, e, 0, card(t, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n"))
	attacker := onBoardCard(t, e, 1, card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	attackSeat0(t, e, attacker)

	// Preconditions: the static prices the required pair at exactly {3}, the
	// budget the offer filter reads is empty, and BOTH pairs are blockable
	// on ability alone -- the price is the only separation.
	if got := e.blockPairCharge(req, attacker).mana; got != 3 {
		t.Fatalf("precondition: blockPairCharge = %d, want 3 (the fixture static must price the pair)", got)
	}
	if e.blockManaBudget(0) != 0 {
		t.Fatalf("precondition: block budget = %d, want 0", e.blockManaBudget(0))
	}
	if !e.canBlock(req, attacker) || !e.canBlock(bear, attacker) {
		t.Fatalf("precondition: pairs must be blockable on ability alone (req→%v bear→%v)",
			e.canBlock(req, attacker), e.canBlock(bear, attacker))
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no blockers decision: the free pair must still be offered")
	}
	free := findBlockOption(d, bear, attacker)
	if free == nil {
		t.Fatalf("free pair not offered: %+v", d.Options)
	}
	for _, o := range d.Options {
		if o.Obj == req {
			t.Fatalf("unaffordable MustBlock pair offered: %+v", o)
		}
	}
	if free.Required {
		t.Fatalf("free pair marked Required although the required creature has no declarable pair: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{free.Index}}); err != nil {
		t.Fatalf("declaration blocking with the free blocker rejected: %v", err)
	}
	if !blockCommitted(t, e, attacker, bear) {
		t.Fatalf("block never committed (BlockedBy %v)", e.G.Obj(attacker).BlockedBy)
	}
}

// TestMustBlockIgnoresMinImpossibleAttacker pins the Min$ half: Underworld
// Cerberus's Min$ 3 leaves the defender's two creatures one short, so the
// offer suppresses every pair with it, and the required corpus Watchdog (a
// ground creature) cannot reach the flier either -- so the requirement
// contributes nothing and the falcon's legal block declares, not a "0 of 1"
// rejection of the only declaration anybody could make.
func TestMustBlockIgnoresMinImpossibleAttacker(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	req := onBoardCard(t, e, 0, mshCorpusCard(t, "Watchdog"))
	falcon := onBoardCard(t, e, 0, card(t, "Name:Runeclaw Falcon\nManaCost:2 W\nTypes:Creature Bird\nPT:1/1\nK:Flying\nOracle:x\n"))
	cerberus := onBoardCard(t, e, 1, mshCorpusCard(t, "Underworld Cerberus"))
	flier := onBoardCard(t, e, 1, card(t, "Name:Test Sprite\nManaCost:1 U\nTypes:Creature Faerie\nPT:1/1\nK:Flying\nOracle:x\n"))
	attackSeat0(t, e, cerberus, flier)

	// Preconditions: the bound is a plain Min$ 3; only two creatures can even
	// attempt the Cerberus (so its pairs are suppressed); the Watchdog can
	// block the Cerberus but not the flier; the falcon can still block the
	// flier.
	min, max, minOK, maxOK, all := e.minMaxBlockerBounds(cerberus)
	if !minOK || min != 3 || maxOK || all {
		t.Fatalf("precondition: Cerberus bounds min=%d minOK=%v max=%d maxOK=%v all=%v", min, minOK, max, maxOK, all)
	}
	if n := e.legalBlockerCount(cerberus, 0); n != 2 {
		t.Fatalf("precondition: legalBlockerCount = %d, want 2 (below the Min$ 3, so the pairs are suppressed)", n)
	}
	if !e.canBlock(req, cerberus) || e.canBlock(req, flier) || !e.canBlock(falcon, flier) {
		t.Fatalf("precondition: pair abilities wrong (req→cerb %v, req→flier %v, falcon→flier %v)",
			e.canBlock(req, cerberus), e.canBlock(req, flier), e.canBlock(falcon, flier))
	}

	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no blockers decision: the falcon's legal block must still be offered")
	}
	if len(d.Options) != 1 {
		t.Fatalf("offered %+v, want exactly the falcon-blocks-flier pair", d.Options)
	}
	if d.Options[0].Obj != falcon || d.Options[0].Attacker != flier {
		t.Fatalf("offered %+v, want falcon blocks flier", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("the only legal declaration rejected: %v", err)
	}
	if !blockCommitted(t, e, flier, falcon) {
		t.Fatalf("block never committed (BlockedBy %v)", e.G.Obj(flier).BlockedBy)
	}
}

// blockCommitted reports whether blocker is recorded on attacker's BlockedBy.
func blockCommitted(t *testing.T, e *Engine, attacker, blocker state.ObjID) bool {
	t.Helper()
	for _, b := range e.G.Obj(attacker).BlockedBy {
		if b == blocker {
			return true
		}
	}
	return false
}
