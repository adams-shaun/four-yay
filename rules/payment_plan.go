package rules

// This file deliberately contains no call to emit.  Payment plans are an
// offer-time witness: execution is owned by the following ticket.

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
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
	Detail string // deterministic unsupported/source diagnostic
	Nodes  int
}

type paymentAbilityTier uint8

const (
	paymentTierDeferred paymentAbilityTier = iota
	paymentTierLastResort
	paymentTierNormal
)

type paymentConsequence struct {
	sacrifice    bool
	life         uint32
	damage       uint32
	noUntap      bool
	returnToHand bool
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
	if detail := e.paymentPlanCastShapeDetail(p, cast.Object); detail != "" {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	if !e.paymentPlanCastCandidate(p, cast.Object) {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	// V1 has no way to carry a target-dependent reprice or a choice made at
	// announcement. Candidate discovery below owns timing, targets and
	// prohibitions; this method owns the exact cost/witness subset.
	base := e.rawBaseCost(p, cast.Object)
	base = withSpellAbilityExtras(o.Face(), base)
	cost := e.offerCostFor(p, cast.Object, base, spellScope(""))
	if detail := paymentPlanCostDetail(cost); detail != "" {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	if !paymentPlanPoolOK(e.G.Players[p]) {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	if global, detail := e.paymentPlanGlobalManaEffect(p, cast.Object); global {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
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
	return e.paymentPlanCastShapeDetail(p, id) == ""
}

func (e *Engine) paymentPlanCastShapeDetail(p state.PlayerID, id state.ObjID) string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return "shape:additional_cost"
	}
	f := o.Face()
	if len(altAddCostParts(f)) != 0 || len(e.optionalCostViews(e.collectCostStatics(), p, id)) != 0 {
		return "shape:optional_cost"
	}
	// Cost$ on the spell ability and the supported cost-static extra are both
	// additional costs; neither is represented by a mana-only plan witness.
	mods := e.costModifiersWithTargets(p, id, spellScope(""), nil, false)
	spellCost := Cost{}
	if sa := f.SpellAbility(); sa != nil {
		spellCost = e.parseCost(sa.Params["Cost"])
	}
	if paymentPlanCostDetail(spellCost) != "" || paymentPlanCostDetail(withSpellAbilityExtras(f, Cost{})) != "" || paymentPlanCostDetail(mods.extra) != "" {
		return "shape:additional_cost"
	}
	if e.hasCastConvoke(id) || e.hasCastImprovise(id) || e.HasKeyword(id, "Delve") {
		return "shape:contribution"
	}
	if f.HasKeyword("Gift") {
		return "shape:gift"
	}
	for name := range f.SVars {
		if _, present, _ := modeCost(f, name); present || modeCostUnparseable(f, name) {
			return "shape:modal_cost"
		}
	}
	for _, key := range []string{"Replicate", "Multikicker", "Squad"} {
		if _, ok := f.KeywordParam(key); ok {
			return "shape:optional_cost"
		}
	}
	if faceReadsManaSpent(f) || faceWantsConverge(f) || faceWantsCastSpend(f) ||
		e.triggeredConvergeReaderOut() || e.triggeredCastSpendReaderOut() || e.paymentPlanSunburstGrantOut() {
		return "shape:mana_spent_reader"
	}
	if e.paymentPlanHasTargetDependentModifier(p, id) {
		return "shape:target_dependent_cost"
	}
	if _, ok := f.KeywordParam("Escalate"); ok {
		return "shape:optional_cost"
	}
	if _, ok := f.KeywordParam("Strive"); ok {
		return "shape:optional_cost"
	}
	return ""
}

func faceReadsManaSpent(f *cards.Face) bool {
	for _, needle := range []string{"ConditionManaSpent$", "Count$Adamant", "Count$EachSpentToCast", "Count$TotalManaSpent", "ManaSpentBy"} {
		if f.Mentions(needle) {
			return true
		}
	}
	return false
}

func paymentPlanCostDetail(c Cost) string {
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
	case !paymentPlanCostOK(c):
		return "cost:other"
	default:
		return ""
	}
}

// paymentPlanHasTargetDependentModifier finds a live cost static that would
// apply to this ordinary spell except for ValidTarget$.  Its actual amount is
// unknowable until CR 601.2c, after the plan has been selected, so V1 leaves
// that cast to the normal target/payment flow.  A spell that announces no
// target cannot meet any ValidTarget$ clause, so it is never taxed and is not
// declined (paymentPlanSpellTargets).  Copying the parameter map is
// important: static views share compiled-card maps.
func (e *Engine) paymentPlanHasTargetDependentModifier(p state.PlayerID, id state.ObjID) bool {
	if o := e.G.Obj(id); o == nil || !paymentPlanSpellTargets(o.Face()) {
		return false
	}
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
		step, ok := e.paymentPlanStepAlternative(units, pa)
		if !ok {
			return fmt.Errorf("payment activation is no longer eligible")
		}
		pool = manaAdd(pool, step.mana)
		produced = manaAdd(produced, step.mana)
	}
	cost := e.offerCostFor(p, cast.Object, withSpellAbilityExtras(e.G.Obj(cast.Object).Face(), e.rawBaseCost(p, cast.Object)), spellScope(""))
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
		len(c.Exile) == 0 && len(c.Reveal) == 0 && len(c.RevealOrChoose) == 0 && len(c.RevealChosen) == 0 && len(c.Behold) == 0 &&
		len(c.TapPermanent) == 0 && len(c.Blight) == 0 && !c.Forage && len(c.Draw) == 0 && len(c.Energy) == 0 &&
		len(c.LifeX) == 0 && !c.LifeHalfUp && len(c.DamageYou) == 0 && len(c.Return) == 0 &&
		len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 && len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 && len(c.Unknown) == 0 && len(c.Exert) == 0
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
	// ma is the exact ability this alternative activates. It is not part of
	// the witness: every intrinsic ability shares one PaymentAbility
	// identity ({intrinsic, basic_land}), so on a source with several (a
	// dual land's {U} and {R}) the identity alone names no single ability.
	// A witness step's Ability AND Produces together select exactly one
	// alternative (paymentPlanStepAlternative), and execution activates
	// that alternative's own ability.
	ma *cards.SA
	// exec is the exact ability to activate to realise this alternative: the
	// ORIGINAL for fixed production, and a withProduced copy of it for a
	// choice-shaped production (Any/Combo/Chosen/ColorIdentity) whose selected
	// colour is recorded in the witness's Produces. The executor resolves
	// step.exec and hands step.ma to the ordinary mana path as the original
	// (for activation limits and replay identity), so no colour prompt is ever
	// posed at execution.
	exec        *cards.SA
	tier        paymentAbilityTier
	consequence paymentConsequence
}

func (e *Engine) planPaymentCost(p state.PlayerID, cast decision.PlannedCast, cost Cost) PaymentPlanOutcome {
	units := e.paymentPlanManaUnits(p)
	// V1 accepts only fixed production.  A permissive window unit is useful to
	// manual payment, but not proof an automatic choice will remain exact.
	choices := make([][]plannedManaActivation, len(units))
	firstSourceDetail := ""
	for i, u := range units {
		choices[i] = e.paymentPlanUnitAlternatives(u)
		if firstSourceDetail == "" {
			for _, alt := range u.alts {
				o := e.G.Obj(u.id)
				p := state.PlayerID(0)
				if o != nil {
					p = o.Controller
				}
				_, _, detail := e.paymentPlanAbilityTier(p, u.id, alt.ma)
				if detail != "" {
					firstSourceDetail = detail
					break
				}
			}
		}
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
	return PaymentPlanOutcome{Reason: "insufficient", Detail: firstSourceDetail, Nodes: nodes}
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
			raw := strings.TrimSpace(ma.Params["Produced"])
			amt := availableAmount(ma)
			if amt <= 0 {
				continue
			}
			counts, any := cards.ProducedCounts(ma.Params["Produced"])
			if !any {
				continue
			}
			// The shared census keeps only deterministic production; extend it
			// with the finite choice shapes V1 can make concrete: Produced$ Any
			// (one alternative per colour, any literal amount) and an amount-1
			// Combo/Chosen/ColorIdentity (one alternative per producible
			// colour). Combo Any and every allocation (amount > 1) stay deferred.
			if raw != "Any" && !paymentPlanChoiceShape(raw) {
				continue
			}
			if raw != "Any" && amt != 1 {
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

// paymentPlanAbilityTier is the single source-shape authority for automatic
// payment. It is intentionally closed-world: new Forge parameters require an
// explicit review before the planner can rely on them. It folds the ability's
// own shape (paymentPlanAbilityShapeTier) with the triggers and replacements
// that can act on this source's tap or mana (paymentPlanSourceInterference):
// a deferring interference defers the source, and the source's own
// fully determined consequence (City of Brass's damage, Mana Vault's
// doesn't-untap) makes an otherwise normal source last resort.
func (e *Engine) paymentPlanAbilityTier(p state.PlayerID, id state.ObjID, ma *cards.SA) (paymentAbilityTier, paymentConsequence, string) {
	tier, c, detail := e.paymentPlanAbilityShapeTier(p, id, ma)
	if tier == paymentTierDeferred {
		return tier, c, detail
	}
	switch it, ic, idetail := e.paymentPlanSourceInterference(id, ma); it {
	case paymentTierDeferred:
		return it, ic, idetail
	case paymentTierLastResort:
		c.sacrifice = c.sacrifice || ic.sacrifice
		c.life += ic.life
		c.damage += ic.damage
		c.noUntap = c.noUntap || ic.noUntap
		c.returnToHand = c.returnToHand || ic.returnToHand
		return paymentTierLastResort, c, "source:last_resort"
	}
	return tier, c, detail
}

// paymentPlanAbilityShapeTier classifies the ability's own cost, production,
// parameters and SubAbility$ chain.
func (e *Engine) paymentPlanAbilityShapeTier(p state.PlayerID, id state.ObjID, ma *cards.SA) (paymentAbilityTier, paymentConsequence, string) {
	deferred := func(detail string) (paymentAbilityTier, paymentConsequence, string) {
		return paymentTierDeferred, paymentConsequence{}, detail
	}
	if ma == nil || ma.API != "Mana" {
		return deferred("source:special_production")
	}
	// The key loop carries the key NAMES only (never a Params value) into
	// the prefix checks below; slices.Sorted(maps.Keys) keeps it a plain
	// string-slice walk rather than a range the param census would have to
	// classify.
	keys := slices.Sorted(maps.Keys(ma.Params))
	for _, key := range keys {
		if strings.HasPrefix(key, "Condition") {
			return deferred("source:conditional")
		}
		if strings.Contains(strings.ToLower(key), "target") || key == "ValidTgts" || key == "ValidTarget" {
			return deferred("source:target")
		}
		if !paymentPlanKnownManaParam(key) {
			return deferred("source:param:" + key)
		}
	}
	if strings.TrimSpace(ma.Params["RestrictValid"]) != "" {
		return deferred("source:special_production")
	}
	if paymentPlanHasSpecialProductionParam(ma) {
		return deferred("source:special_production")
	}
	cost := e.parseCost(ma.Params["Cost"])
	if cost.XMin != 0 || cost.X != 0 || cost.Generic != 0 || cost.Colored.Total() != 0 || len(cost.Discard)+len(cost.SubCounter)+len(cost.Exile)+len(cost.ExileFromTop)+len(cost.TapPermanent)+len(cost.Energy)+len(cost.LifeX) != 0 {
		return deferred("source:last_resort")
	}
	if strings.TrimSpace(ma.Params["SubAbility"]) != "" {
		if e.paymentPlanRiderHasTarget(id, ma) {
			return deferred("source:target")
		}
		if !paymentPlanTapOnlyCost(cost) {
			return deferred("source:last_resort")
		}
		if d, ok := e.paymentPlanDamageRider(id, ma); ok {
			return paymentTierLastResort, paymentConsequence{damage: d}, "source:last_resort"
		}
		if e.paymentPlanParadiseRider(id, ma) {
			return paymentTierLastResort, paymentConsequence{returnToHand: true}, "source:last_resort"
		}
		return deferred("source:rider")
	}
	if cost.Sac != nil || cost.Life != 0 || cost.Return != nil {
		c := paymentConsequence{}
		if len(cost.Sac) > 0 && len(cost.Sac) == 1 && paymentPlanSelfCost(cost.Sac[0], id) {
			c.sacrifice = true
		} else if len(cost.Sac) > 0 {
			return deferred("source:last_resort")
		}
		if cost.Life > 0 {
			c.life = uint32(cost.Life)
		}
		if len(cost.Return) > 0 && len(cost.Return) == 1 && paymentPlanSelfCost(cost.Return[0], id) {
			c.returnToHand = true
		} else if len(cost.Return) > 0 {
			return deferred("source:last_resort")
		}
		return paymentTierLastResort, c, "source:last_resort"
	}
	if !paymentPlanTapOnlyCost(cost) {
		return deferred("source:last_resort")
	}
	return paymentTierNormal, paymentConsequence{}, ""
}

// paymentPlanHasSpecialProductionParam reports whether the ability's own head
// carries a production special-effect parameter (spec 3.2: TriggersWhenSpent$,
// AddsCounters$, the AddsKeywords* family, AddsNoCounter$, PersistentMana$,
// UnlessCost$, Defined$). Such production does something beyond adding plain
// mana to the pool, so the ability is deferred.
func paymentPlanHasSpecialProductionParam(ma *cards.SA) bool {
	for _, key := range slices.Sorted(maps.Keys(ma.Params)) {
		if key == "TriggersWhenSpent" || key == "AddsCounters" || key == "AddsNoCounter" ||
			key == "PersistentMana" || key == "UnlessCost" || key == "Defined" ||
			strings.HasPrefix(key, "AddsKeywords") {
			return true
		}
	}
	return false
}

func paymentPlanKnownManaParam(key string) bool {
	if strings.HasPrefix(key, "AddsKeywords") {
		return true
	}
	switch key {
	case "API", "Cost", "Produced", "Amount", "SubAbility", "SpellDescription", "StackDescription", "AILogic", "PrecostDesc",
		"Activation", "Activator", "ActivationPhases", "PlayerTurn", "OpponentTurn", "ActivationFirstCombat", "ActivationAfterBlockers",
		"IsPresent", "PresentCompare", "CheckSVar", "SVarCompare", "ActivationLimit", "GameActivationLimit", "InstantSpeed",
		"RestrictValid", "TriggersWhenSpent", "AddsCounters", "AddsKeywords", "AddsKeywordsAll", "AddsNoCounter", "PersistentMana", "UnlessCost", "Defined":
		return true
	default:
		return false
	}
}

func paymentPlanSelfCost(part CostPart, id state.ObjID) bool {
	s := strings.ToLower(strings.TrimSpace(part.Spec))
	return part.N == 1 && (s == "cardname" || s == "this token" || s == "cardname/self" || s == "self")
}

func (e *Engine) paymentPlanDamageRider(id state.ObjID, mana *cards.SA) (uint32, bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, false
	}
	return paymentPlanDamageBody(cards.ResolveSVar(o.Face().SVars, strings.TrimSpace(mana.Params["SubAbility"])))
}

// paymentPlanDamageBody reports N when rider is exactly `DealDamage |
// Defined$ You | NumDmg$ <literal N>` with no further parameter or sub.
func paymentPlanDamageBody(rider *cards.SA) (uint32, bool) {
	if rider == nil || rider.API != "DealDamage" || strings.TrimSpace(rider.Params["Defined"]) != "You" || strings.TrimSpace(rider.Params["SubAbility"]) != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(rider.Params["NumDmg"]), 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	for _, k := range slices.Sorted(maps.Keys(rider.Params)) {
		if k != "API" && k != "Defined" && k != "NumDmg" && k != "SpellDescription" && k != "StackDescription" {
			return 0, false
		}
	}
	return uint32(n), true
}

func (e *Engine) paymentPlanRiderHasTarget(id state.ObjID, mana *cards.SA) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return true
	}
	rider := cards.ResolveSVar(o.Face().SVars, strings.TrimSpace(mana.Params["SubAbility"]))
	if rider == nil {
		return false
	}
	for _, key := range slices.Sorted(maps.Keys(rider.Params)) {
		if strings.Contains(strings.ToLower(key), "target") || key == "ValidTgts" || key == "ValidTarget" {
			return true
		}
	}
	return false
}

func (e *Engine) paymentPlanParadiseRider(id state.ObjID, mana *cards.SA) bool {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return false
	}
	rider := cards.ResolveSVar(o.Face().SVars, strings.TrimSpace(mana.Params["SubAbility"]))
	if rider == nil || rider.API != "Pump" || rider.Params["Defined"] != "Self" {
		return false
	}
	for _, key := range slices.Sorted(maps.Keys(rider.Params)) {
		if key != "API" && key != "Defined" && key != "KW" && key != "Duration" && key != "SpellDescription" && key != "StackDescription" {
			return false
		}
	}
	text := strings.ToLower(strings.Join([]string{rider.Params["KW"], rider.Params["SpellDescription"], rider.Params["StackDescription"]}, " "))
	return strings.Contains(text, "hidden") && strings.Contains(text, "return")
}

// paymentPlanUnitAlternatives expands one physical source into the exact
// one-tap outcomes V1 can execute.  A fixed production is one alternative.
// A finite choice -- Produced$ Any (choose one of WUBRG, any literal
// amount), an amount-1 Combo (one alternative per listed/resolved colour),
// a recorded Chosen, or the commander colour identity -- is one alternative
// per producible colour: the planner records the selected colour in
// Produces AND carries a withProduced copy of the ability as exec, so
// execution runs the ordinary mana path with no colour prompt.  An
// allocation (amount > 1), Combo Any, and every open production remain
// manual because they need a shape or source-state read the witness cannot
// represent.
func (e *Engine) paymentPlanUnitAlternatives(u windowManaUnit) []plannedManaActivation {
	var out []plannedManaActivation
	for _, alt := range u.alts {
		payer := state.PlayerID(0)
		if source := e.G.Obj(u.id); source != nil {
			payer = source.Controller
		}
		tier, consequence, _ := e.paymentPlanAbilityTier(payer, u.id, alt.ma)
		if tier != paymentTierNormal {
			continue
		}
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
				mana: m, creature: e.IsCreature(u.id), ma: alt.ma, exec: alt.ma, tier: tier, consequence: consequence})
			continue
		}
		if !alt.any || alt.amt <= 0 {
			continue
		}
		for _, col := range e.paymentPlanChoiceColours(u.id, alt.ma) {
			i := strings.IndexByte("WUBRG", col[0])
			if i < 0 {
				continue
			}
			var m state.Mana
			m[i] = alt.amt
			out = append(out, plannedManaActivation{activation: decision.PaymentActivation{
				Source: u.id, SourceZoneSeq: e.paymentSourceZoneSeq(u.id), Ability: ab, Produces: paymentManaAmount(m)},
				mana: m, creature: e.IsCreature(u.id), ma: alt.ma, exec: withProduced(alt.ma, alt.ma, col), tier: tier, consequence: consequence})
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

// paymentPlanChoiceShape reports whether a Produced$ value is one of the
// finite choice shapes V1 can plan: the literal Any, a bare Chosen/
// ChosenColor, or a Combo list. It is deliberately about the SHAPE only;
// whether the choice resolves to any colour (an unrecorded Chosen, a Combo
// naming no plain colour, an empty commander identity) is
// paymentPlanChoiceColours' fail-closed answer, not this predicate's.
func paymentPlanChoiceShape(raw string) bool {
	switch raw {
	case "Chosen", "ChosenColor", "ComboChosen":
		return true
	}
	return strings.HasPrefix(raw, "Combo ")
}

// paymentPlanChoiceColours returns the concrete colours a choice-shaped
// Produced$ resolves to for source id, or nil when the production is fixed
// or cannot be resolved. The order is deterministic: WUBRG for Produced$ Any
// and a commander identity is already WUBRG (commanderIdentityColours), and
// the ability's own token order for a Combo -- the same order
// manaAbilityComboColours and askManaColor use. A bare Chosen/ChosenColor
// with nothing recorded, a Combo whose tokens name no plain colour, and an
// empty commander identity all yield nil: V1 fails closed rather than
// inventing a colour.
func (e *Engine) paymentPlanChoiceColours(id state.ObjID, ma *cards.SA) []string {
	raw := strings.TrimSpace(ma.Params["Produced"])
	switch raw {
	case "Any":
		return []string{"W", "U", "B", "R", "G"}
	case "Chosen", "ChosenColor", "ComboChosen":
		if col := e.chosenProducedColour(id); col != "" {
			return []string{col}
		}
		return nil
	case "ColorIdentity":
		return e.commanderIdentityColours(e.paymentPlanController(id))
	}
	// Reuse the manual wheel's own flattener: it substitutes a recorded
	// Chosen tail and dedups a recorded colour equal to a fixed token, so the
	// plan and the wheel cannot disagree about a Combo that resolves cleanly.
	if cols, ok := manaAbilityComboColours(ma, e.chosenProducedColour(id)); ok {
		return cols
	}
	if !strings.HasPrefix(raw, "Combo ") {
		return nil
	}
	// A Combo still naming a token manaAbilityComboColours cannot flatten (a
	// Chosen with nothing recorded, a ColorIdentity) has no single colour
	// list; walk its tokens so a fixed token ("Combo U Chosen" with nothing
	// recorded) still yields its own colour, and fail closed on any token this
	// engine cannot resolve ("Combo Any", "Special ...").
	var cols []string
	for _, tok := range strings.Fields(raw) {
		switch {
		case tok == "Combo":
		case tok == "Chosen" || tok == "ChosenColor":
			if col := e.chosenProducedColour(id); col != "" {
				cols = appendColourOnce(cols, col)
			}
		case tok == "ColorIdentity":
			for _, col := range e.commanderIdentityColours(e.paymentPlanController(id)) {
				cols = appendColourOnce(cols, col)
			}
		case len(tok) == 1 && strings.ContainsRune("WUBRG", rune(tok[0])):
			cols = appendColourOnce(cols, tok)
		default:
			return nil
		}
	}
	if len(cols) == 0 {
		return nil
	}
	return cols
}

// appendColourOnce appends col unless it is already present, preserving the
// first-seen order so an alternative list stays deterministic.
func appendColourOnce(cols []string, col string) []string {
	for _, c := range cols {
		if c == col {
			return cols
		}
	}
	return append(cols, col)
}

// paymentPlanController is the controller of source id, or seat 0 for a
// source that is no longer on the battlefield (the helper is only reached
// with a live battlefield source, but a nil read must never panic).
func (e *Engine) paymentPlanController(id state.ObjID) state.PlayerID {
	if o := e.G.Obj(id); o != nil {
		return o.Controller
	}
	return 0
}

// paymentPlanStepAlternative resolves one witness step to the exact
// alternative the planner offers for its source: the one whose ability
// identity AND production both equal the step's. Identity alone is not
// enough -- every intrinsic ability shares {intrinsic, basic_land}, so a
// Volcanic Island's {U} and {R} abilities differ only in Produces. The pair
// is: a printed identity names one ability by index (a Produced$ Any
// ability's alternatives then differ by Produces), and a source's intrinsic
// abilities are de-duplicated by production (cards.ApplyIntrinsics and the
// granted basic-land-type walk). Validation (ValidateCastPayment,
// validatePendingPaymentPlan) and execution (executePlannedManaActivation)
// all resolve a step here, so the alternative validation priced is the one
// execution activates; none of them may pair one alternative's identity with
// another alternative's production.
func (e *Engine) paymentPlanStepAlternative(units []windowManaUnit, pa decision.PaymentActivation) (plannedManaActivation, bool) {
	for _, u := range units {
		if u.id != pa.Source {
			continue
		}
		for _, candidate := range e.paymentPlanUnitAlternatives(u) {
			if candidate.ma != nil && candidate.activation.Ability == pa.Ability && candidate.activation.Produces == pa.Produces {
				return candidate, true
			}
		}
	}
	return plannedManaActivation{}, false
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

// paymentPlanManaInterference is the zero-argument global gate the planned
// executor re-runs before each later step: whether a mana effect whose scope
// the planner cannot prove (paymentPlanGlobalManaEffect) reaches the acting
// payer -- the cast in progress when there is one, else any alive player.
// Everything the planner CAN scope -- a trigger or replacement that can match
// one source's tap or production -- is a per-source tier question
// (paymentPlanSourceInterference), which the executor re-reads through
// paymentPlanUnitAlternatives.
func (e *Engine) paymentPlanManaInterference() bool {
	if pc := e.cast; pc != nil {
		global, _ := e.paymentPlanGlobalManaEffect(pc.player, pc.card)
		return global
	}
	for _, p := range e.G.AliveFrom(0) {
		if global, _ := e.paymentPlanGlobalManaEffect(p, 0); global {
			return true
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
