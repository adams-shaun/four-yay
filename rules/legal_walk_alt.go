package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// mayPlayLandWalk offers land plays through active may-play-from-zone grants.
func (w *legalWalk) mayPlayLandWalk() {
	e, p := w.e, w.p
	// A may-play-from-zone grant (Conduit of Worlds, Crucible of Worlds, ...)
	// makes lands in a granted zone playable this turn. This is the SECOND
	// play_land source alongside the hand walk above, never a replacement for
	// it; the once-per-turn land-drop gate is the same sorcerySpeed &&
	// LandsPlayed < 1 condition the hand walk applies, so a graveyard land
	// and a hand land share one land drop. The zones walked, the Affects
	// filter each grant applies, and the deterministic order all come from
	// mayPlayLandIds.
	for _, id := range e.mayPlayLandIds(p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		w.add("play_land", "Play "+o.Face().Name, id)
	}
}

// mayhemLandWalk offers the bare K:Mayhem graveyard land play.
func (w *legalWalk) mayhemLandWalk() {
	e, p := w.e, w.p
	out := &w.out
	// The bare, parameterless K:Mayhem permission (Oscorp Industries) is a
	// land-only PLAY from the graveyard, not a cast -- the "Timing rules still
	// apply" land shape the mayhem cast walk withholds. It is a THIRD
	// play_land source alongside the hand walk and mayPlayLandIds, sharing
	// their once-per-turn land-drop gate and paying no mana. Deduped against a
	// land a MayPlay grant already offered so the same graveyard land never
	// yields two identical options.
	for _, id := range e.mayhemLandPlayIds(p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		dup := false
		for _, prev := range *out {
			if prev.Kind == "play_land" && prev.Obj == id {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		w.add("play_land", "Play "+o.Face().Name, id)
	}
}

// mayPlaySpellWalk offers casts of non-land cards through may-play grants.
func (w *legalWalk) mayPlaySpellWalk() {
	e, p := w.e, w.p
	sorcery := w.sorcery
	out := &w.out
	// A may-play-from-zone grant also makes NON-LAND cards in a granted zone
	// castable this turn (CR 401.5; Atsushi's "you may play those cards",
	// Opposition Agent's MayPlay+IgnoreColor static). This is a THIRD cast
	// source alongside the hand and command-zone walks, never a replacement;
	// the cast pays the card's PRINTED cost (the grant changes where it may
	// come from, not what it costs), with the grant's MayPlayIgnoreColor$
	// rider payable-as-any-colour consulted by castable (the card is still
	// in the granted zone here) and recorded on the pendingCast at beginCast
	// for the window and the payment to keep. A MayPlayLimit$ grant whose cap
	// is reached offers nothing through itself (mayPlaySpellIds). The offer
	// gate folds the card's own SpellAbility additional costs exactly like
	// the hand walk does, so an offered may-play cast and the cost beginCast
	// charges structurally cannot disagree.
	for _, off := range e.mayPlaySpellIds(p) {
		id := off.id
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || f.IsLand() || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		// The permission's ValidSA$ decides which cast shapes it permits
		// (Brokkos, Apex of Forever's `ValidSA$ Spell.Mutate` permits ONLY
		// the mutate cast from the graveyard, CR 903.3d/702.140a). An empty
		// ValidSA$ is the ordinary permission, so this splits the historical
		// single offer into its plain, mutate, blitz and sneak halves without
		// changing any unrestricted grant's behaviour. Computed BEFORE the
		// sorcery-speed gate so the sneak half below can be offered at its own
		// instant-speed declare-blockers window, which that gate rejects.
		plain, mutate, blitz, sneak := e.mayPlayKinds(p, id)
		// Sneak from the granted zone (CR 702.190a): Ninja Teen's level-3
		// `ValidSA$ Spell.Sneak` permission lets a creature card in the
		// graveyard be cast for its granted sneak cost during the caster's
		// declare-blockers step ("creature cards in your graveyard have sneak
		// {3}{B}"). The offer prices the SAME composed cost beginCast's
		// "sneak" arm charges (sneakCosts, the ONE reader), so the two stages
		// cannot drift, and the mode is the canonical "sneak" so the pay-time
		// FlagSneaked provenance and the CR 702.190b entry rider fire exactly
		// as they do for a printed hand cast. This half sits ABOVE the
		// sorcery-speed timing gate on purpose -- a creature with no Flash is
		// still castable here. off.key == "" matches the mutate/blitz halves'
		// precedent (the corpus carries no MayPlayText$-typed Sneak
		// permission; an untyped Ninja Teen permission is the only one).
		if off.key == "" && sneak && e.sneakTimingOK(p) {
			for _, sc := range e.sneakCosts(p, id) {
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
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		if plain && e.castTargetsAvailable(p, id, f.SpellAbility()) {
			base := pay.RawBaseCost(asPayer(e), p, id)
			// A MayPlayText$-typed offer carries its own permission's riders
			// (mayPlayPermissions); an untyped offer keeps the aggregate read
			// every existing grant used.
			if off.key == "" {
				if free, ok := e.mayPlayGrant(p, id); ok && free {
					// MayPlayWithoutManaCost$ True (the kw-mayplay predicate): the
					// mana part is free, exactly as beginCast's "mayplay" case will
					// charge it; non-mana additional costs still apply (CR 118.9).
					base = Cost{}
				}
			} else if off.free {
				base = Cost{}
			}
			// CR 118.3a: the granting static's RaiseCost$ surcharge is added on
			// top of the printed cost (Kotis, Sibsig Champion's "by exiling
			// three other cards ... in addition to paying its other costs").
			// Composed before offerCostFor so static cost modifiers apply to the
			// raised cost, the CR 601.2f order; the affordability gate below then
			// prices the whole cost against real state (nonManaCastable). A raise
			// mayPlayStatic could not price never reaches here -- mayPlayGrant
			// withholds the card -- but the defensive continue keeps the two
			// sites agreeing if that ever changes.
			if off.key == "" {
				if raise, hasRaise, priced := e.mayPlayRaiseCost(p, id); hasRaise {
					if !priced {
						continue
					}
					base = base.Plus(raise)
				}
			} else if off.hasRaise {
				if !off.priced {
					continue
				}
				base = base.Plus(off.raise)
			}
			cost := pay.WithSpellAbilityExtras(f, w.offerCostFor(p, id, base, spellScope("mayplay")))
			if w.affordable(p, id, cost, false) {
				label := "Cast " + f.Name
				if off.text != "" {
					// MayPlayText$ is the permission's label, so a card matching
					// several permissions offers one named option per still-unused
					// permission (Muldrotha's artifact creature).
					label = "Cast " + f.Name + " (" + off.text + ")"
				}
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: label, Obj: id, Mode: "mayplay", MayPlayPerm: off.key})
			}
			// Bargain is an additional cost on any spell cast through this
			// permission. Price independently from the permission-adjusted raw
			// base: the plain MayPlay option need not be affordable for the
			// Bargain reduction to make this one legal. Compose modifiers once,
			// in Bargain scope (the prior plain cost was already composed in
			// MayPlay scope and must not be fed back through offerCastable).
			if bg := &optionalSacrifices[optSacBargain]; e.stackKeywordPossibleH(id, kwhBargain) &&
				len(e.optionalSacrificeCandidates(bg, p, id, 0)) > 0 {
				bargainedCost := pay.WithSpellAbilityExtras(f,
					w.offerCostFor(p, id, base, spellScope(bg.mode)))
				if w.affordable(p, id, bargainedCost, false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + f.Name + " (" + bg.mode + ")", Obj: id,
						Mode: bg.mode, MayPlayPerm: off.key})
				}
			}
		}
		// Mutate half: the permission names the mutate cast. The mutate cast
		// pays the mutate cost in place of the mana cost and targets a
		// non-Human creature its controller owns -- the same synthesized
		// target SA and the same cost substitution the hand walk's mutate
		// offer uses (legal.go's hand branch, beginCast's "mutated" case),
		// which prices the mutate cost regardless of the zone the card is
		// cast from. A may-play mutate carries no printed-mana "free"
		// exemption: MayPlayWithoutManaCost$ is a property of the permission,
		// but the mutate cost IS the mana cost this cast pays.
		if off.key == "" && mutate {
			if mc, ok := mutateCost(f); ok && e.castTargetsAvailable(p, id, mutateTargetSA()) &&
				w.offerCastable(p, id, mc, spellScope("mutated"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (mutated)", Obj: id, Mode: "mutated"})
			}
		}
		// Blitz half: the permission names the blitz cast (Sabin, Master Monk's
		// `ValidSA$ Spell.Blitz`, "You may cast CARDNAME from its graveyard
		// using its blitz ability"). CR 702.152a's alternative cost replaces
		// the mana cost, so the graveyard offer prices the printed K:Blitz
		// parameter (including its Discard<1/Card> part) through the same
		// keywordAltCost the hand walk uses; beginCast's "blitzed" case charges
		// exactly that cost, so offer and charge cannot drift.
		if off.key == "" && blitz {
			if bc, ok := keywordAltCost(f, "Blitz"); ok &&
				e.castTargetsAvailable(p, id, f.SpellAbility()) &&
				w.offerCastable(p, id, bc, spellScope(altMode(altBlitz)), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (" + altMode(altBlitz) + ")", Obj: id, Mode: altMode(altBlitz)})
			}
		}
	}
}

// commandZoneWalk is the CR 903.8 command-zone cast section.
func (w *legalWalk) commandZoneWalk() {
	e, p := w.e, w.p
	sorcery := w.sorcery
	costStatics := &w.costStatics
	out := &w.out
	// Command zone (CR 903.8, Commander format): a player may cast a
	// commander they own from the command zone. This is a SECOND cast source
	// alongside the hand walk above, never a replacement for it. A
	// commander's timing is its own card's -- a creature commander is
	// sorcery-speed, an instant/flash one instant-speed -- gated by the same
	// sorcery check every other cast uses. The offer is gated on castable
	// with the SAME taxed cost beginCast will charge (commanderTaxFor over
	// the same board), so an offered command-zone cast and the cost it pays
	// structurally cannot disagree. Only a Commander game offers anything
	// here; the explicit format gate is what keeps these rules out of every
	// other game (and TestCommanderTaxGatedOffOutsideCommanderFormat exercises
	// it with a commander actually present in the command zone of a
	// Constructed game), not the
	// incident that a Constructed command zone is normally empty.
	for _, id := range e.G.Zone(state.ZCommand, p) {
		if e.format != FormatCommander {
			continue
		}
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil {
			continue
		}
		if w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		targetsAvailable := e.castTargetsAvailable(p, id, f.SpellAbility())
		if targetsAvailable && w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id), spellScope(""), false) {
			w.add("cast", w.castLabel(f), id)
		}
		if targetsAvailable {
			for i, extra := range e.optionalCostViews(costStatics.get(), p, id) {
				if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, pay.RawBaseCost(asPayer(e), p, id)).Plus(extra), spellScope("optionalcost"), false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast", Label: "Cast " + f.Name + " (optional cost)", Obj: id, Mode: "optionalcost", AltCostIndex: i + 1})
				}
			}
		}
		// Alternative costs replace the printed mana cost but not additional
		// costs such as commander tax (CR 118.9d, 903.8). Dash and the other
		// cast alternatives therefore remain available from the command zone;
		// offerCostFor applies the same tax beginCast later charges.
		for i := range altCastModes {
			ka := &altCastModes[i]
			if !ka.cmdLoop {
				continue
			}
			alt, ok := ka.faceCost(f, w.e.walkFaceFactsOf(f))
			if !ok || (!ka.untargeted && !targetsAvailable) ||
				!w.offerCastable(p, id, alt, spellScope(ka.mode), false) {
				continue
			}
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + ka.mode + ")", Obj: id, Mode: ka.mode})
		}
		// Bestow (CR 702.114a), the command-zone half (a bestowed commander,
		// kestia_the_cultivator's shape): the same synthesized-attach-SA gate
		// the hand walk applies.
		if ba, ok := altCastModes[altBestow].faceCost(f, e.walkFaceFactsOf(f)); ok && e.castTargetsAvailable(p, id, bestowedAttachSA()) &&
			w.offerCastable(p, id, ba, spellScope(altMode(altBestow)), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + altMode(altBestow) + ")", Obj: id, Mode: altMode(altBestow)})
		}
		// Mutate (CR 702.140a), the command-zone half (a commander printed
		// with mutate may be cast for its mutate cost, CR 903.3d): the same
		// synthesized-target-SA gate the hand walk applies.
		if mc, ok := mutateCost(f); ok && e.castTargetsAvailable(p, id, mutateTargetSA()) &&
			w.offerCastable(p, id, mc, spellScope("mutated"), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (mutated)", Obj: id, Mode: "mutated"})
		}
		// Offspring (CR 702.175a), the command-zone half (a commander printed
		// with offspring, or granted it by a static): the same base+additional
		// composition the hand walk offers.
		if targetsAvailable && e.hasCastOffspring(id) {
			if oc, ok := e.offspringCost(id); ok &&
				w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id).Plus(oc), spellScope("offspring"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (offspring)", Obj: id, Mode: "offspring"})
			}
		}
		// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a), the
		// command-zone half: the same fixed-{3} face-down offer the hand walk
		// makes, on the same terms -- it deliberately does NOT gate on
		// targetsAvailable (CR 708.4: a face-down spell has no targets), and
		// the printed keyword parameter (the turn-face-up cost) is not paid
		// now. offerCastable composes the CR 903.8 commander tax on top of
		// the {3}, exactly what beginCast charges for this mode.
		if fam := morphDownFamily(f); fam != "" &&
			w.offerCastable(p, id, Cost{Generic: 3}, spellScope(fam), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (face down)", Obj: id, Mode: fam})
		}
	}
}

// graveyardCastsWalk is the graveyard alternative-cast section (harmonize,
// flashback, aftermath, warp, escape, retrace, jump-start, mayhem), over the
// graveyard cards that can open any of those routes (graveyardCandidates).
func (w *legalWalk) graveyardCastsWalk() {
	e := w.e
	zone := e.G.Zone(state.ZGraveyard, w.p)
	// The candidate list is built in the engine's scratch, taken for the
	// section (a re-entrant walk builds its own) and handed back after.
	buf := e.graveCandBuf
	e.graveCandBuf = nil
	grave, filtered := w.graveyardCandidates(zone, buf[:0])
	switch {
	case !filtered:
		w.graveyardCastsOver(zone)
		e.graveCandBuf = buf[:0]
		return
	case walkSkipVerify && len(grave) != len(zone):
		w.verifyGraveyardCandidates(zone, grave)
	default:
		w.graveyardCastsOver(grave)
	}
	e.graveCandBuf = grave[:0]
}

// graveyardCastsOver is graveyardCastsWalk's body over the graveyard ids
// grave (in zone order).
func (w *legalWalk) graveyardCastsOver(grave []state.ObjID) {
	e, p := w.e, w.p
	sorcery := w.sorcery
	out := &w.out
	// Harmonize is a graveyard alternative. It is offered as its own cast
	// transaction, then spellRestZone exiles it after resolution.
	for _, id := range grave {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		f := o.Face()
		// The printed-keyword read is the cheap gate, so it runs first: every
		// gate here is a pure read, so the order changes no answer. A face with
		// no keyword line at all answers "absent" to every KeywordParam, so it
		// is skipped before the reader builds its Cost.
		if len(f.Keywords) == 0 {
			continue
		}
		hc, ok := harmonizeCost(f)
		if !ok || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		if e.castTargetsAvailable(p, id, f.SpellAbility()) {
			hc, _ = e.harmonizePayment(p, id, hc)
			if w.offerCastable(p, id, hc, spellScope("harmonize"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast", Label: "Cast " + f.Name + " (harmonize)", Obj: id, Mode: "harmonize"})
			}
		}
	}

	// Flashback: a graveyard walk, same instant-speed timing as hand cards,
	// gated on the derived keyword (so a continuous-effect grant, e.g.
	// Snapcaster Mage, counts) rather than the printed one.
	for _, id := range grave {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhFlashback) {
			continue
		}
		if w.castRestricted(p, id) {
			continue
		}
		if e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) {
			continue
		}
		if !e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		// CR 702.34a/601.2f: the flashback cost replaces the mana cost only;
		// the spell's own additional costs (withSpellAbilityExtras) are
		// still paid, exactly as beginCast charges them.
		if fc := pay.WithSpellAbilityExtras(f, e.flashbackCost(id)); w.offerCastable(p, id, fc, spellScope("flashback"), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (flashback)", Obj: id, Mode: "flashback"})
		}
		// CR 702.34a/113.2c: every flashback instance is its own permission.
		// A card that already has flashback and gains another (Sphinx of
		// Forgotten Lore's "flashback cost equal to its mana cost" on Think
		// Twice) may be cast through either, so each further distinct cost is
		// offered as its own option carrying that cost (Option.Cost), which
		// beginCast charges.
		for _, alt := range e.extraFlashbackCosts(id) {
			if fc := pay.WithSpellAbilityExtras(f, ParseCost(alt)); w.offerCastable(p, id, fc, spellScope("flashback"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (flashback " + alt + ")", Obj: id, Mode: "flashback", Cost: alt})
			}
		}
	}

	// Aftermath (CR 702.85a): the alternate face of a Split card may be cast
	// from its owner's graveyard for its printed mana cost (plus its own SP
	// Cost$ additional parts -- start_finish's Sac<1/Creature>), then exiled.
	// Gated on the ALTERNATE face's K:Aftermath, which is what excludes Rooms
	// (both halves are Rooms, neither carries Aftermath) and Adventures
	// (AlternateMode Adventure, not Split). Mode aftermath is consumed by
	// beginCast, which records a FlipFace to the alternate face before the
	// ordinary cast transaction -- rawBaseCost, targets and resolution then
	// read the aftermath face. The withSpellAbilityExtras fold prices the
	// face's own SP Cost$ parts exactly like the adventure_alt offer above:
	// without it a Finish-shaped gate would offer an unpayable cast. The
	// prohibition gate probes the AFTERMATH face being cast
	// (castRestrictedAsFace), not the front face still displayed in the
	// graveyard: a restriction matching only one half must decide only that
	// half's offer.
	for _, id := range grave {
		o := e.G.Obj(id)
		af := aftermathAlternateFace(o)
		if af == nil || w.castRestrictedAsFace(p, id, af) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, af, sorcery) {
			continue
		}
		if !e.castTargetsAvailable(p, id, af.SpellAbility()) {
			continue
		}
		if w.offerCastableAsFace(p, id, af, pay.WithSpellAbilityExtras(af, ParseCost(af.ManaCost)), spellScope("")) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + af.Name + " (aftermath)", Obj: id, Mode: "aftermath"})
		}
	}

	// MayPlay static offers are the main-side walk above (mayPlayLandIds /
	// mayPlaySpellIds over the continuous-effect grants plus the self-grant
	// scan); the kw-mayplay branch's separate mayPlayGrantOffers walk is
	// retired here so a granted card is offered exactly once.

	// Warp from the graveyard requires a separate MayPlay Spell.Warp static;
	// Warp itself grants only the hand alternative. Timeline Culler is the
	// corpus shape carrying that explicit graveyard permission.
	for _, id := range grave {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || len(f.Keywords) == 0 {
			// No keyword line: KeywordParam("Warp") is absent.
			continue
		}
		wc, ok := keywordAltCost(f, "Warp")
		if !ok || !warpGraveyardAllowed(f) || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		instantSpeed := f.IsInstant() || e.hasKeywordH(id, kwhFlash)
		if !instantSpeed && !sorcery {
			continue
		}
		if !e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if w.offerCastable(p, id, wc, spellScope(altMode(altWarp)), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (" + altMode(altWarp) + ")", Obj: id, Mode: altMode(altWarp)})
		}
	}

	// Escape (CR 702.42a): a card in its owner's graveyard carrying the
	// Escape keyword -- printed (Kroxa's K:Escape) or granted by a continuous
	// effect (Underworld Breach's AddKeyword$ Escape, which Derived reads off
	// the layer system) -- may be cast for its escape cost: the card's mana
	// cost plus ExileFromGrave<N/Card.Other> parts, exiling N OTHER cards
	// from the same graveyard. The cast is a normal cast (CR 702.42a gives
	// no post-resolution destination change -- unlike flashback the spell
	// goes where it would otherwise go), so modeFlags marks it FlagEscaped
	// and the ETB machinery reads the flag through Card.Self+escaped.
	for _, id := range grave {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhEscape) || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		ec, ok := e.escapeCost(id)
		if !ok || !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if w.affordable(p, id, w.offerCostFor(p, id, ec, spellScope("escape")), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (escape)", Obj: id, Mode: "escape"})
		}
	}

	// Retrace (CR 702.81a): a card in its owner's graveyard carrying the
	// Retrace keyword may be cast from there by paying its printed mana cost
	// PLUS an additional cost of discarding a land card. Unlike Escape this
	// is not a cost substitution, so the offer prices the ordinary plain-cast
	// base (castOfferBase credits Convoke/Improvise, withSpellAbilityExtras
	// adds the spell's own additional parts) with retraceExtra folded on top.
	// The discard is a real hand cost, so the offer is withheld unless a land
	// card is actually there to discard -- an option that cannot be paid must
	// never be offered (the offerCastable/withSpellAbilityExtras ruling).
	for _, id := range grave {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhRetrace) || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		rx := retraceExtra()
		if !pay.DiscardCostPayable(asPayer(e), p, id, rx.Discard, true) {
			continue
		}
		if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, e.castOfferBase(p, id)).Plus(rx), spellScope("retrace"), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (retrace)", Obj: id, Mode: "retrace"})
		}
	}

	// Jump-start (CR 702.84a): "You may cast this card from your graveyard by
	// discarding a card in addition to paying its other costs. Then exile this
	// card." The retrace shape with any card discardable in place of a land,
	// and the flashback destination on resolution. The keyword takes no
	// parameter (all 13 corpus lines are the bare K:Jump-start), so the whole
	// cost is the printed mana cost plus the additional Discard<1/Card>.
	// Gated on the derived keyword (HasKeyword), so a continuous-effect grant
	// would count; the same timing/target/restriction gates as every other
	// graveyard alt-cast, and the discount payable gate -- an option whose
	// discard cannot be paid must never be offered (the offerCastable ruling).
	for _, id := range grave {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || !e.hasKeywordH(id, kwhJumpStart) || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		js := jumpstartExtra()
		if !pay.DiscardCostPayable(asPayer(e), p, id, js.Discard, true) {
			continue
		}
		if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, e.castOfferBase(p, id)).Plus(js), spellScope("jumpstart"), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (jump-start)", Obj: id, Mode: "jumpstart"})
		}
	}

	// Mayhem (the Doom Prevails keyword): a card in its owner's graveyard
	// that was discarded THIS TURN may be cast for its mayhem cost -- a cost
	// SUBSTITUTION ("cast this card from your graveyard for {4}{R}"), not an
	// addition, and no post-resolution destination change: the oracle's only
	// rider is "Timing rules still apply", which spellTimingOK enforces, so
	// the spell resolves like an ordinary cast. The provenance gate is
	// log-derived (mayhemDiscardedThisTurn), the same shape the warp-recast
	// and foretell gates take, so a replayed game derives the same offer;
	// the cost helper reads the derived keyword list, so a continuous-effect
	// grant would count. The bare parameterless K:Mayhem is the "play this
	// card" LAND shape (Oscorp Industries) and is withheld here -- not a
	// cast. Offer and charge both go through mayhemCastCost, so they cannot
	// drift.
	for _, id := range grave {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil {
			continue
		}
		// mayhemCastCost reads the derived Mayhem parameter, which is absent
		// whenever its subset precheck rules the head out (keywordmay.go).
		if !e.mayHaveDerivedKeywordH(id, kwhMayhem) {
			if derivedMemoVerify {
				e.verifyKeywordPrecheck(id, "Mayhem")
			}
			continue
		}
		mc, ok := e.mayhemCastCost(id)
		if !ok || w.castRestricted(p, id) || e.castSuppressed(p, id) || !e.mayhemDiscardedThisTurn(p, id) {
			continue
		}
		if !e.spellTimingOK(p, id, f, sorcery) ||
			!e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		if w.offerCastable(p, id, pay.WithSpellAbilityExtras(f, mc), spellScope("mayhem"), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (mayhem)", Obj: id, Mode: "mayhem"})
		}
	}
}

// exileCastsWalk is the exile recast section (warp recast).
func (w *legalWalk) exileCastsWalk() {
	e, p := w.e, w.p
	sorcery := w.sorcery
	out := &w.out
	// Warp recast from exile (CR 702: "exile this creature at the beginning
	// of the next end step, then you may cast it from exile on a later
	// turn"). The exile-zone walk offers the cast only to a warp card that
	// the log shows was warp-cast and end-step-exiled, on a turn strictly
	// after that exile -- the flag alone cannot say it (CastFlags reset when
	// the permanent left the battlefield), but the log can. This later cast
	// pays the normal mana cost and is not itself flagged warped.
	//
	// The airbend recast permission is log-derived too; the engine's
	// incremental airbend index (airbend.go) answers each exiled card in O(1).
	airbendAvailable := e.airbendCastAvailable
	for _, id := range e.G.Zone(state.ZExile, p) {
		o := e.G.Obj(id)
		f := o.Face()
		if f == nil || o.IsToken {
			continue
		}
		// CR 722.3c: the prepared designation's exile copy -- an IsCopy
		// object whose PreparedSource names a battlefield permanent -- may be
		// cast by that permanent's controller for as long as the permanent
		// remains prepared. Its Face() is the prepare spell, so timing,
		// targets, cost and resolution all run the ordinary stages: the cast
		// pays the prepare spell's own mana cost (the reminder grants a cast,
		// not a free one; CR 601.2f), and the designation is removed at cast
		// time (rules/cast.go's pushCast), not on resolution.
		if o.IsCopy && o.PreparedSource != 0 {
			if src := e.G.Obj(o.PreparedSource); src != nil && src.Zone == state.ZBattlefield &&
				src.Prepared && src.Controller == p &&
				!w.castRestricted(p, id) && !e.castSuppressed(p, id) &&
				e.spellTimingOK(p, id, f, sorcery) &&
				e.castTargetsAvailable(p, id, f.SpellAbility()) {
				if w.offerCastable(p, id, pay.RawBaseCost(asPayer(e), p, id), spellScope("prepared_copy"), false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + f.Name + " (prepared)", Obj: id, Mode: "prepared_copy"})
				}
			}
			continue
		}
		// CR 714.3a: the main face of an Adventure card resting in the
		// adventure zone (exile, at its Adventure spell face) may be cast from
		// there. Mode adventure_recast is consumed by beginCast, which flips
		// the card back to its main face before the ordinary cast transaction
		// -- so everything downstream reads the main face. The provenance (the
		// card got here by RESOLVING an adventure_alt cast) is log-derived:
		// adventureZoneAvailable. The cost is built from the MAIN face
		// explicitly -- rawBaseCost reads o.Face(), which is still the
		// Adventure face in exile, exactly the reason the Room offer parses its
		// own face's cost too. The adventure-zone entry from a non-resolution
		// path (CR 714.3b) is out of scope, and adventureZoneAvailable refuses
		// it. The prohibition gate probes the MAIN face the recast actually
		// casts (castRestrictedAsFace), not the Adventure spell face still
		// displayed in exile: probing the displayed face would withhold an
		// unrestricted front cast or offer a prohibited one whenever the
		// restriction distinguishes the two faces.
		if adventureSpellFace(o) != nil && int(o.FaceIdx) == 1 && e.adventureZoneAvailable(id) &&
			!w.castRestrictedAsFace(p, id, o.Card.Faces[0]) && !e.castSuppressed(p, id) {
			front := o.Card.Faces[0]
			if e.spellTimingOK(p, id, front, sorcery) && e.castTargetsAvailable(p, id, front.SpellAbility()) &&
				w.offerCastableAsFace(p, id, front, ParseCost(front.ManaCost), spellScope("")) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + front.Name + " (from adventure zone)", Obj: id, Mode: "adventure_recast"})
			}
			continue
		}
		// Foretell cast (CR 702.126a): a card exiled face down by the {2}
		// Foretell ACTION (not by any other effect -- the flag is the action's
		// own provenance marker) may be cast from exile for its FORETELL cost
		// (the K: line's colon parameter) on a later turn. "Later turn" is
		// log-derived (foretellCastAvailable): the flag alone cannot say it,
		// the same reason the warp recast offer below is log-derived. The cast
		// follows the card's own timing (spellTimingOK) and targets. The block
		// sits BEFORE the warp gate's continue: a non-warp card (every foretell
		// carrier) would otherwise never reach it.
		if o.CastFlags&state.FlagForetold != 0 &&
			e.foretellCastAvailable(id) && !w.castRestricted(p, id) && !e.castSuppressed(p, id) &&
			e.spellTimingOK(p, id, f, sorcery) && e.castTargetsAvailable(p, id, f.SpellAbility()) {
			// The K:Foretell parameter prices the later cast (CR 702.126a);
			// a face with no parameter falls back to the rule's action default
			// {2} -- every corpus carrier carries one (measured 55/55), so the
			// fallback is latent. The RAW parsed cost is offered here; cost
			// modifiers (CR 601.2f) apply later, in manaToPay, exactly like
			// the other alternative-cost recasts.
			fc, ok := foretellCost(f)
			if ok && w.offerCastable(p, id, fc, spellScope("foretell_cast"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (foretold)", Obj: id, Mode: "foretell_cast"})
			}
		}
		// Plot's free cast (CR 701.34b): a card carrying the plotted
		// designation (Object.PlottedTurn, the AlterAttribute fold; only the
		// plot ACTION and the corpus's own Attribute grants set it, so an
		// arbitrary exiled Plot carrier is never offered) may be cast from
		// exile without paying its mana cost "on a later turn" -- strictly
		// after the turn it became plotted, NOT after a counter count (Plot
		// has no counters; that is Suspend's mechanic). It is a STANDING
		// permission that follows SORCERY timing whatever the face's own type
		// is -- even a plotted instant waits for its owner's main phase with
		// an empty stack -- so the gate is the engine's sorcery-speed bool
		// plus the face's activation-phase gates, re-offered every priority
		// round the permission holds. The offer sits BEFORE the warp gate's
		// continue, the foretell block's own reason.
		if o.PlottedTurn > 0 && e.G.Turn > o.PlottedTurn &&
			!w.castRestricted(p, id) && !e.castSuppressed(p, id) &&
			sorcery && e.spellTimingOK(p, id, f, true) &&
			e.castTargetsAvailable(p, id, f.SpellAbility()) {
			if w.offerCastable(p, id, Cost{}, spellScope("plot_cast"), false) {
				*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
					Label: "Cast " + f.Name + " (plotted)", Obj: id, Mode: "plot_cast"})
			}
		}
		// Airbend recast (CR 701.65a): a card airbent into exile may be cast
		// by its OWNER for {2} rather than its mana cost, for as long as it
		// remains exiled. The provenance is log-derived (airbendCastAvailable:
		// the exile move's effects.AirbendExileCounter marker), the same shape
		// the warp and foretell offers in this walk take; the offer rides the
		// card's own timing and targets exactly like warp_recast. The block
		// sits BEFORE the warp gate's continue: a non-warp card (every airbent
		// card) would otherwise never reach it.
		if airbendAvailable(id) && !w.castRestricted(p, id) && !e.castSuppressed(p, id) {
			instantSpeed := f.IsInstant() || e.hasKeywordH(id, kwhFlash)
			if (instantSpeed || sorcery) && e.spellTimingOK(p, id, f, sorcery) &&
				e.castTargetsAvailable(p, id, f.SpellAbility()) {
				if w.offerCastable(p, id, Cost{Generic: 2}, spellScope("airbend_cast"), false) {
					*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
						Label: "Cast " + f.Name + " (airbent)", Obj: id, Mode: "airbend_cast"})
				}
			}
		}
		_, ok := keywordAltCost(f, "Warp")
		if !ok || !e.warpRecastAvailable(id) || w.castRestricted(p, id) || e.castSuppressed(p, id) {
			continue
		}
		instantSpeed := f.IsInstant() || e.hasKeywordH(id, kwhFlash)
		if !instantSpeed && !sorcery {
			continue
		}
		if !e.castTargetsAvailable(p, id, f.SpellAbility()) {
			continue
		}
		normal := pay.RawBaseCost(asPayer(e), p, id)
		if w.offerCastable(p, id, normal, spellScope("warp_recast"), false) {
			*out = append(*out, decision.Option{Index: len(*out), Kind: "cast",
				Label: "Cast " + f.Name + " (from warp exile)", Obj: id, Mode: "warp_recast"})
		}
	}
}
