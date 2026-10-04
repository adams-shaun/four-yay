package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// commanderTaxFor composes base with the CR 903.8 commander tax: an
// additional generic {2} -- on top of everything else -- for each previous
// time id has been cast from the command zone this game. It is the ONE place
// a command-zone cast's extra cost is composed, and BOTH sides of the
// two-sided gate apply it to the same base: the legality offer (the
// command-zone walk in rules/legal.go calls commanderTaxFor(base) before
// castable) and the payment (rules/cast.go's beginCast applies
// commanderTaxFor to the cost it resolves). Because both derive the taxed
// cost from this same function over
// the same board, an offered command-zone cast and the cost it charges are
// structurally incapable of disagreeing -- the no-progress spin
// stalledCastLimit bounds is exactly what that disagreement used to cause.
//
// base is expected to be the already-reduced output of adjustedCost (kicker/
// surge/flashback recast it afterwards, but a card in the command zone is
// only ever cast here by its plain cost). Applying the tax to base means cost
// reductions land BEFORE it and never spill onto it: an additional cost is
// outside a plain "cost to cast" reduction, exactly how Kicker's own
// additional {N} composes in beginCast. Colored requirements are untouched.
//
// The Commander-format gate is explicit, not incidental: outside the format
// -- even when a card sits in the (usually empty) command zone -- base passes
// through unchanged. A card that is not its owner's commander, or a commander
// no longer in the command zone, is likewise untaxed.
func (e *Engine) commanderTaxFor(p state.PlayerID, id state.ObjID, base Cost) Cost {
	base.Generic += e.commanderTaxAmount(p, id)
	return base
}

// commanderTaxAmount is the CR 903.8 commander-tax generic amount for a
// command-zone commander cast by p: 2 per prior command-zone cast of id, 0
// for every other card, zone or format. It is the single O(1) tax read; both
// commanderTaxFor (the offer side) and beginCast's taxGeneric capture use it.
func (e *Engine) commanderTaxAmount(p state.PlayerID, id state.ObjID) int32 {
	if e.format != FormatCommander {
		return 0
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZCommand {
		return 0
	}
	for k, cid := range e.G.Players[p].Commanders {
		if cid != id {
			continue
		}
		if n := e.G.Players[p].CmdCasts[k]; n > 0 {
			return 2 * n
		}
		return 0
	}
	return 0
}

// rawBaseCost is id's printed mana cost, without any cost modifier applied:
// the CR 601.2f "mana cost or alternative cost" basis onto which the chosen
// {X} and the RaiseCost/ReduceCost composition (manaToPay) are built. A
// missing object or a Face()-less one degrades to the zero Cost rather than
// panicking, matching adjustedCost's own guard.
func (e *Engine) rawBaseCost(p state.PlayerID, id state.ObjID) Cost {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}
	}
	return e.faceCost(o.Face())
}

// castOfferBase is the composed RAW base every ordinary cast offer is gated on:
// the printed mana cost with CR 702.51 Convoke's creatures and CR 702.66
// Improvise's artifacts credited as generic, in that order (improviseCost
// excludes convokeTaps so one permanent is never committed twice). It is ONE
// helper so the plain cast offer (rules/legal.go's hand walk) and the
// MayFlashCost alternate offer (rules/mayflash.go) cannot drift: the mayflash
// offer charges the same base PLUS its extra cost, and both must price the
// printed cost identically. Returns a value (not the committed taps) because
// the offer gate only needs the credit; the actual commitment is re-derived
// per cast by convokeAsk.
func (e *Engine) castOfferBase(p state.PlayerID, id state.ObjID) Cost {
	// Without either keyword both credits are the identity (convokeCost and
	// improviseCost return the cost they were handed), so the raw base is
	// the answer and the two Cost round trips are skipped.
	if !e.hasCastConvoke(id) && !e.hasCastImprovise(id) {
		return e.rawBaseCost(p, id)
	}
	base, taps := e.convokeCost(p, id, e.rawBaseCost(p, id))
	base, _ = e.improviseCost(p, id, base, taps)
	return base
}

// offerCostFor is the CR 601.2f-composed cost an offer is gated on: the
// selected base cost (a spell's printed mana cost, or an alternative/
// flashback/surge/kicker cost) with RaiseCost then ReduceCost applied to
// Generic, and (for a spell) the CR 903.8 commander tax added last because an
// additional cost is never reduced. {X} is not yet chosen at offer time, so it
// contributes zero generic here and is not reduced; the offer stays
// conservative (a spell offering itself is withheld only when even X=0 is
// unpayable) while the actual charge (manaToPay) applies the modifiers after
// X is folded -- the two never disagree on a card with no {X} in its cost.
func (e *Engine) offerCostFor(p state.PlayerID, id state.ObjID, base Cost, scope costScope) Cost {
	return e.offerCostForUsing(e.collectCostStatics(), p, id, base, scope)
}

func (e *Engine) offerCostForUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, base Cost, scope costScope) Cost {
	return e.composedOfferCost(p, id, base, e.costModifiersWithTargetsUsing(statics, p, id, scope, nil, false), scope)
}

// composedOfferCost is offerCostFor with the modifier collection factored
// out, so offerCastable can evaluate the composition once and reuse it for
// both the per-face enumeration and the composed castable check.
func (e *Engine) composedOfferCost(p state.PlayerID, id state.ObjID, base Cost, mods costMods, scope costScope) Cost {
	var c Cost
	e.composedOfferCostInto(&c, p, id, &base, &mods, scope)
	return c
}

// composedOfferCostInto is composedOfferCost writing the composition into
// *dst, reading *base and *mods in place (neither is written).
func (e *Engine) composedOfferCostInto(dst *Cost, p state.PlayerID, id state.ObjID, base *Cost, mods *costMods, scope costScope) {
	*dst = *base
	mods.ApplyTo(dst)
	if scope.Kind != "Ability" && scope.Kind != "Foretell" && scope.Kind != "Static" {
		*dst = e.commanderTaxFor(p, id, *dst)
	}
}

// offerCastable is THE offer-side gate every cast/activation option is gated
// on. base is the RAW (pre-modifier) cost beginCast stores in pendingCast
// for this exact option and scope the costScope its modifiers are collected
// with, so the gate composes the very charge the payment will make: the
// scope's CR 601.2f modifiers over base, then (for a spell) the CR 903.8
// commander tax, never reduced by either.
//
// The mana feasibility question is posed over the still-unresolved flexible
// pip faces (costMods.feasibleAny): with a SetCost/MinMana floor in the
// composition, CR 202.4b's generic-face mana value of an unresolved twobrid
// pip can overprice the cheaper face the announcement resolves it to, and a
// composed-payable offer would then have no legal announcement. castable on
// the composed cost runs on top, supplying the non-mana parts (Sac/Discard/
// SubCounter/Tap) the enumeration does not model; composed and per-face
// feasibility are conjunctive, and the stricter composed answer can only
// withhold a legal offer (the safe direction), never offer an illegal one.
func (e *Engine) offerCastable(p state.PlayerID, id state.ObjID, base Cost, scope costScope, ability bool) bool {
	return e.offerCastableUsing(e.collectCostStatics(), p, id, &base, scope, ability, nil)
}

// offerCastableUsing is offerCastable's core with the statics collected
// once (the walk shares one collection) and the mana pool optionally
// overridden: hyp nil is the ordinary real-pool gate, hyp non-nil prices the
// mana feasibility against the potential-action walk's hypothetical bound
// (the pool the seat would hold after floating every untapped source) while
// every non-mana read stays real. The two modes share one body, so the walk
// cannot drift from the offer it mirrors.
//
// base is read in place and never written: a step that reshapes the cost
// (fixLifeXCost, the XMin$ floor) works on a local copy.
func (e *Engine) offerCastableUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, base *Cost, scope costScope, ability bool, hyp *state.Mana) bool {
	// The SVar-fixed PayLife<X> conversion (fixLifeXCost) shapes the cost the
	// gate prices into the exact cost the payment will store (beginCast and
	// beginActivation convert through the same helper), so an offered cost and
	// the charge can never disagree about the life part. A face whose SVar:X
	// body is present but unresolvable is WITHHELD here -- the fail-closed
	// direction the rest of the cost grammar takes -- rather than offered with
	// an arbitrary announcement.
	// fixLifeXCost is the identity on a cost with no PayLife<X> part.
	var local Cost
	if len(base.LifeX) > 0 {
		var ok bool
		if local, ok = pay.FixLifeXCost(asPayer(e), p, id, *base); !ok {
			return false
		}
		base = &local
	}
	// The activated ability's own XMin$ parameter (task cost:xmin-param): the
	// same announcement floor xAsk folds from the ability being activated,
	// read here off scope.ab -- the exact printed or granted SA the offer walk
	// scoped. The fold RAISES the cost's own XMin<N> bound by maximum and
	// stops there: feasibleAny's bound pricing (WithX at the smallest legal
	// announcement) composes the cheapest legal price from it, so the offer
	// gate and xAsk share one floor answer and an unannounced cost still
	// reports no charge. A cost that announces no X binds nothing: no {X} pip
	// and no announced-X part means no announcement exists to floor.
	if scope.Ab != nil && costAnnouncesX(*base) {
		if n := xMinAbilityParam(scope.Ab); n > base.XMin {
			if base != &local {
				local = *base
				base = &local
			}
			base.XMin = n
		}
	}
	// mayApply/provenance capture what the potential-target retry below
	// needs to know of this pass (offerRetryFutile).
	mayApply := false
	mods := e.costModifiersCompose(statics, p, id, scope, nil, false, 0, &mayApply)
	provenance := e.costProvenanceSeen
	// A Waterbend<N>/<X> part carried by the cost itself (an ability's own
	// Cost$ like Giant Koi's, or a spell's optional-cost part) credits the
	// same taps a RaiseCost Waterbend does (mods.waterbend), so the offer is
	// priced with the help the payment will offer.
	if base.Waterbend > 0 {
		mods.Waterbend = addClampedGeneric(mods.Waterbend, int64(base.Waterbend))
	}
	if base.WaterbendX {
		mods.WaterbendX = true
		mods.WaterbendPartX++
	}
	// withWaterbendOfferCredit returns mods unchanged unless some waterbend
	// credit is wanted (its want is 0 when both counts are).
	if mods.Waterbend != 0 || mods.WaterbendPartX != 0 {
		mods = e.withWaterbendOfferCredit(p, id, base.XMin, mods)
	}
	tax := int32(0)
	if scope.Kind != "Ability" && scope.Kind != "Foretell" && scope.Kind != "Static" {
		tax = e.commanderTaxAmount(p, id)
	}
	delve := int32(0)
	if e.hasKeywordH(id, kwhDelve) {
		delve = int32(len(e.G.Zone(state.ZGraveyard, p)))
	}
	if !e.manaFeasiblePricedP(p, id, ability, base, &mods, tax, delve, hyp) {
		// A target-dependent reducer cannot be in the ordinary pre-target
		// snapshot, but it may make one legal target choice payable. Retry with
		// exactly those potential reductions; target-dependent raises/floors
		// remain absent until the actual target is known (see the helper's
		// contract).
		//
		// The two passes differ ONLY through ValidTarget$: it is the one
		// place costStaticApplies reads the targets, and the potential pass
		// skips just the ValidTarget$ raises/floors. With no cost static
		// carrying the key, both passes compose the same modifier set, so the
		// retry would re-ask the exact question that just failed: the target
		// census (a pure read) is skipped, not changed.
		var potential costMods
		potentialOK := false
		if futile := pay.OfferRetryFutile(scope, &mods, mayApply, provenance); statics.validTarget && (!futile || walkSkipVerify) {
			potential, potentialOK = e.potentialCostModsUsing(statics, p, id, scope, e.costPotentialTargets(p, id, scope), 0, func(m costMods) bool {
				if !e.manaFeasiblePriced(p, id, ability, *base, m, tax, delve, hyp) {
					return false
				}
				var c Cost
				e.composedOfferCostInto(&c, p, id, base, &m, scope)
				return e.nonManaCastableP(p, id, &c, ability, tapCostSAKind(scope.Ab))
			})
			if futile && potentialOK {
				panic(fmt.Sprintf("rules: futile potential-target retry for obj %d accepted %+v", id, potential))
			}
		}
		if potentialOK {
			mods = potential
		} else if accepted, ok := e.offerSacXModsGated(p, id, ability, base, statics, scope, tax, delve, hyp); ok {
			// The cost announces a Sac<X/Spec> count whose resulting X-dependent
			// reduction (Dargo's "{2} less for each permanent sacrificed this
			// way", read through Count$xPaid) can make the cast payable at a
			// nonzero X the X=0 snapshot misprices. Accept when, and only when,
			// SOME legal announcement is payable -- the same announced-X
			// recomputation manaToPay makes after the announcement, applied at
			// the gate so the offer and the charge agree.
			mods = accepted
		} else if accepted, ok := e.offerNamedModsGated(p, id, ability, base, &mods, statics, scope, tax, delve, hyp); ok {
			// A RaiseCost part counted by a named announcement (Explosive
			// Singularity's "tap any number of untapped creatures ... costs
			// {1} less for each creature tapped this way") can make the cast
			// payable at a nonzero announcement the 0 snapshot misprices --
			// the offerSacXMods sweep, for the named count.
			mods = accepted
		} else {
			return false
		}
	}
	// feasibleAny has established the mana half for a specific announced face
	// when a floor or Color$ reduction is face-sensitive. Do not re-check
	// that result against the unresolved Cost: Color$ W can make the W half
	// of {W/U} free, while applying it before that half is chosen sees no W
	// pip at all. The remaining cost parts are face-independent, so this
	// shared tail preserves every Sac/Discard/counter/tap legality check.
	var composed Cost
	e.composedOfferCostInto(&composed, p, id, base, &mods, scope)
	return e.nonManaCastableP(p, id, &composed, ability, tapCostSAKind(scope.Ab))
}

// offerSacXMods is the offer gate's announced-sacrifice-count affordability
// sweep. A cost carrying a Sac<X/Spec> part announces its count as X (CR
// 601.2b), and a ReduceCost static that reads the paid X (Dargo, the
// Shipwrecker's SVar:X:Count$xPaid over SVar:Y:SVar$X/Times.2) is evaluated
// with X=0 in the ordinary pre-announcement snapshot -- so a cast the player
// can only afford AFTER reducing the cost is never offered. This walks every
// legal announcement (1..the matching permanents the payer can sacrifice,
// the same bound xAsk computes) and reprices exactly the way manaToPay's
// post-announcement recomputation does: the announced cost (base folded at X)
// under the X-bound modifiers, then the commander tax. It returns the
// modifier snapshot of the first feasible announcement, so the caller's
// composed cost and payment agree; ok=false means no legal announcement is
// payable and the cast must stay withheld (the fail-closed direction, never
// a bypass of mana feasibility for every Sac<X> spell). X=0 is deliberately
// not swept here -- the caller already priced it, and an all-zero reduction
// would merely repeat that answer.
func (e *Engine) offerSacXMods(p state.PlayerID, id state.ObjID, ability bool, base Cost, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	if !pay.CostAnnouncesSacX(base) {
		return costMods{}, false
	}
	// Every announced Sac part pays the SAME X. The smallest candidate
	// pool bounds the search, but overlapping pools may require more
	// distinct objects than either pool alone can supply.
	maxX := int32(0)
	boundSet := false
	for _, part := range base.Sac {
		if !part.Announced {
			continue
		}
		n := int32(len(pay.SacrificeCostCandidates(asPayer(e), p, id, part, ability)))
		if !boundSet || n < maxX {
			maxX = n
			boundSet = true
		}
	}
	if maxX <= 0 {
		return costMods{}, false
	}
	// The potential-target variant is the same "does SOME legal announcement
	// exist" relaxation the ordinary retry above makes; target-dependent
	// raises and floors stay absent (they can only raise the price).
	targets := e.costPotentialTargets(p, id, scope)
	for x := int32(1); x <= maxX; x++ {
		if !pay.SacrificeCostAssignable(asPayer(e), p, id, base.Sac, ability, x) {
			continue
		}
		announced := base.WithX(x)
		mods := e.costModifiersWithTargetsXUsing(statics, p, id, scope, nil, false, x)
		if e.manaFeasiblePriced(p, id, ability, announced, mods, tax, delve, hyp) {
			return mods, true
		}
		if len(targets) == 0 {
			continue
		}
		if candidateMods, ok := e.potentialCostModsUsing(statics, p, id, scope, targets, x, func(m costMods) bool {
			return e.manaFeasiblePriced(p, id, ability, announced, m, tax, delve, hyp)
		}); ok {
			return candidateMods, true
		}
	}
	return costMods{}, false
}

// costPotentialTargets returns the legal target candidates that can make a
// target-conditional reduction available at offer time. The final selection
// is still repriced before payment; this is only the "does SOME legal
// announcement exist" half of CR 601.2. Modal spells have no selected mode
// yet and therefore conservatively contribute no potential discount.
func (e *Engine) costPotentialTargets(p state.PlayerID, id state.ObjID, scope costScope) []state.Target {
	sa := pay.CostTargetingSA(e.G, id, scope)
	if sa == nil || !effects.TargetsOf(sa).Targeted() || sa.ParamStr(cards.PKChoices) != "" {
		return nil
	}
	var excludeSelf state.ObjID
	if scope.Kind != "Ability" {
		excludeSelf = id
	}
	candidates := e.legalTargetCandidates(p, id, excludeSelf, sa)
	out := make([]state.Target, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.kind == "player" {
			out = append(out, state.Target{Player: candidate.player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: candidate.obj})
		}
	}
	return out
}

// costAmountTargets trims a potential-target census to a COMPLETE LEGAL TARGET
// ASSIGNMENT before a target-relative Amount$ reads it. The offer gate's
// potential pass hands the whole costPotentialTargets census to the modifier
// composition so every ValidTarget$/ValidSpell$ rule can match against SOME
// candidate, but an Amount$ that counts the cast's targets (Battlefield
// Thaumaturge's TargetedObjectsDistinct, "for each creature it targets") must
// see only as many targets as the declaration actually announces -- pricing
// every legal candidate at once would reduce the cost by the census size and
// offer a cast the table can never complete. The size is resolvedTargetBounds'
// own maximum (the most targets a reduction reading the count can see, and so
// the reduction-favourable witness the existential offer gate wants), capped
// at the census. A resolved maximum of 0 (the "instead" idiom) yields the
// empty assignment, never a target the declaration may not announce. Only the
// potential pass calls this: a non-potential composition already carries the
// announced targets, which are a legal assignment by construction. The chosen
// targets are always repriced at CR 601.2c/h.
func (e *Engine) costAmountTargets(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target) []state.Target {
	if len(targets) == 0 {
		return targets
	}
	sa := pay.CostTargetingSA(e.G, id, scope)
	if sa == nil {
		return targets
	}
	_, max := e.resolvedTargetBounds(p, id, sa, 0)
	// A resolved maximum of 0 (the "instead" idiom) must yield the empty
	// assignment even when the census holds a single candidate: the bound is
	// resolved BEFORE the size fast path so one candidate can never stand in
	// for a target the declaration is not allowed to announce.
	if max == 0 {
		return nil
	}
	if max < 0 || max >= len(targets) {
		return targets
	}
	return targets[:max]
}

// SpellEffectiveCost returns id's non-X own spell cost after offer-time cost
// modifiers, in Forge notation. It is scoped to the hand -- the only zone the
// cast-offer walk (rules/legal.go) prices -- so a battlefield permanent or a
// graveyard flashback card never carries an "effective cast cost" it cannot
// use. It deliberately declines alternative/dynamic shapes rather than
// presenting an incomplete amount.
func (e *Engine) SpellEffectiveCost(p state.PlayerID, id state.ObjID) string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || o.Zone != state.ZHand {
		return ""
	}
	f := o.Face()
	if f.SpellAbility() == nil {
		return ""
	}
	base := e.parseCost(f.ManaCost)
	if base.X != 0 || base.XMin != 0 || base.WaterbendX {
		return ""
	}
	// The own-cost projection does not price alternate cast faces or extra
	// costs. The ordinary face spell shape is the only supported case here.
	if f.SpellAbility().ParamStr(cards.PKAlternativeCost) != "" || f.SpellAbility().ParamStr(cards.PKCost) != "" {
		return ""
	}
	mods := e.costModifiersWithTargetsUsing(e.collectCostStatics(), p, id, spellScope(""), nil, false)
	if mods.HasExtra || mods.SetFloor != 0 || mods.RaiseX != 0 {
		return ""
	}
	cost := e.offerCostFor(p, id, base, spellScope(""))
	if cost.X != 0 {
		return ""
	}
	// Compare NORMALIZED costs on both sides: f.ManaCost is the raw printed
	// string, whose colour order / pip spelling formatCost may re-render
	// identically-in-meaning but differently in text ({2 G B} -> "2 B G").
	// A raw-vs-normalized compare would then report a spurious "effective"
	// cost for a card no static touches. Normalizing the base first makes an
	// unchanged card read unchanged however its printed string is spelled,
	// while a real RaiseCost/ReduceCost still differs after normalization.
	if formatCost(cost) == formatCost(base) {
		return ""
	}
	return formatCost(cost)
}

// AbilityCosts returns id's non-mana activated-ability costs after the same
// offer-time RaiseCost/ReduceCost composition legalActions applies. The order
// is the face's authored ability order. This is a projection helper: it emits
// no event and mutates no game state.
func (e *Engine) AbilityCosts(p state.PlayerID, id state.ObjID) []string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	var out []string
	for _, ab := range o.Face().Abilities {
		if ab.Kind != "AB" || cards.IsManaAbilityAPI(ab.API) {
			continue
		}
		out = append(out, e.abilityOfferCost(p, id, ab))
	}
	return out
}

// abilityOfferCost is one activated ability's offer-time cost in Forge
// notation: the printed Cost$ with its own ReduceCost$ and the applicable
// RaiseCost/ReduceCost statics composed. AbilityCosts projects it onto the
// card, and legalActionsPriced stamps it on the ability's "ability" option,
// so the two can never disagree about what an activation will charge.
func (e *Engine) abilityOfferCost(p state.PlayerID, id state.ObjID, ab *cards.SA) string {
	cost := e.parseCost(ab.ParamStr(cards.PKCost))
	// The ability's own ReduceCost$ (Otawara's Channel): the same
	// composition the offer gate and beginActivation's charge apply, so
	// the decision's displayed cost is the cost the payment will charge.
	if n := e.ownReduceCost(p, id, ab, nil, nil, 0); n > 0 && cost.Generic >= n {
		cost.Generic -= n
	} else if n > 0 {
		cost.Generic = 0
	}
	cost = e.powerUpReducedCost(id, ab, cost)
	return formatCost(e.offerCostFor(p, id, cost, abilityScope(ab)))
}

// manaActivationCostMarker is the wire marker fb-led1 adds to a priority
// window's "activate" option (decision.Option.Cost): the formatCost rendering
// of the FIRST available mana ability (face order, deterministic) whose cost
// is more than a bare tap, "" when every available ability is a bare tap —
// so a plain land's option carries no field and every existing option list
// serialises byte-identically. The engine already gates the offer through
// manaAbilityPayable, which parses and prices the whole cost, so the marker
// costs no new rules knowledge at the offer site; it exists purely so a
// client that (rightly) does not count a bare tap as a play can still tell
// the Lion's Eye Diamond window it must show the player.
//
// The cost is the configured text's compiled parse (compiledCostOf, the same
// ParseCost result frozen), so the per-offer read neither re-parses nor
// copies it; only a marked ability's cost is formatted.
func (e *Engine) manaActivationCostMarker(abilities []*cards.SA) string {
	for _, ma := range abilities {
		// A configured ability's facts carry the same compiled cost
		// (mana_safacts.go), read through its pointer.
		var cc *compiledCost
		if mf := e.manaFactsOf(ma); mf != nil {
			cc = mf.cost
		} else {
			cc = e.compiledCostOf(ma.ParamStr(cards.PKCost))
		}
		if cc.BeyondTap {
			return cc.Formatted()
		}
	}
	return ""
}

// PayLifeInsteadOfB (pay.Engine) reports whether p's side of the battlefield
// carries a Continuous static granting PayLifeInsteadOf:B to p (K'rrik's
// "Affected$ You | AddKeyword$ PayLifeInsteadOf:B"). Every mana payment and
// every cast/activation offer gate consults it, so a plain {B} pip is
// payable with 2 life anywhere K'rrik is in play under its controller.
func (pe *payer) PayLifeInsteadOfB(p state.PlayerID) bool {
	e := (*Engine)(pe)
	for _, sv := range e.activeStatics("Continuous") {
		// A member equal to the keyword needs the keyword as a substring, so
		// the allocation-free substring test rejects every other static
		// before the list is split.
		if raw := sv.ParamStr(cards.PKAddKeyword); !strings.Contains(raw, "PayLifeInsteadOf:B") ||
			!slices.Contains(cards.SplitKeywordList(raw), "PayLifeInsteadOf:B") {
			continue
		}
		if effects.MatchesPlayerSpec(e.G, sv.ParamStr(cards.PKAffected), p, sv.Controller) {
			return true
		}
	}
	return false
}

// MayPlayRider (pay.Engine) reports the may-play payment riders an active
// may-play grant of p's extends to the card id being cast: AnyColor for
// MayPlayIgnoreColor$ True, AnyType for MayPlayIgnoreType$ (Rakdos, the
// Muscle's "mana of any type can be spent to cast those spells"). A rider
// counts when its grant is p's, its AffectedZone names the card's CURRENT
// zone (so a card being cast the ordinary way from hand never inherits an
// exile grant), and the Affected$ spec matches the card. The IsRemembered
// predicate inside a grant's spec is matched against the CONTINUOUS
// EFFECT's Remembered set (the cards the delivering Effect captured),
// through the SpecContext the ordinary filter grammar already carries -- the
// same direct-list reading restrictionApplies uses for Effect-delivered
// CantTarget/CantRegenerate.
func (pe *payer) MayPlayRider(p state.PlayerID, id state.ObjID) pipRider {
	e := (*Engine)(pe)
	var r pipRider
	o := e.G.Obj(id)
	if o == nil {
		return r
	}
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if !ce.MayPlay || ce.Controller != p {
			continue
		}
		color, typ := ce.MayPlayIgnoreColor && !r.AnyColor, ce.MayPlayIgnoreType && !r.AnyType
		if !color && !typ {
			continue
		}
		if ce.MayPlayPlayerTurn && e.G.Active != p {
			continue
		}
		zones, all, ok := effects.ParseZones(ce.AffectedZone)
		if !ok && !all {
			continue
		}
		if !all && !slices.Contains(zones, o.Zone) {
			continue
		}
		if e.matchesSpec(ce.Affects, id, e.effectGrantSpecContext(ce)) {
			r.AnyColor = r.AnyColor || color
			r.AnyType = r.AnyType || typ
			if r.AnyColor && r.AnyType {
				return r
			}
		}
	}
	return r
}

// offerSacXModsGated is offerSacXMods behind its own first test
// (costAnnouncesSacX), read through the pointer so the common no-Sac<X> cost
// is refused without copying the cost and the statics into the call.
func (e *Engine) offerSacXModsGated(p state.PlayerID, id state.ObjID, ability bool, base *Cost, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	announced := false
	for i := range base.Sac {
		if base.Sac[i].Announced {
			announced = true
			break
		}
	}
	if !announced {
		return costMods{}, false
	}
	return e.offerSacXMods(p, id, ability, *base, statics, scope, tax, delve, hyp)
}

// offerNamedModsGated is offerNamedMods behind its own first test
// (costHasNamedCount over the composition's extra cost), read through the
// pointers for the same reason.
func (e *Engine) offerNamedModsGated(p state.PlayerID, id state.ObjID, ability bool, base *Cost, mods *costMods, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	named := false
	for i := range mods.Extra.Exile {
		if isNamedCountPart(mods.Extra.Exile[i]) {
			named = true
			break
		}
	}
	for i := 0; !named && i < len(mods.Extra.TapPermanent); i++ {
		named = isNamedCountPart(mods.Extra.TapPermanent[i])
	}
	if !named {
		return costMods{}, false
	}
	return e.offerNamedMods(p, id, ability, *base, *mods, statics, scope, tax, delve, hyp)
}
