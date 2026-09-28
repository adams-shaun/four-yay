package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// PotentialPlan is the payment planner's verdict on one potential play
// (PotentialActions): whether the seat's floating pool plus its untapped
// sources can pay the play's mana, and, for a printed activated ability, the
// exact witness that does.
//
// It serves a seat playing a surface where mana is tapped before the play
// is offered (a manual-mana engine): the priority offer carries payment
// plans for ordinary casts only (Decision.PaymentActions), so an activated
// ability with a mana cost ({2}: Heap Gate's draw) had no witness, and the
// seat could only tap sources one by one and hope. With Plan it taps exactly
// the witness's sources and then activates.
type PotentialPlan struct {
	// Action is the play as PotentialActions projects it (raw label).
	Action decision.PotentialAction
	// Plan is the witness paying the play's mana part, set only for a
	// printed activated ability (Kind "ability" with a pile index). Its
	// Activations are what to tap; the ability's non-mana costs (a tap of
	// the source, a sacrifice, a discard) are the activation's own asks.
	Plan *decision.PaymentPlan
	// Reason is the planner verdict when Plan is nil: "" (payable: an
	// ordinary cast the planner can pay -- its witness rides
	// Decision.PaymentActions), "insufficient" (PROVEN unpayable: the census
	// covers every untapped mana source and no plan exists), "unsupported"
	// (a shape the planner does not price: X, hybrid, a mode, a granted
	// ability, an unmodelled source ...), "search_limit" or "ambiguous"
	// (another potential play shares this one's Kind/Obj/Ability/Mode, so a
	// seat keyed on those could not tell them apart).
	Reason string
	Detail string
}

// PotentialPaymentPlans prices every potential play of p (the same walk and
// order as PotentialActions) with the exact source-exclusive planner:
//
//   - an ordinary cast (Mode "", no alternative cost) from the hand or the
//     command zone gets PlanCastPayment's verdict (no Plan: the offered
//     witness is Decision.PaymentActions');
//   - a printed activated ability gets a witness for its offer-time mana
//     cost (the printed Cost$ with its own ReduceCost$ and the live
//     RaiseCost/ReduceCost statics, exactly abilityOfferCost's
//     composition), planned with the ability's own source withheld when its
//     cost taps, untaps or sacrifices that source;
//   - every other play is "unsupported".
//
// An "insufficient" verdict is only reported when the planner's source
// census covers every mana ability p could activate (a source the census
// cannot price makes the verdict "unsupported", never a false proof).
//
// A pure read: no event, no state write, no RNG draw. Determinism: zone-order
// walks only; the maps below are lookups.
func (e *Engine) PotentialPaymentPlans(p state.PlayerID) []PotentialPlan {
	if e.G.Over || int(p) < 0 || int(p) >= len(e.G.Players) {
		return nil
	}
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	defer e.paymentPlanQueryScope()()
	hyp := e.PotentialMana(p)
	opts := e.legalActionsPriced(p, &hyp)
	type key struct {
		kind    string
		obj     state.ObjID
		ability int
		mode    string
	}
	seen := map[key]int{} // lookup only
	for _, o := range opts {
		if potentialPlayKind(o.Kind) {
			seen[key{o.Kind, o.Obj, o.Ability, o.Mode}]++
		}
	}
	poolOK := paymentPlanPoolOK(e.G.Players[p])
	complete := -1 // lazily: 1 when the census covers every source
	var out []PotentialPlan
	for _, o := range opts {
		if !potentialPlayKind(o.Kind) {
			continue
		}
		pp := PotentialPlan{Action: decision.PotentialAction{Kind: o.Kind, Obj: o.Obj, Ability: o.Ability, Mode: o.Mode, Label: o.Label}}
		var got PaymentPlanOutcome
		switch {
		case seen[key{o.Kind, o.Obj, o.Ability, o.Mode}] > 1:
			got = PaymentPlanOutcome{Reason: "ambiguous"}
		case !poolOK:
			got = PaymentPlanOutcome{Reason: "unsupported", Detail: "pool"}
		case o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0:
			got = e.potentialCastVerdict(p, o.Obj)
		case o.Kind == "ability" && o.SVar == "" && o.Keyword == "" && o.GainedSource == 0 && o.AltCostIndex == 0:
			got = e.planAbilityPayment(p, o.Obj, o.Ability)
		default:
			got = PaymentPlanOutcome{Reason: "unsupported", Detail: "shape"}
		}
		if got.Reason == "insufficient" {
			if complete < 0 {
				complete = 0
				if e.paymentPlanCensusComplete(p) {
					complete = 1
				}
			}
			if complete == 0 {
				got = PaymentPlanOutcome{Reason: "unsupported", Detail: "census_incomplete"}
			}
		}
		pp.Reason, pp.Detail = got.Reason, got.Detail
		if o.Kind == "ability" && got.Plan != nil && got.Reason == "" {
			plan := decision.ClonePaymentPlan(*got.Plan)
			pp.Plan = &plan
		}
		out = append(out, pp)
	}
	return out
}

// potentialCastVerdict is PlanCastPayment's verdict for the ordinary cast of
// id, whose origin is read from its zone.
func (e *Engine) potentialCastVerdict(p state.PlayerID, id state.ObjID) PaymentPlanOutcome {
	o := e.G.Obj(id)
	if o == nil {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	origin := ""
	switch o.Zone {
	case state.ZHand:
		origin = "hand"
	case state.ZCommand:
		origin = "command_zone"
	default:
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "origin"}
	}
	got := e.PlanCastPayment(p, decision.PlannedCast{Object: id, Face: 0, Origin: origin})
	if got.Plan != nil && got.Reason == "search_limit" {
		return got
	}
	if got.Plan != nil {
		got.Reason = ""
	}
	return got
}

// planAbilityPayment plans the mana part of printed activated ability
// `ability` (a flat pile index, as the "ability" option carries it) of id.
func (e *Engine) planAbilityPayment(p state.PlayerID, id state.ObjID, ability int) PaymentPlanOutcome {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	pa, ok := o.PileAbilityAt(ability)
	if !ok || pa.SA == nil || pa.SA.Kind != "AB" || isManaAbilityAPI(pa.SA.API) {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "ability"}
	}
	ab := pa.SA
	cost, ok := e.fixLifeXCost(p, id, e.parseCost(ab.Params["Cost"]))
	if !ok {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:life_x"}
	}
	if n := e.ownReduceCostOffer(p, id, ab, pa.Merged); n > 0 && cost.Generic >= n {
		cost.Generic -= n
	} else if n > 0 {
		cost.Generic = 0
	}
	composed := e.offerCostFor(p, id, cost, abilityScope(ab))
	mana := Cost{Colored: composed.Colored, Generic: composed.Generic, X: composed.X, XMin: composed.XMin,
		Hybrid: composed.Hybrid, Phyrexian: composed.Phyrexian, Twobrid: composed.Twobrid,
		HybridPhyrexian: composed.HybridPhyrexian, Snow: composed.Snow}
	if detail := paymentPlanCostDetail(mana); detail != "" {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	if global, detail := e.paymentPlanGlobalManaEffect(p, id); global {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	var exclude state.ObjID
	if composed.Tap || composed.Untap {
		exclude = id
	}
	for _, part := range composed.Sac {
		if paymentPlanSelfCost(part, id) {
			exclude = id
		}
	}
	return e.planPaymentCostExcluding(p, decision.PlannedCast{}, Cost{Colored: mana.Colored, Generic: mana.Generic}, exclude)
}

// paymentPlanCensusComplete reports whether the planner's source census
// prices every mana ability p could activate: each battlefield permanent
// with an activatable mana ability (payability aside, as PotentialMana
// reads them) is a census unit with an alternative for every one of those
// abilities. Only then is the planner's "insufficient" a proof.
func (e *Engine) paymentPlanCensusComplete(p state.PlayerID) bool {
	units := e.paymentPlanQueryUnits(p)
	at := make(map[state.ObjID]int, len(units)) // lookup only
	for i, u := range units {
		at[u.id] = i
	}
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		abs := e.appendAvailableManaAbilitiesGate(nil, nil, p, id, true)
		if len(abs) == 0 {
			continue
		}
		i, ok := at[id]
		if !ok {
			return false
		}
		alts := e.paymentPlanQueryAlternatives(units[i])
		for _, ma := range abs {
			covered := false
			for _, a := range alts {
				if sameManaAbility(a.ma, ma) {
					covered = true
					break
				}
			}
			if !covered {
				return false
			}
		}
	}
	return true
}
