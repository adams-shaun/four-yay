package rules

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// PotentialPlan is the payment planner's verdict on one potential play
// (PotentialActions): whether the seat's floating pool plus its mana
// sources can pay the play's mana, and the exact witness (or scripted
// prefix) that does.
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
	// Plan is the witness paying the play's mana part, set for an ordinary
	// cast (the same witness Decision.PaymentActions offers while the pool
	// is plain), a printed activated ability (Kind "ability" with a pile
	// index), a cast in an alternative or additional-cost mode (Mode
	// flashback, bestowed, kicked ...), an ordinary {X} cast (the largest X
	// the sources pay) and a hybrid one.
	// Its Activations are what to tap; the play's non-mana costs (a tap of
	// the source, a sacrifice, a discard) and its X are the play's own asks.
	Plan *decision.PaymentPlan
	// Reason is the planner verdict: "" (payable: Plan or Script says
	// how; an ordinary cast whose witness rides Decision.PaymentActions
	// may carry neither), "insufficient" (PROVEN unpayable: the census
	// covers every mana ability p could activate, or a relaxation of the
	// ones it misses, and no plan exists), "unsupported" (a shape the
	// planner does not price: a granted ability, a mode it does not
	// compose, a play only an uncovered source could pay that the prefix
	// search did not reach ...; PotentialPlayScript is the exact fallback),
	// "search_limit" or "ambiguous" (another potential play shares this
	// one's Kind/Obj/Ability/Mode, so a seat keyed on those could not tell
	// them apart).
	Reason string
	Detail string
	// Script, when set (Reason ""), is a PREFIX the seat plays first: its
	// answers activate mana sources the planner census cannot price (Saruli
	// Caretaker's "{T}, tap an untapped creature", Wall of Roots' counter),
	// found by an exact search on engine clones. After the last step the
	// same query prices the play from the floating pool (a witness, or the
	// cast's payment action), and the seat pays it as usual.
	Script []ScriptStep
}

// PotentialPaymentPlans prices every potential play of p (the same walk and
// order as PotentialActions) with the exact source-exclusive planner:
//
//   - an ordinary cast (Mode "", no alternative cost) from the hand or the
//     command zone gets PlanCastPayment's verdict and witness (the one
//     Decision.PaymentActions offers); a shape it declines ({X}, hybrid,
//     an additional cost) is planned from its composed cost instead;
//   - a printed activated ability gets a witness for its offer-time mana
//     cost (the printed Cost$ with its own ReduceCost$ and the live
//     RaiseCost/ReduceCost statics, exactly abilityOfferCost's
//     composition), planned so that the ability's own tap, tapXType and
//     sacrifice parts stay payable (planComposedCost);
//   - a cast in a mode the cast planner does not witness (flashback,
//     bestow, kicker, may-play, plot ...) gets a witness from
//     potentialModeCastPlan, confirmed by the offer gate itself;
//   - every other play is "unsupported".
//
// Inside the query the floating pool's snow, persistent and producer-typed
// units count as ordinary mana (paymentPlanPoolAccepted). A witness that
// sacrifices or returns a permanent is replayed on a clone
// (potentialWitnessReaches) before it is offered.
//
// An "insufficient" verdict is only reported as a proof: when the planner's
// source census covers every mana ability p could activate, or when a
// relaxation of the abilities it misses (paymentPlanRelaxProof) still
// cannot pay. Otherwise a play only those abilities could pay gets a
// scripted prefix when potentialPrefixScript finds one, and is left
// "unsupported" (census_incomplete) when not.
//
// A pure read: no event, no state write, no RNG draw. Determinism: zone-order
// walks only; the maps below are lookups.
func (e *Engine) PotentialPaymentPlans(p state.PlayerID) []PotentialPlan {
	if e.G.Over || int(p) < 0 || int(p) >= len(e.G.Players) {
		return nil
	}
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	defer pay.PaymentPlanQueryEnd(asPayer(e), e.paymentPlanQueryResumeBegin(p))
	prevPool := e.PaymentPlanPotentialPool
	e.PaymentPlanPotentialPool = true
	defer func() { e.PaymentPlanPotentialPool = prevPool }()
	hyp, opts := e.potentialWalkOf(p, true)
	// ambiguous[i]: another potential play shares opts[i]'s (kind, object,
	// ability, mode) identity. A pairwise scan over the walk's few plays
	// spares the per-query map.
	var ambiguousBuf [128]bool
	ambiguous := ambiguousBuf[:0]
	if len(opts) <= len(ambiguousBuf) {
		ambiguous = ambiguousBuf[:len(opts)]
	} else {
		ambiguous = make([]bool, len(opts))
	}
	for i := range opts {
		a := &opts[i]
		if !potentialPlayKind(a.Kind) {
			continue
		}
		for j := i + 1; j < len(opts); j++ {
			b := &opts[j]
			if a.Obj == b.Obj && a.Ability == b.Ability && a.Kind == b.Kind && a.Mode == b.Mode {
				ambiguous[i], ambiguous[j] = true, true
			}
		}
	}
	poolOK := pay.PaymentPlanPoolAccepted(asPayer(e), p)
	// Every ordinary cast verdict below names a plain cast the PotentialMana
	// walk above listed, so the planner shares one cost-static collection
	// and a priced candidate set across them (castPlanShare).
	share := castPlanShare{statics: costStaticSource{e: e}, candidates: paymentCastCandidates{e: e, p: p, priced: true}}
	censused := false // the census (and its relaxed alternatives) was read
	scripts := 0      // prefix searches run (potentialScriptPlays bounds them)
	var census paymentPlanCensus
	var out []PotentialPlan
	for oi, o := range opts {
		if !potentialPlayKind(o.Kind) {
			continue
		}
		pp := PotentialPlan{Action: decision.PotentialAction{Kind: o.Kind, Obj: o.Obj, Ability: o.Ability, Mode: o.Mode, Label: o.Label}}
		witness := false // the verdict's Plan is the play's witness (not Decision.PaymentActions')
		verdict := func() PaymentPlanOutcome {
			switch {
			case ambiguous[oi]:
				return PaymentPlanOutcome{Reason: "ambiguous"}
			case !poolOK:
				return PaymentPlanOutcome{Reason: "unsupported", Detail: "pool"}
			}
			got, w := e.potentialPlayVerdictShared(p, o, &share)
			witness = w
			return got
		}
		got := verdict()
		if got.Reason == "insufficient" {
			if !censused {
				census, censused = e.paymentPlanCensusOf(p, &hyp), true
			}
			if !census.complete {
				got = PaymentPlanOutcome{Reason: "unsupported", Detail: "census_incomplete"}
				if census.relaxable {
					// The census misses a source the planner cannot execute
					// (Saruli Caretaker's tapXType, Wall of Roots' counter,
					// Heap Gate's {1} fee). Re-ask with each such ability as
					// a free, relaxed alternative: a play no relaxed plan
					// pays is unpayable by any real sequence.
					if r := e.paymentPlanRelaxProof(census, verdict); r.Reason == "insufficient" {
						got = PaymentPlanOutcome{Reason: "insufficient", Detail: "relaxed_census"}
					}
				}
				if got.Reason != "insufficient" && scripts < potentialScriptPlays {
					// Payable, if at all, only through an uncovered source:
					// search the activations of those sources on clones
					// for a prefix after which the planner pays the play.
					scripts++
					if script, ok := e.potentialPrefixScript(p, o, census.gaps, census.fees()); ok {
						pp.Script = script
						got = PaymentPlanOutcome{}
					}
				}
			}
		}
		if witness && got.Plan != nil && got.Reason == "" && paymentPlanConsumes(*got.Plan) && !e.potentialWitnessReaches(p, o, *got.Plan) {
			// The witness sacrifices or returns a permanent the play
			// itself needed (its only target): not a way to pay it.
			got = PaymentPlanOutcome{Reason: "unsupported", Detail: "witness_unreachable"}
		}
		pp.Reason, pp.Detail = got.Reason, got.Detail
		if witness && got.Plan != nil && got.Reason == "" {
			plan := decision.ClonePaymentPlan(*got.Plan)
			pp.Plan = &plan
		}
		out = append(out, pp)
	}
	return out
}

// paymentPlanConsumes reports whether some step of plan sacrifices or
// returns its source.
func paymentPlanConsumes(plan decision.PaymentPlan) bool {
	for _, a := range plan.Activations {
		if a.Consequence != nil && (a.Consequence.Sacrifice || a.Consequence.ReturnToHand) {
			return true
		}
	}
	return false
}

// potentialPlayVerdict is the planner's verdict on potential play o of p,
// before the census check: witness reports whether the outcome's Plan is
// a witness for the play (always, except an unsupported shape).
func (e *Engine) potentialPlayVerdict(p state.PlayerID, o decision.Option) (got PaymentPlanOutcome, witness bool) {
	return e.potentialPlayVerdictShared(p, o, nil)
}

// castPlanShare is the cast planner's per-query inputs shared across one
// PotentialPaymentPlans query's ordinary-cast verdicts: one lazy cost-static
// collection and a candidate set marked priced. Every query names a plain
// cast the query's own PotentialMana walk listed -- exactly the offer
// builder's priced set (paymentCastCandidates' priced argument), so
// membership is certain and the per-cast huge-pool walk PlanCastPayment
// would run is skipped (pricedCandidatesVerify runs it and panics on a
// miss). The statics are a pure read of the unchanged board, shared exactly
// as the offer builder shares them.
type castPlanShare struct {
	statics    costStaticSource
	candidates paymentCastCandidates
}

// potentialPlayVerdictShared is potentialPlayVerdict with the ordinary-cast
// verdict planned over sh (nil: PlanCastPayment's own fresh inputs). sh is
// only valid for an o listed by the PotentialMana walk at this state.
func (e *Engine) potentialPlayVerdictShared(p state.PlayerID, o decision.Option, sh *castPlanShare) (got PaymentPlanOutcome, witness bool) {
	switch {
	case o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0:
		got := e.potentialCastVerdict(p, o.Obj, sh)
		if got.Reason == "unsupported" {
			// A shape the cast planner does not witness -- an {X} spell
			// (the largest X the sources pay), a hybrid one (each pip
			// resolved to a colour), an additional discard or announced
			// sacrifice, a target-dependent price -- planned from its
			// composed cost and confirmed by the offer gate.
			alt := e.potentialModeCastPlan(p, o)
			if alt.Reason == "" || got.Detail == "cost:x" || got.Detail == "cost:hybrid" {
				// A witness always; a proof only for the shapes the
				// composition models exactly (the planner declined the
				// others for a reason the composition may not see).
				return alt, true
			}
		}
		// The cast planner's witness rides Decision.PaymentActions too, but
		// only while the pool is plain (paymentPlanPoolOK); it is carried
		// here so a seat holding typed or snow mana can still lower it.
		return got, true
	case o.Kind == "ability" && o.SVar == "" && o.Keyword == "" && o.GainedSource == 0 && o.AltCostIndex == 0:
		return e.planAbilityPayment(p, o.Obj, o.Ability), true
	case o.Kind == "cast" && o.AltCostIndex == 0:
		return e.potentialModeCastPlan(p, o), true
	}
	return PaymentPlanOutcome{Reason: "unsupported", Detail: "shape"}, false
}

// relaxedPaid is a missed paid ability's relaxed alternatives and fee.
type relaxedPaid struct {
	alts []pay.Alt
	fee  int32
}

// relaxedPaidLimit bounds the paid missed abilities a relaxed proof
// enumerates subsets of (2^n proof runs).
const relaxedPaidLimit = 3

// paymentPlanRelaxProof runs verdict with the census's relaxed alternatives
// appended to every planner search: the free ones always, and the paid ones
// (filters) for each subset in turn, with that subset's fees charged as
// generic on top of the play's cost. It returns "insufficient" only when
// every run is: whatever set of paid abilities a real payment uses, the run
// for that set already covers it. A relaxed alternative is never
// executable, so no Plan is returned.
func (e *Engine) paymentPlanRelaxProof(c paymentPlanCensus, verdict func() PaymentPlanOutcome) PaymentPlanOutcome {
	prev, prevFee := e.PaymentPlanRelaxed, e.PaymentPlanRelaxedFee
	defer func() { e.PaymentPlanRelaxed, e.PaymentPlanRelaxedFee = prev, prevFee }()
	for mask := 0; mask < 1<<len(c.paid); mask++ {
		relaxed := slices.Clone(c.relaxed)
		fee := int32(0)
		for i, pd := range c.paid {
			if mask>>i&1 == 1 {
				relaxed = append(relaxed, pd.alts)
				fee += pd.fee
			}
		}
		if len(relaxed) == 0 {
			relaxed = [][]pay.Alt{nil} // the census alone, still a relaxed run
		}
		e.PaymentPlanRelaxed, e.PaymentPlanRelaxedFee = relaxed, fee
		if got := verdict(); got.Reason != "insufficient" {
			return PaymentPlanOutcome{Reason: got.Reason, Detail: got.Detail}
		}
	}
	return PaymentPlanOutcome{Reason: "insufficient"}
}

// potentialModeCastPlan prices a cast the cast planner does not witness --
// an alternative or additional-cost mode (flashback, bestow, kicker,
// buyback, entwine, surge, evoke, dash, overload, warp, madness, miracle)
// or an {X} spell -- and returns a witness for its mana part.
//
// The cost is composed exactly as beginCast charges it for the mode (the
// same per-mode helper, fixLifeXCost, then offerCostFor's CR 601.2f
// modifiers and commander tax), with an {X} folded in before the modifiers
// as manaToPay does. An {X} spell's witness pays the LARGEST X its sources
// can fund (the X announcement then offers every value up to it, so no X is
// lost); an X the sources cannot pay even at its minimum is insufficient.
// Convoke, improvise and delve credits are not modelled ("unsupported").
//
// Every witness is then checked against the offer gate itself: the play
// must be offered by the potential walk priced with exactly the floating
// pool plus the witness's production. A composition that disagreed with the
// engine's gate is therefore "unsupported", never a wrong witness.
func (e *Engine) potentialModeCastPlan(p state.PlayerID, o decision.Option) PaymentPlanOutcome {
	id := o.Obj
	obj := e.G.Obj(id)
	if obj == nil || obj.Face() == nil {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	f := obj.Face()
	if e.hasCastConvoke(id) || e.hasCastImprovise(id) || e.hasKeywordH(id, kwhDelve) {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:credit"}
	}
	base, ok := e.potentialModeBaseCost(p, id, f, o)
	if !ok {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "shape"}
	}
	if base, ok = pay.FixLifeXCost(asPayer(e), p, id, base); !ok {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:life_x"}
	}
	scope := spellScope(o.Mode)
	if pay.CostAnnouncesSacX(base) || e.costModifiers(p, id, scope).Waterbend > 0 {
		// An announced Sac<X> or a waterbend credit reprices the cast by
		// what the payment taps or sacrifices: not composed here.
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:credit"}
	}
	plan := func(x int32) PaymentPlanOutcome {
		c := base
		if base.X != 0 {
			c = base.WithX(x)
		}
		return e.planComposedCost(p, id, e.offerCostFor(p, id, c, scope), false, false)
	}
	lo := base.XMin
	best := plan(lo)
	if best.Reason == "insufficient" && e.paymentPlanTargetDependentFor(e.collectCostStatics(), p, id, scope) {
		// A ValidTarget$ cost static may price this cast lower for some
		// target (offerCastable's second pass); the composition above
		// does not see it, so it proves nothing.
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "shape:target_dependent_cost"}
	}
	if best.Plan == nil || best.Reason != "" || len(e.PaymentPlanRelaxed) != 0 {
		// No witness, or a relaxed proof run (which reads only whether the
		// minimum is payable, and whose plans are never executed).
		return best
	}
	if base.X != 0 {
		// Payability is monotone in X: binary-search the largest payable X
		// up to what the floating pool and every census source together
		// could fund (the census bounds a witness; PotentialMana's bound may
		// be unbounded).
		hi := e.paymentPlanCensusTotal(p) / int32(base.X)
		for lo < hi {
			mid := lo + (hi-lo+1)/2
			if got := plan(mid); got.Plan != nil && got.Reason == "" {
				best, lo = got, mid
			} else {
				hi = mid - 1
			}
		}
	}
	if !e.potentialWitnessOffers(p, o, *best.Plan) {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "witness_not_offered"}
	}
	return best
}

// paymentPlanCensusTotal is the most mana a witness can hold: p's floating
// pool plus, per census source, its largest alternative.
func (e *Engine) paymentPlanCensusTotal(p state.PlayerID) int32 {
	total := e.G.Players[p].Pool.Total()
	for _, u := range pay.PaymentPlanQueryUnits(asPayer(e), p) {
		best := int32(0)
		for _, a := range pay.PaymentPlanQueryAlternatives(asPayer(e), u) {
			best = max(best, a.Mana.Total())
		}
		total += best
	}
	return total
}

// potentialModeBaseCost is the raw (pre-modifier) cost beginCast charges a
// cast of id in mode, for the modes potentialModeCastPlan prices; ok is
// false for any other mode.
func (e *Engine) potentialModeBaseCost(p state.PlayerID, id state.ObjID, f *cards.Face, o decision.Option) (Cost, bool) {
	cost := pay.RawBaseCost(asPayer(e), p, id)
	mode := o.Mode
	switch potentialModeBaseCostCodes.Code(string(mode)) {
	case potentialModeBaseCostEmpty:
		return pay.WithSpellAbilityExtras(f, cost), true
	case potentialModeBaseCostMayplay, potentialModeBaseCostBargained:
		// A may-play grant (an impulse draw's exile): the printed cost, or
		// none, plus the grant's own raise -- the same read beginCast makes.
		free, raise, hasRaise, priced := e.mayPlayPermFreeRaise(p, id, o.MayPlayPerm)
		if free {
			cost = Cost{}
		}
		if hasRaise {
			if !priced {
				return Cost{}, false
			}
			cost = cost.Plus(raise)
		}
		return pay.WithSpellAbilityExtras(f, cost), true
	case potentialModeBaseCostPlot:
		// The plot special action pays the K:Plot parameter.
		raw, ok := f.KeywordParam("Plot")
		if !ok {
			return Cost{}, false
		}
		return ParseCost(raw), true
	case potentialModeBaseCostFlashback:
		return pay.WithSpellAbilityExtras(f, e.flashbackCostFor(id, o)), true
	case potentialModeBaseCostBestowed:
		return bestowCost(f)
	case potentialModeBaseCostKicked:
		kc, ok := kickerCost(f)
		return cost.Plus(kc), ok
	case potentialModeBaseCostBuyback:
		bc, ok := buybackCost(f)
		return cost.Plus(bc), ok
	case potentialModeBaseCostEntwined:
		ec, ok := entwineCost(f)
		return cost.Plus(ec), ok
	case potentialModeBaseCostSurged:
		return surgeCost(f)
	case potentialModeBaseCostAltCostKeyword:
		head := map[string]string{"evoked": "Evoke", "dashed": "Dash", "overloaded": "Overload", "warped": "Warp",
			"madness": "Madness", "miracle": "Miracle"}[mode]
		mc, ok := f.KeywordParam(head)
		if !ok {
			return Cost{}, false
		}
		return ParseCost(mc), true
	}
	return Cost{}, false
}

// potentialWitnessOffers reports whether the potential walk, priced with
// p's floating pool plus plan's production, offers the play o: the engine's
// own offer gate confirming a witness (non-mana parts are read for real).
func (e *Engine) potentialWitnessOffers(p state.PlayerID, o decision.Option, plan decision.PaymentPlan) bool {
	pool := e.G.Players[p].Pool
	for _, a := range plan.Activations {
		for c, n := range a.Produces {
			pool[c] += int32(n)
		}
	}
	// A cast play is looked up among the walk's cast options only, which
	// the casts-only walk lists exactly as the full walk does
	// (castsOnlyWalkVerify), without pricing every battlefield ability.
	opts := e.legalActionsWalkTemp(p, &pool, o.Kind == "cast")
	defer e.optRelease(opts)
	for _, opt := range opts {
		if opt.Kind == o.Kind && opt.Obj == o.Obj && opt.Ability == o.Ability && opt.Mode == o.Mode && opt.AltCostIndex == o.AltCostIndex {
			return true
		}
	}
	return false
}

// potentialCastVerdict is PlanCastPayment's verdict for the ordinary cast of
// id, whose origin is read from its zone.
func (e *Engine) potentialCastVerdict(p state.PlayerID, id state.ObjID, sh *castPlanShare) PaymentPlanOutcome {
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
	cast := decision.PlannedCast{Object: id, Face: 0, Origin: origin}
	var got PaymentPlanOutcome
	if sh != nil {
		got = e.planCastPaymentMemo(p, cast, &sh.statics, &sh.candidates)
	} else {
		got = e.PlanCastPayment(p, cast)
	}
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
	if !ok || pa.SA == nil || pa.SA.Kind != "AB" || cards.IsManaAbilityAPI(pa.SA.API) {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "ability"}
	}
	ab := pa.SA
	cost, ok := pay.FixLifeXCost(asPayer(e), p, id, e.parseCost(ab.ParamStr(cards.PKCost)))
	if !ok {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:life_x"}
	}
	if n := e.ownReduceCostOffer(p, id, ab, pa.Merged); n > 0 && cost.Generic >= n {
		cost.Generic -= n
	} else if n > 0 {
		cost.Generic = 0
	}
	composed := e.offerCostFor(p, id, cost, abilityScope(ab))
	return e.planComposedCost(p, id, composed, true, true)
}

// planComposedCost plans the mana part of composed, the offer-time cost
// (offerCostFor's composition) of a play of id: an activated ability of the
// permanent id (ability true, selfSource true) or a spell id (ability
// false). Only the mana part is planned; the non-mana parts are the play's
// own asks, and the plan must leave them payable:
//
//   - a fixed-count tapXType part (Heap Gate's "tap an untapped Gate you
//     control") needs untapped candidates the plan did not tap for mana. The
//     planner tries every assignment of candidates to the part (bounded by
//     planReservationLimit) with those candidates withheld from the mana
//     census, so a play is priced whichever candidates it must keep;
//   - a fixed-count sacrifice part needs candidates the plan did not
//     sacrifice or return: a source TAPPED for mana is still a legal
//     sacrifice (CR 602.2b pays the costs in any order), so Makeshift
//     Munitions' "{1}, Sacrifice an artifact" may tap an artifact land for
//     the {1} and then sacrifice it;
//   - a cost that taps or untaps the source itself withholds it.
//
// The verdict is "insufficient" (a proof, modulo the census) only when no
// mana plan exists at all, or when every candidate assignment was tried and
// each had no mana plan; any other failure is "unsupported" with Detail
// "cost:reserved_sources".
func (e *Engine) planComposedCost(p state.PlayerID, id state.ObjID, composed Cost, ability, selfSource bool) PaymentPlanOutcome {
	if len(composed.Hybrid) != 0 {
		return e.planHybridCost(p, id, composed, ability, selfSource)
	}
	mana := Cost{Colored: composed.Colored, Generic: composed.Generic, X: composed.X, XMin: composed.XMin,
		Hybrid: composed.Hybrid, Phyrexian: composed.Phyrexian, Twobrid: composed.Twobrid,
		HybridPhyrexian: composed.HybridPhyrexian, Snow: composed.Snow}
	if detail := pay.PlanCostDetail(mana); detail != "" {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	if global, detail := e.paymentPlanGlobalManaEffect(p, id); global {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	// tapped: sources the cost taps (their tapping abilities cannot pay);
	// kept: sources the cost sacrifices (they may tap for mana first, but
	// no alternative of theirs may consume them).
	var tapped, kept []state.ObjID
	if selfSource && (composed.Tap || composed.Untap) {
		tapped = append(tapped, id)
	}
	var parts []planCostPart
	for _, part := range composed.TapPermanent {
		if part.Dyn == "" && part.N > 0 {
			parts = append(parts, planCostPart{part: part, tap: true})
		}
	}
	for _, part := range composed.Sac {
		switch {
		case part.Announced || part.N <= 0:
		case selfSource && pay.PlanSelfCost(part, id):
			kept = append(kept, id)
		default:
			parts = append(parts, planCostPart{part: part})
		}
	}
	manaOnly := Cost{Colored: mana.Colored, Generic: mana.Generic}
	got := pay.PlanPaymentCostWithout(asPayer(e), p, decision.PlannedCast{}, manaOnly, tapped, nil, kept)
	if got.Plan == nil {
		// No plan even with every other source is a proof.
		return got
	}
	if e.planLeavesCostPayable(p, id, composed, ability, selfSource, *got.Plan, nil) {
		return got
	}
	// The plan spends a permanent the cost itself must tap or sacrifice
	// (Heap Gate's "{1}, {T}, tap an untapped Gate": the other Gate tapped
	// for the {1}; Makeshift Munitions' "{1}, Sacrifice an artifact": the
	// only other artifact a Treasure sacrificed for the {1}). Try each
	// assignment of candidates to those parts: a tap candidate withheld
	// from tapping for mana, a sacrifice candidate from being consumed.
	exhaustive, allInsufficient := true, true
	var found *PaymentPlanOutcome
	e.planEachReservation(p, id, composed, ability, parts, func(tapR, keptR []state.ObjID) bool {
		retry := pay.PlanPaymentCostWithout(asPayer(e), p, decision.PlannedCast{}, manaOnly, append(slices.Clone(tapped), tapR...), nil, append(slices.Clone(kept), keptR...))
		if retry.Plan == nil {
			if retry.Reason != "insufficient" {
				allInsufficient = false
			}
			return true
		}
		if e.planLeavesCostPayable(p, id, composed, ability, selfSource, *retry.Plan, tapR) {
			found = &retry
			return false
		}
		allInsufficient = false
		return true
	}, &exhaustive)
	hasTap, hasSac := false, false
	for _, pt := range parts {
		hasTap, hasSac = hasTap || pt.tap, hasSac || !pt.tap
	}
	switch {
	case found != nil:
		return *found
	case exhaustive && allInsufficient && !(hasTap && hasSac):
		// Every assignment was tried. (With both a tap and a sacrifice
		// part the assignments keep them distinct, which a payment need
		// not: no proof then.)
		return PaymentPlanOutcome{Reason: "insufficient", Detail: "cost:reserved_sources"}
	}
	return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:reserved_sources"}
}

// planCostPart is one fixed-count tap (tap true) or sacrifice part of a
// play's cost that a mana plan may compete for.
type planCostPart struct {
	part CostPart
	tap  bool
}

// planHybridCost plans a cost with hybrid pips ({R/G}: one mana of either
// colour, CR 107.4e) by resolving every pip to each of its colours in turn
// and planning the resolved cost: the first resolution with a witness
// wins, and the play is unpayable when every resolution is. More than
// planHybridLimit pips is unsupported.
func (e *Engine) planHybridCost(p state.PlayerID, id state.ObjID, composed Cost, ability, selfSource bool) PaymentPlanOutcome {
	pips := composed.Hybrid
	if len(pips) > planHybridLimit {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:hybrid"}
	}
	allInsufficient := true
	for mask := 0; mask < 1<<len(pips); mask++ {
		c := composed
		c.Hybrid = nil
		for i, h := range pips {
			sym := h.A
			if mask>>i&1 == 1 {
				sym = h.B
			}
			idx := state.ManaIndex(sym)
			if idx < 0 || idx >= len(c.Colored) {
				return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:hybrid"}
			}
			c.Colored[idx]++
		}
		got := e.planComposedCost(p, id, c, ability, selfSource)
		if got.Plan != nil && got.Reason == "" {
			return got
		}
		if got.Reason != "insufficient" {
			allInsufficient = false
		}
	}
	if allInsufficient {
		return PaymentPlanOutcome{Reason: "insufficient", Detail: "cost:hybrid"}
	}
	return PaymentPlanOutcome{Reason: "unsupported", Detail: "cost:hybrid"}
}

// planHybridLimit bounds planHybridCost's 2^n resolutions.
const planHybridLimit = 4

// planReservationLimit bounds the candidate assignments planComposedCost
// tries for a cost's tap parts (and its sacrifice-conflict retries). A
// larger space is not searched and its verdict is never a proof.
const planReservationLimit = 64

// planEachReservation calls try with each assignment of distinct
// candidates to parts -- untapped candidates for a tap part (never id itself
// when the cost taps it), sacrifice candidates for a sacrifice part -- in
// zone order, as combinations within a part, split into the tap and the
// sacrifice assignments. It stops when try returns false; *exhaustive is
// cleared when planReservationLimit cut the enumeration short.
func (e *Engine) planEachReservation(p state.PlayerID, id state.ObjID, cost Cost, ability bool, parts []planCostPart, try func(tapR, sacR []state.ObjID) bool, exhaustive *bool) {
	cands := make([][]state.ObjID, len(parts))
	for i, pt := range parts {
		if !pt.tap {
			cands[i] = pay.SacrificeCostCandidates(asPayer(e), p, id, pt.part, ability)
			continue
		}
		for _, oid := range pay.TapCostCandidates(asPayer(e), p, id, pt.part) {
			if cost.Tap && oid == id {
				continue
			}
			cands[i] = append(cands[i], oid)
		}
	}
	calls := 0
	chosen := make([][]state.ObjID, len(parts))
	taken := func(oid state.ObjID) bool {
		for _, c := range chosen {
			if slices.Contains(c, oid) {
				return true
			}
		}
		return false
	}
	var rec func(part, from int) bool
	rec = func(part, from int) bool {
		if part == len(parts) {
			if calls >= planReservationLimit {
				*exhaustive = false
				return false
			}
			calls++
			var tapR, sacR []state.ObjID
			for i, pt := range parts {
				if pt.tap {
					tapR = append(tapR, chosen[i]...)
				} else {
					sacR = append(sacR, chosen[i]...)
				}
			}
			return try(tapR, sacR)
		}
		if int32(len(chosen[part])) == parts[part].part.N {
			return rec(part+1, 0)
		}
		for i := from; i < len(cands[part]); i++ {
			oid := cands[part][i]
			if taken(oid) {
				continue
			}
			chosen[part] = append(chosen[part], oid)
			ok := rec(part, i+1)
			chosen[part] = chosen[part][:len(chosen[part])-1]
			if !ok {
				return false
			}
		}
		return true
	}
	rec(0, 0)
}

// planConsumed is the set of plan sources the plan uses up: every source
// (tapped) when tapped is true, else only the last-resort steps that
// sacrifice or return their source.
func planConsumed(plan decision.PaymentPlan, tapped bool) map[state.ObjID]bool {
	out := make(map[state.ObjID]bool, len(plan.Activations)) // lookup only
	for _, a := range plan.Activations {
		if tapped || (a.Consequence != nil && (a.Consequence.Sacrifice || a.Consequence.ReturnToHand)) {
			out[a.Source] = true
		}
	}
	return out
}

// planLeavesCostPayable reports whether cost's fixed-count tap and
// sacrifice parts still have enough candidates once plan's sources are
// spent. A tap part needs untapped candidates the plan did not tap; reserved
// (when non-nil) is the exact assignment the tap parts take, withheld from
// the plan. A sacrifice part needs candidates the plan did not sacrifice or
// return (a tapped source can still be sacrificed) and that no tap part
// claimed; a self-sacrifice needs the plan to leave id in play.
func (e *Engine) planLeavesCostPayable(p state.PlayerID, id state.ObjID, cost Cost, ability, selfSource bool, plan decision.PaymentPlan, reserved []state.ObjID) bool {
	tapped := planConsumed(plan, true)
	gone := planConsumed(plan, false)
	claimed := map[state.ObjID]bool{} // lookup only
	enough := func(cands []state.ObjID, n int32, spent map[state.ObjID]bool) bool {
		for _, oid := range cands {
			if n <= 0 {
				break
			}
			if spent[oid] || claimed[oid] || (cost.Tap && oid == id) {
				continue
			}
			claimed[oid] = true
			n--
		}
		return n <= 0
	}
	if reserved != nil {
		for _, oid := range reserved {
			if tapped[oid] {
				return false
			}
			claimed[oid] = true
		}
	} else {
		for _, part := range cost.TapPermanent {
			if part.Dyn == "" && part.N > 0 && !enough(pay.TapCostCandidates(asPayer(e), p, id, part), part.N, tapped) {
				return false
			}
		}
	}
	for _, part := range cost.Sac {
		if part.Announced || part.N <= 0 {
			continue
		}
		if selfSource && pay.PlanSelfCost(part, id) {
			if gone[id] {
				return false
			}
			continue
		}
		if !enough(pay.SacrificeCostCandidates(asPayer(e), p, id, part, ability), part.N, gone) {
			return false
		}
	}
	return true
}

// paymentPlanCensus is the planner census's coverage of p's mana
// abilities (paymentPlanCensusOf).
type paymentPlanCensus struct {
	// complete: the census prices every mana ability p could activate, so
	// the planner's "insufficient" is a proof.
	complete bool
	// relaxable: every ability the census misses has a relaxed
	// alternative set (paymentPlanRelaxedAlternatives), held in relaxed.
	relaxable bool
	relaxed   [][]pay.Alt
	// paid holds the relaxed alternatives of the missed abilities whose
	// cost includes mana (a filter: Heap Gate's "{1}, {T}: Add one mana of
	// any color"), each with its fee; relaxed holds the free ones.
	paid []relaxedPaid
	// gaps lists, in zone order, the sources with an ability the census
	// misses (potentialPrefixScript activates them).
	gaps []state.ObjID
}

// fees is the mana the missed paid abilities' fees total (how many other
// sources a prefix may activate to fund them).
func (c paymentPlanCensus) fees() int {
	n := 0
	for _, pd := range c.paid {
		n += int(pd.fee)
	}
	return n
}

// paymentPlanCensusOf reports whether the planner's source census prices
// every mana ability p could activate: each battlefield permanent with an
// activatable mana ability is a census unit with an alternative for every
// one of those abilities. Only then is the planner's "insufficient" a
// proof.
//
// "Activatable" is PotentialMana's own membership: the eligibility walk
// with its mana part priced against hyp, the seat's potential-mana bound
// (so a paid source such as Heap Gate's "{1}, {T}: Add one mana of any
// color" counts, since other sources could fund its fee), while every
// non-mana part of the cost is read for real. A tapped land's {T} ability
// can contribute nothing to this payment (CR 602.2b: its cost cannot be
// paid), so it never makes the census incomplete.
//
// For each ability the census misses it also builds the relaxed
// alternatives (paymentPlanRelaxedAlternatives); relaxable is false when
// one of them has none.
func (e *Engine) paymentPlanCensusOf(p state.PlayerID, hyp *state.Mana) paymentPlanCensus {
	out := paymentPlanCensus{complete: true, relaxable: true}
	units := pay.PaymentPlanQueryUnits(asPayer(e), p)
	at := make(map[state.ObjID]int, len(units)) // lookup only
	for i, u := range units {
		at[u.ID] = i
	}
	var probe *Engine
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		var abs []*cards.SA
		for _, ma := range e.appendAvailableManaAbilitiesGate(nil, nil, p, id, true) {
			if e.manaAbilityPayablePool(p, id, ma, hyp) {
				abs = append(abs, ma)
			}
		}
		if len(abs) == 0 {
			continue
		}
		var alts []pay.Alt
		if i, ok := at[id]; ok {
			alts = pay.PaymentPlanQueryAlternatives(asPayer(e), units[i])
		}
		for _, ma := range abs {
			covered := false
			for _, a := range alts {
				if pay.SameManaAbility(a.Ma, ma) {
					covered = true
					break
				}
			}
			if covered {
				continue
			}
			out.complete = false
			if !slices.Contains(out.gaps, id) {
				out.gaps = append(out.gaps, id)
			}
			if !out.relaxable {
				continue
			}
			relaxed, fee, ok := e.paymentPlanRelaxedAlternatives(p, id, ma, &probe)
			if !ok || (fee > 0 && len(out.paid) >= relaxedPaidLimit) {
				out.relaxable, out.relaxed, out.paid = false, nil, nil
				continue
			}
			if fee > 0 {
				out.paid = append(out.paid, relaxedPaid{alts: relaxed, fee: fee})
				continue
			}
			out.relaxed = append(out.relaxed, relaxed)
		}
	}
	if out.complete {
		out.relaxable, out.relaxed, out.paid, out.gaps = false, nil, nil, nil
	}
	return out
}

// paymentPlanRelaxedAlternatives is the RELAXATION of mana ability ma of
// source id for an unpayability proof: one free, normal-tier alternative per
// production the ability can make (a fixed production; one per colour of an
// Any/Combo/Chosen choice; one per colour multiset for an amount above one),
// with its cost ignored -- no fee, no tapped companion, no counter. Every
// real activation sequence maps onto a relaxed plan that produces at least
// as much, so a play no relaxed plan pays is unpayable.
//
// That holds only while the relaxed ability is usable at most once in the
// payment, which is why ok is false unless its cost taps, untaps,
// sacrifices or returns the source or it carries a once-only activation
// limit; ok is false too when the amount cannot be bounded (the engine's
// own evaluator, taken at its larger value on the live board and on the
// all-tapped probe, since a payment only taps) or the production names no
// colour. A relaxed alternative is never a witness: its Source is carried
// for ranking only and the caller discards every relaxed Plan.
//
// fee is the mana the ability's own cost spends (Heap Gate's {1}); the
// proof charges it as generic on top of the play's cost whenever the
// ability may be used (paymentPlanRelaxProof), so a filter is never counted
// as fresh mana.
func (e *Engine) paymentPlanRelaxedAlternatives(p state.PlayerID, id state.ObjID, ma *cards.SA, probe **Engine) ([]pay.Alt, int32, bool) {
	o := e.G.Obj(id)
	if o == nil || ma == nil || ma.API != "Mana" {
		return nil, 0, false
	}
	cost := e.parseCost(ma.ParamStr(cards.PKCost))
	if cost.X != 0 || cost.XMin != 0 || len(cost.Hybrid)+len(cost.Phyrexian)+len(cost.Twobrid)+len(cost.HybridPhyrexian) != 0 || cost.Snow != 0 {
		return nil, 0, false
	}
	fee := cost.Generic + cost.Colored.Total()
	once := cost.Tap || cost.Untap || strings.TrimSpace(ma.ParamStr(cards.PKActivationLimit)) == "1" ||
		strings.TrimSpace(ma.ParamStr(cards.PKGameActivationLimit)) == "1"
	for _, part := range cost.Sac {
		once = once || pay.PlanSelfCost(part, id)
	}
	for _, part := range cost.Return {
		once = once || pay.PlanSelfCost(part, id)
	}
	if !once {
		return nil, 0, false
	}
	amt := availableAmount(ma)
	if amt <= 0 {
		live, ok := e.castWindowAmount(p, id, o, ma)
		if !ok {
			return nil, 0, false
		}
		if *probe == nil {
			*probe = e.paymentPlanTappedProbe(p)
		}
		tapped, ok := (*probe).castWindowAmount(p, id, o, ma)
		if !ok {
			return nil, 0, false
		}
		amt = max(live, tapped)
	}
	mp := effects.ManaOf(ma)
	counts, any := mp.Counts, mp.CountsAny
	creature := e.IsCreature(id)
	var out []pay.Alt
	add := func(m state.Mana) {
		out = append(out, pay.Alt{Activation: decision.PaymentActivation{Source: id},
			Mana: m, Creature: creature, Ma: ma, Tier: pay.TierNormal, Flex: 1, FlexAll: 1})
	}
	if !any {
		var m state.Mana
		for i, n := range counts {
			m[state.ManaIndex(cards.ManaSymbol(i))] += n * amt
		}
		if m.Total() <= 0 {
			return nil, 0, false
		}
		add(m)
		return out, fee, true
	}
	var cols []int
	for i, n := range counts {
		if n > 0 {
			cols = append(cols, int(state.ManaIndex(cards.ManaSymbol(i))))
		}
	}
	if len(cols) == 0 || amt > 6 {
		return nil, 0, false
	}
	// Every multiset of amt units over cols (non-decreasing index runs).
	var m state.Mana
	var rec func(from int, left int32)
	rec = func(from int, left int32) {
		if left == 0 {
			add(m)
			return
		}
		for i := from; i < len(cols); i++ {
			m[cols[i]]++
			rec(i, left-1)
			m[cols[i]]--
		}
	}
	rec(0, amt)
	return out, fee, true
}

type potentialModeBaseCostCode uint16

const (
	potentialModeBaseCostEmpty potentialModeBaseCostCode = iota + 1
	potentialModeBaseCostMayplay
	potentialModeBaseCostBargained
	potentialModeBaseCostPlot
	potentialModeBaseCostFlashback
	potentialModeBaseCostBestowed
	potentialModeBaseCostKicked
	potentialModeBaseCostBuyback
	potentialModeBaseCostEntwined
	potentialModeBaseCostSurged
	potentialModeBaseCostAltCostKeyword
)

var potentialModeBaseCostCodes = state.NewStrCodes(
	state.StrEntry[potentialModeBaseCostCode]{Key: "", Val: potentialModeBaseCostEmpty},
	state.StrEntry[potentialModeBaseCostCode]{Key: "mayplay", Val: potentialModeBaseCostMayplay},
	state.StrEntry[potentialModeBaseCostCode]{Key: "bargained", Val: potentialModeBaseCostBargained},
	state.StrEntry[potentialModeBaseCostCode]{Key: "plot", Val: potentialModeBaseCostPlot},
	state.StrEntry[potentialModeBaseCostCode]{Key: "flashback", Val: potentialModeBaseCostFlashback},
	state.StrEntry[potentialModeBaseCostCode]{Key: "bestowed", Val: potentialModeBaseCostBestowed},
	state.StrEntry[potentialModeBaseCostCode]{Key: "kicked", Val: potentialModeBaseCostKicked},
	state.StrEntry[potentialModeBaseCostCode]{Key: "buyback", Val: potentialModeBaseCostBuyback},
	state.StrEntry[potentialModeBaseCostCode]{Key: "entwined", Val: potentialModeBaseCostEntwined},
	state.StrEntry[potentialModeBaseCostCode]{Key: "surged", Val: potentialModeBaseCostSurged},
	state.StrEntry[potentialModeBaseCostCode]{Key: "evoked", Val: potentialModeBaseCostAltCostKeyword},
	state.StrEntry[potentialModeBaseCostCode]{Key: "dashed", Val: potentialModeBaseCostAltCostKeyword},
	state.StrEntry[potentialModeBaseCostCode]{Key: "overloaded", Val: potentialModeBaseCostAltCostKeyword},
	state.StrEntry[potentialModeBaseCostCode]{Key: "warped", Val: potentialModeBaseCostAltCostKeyword},
	state.StrEntry[potentialModeBaseCostCode]{Key: "madness", Val: potentialModeBaseCostAltCostKeyword},
	state.StrEntry[potentialModeBaseCostCode]{Key: "miracle", Val: potentialModeBaseCostAltCostKeyword},
)
