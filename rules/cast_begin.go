package rules

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// beginCast starts the cast flow for opt (a "cast" priority option): resolve
// which cost opt pays (the base/alternative cost as before, or the
// kicked/surged/flashback cost opt.Mode names), build the pendingCast, and
// run its first stage.
func (e *Engine) beginCast(p state.PlayerID, opt decision.Option) {
	e.beginCastWithPayment(p, opt, nil)
}

// beginCastWithPayment is the authoritative normal-cast entry with an
// optional, already admitted payment witness.  It intentionally takes no
// client cast descriptor: the selector is resolved against the offered action
// in Submit before this point.
func (e *Engine) beginCastWithPayment(p state.PlayerID, opt decision.Option, selection *decision.PaymentSelection) {
	e.beginCastWith(p, opt, selection, false)
}

// beginCastAnnounced begins an announced cast (Intent.Announce): the ordinary
// cast transaction with no witness, whose CR 601.2g window is the announced
// "select mana" window (rules/announce_pay.go).
func (e *Engine) beginCastAnnounced(p state.PlayerID, opt decision.Option) {
	e.beginCastWith(p, opt, nil, true)
}

func (e *Engine) beginCastWith(p state.PlayerID, opt decision.Option, selection *decision.PaymentSelection, announced bool) {
	id := opt.Obj
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	from := o.Zone
	// The no-progress suppression state as it stood before this proposal.
	// The alternate-face routes below flip the card (a FlipFace is a
	// state-changing event to emit's suppression-clearing rule), but that
	// flip is part of the provisional proposal: an aborted cast flips it
	// back (CR 733.1). See emitProposalFlip.
	preSuppress, preAborts := e.suppressedCast, e.castAborts
	var faceBefore *uint8
	if opt.Mode == "modal_spell" {
		if modalSpellBack(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	if opt.Mode == "room_alt" {
		if roomAlternateCastFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 714: the same flip mechanism serves the Adventure faces. From the
	// hand the cast flips to the Adventure spell face (adventure_alt); from
	// the adventure zone it flips back to the main face (adventure_recast).
	// Everything downstream -- rawBaseCost, targets, timing, resolution --
	// then reads the flipped face, because o.Face() is Faces[FaceIdx]. An
	// aborted proposal restores the pre-flip face via pc.faceBefore (CR
	// 733.1), the same reversal a Room cast takes.
	if opt.Mode == "adventure_alt" {
		if adventureSpellFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	if opt.Mode == "adventure_recast" {
		if o.Zone != state.ZExile || adventureSpellFace(o) == nil || o.Face() != o.Card.Faces[1] {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 702.85a: the Aftermath half -- the alternate face of a Split card --
	// is cast only from its owner's graveyard. From the graveyard the cast
	// flips to the alternate face before the ordinary cast transaction;
	// rawBaseCost, targets and resolution then read the aftermath face
	// (rawBaseCost's default below already pays the FLIPPED face's printed
	// mana cost, which is what aftermath charges). An aborted proposal
	// restores the pre-flip face via pc.faceBefore (CR 733.1), the same
	// reversal a Room or Adventure cast takes.
	if opt.Mode == "aftermath" {
		if o.Zone != state.ZGraveyard || aftermathAlternateFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 709.4: the alternate half of a non-Room split card (mode split_alt)
	// is cast from hand exactly like a Room door or an Adventure spell face:
	// one FlipFace to the chosen half before the ordinary cast transaction,
	// after which rawBaseCost, targets and resolution all read that half. An
	// aborted proposal restores the pre-flip face via pc.faceBefore.
	if opt.Mode == "split_alt" {
		if splitAlternateCastFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 310.11: the defeated battle's owner casts it TRANSFORMED (mode
	// defeat_cast). The exiled battle is its front face; one FlipFace to the
	// back face before the ordinary cast transaction, after which targets and
	// resolution read the back face exactly like the Room/Adventure/Split
	// flips above, and an aborted proposal restores the front face via
	// pc.faceBefore (CR 733.1). The cast is free (cost switch below) and
	// bypasses the ordinary timing gate like Suspend's: it is part of the
	// defeat, which happens in a combat the owner is usually not the active
	// player of, so a creature- or sorcery-timed back face must still be
	// castable (every Siege back face is printed exactly for this cast).
	if opt.Mode == "defeat_cast" {
		if o.Zone != state.ZExile || o.Card == nil || len(o.Card.Faces) < 2 || o.FaceIdx != 0 {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	f := o.Face()
	if f == nil {
		return
	}

	var optionalCost Cost
	// Which cost this pays is opt.AltCostIndex, not always adjustedCost
	// (Ruling T19b-b): legalActions gates each "cast" option on that
	// specific option's own cost being payable, so beginCast must charge
	// that same cost. An out-of-range AltCostIndex (a stale option from a
	// board state that no longer holds the granting static) falls back to
	// the base cost rather than indexing out of bounds.
	//
	// The cost stored here is the RAW selected cost, with no cost modifiers
	// (CR 601.2f): RaiseCost/ReduceCost are applied later, in manaToPay,
	// once {X} is folded into Generic. adjustedCost's modifier-into-generic
	// form must not be stored here, or a spell with an {X} in it would have
	// its reduction applied before X is known (and lost), and a
	// flashback/alternative recast would drop the modifiers entirely.
	cost := e.rawBaseCost(p, id)
	var announceAlt *altCostView
	if opt.AltCostIndex > 0 {
		if alts := e.alternativeCosts(p, id); opt.AltCostIndex-1 < len(alts) {
			cost = alts[opt.AltCostIndex-1].cost
			announceAlt = &alts[opt.AltCostIndex-1]
		}
	}
	selectedMode := opt.Mode
	if strings.HasPrefix(selectedMode, "blitzed_grant_") {
		// Keep a unique offer mode for each grant cost, but use canonical Blitz
		// semantics for the rest of the cast pipeline.
		opt.Mode = "blitzed"
	}
	if strings.HasPrefix(selectedMode, "webslinged_grant_") {
		// Web-slinging (CR 702.186a-family): the same grant-cost convention as
		// blitzed_grant_N above -- a unique offer mode per grant cost, canonical
		// web-slinging semantics for the rest of the cast pipeline (the charge
		// below and modeFlags' flag both key the canonical mode).
		opt.Mode = "web-slinging"
	}
	if strings.HasPrefix(selectedMode, "sneaked_grant_") {
		// Sneak (CR 702.190a): the same grant-cost convention as blitzed_grant_N
		// above -- a unique offer mode per grant cost, canonical sneak
		// semantics for the rest of the cast pipeline (the charge below,
		// modeFlags' flag and the returncost defender capture all key the
		// canonical mode).
		opt.Mode = "sneak"
	}
	switch castModeCodes.Code(string(opt.Mode)) {
	case castModeKicked:
		if kc, ok := kickerCost(f); ok {
			cost = cost.Plus(kc)
		}
	// CR 702.101b: a Fuse cast pays BOTH halves' printed mana costs as one
	// combined cost; the card itself stays at its front face (no FlipFace),
	// and the pay-time FlagFused provenance makes resolution run both halves.
	case castModeFuse:
		if ff, fa := fusedSplitFaces(o); ff != nil {
			cost = e.fuseCost(ff, fa)
		}
	case castModeKickedParts:
		// The and/or Kicker's per-part modes (legal.go offers one option per
		// independently payable part): each mode adds exactly the parts its
		// name promises. An out-of-range or unparseable form (a stale option
		// or a face whose Kicker changed) falls back to the base cost --
		// the same no-crash read the AltCostIndex fallback below takes.
		if c1, c2, ok := twoPartKickerCosts(f); ok {
			switch castKickerModeCodes.Code(string(opt.Mode)) {
			case castKickerModeKicked1:
				cost = cost.Plus(c1)
			case castKickerModeKicked2:
				cost = cost.Plus(c2)
			default:
				cost = cost.Plus(c1).Plus(c2)
			}
		}
	case castModeSurged:
		if sc, ok := surgeCost(f); ok {
			cost = sc
		}
	case castModeEntwined:
		// Entwine (CR 702.42a): the additional cost is paid ON TOP of the
		// printed mana cost -- the Buyback shape. The offer gate priced the
		// SAME read (legal.go's offer), so the two stages cannot disagree.
		// The mode announcement then forces every eligible mode in
		// castModeAsk; a stale option whose keyword is gone still pays the
		// base cost alone (no fake charge), and the cast_mode answer falls
		// back to the ordinary single-mode bounds.
		if ec, ok := entwineCost(f); ok {
			cost = cost.Plus(ec)
		}
	case castModeBuyback:
		if bc, ok := buybackCost(f); ok {
			cost = cost.Plus(bc)
		}
	case castModeOffspring:
		// Offspring (CR 702.175a): the additional cost is paid ON TOP of the
		// mana cost ("You may pay an additional [cost] as you cast this
		// spell"), never a substitution -- the Buyback shape. The cost is
		// resolved through the DERIVED keyword read (e.offspringCost), so a
		// layer-6 grant (Zinnia) charges the GRANTED parameter, and a stale
		// option whose keyword is gone pays the plain base cost rather than
		// stranding. The offer gate priced the SAME derived read
		// (rules/legal.go's hand and command-zone walks), so the two stages
		// cannot disagree.
		if oc, ok := e.offspringCost(id); ok {
			cost = cost.Plus(oc)
		}
	case castModeReplicated:
		// Replicate (CR 702.55a): the mode marks the intent to pay the
		// optional replicate cost. The PAYMENT COUNT is a cast announcement
		// of its own (CR 601.2b), settled by replicateAsk before the
		// Convoke/X stages and folded into cost there, so a declined count
		// leaves an exactly plain cast and the offer gate's own base+1
		// composition is never silently charged for a different count. The
		// parameter itself is captured onto the pendingCast after its
		// construction (the suspend block below), keeping this switch's
		// cost-folding contract intact.
	case castModeMultikicked:
		// Multikicker (CR 702.43): the same shape as "replicated" above --
		// the mode marks the intent to pay the optional multikicker cost;
		// the PAYMENT COUNT is settled by multikickAsk before the Convoke/X
		// stages and folded into cost there, so this case folds NOTHING. A
		// declined count leaves an exactly plain cast (modeFlags maps this
		// mode to ""), and the pay-time CastInfo rides the trailing
		// FlagMultikicked event.
	case castModeHarmonize:
		if hc, ok := harmonizeCost(f); ok {
			cost = hc
		}
	case castModeSuspend:
		if sc, ok := suspendCost(f); ok {
			cost = sc.cost
		}
	case castModeSuspendCast:
		cost = Cost{}
	case castModeDefeatCast:
		// CR 310.11: the defeated battle's owner casts the back face without
		// paying its mana cost.
		cost = Cost{}
	case castModePlot:
		// CR 701.34a: the plot ACTION pays the K:Plot colon parameter. Not a
		// cast: payCast's plot branch intercepts before the stack push, and
		// continueCast never reaches the target/push stages for this mode.
		if raw, ok := f.KeywordParam("Plot"); ok {
			cost = ParseCost(raw)
		}
	case castModePlotCast:
		// CR 701.34d: the plotted card's later cast is free -- no mana cost,
		// no raises; targets and resolution run the ordinary stages.
		cost = Cost{}
	case castModePreparedCopy:
		// CR 722.3c: the prepared designation's exile copy is cast as a
		// copy of the prepare spell, with targets and resolution running the
		// ordinary stages. It is NOT free: the reminder grants only "you may
		// cast a copy of its spell" (no "without paying its mana cost"), so
		// CR 601.2f charges the copy's own printed mana cost -- the default
		// rawBaseCost above, which reads the copy's prepare-spell face (Forge's
		// prepared effect is likewise a plain MayPlay$ True grant).
	case castModeForetell:
		// CR 702.126a: the Foretell ACTION pays {2} and exiles the card face
		// down -- never the keyword's own colon parameter, which prices the
		// LATER cast (the foretell_cast case below).
		cost = Cost{Generic: 2}
	case castModeForetellCast:
		// CR 702.126a: use the explicit keyword cost, or the printed-cost
		// reduction carried by an effect's ForetoldCost$ designation.
		if fc, ok := foretellCost(f); ok {
			cost = fc
		} else {
			// The option walk fails closed for this shape; retain a harmless
			// fallback for stale options submitted after the designation changed.
			cost = Cost{Generic: 2}
		}
	case castModeAirbendCast:
		// CR 701.65a: the airbent card's recast pays {2} rather than its mana
		// cost. Cost modifiers (CR 601.2f) apply later in manaToPay, exactly
		// like the other alternative-cost recasts.
		cost = Cost{Generic: 2}
	case castModeFlashback:
		cost = e.flashbackCostFor(id, opt)
	case castModeMayplay:
		// rules/mayplay.go granted this play from a non-hand zone. The
		// printed cost is paid (the default below) unless the granting
		// static said MayPlayWithoutManaCost$ True, in which case the mana
		// part is free while non-mana additional costs still apply
		// (CR 118.9) -- so this mode folds into the withSpellAbilityExtras
		// condition below, exactly like a plain cast. CR 118.3a: the
		// granting static's RaiseCost$ surcharge is then added on top
		// through the SAME helper the offer walk (legal.go's may-play spell
		// walk) used, so the offered cost and the charged cost structurally
		// cannot disagree. A raise the helper could not price leaves
		// mayPlayGrant withholding the card; the cast never reaches here.
		// A MayPlayText$-typed option names its permission, so a card cast
		// through ONE of several matching statics pays exactly that
		// static's riders (mayPlayPermFreeRaise); an untyped option keeps
		// the aggregate read.
		free, raise, hasRaise, priced := e.mayPlayPermFreeRaise(p, id, opt.MayPlayPerm)
		if free {
			cost = Cost{}
		}
		if hasRaise && priced {
			cost = cost.Plus(raise)
		}
	case castModeMiracle:
		// Task 18: a Miracle cast pays the printed Miracle cost (CR 702.93d) in
		// place of the card's normal cost. KeywordParam is read off the face;
		// a missing keyword (offer routed here only from a Miracle offer, and
		// only while the card is in hand) falls back to the empty cost so a
		// stale cast cannot strand.
		if mc, ok := f.KeywordParam("Miracle"); ok {
			cost = ParseCost(mc)
		} else {
			cost = Cost{}
		}
	case castModeEscape:
		if ec, ok := e.escapeCost(id); ok {
			cost = ec
		} else {
			cost = Cost{}
		}
	case castModeRetrace:
		// Retrace (CR 702.81a): a graveyard cast paying the printed mana cost
		// PLUS the additional discard-a-land cost -- never a substitution, so
		// the printed base (cost's rawBaseCost seed) stays and only the
		// additional part is folded on. The offer gate priced exactly this
		// composition (legal.go's graveyard walk) and proved a land payable;
		// a stale option whose keyword is gone folds nothing, degrading to a
		// plain cast rather than charging a discard that was never offered.
		// The derived keyword (e.HasKeyword), exactly what the offer gate
		// reads: a continuous grant (Six's "nonland permanent cards in your
		// graveyard have retrace") is not on the printed face, and reading
		// f here offered the grant's cast but charged no discard -- a free
		// graveyard recast loop (fuzz batch6 line 1, Jeweled Lotus).
		if e.hasKeywordH(id, kwhRetrace) {
			cost = cost.Plus(retraceExtra())
		}
	case castModeJumpstart:
		// Jump-start (CR 702.84a): a graveyard cast paying the printed mana
		// cost PLUS the additional discard-a-card cost -- never a
		// substitution, exactly the retrace shape. The offer gate priced this
		// composition and proved a card payable; the same stale-option
		// degradation applies (a jumpstart mode whose keyword is gone folds
		// nothing rather than charging an unoffered discard).
		if e.hasKeywordH(id, kwhJumpStart) {
			cost = cost.Plus(jumpstartExtra())
		}
	case castModeAltCostKeyword:
		// Grant instances use distinct modes so their cost remains selectable
		// beside printed Blitz, but share Blitz's cast semantics.
		// The alternative-cost keyword family (altcosts): each mode's cost is
		// the printed keyword parameter in place of the mana cost, exactly the
		// Miracle shape. Evoke and Madness casts come from hand and exile
		// respectively via the pending-trigger/cast-offer machinery; a stale
		// option whose keyword is gone (the face cannot change, so in practice
		// only a hand-built option) falls back to the empty cost rather than
		// charging the printed mana cost.
		head := map[string]string{"evoked": "Evoke", "dashed": "Dash",
			"overloaded": "Overload", "warped": "Warp", "madness": "Madness", "blitzed": "Blitz"}[opt.Mode]
		if opt.Mode == "bestowed" {
			// Bestow goes through the ONE resolver the offer gate used
			// (rules/bestow.go's bestowCost: the colon cut and the Unknown
			// withhold), so the charge and the offer can never disagree about
			// what a bestowed cast costs -- a raw ParseCost here would price
			// hypnotic_siren's ":GainControl" suffix as a phantom generic.
			if bc, ok := bestowCost(f); ok {
				cost = bc
			} else {
				cost = Cost{}
			}
			break
		}
		if opt.Mode == "blitzed" {
			cost = Cost{}
			for _, bc := range e.blitzCosts(p, id) {
				if bc.mode == selectedMode {
					cost = bc.cost
					break
				}
			}
		} else if mc, ok := f.KeywordParam(head); ok {
			cost = ParseCost(mc)
		} else {
			cost = Cost{}
		}
	case castModeWebSlinging:
		// Web-slinging (CR 702.186a-family, Marvel's Spider-Man): the
		// web-slinging cost replaces the mana cost AND the composed Cost
		// carries the mandatory Return<1/Creature.YouCtrl+tapped> additional
		// cost, settled by the ordinary Return machinery (returnAsk asks,
		// payCast moves the chosen permanent to its owner's hand beside the
		// other payments). webSlingingCosts is the ONE reader the offer and
		// this charge call, so the two stages cannot drift; a stale option
		// whose keyword is gone falls back to the empty cost like the keyword
		// family above rather than charging the printed mana cost.
		cost = Cost{}
		for _, wc := range e.webSlingingCosts(p, id) {
			if wc.mode == selectedMode {
				cost = wc.cost
				break
			}
		}
	case castModeSneak:
		// Sneak (CR 702.190a): the printed or granted sneak cost replaces the
		// mana cost AND the composed Cost carries the mandatory
		// Return<1/Creature.YouCtrl+attacking+unblocked> additional cost,
		// settled by the ordinary Return machinery (returnAsk asks, payCast
		// moves the chosen attacker to its owner's hand beside the other
		// payments). sneakCosts is the ONE reader the offer and this charge
		// call, so the two stages cannot drift; a stale option whose keyword
		// is gone falls back to the empty cost rather than charging the
		// printed mana cost.
		cost = Cost{}
		for _, sc := range e.sneakCosts(p, id) {
			if sc.mode == selectedMode {
				cost = sc.cost
				break
			}
		}
	case castModeMayhem:
		// Mayhem (the Doom Prevails keyword): a graveyard cast paying the
		// mayhem cost in place of the mana cost -- the alternative-cost
		// substitution family, the Miracle shape. The discard-this-turn
		// provenance gate is the OFFER's gate (legal.go's graveyard walk via
		// mayhemDiscardedThisTurn); the charge only re-reads the cost through
		// the same helper, so offer and charge cannot drift, and a stale
		// option whose keyword is gone falls back to the empty cost like the
		// family above. The mode's flag (state.FlagMayhem) is the whole of
		// what the cast records: Sandman's Quicksand's Card.CastSa
		// Spell.Mayhem condition reads it; there is no exile tail to gate.
		if mc, ok := e.mayhemCastCost(id); ok {
			cost = mc
		} else {
			cost = Cost{}
		}
	case castModeMutated:
		// Mutate (CR 702.140a): the mutate cast pays the MUTATE cost in place
		// of the mana cost -- the same substitution the offer gate priced
		// (legal.go's offerCastable(p, id, mc, spellScope("mutated"), ...)).
		// Without this case the pendingCast charges the PLAIN mana cost, which
		// only ever passes unnoticed when the two costs are payable from the
		// same pool (Everquill Phoenix's {3}{R} mutate vs {2}{R}{R} plain, both
		// payable from RRRR -- Huntmaster Liger's {2}{W} mutate vs {3}{W} plain
		// aborts the cast at the target stage instead). mutateCost applies the
		// same colon-cut and Unknown/X withhold the offer gate used; a stale
		// option whose keyword is gone falls back to the empty cost like the
		// keyword family above.
		if mc, ok := mutateCost(f); ok {
			cost = mc
		} else {
			cost = Cost{}
		}
	case castModeMorphed:
		// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a): the
		// face-down cast pays {3} in place of the mana cost -- the same
		// substitution shape the alternative-cost family below charges
		// (miracle and friends). The keyword's own colon parameter is the
		// LATER turn-face-up cost, never paid now; it stays printed on the
		// face, and the pay-time CastInfo's mode flag (modeFlags) records
		// which family rode so the later turn-face-up action can validate
		// and pay against it. The offer gate (rules/legal.go's hand walk)
		// priced this same {3} through spellScope(fam)'s modifiers, so the
		// charge and the offer cannot disagree, and the fixed {3} has no
		// keyword parameter to fall back on (a stale option degrades to a
		// still-legal {3} cast rather than a free one).
		cost = Cost{Generic: 3}
	case castModeMayflash:
		// MayFlashCost (CR 702.8, the "as though it had flash" alternate
		// cast): the printed mana cost is paid PLUS the keyword's colon
		// parameter -- the oracle wording is "pay {2} MORE to cast it", so
		// this is base.Plus(extra), never a substitution. The offer gate
		// (legal.go's hand walk) priced exactly this composition, so a stale
		// option whose keyword is gone folds nothing rather than charging a
		// cost the gate never proved payable. The extra's non-mana parts
		// (tapXType<Tegwyll's Scouring>, Behold<Molten Exhale>) are settled
		// by the ordinary tap/choice-cost machinery after this fold.
		if mc, ok := mayflashExtraCost(f); ok {
			cost = cost.Plus(mc)
		}
	case castModeEmerged:
		// Emerge (CR 702.118a): the emerge cast pays the printed K:Emerge cost
		// in place of the mana cost AND sacrifices a creature, whose mana
		// value reduces the cost. The reduction is NOT applied here -- the
		// creature is not chosen until sacAsk settles the Sac part -- so this
		// arm composes the emerge cost with the mandatory sacrifice and marks
		// the cast; applyEmergeReduction folds the chosen creature's mana value
		// out of pc.cost once the choice is in. A stale option whose keyword is
		// gone falls back to the empty cost like the keyword family above, and
		// without the Sac part the cast is an ordinary (over-charged) emerge;
		// the offer gate only ever routes here with the keyword present.
		if ec, ok := emergeBase(f); ok {
			cost = ec
		} else {
			cost = Cost{}
		}
	}
	// CR 601.2b/f/h: a spell's own SpellAbility may carry an explicit Cost$
	// (Forge's SP Cost) naming an additional cost -- most commonly a
	// sacrifice (Altar's Reap's "1 B Sac<1/Creature>", the CR 601.2h example).
	// The mana part of that Cost$ REPLACES the printed mana (it is the same
	// cost the card already charges), so only its non-mana parts
	// (Sac/Discard/SubCounter/Tap) are additional and fold into the total cost here; a
	// re-added mana part would double charge. Only a plain cast (and the
	// "conspired" mode, whose offer gate priced the same extras -- the
	// replicated/multikicked modes keep the older no-fold divergence) reaches
	// this (pc.ability < 0 and no alternative/flashback recast), and a spell
	// with no SP Cost$ contributes nothing.
	if opt.Mode == "optionalcost" {
		// The offer stores the selected optional part in AltCostIndex's
		// companion-independent mode; legalActions has already proved it payable.
		// The actual non-mana payment is settled by the ordinary cost stages.
		if opt.AltCostIndex <= 0 {
			return
		}
		parts := e.optionalCostViews(e.collectCostStatics(), p, id)
		if opt.AltCostIndex > len(parts) {
			return
		}
		// The offer priced withSpellAbilityExtras(f, convokeBase).Plus(extra)
		// (legal.go), so fold the same SpellAbility Cost$ extras here before
		// the optional part: the charge must match the gate, or a spell that
		// carries BOTH an OptionalCost static and a spell-ability additional
		// cost undercharges by that additional cost. Zero corpus carriers pair
		// the two today, so this is the structural agreement, not a behaviour
		// change (the fold is a no-op without a SpellAbility Cost$).
		cost = withSpellAbilityExtras(f, cost)
		cost = cost.Plus(parts[opt.AltCostIndex-1])
		optionalCost = parts[opt.AltCostIndex-1]
	}
	if opt.AltCostIndex == 0 && (opt.Mode == "" || opt.Mode == "mayplay" || opt.Mode == "modal_spell" || opt.Mode == "room_alt" ||
		opt.Mode == "adventure_alt" || opt.Mode == "aftermath" || opt.Mode == "split_alt" || opt.Mode == "conspired" || opt.Mode == "casualty" || opt.Mode == "mayflash" || opt.Mode == "retrace" || opt.Mode == "jumpstart" ||
		// CR 702.34a/601.2f: flashback replaces only the mana cost; the
		// spell's own additional cost (Eviscerator's Insight's sacrifice,
		// Electric Revelation's discard) is still paid. The offer gate
		// (legal.go's flashback walk) folds the same extras.
		opt.Mode == "flashback") {
		cost = withSpellAbilityExtras(f, cost)
	}
	// Convoke and Harmonize are announced only after X/mode/pip choices have
	// formed the total cost (convokeAsk). Do not preselect creatures here:
	// doing so let one of those creatures activate a mana ability before its
	// delayed Tap payment.
	// The either-or additional cost (AlternateAdditionalCost) is a CHOICE,
	// not a fixed component, so the parts are only captured here and the ask
	// (altAddAsk) folds the chosen part into cost before any other cost stage
	// runs. Only a plain cast carries them: a kicked/surged/etc. cast of the
	// same card pays that mode's cost without recomposing this choice (no
	// corpus card pairs both shapes). The OFFER gate already proved at least
	// one part is payable (legal.go); the ask narrows it to exactly one.
	tax := int32(0)
	if opt.Mode != "foretell" {
		tax = e.commanderTaxAmount(p, id)
	}
	scope := spellScope(opt.Mode)
	if opt.Mode == "foretell" {
		scope = foretellScope()
	}
	mods := e.costModifiers(p, id, scope)
	// The SVar-fixed PayLife<X> conversion (fixLifeXCost) -- the same helper
	// offerCastable shaped the offered cost with, so the stored cost and the
	// gated charge agree. A fixed face's value folds into Life here; the
	// withheld (unresolvable-body) shape cannot reach this line through any
	// offer gate, and a stale option that does degrades to a no-op before
	// anything is pushed or charged.
	converted, ok := e.fixLifeXCost(p, id, cost)
	if !ok {
		return
	}
	cost = converted
	// A RaiseCost static's non-mana Cost$ (Soul Immolation's `Cost$ Blight<X>`,
	// Grafted Identity's creature sacrifice, the Champion cycle's BeholdExile)
	// is carried in mods.extra so the OFFER gate (composedOfferCost, which
	// applies mods) enforces it and the CHARGE prices it. The pending cast's
	// own cost must carry it too, because every non-mana cost stage -- xAsk's
	// X announcement, sacAsk, blightCostAsk, the settle and costAnnouncesX --
	// reads pc.cost, not the composed charge. Fold it in here once and drop
	// it from mods so manaToPay's mods.apply cannot count the same part twice.
	cost, ok = e.foldRaiseExtra(p, id, cost, &mods)
	if !ok {
		return
	}
	if opt.AltCostIndex == 0 && opt.Mode == "" {
		pcAlt := altAddCostParts(f)
		e.cast = e.newCast(p, id, from, opt.Mode, -1)
		e.cast.cost, e.cast.faceBefore, e.cast.mods, e.cast.taxGeneric, e.cast.altAddParts, e.cast.optionalCost =
			cost, faceBefore, mods, tax, pcAlt, optionalCost
	} else {
		e.cast = e.newCast(p, id, from, opt.Mode, -1)
		e.cast.cost, e.cast.faceBefore, e.cast.mods, e.cast.taxGeneric, e.cast.optionalCost =
			cost, faceBefore, mods, tax, optionalCost
	}
	// Emerge (CR 702.118a): sacAsk folds the chosen sacrifice's mana value out
	// of pc.cost once the mandatory creature sacrifice is settled. The mark is
	// set here, beside the cost the switch composed, so the charge and the
	// reduction can never disagree about what cast they belong to.
	if opt.Mode == "emerged" {
		e.cast.emerge = true
	}
	// Morph family (CR 702.37a/702.168a/702.169a): the mark is set beside
	// the cost the switch composed, so the {3} charge and the face-down
	// entry marker can never disagree about which cast they belong to.
	if opt.Mode == "morphed" || opt.Mode == "megamorphed" || opt.Mode == "disguised" {
		e.cast.faceDown = true
	}
	// Escalate (the modal additional cost "pay this for each mode chosen
	// beyond the first"): the cost is carried as its raw keyword parameter
	// and re-parsed by the cast_modes answer handler -- the same
	// string-survives-Clone convention the replicate capture below documents.
	// Escalate rides the plain cast (no separate option exists): the CR
	// 601.2b mode answer's chosen count is what prices it, so the capture is
	// mode-blind.
	if s, ok := f.KeywordParam("Escalate"); ok && strings.TrimSpace(s) != "" {
		e.cast.escalateParam, e.cast.escalateSet = s, true
	}

	// Strive (CR 702.52, "this spell costs <cost> more for each target
	// beyond the first"): the same raw-parameter capture as Escalate. Strive
	// rides the plain cast (no separate option exists): the CR 601.2c target
	// answer's chosen count is what prices it, so repriceForTargets folds it
	// into pc.cost once the targets are known.
	if s, ok := f.KeywordParam("Strive"); ok && strings.TrimSpace(s) != "" {
		e.cast.striveParam, e.cast.striveSet = s, true
	}

	// kw:MayFlashSac (CR 702.8): capture the rider's condition now, before
	// CR 601.2a puts the spell on the stack, so the empty-stack half of
	// sorcerySpeed is the board the caster announced into rather than this
	// spell's own push. An ability proposal (pc.ability >= 0) never reads it:
	// payCast's flag arm is gated on !pc.isAbility().
	e.cast.offSorcery = e.offSorceryAtCast(p)
	// The announce-bearing alternative (the Shoal cycle) rides the selected
	// cast SA into the transaction: xAsk's announce arm and exAsk's binding
	// read the captured value.
	if announceAlt != nil && announceAlt.announce != "" {
		e.cast.announceX = announceAlt.announce
	}
	// CR 903.8: the commander tax, applied to whatever cost this cast pays
	// (the base/alternative/kicked/flashback/surged/miracle cost resolved
	// above) -- the exact same commanderTaxFor the command-zone offer in
	// legal.go gated castable on, over the same board, so this charge and
	// that offer can never disagree. For a command-zone commander this is the
	// plain base + the tax; for every other card/zone it passes cost through
	// unchanged (commanderTaxFor is a no-op outside the Commander format and
	// off the command zone). It lands AFTER cost modifiers and any keyword
	// recast, so an additional cost is never reduced by them, in line with how
	// Kicker's own additional cost composes.
	//
	// The tax is captured as a separate generic amount (taxGeneric) rather
	// than folded into cost, so manaToPay adds it AFTER the 601.2f modifiers
	// and never lets those spill onto it.
	if opt.Mode == "suspend" {
		if sc, ok := suspendCost(f); ok && sc.timeX {
			e.cast.suspendTimeX, e.cast.suspendMinX = true, sc.minTime
		}
	}
	// Replicate (CR 702.55a): the cost is carried as its raw keyword
	// parameter and re-parsed by replicateAsk and the answer handler -- a
	// string survives the intent boundary's pendingCast Clone without
	// deep-copying cost slices, and ParseCost is deterministic.
	if opt.Mode == "replicated" {
		if _, ok := replicateCost(f); ok {
			e.cast.replicateParam, e.cast.replicateSet = f.KeywordParam("Replicate")
		}
	}
	// Multikicker (CR 702.43): the cost is carried as its raw keyword
	// parameter and re-parsed by multikickAsk and the answer handler -- the
	// same string-survives-Clone convention the replicate capture above
	// documents.
	if opt.Mode == "multikicked" {
		if _, ok := multikickerCost(f); ok {
			e.cast.multikickParam, e.cast.multikickSet = f.KeywordParam("Multikicker")
		}
	}
	// Squad (CR 702.66): the per-payment cost is carried as its raw keyword
	// parameter and re-parsed by squadAsk and the answer handler -- the exact
	// string-survives-Clone convention the replicate capture above documents.
	if opt.Mode == "squadded" {
		if _, ok := squadCost(f); ok {
			e.cast.squadParam, e.cast.squadSet = f.KeywordParam("Squad")
		}
	}
	// Conspire (CR 702.78a) is param-less: the mode itself marks the intent
	// and conspireSet records it for conspireAsk. The tap election is posed
	// by conspireAsk (not a cost part -- the fixed "two creatures you control
	// sharing a colour with the spell" has no Cost$ spelling), and the taps
	// settle through pc.taps exactly like every other tap cost.
	if opt.Mode == "conspired" {
		e.cast.conspireSet = true
	}
	if opt.Mode == "casualty" {
		if info, ok := e.casualtySpec(id); ok {
			if info.variable {
				e.cast.casualtyVariable = true
				e.cast.casualtyN = 0
			} else {
				e.cast.casualtyN = info.threshold
			}
		} else {
			e.cast.casualtyN = -1
		}
	}
	// CR 401.5's MayPlayIgnoreColor$ rider: "you may spend mana as though it
	// were mana of any color to cast it". Recorded from the grant the offer
	// gate consulted while the card was still in the granted zone.
	if opt.Mode == "mayplay" {
		rider := asPayer(e).MayPlayRider(p, id)
		e.cast.mayPlayIgnore, e.cast.mayPlayIgnoreType = rider.AnyColor, rider.AnyType
		e.cast.mayPlayRemembered = e.mayPlayManaConvertRemembered(p, id)
		e.cast.mayPlayPerm = opt.MayPlayPerm
		e.cast.mayPlayHosts = e.mayPlayHostsCovering(p, id)
		e.cast.mayPlayHostsSet = true
	}
	if e.cast != nil {
		e.cast.costRemembered = e.costRememberedCapture(id)
	}
	if selection != nil && e.cast != nil {
		e.cast.payment = &plannedCastPayment{actionID: selection.ActionID, plan: decision.ClonePaymentPlan(selection.Plan)}
	}
	if announced && e.cast != nil {
		e.cast.announced = true
	}
	e.continueCast()
}

// convertedManaCostToken matches Forge's ConvertedManaCost placeholder inside
// a PlayCost$ token, case-insensitively (the corpus spells it exactly this
// way; the case fold costs nothing).
var convertedManaCostToken = regexp.MustCompile(`(?i)convertedmanacost`)

// pricePlayCost prices a Play effect's PlayCost$ token for one chosen card:
// the ConvertedManaCost placeholder is substituted with the card face's mana
// value (Amped Raptor's "an amount of {E} equal to its mana value") and the
// result is parsed with the ordinary cost grammar -- PayEnergy<N>, PayLife<N>,
// a fixed generic, and Discard<N/Spec> all land in the Cost fields the cast
// flow already asks and charges. SuspendCost is resolved from the chosen
// card's K:Suspend before that ordinary grammar. A token the grammar reports as
// unmodelled is NOT degraded the way a printed cost's malformed token would be:
// PlayCost$ is an ALTERNATIVE to the mana cost (CR 118.9 "rather than paying
// its mana cost"), so degrading it to one generic would still charge the
// player full price -- the caller hard-declines instead, ParseUnlessCost-style.
func pricePlayCost(f *cards.Face, token string) (Cost, bool) {
	// SuspendCost is the one PlayCost token whose value is another keyword's
	// cost rather than a standalone cost expression. Read the chosen card's
	// printed K:Suspend, exactly as the Face of Boe's "pay its suspend cost"
	// text requires; an absent or malformed Suspend keyword remains a hard
	// decline.
	if strings.EqualFold(strings.TrimSpace(token), "SuspendCost") {
		info, ok := suspendCost(f)
		if !ok || info.timeX {
			return Cost{}, false
		}
		return info.cost, len(info.cost.Unknown) == 0
	}
	s := convertedManaCostToken.ReplaceAllString(token, strconv.FormatInt(int64(f.ManaValue()), 10))
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// beginPlay is the rules' hand-off for an answered Play effect. It starts a
// cast from the card's current zone and uses its printed cost unless that
// specific Play SA said WithoutManaCost$ True (Spinerock Knoll grants a free
// cast, while Conduit of Worlds requires payment) or carries a PlayCost$
// alternative (Amped Raptor's energy cast), which REPLACES the mana cost
// only -- additional costs and cost modifiers ride exactly as an ordinary
// cast's do (CR 118.9 / 601.2f). The card must still be on the stack of the
// suspended Play resolution when this runs; a malformed answer degrades to a
// logged no-op rather than panic. replaceGraveyard carries the Play SA's
// ReplaceGraveyard$ Exile rider (task replplay1): true stamps the played
// spell's pay-time CastInfo with state.FlagReplaceGraveyard so the resolution
// reader exiles it instead of the graveyard.
func (e *Engine) beginPlay(p state.PlayerID, id state.ObjID, withoutManaCost bool, playCost string, replaceGraveyard, copyCard bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		e.emit(events.Event{Kind: events.Note, Player: p, Text: "Play found no card to play"})
		return
	}
	if o.Face().IsLand() {
		// A "play" permission can play a land, but it does not grant an
		// additional land drop. Lands never become spells or enter the stack.
		if int(p) >= len(e.G.Players) || e.G.Players[p].LandsPlayed >= 1 {
			e.emit(events.Event{Kind: events.Note, Player: p, Text: "Play cannot use an additional land drop"})
			return
		}
		e.cast = e.newCast(p, id, o.Zone, "land", -1)
		e.continueCast()
		return
	}
	// CR 601.3: a player can begin to cast a spell only if a rule or effect
	// allows it and no rule or effect prohibits it. The ordinary cast OFFER
	// runs that prohibition gate (castRestricted -- the CantBeCast family:
	// Teferi's "only any time they could cast a sorcery", Void Winnower's
	// even-mana lockout, a card's own "you can't cast this spell unless
	// ..."). A Play effect begins its cast WITHOUT passing the offer walk,
	// so the free-cast routes (cascade and Discover's election, an impulse
	// "you may play it") would otherwise cast a prohibited card for free.
	// Refuse here with a Note; the card stays in the zone the Play found it
	// in (cascade's chained tail then bottoms it). A play that cannot name
	// a castable card is not an error -- CR 601.3 simply withholds the
	// cast, and the Play's answer is consumed either way.
	if e.castRestricted(p, id) {
		e.emit(events.Event{Kind: events.Note, Player: p, Obj: id,
			Text: "the play cannot cast a restricted card"})
		return
	}
	// Cipher's CopyCard$ True Play rider: the encoded card is NOT moved --
	// a COPY of it is placed on the stack and cast, and the original stays in
	// its zone. Mint through a logged event, never by mutating Game here:
	// replay must derive the same object ID before the subsequent PutOnStack.
	// The copy starts in the temporary library holding zone, which becomes
	// the cast's From; CR 707.12 allows this copy of a card to be cast.
	if copyCard {
		copyID := e.G.NextID
		e.emit(events.Event{Kind: events.StackCopy, Obj: id, Player: p,
			Text: "copy card for play"})
		o = e.G.Obj(copyID)
		if o == nil || !o.IsCopy {
			return
		}
		id = copyID
	}
	cost := e.rawBaseCost(p, id)
	if withoutManaCost {
		cost = Cost{}
	} else if playCost != "" {
		// A PlayCost$ alternative replaces the mana cost; an unpriceable
		// token is a hard DECLINE, never a mana fallback -- the player is
		// never charged full mana for a "rather than" alternative.
		alt, ok := pricePlayCost(o.Face(), playCost)
		if !ok {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "Play cannot price its alternative cost (" + playCost + "); the play is declined"})
			return
		}
		// The alternative's non-mana parts must be payable the way an
		// offered cast's would be (the energy total, the discard
		// candidates): a YES answer the payment cannot settle is declined
		// with a Note, not begun and short-changed at the settle.
		if !e.nonManaCastable(p, id, alt, false, "") {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "The alternative cost cannot be paid (" + playCost + "); the play is declined"})
			return
		}
		if alt.Life > e.G.Players[p].Life {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "The alternative cost cannot be paid (" + playCost + "); the play is declined"})
			return
		}
		cost = alt
	}
	// A normal Play cast pays its printed mana cost; a free Play cast does
	// not; a PlayCost$ cast pays the alternative. All three still pay
	// non-mana additional costs, exactly as an ordinary cast does (CR
	// 118.9 / 601.2f). The cost is stored RAW (no cost modifiers folded):
	// RaiseCost/ReduceCost ride pc.mods and manaToPay applies them after {X}
	// is folded, the same shape beginCast stores.
	cost = withSpellAbilityExtras(o.Face(), cost)
	converted, ok := e.fixLifeXCost(p, id, cost)
	if !ok {
		// The offer gate withheld this cost; a stale Play degrades to a no-op.
		return
	}
	cost = converted
	mods := e.costModifiers(p, id, spellScope(""))
	// A RaiseCost static's non-mana Cost$ rides mods.extra; fold it into the
	// pending cost and drop it from mods so the charge cannot double it (the
	// same agreement beginCast makes).
	cost, ok = e.foldRaiseExtra(p, id, cost, &mods)
	if !ok {
		return
	}
	e.cast = e.newCast(p, id, o.Zone, "play", -1)
	e.cast.cost, e.cast.mods, e.cast.replaceGraveyard = cost, mods, replaceGraveyard
	e.continueCast()
}

// applyDredge performs a dredged replacement of a draw: mill N cards (N = the
// dredge card's Dredge number) from p's library into the graveyard, then move
// the dredge card from p's graveyard to their hand. The ordinary draw was
// skipped by choosing option 0 in DrawFor's dredge ask.
func (e *Engine) applyDredge(p state.PlayerID, dredgeID state.ObjID) {
	o := e.G.Obj(dredgeID)
	if o == nil || o.Face() == nil {
		return
	}
	n := int32(0)
	if v, ok := o.Face().KeywordParam("Dredge"); ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			n = int32(parsed)
		}
	}
	lib := e.G.Zone(state.ZLibrary, p)
	// A stale or malformed answer must not turn an illegal insufficient-library
	// dredge into a partial mill: CR 702.55 requires all N cards.
	if n <= 0 || int(n) > len(lib) {
		return
	}
	for _, id := range lib[:n] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	if dredgeID != 0 && o.Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: dredgeID,
			From: o.Zone, To: state.ZHand})
	}
}

// resumeOrdinaryDraw re-emits the ordinary draw a declined dredge skipped.
// DrawFor posed the dredge ask and suspended; a "no" (option 1) means the
// player draws as normal, which is exactly the Draw event DrawFor would have
// emitted had no dredger been in the graveyard. Only the library's top card
// moves; a decline with an empty library is a loss, checked by the SBA.
func (e *Engine) resumeOrdinaryDraw(p state.PlayerID) {
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		e.playerLoses(p, loseReasonMilled, "drew from an empty library")
		return
	}
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// continueCast runs the cast flow's stages in order -- announced
// Convoke/Harmonize contributions, X, Delve, each Sac and Discard part --
// stopping (and returning) the instant a stage asks a KChoose;
// commitCast runs once every stage has settled. A nil e.cast (a chooseCast
// answer arriving with no flow in progress, only reachable from a
// hand-built decision) is dropped rather than panicked on, mirroring
// castAnswer's own guard.
func (e *Engine) continueCast() {
	if e.cast == nil {
		return
	}
	// Mutate (CR 702.140b): the over/under placement choice is announced
	// before payment the same way the replicate count is, so the pay-time
	// CastInfo can carry FlagMutatedTop.
	if e.mutatePlaceAsk() {
		return
	}
	if e.altAddAsk() {
		return
	}
	// CR 702.168: the Gift promise is announced as a free cast-time choice.
	// It must settle before CR 601.2c's target ask, because a TargetMin$/Max$
	// X bound of Count$PromisedGift.2.1 resolves when that ask is built -- a
	// promise settled after targeting would be invisible to it.
	if e.giftAsk() {
		return
	}
	// A RaiseCost part counted by a named announcement (the March cycle,
	// Explosive Singularity) announces its count before every stage that
	// pays or prices it: the tap election below, xAsk's discounted X bound
	// and the exile pick.
	if e.namedAnnounceAsk() {
		return
	}
	if e.forageAsk() || e.revealCostAsk() || e.revealCostOrChooseAsk() || e.beholdCostAsk() || e.tapPermanentCostAsk() || e.blightCostAsk() {
		return
	}
	// CR 601.2b: the replicate count (CR 702.55a's optional additional cost,
	// paid any number of times) is announced before Convoke/Harmonize and X,
	// whose asks must see and bound against the composed total.
	if e.replicateAsk() {
		return
	}
	// CR 601.2b: the multikicker count (CR 702.43's optional additional cost,
	// paid any number of times) is announced the same way -- no Kicker
	// carrier pairs both keywords (measured over the corpus), so the two
	// asks never coexist on one cast.
	if e.multikickAsk() {
		return
	}
	// CR 601.2b: the squad count (CR 702.66's optional additional cost, paid
	// any number of times) is announced the same way. No corpus carrier pairs
	// Squad with Replicate/Multikicker/Kicker (measured over the 15 K:Squad
	// files), so the asks never coexist on one cast.
	if e.squadAsk() {
		return
	}
	// CR 702.78a: the Conspire tap election (two untapped creatures that
	// share a colour with the spell) is posed before Convoke/X so an elected
	// creature cannot also be announced as a payment source. See conspireAsk.
	if e.conspireAsk() || e.casualtyAsk() {
		return
	}
	// CR 601.2b announces Convoke/Harmonize before X: an announced creature
	// contribution is part of the available payment for X, and cannot be used
	// as a mana source in the later mana window.
	if e.convokeAsk() {
		return
	}
	if e.xAsk() {
		return
	}
	// An announced Blight<X> part's count is the announced X, so its pick is
	// deferred past xAsk (the Sac<X/Spec> / SubCounter<X/Kind> shape) -- the
	// pre-xAsk blightCostAsk above skips it while !pc.xDone.
	if e.blightCostAsk() {
		return
	}
	if e.delveAsk() {
		return
	}
	if e.sacAsk() {
		return
	}
	// The SubCounter cost parts whose removal-target field names a filter ask
	// their payer which permanent the counters come off (Ghave's "remove a
	// +1/+1 counter from a creature you control"), after the announced X
	// exists so the candidate set can require that many counters.
	if e.subCounterAsk() {
		return
	}
	if e.discardAsk() {
		return
	}
	if e.exAsk() {
		return
	}
	if e.returnAsk() {
		return
	}
	if e.putToLibAsk() {
		return
	}
	// CR 601.2b: the ExiledMoveToGrave cost pick (Shelob's "put a creature
	// card exiled with Shelob into its owner's graveyard") runs beside the
	// other non-mana component asks, after the PutToLib ask.
	if e.moveGraveAsk() {
		return
	}
	// Suspend does not put a spell on the stack: its alternate action pays
	// the keyword cost and exiles the card with time counters. Targets are
	// chosen only when its later free cast is announced.
	if e.cast.mode == "suspend" {
		e.payCast()
		return
	}
	// Foretell (CR 702.126a) is the same shape: the {2} special action is not
	// a cast -- no stack push, no targets; the card is exiled face down and
	// its later foretell-cost cast announces its own targets.
	if e.cast.mode == "foretell" {
		e.payCast()
		return
	}
	// Plot (CR 701.34a) is the same shape again: the alternative action is
	// not a cast -- no stack push, no targets; the card is exiled with time
	// counters and its later free cast announces its own targets at sorcery
	// timing.
	if e.cast.mode == "plot" {
		e.payCast()
		return
	}
	// CR 601.2a: the object reaches the stack before the target choice
	// (601.2c) and payment (601.2h). For a spell the cast trigger (601.2i)
	// is held back until payCast; an ability's AbilityPush fires no trigger.
	if e.pushCast() {
		return
	}
	// castprov3: a provenance-keyed cost modifier (Bilbo's
	// "!wasCastFromYourHand" ReduceCost) is unresolvable before CR 601.2a's
	// push — the offer and option-selection snapshots both denied it (full
	// price, the fail-closed direction) because the priced card had no cast
	// in the log yet. Now the PutOnStack is in the log: when the selection
	// pass evaluated such a static (e.costProvenanceSeen, the
	// noCounterSpend-style transient capture), re-price the pending cast so
	// the payment takes the honest reduction. For every other cast the
	// recompute is byte-identical to the offer snapshot (both are the
	// nil-target base snapshot), so no existing price — and no chain head —
	// moves.
	if pc := e.cast; pc != nil && pc.pushed && !pc.provenanceRepriced && e.costProvenanceSeen {
		pc.provenanceRepriced = true
		pc.mods = e.costModifiers(pc.player, pc.card, spellScope(pc.mode))
	}
	// FlagSuspend is exile provenance, not cast-time state. Clear it when the
	// mandatory free cast starts so a later unrelated exile move cannot revive
	// an old suspension. Plot needs no equivalent: its designation is
	// Object.PlottedTurn, and the exile-departure clear in events.Apply's Move
	// already drops it as the card leaves exile for the stack (CR 701.34c).
	if e.cast.mode == "suspend_cast" && !e.cast.suspendCastClear {
		e.emit(events.Event{Kind: events.CastInfo, Obj: e.cast.card})
		e.cast.suspendCastClear = true
	}
	// CR 601.2b: a modal spell announces its modes after reaching the stack
	// and before targets are chosen or costs are paid. The answer is cached on
	// the proposed spell so targetAsk can inspect the selected mode and
	// resolution can execute it without asking again.
	if e.castModeAsk() {
		return
	}
	// CR 601.2b: announce how each hybrid and Phyrexian pip is paid -- which
	// half of a hybrid, whether a Phyrexian pip is paid with life -- before
	// targets (601.2c) and payment (601.2h). Runs as one decision per pip.
	if e.manaAsk() {
		return
	}
	// An Optional$ ManaConvert permission is a real CR 601.2 choice. It is
	// asked after pip announcements, before targets, and the selected arm is
	// then used consistently by target affordability and final payment.
	if e.manaConvertAsk() {
		return
	}
	// CR 601.2c: choose targets, now that the object is on the stack. An SA
	// with no target (or a zero-minimum one with no legal candidate) asks
	// nothing and the flow proceeds to the chain pre-asks and payCast.
	if e.targetAsk() {
		return
	}
	// alltargeted1: the root had no targets of its own (or none legal), but
	// the SubAbility$ chain may still declare targeting bodies Forge asks
	// before payment, and the CollectEvidence amount reads the union.
	if e.postTargetAsks(e.cast) {
		return
	}
	e.payCast()
}

type castModeCode uint16

const (
	castModeKicked castModeCode = iota + 1
	castModeFuse
	castModeKickedParts
	castModeSurged
	castModeEntwined
	castModeBuyback
	castModeOffspring
	castModeReplicated
	castModeMultikicked
	castModeHarmonize
	castModeSuspend
	castModeSuspendCast
	castModeDefeatCast
	castModePlot
	castModePlotCast
	castModePreparedCopy
	castModeForetell
	castModeForetellCast
	castModeAirbendCast
	castModeFlashback
	castModeMayplay
	castModeMiracle
	castModeEscape
	castModeRetrace
	castModeJumpstart
	castModeAltCostKeyword
	castModeWebSlinging
	castModeSneak
	castModeMayhem
	castModeMutated
	castModeMorphed
	castModeMayflash
	castModeEmerged
)

var castModeCodes = state.NewStrCodes(
	state.StrEntry[castModeCode]{Key: "kicked", Val: castModeKicked},
	state.StrEntry[castModeCode]{Key: "fuse", Val: castModeFuse},
	state.StrEntry[castModeCode]{Key: "kicked1", Val: castModeKickedParts},
	state.StrEntry[castModeCode]{Key: "kicked2", Val: castModeKickedParts},
	state.StrEntry[castModeCode]{Key: "kickedboth", Val: castModeKickedParts},
	state.StrEntry[castModeCode]{Key: "surged", Val: castModeSurged},
	state.StrEntry[castModeCode]{Key: "entwined", Val: castModeEntwined},
	state.StrEntry[castModeCode]{Key: "buyback", Val: castModeBuyback},
	state.StrEntry[castModeCode]{Key: "offspring", Val: castModeOffspring},
	state.StrEntry[castModeCode]{Key: "replicated", Val: castModeReplicated},
	state.StrEntry[castModeCode]{Key: "multikicked", Val: castModeMultikicked},
	state.StrEntry[castModeCode]{Key: "harmonize", Val: castModeHarmonize},
	state.StrEntry[castModeCode]{Key: "suspend", Val: castModeSuspend},
	state.StrEntry[castModeCode]{Key: "suspend_cast", Val: castModeSuspendCast},
	state.StrEntry[castModeCode]{Key: "defeat_cast", Val: castModeDefeatCast},
	state.StrEntry[castModeCode]{Key: "plot", Val: castModePlot},
	state.StrEntry[castModeCode]{Key: "plot_cast", Val: castModePlotCast},
	state.StrEntry[castModeCode]{Key: "prepared_copy", Val: castModePreparedCopy},
	state.StrEntry[castModeCode]{Key: "foretell", Val: castModeForetell},
	state.StrEntry[castModeCode]{Key: "foretell_cast", Val: castModeForetellCast},
	state.StrEntry[castModeCode]{Key: "airbend_cast", Val: castModeAirbendCast},
	state.StrEntry[castModeCode]{Key: "flashback", Val: castModeFlashback},
	state.StrEntry[castModeCode]{Key: "mayplay", Val: castModeMayplay},
	state.StrEntry[castModeCode]{Key: "miracle", Val: castModeMiracle},
	state.StrEntry[castModeCode]{Key: "escape", Val: castModeEscape},
	state.StrEntry[castModeCode]{Key: "retrace", Val: castModeRetrace},
	state.StrEntry[castModeCode]{Key: "jumpstart", Val: castModeJumpstart},
	state.StrEntry[castModeCode]{Key: "evoked", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "dashed", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "overloaded", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "warped", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "madness", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "bestowed", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "blitzed", Val: castModeAltCostKeyword},
	state.StrEntry[castModeCode]{Key: "web-slinging", Val: castModeWebSlinging},
	state.StrEntry[castModeCode]{Key: "sneak", Val: castModeSneak},
	state.StrEntry[castModeCode]{Key: "mayhem", Val: castModeMayhem},
	state.StrEntry[castModeCode]{Key: "mutated", Val: castModeMutated},
	state.StrEntry[castModeCode]{Key: "morphed", Val: castModeMorphed},
	state.StrEntry[castModeCode]{Key: "megamorphed", Val: castModeMorphed},
	state.StrEntry[castModeCode]{Key: "disguised", Val: castModeMorphed},
	state.StrEntry[castModeCode]{Key: "mayflash", Val: castModeMayflash},
	state.StrEntry[castModeCode]{Key: "emerged", Val: castModeEmerged},
)

type castKickerModeCode uint16

const (
	castKickerModeKicked1 castKickerModeCode = iota + 1
	castKickerModeKicked2
)

var castKickerModeCodes = state.NewStrCodes(
	state.StrEntry[castKickerModeCode]{Key: "kicked1", Val: castKickerModeKicked1},
	state.StrEntry[castKickerModeCode]{Key: "kicked2", Val: castKickerModeKicked2},
)
