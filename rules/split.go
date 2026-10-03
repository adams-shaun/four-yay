package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// splitCastTargetsAvailable is castTargetsAvailable for a half of a split
// card: the X-pending census reads THIS face's own printed cost and SP Cost$
// rather than the object's current front face, so the alternate half's
// affordability is judged against the half being offered.
func (e *Engine) splitCastTargetsAvailable(p state.PlayerID, id state.ObjID, f *cards.Face) bool {
	if f == nil {
		return false
	}
	xPending := costAnnouncesX(e.faceCost(f))
	if ab := f.SpellAbility(); ab != nil {
		xPending = xPending || costAnnouncesX(e.parseCost(ab.ParamStr(cards.PKCost)))
	}
	return e.targetsAvailable(p, id, id, f.SpellAbility(), xPending)
}

// fusedTimingOK reports whether a Fuse cast (mode "fuse") may be announced
// now. A fused spell is one spell whose characteristics are both halves, so
// it can be cast at instant speed only when BOTH halves are castable at
// instant speed; otherwise it waits for sorcery timing. Every fuse carrier
// in the corpus is a same-type pair (measured), so this is exact for them and
// conservative for any mixed pair a later set prints.
func (e *Engine) fusedTimingOK(p state.PlayerID, id state.ObjID, front, alt *cards.Face, sorcery bool) bool {
	if sorcery {
		return true
	}
	instant := func(f *cards.Face) bool {
		// Face-scoped (castWithFlashAsFace): each half's target-conditional
		// grant is judged against that half's own potential targets and
		// face-local statics -- the front face's grant is not permission for
		// the alternate half, and vice versa. The CR 601.2e recheck runs the
		// same per-half read (flashGrantCoversTargets) against each stage's
		// announced targets, so offer and enforcement agree.
		return f != nil && (f.IsInstant() || e.hasKeywordH(id, kwhFlash) || e.castWithFlashAsFace(p, id, f))
	}
	return instant(front) && instant(alt)
}

// fuseCost is the combined mana cost a Fuse cast pays: each half's printed
// mana cost summed (CR 702.101b), plus each half's SP Cost$ additional
// non-mana parts through the same withSpellAbilityExtras fold a plain cast
// uses. No corpus fuse carrier carries a Cost$ on either half (measured: 0
// of 17 files), so the extras term contributes nothing today but keeps the
// cost shape honest if a later set prints one.
func (e *Engine) fuseCost(front, alt *cards.Face) Cost {
	c := e.faceCost(front).Plus(e.faceCost(alt))
	return withSpellAbilityExtras(front, withSpellAbilityExtras(alt, c))
}

// castStageSA is the spell ability whose targets the targetAsk pass at this
// pendingCast's current targetStage must ask. For every ordinary cast (and
// for an ability) there is exactly one stage and this reproduces the old
// single-SA read verbatim. A Fuse cast has TWO stages: stage 0 is the front
// half's SA, stage 1 the alternate half's. A fused half's modal declaration
// resolves against ITS OWN face's SVar table, so a fused Charm-shaped half
// names its own modes.
func (e *Engine) castStageSA(pc *pendingCast, o *state.Object, f *cards.Face) *cards.SA {
	if pc.isAbility() {
		return e.pcAbility(pc)
	}
	if pc.mode == "fuse" {
		ff, fa := fusedSplitFaces(o)
		if ff == nil {
			return nil
		}
		if pc.targetStage == 0 {
			return modalTargetSA(ff, ff.SpellAbility(), o.ChosenModes)
		}
		return modalTargetSA(fa, fa.SpellAbility(), o.ChosenModes)
	}
	if f == nil {
		return nil
	}
	sa := f.SpellAbility()
	if sa == nil && pc.mode == "bestowed" {
		sa = bestowedAttachSA()
	}
	if sa == nil && pc.mode == "mutated" {
		sa = mutateTargetSA()
	}
	return modalTargetSA(f, sa, o.ChosenModes)
}

// castHasNextTargetStage reports whether another target stage follows the
// current one. Only a Fuse cast has more than one stage, and only with the
// object still at its front face (fusedSplitFaces is non-nil).
func (e *Engine) castHasNextTargetStage(pc *pendingCast, o *state.Object) bool {
	if pc.mode != "fuse" || pc.targetStage != 0 {
		return false
	}
	ff, _ := fusedSplitFaces(o)
	return ff != nil
}

// resolveFused resolves a Fuse cast (CR 702.101b): one spell whose two
// halves' spell abilities run in sequence. It mirrors resolveTop's own spell
// tail -- the CR 608.2b target recheck, the Resolve event, the Ascend
// blessing and the off-stack move -- but over both halves instead of the
// single Face().SpellAbility().
//
// The cast recorded both halves' targets as one flat list on the stack
// object (the two target stages in order) AND each stage's own slice in
// Engine.fuseTargets at payment. Each half resolves the targets chosen FOR
// IT: the stage slice, rechecked against its own ValidTgts spec through
// legalTargets (the CR 608.2b recheck). The scratch-absent fallback -- a
// stack COPY of a fused spell inherits the flat list but not the engine
// scratch -- re-derives the split from the flat list through the spec, the
// pre-slice behaviour, which mis-assigns a target a half's spec merely
// overlaps (Turn // Burn's Creature vs Any); a copy keeps the original's
// targets by the standing copy stand-in, so the fallback is the same
// disclosed narrowing as every other copy family.
// The spell fizzles only when EVERY declared target is now illegal (CR
// 608.2b's one-instance rule); a half that lost its target does as much as
// possible while the other still resolves.
//
// Narrowing fixed: when ANY half SUSPENDS on a mid-resolution ask (an asking
// primitive such as a discard or sacrifice choice), the resumed frame
// completes THAT half through the ordinary resume machinery, and the
// still-unrun halves are chained after it as a fuse-rest continuation
// (resumeResolution's rp.fuseAlt frame) carrying each remaining half's own
// CR 608.2b-filtered target slice -- so Down // Dirty's Dirty runs after the
// answered discard ask exactly as it would have without the suspension.
// A suspension on the LAST half chains no fuse-rest frame: nothing is left to
// run, and the generic completion is byte-identical because the asking
// frame's own fusedTargets binding (resumeResolution) supplies the half's
// targets and suppresses the spurious ValidTgts$ re-ask the empty tail was
// once believed necessary for (review round 3, MINOR 2 -- the tail was
// untested and redundant).
//
// It never writes the engine resume state itself (ruling T21-e, pinned by
// internal/archtest's TestResumeStateOwnedOnlyByTheResolutionMachinery):
// when a half suspends it returns (cont, true), and its caller -- resolveTop,
// the resolution machinery's first pass -- links cont onto the fresh resume
// point the ask installed, exactly as it does for an ordinary spell's
// suspended SubAbility chain.
func (e *Engine) resolveFused(o *state.Object) (*resumePoint, bool) {
	ff, fa := fusedSplitFaces(o)
	if ff == nil || fa == nil {
		// FlagFused only ever rides an offered fuse cast, so this is a
		// malformed log. Move the object off the stack rather than strand it.
		e.moveResolvedOffStack(o)
		return nil, false
	}
	halves := []*cards.Face{ff, fa}
	sas := make([]*cards.SA, len(halves))
	legalByHalf := make([][]state.Target, len(halves))
	stageTargets := e.fuseTargets[o.ID]
	checked := false
	totalLegal := 0
	for i, hf := range halves {
		sa := hf.SpellAbility()
		sas[i] = sa
		if sa == nil {
			continue
		}
		spec := effects.TargetsOf(sa).ValidTgts
		if spec != "" {
			// The half's OWN stage slice when the payment published one --
			// exactly the targets chosen for THIS half, never a target the
			// other half's spec merely overlaps. Scratch absent (a stack
			// copy of a fused spell): re-derive from the flat list through
			// the spec, the pre-slice fallback above.
			own := o.Targets
			if stageTargets != nil && i < len(stageTargets) {
				own = stageTargets[i]
			}
			legalByHalf[i] = e.legalTargets(own, sa, targetZones(sa), o.Controller, o.ID, o.ID)
			totalLegal += len(legalByHalf[i])
			if !(e.resolvedTargetMin(o.Controller, o.ID, sa, 0) == 0 && len(own) == 0) {
				checked = true
			}
		}
	}
	if checked && totalLegal == 0 {
		rest := spellFizzleZone(o)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID,
			From: state.ZStack, To: rest, Text: "fizzled: no legal targets remain"})
		e.ensureLeftTheStack(o.ID, rest, "a replacement fully discarded this "+
			"fused spell's 'fizzled: no legal targets' move without relocating it anywhere; "+
			"sent to its resting zone instead of re-resolving forever")
		return nil, false
	}
	e.emit(events.Event{Kind: events.Resolve, Obj: o.ID, Text: ff.Name + " // " + fa.Name})
	e.grantSpellBlessing(o, ff)
	if cont, suspended := e.runFusedHalves(o, halves, sas, legalByHalf, 0, nil); suspended {
		return cont, true
	}
	e.moveResolvedOffStack(o)
	return nil, false
}

// fusedHalfRoot reports whether sa is the root spell ability of one of the
// fused spell o's halves (matched by Line, the same convention
// chosenTargetsFor's OfferedSA suppression uses -- ResolveSVar parses fresh
// on every call, so pointer identity never holds between derivations), and
// returns that half's index. It is what tells a half's ROOT re-entry (its
// targeting WAS covered by the cast's stage ask, so mark it offered and skip
// the pre-ask) apart from a sub-ability's (whose own targeting ask must
// still fire). The half's TARGET BINDING is not derived here: every frame of
// a half's resolution carries it from Engine.fusedResolving, captured by Ask
// (rules/resolution.go), so a sub-ability too reads the half's own slice as
// its parent list (Flesh // Blood's DBPutCounter reads
// ParentTargeted$CardPower off it).
func fusedHalfRoot(o *state.Object, sa *cards.SA) (int, bool) {
	if o == nil || sa == nil {
		return 0, false
	}
	ff, fa := fusedSplitFaces(o)
	if ff == nil || fa == nil {
		return 0, false
	}
	for i, hf := range []*cards.Face{ff, fa} {
		if hsa := hf.SpellAbility(); hsa != nil && hsa.Line == sa.Line {
			return i, true
		}
	}
	return 0, false
}

// runFusedHalves runs halves[from:] of the fused spell o -- the shared half
// loop of resolveFused's first pass and of every fuse-rest continuation --
// and, when a half suspends on a mid-resolution ask, BUILDS the ordinary
// continuation chain plus a fuse-rest frame for the still-unrun halves (each
// carrying its own captured target slice), with `outer` behind them, and
// returns it with suspended == true. It does not link that chain onto the
// new pending point itself: only the resolution machinery may write the
// engine resume state (ruling T21-e), so the caller -- resolveTop through
// resolveFused, or resumeResolution's rp.fuseAlt branch -- sets
// e.resume.outer to the returned chain and returns, and the fuse-rest
// frame's own completion tail (resumeResolution's rp.fuseAlt branch)
// finishes the object. The chain is built HERE because each frame captures
// Engine.fusedResolving, which is only set for the duration of this call.
func (e *Engine) runFusedHalves(o *state.Object, halves []*cards.Face, sas []*cards.SA,
	legalByHalf [][]state.Target, from int, outer *resumePoint) (*resumePoint, bool) {
	for i := from; i < len(halves); i++ {
		sa := sas[i]
		if sa == nil {
			continue
		}
		hf := halves[i]
		e.damaging = o.ID
		ctx := effects.NewCtxPtr(o.ID, o.Controller, effects.CtxInit{Targets: legalByHalf[i]})
		ctx.ResolvingObj = o.ID
		if effects.TargetsOf(sa).Targeted() {
			ctx.TargetsOffered = true
			ctx.OfferedSA = sa
		}
		ctx.X = o.X
		ctx.Sacrificed = e.sacrificedLKI[o.ID]
		ctx.Exiled = e.castExiled[o.ID]
		ctx.Revealed = e.castRevealed[o.ID]
		effects.SetSVars(ctx, hf.SVars)
		ctx.Modes = o.ChosenModes
		// The half's own CR 608.2b-filtered slice is the AMBIENT resolving
		// target for the whole of this half's chain: Ask captures it onto any
		// mid-resolution ask posed below (root, SubAbility$ or loop body), so
		// a re-entered frame binds the half's targets -- never the flat list
		// both halves' -- as its ParentTargeted$/Targeted list. Kept set
		// through the chain build just below so the continuation frames it
		// stamps inherit the same binding, then restored (structural save:
		// a nested fused resolution, never in the corpus, keeps the outer
		// binding intact).
		savedFused, savedFusedSet := e.fusedResolving, e.fusedResolvingSet
		savedSVars := e.fusedResolvingSVars
		e.fusedResolving, e.fusedResolvingSet, e.fusedResolvingSVars = legalByHalf[i], true, hf.SVars
		effects.Resolve(e, ctx, sa)
		e.damaging = 0
		e.fusedResolving, e.fusedResolvingSet, e.fusedResolvingSVars = savedFused, savedFusedSet, savedSVars
	}
	return nil, false
}

// split.go implements Forge's AlternateMode:Split split cards that are NOT
// Rooms (rooms.go owns those) and NOT Aftermath alternate faces (the
// graveyard-only cast rules/legal.go's aftermathAlternateFace owns). Two
// behaviours live here, both CR 709 / CR 702.101:
//
//   - Each half of a split card is castable on its own (CR 709.4): a hand
//     card is offered both its front face's cast and, as "split_alt", its
//     alternate face's cast. Mode split_alt is consumed by beginCast exactly
//     like room_alt/adventure_alt -- one event-sourced FlipFace to the chosen
//     face before the ordinary cast transaction, so every downstream reader
//     (rawBaseCost, targets, resolution) sees the selected half.
//
//   - Fuse (CR 702.101b) lets the caster cast BOTH halves as ONE spell: mode
//     fuse pays the combined mana cost (each half's printed cost) and
//     resolves both halves' spell abilities in sequence. The cast is not a
//     face flip -- the object keeps FaceIdx 0 -- and the pay-time FlagFused
//     provenance is what rules/stack.go's resolution reader dispatches on.
//
// The split helpers are structural over any two-face Split card: they key on
// the card's AlternateMode and the faces' own keywords, never a card name.

// kw:Fuse is read directly by rules (legal.go's offer, cast.go's cost/flag,
// stack.go's resolution), the same way Kicker/Flashback are: it has no
// cards.expandKeywords expander. Registering it is the coverage census's
// support declaration -- without it the report still counted the 17 fuse
// carriers as blocked on kw:Fuse even though the engine now implements it.
// Pinned by TestSplitFuseKeywordIsRegistered.
func init() { effects.RegisterNonAPI("kw:Fuse") }

// splitAlternateCastFace reports the other castable half of a non-Room Split
// card whose front face is current, or nil when the object is not such a
// card. Both halves must be non-Rooms (a Room has its own offer path), the
// card must have exactly two faces, and the object must still be at face 0 --
// a split card offered from the hand is always at its front face. A face
// carrying K:Aftermath is deliberately excluded: the aftermath half is cast
// only from its owner's graveyard (rules/legal.go's aftermath offer), never
// from hand.
func splitAlternateCastFace(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.Card.AlternateMode != "Split" || len(o.Card.Faces) != 2 || int(o.FaceIdx) != 0 {
		return nil
	}
	front, alt := o.Card.Faces[0], o.Card.Faces[1]
	if front == nil || alt == nil {
		return nil
	}
	if isRoomFace(front) || isRoomFace(alt) {
		return nil
	}
	if alt.HasKeyword("Aftermath") {
		return nil
	}
	return alt
}

// fusedSplitFaces reports the (front, alternate) faces of a non-Room Split
// card carrying K:Fuse whose front face is current, or (nil, nil) when the
// object is not a fuse carrier. Fuse is printed on one of the halves (in the
// corpus always the front face); either face's keyword admits the combined
// cast, so the reader checks both rather than assuming the front.
func fusedSplitFaces(o *state.Object) (*cards.Face, *cards.Face) {
	if o == nil || o.Card == nil || o.Card.AlternateMode != "Split" || len(o.Card.Faces) != 2 || int(o.FaceIdx) != 0 {
		return nil, nil
	}
	front, alt := o.Card.Faces[0], o.Card.Faces[1]
	if front == nil || alt == nil {
		return nil, nil
	}
	if isRoomFace(front) || isRoomFace(alt) {
		return nil, nil
	}
	if !front.HasKeyword("Fuse") && !alt.HasKeyword("Fuse") {
		return nil, nil
	}
	return front, alt
}
