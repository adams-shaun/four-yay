package rules

import (
	"testing"

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
