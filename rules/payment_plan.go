package rules

// This file deliberately contains no call to emit.  Payment plans are an
// offer-time witness; the planned executor lives in cast.go.

import (
	"fmt"
	"math/bits"
	"reflect"
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
	Out := e.paymentActionsForPriority(p, p, seq, nil)
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
func (e *Engine) paymentActionsForPriority(p, idSeat state.PlayerID, seq uint64, options []decision.Option) []decision.PaymentAction {
	if e.G.Over {
		return nil
	}
	// Every candidate's plan is declined on a pool the planner cannot
	// account for (planCastPaymentChecked), and that verdict reads only the
	// player, so no walk can change the empty result.
	if !pay.PlanPoolOK(&e.G.Players[p]) {
		e.PaymentStats.RecordBuild(true)
		return nil
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
	_, candidates := e.potentialWalkOf(p, false)
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
		got := e.planCastPaymentMemo(p, cast, &statics, &legal)
		e.PaymentStats.RecordOutcome(got)
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
	cost := e.offerCostFor(p, cast.Object, pay.WithSpellAbilityExtras(e.G.Obj(cast.Object).Face(), pay.RawBaseCost(asPayer(e), p, cast.Object)), spellScope(""))
	payment, ok := resolveManaWith(cost, pool, state.Mana{}, [7]state.Mana{}, e.G.Players[p].Life, false, pipRider{}, nil)
	expected := pay.Witness(cost, e.G.Players[p].Pool, produced, nil, payment.Pool)
	if !ok || pay.ManaAmount(payment.Pool) != plan.PoolAfter || expected.PoolSpend != plan.PoolSpend {
		return fmt.Errorf("payment witness does not settle")
	}
	return nil
}

func (e *Engine) planPaymentCost(p state.PlayerID, cast decision.PlannedCast, cost Cost) PaymentPlanOutcome {
	q := e.PlanQuery
	if !q.Valid(e.L) || e.PaymentPlanRelaxed != nil || e.PaymentPlanRelaxedFee != 0 || !plainManaCost(cost) {
		return e.planPaymentCostExcluding(p, cast, cost, nil)
	}
	// Within one query scope (one state), the planner's outcome for a plain
	// mana cost reads the cast only through the hand Demand that excludes
	// it (planPaymentCostWithout's rank context): two casts with the same
	// payer, cost and Demand -- two copies of a card in hand -- plan
	// identically, so the scope serves the first one's outcome, with its
	// own copy of the witness.
	Demand := pay.PaymentPlanHandDemand(asPayer(e), p, cast.Object)
	Key := pay.PlanCostKey{Payer: p, Colored: cost.Colored, Generic: cost.Generic, Demand: Demand}
	for i := range q.Plans {
		if q.Plans[i].Key == Key {
			out := q.Plans[i].Out
			if walkCacheVerify {
				if want := e.planPaymentCostWithDemand(p, Demand, cost, nil, nil, nil); !reflect.DeepEqual(want, out) {
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
	out := e.planPaymentCostWithDemand(p, Demand, cost, nil, nil, nil)
	stored := out
	if out.Plan != nil {
		plan := decision.ClonePaymentPlan(*out.Plan)
		stored.Plan = &plan
	}
	q.Plans = append(q.Plans, pay.PlanCostMemo{Key: Key, Out: stored})
	return out
}

// plainManaCost reports whether c is only coloured and generic mana -- the
// costs whose plan the query scope memoises. Verify mode checks the field
// walk against the whole struct, so a Cost field added later cannot slip
// past it.
func plainManaCost(c Cost) bool {
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
	if walkCacheVerify && ok != reflect.DeepEqual(c, Cost{Colored: c.Colored, Generic: c.Generic}) {
		panic(fmt.Sprintf("payment plan: plainManaCost(%+v) = %v disagrees with the struct", c, ok))
	}
	return ok
}

// planPaymentCostExcluding is planPaymentCost over the census with the
// sources in exclude left out entirely (planPaymentCostWithout's gone).
func (e *Engine) planPaymentCostExcluding(p state.PlayerID, cast decision.PlannedCast, cost Cost, exclude []state.ObjID) PaymentPlanOutcome {
	return e.planPaymentCostWithout(p, cast, cost, nil, exclude, nil)
}

// planPaymentCostWithout is planPaymentCost over the census with three
// kinds of withheld source (PotentialPaymentPlans): a tapped source is one
// the play's own cost taps (the ability's {T}, a tapXType candidate), so it
// keeps only its alternatives whose ability does not tap it (Wall of
// Roots' counter); a kept source is one the play's cost sacrifices, so it
// keeps only its alternatives that leave it on the battlefield (it may tap
// for mana first); a gone source is left out entirely. With none it is
// planPaymentCost exactly, cached classes included; a withheld census
// groups its own classes, because the query cache's classes are keyed by
// payer and phase only.
func (e *Engine) planPaymentCostWithout(p state.PlayerID, cast decision.PlannedCast, cost Cost, tapped, gone, kept []state.ObjID) PaymentPlanOutcome {
	return e.planPaymentCostWithDemand(p, pay.PaymentPlanHandDemand(asPayer(e), p, cast.Object), cost, tapped, gone, kept)
}

// planPaymentCostWithDemand is planPaymentCostWithout over the hand demand
// that excludes the cast (paymentPlanHandDemand), the one place the cast
// enters the plan.
func (e *Engine) planPaymentCostWithDemand(p state.PlayerID, demand [5]int, cost Cost, tapped, gone, kept []state.ObjID) PaymentPlanOutcome {
	defer pay.PaymentPlanQueryEnd(asPayer(e), pay.PaymentPlanQueryBegin(asPayer(e)))
	units := e.paymentPlanQueryUnits(p)
	queryClasses := e.paymentPlanQueryClasses
	ownClasses := func(_ state.PlayerID, _ pay.Tier, choices [][]pay.Alt) []pay.Class {
		return pay.Classes(choices)
	}
	if len(gone) != 0 {
		kept := make([]windowManaUnit, 0, len(units))
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
	withhold := func(alts []pay.Alt) []pay.Alt {
		if len(alts) == 0 {
			return alts
		}
		src := alts[0].Activation.Source
		tap, keep := slices.Contains(tapped, src), slices.Contains(kept, src)
		if !tap && !keep {
			return alts
		}
		var out []pay.Alt
		for _, alt := range alts {
			if tap && e.parseCost(alt.Ma.ParamStr(cards.PKCost)).Tap {
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
	choices := make([][]pay.Alt, len(units))
	for i, u := range units {
		choices[i] = withhold(e.paymentPlanQueryAlternatives(u))
	}
	if len(e.PaymentPlanRelaxed) != 0 {
		// PotentialPaymentPlans' relaxed proof (paymentPlanRelaxProof): the
		// census's uncovered sources as free, never-executed alternatives,
		// withheld exactly like the census's own, and the fees of the paid
		// ones it admits charged as generic.
		cost.Generic += e.PaymentPlanRelaxedFee
		for _, alts := range e.PaymentPlanRelaxed {
			if len(alts) == 0 || slices.Contains(gone, alts[0].Activation.Source) {
				continue
			}
			choices = append(choices, withhold(alts))
		}
		queryClasses = ownClasses
	}
	// Phase 1 (spec 5): normal sources only. If it finds a complete plan,
	// that is the offer and last-resort sources are never considered.
	life := e.G.Players[p].Life
	phase1 := pay.PhaseChoices(choices, pay.TierNormal)
	rankCtx := pay.NewRankContext(choices, demand)
	search := pay.SearchInto(&e.hypPool().planSearch, cost, e.G.Players[p].Pool, life, rankCtx,
		phase1, queryClasses(p, pay.TierNormal, phase1), paymentSearchEnv())
	nodes := search.Nodes
	// Phase 2 runs only when phase 1 PROVES no plan exists (insufficient,
	// not search_limit): normal plus last-resort alternatives, ranked by the
	// irreversible-cost key, never a plan whose summed life + damage would
	// reduce the caster to 0 or less. The rank context is phase 1's: keys 6
	// and 7 read the untapped normal remainder in both phases.
	if search.Best == nil && !search.Limited {
		if phase2 := pay.PlanLastResortChoices(choices, life); phase2 != nil {
			search = pay.SearchInto(&e.hypPool().planSearch, cost, e.G.Players[p].Pool, life, rankCtx,
				phase2, queryClasses(p, pay.TierLastResort, phase2), paymentSearchEnv())
			nodes += search.Nodes
		}
	}
	switch {
	case search.Best != nil && search.Limited:
		return PaymentPlanOutcome{Plan: search.Best, Nodes: nodes, Reason: "search_limit"}
	case search.Best != nil:
		return PaymentPlanOutcome{Plan: search.Best, Nodes: nodes}
	case search.Limited:
		return PaymentPlanOutcome{Reason: "search_limit", Nodes: nodes}
	}
	// The first source diagnostic is read only for the outcome that
	// reports it.
	firstSourceDetail := ""
	for _, u := range units {
		if firstSourceDetail != "" {
			break
		}
		for _, alt := range u.Alts {
			o := e.G.Obj(u.ID)
			p := state.PlayerID(0)
			if o != nil {
				p = o.Controller
			}
			_, _, detail := e.paymentPlanAbilityTier(p, u.ID, alt.Ma)
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
			raw := mf.produced
			amt := mf.amount
			if amt <= 0 {
				continue
			}
			counts, any := mf.counts, mf.any
			if !any {
				// Fixed production whose cost is not a bare tap (Eldrazi
				// Spawn's Sac<1/CARDNAME>: Add {C}) is outside the shared
				// census, which lists free-cost abilities only. Such an
				// ability is never normal (paymentPlanTapOnlyCost); the tier
				// gate in paymentPlanUnitAlternatives keeps it only when it is
				// a last-resort shape.
				if mf.freeCost || mf.restrictValid {
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
			if mf.amount > 0 {
				continue // windowManaUnits' static path already priced it.
			}
			// Only a V1 source contract (a bare tap) can be executed from a
			// witness, so never build the probe for an ability the plan could
			// not activate anyway.
			if !mf.tapOnly {
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

// paymentPlanAbilityTier is the single source-shape authority for automatic
// payment. It is intentionally closed-world: new Forge parameters require an
// explicit review before the planner can rely on them. It folds the ability's
// own shape (paymentPlanAbilityShapeTier) with the triggers and replacements
// that can act on this source's tap or mana (paymentPlanSourceInterference):
// a deferring interference defers the source, and the source's own
// fully determined consequence (City of Brass's damage, Mana Vault's
// doesn't-untap) makes an otherwise normal source last resort.
func (e *Engine) paymentPlanAbilityTier(p state.PlayerID, id state.ObjID, ma *cards.SA) (pay.Tier, pay.Consequence, string) {
	tier, c, detail := e.paymentPlanAbilityShapeTier(p, id, ma)
	if tier == pay.TierDeferred {
		return tier, c, detail
	}
	// A last-resort step always discloses a consequence (spec §4: a present
	// consequence sets at least one field); a shape that classified last
	// resort without one is not a shape the witness can describe.
	if tier == pay.TierLastResort && c == (pay.Consequence{}) {
		return pay.TierDeferred, pay.Consequence{}, "source:last_resort"
	}
	switch it, ic, idetail := e.paymentPlanSourceInterference(id, ma); it {
	case pay.TierDeferred:
		return it, ic, idetail
	case pay.TierLastResort:
		c.Sacrifice = c.Sacrifice || ic.Sacrifice
		c.Life += ic.Life
		c.Damage += ic.Damage
		c.NoUntap = c.NoUntap || ic.NoUntap
		c.ReturnToHand = c.ReturnToHand || ic.ReturnToHand
		return pay.TierLastResort, c, "source:last_resort"
	}
	return tier, c, detail
}

// paymentPlanAbilityShapeTier classifies the ability's own cost, production,
// parameters and SubAbility$ chain.
//
// Without a SubAbility$ the verdict reads only the ability's own Params and
// its compiled cost (paymentPlanShapeTierOf), so a configured ability's
// verdict is computed once with its configured facts (manaSAFacts.shape*)
// instead of walking its parameter map on every census; verify mode
// (manaSAFactsVerify) recomputes the facts on every hit.
func (e *Engine) paymentPlanAbilityShapeTier(p state.PlayerID, id state.ObjID, ma *cards.SA) (pay.Tier, pay.Consequence, string) {
	if ma != nil {
		if f := e.manaFactsOf(ma); f != nil && f.shapeKnown {
			if manaSAFactsVerify {
				tier, c, detail, rider := pay.PaymentPlanShapeTierOf(ma, e.parseCost(ma.ParamStr(cards.PKCost)))
				if rider || tier != f.shapeTier || c != f.shapeCons || detail != f.shapeDetail {
					panic(fmt.Sprintf("rules: configured payment shape for %q disagrees with a recompute", ma.Line))
				}
			}
			return f.shapeTier, f.shapeCons, f.shapeDetail
		}
	}
	var cost Cost
	if ma != nil {
		cost = e.parseCost(ma.ParamStr(cards.PKCost))
	}
	tier, c, detail, rider := pay.PaymentPlanShapeTierOf(ma, cost)
	if !rider {
		return tier, c, detail
	}
	deferred := func(detail string) (pay.Tier, pay.Consequence, string) {
		return pay.TierDeferred, pay.Consequence{}, detail
	}
	if pay.PlanRiderHasTarget(e.G, id, ma) {
		return deferred("source:target")
	}
	if !pay.PaymentPlanTapOnlyCost(cost) {
		return deferred("source:last_resort")
	}
	if d, ok := pay.PaymentPlanDamageRider(asPayer(e), id, ma); ok {
		return pay.TierLastResort, pay.Consequence{Damage: d}, "source:last_resort"
	}
	if pay.PaymentPlanParadiseRider(asPayer(e), id, ma) {
		return pay.TierLastResort, pay.Consequence{ReturnToHand: true}, "source:last_resort"
	}
	return deferred("source:rider")
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
func (e *Engine) paymentPlanUnitAlternatives(u windowManaUnit) []pay.Alt {
	_, alts := e.appendUnitAlternatives(nil, u)
	return alts
}

// appendUnitAlternatives is paymentPlanUnitAlternatives appending into dst
// (a query scope's alternative arena): it returns the grown dst and u's
// alternatives as a capped span of it, or nil when u has none.
func (e *Engine) appendUnitAlternatives(dst []pay.Alt, u windowManaUnit) (grown, alts []pay.Alt) {
	start := len(dst)
	out := dst
	// The source's payer, zone-entry sequence and creature bit are read once
	// for all its alternatives (each a pure read of the source).
	payer := state.PlayerID(0)
	if source := e.G.Obj(u.ID); source != nil {
		payer = source.Controller
	}
	sourceRead := false
	var zoneSeq uint64
	var creature bool
	for _, alt := range u.Alts {
		tier, consequence, _ := e.paymentPlanAbilityTier(payer, u.ID, alt.Ma)
		switch tier {
		case pay.TierNormal:
			if !e.manaStaticOf(alt.Ma).tapOnly {
				continue
			}
		case pay.TierLastResort:
			// The classifier vetted the whole cost and chain: {T} plus the
			// disclosed self-sacrifice/life/self-return parts, or a tap-only
			// cost with a disclosed rider, trigger or replacement.
		default:
			continue
		}
		ab, ok := pay.PaymentAbility(e.G, u.ID, alt.Ma)
		if !ok {
			continue
		}
		if !sourceRead {
			sourceRead = true
			zoneSeq, creature = e.paymentSourceZoneSeq(u.ID), e.IsCreature(u.ID)
		}
		if pay.PlanAltOK(alt) {
			m := alt.Mana()
			if out == nil {
				out = make([]pay.Alt, 0, len(u.Alts))
			}
			out = append(out, pay.Alt{Activation: decision.PaymentActivation{
				Source: u.ID, SourceZoneSeq: zoneSeq, Ability: ab, Produces: pay.ManaAmount(m)},
				Mana: m, Creature: creature, Ma: alt.Ma, Tier: tier, Consequence: consequence})
			continue
		}
		if !alt.Any || alt.Amt <= 0 {
			continue
		}
		for _, col := range pay.PaymentPlanChoiceColours(asPayer(e), u.ID, alt.Ma) {
			i := strings.IndexByte("WUBRG", col[0])
			if i < 0 {
				continue
			}
			var m state.Mana
			m[i] = alt.Amt
			if out == nil {
				out = make([]pay.Alt, 0, len(u.Alts))
			}
			out = append(out, pay.Alt{Activation: decision.PaymentActivation{
				Source: u.ID, SourceZoneSeq: zoneSeq, Ability: ab, Produces: pay.ManaAmount(m)},
				Mana: m, Creature: creature, Ma: alt.Ma, ExecProduced: col, Tier: tier, Consequence: consequence})
		}
	}
	// Preserve flexible sources: rank each selected source by every eligible
	// outcome it could have supplied, rather than by only the outcome the
	// search happened to choose. Flexibility is the number of DISTINCT mana
	// types those outcomes produce (spec 5 key 5), so a Produced$ Any source
	// counts 5, a typed dual 2, and two abilities that both add {U} count 1.
	if len(out) == start {
		return out, nil
	}
	grown = out
	out = out[start:len(out):len(out)]
	var types, all uint8 // bit i = mana index i (W/U/B/R/G/C)
	for _, a := range out {
		for i, n := range a.Mana {
			if n > 0 {
				all |= 1 << i
				if a.Tier == pay.TierNormal {
					types |= 1 << i
				}
			}
		}
	}
	flex, flexAll := bits.OnesCount8(types), bits.OnesCount8(all)
	colours := types &^ (1 << state.MC)
	for i := range out {
		out[i].Flex = flex
		out[i].FlexAll = flexAll
		out[i].Colours = colours
	}
	return grown, out
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
func (e *Engine) paymentPlanStepAlternative(units []windowManaUnit, pa decision.PaymentActivation) (pay.Alt, bool) {
	for _, u := range units {
		if u.ID != pa.Source {
			continue
		}
		for _, candidate := range e.paymentPlanQueryAlternatives(u) {
			if candidate.Ma != nil && candidate.Activation.Ability == pa.Ability && candidate.Activation.Produces == pa.Produces &&
				pay.ConsequenceEqual(candidate.Consequence, pa.Consequence) {
				return candidate, true
			}
		}
	}
	return pay.Alt{}, false
}

// paymentSourceZoneSeq is the existing log sequence of this object's current
// zone entry. Genesis objects have no entry event and use the contract's zero
// sentinel. It reads the engine's incremental zone-entry index
// (payment_zone_entry.go), which answers exactly as the backward scan
// (paymentSourceZoneSeqScan) does.
func (e *Engine) paymentSourceZoneSeq(id state.ObjID) uint64 {
	got := e.zoneEntrySeq(id)
	if walkCacheVerify {
		if want := pay.PaymentSourceZoneSeqScan(asPayer(e), id); got != want {
			panic(fmt.Sprintf("payment zone-entry index: object %d seq %d, log scan %d", id, got, want))
		}
	}
	return got
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
