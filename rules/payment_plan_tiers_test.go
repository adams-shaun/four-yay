package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Classification table (spec 3.2 as amended 2026-09-26). Each entry pins the
// tier, the first deterministic reason detail and the exact consequence of one
// corpus producer's ability.
func TestPaymentPlanTiers(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		tier   pay.Tier
		detail string
		check  func(pay.Consequence) bool
	}{
		// Normal producers: the whole resolution is "tap, add the mana".
		{"Island", pay.TierNormal, "", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Sol Ring", pay.TierNormal, "", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Birds of Paradise", pay.TierNormal, "", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Llanowar Elves", pay.TierNormal, "", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Gilded Lotus", pay.TierNormal, "", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Mind Stone", pay.TierNormal, "", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		// Last resort: the normal shape plus one fully determined consequence.
		{"Ancient Tomb", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Damage == 2 }},
		{"Tarnished Citadel", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Damage == 3 }},
		{"Adarkar Wastes", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Damage == 1 }},
		{"Elves of Deep Shadow", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Damage == 1 }},
		{"Mana Confluence", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Life == 1 }},
		{"Horizon Canopy", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Life == 1 }},
		{"Lotus Petal", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.Sacrifice }},
		{"Undiscovered Paradise", pay.TierLastResort, "source:last_resort", func(c pay.Consequence) bool { return c.ReturnToHand }},
		// Deferred: everything else, manual only.
		{"Witch Engine", pay.TierDeferred, "source:target", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Cryptolith Fragment", pay.TierDeferred, "source:rider", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Mox Poison", pay.TierDeferred, "source:rider", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Rainbow Vale", pay.TierDeferred, "source:rider", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"River of Tears", pay.TierDeferred, "source:conditional", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Gemstone Caverns", pay.TierDeferred, "source:conditional", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Pyromancer's Goggles", pay.TierDeferred, "source:special_production", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		// Chrome Mox's production is AB$ ManaReflected (a reflected-colour
		// read of the imprinted card), not a plain Mana ability, so the
		// classifier's closed-world API check defers it.
		{"Chrome Mox", pay.TierDeferred, "source:special_production", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Ashnod's Altar", pay.TierDeferred, "source:last_resort", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
		{"Cavern of Souls", pay.TierDeferred, "source:special_production", func(c pay.Consequence) bool { return c == (pay.Consequence{}) }},
	}
	e, _, _ := newFixtureDeck(t, 9810, "Name:Probe\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := corpusTierFixture(t, tc.name)
			id := onBoardCard(t, e, 0, c)
			if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: source is not on battlefield (zone %s)", o.Zone)
			}
			// The planner classifies both production APIs the mana window
			// offers: plain Mana and ManaReflected (Chrome Mox).
			abilities := append(e.G.Obj(id).Face().ManaAbilities(), e.G.Obj(id).Face().ManaReflectedAbilities()...)
			if len(abilities) == 0 {
				t.Fatalf("precondition: %s carries no mana ability", tc.name)
			}
			found := false
			for _, ma := range abilities {
				tier, consequence, detail := e.paymentPlanAbilityTier(0, id, ma)
				if tier == tc.tier && detail == tc.detail && tc.check(consequence) {
					found = true
					break
				}
			}
			if !found {
				for _, ma := range abilities {
					tier, consequence, detail := e.paymentPlanAbilityTier(0, id, ma)
					t.Logf("%s: tier=%d consequence=%+v detail=%q params=%v", ma.Line, tier, consequence, detail, ma.Params)
				}
				t.Fatalf("no ability classified tier %d detail %q with expected consequence", tc.tier, tc.detail)
			}
		})
	}
}

// TestPaymentPlanTiersTokenAbilities classifies the Treasure and Gold token
// scripts' own mana abilities as last resort with a sacrifice consequence
// (spec 3.2's Sac<1/CARDNAME> row: "with or without {T}"; the token spelling
// is Sac<1/CARDNAME/this token>).
func TestPaymentPlanTiersTokenAbilities(t *testing.T) {
	t.Parallel()
	for _, stem := range []string{"c_a_treasure_sac", "c_a_gold_sac"} {
		t.Run(stem, func(t *testing.T) {
			c, ok := testutil.CorpusRegistry(t).Token(stem)
			if !ok {
				t.Fatalf("corpus token %q missing", stem)
			}
			if len(c.Faces[0].ManaAbilities()) == 0 {
				t.Fatal("precondition: token script carries no mana ability")
			}
			e, _, _ := newFixtureDeck(t, 9830, "Name:Probe\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
			id := onBoardCard(t, e, 0, c)
			if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: token object is not on battlefield (zone %s)", o.Zone)
			}
			found := false
			for _, ma := range e.G.Obj(id).Face().ManaAbilities() {
				tier, consequence, detail := e.paymentPlanAbilityTier(0, id, ma)
				if tier == pay.TierLastResort && detail == "source:last_resort" && consequence.Sacrifice {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s mana ability is not last-resort sacrifice", stem)
			}
		})
	}
}

func TestPaymentPlanDoesNotUseLastResortSources(t *testing.T) {
	t.Parallel()
	for _, basics := range []int{0, 2} {
		e, _, spell := newFixtureDeck(t, uint64(9820+basics), "Name:Two Probe\nManaCost:2\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		tomb := onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
		for i := 0; i < basics; i++ {
			onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
		}
		out := e.PlanCastPayment(0, paymentCast(spell))
		if basics == 0 {
			// No normal plan exists, so the Tomb funds a last-resort plan that
			// discloses its damage (aph-last-resort-plans).
			if out.Plan == nil || len(out.Plan.Activations) != 1 || out.Plan.Activations[0].Source != tomb ||
				out.Plan.Activations[0].Consequence == nil || *out.Plan.Activations[0].Consequence != (decision.PaymentConsequence{Damage: 2}) {
				t.Fatalf("Tomb-only plan = %+v, want the Tomb disclosing damage:2", out)
			}
			continue
		}
		if out.Plan == nil {
			t.Fatalf("two Swamps should fund plan: %+v", out)
		}
		for _, activation := range out.Plan.Activations {
			if activation.Source == tomb {
				t.Fatalf("normal plan spent Ancient Tomb: %+v", out.Plan.Activations)
			}
		}
		if len(out.Plan.Activations) != 2 {
			t.Fatalf("used %d activations, want two Swamps", len(out.Plan.Activations))
		}
	}
}

func TestPaymentPlanNormalSourceRejectsRider(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9811, "Name:Probe\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	id := onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
	for _, ma := range e.G.Obj(id).Face().ManaAbilities() {
		if ma.API != "Mana" {
			t.Fatalf("precondition: expected Mana ability, got %q", ma.API)
		}
		tier, _, _ := e.paymentPlanAbilityTier(0, id, ma)
		if tier == pay.TierNormal {
			t.Fatalf("damage rider was classified normal: %v", ma.Params)
		}
	}
}

// ---------------------------------------------------------------------------
// Proofs ported from the 2026-09-26 audit branches (see the brief's Evidence
// section), converted to plain assertions of the tiered contract.

// tierPlanUses reports whether a plan outcome's witness activates id.
func tierPlanUses(got PaymentPlanOutcome, id state.ObjID) (decision.PaymentActivation, bool) {
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

// tierSubmitPlan publishes the priority decision's PaymentActions and submits
// the plan for spell, the way a client that enabled Auto-pay would.
func tierSubmitPlan(t *testing.T, e *Engine, spell state.ObjID) decision.PaymentPlan {
	t.Helper()
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
	for _, a := range d.PaymentActions {
		if a.Cast.Object != spell {
			continue
		}
		in := decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit planned cast: %v", err)
		}
		return a.Plans[0]
	}
	t.Fatalf("no payment action for spell %d: %#v", spell, d.PaymentActions)
	return decision.PaymentPlan{}
}

// tierFillGraveyard puts n filler cards in seat 0's graveyard (threshold for
// the Odyssey land cycle's gates).
func tierFillGraveyard(t *testing.T, e *Engine, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		gy := e.G.AddObject(card(t, "Name:Filler\nTypes:Instant\nOracle:x\n"), 0)
		gy.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), gy.ID))
	}
	e.staticEpoch, e.activeEpoch = -1, -1
}

// TestPaymentPlanTiersFPAncientTomb (FP-2). The {2} plan the fewest-sources
// rank used to build from Ancient Tomb moved life 20 -> 18 on execution with
// nothing announced in the witness. Now: tomb only is a last-resort plan
// whose witness DISCLOSES the 2 damage (aph-last-resort-plans), and with two
// Swamps the plan uses the Swamps and execution leaves life untouched.
func TestPaymentPlanTiersFPAncientTomb(t *testing.T) {
	t.Parallel()
	// Tomb only: the damage is announced in the witness.
	e, _, spell := newFixtureDeck(t, 9840, "Name:Two Probe\nManaCost:2\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	tomb := onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
	if tomb == 0 {
		t.Fatal("precondition: Ancient Tomb not on the battlefield")
	}
	out := e.PlanCastPayment(0, paymentCast(spell))
	if a, ok := tierPlanUses(out, tomb); !ok || a.Consequence == nil || *a.Consequence != (decision.PaymentConsequence{Damage: 2}) {
		t.Fatalf("tomb-only plan = %+v, want the Tomb with its 2 damage disclosed", out)
	}
	if out.Reason != "" {
		t.Fatalf("reason = %q, want a clean last-resort plan", out.Reason)
	}

	// Tomb + two Swamps: the Swamps fund the plan, life unchanged on execution.
	e2, _, spell2 := newFixtureDeck(t, 9841, "Name:Two Probe\nManaCost:2\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	tomb2 := onBoardCard(t, e2, 0, corpusTierFixture(t, "Ancient Tomb"))
	swamps := []state.ObjID{
		onBoard(t, e2, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"),
		onBoard(t, e2, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"),
	}
	out2 := e2.PlanCastPayment(0, paymentCast(spell2))
	if out2.Plan == nil {
		t.Fatalf("two Swamps should fund the {2} plan: %+v", out2)
	}
	if a, ok := tierPlanUses(out2, tomb2); ok {
		t.Fatalf("plan taps Ancient Tomb (2 unannounced damage): %+v", a)
	}
	if len(out2.Plan.Activations) != 2 {
		t.Fatalf("used %d activations, want the two Swamps only", len(out2.Plan.Activations))
	}
	for _, act := range out2.Plan.Activations {
		if act.Source != swamps[0] && act.Source != swamps[1] {
			t.Fatalf("unexpected activation source %d: %+v", act.Source, act)
		}
	}
	if before := e2.G.Players[0].Life; before != 20 {
		t.Fatalf("precondition: life = %d, want 20", before)
	}
	tierSubmitPlan(t, e2, spell2)
	if after := e2.G.Players[0].Life; after != 20 {
		t.Errorf("planned payment moved life %d -> %d", 20, after)
	}
	if e2.G.Obj(tomb2).Tapped {
		t.Errorf("Ancient Tomb was tapped by the plan")
	}
	for _, s := range swamps {
		if !e2.G.Obj(s).Tapped {
			t.Errorf("Swamp %d was not tapped by the executed plan", s)
		}
	}
}

// TestPaymentPlanTiersFPHarmfulRiderFamily (FP-2b). Every harmful rider shape
// the census found admitted silently is now either withheld from plans
// (deferred: life loss to each player, poison, control change, the targeted
// rider) or, when it is a fully determined last-resort consequence (damage to
// you, return to hand), planned only with that consequence disclosed in the
// witness step (aph-last-resort-plans).
func TestPaymentPlanTiersFPHarmfulRiderFamily(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Tarnished Citadel", "Cryptolith Fragment", "Elves of Deep Shadow", "Mox Poison",
		"Rainbow Vale", "Undiscovered Paradise", "Witch Engine", "Cabal Pit"} {
		t.Run(name, func(t *testing.T) {
			e, _, spell := newFixtureDeck(t, 9842, "Name:One Probe\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
			id := onBoardCard(t, e, 0, corpusTierFixture(t, name))
			e.G.Obj(id).SummonSick = false
			tierFillGraveyard(t, e, 7)
			// Precondition: the card carries at least one non-normal mana ability
			// (the rider/consequence this test is about), and collect its indices.
			harmful := map[uint32]pay.Tier{}
			disclosed := map[uint32]pay.Consequence{}
			for _, ma := range e.G.Obj(id).Face().ManaAbilities() {
				if tier, c, detail := e.paymentPlanAbilityTier(0, id, ma); tier != pay.TierNormal {
					for i, a := range e.G.Obj(id).Face().Abilities {
						if a == ma {
							harmful[uint32(i)] = tier
							disclosed[uint32(i)] = c
						}
					}
					t.Logf("%s: non-normal mana ability (tier %d, %s)", name, tier, detail)
				}
			}
			if len(harmful) == 0 {
				t.Fatalf("precondition: %s carries no non-normal mana ability", name)
			}
			out := e.PlanCastPayment(0, paymentCast(spell))
			if out.Plan == nil {
				return
			}
			for _, a := range out.Plan.Activations {
				tier, ok := harmful[a.Ability.Index]
				if !ok {
					continue
				}
				if tier != pay.TierLastResort {
					t.Errorf("plan admits %s deferred ability %d", name, a.Ability.Index)
					continue
				}
				if !paymentConsequenceEqual(disclosed[a.Ability.Index], a.Consequence) {
					t.Errorf("plan admits %s ability %d without disclosing its consequence %+v (step says %+v)", name, a.Ability.Index, disclosed[a.Ability.Index], a.Consequence)
				}
			}
		})
	}
}

// TestPaymentPlanTiersFPConditionalProduction (FP-4). River of Tears adds {U},
// or {B} instead once its controller has played a land this turn. The planner
// used to read only the head's Produced$ and promise {U}; the cast then
// reversed when execution produced {B}. Now the conditional production is
// never planned.
func TestPaymentPlanTiersFPConditionalProduction(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9843, "Name:Blue Probe\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	if river := onBoardCard(t, e, 0, corpusTierFixture(t, "River of Tears")); river == 0 {
		t.Fatal("precondition: River of Tears not on the battlefield")
	}
	e.G.Players[0].LandsPlayed = 1
	out := e.PlanCastPayment(0, paymentCast(spell))
	if out.Plan != nil {
		t.Errorf("planned conditional production after a land drop: %+v", out.Plan.Activations)
	}
	if out.Reason != "insufficient" || out.Detail != "source:conditional" {
		t.Errorf("reason/detail = %q/%q, want insufficient/source:conditional", out.Reason, out.Detail)
	}
}

// TestPaymentPlanTiersFPTriggersWhenSpent (FP-5). Pyromancer's Goggles' mana
// copies the red instant or sorcery it pays for; a plan must not spend it.
func TestPaymentPlanTiersFPTriggersWhenSpent(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9844, "Name:Red Probe\nManaCost:R\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	goggles := onBoardCard(t, e, 0, corpusTierFixture(t, "Pyromancer's Goggles"))
	out := e.PlanCastPayment(0, paymentCast(spell))
	if a, ok := tierPlanUses(out, goggles); ok {
		t.Errorf("plan spends TriggersWhenSpent mana: %+v", a)
	}
	if out.Plan != nil {
		t.Errorf("Goggles is the only producer, yet a plan exists: %+v", out.Plan.Activations)
	}
	if out.Reason != "insufficient" || out.Detail != "source:special_production" {
		t.Errorf("reason/detail = %q/%q, want insufficient/source:special_production", out.Reason, out.Detail)
	}
}

// TestPaymentPlanTiersMirrorTombFixture is the mirror audit's authored fixture
// (commit 0da6e8e59): a Tomb-shaped IR card whose ability carries SubAbility$
// DBHurt next to two Swamps. The witness used to select the Tomb (fewest
// sources); now it uses the Swamps.
func TestPaymentPlanTiersMirrorTombFixture(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9845, "Name:Two Generic Test\nManaCost:2\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	tomb := onBoard(t, e, 0, "Name:Tomb Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | SubAbility$ DBHurt | SpellDescription$ Tomb fixture.\nSVar:DBHurt:DB$ DealDamage | Defined$ You | NumDmg$ 2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("plan = %+v (reason %s detail %s), want the two-Swamp witness", got.Plan, got.Reason, got.Detail)
	}
	for _, act := range got.Plan.Activations {
		if act.Source == tomb {
			t.Errorf("witness %+v selects the self-damaging producer %d", got.Plan.Activations, tomb)
		}
	}
	if len(got.Plan.Activations) != 2 {
		t.Errorf("used %d activations, want the two Swamps", len(got.Plan.Activations))
	}
}

// TestPaymentPlanTiersUnannouncedLifeAtOneLife (fuzz proof). At 1 life with
// three Swamps and an Ancient Tomb, the fewest-sources rank used to pick Tomb
// + Swamps for {2}{B}: the fuzz game lost to its own plan. Now the Tomb is
// never in the plan, and the offered plan on the priority decision does not
// use it either.
func TestPaymentPlanTiersUnannouncedLifeAtOneLife(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9846, "Name:Black Probe\nManaCost:2 B\nTypes:Sorcery\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	toMain1(t, e)
	tomb := onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
	for i := 0; i < 3; i++ {
		onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1 - e.G.Players[0].Life})
	if life := e.G.Players[0].Life; life != 1 {
		t.Fatalf("precondition: life = %d, want 1", life)
	}
	out := e.PlanCastPayment(0, paymentCast(spell))
	if out.Plan == nil {
		t.Fatalf("three Swamps should fund the {2}{B} plan without the Tomb: %+v", out)
	}
	if a, ok := tierPlanUses(out, tomb); ok {
		t.Fatalf("plan taps Ancient Tomb (2 unannounced damage) at 1 life: %+v", a)
	}
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %#v, want priority", d)
	}
	d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
	for _, a := range d.PaymentActions {
		if a.Cast.Object != spell {
			continue
		}
		for _, plan := range a.Plans {
			for _, act := range plan.Activations {
				if act.Source == tomb {
					t.Errorf("offered plan taps Ancient Tomb at 1 life: %+v", plan.Activations)
				}
			}
		}
		return
	}
	t.Fatalf("no payment action for the spell: %#v", d.PaymentActions)
}

// TestPaymentPlanTiersGemstoneCavernsLuck (fuzz proof). A lucky Gemstone
// Caverns (which adds one mana of any colour, conditionally) was planned as
// {C} and the cast asked a colour at execution. The conditional production is
// never planned.
func TestPaymentPlanTiersGemstoneCavernsLuck(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9847, "Name:Colorless Probe\nManaCost:C\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	toMain1(t, e)
	caverns := onBoardCard(t, e, 0, corpusTierFixture(t, "Gemstone Caverns"))
	if caverns == 0 {
		t.Fatal("precondition: Gemstone Caverns not on the battlefield")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: caverns, Counter: "LUCK", Amount: 1})
	if got := e.PlanCastPayment(0, paymentCast(spell)); got.Plan != nil {
		t.Fatalf("{C} spell planned from a lucky Gemstone Caverns (which adds a coloured mana): %+v", got.Plan.Activations)
	}
}

// TestPaymentPlanTiersExecutedPlanSpendsOnlyAnnouncedSources (gaps proof,
// TestPaymentPlanAuditBugSideEffectProducerSpendsUnannouncedLife): a plan on a
// board that also holds an Ancient Tomb is built from the Mountains and its
// execution leaves life and the Tomb untouched.
func TestPaymentPlanTiersExecutedPlanSpendsOnlyAnnouncedSources(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9848, "Name:Two Generic\nManaCost:2\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	m1 := onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	m2 := onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	tomb := onBoardCard(t, e, 0, corpusTierFixture(t, "Ancient Tomb"))
	out := e.PlanCastPayment(0, paymentCast(spell))
	if out.Plan == nil {
		t.Fatalf("two Mountains should fund the {2} plan: %+v", out)
	}
	if a, ok := tierPlanUses(out, tomb); ok {
		t.Fatalf("plan %#v uses Ancient Tomb", a)
	}
	before := e.G.Players[0].Life
	if before != 20 {
		t.Fatalf("precondition: life = %d, want 20", before)
	}
	tierSubmitPlan(t, e, spell)
	if after := e.G.Players[0].Life; after != before {
		t.Errorf("planned payment changed life %d -> %d", before, after)
	}
	if e.G.Obj(tomb).Tapped {
		t.Errorf("Ancient Tomb was tapped by the executed plan")
	}
	for _, m := range []state.ObjID{m1, m2} {
		if !e.G.Obj(m).Tapped {
			t.Errorf("Mountain %d was not tapped by the executed plan", m)
		}
	}
}

func corpusTierFixture(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q missing", name)
	}
	return c
}
