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
