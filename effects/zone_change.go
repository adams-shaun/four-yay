package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// changeZoneAltDestination resolves ChangeZone's conditional alternate
// destination (Forge's ChangeZoneEffect.handleAltDest): DestAltSVar$ names an
// SVar (or inline count expression) evaluated against the resolving host card
// and compared under DestAltSVarCompare$ (default GE1, i.e. truthy). When the
// condition holds, the move takes DestinationAlternative$ instead of
// Destination$.
//
// The optional "MANDATORY " prefix is stripped. Forge reads MANDATORY as the
// difference between forcing the alternate and offering the player a
// confirmAction; this engine has no destination-confirm ask, so BOTH branches
// take the alternate deterministically when the condition holds, and the
// non-mandatory shape records one Note disclosing the dropped confirm (the
// expansion-specific riders of six corpus carriers, all Destination$ Hand ->
// DestinationAlternative$ Battlefield). MANDATORY itself therefore changes no
// behaviour today; it is parsed so the two spellings cannot drift.
//
// Unlike CheckSVarHolds's other call sites, an unreadable condition here fails
// CLOSED to the primary destination (plus a Note): moving a card to a zone the
// condition cannot justify would be a silently wrong board, whereas keeping
// the primary is the pre-existing behaviour and therefore replay-safe.
func changeZoneAltDestination(h Host, c *Ctx, cz *ChangeZoneParams, primary state.Zone) state.Zone {
	if cz.DestAltSVarText == "" {
		return primary
	}
	holds, evaluated := CheckSVarHolds(h, c, cz.DestAltCond, cz.DestAltSVarCompare)
	if !evaluated {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DestAltSVar$ " + cz.DestAltSVarText +
				" is not a condition this engine can evaluate; the move takes the primary destination"})
		return primary
	}
	if !holds {
		return primary
	}
	if !cz.DestinationAltKnown {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DestAltSVar$ " + cz.DestAltSVarText +
				" holds but DestinationAlternative$ " + cz.DestinationAltText +
				" is not a zone this engine models; the move takes the primary destination"})
		return primary
	}
	if !cz.DestAltMandatory {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DestAltSVar$ " + cz.DestAltSVarText +
				" holds: the alternate destination " + cz.DestinationAltText +
				" is taken (Forge would ask which destination; this engine does not ask)"})
	}
	return cz.DestinationAlt
}

// clearChangeZoneImprint uses the same event as Cleanup's ClearImprinted rider.
// In particular, ImprintLast replaces rather than appends on every mover.
func clearChangeZoneImprint(h Host, c *Ctx) {
	if c.Source == 0 {
		return
	}
	if o := h.Game().Obj(c.Source); o != nil && (len(o.Imprinted) > 0 || len(o.ImprintTokens) > 0 || len(o.SeekFound) > 0) {
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, Text: "clear"})
	}
}

// changeZonePrelude is effChangeZone's entry, up to the dispatch on Origin$:
// the unread-parameter Note, the UntilHostLeavesPlay bail (stop), Unimprint's
// pre-move clear, the DestAltSVar$ destination and the unrecognised
// OriginAlternative$ Note. It is shared with the resolution kernel's answered
// asks (changeZoneReentryEcho), which re-emit exactly what a legacy re-entry
// of effChangeZone emits on its way back to the answered walk.
func changeZonePrelude(h Host, c *Ctx, cz *ChangeZoneParams) (to state.Zone, stop bool) {
	cz.noteUnread(h, c)
	if exileHostGoneFor(h, c, cz.Riders.Duration) {
		return 0, true
	}
	// Unimprint is a pre-move operation, even when no candidate is moved.
	// Re-entering after a choice may clear an already empty list; the fold
	// remains replayable and the later successful move supplies the new card.
	if cz.Unimprint {
		clearChangeZoneImprint(h, c)
	}
	to = changeZoneAltDestination(h, c, cz, cz.Destination)
	if cz.OriginPresent && cz.OriginAltPresent && !cz.OriginAltOK {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unrecognised ChangeZone OriginAlternative " + cz.OriginAltText})
	}
	return to, false
}

// changeZoneDefinedPlayerNote is the object path's loud read of a Hand-origin
// DefinedPlayer$ beside a Defined$ that already names the moved objects.
func changeZoneDefinedPlayerNote(h Host, c *Ctx, cz *ChangeZoneParams, originZones []state.Zone, originAll bool) {
	if len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
		cz.Defined != "" && cz.DefinedPlayer.Text != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DefinedPlayer$ " + cz.DefinedPlayer.Text +
				" is unread next to Defined$ " + cz.Defined +
				" (the move goes to the named objects alone)"})
	}
}

func effChangeZone(h Host, c *Ctx, sa *cards.SA) {
	cz := ChangeZoneOf(sa)
	to, stop := changeZonePrelude(h, c, cz)
	if stop {
		return
	}
	// Set only when an explicit multi-zone Origin$ including Hand falls
	// through the dedicated walkers above to the object path; the diagnostic
	// for a resolution that ends up moving nothing is emitted after the move
	// loop, where `moved` knows the truth.
	mixedOriginNoteFrom := ""
	var originZones []state.Zone
	var originAll bool
	if cz.OriginPresent {
		from := cz.OriginText
		valid := cz.OriginOwnOK
		originZones, originAll = cz.Origin, cz.OriginAll
		// OriginAlternative$ is Forge's "and/or" second origin: the zones
		// named there join Origin$ into ONE candidate set at the
		// choose-a-card-from-any-of-these-zones step ("search your graveyard,
		// hand, and/or library"). Every one of the corpus's 62 carriers pairs
		// it with Origin$ Library; without this merge the exact-Library branch
		// below sees a library-only origin and silently searches just that.
		// Compound Origin$ spells use the same union. An object-valued
		// selector (Eladamri's ChosenCard) has already made its choice, while
		// a player-valued selector needs a choice from that player's zones.
		// Parse with the same vocabulary as Origin$. A zone word ParseZones
		// does not model an origin is noted loudly and dropped from the
		// merged set while every KNOWN zone keeps searching -- bailing the
		// whole effect (folding altValid into `valid`) would lose the library
		// half of invasion_of_arcavios's "library, graveyard, and/or outside
		// the game", a regression over the pre-OriginAlternative engine,
		// which still searched the library. (The unrecognised-word Note is
		// emitted by changeZonePrelude.)
		hidden := cz.Hidden
		// ... and the branch excludes every origin the dedicated walkers own:
		// exactly-Library is the search below, exactly-Hand the hand movers,
		// a mixed-Hand origin the loud note -- and this branch must sit BEFORE
		// the unrecognised-Origin bail, because some hidden origins may still
		// resolve (Burning Wish's wish, whose
		// SubAbility$ self-exile must run). Origin$ All stays on the object
		// path too -- every no-Defined$ corpus line naming it carries Defined$
		// (all eight are Dauthi-shaped replacements), and a game-wide all-zones
		// pick would offer hidden hand/library cards by name. When a Defined$
		// DOES name the objects, Forge's resolver takes them without a choose
		// ask, reveals nothing and shuffles nothing (`!defined` fails both the
		// reveal and the shuffle conditions) -- exactly what the object path
		// below already performs, which is why Dauthi Voidwalker's Hidden$
		// "exile it instead" replacement carries no behaviour of its own
		// beyond this read. A Hidden$ compound origin naming Sideboard skips
		// this branch too: the search below is its chooser (Karn, the Great
		// Creator's -2), and effHiddenPick's game-wide or owner-public fetch
		// list is the wrong shape for an owner-private sideboard union.
		if hidden && cz.Defined == "" && !cz.Imprint &&
			!originAll && !mixedOriginIncludesHand(originZones, originAll) &&
			!zoneIn(originZones, state.ZLibrary) &&
			!zoneIn(originZones, state.ZSideboard) &&
			!(len(originZones) == 1 && originZones[0] == state.ZHand) {
			effHiddenPick(h, c, sa, cz, to, originZones, originAll, valid, from)
			return
		}
		// hiddenpick1: Forge's SpellAbility.isHidden() (hasParam("Hidden") ||
		// the origin zones hold hidden info) routes the resolution through the
		// hidden-origin resolver (ChangeZoneEffect.changeHiddenOriginResolve)
		// even when the origin zones are PUBLIC. There the objects are not the
		// source default: with no Defined$ the fetch list is the origin zones'
		// cards matching ChangeType$ -- game-wide for a public origin when no
		// fetch player is named (Kor Skyfisher's bounce, Temur Sabertooth's
		// "another creature", the graveyard/exile mill follow-ups) or the
		// DefinedPlayer$/targeted player's own zones (Relic of Progenitus) --
		// and the resolver asks the chooser to pick ChangeNum$ of them. The
		// object path below would instead move the Defined() source default
		// silently (a self-bounce) or skip the player fetchers entirely (a
		// silent no-op). Hidden sideboard searches resolve here (Burning Wish's
		// wish, whose SubAbility$ self-exile
		// must run), which is why the branch sits before the
		// unrecognised-Origin bail. Origin$ All stays on the object path too
		// -- every no-Defined$ corpus line naming it carries Defined$ (all
		// eight are Dauthi-shaped replacements), and a game-wide all-zones
		// pick would offer hidden hand/library cards by name. When a Defined$
		// DOES name the objects, Forge's resolver takes them without a choose
		// ask, reveals nothing and shuffles nothing (`!defined` fails both the
		// reveal and the shuffle conditions) -- exactly what the object path
		// below already performs, which is why Dauthi Voidwalker's Hidden$
		// "exile it instead" replacement carries no behaviour of its own
		// beyond this read.
		if !valid {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unrecognised ChangeZone Origin " + from})
			return
		}
		// A hidden-origin fetch offers the union from Origin$ and
		// OriginAlternative$, including a player-selected hand/other-zone pair.
		// Concrete object selectors stay on the already-answered object path:
		// Eladamri's ChosenCard was picked by ChooseCard, not this search.
		// The searching player may fail to find a card with the stated quality (Min
		// is always zero), and the answer resumes this same effect before its
		// SubAbility runs. The exact-Library spelling is the single-zone case of
		// the same path; the alternatives are PUBLIC zones (Graveyard, Exile,
		// Hand) whose candidates join the library's in one option list. A
		// mixed-Hand alternative (Gate to the Afterlife's Graveyard,Hand) is
		// deliberately OWNED here rather than by the mixed-origin note below,
		// because the search IS the origin-aware chooser that note says does not
		// exist: the fetch player sees their own hand, so no hidden information
		// is exposed by offering it by name.
		fetchSelector := changeZoneFetchSelector(h, c, cz)
		if !originAll && (len(originZones) == 1 || fetchSelector) &&
			(zoneIn(originZones, state.ZLibrary) || zoneIn(originZones, state.ZSideboard) ||
				(len(originZones) > 1 && zoneIn(originZones, state.ZHand))) &&
			(!zoneIn(originZones, state.ZBattlefield) || zoneIn(originZones, state.ZHand)) {
			// Forge treats a Defined$ that resolves to objects in a hidden
			// library as the already-selected fetch list, not as the owner of a
			// fresh whole-library search. This is structural rather than keyed to
			// Remembered: ChosenCard, TopOfLibrary once resolved, and future
			// object-valued Defined selectors share the same dispatcher. Only the
			// single-zone case takes it: with OriginAlternative$ present the
			// corpus carries no Defined$ (measured 0 of 62), so this is latent
			// rather than live.
			if len(originZones) == 1 && moveDefinedLibraryObjects(h, c, sa, cz, to) {
				return
			}
			effSearchLibrary(h, c, sa, cz, to, originZones)
			return
		}
		// An unbound concrete object selector in a mixed-Hand origin must not
		// become a free search. Keep its object path -- the chooser question the
		// old note here claimed was unimplemented is answered by the search
		// path above (one private option list across the named origins, per
		// fetch player's own zones) and by this object path for a Defined$
		// that already names its objects -- and leave the diagnostic to the
		// post-move-loop emission, so a resolution whose objects DID move (a
		// chosen card, a remembered pair) is not slandered by a note.
		if mixedOriginIncludesHand(originZones, originAll) {
			mixedOriginNoteFrom = from
		}
		// A ChangeZone from exactly Hand with no object selector is Forge's
		// hidden-origin hand put-back: the chooser picks ChangeNum$ cards (a
		// ChangeType$ filter narrows the pool; its absence -- Brainstorm's "put
		// two cards from your hand on top of your library", Jace, the Mind
		// Sculptor's [0] -- offers the whole hand). With no Defined$/
		// DefinedPlayer$/ValidTgts$ the object path below would resolve Defined
		// to the SOURCE default and then skip every candidate on the Origin$
		// precondition -- the silent no-op the handmove1 fix replaces with a
		// real hand choice (the rv2b extension drops handmove1's ChangeType$
		// requirement: the whole 239-line no-selector Origin$ Hand population
		// routes here now, 19 of it untyped).
		// An SVar or inline count expression is evaluated through Num where the
		// count grammar supports it (for example Wrenn and Seven's SVar X counts
		// lands in hand). An unknown count remains loud rather than falling through
		// to the old source-default no-op: it emits a Note and moves nothing.
		if len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
			cz.Defined == "" && cz.DefinedPlayer.Text == "" && cz.ValidTgts.Text == "" && !cz.Imprint {
			if _, supported := handMoveCountOf(h, c, cz); supported {
				effChangeZoneHand(h, c, sa, cz, to)
				return
			}
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "cannot choose ChangeNum$ " + cz.ChangeNum.Text +
					" cards from hand (a non-literal count is not a bound this engine can evaluate)"})
			return
		}
		// A ChangeZone from exactly Hand whose hand OWNER is selected --
		// DefinedPlayer$-alone (Kynaios and Tiro's "each player may put a land
		// card from their hand onto the battlefield", Braids, Conjurer Adept,
		// Mindleech Ghoul) or ValidTgts$-alone naming the players (Karn
		// Liberated's "[+4]: Target player exiles a card from their hand",
		// Kyoki, Sanity's Eclipse) -- is the per-owner hidden-hand shape: one
		// chooser ask per hand owner, chained through the persisted
		// Ctx.HandMoveTarget cursor. The rv2b r2 finding: these shapes used to
		// fall through to the object path, where Defined() resolved to the
		// source (or to targets the Origin$ precondition then skipped because
		// they are PLAYERS, not hand cards) -- another silent no-op.
		if len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
			cz.Defined == "" && (cz.DefinedPlayer.Text != "" || cz.ValidTgts.Text != "") {
			effChangeZoneHandOwners(h, c, sa, cz, to)
			return
		}
		// DefinedPlayer$ alongside a Defined$ that names concrete objects
		// (Wilt-Leaf Liege's DefinedPlayer$ ReplacedPlayer + Defined$
		// ReplacedCard, 1 corpus line) keeps the object path -- the moved
		// objects are already named -- but the owner parameter is unread
		// there, so the shape is loud about it rather than silent.
		changeZoneDefinedPlayerNote(h, c, cz, originZones, originAll)
	}
	// Answered ShuffleNonMandatory$ confirm re-entry for the OBJECT path
	// (searchmay1): a hidden-library search's own re-entry is handled inside
	// effSearchLibrary above and returns, so reaching here with an answer
	// means the SA moved objects from a public origin (a graveyard/top
	// shuffle-in) and those moves already landed on the first pass. Run only
	// the answered tail -- consume the answer, shuffle on "yes" -- and stop:
	// re-resolving targets would re-run the move pass and re-pose the
	// pre-asks below against objects that have left their origin zone.
	if c.SearchShuffle != "" && objectPathShuffleOwed(cz) {
		objectPathShuffleTail(h, c, sa, cz, nil)
		return
	}
	// WithCountersType$/WithCountersAmount$ make the move put counters on the
	// object it lands with -- the Undying expansion's "return to the battlefield
	// with a +1/+1 counter" (cards/keywords.go) and a card exiled with TIME
	// counters (suspend). The CounterChange is emitted AFTER the MoveZone, so it
	// lands on the moved (new) object's back at its destination, exactly as Move
	// waiting to run first would want, and the counter survives onto the object
	// because it is added post-move. Counter (not the Move carrying it along) is
	// what keeps events/apply.go's Move from knowing anything about counters.
	// counterDestination is the one gate every mover shares: a destination that
	// cannot carry the counters neither parses the amount nor emits anything.
	withKind := cz.WithCountersType
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, cz)
	}
	targets := Defined(h, c, sa)
	targetAskPending := false
	// ValidTgts$ targeting whose ask was never offered: the placement ask
	// (rules' pushTrigger) reads only the trigger's OWN Execute SA, so a
	// deeper sub's ValidTgts$ -- the "when you do" family's shape (Forum
	// Filibuster's `TrigReturn`, 134 raw corpus T: chains reaching one) --
	// arrives here with no chosen targets and used to move nothing silently.
	// Offer the targets now, through the same Host.LegalTargets census the
	// announcement ask uses (targetZones' Origin$-implied graveyard included),
	// as a KChoose over the shared "choice" resume arm; the answer lands in
	// Ctx.Choice and the re-entered pass consumes it (fx42 scoping -- a nested
	// ChangeZone in the same chain poses its own ask). A host that cannot ask
	// takes the deterministic first-max stand-in (R-9, the same mirror the
	// effDig ask's botpolicy arm answers with option 0). The chosen bounds are
	// TargetMin$/TargetMax$ through the ordinary Num grammar (TrigReturn's
	// TargetMin$ 0 / TargetMax$ 1 -- "up to one"), clamped to the eligible
	// count; a bound pair that admits nothing (Min == Max == 0, or no eligible
	// candidate) poses no ask and moves nothing -- a decision nobody could
	// answer differently is never emitted.
	if ans, ok, served := changeZoneChosenTargetsFor(h, c, sa, &cz.changeZoneTargeting); ok {
		if served {
			// The resolution kernel's answer in hand: the "choice"
			// re-entry's own events come before it consumes the answer.
			objectPathMoveEcho(h, c, cz, to)
		}
		targets = ans
		// A suspension (nil answer, ok) leaves the chooser pending: the note
		// below must wait for the answering re-entry, which moves the targets.
		targetAskPending = ans == nil
		if ans != nil {
			// This link's own answer is a later link's ParentTarget.
			noteLinkAnswer(c, ans)
		}
	}
	// The O-Ring return shape (Journey to Nowhere, Leonin Relic-Warder): the
	// LEAVE-battlefield trigger's Execute is `DB$ ChangeZone | Defined$
	// Remembered`, and Forge reads the HOST CARD's remembered list there --
	// the cross-resolution memory RememberTargets$ wrote -- not this build's
	// Ctx binding. This build's trigger resolutions seed Ctx.Remembered with
	// the event capture (triggerRemembered), which for a self-trigger is
	// exactly [{source}], so the ctx set is distinguishable: when the
	// resolved set is exactly that capture and the source's persistent
	// Remembered is non-empty, the card's list is what the script meant.
	// Mid-chain readings are unaffected: a chain that remembered its own
	// source through the object path below wrote BOTH halves (ctx and
	// persistent), so the replacement is the same set.
	if cz.DefinedRemembered {
		if len(targets) == 1 && !targets[0].IsPlayer && targets[0].Obj == c.Source {
			if src := h.Game().Obj(c.Source); src != nil && len(src.Remembered) > 0 {
				targets = append([]state.Target(nil), src.Remembered...)
			}
		}
	}
	// AlternativeDecider$ chooses which library position receives the
	// targeted card. The owner, not the spell's controller, answers (the
	// referent may be the OPPONENT of the caster, and the ask goes to that
	// seat). Keep the ask after target resolution so TargetedOwner is bound to
	// the actual referent, and before any move so replay re-entry cannot
	// partially apply. The one shape modelled is the corpus's uniform
	// AlternativeDecider shape: ONE targeted card moving to a library whose
	// primary position is the TOP (or second from top) and whose alternative
	// position is `-1` (bottom). Other shapes stay LOUD and take the
	// pre-existing deterministic placement rather than silently offering a
	// choice the script never posed.
	altDecider := cz.AlternativeDecider
	altAnswer := string("")

	altBottom := false
	altEngaged := false
	if altDecider != "" && len(targets) > 0 {
		primaryPosition := cz.LibraryPositionText
		altPosition := cz.LibraryPositionAltText
		shapeOK := to == state.ZLibrary && len(targets) == 1 && !targets[0].IsPlayer &&
			altPosition == "-1" && (primaryPosition == "" || primaryPosition == "0" || primaryPosition == "1")
		switch {
		case !shapeOK:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "AlternativeDecider$ " + altDecider + " is not the top-or-bottom library shape this engine can ask; the primary destination is taken"})
		default:
			target := h.Game().Obj(targets[0].Obj)
			var chooser state.PlayerID
			chooserOK := false
			switch altDecider {
			case "TargetedOwner":
				if target != nil {
					chooser, chooserOK = target.Owner, true
				}
			}
			if !chooserOK || int(chooser) >= len(h.Game().Players) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "AlternativeDecider$ " + altDecider + " cannot resolve an object owner"})
			} else if altAnswer == "" {
				primaryLabel := "top"
				if primaryPosition == "1" {
					primaryLabel = "second from top"
				}
				d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "changezone_alternative", ResumeSA: sa,
					Prompt: "Choose your library position",
					Options: []decision.Option{
						{Index: 0, Kind: "primary", Label: primaryLabel},
						{Index: 1, Kind: "bottom", Label: "bottom"},
					}}
				if ans, ok := AskTape(h, d); ok {
					// The resolution kernel's answer in hand: the
					// "changezone_alternative" re-entry's own events, then
					// the answered position exactly as that re-entry reads
					// the carried label.
					objectPathMoveEcho(h, c, cz, to)
					altBottom = len(ans) > 0 && ans[0].Label == "bottom"
					altEngaged = true
				} else {

					// R-9 no-ask host: the primary placement, deterministically.
					altAnswer = "top"
					altBottom = false
					altEngaged = true
				}

			} else {
				altBottom = altAnswer == "bottom"
				altEngaged = true
			}
		}
	}
	forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
	// ForgetOtherTargets$ True (Journey to Nowhere, Leonin Relic-Warder):
	// Forge's ChangeZoneEffect.forgetOtherTargets -- forget every previously
	// remembered object before this effect resolves, so a source that
	// remembered something earlier (a re-entered O-Ring exiling a second
	// creature) remembers only its own targets and the return trigger
	// returns exactly this effect's set. Both halves clear: the resolution's
	// ctx list and the source's event-backed persistent one.
	if cz.ForgetOtherTargets {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// Imprint effects such as Chrome Mox select eligible cards from their
	// controller's hand. Keep them out of the generic hand mover so their
	// successful exile can be recorded in the replayable Imprint event.
	if len(targets) == 1 && targets[0].Obj == c.Source && !targets[0].IsPlayer &&
		len(originZones) == 1 && originZones[0] == state.ZHand && !originAll && cz.Imprint {
		targets = nil

		spec := cz.ChangeType
		for _, id := range h.Game().Zone(state.ZHand, c.Controller) {
			if o := h.Game().Obj(id); o != nil && MatchesSpecCtx(h.Game(), spec, id, c.SpecContext(c.Controller)) {
				targets = append(targets, state.Target{Obj: id})
			}
		}
		max := numText(h, c, cz.ChangeNum, 1)
		if int32(len(targets)) > max {
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: int(max), Max: int(max), Source: c.Source,
				ResumeKind: "imprint", ResumeSA: sa, Prompt: "Choose a card to imprint"}
			for _, target := range targets {
				o := h.Game().Obj(target.Obj)
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "imprint", Obj: target.Obj, Label: o.Face().Name})
			}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the "imprint"
				// re-entry's own events, then the answered cards are
				// the ones moved, exactly as that re-entry's answered Imprint
				// branch reads them.
				objectPathMoveEcho(h, c, cz, to)
				targets = targets[:0]
				for _, id := range tapeAnswerObjs(ans) {
					targets = append(targets, state.Target{Obj: id})
				}
			} else {
				targets = targets[:max]
			}
		}

	}
	var imprinted []state.ObjID
	// Forge keeps every DB$ Effect in an implicit "effect" object in the
	// Command zone, and the corpus's one-shot idiom `DB$ ChangeZone | Defined$
	// Self | Origin$ Command | Destination$ Exile` is that effect object
	// exiling itself -- ending the effect after one use (Deflecting Palm's
	// RPreventNextFromSource: "the NEXT time the chosen source would deal
	// damage"). This build has no effect object, so when the chain resolves
	// inside an Effect-created replacement's body (Ctx.EffectFrame is bound
	// by rules' seedEffectReplCtx) and the ChangeZone names that frame's own
	// source, the shape ends exactly that registration. Everywhere else the
	// ordinary move below runs unchanged (it moves nothing: the named source
	// is not in the Command zone), so no other resolution changes.
	if f := c.EffectFrame; f.Source != 0 && f.Source == c.Source && to == state.ZExile && !originAll &&
		len(originZones) == 1 && originZones[0] == state.ZCommand &&
		len(targets) == 1 && !targets[0].IsPlayer && targets[0].Obj == c.Source {
		if f.Stamp != 0 {
			h.EndEffect(f.Source, f.Stamp)
		} else {
			// A source-scoped frame (no per-registration stamp): the idiom ran
			// from an Effect's OWN body -- its Triggers$ body, or the chain of
			// the spell/ability that registered it -- so end every Effect-created
			// registration from that source. The ender's FromEffect marker keeps
			// the source's printed statics out of it.
			h.EndEffectSource(f.Source)
		}
		return
	}
	// The ImprintOnHost$ ender (task param:api:Effect.ImprintOnHost): the
	// same "exile the implicit effect object" idiom as the block above, but
	// keyed on the HOST's imprint instead of the effect's own self-exile.
	// Forge's DB$ Effect | ImprintOnHost$ True imprints the created effect
	// token on the host card and moves the token to the Command zone; the
	// corpus's `DB$ ChangeZone | Defined$ Imprinted | Origin$ Command |
	// Destination$ Exile` (Superior Foes of Spider-Man, Furious Rise,
	// Unstable Amulet, Word of Command, Semester's End -- 5 files) exiles
	// that token, ending the effect it carries ("you may play that card
	// until you exile another card with this creature" -- the second dig's
	// trigger exiles the FIRST effect's token before the new Effect
	// registers). This build has no effect-token object, so the marker
	// rides the registrations (state.ContinuousEffect.ImprintOnHost) and
	// the idiom ends exactly those through Host.EndImprintedEffects. The
	// ordinary move below still runs: the source's real imprinted cards
	// (Chrome Mox's) are never in the Command zone in this build, so the
	// Origin$ precondition skips them exactly as it did before.
	if to == state.ZExile && !originAll && len(originZones) == 1 &&
		originZones[0] == state.ZCommand &&
		cz.DefinedImprinted {
		h.EndImprintedEffects(c.Source)
	}
	// The objects the move loop actually moved, in move order: ChangeZone's
	// AtEOT$ affected set is the MOVED objects (some carriers carry
	// RememberChanged$ and some do not, so the moved set is collected here
	// rather than read back out of Remembered).
	var moved []state.ObjID
	// The Attacking$ entry rider is classified ONCE for the whole call, before
	// the mover loop: its degrades are one Note per ChangeZone, not one per
	// moved object.
	rider := classifyAttackingEntryText(c, cz.Riders.Attacking, to)
	for _, t := range targets {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		// Origin$, when given, is a precondition: the object must actually be
		// where the script expects, or the movement does not happen. This is
		// also this build's only CR 608.2b guard for ChangeZone: a target
		// moved away by an earlier effect in the same resolution, or by a
		// response that has already resolved, is simply skipped rather than
		// moved a second time or moved from the wrong zone.
		if cz.OriginPresent && !originAll && !cz.OriginMask.Has(o.Zone) {
			continue
		}
		// A CantExile restriction (The Master, Multiplied: "Triggered abilities
		// you control can't cause you to ... exile creature tokens you
		// control") withholds the object from this exile entirely: it never
		// leaves the battlefield, no MoveZone is emitted and none of the
		// inlined riders (exiled-with, RememberChanged, exile-return) run. The
		// shared settle path (settleChangeZoneMoveAs) carries the same guard
		// for every other ChangeZone mover.
		if to == state.ZExile && h.ExileBlocked(o.ID, false) {
			continue
		}
		// Inlined rather than routed through settleChangeZoneMove: this loop
		// carries the exiled-with association and the RememberChanged$
		// event-backed rider (eventRemember) in a specific order (MoveZone,
		// exiled-with, RememberChanged, WithCounters) that predates the
		// shared settle helper, and neither is shared with that helper's
		// other callers (see settleChangeZoneMoveAs's doc comment). The
		// MoveZone event itself still goes through moveZoneEvent (every exile
		// mover shares that one constructor) plus the same Imprint$True/
		// ExiledWithSource-static IDs augmentation settleChangeZoneMoveAs
		// applies, so events.Apply's ExiledWith-scalar derivation (o.ExiledWith
		// = e.IDs[0]) fires here exactly as it does on that path -- Chrome
		// Mox's own DefinedCards$ ExiledWith read needs it, not just the
		// distinct ExiledCards list exiledWithAssociation below maintains.
		ev := moveZoneEvent(c, o.ID, o.Zone, to)
		// Capture the LKI before the emit: events.Apply's Move fold resets a
		// battlefield departure's controller to its owner (CR 400.7), so this
		// is the last point the pre-move controller is readable.
		if cz.RememberLKI {
			c.ChangeZoneLKI = append(c.ChangeZoneLKI, state.LKIObject{Obj: o.ID, Controller: o.Controller, Owner: o.Owner})
		}
		if to == state.ZExile && len(ev.IDs) == 0 && (faceStaticsNameExiledWithSource(h, c.Source) || cz.Imprint) {
			ev.IDs = []state.ObjID{c.Source}
		}
		applyMoveFaceDown(h, c, &cz.Riders.FaceDownRiders, &ev, to)
		fromZone := o.Zone
		// A Transformed$ True entry flips to the back face BEFORE the MoveZone
		// is folded, so events.Apply's Move grants CR 306.5b loyalty for the
		// face the permanent enters with. See applyTransformed.
		if to == state.ZBattlefield {
			applyTransformed(h, c, cz.Riders.Transformed, o.ID)
		}
		markChangeZoneAttach(h, c, sa, cz, &ev)
		h.Emit(ev)
		moved = append(moved, o.ID)
		exiledWithAssociation(h, c, o.ID, to)
		if to == state.ZExile {
			recordExileReturnFor(h, c, cz.Riders.Duration, o.ID, fromZone, to)
		}
		// RememberLKI$ True (Reanimate's "creature card" whose mana value the
		// chained lose-life SVar reads, RememberedLKI$CardManaCost) joins the
		// moved object to the ability's Remembered -- a resolution-local Ctx
		// value, replayed identically because replay re-runs the same SA. The
		// two flags stack; an object is not remembered twice.
		if cz.RememberLKI && !cz.RememberChanged {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
		}
		if cz.RememberChanged {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			eventRemember(h, c, o.ID)
		}
		// RememberTargets$ True (Journey to Nowhere's exile trigger, Bile
		// Blight's Pump sibling): the CHOSEN TARGETS join the ability's
		// Remembered, in both halves -- the ctx list the chain's later
		// sub-abilities read (Bile Blight's PumpAll Remembered.sameName) and
		// the source's event-backed persistent list, which a LATER, separate
		// resolution reads through Defined$ Remembered via the O-Ring rescue
		// above (Journey's leave-battlefield return trigger). Only a target
		// the move actually moved is remembered: a target skipped by the
		// Origin$ precondition was never exiled and must never come back.
		if cz.RememberTargets {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			eventRemember(h, c, o.ID)
		}
		eventForgetChanged(h, c, sa, o.ID)
		if withKind != "" && counterDestination(to) {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: withKind, Amount: withAmt})
		}
		// GainControl$ hands the moved object to the named player (Reanimate:
		// "return target creature card... to the battlefield under your
		// control"). Only a battlefield entry can carry a control change (CR
		// 701.22a controls permanents); a card moved to a hidden or public
		// non-battlefield zone keeps its owner. Not part of the "inlined
		// rather than settleChangeZoneMove" scoping above -- GainControl$ is
		// unconditional on the move landing on the battlefield, the same as
		// settleChangeZoneMoveAs's own tail call.
		if to == state.ZBattlefield {
			applyGainControlFor(h, c, cz.Riders.GainControl, o.ID)
			changeZoneAttachedTo(h, c, sa, cz, o.ID)
		}
		// Tapped$ True (CR 110.5's entry state): the moved permanent enters
		// tapped. The object path did not apply this rider before, so a
		// targeted graveyard/exile return carrying it (Zuko's Conviction's
		// kicked alternate, every "return it to the battlefield tapped"
		// spell) entered untapped -- the same Tap event applyLibrarySearch
		// and the hand movers emit, so the entry state is a real event and
		// replay derives it.
		if to == state.ZBattlefield && cz.Tapped {
			h.Emit(events.Event{Kind: events.Tap, Obj: o.ID, Player: c.Controller, Text: "entered tapped"})
		}
		rider.apply(h, c, o.ID, c.Controller, to)
		// StaticEffect$ on the inlined object path: the same rider the shared
		// settle path applies for every other mover (the main loop deliberately
		// predates settleChangeZoneMoveAs and is not routed through it).
		if to == state.ZBattlefield {
			applyStaticEffect(h, c, sa, to, []state.ObjID{o.ID})
			// LeaveBattlefield$ Exile on the inlined object path (Isareth the
			// Awakener, From the Catacombs): the same promise the shared settle
			// path registers, on the object this move just landed, after its
			// entry riders are settled.
			registerLeaveExile(h, c, o.ID, cz.LeaveBattlefield, "", true)
		}
		if cz.Imprint && (to == state.ZExile || cz.ImprintLast) {
			if landed := h.Game().Obj(o.ID); landed != nil && landed.Zone == to &&
				(to == state.ZExile || !landed.IsToken) {
				imprinted = append(imprinted, o.ID)
			}
		}
	}
	if len(imprinted) > 0 {
		if cz.ImprintLast {
			clearChangeZoneImprint(h, c)
			imprinted = imprinted[len(imprinted)-1:]
		}
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: imprinted})
	}
	// The mixed-Hand diagnostic, now that the pass's truth is known: every
	// no-selector/player-selector mixed origin was answered by the search
	// path's chooser above, and a Defined$ naming its objects moves them
	// through the Origin$-preconditioned loop -- so only a mixed-Hand
	// resolution that moved nothing AND posed no pending target ask is left
	// loud (a suspended ask emits on its answering re-entry, which moves).
	if mixedOriginNoteFrom != "" && len(moved) == 0 && !targetAskPending {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "no object of the mixed ChangeZone Origin$ " + mixedOriginNoteFrom +
				" resolution was eligible to move"})
	}
	// AtEOT$ (Puppeteer Clique's reanimation: "at the beginning of your next
	// end step, exile it"): schedule the end-step departure for every object
	// this move actually moved. Scheduled BEFORE the library shuffle tail:
	// a ShuffleNonMandatory$ confirm suspension returns out of the tail, and
	// the re-entry's early-return branch (c.SearchShuffle above) would never
	// reach a schedule call placed after it -- the same order
	// applyLibrarySearch uses for its own hidden-origin tail.
	scheduleAtEOT(h, c, sa, moved)
	// Object-path library shuffle tail (searchmay1): a ChangeZone that moved
	// objects INTO a library and states Shuffle$ True now shuffles. This is
	// the tail the AGENTS.md row named as "the object-path shuffle": the
	// path previously shuffled nothing at all. Four corpus lines also set
	// ShuffleNonMandatory$; three SP-parented DB subs (Cathartic Parting,
	// Devious Cover-Up, Put Away) still inherit the parent's targets and
	// cannot reach this tail until sub-ability targeting is separated. The 76
	// mandatory carriers (Turn the Earth, Quandrix Command, Rite of Renewal,
	// Stream of Consciousness, the death-trigger "shuffle CARDNAME into its
	// owner's library" family) shuffled nothing either and now shuffle. Only
	// the explicit Shuffle$ True is read: the corpus's LibraryPosition$
	// "put it on top" movers state no Shuffle$ and must NOT shuffle. Each
	// distinct card owner's library is shuffled once (a cross-graveyard mover
	// like Turn the Earth touches several players), in first-move order so
	// the event stream stays deterministic.
	// LibraryPosition$ placement tail on the object-target path
	// (golgari_thug1): every targeted mover -- a ValidTgts$ ask, a
	// changeZoneChosenTargets answer, or a Defined$ that names concrete
	// objects -- used to stop after the MoveZone loop, so a targeted
	// ChangeZone INTO a library that names a position never reached the
	// placement helpers the hidden-origin movers share. The MoveZone bottom
	// append stood: Golgari Thug's "put target creature card from your
	// graveyard on top of your library" (LibraryPosition$ 0) left the card at
	// the BOTTOM. Measured at the corpus pin, 140 corpus lines carry the
	// class (targeted ChangeZone, Destination$ Library, an explicit
	// LibraryPosition$): 70 top, 51 bottom, 12 second-from-top, 3+1 deeper,
	// 3 SVar-resolved X. The placement runs BEFORE the Shuffle$ tail (the
	// ChangeZoneAll order: "put on top ..., then shuffle") and is skipped
	// when AlternativeDecider$ engaged -- that branch places below through
	// its own primary/alternative election.
	if to == state.ZLibrary && len(moved) > 0 && !altEngaged {
		placeTargetedLibraryObjects(h, c, cz, moved)
	}
	if to == state.ZLibrary && len(moved) > 0 && objectPathShuffleOwed(cz) {
		if objectPathShuffleTail(h, c, sa, cz, moved) {
			return
		}
	}
	if altEngaged && len(moved) > 0 {
		position := int32(0)
		if altBottom {
			position = -1
		} else if cz.LibraryPositionText == "1" {
			position = 1
		}
		libraryOrderPlacementAt(h, h.Game().Obj(moved[0]).Owner, moved, position)
	}
}

// changeZoneChosenTargets serves effChangeZone's object path the targets of a
// ValidTgts$-declared targeting when no ask has offered them yet. The ok
// return is NOT "targets were found" -- it is "use the returned set INSTEAD of
// Defined's own fallthrough": ok=true with a nil set means the ask was posed
// and SUSPENDED the resolution (the caller must return before moving
// anything), and the answered re-entry consumes Ctx.Choice here. Every other
// shape returns false and the caller keeps Defined's own behaviour
// (placement-chosen targets, Defined$-named objects, the source default).
//
// The ask never fires when the resolution already carries targets (the
// placement ask's answered set) or when the SA also carries Defined$ (an
// already-named fetch list is Forge's no-ask shape). Bounds come from
// TargetMin$/TargetMax$ through the ordinary Num grammar, clamped to the
// eligible count; Min == Max == 0 or an empty eligible set is no ask and no
// move. A host that cannot ask takes the deterministic first-max stand-in
// (R-9), which is exactly what botpolicy's clamp fallback answers with.
func changeZoneChosenTargets(h Host, c *Ctx, sa *cards.SA) ([]state.Target, bool) {
	t := compileChangeZoneTargeting(TargetsOf(sa), DefinedOf(sa))
	ts, ok, _ := changeZoneChosenTargetsFor(h, c, sa, &t)
	return ts, ok
}

// changeZoneChosenTargetsFor is changeZoneChosenTargets over sa's compiled
// targeting half (effChangeZone passes its compiled record's). served
// reports the resolution kernel's answer to the ask (poseTargetsAsk).
func changeZoneChosenTargetsFor(h Host, c *Ctx, sa *cards.SA, cz *changeZoneTargeting) (ts []state.Target, ok bool, served bool) {
	if !cz.ValidTgts.Present || cz.Defined != "" {
		return nil, false, false
	}
	if c.SubPreAsk != nil {
		if ts, ok := c.SubPreAsk[sa.Line]; ok {
			return ts, true, false
		}
	}
	if c.TargetsOffered && (c.OfferedSA == nil || sa.Line == c.OfferedSA.Line) {
		// The announcement/placement ask offered THIS SA's targeting (rules
		// sets the marker exactly for the SA the ask covered, and OfferedSA
		// names it); the chosen-zero election must not be re-asked here. A
		// deeper sub's own targeting was never offered -- the same
		// mvts1 boundary chosenTargetsFor's OfferedSA check draws -- so it
		// falls through to its own ask below.
		return nil, false, false
	}

	if len(c.Targets) > 0 {
		// Inherit ONLY when the targets genuinely belong to THIS SA -- the
		// OfferedSA marker names exactly the SA the placement/announcement ask
		// covered (task spcz1; previously every sub that did not declare
		// TargetUnique$ True inherited, so a targeted root's SubAbility$
		// ChangeZone read the PARENT's targets through Defined's ValidTgts$
		// fallthrough and its own Origin$ filter rejected them into a silent
		// no-op: Cathartic Parting's and Put Away's graveyard "may shuffle"
		// clause never asked). A sub that DOES mean to reuse the parent's
		// target says so with TargetUnique$ True (Withdraw): the shared ask's
		// filter excludes the inherited parent target via TargetsAlreadyChosen,
		// so it asks for ANOTHER target instead of inheriting blindly.
		if c.OfferedSA != nil && sa.Line == c.OfferedSA.Line {
			return nil, false, false
		}
	}
	// Legality stays referenced to the ability controller; only the
	// decision's Player moves to the TargetingPlayer$ chooser (the same
	// resolver every rules-tier target ask uses).
	candidates := subAskCandidates(h, c, sa)
	chooser := h.ChooserFor(c, sa)
	if ch, posed := opponentPick(h, c, sa, chooser); posed {
		// The controller's which-opponent selection ask was posted: the walk
		// is suspended and re-enters this very SA, where the answered
		// selection makes ChooserFor return the chosen seat.
		return nil, true, false
	} else if !posed {
		chooser = ch
	}
	min := numText(h, c, cz.TargetMin, 1)
	max := numText(h, c, cz.TargetMax, 1)
	if max > int32(len(candidates)) {
		max = int32(len(candidates))
	}
	if min > max {
		min = max
	}
	if min < 0 {
		min = 0
	}
	if max <= 0 {
		// Nothing eligible (or an explicitly zero bound): no ask, no move.
		ts, ok := noSubTargets(c, sa)
		return ts, ok, false
	}
	return poseTargetsAsk(h, c, sa, chooser, candidates, min, max, "choice")
}

// markChangeZoneAttach marks a ChangeZone battlefield entry of an Aura with
// its AttachedTo$/AttachedToPlayer$ named bearers, resolved BEFORE the move
// (events.MarkNamedAttachEntry). The engine's entry gate then settles the
// Aura: it enters attached to the first named bearer it can legally enchant,
// or stays in its zone when none is (CR 303.4f/g, rules/aura_entry.go), and
// changeZoneAttachedTo, which runs after the move, finds it already attached
// (or not on the battlefield) and leaves it. Anything else (an Equipment, a
// face-down entry) is not marked and keeps the post-move rider. Every
// ChangeZone mover that follows its move with changeZoneAttachedTo calls this
// first; rules/aura_entry_census_test.go pins the pairing.
func markChangeZoneAttach(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, ev *events.Event) {
	if ev.To != state.ZBattlefield || (cz.AttachedTo == "" && cz.AttachedToPlayer == "") ||
		ev.Counter != "" {
		return
	}
	if o := h.Game().Obj(ev.Obj); o == nil || !hasType(o, "Aura") {
		return
	}
	targets := changeZoneAttachTargets(h, c, sa, cz)
	named := make([]state.ObjID, 0, len(targets))
	for _, t := range targets {
		if t.IsPlayer {
			named = append(named, state.PlayerRef(t.Player))
		} else {
			named = append(named, t.Obj)
		}
	}
	events.MarkNamedAttachEntry(ev, named)
}

// changeZoneAttachTargets resolves the named bearers in order: the living
// seats AttachedToPlayer$ names, else the objects AttachedTo$ names (the
// same Defined$ resolution, card-filter fallback included, the post-move
// rider below uses).
func changeZoneAttachTargets(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams) []state.Target {
	var out []state.Target
	if cz.AttachedToPlayer != "" {
		for _, t := range DefinedSpec(h, c, cz.AttachedToPlayer) {
			if t.IsPlayer && int(t.Player) < len(h.Game().Players) && !h.Game().Players[t.Player].Lost {
				out = append(out, t)
			}
		}
		return out
	}
	for _, t := range DefinedSpec(h, c, changeZoneAttachSelector(h, c, cz.AttachedTo)) {
		if !t.IsPlayer && t.Obj != 0 && h.Game().Obj(t.Obj) != nil {
			out = append(out, t)
		}
	}
	return out
}

// changeZoneAttachSelector is AttachedTo$'s Defined$ spelling: a bare card
// filter the Defined$ grammar cannot classify resolves as a battlefield
// filter ("Valid <filter>"); see changeZoneAttachedTo.
func changeZoneAttachSelector(h Host, c *Ctx, val string) string {
	if val != "Valid" && !strings.HasPrefix(val, "Valid ") {
		if _, ok := knownDefinedTargets(h, c, val); !ok {
			return "Valid " + val
		}
	}
	return val
}

// changeZoneAttachedTo implements ChangeZone's AttachedTo$ param: "the moved
// card enters the battlefield attached to the resolved target" (Forum
// Filibuster's `AttachedTo$ DelayTriggerRememberedLKI` -- attach the returned
// Aura to the remembered token; the 42 raw corpus ChangeZone lines carrying
// the param: Self x15 is the dominant form, "return an Aura ... attached to
// CARDNAME"). The value is a Defined$-grammar selector, resolved with the
// ordinary resolver against a shallow SA that carries it in Defined$ (the
// same shape effToken's own AttachedTo$ rider takes), so every corpus
// spelling (Self, ParentTarget, TriggeredCardLKICopy, ChosenCard,
// DelayTriggerRememberedLKI, a card filter, ...) resolves without a second
// resolver. The Attach event is the same shape that rider emits: Obj is the
// MOVED card (it takes the AttachedTo back-reference), IDs[0] the target it
// attaches to. A value that resolves to no object -- a selector this grammar
// cannot evaluate, or a target that left play -- is ONE loud Note and the
// card enters unattached (an Aura's unattached state), never a guessed
// target and never a silent skip. Battlefield destinations only: the param
// on a move that does not enter the battlefield has no CR meaning (nothing
// can be attached in a hidden zone) and is left unread.
func changeZoneAttachedTo(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, moved state.ObjID) {
	val := cz.AttachedTo
	playerVal := cz.AttachedToPlayer
	if moved == 0 || (val == "" && playerVal == "") {
		return
	}
	if o := h.Game().Obj(moved); o == nil || o.Zone != state.ZBattlefield ||
		o.AttachedTo != 0 || o.HasAttachedPlayer {
		// Already attached as it entered (the engine settled a marked
		// non-cast Aura, markChangeZoneAttach), or never entered.
		return
	}
	if playerVal != "" {
		changeZoneAttachedToPlayer(h, c, sa, moved, playerVal)
		return
	}
	// A bare card-filter spelling ("Creature" -- Retether's mass return;
	// "Creature.YouCtrl" -- One Last Job, Storm Herald, Nomad Mythmaker;
	// "Creature.sharesCreatureTypeWith <ref>" -- Runed Crown) is not a
	// Defined$ referent (definedSpec has no case for it) and MUST NOT ride
	// Defined's source fallback: that would fasten the moved Aura to the
	// resolving spell/ability itself, an attach the CR 704.5m SBA then
	// sweeps the moment the source leaves play. When knownDefinedTargets
	// cannot classify the value, resolve it as a battlefield card filter --
	// the same walk the Valid-prefixed branch runs -- so a spelling this
	// grammar cannot evaluate fails closed to the loud Note below, never to
	// a guessed attach (changeZoneAttachSelector).
	var to state.ObjID
	for _, t := range DefinedSpec(h, c, changeZoneAttachSelector(h, c, val)) {
		if !t.IsPlayer {
			to = t.Obj
			break
		}
	}
	if to == 0 || h.Game().Obj(to) == nil {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZone AttachedTo$ " + val + " resolved to nothing; the card enters unattached"})
		return
	}
	emitAttach(h, moved, to)
}

// changeZoneAttachedToPlayer implements ChangeZone's AttachedToPlayer$ param:
// "the moved card enters the battlefield attached to the resolved PLAYER"
// (Lynde, Cheerful Tormentor's `AttachedToPlayer$ You`, Curse of Misfortunes'
// `EnchantedPlayer`, Bitterheart Witch's `Targeted`, the two Trandformed$
// Curses' `ParentTarget`). It is the player-destination twin of
// changeZoneAttachedTo above; state.Object cannot carry both a permanent and
// a player bearer, and events.Attach's player branch is what folds the seat
// into AttachedPlayer/HasAttachedPlayer. The value is resolved with the
// ordinary resolver against a shallow SA carrying it in Defined$ (the same
// shape the object twin takes and definedSpec already supports: You,
// Targeted, ParentTarget, EnchantedPlayer), and the FIRST living player it
// names is the bearer. A value that resolves to no living seat -- an
// unmodelled selector, or a target that left -- is ONE loud Note and the card
// enters unattached, never a guessed seat. Battlefield destinations only (the
// caller gates on that); nothing can be attached in a hidden zone.
func changeZoneAttachedToPlayer(h Host, c *Ctx, sa *cards.SA, moved state.ObjID, val string) {
	var seat state.PlayerID
	found := false
	for _, t := range DefinedSpec(h, c, val) {
		if t.IsPlayer && int(t.Player) < len(h.Game().Players) && !h.Game().Players[t.Player].Lost {
			seat, found = t.Player, true
			break
		}
	}
	if !found {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZone AttachedToPlayer$ " + val + " resolved to nothing; the card enters unattached"})
		return
	}
	// The raw player-attach event, exactly the shape effAttach's own
	// Enchant:Player branch emits: Obj is the MOVED card, Player the seat.
	// emitAttach is deliberately NOT reused -- its Unattached detach half
	// reads o.AttachedTo (the permanent link) only, and the Attach fold's
	// player branch already clears AttachedTo, so a re-attach from one
	// player to another or from a permanent to a player is handled by the
	// one event (CR 701.3b: the new attachment supersedes the old).
	h.Emit(events.Event{Kind: events.Attach, Obj: moved, Player: seat, Text: "attach to player"})
}

// applyFaceDownMarker stamps a just-built ChangeZone MoveZone with the
// face-down encoding the card text asks for. Two spellings reach it, and they
// mean different CR things:
//
//   - ExileFaceDown$ True (Necropotence's "exile the top card of your library
//     face down"): the "exiled_with_face_down" decode sets Object.FaceDown AND
//     records the exiling source as the ExiledWith association -- the same
//     encoding Hideaway's face-down exile uses. The IDs provenance payload is
//     cleared so the two carriers cannot disagree on one event.
//
//   - FaceDown$ True (Yedora, Grave Gardener; the manifest marker's own
//     spelling) on a battlefield entry: the "entered_face_down" decode folds
//     Object.FaceDown plus the optional FaceDownSetType$/FaceDownPower$/
//     FaceDownToughness$ payload the card text names, exactly as a Manifest
//     does. A hand/library-origin face-down entry is marked Secret (its face
//     would otherwise leak through the transcript), matching the Manifest
//     precedent; a graveyard-origin one stays public (CR 708.9 already
//     revealed it on leaving the battlefield).
//
//   - FaceDown$ True on an exile destination (Tezzeret's Reckoning): the card
//     is put into exile face down WITHOUT an ExiledWith association, so the
//     bare spelling uses its own "face_down" marker rather than borrowing
//     ExileFaceDown$'s source-carrying one.
//
//   - WithMayLook$ True with an exile face-down destination (Ixhel, Scion of
//     Atraxa; Gonti; Thief of Sanity): the exiling effect's controller may
//     look at the exiled card's face for as long as it remains exiled, even
//     though the card's owner may NOT. It rides the same MoveZone as
//     ExileFaceDown$, but with the looker in Amount and the exiling source in
//     IDs (the marker's own layout, decoded under the
//     "exiled_with_face_down_maylook" Counter value), so replay folds both
//     the look permission and the ExiledWith provenance with no new event kind
//     and no Event field change. The pair composes with Foretold$ (a distinct
//     marker) because Forge keeps the two designations independent; measured
//     at the current corpus pin no card carries both, but the decode keeps
//     them separable rather than silently dropping one.
//
// It is called from every ChangeZone mover (the object path, the shared
// settle helper the hand/library routes use, and applyLibrarySearch), so the
// read composes with each without a second caller-side branch. The Dig
// mover (effects/cardflow.go effDig) calls it too, so the same WithMayLook$
// read serves both APIs from this one choke point.
//
// Unearth$ True (cards/kw_unearth.go's K:Unearth expansion) also lands on
// the battlefield entry here: it stamps the "entered_unearthed" counter so
// rules' entry hook (rules/unearth.go, reached from checkTriggers) can tell
// an unearth return from every other battlefield entry and apply CR
// 702.84a's haste grant and end-step exile promise. The counter is not a
// face-down marker (IsFaceDownEntry returns false for it), so the two
// encodings never collide; no corpus line combines Unearth with
// FaceDown$/ExileFaceDown$.
func applyFaceDownMarker(h Host, sa *cards.SA, c *Ctx, ev *events.Event, to state.Zone) {
	r := compileFaceDownRiders(sa)
	applyMoveFaceDown(h, c, &r, ev, to)
}

// applyMoveFaceDown is applyFaceDownMarker over compiled riders: ChangeZone
// passes its compiled record's, the sibling APIs a stack compile.
func applyMoveFaceDown(h Host, c *Ctx, r *FaceDownRiders, ev *events.Event, to state.Zone) {
	faceDown := r.FaceDown
	exileFaceDown := r.ExileFaceDown
	withMayLook := r.WithMayLook
	foretold := r.Foretold
	if to == state.ZBattlefield && r.Unearth {
		ev.Counter = events.UnearthEntryCounter
	}
	switch {
	case to == state.ZExile && exileFaceDown && withMayLook:
		// The may-look layout: Amount carries the looker (the exiling
		// effect's controller), IDs carries the exiling source. The looker
		// is authoritative, so the card's owner is NOT admitted by the
		// view's default controller check.
		ev.Counter = "exiled_with_face_down_maylook"
		if foretold {
			ev.Counter = "exiled_with_face_down_maylook_foretold"
		}
		ev.Amount = int32(c.Controller)
		ev.IDs = []state.ObjID{c.Source}
	case to == state.ZExile && exileFaceDown:
		if foretold {
			ev.Counter = "exiled_with_face_down_foretold"
		} else {
			ev.Counter = "exiled_with_face_down"
		}
		ev.Amount = int32(c.Source)
		ev.IDs = nil
	case to == state.ZExile && faceDown:
		ev.Counter = "face_down"
		ev.Amount = 0
		ev.IDs = nil
	case to == state.ZBattlefield && faceDown:
		setType := r.FaceDownSetType
		power, hasPower := numResolvedText(h, c, r.FaceDownPower, 0)
		toughness, hasTough := numResolvedText(h, c, r.FaceDownToughness, 0)
		hasPT := hasPower || hasTough
		ev.Counter = events.FaceDownEntryCounterFor(setType, power, toughness, hasPT)
		if ev.From == state.ZHand || ev.From == state.ZLibrary {
			ev.Secret = true
		}
	}
}

// settleChangeZoneMove is the one settle path every ChangeZone mover shares:
// the MoveZone itself, then RememberChanged$ (the moved object joins the
// ability's Remembered -- a DelayedTrigger running as a later SubAbility of
// the same chain captures it, and the value is a parameter of the ongoing
// resolution (Ctx), not game state, so mutating it here is fine), then the
// WithCountersType$/WithCountersAmount$ entry counters when the move lands on
// a counter-bearing destination (battlefield or exile -- counterDestination).
// Keeping the object path and the hand-choice path on this one helper means
// the two cannot drift apart on any of the three.
func settleChangeZoneMove(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, id state.ObjID, from, to state.Zone, withKind string, withAmt int32, rider *attackingEntry) {
	settleChangeZoneMoveAs(h, c, sa, cz, id, from, to, withKind, withAmt, 0, false, rider)
}

// settleChangeZoneMoveAs is the one settle path every ChangeZone mover shares
// (settleChangeZoneMove is its event-Player-unset form), plus the explicit
// event-Player form: a hidden-zone move of ANOTHER player's card carries that
// player as the event's Player -- the same attribution the library search's
// move applies -- so the view layer's hidden-card redaction sees the move the
// way the owner does. The MoveZone itself, then RememberChanged$ (the moved
// object joins the ability's Remembered -- a DelayedTrigger running as a
// later SubAbility of the same chain captures it, and the value is a
// parameter of the ongoing resolution (Ctx), not game state, so mutating it
// here is fine), then the WithCountersType$/WithCountersAmount$ entry
// counters when the move lands on a counter-bearing destination (battlefield
// or exile -- counterDestination). Keeping the object path and the hand-choice
// path on this one helper means the two cannot drift apart on any of the
// three. Tapped$ True is event-backed for EVERY shape through this path:
// this settle's own tail emits the hand-origin entry Tap (the object path,
// the library search's library-origin branch and the Dig windows carry their
// own), so no card this helper moves onto the battlefield silently enters
// untapped.
//
// The exiled-with association and the RememberChanged$ event-backed rider
// (eventRemember) are NOT done here: they are scoped to the two ORIGINAL
// ChangeZone movers that carried them before this helper existed (the
// object-target loop in effChangeZone and applyLibrarySearch's hidden-search
// mover), not to every caller of this now-shared settle path -- widening
// their scope here would move acceptance-game replay hashes beyond the
// reviewed change.

// gainControlOf resolves a ChangeZone SA's GainControl$ parameter (Reanimate's
// "onto the battlefield under your control", Control Magic-family Steal
// effects' "under your control") and returns the player the moved object must
// come under the control of. Forge's ChangeZoneEffect names the gain target
// in that one parameter: "True" and "You" both mean the resolving
// controller (the corpus's 287 True lines and 43 You lines); every other
// value is a player selector resolved through the shared Defined grammar
// (ChosenPlayer, Targeted, Player.IsRemembered, ParentTarget, ...), taking
// the first resolved player deterministically. The third return reports
// whether the parameter is PRESENT at all; the second whether the value
// resolved. An unresolvable value (a spec whose resolution names no player,
// or one the Defined grammar does not know) is false and the caller is loud
// rather than silently keeping the owner -- the same fail-closed convention
// every unread parameter here follows.
func gainControlOf(h Host, c *Ctx, gc ParamText) (state.PlayerID, bool, bool) {
	raw, present := gc.Text, gc.Present
	if !present || strings.TrimSpace(raw) == "" {
		return 0, false, true
	}
	if strings.EqualFold(raw, "True") || strings.EqualFold(raw, "You") {
		return c.Controller, true, true
	}
	targets, known := definedSpec(h, c, raw)
	if !known {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unrecognised ChangeZone GainControl$ " + raw})
		return 0, false, true
	}
	for _, t := range targets {
		if t.IsPlayer {
			return t.Player, true, true
		}
		if o := h.Game().Obj(t.Obj); o != nil {
			return o.Controller, true, true
		}
	}
	// The selector resolved (its grammar is known) but named no living
	// player: nothing to hand control to, and no silent owner-keep either.
	return 0, false, true
}

// applyGainControlFor is applyGainControl over a compiled GainControl$.
func applyGainControlFor(h Host, c *Ctx, gc ParamText, id state.ObjID) {
	p, ok, present := gainControlOf(h, c, gc)
	if !present || !ok {
		return
	}
	if o := h.Game().Obj(id); o != nil && o.Controller != p {
		h.Emit(events.Event{Kind: events.ControlChange, Obj: id, Player: p,
			Text: "GainControl"})
	}
}

// applyTransformed implements ChangeZone's Transformed$ True: a double-faced
// card this effect moves to the battlefield enters TRANSFORMED (CR 711.10a:
// a transforming double-faced card enters with its back face up when an
// effect says so) — the Ojer Axonil death trigger's "return it to the
// battlefield tapped and transformed", the Kytheon/Kumano "return it
// transformed" returns. The flip is the one FlipFace event effSetState
// emits, to the face AFTER the one the card carries out of its zone.
//
// Callers emit this BEFORE the MoveZone, while the card is still in its
// origin zone, so events.Apply's Move sees the face the permanent actually
// enters with. That matters when the back face is a planeswalker: CR 306.5b
// grants loyalty counters on the ENTRY face, and Move reads o.Face(). Flipping
// after the move (the old order) granted nothing, so the walker entered at 0
// loyalty and rules/sba.go killed it. Flip-then-move is the same order the
// modal-land play path uses (rules/legal.go) and the order the CR 712.4d
// land-back test pins. A card with fewer than two faces is not a transform
// and is left alone.
func applyTransformed(h Host, c *Ctx, transformed bool, id state.ObjID) {
	if !transformed {
		return
	}
	o := h.Game().Obj(id)
	if o == nil || o.Card == nil || len(o.Card.Faces) < 2 {
		return
	}
	next := (int(o.FaceIdx) + 1) % len(o.Card.Faces)
	h.Emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: int32(next), Text: "Transformed"})
}

// attackingEntryKind is what a move body's `Attacking$` rider resolved to for
// one call: see attackingEntry.
type attackingEntryKind uint8

const (
	attackingEntryNone        attackingEntryKind = iota // no rider, or a non-battlefield destination
	attackingEntryAttacks                               // literal True with a bound trigger defender
	attackingEntryNoDefender                            // literal True, but no defender in context
	attackingEntryUnsupported                           // a selector form (Remembered, TriggeredDefender, ...)
)

// attackingEntry is ONE move call's classification of its `Attacking$` entry
// rider (Alesha's "tapped and attacking" graveyard return, Preeminent
// Captain's Soldier, the Dig "onto the battlefield attacking" family).
//
// The rider is a property of the EFFECT, not of each card the effect moves, so
// every mover classifies it ONCE before its loop and the loop then applies only
// the derived entry state. That is what keeps the degrades honest: a call with
// no defending player in context, or one carrying a selector form this build
// does not model, emits exactly ONE deterministic loud Note for the whole call
// -- never one per moved object, which is what a multi-object ChangeZone or a
// Dig window would otherwise produce. It mirrors effects/token.go's
// TokenAttacking$ read, whose single-mint shape gets that for free.
type attackingEntry struct {
	kind     attackingEntryKind
	defender state.PlayerID
	// battle is the planeswalker or battle object the moved permanent
	// enters attacking (CR 702.49b's non-player defender), when the rider's
	// context carries one. It is appended to the TokenAttacks event's IDs so
	// the event fold sets Object.AttackingBattle. Zero for a player defender
	// and for every effect that does not bind one.
	battle state.ObjID
	note   string
	noted  bool
}

func classifyAttackingEntry(c *Ctx, sa *cards.SA, to state.Zone) attackingEntry {
	if to != state.ZBattlefield {
		return attackingEntry{}
	}
	return classifyAttackingEntryText(c, attackingParam(sa), to)
}

// classifyAttackingEntryText is classifyAttackingEntry over a compiled
// Attacking$ text.
func classifyAttackingEntryText(c *Ctx, attack string, to state.Zone) attackingEntry {
	if to != state.ZBattlefield {
		return attackingEntry{}
	}
	if attack == "" {
		return attackingEntry{}
	}
	if strings.EqualFold(attack, "True") {
		if c.DefendingPlayer.IsPlayer {
			return attackingEntry{kind: attackingEntryAttacks, defender: c.DefendingPlayer.Player, battle: c.DefendingBattle}
		}
		return attackingEntry{kind: attackingEntryNoDefender,
			note: "Attacking$ with no defending player in context; the permanent enters tapped but does not attack"}
	}
	return attackingEntry{kind: attackingEntryUnsupported,
		note: "Attacking$ " + attack + " is not implemented; the permanent enters but does not attack"}
}

// apply delivers the classified entry state for one moved object. A nil
// receiver is the "no rider" case every non-hoisting caller can pass.
func (a *attackingEntry) apply(h Host, c *Ctx, id state.ObjID, player state.PlayerID, to state.Zone) {
	if a == nil || a.kind == attackingEntryNone || to != state.ZBattlefield {
		return
	}
	if a.kind == attackingEntryAttacks {
		ids := []state.ObjID{state.ObjID(a.defender)}
		if a.battle != 0 {
			ids = append(ids, a.battle)
		}
		h.Emit(events.Event{Kind: events.TokenAttacks, Obj: id, Player: player,
			IDs: ids, Text: "entered attacking"})
		return
	}
	if a.kind == attackingEntryNoDefender {
		// An "enters attacking" object must enter TAPPED even when the
		// trigger context cannot identify a defender. Keep that entry state
		// while degrading only the attack assignment; callers that already
		// emitted their Tapped$ entry event do not get a duplicate Tap.
		if o := h.Game().Obj(id); o != nil && !o.Tapped {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: player, Text: "entered tapped"})
		}
	}
	if a.noted {
		return
	}
	a.noted = true
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: a.note})
}

func settleChangeZoneMoveAs(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, id state.ObjID, from, to state.Zone, withKind string, withAmt int32, player state.PlayerID, hasPlayer bool, rider *attackingEntry) {
	// A CantExile restriction (The Master, Multiplied) swallows the exile
	// before it happens: the object stays where it is, no MoveZone event is
	// emitted and none of this settle path's riders (exiled-with, exile-return,
	// imprint) run. This is the shared ChangeZone settle every mover below the
	// two inlined paths (effChangeZone's object loop and applyLibrarySearch's
	// library-origin move) funnels through, so a battlefield token can never be
	// taken by an exile that reached here instead.
	if to == state.ZExile && h.ExileBlocked(id, false) {
		return
	}
	ev := moveZoneEvent(c, id, from, to)
	if cz.RememberLKI {
		if o := h.Game().Obj(id); o != nil {
			c.ChangeZoneLKI = append(c.ChangeZoneLKI, state.LKIObject{Obj: id, Controller: o.Controller, Owner: o.Owner})
		}
	}
	if to == state.ZExile && len(ev.IDs) == 0 && (faceStaticsNameExiledWithSource(h, c.Source) || cz.Imprint) {
		// The S: static spelling of the same provenance need: a source whose
		// own Static lines name ExiledWithSource (Intellect Devourer's
		// MayPlay+ExiledWithSource grant) tracks its exiles exactly like the
		// SVar shapes exileProvenanceNeeded covers; Imprint$ True is the
		// Chrome Mox spelling, feeding the Defined.Imprinted reflected-mana
		// selector. Extra IDs on an exile move are inert for every consumer
		// that never reads them.
		ev.IDs = []state.ObjID{c.Source}
	}
	if hasPlayer {
		ev.Player = player
	}
	applyMoveFaceDown(h, c, &cz.Riders.FaceDownRiders, &ev, to)
	// A Transformed$ True entry flips to the back face BEFORE the MoveZone is
	// folded, so events.Apply's Move grants CR 306.5b loyalty for the face the
	// permanent enters with. See applyTransformed.
	if to == state.ZBattlefield {
		applyTransformed(h, c, cz.Riders.Transformed, id)
	}
	markChangeZoneAttach(h, c, sa, cz, &ev)
	h.Emit(ev)
	if to == state.ZExile {
		recordExileReturnFor(h, c, cz.Riders.Duration, id, from, to)
	}
	// Imprint$ True on the shared settle path (Dakra Mystic's DBPutRevealed:
	// `Defined$ Remembered | Origin$ Library | Destination$ Graveyard |
	// Imprint$ True`): Forge records every card a ChangeZone moved in the
	// source's persistent imprintedCards association, whatever the
	// destination, and a later `Defined$ Imprinted` sub reads it back
	// (Dakra's follow-up draw is gated `ConditionDefined$ Imprinted ...
	// EQ0`). The card joins the source's association through the ordinary
	// events.Imprint association, so replay folds it. Confirmed by the
	// object's post-move zone: a skipped candidate is never imprinted, and a
	// token never is. This is the one settle path every movement route
	// shares; the two inlined movers that predate it (the object-target loop
	// and applyLibrarySearch's library-origin branch) keep their own
	// collection and do not call through here, so nothing is recorded twice.
	if cz.Imprint && c.Source != 0 {
		if o := h.Game().Obj(id); o != nil && o.Zone == to && !o.IsToken {
			if cz.ImprintLast {
				clearChangeZoneImprint(h, c)
			}
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
		}
	}
	// RememberLKI$ True (the corpus's 77 ChangeZone lines -- Reanimate's
	// "creature card" whose mana value the chained lose-life SVar reads,
	// RememberedLKI$CardManaCost) joins the moved object to the ability's
	// Remembered. Same Ctx binding RememberChanged$ uses: a resolution-local
	// value, replayed identically because replay re-runs the same SA. The two
	// flags stack; an object is not remembered twice. RememberLKI$
	// Targeted (2 lines, a different capture point -- the CHOSEN target, not
	// the moved object) is left to its own work and is not silently folded
	// into this read.
	if cz.RememberLKI && !cz.RememberChanged {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
	if cz.RememberChanged {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
	if withKind != "" && counterDestination(to) {
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: withKind, Amount: withAmt})
	}
	// GainControl$ hands the moved object to the named player. Only a
	// battlefield entry can carry a control change (CR 701.22a controls
	// permanents); a card moved to a hidden or public non-battlefield zone
	// keeps its owner.
	if to == state.ZBattlefield {
		applyGainControlFor(h, c, cz.Riders.GainControl, id)
		// Tapped$ True (CR 110.5's entry state) for the hand-origin movers:
		// the same "entered tapped" Tap the object path, the library search's
		// library-origin branch and the Dig windows emit. Gated on the hand
		// origin because this helper's OTHER callers (the hidden pick, the
		// library search's alternative-origin branch) emit their own Tap after
		// the call and a second one here would double-emit.
		if from == state.ZHand && cz.Tapped {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: player, Text: "entered tapped"})
		}
		rider.apply(h, c, id, player, to)
		// StaticEffect$ <name> (the "return it ... It's a Spirit Detective"
		// rider): the named Continuous static registers onto the moved card
		// once its move and entry riders are settled. A no-op on every SA
		// without the parameter.
		applyStaticEffect(h, c, sa, to, []state.ObjID{id})
		// LeaveBattlefield$ Exile (Isareth the Awakener, From the Catacombs):
		// the promise rides the entered object for as long as it stays on the
		// battlefield (effects/leavebattlefield.go) -- no Duration$ on either
		// carrier, and the move sweep ends it on the departure itself.
		registerLeaveExile(h, c, id, cz.LeaveBattlefield, "", true)
	}
}

// exiledWithAssociation emits Forge's ChangeZoneEffect.handleExiledWith
// association for a non-token card this effect just exiled: the host's
// distinct exiledCards collection. It is deliberately NOT an ImprintCards$
// association: DefinedCards$ ExiledWith consumes this list, while
// ImprintedController only consumes explicit ImprintCards$ entries. Scoped to
// the object-target loop and applyLibrarySearch, the two movers that carried
// this association originally.
func exiledWithAssociation(h Host, c *Ctx, id state.ObjID, to state.Zone) {
	if to != state.ZExile || c.Source == 0 {
		return
	}
	if o := h.Game().Obj(id); o != nil && !o.IsToken {
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}, Text: "exiled-with"})
	}
}

// exileHostGoneFor is exileHostGone over a compiled Duration$.
func exileHostGoneFor(h Host, c *Ctx, dur ParamText) bool {
	if !strings.EqualFold(dur.Text, "UntilHostLeavesPlay") || c.Source == 0 {
		return false
	}
	o := h.Game().Obj(c.Source)
	return o == nil || o.Zone != state.ZBattlefield
}

// recordExileReturnFor is recordExileReturn over a compiled Duration$.
func recordExileReturnFor(h Host, c *Ctx, dur ParamText, id state.ObjID, from, to state.Zone) {
	raw, present := dur.Text, dur.Present
	if !present || strings.TrimSpace(raw) == "" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(raw), "UntilHostLeavesPlay") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unmodelled ChangeZone Duration$ " + strings.TrimSpace(raw)})
		return
	}
	if to != state.ZExile || c.Source == 0 {
		return
	}
	if o := h.Game().Obj(id); o == nil || o.Zone != state.ZExile || o.IsToken {
		return
	}
	h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id},
		Amount: int32(from), Text: "until-host-leaves"})
}
