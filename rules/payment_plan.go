package rules

// This file deliberately contains no call to emit.  Payment plans are an
// offer-time witness; the planned executor lives in cast.go.

import (
	"cmp"
	"fmt"
	"maps"
	"math/bits"
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

// PlanCastPayment builds one V1 witness for an ordinary cast from the hand
// or, in a Commander game, from the command zone (the plain taxed CR 903.8
// cast; alternate-cost command-zone casts stay manual like every other Mode
// cast).  It does not change the game, log, pending decision, or RNG.  It runs its own
// candidate walk for this one cast (ValidateCastPayment, the audit tools and
// tests call it for a single cast); the offer builder, which plans every
// candidate of one decision, shares one walk and one cost-static collection
// across them through planCastPaymentChecked.
func (e *Engine) PlanCastPayment(p state.PlayerID, cast decision.PlannedCast) PaymentPlanOutcome {
	statics := costStaticSource{e: e}
	candidates := paymentCastCandidates{e: e, p: p}
	return e.planCastPaymentChecked(p, cast, &statics, &candidates)
}

// CastPaymentCost is the mana cost a plain cast of the hand card id by p
// would lock in now (CR 601.2f): the printed cost with its spell-ability
// extras, re-priced by every live cost static exactly as the planner prices
// it (planCastPaymentChecked). ok is false for an object that is not a card
// in p's hand. A pure read, for diagnostics (internal/paymirror) that need
// to show a cast's price moved since a plan was witnessed.
func (e *Engine) CastPaymentCost(p state.PlayerID, id state.ObjID) (cost decision.PaymentCost, ok bool) {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Owner != p || o.Face() == nil {
		return decision.PaymentCost{}, false
	}
	base := withSpellAbilityExtras(o.Face(), e.rawBaseCost(p, id))
	return paymentCost(e.offerCostFor(p, id, base, spellScope(""))), true
}

// planCastPaymentChecked is PlanCastPayment over caller-owned per-build
// inputs: statics is the build's one lazy cost-static collection and
// candidates its one lazy hypothetical candidate walk. Both are pure reads of
// the unchanged state, so sharing them across the candidates of one build
// answers every check exactly as a fresh collection and walk would. The
// check order, and therefore every Reason and Detail, is PlanCastPayment's.
func (e *Engine) planCastPaymentChecked(p state.PlayerID, cast decision.PlannedCast, statics *costStaticSource, candidates *paymentCastCandidates) PaymentPlanOutcome {
	if cast.Face != 0 {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	// V1 plans exactly two ordinary-cast origins: the hand (the original
	// shape) and the command zone (the CR 903.8 plain taxed cast; alternate
	// command-zone casts -- dash, evoke, bestow, ... -- carry a Mode and are
	// withheld, as everywhere else).  Any other origin fails closed, and the
	// object must actually sit in its origin's zone: a mislabeled origin
	// would compose a cost (and, from ZCommand, a commander tax) that
	// beginCast would never charge.
	originZone := state.ZHand
	switch cast.Origin {
	case "hand":
	case "command_zone":
		originZone = state.ZCommand
	default:
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	o := e.G.Obj(cast.Object)
	if o == nil || o.Zone != originZone || o.Owner != p || o.Face() == nil || int(o.FaceIdx) != cast.Face {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	if detail := e.paymentPlanCastShapeDetailUsing(statics.get(), p, cast.Object); detail != "" {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	if !candidates.has(cast.Object) {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	// V1 has no way to carry a target-dependent reprice or a choice made at
	// announcement. Candidate discovery below owns timing, targets and
	// prohibitions; this method owns the exact cost/witness subset.
	base := e.rawBaseCost(p, cast.Object)
	base = withSpellAbilityExtras(o.Face(), base)
	cost := e.offerCostForUsing(statics.get(), p, cast.Object, base, spellScope(""))
	if detail := paymentPlanNonManaAdmissible(cost); detail != "" {
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
	c := paymentCastCandidates{e: e, p: p}
	return c.has(id)
}

// paymentCastCandidates is one player's plain-cast candidate set from ONE
// hypothetical legal-action walk, run on the first membership query.
// Candidate legality is intentionally independent of present mana: a large
// local pool lets the shared walk retain a cast which the planner will later
// classify as insufficient, while its non-mana gates (timing, mandatory
// targets, CantBeCast) remain authoritative and live. A member is a
// "cast" option with no Mode and no AltCostIndex -- exactly the option
// paymentPlanCastCandidate has always matched -- so membership answers that
// per-cast walk for every candidate of a build at the cost of one walk.
//
// priced marks a set whose every query names a plain cast the offer
// builder's OWN priced walk (legalActionsPriced at PotentialMana) listed.
// That walk and the huge-pool walk run the identical body; the one place
// the pool enters a plain cast's admission is offerCastable's mana
// feasibility, which only ever asks whether the pool COVERS a composed
// cost, and the huge pool covers every cost the PotentialMana bound does.
// (The two retry arms behind a failed first pass differ only for a
// ValidTarget$ cost static or an announced Sac<X>, and planCastPaymentChecked
// has already declined both shapes -- shape:target_dependent_cost,
// shape:additional_cost -- before it asks.) So membership is certain and
// the huge walk is skipped; pricedCandidatesVerify runs it anyway and
// panics on a miss.
type paymentCastCandidates struct {
	e      *Engine
	p      state.PlayerID
	ids    []state.ObjID
	ready  bool
	priced bool
}

// pricedCandidatesVerify: see derivedMemoVerify. Set by the rules test binary.
var pricedCandidatesVerify = derivedMemoVerifyFlag != ""

func (c *paymentCastCandidates) has(id state.ObjID) bool {
	if c.priced && !pricedCandidatesVerify {
		return true
	}
	if !c.ready {
		hyp := state.Mana{1 << 28, 1 << 28, 1 << 28, 1 << 28, 1 << 28, 1 << 28}
		// Only plain casts are read, so the walk skips the non-cast
		// sections (legalActionsWalk's castsOnly).
		for _, opt := range c.e.legalActionsWalk(c.p, &hyp, true) {
			if opt.Kind == "cast" && opt.Mode == "" && opt.AltCostIndex == 0 {
				c.ids = append(c.ids, opt.Obj)
			}
		}
		c.ready = true
	}
	in := slices.Contains(c.ids, id)
	if c.priced && !in {
		panic(fmt.Sprintf("payment plan: priced-walk plain cast %d missing from the huge-pool walk", id))
	}
	return in
}

// paymentPlanCastShapeOK excludes plain casts whose announced cost or result
// depends on a choice V1 cannot bind into its witness.  The ordinary priority
// option remains available; this only withholds the additive automatic offer.
func (e *Engine) paymentPlanCastShapeOK(p state.PlayerID, id state.ObjID) bool {
	return e.paymentPlanCastShapeDetail(p, id) == ""
}

func (e *Engine) paymentPlanCastShapeDetail(p state.PlayerID, id state.ObjID) string {
	return e.paymentPlanCastShapeDetailUsing(e.collectCostStatics(), p, id)
}

// paymentPlanCastShapeDetailUsing is paymentPlanCastShapeDetail over one
// already-collected cost-static set (the offer builder's, shared by every
// candidate of one decision).
func (e *Engine) paymentPlanCastShapeDetailUsing(statics costStaticViews, p state.PlayerID, id state.ObjID) string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return "shape:additional_cost"
	}
	f := o.Face()
	if len(altAddCostParts(f)) != 0 || len(e.optionalCostViews(statics, p, id)) != 0 {
		return "shape:optional_cost"
	}
	// Cost$ on the spell ability and the supported cost-static extra are both
	// additional costs; neither is represented by a mana-only plan witness.
	mods := e.costModifiersWithTargetsUsing(statics, p, id, spellScope(""), nil, false)
	spellCost := Cost{}
	if sa := f.SpellAbility(); sa != nil {
		spellCost = e.parseCost(sa.Params["Cost"])
	}
	// A cost static's own non-mana extra (Soul Immolation's Blight<X>) stays
	// withheld entirely: the mana-only witness cannot describe it.
	if paymentPlanCostDetail(mods.extra) != "" {
		return "shape:additional_cost"
	}
	// The spell ability's own Cost$ admits exactly one non-mana shape: a
	// fixed-count mandatory sacrifice. The mana half of that same cost still
	// has to be V1-clean, which paymentPlanNonManaAdmissible checks after
	// removing the Sac parts. Every other non-mana part (Discard, PayLife,
	// Exile, tapXType, RevealOrChoose, Exert, ...) and every variable-count
	// Sac<X/...>/Sac<All/...> still declines.
	if paymentPlanNonManaAdmissible(spellCost) != "" || paymentPlanNonManaAdmissible(withSpellAbilityExtras(f, Cost{})) != "" {
		return "shape:additional_cost"
	}
	if e.hasCastConvoke(id) || e.hasCastImprovise(id) || e.HasKeyword(id, "Delve") {
		return "shape:contribution"
	}
	if f.HasKeyword("Gift") {
		return "shape:gift"
	}
	// faceHasModeCost, memoised per face (face_scan_memo.go).
	if e.faceScanHas(f, faceScanModalCost) {
		return "shape:modal_cost"
	}
	for _, key := range []string{"Replicate", "Multikicker", "Squad"} {
		if _, ok := f.KeywordParam(key); ok {
			return "shape:optional_cost"
		}
	}
	if e.faceScanHas(f, faceScanReadsManaSpent) || faceWantsConverge(f) || faceWantsCastSpend(f) || e.paymentPlanBoardSpendReaderOut() {
		return "shape:mana_spent_reader"
	}
	if e.paymentPlanHasTargetDependentModifierUsing(statics, p, id) {
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

// paymentPlanNonManaAdmissible reports whether c is a cost a mana-only V1
// witness can describe: its mana half must classify cleanly and its only
// permitted non-mana part is a fixed-count mandatory Sac<N/Spec>. A
// variable-count Sac<X/Spec> (CostPart.Announced, the announced count binds
// the cast's X) and Sac<All/...> (which never reaches Cost.Sac -- it parses as
// Unknown) are not admissible, and every other non-mana field declines. The
// mana half is checked by removing the Sac parts and running the same
// classifier the gate has always used, so X/hybrid/Phyrexian/snow and the
// other mana-class shapes still decline here. Returns the decline detail, or
// "" when the cost is admissible.
func paymentPlanNonManaAdmissible(c Cost) string {
	for _, part := range c.Sac {
		if part.Announced || part.N <= 0 {
			return "cost:sacrifice"
		}
	}
	rest := c
	rest.Sac = nil
	return paymentPlanCostDetail(rest)
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
	return e.paymentPlanHasTargetDependentModifierUsing(e.collectCostStatics(), p, id)
}

// paymentPlanHasTargetDependentModifierUsing is
// paymentPlanHasTargetDependentModifier over one already-collected
// cost-static set.
func (e *Engine) paymentPlanHasTargetDependentModifierUsing(statics costStaticViews, p state.PlayerID, id state.ObjID) bool {
	if o := e.G.Obj(id); o == nil || !paymentPlanSpellTargets(o.Face()) {
		return false
	}
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
// Without the decision in hand it derives BaseOptionIndex from a fresh
// legalActions walk; EnsurePaymentActions passes the pending Options instead.
func (e *Engine) PaymentActionsForPriority(p state.PlayerID, seq uint64) []decision.PaymentAction {
	return e.paymentActionsForPriority(p, seq, nil)
}

// paymentActionsForPriority is the one-pass offer builder (spec 5 as amended:
// the pending decision's Options and one hypothetical candidate walk, not a
// full legal-action walk per candidate). options is the priority decision's
// own Options, which BaseOptionIndex indexes; nil derives them with one
// legalActions walk, and only once some action needs them.
//
// Per build it runs ONE legal-action walk: the PotentialMana walk, which
// discovers the candidates and their order and labels, casts only
// (legalActionsWalk's castsOnly). The huge-pool walk PlanCastPayment
// requires of each candidate is implied by it (paymentCastCandidates'
// priced), and the PotentialMana walk also withholds a cast its potential
// sources cannot afford, which the huge-pool walk admits, so the planner
// only ever sees the candidates it saw before. Cost statics are collected
// once and shared by every candidate.
func (e *Engine) paymentActionsForPriority(p state.PlayerID, seq uint64, options []decision.Option) []decision.PaymentAction {
	if e.G.Over {
		return nil
	}
	// Every candidate's plan is declined on a pool the planner cannot
	// account for (planCastPaymentChecked), and that verdict reads only the
	// player, so no walk can change the empty result.
	if !paymentPlanPoolOK(e.G.Players[p]) {
		e.paymentStats.recordBuild(true)
		return nil
	}
	e.paymentStats.recordBuild(false)
	// The build is a pure read: one memo scope makes every nested walk
	// (PotentialMana, the candidate walk, each candidate's window-unit and
	// legality reads) share one generation and one board-static scan.
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	// One query scope (zone-entry index, source census) serves every
	// candidate's planner query.
	defer e.paymentPlanQueryScope()()
	// legalActionsPriced is the authoritative candidate walk.  Its hypothetical
	// pool is only a superset gate; every admission below still has an exact
	// source-exclusive witness.
	hyp := e.PotentialMana(p)
	// Only plain casts are read below, so the walk skips the sections that
	// append non-cast options (legalActionsWalk's castsOnly): the same cast
	// options in the same order, without pricing every battlefield ability.
	candidates := e.legalActionsWalk(p, &hyp, true)
	statics := costStaticSource{e: e}
	legal := paymentCastCandidates{e: e, p: p, priced: true}
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
		// Derive the origin from the object's actual zone.  Only the hand and
		// the command zone (Commander format) ever offer a plain cast
		// (Mode "" AltCostIndex 0); anything else fails closed.
		originObj := e.G.Obj(opt.Obj)
		if originObj == nil {
			continue
		}
		var origin string
		switch originObj.Zone {
		case state.ZHand:
			origin = "hand"
		case state.ZCommand:
			origin = "command_zone"
		default:
			continue
		}
		cast := decision.PlannedCast{Object: opt.Obj, Face: 0, Origin: origin}
		got := e.planCastPaymentChecked(p, cast, &statics, &legal)
		e.paymentStats.recordOutcome(got)
		if got.Plan == nil {
			continue
		}
		// The candidate walk's target census ran with the card in hand; the
		// planned cast asks for targets with it on the stack (CR 601.2a
		// before 601.2c). A one-click plan whose mandatory targets vanish
		// when the card leaves the hand would only reverse (CR 733.1), so
		// it is not offered (castprobe.go).
		if !e.castTargetsAvailableOnStack(p, opt.Obj) {
			continue
		}
		// Likewise the plan executes in the CR 601.2g window, after the card
		// left the hand: a planned source whose activation restriction or
		// amount reads the hand must still hold there, or the step falls
		// back source_changed and the cast reverses (castprobe.go).
		if !e.paymentPlanHoldsOnStack(p, opt.Obj, *got.Plan) {
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
		if options == nil {
			options = e.legalActions(p)
		}
		for i := range options {
			if options[i].Kind == "cast" && options[i].Obj == opt.Obj && options[i].Mode == "" && options[i].AltCostIndex == 0 {
				idx := options[i].Index
				a.BaseOptionIndex = &idx
				break
			}
		}
		out = append(out, a)
		e.paymentStats.recordOffered(len(a.Plans))
	}
	return out
}

// ValidateCastPayment independently re-derives the eligible sources and cost
// then proves that exactly the submitted witness pays it.  It is deliberately
// usable by the executor without trusting an offer cache or an ID.
func (e *Engine) ValidateCastPayment(p state.PlayerID, cast decision.PlannedCast, plan decision.PaymentPlan) error {
	// A pure read: one Derived memo scope (derivedmemo.go) serves the
	// candidate walk, the planner and the census below, and one query scope
	// serves the planner's census to the rebuild.
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	defer e.paymentPlanQueryScope()()
	got := e.PlanCastPayment(p, cast)
	// PP-14: a Sac-bearing additional cost is answered by the ordinary in-flow
	// ask AFTER this validation, so the distinct-candidate assignment must
	// still hold at submit time. Re-run the offer gate's own sacrifice
	// feasibility check (nonManaCastable) on the composed cost and reject with
	// a clear error when a candidate left the battlefield between offer and
	// submit; the seat then falls back to the manual window instead of
	// committing a cast whose additional cost can no longer be paid.
	if o := e.G.Obj(cast.Object); o != nil && o.Face() != nil {
		composed := e.offerCostFor(p, cast.Object, withSpellAbilityExtras(o.Face(), e.rawBaseCost(p, cast.Object)), spellScope(""))
		if len(composed.Sac) != 0 && !e.nonManaCastable(p, cast.Object, composed, false) {
			return fmt.Errorf("payment plan sacrifice cost no longer payable")
		}
	}
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
	units := e.paymentPlanQueryUnits(p)
	pool := e.G.Players[p].Pool
	produced := state.Mana{}
	var pain int64
	lastResort := false
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
		pain += paymentPlanConsequencePain(step.consequence)
		lastResort = lastResort || step.tier == paymentTierLastResort
		pool = manaAdd(pool, step.mana)
		produced = manaAdd(produced, step.mana)
	}
	// The lethal guard and the phase rule (spec 5): a witness never kills its
	// caster, and it uses a last-resort source only when no plan from normal
	// sources exists -- exactly when the planner's own best plan uses one.
	if pain > 0 && pain >= int64(e.G.Players[p].Life) {
		return fmt.Errorf("payment plan life and damage would be lethal")
	}
	if lastResort && !paymentPlanUsesLastResort(*got.Plan) {
		return fmt.Errorf("payment plan uses a last-resort source while a normal plan exists")
	}
	cost := e.offerCostFor(p, cast.Object, withSpellAbilityExtras(e.G.Obj(cast.Object).Face(), e.rawBaseCost(p, cast.Object)), spellScope(""))
	payment, ok := cost.resolveManaWith(pool, state.Mana{}, [7]state.Mana{}, e.G.Players[p].Life, false, pipRider{}, nil)
	expected := paymentWitness(cost, e.G.Players[p].Pool, produced, nil, payment.pool)
	if !ok || paymentManaAmount(payment.pool) != plan.PoolAfter || expected.PoolSpend != plan.PoolSpend {
		return fmt.Errorf("payment witness does not settle")
	}
	return nil
}

// paymentPlanUsesLastResort reports whether any step of plan discloses a
// consequence (exactly the last-resort steps).
func paymentPlanUsesLastResort(plan decision.PaymentPlan) bool {
	for _, a := range plan.Activations {
		if a.Consequence != nil {
			return true
		}
	}
	return false
}

// paymentPlanRemainingPain sums the disclosed life + damage of plan's steps
// from index from on (the executor's lethal revalidation).
func paymentPlanRemainingPain(plan decision.PaymentPlan, from int) int64 {
	var pain int64
	for i := from; i < len(plan.Activations); i++ {
		if c := plan.Activations[i].Consequence; c != nil {
			pain += int64(c.Life) + int64(c.Damage)
		}
	}
	return pain
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
	// flex and colours describe the SOURCE, not this alternative: over every
	// eligible alternative of the source, flex is the number of distinct mana
	// types (W/U/B/R/G/C) it can produce and colours is the WUBRG bitmask
	// (bit i = state.MW+i) of the colours it can produce. They feed rank keys
	// 5 (flexibility consumed) and 7 (remainder diversity).
	//
	// Both are computed over the source's NORMAL alternatives (the phase-1
	// view, and the "untapped normal remainder" keys 6 and 7 read); a source
	// with only last-resort alternatives has colours 0. flexAll is the same
	// count over every eligible alternative, normal and last resort, which
	// is what key 5 reads in phase 2 (paymentPlanLastResortChoices).
	flex    int
	flexAll int
	colours uint8
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
	defer e.paymentPlanQueryScope()()
	units := e.paymentPlanQueryUnits(p)
	// V1 accepts only fixed production.  A permissive window unit is useful to
	// manual payment, but not proof an automatic choice will remain exact.
	choices := make([][]plannedManaActivation, len(units))
	for i, u := range units {
		choices[i] = e.paymentPlanQueryAlternatives(u)
	}
	// Phase 1 (spec 5): normal sources only. If it finds a complete plan,
	// that is the offer and last-resort sources are never considered.
	life := e.G.Players[p].Life
	phase1 := paymentPlanPhaseChoices(choices, paymentTierNormal)
	rankCtx := newPaymentPlanRankContext(choices, e.paymentPlanHandDemand(p, cast.Object))
	search := searchPaymentPlan(cost, e.G.Players[p].Pool, life, rankCtx,
		phase1, e.paymentPlanQueryClasses(p, paymentTierNormal, phase1))
	nodes := search.nodes
	// Phase 2 runs only when phase 1 PROVES no plan exists (insufficient,
	// not search_limit): normal plus last-resort alternatives, ranked by the
	// irreversible-cost key, never a plan whose summed life + damage would
	// reduce the caster to 0 or less. The rank context is phase 1's: keys 6
	// and 7 read the untapped normal remainder in both phases.
	if search.best == nil && !search.limited {
		if phase2 := paymentPlanLastResortChoices(choices, life); phase2 != nil {
			search = searchPaymentPlan(cost, e.G.Players[p].Pool, life, rankCtx,
				phase2, e.paymentPlanQueryClasses(p, paymentTierLastResort, phase2))
			nodes += search.nodes
		}
	}
	switch {
	case search.best != nil && search.limited:
		return PaymentPlanOutcome{Plan: search.best, Nodes: nodes, Reason: "search_limit"}
	case search.best != nil:
		return PaymentPlanOutcome{Plan: search.best, Nodes: nodes}
	case search.limited:
		return PaymentPlanOutcome{Reason: "search_limit", Nodes: nodes}
	}
	// The first source diagnostic is read only for the outcome that
	// reports it.
	firstSourceDetail := ""
	for _, u := range units {
		if firstSourceDetail != "" {
			break
		}
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
	return PaymentPlanOutcome{Reason: "insufficient", Detail: firstSourceDetail, Nodes: nodes}
}

// paymentPlanManaUnits extends the shared fixed-production payment census
// with the choice shapes a V1 witness can make concrete (Produced$ Any with a
// fixed amount, an amount-1 Combo/Chosen/ColorIdentity) and with the
// fixed-production abilities whose cost is not a bare tap (the census lists
// free-cost abilities only), so the tier gate sees every last-resort
// candidate (spec §3.2). The shared census must keep withholding the choice
// shapes for attack/unless windows, which cannot answer a colour choice; this
// planner records its selected W/U/B/R/G output and executes that exact
// rewrite.
//
// It additionally prices a source the shared census withholds because its
// Amount$ is not statically literal, when the engine's own evaluator resolves
// it to a value provably invariant to the payment's own activations
// (paymentPlanStableAmount): an Urza land's SVar-indirected
// Count$UrzaLands.3.1. The shared census must keep withholding those too,
// for the attack/unless windows, which can neither make a colour choice
// concrete nor re-verify a changing amount at activation.
//
// The census is a pure read, so it runs in one Derived memo scope
// (derivedmemo.go): outside a walk (the executor's per-step revalidation,
// ValidateCastPayment) every source's availability check otherwise
// re-derived the board. The two layers below ask each untapped source for
// the same payment-window membership, which reads the board only, so the
// first layer's list is kept (by zone position) for the second.
func (e *Engine) paymentPlanManaUnits(p state.PlayerID) []windowManaUnit {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	units := e.windowManaUnits(p)
	zone := e.G.Zone(state.ZBattlefield, p)
	windowMas := make([][]*cards.SA, len(zone))
	for zi, id := range zone {
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
		windowMas[zi] = e.availableManaAbilitiesForWindow(p, id, false)
		for _, ma := range windowMas[zi] {
			raw := strings.TrimSpace(ma.Params["Produced"])
			amt := availableAmount(ma)
			if amt <= 0 {
				continue
			}
			counts, any := cards.ProducedCounts(ma.Params["Produced"])
			if !any {
				// Fixed production whose cost is not a bare tap (Eldrazi
				// Spawn's Sac<1/CARDNAME>: Add {C}) is outside the shared
				// census, which lists free-cost abilities only. Such an
				// ability is never normal (paymentPlanTapOnlyCost); the tier
				// gate in paymentPlanUnitAlternatives keeps it only when it is
				// a last-resort shape.
				cost := e.parseCost(ma.Params["Cost"])
				if manaFreeCost(cost) || strings.TrimSpace(ma.Params["RestrictValid"]) != "" {
					continue
				}
				total := int32(0)
				for _, n := range counts {
					total += n
				}
				if total <= 0 {
					continue
				}
				if idx < 0 {
					units = append(units, windowManaUnit{id: id})
					idx = len(units) - 1
				}
				units[idx].alts = append(units[idx].alts, windowManaAlt{ma: ma, counts: counts, amt: amt})
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
	// Evaluated-amount layer: an ability windowManaUnits skipped because
	// availableAmount could not statically price its Amount$ is offered here
	// whenever paymentPlanStableAmount resolves it with the engine's own
	// evaluator AND proves the value cannot move when the payment taps its
	// sources. The probe is built lazily: a board with no evaluated-amount
	// source (the overwhelming majority) pays no clone at all.
	var probe *Engine
	for zi, id := range zone {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil {
			continue
		}
		if walkCacheVerify {
			if fresh := e.availableManaAbilitiesForWindow(p, id, false); !slices.EqualFunc(fresh, windowMas[zi], sameManaAbility) {
				panic(fmt.Sprintf("payment plan census: window membership for %d moved inside the census", id))
			}
		}
		for _, ma := range windowMas[zi] {
			if availableAmount(ma) > 0 {
				continue // windowManaUnits' static path already priced it.
			}
			// Only a V1 source contract (a bare tap) can be executed from a
			// witness, so never build the probe for an ability the plan could
			// not activate anyway.
			if !paymentPlanTapOnlyCost(e.parseCost(ma.Params["Cost"])) {
				continue
			}
			if probe == nil {
				probe = e.paymentPlanTappedProbe(p)
			}
			amt, ok := e.paymentPlanStableAmount(probe, p, id, o, ma)
			if !ok {
				continue
			}
			counts, any := cards.ProducedCounts(ma.Params["Produced"])
			if any {
				units = appendPaymentPlanUnitAlt(units, id, windowManaAlt{ma: ma, counts: counts, amt: amt, any: true})
				continue
			}
			total := int32(0)
			for _, n := range counts {
				total += n
			}
			if total <= 0 {
				continue
			}
			units = appendPaymentPlanUnitAlt(units, id, windowManaAlt{ma: ma, counts: counts, amt: amt})
		}
	}
	return units
}

// appendPaymentPlanUnitAlt appends one alternative to the unit that already
// names id, creating the unit if the shared census and the choice-shape layer
// both left it out. It keeps the evaluated-amount layer from duplicating the
// unit-lookup bookkeeping the choice-shape loop spells out inline.
func appendPaymentPlanUnitAlt(units []windowManaUnit, id state.ObjID, alt windowManaAlt) []windowManaUnit {
	for i := range units {
		if units[i].id == id {
			units[i].alts = append(units[i].alts, alt)
			return units
		}
	}
	return append(units, windowManaUnit{id: id, freeCount: 1, alts: []windowManaAlt{alt}})
}

// paymentPlanTappedProbe clones the engine and taps every battlefield
// permanent p controls. The clone is e.Clone(), whose zeroed layer caches
// (staticEpoch/activeEpoch/continuousVersion) force any Derived read to
// rebuild against the cloned, all-tapped board rather than serving the live
// engine's cache. It is a pure throwaway: the clone never emits and is
// discarded after the two evaluations in paymentPlanStableAmount.
func (e *Engine) paymentPlanTappedProbe(p state.PlayerID) *Engine {
	probe := e.Clone()
	for _, id := range probe.G.Zone(state.ZBattlefield, p) {
		if o := probe.G.Obj(id); o != nil {
			o.Tapped = true
		}
	}
	return probe
}

// paymentPlanStableAmount prices a mana ability's Amount$ with the engine's
// own evaluator (castWindowAmount: the source face's SVar table and
// effects.Num's grammar) and admits the value only when it is provably
// invariant to the state a V1 payment changes for its sources.
//
// A V1 plan admits only normal-tier, tap-only alternatives
// (paymentPlanAbilityShapeTier / paymentPlanTapOnlyCost): activating one taps
// its source, adds mana, and changes nothing else -- no sacrifice, no return,
// no life. So the only per-object field an amount could read that the payment
// moves is the tapped bit. This proves invariance constructively, not by
// enumerating count heads: it re-evaluates the same expression on a clone
// whose battlefield permanents are all tapped, and requires the two values to
// agree. A count of untapped permanents therefore refuses the plan, while the
// Urza lands' presence/subtype count is unchanged.
//
// The check can only be too strict, never too lax, and even then it is not the
// last word: the executor re-derives this alternative at every activation and
// compares the mana actually added with the witness
// (paymentPlanStepReady / paymentPlanProducedExactly), so a value that moved
// between the plan and the activation is a plan failure, never a silent
// overpay.
func (e *Engine) paymentPlanStableAmount(probe *Engine, p state.PlayerID, source state.ObjID, o *state.Object, ma *cards.SA) (int32, bool) {
	amt, ok := e.castWindowAmount(p, source, o, ma)
	if !ok || amt <= 0 {
		return 0, false
	}
	if probeAmt, ok := probe.castWindowAmount(p, source, o, ma); !ok || probeAmt != amt {
		return 0, false
	}
	return amt, true
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
	// A last-resort step always discloses a consequence (spec §4: a present
	// consequence sets at least one field); a shape that classified last
	// resort without one is not a shape the witness can describe.
	if tier == paymentTierLastResort && c == (paymentConsequence{}) {
		return paymentTierDeferred, paymentConsequence{}, "source:last_resort"
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
	//
	// The verdict is the one the sorted walk reaches first: the smallest key
	// failing any of the three checks decides it. Taking that minimum over
	// the unsorted keys is order-independent (so deterministic) and spares
	// the sorted copy.
	failing, found := "", false
	for key := range maps.Keys(ma.Params) {
		if (!found || key < failing) && paymentPlanShapeKeyFails(key) {
			failing, found = key, true
		}
	}
	if found {
		key := failing
		if strings.HasPrefix(key, "Condition") {
			return deferred("source:conditional")
		}
		if containsTargetFold(key) || key == "ValidTgts" || key == "ValidTarget" {
			return deferred("source:target")
		}
		return deferred("source:param:" + key)
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
		// Every other cost part must be absent: the witness discloses only
		// the tap, the self-sacrifice, the life and the self-return.
		if !paymentPlanLastResortCostOK(cost) {
			return deferred("source:last_resort")
		}
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

// paymentPlanLastResortCostOK reports whether a last-resort activation cost
// is exactly {T} (optional), Sac<1/...>, PayLife<N> and Return<1/...>: every
// other part -- mana, X, counters, discard, exile, mill, reveal, energy, a tap
// of another permanent, an unparsed token -- is outside what a witness step
// can disclose, so the ability stays deferred.
func paymentPlanLastResortCostOK(c Cost) bool {
	rest := c
	rest.Tap, rest.Sac, rest.Life, rest.Return = false, nil, 0, nil
	return rest.Generic == 0 && rest.Colored == (state.Mana{}) && len(rest.ExileFromTop) == 0 && paymentPlanCostOK(rest)
}

// paymentPlanHasSpecialProductionParam reports whether the ability's own head
// carries a production special-effect parameter (spec 3.2: TriggersWhenSpent$,
// AddsCounters$, the AddsKeywords* family, AddsNoCounter$, PersistentMana$,
// UnlessCost$, Defined$). Such production does something beyond adding plain
// mana to the pool, so the ability is deferred.
func paymentPlanHasSpecialProductionParam(ma *cards.SA) bool {
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

// paymentPlanShapeKeyFails reports whether key alone defers the ability in
// paymentPlanAbilityShapeTier's key walk (a Condition key, a target key or
// an unreviewed parameter).
func paymentPlanShapeKeyFails(key string) bool {
	return strings.HasPrefix(key, "Condition") || containsTargetFold(key) || key == "ValidTgts" || key == "ValidTarget" ||
		!paymentPlanKnownManaParam(key)
}

// containsTargetFold is strings.Contains(strings.ToLower(key), "target")
// without the lowered copy for an ASCII key (a non-ASCII key takes the
// original expression, so the answer is identical for every input).
func containsTargetFold(key string) bool {
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
		switch tier {
		case paymentTierNormal:
			if !paymentPlanTapOnlyCost(e.parseCost(alt.ma.Params["Cost"])) {
				continue
			}
		case paymentTierLastResort:
			// The classifier vetted the whole cost and chain: {T} plus the
			// disclosed self-sacrifice/life/self-return parts, or a tap-only
			// cost with a disclosed rider, trigger or replacement.
		default:
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
	// search happened to choose. Flexibility is the number of DISTINCT mana
	// types those outcomes produce (spec 5 key 5), so a Produced$ Any source
	// counts 5, a typed dual 2, and two abilities that both add {U} count 1.
	var types, all uint8 // bit i = mana index i (W/U/B/R/G/C)
	for _, a := range out {
		for i, n := range a.mana {
			if n > 0 {
				all |= 1 << i
				if a.tier == paymentTierNormal {
					types |= 1 << i
				}
			}
		}
	}
	flex, flexAll := bits.OnesCount8(types), bits.OnesCount8(all)
	colours := types &^ (1 << state.MC)
	for i := range out {
		out[i].flex = flex
		out[i].flexAll = flexAll
		out[i].colours = colours
	}
	return out
}

// paymentPlanLastResortChoices is phase 2's alternative table (spec 5): every
// unit's normal AND last-resort alternatives, each carrying the source's
// phase-2 flexibility (flexAll, key 5 over every eligible alternative), minus
// any alternative whose own life + damage would by itself be lethal at the
// caster's current life (the lethal guard, applied to one step here and to
// the whole plan by the search). Unit positions are preserved. It returns
// nil when no unit has a last-resort alternative left, so phase 2 cannot
// differ from phase 1 and is skipped.
func paymentPlanLastResortChoices(choices [][]plannedManaActivation, life int32) [][]plannedManaActivation {
	out := make([][]plannedManaActivation, len(choices))
	any := false
	for i, alts := range choices {
		for _, a := range alts {
			if a.tier < paymentTierLastResort {
				continue
			}
			if pain := paymentPlanConsequencePain(a.consequence); pain > 0 && pain >= int64(life) {
				continue
			}
			if a.tier == paymentTierLastResort {
				any = true
			}
			a.flex = a.flexAll
			out[i] = append(out[i], a)
		}
	}
	if !any {
		return nil
	}
	return out
}

// paymentPlanConsequencePain is the life a step costs its caster: life paid
// plus damage dealt to its controller (the lethal guard's measure).
func paymentPlanConsequencePain(c paymentConsequence) int64 {
	return int64(c.life) + int64(c.damage)
}

// wire is the consequence as the witness discloses it: nil for a normal step.
func (c paymentConsequence) wire() *decision.PaymentConsequence {
	if c == (paymentConsequence{}) {
		return nil
	}
	return &decision.PaymentConsequence{Sacrifice: c.sacrifice, Life: c.life, Damage: c.damage, NoUntap: c.noUntap, ReturnToHand: c.returnToHand}
}

// paymentConsequenceEqual reports whether a witness step's disclosed
// consequence is exactly c.
func paymentConsequenceEqual(c paymentConsequence, w *decision.PaymentConsequence) bool {
	got := c.wire()
	if got == nil || w == nil {
		return got == nil && w == nil
	}
	return *got == *w
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
// Volcanic Island's {U} and {R} abilities differ only in Produces. The
// disclosed consequence must also be exactly the one the source would incur
// now (spec §6: a changed consequence is production_changed). The pair
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
			if candidate.ma != nil && candidate.activation.Ability == pa.Ability && candidate.activation.Produces == pa.Produces &&
				paymentConsequenceEqual(candidate.consequence, pa.Consequence) {
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
// sentinel. Inside a planner query it reads the query's zone-entry index
// (paymentPlanQuery's paymentZoneSeqIndex, built by one backward pass);
// otherwise, and for any object the index does not cover, it scans
// (paymentSourceZoneSeqScan).
func (e *Engine) paymentSourceZoneSeq(id state.ObjID) uint64 {
	if q := e.paymentPlanQuery; q.valid(e) {
		got := q.zoneSeqs.lookup(e, id)
		if walkCacheVerify {
			if want := e.paymentSourceZoneSeqScan(id); got != want {
				panic(fmt.Sprintf("payment zone-entry index: object %d seq %d, log scan %d", id, got, want))
			}
		}
		return got
	}
	return e.paymentSourceZoneSeqScan(id)
}

// paymentSourceZoneSeqScan is the reference answer: it deliberately scans
// backwards so a later incarnation cannot be authorized by a witness made for
// an earlier visit to the battlefield.
func (e *Engine) paymentSourceZoneSeqScan(id state.ObjID) uint64 {
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

// Irreversible-cost weights (spec 5 key 1, as amended 2026-09-26). They are
// Arena-calibrated: two self-sacrificed Treasures (20) are cheaper than one
// Mana Vault that does not untap (25), which is cheaper than three Treasures
// (30); a point of damage or life (3) is cheaper than any sacrifice. The key
// is summed per activated ability, so a source's painless ability always
// beats its painful one for the same need.
const (
	paymentPlanCostSacrificeSelf         = 10 // sacrifice-self, non-creature source
	paymentPlanCostSacrificeSelfCreature = 20 // sacrifice-self, creature source
	paymentPlanCostNoUntap               = 25 // the source does not untap next untap step
	paymentPlanCostPerLifeOrDamage       = 3  // per point of life paid or damage taken
	paymentPlanCostReturnToHand          = 8  // the source returns to its owner's hand
)

// paymentPlanConsequenceCost is one activation's irreversible cost under the
// weights above.
func paymentPlanConsequenceCost(c paymentConsequence, creature bool) int64 {
	var n int64
	if c.sacrifice {
		if creature {
			n += paymentPlanCostSacrificeSelfCreature
		} else {
			n += paymentPlanCostSacrificeSelf
		}
	}
	if c.noUntap {
		n += paymentPlanCostNoUntap
	}
	n += paymentPlanCostPerLifeOrDamage * (int64(c.life) + int64(c.damage))
	if c.returnToHand {
		n += paymentPlanCostReturnToHand
	}
	return n
}

// paymentPlanRankContext is the per-query input the rank needs beyond one
// plan: for each colour (WUBRG), how many untapped sources with an eligible
// normal alternative can produce it, and the acting player's own hand's
// colour demand. Both are computed once from the query, so a plan's
// remainder is the census minus the plan's own chosen sources.
type paymentPlanRankContext struct {
	colourSources [5]int
	// Key 6 inputs (spec 5 amended): handDemand[c] is the largest number of
	// colour c's pips on any single nonland card in the acting player's own
	// hand other than the cast card; demandOrder lists the colour indices by
	// descending demand (WUBRG ties); reserveBase is max demand + 1, the
	// digit base of the packed coverage. A zero reserveBase (a directly
	// constructed context, or no demanded colour) leaves key 6 at 0 for
	// every plan, the tie value the placeholder pinned.
	handDemand  [5]int
	demandOrder [5]int
	reserveBase int
}

func newPaymentPlanRankContext(choices [][]plannedManaActivation, demand [5]int) paymentPlanRankContext {
	var ctx paymentPlanRankContext
	for _, alts := range choices {
		if len(alts) == 0 {
			continue
		}
		for c := range ctx.colourSources {
			if alts[0].colours&(1<<c) != 0 {
				ctx.colourSources[c]++
			}
		}
	}
	ctx.handDemand = demand
	max := 0
	for _, n := range demand {
		if n > max {
			max = n
		}
	}
	ctx.reserveBase = max + 1
	ctx.demandOrder = [5]int{0, 1, 2, 3, 4}
	// Stable over the WUBRG seed order, so equal demands keep WUBRG order.
	slices.SortStableFunc(ctx.demandOrder[:], func(a, b int) int {
		return cmp.Compare(demand[b], demand[a])
	})
	return ctx
}

// paymentPlanHandDemand measures the acting player's OWN hand's colour
// demand (spec 5 key 6): for each WUBRG colour, the largest number of that
// colour's pips on any single nonland card in hand other than the card being
// cast. It reads only the acting player's own hand and the printed mana
// costs, so an opponent's hand and every other zone stay out of the rank.
func (e *Engine) paymentPlanHandDemand(p state.PlayerID, exclude state.ObjID) [5]int {
	var demand [5]int
	for _, id := range e.G.Zone(state.ZHand, p) {
		if id == exclude {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().IsLand() {
			continue
		}
		pips := costPips(e.rawBaseCost(p, id))
		for c := range demand {
			if pips[c] > demand[c] {
				demand[c] = pips[c]
			}
		}
	}
	return demand
}

// costPips counts a parsed printed cost's coloured pips per WUBRG colour:
// each plain pip for its colour, each hybrid pip for both its colours, a
// Phyrexian pip for its colour, a twobrid for its coloured face (its generic
// face is not a pip) and a hybrid-Phyrexian pip for both its colours.
// Generic, {X} and {C} pips count for no colour.
func costPips(c Cost) [5]int {
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

// paymentPlanRankStep is one witness step as the final tie-break compares it.
type paymentPlanRankStep struct {
	act         decision.PaymentActivation
	consequence paymentConsequence
}

// paymentPlanRank is spec 5's lexicographic rank tuple (amended 2026-09-26);
// less orders it, lower first, key by key in field order.
type paymentPlanRank struct {
	cost        int64 // 1. irreversible cost (0 for every normal-tier plan)
	creatures   int   // 2. creature sources activated
	sources     int   // 3. newly activated sources
	surplus     int32 // 4. surplus mana left in the pool after paying
	flex        int   // 5. distinct mana types the chosen sources could have made
	handReserve int   // 6. packed hand-reserve coverage, MORE first (spec 5 amended; was a zero placeholder)
	remainder   int   // 7. distinct colours the unused sources still make (MORE first)
	steps       []paymentPlanRankStep
}

func (r paymentPlanRank) less(o paymentPlanRank) bool {
	if r.cost != o.cost {
		return r.cost < o.cost
	}
	if r.creatures != o.creatures {
		return r.creatures < o.creatures
	}
	if r.sources != o.sources {
		return r.sources < o.sources
	}
	if r.surplus != o.surplus {
		return r.surplus < o.surplus
	}
	if r.flex != o.flex {
		return r.flex < o.flex
	}
	// Key 6 (hand reserve) is MORE coverage first, like key 7's remainder.
	if r.handReserve != o.handReserve {
		return r.handReserve > o.handReserve
	}
	if r.remainder != o.remainder {
		return r.remainder > o.remainder
	}
	return comparePaymentPlanSteps(r.steps, o.steps) < 0
}

// comparePaymentPlanSteps is key 8: the typed witness, compared numerically
// step by step (source object ID, ability kind, face, index, intrinsic name,
// produced vector, consequence), then by length. It replaces a fmt.Sprint
// string compare that sorted object 10 before object 9.
func comparePaymentPlanSteps(a, b []paymentPlanRankStep) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		if c := cmp.Compare(x.act.Source, y.act.Source); c != 0 {
			return c
		}
		if c := cmp.Compare(x.act.Ability.Kind, y.act.Ability.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(x.act.Ability.Face, y.act.Ability.Face); c != 0 {
			return c
		}
		if c := cmp.Compare(x.act.Ability.Index, y.act.Ability.Index); c != 0 {
			return c
		}
		if c := cmp.Compare(x.act.Ability.Intrinsic, y.act.Ability.Intrinsic); c != 0 {
			return c
		}
		for k := range x.act.Produces {
			if c := cmp.Compare(x.act.Produces[k], y.act.Produces[k]); c != 0 {
				return c
			}
		}
		if c := comparePaymentConsequence(x.consequence, y.consequence); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func comparePaymentConsequence(a, b paymentConsequence) int {
	flag := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if c := cmp.Compare(flag(a.sacrifice), flag(b.sacrifice)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.life, b.life); c != 0 {
		return c
	}
	if c := cmp.Compare(a.damage, b.damage); c != 0 {
		return c
	}
	if c := cmp.Compare(flag(a.noUntap), flag(b.noUntap)); c != 0 {
		return c
	}
	return cmp.Compare(flag(a.returnToHand), flag(b.returnToHand))
}

// rankPaymentPlan computes plan p's rank. as is the plan's chosen
// alternatives (one per source, in witness order), after the pool left once
// the cost is paid, and ctx the query's source census.
func rankPaymentPlan(ctx paymentPlanRankContext, p decision.PaymentPlan, as []plannedManaActivation, after state.Mana) paymentPlanRank {
	r := paymentPlanRank{sources: len(as), surplus: after.Total(), steps: make([]paymentPlanRankStep, len(p.Activations))}
	remaining := ctx.colourSources
	for _, a := range as {
		r.cost += paymentPlanConsequenceCost(a.consequence, a.creature)
		if a.creature {
			r.creatures++
		}
		r.flex += a.flex
		for c := range remaining {
			if a.colours&(1<<c) != 0 {
				remaining[c]--
			}
		}
	}
	for _, n := range remaining {
		if n > 0 {
			r.remainder++
		}
	}
	// Key 6: the packed hand-reserve coverage. Each digit is the plan's
	// coverage min(untapped normal remainder, demand) for one colour, most
	// significant digit first in the demand order, so the packed integer
	// compares lexicographically in that order (less reads it larger first).
	// Zero demand packs to 0 for every plan.
	if ctx.reserveBase > 0 {
		cov := 0
		for _, c := range ctx.demandOrder {
			n := remaining[c]
			if d := ctx.handDemand[c]; n > d {
				n = d
			}
			cov = cov*ctx.reserveBase + n
		}
		r.handReserve = cov
	}
	for i := range p.Activations {
		r.steps[i].act = p.Activations[i]
		if i < len(as) {
			r.steps[i].consequence = as[i].consequence
		}
	}
	return r
}
func paymentWitness(c Cost, initial, produced state.Mana, as []plannedManaActivation, after state.Mana) decision.PaymentPlan {
	acts := make([]decision.PaymentActivation, len(as))
	for i := range as {
		acts[i] = as[i].activation
		acts[i].Consequence = as[i].consequence.wire()
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
