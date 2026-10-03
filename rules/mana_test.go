package rules

import (
	"math"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func pool(w, u, b, r, g, c int32) state.Mana { return state.Mana{w, u, b, r, g, c} }

func TestParseCostForms(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]Cost{
		"R":       {Colored: pool(0, 0, 0, 1, 0, 0)},
		"2 U U":   {Colored: pool(0, 2, 0, 0, 0, 0), Generic: 2},
		"1 W":     {Colored: pool(1, 0, 0, 0, 0, 0), Generic: 1},
		"{2}{R}":  {Colored: pool(0, 0, 0, 1, 0, 0), Generic: 2},
		"no cost": {},
		"":        {},
		"X R":     {Colored: pool(0, 0, 0, 1, 0, 0), X: 1},
		"C":       {Colored: pool(0, 0, 0, 0, 0, 1)},
	} {
		got := ParseCost(src)
		if got.Colored != want.Colored || got.Generic != want.Generic || got.X != want.X {
			t.Errorf("ParseCost(%q) = %+v, want %+v", src, got, want)
		}
	}
}

// TestParseCostCleansRealSacSpec is the Ruling FL-54 addendum's regression
// test, built on a REAL corpus string (found via grep of .cards/cardsfolder):
//
//	A:AB$ Draw | Cost$ 2 B Sac<1/Artifact;Creature/artifact or creature>
//
// The OLD nonManaCost regexp captured the whole "Artifact;Creature/artifact
// or creature" as the Spec, so MatchesSpec saw a spec carrying a trailing
// "/description" and a ";" OR alternation it has no way to match -- the
// cost could never be paid and the cast was silently witheld. The parse must
// strip the trailing "/description" and fold the ";" alternation into the
// "," MatchesSpec already understands.
func TestParseCostCleansRealSacSpec(t *testing.T) {
	t.Parallel()
	c := ParseCost("2 B Sac<1/Artifact;Creature/artifact or creature>")
	if len(c.Sac) != 1 || c.Sac[0].N != 1 || c.Sac[0].Spec != "Artifact,Creature" {
		t.Fatalf("Sac = %+v, want N=1 Spec=Artifact,Creature", c.Sac)
	}
	if c.Colored[state.MB] != 1 || c.Generic != 2 {
		t.Fatalf("mana = %+v, want 1 black + 2 generic", c.Colored)
	}
}

// TestParseCostClampsAbsurdGeneric is Task 20's hygiene guard: ParseCost sums
// numeric tokens into Cost.Generic as raw int32, so two legitimate-per-token
// but collectively overflowing values wrapped to a negative total. The sum
// must be clamped at math.MaxInt32 across all tokens.
func TestParseCostLifeCosts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		life int32
	}{
		{name: "one", src: "T PayLife<1> Sac<1/CARDNAME>", life: 1},
		{name: "two", src: "PayLife<2>", life: 2},
		{name: "fifty", src: "PayLife<50>", life: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := ParseCost(tc.src)
			if c.Life != tc.life || c.Generic != 0 {
				t.Fatalf("ParseCost(%q) = %+v, want Life=%d Generic=0", tc.src, c, tc.life)
			}
		})
	}

	// Only a fixed decimal amount is understood. This malformed token must
	// retain ParseCost's long-standing unrecognised-symbol fallback.
	if c := ParseCost("PayLife<garbage>"); c.Life != 0 || c.Generic != 1 {
		t.Fatalf("malformed PayLife token = %+v, want Life=0 Generic=1", c)
	}
}

func TestParseCostClampsAbsurdGeneric(t *testing.T) {
	t.Parallel()
	if c := ParseCost("2147483647 2147483647"); c.Generic != math.MaxInt32 {
		t.Fatalf("generic %d", c.Generic)
	}
}

func TestCMCCountsColoredAndGeneric(t *testing.T) {
	t.Parallel()
	if got := ParseCost("2 U U").CMC(); got != 4 {
		t.Errorf("CMC = %d, want 4", got)
	}
	if got := ParseCost("no cost").CMC(); got != 0 {
		t.Errorf("CMC = %d, want 0", got)
	}
}

func TestLifeCostPayability(t *testing.T) {
	t.Parallel()
	c := ParseCost("PayLife<1>")
	if !payable(c, state.Mana{}, state.Mana{}, [7]state.Mana{}, 1) {
		t.Fatal("one life should pay PayLife<1> without mana")
	}
	if payable(c, state.Mana{}, state.Mana{}, [7]state.Mana{}, 0) {
		t.Fatal("zero life must not pay PayLife<1>")
	}
	if poolCanPay(c, state.Mana{}) {
		t.Fatal("pool-only CanPay must not claim a life cost is mana-payable")
	}
	if !c.Priceable() {
		t.Fatal("payMana must price a fixed life cost against the payer's life")
	}
}

func TestCanPayRequiresTheRightColors(t *testing.T) {
	t.Parallel()
	c := ParseCost("1 R")
	if !poolCanPay(c, pool(0, 0, 0, 1, 0, 1)) {
		t.Error("R + C should pay {1}{R}")
	}
	if poolCanPay(c, pool(1, 1, 0, 0, 0, 0)) {
		t.Error("W + U must not pay {1}{R}")
	}
	if poolCanPay(c, pool(0, 0, 0, 1, 0, 0)) {
		t.Error("a single R must not pay {1}{R}")
	}
}

// Generic cost must not consume mana the coloured requirement still needs.
func TestPaySpendsGenericLast(t *testing.T) {
	t.Parallel()
	c := ParseCost("1 R R")
	after, ok := poolPay(c, pool(0, 0, 0, 3, 0, 0))
	if !ok {
		t.Fatal("RRR should pay {1}{R}{R}")
	}
	if after.Total() != 0 {
		t.Fatalf("pool after = %v, want empty", after)
	}

	after, ok = poolPay(c, pool(1, 0, 0, 2, 0, 0))
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
	after, ok := poolPay(ParseCost("2 R"), before)
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
	gwCost := ParseCost("1 GW")
	if gwCost.Generic != 1 || len(gwCost.Hybrid) != 1 || gwCost.Hybrid[0] != (ManaPair{A: 'G', B: 'W'}) || gwCost.Colored.Total() != 0 {
		t.Errorf("ParseCost(\"1 GW\") = %+v, want Generic=1 + one G/W hybrid", gwCost)
	}
	// A hybrid is payable by either of its colours, never by a third colour
	// nor by colourless alone (CR 107.4e).
	if !poolCanPay(gwCost, pool(0, 0, 0, 0, 2, 0)) {
		t.Error("GW cost should be payable by GG")
	}
	if !poolCanPay(gwCost, pool(2, 0, 0, 0, 0, 0)) {
		t.Error("GW cost should be payable by WW")
	}
	if poolCanPay(gwCost, pool(0, 0, 2, 0, 0, 0)) {
		t.Error("GW cost must not be payable by BB")
	}
	if poolCanPay(gwCost, pool(0, 0, 0, 0, 0, 2)) {
		t.Error("GW cost must not be payable by CC alone")
	}

	// Forge spells monocolour hybrid as "2B" (Beseech the Queen). It is a
	// REAL alternative payment since the rv2c cost-modifier task removed the
	// flatten-to-generic stand-in: one pip, payable by two generic mana or by
	// one black (CR 107.4e).
	monoCost := ParseCost("2B")
	if len(monoCost.Twobrid) != 1 || monoCost.Twobrid[0] != (Twobrid{Generic: 2, Col: 'B'}) || monoCost.Generic != 0 {
		t.Errorf("ParseCost(\"2B\") = %+v, want one 2/B monocolour hybrid", monoCost)
	}
	if !poolCanPay(monoCost, pool(0, 0, 2, 0, 0, 0)) {
		t.Error("2B should be payable by BB")
	}
	if !payable(monoCost, pool(0, 0, 0, 0, 0, 2), state.Mana{}, [7]state.Mana{}, 0) {
		t.Error("2B should be payable by two generic mana")
	}

	// Forge rarely spells hybrid as "W/U" (one card out of 33,669).
	slashCost := ParseCost("W/U")
	if slashCost.Generic != 0 || len(slashCost.Hybrid) != 1 || slashCost.Hybrid[0] != (ManaPair{A: 'W', B: 'U'}) {
		t.Errorf("ParseCost(\"W/U\") = %+v, want one W/U hybrid", slashCost)
	}
	if !poolCanPay(slashCost, pool(0, 1, 0, 0, 0, 0)) {
		t.Error("W/U hybrid should be payable by U")
	}

	// Phyrexian mana (UP, BP). Dismember is "ManaCost:1 BP BP".
	dismemberCost := ParseCost("1 BP BP")
	if dismemberCost.Generic != 1 || len(dismemberCost.Phyrexian) != 2 || dismemberCost.Colored.Total() != 0 {
		t.Errorf("ParseCost(\"1 BP BP\") = %+v, want Generic=1 + two black Phyrexian pips", dismemberCost)
	}
	// Pool-only CanPay offers no life, so a Phyrexian pip needs its colour.
	if !poolCanPay(dismemberCost, pool(0, 0, 3, 0, 0, 0)) {
		t.Error("Dismember should be payable by BBB")
	}
	if poolCanPay(dismemberCost, pool(0, 0, 0, 3, 0, 0)) {
		t.Error("Dismember must not be pool-payable by RRR without life")
	}
	// With life offered, RRR plus four life pays Dismember (CR 107.4f).
	if !payable(dismemberCost, pool(0, 0, 0, 3, 0, 0), state.Mana{}, [7]state.Mana{}, 20) {
		t.Error("Dismember should be payable by RRR with life")
	}

	// Gitaxian Probe is "ManaCost:UP".
	probeCost := ParseCost("UP")
	if probeCost.Generic != 0 || len(probeCost.Phyrexian) != 1 || probeCost.Phyrexian[0] != 'U' {
		t.Errorf("ParseCost(\"UP\") = %+v, want one blue Phyrexian pip", probeCost)
	}
	if poolCanPay(probeCost, pool(0, 0, 0, 0, 1, 0)) {
		t.Error("Gitaxian Probe must not be pool-payable by G alone without life")
	}
	if !payable(probeCost, pool(0, 0, 0, 0, 1, 0), state.Mana{}, [7]state.Mana{}, 20) {
		t.Error("Gitaxian Probe should be payable by G with life")
	}
}

// This test pins the known numeric validation: negative and out-of-range
// numeric tokens are treated as unrecognized symbols and contribute +1 generic.
func TestNumericTokenValidation(t *testing.T) {
	t.Parallel()
	// Negative tokens should fall through to +1 generic.
	negCost := ParseCost("-1")
	if negCost.Generic != 1 || negCost.Colored.Total() != 0 {
		t.Errorf("ParseCost(\"-1\") = %+v, want generic=1", negCost)
	}

	// Tokens above int32 max should fall through to +1 generic.
	// int32 max is 2147483647.
	overflowCost := ParseCost("2147483648")
	if overflowCost.Generic != 1 || overflowCost.Colored.Total() != 0 {
		t.Errorf("ParseCost(\"2147483648\") = %+v, want generic=1", overflowCost)
	}

	// Tokens above int64 max should fall through to +1 generic.
	// int64 max is 9223372036854775807.
	largeOverflowCost := ParseCost("9223372036854775808")
	if largeOverflowCost.Generic != 1 || largeOverflowCost.Colored.Total() != 0 {
		t.Errorf("ParseCost(\"9223372036854775808\") = %+v, want generic=1", largeOverflowCost)
	}

	// Valid boundary: int32 max should parse correctly.
	validMaxCost := ParseCost("2147483647")
	if validMaxCost.Generic != 2147483647 || validMaxCost.Colored.Total() != 0 {
		t.Errorf("ParseCost(\"2147483647\") = %+v, want generic=2147483647", validMaxCost)
	}
}

func TestParseCostNonManaParts(t *testing.T) {
	t.Parallel()
	c := ParseCost("2 C Sac<1/Land>")
	if c.Generic != 2 || c.Colored[state.MC] != 1 || len(c.Sac) != 1 || c.Sac[0] != (CostPart{N: 1, Spec: "Land"}) {
		t.Fatalf("%+v", c)
	}
	c = ParseCost("SubCounter<2/P1P1>")
	if c.CMC() != 0 || len(c.SubCounter) != 1 || c.SubCounter[0] != (CostPart{N: 2, Spec: "P1P1"}) || !c.HasNonMana() {
		t.Fatalf("%+v", c)
	}
	c = ParseCost("Sac<1/CARDNAME> Discard<0/Hand> Discard<2/Card.nonLand/nonland cards>")
	if c.Generic != 0 || len(c.Discard) != 2 || c.Discard[0] != (CostPart{N: 0, Spec: "Hand"}) || c.Discard[1] != (CostPart{N: 2, Spec: "Card.nonLand", Desc: "nonland cards"}) || !c.HasNonMana() {
		t.Fatalf("discard cost parsed as %+v", c)
	}
	c = ParseCost("T")
	if !c.Tap || c.CMC() != 0 {
		t.Fatalf("%+v", c)
	}
	c = ParseCost("X X W W W")
	if c.X != 2 || c.Colored[state.MW] != 3 || c.CMC() != 3 {
		t.Fatalf("%+v", c)
	}
	if w := c.WithX(2); w.X != 0 || w.Generic != 4 || w.CMC() != 7 {
		t.Fatalf("WithX %+v", w)
	}
	if p := ParseCost("R").Plus(ParseCost("R")); p.Colored[state.MR] != 2 {
		t.Fatalf("Plus %+v", p)
	}
	if ParseCost("3 U").HasNonMana() {
		t.Fatal("mana-only cost reports non-mana parts")
	}
}
