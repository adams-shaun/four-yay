package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// Task cost-xmin1: Forge's XMin<N> cost token -- the announced-X LOWER BOUND
// ("X can't be 0", XMin1) -- was not modelled by ParseCost. It reached the
// unrecognised-symbol fallback, which charged one phantom generic pip AND
// reported a cost:XMin1 census label, and the X ask then offered X = 0 for a
// card whose text forbids it. The bound now parses to Cost.XMin (no generic,
// no Unknown), Plus takes the max of two bounds, the offer gate prices the
// cost at its smallest legal X, and xAsk starts its option list there.

// TestParseCostModelsXMinLowerBound pins the parse itself, at the exact
// shapes the corpus carries: the phantom generic pip must be gone and the
// bound must land on Cost.XMin. A malformed instance (an overflowing N)
// still takes the ordinary reported one-generic fallback.
func TestParseCostModelsXMinLowerBound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cost                    string
		wantGeneric             int32
		wantX                   int
		wantXMin                int32
		wantColored             [6]int32
		wantUnknownIsEmpty      bool
		wantUnknownHeadIfReport string
	}{
		// The reported phantom-pip carrier: {XMin1} {X} {B} is one blue pip
		// and one announced X, no generic at all.
		{cost: "XMin1 X B", wantGeneric: 0, wantX: 1, wantXMin: 1,
			wantColored: [6]int32{0, 0, 1, 0, 0, 0}, wantUnknownIsEmpty: true},
		{cost: "XMin1 X", wantGeneric: 0, wantX: 1, wantXMin: 1, wantUnknownIsEmpty: true},
		{cost: "XMin1", wantGeneric: 0, wantX: 0, wantXMin: 1, wantUnknownIsEmpty: true},
		// The grammar is XMin<N>, not a literal XMin1: the two XMin4
		// carriers (Ore-Rich Stalactite, The Enigma Jewel) must bound at 4.
		{cost: "XMin4", wantGeneric: 0, wantX: 0, wantXMin: 4, wantUnknownIsEmpty: true},
		// The contrast shape with no bound: the same card's {X} alone is
		// generic-free and reports nothing either (it never did).
		{cost: "X B", wantGeneric: 0, wantX: 1, wantXMin: 0,
			wantColored: [6]int32{0, 0, 1, 0, 0, 0}, wantUnknownIsEmpty: true},
		// A malformed/overflowing instance keeps the safe fallback and
		// reports the recognised head.
		{cost: "XMin99999999999999999999", wantGeneric: 1, wantX: 0, wantXMin: 0,
			wantUnknownHeadIfReport: "XMin99999999999999999999"},
	}
	for _, tc := range cases {
		c := ParseCost(tc.cost)
		if c.Generic != tc.wantGeneric {
			t.Errorf("ParseCost(%q).Generic = %d, want %d", tc.cost, c.Generic, tc.wantGeneric)
		}
		if c.X != tc.wantX {
			t.Errorf("ParseCost(%q).X = %d, want %d", tc.cost, c.X, tc.wantX)
		}
		if c.XMin != tc.wantXMin {
			t.Errorf("ParseCost(%q).XMin = %d, want %d", tc.cost, c.XMin, tc.wantXMin)
		}
		if [6]int32(c.Colored) != tc.wantColored {
			t.Errorf("ParseCost(%q).Colored = %v, want %v", tc.cost, c.Colored, tc.wantColored)
		}
		if tc.wantUnknownIsEmpty && len(c.Unknown) != 0 {
			t.Errorf("ParseCost(%q).Unknown = %v, want empty", tc.cost, c.Unknown)
		}
		if tc.wantUnknownHeadIfReport != "" {
			if len(c.Unknown) != 1 || c.Unknown[0] != tc.wantUnknownHeadIfReport {
				t.Errorf("ParseCost(%q).Unknown = %v, want [%s]", tc.cost, c.Unknown, tc.wantUnknownHeadIfReport)
			}
		}
	}
	// Plus composes bounds by max -- the kicked Thieving Skydiver case, where
	// the printed cost carries none and the Kicker part carries XMin1.
	base := ParseCost("1 U")
	kick := ParseCost("XMin1 X")
	sum := base.Plus(kick)
	if sum.XMin != 1 || sum.X != 1 || sum.Generic != 1 {
		t.Fatalf("Plus XMin composition = %+v, want XMin1 X1 Generic1", sum)
	}
	// WithX consumes the bound, so a later announcement cannot re-charge the
	// floor as generic mana.
	if folded := sum.WithX(1); folded.XMin != 0 || folded.Generic != 2 {
		t.Fatalf("WithX(1) = %+v, want XMin cleared and Generic 2", folded)
	}
}

// TestThievingSkydiverKickedXMinOffersOnlyNonzeroX is the corpus-carrier
// leaf. Thieving Skydiver is {1}{U} with "K:Kicker:XMin1 X" -- "pay an
// additional {X}; X can't be 0". Taken kicked on a {2}{U} pool (exactly
// {1}{U} plus the one generic the minimum X=1 costs), the X announcement must
// offer ONLY X = 1: X = 0 is illegal by the card's own text and would gain
// control of nothing, and the phantom generic pip XMin1 used to charge (which
// would have made {2}{U} unpayable) must be gone. The object's stamped X is
// the announced one.
func TestThievingSkydiverKickedXMinOffersOnlyNonzeroX(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Thieving Skydiver"))
	skydiver := e.G.Zone(state.ZHand, 0)[0]
	if skydiver == 0 {
		t.Fatal("Thieving Skydiver not in seat 0's hand")
	}
	// The Kicker parameter must parse to the bound, not to a phantom pip:
	// without that this test's whole premise (the kicked cost carries an
	// XMin lower bound) is not being exercised.
	kc, ok := kickerCost(e.G.Obj(skydiver).Face())
	if !ok || kc.XMin != 1 || kc.X != 1 || kc.Generic != 0 {
		t.Fatalf("Thieving Skydiver Kicker parse = %+v, ok=%v; want XMin1 X1 Generic0", kc, ok)
	}
	// {2}{U}: the minimum legal kicked price {1}+X(1)+{U}.
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MU] = 1
	castMode(t, e, skydiver, "kicked")
	if e.cast == nil || e.cast.cost.XMin != 1 {
		t.Fatalf("kicked pending cast = %+v, want Cost.XMin 1", e.cast)
	}
	opts := xAskOptions(t, e)
	if len(opts) != 1 || opts[0].Amount != 1 || opts[0].Label != "X = 1" {
		t.Fatalf("kicked XMin1 ask offered %+v, want only X = 1", opts)
	}
	chooseX(t, e, 1)
	o := e.G.Obj(skydiver)
	if o.Zone != state.ZStack {
		t.Fatalf("Thieving Skydiver zone = %s after kicked cast, want stack", o.Zone)
	}
	if o.X != 1 {
		t.Fatalf("stamped X = %d, want the announced 1", o.X)
	}
	if o.CastFlags&state.FlagKicked == 0 {
		t.Fatalf("kicked cast not flagged: %+v", o.CastFlags)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after {1}{U}+X(1) = %v, want empty (no phantom XMin1 pip)", e.G.Players[0].Pool)
	}
}

// TestXMinKickerOfferGateWithholdsUnpayableMinimum is the offer-gate leaf:
// an inline fixture with the same kicker shape as Thieving Skydiver. With
// only {1}{U} the kicked option must be WITHHELD (its smallest legal X=1
// needs {2}{U}); with {2}{U} it must be offered. This pins the gate the
// brief warns would otherwise price X as 0 and offer an illegal X = 0.
func TestXMinKickerOfferGateWithholdsUnpayableMinimum(t *testing.T) {
	t.Parallel()
	// The fixture's own cost is {1}{U}; K:Kicker:XMin1 X with no effect body
	// keeps it a pure cost-shape test (no target ask, no triggered ability).
	src := "Name:Skydive Test\nManaCost:1 U\nTypes:Creature Merfolk Rogue\nPT:2/1\nK:Kicker:XMin1 X\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 41, src)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("fixture not in hand: %+v", o)
	}
	// Precondition: the Kicker parameter really carries XMin1, so the gate
	// below is exercised rather than a cost-less fixture passing vacuously.
	if kc, ok := kickerCost(e.G.Obj(id).Face()); !ok || kc.XMin != 1 {
		t.Fatalf("fixture Kicker parse = %+v, ok=%v; want XMin1", kc, ok)
	}
	addMana(t, e, 0, "U")
	addMana(t, e, 0, "C")
	var modes []string
	for _, o := range castOptions(t, e) {
		modes = append(modes, o.Mode)
	}
	for _, m := range modes {
		if m == "kicked" {
			t.Fatalf("kicked offered on {1}{U} (modes %v): its minimum X=1 needs {2}{U}", modes)
		}
	}
	// The plain cast is payable at {1}{U} and must remain offered, so the
	// absence above is the gate withholding the kicked option, not an empty
	// option list.
	plain := false
	for _, m := range modes {
		if m == "" {
			plain = true
		}
	}
	if !plain {
		t.Fatalf("plain cast not offered on {1}{U}: modes %v", modes)
	}
	addMana(t, e, 0, "C")
	kicked := false
	for _, o := range castOptions(t, e) {
		if o.Mode == "kicked" {
			kicked = true
		}
	}
	if !kicked {
		t.Fatal("kicked not offered on {2}{U}, where its minimum X=1 is payable")
	}
}
