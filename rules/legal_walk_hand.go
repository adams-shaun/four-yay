package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// handWalk is the hand section of the priority walk: land plays and every
// cast mode the printed (and alternate) faces offer.
func (w *legalWalk) handWalk() {
	e, p := w.e, w.p
	sorcery := w.sorcery
	costStatics := &w.costStatics
	out := &w.out
	for _, id := range e.G.Zone(state.ZHand, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil {
			continue
		}
		// The printed keyword-cost heads this face can answer
		// (printed_heads.go): a clear bit skips that head's reader below.
		ph := w.printedHeads(f)
		if derivedMemoVerify {
			verifyPrintedHeads(f, ph)
		}
		if f.IsLand() {
			if sorcery && !playLandForbidden(e, p, state.ZHand, id) && e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p)) {
				w.add("play_land", w.playLabel(f), id)
				// A Modal DFC may also be played as its back land, even
				// when its front face is itself a land (CR 712.8).
				if back := modalLandBack(o); back != nil {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "play_land",
						Label: "Play " + back.Name, Obj: id, Mode: "modal_land"})
				}
			}
			// CR 702.37a/702.168a/702.169a: a LAND printed with the Morph/
			// Megamorph/Disguise keyword is ALSO castable face down for {3}
			// (Zoetic Cavern, Branch of Vitu-Ghazi).  That cast is a creature
			// SPELL, not a land play: it does not consume the land drop and
			// it goes on the stack (CR 305.1 "play a land" is a special
			// action), so it is offered even after the land play for the turn
			// has been used.  This block sits INSIDE the IsLand branch on
			// purpose: the front-face land must never acquire the ordinary
			// printed cast the nonland walk below offers, and only the
			// fixed-{3} face-down mode is added.  The same guards the existing
			// nonland face-down offer relies on are applied here explicitly
			// (this branch continues before the walk reaches them): the
			// castSuppressed/castRestricted prohibitions, the ordinary
			// spellTimingOK gate, and offerCastable's cost-modifier
			// affordability.  Like that offer it deliberately does NOT gate
			// on targetsAvailable (CR 708.4: a face-down spell has no targets)
			// and never charges the printed keyword parameter (the later
			// turn-face-up cost).
			if ph.has(phMorph | phMegamorph | phDisguise) {
				if fam := morphDownFamily(f); fam != "" && !e.castSuppressed(p, id) &&
					!w.castRestricted(p, id) && e.spellTimingOK(p, id, f, sorcery) &&
					w.offerCastable(p, id, Cost{Generic: 3}, spellScope(fam), false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + f.Name + " (face down)", Obj: id, Mode: fam})
				}
			}
			continue
		}
		// CR 712.8/712.4d: a Modal DFC in hand may be played as its back
		// face when that face is a land.  Keep this separate from the ordinary
		// front-face land path so its existing option remains byte-identical.
		if sorcery && !playLandForbidden(e, p, state.ZHand, id) && e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p)) {
			if back := modalLandBack(o); back != nil {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "play_land",
					Label: "Play " + back.Name, Obj: id, Mode: "modal_land"})
			}
		}
		if e.castSuppressed(p, id) {
			continue
		}
		// CR 712.8: a Modal DFC's nonland back face can be cast from hand
		// independently of its front face. The chosen face's timing, targets,
		// restriction and composed cost govern this offer; beginCast flips
		// provisionally. The prohibition gate is castRestrictedAsFace, not the
		// card-level castRestricted continue below: a restriction that matches
		// only the front face must not withhold the back-face offer, and one
		// that matches only the back face must not be missed by probing only
		// the front (both directions are regression-tested in
		// rules/modal_spell_face_restriction_test.go).
		if mf := modalSpellBack(o); mf != nil && e.spellTimingOK(p, id, mf, sorcery) &&
			e.castTargetsAvailable(p, id, mf.SpellAbility()) &&
			!w.castRestrictedAsFace(p, id, mf) {
			if w.offerCastableAsFace(p, id, mf, pay.WithSpellAbilityExtras(mf, e.faceCost(mf)), spellScope("")) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + mf.Name, Obj: id, Mode: "modal_spell"})
			}
		}
		// CR 709.4/709.5: a non-Room split card's alternate half is castable
		// on its own (mode split_alt, consumed by beginCast's FlipFace exactly
		// like room_alt) and, when the card carries K:Fuse, BOTH halves may be
		// cast as one fused spell (mode fuse, paying the combined cost and
		// resolving both halves). The split_alt offer reads the half's OWN
		// timing (review MAJOR 2), targets and cost through
		// splitCastTargetsAvailable, so a half with no legal target -- or one
		// the seat cannot pay for -- is withheld independently of the front
		// face. It stands ABOVE the front-face spellTimingOK gate below on
		// purpose: CR 709.4 gives each half its own timing, so a front-Sorcery
		// half must not withhold its instant alternate half during an
		// opponent's turn (incubation_incongruity, discovery_dispersal,
		// said_done, spring_mind). For that reason it also precedes the plain
		// cast in the option list for a split card.
		//
		// The prohibition gate is castRestrictedAsFace on the HALF being cast,
		// not the card-level castRestricted continue below (which reads the
		// front face). A restriction matching only the front half must not
		// withhold this half's offer, and one matching only this half must not
		// be missed by probing the front (the same two directions the
		// modal_spell offer fixed in e89e1a48b; regression-tested in
		// rules/alternate_face_restriction_test.go). castSuppressed above
		// stays shared: casting a half is casting the card.
		if sf := splitAlternateCastFace(o); sf != nil {
			// The target-conditional read is FACE-SCOPED (castWithFlashAsFace):
			// a grant on one half is judged against THAT half's own potential
			// targets and face-local statics, so the front half's grant cannot
			// offer the alternate half, and a qualifying alternate-half target
			// cannot be withheld because the front face is not a target. The
			// CR 601.2e recheck (recheckIllegal) re-runs the same face-scoped
			// read against the ANNOUNCED targets, so offer and enforcement
			// agree on one interpretation.
			instant := sf.IsInstant() || e.hasKeywordH(id, kwhFlash) || e.castWithFlashAsFace(p, id, sf)
			if (instant || sorcery) && e.splitCastTargetsAvailable(p, id, sf) &&
				!w.castRestrictedAsFace(p, id, sf) {
				if w.offerCastableAsFace(p, id, sf, pay.WithSpellAbilityExtras(sf, e.faceCost(sf)), spellScope("")) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + sf.Name, Obj: id, Mode: "split_alt"})
				}
			}
		}
		// CR 714.3a: an Adventure spell face has its own timing and may be
		// cast from hand even when the creature front is not currently castable.
		// It stands above the front-face gate for the same reason split_alt
		// does, and its prohibition is probed against the ADVENTURE face it
		// casts (castRestrictedAsFace), not the creature front the card-level
		// continue below reads -- a restriction matching only one face must
		// decide only that face's offer.
		if int(o.FaceIdx) == 0 {
			if af := adventureSpellFace(o); af != nil && e.spellTimingOK(p, id, af, sorcery) &&
				e.castTargetsAvailable(p, id, af.SpellAbility()) && !w.castRestrictedAsFace(p, id, af) {
				if w.offerCastableAsFace(p, id, af, pay.WithSpellAbilityExtras(af, ParseCost(af.ManaCost)), spellScope("")) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + af.Name, Obj: id, Mode: "adventure_alt"})
				}
			}
		}
		// CR 309.4b: either door of a Room may be cast. Mode room_alt is
		// consumed by beginCast, which records a FlipFace before the ordinary
		// cast transaction; from then on every cost/target/resolution reader
		// sees the selected face. Like the other alternate-face offers, this
		// precedes the front-face restriction gate and probes the door being
		// cast (rather than the displayed front face).
		if rf := roomAlternateCastFace(o); rf != nil {
			instant := rf.IsInstant() || e.hasKeywordH(id, kwhFlash)
			if (instant || sorcery) && e.castTargetsAvailable(p, id, rf.SpellAbility()) &&
				!w.castRestrictedAsFace(p, id, rf) {
				if w.offerCastableAsFace(p, id, rf, pay.WithSpellAbilityExtras(rf, e.faceCost(rf)), spellScope("")) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + rf.Name, Obj: id, Mode: "room_alt"})
				}
			}
		}
		// The card-level continue withholds the ORDINARY front-face cast (and
		// every front-face alternative below it -- foretell, mayflash, kicker,
		// ...) when a CantBeCast restriction matches the front face. The
		// alternate-face offers above already ran, each probed against its own
		// face.
		if w.castRestricted(p, id) {
			continue
		}
		// CR 709.5: a fuse card's fused cast is a single spell with BOTH
		// halves' characteristics, so it is NOT an alternate-face cast: the
		// object stays at its front face (beginCast's fuse arm flips nothing).
		// The gate is therefore the front-face castRestricted continue just
		// above for the front half PLUS a castRestrictedAsFace probe for the
		// alternate half: a prohibition that matches either half's
		// characteristics withholds the fused offer (CR 709.5 -- one spell
		// with both halves' characteristics). Probing the alternate half is
		// the same scoped face read the split_alt offer uses and restores the
		// face before returning, so replay is untouched; it must NOT replace
		// the front-face continue, or a front-restricted card would offer its
		// fused cast. The recheck (CR 601.2e, cast.go's recheckIllegal) runs
		// the same both-halves rule so offer and enforcement agree.
		if ff, fa := fusedSplitFaces(o); ff != nil {
			if !w.castRestrictedAsFace(p, id, fa) &&
				e.fusedTimingOK(p, id, ff, fa, sorcery) &&
				e.splitCastTargetsAvailable(p, id, ff) && e.splitCastTargetsAvailable(p, id, fa) {
				if w.offerCastable(p, id, e.fuseCost(ff, fa), spellScope(""), false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + ff.Name + " // " + fa.Name + " (fused)", Obj: id, Mode: "fuse"})
				}
			}
		}
		// Foretell (CR 702.126a): the special action pays {2} and exiles the
		// card from the hand FACE DOWN -- never the keyword's own colon
		// parameter, which prices the LATER cast. The keyword read is DERIVED
		// (printed K:Foretell line plus a layer-6 AddKeyword$ Foretell grant --
		// Dream Devourer's "each nonland card in your hand without foretell has
		// foretell"), so a granted hand card gets the same {2} action; the
		// granted card's later cast prices through foretellCost's
		// printed-cost-less-{2} fallback, which IS the granted foretell cost
		// ("its foretell cost is equal to its mana cost reduced by {2}").
		// "During your turn" is the timing gate (deliberately NO
		// instant/sorcery-speed check, unlike Suspend -- CR 702.126a's action
		// text names only the turn, and a special action needs only priority,
		// never the ability to cast an instant), widened by a granted
		// `AddKeyword$ Foretell on any player's turn` player keyword (Cosmos
		// Charger). The block sits BEFORE the front-face timing gate: the
		// action's timing is its own turn window, never the card's cast
		// timing -- a creature with foretell is offered the action on any
		// step of its controller's turn (with a stack, in an upkeep), and
		// under the any-turn grant on any step of anyone's turn.
		// castRestricted/castSuppressed above still bound the offer.
		if _, ok := e.derivedKeywordParamH(id, kwhForetell); ok &&
			(e.G.Active == p || e.playerForetellsAnyTurn(p)) &&
			w.offerCastable(p, id, Cost{Generic: 2}, foretellScope(), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast", Label: "Foretell " + f.Name, Obj: id, Mode: "foretell"})
		}
		// Sneak (CR 702.190a): cast for the keyword's alternative cost, with
		// the mandatory additional cost of returning an unblocked creature you
		// control to its owner's hand. The window is the caster's own declare
		// blockers step at instant speed, so the offer sits ABOVE the
		// sorcery-speed timing gate below -- a creature printed with no Flash
		// is offered here and skipped by that gate. sneakCosts is the ONE cost
		// reader beginCast's charge calls too, and offerCastable's shared tail
		// (nonManaCastable) censuses the Return part's candidates: an option
		// whose return cannot be paid must never be offered (the offerCastable
		// ruling). castRestricted/castSuppressed bind the offer exactly as they
		// do the plain cast.
		if e.sneakTimingOK(p) && !w.castRestricted(p, id) && !e.castSuppressed(p, id) {
			// sneakCosts is computed BEFORE the target census so a hand card
			// with no Sneak instance (the overwhelming majority of cards in
			// this window) never pays for castTargetsAvailable's walk; the
			// census is only meaningful when there is a sneak offer to gate.
			if scs := e.sneakCosts(p, id); len(scs) > 0 && e.castTargetsAvailable(p, id, f.SpellAbility()) {
				for _, sc := range scs {
					if !w.offerCastable(p, id, sc.cost, spellScope(sc.mode), false) {
						continue
					}
					label := "sneak"
					if sc.mode != "sneak" {
						label = "sneak (granted)"
					}
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + f.Name + " (" + label + ")", Obj: id, Mode: sc.mode})
				}
			}
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			// MayFlashCost (Forge's K:MayFlashCost, CR 702.8): when the ordinary
			// timing gate fails, a face printed with the keyword is NOT skipped
			// outright -- it may be cast at instant timing by paying the extra.
			// The mayflash offer below is the ONLY option this branch adds; the
			// plain cast and every other mode on the face stay behind the
			// sorcery-speed gate. When it is already sorcery timing the plain
			// cast is strictly cheaper, so no mayflash option is offered and the
			// face takes the ordinary path (no redundant duplicate offer).
			if ph.has(phMayFlashCost) && e.mayflashTimingOK(p, f) {
				if extra, ok := mayflashExtraCost(f); ok && e.castTargetsAvailable(p, id, f.SpellAbility()) {
					if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, e.castOfferBase(p, id)).Plus(extra), spellScope("mayflash"), false) {
						*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
							Label: "Cast " + f.Name + " (may-flash)", Obj: id, Mode: "mayflash"})
					}
				}
			}
			continue
		}
		// targetsAvailable is the plain SpellAbility's target census, run on
		// first use and remembered for the rest of this card. Every offer
		// below that needs it tests it LAST among pure conjuncts -- after
		// its offerCastable, whose empty-pool refusal is the cheap common
		// answer (legalWalk.offerFloorRefuses) -- so a card no offer can
		// afford (the common case in a priority window) never pays for the
		// census; the walk is a pure read, so a late census equals an eager
		// one and each offer's answer is unchanged.
		taDone, taOK := false, false
		targetsAvailable := func() bool {
			if !taDone {
				taOK, taDone = e.castTargetsAvailable(p, id, f.SpellAbility()), true
			}
			return taOK
		}
		// offerCastable prices the MANA the offer will charge (601.2f
		// modifiers, then the commander tax) over the RAW base beginCast will
		// store; withSpellAbilityExtras adds the spell's own ADDITIONAL
		// non-mana parts on top. Both are needed and they compose in this
		// order: an additional cost is never reduced by a cost modifier, the
		// same reason the commander tax lands after the modifiers rather than
		// before them.
		//
		// The gate must see the SAME cost beginCast will charge, additional
		// non-mana parts included, or an unpayable cast gets offered and then
		// aborts having consumed nothing -- which, since the abort leaves the
		// board that produced the offer untouched, is an unbounded livelock
		// rather than a wasted click. That is exactly what Village Rites did
		// to a live 4-player game (see withSpellAbilityExtras in cast.go).
		// Only the plain cast folds the extras, matching beginCast's own
		// condition: the kicked/surged/flashback/miracle offers below set
		// Mode, and beginCast skips the fold for those.
		// convokeBase is the offer gate's composed RAW base: the printed mana
		// cost with CR 702.51 Convoke and CR 702.66 Improvise's generic credits.
		// castOfferBase is the shared recipe so the plain and mayflash offers
		// cannot drift (the mayflash branch above uses it too).
		convokeBase := e.castOfferBase(p, id)
		// An either-or additional cost (AlternateAdditionalCost) makes the
		// plain cast's gate existential: the cast is offerable when AT LEAST
		// ONE alternative part is payable (the choice itself is asked by the
		// cast flow, altAddAsk), never when all of them are unpayable. Cards
		// without the keyword keep the ordinary single-cost gate.
		var altParts []string
		if ph.has(phAlternateAdditionalCost) {
			altParts = altAddCostParts(f)
		}
		if len(altParts) > 0 {
			for _, part := range altParts {
				if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, convokeBase).Plus(e.parseCost(part)), spellScope(""), false) {
					if targetsAvailable() {
						w.add("cast", w.castLabel(f), id)
					}
					break
				}
			}
		} else if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, convokeBase), spellScope(""), false) && targetsAvailable() {
			w.add("cast", w.castLabel(f), id)
		}
		if views := e.optionalCostViews(costStatics.get(), p, id); len(views) > 0 {
			for i, extra := range views {
				if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, convokeBase).Plus(extra), spellScope("optionalcost"), false) && targetsAvailable() {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + f.Name + " (optional cost)", Obj: id, Mode: "optionalcost", AltCostIndex: i + 1})
				}
			}
		}
		for i, alt := range e.alternativeCosts(p, id) {
			// An announce-bearing alternative (the Shoal cycle's Announce$ X):
			// the exile filter's cmcEQX is bound to the value the caster will
			// announce, so the "some announcement is payable" gate is
			// existential over X — at least one exilable card must match at
			// SOME mana value. The gate below replaces offerCastable's
			// unannounced (X=0) exile check with that existential scan and
			// gates the mana-only remainder on the same cost minus its exile
			// parts; beginCast asks the X (xAsk's announce arm), then exAsk
			// re-walks the part with the announced X bound.
			gate := alt.cost
			if alt.announce != "" {
				gate.Exile = nil
				if len(e.altCostXCandidates(p, id, alt)) == 0 {
					continue
				}
			}
			// Ruling (Task 9 fix round 1, Important 1): this used to gate on
			// mana-only alt.CanPay, but ParseCost now produces Sac/SubCounter/
			// Tap parts that the cast flow enforces -- an AlternativeCost whose
			// Cost$ carries Sac<N/...> was offered without checking that N
			// matching permanents exist, and beginCast then asked a sacrifice
			// decision with zero options that no answer could escape. castable
			// is the same gate every other "cast" option uses.
			if w.offerCastable(p, id, gate, spellScope(""), false) && targetsAvailable() {
				// AltCostIndex is i+1, not i: the zero value must mean "the
				// card's own cost" so every other Option literal in the tree
				// (play_land, activate, pass, and the base "cast" option
				// added just above via the shared add closure) needs no
				// change to keep meaning that.
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: altCostLabel(f.Name, i), Obj: id, AltCostIndex: i + 1})
			}
		}
		// The and/or Kicker (Forge's colon-separated two-part Kicker:<a>:<b>,
		// Wastescape Battlemage's "Kicker {G} and/or {1}{U}"): each part is an
		// independent optional additional cost (CR 601.2b), so each payable
		// combination is its own cast option -- part 1, part 2, or both. The
		// modes ride FlagKicked1/FlagKicked2 (modeFlags), which the
		// "Card.Self+kicked <n>" trigger and replacement specs read.
		if ph.has(phKicker) {
			if c1, c2, ok := twoPartKickerCosts(f); ok {
				base := pay.RawBaseCost(asPayer(e), p, id)
				for _, kp := range [...]struct {
					mode, label string
					cost        Cost
				}{
					{"kicked1", "Cast " + f.Name + " (kicked 1)", c1},
					{"kicked2", "Cast " + f.Name + " (kicked 2)", c2},
					{"kickedboth", "Cast " + f.Name + " (kicked both)", c1.Plus(c2)},
				} {
					if w.offerCastable(p, id, base.Plus(kp.cost), spellScope(kp.mode), false) && targetsAvailable() {
						*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
							Label: kp.label, Obj: id, Mode: kp.mode})
					}
				}
			} else if kc, ok := kickerCost(f); ok && w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(kc), spellScope("kicked"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (kicked)", Obj: id, Mode: "kicked"})
			}
		}
		if ph.has(phSurge) {
			if sc, ok := surgeCost(f); ok && pay.SpellsCastThisTurn(asPayer(e), p) > 0 && w.offerCastable(p, id, sc, spellScope("surged"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (surged)", Obj: id, Mode: "surged"})
			}
		}
		// Entwine (CR 702.42a): "You may pay an additional [cost] as you cast
		// this spell." It is an optional additional cost paid ON TOP of the
		// printed cost (the buyback/offspring shape), so the entwined variant
		// is its own cast option priced base+cost through offerCastable's
		// shared gate. Unlike Escalate it is a flat cost, not a per-extra-mode
		// charge, so the offer needs no mode-count clamp and the magic is all
		// in beginCast's fold plus castModeAsk's all-modes announcement. The
		// offer is gated on the card actually having a Charm spell ability:
		// Entwine's ONLY modelled meaning is "choose all modes", so charging
		// its cost for a face with no modal body (no corpus carrier -- all 32
		// Entwine files are Charms) would take mana for nothing; such a face
		// keeps its printed-cast path and its coverage gap stays visible.
		if ph.has(phEntwine) {
			if ec, ok := entwineCost(f); ok && isCharmSpell(f) &&
				w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(ec), spellScope("entwined"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (entwined)", Obj: id, Mode: "entwined"})
			}
		}
		// Replicate (CR 702.55a): the replicated variant is its own cast
		// option paying the base cost plus ONE replicate payment -- one
		// payment is what gates the offer; the count ask (replicateAsk)
		// settles how many afterwards and the 601.2g payment window may still
		// produce mana for the composed total, exactly like a kicked cast, so
		// the max count cannot be fixed at offer time. The non-mana parts of
		// the payment fail closed in nonManaCastable (offerCastable's shared
		// tail), so the two tapXType carriers' replicate never offers.
		if ph.has(phReplicate) {
			if rc, ok := replicateCost(f); ok &&
				w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(rc), spellScope("replicated"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (replicated)", Obj: id, Mode: "replicated"})
			}
		}
		// Multikicker (CR 702.43): the multikicked variant is its own cast
		// option paying the base cost plus ONE multikicker payment -- the
		// replicate offer's exact shape (one payment is what gates the offer;
		// the count ask, multikickAsk, settles how many afterwards). No corpus
		// carrier pairs Kicker with Multikicker (measured), so this offer
		// never collides with the kicked family above.
		if ph.has(phMultikicker) {
			if mkc, ok := multikickerCost(f); ok &&
				w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(mkc), spellScope("multikicked"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (multikicked)", Obj: id, Mode: "multikicked"})
			}
		}
		// Squad (CR 702.66): the squadded variant pays the base cost plus ONE
		// squad payment -- the replicate/multikicker offer's exact shape (one
		// payment gates the offer; the count ask, squadAsk, settles how many
		// afterwards, and the 601.2g payment window may still produce mana for
		// the composed total). No corpus carrier pairs Squad with
		// Replicate/Multikicker/Kicker (measured over the 15 K:Squad files), so
		// this offer never collides with the count asks above. Non-mana parts
		// fail closed in nonManaCastable (offerCastable's shared tail), so a
		// squad cost ParseCost cannot price never offers.
		if ph.has(phSquad) {
			if sqc, ok := squadCost(f); ok &&
				w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(sqc), spellScope("squadded"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (squadded)", Obj: id, Mode: "squadded"})
			}
		}
		// Conspire (CR 702.78a): the conspired variant pays NO extra mana --
		// the base cost is unchanged and the cost is the tap of two untapped
		// creatures the caster controls that share a colour with the spell.
		// So unlike the replicate/multikicker offers there is no cost to
		// compose: the offer is gated on the same base cast being offerable
		// (re-checked with the base cost, exactly what the plain offer used)
		// AND on the derived-keyword read (hasCastConspire, the
		// hasCastConvoke shape, so a layer-6 grant reaching the stack matches)
		// AND on at least two eligible creatures existing. Do NOT route the
		// tap through offerCastable with a fabricated cost -- the tap has no
		// Cost$ representation; conspireAsk enforces it at announcement.
		if e.hasCastConspire(id) &&
			w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, convokeBase), spellScope(""), false) &&
			len(e.conspireCandidates(p, id)) >= 2 && targetsAvailable() {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (conspired)", Obj: id, Mode: "conspired"})
		}
		teamworkOffer(w, id, f, convokeBase, targetsAvailable())
		// The optional additional sacrifices (rules/optional_sacrifice.go:
		// Casualty, Bargain) price the ordinary spell -- never a substitution
		// -- and require at least one eligible permanent.
		for i := range optionalSacrifices {
			r := &optionalSacrifices[i]
			n, ok := r.offered(e, id)
			if !ok || !w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, convokeBase), spellScope(r.scope), false) ||
				len(e.optionalSacrificeCandidates(r, p, id, n)) == 0 || !targetsAvailable() {
				continue
			}
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + r.mode + ")", Obj: id, Mode: r.mode})
		}
		// The alternative-cost keyword family (altcosts), from the hand: evoke
		// (CR 702), dash, overload and warp each become their own "cast" mode
		// option paying the printed keyword cost in place of the mana cost.
		// Blitz rides the same walk but reads the ID-AWARE grant (e.blitzCost):
		// a layer-6 AddKeyword$ Blitz grant (Henzie, Toolbox Torre) prices its
		// cost from the candidate card -- CardManaCost placeholders and the
		// grant's trailing spell filter -- where the printed-only
		// keywordAltCost read cannot see it.
		// Madness does NOT offer from the hand here (CR 702.35a: the madness
		// cast window opens only on the discard, through the pending-trigger
		// machinery, exactly like Miracle); warp additionally offers from the
		// graveyard and -- after an end-step exile -- from exile, in the walks
		// below.
		for i := range altCastModes {
			ka := &altCastModes[i]
			if !ka.handLoop || !ph.has(ka.ph) {
				continue
			}
			alt, ok := ka.faceCost(f, w.e.walkFaceFactsOf(f))
			if !ok || !w.offerCastable(p, id, alt, spellScope(ka.mode), false) ||
				(!ka.untargeted && !targetsAvailable()) {
				continue
			}
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + ka.mode + ")", Obj: id, Mode: ka.mode})
		}
		// Web-slinging (CR 702.186a-family, Marvel's Spider-Man): the printed
		// (or granted) web-slinging cost replaces the mana cost, and the cast
		// carries the mandatory additional cost of returning a tapped creature
		// you control to its owner's hand -- the composed Cost the shared
		// reader (webslinging.go's webSlingingCosts) prices for BOTH this offer
		// and beginCast's charge, so the two stages cannot drift. The same
		// timing/restriction gates the plain cast above ran apply unchanged
		// (the keyword adds no timing rider), and offerCastable's shared tail
		// (nonManaCastable) censuses the Return part's candidates: an option
		// whose return cannot be paid must never be offered (the offerCastable
		// ruling).
		for _, wc := range e.webSlingingCosts(p, id) {
			if !w.offerCastable(p, id, wc.cost, spellScope(wc.mode), false) || !targetsAvailable() {
				continue
			}
			label := "web-slinging"
			if wc.mode != "web-slinging" {
				label = "web-slinging (granted)"
			}
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + label + ")", Obj: id, Mode: wc.mode})
		}
		if blitzes := e.blitzCosts(p, id); len(blitzes) > 0 {
			for _, blitz := range blitzes {
				if !w.offerCastable(p, id, blitz.cost, spellScope(blitz.mode), false) || !targetsAvailable() {
					continue
				}
				label := altMode(altBlitz)
				if blitz.mode != label {
					label += " (granted)"
				}
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (" + label + ")", Obj: id, Mode: blitz.mode})
			}
		}
		// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a), from
		// the hand: each family becomes its own "cast" mode option paying
		// the fixed {3} face-down cost in place of the mana cost. The
		// face-down spell has no targets and no printed spell abilities
		// (CR 708.4), so the offer deliberately does NOT gate on
		// targetsAvailable, unlike the keyword family above -- beginCast's
		// target stage (pc.faceDown) and the resolution reader's printed
		// abilities are skipped the same way. The printed keyword parameter
		// (the turn-face-up cost) is NOT paid now; the pay-time CastInfo's
		// mode flag records which family rode so a later turn-face-up action
		// can validate and pay against it. The timing gate is the ordinary
		// spellTimingOK the walk already ran above (CR 702.37a's "any time
		// you could cast a sorcery" -- morph prints only on creature faces).
		if ph.has(phMorph | phMegamorph | phDisguise) {
			if fam := morphDownFamily(f); fam != "" &&
				w.offerCastable(p, id, Cost{Generic: 3}, spellScope(fam), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (face down)", Obj: id, Mode: fam})
			}
		}
		// Emerge (CR 702.118a): "cast this spell by sacrificing a creature and
		// paying the emerge cost reduced by that creature's mana value". It is
		// a casting option, but NOT a plain substitution -- the cast sacrifices
		// a creature AND the cost is reduced by its mana value -- so it does
		// not ride the keyword family above. emergeOfferCost composes the
		// printed K:Emerge cost with the mandatory Sac<1/Creature> part,
		// priced separately for each sacrifice candidate. sacAsk permits only
		// candidates whose own reduced cost remains payable. Unsupported printed
		// cost shapes are withheld; the plain cast remains unaffected.
		if ph.has(phEmerge) {
			if _, ok := e.emergeOfferCost(p, id, f, func(c Cost) bool {
				return w.offerCastable(p, id, c, spellScope("emerged"), false)
			}); ok && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (emerged)", Obj: id, Mode: "emerged"})
			}
		}
		// Bestow (CR 702.114a): the bestowed cast is its own "cast" option
		// paying the bestow cost in place of the mana cost, and the spell is
		// an Aura with enchant creature, so the offer gates on the targets of
		// the SYNTHESIZED attach SA -- the face has no SP of its own, so the
		// plain cast's targetsAvailable (from the nil SpellAbility) says
		// nothing about it. bestowCost prices every printed shape (an {X}, a
		// CollectEvidence<N>, and the colon-suffixed metadata line) and
		// withholds only a cost token ParseCost cannot model at all, the
		// replicate convention.
		if ph.has(phBestow) {
			if ba, ok := bestowCost(f); ok && e.castTargetsAvailable(p, id, bestowedAttachSA()) &&
				w.offerCastable(p, id, ba, spellScope(altMode(altBestow)), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (" + altMode(altBestow) + ")", Obj: id, Mode: altMode(altBestow)})
			}
		}
		// Mutate (CR 702.140a): the mutate cast pays the mutate cost in place
		// of the mana cost and targets a non-Human creature its controller
		// owns. Like bestow, the gate is the SYNTHESIZED target SA's
		// feasibility -- the creature face has no SP for the mutation.
		if ph.has(phMutate) {
			if mc, ok := mutateCost(f); ok && e.castTargetsAvailable(p, id, mutateTargetSA()) &&
				w.offerCastable(p, id, mc, spellScope("mutated"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (mutated)", Obj: id, Mode: "mutated"})
			}
		}
		if ph.has(phBuyback) {
			if bc, ok := buybackCost(f); ok && w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(bc), spellScope("buyback"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast", Label: "Cast " + f.Name + " (buyback)", Obj: id, Mode: "buyback"})
			}
		}
		// Offspring (CR 702.175a): the optional ADDITIONAL cost half, offered
		// beside the plain cast. e.offspringCost reads the DERIVED keyword list
		// with the stack-zone override (rules/offspring.go), so BOTH the
		// printed K:Offspring line and a layer-6 AddKeyword$ Offspring grant
		// reaching the cast (Zinnia, Valley's Voice's "Creature spells you cast
		// have offspring {2}") are priced through the ONE read -- the granted
		// cost is charged, never a printed one, and the offer and beginCast's
		// modeCost stage structurally cannot disagree. The offer gate is the
		// same base+additional composition beginCast will charge, and
		// targetsAvailable keeps a target-bearing creature spell's offer honest
		// (the plain cast's gate, which the offspring cast shares).
		if e.hasCastOffspring(id) {
			if oc, ok := e.offspringCost(id); ok &&
				w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(oc), spellScope("offspring"), false) && targetsAvailable() {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (offspring)", Obj: id, Mode: "offspring"})
			}
		}
		if ph.has(phSuspend) {
			if sc, ok := suspendCost(f); ok {
				offer := sc.cost
				if sc.timeX {
					// XMin<N> is part of the announcement, not a later payment
					// preference: do not offer a Suspend X action that cannot pay
					// even its smallest legal X.
					offer = offer.WithX(sc.minTime)
				}
				if w.offerCastable(p, id, offer, spellScope("suspend"), false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast", Label: "Suspend " + f.Name, Obj: id, Mode: "suspend"})
				}
			}
		} // Plot (CR 701.34a): the alternative ACTION pays the K: line's colon
		// parameter and exiles the card with the plotted designation -- NO
		// counters (the K:Plot token is the COST {generic}+{colour}, never a
		// counter count). "Plot only as a sorcery" is the engine's own
		// sorcery-speed bool -- unlike the plain cast above, which follows the
		// face's own type timing, even an instant's plot action waits for its
		// controller's main phase with an empty stack. No target ask: the
		// action itself only exiles; the later plot_cast announces its own
		// targets. The free cast's later-turn gate lives in Object.PlottedTurn.
		if ph.has(phPlot) {
			if raw, ok := f.KeywordParam("Plot"); ok && sorcery &&
				w.offerCastable(p, id, ParseCost(raw), spellScope("plot"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Plot " + f.Name, Obj: id, Mode: "plot"})
			}
		}
	}
}
