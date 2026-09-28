package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestCantBeCastRemovesTheCastOption is row 1: CantBeCast | Caster$ Opponent
// gates by who is doing the casting, not by whose battlefield the restrictor
// sits on.
func TestCantBeCastRemovesTheCastOption(t *testing.T) {
	t.Parallel()
	bear := card(t, "Name:Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	restrictor := card(t, "Name:Restrictor\nManaCost:1 W\nTypes:Artifact\n"+
		"S:Mode$ CantBeCast | ValidCard$ Creature | Caster$ Opponent\nOracle:x\n")

	e := handEngine(t, bear)
	e.G.Players[0].Pool[state.MG] = 1
	ro := e.G.AddObject(restrictor, 0)
	ro.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{ro.ID})

	if kinds(e.legalActions(0))["cast"] != 1 {
		t.Fatal("the restrictor's own controller should still be able to cast their creature")
	}

	// Give the opponent (seat 1) the same creature in hand, on their own turn.
	bear2 := e.G.AddObject(bear, 1)
	bear2.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{bear2.ID})
	e.G.Players[1].Pool[state.MG] = 1
	e.G.Active = 1

	if kinds(e.legalActions(1))["cast"] != 0 {
		t.Fatal("the opponent's creature cast should be restricted")
	}
}

// TestRaiseCostMakesASpellUnaffordable is row 2.
func TestRaiseCostMakesASpellUnaffordable(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	tax := card(t, "Name:Tax\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ RaiseCost | ValidCard$ Instant | Amount$ 2\nOracle:x\n")
	e := handEngine(t, zap)
	to := e.G.AddObject(tax, 0)
	to.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{to.ID})

	e.G.Players[0].Pool[state.MC] = 1
	if kinds(e.legalActions(0))["cast"] != 0 {
		t.Fatal("a 1-mana spell raised by 2 should not be castable with 1 mana")
	}
	e.G.Players[0].Pool[state.MC] = 3
	if kinds(e.legalActions(0))["cast"] != 1 {
		t.Fatal("the raised cost of 3 should be castable with 3 mana")
	}
}

// TestReduceCostFloorsAtZero is row 3: CR 601.2f -- a reduction can eat the
// generic component but must never spill into the coloured requirement.
func TestReduceCostFloorsAtZero(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	discount := card(t, "Name:Discount\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ ReduceCost | ValidCard$ Instant | Amount$ 5\nOracle:x\n")
	e := handEngine(t, zap)
	do := e.G.AddObject(discount, 0)
	do.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{do.ID})

	id := e.G.Zone(state.ZHand, 0)[0]
	c := e.adjustedCost(0, id)
	if c.Generic != 0 {
		t.Fatalf("generic = %d, want 0 (floored, not negative)", c.Generic)
	}
	if c.Colored[state.MR] != 1 {
		t.Fatalf("the R requirement must survive the reduction untouched, got %v", c.Colored)
	}

	// End to end: only red mana in the pool, no generic at all.
	e.G.Players[0].Pool[state.MR] = 1
	if kinds(e.legalActions(0))["cast"] != 1 {
		t.Fatal("a cost reduced to bare {R} should be payable with a single red mana")
	}
}

// TestReduceCostAffectsActualPayment goes past option visibility (which the
// brief's own gating covers) to the mana actually spent when the option is
// submitted. This is not one of the brief's eight rows, but adjustedCost's
// own doc says it computes what a spell costs, and legalActions' gate is the
// only consumer the brief wires up; without also routing castSpell's payment
// through adjustedCost, a reduced-cost spell would still be charged its
// printed cost at resolution, which -- since payMana silently no-ops on an
// unpayable cost -- means the pool holding only the reduced amount would
// leave the spell cast for free instead of for the reduced price.
func TestReduceCostAffectsActualPayment(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	discount := card(t, "Name:Discount\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ ReduceCost | ValidCard$ Instant | Amount$ 1\nOracle:x\n")
	e := handEngine(t, zap)
	do := e.G.AddObject(discount, 0)
	do.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{do.ID})
	// Only the red pip's worth of mana: enough for the reduced {R} cost, not
	// enough for the printed {1}{R}.
	e.G.Players[0].Pool[state.MR] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")

	// CR 601.2a: the spell is on the stack before its target is chosen, and
	// payment (601.2h) follows the target (601.2c) -- so answer the target
	// first, then assert the reduced cost was charged.
	if len(e.G.Stack) != 1 {
		t.Fatal("the spell should have reached the stack")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	submitChoices(t, e, indexOfPlayerOption(d, 1))
	if e.G.Players[0].Pool[state.MR] != 0 {
		t.Fatalf("pool = %v, want the R spent for the reduced cost", e.G.Players[0].Pool)
	}
}

// TestRaiseCostAffectsActualPayment is the mirror check for the raise
// direction: undercharging (paying the printed cost instead of the raised
// one) is a quieter bug than ReduceCost's free-cast case, since payMana
// would still succeed, just for less than intended.
func TestRaiseCostAffectsActualPayment(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	tax := card(t, "Name:Tax\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ RaiseCost | ValidCard$ Instant | Amount$ 2\nOracle:x\n")
	e := handEngine(t, zap)
	to := e.G.AddObject(tax, 0)
	to.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{to.ID})
	e.G.Players[0].Pool[state.MC] = 3
	e.askPriority(0)
	castFirst(t, e, "cast")

	// CR 601.2c-before-601.2h: answer the target, then assert the raised
	// cost was actually paid.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	submitChoices(t, e, indexOfPlayerOption(d, 1))
	if e.G.Players[0].Pool[state.MC] != 0 {
		t.Fatalf("pool = %v, want all 3 raised-cost mana spent", e.G.Players[0].Pool)
	}
}

// TestAlternativeCostAddsASecondCastOption is row 4.
func TestAlternativeCostAddsASecondCastOption(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:2 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\n"+
		"S:Mode$ AlternativeCost | ValidCard$ Card.Self | Cost$ 1 U\nOracle:x\n")
	e := handEngine(t, zap)
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MU] = 1
	e.G.Players[0].Pool[state.MC] = 2

	var labels []string
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" {
			labels = append(labels, o.Label)
		}
	}
	if len(labels) != 2 {
		t.Fatalf("expected two cast options (normal + alternative), got %v", labels)
	}
	if labels[0] == labels[1] {
		t.Fatalf("the two cast options must have distinct labels, got %v", labels)
	}
}

// TestCantBeActivatedSuppressesManaAbilities is row 5.
func TestCantBeActivatedSuppressesManaAbilities(t *testing.T) {
	t.Parallel()
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	hatepiece := card(t, "Name:Hatepiece\nManaCost:1 W\nTypes:Artifact\n"+
		"S:Mode$ CantBeActivated | ValidCard$ Land.OppCtrl\nOracle:x\n")

	e := handEngine(t)
	land := e.G.AddObject(mtn, 0)
	land.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{land.ID})

	hp := e.G.AddObject(hatepiece, 1)
	hp.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{hp.ID})

	if kinds(e.legalActions(0))["activate"] != 0 {
		t.Fatal("the land's mana ability should be suppressed by the opponent's static")
	}
}

// TestCantBlockRemovesEveryBlockOption is row 6. This exercises
// blockRestricted directly, in isolation from a real declare-blockers
// decision -- askBlockers/handleBlockers (rules/combat.go) are fully
// implemented and covered end-to-end elsewhere; this test is purely about
// the static-restriction primitive the two of them call.
func TestCantBlockRemovesEveryBlockOption(t *testing.T) {
	t.Parallel()
	blockerCard := card(t, "Name:Blocker\nManaCost:1 W\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
	attackerCard := card(t, "Name:Attacker\nManaCost:1 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	hex := card(t, "Name:Hex\nManaCost:1 U\nTypes:Enchantment\n"+
		"S:Mode$ CantBlock | ValidCard$ Creature\nOracle:x\n")

	e := handEngine(t)
	bl := e.G.AddObject(blockerCard, 0)
	bl.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{bl.ID})
	at := e.G.AddObject(attackerCard, 1)
	at.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{at.ID})

	if e.blockRestricted(bl.ID, at.ID) {
		t.Fatal("no restriction registered yet, blocking should be legal")
	}

	hx := e.G.AddObject(hex, 1)
	hx.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, append(e.G.Zone(state.ZBattlefield, 1), hx.ID))

	if !e.blockRestricted(bl.ID, at.ID) {
		t.Fatal("CantBlock should suppress every block for this creature")
	}
}

// TestCantBlockByRemovesOnlyMatchingPairs is row 7.
func TestCantBlockByRemovesOnlyMatchingPairs(t *testing.T) {
	t.Parallel()
	smallCard := card(t, "Name:Small\nManaCost:W\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
	bigCard := card(t, "Name:Big\nManaCost:2 W\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	attackerCard := card(t, "Name:Menacing\nManaCost:1 R\nTypes:Creature Goblin\nPT:2/2\n"+
		"S:Mode$ CantBlockBy | ValidCard$ Card.Self | ValidBlocker$ Creature.powerLE1\nOracle:x\n")

	e := handEngine(t)
	small := e.G.AddObject(smallCard, 0)
	small.Zone = state.ZBattlefield
	big := e.G.AddObject(bigCard, 0)
	big.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{small.ID, big.ID})
	att := e.G.AddObject(attackerCard, 1)
	att.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{att.ID})

	if !e.blockRestricted(small.ID, att.ID) {
		t.Fatal("the 1/1 should be restricted from blocking this attacker")
	}
	if e.blockRestricted(big.ID, att.ID) {
		t.Fatal("the 2/2 should still be able to block")
	}
}

// TestStaticsFromNonBattlefieldZonesDoNotApply is row 8.
func TestStaticsFromNonBattlefieldZonesDoNotApply(t *testing.T) {
	t.Parallel()
	restrictor := card(t, "Name:Restrictor\nManaCost:1 W\nTypes:Artifact\n"+
		"S:Mode$ CantBeCast | ValidCard$ Creature\nOracle:x\n")
	bear := card(t, "Name:Bear\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e := handEngine(t, bear)
	ro := e.G.AddObject(restrictor, 0)
	ro.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{ro.ID})

	if e.castRestricted(0, e.G.Zone(state.ZHand, 0)[0]) {
		t.Fatal("a static on a card in the graveyard must not restrict anything")
	}
}

// TestActiveStaticsIsDeterministicallyOrdered guards the no-nondeterminism
// constraint: activeStatics must never let map iteration leak into its
// output order, since cost adjustment and the option list both depend on it
// being stable run to run.
func TestActiveStaticsIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()
	a := card(t, "Name:A\nManaCost:1\nTypes:Artifact\nS:Mode$ RaiseCost | ValidCard$ Creature | Amount$ 1\nOracle:x\n")
	b := card(t, "Name:B\nManaCost:1\nTypes:Artifact\nS:Mode$ RaiseCost | ValidCard$ Creature | Amount$ 1\nOracle:x\n")

	e := handEngine(t)
	oa := e.G.AddObject(a, 0)
	oa.Zone = state.ZBattlefield
	ob := e.G.AddObject(b, 0)
	ob.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{oa.ID, ob.ID})

	var first []state.ObjID
	for i := 0; i < 20; i++ {
		var got []state.ObjID
		for _, sv := range e.activeStatics("RaiseCost") {
			got = append(got, sv.Source)
		}
		if i == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("run %d: length changed: %v vs %v", i, got, first)
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d: order changed at %d: %v vs %v", i, j, got, first)
			}
		}
	}
	if len(first) != 2 || first[0] != oa.ID || first[1] != ob.ID {
		t.Fatalf("expected [%d %d] in registration order, got %v", oa.ID, ob.ID, first)
	}
}

// --- Fix round 1: Ruling T19b-b -- alternative-cost casting must pay the
// alternative, and a failed payment must not put the spell on the stack ---
//
// The bug this closes: legalActions offers an alt-cost "cast" option gated
// on the ALTERNATIVE cost's affordability, but castSpell always paid
// adjustedCost -- the BASE cost. When the base cost cannot be paid (the
// whole point of an alternative cost existing), Cost.Pay fails and payMana
// silently no-op'd, so castSpell carried on and put the spell on the stack
// anyway, having spent nothing. Reachable from ordinary, well-formed card
// data (a 4-mana spell with an alternative {U} cost and a pool holding only
// one blue), not a malformed-input edge case.

// TestAlternativeCostChargesTheAlternativeAmount is the positive case:
// choosing the alternative-cost option must charge exactly that cost, not
// the (unpayable) base cost.
func TestAlternativeCostChargesTheAlternativeAmount(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:4\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\n"+
		"S:Mode$ AlternativeCost | ValidCard$ Card.Self | Cost$ U\nOracle:x\n")
	e := handEngine(t, zap)
	// Only enough for the alternative cost, nowhere near enough for the
	// printed {4}.
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.AltCostIndex > 0 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no alternative-cost cast option offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit alt-cost cast: %v", err)
	}

	// CR 601.2c-before-601.2h: the alternative cost is charged only once the
	// target is chosen (the spell is already on the stack per 601.2a). Answer
	// the target first, then assert the alternative amount was paid.
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", td)
	}
	submitChoices(t, e, indexOfPlayerOption(td, 1))
	if e.G.Players[0].Pool[state.MU] != 0 {
		t.Fatalf("pool = %v, want the U spent for the alternative cost", e.G.Players[0].Pool)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool = %v, want nothing left over", e.G.Players[0].Pool)
	}
	if len(e.G.Stack) != 1 {
		t.Fatal("the spell should have reached the stack, paid for by the alternative cost")
	}
}

// TestUnpayableCastDoesNotReachTheStack is the abort-path case: if the cost
// actually cannot be paid, the spell must not reach the stack and nothing
// must be deducted. Calls beginCast directly (bypassing legalActions, which
// would never offer an unpayable option) to exercise the failure path
// deterministically.
func TestUnpayableCastDoesNotReachTheStack(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:4\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	e := handEngine(t, zap)
	// Empty pool: the cost cannot be paid at all.
	id := e.G.Zone(state.ZHand, 0)[0]
	e.beginCast(0, decision.Option{Obj: id})

	if len(e.G.Stack) != 0 {
		t.Fatal("a cast that could not be paid for must not reach the stack")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool = %v, want untouched after a failed cast", e.G.Players[0].Pool)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 1 {
		t.Fatal("the card should remain in hand after a failed cast")
	}
}

// --- Fix round 1: Ruling T19b-c -- parseAmount bounds and sign validation ---
//
// Unlike mana.go's ParseCost (which checks 0 <= n <= math.MaxInt32 before
// ever casting to int32), parseAmount cast straight to int32 with no check
// at all, so a too-large Amount$ silently wraps negative and a negative
// Amount$ is accepted as-is. Both invert the static's intent: a wrapped
// RaiseCost becomes a discount, a wrapped ReduceCost becomes a tax, and a
// plain negative ReduceCost Amount becomes a raise.

// TestParseAmountRejectsOutOfRangeAndNegativeValues is the direct unit-level
// check across the cases the reviewer found, plus the ordinary and boundary
// cases that must keep working.
func TestParseAmountRejectsOutOfRangeAndNegativeValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		def  int32
		want int32
	}{
		{"3000000000", 1, 1},          // overflows int32: must fall back, not wrap negative
		{"-5", 1, 1},                  // negative: must fall back, not flip the sign's effect
		{"2147483647", 1, 2147483647}, // math.MaxInt32 itself is still fine
		{"2147483648", 1, 1},          // MaxInt32+1 overflows: must fall back
		{"", 7, 7},                    // missing/empty: default
		{"abc", 3, 3},                 // malformed: default
		{"5", 1, 5},                   // ordinary case unaffected
	}
	for _, c := range cases {
		if got := parseAmount(c.in, c.def); got != c.want {
			t.Errorf("parseAmount(%q, %d) = %d, want %d", c.in, c.def, got, c.want)
		}
	}
}

// TestRaiseCostAmountOverflowDoesNotWrapNegative is the end-to-end
// reproduction of the reviewer's first case: an out-of-range Amount$ on a
// RaiseCost must not wrap into a negative adjustment that makes the spell
// castable with an empty pool.
func TestRaiseCostAmountOverflowDoesNotWrapNegative(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	tax := card(t, "Name:Tax\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ RaiseCost | ValidCard$ Instant | Amount$ 3000000000\nOracle:x\n")
	e := handEngine(t, zap)
	to := e.G.AddObject(tax, 0)
	to.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{to.ID})

	if kinds(e.legalActions(0))["cast"] != 0 {
		t.Fatal("an out-of-range RaiseCost Amount$ must not wrap negative and make the spell castable with an empty pool")
	}
}

// TestReduceCostAmountOverflowDoesNotIncreaseCost is the reviewer's second
// case: the same out-of-range value on a ReduceCost must not invert into a
// cost increase.
func TestReduceCostAmountOverflowDoesNotIncreaseCost(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	discount := card(t, "Name:Discount\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ ReduceCost | ValidCard$ Instant | Amount$ 3000000000\nOracle:x\n")
	e := handEngine(t, zap)
	do := e.G.AddObject(discount, 0)
	do.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{do.ID})

	id := e.G.Zone(state.ZHand, 0)[0]
	c := e.adjustedCost(0, id)
	if c.Generic != 0 {
		t.Fatalf("generic = %d, want 0: an out-of-range Amount$ should fall back to the default reduction, not inflate the cost", c.Generic)
	}
}

// TestReduceCostNegativeAmountDoesNotBecomeARaise is the reviewer's third
// case: a plain negative Amount$ on a ReduceCost must apply NO reduction and
// never flip into a cost increase — modAmount degrades an unresolvable or
// negative amount to zero, the honest read of an amount the engine cannot
// evaluate (the old parseAmount(...,1) fallback is gone with the rv2c task).
func TestReduceCostNegativeAmountDoesNotBecomeARaise(t *testing.T) {
	t.Parallel()
	zap := card(t, "Name:Zap\nManaCost:1\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	discount := card(t, "Name:Discount\nManaCost:1\nTypes:Artifact\n"+
		"S:Mode$ ReduceCost | ValidCard$ Instant | Amount$ -5\nOracle:x\n")
	e := handEngine(t, zap)
	do := e.G.AddObject(discount, 0)
	do.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{do.ID})

	id := e.G.Zone(state.ZHand, 0)[0]
	c := e.adjustedCost(0, id)
	if c.Generic != 1 {
		t.Fatalf("generic = %d, want 1: a negative Amount$ applies no reduction at all (never a raise, never a phantom discount)", c.Generic)
	}
}
