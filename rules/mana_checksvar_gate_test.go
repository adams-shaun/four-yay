// Ticket mana-checksvar-gate: an activated mana ability's CheckSVar$/
// SVarCompare$ "Activate only if ..." gate (Glistening Sphere's Corrupted
// "{T}: Add three mana of any one color. Activate only if an opponent has
// three or more poison counters.") is evaluated by the shared sVarGateOK
// evaluator inside the mana gate, so the priority offer, the payment windows
// and the V1 planner all withhold the ability while its condition is false.
// fpCorpus/fpPlanUses are ported from the throwaway proof branch
// wt/autopay-census-proofs (never merged); censusContains lives in
// rules/autopay_census_test.go.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func fpCorpus(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q missing", name)
	}
	return c
}

func fpPlanUses(got PaymentPlanOutcome, id state.ObjID) (decision.PaymentActivation, bool) {
	if got.Plan == nil {
		return decision.PaymentActivation{}, false
	}
	for _, a := range got.Plan.Activations {
		if a.Source == id {
			return a, true
		}
	}
	return decision.PaymentActivation{}, false
}

// TestAutopayFPCheckSVarGateHoldsForManaAbilities is the census proof: with
// no poison counters anywhere, Glistening Sphere's Corrupted ability must be
// absent from both the priority mana offer and the V1 {3} plan. Fails on the
// pre-gate tree.
func TestAutopayFPCheckSVarGateHoldsForManaAbilities(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9804, "Name:Three Probe\nManaCost:3\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	sphere := onBoardCard(t, e, 0, fpCorpus(t, "Glistening Sphere"))
	gated := e.G.Obj(sphere).Face().Abilities[1]
	if gated.Params["CheckSVar"] == "" {
		t.Fatal("fixture: ability 1 must be the CheckSVar-gated Corrupted ability")
	}
	if censusContains(e.availableManaAbilitiesForWindow(0, sphere, true), gated) {
		t.Errorf("Corrupted mana ability offered with no opponent poison counters")
	}
	if a, ok := fpPlanUses(e.PlanCastPayment(0, paymentCast(spell)), sphere); ok && a.Ability.Index == 1 {
		t.Errorf("V1 plan uses the ungated Corrupted ability: %+v", a)
	}
}

// TestManaAbilityCheckSVarGate end to end: the same ability is withheld at
// zero poison, becomes offered (and planned, and manually activatable through
// the priority option for exactly three mana) once an opponent holds three
// poison counters.
func TestManaAbilityCheckSVarGate(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9804, "Name:Three Probe\nManaCost:3\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	sphere := onBoardCard(t, e, 0, fpCorpus(t, "Glistening Sphere"))
	if o := e.G.Obj(sphere); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Glistening Sphere is not on the battlefield")
	}
	if o := e.G.Obj(sphere); o.Tapped {
		t.Fatal("precondition: Glistening Sphere is tapped")
	}
	gated := e.G.Obj(sphere).Face().Abilities[1]
	if gated.Params["CheckSVar"] == "" {
		t.Fatal("fixture: ability 1 must be the CheckSVar-gated Corrupted ability")
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("precondition: seat 1 holds %d poison counters, want 0", got)
	}
	if censusContains(e.availableManaAbilitiesForWindow(0, sphere, true), gated) {
		t.Fatal("gate read true with no poison counter anywhere")
	}
	// The gate body is SVar:X:PlayerCountOpponents$HighestCounters.Poison with
	// SVarCompare$ GE3. The placement is eventless, so stale the derived
	// memos the way an emitted event would.
	e.G.Players[1].AddCounter("POISON", 3)
	e.staticEpoch = -1
	e.activeEpoch = -1
	if got := e.G.Players[1].Counter("POISON"); got != 3 {
		t.Fatalf("precondition: seat 1 poison = %d, want 3", got)
	}
	mas := e.availableManaAbilitiesForWindow(0, sphere, true)
	idx := -1
	for i, ma := range mas {
		if ma == gated {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Corrupted mana ability still withheld at three opponent poison counters")
	}
	if idx != 1 {
		t.Fatalf("fixture: gated ability's window index = %d, want 1 (printed order)", idx)
	}
	// The V1 planner funds the {3} cast with the gated ability.
	plan := e.PlanCastPayment(0, paymentCast(spell))
	a, ok := fpPlanUses(plan, sphere)
	if !ok || a.Ability.Index != 1 {
		t.Fatalf("plan for {3} does not use the gated ability: plan %+v, used %+v (found %v)", plan.Plan, a, ok)
	}
	// Manual activation through the priority option: the wheel must offer the
	// gated ability, and resolving it adds exactly three mana. The wheel is
	// re-asked after the eventless counter placement so its option list reads
	// the refreshed mana window.
	e.pending = nil
	e.askPriority(0)
	activateMana(t, e, sphere)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("mana-ability wheel = %+v, want KChoose", d)
	}
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "mana" && o.Ability == idx {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("no wheel option for gated ability %d: %+v", idx, d.Options)
	}
	submitChoices(t, e, opt)
	// "Add three mana of any one color" (Produced$ Any) poses the stage-2
	// colour ask; answer it with the first colour the wheel offers.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after the wheel answer, pending = %+v, want the KChoose colour ask", d)
	}
	col := -1
	for _, o := range d.Options {
		if o.Kind == "mana" && o.ManaSymbol != "" {
			col = o.Index
			break
		}
	}
	if col < 0 {
		t.Fatalf("no colour option on the stage-2 ask: %+v", d.Options)
	}
	submitChoices(t, e, col)
	if got := e.G.Players[0].Pool.Total(); got != 3 {
		t.Fatalf("pool after activating the gated ability = %d, want 3", got)
	}
}
