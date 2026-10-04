package pay

import (
	"testing"

	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

func pool(w, u, b, r, g, c int32) state.Mana { return state.Mana{w, u, b, r, g, c} }

func TestLifeCostPayability(t *testing.T) {
	t.Parallel()
	c := cost.ParseCost("PayLife<1>")
	if !Payable(c, state.Mana{}, state.Mana{}, [7]state.Mana{}, 1) {
		t.Fatal("one life should pay PayLife<1> without mana")
	}
	if Payable(c, state.Mana{}, state.Mana{}, [7]state.Mana{}, 0) {
		t.Fatal("zero life must not pay PayLife<1>")
	}
	if PoolCanPay(c, state.Mana{}) {
		t.Fatal("pool-only CanPay must not claim a life cost is mana-payable")
	}
	if !c.Priceable() {
		t.Fatal("payMana must price a fixed life cost against the payer's life")
	}
}

func TestCanPayRequiresTheRightColors(t *testing.T) {
	t.Parallel()
	c := cost.ParseCost("1 R")
	if !PoolCanPay(c, pool(0, 0, 0, 1, 0, 1)) {
		t.Error("R + C should pay {1}{R}")
	}
	if PoolCanPay(c, pool(1, 1, 0, 0, 0, 0)) {
		t.Error("W + U must not pay {1}{R}")
	}
	if PoolCanPay(c, pool(0, 0, 0, 1, 0, 0)) {
		t.Error("a single R must not pay {1}{R}")
	}
}

// Generic cost must not consume mana the coloured requirement still needs.
func TestPaySpendsGenericLast(t *testing.T) {
	t.Parallel()
	c := cost.ParseCost("1 R R")
	after, ok := PoolPay(c, pool(0, 0, 0, 3, 0, 0))
	if !ok {
		t.Fatal("RRR should pay {1}{R}{R}")
	}
	if after.Total() != 0 {
		t.Fatalf("pool after = %v, want empty", after)
	}

	after, ok = PoolPay(c, pool(1, 0, 0, 2, 0, 0))
	if !ok {
		t.Fatal("W + RR should pay {1}{R}{R}")
	}
	if after[state.MR] != 0 || after[state.MW] != 0 {
		t.Fatalf("pool after = %v, want empty", after)
	}
}

func TestPayFailsCleanly(t *testing.T) {
	t.Parallel()
	before := pool(0, 0, 0, 1, 0, 0)
	after, ok := PoolPay(cost.ParseCost("2 R"), before)
	if ok {
		t.Fatal("insufficient mana was accepted")
	}
	if after != before {
		t.Fatal("a failed payment must not mutate the pool")
	}
}

// TestHybridAndPhyrexianAlternativePayments pins the CR 107.4e / 107.4f
// alternative-payment representation that replaced the old M1 approximation
// (which flattened a hybrid and a Phyrexian symbol to one generic each and so
// mispriced Dismember and Gitaxian Probe, accepting regular mana for a
// Phyrexian pip). A two-colour hybrid is a choice of one of its two colours; a
// Phyrexian pip is its colour or two life. The over-permissive acceptance this
// test used to document is exactly the defect the CR 601.2b/107.4e-f leaves
// measure, so the corrected assertions below replace it.
func TestHybridAndPhyrexianAlternativePayments(t *testing.T) {
	t.Parallel()
	// Forge spells colour hybrid as "GW" (Kitchen Finks), "RW" (Figure of Destiny).
	gwCost := cost.ParseCost("1 GW")
	if gwCost.Generic != 1 || len(gwCost.Hybrid) != 1 || gwCost.Hybrid[0] != (cost.ManaPair{A: 'G', B: 'W'}) || gwCost.Colored.Total() != 0 {
		t.Errorf("cost.ParseCost(\"1 GW\") = %+v, want Generic=1 + one G/W hybrid", gwCost)
	}
	// A hybrid is payable by either of its colours, never by a third colour
	// nor by colourless alone (CR 107.4e).
	if !PoolCanPay(gwCost, pool(0, 0, 0, 0, 2, 0)) {
		t.Error("GW cost should be payable by GG")
	}
	if !PoolCanPay(gwCost, pool(2, 0, 0, 0, 0, 0)) {
		t.Error("GW cost should be payable by WW")
	}
	if PoolCanPay(gwCost, pool(0, 0, 2, 0, 0, 0)) {
		t.Error("GW cost must not be payable by BB")
	}
	if PoolCanPay(gwCost, pool(0, 0, 0, 0, 0, 2)) {
		t.Error("GW cost must not be payable by CC alone")
	}

	// Forge spells monocolour hybrid as "2B" (Beseech the Queen). It is a
	// REAL alternative payment since the rv2c cost-modifier task removed the
	// flatten-to-generic stand-in: one pip, payable by two generic mana or by
	// one black (CR 107.4e).
	monoCost := cost.ParseCost("2B")
	if len(monoCost.Twobrid) != 1 || monoCost.Twobrid[0] != (cost.Twobrid{Generic: 2, Col: 'B'}) || monoCost.Generic != 0 {
		t.Errorf("cost.ParseCost(\"2B\") = %+v, want one 2/B monocolour hybrid", monoCost)
	}
	if !PoolCanPay(monoCost, pool(0, 0, 2, 0, 0, 0)) {
		t.Error("2B should be payable by BB")
	}
	if !Payable(monoCost, pool(0, 0, 0, 0, 0, 2), state.Mana{}, [7]state.Mana{}, 0) {
		t.Error("2B should be payable by two generic mana")
	}

	// Forge rarely spells hybrid as "W/U" (one card out of 33,669).
	slashCost := cost.ParseCost("W/U")
	if slashCost.Generic != 0 || len(slashCost.Hybrid) != 1 || slashCost.Hybrid[0] != (cost.ManaPair{A: 'W', B: 'U'}) {
		t.Errorf("cost.ParseCost(\"W/U\") = %+v, want one W/U hybrid", slashCost)
	}
	if !PoolCanPay(slashCost, pool(0, 1, 0, 0, 0, 0)) {
		t.Error("W/U hybrid should be payable by U")
	}

	// Phyrexian mana (UP, BP). Dismember is "ManaCost:1 BP BP".
	dismemberCost := cost.ParseCost("1 BP BP")
	if dismemberCost.Generic != 1 || len(dismemberCost.Phyrexian) != 2 || dismemberCost.Colored.Total() != 0 {
		t.Errorf("cost.ParseCost(\"1 BP BP\") = %+v, want Generic=1 + two black Phyrexian pips", dismemberCost)
	}
	// Pool-only CanPay offers no life, so a Phyrexian pip needs its colour.
	if !PoolCanPay(dismemberCost, pool(0, 0, 3, 0, 0, 0)) {
		t.Error("Dismember should be payable by BBB")
	}
	if PoolCanPay(dismemberCost, pool(0, 0, 0, 3, 0, 0)) {
		t.Error("Dismember must not be pool-payable by RRR without life")
	}
	// With life offered, RRR plus four life pays Dismember (CR 107.4f).
	if !Payable(dismemberCost, pool(0, 0, 0, 3, 0, 0), state.Mana{}, [7]state.Mana{}, 20) {
		t.Error("Dismember should be payable by RRR with life")
	}

	// Gitaxian Probe is "ManaCost:UP".
	probeCost := cost.ParseCost("UP")
	if probeCost.Generic != 0 || len(probeCost.Phyrexian) != 1 || probeCost.Phyrexian[0] != 'U' {
		t.Errorf("cost.ParseCost(\"UP\") = %+v, want one blue Phyrexian pip", probeCost)
	}
	if PoolCanPay(probeCost, pool(0, 0, 0, 0, 1, 0)) {
		t.Error("Gitaxian Probe must not be pool-payable by G alone without life")
	}
	if !Payable(probeCost, pool(0, 0, 0, 0, 1, 0), state.Mana{}, [7]state.Mana{}, 20) {
		t.Error("Gitaxian Probe should be payable by G with life")
	}
}

// This test pins the known numeric validation: negative and out-of-range
// numeric tokens are treated as unrecognized symbols and contribute +1 generic.
