package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestPaymentPlanShapeGate(t *testing.T) {
	for i, tc := range []struct {
		name, face, detail string
	}{
		{"sacrifice", "A:SP$ Draw | Cost$ B Sac<1/Creature> | NumCards$ 1", "shape:additional_cost"},
		{"discard", "A:SP$ Draw | Cost$ B Discard<1/Card> | NumCards$ 1", "shape:additional_cost"},
		{"life", "A:SP$ Draw | Cost$ B PayLife<2> | NumCards$ 1", "shape:additional_cost"},
		{"exile", "A:SP$ Draw | Cost$ B Exile<1/Card> | NumCards$ 1", "shape:additional_cost"},
		{"tapXType", "A:SP$ Draw | Cost$ B tapXType<1/Creature> | NumCards$ 1", "shape:additional_cost"},
		{"revealOrChoose", "A:SP$ Draw | Cost$ B RevealOrChoose<1/Creature> | NumCards$ 1", "shape:additional_cost"},
		{"exert", "A:SP$ Draw | Cost$ B Exert<1/CARDNAME> | NumCards$ 1", "shape:additional_cost"},
		{"delve", "K:Delve\nA:SP$ Draw | NumCards$ 1", "shape:contribution"},
		{"improvise", "K:Improvise\nA:SP$ Draw | NumCards$ 1", "shape:contribution"},
		{"convoke", "K:Convoke\nA:SP$ Draw | NumCards$ 1", "shape:contribution"},
		{"gift", "K:Gift:1\nA:SP$ Draw | NumCards$ 1", "shape:gift"},
		{"spree", "K:Spree\nA:SP$ Charm | Choices$ DBOne | MinCharmNum$ 1\nSVar:DBOne:DB$ Draw | NumCards$ 1 | ModeCost$ 1", "shape:modal_cost"},
		{"tiered", "K:Tiered\nA:SP$ Charm | Choices$ DBOne | MinCharmNum$ 1\nSVar:DBOne:DB$ Draw | NumCards$ 1 | ModeCost$ 1", "shape:modal_cost"},
		{"modeCost", "A:SP$ Charm | Choices$ DBOne | MinCharmNum$ 1\nSVar:DBOne:DB$ Draw | NumCards$ 1 | ModeCost$ 1", "shape:modal_cost"},
		{"replicate", "K:Replicate:1\nA:SP$ Draw | NumCards$ 1", "shape:optional_cost"},
		{"multikicker", "K:Multikicker:1\nA:SP$ Draw | NumCards$ 1", "shape:optional_cost"},
		{"squad", "K:Squad:1\nA:SP$ Draw | NumCards$ 1", "shape:optional_cost"},
		{"manaSpentCondition", "A:SP$ Draw | NumCards$ 1\nSVar:Check:ConditionManaSpent$ G", "shape:mana_spent_reader"},
		{"adamant", "A:SP$ Draw | NumCards$ 1\nSVar:Check:Count$Adamant", "shape:mana_spent_reader"},
		{"eachSpent", "A:SP$ Draw | NumCards$ 1\nSVar:Check:Count$EachSpentToCast", "shape:mana_spent_reader"},
		{"totalSpent", "A:SP$ Draw | NumCards$ 1\nSVar:Check:Count$TotalManaSpent", "shape:mana_spent_reader"},
		{"manaSpentBy", "A:SP$ Draw | NumCards$ 1\nSVar:Check:ManaSpentBy$ You", "shape:mana_spent_reader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Shape Gate Spell\nManaCost:B\nTypes:Instant\n" + tc.face + "\nOracle:test\n"
			e, _, spell := newFixtureDeck(t, uint64(9500+i), src)
			onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:test\n")
			if tc.name == "sacrifice" || tc.name == "tapXType" || tc.name == "revealOrChoose" {
				onBoard(t, e, 0, "Name:Victim\nManaCost:1\nTypes:Creature Test\nPT:1/1\nOracle:test\n")
			}
			got := e.PlanCastPayment(0, paymentCast(spell))
			if got.Plan != nil || got.Reason != "unsupported" || got.Detail != tc.detail {
				t.Fatalf("plan outcome = %+v, want unsupported detail %q", got, tc.detail)
			}
			e.G.Players[0].Pool[state.MB] = 1 // model floating the required mana before the legacy cast action.
			e.pending = nil
			e.askPriority(0)
			legacy := false
			for _, opt := range e.Pending().Options {
				if opt.Kind == "cast" && opt.Obj == spell {
					legacy = true
					break
				}
			}
			if !legacy {
				t.Fatalf("legacy cast option was removed for %s", tc.name)
			}
		})
	}
}

// PP-08 keeps the cost-class exclusions that were already in place before
// this ticket: an {X}, hybrid, Phyrexian or snow printed cost still receives
// no plan, now with the machine-readable cost detail. This is the invariant
// the gaps audit proved on main; it must not regress when the additional-cost
// and contribution gates above were added.
func TestPaymentPlanShapeGateCostClassesStillDecline(t *testing.T) {
	for i, tc := range []struct {
		name, manaCost, detail string
	}{
		{"x", "X R", "cost:x"},
		{"hybrid", "R/G", "cost:hybrid"},
		{"phyrexian", "R/P", "cost:phyrexian"},
		{"snow", "S", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Cost Class Spell\nManaCost:" + tc.manaCost + "\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:test\n"
			e, _, spell := newFixtureDeck(t, uint64(9560+i), src)
			onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:test\n")
			onBoard(t, e, 0, "Name:Snow Mountain\nTypes:Basic Snow Land Mountain\nOracle:test\n")
			onBoard(t, e, 0, "Name:Forest\nTypes:Basic Land Forest\nOracle:test\n")
			got := e.PlanCastPayment(0, paymentCast(spell))
			// A snow cost is refused even earlier (the candidate walk), so its
			// Detail is empty; the contract here is only that no plan is
			// offered. X/hybrid/Phyrexian reach the cost classifier and name
			// their class.
			if got.Plan != nil || got.Reason != "unsupported" || (tc.detail != "" && got.Detail != tc.detail) {
				t.Fatalf("cost class %s outcome = %+v, want unsupported detail %q", tc.name, got, tc.detail)
			}
		})
	}
}

func TestPaymentPlanShapeGateGrantedImprovise(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9520, "Name:Nonartifact Spell\nManaCost:1 B\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:test\n")
	stat := onBoardCard(t, e, 0, corpusCard(t, "Inspiring Statuary"))
	if e.G.Obj(stat) == nil || e.G.Obj(spell).Zone != state.ZHand {
		t.Fatal("grant source or spell fixture missing")
	}
	if !e.hasCastImprovise(spell) {
		t.Fatal("fixture failed to grant Improvise to the announced spell")
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan != nil || got.Reason != "unsupported" || got.Detail != "shape:contribution" {
		t.Fatalf("granted Improvise outcome = %+v, want contribution decline", got)
	}
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MB] = 1
	e.pending = nil
	e.askPriority(0)
	for _, opt := range e.Pending().Options {
		if opt.Kind == "cast" && opt.Obj == spell {
			return
		}
	}
	t.Fatal("legacy cast option was removed for granted Improvise")
}

func TestPaymentPlanShapeGateSpreeOffers(t *testing.T) {
	for _, tc := range []struct {
		name, manaCost, modeCost string
		sources                  int
	}{
		{name: "one_source", manaCost: "", modeCost: "B", sources: 1},
		{name: "two_sources", manaCost: "B", modeCost: "B", sources: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:Spree Gate Spell\nManaCost:" + tc.manaCost + "\nTypes:Instant\nK:Spree\n" +
				"A:SP$ Charm | Choices$ DBOne | MinCharmNum$ 1\n" +
				"SVar:DBOne:DB$ Draw | NumCards$ 1 | ModeCost$ " + tc.modeCost + "\nOracle:test\n"
			e, _, spell := newFixtureDeck(t, uint64(9530+tc.sources), src)
			for i := 0; i < tc.sources; i++ {
				onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:test\n")
			}
			if e.G.Obj(spell).Zone != state.ZHand {
				t.Fatal("Spree spell is not in hand")
			}
			if detail := e.paymentPlanCastShapeDetail(0, spell); detail != "shape:modal_cost" {
				t.Fatalf("Spree shape detail = %q, want modal-cost decline", detail)
			}
			for _, action := range e.PaymentActionsForPriority(0, 77) {
				if action.Cast.Object == spell {
					t.Fatalf("Spree received payment action before mana floated: %+v", action)
				}
			}
			if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil {
				t.Fatalf("Spree plan = %+v, want no plan", got)
			}
			if tc.name == "one_source" {
				e.G.Players[0].Pool[state.MC] = 1
			} else {
				e.G.Players[0].Pool[state.MB] = 2
			}
			e.askPriority(0)
			for _, action := range e.Pending().PaymentActions {
				if action.Cast.Object == spell {
					t.Fatalf("Spree received payment action: %+v", action)
				}
			}
			for _, opt := range e.Pending().Options {
				if opt.Kind == "cast" && opt.Obj == spell {
					return
				}
			}
			t.Fatalf("ordinary Spree cast option was removed; options=%+v", e.Pending().Options)
		})
	}
}

// The executor half of the audit story (ticket autopay-exec-harden) is no
// longer reachable through the planner offer for these shapes, because PP-08
// withholds it. Build the witness directly, exactly as
// TestPaymentPlanExecutesAfterCastTimeChoice does for sacrifice, and prove the
// executor still pays the whole cost after the additional-cost choice: the
// spell must not sit on the stack unpaid. This test owns the discard and delve
// fixtures; sacrifice stays in the existing test.
func TestPaymentPlanShapeGateAdditionalCostExecutorPays(t *testing.T) {
	red := state.Mana{}
	red[state.ManaIndex('R')] = 1

	mountainWitness := func(e *Engine, cost decision.PaymentCost, sources ...state.ObjID) decision.PaymentPlan {
		acts := make([]decision.PaymentActivation, 0, len(sources))
		for _, id := range sources {
			var m state.Mana
			m[state.ManaIndex('R')] = 1
			acts = append(acts, decision.PaymentActivation{
				Source: id, SourceZoneSeq: e.paymentSourceZoneSeq(id),
				Ability:  decision.PaymentAbility{Kind: decision.PaymentAbilityIntrinsic, Intrinsic: "basic_land"},
				Produces: paymentManaAmount(m)})
		}
		return decision.PaymentPlan{Version: decision.PaymentPlanV1, Cost: cost, Activations: acts}
	}

	t.Run("discard", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9570, "Name:Gate Thrill Shape\nManaCost:1 R\nTypes:Instant\nA:SP$ Draw | Cost$ 1 R Discard<1/Card> | NumCards$ 2\nOracle:x\n")
		m1 := onBoard(t, e, 0, paymentPlanMountain)
		m2 := onBoard(t, e, 0, paymentPlanMountain)
		var discard state.ObjID
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if id != spell {
				discard = id
				break
			}
		}
		if discard == 0 || e.G.Obj(discard) == nil {
			t.Fatal("fixture has no second card to discard")
		}
		redAmt := decision.ManaAmount{0, 0, 0, 1, 0, 0}
		plan := mountainWitness(e, decision.PaymentCost{Generic: 1, Mana: redAmt}, m1, m2)
		start := len(e.L.Events)
		e.pending = nil
		e.beginCastWithPayment(0, decision.Option{Kind: "cast", Obj: spell}, &decision.PaymentSelection{ActionID: "fixture", Plan: plan})
		paymentPlanChooseObj(t, e, discard)
		if fb := paymentPlanSettle(t, e, plan); fb != nil {
			t.Fatalf("direct discard witness fell back: %#v", fb)
		}
		assertPlannedCastPaid(t, e, spell, start, 1, red)
		if z := e.G.Obj(discard).Zone; z != state.ZGraveyard {
			t.Errorf("discarded card zone = %s, want graveyard (additional cost unpaid)", z)
		}
	})

	delve := func(t *testing.T, seed uint64) (*Engine, state.ObjID, []state.ObjID, []state.ObjID) {
		e, _, spell := newFixtureDeck(t, seed, "Name:Gate Delve Shape\nManaCost:2 R\nTypes:Instant\nK:Delve\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
		lands := []state.ObjID{onBoard(t, e, 0, paymentPlanMountain), onBoard(t, e, 0, paymentPlanMountain), onBoard(t, e, 0, paymentPlanMountain)}
		var gy []state.ObjID
		for _, id := range e.G.Zone(state.ZLibrary, 0)[:2] {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
			gy = append(gy, id)
		}
		return e, spell, lands, gy
	}

	t.Run("delve nothing", func(t *testing.T) {
		e, spell, lands, _ := delve(t, 9571)
		redAmt := decision.ManaAmount{0, 0, 0, 1, 0, 0}
		plan := mountainWitness(e, decision.PaymentCost{Generic: 2, Mana: redAmt}, lands...)
		start := len(e.L.Events)
		e.pending = nil
		e.beginCastWithPayment(0, decision.Option{Kind: "cast", Obj: spell}, &decision.PaymentSelection{ActionID: "fixture", Plan: plan})
		if dd := e.Pending(); dd == nil || dd.Kind != decision.KChoose || dd.Options[0].Kind != "exile" {
			t.Fatalf("pending = %s, want the delve ask", paymentPlanPendingSummary(dd))
		}
		submitChoices(t, e)
		if fb := paymentPlanSettle(t, e, plan); fb != nil {
			t.Fatalf("direct delve witness fell back: %#v", fb)
		}
		assertPlannedCastPaid(t, e, spell, start, 2, red)
		for _, id := range lands {
			if !e.G.Obj(id).Tapped {
				t.Errorf("planned source %d was not tapped", id)
			}
		}
	})

	t.Run("delve one card", func(t *testing.T) {
		e, spell, lands, gy := delve(t, 9572)
		redAmt := decision.ManaAmount{0, 0, 0, 1, 0, 0}
		plan := mountainWitness(e, decision.PaymentCost{Generic: 2, Mana: redAmt}, lands...)
		start := len(e.L.Events)
		e.pending = nil
		e.beginCastWithPayment(0, decision.Option{Kind: "cast", Obj: spell}, &decision.PaymentSelection{ActionID: "fixture", Plan: plan})
		paymentPlanChooseObj(t, e, gy[0])
		// The exiled card pays {1}: the plan's {2}{R} witness no longer
		// describes the mana cost, so automation must stop before tapping.
		pd := e.Pending()
		if pd == nil || pd.PaymentFallback == nil || pd.PaymentFallback.Reason != paymentFallbackCostChanged {
			t.Fatalf("pending = %s, want the manual window with a cost_changed fallback", paymentPlanPendingSummary(pd))
		}
		if n := len(producedManaSince(e, start)); n != 0 {
			t.Fatalf("executor produced %d mana for a changed cost", n)
		}
		paymentPlanSettle(t, e, plan)
		assertPlannedCastPaid(t, e, spell, start, 1, red)
		if z := e.G.Obj(gy[0]).Zone; z != state.ZExile {
			t.Errorf("delved card zone = %s, want exile (delve unpaid)", z)
		}
	})
}
