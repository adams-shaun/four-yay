package rules

// This file deliberately contains no call to emit.  Payment plans are an
// offer-time witness; the planned executor lives in cast.go.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

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
	base := pay.WithSpellAbilityExtras(o.Face(), pay.RawBaseCost(asPayer(e), p, id))
	return pay.WireCost(e.offerCostFor(p, id, base, spellScope(""))), true
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
	switch planCastPaymentCheckedCodes.Code(string(cast.Origin)) {
	case planCastPaymentCheckedHand:
	case planCastPaymentCheckedCommandZone:
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
	base := pay.RawBaseCost(asPayer(e), p, cast.Object)
	base = pay.WithSpellAbilityExtras(o.Face(), base)
	cost := e.offerCostForUsing(statics.get(), p, cast.Object, base, spellScope(""))
	if detail := pay.PlanNonManaAdmissible(cost); detail != "" {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	if !pay.PaymentPlanPoolAccepted(asPayer(e), p) {
		return PaymentPlanOutcome{Reason: "unsupported"}
	}
	if global, detail := e.paymentPlanGlobalManaEffect(p, cast.Object); global {
		return PaymentPlanOutcome{Reason: "unsupported", Detail: detail}
	}
	return pay.PlanPaymentCost(asPayer(e), p, cast, pay.PlanManaHalf(cost))
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
		opts := c.e.legalActionsWalkTemp(c.p, &hyp, true)
		for _, opt := range opts {
			if opt.Kind == "cast" && opt.Mode == "" && opt.AltCostIndex == 0 {
				c.ids = append(c.ids, opt.Obj)
			}
		}
		c.e.optRelease(opts)
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
		spellCost = e.parseCost(sa.ParamStr(cards.PKCost))
	}
	// A cost static's own non-mana extra (Soul Immolation's Blight<X>) stays
	// withheld entirely: the mana-only witness cannot describe it.
	if pay.PlanCostDetail(mods.Extra) != "" {
		return "shape:additional_cost"
	}
	// The spell ability's own Cost$ admits exactly one non-mana shape: a
	// fixed-count mandatory sacrifice. The mana half of that same cost still
	// has to be V1-clean, which paymentPlanNonManaAdmissible checks after
	// removing the Sac parts. Every other non-mana part (Discard, PayLife,
	// Exile, tapXType, RevealOrChoose, Exert, ...) and every variable-count
	// Sac<X/...>/Sac<All/...> still declines.
	if pay.PlanNonManaAdmissible(spellCost) != "" || pay.PlanNonManaAdmissible(pay.WithSpellAbilityExtras(f, Cost{})) != "" {
		return "shape:additional_cost"
	}
	if e.hasCastConvoke(id) || e.hasCastImprovise(id) || e.hasKeywordH(id, kwhDelve) {
		return "shape:contribution"
	}
	// Waterbend (a RaiseCost static's Waterbend<N>, Benevolent River
	// Spirit, or the spell's own Cost$) is the same tap-to-help credit as
	// convoke and improvise: the cast poses its helper choice before the
	// mana is paid, and any helper tapped there reprices the generic the
	// witness funded (and may tap a source the witness spends), so the
	// planned cast falls back cost_changed. potentialModeCastPlan declines it
	// for the same reason.
	if mods.Waterbend != 0 || mods.WaterbendX || mods.WaterbendPartX != 0 ||
		spellCost.Waterbend != 0 || spellCost.WaterbendX {
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

// paymentPlanHasTargetDependentModifier finds a live cost static that would
// apply to this ordinary spell except for ValidTarget$, or one that applies
// and reads the announced targets in its amount, ValidSpell$ or CheckSVar$
// (costStaticReadsTargets: Battlefield Thaumaturge's "{1} less for each
// creature it targets").  Its actual amount is unknowable until CR 601.2c,
// after the plan has been selected, so V1 leaves that cast to the normal
// target/payment flow.  A spell that announces no
// target cannot meet any ValidTarget$ clause, so it is never taxed and is not
// declined (paymentPlanSpellTargets).  Copying the parameter map is
// important: static views share compiled-card maps.
func (e *Engine) paymentPlanHasTargetDependentModifier(p state.PlayerID, id state.ObjID) bool {
	if o := e.G.Obj(id); o == nil || !pay.PaymentPlanSpellTargets(o.Face()) {
		return false
	}
	return e.paymentPlanHasTargetDependentModifierUsing(e.collectCostStatics(), p, id)
}

// paymentPlanHasTargetDependentModifierUsing is
// paymentPlanHasTargetDependentModifier over one already-collected
// cost-static set.
func (e *Engine) paymentPlanHasTargetDependentModifierUsing(statics costStaticViews, p state.PlayerID, id state.ObjID) bool {
	return e.paymentPlanTargetDependentFor(statics, p, id, spellScope(""))
}

// paymentPlanTargetDependentFor is paymentPlanHasTargetDependentModifierUsing
// for a cast in scope (a flashback or bestowed cast's own scope).
func (e *Engine) paymentPlanTargetDependentFor(statics costStaticViews, p state.PlayerID, id state.ObjID, scope costScope) bool {
	if o := e.G.Obj(id); o == nil || !pay.PaymentPlanSpellTargets(o.Face()) {
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
			if strings.TrimSpace(sv.ParamStr(cards.PKValidTarget)) == "" {
				// No ValidTarget$, but its amount, ValidSpell$ or CheckSVar$
				// may still read the announced targets (Battlefield
				// Thaumaturge's "{1} less for each creature it targets"):
				// such a static prices the cast per announcement unless a
				// target-INDEPENDENT gate already denies it (costStaticGate's
				// indepFail), so it declines the witness too.
				// A self static (affinity, Card.Self) of another card never
				// prices this one: skip it before the text census.
				if id != sv.Source && sv.ParamStr(cards.PKValidCard) == "Card.Self" {
					continue
				}
				if costStaticReadsTargets(sv) {
					if ok, indepFail := e.costStaticGate(sv, group.mode, p, id, scope, nil, false); ok || !indepFail {
						return true
					}
				}
				continue
			}
			params := make(map[string]string, len(sv.Params)-1)
			for k, v := range sv.Params {
				if k != "ValidTarget" {
					params[k] = v
				}
			}
			sv.Params = params
			if e.costStaticApplies(sv, group.mode, p, id, scope, nil, false) {
				return true
			}
		}
	}
	return false
}

// costStaticReadsTargets reports whether a cost-modifier static WITHOUT
// ValidTarget$ can still price a cast differently per target announcement:
// a ValidSpell$ IsTargeting alternative (validSpellHasTargeting), or an
// Amount$ / CheckSVar$ whose expression -- through the static face's SVar
// table -- names a target reference (Targeted$, TargetedObjects$,
// TargetedObjectsDistinct$, TargetedController$, TargetedByTarget$,
// ParentTargeted$, AllTargeted$, SpellTargeted$, the targetedBy filter
// family ...). Every target-reading count head and filter property spells
// "target" (effects' ref switches), so the case-insensitive match over the
// transitive body is the census; a plain integer literal never reads one.
// The offer gate's markCostValidTarget is deliberately wider (any computed
// amount arms its cheap potential-target retry); the planner, which would
// decline the whole witness, keeps target-independent computed amounts
// (affinity, Count$Valid ... YouCtrl) plannable.
func costStaticReadsTargets(sv staticView) bool {
	if validSpellHasTargeting(sv.ParamStr(cards.PKValidSpell)) {
		return true
	}
	for _, v := range [...]string{sv.ParamStr(cards.PKAmount), sv.ParamStr(cards.PKCheckSVar)} {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, literal := parseInt10(v); literal {
			continue
		}
		if bodyReadsRef(v, sv.SVars, 0, pay.MentionsTarget) {
			return true
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
	// The builder's plans may be shared with the decision's cast-plan memo
	// (planCastPaymentMemo); hand the caller its own copy, as
	// EnsurePaymentActions does.
	Out, _ := e.paymentActionsForPriority(p, p, seq, nil)
	if Out == nil {
		return nil
	}
	return (&decision.Decision{PaymentActions: Out}).Clone().PaymentActions
}

// paymentActionsForPriority is the one-pass offer builder (spec 5 as amended:
// the pending decision's Options and one hypothetical candidate walk, not a
// full legal-action walk per candidate). options is the priority decision's
// own Options, which BaseOptionIndex indexes; nil derives them with one
// legalActions walk, and only once some action needs them.
//
// p is the seat the casts are planned FOR (the acting seat: its hand, mana
// base and pool); idSeat is the seat the action and plan IDs bind, the
// decision's ANSWERING seat, because Decision.Validate (a wire contract a
// rules-ignorant client also runs) re-derives them from d.Player. The two
// differ only under a CR 722 control redirect (Decision.Acting).
//
// Per build it runs ONE legal-action walk: the PotentialMana walk, which
// discovers the candidates and their order and labels, casts only
// (legalActionsWalk's castsOnly). The huge-pool walk PlanCastPayment
// requires of each candidate is implied by it (paymentCastCandidates'
// priced), and the PotentialMana walk also withholds a cast its potential
// sources cannot afford, which the huge-pool walk admits, so the planner
// only ever sees the candidates it saw before. Cost statics are collected
// once and shared by every candidate.
func (e *Engine) paymentActionsForPriority(p, idSeat state.PlayerID, seq uint64, options []decision.Option) ([]decision.PaymentAction, []state.ObjID) {
	if e.G.Over {
		return nil, nil
	}
	// Every candidate's plan is declined on a pool the planner cannot
	// account for (planCastPaymentChecked), and that verdict reads only the
	// player, so no walk can change the empty result.
	if !pay.PlanPoolOK(&e.G.Players[p]) {
		e.PaymentStats.RecordBuild(true)
		return nil, nil
	}
	e.PaymentStats.RecordBuild(false)
	// The builder's potential walk is what the priority walk's block record
	// serves (walk_block_reuse.go): record from now on.
	e.WalkRecDemand = true
	// The build is a pure read: one memo scope makes every nested walk
	// (PotentialMana, the candidate walk, each candidate's window-unit and
	// legality reads) share one generation and one board-static scan.
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	// One query scope (source census, alternatives) serves every
	// candidate's planner query, and is kept for the decision's other pure
	// payment readers (paymentPlanQueryResumeBegin).
	defer pay.PaymentPlanQueryEnd(asPayer(e), pay.PaymentPlanQueryBegin(asPayer(e)))
	defer e.paymentPlanQueryKeep(p)
	// legalActionsPriced is the authoritative candidate walk.  Its hypothetical
	// pool is only a superset gate; every admission below still has an exact
	// source-exclusive witness.
	// Only plain casts are read below, so the walk skips the sections that
	// append non-cast options (legalActionsWalk's castsOnly): the same cast
	// options in the same order, without pricing every battlefield ability.
	// The walk is shared with the decision's other potential readers
	// (potential_walk_cache.go), which may hand back the full walk.
	hyp, candidates := e.potentialWalkOf(p, false)
	statics := costStaticSource{e: e}
	legal := paymentCastCandidates{e: e, p: p, priced: true}
	var out []decision.PaymentAction
	var unpayable []state.ObjID
	// The planner's "insufficient" is a PROOF only when the source census
	// prices every mana ability p could activate, or when a relaxation of the
	// abilities it misses (paymentPlanRelaxProof) still cannot pay. The cast
	// planner's raw verdict (planCastPaymentMemo) does no such post-processing,
	// so a cast payable only through an uncovered source's scripted prefix
	// (Saruli Caretaker, Wall of Roots, Heap Gate's filter) reaches the builder
	// as "insufficient" while PotentialPaymentPlans proves it payable. Marking
	// that a proof would hide a genuinely playable cast's "tap other mana
	// first" row, so the census gate below is the same proof the planner
	// reports (PotentialPaymentPlans), minus its script search: a
	// census-incomplete, non-relaxable insufficiency is not appended.
	var (
		census   paymentPlanCensus
		censused bool
	)
	provesUnpayable := func(cast decision.PlannedCast) bool {
		verdict := func() PaymentPlanOutcome {
			return e.planCastPaymentMemo(p, cast, &statics, &legal)
		}
		if got := verdict(); got.Reason != pay.ReasonInsufficient {
			return false
		}
		if !censused {
			census, censused = e.paymentPlanCensusOf(p, &hyp), true
		}
		if census.complete {
			return true
		}
		return census.relaxable && e.paymentPlanRelaxProof(census, verdict).Reason == pay.ReasonInsufficient
	}
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
		got := e.planCastPaymentMemo(p, cast, &statics, &legal)
		e.PaymentStats.RecordOutcome(got)
		if got.Plan == nil {
			if got.Reason == pay.ReasonInsufficient && provesUnpayable(cast) {
				unpayable = append(unpayable, opt.Obj)
			}
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
		pid, err := decision.PaymentPlanID(seq, idSeat, cast, plan)
		if err != nil {
			continue // impossible for a rules-built V1 witness; fail closed.
		}
		plan.ID = pid
		aid, err := decision.PaymentActionID(decision.PaymentPlanV1, seq, idSeat, cast)
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
		e.PaymentStats.RecordOffered(len(a.Plans))
	}
	return out, unpayable
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
	defer pay.PaymentPlanQueryEnd(asPayer(e), e.paymentPlanQueryResumeBegin(p))
	got := e.planCastPaymentAtDecision(p, cast)
	// PP-14: a Sac-bearing additional cost is answered by the ordinary in-flow
	// ask AFTER this validation, so the distinct-candidate assignment must
	// still hold at submit time. Re-run the offer gate's own sacrifice
	// feasibility check (nonManaCastable) on the composed cost and reject with
	// a clear error when a candidate left the battlefield between offer and
	// submit; the seat then falls back to the manual window instead of
	// committing a cast whose additional cost can no longer be paid.
	if o := e.G.Obj(cast.Object); o != nil && o.Face() != nil {
		composed := e.offerCostFor(p, cast.Object, pay.WithSpellAbilityExtras(o.Face(), pay.RawBaseCost(asPayer(e), p, cast.Object)), spellScope(""))
		if len(composed.Sac) != 0 && !pay.NonManaCastable(asPayer(e), p, cast.Object, composed, false, "") {
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
	units := pay.PaymentPlanQueryUnits(asPayer(e), p)
	pool := e.G.Players[p].Pool
	produced := state.Mana{}
	var pain int64
	lastResort := false
	seen := make(map[state.ObjID]bool, len(plan.Activations))
	for _, pa := range plan.Activations {
		if seen[pa.Source] {
			return fmt.Errorf("payment source %d reused", pa.Source)
		}
		if pa.SourceZoneSeq != pay.PaymentSourceZoneSeq(asPayer(e), pa.Source) {
			return fmt.Errorf("payment source %d changed zone incarnation", pa.Source)
		}
		seen[pa.Source] = true
		step, ok := pay.PaymentPlanStepAlternative(asPayer(e), units, pa)
		if !ok {
			return fmt.Errorf("payment activation is no longer eligible")
		}
		pain += pay.ConsequencePain(step.Consequence)
		lastResort = lastResort || step.Tier == pay.TierLastResort
		pool = pay.ManaAdd(pool, step.Mana)
		produced = pay.ManaAdd(produced, step.Mana)
	}
	// The lethal guard and the phase rule (spec 5): a witness never kills its
	// caster, and it uses a last-resort source only when no plan from normal
	// sources exists -- exactly when the planner's own best plan uses one.
	if pain > 0 && pain >= int64(e.G.Players[p].Life) {
		return fmt.Errorf("payment plan life and damage would be lethal")
	}
	if lastResort && !pay.PlanUsesLastResort(*got.Plan) {
		return fmt.Errorf("payment plan uses a last-resort source while a normal plan exists")
	}
	cost := pay.PlanManaHalf(e.offerCostFor(p, cast.Object, pay.WithSpellAbilityExtras(e.G.Obj(cast.Object).Face(), pay.RawBaseCost(asPayer(e), p, cast.Object)), spellScope("")))
	payment, ok := resolveManaWith(cost, pool, state.Mana{}, [7]state.Mana{}, e.G.Players[p].Life, false, pipRider{}, nil)
	expected := pay.Witness(cost, e.G.Players[p].Pool, produced, nil, payment.Pool)
	if !ok || pay.ManaAmount(payment.Pool) != plan.PoolAfter || expected.PoolSpend != plan.PoolSpend {
		return fmt.Errorf("payment witness does not settle")
	}
	return nil
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
	return e.paymentPlanManaUnitsOnly(p, nil)
}

// paymentPlanManaUnitsOnly is paymentPlanManaUnits restricted to the
// sources in only (nil: every source). Every layer below computes a
// source's unit from that source alone (the evaluated-amount probe is the
// whole board tapped, whichever source asks), so the restricted census is
// exactly the full census's units for those sources, in the same order;
// verify mode (walkCacheVerify) compares the two.
func (e *Engine) paymentPlanManaUnitsOnly(p state.PlayerID, only []state.ObjID) []windowManaUnit {
	units := e.paymentPlanManaUnitsOnlyCompute(p, only)
	if walkCacheVerify && only != nil {
		var want []windowManaUnit
		for _, u := range e.paymentPlanManaUnitsOnlyCompute(p, nil) {
			if slices.Contains(only, u.ID) {
				want = append(want, u)
			}
		}
		if !pay.SameUnits(units, want) {
			panic(fmt.Sprintf("payment plan census: restricted census for %v is not the full census's", only))
		}
	}
	return units
}

func (e *Engine) paymentPlanManaUnitsOnlyCompute(p state.PlayerID, only []state.ObjID) []windowManaUnit {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	zone := e.G.Zone(state.ZBattlefield, p)
	// Each of p's sources' payment-window abilities, read once here for
	// both the shared census (windowManaUnitsWith) and the layers below:
	// the shared census reads every source with a face, tapped or not.
	windowMas := make([][]*cards.SA, len(zone))
	for zi, id := range zone {
		if only != nil && !slices.Contains(only, id) {
			continue
		}
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			windowMas[zi] = e.availableManaAbilitiesForWindow(p, id, false)
		}
	}
	units := e.windowManaUnitsWith(p, only, windowMas)
	for zi, id := range zone {
		if only != nil && !slices.Contains(only, id) {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil {
			continue
		}
		idx := -1
		for i := range units {
			if units[i].ID == id {
				idx = i
				break
			}
		}
		for _, ma := range windowMas[zi] {
			mf := e.manaStaticOf(ma)
			raw := mf.Produced
			amt := mf.Amount
			if amt <= 0 {
				continue
			}
			counts, any := mf.Counts, mf.Any
			if !any {
				// Fixed production whose cost is not a bare tap (Eldrazi
				// Spawn's Sac<1/CARDNAME>: Add {C}) is outside the shared
				// census, which lists free-cost abilities only. Such an
				// ability is never normal (paymentPlanTapOnlyCost); the tier
				// gate in paymentPlanUnitAlternatives keeps it only when it is
				// a last-resort shape.
				if mf.FreeCost || mf.RestrictValid {
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
					units = append(units, windowManaUnit{ID: id})
					idx = len(units) - 1
				}
				units[idx].Alts = append(units[idx].Alts, windowManaAlt{Ma: ma, Counts: counts, Amt: amt})
				continue
			}
			// The shared census keeps only deterministic production; extend it
			// with the finite choice shapes V1 can make concrete: Produced$ Any
			// (one alternative per colour, any literal amount) and an amount-1
			// Combo/Chosen/ColorIdentity (one alternative per producible
			// colour). Combo Any and every allocation (amount > 1) stay deferred.
			if raw != "Any" && !pay.PaymentPlanChoiceShape(raw) {
				continue
			}
			if raw != "Any" && amt != 1 {
				continue
			}
			if idx < 0 {
				units = append(units, windowManaUnit{ID: id})
				idx = len(units) - 1
			}
			units[idx].Alts = append(units[idx].Alts, windowManaAlt{Ma: ma, Counts: counts, Amt: amt, Any: true})
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
		if only != nil && !slices.Contains(only, id) {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil {
			continue
		}
		if walkCacheVerify {
			if fresh := e.availableManaAbilitiesForWindow(p, id, false); !slices.EqualFunc(fresh, windowMas[zi], pay.SameManaAbility) {
				panic(fmt.Sprintf("payment plan census: window membership for %d moved inside the census", id))
			}
		}
		for _, ma := range windowMas[zi] {
			mf := e.manaStaticOf(ma)
			if mf.Amount > 0 {
				continue // windowManaUnits' static path already priced it.
			}
			// Only a V1 source contract (a bare tap) can be executed from a
			// witness, so never build the probe for an ability the plan could
			// not activate anyway.
			if !mf.TapOnly {
				continue
			}
			if probe == nil {
				probe = e.paymentPlanTappedProbe(p)
			}
			amt, ok := e.paymentPlanStableAmount(probe, p, id, o, ma)
			if !ok {
				continue
			}
			mp := effects.ManaOf(ma)
			counts, any := mp.Counts, mp.CountsAny
			if any {
				units = pay.AppendUnitAlt(units, id, windowManaAlt{Ma: ma, Counts: counts, Amt: amt, Any: true})
				continue
			}
			total := int32(0)
			for _, n := range counts {
				total += n
			}
			if total <= 0 {
				continue
			}
			units = pay.AppendUnitAlt(units, id, windowManaAlt{Ma: ma, Counts: counts, Amt: amt})
		}
	}
	return units
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

type planCastPaymentCheckedCode uint16

const (
	planCastPaymentCheckedHand planCastPaymentCheckedCode = iota + 1
	planCastPaymentCheckedCommandZone
)

var planCastPaymentCheckedCodes = state.NewStrCodes(
	state.StrEntry[planCastPaymentCheckedCode]{Key: "hand", Val: planCastPaymentCheckedHand},
	state.StrEntry[planCastPaymentCheckedCode]{Key: "command_zone", Val: planCastPaymentCheckedCommandZone},
)
