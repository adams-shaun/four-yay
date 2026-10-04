package pay

import (
	"maps"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// classify.go holds the auto-pay planner's pure classification reads
// (lasagna spec W5 E7): which cost and pool shapes a V1 witness admits, the
// last-resort consequence shapes, and the plan/step comparisons the cast flow
// and the offer builder share. None reads the engine.

func ActionFor(d *decision.Decision, id string) (decision.PaymentAction, bool) {
	if d == nil {
		return decision.PaymentAction{}, false
	}
	for _, action := range d.PaymentActions {
		if action.ID == id {
			return decision.ClonePaymentAction(action), true
		}
	}
	return decision.PaymentAction{}, false
}

func FaceReadsManaSpent(f *cards.Face) bool {
	for _, needle := range []string{"ConditionManaSpent$", "Count$Adamant", "Count$EachSpentToCast", "Count$TotalManaSpent", "ManaSpentBy"} {
		if f.Mentions(needle) {
			return true
		}
	}
	return false
}

// PlanNonManaAdmissible reports whether c is a cost a mana-only V1
// witness can describe: its mana half must classify cleanly and its only
// permitted non-mana part is a fixed-count mandatory Sac<N/Spec>. A
// variable-count Sac<X/Spec> (cost.CostPart.Announced, the announced count binds
// the cast's X) and Sac<All/...> (which never reaches Cost.Sac -- it parses as
// Unknown) are not admissible, and every other non-mana field declines. The
// mana half is checked by removing the Sac parts and running the same
// classifier the gate has always used, so X/hybrid/Phyrexian/snow and the
// other mana-class shapes still decline here. Returns the decline detail, or
// "" when the cost is admissible.
func PlanNonManaAdmissible(c Cost) string {
	for _, part := range c.Sac {
		if part.Announced || part.N <= 0 {
			return "cost:sacrifice"
		}
	}
	rest := c
	rest.Sac = nil
	return PlanCostDetail(rest)
}

func PlanCostDetail(c Cost) string {
	switch {
	case c.X != 0 || c.XMin != 0:
		return "cost:x"
	case c.Snow != 0:
		return "cost:snow"
	case len(c.Hybrid) != 0:
		return "cost:hybrid"
	case len(c.Phyrexian) != 0:
		return "cost:phyrexian"
	case len(c.Twobrid) != 0:
		return "cost:twobrid"
	case len(c.HybridPhyrexian) != 0:
		return "cost:hybrid_phyrexian"
	case c.Life != 0 || len(c.LifeX) != 0 || c.LifeHalfUp:
		return "cost:life"
	case c.Tap:
		return "cost:tap"
	case len(c.Sac) != 0:
		return "cost:sacrifice"
	case len(c.Discard) != 0:
		return "cost:discard"
	case len(c.SubCounter) != 0:
		return "cost:sub_counter"
	case len(c.AddCounter) != 0:
		return "cost:add_counter"
	case len(c.Exile) != 0 || len(c.ExileFromTop) != 0:
		return "cost:exile"
	case len(c.Reveal) != 0 || len(c.RevealOrChoose) != 0 || len(c.RevealChosen) != 0:
		return "cost:reveal"
	case len(c.Behold) != 0:
		return "cost:behold"
	case len(c.TapPermanent) != 0:
		return "cost:tap_permanent"
	case len(c.Blight) != 0:
		return "cost:blight"
	case c.Forage:
		return "cost:forage"
	case len(c.Draw) != 0:
		return "cost:draw"
	case len(c.Energy) != 0:
		return "cost:energy"
	case len(c.DamageYou) != 0:
		return "cost:damage"
	case len(c.GainLife) != 0:
		return "cost:gain_life"
	case len(c.Return) != 0:
		return "cost:return"
	case len(c.PutToLib) != 0:
		return "cost:put_to_library"
	case len(c.MoveToGrave) != 0:
		return "cost:move_to_grave"
	case len(c.Mill) != 0:
		return "cost:mill"
	case len(c.Evidence) != 0:
		return "cost:evidence"
	case len(c.RollDice) != 0:
		return "cost:roll_dice"
	case len(c.Exert) != 0:
		return "cost:exert"
	case len(c.Unknown) != 0:
		return "cost:unknown"
	case len(c.Withheld) != 0:
		return "cost:withheld"
	case !PlanCostOK(c):
		return "cost:other"
	default:
		return ""
	}
}

// MentionsTarget reports whether s names a target reference in any case.
func MentionsTarget(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if strings.EqualFold(s[i:i+6], "target") {
			return true
		}
	}
	return false
}

// PlanUsesLastResort reports whether any step of plan discloses a
// consequence (exactly the last-resort steps).
func PlanUsesLastResort(plan decision.PaymentPlan) bool {
	for _, a := range plan.Activations {
		if a.Consequence != nil {
			return true
		}
	}
	return false
}

// PlanRemainingPain sums the disclosed life + damage of plan's steps
// from index from on (the executor's lethal revalidation).
func PlanRemainingPain(plan decision.PaymentPlan, from int) int64 {
	var pain int64
	for i := from; i < len(plan.Activations); i++ {
		if c := plan.Activations[i].Consequence; c != nil {
			pain += int64(c.Life) + int64(c.Damage)
		}
	}
	return pain
}

func PlanCostOK(c Cost) bool {
	return c.X == 0 && c.XMin == 0 && c.Snow == 0 && c.Life == 0 &&
		len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 && len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 &&
		!c.Tap && len(c.Sac) == 0 && len(c.Discard) == 0 && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.Reveal) == 0 && len(c.RevealOrChoose) == 0 && len(c.RevealChosen) == 0 && len(c.Behold) == 0 &&
		len(c.TapPermanent) == 0 && len(c.Blight) == 0 && !c.Forage && len(c.Draw) == 0 && len(c.Energy) == 0 &&
		len(c.LifeX) == 0 && !c.LifeHalfUp && len(c.DamageYou) == 0 && len(c.GainLife) == 0 && len(c.Return) == 0 &&
		len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 && len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 && len(c.Unknown) == 0 && len(c.Exert) == 0
}

// PlanPoolOK reports whether p's floating mana is plain: no snow,
// persistent or restricted mana and every producer-typed unit
// (state.Player.ManaUnits) empty. It reads the player in place; the units
// are formed exactly as ManaUnits forms them, without its two copies of
// the player.
func PlanPoolOK(p *state.Player) bool {
	if p.Snow.Total() != 0 || p.PersistentMana.Total() != 0 || len(p.RestrictedMana) != 0 {
		return false
	}
	for t := range p.TypedMana {
		m := p.TypedMana[t]
		if t < len(p.ArtifactTyped) {
			for i := range m {
				m[i] -= p.ArtifactTyped[t][i]
			}
		}
		if m.Total() != 0 {
			return false
		}
	}
	for t := range p.ArtifactTyped {
		if p.ArtifactTyped[t].Total() != 0 {
			return false
		}
	}
	return true
}

// PlanLastResortCostOK reports whether a last-resort activation cost
// is exactly {T} (optional), Sac<1/...>, PayLife<N> and Return<1/...>: every
// other part -- mana, X, counters, discard, exile, mill, reveal, energy, a tap
// of another permanent, an unparsed token -- is outside what a witness step
// can disclose, so the ability stays deferred.
func PlanLastResortCostOK(c Cost) bool {
	rest := c
	rest.Tap, rest.Sac, rest.Life, rest.Return = false, nil, 0, nil
	return rest.Generic == 0 && rest.Colored == (state.Mana{}) && len(rest.ExileFromTop) == 0 && PlanCostOK(rest)
}

// PlanHasSpecialProductionParam reports whether the ability's own head
// carries a production special-effect parameter (spec 3.2: TriggersWhenSpent$,
// AddsCounters$, the AddsKeywords* family, AddsNoCounter$, PersistentMana$,
// UnlessCost$, Defined$). Such production does something beyond adding plain
// mana to the pool, so the ability is deferred.
func PlanHasSpecialProductionParam(ma *cards.SA) bool {
	// An any-key test: key order cannot change the answer, so the keys are
	// walked unsorted.
	for key := range maps.Keys(ma.Params) {
		if key == "TriggersWhenSpent" || key == "AddsCounters" || key == "AddsNoCounter" ||
			key == "PersistentMana" || key == "UnlessCost" || key == "Defined" ||
			strings.HasPrefix(key, "AddsKeywords") {
			return true
		}
	}
	return false
}

// ContainsTargetFold is strings.Contains(strings.ToLower(key), "target")
// without the lowered copy for an ASCII key (a non-ASCII key takes the
// original expression, so the answer is identical for every input).
func ContainsTargetFold(key string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] >= 0x80 {
			return strings.Contains(strings.ToLower(key), "target")
		}
	}
	const w = "target"
	for i := 0; i+len(w) <= len(key); i++ {
		j := 0
		for ; j < len(w); j++ {
			c := key[i+j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != w[j] {
				break
			}
		}
		if j == len(w) {
			return true
		}
	}
	return false
}

func PlanSelfCost(part cost.CostPart, id state.ObjID) bool {
	s := strings.ToLower(strings.TrimSpace(part.Spec))
	return part.N == 1 && (s == "cardname" || s == "this token" || s == "cardname/self" || s == "self")
}

// PlanLastResortChoices is phase 2's alternative table (spec 5): every
// unit's normal AND last-resort alternatives, each carrying the source's
// phase-2 flexibility (flexAll, key 5 over every eligible alternative), minus
// any alternative whose own life + damage would by itself be lethal at the
// caster's current life (the lethal guard, applied to one step here and to
// the whole plan by the search). Unit positions are preserved. It returns
// nil when no unit has a last-resort alternative left, so phase 2 cannot
// differ from phase 1 and is skipped.
func PlanLastResortChoices(choices [][]Alt, life int32) [][]Alt {
	out := make([][]Alt, len(choices))
	any := false
	for i, alts := range choices {
		for _, a := range alts {
			if a.Tier < TierLastResort {
				continue
			}
			if pain := ConsequencePain(a.Consequence); pain > 0 && pain >= int64(life) {
				continue
			}
			if a.Tier == TierLastResort {
				any = true
			}
			a.Flex = a.FlexAll
			out[i] = append(out[i], a)
		}
	}
	if !any {
		return nil
	}
	return out
}

// consequence is exactly c.
func ConsequenceEqual(c Consequence, w *decision.PaymentConsequence) bool {
	got := c.Wire()
	if got == nil || w == nil {
		return got == nil && w == nil
	}
	return *got == *w
}

// AppendColourOnce appends col unless it is already present, preserving the
// first-seen order so an alternative list stays deterministic.
func AppendColourOnce(cols []string, col string) []string {
	for _, c := range cols {
		if c == col {
			return cols
		}
	}
	return append(cols, col)
}

// CostPips counts a parsed printed cost's coloured pips per WUBRG colour:
// each plain pip for its colour, each hybrid pip for both its colours, a
// Phyrexian pip for its colour, a twobrid for its coloured face (its generic
// face is not a pip) and a hybrid-Phyrexian pip for both its colours.
// Generic, {X} and {C} pips count for no colour.
func CostPips(c Cost) [5]int {
	var d [5]int
	for i, n := range c.Colored {
		if i < len(d) {
			d[i] += int(n)
		}
	}
	add := func(sym byte) {
		if i := state.ManaIndex(sym); i < len(d) {
			d[i]++
		}
	}
	for _, h := range c.Hybrid {
		add(h.A)
		add(h.B)
	}
	for _, p := range c.Phyrexian {
		add(p)
	}
	for _, t := range c.Twobrid {
		add(t.Col)
	}
	for _, h := range c.HybridPhyrexian {
		add(h.A)
		add(h.B)
	}
	return d
}

// PlanClassMembers is each class's member list (the verify-mode view
// of a class grouping).
func PlanClassMembers(classes []Class) [][]int {
	out := make([][]int, len(classes))
	for k := range classes {
		out[k] = classes[k].Members
	}
	return out
}

// PlanSameAlternative compares two computations of one alternative.
func PlanSameAlternative(a, b Alt) bool {
	return a == b
}

// PlanSameStep compares two resolutions of one witness step at one
// state: every field, the ability up to the identity of an ability built
// per call (a CR 305.6 intrinsic).
func PlanSameStep(a, b Alt) bool {
	if !SameManaAbility(a.Ma, b.Ma) {
		return false
	}
	a.Ma, b.Ma = nil, nil
	return a == b
}
