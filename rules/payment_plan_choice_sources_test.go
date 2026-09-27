package rules

// aph-combo-chosen-identity: the finite choice-shaped mana producers a V1
// payment witness may activate. Spec §3.2 admits "Add R or G" (Produced$
// Combo), a recorded as-enters colour (Produced$ Chosen / Combo … Chosen),
// and the commander colour identity (Produced$ ColorIdentity), each as one
// concrete alternative per producible colour. This file is the ticket's own
// test file; the census proofs it ports live on throwaway branches and are
// never cherry-picked (brief Standing rules).

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// choiceCard reads a card by name from the linked corpus at test time (the
// fpCorpus pattern). A missing corpus is a SKIP via CorpusRegistry, so the
// Done-means "0 SKIP" check is what proves the corpus was present.
func choiceCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q missing", name)
	}
	return c
}

// choiceAlts returns the mana each V1 alternative of source id produces,
// re-derived through the planner's own units. A source with no unit has no
// alternatives and returns nil; a source WITH a unit that yields none also
// returns nil, which is the fail-closed answer the deferred-shape tests pin.
func choiceAlts(t *testing.T, e *Engine, id state.ObjID) []state.Mana {
	t.Helper()
	for _, u := range e.paymentPlanManaUnits(0) {
		if u.id != id {
			continue
		}
		var out []state.Mana
		for _, a := range e.paymentPlanUnitAlternatives(u) {
			out = append(out, a.mana)
		}
		return out
	}
	return nil
}

// choiceColours lists the single producing slot of each alternative as a
// WUBRG letter (or "C"), for readable equality assertions.
func choiceColours(alts []state.Mana) []string {
	var out []string
	for _, m := range alts {
		for i, n := range m {
			if n > 0 {
				out = append(out, string(cards.ManaSymbol(i)))
				break
			}
		}
	}
	return out
}

func choicePlanUses(p decision.PaymentPlan, id state.ObjID) (decision.PaymentActivation, bool) {
	for _, a := range p.Activations {
		if a.Source == id {
			return a, true
		}
	}
	return decision.PaymentActivation{}, false
}

// TestPaymentPlanChoiceSources is the ticket's Done-means group. Each subtest
// asserts its own precondition (the source really is on the battlefield and
// the plan really exercised it) so an empty plan cannot pass silently.
func TestPaymentPlanChoiceSources(t *testing.T) {
	t.Run("combo land funds and executes with no colour ask", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9401, "Name:White Probe\nManaCost:W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		gate := onBoardCard(t, e, 0, choiceCard(t, "Selesnya Guildgate"))
		if o := e.G.Obj(gate); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
			t.Fatalf("precondition: Guildgate zone=%v tapped=%v", o == nil, o.Tapped)
		}
		got := e.PlanCastPayment(0, paymentCast(spell))
		a, ok := choicePlanUses2(got, gate)
		if !ok || a.Produces[state.ManaIndex('W')] != 1 || len(got.Plan.Activations) != 1 {
			t.Fatalf("plan = %+v reason=%s, want the Guildgate -> W", got.Plan, got.Reason)
		}
		if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err != nil {
			t.Fatalf("validate Guildgate witness: %v", err)
		}
		d := ppAsk(t, e)
		act := ppAction(t, d, spell)
		ppSubmitPlan(t, e, d, act)
		nd := e.Pending()
		if nd == nil || nd.Kind != decision.KPriority {
			t.Fatalf("after the planned cast pending = %#v, want the ordinary priority (no colour ask)", nd)
		}
		if !e.G.Obj(gate).Tapped {
			t.Error("Guildgate was not tapped by the executed plan")
		}
		if z := e.G.Obj(spell).Zone; z != state.ZStack {
			t.Errorf("spell zone after payment = %s, want stack", z)
		}
		if p := e.G.Players[0].Pool.Total(); p != 0 {
			t.Errorf("pool after payment = %d, want 0", p)
		}
	})

	t.Run("combo dual backtracks over its two colours", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9402, "Name:WU Probe\nManaCost:W U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		onBoard(t, e, 0, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
		dual := onBoard(t, e, 0, "Name:Combo Dual\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo W U\nOracle:x\n")
		got := e.PlanCastPayment(0, paymentCast(spell))
		if got.Plan == nil || len(got.Plan.Activations) != 2 {
			t.Fatalf("plan = %+v reason=%s, want a two-source witness", got.Plan, got.Reason)
		}
		a, ok := choicePlanUses2(got, dual)
		if !ok || a.Produces[state.ManaIndex('U')] != 1 {
			t.Fatalf("dual activation = %+v (used=%v), want it producing U (the Plains covers W)", a, ok)
		}
		if err := e.ValidateCastPayment(0, paymentCast(spell), *got.Plan); err != nil {
			t.Fatalf("validate combo dual witness: %v", err)
		}
	})

	t.Run("chosen colour is planned and resolves", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9403, "Name:Red Probe\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		isle := onBoardCard(t, e, 0, choiceCard(t, "Thriving Isle"))
		e.G.Obj(isle).ChosenColor = "R"
		got := e.PlanCastPayment(0, paymentCast(spell))
		a, ok := choicePlanUses2(got, isle)
		if !ok || a.Produces[state.ManaIndex('R')] != 1 {
			t.Fatalf("want Thriving Isle -> R (chosen), got %+v reason=%s", a, got.Reason)
		}
		// Precondition: the recorded colour really drove a non-trivial set.
		if cols := choiceColours(choiceAlts(t, e, isle)); !reflect.DeepEqual(cols, []string{"U", "R"}) {
			t.Fatalf("Thriving Isle alternatives = %v, want [U R]", cols)
		}
	})

	t.Run("chosen colour unrecorded leaves only the fixed colour", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9404, "Name:White Probe\nManaCost:W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		isle := onBoardCard(t, e, 0, choiceCard(t, "Thriving Isle"))
		if c := e.G.Obj(isle).ChosenColor; c != "" {
			t.Fatalf("precondition: ChosenColor = %q, want empty", c)
		}
		cols := choiceColours(choiceAlts(t, e, isle))
		if !reflect.DeepEqual(cols, []string{"U"}) {
			t.Fatalf("Thriving Isle alternatives with nothing recorded = %v, want [U]", cols)
		}
		// A {R} instant with only the Isle and no recorded colour has no plan.
		if got := e.PlanCastPayment(0, paymentCast(choiceHand(t, e, "Name:R Probe\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"))); got.Plan != nil {
			t.Fatalf("unrecorded Chosen planned %+v, want insufficient", got.Plan)
		}
	})

	t.Run("commander identity is planned", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9405, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		cmdr := onBoard(t, e, 0, "Name:Probe Commander\nManaCost:G W\nTypes:Legendary Creature Elf\nPT:2/2\nOracle:x\n")
		e.G.Players[0].Commanders = []state.ObjID{cmdr}
		tower := onBoardCard(t, e, 0, choiceCard(t, "Command Tower"))
		got := e.PlanCastPayment(0, paymentCast(spell))
		a, ok := choicePlanUses2(got, tower)
		if !ok || a.Produces[state.ManaIndex('G')] != 1 {
			t.Fatalf("want Command Tower -> G, got %+v reason=%s", a, got.Reason)
		}
		// commanderIdentityColours is WUBRG-ordered, so W precedes G.
		if cols := choiceColours(choiceAlts(t, e, tower)); !reflect.DeepEqual(cols, []string{"W", "G"}) {
			t.Fatalf("Command Tower alternatives = %v, want [W G]", cols)
		}
	})

	t.Run("commander identity excludes off-identity colours", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9406, "Name:Blue Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		cmdr := onBoard(t, e, 0, "Name:Probe Commander\nManaCost:G W\nTypes:Legendary Creature Elf\nPT:2/2\nOracle:x\n")
		e.G.Players[0].Commanders = []state.ObjID{cmdr}
		tower := onBoardCard(t, e, 0, choiceCard(t, "Command Tower"))
		u := choiceHand(t, e, "Name:Blue Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		if got := e.PlanCastPayment(0, paymentCast(u)); got.Plan != nil || got.Reason != "insufficient" {
			t.Fatalf("off-identity {U} with only the Tower = %+v reason=%q, want insufficient", got.Plan, got.Reason)
		}
		_ = tower
	})

	t.Run("commander identity with no commander has no alternatives", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9407, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		if n := len(e.G.Players[0].Commanders); n != 0 {
			t.Fatalf("precondition: Commanders = %d, want none", n)
		}
		tower := onBoardCard(t, e, 0, choiceCard(t, "Command Tower"))
		if alts := choiceAlts(t, e, tower); len(alts) != 0 {
			t.Fatalf("Tower without a commander offered %v, want no alternatives", choiceColours(alts))
		}
	})

	t.Run("commander identity change falls back production_changed", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9408, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
		cmdr := onBoard(t, e, 0, "Name:Probe Commander\nManaCost:G W\nTypes:Legendary Creature Elf\nPT:2/2\nOracle:x\n")
		e.G.Players[0].Commanders = []state.ObjID{cmdr}
		tower := onBoardCard(t, e, 0, choiceCard(t, "Command Tower"))
		d := paymentPlanReask(t, e)
		a := paymentPlanActionFor(t, d, spell)
		if act, ok := choicePlanUses(a.Plans[0], tower); !ok || act.Produces[state.ManaIndex('G')] != 1 {
			t.Fatalf("precondition: plan = %#v, want the Tower producing G", a.Plans[0])
		}
		submitPaymentPlan(t, e, d, a)
		if td := e.Pending(); td == nil || td.Kind != decision.KTarget {
			t.Fatalf("pending after submit = %s, want the target ask", paymentPlanPendingSummary(td))
		}
		// Post-offer change: a {U} commander reshapes the identity.
		cmdr2 := onBoard(t, e, 0, "Name:Probe Commander Two\nManaCost:U\nTypes:Legendary Creature Elf\nPT:2/2\nOracle:x\n")
		e.G.Players[0].Commanders = []state.ObjID{cmdr2}
		start := len(e.L.Events)
		submitChoices(t, e, 0)
		if n := paymentPlanTapsSince(e, start, tower); n != 0 {
			t.Fatalf("Tower tapped %d times after its identity changed", n)
		}
		nd := e.Pending()
		if nd == nil || nd.PaymentFallback == nil || nd.PaymentFallback.Reason != paymentFallbackProductionChanged {
			t.Fatalf("pending = %s, want production_changed before tapping", paymentPlanPendingSummary(nd))
		}
		if nd.PaymentFallback.PlanID != a.Plans[0].ID {
			t.Errorf("fallback plan = %q, want %q", nd.PaymentFallback.PlanID, a.Plans[0].ID)
		}
	})

	t.Run("last-resort coloured half is planned only as a last resort, its colourless normally", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9409, "Name:White Probe\nManaCost:W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		wastes := onBoardCard(t, e, 0, choiceCard(t, "Adarkar Wastes"))
		// The {C} ability is the only normal alternative; the coloured pain
		// half is last resort with damage:1 (aph-last-resort-plans).
		var normal []state.Mana
		for _, u := range e.paymentPlanManaUnits(0) {
			if u.id != wastes {
				continue
			}
			for _, a := range e.paymentPlanUnitAlternatives(u) {
				switch a.tier {
				case paymentTierNormal:
					normal = append(normal, a.mana)
				case paymentTierLastResort:
					if a.consequence != (paymentConsequence{damage: 1}) {
						t.Fatalf("pain half consequence = %+v, want damage:1", a.consequence)
					}
				}
			}
		}
		if cols := choiceColours(normal); !reflect.DeepEqual(cols, []string{"C"}) {
			t.Fatalf("Adarkar Wastes normal alternatives = %v, want the {C} ability only (the pain half is last resort)", cols)
		}
		if cols := choiceColours(choiceAlts(t, e, wastes)); !reflect.DeepEqual(cols, []string{"C", "W", "U"}) {
			t.Fatalf("Adarkar Wastes alternatives = %v, want C then the last-resort W and U", cols)
		}
		// A {W} instant with only the Wastes uses its coloured half as a last
		// resort, disclosing the damage.
		w := choiceHand(t, e, "Name:W Probe\nManaCost:W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		if got := e.PlanCastPayment(0, paymentCast(w)); got.Plan == nil || len(got.Plan.Activations) != 1 ||
			got.Plan.Activations[0].Consequence == nil || *got.Plan.Activations[0].Consequence != (decision.PaymentConsequence{Damage: 1}) {
			t.Fatalf("{W} with only a painland = %+v reason=%q, want the pain half disclosing damage:1", got.Plan, got.Reason)
		}
		// And its {C} ability is a normal plan for a colourless cost.
		c := choiceHand(t, e, "Name:C Probe\nManaCost:C\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		got := e.PlanCastPayment(0, paymentCast(c))
		if a, ok := choicePlanUses2(got, wastes); !ok || a.Produces[state.ManaIndex('C')] != 1 || a.Consequence != nil {
			t.Fatalf("{C} plan = %+v reason=%s, want the Wastes' colourless ability", got.Plan, got.Reason)
		}
	})

	t.Run("witness colour outside the ability set is rejected", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9410, "Name:White Probe\nManaCost:W\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		gate := onBoardCard(t, e, 0, choiceCard(t, "Selesnya Guildgate"))
		got := e.PlanCastPayment(0, paymentCast(spell))
		if got.Plan == nil {
			t.Fatalf("precondition: no plan (%s)", got.Reason)
		}
		tampered := *got.Plan
		tampered.Activations = append([]decision.PaymentActivation(nil), got.Plan.Activations...)
		bad := tamperedIndex(tampered.Activations, gate)
		var m decision.ManaAmount
		m[state.ManaIndex('U')] = 1 // outside the Guildgate's {G,W}
		tampered.Activations[bad].Produces = m
		if err := e.ValidateCastPayment(0, paymentCast(spell), tampered); err == nil {
			t.Fatal("ValidateCastPayment accepted a witness naming an off-ability colour")
		}
	})
}

// tamperedIndex returns the index of the activation naming source id, failing
// if the tamper target is not present.
func tamperedIndex(acts []decision.PaymentActivation, id state.ObjID) int {
	for i := range acts {
		if acts[i].Source == id {
			return i
		}
	}
	panic("activation for source not found")
}

// choicePlanUses2 is choicePlanUses over a PaymentPlanOutcome.
func choicePlanUses2(got PaymentPlanOutcome, id state.ObjID) (decision.PaymentActivation, bool) {
	if got.Plan == nil {
		return decision.PaymentActivation{}, false
	}
	return choicePlanUses(*got.Plan, id)
}

// choiceHand adds an authored card straight to seat 0's hand and returns its
// object id, so a PlanCastPayment can be asked about a cost the fixture deck
// did not already hold.
func choiceHand(t *testing.T, e *Engine, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	return o.ID
}

// TestPaymentPlanChoiceSourcesComboAnyStaysDeferred pins the out-of-scope
// boundary: Combo Any and a multi-unit Combo allocation never produce an
// alternative.
func TestPaymentPlanChoiceSourcesComboAnyStaysDeferred(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 9411, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	comboAny := onBoard(t, e, 0, "Name:Combo Any Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo Any\nOracle:x\n")
	if alts := choiceAlts(t, e, comboAny); len(alts) != 0 {
		t.Fatalf("Combo Any offered %v, want no alternatives", choiceColours(alts))
	}
	alloc := onBoard(t, e, 0, "Name:Combo Alloc Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo W U | Amount$ 2\nOracle:x\n")
	if alts := choiceAlts(t, e, alloc); len(alts) != 0 {
		t.Fatalf("amount-2 Combo offered %v, want no alternatives", choiceColours(alts))
	}
}

// TestPaymentPlanChoiceSourcesPathOfAncestryStaysDeferred pins the brief's
// TriggersWhenSpent exclusion on the real corpus card.
func TestPaymentPlanChoiceSourcesPathOfAncestryStaysDeferred(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 9412, "Name:Green Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	path := onBoardCard(t, e, 0, choiceCard(t, "Path of Ancestry"))
	if alts := choiceAlts(t, e, path); len(alts) != 0 {
		t.Fatalf("Path of Ancestry offered %v, want no alternatives (TriggersWhenSpent)", choiceColours(alts))
	}
}
