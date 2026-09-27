package rules

// This file deliberately contains no call to emit.  Payment plans are an
// offer-time witness: execution is owned by the following ticket.

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// PaymentPlanOutcome describes a pure planner query.  Reason is deliberately
// a small machine-readable vocabulary so callers can distinguish an ordinary
// shortage from a V1 shape it must leave to manual payment.
type PaymentPlanOutcome struct {
	Plan   *decision.PaymentPlan
	Reason string // "", "unsupported", "insufficient", or "search_limit"
	Nodes  int
}

func paymentActionFor(d *decision.Decision, id string) (decision.PaymentAction, bool) {
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

// PlanCastPayment builds one V1 witness for an ordinary cast from hand.  It
// does not change the game, log, pending decision, or RNG.
func (e *Engine) PlanCastPayment(p state.PlayerID, cast decision.PlannedCast) PaymentPlanOutcome {
	if cast.Origin != "hand" || cast.Face != 0 {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	o := e.G.Obj(cast.Object)
	if o == nil || o.Zone != state.ZHand || o.Owner != p || o.Face() == nil || int(o.FaceIdx) != cast.Face {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	if !e.paymentPlanCastCandidate(p, cast.Object) || !e.paymentPlanCastShapeOK(p, cast.Object) {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	// V1 has no way to carry a target-dependent reprice or a choice made at
	// announcement.  Candidate discovery below owns timing, targets and
	// prohibitions; this method owns the exact cost/witness subset.
	cost := e.offerCostFor(p, cast.Object, e.rawBaseCost(p, cast.Object), spellScope(""))
	if !paymentPlanCostOK(cost) || !paymentPlanPoolOK(e.G.Players[p]) || e.paymentPlanManaInterference() {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	return e.planPaymentCost(p, cast, cost)
}

// paymentPlanCastCandidate deliberately delegates timing, mandatory-target
// feasibility and CantBeCast to the normal hypothetical cast walk.  The
// hypothetical aggregate only discovers candidates; exact admission still
// happens through the source-exclusive plan below.
func (e *Engine) paymentPlanCastCandidate(p state.PlayerID, id state.ObjID) bool {
	// Candidate legality is intentionally independent of present mana.  A
	// large local pool lets the shared walk retain a cast which this planner
	// will later classify as insufficient, while its non-mana gates remain
	// authoritative and live.
	hyp := state.Mana{1 << 28, 1 << 28, 1 << 28, 1 << 28, 1 << 28, 1 << 28}
	for _, opt := range e.legalActionsPriced(p, &hyp) {
		if opt.Kind == "cast" && opt.Obj == id && opt.Mode == "" && opt.AltCostIndex == 0 {
			return true
		}
	}
	return false
}

// paymentPlanCastShapeOK excludes plain casts whose announced cost or result
// depends on a choice V1 cannot bind into its witness.  The ordinary priority
// option remains available; this only withholds the additive automatic offer.
func (e *Engine) paymentPlanCastShapeOK(p state.PlayerID, id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return false
	}
	f := o.Face()
	// AlternateAdditionalCost is a mandatory election on an otherwise plain
	// cast; optional-cost statics add a second plain-looking cast route.  A
	// plan identifies neither choice, so it must not select either by accident.
	if len(altAddCostParts(f)) != 0 || len(e.optionalCostViews(e.collectCostStatics(), p, id)) != 0 {
		return false
	}
	// These keywords alter the final cost after announcement.  They are not
	// payment-plan fields, even though their ordinary base option has Mode "".
	if _, ok := f.KeywordParam("Escalate"); ok {
		return false
	}
	if _, ok := f.KeywordParam("Strive"); ok {
		return false
	}
	// The plan's exact colour spending is deliberate.  Do not offer it where
	// that spending is itself observed by the spell or a live reader.
	if faceWantsConverge(f) || faceWantsCastSpend(f) || e.triggeredConvergeReaderOut() ||
		e.triggeredCastSpendReaderOut() || e.sunburstGrantOut() {
		return false
	}
	return !e.paymentPlanHasTargetDependentModifier(p, id)
}

// paymentPlanHasTargetDependentModifier finds a live cost static that would
// apply to this ordinary spell except for ValidTarget$.  Its actual amount is
// unknowable until CR 601.2c, after the plan has been selected, so V1 leaves
// that cast to the normal target/payment flow.  Copying the parameter map is
// important: static views share compiled-card maps.
func (e *Engine) paymentPlanHasTargetDependentModifier(p state.PlayerID, id state.ObjID) bool {
	statics := e.collectCostStatics()
	for _, group := range []struct {
		mode  string
		views []staticView
	}{
		{"RaiseCost", statics.raise},
		{"ReduceCost", statics.reduce},
		{"SetCost", statics.set},
	} {
		for _, sv := range group.views {
			if strings.TrimSpace(sv.Params["ValidTarget"]) == "" {
				continue
			}
			params := make(map[string]string, len(sv.Params)-1)
			for k, v := range sv.Params {
				if k != "ValidTarget" {
					params[k] = v
				}
			}
			sv.Params = params
			if e.costStaticApplies(sv, group.mode, p, id, spellScope(""), nil, false) {
				return true
			}
		}
	}
	return false
}

// PaymentActionsForPriority builds the additive extension for one concrete
// priority decision. ask publishes its result after fixing the decision Seq;
// callers may also inspect this pure builder without changing an ask.
func (e *Engine) PaymentActionsForPriority(p state.PlayerID, seq uint64) []decision.PaymentAction {
	if e.G.Over {
		return nil
	}
	// legalActionsPriced is the authoritative candidate walk.  Its hypothetical
	// pool is only a superset gate; every admission below still has an exact
	// source-exclusive witness.
	hyp := e.PotentialMana(p)
	candidates := e.legalActionsPriced(p, &hyp)
	legacy := e.legalActions(p)
	var out []decision.PaymentAction
	for _, opt := range candidates {
		// A V1 PlannedCast records the ordinary printed-cost cast only.  An
		// AlternativeCost has no Mode marker, but AltCostIndex identifies it;
		// accepting that option would build the same PlannedCast and canonical
		// action ID as the ordinary cast.  Apart from presenting the wrong
		// cost, that produces duplicate IDs on the wire.
		if opt.Kind != "cast" || opt.Mode != "" || opt.AltCostIndex != 0 {
			continue
		}
		cast := decision.PlannedCast{Object: opt.Obj, Face: 0, Origin: "hand"}
		got := e.PlanCastPayment(p, cast)
		if got.Plan == nil {
			continue
		}
		plan := *got.Plan
		pid, err := decision.PaymentPlanID(seq, p, cast, plan)
		if err != nil {
			continue // impossible for a rules-built V1 witness; fail closed.
		}
		plan.ID = pid
		aid, err := decision.PaymentActionID(decision.PaymentPlanV1, seq, p, cast)
		if err != nil {
			continue
		}
		a := decision.PaymentAction{ID: aid, Cast: cast, Label: opt.Label, Plans: []decision.PaymentPlan{plan}}
		for i := range legacy {
			if legacy[i].Kind == "cast" && legacy[i].Obj == opt.Obj && legacy[i].Mode == "" && legacy[i].AltCostIndex == 0 {
				idx := legacy[i].Index
				a.BaseOptionIndex = &idx
				break
			}
		}
		out = append(out, a)
	}
	return out
}

// ValidateCastPayment independently re-derives the eligible sources and cost
// then proves that exactly the submitted witness pays it.  It is deliberately
// usable by the executor without trusting an offer cache or an ID.
func (e *Engine) ValidateCastPayment(p state.PlayerID, cast decision.PlannedCast, plan decision.PaymentPlan) error {
	got := e.PlanCastPayment(p, cast)
	if got.Reason == "unsupported" {
		return fmt.Errorf("payment plan unsupported")
	}
	if got.Plan == nil {
		return fmt.Errorf("payment plan %s", got.Reason)
	}
	if plan.Version != decision.PaymentPlanV1 || plan.Cost != got.Plan.Cost {
		return fmt.Errorf("payment plan cost or version changed")
	}
	if len(plan.Activations) > decision.MaxPaymentActivations {
		return fmt.Errorf("payment plan has too many activations")
	}
	// Rebuild only the explicitly named alternatives.  This is independent of
	// planner ranking: a valid non-preferred witness remains legal.
	units := e.paymentPlanManaUnits(p)
	pool := e.G.Players[p].Pool
	produced := state.Mana{}
	seen := make(map[state.ObjID]bool, len(plan.Activations))
	for _, pa := range plan.Activations {
		if seen[pa.Source] {
			return fmt.Errorf("payment source %d reused", pa.Source)
		}
		if pa.SourceZoneSeq != e.paymentSourceZoneSeq(pa.Source) {
			return fmt.Errorf("payment source %d changed zone incarnation", pa.Source)
		}
		seen[pa.Source] = true
		matched := false
		for _, u := range units {
			if u.id != pa.Source {
				continue
			}
			for _, candidate := range e.paymentPlanUnitAlternatives(u) {
				if candidate.activation.Ability == pa.Ability && candidate.activation.Produces == pa.Produces {
					matched = true
					pool = manaAdd(pool, candidate.mana)
					produced = manaAdd(produced, candidate.mana)
					break
				}
			}
		}
		if !matched {
			return fmt.Errorf("payment activation is no longer eligible")
		}
	}
	cost := e.offerCostFor(p, cast.Object, e.rawBaseCost(p, cast.Object), spellScope(""))
	payment, ok := cost.resolveManaWith(pool, state.Mana{}, [7]state.Mana{}, e.G.Players[p].Life, false, pipRider{}, nil)
	expected := paymentWitness(cost, e.G.Players[p].Pool, produced, nil, payment.pool)
	if !ok || paymentManaAmount(payment.pool) != plan.PoolAfter || expected.PoolSpend != plan.PoolSpend {
		return fmt.Errorf("payment witness does not settle")
	}
	return nil
}

func paymentPlanCostOK(c Cost) bool {
	return c.X == 0 && c.XMin == 0 && c.Snow == 0 && c.Life == 0 &&
		len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 && len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 &&
		!c.Tap && len(c.Sac) == 0 && len(c.Discard) == 0 && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.Reveal) == 0 && len(c.RevealChosen) == 0 && len(c.Behold) == 0 &&
		len(c.TapPermanent) == 0 && len(c.Blight) == 0 && !c.Forage && len(c.Draw) == 0 && len(c.Energy) == 0 &&
		len(c.LifeX) == 0 && !c.LifeHalfUp && len(c.DamageYou) == 0 && len(c.Return) == 0 &&
		len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 && len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 && len(c.Unknown) == 0
}

func paymentPlanPoolOK(p state.Player) bool {
	if p.Snow.Total() != 0 || p.PersistentMana.Total() != 0 || len(p.RestrictedMana) != 0 {
		return false
	}
	for _, m := range p.ManaUnits() {
		if m.Total() != 0 {
			return false
		}
	}
	return true
}

func paymentManaAmount(m state.Mana) decision.ManaAmount {
	var out decision.ManaAmount
	for i, n := range m {
		if n > 0 {
			out[i] = uint32(n)
		}
	}
	return out
}

func paymentCost(c Cost) decision.PaymentCost {
	return decision.PaymentCost{Generic: uint32(c.Generic), Mana: paymentManaAmount(c.Colored)}
}

type plannedManaActivation struct {
	activation decision.PaymentActivation
	mana       state.Mana
	creature   bool
	flex       int
}

func (e *Engine) planPaymentCost(p state.PlayerID, cast decision.PlannedCast, cost Cost) PaymentPlanOutcome {
	units := e.paymentPlanManaUnits(p)
	// V1 accepts only fixed production.  A permissive window unit is useful to
	// manual payment, but not proof an automatic choice will remain exact.
	choices := make([][]plannedManaActivation, len(units))
	for i, u := range units {
		choices[i] = e.paymentPlanUnitAlternatives(u)
	}
	pool := e.G.Players[p].Pool
	var best *decision.PaymentPlan
	bestRank := paymentPlanRank{}
	nodes, limited := 0, false
	var walk func(int, state.Mana, []plannedManaActivation)
	walk = func(at int, produced state.Mana, chosen []plannedManaActivation) {
		if nodes >= decision.MaxPaymentPlanSearchNodes {
			limited = true
			return
		}
		nodes++
		if paid, ok := cost.resolveManaWith(manaAdd(pool, produced), state.Mana{}, [7]state.Mana{}, e.G.Players[p].Life, false, pipRider{}, nil); ok {
			plan := paymentWitness(cost, pool, produced, chosen, paid.pool)
			r := rankPaymentPlan(plan, chosen, produced, paid.pool)
			if best == nil || r.less(bestRank) {
				best, bestRank = &plan, r
			}
			return
		}
		if at == len(choices) || len(chosen) == decision.MaxPaymentActivations {
			return
		}
		// Skip/choose preserves battlefield order and therefore produces a
		// canonical witness without enumerating activation permutations.
		walk(at+1, produced, chosen)
		for _, a := range choices[at] {
			walk(at+1, manaAdd(produced, a.mana), append(chosen, a))
		}
	}
	walk(0, state.Mana{}, nil)
	if best != nil {
		return PaymentPlanOutcome{Plan: best, Nodes: nodes, Reason: func() string {
			if limited {
				return "search_limit"
			}
			return ""
		}()}
	}
	if limited {
		return PaymentPlanOutcome{Reason: "search_limit", Nodes: nodes}
	}
	return PaymentPlanOutcome{Reason: "insufficient", Nodes: nodes}
}

// paymentPlanManaUnits extends the shared fixed-production payment census
// with only the one choice shape a V1 witness can make concrete: Produced$
// Any with a fixed amount.  The shared census must keep withholding it for
// attack/unless windows, which cannot answer a colour choice; this planner
// records its selected W/U/B/R/G output and executes that exact rewrite.
func (e *Engine) paymentPlanManaUnits(p state.PlayerID) []windowManaUnit {
	units := e.windowManaUnits(p)
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil {
			continue
		}
		idx := -1
		for i := range units {
			if units[i].id == id {
				idx = i
				break
			}
		}
		for _, ma := range e.availableManaAbilitiesForWindow(p, id, false) {
			if strings.TrimSpace(ma.Params["RestrictValid"]) != "" ||
				strings.TrimSpace(ma.Params["Produced"]) != "Any" ||
				!manaFreeCost(e.parseCost(ma.Params["Cost"])) {
				continue
			}
			amt := availableAmount(ma)
			if amt <= 0 {
				continue
			}
			counts, any := cards.ProducedCounts(ma.Params["Produced"])
			if !any {
				continue
			}
			if idx < 0 {
				units = append(units, windowManaUnit{id: id})
				idx = len(units) - 1
			}
			units[idx].alts = append(units[idx].alts, windowManaAlt{ma: ma, counts: counts, amt: amt, any: true})
		}
	}
	return units
}

// paymentPlanTapOnlyCost is the V1 source contract.  A payment witness can
// record and replay the source, ability and mana result, but it intentionally
// carries no representation for an additional activation cost.  Require an
// actual tap and reject every parsed or unknown companion cost, including
// Mill<N>, even where the ordinary manual mana window can pay it without a
// further choice.
func paymentPlanTapOnlyCost(c Cost) bool {
	return c.Tap && c.XMin == 0 && manaFreeCost(c) && castWindowOtherPartsAbsent(c)
}

func paymentPlanAltOK(a windowManaAlt) bool {
	if a.life != 0 || a.amt <= 0 || a.ma == nil || a.ma.API != "Mana" || a.any {
		return false
	}
	return a.mana().Total() > 0
}

// paymentPlanUnitAlternatives expands one physical source into the exact
// one-tap outcomes V1 can execute.  A fixed Produced$ Any amount is finite:
// choose one of WUBRG now, record it in Produces, then run the ordinary mana
// ability with Produced rewritten to that selected colour.  Combo/Chosen and
// every other open production remain manual because they need an allocation
// or a source-state read not represented by the witness.
func (e *Engine) paymentPlanUnitAlternatives(u windowManaUnit) []plannedManaActivation {
	var out []plannedManaActivation
	for _, alt := range u.alts {
		if !paymentPlanTapOnlyCost(e.parseCost(alt.ma.Params["Cost"])) {
			continue
		}
		ab, ok := e.paymentAbility(u.id, alt.ma)
		if !ok {
			continue
		}
		if paymentPlanAltOK(alt) {
			m := alt.mana()
			out = append(out, plannedManaActivation{activation: decision.PaymentActivation{
				Source: u.id, SourceZoneSeq: e.paymentSourceZoneSeq(u.id), Ability: ab, Produces: paymentManaAmount(m)},
				mana: m, creature: e.IsCreature(u.id)})
			continue
		}
		if strings.TrimSpace(alt.ma.Params["Produced"]) != "Any" || !alt.any || alt.amt <= 0 {
			continue
		}
		for i := 0; i < 5; i++ {
			var m state.Mana
			m[i] = alt.amt
			out = append(out, plannedManaActivation{activation: decision.PaymentActivation{
				Source: u.id, SourceZoneSeq: e.paymentSourceZoneSeq(u.id), Ability: ab, Produces: paymentManaAmount(m)},
				mana: m, creature: e.IsCreature(u.id)})
		}
	}
	// Preserve flexible sources: rank each selected source by every eligible
	// outcome it could have supplied, rather than by only the outcome the
	// search happened to choose.
	for i := range out {
		out[i].flex = len(out)
	}
	return out
}

func (e *Engine) paymentAbility(id state.ObjID, ma *cards.SA) (decision.PaymentAbility, bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || ma == nil {
		return decision.PaymentAbility{}, false
	}
	if strings.HasPrefix(ma.Line, "intrinsic:") {
		return decision.PaymentAbility{Kind: decision.PaymentAbilityIntrinsic, Intrinsic: "basic_land"}, true
	}
	for i, a := range o.Face().Abilities {
		if a == ma {
			return decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted, Face: uint32(o.FaceIdx), Index: uint32(i)}, true
		}
	}
	return decision.PaymentAbility{}, false // grants, merged and foreign abilities are V1 exclusions.
}

// paymentSourceZoneSeq is the existing log sequence of this object's current
// zone entry. Genesis objects have no entry event and use the contract's zero
// sentinel. It deliberately scans backwards so a later incarnation cannot be
// authorized by a witness made for an earlier visit to the battlefield.
func (e *Engine) paymentSourceZoneSeq(id state.ObjID) uint64 {
	o := e.G.Obj(id)
	if o == nil {
		return decision.GenesisZoneSeq
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Obj != id {
			continue
		}
		if ev.Kind == events.MoveZone && ev.To == o.Zone {
			return ev.Seq
		}
		if ev.Kind == events.TokenCreate && o.Zone == state.ZBattlefield {
			return ev.Seq
		}
	}
	return decision.GenesisZoneSeq
}

// paymentPlanManaInterference declines the whole V1 plan when the battlefield
// has an effect that can alter a selected tap or mana event.  The ordinary
// source walk remains available for manual play; this deliberately conservative
// gate prevents a nominally bare activation from becoming a false guarantee.
func (e *Engine) paymentPlanManaInterference() bool {
	// Effect-created replacements are stored in the continuous registry, not
	// on a face. They participate in the same ProduceMana matcher as printed
	// lines, so a plan must decline them too rather than publishing a witness
	// whose predicted output differs at execution/replay.
	for _, ce := range e.active() {
		if ce.ReplacementEvent == "ProduceMana" {
			return true
		}
	}
	for _, p := range e.G.Players {
		for _, id := range e.G.Zone(state.ZBattlefield, p.ID) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			for _, t := range o.Face().Triggers {
				if t.Mode == "Taps" || t.Mode == "TapsForMana" {
					return true
				}
			}
			for _, r := range o.Face().Repls {
				if strings.Contains(strings.ToLower(r.Event), "mana") || strings.Contains(strings.ToLower(r.Event), "tap") {
					return true
				}
			}
		}
	}
	return false
}

type paymentPlanRank struct {
	sources, creatures int
	surplus            int32
	flex               int
	text               string
}

func (r paymentPlanRank) less(o paymentPlanRank) bool {
	if r.sources != o.sources {
		return r.sources < o.sources
	}
	if r.creatures != o.creatures {
		return r.creatures < o.creatures
	}
	if r.surplus != o.surplus {
		return r.surplus < o.surplus
	}
	if r.flex != o.flex {
		return r.flex < o.flex
	}
	return r.text < o.text
}
func rankPaymentPlan(p decision.PaymentPlan, as []plannedManaActivation, produced, after state.Mana) paymentPlanRank {
	r := paymentPlanRank{sources: len(as), text: fmt.Sprint(p.Activations)}
	for _, a := range as {
		if a.creature {
			r.creatures++
		}
		r.flex += a.flex
	}
	r.surplus = after.Total()
	return r
}
func paymentWitness(c Cost, initial, produced state.Mana, as []plannedManaActivation, after state.Mana) decision.PaymentPlan {
	acts := make([]decision.PaymentActivation, len(as))
	for i := range as {
		acts[i] = as[i].activation
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
	return decision.PaymentPlan{Version: decision.PaymentPlanV1, Cost: paymentCost(c), Activations: acts, PoolSpend: spend, PoolAfter: paymentManaAmount(after)}
}
