package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Boards for aph-rank-order (spec 5, amended 2026-09-26): complete plans rank
// by irreversible cost, then creature sources, then newly activated sources,
// surplus, flexibility consumed (distinct mana types), the hand-reserve
// placeholder, remainder diversity, and finally the typed witness compared
// numerically.

const (
	rankForest = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
	rankIsland = "Name:Island\nTypes:Basic Land Island\nOracle:x\n"
)

// rankStep is one expected witness step: its source and produced vector.
type rankStep struct {
	source   state.ObjID
	produces decision.ManaAmount
}

func rankMana(sym byte, n uint32) decision.ManaAmount {
	var m decision.ManaAmount
	m[state.ManaIndex(sym)] = n
	return m
}

// rankWantSteps asserts the plan's exact activation list (source and
// production, in witness order).
func rankWantSteps(t *testing.T, got PaymentPlanOutcome, want ...rankStep) {
	t.Helper()
	if got.Plan == nil {
		t.Fatalf("no plan: reason=%q detail=%q nodes=%d", got.Reason, got.Detail, got.Nodes)
	}
	if got.Reason != "" {
		t.Fatalf("plan outcome reason = %q, want a clean plan", got.Reason)
	}
	acts := got.Plan.Activations
	ok := len(acts) == len(want)
	for i := 0; ok && i < len(acts); i++ {
		ok = acts[i].Source == want[i].source && acts[i].Produces == want[i].produces
	}
	if !ok {
		t.Fatalf("witness activations = %+v, want %+v", acts, want)
	}
}

// rankSubmit publishes the offer for spell and submits its plan, as an
// auto-pay client would.
func rankSubmit(t *testing.T, e *Engine, spell state.ObjID) {
	t.Helper()
	d := ppAsk(t, e)
	ppSubmitPlan(t, e, d, ppAction(t, d, spell))
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("planned spell zone = %s, want stack", e.G.Obj(spell).Zone)
	}
}

// Done-means 1 (probe P8): creatures before sources. Forge (+13 per
// attack/block ability), XMage and Arena ("QQ taps all non-creatures") keep a
// creature untapped when lands can pay; V1 used to tap Forest + the creature
// because it ranked fewest sources first.
func TestPaymentPlanRankCreatureAfterLands(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9901, "Name:Green Three\nManaCost:2 G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	construct := onBoardReady(t, e, 0, "Name:Twin Construct\nTypes:Artifact Creature Construct\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2\nOracle:x\n")
	f1 := onBoard(t, e, 0, rankForest)
	f2 := onBoard(t, e, 0, rankForest)
	f3 := onBoard(t, e, 0, rankForest)
	g := rankMana('G', 1)
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)), rankStep{f1, g}, rankStep{f2, g}, rankStep{f3, g})
	rankSubmit(t, e, spell)
	if e.G.Obj(construct).Tapped {
		t.Fatal("the creature source was tapped although three Forests paid")
	}
	for _, f := range []state.ObjID{f1, f2, f3} {
		if !e.G.Obj(f).Tapped {
			t.Fatalf("Forest %d untapped after the planned payment", f)
		}
	}
}

// Done-means 2 (probe P6): a land printing "T: Any, 3 damage to you" BEFORE
// "T: add {C}". The painful ability is last resort, so the land's painless
// {C} ability pays the generic.
func TestPaymentPlanRankPainlessAbilityOfPainfulLand(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9902, "Name:Blue Two\nManaCost:1 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, rankIsland)
	land := onBoard(t, e, 0, "Name:Pain Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | SubAbility$ DBPain | SpellDescription$ Any, pain.\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Colourless.\nSVar:DBPain:DB$ DealDamage | Defined$ You | NumDmg$ 3\nOracle:x\n")
	got := e.PlanCastPayment(0, paymentCast(spell))
	rankWantSteps(t, got, rankStep{island, rankMana('U', 1)}, rankStep{land, rankMana('C', 1)})
	if ab := got.Plan.Activations[1].Ability; ab.Kind != decision.PaymentAbilityPrinted || ab.Index != 1 {
		t.Fatalf("land step ability = %+v, want the printed {C} ability (index 1)", ab)
	}
	life := e.G.Players[0].Life
	rankSubmit(t, e, spell)
	if e.G.Players[0].Life != life {
		t.Fatalf("life %d -> %d: the painful ability was activated", life, e.G.Players[0].Life)
	}
}

// Done-means 3 (probe P1): {2}{U} with three Islands and a "T: {C}{C}, 2
// damage to you" land pays with the three Islands.
func TestPaymentPlanRankDamageLandNotUsedWhenIslandsPay(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9903, "Name:Blue Three\nManaCost:2 U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	tomb := onBoard(t, e, 0, "Name:Tomb Test\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | SubAbility$ DBHurt | SpellDescription$ x\nSVar:DBHurt:DB$ DealDamage | Defined$ You | NumDmg$ 2\nOracle:x\n")
	i1 := onBoard(t, e, 0, rankIsland)
	i2 := onBoard(t, e, 0, rankIsland)
	i3 := onBoard(t, e, 0, rankIsland)
	u := rankMana('U', 1)
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)), rankStep{i1, u}, rankStep{i2, u}, rankStep{i3, u})
	rankSubmit(t, e, spell)
	if e.G.Obj(tomb).Tapped {
		t.Fatal("the damage land was tapped")
	}
}

// Done-means 4: remainder diversity ("prefer to leave up a diverse spread of
// mana", Arena 1.11.00). {1}{G} over Island, Forest x3 taps two Forests and
// leaves Island + Forest (two colours) rather than two Forests (one). The
// Island is placed first so the old string tie-break would have picked it.
func TestPaymentPlanRankRemainderDiversity(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9904, "Name:Green Two\nManaCost:1 G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	island := onBoard(t, e, 0, rankIsland)
	f1 := onBoard(t, e, 0, rankForest)
	f2 := onBoard(t, e, 0, rankForest)
	onBoard(t, e, 0, rankForest)
	g := rankMana('G', 1)
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)), rankStep{f1, g}, rankStep{f2, g})
	rankSubmit(t, e, spell)
	if e.G.Obj(island).Tapped {
		t.Fatal("the Island was tapped; the remainder lost its only blue source")
	}
}

// Done-means 5: PP-03 still prefers the Mountain for the generic, and
// flexibility is the number of DISTINCT mana types a source's eligible
// alternatives produce, not the number of alternatives.
func TestPaymentPlanRankFlexCountsDistinctTypes(t *testing.T) {
	t.Parallel()
	t.Run("PP-03 prefers the Mountain", func(t *testing.T) {
		e, _, spell := newFixtureDeck(t, 9905, "Name:Grixis Plan\nManaCost:1 U B\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		island := onBoard(t, e, 0, ppIsland)
		swamp := onBoard(t, e, 0, ppSwamp)
		badlands := onBoard(t, e, 0, "Name:Badlands Test\nTypes:Land Swamp Mountain\nOracle:x\n")
		mountain := onBoard(t, e, 0, ppMountain)
		rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)),
			rankStep{island, rankMana('U', 1)}, rankStep{swamp, rankMana('B', 1)}, rankStep{mountain, rankMana('R', 1)})
		rankSubmit(t, e, spell)
		if e.G.Obj(badlands).Tapped {
			t.Fatal("the Badlands was tapped")
		}
	})
	t.Run("distinct types per source", func(t *testing.T) {
		e, _, _ := newFixtureDeck(t, 9915, "Name:Probe\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
		island := onBoard(t, e, 0, ppIsland)
		badlands := onBoard(t, e, 0, "Name:Badlands Test\nTypes:Land Swamp Mountain\nOracle:x\n")
		lotus := onBoardReady(t, e, 0, "Name:Lotus Test\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ Any | Amount$ 3\nOracle:x\n")
		boros := onBoard(t, e, 0, "Name:Plateau Test\nTypes:Land Plains Mountain\nOracle:x\n")
		// Two alternatives, one mana type: the old key (number of
		// alternatives) said 2.
		twinBlue := onBoard(t, e, 0, "Name:Twin Blue\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ U\nA:AB$ Mana | Cost$ T | Produced$ U | Amount$ 2\nOracle:x\n")
		wantFlex := map[state.ObjID]int{island: 1, badlands: 2, lotus: 5, boros: 2, twinBlue: 1}
		wantAlts := map[state.ObjID]int{island: 1, badlands: 2, lotus: 5, boros: 2, twinBlue: 2}
		seen := 0
		for _, u := range e.paymentPlanManaUnits(0) {
			want, ok := wantFlex[u.ID]
			if !ok {
				continue
			}
			seen++
			alts := e.paymentPlanUnitAlternatives(u)
			if len(alts) != wantAlts[u.ID] {
				t.Fatalf("source %d has %d eligible alternatives, want %d", u.ID, len(alts), wantAlts[u.ID])
			}
			for _, a := range alts {
				if a.Flex != want {
					t.Fatalf("source %d (%d alternatives) flex = %d, want %d", u.ID, len(alts), a.Flex, want)
				}
			}
		}
		if seen != len(wantFlex) {
			t.Fatalf("found %d of %d sources among the mana units", seen, len(wantFlex))
		}
	})
}

// Done-means 6: the final tie-break compares the typed witness numerically.
// The former fmt.Sprint key sorted object 100 before object 99 (and 10
// before 9); two identical Islands straddling a decimal boundary must use the
// lower object ID. (The pure rank test below pins IDs 9 and 10 exactly.)
func TestPaymentPlanRankNumericTieBreak(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9906, "Name:Blue One\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	boundary := state.ObjID(9)
	for boundary < e.G.NextID {
		boundary = boundary*10 + 9
	}
	for e.G.NextID < boundary {
		onBoard(t, e, 1, "Name:Filler Rock\nTypes:Artifact\nOracle:x\n")
	}
	low := onBoard(t, e, 0, rankIsland)
	high := onBoard(t, e, 0, rankIsland)
	if low != boundary || high != boundary+1 {
		t.Fatalf("precondition: Islands are %d and %d, want %d and %d", low, high, boundary, boundary+1)
	}
	rankWantSteps(t, e.PlanCastPayment(0, paymentCast(spell)), rankStep{low, rankMana('U', 1)})
}

// Done-means 8: the offer is a pure function of the position. Re-running the
// query and cloning the engine yield identical plan (and action) IDs, on a
// board where several keys tie.
func TestPaymentPlanRankDeterministicAcrossRerunAndClone(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9908, "Name:Green Two\nManaCost:1 G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	onBoard(t, e, 0, rankIsland)
	for i := 0; i < 4; i++ {
		onBoard(t, e, 0, rankForest)
	}
	onBoardReady(t, e, 0, "Name:Elf Dork\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n")
	d := ppAsk(t, e)
	first := ppAction(t, d, spell)
	again := e.PaymentActionsForPriority(0, d.Seq)
	cloned := e.Clone().PaymentActionsForPriority(0, d.Seq)
	if !reflect.DeepEqual(d.PaymentActions, again) || !reflect.DeepEqual(d.PaymentActions, cloned) {
		t.Fatalf("offers differ:\nfirst  %#v\nrerun  %#v\nclone  %#v", d.PaymentActions, again, cloned)
	}
	if first.Plans[0].ID == "" {
		t.Fatal("offered plan has no ID")
	}
}

// rankInputStep is one step of a hand-made rank input; rankOf ranks a plan
// of such steps (each a printed ability producing one colourless, flex 1)
// with the given consequences and pool left after payment.
type rankInputStep struct {
	source      state.ObjID
	creature    bool
	consequence pay.Consequence
}

func rankOf(ctx pay.RankContext, after state.Mana, steps ...rankInputStep) pay.Rank {
	as := make([]pay.Alt, len(steps))
	acts := make([]decision.PaymentActivation, len(steps))
	for i, s := range steps {
		acts[i] = decision.PaymentActivation{Source: s.source, Ability: decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted}, Produces: rankMana('C', 1)}
		as[i] = pay.Alt{Activation: acts[i], Creature: s.creature, Consequence: s.consequence, Flex: 1}
	}
	return pay.RankPlan(ctx, decision.PaymentPlan{Version: decision.PaymentPlanV1, Activations: acts}, as, after)
}

func rankRepeat(n int, s rankInputStep) []rankInputStep {
	out := make([]rankInputStep, n)
	for i := range out {
		out[i] = s
		out[i].source = s.source + state.ObjID(i)
	}
	return out
}

// Done-means 7: the irreversible-cost key on constructed rank inputs. Arena
// (2026.56.10) "favor[s] sacrificing two treasures before tapping Manavault
// or Grim Monolith. It will tap those before sacrificing three treasures";
// damage is cheaper than a sacrifice (Arena 2022.20.0 prefers tapping a
// creature to a pain land's damage, and key 2 below key 1 encodes that too).
func TestPaymentPlanRankIrreversibleCostKey(t *testing.T) {
	t.Parallel()
	ctx := pay.RankContext{}
	sac := rankInputStep{source: 1, consequence: pay.Consequence{Sacrifice: true}}
	vault := rankInputStep{source: 1, consequence: pay.Consequence{NoUntap: true}}
	twoSac := rankOf(ctx, state.Mana{}, rankRepeat(2, sac)...)
	oneVault := rankOf(ctx, state.Mana{}, vault)
	threeSac := rankOf(ctx, state.Mana{}, rankRepeat(3, sac)...)
	damage2 := rankOf(ctx, state.Mana{}, rankInputStep{source: 1, consequence: pay.Consequence{Damage: 2}})
	oneSac := rankOf(ctx, state.Mana{}, sac)
	for _, c := range []struct {
		name string
		r    pay.Rank
		want int64
	}{
		{"two sacrifices", twoSac, 20}, {"one no-untap", oneVault, 25}, {"three sacrifices", threeSac, 30},
		{"damage 2", damage2, 6}, {"one sacrifice", oneSac, 10},
		{"life 1 + return to hand", rankOf(ctx, state.Mana{}, rankInputStep{source: 1, consequence: pay.Consequence{Life: 1, ReturnToHand: true}}), 11},
		{"sacrifice-self creature", rankOf(ctx, state.Mana{}, rankInputStep{source: 1, creature: true, consequence: pay.Consequence{Sacrifice: true}}), 20},
	} {
		if c.r.Cost != c.want {
			t.Errorf("%s: cost key = %d, want %d", c.name, c.r.Cost, c.want)
		}
	}
	for _, c := range []struct {
		name        string
		lower, high pay.Rank
	}{
		{"2 sacrifices < 1 no-untap", twoSac, oneVault},
		{"1 no-untap < 3 sacrifices", oneVault, threeSac},
		{"damage 2 < 1 sacrifice", damage2, oneSac},
	} {
		if !c.lower.Less(c.high) || c.high.Less(c.lower) {
			t.Errorf("%s: order not strict (%+v vs %+v)", c.name, c.lower, c.high)
		}
	}
	// Any cost > 0 loses to cost 0 regardless of every later key: the
	// costly plan is one non-creature source with no surplus; the free plan
	// taps three creatures and floats two mana.
	costly := rankOf(pay.RankContext{ColourSources: [5]int{1, 1, 1, 1, 1}}, state.Mana{}, rankInputStep{source: 1, consequence: pay.Consequence{Damage: 1}})
	free := rankOf(ctx, state.Mana{0, 0, 0, 0, 0, 2}, rankRepeat(3, rankInputStep{source: 2, creature: true})...)
	if !free.Less(costly) || costly.Less(free) {
		t.Fatalf("cost-0 plan %+v must beat cost-%d plan %+v", free, costly.Cost, costly)
	}
}

// Key order below the cost key, on constructed inputs: creatures, then
// sources, then surplus.
func TestPaymentPlanRankKeyOrder(t *testing.T) {
	t.Parallel()
	ctx := pay.RankContext{}
	land := rankInputStep{source: 1}
	creature := rankInputStep{source: 1, creature: true}
	oneCreature := rankOf(ctx, state.Mana{}, creature)
	threeLands := rankOf(ctx, state.Mana{}, rankRepeat(3, land)...)
	if !threeLands.Less(oneCreature) {
		t.Fatal("three lands must beat one creature (creatures before sources)")
	}
	twoLandsSurplus := rankOf(ctx, state.Mana{0, 0, 0, 0, 0, 1}, rankRepeat(2, land)...)
	if !twoLandsSurplus.Less(threeLands) {
		t.Fatal("two sources with surplus must beat three sources without (sources before surplus)")
	}
	if r := rankOf(ctx, state.Mana{}); r.HandReserve != 0 {
		t.Fatalf("zero-demand hand reserve (a zero context has reserveBase 0) = %d, want 0", r.HandReserve)
	}
}

// Remainder diversity on constructed inputs: the plan's own sources are
// removed from the per-query colour census.
func TestPaymentPlanRankRemainderKey(t *testing.T) {
	t.Parallel()
	// Census: two green sources, one blue.
	ctx := pay.RankContext{}
	ctx.ColourSources[state.MG] = 2
	ctx.ColourSources[state.MU] = 1
	step := func(id state.ObjID, col int) pay.Alt {
		return pay.Alt{Activation: decision.PaymentActivation{Source: id}, Flex: 1, Colours: 1 << col}
	}
	rank := func(as ...pay.Alt) pay.Rank {
		acts := make([]decision.PaymentActivation, len(as))
		for i := range as {
			acts[i] = as[i].Activation
		}
		return pay.RankPlan(ctx, decision.PaymentPlan{Activations: acts}, as, state.Mana{})
	}
	greenGreen := rank(step(1, state.MG), step(2, state.MG))
	greenBlue := rank(step(1, state.MG), step(3, state.MU))
	if greenGreen.Remainder != 1 || greenBlue.Remainder != 1 {
		t.Fatalf("remainders = %d, %d; want 1 (blue left) and 1 (green left)", greenGreen.Remainder, greenBlue.Remainder)
	}
	oneGreen := rank(step(1, state.MG))
	if oneGreen.Remainder != 2 {
		t.Fatalf("remainder after one green = %d, want 2", oneGreen.Remainder)
	}
	if !oneGreen.Less(rank(step(3, state.MU))) {
		t.Fatal("leaving green+blue must beat leaving only green")
	}
}

// Done-means 6 on exact object IDs 9 and 10: the typed compare is numeric
// (the old fmt.Sprint key put "{10 ..." before "{9 ...").
func TestPaymentPlanRankTypedWitnessCompare(t *testing.T) {
	t.Parallel()
	ctx := pay.RankContext{}
	nine := rankOf(ctx, state.Mana{}, rankInputStep{source: 9})
	ten := rankOf(ctx, state.Mana{}, rankInputStep{source: 10})
	if !nine.Less(ten) || ten.Less(nine) {
		t.Fatal("object 9 must rank before object 10")
	}
	if nine.Less(nine) {
		t.Fatal("less is not irreflexive")
	}
	// Same source: ability kind, face, index, intrinsic, production, then
	// length.
	base := decision.PaymentActivation{Source: 9, Ability: decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted, Index: 1}, Produces: rankMana('U', 1)}
	vary := []decision.PaymentActivation{
		{Source: 9, Ability: decision.PaymentAbility{Kind: decision.PaymentAbilityIntrinsic, Intrinsic: "basic_land"}, Produces: rankMana('U', 1)},
		{Source: 9, Ability: decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted, Index: 0}, Produces: rankMana('U', 1)},
		{Source: 9, Ability: decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted, Index: 1}, Produces: rankMana('B', 1)},
	}
	for i, v := range vary {
		lo := []pay.RankStep{{Act: v}}
		hi := []pay.RankStep{{Act: base}}
		if pay.CompareSteps(lo, hi) >= 0 || pay.CompareSteps(hi, lo) <= 0 {
			t.Errorf("variant %d %+v does not sort before %+v", i, v, base)
		}
	}
	short := []pay.RankStep{{Act: base}}
	long := []pay.RankStep{{Act: base}, {Act: base}}
	if pay.CompareSteps(short, long) >= 0 {
		t.Error("a strict prefix must sort first")
	}
}
