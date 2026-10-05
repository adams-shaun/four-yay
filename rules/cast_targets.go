package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// modeIsKicked reports whether a pendingCast.mode names a kicked cast: the
// single-cost Kicker's "kicked", the and/or Kicker's per-part "kicked1"/
// "kicked2"/"kickedboth", or a multikicked cast. It is the ONE home of that
// set, shared by modeFlags (the pay-time flag stamp), spellConstraintMatches'
// CastStatic match and targetBoundCtx's pre-payment Count$Kicked binding, so
// the three spellings cannot drift.
func modeIsKicked(mode string) bool {
	return modeIsKickedSet.Has(mode)
}

// modeFlags maps a pendingCast.mode to the CastInfo Counter string
// (events.FlagsString of the matching CastFlags bit), "" for a plain cast.
func modeFlags(mode string) string {
	switch castModeCodes.Code(mode) {
	// Modes whose flag is the whole of their provenance read the ONE cast
	// mode table instead of repeating the word in modeFlagsCodes.
	case castModeSurged:
		return events.FlagsString(state.FlagSurged)
	case castModeMayplay:
		return events.FlagsString(state.FlagMayPlay)
	case castModeHarmonize:
		return events.FlagsString(state.FlagHarmonize)
	}
	if castModeCodes.Code(mode) == castModeBargained {
		// Bargain (CR 702.166, rules/optional_sacrifice.go): the election's
		// provenance, read by the Count$Bargained/Count$Bargain heads, the
		// bare Condition$ Bargain gate, the `bargained` predicate and the
		// Spell.Bargain cost constraint. A CastProvenanceFlag: a stack copy
		// (never cast, CR 707.10) does not inherit it. A bargain whose
		// candidates vanished resets the mode before payment.
		return events.FlagsString(state.FlagBargained)
	}
	if m := altCastFor(mode); m != nil {
		// The alternative-cost keyword family (rules/altcast_modes.go): the
		// row's flag is what the ETB machinery reads (altCostEnter: evoke's
		// sacrifice, dash's haste + return, blitz's riders, warp's exile,
		// impending's counters; resolveTop's bestow substitution). Blitz,
		// warp and impending are CastProvenanceFlags, so a stack copy does
		// not inherit them. A row without a flag (madness) records nothing.
		if m.flag == 0 {
			return ""
		}
		return events.FlagsString(m.flag)
	}
	switch modeFlagsCodes.Code(string(mode)) {
	case modeFlagsKicked:
		return events.FlagsString(state.FlagKicked)
	// The and/or Kicker's per-part modes: each index flag rides with the
	// bare FlagKicked (every part paid IS a kicked cast -- the bare
	// predicate and the Condition$ Kicked gate keep matching), so the
	// CastInfo wire carries the part's identity and the generic read.
	case modeFlagsKicked1:
		return events.FlagsString(state.FlagKicked | state.FlagKicked1)
	case modeFlagsKicked2:
		return events.FlagsString(state.FlagKicked | state.FlagKicked2)
	case modeFlagsKickedboth:
		return events.FlagsString(state.FlagKicked | state.FlagKicked1 | state.FlagKicked2)
	case modeFlagsFlashback:
		return events.FlagsString(state.FlagFlashback)
	// Jump-start (CR 702.84a): like flashback, the flag is what the
	// resolution reader (spellRestZone) and the fizzle reader
	// (spellFizzleZone) read to exile the card instead of the graveyard, on
	// resolution AND when countered -- the card may not be jump-started a
	// second time from the graveyard.
	case modeFlagsJumpstart:
		return events.FlagsString(state.FlagJumpstart)
	// Aftermath (CR 702.85a): the flag is what the resolution reader
	// (spellRestZone) and the fizzle reader (spellFizzleZone) read to exile
	// the card instead of the graveyard -- on resolution AND when countered,
	// the same "any time it would leave the stack" convention flashback's
	// TestFlashbackedSpellCounteredGoesToExile pins.
	case modeFlagsAftermath:
		return events.FlagsString(state.FlagAftermath)
	// Fuse (CR 702.101b): one spell resolving both halves. The flag is the
	// provenance rules/stack.go's resolution reader dispatches on to run both
	// faces' spell abilities instead of the single Face().SpellAbility().
	// The card stays at its front face, so no face-flip reader is involved.
	case modeFlagsFuse:
		return events.FlagsString(state.FlagFused)
	case modeFlagsMiracle:
		return events.FlagsString(state.FlagMiracle)
	case modeFlagsEscape:
		return events.FlagsString(state.FlagEscaped)
	// The Adventure spell face's cast (CR 714.3a): the flag is what the
	// resolution reader (spellRestZone) uses to exile the spell into the
	// adventure zone instead of the graveyard. adventure_recast deliberately
	// has NO case here -- casting the main face from the adventure zone is an
	// ordinary cast, exactly like warp_recast.
	case modeFlagsAdventureAlt:
		return events.FlagsString(state.FlagAdventure)
	case modeFlagsBuyback:
		return events.FlagsString(state.FlagBuyback)
	// Offspring (CR 702.175a): the mode marks the intent to pay the optional
	// ADDITIONAL offspring cost, and the offer exists only when it is payable
	// (rules/legal.go's walks), so -- unlike Squad/Multikicker/Replicate,
	// whose count asks can still answer 0 -- there is no decline case and the
	// flag is unconditional. Bare FlagOffspringPaid rides the ordinary
	// pay-time CastInfo (payCast), and the keyword expansion's ETB trigger
	// reads it through Count$OffspringPaid to mint the 1/1 token copy.
	case modeFlagsOffspring:
		return events.FlagsString(state.FlagOffspringPaid)
	case modeFlagsOptionalcost:
		return events.FlagsString(state.FlagOptionalCostPaid)
	case modeFlagsSuspend:
		return events.FlagsString(state.FlagSuspend)
	// Foretell's later cast (CR 702.126a): the flag is the provenance an ETB
	// reader (Lupine Harbingers' CheckSVar$ WasForetold) and Count$Foretold
	// read off the permanent the spell becomes -- the stack->battlefield
	// persistence the Suspend flag rides too. The {2} ACTION's CastInfo is
	// emitted directly by payCast's foretell branch (which then returns, so
	// the ordinary flags path below is never reached for that mode); the
	// action's flag has no modeFlags case for the same reason suspend's
	// branch does not share this switch.
	case modeFlagsForetellCast:
		return events.FlagsString(state.FlagForetold)
	// Mayhem (the Doom Prevails keyword): the flag is the provenance the
	// Card.CastSa Spell.Mayhem condition reads (Sandman's Quicksand's "if
	// this spell's mayhem cost was paid" split), through the CastSa
	// provenance strip (rules/cast_provenance.go's castSaAdmits and the
	// per-event walk in spellsCastThisTurnMatching, effects/conditions.go's
	// conditionMet). Mayhem has no exile tail, so the flag is the whole of
	// what the cast records.
	case modeFlagsMayhem:
		return events.FlagsString(state.FlagMayhem)
	// Web-slinging (CR 702.186a-family, Marvel's Spider-Man): the flag is the
	// provenance the Card.Self+webSlinged filter predicate reads -- Spiders-Man,
	// Heroic Horde's ETB trigger and Scarlet Spider, Ben Reilly's Sensational
	// Save replacement. It is a CastProvenanceFlag (state/object.go), so a
	// stack copy does not inherit it. webslinged_grant_N modes are normalized
	// to this canonical mode in beginCastWith before this switch is reached.
	case modeFlagsWebSlinging:
		return events.FlagsString(state.FlagWebSlinged)
	// Sneak (CR 702.190a): the flag is the provenance the `sneaked` filter
	// predicate reads -- Karai, Future of the Foot, Leonardo, Leader in Blue,
	// Turncoat Kunoichi and The Last Ronin's Technique -- and the marker
	// rules/altcast.go's altCostEnter reads to place the permanent tapped and
	// attacking CR 702.190b's defender. It is a CastProvenanceFlag
	// (state/object.go), so a stack copy does not inherit it. sneaked_grant_N
	// modes are normalized to this canonical mode in beginCastWith before
	// this switch is reached.
	case modeFlagsSneak:
		return events.FlagsString(state.FlagSneaked)
	// Mutate (CR 702.140a): the flag is the provenance the resolution reader
	// uses to merge the spell into its target. modeFlags maps "mutated" to
	// the bare flag; payCast ORs FlagMutatedTop in when the answered placement
	// put the mutating card on top (CR 702.140b).
	case modeFlagsMutated:
		return events.FlagsString(state.FlagMutated)
	// Multikicker (CR 702.43): the mode marks the INTENT to pay the
	// optional multikicker cost, and the count ask (multikickAsk) can still
	// answer 0 -- a DECLINED multikick must stay the byte-identical plain
	// cast, no flag and no event, exactly the "replicated" contract above.
	// When a payment WAS made, payCast ORs bare FlagKicked (a multikicked
	// cast IS a kicked cast) and FlagMultikicked onto the trailing CastInfo.
	case modeFlagsMultikicked:
		return ""
	// Squad (CR 702.66): the mode marks the INTENT to pay the optional squad
	// cost, and the count ask (squadAsk) can still answer 0 -- a DECLINED
	// squad must stay the byte-identical plain cast, no flag and no event,
	// exactly the "replicated"/"multikicked" contract above. When a payment
	// WAS made, payCast ORs FlagSquadPaid onto a trailing CastInfo.
	case modeFlagsSquadded:
		return ""
	// Conspire (CR 702.78a): the mode marks the INTENT to tap two eligible
	// creatures, and the offer can be taken only when they exist, but a
	// DECLINED/plain cast must stay byte-identical -- no flag and no event,
	// exactly the "replicated" contract above. When the tap WAS paid,
	// payCast ORs FlagConspired onto a trailing CastInfo.
	case modeFlagsConspired:
		return ""
	// The morph family's face-down cast (CR 702.37a/702.168a/702.169a): the
	// flag is the provenance that names the keyword family the {3} cast
	// rode, what the resolution reader (rules/stack.go resolveTop) dispatches
	// on to resolve the spell with no printed spell abilities and no
	// targets, what rules/resolution.go's moveResolvedOffStack re-carries
	// the face-down entry marker for, and what a later turn-face-up action
	// prices its cost from. The three sibling bits shape the resolution the
	// FlagFused/FlagBestowed way, so they are deliberately NOT in
	// CastProvenanceFlags.
	case modeFlagsMorphed:
		return events.FlagsString(state.FlagMorphed)
	case modeFlagsMegamorphed:
		return events.FlagsString(state.FlagMegamorphed)
	case modeFlagsDisguised:
		return events.FlagsString(state.FlagDisguised)
	}
	return ""
}

// flashPermittedCandidates narrows a cast's target pool to the candidates
// that the face's target-conditional CastWithFlash grant covers. It is the
// ask-side twin of the offer's castWithFlash existential and the CR 601.2e
// recheck: an off-sorcery cast whose timing rests on
// `ValidSA$ Spell.IsTargeting Valid <spec>` may only announce a target matching
// <spec>, so handing the player a target the grant does not cover would
// guarantee the post-push reversal. Callers gate the call on
// flashGrantCoversTargets(pc.player, pc.card, f, nil), which is false exactly
// for the shape where the grant IS the timing basis -- an instant, a printed
// Flash or a MayFlashSac rider keeps its full pool unchanged.
func (e *Engine) flashPermittedCandidates(pc *pendingCast, f *cards.Face, candidates []targetCandidate) []targetCandidate {
	out := make([]targetCandidate, 0, len(candidates))
	for _, c := range candidates {
		t := state.Target{Obj: c.obj}
		if c.kind == "player" {
			t = state.Target{Player: c.player, IsPlayer: true}
		}
		if e.flashGrantCoversTargets(pc.player, pc.card, f, []state.Target{t}) {
			out = append(out, c)
		}
	}
	return out
}

// targetAsk is the last stage of continueCast before commitCast: it asks the
// spell or activated ability's target selection (CR 601.2c / 602.2b) while the
// proposal is still provisional -- BEFORE any cost is paid, any sacrificial
// permanent moves, or the object is put on the stack. That ordering is what
// makes the cast a transaction: the target answer (601.2c) precedes payment
// (601.2h), and the cast trigger (601.2i, fired by PutOnStack) waits until the
// proposal is complete. handleTarget (stack.go) completes the transaction by
// calling commitCast and then records the chosen targets onto the object that
// actually reached the stack.
//
// It returns true when it either asked a target decision or ABORTED the
// proposal. A proposal that can never complete is reversed here, before
// anything has been paid or moved (CR 733.1): the card left the zone, the
// resolved mana cost is no longer payable, or a mandatory target (min >= 1)
// has zero legal candidates. Clearing e.cast with nothing committed restores
// the pre-proposal board. An SA with no ValidTgts (or a zero-minimum target
// with no legal candidate, Requirement N2) returns false so commitCast runs
// directly.
func (e *Engine) targetAsk() bool {
	pc := e.cast
	if pc == nil || pc.passedTarget {
		// passedTarget: the flow has already moved through the 601.2c target
		// choice into payCast (a spell or ability whose target was chosen, or
		// one with no target); a mana-window resume re-enters continueCast and
		// must not re-ask for a target already settled.
		return false
	}
	if pc.faceDown {
		// Morph family (CR 708.4): a face-down spell has no targets to
		// announce -- the printed targets do not exist while the spell is
		// face down. The flow proceeds to payCast; the resolution reader
		// (resolveTop's morph dispatch) skips the printed spell abilities the
		// same way.
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil {
		return false
	}
	f := o.Face()
	sa := e.castStageSA(pc, o, f)
	// A Fuse half that declares no targets (Alive // Well's Well) is skipped:
	// advance to the next stage while one remains, so a half with a target
	// requirement is still asked. When no stage declares targets the flow
	// proceeds directly to payment, exactly as a single targetless cast does.
	for sa != nil && !effects.TargetsOf(sa).Targeted() && e.castHasNextTargetStage(pc, o) {
		pc.targetStage++
		sa = e.castStageSA(pc, o, f)
	}
	if sa == nil || !effects.TargetsOf(sa).Targeted() {
		return false
	}
	// A proposal whose resolved mana cost can no longer be paid, and with no
	// untapped mana-ability source the 601.2g window could activate to enable
	// it, can never complete. Reverse it (CR 733.1), which undoes the pushCast
	// stack move (E2: no progress was made, so hold this card's option out of
	// the window rather than re-offering the same unpayable cast). When such a
	// source exists, the window (asked later, in payCast) may still supply the
	// mana, so the proposal is not yet dead -- it proceeds to the target ask
	// and then the window. Resolved means X is fixed, the CR 601.2f modifiers
	// are applied and Delve credit is subtracted, all settled by the stages
	// above.
	mana := e.castPaymentMana(pc)
	// resolvedMana carries no live pip at this stage (the pip announcements
	// are already settled, manaAsk runs before targetAsk), so the composed
	// payable check here is the same composition manaToPay charges;
	// paymentMana additionally folds the announced Convoke/Harmonize
	// contributions in (zero when none were announced). costPayable is the
	// conversion-aware equivalent: the SAME resolveMana payManaConvFor will
	// run, including RestrictValid$ provenance. The
	// targetDependentCostMayPay arm keeps the ValidTarget$ reducer exception.
	if !pay.CostPayableClass(asPayer(e), pc.player, paymentForCast(pc, mana),
		pipRider{AnyColor: pc.mayPlayIgnore, AnyType: pc.mayPlayIgnoreType}, mana) &&
		!e.hasUntappedManaSource(pc.player) && !e.targetDependentCostMayPay(pc) {
		e.abortCast(pc, "cast aborted: cost no longer payable", true)
		return true
	}
	min, max := e.resolvedTargetBounds(pc.player, pc.card, sa, pc.x)
	// CR 115.5: a spell may not target itself (excludeSelf == the card); an
	// activated ability CAN target its own Source permanent (Mother of Runes
	// targeting itself). The Face-less ability stack object on the stack is
	// never offered (legalTargetCandidates drops Face()-less stack objects),
	// so the source permanent is still a legal target of its own ability.
	// An attach ability (the Equip/Reconfigure expansion mints AB$ Attach;
	// a spell SA may carry API$ Attach directly) can never target its own
	// source permanent: CR 701.3a attaches an object to ANOTHER permanent,
	// and effAttach refuses the self-attach at resolution. The pool and the
	// resolution must agree, so the source is excluded AT THE ASK for an
	// attach SA even though the Mother-of-Runes convention lets a generic
	// activated ability target its own source. For Equip the exclusion is
	// inert (an Equipment face does not match Creature specs); it is live
	// exactly for Reconfigure, whose unattached form IS a creature and was
	// offered itself here (r2 review MAJOR).
	var excludeSelf state.ObjID
	if !pc.isAbility() || sa.API == "Attach" {
		excludeSelf = pc.card
	}
	candidates := e.legalTargetCandidates(pc.player, pc.card, excludeSelf, sa)
	// CR 601.2e: a cast whose off-sorcery timing rests on a target-conditional
	// CastWithFlash grant may only announce a target the grant covers. The
	// offer admitted the cast because SOME qualifying target exists
	// (castWithFlash's potential-target census); without this the ask would
	// still offer every legal target and an uncovered pick would be reversed
	// after the push. Filter to the grant-covered pool so offer and
	// announcement judge the same set. The fuse cast judges each half against
	// its own stage targets in recheckIllegal, so it is left unfiltered here.
	//
	// The permission is EXISTENTIAL over the announced set
	// (spellMatchesValidSA judges the whole target list), so the per-candidate
	// narrowing is exact only for a single-target ask. A multi-target ask
	// whose pool has a proper subset of covered candidates keeps the FULL
	// pool: a legal completion may mix covered and uncovered targets, and
	// pruning the uncovered side would hide legal choices and -- under a
	// mandatory minimum above the covered count -- abort a castable spell
	// (offer census admitted it, the ask could not announce it). The completed
	// selection is judged by recheckIllegal (CR 601.2e) over the same grant,
	// before any cost is paid. With no covered candidate at all the pool
	// empties and the mandatory-minimum census below aborts the proposal (or,
	// at Min 0, the CR 601.2e recheck reverses an untargeted announcement).
	if !pc.isAbility() && pc.offSorcery && f != nil && pc.mode != "fuse" &&
		!e.flashGrantCoversTargets(pc.player, pc.card, f, nil) {
		covered := e.flashPermittedCandidates(pc, f, candidates)
		if len(covered) == len(candidates) || len(covered) == 0 || (min <= 1 && max <= 1) {
			candidates = covered
		}
	}
	// Overload changes the word "target" to "each". It makes no selection at
	// announcement time: the current matching set is derived at resolution,
	// so permanents entering or changing controller in response are handled.
	// No target decision/event is emitted and zero objects is legal.
	if altCastIs(pc.mode, altOverload) {
		return false
	}
	// CR 601.2c: distinct modal modes each declare and choose their own
	// target. The combined decision uses one exclusive Group per mode, so
	// its exact count cannot be satisfied by choosing two targets for one
	// mode while omitting another.
	if pc.targetStage == 0 && !pc.isAbility() && f != nil {
		if root := f.SpellAbility(); root != nil {
			choices := effects.CharmOf(root).Modes
			if status, _ := effects.CharmCrossModeShape(f.SVars, choices); status == effects.CharmUniqueSupported {
				var tbms []*cards.SA
				for _, name := range o.ChosenModes {
					if sub := cards.ResolveSVar(f.SVars, name); sub != nil && effects.TargetsOf(sub).Targeted() {
						tbms = append(tbms, sub)
					}
				}
				if len(tbms) >= 2 && e.askCrossModeCharmTargets(pc.player, pc.card, tbms) {
					return true
				}
			} else {
				asked, infeasible := e.askCharmModeTargets(pc.player, pc.card, f.SVars, root, o.ChosenModes)
				if infeasible {
					e.abortCast(pc, "cast aborted: no legal modal target", true)
					return true
				}
				if asked {
					return true
				}
			}
		}
	}
	// A ValidTarget$ cost modifier can make this proposal offerable only for
	// particular targets. Once mana faces are announced, do not put a target
	// on the menu unless repricing that target can still complete the cast:
	// selecting an unaffordable target and then reversing the proposal is not
	// a legal CR 601.2c choice. An untapped mana source keeps a candidate on
	// the menu because the 601.2g window may make its final cost payable.
	//
	// For a multi-target declaration this is deliberately conservative: a
	// target that needs another selected target to satisfy ValidTarget$ is
	// withheld rather than exposing a selection subset that would abort. The
	// engine's decision type cannot express cross-option dependencies, and
	// withholding is safer than offering an illegal transaction.
	candidates = e.affordableTargetCandidates(pc, candidates)
	// MaxTotalTargetPower$ (Reunion of the House): the running total-power
	// cap over the selection. Prune the candidates that can provably join
	// no legal selection (individually over the cap unless a negative-power
	// candidate could offset them -- Scourge of the Skyclaves's CDA is -1 at
	// a 21-life opponent and 11 + (-1) = 10 is legal under a cap of 10)
	// BEFORE the mandatory-minimum census so a cast whose every candidate
	// alone busts the cap aborts like a targetless one, and carry the
	// running cap as the decision's cumulative budget (Decision.MaxSum over
	// each option's Value = the candidate's power) -- the same wire contract
	// a Dig's WithTotalCMC$ budget uses, so Decision.Validate enforces the
	// cap on every submitted answer and the bot's Clamp/FitRequired repair
	// mirrors it. A candidate whose power alone fits but whose combination
	// busts the cap stays offered: the wire contract rejects the combination.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, pc.player, pc.card, sa, pc.x)
	candidates, cmcCap, cmcCapped := e.totalCMCCappedCandidates(candidates, pc.player, pc.card, sa, pc.x)
	// CR 601.2c/733.1: a root target whose choice leaves a later
	// TargetUnique$ chain link ("another target creature") no distinct legal
	// target can only end in the reversal; withhold it like an unaffordable
	// one (uniqueChainViableCandidates).
	candidates = uniqueChainViableCandidates(e, pc, castSubAskLinks(e, pc, sa), 0, nil, min, candidates)
	// Forge's per-controller selection shapes (TargetsForEachPlayer$ one per
	// player; TargetsWithDifferentControllers$ one per controller): the same
	// bounds/group/capacity read the trigger-path askTarget uses, so a OneEach
	// CAST ask (Unexplained Absence's "up to one target nonland permanent
	// each player controls") offers the whole table's slots and the wire's
	// mutual-exclusion rule enforces one pick per controller. Before this the
	// cast-time ask ignored the shape and capped the ask at the plain Max.
	// Read AFTER affordability and the power-cap prune so `distinct` is the
	// real selectable capacity: an unaffordable or over-cap candidate cannot
	// contribute a controller to it.
	min, max, _, _ = e.oneEachTargetBounds(sa, candidates, min, max)
	min, max, _, sameController := e.sameControllerTargetBounds(sa, candidates, min, max)
	min, max, _, setMode, setKind := e.setPropTargetBounds(sa, candidates, min, max)
	// The ONE feasibility rule (rules/legal.go's targetChoiceFeasible) is the
	// same predicate the cast offer census ran (targetSAAvailable), so an
	// offered cast can always announce a legal target: the count, the
	// per-controller exclusivity/OneEach capacity, the same-controller group
	// capacity and the set-property capacity are judged identically at offer
	// and at the ask.
	if min > 0 && !e.targetChoiceFeasible(sa, candidates, min) {
		// CR 601.2c: a proposal with fewer legal targets than its mandatory
		// minimum -- or one whose per-controller constraint admits fewer
		// distinct controllers than its mandatory minimum -- cannot be
		// announced. Reverse the whole proposal (CR 733.1):
		// the pushed object returns to where it was, nothing is paid and no
		// cast trigger fires. No library was shuffled during the proposal, so
		// the 733.1 library exception does not apply.
		//
		// suppress=true engages the F05-2 (CR 733.2) no-progress discipline
		// like every other abort site: the FIRST identical abort of this card
		// in the window leaves the option offered (a player may still make a
		// play that creates a legal target -- cast a creature, then Shelter),
		// the SECOND holds it out of the window. Without it a seat whose
		// policy keeps re-picking the same castable-but-targetless spell
		// livelocks inside one priority window forever (measured: the bot
		// bench replayed "cast Shelter -> abort" 20000 times, engine note
		// "cast aborted: no legal target", zero state change). Any genuine
		// state change clears the count and the held-out set, so a target
		// created later re-offers the cast normally.
		e.abortCast(pc, "cast aborted: no legal target", true)
		return true
	}
	if min == 0 && len(candidates) == 0 {
		// Requirement N2: a subject that MAY target zero things resolves
		// untargeted when no legal target exists; proceed straight to payCast
		// with no target decision.
		return false
	}
	if max == 0 {
		// A dynamic bound RESOLVED to zero (Tear Asunder's kicked main SA:
		// TargetMin$ X | TargetMax$ X over SVar:X:Count$Kicked.0.1) declares
		// that this stage takes no targets -- the chained sub does the
		// work. A Min 0 / Max 0 ask would offer nothing selectable; skip
		// straight to payCast exactly as the N2 arm above does.
		return false
	}
	// The decision's Source is the object that must not be offered as its own
	// target (CR 115.5). For a spell that is the card (excluded via
	// excludeSelf). For an activated ability the object that may not target
	// itself is the ability stack object, which is not minted yet (the push
	// is a no-op for an ability; payCast's AbilityPush creates it), so Source
	// is 0 and the source permanent remains a legal target of its own ability
	// (Mother of Runes) via excludeSelf == 0. The prompt keeps the source
	// permanent's name for readability.
	var src state.ObjID
	if !pc.isAbility() || sa.API == "Attach" {
		src = pc.card
	}
	// CR 601.2c: TargetingPlayer$ names another player as the chooser for this
	// target declaration. The cast/activation form (Player.Opponent) has no
	// trigger context, so before this the ask silently stayed with the caster.
	// targetAskChooser is the same home askTarget uses, so both the cast flow
	// and the trigger/resolution flow route identically; target legality keeps
	// pc.player as the controller reference below.
	chooser := pc.player
	pickOwed := false
	if who, ok, pick := e.targetAskChooser(pc.player, pc.card, sa); pick {
		pickOwed = true
	} else if ok {
		chooser = who
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KTarget, Min: min, Max: max,
		Prompt: "Choose a target for " + e.targetName(pc.card),
		Source: src, TargetEffect: e.describeTargetEffect(pc.player, pc.card, sa, pc.x),
		TargetsWithSameController: sameController, SetPropMode: setMode}
	if !pc.isAbility() && pc.stackObj == pc.card {
		lim := max
		if lim < 0 || lim > len(candidates) {
			lim = len(candidates)
		}
		d.AffordableTargets = e.striveAffordableTargets(pc, lim)
	}
	for _, candidate := range candidates {
		// Shared with stack.go's askTarget so a Face-less ability object (a
		// TargetType$ Activated/Triggered census) can never nil-deref here.
		label := e.targetOptionLabel(candidate)
		o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: label, Obj: candidate.obj, Player: candidate.player}
		o.Group = e.targetControllerGroup(sa, candidate)
		o.Controller = e.candidateControllerSeat(candidate)
		o.SetProps = e.setPropTokensFor(setKind, candidate)
		// Option.Value is omitempty and read only under a budget
		// (Decision.HasBudget), so a budget-less target ask keeps its wire
		// payload byte-identical. Every present cap -- zero and negative
		// included, via Decision.Budgeted -- rides the wire, so
		// Decision.Validate enforces the total on every submitted answer.
		// The Value is the DERIVED power (Engine.Power), matching the
		// pruning read -- the printed Face().Power() read a CDA creature as
		// zero.
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(e.Power(candidate.obj))
			}
		}
		if cmcCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				if powerCapped {
					o.Value2 = int(co.Face().ManaValue())
				} else {
					o.Value = int(co.Face().ManaValue())
				}
			}
		}
		d.Options = append(d.Options, o)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	if cmcCapped {
		// Preserve the historical Value/MaxSum wire for CMC-only asks.
		// On dual asks power owns the first currency and CMC the second.
		if powerCapped {
			d.MaxSum2, d.Budgeted2 = cmcCap, true
		} else {
			d.MaxSum, d.Budgeted = cmcCap, true
		}
	}
	if pickOwed {
		// The multi-opponent Opponent form (agent-20260925T085158Z-c861188d):
		// the controller first names WHICH opponent answers. The selection
		// ask is posed here, at the cast flow's suspension boundary, and
		// the answer re-enters continueCast, whose targetAsk re-run reads
		// the pin and poses the target ask to the chosen seat.
		e.poseOpponentPick(pc.player, pc.card, sa, oppPickCastRoot)
		return true
	}
	effects.RandomTargetsAsk(e, d, sa)
	e.ask(d)
	return true
}

// allTargets is Forge's AllTargeted$ union (task alltargeted1): the root
// cast-time targets followed by every pre-asked sub-ability chain answer in
// chain order. The cost-evaluation sites that read AllTargeted$ before
// payment (repriceForTargets, evidenceAsk) thread this through
// Ctx.AllTargets; a chain with no sub answers unions to exactly the root's
// own set.
func (pc *pendingCast) allTargets() []state.Target {
	if len(pc.subAns) == 0 {
		return pc.targets
	}
	out := append([]state.Target(nil), pc.targets...)
	for _, ts := range pc.subAns {
		out = append(out, ts...)
	}
	return out
}

// collectSubTargetPreAsks walks the root SA's SubAbility$ chain in order and
// returns the links whose targets are announced as the spell is cast or the
// ability activated (CR 601.2c): every link that declares ValidTgts$, except
// a ChangeZone link whose target zone this census cannot resolve
// (castSubChangeZoneAnnounceable -- effChangeZone's own mid-resolution ask,
// changeZoneChosenTargets, still owns those shapes -- a later stage). The
// graveyard-origin ChangeZone link is announced, not excluded: its public
// graveyard is the one non-battlefield zone the cast census establishes.
//
// A link's Defined$ does not excuse it. `Defined$ You | ValidTgts$ Player`
// (Biomantic Mastery's "another target player", Humble Defector's "target
// opponent gains control") names who ACTS, the target is still a target; the
// mid-resolution path asks for it (chosenTargetsFor) and so does this walk.
// A target-REUSE Defined$ beside a ValidTgts$ (`Defined$ ParentTarget |
// ValidTgts$ ...` -- Fight's and ExchangeControl's two-list bodies, which
// read the answer from Ctx.SubPreAsk; Donate's `Defined$ Targeted`) is a
// second target declaration that no mid-resolution ask ever posed: it is
// announced here.
//
// Deliberately excluded whole: modal (Charm) and CopySpellAbility roots --
// castModeAsk/AskCopyTargets own their targeting -- and trigger bodies (this
// walks the cast flow only; a trigger's chain is CR 603.3d's, announced when
// the trigger is put on the stack, a later stage).
func (e *Engine) collectSubTargetPreAsks(root *cards.SA) []*cards.SA {
	if root == nil || root.Sub == nil || root.API == "Charm" || root.API == "CopySpellAbility" {
		return nil
	}
	var out []*cards.SA
	for sa := root.Sub; sa != nil; sa = sa.Sub {
		if !effects.TargetsOf(sa).Targeted() {
			continue
		}
		// CR 601.2c: a targeted ChangeZone link announces on cast only when
		// the cast census can resolve its target zone (see
		// castSubChangeZoneAnnounceable); the unjudgeable ChangeZone shapes
		// keep their mid-resolution ask (changeZoneChosenTargets).
		if !castSubChangeZoneAnnounceable(sa) {
			continue
		}
		out = append(out, sa)
	}
	return out
}

// castStageSVars returns the SVar table the cast's cost heads resolve
// against: an activation reads the merged pile's table, a spell its face's.
// The same split evidenceAmount and ownReduceCost apply.
func (e *Engine) castStageSVars(pc *pendingCast) map[string]string {
	o := e.G.Obj(pc.card)
	if o == nil {
		return nil
	}
	if pc.isAbility() {
		return e.pileSVars(pc.card, pc.abilityMerged)
	}
	if f := o.Face(); f != nil {
		return f.SVars
	}
	return nil
}

// bodyReadsAllTargeted reports whether v, or any SVar body it reaches, names
// the AllTargeted$ reference -- the cost heads (Wayta's ReduceCost$, Urgent
// Necropsy's CollectEvidence<X>) that read the union of the root's and the
// chain's targets, which is one reason the chain is announced before payment
// (CR 601.2c before 601.2f). It no longer gates the chain announcement (every
// chain is announced on cast now); it is kept as the tested scan of that
// cost shape. v is either a literal count body or an SVar
// name; every identifier-shaped word in an expanded body is followed too
// (Wayta's Count$Compare names its Y operand as a bare word). depth bounds
// the walk so a self- or mutually-referential SVar table terminates. The scan
// walks a SLICE of words and only LOOKS UP svars, so no map iteration order
// can reach the result.
func bodyReadsAllTargeted(v string, svars map[string]string, depth int) bool {
	return bodyReadsRef(v, svars, depth, func(s string) bool {
		return strings.Contains(s, "AllTargeted")
	})
}

// bodyReadsRootTarget reports whether v, or any SVar body it reaches, reads a
// ROOT-target reference (Targeted$ / ParentTarget$ / ThisTargetedCard$ -- the
// names refTargets binds to Ctx.Targets, the ability's OWN chosen targets).
// The AllTargeted$ union is deliberately excluded: it is the sub-ability
// pre-ask's shape (alltargeted1), priced only by repriceForTargets, and this
// predicate arms the offer-time potential-target read for an equip cost
// reduction (CR 702.6), never that union. bodyReadsRef's shared walk means a
// body can never be detected by one predicate and missed by the other's
// ordering; the two only differ in which ref names they accept.
func bodyReadsRootTarget(v string, svars map[string]string, depth int) bool {
	return bodyReadsRef(v, svars, depth, func(s string) bool {
		if strings.Contains(s, "AllTargeted") {
			return false
		}
		return strings.Contains(s, "Targeted$") ||
			strings.Contains(s, "ParentTarget$") ||
			strings.Contains(s, "ThisTargetedCard$")
	})
}

// bodyReadsRef is the shared transitive SVar/word walk both ref predicates
// use. match decides whether a single expanded body names the ref; the walk
// still follows SVar references and identifier-shaped bare words so a ref
// reached only through an indirection (Count$Compare's Y operand) is found.
func bodyReadsRef(v string, svars map[string]string, depth int, match func(string) bool) bool {
	v = strings.TrimSpace(v)
	if v == "" || depth > 4 {
		return false
	}
	if match(v) {
		return true
	}
	if len(svars) == 0 {
		return false
	}
	if b, ok := svars[v]; ok && bodyReadsRef(b, svars, depth+1, match) {
		return true
	}
	for _, w := range strings.FieldsFunc(v, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		if w == v {
			continue
		}
		if b, ok := svars[w]; ok && bodyReadsRef(b, svars, depth+1, match) {
			return true
		}
	}
	return false
}

// postTargetAsks poses the next outstanding CR 601.2c announcement ask AFTER
// the root target stage: the sub-ability chain pre-ask first (alltargeted1),
// then the CollectEvidence amount ask (whose X reads the union the sub
// answers complete). It returns true when it parked the flow on a decision;
// false means every post-target stage is settled and the caller pays.
func (e *Engine) postTargetAsks(pc *pendingCast) bool {
	// The flow is now past the 601.2c ROOT target stage (answered or not
	// asked): mark it so a later continueCast re-entry -- the CollectEvidence
	// answer's own -- does not re-pose the root ask (targetAsk's
	// passedTarget guard). payCast sets the same flag again; idempotent.
	pc.passedTarget = true
	if pc.faceDown {
		// Morph family (CR 708.4): a face-down spell announces no sub targets
		// either -- its printed chain does not exist while face down.
		return false
	}
	if e.subTargetAsk(pc) {
		return true
	}
	return e.evidenceAsk()
}

// subTargetAsk poses the next un-answered chain sub's target ask, collecting
// the chain once (lazily, on the first call after the root stage). The bounds,
// the CR 115.5 self-exclusion and the option shape mirror targetAsk's; a
// TargetUnique$ sub excludes every already-chosen target (root answers and
// earlier sub answers) exactly as the mid-resolution path's accumulator does.
// A Min-0 sub with no legal candidate is recorded as an ANSWERED EMPTY set
// (Requirement N2: nobody could answer differently) and the walk advances; a
// mandatory one aborts the proposal (CR 733.1, the target stage's own rule --
// nothing has been paid yet). The answers ride the ordinary KTarget flow
// (handleTarget's cast_sub branch), so replay re-derives them like any other
// cast-flow answer.
func (e *Engine) subTargetAsk(pc *pendingCast) bool {
	if !pc.subCollected {
		pc.subCollected = true
		var root *cards.SA
		if o := e.G.Obj(pc.card); o != nil {
			if pc.isAbility() {
				root = e.pcAbility(pc)
			} else if f := o.Face(); f != nil {
				root = e.castStageSA(pc, o, f)
			}
		}
		// CR 601.2c: every target of the spell or ability is chosen as it is
		// cast or activated -- the root's and every SubAbility$ link's alike.
		// The chain walk (collectSubTargetPreAsks) names the links. Not
		// announced here:
		//   - Fuse: the two halves use separate target slices and resolution
		//     frames; a stage-0 chain walk cannot attribute the other half's
		//     subs. Its links keep their mid-resolution ask (a later stage).
		//   - Overload: "target" reads "each" (CR 702.96a), so nothing in the
		//     chain is a target at all.
		//   - a link the census cannot judge yet (castSubPreAskable), which
		//     also keeps its mid-resolution ask. A targeted ChangeZone link
		//     the cast census cannot resolve (castSubChangeZoneAnnounceable)
		//     never reaches this list: collectSubTargetPreAsks drops it.
		pc.subAsks = castSubAskLinks(e, pc, root)
		pc.subAns = make([][]state.Target, len(pc.subAsks))
	}
	for pc.subStage < len(pc.subAsks) {
		sub := pc.subAsks[pc.subStage]
		if !castSubTargetsOwed(pc, sub) {
			// CR 601.2c: "if the spell has ... additional costs ... such as
			// kicker ... that will be paid, [targets are required] only if
			// those costs are paid". The link exists only behind the unpaid
			// cost, so it announces no target: an ANSWERED EMPTY set, which
			// also keeps the resolution from re-posing the ask.
			pc.subAns[pc.subStage] = []state.Target{}
			pc.subStage++
			continue
		}
		var excludeSelf state.ObjID
		if !pc.isAbility() || sub.API == "Attach" {
			excludeSelf = pc.card
		}
		candidates := e.legalTargetCandidates(pc.player, pc.card, excludeSelf, sub)
		if effects.TargetUniqueRequested(sub) {
			chosen := append([]state.Target(nil), pc.targets...)
			for i := 0; i < pc.subStage; i++ {
				if effects.TargetUniqueRequested(pc.subAsks[i]) {
					chosen = append(chosen, pc.subAns[i]...)
				}
			}
			filtered := candidates[:0]
			for _, cand := range candidates {
				t := state.Target{Obj: cand.obj, Player: cand.player, IsPlayer: cand.kind == "player"}
				if len(effects.TargetUniqueFilter(sub, []state.Target{t}, chosen)) != 0 {
					filtered = append(filtered, cand)
				}
			}
			candidates = filtered
			// A later unique link must avoid this answer too: withhold a
			// candidate that leaves it nothing (uniqueChainViableCandidates).
			subMin, _ := e.resolvedTargetBounds(pc.player, pc.card, sub, pc.x)
			candidates = uniqueChainViableCandidates(e, pc, pc.subAsks, pc.subStage+1, chosen, subMin, candidates)
		}
		min, max := e.resolvedTargetBounds(pc.player, pc.card, sub, pc.x)
		if min > 0 && len(candidates) < min {
			e.abortCast(pc, "cast aborted: no legal target for a chained ability", true)
			return true
		}
		if min == 0 && len(candidates) == 0 {
			pc.subAns[pc.subStage] = []state.Target{}
			pc.subStage++
			continue
		}
		if max == 0 {
			// A dynamic bound RESOLVED to zero: this stage takes no targets,
			// recorded as an ANSWERED EMPTY set exactly as the N2 arm above.
			pc.subAns[pc.subStage] = []state.Target{}
			pc.subStage++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KTarget, Min: min, Max: max,
			Prompt: "Choose a target for " + e.targetName(pc.card) + "'s chained ability",
			Source: excludeSelf, ResumeKind: "cast_sub",
			TargetEffect: e.describeTargetEffect(pc.player, pc.card, sub, pc.x)}
		pickOwed := false
		if who, ok, pick := e.targetAskChooser(pc.player, pc.card, sub); pick {
			pickOwed = true
		} else if ok {
			d.Player = who
		}
		for _, candidate := range candidates {
			label := e.targetOptionLabel(candidate)
			o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
				Label: label, Obj: candidate.obj, Player: candidate.player}
			o.Group = e.targetControllerGroup(sub, candidate)
			o.Controller = e.candidateControllerSeat(candidate)
			d.Options = append(d.Options, o)
		}
		if pickOwed {
			// The multi-opponent Opponent form: the controller names WHICH
			// opponent answers, then the sub's target ask is re-posed.
			e.poseOpponentPick(pc.player, pc.card, sub, oppPickCastSub)
			return true
		}
		effects.RandomTargetsAsk(e, d, sub)
		e.ask(d)
		return true
	}
	return false
}

// castSubChangeZoneAnnounceable reports whether the cast flow can announce a
// targeted ChangeZone chain link's targets while the spell is being cast
// (CR 601.2c). It returns true for every non-ChangeZone link -- those are
// judged by the ordinary cast flow -- and, for a ChangeZone link, when the
// cast census can resolve its targets: a player-target link (zone-free) or an
// explicit Origin$ Graveyard OBJECT target, either inferred by
// originImpliedTargetZone or declared by TgtZone$ Graveyard. Both routes feed
// targetZones for the offer census, cast ask and CR 608.2b recheck. Other
// origins without a usable explicit zone fall back to the battlefield; other
// explicitly zoned origins remain outside this ticket's graveyard scope.
// Those links keep their mid-resolution ask (changeZoneChosenTargets).
// Cathartic Parting's graveyard link is exactly the admitted shape: its four
// "target cards from your graveyard" are announced with the spell instead of
// asked at resolution. Battlefield-origin object links (a bounce spell's
// second "target creature") are the same announcement class but are NOT
// admitted here: that is a wider change than this ticket scopes, and the
// castCensusChangeZoneCarriers ratchet pins them.
func castSubChangeZoneAnnounceable(sa *cards.SA) bool {
	if sa.API != "ChangeZone" && sa.CompiledAPI() != cards.APIChangeZone {
		return true
	}
	tp := effects.TargetsOf(sa)
	// A player target is not zone-bound: the cast census offers players by
	// the same path every other player-target link uses, so the link's Origin$
	// is irrelevant (a player-targeted hidden search stays supported as a
	// normal player target).
	if effects.SpecTargetsOnlyPlayers(tp.ValidTgts) {
		return true
	}
	// A concrete Graveyard TgtZone$ is authoritative even when Origin$ is
	// also specified (Geth's Summons). Require the matching single origin
	// and an object-only selector: mixed player/object and multi-zone offers
	// cannot be treated as this public-graveyard announcement shape.
	if tp.ZoneText != "" {
		return len(tp.Zones) == 1 && tp.Zones[0] == state.ZGraveyard &&
			!tp.Has(effects.TgtTypeStack) && !tp.Has(effects.TgtValidPlayers) &&
			effects.ChangeZoneOf(sa).OriginExactly(state.ZGraveyard)
	}
	_, ok := originImpliedTargetZone(sa)
	return ok
}

// castSubPreAskable reports whether the cast flow can announce this chain
// link's targets itself. The one shape it cannot: a link whose legality reads
// an EARLIER target of the same spell or ability (Searing Blaze's
// `Creature.ControlledBy ParentTargetedController`, Keeper of the Dead's
// `Creature.nonBlack+TargetedPlayerCtrl`, Goblin Welder's
// TargetsWithDefinedController$ ParentTargetedController, Mogg Assassin's
// TargetingPlayer$ ParentTargetedController). CR 601.2c still wants it on
// cast, relative to the target just chosen -- but the target census
// (candidatesFor -> targetSpecContext) binds the Targeted*/ParentTarget
// referents only for a RESOLVING context, never while an offer is being
// built, so here the pool would come back empty and a mandatory link would
// abort a castable spell. The link keeps the mid-resolution path it has on
// main (where the same census cannot bind the referent either -- measured:
// the ask is never posed). Closing it needs the census to take the
// proposal's already-announced targets; ~12 corpus links.
func (e *Engine) castSubPreAskable(pc *pendingCast, sub *cards.SA) bool {
	return !subTargetingReadsRootTarget(sub, e.castStageSVars(pc))
}

// subTargetingReadsRootTarget reports whether a chain link's target
// declaration is relative to an EARLIER target of the same spell or ability:
// its spec, its controller restriction, its chooser, or a dynamic bound.
func subTargetingReadsRootTarget(sub *cards.SA, svars map[string]string) bool {
	tp := effects.TargetsOf(sub)
	for _, v := range [...]string{tp.ValidTgts, tp.DefinedController, tp.TargetingPlayer} {
		if strings.Contains(v, "Targeted") || strings.Contains(v, "ParentTarget") {
			return true
		}
	}
	return bodyReadsRootTarget(tp.Min.Text, svars, 0) ||
		bodyReadsRootTarget(tp.Max.Text, svars, 0)
}

// castSubTargetsOwed reports whether a chain link behind an optional
// additional cost announces targets for THIS cast. Only the two cost gates
// whose outcome is fixed when the cast option was picked are read -- every
// other Condition*$ (a later discard, a colour of mana spent, a board census)
// is judged as the link resolves and never excuses the announcement.
func castSubTargetsOwed(pc *pendingCast, sub *cards.SA) bool {
	switch cond := strings.TrimSpace(sub.ParamStr(cards.PKCondition)); {
	case strings.EqualFold(cond, "Kicked"):
		return modeIsKicked(pc.mode) || (pc.multikickSet && pc.multikickTimes > 0)
	case strings.EqualFold(cond, "OptionalCost"):
		return pc.mode == "optionalcost"
	}
	return true
}

// answerCastSubTarget records a cast_sub KTarget answer against the stage it
// was asked for and re-prices (the union grew, so a target-dependent
// ReduceCost$ may now apply -- the net form is idempotent, which matters
// because repriceForTargets already ran on the root answer).
func (e *Engine) answerCastSubTarget(pc *pendingCast, chosen []decision.Option) {
	if pc.subStage >= len(pc.subAsks) {
		return
	}
	pc.subAns[pc.subStage] = targetOptions(chosen)
	pc.subStage++
	e.repriceForTargets(pc)
	// A spell is already on the stack (pushCast), so the chain target is
	// recorded now, before payment (601.2c before 601.2h) -- exactly when the
	// root's is. An activated ability's object is minted by payCast's
	// AbilityPush; recordCastSubTargets runs there for it.
	if !pc.isAbility() && pc.stackObj != 0 {
		e.recordSubTargets(pc.stackObj, pc.subAns[pc.subStage-1])
	}
}

// recordSubTargets emits the TargetsChosen events for one chain link's
// announced targets (CR 601.2c). They are real targetings -- ward (CR
// 702.21a), "becomes the target" and the crime check (CR 700.13) match them
// like the root's -- but carry events.SubTargetNotice so the fold appends to
// Object.SubTargets instead of the root's Targets. One call is one targeting
// batch (BecomesTargetOnce), the same bracket recordChosenTargets opens.
func (e *Engine) recordSubTargets(obj state.ObjID, ts []state.Target) {
	if len(ts) == 0 {
		return
	}
	e.openTargetBatch()
	defer e.closeTargetBatch()
	for _, t := range ts {
		ev := events.Event{Kind: events.TargetsChosen, Obj: obj, Amount: 2, Text: events.SubTargetNotice}
		if t.IsPlayer {
			ev.Amount, ev.Player = 3, t.Player
		} else {
			ev.IDs = []state.ObjID{t.Obj}
		}
		e.emit(ev)
	}
}

// recordCastSubTargets records every answered chain link's targets onto an
// activated ability's freshly minted stack object, in chain order.
func (e *Engine) recordCastSubTargets(pc *pendingCast) {
	if pc.stackObj == 0 {
		return
	}
	for _, ts := range pc.subAns {
		e.recordSubTargets(pc.stackObj, ts)
	}
}

// installSubPreAsk publishes the answered sub-ask record onto the stack
// object that will resolve the chain (spells: pushed by pushCast, so the id
// exists at payCast entry; abilities: minted by payCast's AbilityPush, so
// the ability arm installs after it). The record is Engine.castSubTargets;
// resolution attaches it through Ctx.SubPreAsk and chosenTargetsFor consumes
// it line by line. Empty slices are real answered-zero records and must
// install too, or the resolution would re-ask the sub.
func (e *Engine) installSubPreAsk(pc *pendingCast) {
	if pc.stackObj == 0 || len(pc.subAsks) == 0 {
		return
	}
	m := make(map[string][]state.Target, len(pc.subAsks))
	for i, sa := range pc.subAsks {
		var ts []state.Target
		if i < len(pc.subAns) {
			ts = pc.subAns[i]
		}
		if ts == nil {
			ts = []state.Target{}
		}
		m[sa.Line] = ts
	}
	if e.castSubTargets == nil {
		e.castSubTargets = make(map[state.ObjID]map[string][]state.Target)
	}
	e.castSubTargets[pc.stackObj] = m
}

// evidenceAsk poses the CollectEvidence payment (alltargeted1): the payer
// exiles cards from their OWN graveyard whose total mana value reaches the
// resolved amount (CR 701.30b, the same action the Ward evidence payment
// performs). The amount is resolved once, here, against the settled target
// union -- the corpus carrier's SVar reads AllTargeted$CardManaCost, which is
// exactly why this stage runs after the sub pre-asks. The ask offers the
// graveyard ordered by mana value DESCENDING (ties by object id, so the
// order is deterministic) with Min at the greedy minimum card count, so the
// deterministic bot's first-Min answer always reaches the amount and the
// settlement validation below never rejects it; a human may pick any
// combination of at least Min cards. An answer whose total falls short is
// rejected and the ask re-posed (the same settle-validate shape the Ward
// evidence payment uses); a graveyard that cannot reach the amount at all
// aborts the proposal (CR 733.1 -- nothing has been paid yet).
func (e *Engine) evidenceAsk() bool {
	pc := e.cast
	if pc == nil || len(pc.cost.Evidence) == 0 || pc.evidenceSettled {
		return false
	}
	if !pc.evidenceResolved {
		pc.evidenceN = e.evidenceAmount(pc)
		pc.evidenceResolved = true
	}
	if pc.evidenceN <= 0 {
		// Nothing owed: a zero-target cast, or a body the count evaluator
		// cannot resolve (its degrade-to-zero convention -- the evidence is
		// never silently over-charged).
		pc.evidenceSettled = true
		return false
	}
	if len(pc.evidence) > 0 {
		valid := true
		for _, id := range pc.evidence {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard || o.Owner != pc.player {
				valid = false
				break
			}
		}
		if valid && evidenceManaValue(e.G, pc.player, pc.evidence) >= pc.evidenceN {
			pc.evidenceSettled = true
			return false
		}
		pc.evidence = nil
		e.emit(events.Event{Kind: events.Note, Player: pc.player,
			Text: "evidence selection's total mana value is too low; choose again"})
	}
	candidates := evidenceGraveCandidates(e.G, pc.player, nil)
	if evidenceManaValue(e.G, pc.player, candidates) < pc.evidenceN {
		e.abortCast(pc, "evidence cost no longer payable; cast aborted", true)
		return true
	}
	// Mana value descending, ties by object id: the option order makes the
	// greedy minimum achievable by the FIRST Min options, which is what both
	// the deterministic bot's generic KChoose arm and Clamp's top-up take.
	ids := evidenceOrder(e.G, candidates)
	min := evidenceGreedyMin(e.G, ids, pc.evidenceN)
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: min, Max: len(ids),
		Prompt: "Exile evidence with total mana value " + strconv.Itoa(int(pc.evidenceN)) +
			" to cast " + e.targetName(pc.card), Source: pc.card}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "evidence",
			Obj: id, Label: e.G.Obj(id).Face().Name, Value: int(e.G.Obj(id).Face().Cmc())})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// evidenceAmount resolves the CollectEvidence amount against the settled
// target union. A literal part prices itself; a named part resolves through
// the source face's SVar table via effects.EvalCountOK -- the same resolver
// ownReduceCost uses -- with Ctx.AllTargets bound so an AllTargeted$ body
// reads the whole chain union. A body the evaluator cannot resolve
// contributes 0 (the degrade convention); the total is clamped at 0.
func (e *Engine) evidenceAmount(pc *pendingCast) int32 {
	o := e.G.Obj(pc.card)
	if o == nil {
		return 0
	}
	var svars map[string]string
	if pc.isAbility() {
		svars = e.pileSVars(pc.card, pc.abilityMerged)
	} else if f := o.Face(); f != nil {
		svars = f.SVars
	}
	total := int32(0)
	for _, part := range pc.cost.Evidence {
		if part.Dyn == "" {
			total = addClampedGeneric(total, int64(part.N))
			continue
		}
		body, ok := svars[part.Dyn]
		if !ok {
			continue
		}
		ctx := effects.NewCtxPtr(pc.card, pc.player, effects.CtxInit{SVars: svars, Targets: pc.targets})
		ctx.AllTargets = pc.allTargets()
		if n, ok := effects.EvalCountOK(e, ctx, body); ok && n > 0 {
			total = addClampedGeneric(total, int64(n))
		}
	}
	return total
}

// activationPushEvent names the replayable activation boundary for both
// printed and keyword-granted abilities. Use it for spend riders too: a
// synthetic AbilityPush with ability=-1 cannot describe a granted body.
//
// A K:Ninjutsu activation's captured defender (pc.ninjutsuDefender) rides the
// event's IDs: events.Apply's AbilityPush arm decodes a PlayerRef sentinel
// into the ability object's Remembered, and rules/stack.go re-binds it to the
// resolving Ctx's DefendingPlayer so the ChangeZone body's Attacking$ True
// rider places the permanent attacking the returned creature's defender.
func (pc *pendingCast) activationPushEvent(e *Engine) events.Event {
	if pc.grantKeyword != "" {
		return events.Event{Kind: events.KeywordAbilityPush, Player: pc.player,
			Obj: pc.card, Counter: pc.grantKeyword}
	}
	ev := events.Event{Kind: events.AbilityPush, Obj: pc.card,
		Player: pc.player, Amount: int32(pc.ability)}
	if e.activationIsNinjutsu(pc) && pc.ninjutsuHasDefender {
		ids := []state.ObjID{state.PlayerRef(pc.ninjutsuDefender)}
		if pc.ninjutsuDefenderObject != 0 {
			ids = append(ids, pc.ninjutsuDefenderObject)
		}
		ev.IDs = ids
	}
	return ev
}

// activationIsNinjutsu reports whether pc is a K:Ninjutsu activation: the
// activated ability's expansion (cards/kw_ninjutsu.go) stamps Keyword$
// Ninjutsu, the same tag rules/statics.go's abilityConstraintMatches reads.
// Only a printed face-ability activation carries the tag, so pc.ability alone
// resolves the SA; a stale index or a granted body is not ninjutsu.
func (e *Engine) activationIsNinjutsu(pc *pendingCast) bool {
	if pc == nil || pc.ability < 0 || pc.grantKeyword != "" || pc.gainedFrom != 0 {
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil {
		return false
	}
	pa, ok := o.PileAbilityAt(pc.ability)
	if !ok || pa.SA == nil {
		return false
	}
	return pay.SaHasKeyword(pa.SA, "Ninjutsu")
}

// finishTargetedCast is the completion tail every cast-flow target answer
// converges on once no post-target ask is outstanding. payCast records an
// ability's root targets once its stack object is minted (possibly after a
// mana window); a spell's targets were already recorded before the park.
// The tail carries the CR 117.3c priority discipline the root arm
// always owned: the caster keeps priority only once no announcement decision
// is outstanding, and a trigger drain parked on the target ask resumes
// through its own continuation.
func (e *Engine) finishTargetedCast(pc *pendingCast, player state.PlayerID) {
	// targetedFinish tells payCast's ability arm that THIS function owns the
	// mana-spent dispatch below. An ability whose root declares no target but
	// whose chain link was asked reaches here with rootOpts nil, and payCast's
	// own "no target-recording continuation" dispatch would fire the rider a
	// second time. Cleared on return so a payment parked in the 601.2g window
	// (no stack object yet, nothing dispatched here) dispatches from the
	// resumed payCast instead.
	pc.targetedFinish = true
	e.payCast()
	pc.targetedFinish = false
	// payCast closes the proposal after creating the stack object (and has
	// already recorded an ability's root targets, on either the immediate pay
	// path or a resumed payCast). Keep its completed target bindings available
	// while the deferred spend rider matches, then close it again before
	// control returns to the host. A proposal still parked in the 601.2g mana
	// window has minted no stack object yet, so the guard also keeps the
	// dispatch off a suspended payment.
	if pc.isAbility() && pc.stackObj != 0 {
		e.cast = pc
		e.fireManaSpentTriggers(pc.activationPushEvent(e), nil)
		e.cast = nil
	}
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
	} else if e.pending == nil {
		e.emit(events.Event{Kind: events.Priority, Player: player, Amount: 0})
	}
}

// pushCast implements CR 601.2a: the card reaches the stack BEFORE the
// target choice (601.2c) and payment (601.2h), which is what makes the
// transaction match the CR's ordered list. The cast trigger (601.2i) is held
// back by emit (deferCastTrigger) because it fires only once the spell is
// actually cast -- after payment; payCast's fireDeferredCastTrigger re-walks
// the held PutOnStack. CastInfo (the X / mode-flag recording) is deferred to
// payCast too, so an aborted proposal leaves no cast-time trace on the card.
// An activated ability is a no-op here: CR 602.2b imports 601.2 but its
// stack object is minted by payCast's AbilityPush AFTER the target and cost
// settle, and an aborted activation must reverse with no stack object left
// behind (CR 733.1) -- pushing it first would strand a Face-less object in
// exile. A land play never goes on the stack.
//
// It returns true when the cast cannot proceed at all (the card left its
// zone before the push) and was aborted; in that case nothing was pushed, so
// no reversal is owed.
func (e *Engine) pushCast() bool {
	pc := e.cast
	if pc == nil || pc.mode == "land" || pc.mode == "suspend" || pc.mode == "plot" || pc.isAbility() {
		return false
	}
	if pc.pushed {
		// The object is already on the stack (a mana-window resume re-enters
		// continueCast); do not push it a second time.
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Zone != pc.from {
		e.cast, e.choosing = nil, chooseNone
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Text: "cast aborted: the card moved"})
		return true
	}
	// Capture the held-out suppression set before the push, so an aborted
	// (reversed) cast can restore it -- the push below is a state-changing
	// event that emit treats as progress and clears the set, yet an aborted
	// cast is net no progress.
	pc.preSuppress = e.suppressedCast
	pc.preAborts = e.castAborts
	// CR 601.2a: the spell reaches the stack. The cast trigger is held back
	// (deferCastTrigger) so it cannot fire before the spell is paid for.
	e.deferCastTrigger = true
	ev := events.Event{Kind: events.PutOnStack, Obj: pc.card, Player: pc.player, From: pc.from, To: state.ZStack, Text: o.Face().Name}
	if pc.faceDown {
		// Morph family (CR 708.4): the face-down spell's identity is hidden
		// while it sits on the stack. The face-down entry marker rides the
		// Counter so events.Apply folds Object.FaceDown onto the stack
		// object (the view then redacts the card for everyone but the
		// caster, and moveResolvedOffStack re-carries the marker onto the
		// battlefield entry), and Secret keeps the printed name out of the
		// other seats' event projections -- a non-Secret PutOnStack would
		// name the card in Text to every viewer. The cloak marker is the
		// Disguise entry's carrier (its face-down ward {2} rides the same
		// state bit the Cloak machinery reads). No ordinary cast carries a
		// Counter here, so unrelated casts are byte-identical.
		ev.Counter = events.FaceDownEntryCounter
		if pc.mode == "disguised" {
			ev.Counter = events.CloakEntryCounter
		}
		ev.Secret = true
	}
	e.emit(ev)
	// CR 702.190b: a sneak cast captured the defender its returned attacker
	// was attacking. Fold it onto the now-existing stack object's dedicated
	// SneakDefender field (a Choose "sneak-defender" event, NOT the generic
	// Remembered channel -- card memory can otherwise carry a stale player),
	// and the stack->battlefield move preserves it, so altCostEnter's entry
	// hook can place the permanent tapped and attacking that defender. Only a
	// sneak cast that actually paid the Return cost emits; every unrelated
	// cast stays byte-identical.
	if pc.sneakHasDefender {
		ids := []state.ObjID{state.PlayerRef(pc.sneakDefender)}
		if pc.sneakDefenderObject != 0 {
			ids = append(ids, pc.sneakDefenderObject)
		}
		e.emit(events.Event{Kind: events.Choose, Obj: pc.card, Player: pc.player,
			Counter: "sneak-defender", IDs: ids})
	}
	e.deferCastTrigger = false
	// CR 722.3c: the prepared permanent loses its designation "at the time
	// the spell becomes cast" (CR 601.2i) -- here, as the copy reaches the
	// stack, never on resolution. Apply's fold clears Object.Prepared, so the
	// copy is no longer offered; the orphaned exile copy is not re-offered
	// because its source no longer answers Prepared.
	if pc.mode == "prepared_copy" {
		if cp := e.G.Obj(pc.card); cp != nil && cp.PreparedSource != 0 {
			e.emit(events.Event{Kind: events.AlterAttribute, Obj: cp.PreparedSource,
				Text: "Prepared", Amount: -1})
		}
	}
	// CR 601.2a: the player who cast the spell is its controller. A card
	// another seat controlled (Rashmi and Ragavan's exiled OPPONENT card,
	// Gonti's stolen card, Intellect Devourer's may-play exile) comes under
	// the caster's control the moment it is cast, and the resulting permanent
	// enters the battlefield under the caster's control; an ordinary cast's
	// card already answers to the caster, so no event rides those.
	if o := e.G.Obj(pc.card); o != nil && o.Controller != pc.player {
		e.emit(events.Event{Kind: events.ControlChange, Obj: pc.card, Player: pc.player})
	}
	pc.stackObj = pc.card
	pc.pushed = true
	// CR 702.168: the Gift election was announced before CR 601.2a, so fold
	// it onto the now-existing stack object here -- the target ask that
	// follows reads Count$PromisedGift off it, and events.Move carries the
	// promise across the stack->battlefield move for a permanent's ETB. Only
	// a cast that actually reached the Gift ask emits (Amount 1 for a
	// promise, 0 for a decline); every unrelated cast stays byte-identical.
	if pc.giftDone {
		amt := int32(0)
		if pc.giftPromise {
			amt = 1
		}
		e.emit(events.Event{Kind: events.GiftPromise, Obj: pc.card, Player: pc.giftTo, Amount: amt})
	}
	// CR 903.8: the cast counter increments the INSTANT the spell is put on
	// the stack, never when it resolves -- so a commander spell that is later
	// countered still raises the next cast's tax. Only a cast FROM the
	// command zone counts, and recordCmdCast itself carries the Commander
	// format gate.
	if pc.from == state.ZCommand {
		e.recordCmdCast(pc.player, pc.card)
	}
	return false
}

// recheckIllegal implements CR 601.2e: once every announcement choice (the
// {X} value) is made but before the cost is paid, the game rechecks that the
// proposed spell can legally be cast, considering the characteristics the
// choices changed -- most importantly the mana value with {X} counted at its
// chosen value (CR 202.3e). A CantBeCast restriction that the chosen {X} now
// makes applicable forbids the spell, so the proposal is reversed (CR 733.1)
// and nothing is paid. Only a spell is rechecked: an activated ability's
// legality was fully gated before it was offered, and 202.3e's X-count is a
// spell-mana-value rule. Returns true (and has reversed the proposal) when
// the spell has become illegal.
func (e *Engine) recheckIllegal(pc *pendingCast) bool {
	if pc.isAbility() {
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return false
	}
	// CR 202.3e: the spell's mana value counts {X} at the chosen value, and
	// is a property of the card's printed mana cost -- never the alternative
	// cost (flashback) it may be paid with. restrictionOnFace evaluates the
	// same continuous-gate + ValidCard grammar the offer's
	// castRestrictedUsing runs, against one face, with that face's mana
	// value: the offer reads the front face for the ordinary cast and, for a
	// fuse cast, BOTH halves through castRestrictedAsFace (CR 709.5), so the
	// recheck must answer with the same both-halves rule or an offered cast
	// would abort here (or a prohibited one would slip through). offerAsFace
	// is the scoped read the offer probes with; for the front face it is a
	// no-op.
	restrictionOnFace := func(face *cards.Face) bool {
		if face == nil {
			return false
		}
		printed := ParseCost(face.ManaCost)
		mv := printed.CMC()
		if printed.X > 0 {
			mv = printed.WithX(pc.x).CMC()
		}
		return e.offerAsFace(pc.card, face, func() bool {
			for _, sv := range e.castRestrictionSources(e.activeStatics("CantBeCast"), pc.card) {
				if !e.actorMatches(sv, "Caster", pc.player) {
					continue
				}
				// The same shared continuous gate castRestrictedUsing runs: the
				// CR 608.2b recheck must answer with the ONE grammar the offer
				// answered with, or a cast offered under a false gate would abort
				// here (and vice versa). It subsumes the checkSVarHolds the caller
				// used to run separately.
				if !e.continuousGateHolds(sv) || !e.restrictionGateHolds(sv, pc.card) {
					continue
				}
				sc := e.specCtx(sv.Source, sv.Controller)
				sc.HasManaValue = true
				sc.ManaValue = mv
				if e.matchesSpec(sv.ParamStr(cards.PKValidCard), pc.card, sc) {
					return true
				}
			}
			return false
		})
	}
	restricted := restrictionOnFace(o.Face())
	if !restricted && pc.mode == "fuse" {
		if _, fa := fusedSplitFaces(o); fa != nil {
			restricted = restrictionOnFace(fa)
		}
	}
	if restricted {
		// suppress=true, not false: an illegal-proposal abort is a
		// no-progress reversal (CR 733.1) exactly like every other abort
		// site, so it rides the same F05-2 (CR 733.2) discipline -- first
		// identical abort retryable, second holds the option out of the
		// window. With false, a seat that re-picks the same X (the only
		// value it knows) re-announces the same illegal spell forever.
		e.abortCast(pc, "cast aborted: proposed spell is illegal (CR 601.2e)", true)
		return true
	}
	// Target-conditional CastWithFlash (task istargeting-flash): a spell
	// announced at a time a sorcery could not have been cast on the strength
	// of a CastWithFlash permission whose ValidSA$ requires targeting
	// something must actually have a qualifying announced target. The
	// permission is otherwise unread after the offer, so without this a cast
	// that took the flash window on a target the grant never covered would
	// still complete. offSorcery is set only by beginCast (the ordinary
	// offered cast: beginPlay's free-cast routes leave it false), and among
	// those it is true only when the face is not an instant, has no Flash and
	// no MayFlashSac rider -- so for a card with a target-conditional grant
	// the ONLY remaining way the offer passed spellTimingOK is that grant.
	// Re-running the same castWithFlashTargets the offer read, now with the
	// announced targets, keeps offer and recheck on ONE interpretation.
	//
	// The mode exclusion covers the two offers that grant their own timing
	// WITHOUT spellTimingOK (MayFlashCost's paid flash and the defeat cast).
	// Every other mode polices the face its timing rested on:
	// flashGrantCoversTargets answers false only when the face's off-sorcery
	// timing could have rested on an IsTargeting-conditional CastWithFlash
	// grant AND the announced targets do not satisfy it, so an unconditional
	// Flash/instant permission is never rejected for a non-qualifying target.
	// split_alt reads the LIVE face: beginCast already flipped to the cast
	// half, so o.Face() IS the half whose grant was judged at the offer
	// (through castWithFlashAsFace) and whose announced targets must satisfy
	// it now. A fuse cast stays at its front face and carries BOTH halves:
	// each non-instant half is judged against its OWN stage's targets through
	// the same face-scoped read (flashGrantCoversTargets scopes the alternate
	// half), never against the other half's grant or the flat target list.
	if pc.offSorcery && pc.mode != "mayflash" && pc.mode != "defeat_cast" {
		if pc.mode == "fuse" {
			if ff, fa := fusedSplitFaces(o); ff != nil {
				for i, half := range []*cards.Face{ff, fa} {
					var ts []state.Target
					if i < len(pc.stageTargets) {
						ts = pc.stageTargets[i]
					}
					if e.flashGrantCoversTargets(pc.player, pc.card, half, ts) {
						continue
					}
					e.abortCast(pc, "cast aborted: flash permission's target requirement unmet (CR 601.2e)", true)
					return true
				}
			}
		} else if f := o.Face(); !e.flashGrantCoversTargets(pc.player, pc.card, f, pc.targets) {
			e.abortCast(pc, "cast aborted: flash permission's target requirement unmet (CR 601.2e)", true)
			return true
		}
	}
	return false
}

var modeIsKickedSet = state.NewNameSet(
	"kicked",
	"kicked1",
	"kicked2",
	"kickedboth",
	"multikicked",
)

type modeFlagsCode uint16

const (
	modeFlagsKicked modeFlagsCode = iota + 1
	modeFlagsKicked1
	modeFlagsKicked2
	modeFlagsKickedboth
	modeFlagsFlashback
	modeFlagsJumpstart
	modeFlagsAftermath
	modeFlagsFuse
	modeFlagsMiracle
	modeFlagsEscape
	modeFlagsAdventureAlt
	modeFlagsBuyback
	modeFlagsOffspring
	modeFlagsOptionalcost
	modeFlagsSuspend
	modeFlagsForetellCast
	modeFlagsMayhem
	modeFlagsWebSlinging
	modeFlagsSneak
	modeFlagsMutated
	modeFlagsMultikicked
	modeFlagsSquadded
	modeFlagsConspired
	modeFlagsMorphed
	modeFlagsMegamorphed
	modeFlagsDisguised
)

var modeFlagsCodes = state.NewStrCodes(
	state.StrEntry[modeFlagsCode]{Key: "kicked", Val: modeFlagsKicked},
	state.StrEntry[modeFlagsCode]{Key: "kicked1", Val: modeFlagsKicked1},
	state.StrEntry[modeFlagsCode]{Key: "kicked2", Val: modeFlagsKicked2},
	state.StrEntry[modeFlagsCode]{Key: "kickedboth", Val: modeFlagsKickedboth},
	state.StrEntry[modeFlagsCode]{Key: "flashback", Val: modeFlagsFlashback},
	state.StrEntry[modeFlagsCode]{Key: "jumpstart", Val: modeFlagsJumpstart},
	state.StrEntry[modeFlagsCode]{Key: "aftermath", Val: modeFlagsAftermath},
	state.StrEntry[modeFlagsCode]{Key: "fuse", Val: modeFlagsFuse},
	state.StrEntry[modeFlagsCode]{Key: "miracle", Val: modeFlagsMiracle},
	state.StrEntry[modeFlagsCode]{Key: "escape", Val: modeFlagsEscape},
	state.StrEntry[modeFlagsCode]{Key: "adventure_alt", Val: modeFlagsAdventureAlt},
	state.StrEntry[modeFlagsCode]{Key: "buyback", Val: modeFlagsBuyback},
	state.StrEntry[modeFlagsCode]{Key: "offspring", Val: modeFlagsOffspring},
	state.StrEntry[modeFlagsCode]{Key: "optionalcost", Val: modeFlagsOptionalcost},
	state.StrEntry[modeFlagsCode]{Key: "suspend", Val: modeFlagsSuspend},
	state.StrEntry[modeFlagsCode]{Key: "foretell_cast", Val: modeFlagsForetellCast},
	state.StrEntry[modeFlagsCode]{Key: "mayhem", Val: modeFlagsMayhem},
	state.StrEntry[modeFlagsCode]{Key: "web-slinging", Val: modeFlagsWebSlinging},
	state.StrEntry[modeFlagsCode]{Key: "sneak", Val: modeFlagsSneak},
	state.StrEntry[modeFlagsCode]{Key: "mutated", Val: modeFlagsMutated},
	state.StrEntry[modeFlagsCode]{Key: "multikicked", Val: modeFlagsMultikicked},
	state.StrEntry[modeFlagsCode]{Key: "squadded", Val: modeFlagsSquadded},
	state.StrEntry[modeFlagsCode]{Key: "conspired", Val: modeFlagsConspired},
	state.StrEntry[modeFlagsCode]{Key: "casualty", Val: modeFlagsConspired},
	state.StrEntry[modeFlagsCode]{Key: "morphed", Val: modeFlagsMorphed},
	state.StrEntry[modeFlagsCode]{Key: "megamorphed", Val: modeFlagsMegamorphed},
	state.StrEntry[modeFlagsCode]{Key: "disguised", Val: modeFlagsDisguised},
)
