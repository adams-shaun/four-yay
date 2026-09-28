package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLullmagesDominationTargetControllerDiscount pins the real-corpus
// reduction (agent-20260928T043626Z-b7e271c1): Lullmage's Domination's
// ReduceCost static carries its discount entirely in an SVar chain
// (`Amount$ XGrave`, `SVar:XGrave:Count$Compare CheckTgt GE8.3.0`,
// `SVar:CheckTgt:TargetedController$CardsInGraveyard`). Before the fix the
// TargetedController$ count head resolved to (0, false), so Count$Compare read
// 0, GE8 failed, and the generic {3} was never reduced. This test prices the
// real card with a chosen target whose controller has 8+ cards in its
// graveyard and asserts the generic drops to 0, with the no-target case as the
// negative control.
func TestLullmagesDominationTargetControllerDiscount(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Lullmage's Domination"))
	spell := e.G.Zone(state.ZHand, 0)[0]

	// Seat 1's creature is the chosen target; seat 1's graveyard holds eight
	// cards. Seat 0's own graveyard is empty, so a wrong-perspective read (the
	// resolving controller) would leave the discount unearned.
	tgt := battlePerm(t, e, 1, "Name:Mark\nManaCost:2 U\nTypes:Creature Merfolk\nPT:2/2\nOracle:x\n")
	for i := 0; i < 8; i++ {
		c := e.G.AddObject(card(t, "Name:Past\nManaCost:U\nTypes:Instant\nOracle:x\n"), 1)
		c.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), c.ID))
	}

	// Preconditions: the spell is in the zone the static reads, the target is
	// seat 1's battlefield creature, seat 1's graveyard really has 8+ cards,
	// and seat 0's does not -- without all of these the assertions are vacuous.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: spell zone = %v, want hand", o.Zone)
	}
	if o := e.G.Obj(tgt); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: target zone=%v controller=%v, want battlefield seat 1", o.Zone, o.Controller)
	}
	if n := len(e.G.Zone(state.ZGraveyard, 1)); n < 8 {
		t.Fatalf("precondition: seat 1 graveyard = %d, want >=8", n)
	}
	if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 graveyard = %d, want 0", n)
	}

	base := e.parseCost("3 U U U")
	if base.Generic != 3 {
		t.Fatalf("precondition: base generic = %d, want 3", base.Generic)
	}
	statics := e.collectCostStatics()

	// Negative control: with no target bound the controller read has no
	// players, so the discount must NOT apply ({3} stays {3}). This row would
	// also pass if the static were never applied at all, which is why the
	// positive row below is the real assertion.
	none := e.costModifiersWithTargetsUsing(statics, 0, spell, spellScope(""), nil, true).apply(base)
	if none.Generic != 3 {
		t.Fatalf("negative control: no-target generic = %d, want 3", none.Generic)
	}

	// The real assertion: one chosen target whose controller has 8+ graveyard
	// cards earns the full {3} reduction.
	got := e.costModifiersWithTargetsUsing(statics, 0, spell, spellScope(""), []state.Target{{Obj: tgt}}, true).apply(base)
	if got.Generic != 0 {
		t.Fatalf("TargetedController discount: generic = %d, want 0 ({3} less)", got.Generic)
	}
}

// TestLullmagesDominationXMenuHonoursTargetControllerDiscount covers the
// offer-time half the static-price test above cannot: CR 601.2b announces {X}
// BEFORE CR 601.2c chooses targets, so xAsk prices candidate X from pc.mods --
// a snapshot bound to no targets. Lullmage's {3} reduction is target-
// conditional (its controller's graveyard), so with only {U}{U}{U} the X=1
// announcement that is payable AFTER a qualifying target is chosen was not
// offered at all, and the discount was unusable for nonzero X. The X ask now
// retries a candidate through the same potential-target composition the offer
// gate uses (a target-relative Count$Compare amount marks the collection
// target-conditional; rules/statics.go's markCostValidTarget recognises the
// whole Targeted ref family, not just TargetedByTarget$). The chosen target is
// still repriced before payment, so this only widens the menu to X some legal
// target can pay.
func TestLullmagesDominationXMenuHonoursTargetControllerDiscount(t *testing.T) {
	t.Parallel()

	// seat0Pool is exactly the {U}{U}{U} base: enough for X=0, and for X=1
	// only under the {3} reduction (an unreduced X=1 needs {1}{U}{U}{U}).
	build := func(qualifying bool) (*Engine, state.ObjID) {
		e := handEngine(t, corpusAlternativeCard(t, "Lullmage's Domination"))
		// A cmc-1 creature is the only creature on the board, so X=1 has a
		// legal target (Creature.cmcEQX) and every larger X does not.
		tgt := battlePerm(t, e, 1, "Name:Mark\nManaCost:U\nTypes:Creature Merfolk\nPT:2/2\nOracle:x\n")
		n := 8
		if !qualifying {
			n = 7
		}
		for i := 0; i < n; i++ {
			c := e.G.AddObject(card(t, "Name:Past\nManaCost:U\nTypes:Instant\nOracle:x\n"), 1)
			c.Zone = state.ZGraveyard
			e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), c.ID))
		}
		e.G.Players[0].Pool[state.MU] = 3
		return e, tgt
	}

	// xDecision drives the cast to its X decision and returns the pending
	// decision's offered X values with the decision still unanswered, so the
	// caller can continue the same cast from it.
	xDecision := func(t *testing.T, e *Engine) ([]int, *decision.Decision) {
		t.Helper()
		e.askPriority(0)
		spell := e.G.Zone(state.ZHand, 0)[0]
		castMode(t, e, spell, "")
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
			t.Fatalf("want the X decision, got %+v", d)
		}
		var xs []int
		for _, o := range d.Options {
			if o.Kind == "x" {
				xs = append(xs, o.Amount)
			}
		}
		return xs, d
	}

	// Preconditions for the qualifying board: exactly {U}{U}{U} (so an
	// unreduced X=1 is unpayable), a cmc-1 creature controlled by seat 1, and
	// seat 1's graveyard at or above the eight the reduction reads.
	e, tgt := build(true)
	if e.G.Players[0].Pool.Total() != 3 {
		t.Fatalf("precondition: pool total = %d, want exactly 3", e.G.Players[0].Pool.Total())
	}
	if mv := e.G.Obj(tgt).Face().ManaValue(); mv != 1 {
		t.Fatalf("precondition: target mana value = %d, want 1 (X=1's only legal target)", mv)
	}
	if o := e.G.Obj(tgt); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: target zone=%v controller=%v, want battlefield seat 1", o.Zone, o.Controller)
	}
	if n := len(e.G.Zone(state.ZGraveyard, 1)); n != 8 {
		t.Fatalf("precondition: seat 1 graveyard = %d, want 8", n)
	}
	if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 graveyard = %d, want 0", n)
	}
	// The reduced X=1 is offered; X=0 is too. X=2+ has no legal target.
	xs, d := xDecision(t, e)
	if !slices.Contains(xs, 1) {
		t.Fatalf("X menu = %v, want 1 offered (payable under the {3} reduction)", xs)
	}

	// Negative control: the same board with only 7 graveyard cards earns no
	// reduction, so the unreduced X=1 is unpayable and must NOT be offered --
	// this row would also pass if the menu offered every X regardless.
	e2, _ := build(false)
	if n := len(e2.G.Zone(state.ZGraveyard, 1)); n != 7 {
		t.Fatalf("precondition: negative-control graveyard = %d, want 7", n)
	}
	if xs2, _ := xDecision(t, e2); slices.Contains(xs2, 1) {
		t.Fatalf("negative control: X menu = %v, X=1 must not be offered without the reduction", xs2)
	}

	// End-to-end: answer X=1 on the qualifying cast (still pending from
	// xDecision above), choose the creature, and the cast must complete paying
	// only the reduced {U}{U}{U} -- the creature changes controller.
	xIdx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 1 {
			xIdx = o.Index
		}
	}
	submitChoices(t, e, xIdx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the target decision after X=1, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Obj == tgt {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("the cmc-1 creature is not offered as X=1's target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(tgt); o.Controller != 0 || o.Zone != state.ZBattlefield {
		t.Fatalf("after resolution target controller=%v zone=%v, want seat 0 battlefield (GainControl)", o.Controller, o.Zone)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after cast = %d, want 0 (charged the reduced {U}{U}{U}, not {1}{U}{U}{U})", got)
	}
}
