package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
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
	mods.applyTo(dst)
	if scope.kind != "Ability" && scope.kind != "Foretell" && scope.kind != "Static" {
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

// fixLifeXCost resolves an announced PayLife<X> cost part whose source face
// defines SVar:X with a body that is NOT Count$xPaid. Forge's SVar:X is the
// definition that body gives the cost's X, and exactly two shapes exist in
// the corpus:
//
//   - Count$xPaid (Toxic Deluge, Krumar Initiate's "Cost$ X B T PayLife<X>",
//     every SP face carrying the token): "the announced X" -- the payer
//     announces X freely (bounded by life, xAsk) and the life part settles at
//     the same value the printed {X} folds at. The cost passes through
//     unchanged.
//   - any other resolvable body (Murderous Betrayal's
//     SVar:X:Count$YourLifeTotal/HalfUp, Tornado's
//     SVar:X:Count$CardCounters.VELOCITY/Times.3): the value is FIXED -- the
//     payer announces nothing, and the settle pays exactly the evaluated
//     amount. Each such part is folded into Cost.Life, the fixed additional
//     cost both the payable gates (resolveManaWith's life check) and the
//     settle (payMana's LifeChange) already price and charge -- the same
//     place a RaiseCost's fixed life raise lands, so CR 601.2f's ordering
//     treats it as an additional cost that no reduction ever touches.
//
// The verdict is three-way, and ok=false is WITHHOLD: the SVar:X body is
// present, not Count$xPaid, and either unresolvable (War Room's commander
// colour identity) or negative -- the ability is not offered at all (the
// fail-closed direction ParseUnlessCost and manaAbilityPayable already take)
// rather than offered with an arbitrary announcement the payer cannot be
// held to. A cost pairing the non-xPaid body with a printed {X} or another
// announced part sharing the X (Sac<X/Spec>, PayEnergy<X>,
// SubCounter<X/Kind>) is also withheld: the corpus carries no such face, and
// what the fixed body would mean for a shared announcement is undefined
// here. The idempotence contract matters: offerCastable converts at the
// gate, the payment sites convert again, and a converted cost (no LifeX
// left) early-returns unchanged.
func (e *Engine) fixLifeXCost(p state.PlayerID, id state.ObjID, c Cost) (Cost, bool) {
	if len(c.LifeX) == 0 {
		return c, true
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return c, true
	}
	body, present := o.Face().SVars["X"]
	if !present || strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		return c, true
	}
	if c.X > 0 || costAnnouncesSacX(c) {
		return c, false
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return c, false
		}
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return c, false
		}
	}
	ctx := effects.NewCtxPtr(id, p, effects.CtxInit{SVars: o.Face().SVars})
	n, resolvable := effects.EvalCountOK(e, ctx, body)
	if !resolvable || n < 0 {
		return c, false
	}
	out := c
	out.LifeX = nil
	for range len(c.LifeX) {
		out.Life = addClampedGeneric(out.Life, int64(n))
	}
	return out, true
}

// drawCostCount resolves one Draw cost part's count at payment time. A
// literal part (Dyn == "") is simply N. A dynamic part (Forge's
// Draw<X/Spec>, Champion of Wits' "draw cards equal to its power") reads the
// source face's SVar table: the body named by part.Dyn (SVar:X for
// Draw<X/...>) is evaluated exactly the way fixLifeXCost evaluates its
// PayLife<X> body, with the source object bound as the count context so
// Count$CardPower reads the drawing permanent's own power. ok=false means
// the source face, the SVar, or the body is unavailable -- the cost is
// unpayable (the fail-closed direction), never a silent zero draw.
func (e *Engine) drawCostCount(id state.ObjID, you state.PlayerID, part CostPart) (int32, bool) {
	return e.drawCostCountTrig(id, you, part, nil)
}

// drawCostCountTrig is drawCostCount with an optional fire-time trigger
// context seeded into the evaluation: the triggered-cost window's dynamic
// Draw<X/Spec> part (Hordewing Skaab's "draw cards equal to the number of
// opponents dealt damage this way", SVar:X:TriggeredPlayersTargets$Amount)
// reads the DAMAGE BATCH the triggering event captured, which the bare
// cast-flow context carries nothing of. A nil context is the ordinary
// cast/activation read, unchanged.
func (e *Engine) drawCostCountTrig(id state.ObjID, you state.PlayerID, part CostPart, tcx *effects.TriggerContext) (int32, bool) {
	if part.Dyn == "" {
		return part.N, true
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, false
	}
	body, present := o.Face().SVars[part.Dyn]
	if !present {
		return 0, false
	}
	ctx := effects.NewCtxPtr(id, you, effects.CtxInit{SVars: o.Face().SVars})
	if tcx != nil {
		ctx.TriggerContext = *tcx
	}
	n, resolvable := effects.EvalCountOK(e, ctx, body)
	if !resolvable || n < 0 {
		return 0, false
	}
	return n, true
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
		if local, ok = e.fixLifeXCost(p, id, *base); !ok {
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
	if scope.ab != nil && costAnnouncesX(*base) {
		if n := xMinAbilityParam(scope.ab); n > base.XMin {
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
		mods.waterbend = addClampedGeneric(mods.waterbend, int64(base.Waterbend))
	}
	if base.WaterbendX {
		mods.waterbendX = true
		mods.waterbendPartX++
	}
	// withWaterbendOfferCredit returns mods unchanged unless some waterbend
	// credit is wanted (its want is 0 when both counts are).
	if mods.waterbend != 0 || mods.waterbendPartX != 0 {
		mods = e.withWaterbendOfferCredit(p, id, base.XMin, mods)
	}
	tax := int32(0)
	if scope.kind != "Ability" && scope.kind != "Foretell" && scope.kind != "Static" {
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
		if futile := offerRetryFutile(scope, &mods, mayApply, provenance); statics.validTarget && (!futile || walkSkipVerify) {
			potential, potentialOK = e.potentialCostModsUsing(statics, p, id, scope, e.costPotentialTargets(p, id, scope), 0, func(m costMods) bool {
				if !e.manaFeasiblePriced(p, id, ability, *base, m, tax, delve, hyp) {
					return false
				}
				var c Cost
				e.composedOfferCostInto(&c, p, id, base, &m, scope)
				return e.nonManaCastableP(p, id, &c, ability, tapCostSAKind(scope.ab))
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
	return e.nonManaCastableP(p, id, &composed, ability, tapCostSAKind(scope.ab))
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
	if !costAnnouncesSacX(base) {
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
		n := int32(len(e.sacrificeCostCandidates(p, id, part, ability)))
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
		if !e.sacrificeCostAssignable(p, id, base.Sac, ability, x) {
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
	sa := e.costTargetingSA(id, scope)
	if sa == nil || sa.Params["ValidTgts"] == "" || sa.Params["Choices"] != "" {
		return nil
	}
	var excludeSelf state.ObjID
	if scope.kind != "Ability" {
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

// costTargetingSA returns the spell or activated ability whose ValidTgts$ and
// TargetMin$/TargetMax$ a target-relative cost read shares. A spell's face
// carries the declaration while it is in hand; an activated ability is the
// scope's own SA. This is the ONE derivation costPotentialTargets and
// costAmountTargets use, so the offer census and the amount's legal-assignment
// size can never name different declarations.
func (e *Engine) costTargetingSA(id state.ObjID, scope costScope) *cards.SA {
	if scope.kind == "Static" {
		// A special action (specialActionScope) announces no targets.
		return nil
	}
	if scope.kind == "Ability" {
		return scope.ab
	}
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().SpellAbility()
	}
	return nil
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
	sa := e.costTargetingSA(id, scope)
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
	if f.SpellAbility().Params["AlternativeCost"] != "" || f.SpellAbility().Params["Cost"] != "" {
		return ""
	}
	mods := e.costModifiersWithTargetsUsing(e.collectCostStatics(), p, id, spellScope(""), nil, false)
	if mods.hasExtra || mods.setFloor != 0 || mods.raiseX != 0 {
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
		if ab.Kind != "AB" || isManaAbilityAPI(ab.API) {
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
			cc = e.compiledCostOf(ma.Params["Cost"])
		}
		if cc.beyondTap {
			return cc.formatted()
		}
	}
	return ""
}

// energyPayable reports whether the payer's ENERGY counter total covers the
// cost's fixed energy parts (Forge CostPayEnergy.canPay reads the same total).
// A dynamic PayEnergy<X> part is bounded by that total at its own X ask, so
// this gate makes no assumption about the not-yet-chosen value.
func (e *Engine) energyPayable(p state.PlayerID, c *Cost) bool {
	total := c.EnergyCostTotal()
	return total == 0 || e.G.Players[p].Counter("ENERGY") >= total
}

// pip is one flexible mana demand inside a cost's mana part, as a list of
// alternative payments tried in order. The alternative kinds are exactly the
// mana symbols CR 107.4 knows: one unit of a colour, N generic mana (a
// monocolour hybrid's "2" face), two life (a Phyrexian face), and snow mana
// (a {S} pip, payable only by a mana a snow permanent produced). A pip with
// one colour listed twice is just a strict colour pip.
type pip struct {
	alts []pipAlt
}

type pipAlt struct {
	color   byte  // one unit of this colour (0 = not a colour alternative)
	generic int32 // this many generic mana (0 = not a generic alternative)
	life    int32 // two life (0 = not a life alternative)
	snow    bool  // one snow mana unit
}

// pipRider carries the payer-side may-play riders a cost's pip alternatives
// expand under: anyColor is MayPlayIgnoreColor$ ("mana of any color",
// CR 401.5), anyType is MayPlayIgnoreType$ ("mana of any type", Rakdos, the
// Muscle). The zero value is the plain exact-colour payment.
type pipRider struct {
	anyColor bool
	anyType  bool
}

func expandCostPips(c Cost, dst []pip, bLifeOK bool, rider pipRider) []pip {
	// Size the list once: every pip source below contributes exactly one
	// pip per unit counted here.
	n := len(c.Hybrid) + len(c.Twobrid) + len(c.Phyrexian) + len(c.HybridPhyrexian)
	if c.Snow > 0 {
		n += int(c.Snow)
	}
	for _, letter := range pipLetters {
		if k := c.Colored[state.ManaIndex(letter)]; k > 0 {
			n += int(k)
		}
	}
	// Built into the caller's buffer when it fits (resolveManaWith's stack
	// array), so the common small cost allocates no pip list.
	out := dst[:0]
	if cap(out) < n {
		out = make([]pip, 0, n)
	}
	// The coloured slots including the colourless one: a plain {C} pip is a
	// strict colourless requirement generic must not satisfy by stealing the
	// pool's only colourless, so it is reserved like any coloured pip.
	for _, letter := range pipLetters {
		for n := c.Colored[state.ManaIndex(letter)]; n > 0; n-- {
			// The strict one-colour alternative list is shared read-only
			// (every pip consumer only ranges alts); it is capped at its
			// length, so the K'rrik append below copies rather than
			// writing into the shared array.
			alts := strictColourAlts[state.ManaIndex(letter)][:1:1]
			if rider.anyType {
				alts = anyTypeAlts()
			} else if rider.anyColor && letter != 'C' {
				alts = anyColorAlts()
			}
			if bLifeOK && letter == 'B' {
				alts = append(alts, pipAlt{life: 2})
			}
			out = append(out, pip{alts: alts})
		}
	}
	for _, pair := range c.Hybrid {
		alts := []pipAlt{{color: pair.A}, {color: pair.B}}
		if rider.anyType {
			alts = anyTypeAlts()
		} else if rider.anyColor {
			alts = anyColorAlts()
		}
		out = append(out, pip{alts: alts})
	}
	for _, t := range c.Twobrid {
		var alts []pipAlt
		switch {
		case rider.anyType:
			alts = anyTypeAlts()
		case rider.anyColor:
			alts = anyColorAlts()
		default:
			alts = []pipAlt{{color: t.Col}}
		}
		if t.Generic > 0 {
			alts = append(alts, pipAlt{generic: t.Generic})
		}
		out = append(out, pip{alts: alts})
	}
	for _, letter := range c.Phyrexian {
		alts := []pipAlt{{color: letter}, {life: 2}}
		if rider.anyType {
			alts = append(anyTypeAlts(), pipAlt{life: 2})
		} else if rider.anyColor {
			alts = append(anyColorAlts(), pipAlt{life: 2})
		}
		out = append(out, pip{alts: alts})
	}
	for _, hp := range c.HybridPhyrexian {
		alts := []pipAlt{{color: hp.A}, {color: hp.B}, {life: 2}}
		if rider.anyType {
			alts = append(anyTypeAlts(), pipAlt{life: 2})
		} else if rider.anyColor {
			alts = append(anyColorAlts(), pipAlt{life: 2})
		}
		out = append(out, pip{alts: alts})
	}
	for n := c.Snow; n > 0; n-- {
		out = append(out, pip{alts: []pipAlt{{snow: true}}})
	}
	return out
}

// strictColourAlts holds, per mana index, the one-element strict colour
// alternative list costPips hands every plain coloured pip (read-only).
var strictColourAlts = func() (t [len(pipLetters)][1]pipAlt) {
	for _, letter := range pipLetters {
		t[state.ManaIndex(letter)][0] = pipAlt{color: letter}
	}
	return t
}()

// anyColorAlts is the colour alternatives a coloured pip accepts under the
// may-play ignore-colour rider (MayPlayIgnoreColor$ True, CR 401.5): any of
// the five colours, tried in fixed WUBRG order. A {C} pip never reaches this
// helper: CR 107.4c's "any color" never includes colourless.
func anyColorAlts() []pipAlt {
	return []pipAlt{{color: 'W'}, {color: 'U'}, {color: 'B'}, {color: 'R'}, {color: 'G'}}
}

// anyTypeAlts is the MayPlayIgnoreType$ alternative set: ALL six mana types
// (Rakdos, the Muscle's "mana of any type can be spent to cast those spells"
// — "any type" is every mana type, colourless included, unlike "any color"
// which CR 107.4c keeps away from {C}). Used for every pip kind under the
// anyType rider; coloured pips, {C} pips, hybrids and Phyrexians all widen
// to it.
func anyTypeAlts() []pipAlt {
	return []pipAlt{{color: 'W'}, {color: 'U'}, {color: 'B'}, {color: 'R'}, {color: 'G'}, {color: 'C'}}
}

// manaPayment is what resolveMana found: the pool and its two parallel
// tallies after every pip and the generic requirement were paid, plus any
// Phyrexian face spent. Snow units are always consumed alongside their pool
// slot (Snow[i] never exceeds Pool[i]); typed units (task castfilter2) are
// consumed alongside theirs (TypedMana[k][i] never exceeds Pool[i]).
type manaPayment struct {
	pool      state.Mana
	snow      state.Mana
	typed     [7]state.Mana
	lifeSpent int32
}

// takeUnit consumes one mana unit from slot i of rem/sn/typed, preferring a
// PLAIN unit when one exists, then the typed units in their fixed
// Treasure > Cave > Desert order, and a SNOW unit LAST so a snow unit stays
// available for a later {S} pip (typed units have no pips of their own, so
// they go before snow but after plain); the backtracking search undoes the
// choice if the rest of the cost cannot be paid that way. plain is the
// slot's untagged remainder; each tally is <= the pool by construction.
func takeUnit(rem, sn *state.Mana, typed *[7]state.Mana, i int) {
	plain := (*rem)[i] - (*sn)[i]
	for t := range *typed {
		plain -= (*typed)[t][i]
	}
	if plain > 0 {
		(*rem)[i]--
		return
	}
	for t := range *typed {
		if (*typed)[t][i] > 0 {
			(*rem)[i]--
			(*typed)[t][i]--
			return
		}
	}
	(*rem)[i]--
	(*sn)[i]--
}

// resolveMana finds a concrete payment of the cost's mana and fixed-life
// parts from pool, the pool's parallel snow tally and the payer's life,
// preferring to spend coloured pool mana over life for a Phyrexian pip and
// the first alternative of each pip, so the assignment is deterministic. It
// returns the payment with the coloured pips and generic requirement spent,
// and whether the whole cost is payable. The generic requirement is paid
// last from whatever the pips left, so coloured mana is never spent on
// generic while a pip still needs it; a monocolour hybrid's generic face
// competes in the backtracking search as the pip's later alternative (its
// generic amount joins the requirement for the rest of the search).
//
// conv, when non-nil, is the stat:ManaConvert conversion set this payment is
// resolved under (rules/mana_convert.go): it WIDENS what a pip's colour
// alternatives accept -- the payer's converted mana may be spent as though
// it were another colour -- and onlyC may also NARROW it ("spend other mana
// only as though it were colorless"). A nil conv is the plain exact-colour
// match every pre-existing caller keeps, so games with no ManaConvert static
// on the battlefield resolve byte-identically.
func resolveMana(c Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, conv *manaConv) (manaPayment, bool) {
	return resolveManaWith(c, pool, snow, typed, life, false, pipRider{}, conv)
}

// resolveManaWith is resolveMana with the two payer-side grants applied:
// when bLifeOK is set, every plain {B} pip additionally accepts 2 life
// (K'rrik, Son of Yawgmoth's "For each {B} in a cost, you may pay 2 life
// rather than pay that mana"); when anyColor is set, every coloured pip
// (plain, hybrid, twobrid, Phyrexian or hybrid-Phyrexian) is payable by ANY
// colour in the pool -- the may-play grant's MayPlayIgnoreColor$ rider, "you
// may spend mana as though it were mana of any color to cast it" (CR 401.5).
// A {C} pip stays colourless-only under anyColor: CR 107.4c's "any color"
// never includes colourless. Both grants keep main search's deterministic
// first-alternative preference; the expanded alternatives are tried in fixed
// WUBRG order (see anyColorAlts).
//
// The life parameter is the payer's life total, which may already be 0 or
// less mid-cast (Ancient Tomb's own 2 damage landing between two planned
// activations): state-based actions are not checked until a player would
// receive priority (CR 704.3), and the payer may still finish paying. The
// gate therefore binds only the cost's FIXED life component (c.Life): paying
// N>0 life requires life >= N (CR 119.4), while a cost with no life
// component pays 0 life, which is always legal, whatever the life total. The
// Phyrexian/life pip alternatives inside the search still require life >= 2
// each, so a dead payer can never pay a pip with life.
func resolveManaWith(c Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, bLifeOK bool, rider pipRider, conv *manaConv) (manaPayment, bool) {
	if c.Life > 0 && life < c.Life {
		return manaPayment{}, false
	}
	var pipBuf [8]pip
	pips := expandCostPips(c, pipBuf[:0], bLifeOK, rider)
	rem := pool
	sn := snow
	tp := typed
	life -= c.Life
	lifeSpent := c.Life
	// pipExact reports whether col is one of the pip's colour alternatives
	// (a strict colour pip lists its colour once; a hybrid lists two).
	pipExact := func(p pip, col byte) bool {
		for _, alt := range p.alts {
			if alt.color == col {
				return true
			}
		}
		return false
	}
	// pipAccepts reports whether one unit of pool colour col (manaLetters
	// index) may pay pip p under conv. Exact colours always match; conv
	// widens (wild/to) and narrows (onlyC) around that base. Non-colour
	// alternatives (generic, life, snow) are unaffected by conv.
	pipAccepts := func(p pip, col byte, di int) bool {
		isC := len(p.alts) == 1 && p.alts[0].color == 'C'
		if conv == nil {
			return pipExact(p, col)
		}
		if conv.onlyC[di] {
			// The <-C restriction: this pool colour may be spent ONLY as
			// colorless mana -- a {C} pip or generic (generic is handled
			// outside the pip loop and takes any mana), never a coloured
			// or hybrid pip, not even its own colour's.
			return isC
		}
		if pipExact(p, col) {
			return true
		}
		if conv.wild[di] {
			// "spend as mana of any color" widens to every coloured or
			// hybrid pip; the colourless-specific {C} pip is a TYPE, not a
			// colour (CR 107.4c), so it is covered only by the
			// AnyType->AnyType wording (conv.wildC).
			if isC {
				return conv.wildC
			}
			return true
		}
		for _, alt := range p.alts {
			if alt.color != 0 && conv.to[di][state.ManaIndex(alt.color)] {
				return true
			}
		}
		return false
	}
	// finalGeneric is the successful search path's generic requirement: the
	// cost's own Generic plus every monocolour-hybrid pip that paid its
	// generic face on that path. The closing deduction spends exactly it.
	finalGeneric := c.Generic
	var rec func(i int, generic int32) bool
	rec = func(i int, generic int32) bool {
		if i == len(pips) {
			if rem.Total() < generic {
				return false
			}
			finalGeneric = generic
			return true
		}
		p := pips[i]
		// Try each alternative in order: for a hybrid this prefers A over B;
		// for a single-colour pip there is one colour alternative, then the
		// pip's life face for a Phyrexian pip.
		for _, alt := range p.alts {
			switch {
			case alt.color != 0:
				di := state.ManaIndex(alt.color)
				if rem[di] > 0 && pipAccepts(p, alt.color, di) {
					beforeRem, beforeSn, beforeTyped := rem, sn, tp
					takeUnit(&rem, &sn, &tp, di)
					if rec(i+1, generic) {
						return true
					}
					rem, sn, tp = beforeRem, beforeSn, beforeTyped
				}
			case alt.generic > 0:
				// A monocolour hybrid's generic face: this pip joins the
				// generic requirement (tried after the colour face, so a
				// colour unit is preferred when the search can still pay).
				if rec(i+1, generic+alt.generic) {
					return true
				}
			case alt.life > 0:
				if life >= 2 {
					life -= 2
					lifeSpent += 2
					if rec(i+1, generic) {
						return true
					}
					lifeSpent -= 2
					life += 2
				}
			case alt.snow:
				// A {S} pip consumes an actual SNOW unit: both the pool slot
				// and the parallel snow tally, so the unit that leaves is the
				// unit that was snow (never a plain unit misattributed into
				// the tally).
				for di, s := range sn {
					if s > 0 {
						beforeRem, beforeSn, beforeTyped := rem, sn, tp
						rem[di]--
						sn[di]--
						if rec(i+1, generic) {
							return true
						}
						rem, sn, tp = beforeRem, beforeSn, beforeTyped
					}
				}
			}
		}
		// A conversion may let OTHER pool colours pay this pip too -- e.g.
		// "spend white mana as though it were red" offers the pool's white
		// mana for a red pip, which the exact-colour alternatives above
		// cannot see. The walk order is manaLetters (WUBRGC), so the
		// assignment stays deterministic; converted mana is tried only after
		// every exact alternative, so a conversion never displaces an exact
		// payment.
		if conv != nil {
			for di := range manaLetters {
				col := manaLetters[di][0]
				if !pipExact(p, col) && rem[di] > 0 && pipAccepts(p, col, di) {
					beforeRem, beforeSn, beforeTyped := rem, sn, tp
					takeUnit(&rem, &sn, &tp, di)
					if rec(i+1, generic) {
						return true
					}
					rem, sn, tp = beforeRem, beforeSn, beforeTyped
				}
			}
		}
		return false
	}
	if !rec(0, c.Generic) {
		return manaPayment{}, false
	}
	// The search found a pip assignment that leaves enough total mana; deduct
	// the generic requirement from that remainder, preferring colourless then
	// colours in fixed WUBRG order so payment is deterministic. Generic can
	// be paid by any leftover mana, so a total >= Generic always suffices.
	need := finalGeneric
	for _, i := range [...]int{state.MC, state.MW, state.MU, state.MB, state.MR, state.MG} {
		for need > 0 && rem[i] > 0 {
			takeUnit(&rem, &sn, &tp, i)
			need--
		}
	}
	return manaPayment{pool: rem, snow: sn, typed: tp, lifeSpent: lifeSpent}, true
}

// payable reports whether the cost's mana and fixed-life parts can be paid
// by pool, its parallel snow tally and the payer's current life (a Phyrexian
// pip may additionally be paid with two life; a {S} pip only by snow mana).
// This is the offering gate's feasibility question, and the real answer to
// "is there ANY way this cost can be paid right now" -- the same resolveMana
// the payment stage uses, so an offered cost and the cost it charges can
// never disagree.
func payable(c Cost, pool, snow state.Mana, typed [7]state.Mana, life int32) bool {
	_, ok := resolveMana(c, pool, snow, typed, life, nil)
	return ok
}

func poolCanPay(c Cost, p state.Mana) bool {
	// Pool-only feasibility, no life and no snow offered: a hybrid must be
	// paid by one of its colours in the pool, a Phyrexian pip by its colour,
	// a monocolour hybrid by its colour (its generic face is not offered
	// here) and a {S} pip is unpayable. This is the pure pricing question the
	// corpus invariants ask, and it never treats a hybrid as generic nor lets
	// colourless `pay` it.
	_, ok := resolveMana(c, p, state.Mana{}, [7]state.Mana{}, 0, nil)
	return ok
}

// Pay spends the cost from a pool and returns what is left. Coloured
// requirements come out first so generic can never strand a colour the cost
// still needs; hybrid pips take one of their pair and Phyrexian pips their
// colour (pool-only -- the cast flow's payMana handles the life half and
// passes a fully-resolved cost here). Mana-only: non-mana parts
// (Tap/Sac/Discard/SubCounter) are the cast flow's own job (rules/cast.go), never
// this function's.
func poolPay(c Cost, p state.Mana) (state.Mana, bool) {
	// Pool-only: no life and no snow are offered, so a Phyrexian pip is paid
	// by its colour (the cast flow's payMana handles the life half and passes
	// a fully resolved cost here). resolveMana already reserves the coloured
	// pips and deducts generic, so the returned pool is fully spent. A failed
	// search returns the input pool untouched.
	pay, ok := resolveMana(c, p, state.Mana{}, [7]state.Mana{}, 0, nil)
	if !ok {
		return p, false
	}
	return pay.pool, true
}

// payerGrantsPayLifeInsteadOfB reports whether p's side of the battlefield
// carries a Continuous static granting PayLifeInsteadOf:B to p (K'rrik's
// "Affected$ You | AddKeyword$ PayLifeInsteadOf:B"). Every mana payment and
// every cast/activation offer gate consults it, so a plain {B} pip is
// payable with 2 life anywhere K'rrik is in play under its controller.
func (e *Engine) payerGrantsPayLifeInsteadOfB(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("Continuous") {
		// A member equal to the keyword needs the keyword as a substring, so
		// the allocation-free substring test rejects every other static
		// before the list is split.
		if raw := sv.ParamStr(cards.PKAddKeyword); !strings.Contains(raw, "PayLifeInsteadOf:B") ||
			!slices.Contains(cards.SplitKeywordList(raw), "PayLifeInsteadOf:B") {
			continue
		}
		if effects.MatchesPlayerSpec(e.G, sv.Params["Affected"], p, sv.Controller) {
			return true
		}
	}
	return false
}

// payerGrantsIgnoreColor reports whether an active may-play grant of p's
// carrying MayPlayIgnoreColor$ True selects the card id being cast: the
// grant is p's, its AffectedZone names the card's CURRENT zone (so a card
// being cast the ordinary way from hand never inherits an exile grant), and
// the Affected$ spec matches the card. The IsRemembered predicate inside a
// grant's spec is matched against the CONTINUOUS EFFECT's Remembered set
// (the cards the delivering Effect captured), through the SpecContext the
// ordinary filter grammar already carries -- the same direct-list reading
// restrictionApplies uses for Effect-delivered CantTarget/CantRegenerate.
func (e *Engine) payerGrantsIgnoreColor(p state.PlayerID, id state.ObjID) bool {
	return e.payerGrantsMayPlayRider(p, id, func(ce ContinuousEffect) bool { return ce.MayPlayIgnoreColor })
}

// payerGrantsIgnoreType is payerGrantsIgnoreColor for the MayPlayIgnoreType$
// rider (Rakdos, the Muscle's "mana of any type can be spent to cast those
// spells"): the same zone/Affects/remembered-set reading, keyed on the wider
// rider.
func (e *Engine) payerGrantsIgnoreType(p state.PlayerID, id state.ObjID) bool {
	return e.payerGrantsMayPlayRider(p, id, func(ce ContinuousEffect) bool { return ce.MayPlayIgnoreType })
}

// payerGrantsMayPlayRider is the shared body of the two may-play payment
// riders: it walks the active may-play grants of p's and reports whether one
// carrying the asked rider selects the card id being cast.
func (e *Engine) payerGrantsMayPlayRider(p state.PlayerID, id state.ObjID, rider func(ContinuousEffect) bool) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if !ce.MayPlay || !rider(*ce) || ce.Controller != p {
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
			return true
		}
	}
	return false
}

// rememberedTargets lifts a ContinuousEffect's Remembered object ids into
// the []state.Target shape the filter grammar's SpecContext carries.
func rememberedTargets(ids []state.ObjID) []state.Target {
	if len(ids) == 0 {
		return nil
	}
	out := make([]state.Target, 0, len(ids))
	for _, id := range ids {
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// offerRetryFutile reports that offerCastableUsing's potential-target retry
// would re-ask exactly the mana question its first pass just failed: no
// cost static survived a target-independent gate (so the retry, whatever
// targets it binds, composes no static either), the composition is the zero
// costMods the retry's own empty composition is (no waterbend credit was
// folded on top), the scope is not an ability's (whose own ReduceCost$
// reads targets, ownManaReduction), and no ValidCard$ provenance capture is
// pending (the retry would leave it as the first pass did). The retry's
// accept is then manaFeasiblePriced over identical arguments, which failed.
func offerRetryFutile(scope costScope, mods *costMods, mayApply, provenance bool) bool {
	if mayApply || provenance || (scope.kind == "Ability" && scope.ab != nil) {
		return false
	}
	return costModsZero(mods)
}

// costModsZero reports whether m is the zero composition in every field
// (TestCostModsZeroCoversEveryField pins the field list).
func costModsZero(m *costMods) bool {
	return len(m.raises) == 0 && !m.hasExtra && m.raiseCol == (state.Mana{}) && m.raiseGen == 0 &&
		m.raiseLife == 0 && len(m.reduces) == 0 && m.setFloor == 0 && m.waterbend == 0 &&
		!m.waterbendX && m.waterbendPartX == 0 && m.raiseX == 0
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
	for i := range mods.extra.Exile {
		if isNamedCountPart(mods.extra.Exile[i]) {
			named = true
			break
		}
	}
	for i := 0; !named && i < len(mods.extra.TapPermanent); i++ {
		named = isNamedCountPart(mods.extra.TapPermanent[i])
	}
	if !named {
		return costMods{}, false
	}
	return e.offerNamedMods(p, id, ability, *base, *mods, statics, scope, tax, delve, hyp)
}
