package effects

import (
	"math"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestNumReadsLiterals(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{}
	if got := Num(h, c, sa(t, "SP$ DealDamage | NumDmg$ 3"), "NumDmg", 1); got != 3 {
		t.Errorf("NumDmg = %d", got)
	}
	if got := Num(h, c, sa(t, "SP$ DealDamage"), "NumDmg", 7); got != 7 {
		t.Errorf("missing key should return the default, got %d", got)
	}
	if got := Num(h, c, sa(t, "SP$ DealDamage | NumDmg$ -2"), "NumDmg", 0); got != -2 {
		t.Errorf("negative literal = %d", got)
	}
}

func TestNumFollowsSVarIndirection(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{SVars: map[string]string{"Y": "Count$ThisIsNotReal"}}
	// An SVar naming an unmodelled Count$ head resolves to zero, not garbage.
	if got := Num(h, c, sa(t, "SP$ Draw | NumCards$ Y"), "NumCards", 9); got != 0 {
		t.Errorf("unknown Count$ head = %d, want 0", got)
	}
	c.SVars["Z"] = "Count$xPaid"
	c.X = 4
	if got := Num(h, c, sa(t, "SP$ Draw | NumCards$ Z"), "NumCards", 0); got != 4 {
		t.Errorf("Count$xPaid = %d, want 4", got)
	}
}

// TestNumEvaluatesInlineCountExpression locks in Task 17's Num extension:
// Storm's expansion puts Count$ThisTurnCast/Minus1 inline in the Amount$
// param (cards/keywords.go), not behind an SVar name -- before the extension
// Num mistook that literal for an SVar lookup, failed it, and judged the
// whole amount zero, silencing every Storm copy.
func TestNumEvaluatesInlineCountExpression(t *testing.T) {
	h, c := fixtureHost(t)
	got := Num(h, c, sa(t, "SP$ Draw | Amount$ Count$PlayerCountPlayers/Minus1"), "Amount", 9)
	if got != 1 {
		t.Errorf("Num inline Count$ = %d, want 1 (2 players minus 1)", got)
	}
}

func TestEvalCountPlusResolvesSVarOperand(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, SVars: map[string]string{
		"Controlled": "Count$Valid Creature.YouCtrl",
	}}
	// The board fixture has two creatures controlled by seat 0. The operand
	// must be evaluated as an SVar, not discarded as a non-numeric suffix.
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl/Plus.Controlled"); got != 4 {
		t.Fatalf("Count$.../Plus.Controlled = %d, want 4", got)
	}
}

func TestEvalCountValidCountsTheBattlefield(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	if got := EvalCount(h, c, "Count$Valid Creature"); got != 3 {
		t.Errorf("Valid Creature = %d, want 3", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl"); got != 2 {
		t.Errorf("Valid Creature.YouCtrl = %d, want 2", got)
	}
	if got := EvalCount(h, c, "Count$Valid Land.YouCtrl"); got != 1 {
		t.Errorf("Valid Land.YouCtrl = %d, want 1", got)
	}
}

func TestEvalCountValidSumsAPropertySuffix(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	// You control the Bear (2/2, MV 2), the Flier (1/1, MV 2) and the
	// Giant (5/5, MV 5) is the opponent's: the sum property suffix sums
	// over the MATCHES only.
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl$CardPower"); got != 3 {
		t.Errorf("Creature.YouCtrl$CardPower = %d, want 3", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl$CardToughness"); got != 3 {
		t.Errorf("Creature.YouCtrl$CardToughness = %d, want 3", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature$CardManaCost"); got != 9 {
		t.Errorf("Creature$CardManaCost = %d, want 9 (Bear+Giant+Flier: 2+5+2)", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl$CardManaCost"); got != 4 {
		t.Errorf("Creature.YouCtrl$CardManaCost = %d, want 4", got)
	}
	// The Different* distinct-set properties ARE read since diffcount1:
	// distinct DERIVED powers among the matches -- Bear 2, Flier 1, Giant
	// 5 -- is 3, not a sum and not a fail-closed zero.
	if got := EvalCount(h, c, "Count$Valid Creature$DifferentCardPower"); got != 3 {
		t.Errorf("DifferentCardPower token = %d, want 3 (distinct powers 2,1,5)", got)
	}
}

// TestEvalCountValidExtremeProperties pins the four extreme-reduction
// property suffixes: the MAXIMUM (Greatest*) or MINIMUM (Least*) of the
// property over the matches, not a sum. Several different values, because
// the defect being fixed is a silent zero and a single-value test can pass
// by coincidence.
func TestEvalCountValidExtremeProperties(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	// Board: controller 0 has Bear 2/2 (MV 2) and Flier 1/1 (MV 2);
	// controller 1 has the Giant 5/5 (MV 5).
	cases := []struct {
		expr string
		want int32
	}{
		{"Count$Valid Creature$GreatestCardPower", 5},
		{"Count$Valid Creature.YouCtrl$GreatestCardPower", 2},
		{"Count$Valid Creature.YouCtrl$LeastCardPower", 1},
		{"Count$Valid Creature$LeastCardPower", 1},
		{"Count$Valid Creature$GreatestCardToughness", 5},
		{"Count$Valid Creature.YouCtrl$GreatestCardToughness", 2},
		// Bear MV 2, Giant MV 5: the greatest mana value, not the sum (9).
		{"Count$Valid Creature$GreatestCardManaCost", 5},
		{"Count$Valid Creature.YouCtrl$GreatestCardManaCost", 2},
		// Zero matches -> 0, never an int-min/max sentinel.
		{"Count$Valid Creature.YouCtrl+NonExistent$GreatestCardPower", 0},
		{"Count$Valid Creature.YouCtrl+NonExistent$LeastCardPower", 0},
	}
	for _, tc := range cases {
		if got := EvalCount(h, c, tc.expr); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, got, tc.want)
		}
	}
	// A second board strength so a hard-coded 5 cannot pass both: the
	// DERIVED read must follow +1/+1 counters, not the printed face.
	for _, id := range g.Zone(state.ZBattlefield, 0) {
		if o := g.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Bear" {
			o.AddCounter("P1P1", 5)
		}
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl$GreatestCardPower"); got != 7 {
		t.Errorf("Creature.YouCtrl$GreatestCardPower after counters = %d, want 7", got)
	}
}

func TestEvalCountValidCountsDistinctColors(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	// Controller 0's board: Bear (G), Flier (U), Aura (W), Walker (U),
	// Siege (R) plus a colourless Mountain and a colourless Relic; the
	// Giant (R) is the opponent's. DISTINCT colours among the matches,
	// not a sum over permanents: 4 (W,U,R,G), the Walker's second U
	// counted once.
	if got := EvalCount(h, c, "Count$Valid Permanent.YouCtrl$Colors"); got != 4 {
		t.Errorf("Permanent.YouCtrl$Colors = %d, want 4", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl$Colors"); got != 2 {
		t.Errorf("Creature.YouCtrl$Colors = %d, want 2 (G+U)", got)
	}
	// The opponent's permanents never enter a YouCtrl count (their Giant
	// is also R, so an unscoping bug would still show 4).
	if got := EvalCount(h, c, "Count$Valid Permanent.YouCtrl$Colors"); got != 4 {
		t.Errorf("re-read Permanent.YouCtrl$Colors = %d, want 4", got)
	}
	// A spec matching only colourless permanents counts zero colours.
	if got := EvalCount(h, c, "Count$Valid Land$Colors"); got != 0 {
		t.Errorf("Land$Colors = %d, want 0 (colourless Mountain)", got)
	}
	// A compound spec still matches; the Legendary Walker alone is U.
	if got := EvalCount(h, c, "Count$Valid Permanent.YouCtrl+Legendary$Colors"); got != 1 {
		t.Errorf("Permanent.YouCtrl+Legendary$Colors = %d, want 1 (U)", got)
	}
	// The corpus's one op suffix (happily_ever_after) parses and does not
	// bind at 5 on a four-colour board...
	if got := EvalCount(h, c, "Count$Valid Permanent.YouCtrl$Colors/LimitMax.5"); got != 4 {
		t.Errorf("Colors/LimitMax.5 = %d, want 4", got)
	}
	// ...and clamps when it does bind.
	if got := EvalCount(h, c, "Count$Valid Permanent.YouCtrl$Colors/LimitMax.2"); got != 2 {
		t.Errorf("Colors/LimitMax.2 = %d, want 2 (clamped)", got)
	}
	if got := EvalCount(h, c, "Count$Valid Permanent.YouCtrl$Colors/Bogus.3"); got != 4 {
		t.Errorf("Colors/Bogus.3 = %d, want 4 (unknown op ignored, plain Colors)", got)
	}
	// ...and the Different* family reads through the same suffix cut:
	// distinct powers 2 (Bear), 1 (Flier), 5 (Giant) = 3.
	if got := EvalCount(h, c, "Count$Valid Creature$DifferentCardPower"); got != 3 {
		t.Errorf("DifferentCardPower token = %d, want 3 (distinct powers 2,1,5)", got)
	}
	// An UNKNOWN Different* spelling outside the family still keeps the
	// whole-token fail-closed read.
	if got := EvalCount(h, c, "Count$Valid Creature$DifferentBogus"); got != 0 {
		t.Errorf("DifferentBogus token = %d, want 0 (out of scope, fail closed)", got)
	}
}

func TestEvalCountZoneScopedForms(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	// Move a creature to the graveyard and one to hand.
	moveTo(g, ids["myBear"], state.ZGraveyard)
	moveTo(g, ids["myFlier"], state.ZHand)
	c := &Ctx{Controller: 0}
	if got := EvalCount(h, c, "Count$ValidGraveyard Creature.YouOwn"); got != 1 {
		t.Errorf("graveyard creatures = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Count$ValidHand Card.YouOwn"); got != 1 {
		t.Errorf("hand cards = %d, want 1", got)
	}
}

// TestEvalCountValidStackScansTheSharedStackOnce pins the ValidStack head
// against the shared-zone double-count: state.Game.Zone returns g.Stack for
// EVERY seat, so the zone-count scan must visit the shared stack exactly
// once, not once per alive seat. On the two-seat fixture below one spell on
// the stack read 2 before the fix and reads 1 after (Mindbreak Trap's
// MaxTgts = the number of spells on the stack; Display of Power's copy
// count). The preconditions are asserted so a vacuous setup fails loudly.
func TestEvalCountValidStackScansTheSharedStackOnce(t *testing.T) {
	h := newHost(t, 2)
	// Precondition: both seats are alive, so a per-seat walk visits two
	// seats and a broken scan would count the single spell twice.
	if alive := h.g.AliveFrom(0); len(alive) != 2 {
		t.Fatalf("fixture precondition: %d alive seats, want 2", len(alive))
	}
	src := h.g.AddObject(mkCard(t, "Name:Spell\nManaCost:1 U\nTypes:Instant\nOracle:x\n"), 0)
	src.Zone = state.ZStack
	h.g.SetZone(state.ZStack, 0, []state.ObjID{src.ID})
	// Precondition: exactly one stack object, and the shared zone returns
	// the same list for both seats (the property under test).
	if got := h.g.Zone(state.ZStack, 0); len(got) != 1 {
		t.Fatalf("fixture precondition: seat 0 stack has %d objects, want 1", len(got))
	}
	if got := h.g.Zone(state.ZStack, 1); len(got) != 1 {
		t.Fatalf("fixture precondition: seat 1 stack has %d objects, want 1", len(got))
	}
	c := &Ctx{Controller: 0}
	if got := EvalCount(h, c, "Count$ValidStack Card"); got != 1 {
		t.Errorf("Count$ValidStack Card = %d, want 1 (one spell, two alive seats)", got)
	}
	if got := EvalCount(h, c, "Count$ValidStack Instant"); got != 1 {
		t.Errorf("Count$ValidStack Instant = %d, want 1", got)
	}
	// A spec that matches nothing must stay 0 through the same shared scan.
	if got := EvalCount(h, c, "Count$ValidStack Creature"); got != 0 {
		t.Errorf("Count$ValidStack Creature = %d, want 0", got)
	}
}

func TestEvalCountPlayerAndLifeForms(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	g.Players[0].Life = 13
	if got := EvalCount(h, c, "Count$YourLifeTotal"); got != 13 {
		t.Errorf("YourLifeTotal = %d", got)
	}
	if got := EvalCount(h, c, "Count$PlayerCountPlayers"); got != 2 {
		t.Errorf("PlayerCountPlayers = %d", got)
	}
	if got := EvalCount(h, c, "Count$PlayerCountOpponents"); got != 1 {
		t.Errorf("PlayerCountOpponents = %d", got)
	}
}

func TestEvalCountArithmeticSuffix(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	// Forge appends ".Plus1", ".Minus1", ".Twice" and similar to a count.
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl/Plus1"); got != 3 {
		t.Errorf("Plus1 = %d, want 3", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl/Minus1"); got != 1 {
		t.Errorf("Minus1 = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Count$Valid Creature.YouCtrl/Twice"); got != 4 {
		t.Errorf("Twice = %d, want 4", got)
	}
}

// TestCountOpsDoNotOverflow is Task 20's hygiene guard for applyCountOp: the
// /Op arithmetic (Plus/Minus/Times/Twice/Negative) was computed in raw int32,
// where MaxInt32 doubled, negated or nudged silently wrapped. It must now run
// in int64 and clamp to the int32 range so a huge Count$ can neither overflow
// to a sign-flipped value nor panic.
func TestCountOpsDoNotOverflow(t *testing.T) {
	if applyCountOp(math.MaxInt32, "Twice") != math.MaxInt32 ||
		applyCountOp(math.MinInt32, "Negative") != math.MaxInt32 ||
		applyCountOp(math.MaxInt32, "Plus5") != math.MaxInt32 {
		t.Fatal("count ops overflow")
	}
}

// TestCountOpsDivisionRoundings is the table guard for Forge's AmountOperators
// division family. Every spelling the corpus carries must divide by its named
// divisor with the right rounding direction; before the Divide arm landed
// DivideEvenlyUp (and bare Divide) fell through applyCountOp untouched, so
// Legate Lanius's "a tenth, rounded up" read as the whole count.
func TestCountOpsDivisionRoundings(t *testing.T) {
	cases := []struct {
		in   int32
		op   string
		want int32
	}{
		// DivideEvenlyUp = ceil (Legate Lanius's real corpus suffix).
		{0, "DivideEvenlyUp.10", 0},
		{1, "DivideEvenlyUp.10", 1},
		{9, "DivideEvenlyUp.10", 1},
		{10, "DivideEvenlyUp.10", 1},
		{11, "DivideEvenlyUp.10", 2},
		{20, "DivideEvenlyUp.10", 2},
		{21, "DivideEvenlyUp.10", 3},
		{7, "DivideEvenlyUp.2", 4},
		// DivideEvenlyDown = floor (the pre-existing arm's real suffixes).
		{0, "DivideEvenlyDown.2", 0},
		{1, "DivideEvenlyDown.2", 0},
		{2, "DivideEvenlyDown.2", 1},
		{3, "DivideEvenlyDown.2", 1},
		{35, "DivideEvenlyDown.5", 7},
		{34, "DivideEvenlyDown.7", 4},
		// DivideEvenly / bare Divide = Forge's default, floor.
		{9, "DivideEvenly.4", 2},
		{8, "DivideEvenly.4", 2},
		{9, "Divide.4", 2},
		{8, "Divide.4", 2},
		// A negative operand floors toward negative infinity, not toward zero
		// (the truncation note the old DivideEvenlyDown arm carried).
		{-3, "DivideEvenlyDown.2", -2},
		{-3, "DivideEvenlyUp.2", -1},
		// A missing / non-numeric / non-positive divisor leaves the value
		// unchanged rather than dividing by zero.
		{7, "DivideEvenlyUp", 7},
		{7, "DivideEvenlyDown.0", 7},
		{7, "DivideEvenlyDown.-2", 7},
		{7, "DivideEvenlyDown.NumOpps", 7},
		// The non-division ops are untouched by the new arm.
		{3, "Plus1", 4},
		{3, "Minus1", 2},
		{3, "Twice", 6},
		{3, "HalfUp", 2},
		{3, "HalfDown", 1},
	}
	for _, tc := range cases {
		if got := applyCountOp(tc.in, tc.op); got != tc.want {
			t.Errorf("applyCountOp(%d, %q) = %d, want %d", tc.in, tc.op, got, tc.want)
		}
	}
}

func TestNumGuardsNilCtx(t *testing.T) {
	h := newHost(t, 2)
	// Nil Ctx should not panic; it's treated as an empty Ctx.
	if got := Num(h, nil, sa(t, "SP$ X | NumDmg$ 5"), "NumDmg", 9); got != 5 {
		t.Errorf("literal with nil Ctx = %d, want 5", got)
	}
	if got := Num(h, nil, sa(t, "SP$ X | NumDmg$ Y"), "NumDmg", 7); got != 0 {
		t.Errorf("SVar reference with nil Ctx = %d, want 0 (default)", got)
	}
}

func TestEvalCountGuardsNilHostAndCtx(t *testing.T) {
	h := newHost(t, 2)
	c := &Ctx{Controller: 0}
	// Nil Host should not panic; it returns the default.
	if got := EvalCount(nil, c, "Count$YourLifeTotal"); got != 0 {
		t.Errorf("nil Host = %d, want 0", got)
	}
	// Nil Ctx should not panic; it returns the default.
	if got := EvalCount(h, nil, "Count$YourLifeTotal"); got != 0 {
		t.Errorf("nil Ctx = %d, want 0", got)
	}
}

func TestEvalCountGuardsOutOfRangeController(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	// Controller out of range (>= len(Players)) should return 0, not panic.
	c := &Ctx{Controller: state.PlayerID(len(g.Players))}
	if got := EvalCount(h, c, "Count$YourLifeTotal"); got != 0 {
		t.Errorf("out-of-range Controller = %d, want 0", got)
	}
	// Large out-of-range Controller should also return 0.
	c.Controller = state.PlayerID(255)
	if got := EvalCount(h, c, "Count$YourLifeTotal"); got != 0 {
		t.Errorf("large out-of-range Controller = %d, want 0", got)
	}
}

func TestSetSVarsCopiesTheMap(t *testing.T) {
	c := &Ctx{}
	original := map[string]string{"A": "Count$xPaid", "B": "Count$YourLifeTotal"}
	SetSVars(c, original)

	// Verify the SVar was copied, not aliased.
	if c.SVars["A"] != "Count$xPaid" {
		t.Errorf("SVars copy failed: A = %q", c.SVars["A"])
	}

	// Mutate the original map after the call.
	original["A"] = "Count$ModifiedAfter"
	original["C"] = "Count$NewEntry"

	// The copy in c.SVars should be unaffected.
	if c.SVars["A"] != "Count$xPaid" {
		t.Errorf("original mutation affected the copy: A = %q", c.SVars["A"])
	}
	if _, ok := c.SVars["C"]; ok {
		t.Errorf("new entry in original appeared in copy")
	}

	// Test that nil input leaves SVars nil (not an empty map).
	c2 := &Ctx{}
	SetSVars(c2, nil)
	if c2.SVars != nil {
		t.Errorf("nil input should leave SVars nil, got %v", c2.SVars)
	}
}

func TestNumSelfReferencingSVar(t *testing.T) {
	h := newHost(t, 2)
	// An SVar that refers to itself should terminate and return 0.
	c := &Ctx{SVars: map[string]string{"Self": "Count$Self"}}
	if got := Num(h, c, sa(t, "SP$ X | NumDmg$ Self"), "NumDmg", 9); got != 0 {
		t.Errorf("self-referencing SVar = %d, want 0", got)
	}
}

func TestNumCyclicSVars(t *testing.T) {
	h := newHost(t, 2)
	// Two SVars referring to each other should terminate and return 0.
	c := &Ctx{SVars: map[string]string{
		"A": "Count$B",
		"B": "Count$A",
	}}
	if got := Num(h, c, sa(t, "SP$ X | NumDmg$ A"), "NumDmg", 9); got != 0 {
		t.Errorf("cyclic SVar A = %d, want 0", got)
	}
	if got := Num(h, c, sa(t, "SP$ X | NumDmg$ B"), "NumDmg", 9); got != 0 {
		t.Errorf("cyclic SVar B = %d, want 0", got)
	}
}

func TestCountCardCountersKickedAndTimes(t *testing.T) {
	h, c := fixtureHost(t)
	src := h.Game().Obj(c.Source)
	src.AddCounter("CHARGE", 3)
	if n := EvalCount(h, c, "Count$CardCounters.CHARGE"); n != 3 {
		t.Fatalf("CardCounters.CHARGE = %d", n)
	}
	if n := EvalCount(h, c, "Count$CardCounters.P1P1"); n != 0 {
		t.Fatalf("absent counter kind = %d", n)
	}
	if n := EvalCount(h, c, "Count$Kicked.4.0"); n != 0 {
		t.Fatalf("not kicked = %d", n)
	}
	src.CastFlags = state.FlagKicked
	if n := EvalCount(h, c, "Count$Kicked.4.0"); n != 4 {
		t.Fatalf("kicked = %d", n)
	}
	if n := EvalCount(h, c, "Count$CardCounters.CHARGE/Times.2"); n != 6 {
		t.Fatalf("Times.2 = %d", n)
	}
}

// moveTo is a test helper: relocate an object without going through events.
func moveTo(g *state.Game, id state.ObjID, z state.Zone) {
	o := g.Obj(id)
	src := g.Zone(o.Zone, o.Owner)
	out := src[:0:0]
	for _, x := range src {
		if x != id {
			out = append(out, x)
		}
	}
	g.SetZone(o.Zone, o.Owner, out)
	g.SetZone(z, o.Owner, append(g.Zone(z, o.Owner), id))
	o.Zone = z
}

var _ = cards.Card{}

// TestPlayerCountExtremePropertiesFailUnresolvable pins the r2 review's
// fail-direction fix on the PlayerCount wrapper: the two life extremes
// (LowestLifeTotal/HighestLifeTotal) and the count/counted-quantity extremes
// (HighestValid/LowestValid over any Count$ zone, plus the
// HighestLifeLostThisTurn pair, and since fuzz-cov3 the per-player zone
// sizes/tallies HighestCardsInHand et al.) are evaluated; ANY other property
// -- e.g. HighestSpeed -- reports (0, false), so a gate over one fails per
// its caller's documented direction instead of silently enforcing a fake
// zero.
func TestPlayerCountExtremePropertiesFailUnresolvable(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	g.Players[0].Life = 13
	g.Players[1].Life = 7
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$LowestLifeTotal"); !ok || got != 7 {
		t.Fatalf("LowestLifeTotal = (%d, %v), want (7, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountPlayers$HighestLifeTotal"); !ok || got != 13 {
		t.Fatalf("HighestLifeTotal = (%d, %v), want (13, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountPlayers$LowestCardsInHand"); !ok || got != int32(min(len(g.Zone(state.ZHand, 0)), len(g.Zone(state.ZHand, 1)))) {
		t.Fatalf("LowestCardsInHand = (%d, %v), want the smaller hand", got, ok)
	}
	for _, body := range []string{
		"Count$PlayerCountOpponents$HighestSpeed",
		"Count$PlayerCountPlayers$LowestCardsExiledThisTurn",
	} {
		if got, ok := EvalCountOK(h, c, body); ok {
			t.Fatalf("%s reported EVALUATED as %d -- an unmodelled property must fail unresolvable, not enforce a fake zero", body, got)
		}
	}
}

// TestPlayerCountGroupAmountHeadCountsTheGroup pins the `Amount` property on
// the two living PlayerCount groups (pfpe1): Forge's property Amount counts 1
// per member, so PlayerCountOpponents$Amount is the opponent count -- the
// "one each" bound the corpus names SVar:OneEach (99 raw Opponents + 52 raw
// Players lines) and the per-player target maximum the TargetsForEachPlayer$
// shape reads (Havoc Eater's TargetMax$ X with
// SVar:X:PlayerCountOpponents$Amount). Any other unmodelled property on the
// same head keeps the fail-closed unresolvable verdict above.
func TestPlayerCountGroupAmountHeadCountsTheGroup(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$Amount"); !ok || got != 1 {
		t.Fatalf("PlayerCountOpponents$Amount = (%d, %v), want (1, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountPlayers$Amount"); !ok || got != 2 {
		t.Fatalf("PlayerCountPlayers$Amount = (%d, %v), want (2, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$Amount/Plus1"); !ok || got != 2 {
		t.Fatalf("PlayerCountOpponents$Amount/Plus1 = (%d, %v), want (2, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$HighestAmount"); ok {
		t.Fatalf("HighestAmount reported EVALUATED as %d -- only the bare Amount property is the group count", got)
	}
}

// TestNumResolvedReadsBareInlineCountBody pins the direct-parameter spelling
// of a bare count body (pfpe1 follow-up): Tolarian Contempt writes its
// TargetsForEachPlayer$ bound INLINE -- TargetMax$ PlayerCountOpponents$
// Amount, no SVar name and no Count$ prefix -- and the Num grammar must
// resolve it exactly as it resolves the SVar-mediated spelling of the same
// body (Havoc Eater's SVar:X:PlayerCountOpponents$Amount behind
// TargetMax$ X). Before the fix the bare inline form fell through every
// recognised shape in NumResolved and returned (0, false), so the bound
// degraded to the default 1 and the "for each opponent" ask collapsed to a
// single target. A token naming no modelled head keeps the unresolvable
// verdict so Num's degrade-to-zero contract is unchanged.
func TestNumResolvedReadsBareInlineCountBody(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	sa := &cards.SA{Params: map[string]string{"TargetMax": "PlayerCountOpponents$Amount"}}
	if n, ok := NumResolved(h, c, sa, "TargetMax", 1); !ok || n != 1 {
		t.Fatalf("bare inline PlayerCountOpponents$Amount = (%d, %v), want (1, true)", n, ok)
	}
	sa2 := &cards.SA{Params: map[string]string{"TargetMax": "PlayerCountPlayers$Amount"}}
	if n, ok := NumResolved(h, c, sa2, "TargetMax", 1); !ok || n != 2 {
		t.Fatalf("bare inline PlayerCountPlayers$Amount = (%d, %v), want (2, true)", n, ok)
	}
	// The SVar-mediated spelling of the same body keeps resolving (the
	// Havoc Eater shape -- this is the before/after control).
	c3 := &Ctx{Controller: 0, SVars: map[string]string{"X": "PlayerCountOpponents$Amount"}}
	sa3 := &cards.SA{Params: map[string]string{"TargetMax": "X"}}
	if n, ok := NumResolved(h, c3, sa3, "TargetMax", 1); !ok || n != 1 {
		t.Fatalf("SVar-mediated PlayerCountOpponents$Amount = (%d, %v), want (1, true)", n, ok)
	}
	// An unmodelled bare token stays unresolvable -- the verdict, not a
	// silent zero, is what resolvedTargetBounds keys on.
	sa4 := &cards.SA{Params: map[string]string{"TargetMax": "MaxTgts$Foo"}}
	if _, ok := NumResolved(h, c, sa4, "TargetMax", 1); ok {
		t.Fatal("MaxTgts$Foo reported evaluated -- an unmodelled head must stay unresolvable")
	}
}

// TestChosenNumberHeadReadsTheFrozenBinding locks the Count$ChosenNumber
// head (task wildgrowth1): the head reads Ctx.ChosenNumber -- the
// Effect-created replacement's SetChosenNumber$ binding rules' replCtx
// threads in -- and its VERDICT is the bound flag. A bound context evaluates
// (including a bound zero, torgal with no Dogs); an UNBOUND context stays
// unresolved, so the Choose-event population (whose binding lives on
// state.Object.ChosenNumber, never on Ctx) keeps its pre-wildgrowth fail
// direction at every EvalCountOK consumer instead of enforcing a meaningless
// zero.
func TestChosenNumberHeadReadsTheFrozenBinding(t *testing.T) {
	h := newHost(t, 2)
	bound := &Ctx{Num: NumberInputs{Chosen: 5, ChosenBound: true}}
	if n, ok := EvalCountOK(h, bound, "Count$ChosenNumber"); !ok || n != 5 {
		t.Errorf("bound Count$ChosenNumber = (%d, %v), want (5, true)", n, ok)
	}
	// A bound ZERO is a legitimate binding, not a failed one.
	boundZero := &Ctx{Num: NumberInputs{ChosenBound: true}}
	if n, ok := EvalCountOK(h, boundZero, "Count$ChosenNumber"); !ok || n != 0 {
		t.Errorf("bound-zero Count$ChosenNumber = (%d, %v), want (0, true)", n, ok)
	}
	// Unbound: UNRESOLVED. The value-true verdict of the first draft flipped
	// every EvalCountOK consumer for the Choose-event cards (void's
	// Artifact.cmcEQX DestroyAll matched MV-0; plague_of_vermin's GE1 SVar
	// gate enforced 0 fail-closed) -- the verdict must stay false here.
	unbound := &Ctx{}
	if n, ok := EvalCountOK(h, unbound, "Count$ChosenNumber"); ok {
		t.Errorf("unbound Count$ChosenNumber = (%d, %v), want unresolved (0, false)", n, ok)
	}
}

// TestThisTurnEnteredGraveyardCountsPermanentBase pins the Defect-2 fix in
// countEntered: a spec's filter must be evaluated in the entry's DESTINATION
// zone. A Grizzly Bears moved battlefield->graveyard this turn is no longer
// on the battlefield, so the ordinary matcher's `Permanent` base
// (o.Zone == ZBattlefield) rejects it and the count came back 0. The
// zone-aware matcher reads a non-battlefield `Permanent` base as a permanent
// CARD (Forge's Card.isPermanent()), which is what Gravestorm's
// Count$ThisTurnEntered_Graveyard_from_Battlefield_Permanent needs.
func TestThisTurnEnteredGraveyardCountsPermanentBase(t *testing.T) {
	h := newHost(t, 2)
	card := mkCard(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear := h.g.AddObject(card, 0)
	g := h.g
	// Move the object out of the battlefield for real, mirroring events.Move:
	// update the live zone lists, the object's Zone, and the per-add entry
	// list the ThisTurnEntered heads fold.
	g.SetZone(state.ZBattlefield, 0, nil)
	g.SetZone(state.ZGraveyard, 0, append(g.Zone(state.ZGraveyard, 0), bear.ID))
	bear.Zone = state.ZGraveyard
	g.Entered = append(g.Entered, state.ZoneEntry{Obj: bear.ID, To: state.ZGraveyard, From: state.ZBattlefield})

	c := &Ctx{Controller: 0}
	for _, tc := range []struct {
		body string
		want int32
	}{
		// Permanent is the Defect-2 carrier: 0 before the fix.
		{"Count$ThisTurnEntered_Graveyard_from_Battlefield_Permanent", 1},
		// Creature/Card are type/identity tests and were already 1; they
		// must stay 1 so the fix does not regress the common shape.
		{"Count$ThisTurnEntered_Graveyard_from_Battlefield_Creature", 1},
		{"Count$ThisTurnEntered_Graveyard_from_Battlefield_Card", 1},
	} {
		got, ok := EvalCountOK(h, c, tc.body)
		if !ok || got != tc.want {
			t.Errorf("%s = (%d, %v), want (%d, true)", tc.body, got, ok, tc.want)
		}
	}
	// An unrelated permanent that never entered the graveyard must not
	// change the count -- the fix must scope to the entry list, not widen.
	h.g.AddObject(mkCard(t, "Name:Rock\nManaCost:1\nTypes:Artifact\nOracle:x\n"), 0)
	if got, _ := EvalCountOK(h, c, "Count$ThisTurnEntered_Graveyard_from_Battlefield_Creature"); got != 1 {
		t.Errorf("unrelated object changed the count to %d", got)
	}
}

// TestThisTurnEnteredExileSkipsSpellCopies pins the CR 707.10h half of the
// zone-aware matcher: a spell copy the engine parks in exile when it resolves
// has ceased to exist (a copy that leaves the stack is a transient reference,
// not a card), so every Count$ThisTurnEntered_<off-battlefield zone> head must
// skip it. The Ennis, Debate Moderator end step reads
// Count$ThisTurnEntered_Exile_Card.!token and was counting exiled Storm/
// Gravestorm copies before the gate went into matchesZoneSpecCtx. A copy that
// resolved onto the BATTLEFIELD (CR 707.10g) is a real permanent and must keep
// counting.
func TestThisTurnEnteredExileSkipsSpellCopies(t *testing.T) {
	h := newHost(t, 2)
	g := h.g

	// A real card exiled this turn: the one honest entry.
	real := g.AddObject(mkCard(t, "Name:Trick\nManaCost:U\nTypes:Instant\nOracle:x\n"), 0)
	real.Zone = state.ZExile
	g.SetZone(state.ZExile, 0, append(g.Zone(state.ZExile, 0), real.ID))
	g.Entered = append(g.Entered, state.ZoneEntry{Obj: real.ID, To: state.ZExile, From: state.ZStack})

	// A spell copy the engine parked in exile after resolution (IsCopy,
	// live zone exile): ceased per CR 707.10h, must match nothing.
	copyObj := g.AddObject(mkCard(t, "Name:Tendrils\nManaCost:2 B B\nTypes:Sorcery\nOracle:x\n"), 0)
	copyObj.IsCopy = true
	copyObj.Zone = state.ZExile
	g.SetZone(state.ZExile, 0, append(g.Zone(state.ZExile, 0), copyObj.ID))
	g.Entered = append(g.Entered, state.ZoneEntry{Obj: copyObj.ID, To: state.ZExile, From: state.ZStack})

	// A copy of a permanent spell that resolved onto the battlefield
	// (CR 707.10g): a real permanent, must still count.
	permObj := g.AddObject(mkCard(t, "Name:Clone\nManaCost:2 U\nTypes:Creature Shapeshifter\nPT:0/0\nOracle:x\n"), 0)
	permObj.IsCopy = true
	permObj.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), permObj.ID))
	g.Entered = append(g.Entered, state.ZoneEntry{Obj: permObj.ID, To: state.ZBattlefield, From: state.ZStack})

	c := &Ctx{Controller: 0}
	for _, tc := range []struct {
		body string
		want int32
	}{
		// The Ennis head: 2 before the CR 707.10h gate (real card + exiled
		// copy), 1 after.
		{"Count$ThisTurnEntered_Exile_Card.!token", 1},
		{"Count$ThisTurnEntered_Exile_Card", 1},
		// The battlefield copy is real (CR 707.10g) and must count.
		{"Count$ThisTurnEntered_Battlefield_Permanent", 1},
	} {
		got, ok := EvalCountOK(h, c, tc.body)
		if !ok || got != tc.want {
			t.Errorf("%s = (%d, %v), want (%d, true)", tc.body, got, ok, tc.want)
		}
	}
}

// TestPlayerCountConditionFamily pins the Condition<OP><RHS> <property>
// dispatch (the PlayerCount<group>$Condition family, condition1): per-member
// property evaluation (the spec's You* qualifiers bind to the COUNTED
// member, never the resolving controller), a literal threshold, an SVar
// threshold resolved per member (the relative StartingLife/HalfDown read),
// the PlayerCountHasLost$Amount head, and the unresolvable-property verdict
// (0, false) — a gate over an unmodelled property must fail per its caller's
// documented direction, never enforce a fake zero.
func TestPlayerCountConditionFamily(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g,
		drawn:        map[state.PlayerID]int32{0: 2, 1: 2},
		castsBy:      map[state.PlayerID]int{1: 2},
		startingLife: 20}
	c := &Ctx{Controller: 0}
	// Both seats drew two cards, but only the OPPONENT is a group member.
	if got := EvalCount(h, c, "Count$PlayerCountOpponents$ConditionGE2 CardsDrawn"); got != 1 {
		t.Errorf("opponents with GE2 CardsDrawn = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Count$PlayerCountPlayers$ConditionGE2 CardsDrawn"); got != 2 {
		t.Errorf("players with GE2 CardsDrawn = %d, want 2", got)
	}
	// SpellsCastThisTurn backs 3 of the 9 corpus Condition carriers (Ertai's
	// Scorn, Mindbreak Trap, the ConditionGE3 SpellsCastThisTurn shape) and
	// counts per member: the opponent cast 2, the controller cast none.
	if got := EvalCount(h, c, "Count$PlayerCountOpponents$ConditionGE2 SpellsCastThisTurn"); got != 1 {
		t.Errorf("opponents with GE2 SpellsCastThisTurn = %d, want 1", got)
	}
	if got := EvalCount(h, c, "Count$PlayerCountPlayers$ConditionGE2 SpellsCastThisTurn"); got != 1 {
		t.Errorf("players with GE2 SpellsCastThisTurn = %d, want 1 (only seat 1 cast)", got)
	}
	// The per-member ThisTurnEntered leg: one creature entered per seat; the
	// spec's YouCtrl binds to the counted member.
	g.Entered = append(g.Entered,
		state.ZoneEntry{Obj: ids["myBear"], To: state.ZBattlefield, From: state.ZHand},
		state.ZoneEntry{Obj: ids["theirBig"], To: state.ZBattlefield, From: state.ZHand})
	if got := EvalCount(h, c, "Count$PlayerCountOpponents$ConditionGE1 ThisTurnEntered_Battlefield_Creature.YouCtrl"); got != 1 {
		t.Errorf("opponents with GE1 creature entry = %d, want 1 (the resolved controller's own entry must not count)", got)
	}
	if got := EvalCount(h, c, "Count$PlayerCountPlayers$ConditionGE1 ThisTurnEntered_Battlefield_Creature.YouCtrl"); got != 2 {
		t.Errorf("players with GE1 creature entry = %d, want 2", got)
	}
	// The SVar-RHS read resolved PER MEMBER: Anya's `ConditionLTZ LifeTotal`
	// with Z = the member's own half starting life (20/2 = 10). Opponent at
	// 9 counts; back at 10 it does not.
	sv := &Ctx{Controller: 0, SVars: map[string]string{
		"Z": "PlayerCountDefinedPlayer.PlayerUID_RelativePlayerUID$StartingLife/HalfDown"}}
	g.Players[1].Life = 9
	if got := EvalCount(h, sv, "Count$PlayerCountOpponents$ConditionLTZ LifeTotal"); got != 1 {
		t.Errorf("opponents below half starting life = %d, want 1", got)
	}
	g.Players[1].Life = 10
	if got := EvalCount(h, sv, "Count$PlayerCountOpponents$ConditionLTZ LifeTotal"); got != 0 {
		t.Errorf("opponents below half starting life at 10 = %d, want 0", got)
	}
	// PlayerCountHasLost$Amount: the lost-seat count (Hot Pursuit's gate).
	g.Players[1].Lost = true
	if got := EvalCount(h, c, "Count$PlayerCountHasLost$Amount"); got != 1 {
		t.Errorf("lost seats = %d, want 1", got)
	}
	// The /Op suffix applies (Rampant Frogantua's Amount/Times.10 — its
	// +10/+10-per-lost-player SVar): 1 lost seat x 10.
	if got := EvalCount(h, c, "Count$PlayerCountHasLost$Amount/Times.10"); got != 10 {
		t.Errorf("lost seats /Times.10 = %d, want 10", got)
	}
	// An unmodelled property fails UNRESOLVABLE, never a fake zero — and
	// the verdict must hold on an EMPTY group too (the HasLost assertions
	// above left the only opponent lost): the per-member loop never runs
	// there, so the property must be validated before it. An `...LE0`-shaped
	// gate over an unmodelled property on an empty group would otherwise
	// evaluate true (0 <= 0) — the wrong-wide class.
	for _, body := range []string{
		"Count$PlayerCountOpponents$ConditionGE2 BogusProp",
		"Count$PlayerCountOpponents$ConditionLE0 BogusProp",
		// A named RHS with no body anywhere is equally unresolvable on the
		// empty group: the RHS body must be looked up before the range too.
		"Count$PlayerCountOpponents$ConditionLENoSuchSVar LifeTotal",
		// A MALFORMED ThisTurnEntered_ spec must fail unresolvable too: the
		// pre-check has to share the evaluator's own grammar (a bare prefix
		// or an unknown zone word is not a spec), or an `...LE0`-shaped gate
		// over one evaluates true (0 <= 0) over nothing.
		"Count$PlayerCountOpponents$ConditionLE0 ThisTurnEntered_",
		"Count$PlayerCountOpponents$ConditionLE0 ThisTurnEntered_Nonsense",
		"Count$PlayerCountOpponents$ConditionLE0 ThisTurnEntered_Battlefield_",
	} {
		if got, ok := EvalCountOK(h, c, body); ok {
			t.Errorf("%s reported EVALUATED as %d on an empty group — must fail unresolvable", body, got)
		}
	}
	// A WELL-FORMED ThisTurnEntered_ spec over the SAME empty group is still
	// the honest zero — a modelled property must not be caught by the
	// malformed-spec guard.
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$ConditionLE0 ThisTurnEntered_Battlefield_Creature"); !ok || got != 0 {
		t.Errorf("well-formed ThisTurnEntered over an empty group = (%d, %v), want (0, true)", got, ok)
	}
	// An SVar RHS whose body EXISTS but does not resolve is unresolvable on
	// the empty group too (the body is only otherwise evaluated per member).
	bad := &Ctx{Controller: 0, SVars: map[string]string{"Z": "Count$BogusHead"}}
	if got, ok := EvalCountOK(h, bad, "Count$PlayerCountOpponents$ConditionLTZ LifeTotal"); ok {
		t.Errorf("non-resolving SVar RHS reported EVALUATED as %d on an empty group — must fail unresolvable", got)
	}
	// Same verdict on a LIVE group (the per-member read the original pin
	// covered).
	g.Players[1].Lost = false
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountOpponents$ConditionGE2 BogusProp"); ok {
		t.Errorf("BogusProp reported EVALUATED as %d on a live group — must fail unresolvable", got)
	}
}

// TestEvalCountValidAllScansEveryCardZone pins the ValidAll head: the scan
// covers EVERY card zone (library, hand, battlefield, graveyard, exile,
// command) plus one stack pass, and each candidate is matched against its
// OWN zone -- the way Forge evaluates a ValidAll spec against the card's
// actual zone. This is Cactus Preserve's and Tangleweave Armor's exact SVar
// shape (greatest mana value among your commanders, who sit in the command
// zone) and Kefka's (an imprinted card, which lives in exile).
func TestEvalCountValidAllScansEveryCardZone(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	// board(t) puts one Instant ("Trick") and one Sorcery ("Ritual") in
	// seat 0's GRAVEYARD: the battlefield-only Valid head counts neither,
	// ValidAll counts both -- and the comma-alternative spec reaches the
	// pair the single-base spec cannot.
	if got := EvalCount(h, c, "Count$Valid Instant"); got != 0 {
		t.Errorf("precondition: Valid Instant = %d, want 0 (graveyard cards are outside the battlefield scan)", got)
	}
	if got := EvalCount(h, c, "Count$ValidAll Instant"); got != 1 {
		t.Errorf("ValidAll Instant = %d, want 1 (the graveyard instant)", got)
	}
	if got := EvalCount(h, c, "Count$ValidAll Instant,Sorcery"); got != 2 {
		t.Errorf("ValidAll Instant,Sorcery = %d, want 2 (the graveyard instant and sorcery)", got)
	}
	// The command zone: the Walker planeswalker moves there and is
	// registered as seat 0's commander. IsCommander reads the seat's
	// Commanders list, and the commander now sits in ZCommand -- neither
	// the battlefield Valid head nor a battlefield-only scan can see it.
	walker := g.Obj(ids["myWalker"])
	var bf []state.ObjID
	for _, id := range g.Zone(state.ZBattlefield, 0) {
		if id != ids["myWalker"] {
			bf = append(bf, id)
		}
	}
	g.SetZone(state.ZBattlefield, 0, bf)
	walker.Zone = state.ZCommand
	g.SetZone(state.ZCommand, 0, []state.ObjID{ids["myWalker"]})
	g.Players[0].Commanders = []state.ObjID{ids["myWalker"]}
	if got := EvalCount(h, c, "Count$Valid Card.IsCommander"); got != 0 {
		t.Errorf("Valid Card.IsCommander = %d, want 0 (the commander is in the command zone)", got)
	}
	if got := EvalCount(h, c, "Count$ValidAll Card.IsCommander+YouOwn"); got != 1 {
		t.Errorf("ValidAll Card.IsCommander+YouOwn = %d, want 1", got)
	}
	// The extreme property folds over the all-zones scan: the greatest
	// commander mana value reads the walker's mana value {2}{U} = 3.
	if got := EvalCount(h, c, "Count$ValidAll Card.IsCommander+YouOwn$GreatestCardManaCost"); got != 3 {
		t.Errorf("ValidAll Card.IsCommander+YouOwn$GreatestCardManaCost = %d, want 3", got)
	}
	// Extreme properties keep working over the mixed-zone scan: the
	// battlefield creatures' greatest power is still the Giant's 5.
	if got := EvalCount(h, c, "Count$ValidAll Creature$GreatestCardPower"); got != 5 {
		t.Errorf("ValidAll Creature$GreatestCardPower = %d, want 5", got)
	}
	// The stack pass: ValidAll scans the global stack exactly ONCE (the
	// single stack pass after the per-seat zones), so a spell on the stack
	// counts 1, not once per alive seat.
	trick := g.Obj(ids["myInstant"])
	var gy []state.ObjID
	for _, id := range g.Zone(state.ZGraveyard, 0) {
		if id != ids["myInstant"] {
			gy = append(gy, id)
		}
	}
	g.SetZone(state.ZGraveyard, 0, gy)
	trick.Zone = state.ZStack
	g.SetZone(state.ZStack, 0, []state.ObjID{ids["myInstant"]})
	if got := EvalCount(h, c, "Count$ValidAll Instant"); got != 1 {
		t.Errorf("ValidAll Instant with the spell on the stack = %d, want 1 (the stack is scanned once, not once per alive seat)", got)
	}
}

// TestEvalCountValidZoneScanIsAllocationFree pins the zone-count branch's
// hot-path contract: Count$Valid is on the hottest condition path
// (effects.CheckSVarHolds intervening-ifs, static gates, SVarCompare), so
// its single-zone scan must iterate IN PLACE -- no materialised candidate
// slice, no per-candidate heap work. The r1 restructure went 0 -> 4
// allocs/op and was reverted to the in-place fold; this pin keeps the
// restructure from recurring.
func TestEvalCountValidZoneScanIsAllocationFree(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0}
	// Precondition: the head actually counts the battlefield creatures
	// (Bear + Flier + Giant) so the alloc pin runs over a real scan, not
	// an empty one that never enters the per-candidate body.
	if got := EvalCount(h, c, "Count$Valid Creature"); got != 3 {
		t.Fatalf("precondition: Count$Valid Creature = %d, want 3", got)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if got := EvalCount(h, c, "Count$Valid Creature"); got != 3 {
			t.Fatalf("Count$Valid Creature = %d, want 3", got)
		}
	})
	if allocs != 0 {
		t.Fatalf("Count$Valid Creature allocated %.0f objects per eval; want zero (the zone-count scan must iterate in place)", allocs)
	}
	// The warm-Ctx shape the r3 review added: the layer walk (rules/
	// layers.go's cdaSetPT) builds a FRESH &effects.Ctx{...} per call and
	// hands it to EvalCount, so a warm caller-built Ctx must not be forced
	// to the heap by anything evalCountBody does with it. The r2 fold
	// stored c.SpecContext(...) in the zoneCountFold struct, which made
	// escape analysis summarise the *Ctx param as leaking -- the rules
	// Derived pin went 0 -> 1 alloc/call while this test still passed
	// (its Ctx was built outside the region). Building the Ctx INSIDE the
	// region here pins the caller-side contract in this package, where the
	// break happens.
	warm := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cWarm := &Ctx{Controller: 0}
			if got := EvalCount(h, cWarm, "Count$Valid Creature"); got != 3 {
				b.Fatalf("Count$Valid Creature = %d, want 3", got)
			}
		}
	})
	if warm.AllocsPerOp() != 0 {
		t.Fatalf("warm Ctx built inside the region allocated %d objects/op; want zero (a caller-built Ctx must not escape through the count scan -- the layer walk builds one per object)", warm.AllocsPerOp())
	}
}

// TestCountDottedBranchesResolveSVarNames pins the dotted yes/no branch heads'
// operand grammar: a branch may name an SVar on the same face, not only a
// literal. Divine Resilience's TargetMax$ Y is SVar:Y:Count$Kicked.Z.1 with
// Z the creatures you control; the literal-only read answered 0 for the
// named branch, so the kicked cast was bounded at zero targets and asked
// nothing.
func TestCountDottedBranchesResolveSVarNames(t *testing.T) {
	h, c := fixtureHost(t)
	src := h.Game().Obj(c.Source)
	c.SVars = map[string]string{"Z": "Count$Compare 1 GE1.3.0", "Y": "2"}
	if n := EvalCount(h, c, "Count$Kicked.Z.1"); n != 1 {
		t.Fatalf("not kicked Count$Kicked.Z.1 = %d, want the literal 1", n)
	}
	src.CastFlags = state.FlagKicked
	if n := EvalCount(h, c, "Count$Kicked.Z.1"); n != 3 {
		t.Fatalf("kicked Count$Kicked.Z.1 = %d, want Z's 3", n)
	}
	if n := EvalCount(h, c, "Count$Kicked.Y.Z"); n != 2 {
		t.Fatalf("kicked Count$Kicked.Y.Z = %d, want Y's 2", n)
	}
	src.CastFlags = 0
	c.Kicker.PendingKicked = true
	if n := EvalCount(h, c, "Count$Kicked.Y.Z"); n != 2 {
		t.Fatalf("pending-kicked Count$Kicked.Y.Z = %d, want Y's 2", n)
	}
	c.Kicker.PendingKicked = false
	if n := EvalCount(h, c, "Count$Kicked.Y.Z"); n != 3 {
		t.Fatalf("unkicked Count$Kicked.Y.Z = %d, want Z's 3", n)
	}
	if n := EvalCount(h, c, "Count$StartingPlayer.Y.Z"); n != 2 && n != 3 {
		t.Fatalf("Count$StartingPlayer.Y.Z = %d, want a named branch (2 or 3)", n)
	}
}
