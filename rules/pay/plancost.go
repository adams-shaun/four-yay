package pay

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

func PlanPaymentCost(e Engine, p state.PlayerID, cast decision.PlannedCast, cost costvocab.Cost) PlanOutcome {
	q := e.Session().PlanQuery
	if !q.Valid(e.Log()) || e.Session().PaymentPlanRelaxed != nil || e.Session().PaymentPlanRelaxedFee != 0 || !plainManaCost(cost, e.Verify()) {
		return planPaymentCostExcluding(e, p, cast, cost, nil)
	}
	// Within one query scope (one state), the planner's outcome for a plain
	// mana cost reads the cast only through the hand Demand that excludes
	// it (planPaymentCostWithout's rank context): two casts with the same
	// payer, cost and Demand -- two copies of a card in hand -- plan
	// identically, so the scope serves the first one's outcome, with its
	// own copy of the witness.
	Demand := PaymentPlanHandDemand(e, p, cast.Object)
	Key := PlanCostKey{Payer: p, Colored: cost.Colored, Generic: cost.Generic, Demand: Demand}
	for i := range q.Plans {
		if q.Plans[i].Key == Key {
			out := q.Plans[i].Out
			if e.Verify() {
				if want := planPaymentCostWithDemand(e, p, Demand, cost, nil, nil, nil); !PlanOutcomeEqual(want, out) {
					panic(fmt.Sprintf("payment plan query: cost memo for %+v served %+v, planned %+v", Key, out, want))
				}
			}
			if out.Plan != nil {
				plan := decision.ClonePaymentPlan(*out.Plan)
				out.Plan = &plan
			}
			return out
		}
	}
	out := planPaymentCostWithDemand(e, p, Demand, cost, nil, nil, nil)
	stored := out
	if out.Plan != nil {
		plan := decision.ClonePaymentPlan(*out.Plan)
		stored.Plan = &plan
	}
	q.Plans = append(q.Plans, PlanCostMemo{Key: Key, Out: stored})
	return out
}

// plainManaCost reports whether c is only coloured and generic mana -- the
// costs whose plan the query scope memoises. Verify mode checks the field
// walk against the whole struct, so a Cost field added later cannot slip
// past it.
func plainManaCost(c costvocab.Cost, verify bool) bool {
	ok := c.Life == 0 && c.X == 0 && c.XMin == 0 && c.Snow == 0 && c.Waterbend == 0 && !c.WaterbendX &&
		!c.Tap && !c.Untap && !c.Forage && !c.LifeHalfUp &&
		len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 && len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 &&
		len(c.Sac) == 0 && len(c.Discard) == 0 && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.ExileFromTop) == 0 && len(c.Reveal) == 0 && len(c.RevealOrChoose) == 0 &&
		len(c.RevealChosen) == 0 && len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.UntapPermanent) == 0 &&
		len(c.Blight) == 0 && len(c.Exert) == 0 && len(c.LifeX) == 0 && len(c.Draw) == 0 && len(c.Energy) == 0 &&
		len(c.DamageYou) == 0 && len(c.GainLife) == 0 && len(c.Return) == 0 && len(c.PutToLib) == 0 &&
		len(c.MoveToGrave) == 0 && len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 &&
		len(c.Unknown) == 0 && len(c.Withheld) == 0
	if verify && ok != c.Equal(&costvocab.Cost{Colored: c.Colored, Generic: c.Generic}) {
		panic(fmt.Sprintf("payment plan: plainManaCost(%+v) = %v disagrees with the struct", c, ok))
	}
	return ok
}

// planPaymentCostExcluding is planPaymentCost over the census with the
// sources in exclude left out entirely (planPaymentCostWithout's gone).
func planPaymentCostExcluding(e Engine, p state.PlayerID, cast decision.PlannedCast, cost costvocab.Cost, exclude []state.ObjID) PlanOutcome {
	return PlanPaymentCostWithout(e, p, cast, cost, nil, exclude, nil)
}

// PlanPaymentCostWithout is planPaymentCost over the census with three
// kinds of withheld source (PotentialPaymentPlans): a tapped source is one
// the play's own cost taps (the ability's {T}, a tapXType candidate), so it
// keeps only its alternatives whose ability does not tap it (Wall of
// Roots' counter); a kept source is one the play's cost sacrifices, so it
// keeps only its alternatives that leave it on the battlefield (it may tap
// for mana first); a gone source is left out entirely. With none it is
// planPaymentCost exactly, cached classes included; a withheld census
// groups its own classes, because the query cache's classes are keyed by
// payer and phase only.
func PlanPaymentCostWithout(e Engine, p state.PlayerID, cast decision.PlannedCast, cost costvocab.Cost, tapped, gone, kept []state.ObjID) PlanOutcome {
	return planPaymentCostWithDemand(e, p, PaymentPlanHandDemand(e, p, cast.Object), cost, tapped, gone, kept)
}

// planPaymentCostWithDemand is planPaymentCostWithout over the hand demand
// that excludes the cast (paymentPlanHandDemand), the one place the cast
// enters the plan.
func planPaymentCostWithDemand(e Engine, p state.PlayerID, demand [5]int, cost costvocab.Cost, tapped, gone, kept []state.ObjID) PlanOutcome {
	defer PaymentPlanQueryEnd(e, PaymentPlanQueryBegin(e))
	units := PaymentPlanQueryUnits(e, p)
	queryClasses := func(p state.PlayerID, minTier Tier, choices [][]Alt) []Class {
		return paymentPlanQueryClasses(e, p, minTier, choices)
	}
	ownClasses := func(_ state.PlayerID, _ Tier, choices [][]Alt) []Class {
		return Classes(choices)
	}
	if len(gone) != 0 {
		kept := make([]WindowUnit, 0, len(units))
		for _, u := range units {
			if !slices.Contains(gone, u.ID) {
				kept = append(kept, u)
			}
		}
		units = kept
		queryClasses = ownClasses
	}
	// withhold drops a tapped source's tapping alternatives and a kept
	// source's consuming ones.
	withhold := func(alts []Alt) []Alt {
		if len(alts) == 0 {
			return alts
		}
		src := alts[0].Activation.Source
		tap, keep := slices.Contains(tapped, src), slices.Contains(kept, src)
		if !tap && !keep {
			return alts
		}
		var out []Alt
		for _, alt := range alts {
			if tap && ParseCostOf(e, alt.Ma.ParamStr(cards.PKCost)).Tap {
				continue
			}
			if keep && (alt.Consequence.Sacrifice || alt.Consequence.ReturnToHand) {
				continue
			}
			out = append(out, alt)
		}
		return out
	}
	if len(tapped) != 0 || len(kept) != 0 {
		queryClasses = ownClasses
	}
	// V1 accepts only fixed production.  A permissive window unit is useful to
	// manual payment, but not proof an automatic choice will remain exact.
	choices := make([][]Alt, len(units))
	for i, u := range units {
		choices[i] = withhold(PaymentPlanQueryAlternatives(e, u))
	}
	if len(e.Session().PaymentPlanRelaxed) != 0 {
		// PotentialPaymentPlans' relaxed proof (paymentPlanRelaxProof): the
		// census's uncovered sources as free, never-executed alternatives,
		// withheld exactly like the census's own, and the fees of the paid
		// ones it admits charged as generic.
		cost.Generic += e.Session().PaymentPlanRelaxedFee
		for _, alts := range e.Session().PaymentPlanRelaxed {
			if len(alts) == 0 || slices.Contains(gone, alts[0].Activation.Source) {
				continue
			}
			choices = append(choices, withhold(alts))
		}
		queryClasses = ownClasses
	}
	// Phase 1 (spec 5): normal sources only. If it finds a complete plan,
	// that is the offer and last-resort sources are never considered.
	life := e.Game().Players[p].Life
	phase1 := PhaseChoices(choices, TierNormal)
	rankCtx := NewRankContext(choices, demand)
	search := SearchInto(e.SearchScratch(), cost, e.Game().Players[p].Pool, life, rankCtx,
		phase1, queryClasses(p, TierNormal, phase1), PaymentSearchEnv(e.Verify()))
	nodes := search.Nodes
	// Phase 2 runs only when phase 1 PROVES no plan exists (insufficient,
	// not search_limit): normal plus last-resort alternatives, ranked by the
	// irreversible-cost key, never a plan whose summed life + damage would
	// reduce the caster to 0 or less. The rank context is phase 1's: keys 6
	// and 7 read the untapped normal remainder in both phases.
	if search.Best == nil && !search.Limited {
		if phase2 := PlanLastResortChoices(choices, life); phase2 != nil {
			search = SearchInto(e.SearchScratch(), cost, e.Game().Players[p].Pool, life, rankCtx,
				phase2, queryClasses(p, TierLastResort, phase2), PaymentSearchEnv(e.Verify()))
			nodes += search.Nodes
		}
	}
	switch {
	case search.Best != nil && search.Limited:
		return PlanOutcome{Plan: search.Best, Nodes: nodes, Reason: "search_limit"}
	case search.Best != nil:
		return PlanOutcome{Plan: search.Best, Nodes: nodes}
	case search.Limited:
		return PlanOutcome{Reason: "search_limit", Nodes: nodes}
	}
	// The first source diagnostic is read only for the outcome that
	// reports it.
	firstSourceDetail := ""
	for _, u := range units {
		if firstSourceDetail != "" {
			break
		}
		for _, alt := range u.Alts {
			o := e.Game().Obj(u.ID)
			p := state.PlayerID(0)
			if o != nil {
				p = o.Controller
			}
			_, _, detail := PaymentPlanAbilityTier(e, p, u.ID, alt.Ma)
			if detail != "" {
				firstSourceDetail = detail
				break
			}
		}
	}
	return PlanOutcome{Reason: "insufficient", Detail: firstSourceDetail, Nodes: nodes}
}

// PaymentPlanQueryUnits is paymentPlanManaUnits, computed once per query
// scope and player. The returned units are shared: callers only read them.
func PaymentPlanQueryUnits(e Engine, p state.PlayerID) []WindowUnit {
	q := e.Session().PlanQuery
	if !q.Valid(e.Log()) {
		return e.Eval().ManaUnits(p)
	}
	if units, ok := q.Units[p]; ok {
		if e.Verify() && !SameUnits(units, e.Eval().ManaUnits(p)) {
			panic(fmt.Sprintf("payment plan query: cached source census for player %d is stale", p))
		}
		return units
	}
	units := e.Eval().ManaUnits(p)
	if q.Units == nil {
		q.Units = map[state.PlayerID][]WindowUnit{}
	}
	q.Units[p] = units
	return units
}

// paymentPlanQueryClasses is paymentPlanClasses over one phase's choices,
// computed once per query scope, payer and phase: within a scope the
// choices are the cached census's, so every candidate cast groups them the
// same way. The classes are shared: the search only reads them.
func paymentPlanQueryClasses(e Engine, p state.PlayerID, minTier Tier, choices [][]Alt) []Class {
	q := e.Session().PlanQuery
	if !q.Valid(e.Log()) {
		return Classes(choices)
	}
	key := PlanClassesKey{Payer: p, MinTier: minTier}
	if classes, ok := q.Classes[key]; ok {
		if e.Verify() && !intSlicesEqual(PlanClassMembers(classes), PlanClassMembers(Classes(choices))) {
			panic(fmt.Sprintf("payment plan query: cached classes for player %d are stale", p))
		}
		return classes
	}
	classes := Classes(choices)
	if q.Classes == nil {
		q.Classes = map[PlanClassesKey][]Class{}
	}
	q.Classes[key] = classes
	return classes
}

// PaymentSearchEnv wires the pay package's search to the engine's mana
// solver: a complete count vector is settled with resolveManaWith, the same
// solver execution uses.
func PaymentSearchEnv(verify bool) Env {
	return Env{
		Settle: func(c costvocab.Cost, pool state.Mana, life int32) (state.Mana, bool) {
			paid, ok := ResolveManaWith(c, pool, state.Mana{}, [7]state.Mana{}, life, false, PipRider{}, nil)
			return paid.Pool, ok
		},
		Verify: verify,
	}
}
