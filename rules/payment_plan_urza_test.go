package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The V1 payment planner must be able to plan with the three Urza lands,
// whose mana ability's Amount$ is an SVar-indirected Count$UrzaLands
// expression (rules/urza_lands_test.go authors the same scripts; the corpus
// test in rules/urza_lands_corpus_test.go pins the real ones). The shared
// fixed-production census withholds them because availableAmount cannot price
// the count statically; paymentPlanManaUnits now prices it through the
// engine's own evaluator (castWindowAmount) when the value is provably
// invariant to the payment's own taps (paymentPlanStableAmount).
//
// The board is built with findAndMoveToBattlefield (a logged MoveZone), not
// the eventless onBoard helper, so replayCheck can reconstruct it: a plan
// witness is part of the replay history and this test verifies the whole
// planned cast replays.

// planSevenSpell is the {7} payoff the assembled board funds.
const planSevenSpell = "Name:Plan Seven\nManaCost:7\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"

// planUrzaOtherLand is a plain one-mana colourless land, so the assembled
// board has one more source than the {7} needs and the rank's "fewer newly
// activated sources" key (spec 5 key 3) must leave it untapped.
const planUrzaOtherLand = "Name:Plan Other Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n"

// planUntappedCountLand is the counterexample the stability gate exists for:
// a land whose mana amount is a count of UNTAPPED lands. The evaluator
// resolves it (precondition below), but tapping it to pay changes the count,
// so the planner must never commit the pre-activation value to a witness.
const planUntappedCountLand = "Name:Plan Untapped Count Land\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ UntapCount\n" +
	"SVar:UntapCount:Count$Valid Land.untapped\nOracle:x\n"

// planUrzaTowerActivation returns the witness step naming tower, failing when
// no plan step taps it.
func planUrzaTowerActivation(t *testing.T, plan decision.PaymentPlan, tower state.ObjID) decision.PaymentActivation {
	t.Helper()
	for _, act := range plan.Activations {
		if act.Source == tower {
			return act
		}
	}
	t.Fatalf("plan %#v never taps the Tower %d", plan, tower)
	return decision.PaymentActivation{}
}

// TestPaymentPlanUrzaTowerPlansForThree: Tower + Mine + Power Plant + one
// other land on the battlefield, a {7} spell in hand. The planner must build
// a witness that taps the assembled Tower for 3 (Mine 2, Power Plant 2, sum
// 7) and leave the surplus land untapped; executing that witness casts the
// spell and empties the pool.
func TestPaymentPlanUrzaTowerPlansForThree(t *testing.T) {
	t.Parallel()
	e, cfg, spell := newFixtureDeck(t, 9411, planSevenSpell, urzaTowerSrc, urzaMineSrc, urzaPlantSrc, planUrzaOtherLand)
	// Precondition: the spell is a plain castable-from-hand card with the
	// printed {7} cost this test plans against.
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand || o.Face() == nil || o.Face().ManaValue() != 7 {
		t.Fatalf("fixture spell = %+v, want a {7} card in hand", e.G.Obj(spell))
	}
	tower := findAndMoveToBattlefield(t, e, 0, "Urza's Tower")
	mine := findAndMoveToBattlefield(t, e, 0, "Urza's Mine")
	plant := findAndMoveToBattlefield(t, e, 0, "Urza's Power Plant")
	other := findAndMoveToBattlefield(t, e, 0, "Plan Other Land")
	// Precondition: all four are untapped permanents on seat 0's battlefield,
	// so the count the Tower's ability reads has all three assembled subtypes
	// present.
	for _, tc := range []struct {
		id   state.ObjID
		name string
	}{{tower, "Tower"}, {mine, "Mine"}, {plant, "Plant"}, {other, "Other"}} {
		o := e.G.Obj(tc.id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.Tapped || o.Face() == nil {
			t.Fatalf("%s not an untapped battlefield permanent under seat 0: %+v", tc.name, o)
		}
	}

	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("assembled Urza board gave no plan: %+v", got)
	}
	plan := *got.Plan
	if len(plan.Activations) != 3 {
		t.Fatalf("plan = %#v, want exactly the three assembled Urza lands (the rank keeps the surplus land untapped)", plan)
	}
	towerAct := planUrzaTowerActivation(t, plan, tower)
	if got := towerAct.Produces[state.MC]; got != 3 {
		t.Fatalf("Tower witness Produces colourless = %d, want 3 (assembled Count$UrzaLands.3.1)", got)
	}
	var total int
	for _, act := range plan.Activations {
		for _, n := range act.Produces {
			total += int(n)
		}
	}
	if total != 7 {
		t.Fatalf("plan produces %d mana, want exactly the 7 the cost needs", total)
	}

	// Execute the witness through the real cast flow: the spell resolves to
	// the stack and the pool is empty afterward (no surplus floated, no
	// overpay).
	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	submitPaymentPlan(t, e, d, a)
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("spell zone after the planned cast = %s, want stack (pending %s)", z, paymentPlanPendingSummary(e.Pending()))
	}
	if p := e.G.Players[0].Pool.Total(); p != 0 {
		t.Fatalf("pool after the planned cast = %d, want 0", p)
	}
	if !e.G.Obj(tower).Tapped || !e.G.Obj(mine).Tapped || !e.G.Obj(plant).Tapped {
		t.Fatalf("planned sources tapped = tower:%v mine:%v plant:%v, want all three tapped",
			e.G.Obj(tower).Tapped, e.G.Obj(mine).Tapped, e.G.Obj(plant).Tapped)
	}
	if e.G.Obj(other).Tapped {
		t.Fatal("the surplus land was tapped; the plan should not have needed it")
	}
	if nd := e.Pending(); nd != nil && nd.PaymentFallback != nil {
		t.Fatalf("planned Urza cast fell back: %s", paymentPlanPendingSummary(nd))
	}
	replayCheck(t, e, cfg)
}

// TestPaymentPlanUrzaTowerPlansForOne: the same Tower with NO Mine or Power
// Plant is unassembled, so its Count$UrzaLands.3.1 evaluates to 1. A {1}
// spell must plan as a single Tower tap producing 1, and cast cleanly.
func TestPaymentPlanUrzaTowerPlansForOne(t *testing.T) {
	t.Parallel()
	e, cfg, spell := newFixtureDeck(t, 9412, "Name:Plan One\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n", urzaTowerSrc, urzaMineSrc, urzaPlantSrc)
	tower := findAndMoveToBattlefield(t, e, 0, "Urza's Tower")
	o := e.G.Obj(tower)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.Face() == nil {
		t.Fatalf("Tower not an untapped battlefield permanent: %+v", o)
	}
	// Precondition: the Mine and Power Plant are still in the library, so this
	// is the unassembled branch of the count and the Tower is the only source.
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if id == tower {
			continue
		}
		if f := e.G.Obj(id).Face(); f != nil {
			t.Fatalf("unexpected second battlefield permanent %q; this test needs the Tower alone", f.Name)
		}
	}

	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("unassembled Urza Tower gave no plan: %+v", got)
	}
	plan := *got.Plan
	if len(plan.Activations) != 1 {
		t.Fatalf("plan = %#v, want the single Tower tap", plan)
	}
	act := planUrzaTowerActivation(t, plan, tower)
	if got := act.Produces[state.MC]; got != 1 {
		t.Fatalf("unassembled Tower witness Produces colourless = %d, want 1 (Count$UrzaLands.3.1 not assembled)", got)
	}

	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	submitPaymentPlan(t, e, d, a)
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("spell zone after the planned cast = %s, want stack (pending %s)", z, paymentPlanPendingSummary(e.Pending()))
	}
	if p := e.G.Players[0].Pool.Total(); p != 0 {
		t.Fatalf("pool after the planned cast = %d, want 0", p)
	}
	replayCheck(t, e, cfg)
}

// TestPaymentPlanRefusesUntappedCountAmount pins the stability gate itself. A
// mana amount that reads UNTAPPED permanents is resolvable (the precondition
// proves the evaluator prices it) but is NOT invariant to the payment's own
// taps: tapping the source to pay drops the count. The planner must refuse to
// commit the pre-activation value (paymentPlanStableAmount), so a {1} spell
// with this land as its only source gets no plan. Removing just the
// invariance check -- keeping the evaluation -- makes this test fail: the land
// would price 1, a plan would form, and the executor would then see it produce
// 0.
func TestPaymentPlanRefusesUntappedCountAmount(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9413, "Name:Plan One B\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n", planUntappedCountLand)
	source := findAndMoveToBattlefield(t, e, 0, "Plan Untapped Count Land")
	o := e.G.Obj(source)
	ma := o.Face().Abilities[0]
	// Precondition: the evaluator resolves this Amount$ to a positive value,
	// so the ONLY thing that can refuse the source is the invariance gate (a
	// source the evaluator cannot price is refused for a different reason).
	resolved, ok := e.castWindowAmount(0, source, o, ma)
	if !ok || resolved <= 0 {
		t.Fatalf("precondition: castWindowAmount = %d, ok=%v; want a positive resolvable amount", resolved, ok)
	}
	// Precondition: tapping the payer's permanents changes the value (it
	// drops to 0, which castWindowAmount reports as unpriced), so the gate has
	// something real to detect.
	if probeAmt, ok := e.paymentPlanTappedProbe(0).castWindowAmount(0, source, o, ma); ok && probeAmt == resolved {
		t.Fatalf("precondition: tapped probe amount = %d; want it to differ from the untapped %d", probeAmt, resolved)
	}
	if _, ok := e.paymentPlanStableAmount(e.paymentPlanTappedProbe(0), 0, source, o, ma); ok {
		t.Fatal("paymentPlanStableAmount admitted a tap-dependent untapped-count amount")
	}
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil {
		t.Fatalf("planner committed a tap-dependent amount: %#v", got.Plan)
	}
}
