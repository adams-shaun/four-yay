package pay

// The payment planner's vocabulary and rank (spec 5, amended 2026-09-26):
// the alternatives a mana source offers, the consequence an activation
// carries, the lexicographic rank tuple that orders plans and the witness a
// chosen plan discloses. Everything here is a pure function of its
// arguments; nothing reads or writes game state.

import (
	"cmp"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// Cost is the parsed cost vocabulary the planner prices.
type Cost = cost.Cost

type Tier uint8

const (
	TierDeferred Tier = iota
	TierLastResort
	TierNormal
)

type Consequence struct {
	Sacrifice    bool
	Life         uint32
	Damage       uint32
	NoUntap      bool
	ReturnToHand bool
}

func ManaAmount(m state.Mana) decision.ManaAmount {
	var out decision.ManaAmount
	for i, n := range m {
		if n > 0 {
			out[i] = uint32(n)
		}
	}
	return out
}

func WireCost(c Cost) decision.PaymentCost {
	return decision.PaymentCost{Generic: uint32(c.Generic), Mana: ManaAmount(c.Colored)}
}

type Alt struct {
	Activation decision.PaymentActivation
	Mana       state.Mana
	Creature   bool
	// Flex and colours describe the SOURCE, not this alternative: over every
	// eligible alternative of the source, Flex is the number of distinct mana
	// types (W/U/B/R/G/C) it can produce and colours is the WUBRG bitmask
	// (bit i = state.MW+i) of the colours it can produce. They feed rank keys
	// 5 (flexibility consumed) and 7 (remainder diversity).
	//
	// Both are computed over the source's NORMAL alternatives (the phase-1
	// view, and the "untapped normal remainder" keys 6 and 7 read); a source
	// with only last-resort alternatives has colours 0. flexAll is the same
	// count over every eligible alternative, normal and last resort, which
	// is what key 5 reads in phase 2 (paymentPlanLastResortChoices).
	Flex    int
	FlexAll int
	Colours uint8
	// Ma is the exact ability this alternative activates. It is not part of
	// the witness: every intrinsic ability shares one PaymentAbility
	// identity ({intrinsic, basic_land}), so on a source with several (a
	// dual land's {U} and {R}) the identity alone names no single ability.
	// A witness step's Ability AND Produces together select exactly one
	// alternative (paymentPlanStepAlternative), and execution activates
	// that alternative's own ability.
	Ma *cards.SA
	// ExecProduced names the exact ability to activate to realise this
	// alternative: "" for fixed production (ma itself), and the selected
	// colour of a choice-shaped production (Any/Combo/Chosen/ColorIdentity),
	// recorded in the witness's Produces, whose withProduced copy of ma the
	// executor activates (alternativeExec). The executor hands ma to the
	// ordinary mana path as the original (for activation limits and replay
	// identity), so no colour prompt is ever posed at execution. The copy is
	// built only for the step that runs, never for the planner's census.
	ExecProduced string
	Tier         Tier
	Consequence  Consequence
}

// ConsequencePain is the life a step costs its caster: life paid
// plus damage dealt to its controller (the lethal guard's measure).
func ConsequencePain(c Consequence) int64 {
	return int64(c.Life) + int64(c.Damage)
}

// wire is the consequence as the witness discloses it: nil for a normal step.
func (c Consequence) Wire() *decision.PaymentConsequence {
	if c == (Consequence{}) {
		return nil
	}
	return &decision.PaymentConsequence{Sacrifice: c.Sacrifice, Life: c.Life, Damage: c.Damage, NoUntap: c.NoUntap, ReturnToHand: c.ReturnToHand}
}

// Irreversible-cost weights (spec 5 key 1, as amended 2026-09-26). They are
// Arena-calibrated: two self-sacrificed Treasures (20) are cheaper than one
// Mana Vault that does not untap (25), which is cheaper than three Treasures
// (30); a point of damage or life (3) is cheaper than any sacrifice. The key
// is summed per activated ability, so a source's painless ability always
// beats its painful one for the same need.
const (
	costSacrificeSelf         = 10 // sacrifice-self, non-creature source
	costSacrificeSelfCreature = 20 // sacrifice-self, creature source
	costNoUntap               = 25 // the source does not untap next untap step
	costPerLifeOrDamage       = 3  // per point of life paid or damage taken
	costReturnToHand          = 8  // the source returns to its owner's hand
)

// ConsequenceCost is one activation's irreversible cost under the
// weights above.
func ConsequenceCost(c Consequence, creature bool) int64 {
	var n int64
	if c.Sacrifice {
		if creature {
			n += costSacrificeSelfCreature
		} else {
			n += costSacrificeSelf
		}
	}
	if c.NoUntap {
		n += costNoUntap
	}
	n += costPerLifeOrDamage * (int64(c.Life) + int64(c.Damage))
	if c.ReturnToHand {
		n += costReturnToHand
	}
	return n
}

// RankContext is the per-query input the rank needs beyond one
// plan: for each colour (WUBRG), how many untapped sources with an eligible
// normal alternative can produce it, and the acting player's own hand's
// colour demand. Both are computed once from the query, so a plan's
// remainder is the census minus the plan's own chosen sources.
type RankContext struct {
	ColourSources [5]int
	// Key 6 inputs (spec 5 amended): HandDemand[c] is the largest number of
	// colour c's pips on any single nonland card in the acting player's own
	// hand other than the cast card; demandOrder lists the colour indices by
	// descending demand (WUBRG ties); reserveBase is max demand + 1, the
	// digit base of the packed coverage. A zero reserveBase (a directly
	// constructed context, or no demanded colour) leaves key 6 at 0 for
	// every plan, the tie value the placeholder pinned.
	HandDemand  [5]int
	DemandOrder [5]int
	ReserveBase int
}

func NewRankContext(choices [][]Alt, demand [5]int) RankContext {
	var ctx RankContext
	for _, alts := range choices {
		if len(alts) == 0 {
			continue
		}
		for c := range ctx.ColourSources {
			if alts[0].Colours&(1<<c) != 0 {
				ctx.ColourSources[c]++
			}
		}
	}
	ctx.HandDemand = demand
	max := 0
	for _, n := range demand {
		if n > max {
			max = n
		}
	}
	ctx.ReserveBase = max + 1
	ctx.DemandOrder = [5]int{0, 1, 2, 3, 4}
	// Stable over the WUBRG seed order, so equal demands keep WUBRG order.
	slices.SortStableFunc(ctx.DemandOrder[:], func(a, b int) int {
		return cmp.Compare(demand[b], demand[a])
	})
	return ctx
}

// RankStep is one witness step as the final tie-break compares it.
type RankStep struct {
	Act         decision.PaymentActivation
	Consequence Consequence
}

// Rank is spec 5's lexicographic rank tuple (amended 2026-09-26);
// less orders it, lower first, key by key in field order.
type Rank struct {
	Cost        int64 // 1. irreversible cost (0 for every normal-tier plan)
	Creatures   int   // 2. creature sources activated
	Sources     int   // 3. newly activated sources
	Surplus     int32 // 4. surplus mana left in the pool after paying
	Flex        int   // 5. distinct mana types the chosen sources could have made
	HandReserve int   // 6. packed hand-reserve coverage, MORE first (spec 5 amended; was a zero placeholder)
	Remainder   int   // 7. distinct colours the unused sources still make (MORE first)
	Steps       []RankStep
}

func (r Rank) Less(o Rank) bool {
	if r.Cost != o.Cost {
		return r.Cost < o.Cost
	}
	if r.Creatures != o.Creatures {
		return r.Creatures < o.Creatures
	}
	if r.Sources != o.Sources {
		return r.Sources < o.Sources
	}
	if r.Surplus != o.Surplus {
		return r.Surplus < o.Surplus
	}
	if r.Flex != o.Flex {
		return r.Flex < o.Flex
	}
	// Key 6 (hand reserve) is MORE coverage first, like key 7's remainder.
	if r.HandReserve != o.HandReserve {
		return r.HandReserve > o.HandReserve
	}
	if r.Remainder != o.Remainder {
		return r.Remainder > o.Remainder
	}
	return CompareSteps(r.Steps, o.Steps) < 0
}

// CompareSteps is key 8: the typed witness, compared numerically
// step by step (source object ID, ability kind, face, index, intrinsic name,
// produced vector, consequence), then by length. It replaces a fmt.Sprint
// string compare that sorted object 10 before object 9.
func CompareSteps(a, b []RankStep) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		if c := cmp.Compare(x.Act.Source, y.Act.Source); c != 0 {
			return c
		}
		if c := cmp.Compare(x.Act.Ability.Kind, y.Act.Ability.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(x.Act.Ability.Face, y.Act.Ability.Face); c != 0 {
			return c
		}
		if c := cmp.Compare(x.Act.Ability.Index, y.Act.Ability.Index); c != 0 {
			return c
		}
		if c := cmp.Compare(x.Act.Ability.Intrinsic, y.Act.Ability.Intrinsic); c != 0 {
			return c
		}
		for k := range x.Act.Produces {
			if c := cmp.Compare(x.Act.Produces[k], y.Act.Produces[k]); c != 0 {
				return c
			}
		}
		if c := compareConsequence(x.Consequence, y.Consequence); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func compareConsequence(a, b Consequence) int {
	flag := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if c := cmp.Compare(flag(a.Sacrifice), flag(b.Sacrifice)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Life, b.Life); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Damage, b.Damage); c != 0 {
		return c
	}
	if c := cmp.Compare(flag(a.NoUntap), flag(b.NoUntap)); c != 0 {
		return c
	}
	return cmp.Compare(flag(a.ReturnToHand), flag(b.ReturnToHand))
}

// RankPlan computes plan p's rank. as is the plan's chosen
// alternatives (one per source, in witness order), after the pool left once
// the cost is paid, and ctx the query's source census.
func RankPlan(ctx RankContext, p decision.PaymentPlan, as []Alt, after state.Mana) Rank {
	r := Rank{Sources: len(as), Surplus: after.Total(), Steps: make([]RankStep, len(p.Activations))}
	remaining := ctx.ColourSources
	for _, a := range as {
		r.Cost += ConsequenceCost(a.Consequence, a.Creature)
		if a.Creature {
			r.Creatures++
		}
		r.Flex += a.Flex
		for c := range remaining {
			if a.Colours&(1<<c) != 0 {
				remaining[c]--
			}
		}
	}
	for _, n := range remaining {
		if n > 0 {
			r.Remainder++
		}
	}
	// Key 6: the packed hand-reserve coverage. Each digit is the plan's
	// coverage min(untapped normal remainder, demand) for one colour, most
	// significant digit first in the demand order, so the packed integer
	// compares lexicographically in that order (less reads it larger first).
	// Zero demand packs to 0 for every plan.
	if ctx.ReserveBase > 0 {
		cov := 0
		for _, c := range ctx.DemandOrder {
			n := remaining[c]
			if d := ctx.HandDemand[c]; n > d {
				n = d
			}
			cov = cov*ctx.ReserveBase + n
		}
		r.HandReserve = cov
	}
	for i := range p.Activations {
		r.Steps[i].Act = p.Activations[i]
		if i < len(as) {
			r.Steps[i].Consequence = as[i].Consequence
		}
	}
	return r
}
func Witness(c Cost, initial, produced state.Mana, as []Alt, after state.Mana) decision.PaymentPlan {
	acts := make([]decision.PaymentActivation, len(as))
	for i := range as {
		acts[i] = as[i].Activation
		acts[i].Consequence = as[i].Consequence.Wire()
	}
	spend := decision.ManaAmount{}
	for i := range initial {
		totalSpent := initial[i] + produced[i] - after[i]
		if totalSpent > initial[i] {
			totalSpent = initial[i]
		}
		if totalSpent > 0 {
			spend[i] = uint32(totalSpent)
		}
	}
	return decision.PaymentPlan{Version: decision.PaymentPlanV1, Cost: WireCost(c), Activations: acts, PoolSpend: spend, PoolAfter: ManaAmount(after)}
}
