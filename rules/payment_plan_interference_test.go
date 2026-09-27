package rules

// aph-interference-scope: the V1 planner scopes mana/tap interference to the
// sources it can affect (spec 3.2 as amended 2026-09-26). A trigger or
// replacement on one source affects only that source's tier; an opponent's
// Mana Vault / City of Brass / Claustrophobia / Manabarbs / cost static never
// suppresses another player's plan; only an unprovable global effect declines
// with `global_mana_effect`.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ppiCorpus reads a corpus card by name, failing loudly when absent (a
// skipped corpus test is not a pass).
func ppiCorpus(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q missing", name)
	}
	return c
}

// ppiPlanUses reports whether a witness names id, returning the activation.
func ppiPlanUses(got PaymentPlanOutcome, id state.ObjID) (decision.PaymentActivation, bool) {
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

// ppiManaAbility is the payment-window mana ability on id the classifier
// reads. The PRECONDITION matters: a nil ability would make every assertion
// below vacuous.
func ppiManaAbility(t *testing.T, e *Engine, id state.ObjID) *cards.SA {
	t.Helper()
	alts := e.availableManaAbilitiesForWindow(0, id, false)
	if len(alts) == 0 {
		t.Fatalf("fixture: no payment-window mana ability on %d", id)
	}
	return alts[0]
}

// ppiTier classifies id's first mana ability through the source-shape
// authority the planner uses.
func ppiTier(t *testing.T, e *Engine, id state.ObjID) (paymentAbilityTier, paymentConsequence, string) {
	t.Helper()
	ma := ppiManaAbility(t, e, id)
	return e.paymentPlanAbilityTier(e.paymentPlanController(id), id, ma)
}

// ppiUntargetedSpell is the untargeted {U} probe used across the opponent
// cases.
const ppiUntargetedSpell = "Name:Blue Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"

// TestPaymentPlanInterferenceOpponentCardsLeaveTheIslandPlan is the
// done-means core: each corpus card placed on the OPPONENT's battlefield
// whose effect does not reach seat 0's Island leaves seat 0's Island plan
// byte-identical to the plan without it.  Manabarbs is deliberately NOT here:
// its `TapsForMana | ValidCard$ Land` trigger DOES match the Island, so spec
// 3.2/PP-09 defers the Island (it is not an effect printed on the opponent's
// own permanent); TestPaymentPlanInterferenceOpponentTapTriggerDefers covers
// it.  See the report's Deviations: the attached brief's done-means lists
// Manabarbs among the identical-plan cards, which contradicts its own item 1
// ("... Manabarbs" as a deferring trigger), spec PP-09 and the existing
// pp-matrix test.
func TestPaymentPlanInterferenceOpponentCardsLeaveTheIslandPlan(t *testing.T) {
	for _, name := range []string{
		"Mana Vault", "Grim Monolith", "Basalt Monolith", "City of Brass",
		"Claustrophobia", "Engineered Explosives",
		"Syr Elenora, the Discerning", "Icefall Regent",
	} {
		t.Run(name, func(t *testing.T) {
			e, _, spell := newFixtureDeck(t, 9901, ppiUntargetedSpell)
			island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
			before := e.PlanCastPayment(0, paymentCast(spell))
			if before.Plan == nil {
				t.Fatalf("fixture: no baseline plan: %+v", before)
			}
			// PRECONDITION: the baseline plan actually uses the Island.
			if _, ok := ppiPlanUses(before, island); !ok {
				t.Fatalf("fixture: baseline plan does not use the Island: %+v", before.Plan)
			}
			opp := onBoardCard(t, e, 1, ppiCorpus(t, name))
			if e.G.Obj(opp) == nil || e.G.Obj(opp).Zone != state.ZBattlefield {
				t.Fatalf("fixture: opponent card %q not on the battlefield", name)
			}
			after := e.PlanCastPayment(0, paymentCast(spell))
			if after.Plan == nil {
				t.Fatalf("opponent's %s disabled the Island plan: %+v", name, after)
			}
			if !reflect.DeepEqual(before.Plan, after.Plan) {
				t.Fatalf("opponent's %s changed the plan:\n before %+v\n after  %+v", name, before.Plan, after.Plan)
			}
		})
	}
}

// TestPaymentPlanInterferenceOwnWildGrowthPicksTheCleanIsland: seat 0's own
// Wild Growth on Island A defers A; an untouched Island B funds the {U} plan.
func TestPaymentPlanInterferenceOwnWildGrowthPicksTheCleanIsland(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9902, ppiUntargetedSpell)
	wild := onBoardCard(t, e, 0, ppiCorpus(t, "Wild Growth"))
	islandA := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	islandB := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	// PRECONDITION: Wild Growth is attached to A (its ValidCard$ is
	// Card.AttachedBy), and both Islands are distinct untapped sources.
	e.G.Obj(wild).AttachedTo = islandA
	e.staticEpoch, e.activeEpoch = -1, -1
	if e.G.Obj(islandA).Tapped || e.G.Obj(islandB).Tapped {
		t.Fatal("fixture: Islands must start untapped")
	}
	if islandA == islandB {
		t.Fatal("fixture: the two Islands must be distinct objects")
	}
	// A itself is deferred (Wild Growth's TapsForMana can match it).
	if tier, _, detail := ppiTier(t, e, islandA); tier != paymentTierDeferred {
		t.Fatalf("Wild Growth's land tier = %v detail=%q, want deferred", tier, detail)
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("no plan with a clean Island B available: %+v", got)
	}
	if _, ok := ppiPlanUses(got, islandA); ok {
		t.Fatalf("plan used the Wild Growth Island A: %+v", got.Plan.Activations)
	}
	if _, ok := ppiPlanUses(got, islandB); !ok {
		t.Fatalf("plan did not use the clean Island B: %+v", got.Plan.Activations)
	}
}

// TestPaymentPlanInterferenceOwnConsequencesAreLastResort: seat 0's own City
// of Brass is `damage:1` last resort and Mana Vault is `no_untap` last
// resort; neither appears in a plan, yet another clean source still funds it.
func TestPaymentPlanInterferenceOwnConsequencesAreLastResort(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9903, ppiUntargetedSpell)
	brass := onBoardCard(t, e, 0, ppiCorpus(t, "City of Brass"))
	vault := onBoardCard(t, e, 0, ppiCorpus(t, "Mana Vault"))
	island := onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")

	for _, tc := range []struct {
		name string
		id   state.ObjID
		want paymentConsequence
	}{
		{"City of Brass", brass, paymentConsequence{damage: 1}},
		{"Mana Vault", vault, paymentConsequence{noUntap: true}},
	} {
		tier, consequence, detail := ppiTier(t, e, tc.id)
		if tier != paymentTierLastResort {
			t.Fatalf("%s tier = %v detail=%q, want last resort", tc.name, tier, detail)
		}
		if consequence != tc.want {
			t.Fatalf("%s consequence = %+v, want %+v", tc.name, consequence, tc.want)
		}
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("no plan with a clean Island available: %+v", got)
	}
	for _, id := range []state.ObjID{brass, vault} {
		if _, ok := ppiPlanUses(got, id); ok {
			t.Fatalf("plan used a last-resort source %d: %+v", id, got.Plan.Activations)
		}
	}
	if _, ok := ppiPlanUses(got, island); !ok {
		t.Fatalf("plan did not use the clean Island: %+v", got.Plan.Activations)
	}
}

// TestPaymentPlanInterferenceManaReflectionDefersOnlyItsControllersSources:
// seat 0's Mana Reflection defers every seat-0 source with a detail naming
// it; seat 1 plans normally.
func TestPaymentPlanInterferenceManaReflectionDefersOnlyItsControllersSources(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9904, ppiUntargetedSpell)
	reflection := onBoardCard(t, e, 0, ppiCorpus(t, "Mana Reflection"))
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	// PRECONDITION: the reflection is on seat 0's battlefield.
	if e.G.Obj(reflection) == nil || e.G.Obj(reflection).Controller != 0 {
		t.Fatal("fixture: Mana Reflection must be controlled by seat 0")
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan != nil {
		t.Fatalf("seat 0 has a plan under its own Mana Reflection: %+v", got.Plan)
	}
	if got.Reason != "insufficient" || !strings.Contains(got.Detail, "Mana Reflection") {
		t.Fatalf("seat 0 outcome = %+v, want insufficient and a detail naming Mana Reflection", got)
	}

	// Seat 1 is unaffected: give it an Island and a {U} spell in hand.
	seat1Island := onBoard(t, e, 1, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	seat1Spell := e.G.AddObject(card(t, ppiUntargetedSpell), 1)
	seat1Spell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), seat1Spell.ID))
	e.staticEpoch, e.activeEpoch = -1, -1
	// PRECONDITION: seat 1 holds the spell and controls the Island.
	if o := e.G.Obj(seat1Spell.ID); o == nil || o.Zone != state.ZHand || o.Owner != 1 {
		t.Fatal("fixture: seat 1 spell not in seat 1's hand")
	}
	if o := e.G.Obj(seat1Island); o == nil || o.Controller != 1 {
		t.Fatal("fixture: seat 1 Island not controlled by seat 1")
	}
	got1 := e.PlanCastPayment(1, paymentCast(seat1Spell.ID))
	if got1.Plan == nil {
		t.Fatalf("seat 1 has no plan under seat 0's Mana Reflection: %+v", got1)
	}
	if _, ok := ppiPlanUses(got1, seat1Island); !ok {
		t.Fatalf("seat 1's plan did not use its Island: %+v", got1.Plan.Activations)
	}
}

// TestPaymentPlanInterferenceCelestialDawnIsGlobal: a ManaConvert static
// reaching the payer declines every plan with a global_mana_effect detail.
func TestPaymentPlanInterferenceCelestialDawnIsGlobal(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9905, "Name:Green Instant Test\nManaCost:G\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Dawn Test\nTypes:Enchantment\nS:Mode$ ManaConvert | ValidPlayer$ You | ManaConversion$ White->AnyColor nonWhite<-C | Description$ Fixture: white as any colour, other mana only as colorless.\nOracle:x\n")
	elf := onBoard(t, e, 0, "Name:Elf Test\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add G.\nOracle:x\n")
	e.G.Obj(elf).SummonSick = false
	// PRECONDITION: the elf is a legal, untapped source on seat 0's board.
	if ma := e.availableManaAbilitiesForWindow(0, elf, false); len(ma) == 0 {
		t.Fatal("fixture: elf has no payment-window mana ability")
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan != nil {
		t.Fatalf("plan under Celestial Dawn: %+v", got.Plan)
	}
	if !strings.HasPrefix(got.Detail, "global_mana_effect") {
		t.Fatalf("Celestial Dawn detail = %q, want a global_mana_effect prefix", got.Detail)
	}
}

// TestPaymentPlanInterferenceTargetedSpellStillDeclines: an opponent's
// ValidTarget$-only cost static declines a spell that ACTUALLY targets.
func TestPaymentPlanInterferenceTargetedSpellStillDeclines(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9906,
		"Name:Targeted Probe\nManaCost:U\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | TgtPrompt$ Select target creature | NumAtt$ +1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	target := onBoard(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// PRECONDITION: the spell declares a target and a legal target exists.
	if !e.paymentPlanSpellTargets(spell) {
		t.Fatal("fixture: the probe must declare a target")
	}
	if e.G.Obj(target) == nil || e.G.Obj(target).Zone != state.ZBattlefield {
		t.Fatal("fixture: target creature must be on the battlefield")
	}
	onBoardCard(t, e, 1, ppiCorpus(t, "Syr Elenora, the Discerning"))
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan != nil {
		t.Fatalf("targeted spell got a plan under an opponent's ValidTarget cost static: %+v", got.Plan)
	}
	if got.Detail != "shape:target_dependent_cost" {
		t.Fatalf("detail = %q, want shape:target_dependent_cost", got.Detail)
	}
}

// TestPaymentPlanInterferenceOpponentTapTriggerDefers: an opponent's
// Manabarbs (`TapsForMana | ValidCard$ Land`) matches the Island, so the
// Island is deferred and no plan funds the probe.  A deferral is
// `insufficient`, not the old whole-offer `unsupported`, which is exactly
// what the census kill-switch's "opponent-side trigger rows report 0"
// measures.
func TestPaymentPlanInterferenceOpponentTapTriggerDefers(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9908, ppiUntargetedSpell)
	onBoard(t, e, 0, "Name:Island\nTypes:Basic Land Island\nOracle:x\n")
	barbs := onBoardCard(t, e, 1, ppiCorpus(t, "Manabarbs"))
	// PRECONDITION: the opponent's Manabarbs is on the battlefield and its
	// trigger really does match a hypothetical tap of seat 0's land.
	if e.G.Obj(barbs) == nil || e.G.Obj(barbs).Zone != state.ZBattlefield {
		t.Fatal("fixture: Manabarbs must be on the opponent's battlefield")
	}
	if global, _ := e.paymentPlanGlobalManaEffectFor(0, spell); global {
		t.Fatal("fixture: Manabarbs must be per-source, not a global mana effect")
	}
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan != nil {
		t.Fatalf("opponent's Manabarbs left a plan: %+v", got.Plan)
	}
	if got.Reason == "unsupported" {
		t.Fatalf("Manabarbs declined the whole offer (%+v), want a per-source deferral", got)
	}
}

// TestPaymentPlanInterferenceUntargetedSpellIsNotTargeted: guard
// paymentPlanSpellTargets against a regression that would make every spell
// look targeted.
func TestPaymentPlanInterferenceUntargetedSpellIsNotTargeted(t *testing.T) {
	e, _, spell := newFixtureDeck(t, 9907, ppiUntargetedSpell)
	if e.paymentPlanSpellTargets(spell) {
		t.Fatal("an untargeted instant was reported as targeting")
	}
}
