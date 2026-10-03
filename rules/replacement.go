// replacement.go applies R: line replacement effects ahead of an event's
// own logging, from engine.go's emit. applyReplacements discovers every
// applicable effect in deterministic order, then applies the event-specific
// CR 616 ordering and optional-effect rules.

package rules

import (
	"fmt"

	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// applyReplacements is called from emit before the event is logged. The
// single M1 replacement event is "Moved" (R:Event$ Moved), which applies to
// a MoveZone event and honours Origin$, Destination$, ValidCard$ and
// ReplaceWith$.
//
// Matches resolve ReplaceWith$ inside applyingReplacement, so nested emits
// cannot recursively apply the same replacement forever. MoveZone,
// BeginPhase and ProduceMana competitions park the event for the affected
// player's CR 616 order choice; the event-specific handlers below describe
// when the chosen effect completes the event and when remaining effects are
// reconsidered.
//
// Task 29 (Ruling T26-a): what happens to the ORIGINAL event once a
// replacement matches depends on Forge's ReplacementResult$, which this used
// to ignore entirely -- every match was treated as ReplacementResult$
// Replaced, discarding the original event unconditionally. That is right for
// "Replaced" (and for no ReplacementResult$ at all: Task 22's four pins were
// measured against fixtures with neither, and must keep reading as
// Replaced), but wrong for ReplacementResult$ Updated -- Forge's idiom for
// "the event still happens, augmented" (CR 616.1's "an effect that modifies
// how an event occurs"), which is by far the dominant shape in the corpus:
// 838 of 842 ReplacementResult$-bearing R: lines say Updated, and 835 of
// those are exactly this "enters the battlefield tapped" pattern (Hallowed
// Fountain, Celestial Colonnade, Geralf's Messenger, ...). Treating Updated
// as a full replace discarded the permanent's own MoveZone onto the
// battlefield, so it never left the stack and resolveTop kept re-resolving
// the same object forever (see Task 26's report and the resolveTop guard
// below for the other half of this fix).
func (e *Engine) finalityReplacementApplies(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Counter("FINALITY") <= 0 {
		return false
	}
	for _, typ := range e.typeCharacteristics(id, 0) {
		if typ == "Creature" {
			return true
		}
	}
	return false
}

func (e *Engine) applyReplacements(ev events.Event) (events.Event, bool) {
	// A CantDraw static (CR 121.6: "If a player would draw a card and an
	// effect says that player can't, the draw does not happen") is a "can't"
	// replacement with no ReplaceWith$ body: the proposed Draw is swallowed
	// whole, so no Draw event is logged and no Draw replacement (dredge,
	// Notion Thief) may apply to it. Handled here, beside the positive-
	// LifeChange CantGainLife gate in applyLifeReplacements below, so the two
	// halves of a card like Mornsong Aria ("Players can't draw cards or gain
	// life") take the same shape -- one static read, one prevented Note.
	if ev.Kind == events.Draw && e.drawForbidden(ev.Player) {
		return e.emit(events.Event{Kind: events.Note, Player: ev.Player, Text: "prevented: cannot draw cards"}), true
	}
	// Positive LifeChange is a gain; it never carries a repl:DamageDone
	// match (that class names a Damage event only), so it routes straight
	// to the life replacement machinery.
	if ev.Kind == events.LifeChange {
		return e.applyLifeReplacements(ev)
	}
	if ev.Kind == events.Damage && ev.Obj == 0 {
		// Player-targeted damage is CR 616-eligible for TWO competing
		// classes: repl:DamageDone (Battletide Alchemist's own damage-
		// specific prevention) and the general repl:LifeReduced life-loss
		// machinery (CR 615's "the next time a player would lose life" —
		// damage-caused loss counts). DamageDone is the more specific class
		// and is tried first through the ordinary dispatch below; only when
		// nothing there applies (or a DamageDone ReplaceEffect body rewrote
		// the amount in place without fully replacing the event) does the
		// event fall through to applyLifeReplacements, which also
		// recognises damage-caused loss (lifeLoss/trigger_match.go). A card
		// with both classes active competing for the SAME event is not in
		// the corpus this build measures against; that composition is left
		// for whichever ticket first needs it.
		replaced, handled := e.applyReplacementsDispatch(ev)
		if handled {
			return replaced, true
		}
		return e.applyLifeReplacements(replaced)
	}
	return e.applyReplacementsDispatch(ev)
}

func (e *Engine) applyReplacementsDispatch(ev events.Event) (events.Event, bool) {
	if ev.Kind == events.Attach && e.attachedApplying {
		return ev, false
	}
	if ev.Kind == events.Attach {
		if e.applyAttachedReplacement(ev) {
			return ev, true
		}
		// Only ChooseName has a parked Attached replacement continuation.
		// Other Attached bodies must leave the Attach event untouched until
		// their own continuation is implemented.
		return ev, false
	}
	event, ok := replacementEvent(ev)
	if !ok {
		return ev, false
	}
	// The CantPutCounter prohibition that used to sit here is now enforced in
	// Engine.emit, BEFORE this replacement dispatch, so it applies even while
	// a replacement body is in flight (task addcounter1/2). Keeping it here
	// would skip it under applyingReplacement, the hole this task closes.
	// FINALITY (CR 122.1) is a replacement at the common move boundary:
	// a creature with a finality counter that would go from the battlefield to
	// a graveyard is exiled instead. This covers destruction, toughness-based
	// SBAs, legend-rule departures and sacrifices alike. The derived type walk
	// also handles a permanent animated into a creature, while the battlefield
	// origin guard prevents unrelated graveyard moves from being widened.
	if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield &&
		ev.To == state.ZGraveyard && e.finalityReplacementApplies(ev.Obj) {
		ev.To = state.ZExile
	}
	// CR 702.84b (Unearth): "Exile it ... if it would leave the
	// battlefield." A replacement effect, so it is matched at this common
	// move boundary before the destination is logged -- any departure
	// (graveyard, hand, library, exile, command zone) is redirected to
	// exile while the unearth promise is live (rules/unearth.go). The read
	// is game-state-derived from the live __kwUnearthExile registration, so
	// a log-only replay redirects the same move.
	if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield &&
		e.unearthReplacementApplies(ev.Obj) {
		ev.To = state.ZExile
	}
	// Madness is an optional discard replacement and must park before either
	// destination is logged. The guarded re-emit still permits ordinary card
	// and format replacements to redirect the chosen destination.
	if ev.Kind == events.MoveZone && !e.applyingMadnessChoice && e.madnessReplacementApplies(ev) {
		e.parkMadnessDiscard(ev)
		return ev, true
	}
	// All card-defined as-enters choices (including Riot and Unleash) are
	// parked through the shared mid-resolution ETB path below.
	if e.applyETBChoiceReplacement(ev) {
		return ev, true
	}
	// Defensive legacy Riot fallback.
	if e.applyRiotReplacement(ev) {
		return ev, true
	}
	// kw:Unleash (CR 702.86) asks its take-the-counter-or-not question as the
	// creature would enter, the Riot parking discipline (rules/unleash.go).
	if e.applyUnleashReplacement(ev) {
		return ev, true
	}
	// CR 310.10: a Battle Siege's protector is chosen as it enters. Parked
	// exactly like Riot above so every entry path records it; the parked move
	// is emitted once the answer is logged.
	if e.applySiegeProtector(ev) {
		return ev, true
	}
	// CR 903.9 (Task m32): a commander about to be put into its owner's
	// graveyard, hand or library from anywhere, or exiled from anywhere, may
	// instead be put into the command zone by its OWNER. This is a
	// replacement effect exactly like the R: lines below -- it applies before
	// the object would change zones, so the commander never touches the
	// destination -- but it is a construct rule, not a card line, so it is
	// matched first (before any card-text replacement the same move might
	// also match, which per CR 616.1 would then apply to whichever zone
	// change actually happens). Matching parks the event: the owner is asked
	// (decision.KCommanderZone) and the parked move is emitted for real only
	// when the answer arrives -- to the command zone on an accept, verbatim
	// on a decline -- so the log always carries the zone change that actually
	// happened and a log-only replay reproduces it. The choice itself is a
	// player decision recorded as an Intent plus the DecisionAsk/DecisionMade
	// events every ask produces. Returning handled discards the original
	// event, which is exactly right: nothing has happened yet, and whatever
	// happens is the owner's answer, not this park.
	if ev.Kind == events.MoveZone && e.commanderZoneReplacementApplies(ev) {
		e.parkCommanderZoneMove(ev)
		return ev, true
	}
	// Collect EVERY replacement effect this event matches, in
	// forEachObject's deterministic scan order, rather than the single first
	// match the M1 build took.
	var matches, manaCandidates []replMatch
	// Effect-created replacements (Blood of the Martyr and the broader
	// ReplacementEffects$ family) are active independently of their source's
	// current zone. active() enforces the Effect duration; reconstruct the
	// Forge R: body into the same replMatch path used by printed replacements
	// so filters, ordering and replacement context cannot drift.
	var ceL []ContinuousEffect
	if e.effectReplacementsPossible() {
		ceL = e.active()
	} else if trigZoneSkipVerify {
		// trigger_grantfree.go's replSeen proof: verify mode reads the list
		// anyway and panics if it holds an effect-created replacement.
		act := e.active()
		for i := range act {
			if act[i].ReplacementEvent != "" {
				panic("rules: effect-replacement proof skipped an active effect-created replacement")
			}
		}
	}
	for ceI := 0; ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.ReplacementEvent == "" || ce.ReplacementEvent != event {
			continue
		}
		if with := replacementBodySA(ce.ReplacementBody); with != nil {
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams, With: with}
			if e.replacementMatchesEffectCreatedBy(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers, ce.Controller) {
				matches = append(matches, replMatch{id: ce.Source, repl: r, remembered: ce.Remembered,
					rememberedPlayers: ce.RememberedPlayers,
					chosen:            ce.ChosenNumber, controller: ce.Controller, frozenController: true,
					key: "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))})
			}
		} else if ce.ReplacementBody == "" && (strings.EqualFold(strings.TrimSpace(ce.ReplacementParam(cards.PKLayer)), "CantHappen") ||
			(event == "DamageDone" && strings.EqualFold(ce.ReplacementParam(cards.PKPrevent), "True"))) {
			// The Effect-created CantHappen form (Mistrise Village's AntiMagic:
			// "the next spell you cast this turn can't be countered"): no
			// ReplaceWith$ — stopping the event is the complete replacement,
			// the same shape printed R: lines take (the With==nil arm below).
			// The bodyless Prevent$ True DamageDone form (Selfless Squire's
			// RPrevent, task dponce1; the wider bodyless prevent family it
			// belongs to) is the same idiom for damage: full prevention is the
			// complete replacement, applied by the shared damage dispatch
			// (applyNonMoveReplacements' Prevent$ arm) exactly as a printed R:
			// line's would be.
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
			if e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) {
				matches = append(matches, replMatch{id: ce.Source, repl: r, remembered: ce.Remembered,
					rememberedPlayers: ce.RememberedPlayers,
					key:               "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))})
			}
		}
	}
	e.forEachReplacementSourceFor(replEventBit(event), func(id state.ObjID) {
		f := e.replacementFace(id, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			if !replacementEventNameMatches(f.Repls[i].Event, event) {
				continue
			}
			m := replMatch{id: id, face: f, repl: &f.Repls[i]}
			// Mana replacement applicability must be re-evaluated after
			// every rewrite (CR 616.1). Keep even the candidates that do
			// not match the initial amount: multiplying mana can make a
			// later ManaAmount$ gate newly applicable.
			if ev.Kind == events.ManaAdd {
				manaCandidates = append(manaCandidates, m)
			}
			if ev.Kind == events.MoveZone && movedLineRejects(&f.Repls[i], id, ev) {
				if replZoneSkipVerify && e.replacementMatches(f.Repls[i], id, ev) {
					panic(fmt.Sprintf("rules: Moved prefilter rejected obj %d's matching line for a move of obj %d", id, ev.Obj))
				}
				continue
			}
			if e.replacementMatches(f.Repls[i], id, ev) {
				matches = append(matches, m)
			}
		}
	})
	// kw:Bloodthirst (CR 702.54): the entering permanent's own bloodthirst --
	// printed or layer-6 granted -- is one more Updated entry replacement,
	// collected AFTER the face-Repl scan so the deterministic composition
	// order stays "the card's own entry effects, then the keyword's". All
	// entry augmentations commute, so the append position cannot change a
	// result; it only fixes the scan order.
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
		if m := e.bloodthirstEntryMatch(ev); m != nil && e.replacementMatches(*m.repl, m.id, ev) {
			matches = append(matches, *m)
		}
		// kw:Sunburst (CR 702.47): the layer-6 `Keywords$ Sunburst` GRANT shape
		// (Solar Array, Lux Artillery) is one more Updated entry replacement,
		// collected after the face-Repl scan for the same deterministic
		// composition reason bloodthirst's is; a printed K:Sunburst face carries
		// the cards-side expansion (cards/kw_sunburst.go) instead, which this
		// scan already collected, and the synthetic skips it.
		if m := e.sunburstEntryMatch(ev); m != nil && e.replacementMatches(*m.repl, m.id, ev) {
			matches = append(matches, *m)
		}
	}
	// The GRANTED "Prevent all [combat] damage dealt to/by CARDNAME." keyword
	// (CR 615.1), read off the derived keyword list the same way
	// (rules/replacement_prevent_kw.go).
	if ev.Kind == events.Damage {
		matches = append(matches, e.grantedPreventMatches(ev)...)
	}
	matches = e.dropAppliedReplacements(matches)
	if ev.Kind == events.ManaAdd {
		return e.continueManaReplacements(ev, manaCandidates, nil, false, e.manaFromTap, e.manaProducer)
	}
	if ev.Kind == events.Scry {
		// The scry instruction boundary (CR 614.4): the proposal is held, not
		// logged, so continueScryReplacements owns the whole return -- it
		// rewrites the held instruction's count in place (handled=true, event
		// still a Scry) or replaces it whole (handled=true, zero event), so
		// the generic single-match/CR-616.1 path below must never see it.
		return e.continueScryReplacements(ev, matches, nil, nil, 0)
	}
	if ev.Kind == events.RollDice {
		// The roll-action boundary (CR 614.4, task rolldice-repl): the
		// proposal is held, not logged, so continueRollDiceReplacements owns
		// the whole return -- it rewrites the held proposal's dice count and
		// ignored-low count in place and the caller rolls the rewritten roll.
		return e.continueRollDiceReplacements(ev, matches)
	}
	if ev.Kind == events.PlanarRoll {
		// The planar-dice class (Ichor Elixir) and the bare roll BOTH run
		// here: even with no replacement matching, the roll itself is the
		// dispatch's job — the emitted event is the PROPOSAL (Amount$), the
		// completed record (results in IDs) must exist either way.
		return e.continuePlanarRollReplacements(ev, matches)
	}
	if len(matches) == 0 {
		return ev, false
	}
	switch ev.Kind {
	case events.Untap:
		return e.continueUntapReplacements(ev, matches)
	case events.StepChange:
		return e.continuePhaseReplacements(ev, matches, nil)
	case events.FlipFace:
		return e.applyTransformReplacement(ev, matches)
	case events.TokenCreate:
		return e.continueCreateTokenReplacements(ev, matches)
	case events.Explore:
		return e.continueExploreReplacements(ev, matches)
	case events.Damage:
		matches = e.applicableDamageReplacements(ev, matches)
		if len(matches) == 0 {
			return ev, false
		}
		if len(matches) > 1 || hasOptionalReplacement(matches) {
			if p, ok := e.damageAffectedPlayer(ev); ok && !e.G.Players[p].Lost {
				e.poseDamageReplacementChoice(ev, matches, e.replacementAskPlayer(matches, p))
				return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
					Text: "damage awaiting replacement-order choice"}, true
			}
		}
		return e.applyNonMoveReplacements(ev, matches)
	case events.CounterChange, events.PlayerCounterChange:
		return e.applyAddCounterReplacements(ev, matches)
	case events.TurnFaceUp:
		return e.applyTurnFaceUpReplacements(ev, matches)
	case events.Cascade:
		// The instruction proposal. Unlike every other kind here it has no
		// logged event to emit or suppress: applyCascadeReplacements runs the
		// matching ReplaceWith$ body with the caller's residue chained after
		// it, and the caller owns the cascade's continuation entirely.
		return e.applyCascadeReplacements(ev, matches)
	}

	// CR 616.1: if two or more replacement effects would modify the way this
	// event affects an object, each gets one opportunity to apply and the
	// affected player chooses the order. Two shapes follow.
	//
	//   - All matches are "Updated" (Forge's "the event still happens,
	//     augmented" idiom, e.g. an "enters tapped" plus an "enters with
	//     counters"). The original event is emitted once and each With is
	//     resolved in turn, so the object finishes with BOTH characteristics
	//     set (CR 616.1f) instead of whichever scan happened to reach first.
	//     Every ordering lands the same result for that commute shape, so no
	//     player order choice is posed -- the CR 616.1 choice is about order
	//     that can CHANGE the result, and a pure-augment competition never
	//     reorders a destination, so it would be a decision nobody answers
	//     differently.
	//
	//   - Otherwise at least one "Replaced"/destination-changing replacement
	//     competes (Rest in Peace's exile vs. Darksteel Colossus's shuffle):
	//     the affected controller must choose BEFORE anything relocates, so
	//     the event is parked and a KReplacement choice is posed (see
	//     poseReplacementChoice / handleReplacement).
	if len(matches) == 1 {
		return e.applyReplacement(ev, matches[0])
	}
	allUpdated := true
	for _, m := range matches {
		if m.repl.ParamStr(cards.PKReplacementResult) != "Updated" {
			allUpdated = false
			break
		}
	}
	if allUpdated {
		return e.composeUpdatedReplacements(ev, matches)
	}
	// Not all "Updated": at least one destination-changing ("Replaced")
	// replacement competes, so the affected controller must choose BEFORE
	// anything relocates (CR 616.1). A controller who has left the game makes
	// no choices (CR 800.4a), so for them the first replacement that has a
	// body applies deterministically rather than stalling the engine.
	if ea := e.G.Obj(ev.Obj); ea != nil {
		p := ea.Controller
		if int(p) < len(e.G.Players) && !e.G.Players[p].Lost {
			e.poseReplacementChoice(ev, matches)
			return ev, true
		}
	}
	for _, m := range matches {
		if m.repl.With != nil {
			return e.applyReplacement(ev, m)
		}
	}
	// Every match is bodyless: for a Moved event a bodyless replacement's
	// complete replacement is "the move does not happen" (CR 614.1a, the
	// cantHappen shape Grafdigger's Cage / Kunoros, Hound of Athreos /
	// Soulless Jailer / Weathered Runestone / Worms of the Earth carry), so
	// stopping the event is the deterministic fallback for a controller who
	// has left the game just as a With-bearing match would be. Any other
	// event kind stays unhandled here.
	if ev.Kind == events.MoveZone {
		return ev, true
	}
	return ev, false
}

// applyNonMoveReplacements applies a lone damage replacement, or the
// deterministic fallback used when the affected player has left the game.
// Competing replacements for a live affected player are parked and ordered by
// KReplacement instead.
func (e *Engine) applyNonMoveReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		// CR 616.1e: after each modification, applicability is checked again
		// against the changed event (not the original amount). The recheck
		// uses the same matcher class the collection used (see
		// applicableDamageReplacements): an Effect-created match is never
		// re-gated on ActiveZones$.
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if ev.Kind == events.Damage && damageReplacementPrevents(*m.repl) {
			if e.cantPreventDamage(e.damaging, ev.Obj) {
				// Neither a Prevent$ True line nor a DB$ ReplaceDamage body
				// may touch damage that cannot be prevented (Spider-Punk).
				continue
			}
			if strings.EqualFold(m.repl.ParamStr(cards.PKPrevent), "True") {
				// Stored through a re-entrant emit (the ReplaceDamage arm's
				// shape, task dponce1): the log record IS the prevention's
				// occurrence, so Mode$ DamagePreventedOnce triggers fire off it
				// -- Amount carries the prevented damage (Note is an Apply
				// no-op marker; no reader of Note.Amount predates this). Obj is
				// the damaged object (0 for a player hit) and Player the
				// damaged player, the uniform shape every stored prevention
				// Note keeps. The returned kind is still a Note, never a
				// Damage: the combat assignment loop and speed.go read it.
				return e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
					Amount: ev.Amount, Text: "damage prevented by replacement effect"}), true
			}
			// a ReplaceDamage body falls through to its subtracting arm below
		}
		if m.repl.With == nil {
			// Counter's Layer$ CantHappen shape has no ReplaceWith$: stopping
			// the event is its complete replacement.
			return ev, true
		}
		if m.repl.With.API == "ReplaceDamage" {
			// Handled here, not through runReplaceWith/effects.Resolve: the
			// body subtracts its Amount from the held event and reports
			// terminal when fully prevented (its prevention Note is the log's
			// record, exactly as a Prevent$ True match's) or leaves the
			// reduced event standing for the next modifier. Its SubAbility$
			// chain is deliberately not run (see applyReplaceDamageBody).
			if e.applyReplaceDamageBody(&ev, m) {
				return ev, true
			}
			continue
		}
		ctx := e.replCtx(m, ev)
		rewritesHeldEvent := replacementBodyRewritesHeldEvent(m.repl.With, ctx.SVars)
		e.runReplaceWith(ctx, ev.Obj, m.repl.With, &ev)
		if rewritesHeldEvent {
			// The body rewrote the held amount (changed) or could not resolve
			// its value and left it alone; either way the event stands and the
			// next modifier applies to the result.
			continue
		}
		// A body that does not rewrite the held event (DB$ DealDamage,
		// DB$ RemoveCounters, ...) supplied its own outcome; its emissions
		// replace the original event.
		return ev, true
	}
	return ev, false
}

// replMatch is one replacement effect the engine found applicable to an
// event: its owning source permanent and the R: line on that permanent's
// face. A plain value, cloned by copy.
type replMatch struct {
	id   state.ObjID
	face *cards.Face // prospective face for an "as this transforms" replacement
	repl *cards.Repl
	// key identifies an Effect-created replacement across active() rebuilds.
	// Printed replacement pointers are immutable face entries and need no key.
	key string
	// remembered carries the Effect-created replacement's remembered ids so
	// a per-mint re-match (continueCreateTokenReplacements) can re-evaluate
	// its IsRemembered specs exactly as the initial match did. Printed
	// replacements never carry one.
	remembered []state.ObjID
	// rememberedPlayers is the player half of the same capture
	// (state.ContinuousEffect.RememberedPlayers): a prevention shield's
	// player recipients (effects' PreventDamage) scope their match by it
	// through rules' damageReplacementMatches, the one damage gate that can
	// see it. Printed replacements never carry one.
	rememberedPlayers []state.PlayerID
	// chosen carries the Effect-created replacement's SetChosenNumber$ binding
	// (state.ContinuousEffect.ChosenNumber, task wildgrowth1): the number the
	// Effect resolved at creation, which replCtx threads into the body Ctx so
	// the body's Count$ChosenNumber head reads the frozen binding. Zero on
	// every printed replacement (and on an Effect that bound nothing).
	chosen int32
	// Effect-created replacements retain the controller who resolved their
	// granting ability; later source control changes cannot redefine You.
	controller       state.PlayerID
	frozenController bool
}

// rememberedSpecContext builds the match context a ValidCard$/ValidLKI$
// spec on a Moved replacement evaluates under: the ordinary You/Source pair,
// plus the remembered ids as targets when the caller carries any (the
// Effect-created ReplaceDyingDefined$ family), plus the source object's
// chosen cards. The chosen half is the event-backed Choose answer the
// ChooseCard/ChooseSource family records on its source (events.Apply's
// Choose "chosen" fold) and every ChosenCard/ChosenCardStrict predicate
// gates on -- Forge reads the source's chosen list here, and reading it from
// the SAME event-backed source the resolution-time filter reads (rather than
// a second engine-runtime copy on the replacement registration) keeps the two
// paths from drifting. A source with no choice leaves ChosenValid false, so
// the predicates fail closed exactly as before. Nil ids yield the plain
// context every caller without a remembered set already built.
func (e *Engine) rememberedSpecContext(you state.PlayerID, source state.ObjID, remembered []state.ObjID) effects.SpecContext {
	sc := e.withNames(effects.NewSpecContext(you, source))
	if chosen := effects.ChosenTargetsFrom(e.G, source); len(chosen) > 0 {
		sc.Chosen = chosen
		sc.ChosenValid = true
	}
	if len(remembered) > 0 {
		for _, id := range remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: id})
		}
	}
	return sc
}

func hasOptionalReplacement(matches []replMatch) bool {
	for _, m := range matches {
		if strings.EqualFold(m.repl.ParamStr(cards.PKOptional), "True") {
			return true
		}
	}
	return false
}

// replacementOptionalDecider resolves the controller an Optional$ True
// replacement's "may" belongs to, from Forge's OptionalDecider$ -- named in
// the replacement source's frame. The corpus's two Optional$ DamageDone lines
// (Blood of the Martyr, Battletide Alchemist) both say "You": the source's
// controller, not the damaged player. An ABSENT parameter resolves to nobody
// (the caller keeps its historical default); a present spec this build does
// not resolve also resolves to nobody, and the match gate above excluded the
// replacement already, so this is only reachable for "You".
func (e *Engine) replacementOptionalDecider(r cards.Repl, source state.ObjID) (state.PlayerID, bool) {
	if strings.TrimSpace(r.ParamStr(cards.PKOptionalDecider)) != "You" {
		return 0, false
	}
	ctrl := e.controllerOf(source)
	if ctrl < 0 || int(ctrl) >= len(e.G.Players) {
		return 0, false
	}
	return ctrl, true
}

// replacementAskPlayer picks who answers a damage replacement competition.
// The CR 616.1 order choice belongs to the affected player -- except that a
// competition of exactly ONE Optional$ True replacement belongs to that
// replacement's OptionalDecider$ (Blood of the Martyr and Battletide
// Alchemist both name "You": their own controller), because then the only
// question posed is the optional replacement's own "may", not an order.
// A decider who has lost or left makes no choices (CR 800.4a), so the
// affected player answers instead.
func (e *Engine) replacementAskPlayer(matches []replMatch, affected state.PlayerID) state.PlayerID {
	if len(matches) == 1 && strings.EqualFold(matches[0].repl.ParamStr(cards.PKOptional), "True") {
		if dp, ok := e.replacementOptionalDecider(*matches[0].repl, matches[0].id); ok &&
			!e.G.Players[dp].Lost {
			return dp
		}
	}
	return affected
}

// parseEffectKey splits a replMatch's effect-created key
// ("effect:<source>:<timestamp>") back into its parts.
func parseEffectKey(key string) (state.ObjID, uint32, bool) {
	rest, ok := strings.CutPrefix(key, "effect:")
	if !ok {
		return 0, 0, false
	}
	src, ts, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, 0, false
	}
	id, err := strconv.ParseUint(src, 10, 32)
	if err != nil {
		return 0, 0, false
	}
	stamp, err := strconv.ParseUint(ts, 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return state.ObjID(id), uint32(stamp), true
}

// replacementBodySA turns the body retained by an Effect-created replacement
// into the same immutable SA shape cards.Parse builds for a printed R: line.
// It deliberately shares the ordinary `Kind$ API | Key$ Value` grammar rather
// than recognizing Blood of the Martyr by name.
func replacementBodySA(body string) *cards.SA {
	parts := strings.Split(body, "|")
	if len(parts) == 0 {
		return nil
	}
	head := strings.TrimSpace(parts[0])
	kind, api, ok := strings.Cut(head, "$")
	if !ok || strings.TrimSpace(api) == "" {
		return nil
	}
	sa := &cards.SA{Kind: strings.TrimSpace(kind), API: strings.TrimSpace(api), Params: make(map[string]string)}
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "$")
		if ok && strings.TrimSpace(key) != "" {
			sa.Params[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return sa
}

// replacementEventNameMatches compares a printed R:Event$ name with the
// event name an engine event maps to. "DrawCards" is Forge's spelling of
// the draw replacement event (Quantum Riddler's "you draw that many cards
// plus one instead", Alms Collector's "if an opponent would draw two or
// more cards"); the engine maps events.Draw to "Draw", so the alias reads
// here rather than in the IR (two corpus carriers, both
// CheckSVar$/Number$-gated).
func replacementEventNameMatches(replEvent, event string) bool {
	if replEvent == event {
		return true
	}
	return event == "Draw" && replEvent == "DrawCards"
}

// replacementEvent maps the event log's concrete events to Forge R:Event$
// names. ManaAdd's producer and tap provenance live in synchronous Engine
// scratch instead of its hash-chained fields; ProduceMana matching requires
// both, while the logged event remains the ordinary final mana production.
func replacementEvent(ev events.Event) (string, bool) {
	switch ev.Kind {
	case events.Attach:
		return "Attached", true
	case events.MoveZone:
		return "Moved", true
	case events.Untap:
		return "Untap", true
	case events.StepChange:
		return "BeginPhase", true
	case events.FlipFace:
		return "Transform", true
	case events.ManaAdd:
		return "ProduceMana", true
	case events.Damage:
		return "DamageDone", true
	case events.Draw:
		return "Draw", true
	case events.TokenCreate:
		return "CreateToken", true
	case events.Explore:
		return "Explore", true
	case events.Cascade:
		// The cascade instruction's replacement boundary (CR 614.4; Averna,
		// the Chaos Bloom). Only the synthetic PROPOSAL (Engine.
		// ProposeCascadeReplacement) reaches the collection: the Kind is never
		// emitted, so no logged event can ever map here. The exiled batch
		// rides ev.IDs and becomes Ctx.ReplacedCards on the body's context.
		return "Cascade", true
	case events.Scry:
		// The scry instruction boundary. Only the synthetic PROPOSAL
		// (Engine.Scry) reaches the collection; the completed record is
		// emitted through emitScryRecord, outside the replacement pass.
		return "Scry", true
	case events.RollDice:
		// The roll-action boundary (task rolldice-repl). Only the synthetic
		// PROPOSAL (Engine.RollDiceProposed) reaches the collection: the Kind
		// is never emitted, so no logged event can ever map here.
		return "RollDice", true
	case events.PlanarRoll:
		return "RollPlanarDice", true
	case events.CounterChange, events.PlayerCounterChange:
		// The counter-placement replacement class (Hardened Scales, Branching
		// Evolution, Doubling Season, Vorinclex): R:Event$ AddCounter modifies
		// how many counters the event places, in place, exactly as DamageDone's
		// ReplaceDamage bodies rewrite a held Damage amount. Both the object
		// form (CounterChange) and the player form (PlayerCounterChange) share
		// the class; the matcher splits them on ValidCard$/ValidObject$ vs
		// ValidPlayer$.
		return "AddCounter", true
	case events.TurnFaceUp:
		// The turn-up boundary itself (CR 614.1a with CR 708.6/702.36e, task
		// cli-20260924T031747Z-6d0658fc): Hooded Hydra's five +1/+1 counters,
		// Karlov Watchdog's CantHappen prohibition, Gift of Doom's attach.
		// These match the marker events.TurnFaceUp that the morph-family
		// special action and the SetState effect's turn-up arm emit.
		return "TurnFaceUp", true
	default:
		return "", false
	}
}

// replacementFace returns the source face whose R: lines apply now. A
// transform's "as this transforms into ..." replacement belongs to the
// destination face, while every other replacement reads the source's current
// face. This avoids making the alternate face live for unrelated events.
func (e *Engine) replacementFace(id state.ObjID, ev events.Event) *cards.Face {
	o := e.G.Obj(id)
	if o == nil || o.Card == nil {
		return nil
	}
	if ev.Kind == events.FlipFace && id == ev.Obj && ev.Amount >= 0 && int(ev.Amount) < len(o.Card.Faces) {
		return o.Card.Faces[ev.Amount]
	}
	if e.printedAbilitiesLost(o) {
		// CR 613.1f: a battlefield permanent that lost all abilities has no
		// printed replacement ability. (An entering permanent is not on the
		// battlefield yet, so its own as-enters replacements still apply.)
		return nil
	}
	return o.Face()
}

// replCtx builds the effects.Ctx a replacement's ReplaceWith$ resolves
// under: source is the permanent whose R: line owns the replacement, X is
// the {X} paid for the moving object, Replaced names the object the replaced
// event was about (so a Defined$ ReplacedCard body finds its subject), and
// the SVar table comes from the source's face. This is exactly the context
// the single-match path built inline for M1; it is factored out so the
// composition and order-choice paths reuse it.
//
// Ctx.Remembered deliberately starts EMPTY. The replaced object is reached
// through Replaced/Defined$ ReplacedCard, never through Remembered: every
// corpus body that counts Remembered$Amount inside a replacement (the
// pay-before-ETB family's ConditionCheckSVar$ X gates — Mox Diamond, Scorched
// Ruins, Soldevi Excavations, the sac-lands — and the CounterNum$ reads)
// rides its own Remember* rider (RememberDiscarded$/RememberSacrificed$/
// RememberChanged$/RememberRevealed$), and seeding the replaced card here
// made every one of those counts one too high (measured over the compiled
// corpus: all nine gating bodies carry a rider; none reads the seed). The
// seed was fx44's stand-in for the suspended-resolution case; the resume
// thread carries the body's own Remembered instead (rules/resolution.go's
// replacement branch).
//
// The rule is scoped to PRINTED replacements. An EFFECT-created replacement
// (m.key != "", task wildgrowth1) is a different population: the effect's
// registered Remembered list IS the intended binding -- the trigger captured
// the cast spell with RememberObjects$ and the body's Remembered$ specs (the
// runadi_behemoth_caller / gluttonous_hellkite CounterNum$ readers) read
// exactly that -- and none of those bodies rides a competing Remember* rider,
// so the double-counting defect the printed rule exists for cannot arise.
// replCtx threads the effect's frozen SetChosenNumber$ binding alongside it
// (replMatch.chosen), what the body's Count$ChosenNumber head reads.
func (e *Engine) replCtx(m replMatch, ev events.Event) *effects.Ctx {
	o := e.G.Obj(m.id)
	target := state.Target{Obj: ev.Obj}
	if ev.Kind == events.Damage && ev.Obj == 0 {
		target = state.Target{Player: ev.Player, IsPlayer: true}
	}
	if o == nil {
		// No live source object: no controller (seat 0).
		ctx := effects.NewCtxPtr(m.id, 0, effects.CtxInit{})
		ctx.Repl.Target = target
		ctx.Repl.Source, ctx.Repl.Amount = e.protectionSource(e.damaging), drawMatchAmount(ev)
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
			ctx.Repl.ExcludeFromBattlefieldCount = ev.Obj
		}
		e.seedEffectReplCtx(ctx, m)
		return ctx
	}
	controller := o.Controller
	if m.frozenController {
		controller = m.controller
	}
	ctx := effects.NewCtxPtr(m.id, controller, effects.CtxInit{
		// X is the {X} paid for the moving object, so an ETB replacement that
		// reads it (etbCounter's CounterNum$ X, e.g. Endless One / Walking
		// Ballista / Chalice of the Void) sees the value the player actually
		// chose. Move preserves X from the stack onto the permanent (events/
		// apply.go, the "hand/stack -> battlefield must NOT reset them"
		// comment), so o.X is the cast-time value here.
		X:        o.X,
		Captured: []state.Target{{Obj: ev.Obj}}})
	ctx.Repl.Target = target
	ctx.Repl.Source, ctx.Repl.Amount = e.protectionSource(e.damaging), drawMatchAmount(ev)
	// Replaced names the object the replaced event (ev) was about, so a
	// ReplaceWith$ that says Defined$ ReplacedCard (the Rest in Peace /
	// Dryad Militant / Leyline of the Void shape: "exile it instead") can
	// act on exactly the card being kept out of the graveyard -- not the
	// source that owns the replacement.
	ctx.Repl.Replaced = ev.Obj
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
		ctx.Repl.ExcludeFromBattlefieldCount = ev.Obj
	}
	// The cascade instruction's ordered exiled batch, the plural referent
	// Averna's ReplaceWith$ body reads as Defined$ ReplacedCards.<qual>. Only a
	// Cascade proposal carries it; every other replacement leaves the field
	// empty (the singular Replaced seed above is unchanged).
	if ev.Kind == events.Cascade {
		ctx.Repl.Cards = append([]state.ObjID(nil), ev.IDs...)
	}
	f := m.face
	if f == nil {
		f = o.Face()
	}
	if ev.Kind == events.Draw {
		// A replaced DRAW names the draw-er, not (only) the card: the body's
		// ReplacedPlayer selectors (UnlessPayer$, Defined$) resolve against
		// this, and its own DB$ Draw re-does the draw the original event
		// would have done, so the Remembered/Captured seed above is dropped
		// for draws — the body's own RememberDrawn$ records what it actually
		// drew (a seeded stale entry would double the reveal's and the
		// discard condition's population).
		ctx.Repl.Player = state.Target{Player: ev.Player, IsPlayer: true}
		ctx.Remembered, ctx.Captured = nil, nil
	}
	if f != nil {
		effects.SetSVars(ctx, f.SVars)
	}
	if o != nil && m.repl != nil && m.repl.ParamStr(cards.PKKeyword) == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "Clone" {
		ctx.CloneEnter.ETB = true
		ctx.CloneEnter.Become = ev.Obj
		ctx.CloneEnter.BecomeValid = true
		ctx.CloneEnter.ChoiceValid = o.ETBCloneChoiceValid
		ctx.CloneEnter.Choice = o.ETBCloneChoice
	}
	// The as-enters colour-choice body (K:ETBReplacement:Other:ChooseColor)
	// marks itself: the entry machinery (applyETBChoiceReplacement ->
	// resumeETBEntry) already posed the entry ask and recorded the answer on
	// the entering object before this body runs at the re-emitted move, so
	// effChooseColor keeps the historical no-op for THIS invocation rather
	// than posing a second ask (task cli-20260923T060000Z-choose-color; the
	// flag is what keeps a FRESH resolution-time ask after an earlier
	// ChooseColor's answer askable -- the stale-source-state guard cannot be
	// unconditional). The effect consumes the flag, so a nested ChooseColor
	// in the same chain poses its own fresh ask.
	if o != nil && m.repl != nil && m.repl.ParamStr(cards.PKKeyword) == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "ChooseColor" {
		ctx.ETB.ColorRecorded = true
	}
	// The as-enters NUMBER-choice body (K:ETBReplacement:Other:ChooseNumber,
	// Talion the Kindly Lord) marks itself the same way: resumeETBEntry already
	// posed the entry ask and recorded the answer on the entering object, so
	// effChooseNumber keeps the historical no-op for THIS invocation rather
	// than posing a second ask (task cli-20260923T060000Z-choose-number). The
	// flag is exact where a bare o.ChosenNumber guard is not: a recorded entry
	// answer of 0 is indistinguishable from unset on the object, but the flag
	// is set precisely when the machinery recorded one.
	if o != nil && m.repl != nil && m.repl.ParamStr(cards.PKKeyword) == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "ChooseNumber" {
		ctx.ETB.NumberRecorded = true
	}
	if o != nil && m.repl != nil && m.repl.ParamStr(cards.PKKeyword) == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "ChooseEvenOdd" {
		ctx.ETB.EvenOddRecorded = true
	}
	e.seedEffectReplCtx(ctx, m)
	return ctx
}

// seedEffectReplCtx threads an Effect-created match's own bindings into the
// body Ctx: the frozen SetChosenNumber$ number and (key-only: never a printed
// replacement) the effect's registered Remembered list. See replCtx's doc for
// why the seed is scoped to effect-created matches. The Draw branch above
// clears ctx.Remembered for the bodies that re-draw; no Draw body is ever
// registered live (effects' registration gate keeps them on the loud Note),
// so a live seed cannot be erased by that clearing on a registered shape.
func (e *Engine) seedEffectReplCtx(ctx *effects.Ctx, m replMatch) {
	ctx.Num.Chosen = m.chosen
	// The bound flag is the Count$ChosenNumber head's verdict: effect-created
	// only, so a printed or choose-event context stays UNRESOLVED and the
	// EvalCountOK consumers keep their fail direction (see Ctx.ChosenNumberBound).
	ctx.Num.ChosenBound = m.key != ""
	if src, ts, ok := parseEffectKey(m.key); ok {
		ctx.EffectFrame = effects.EffectFrame{Source: src, Stamp: ts}
	}
	if m.key == "" || (len(m.remembered) == 0 && len(m.rememberedPlayers) == 0) {
		return
	}
	for _, id := range m.remembered {
		ctx.Remembered = append(ctx.Remembered, state.Target{Obj: id})
	}
	for _, player := range m.rememberedPlayers {
		ctx.Remembered = append(ctx.Remembered, state.Target{Player: player, IsPlayer: true})
	}
}

// replacementBodyRewritesHeldEvent classifies the whole ReplaceWith$
// chain, not just its first API. A chain may perform work (for example RollDice)
// before a later ReplaceEffect rewrites the held event. Resolve the same
// unlinked Effect-created SubAbility shape runReplaceWith links before walking.
func replacementBodyRewritesHeldEvent(with *cards.SA, svars map[string]string) bool {
	var walk func(*cards.SA) bool
	walk = func(with *cards.SA) bool {
		if with == nil {
			return false
		}
		if with.API == "ReplaceEffect" {
			return true
		}
		sub := with.Sub
		if sub == nil && svars != nil {
			if name := strings.TrimSpace(with.ParamStr(cards.PKSubAbility)); name != "" {
				sub = cards.ResolveSVar(svars, name)
			}
		}
		return walk(sub)
	}
	return walk(with)
}

// runReplaceWith resolves one ReplaceWith$ body inside the replacement
// re-entrancy guard (CR 616.1, a replacement applies once), recording the
// replaced object for the body's Defined$/Remembered$ reads and restoring
// both the guard and that record afterward so an outer replacement keeps its
// own state.
// runReplaceWith resolves one ReplaceWith$ body inside the applyingReplacement
// guard, recording the state a nested emit needs: replReplaced/replAction
// (Engine.emit's events.CarryAction, so a body's own move of replReplaced
// carries the replaced event's action marker), replacingEvent/replacingSource
// (ReplaceEvent's target -- a DB$ ReplaceDamage body reaching back to rewrite
// the live Damage event's Amount/Affected fields), and replReplacedPlayer (the
// draw-er a Draw replacement's body's ReplacedPlayer selectors read on
// resume). ev is nil wherever the original event was already logged (the
// "Updated" shape) or has no action marker worth carrying (mana/ETB
// replacements); passing it derives the action automatically rather than
// making every caller compute it.
func (e *Engine) runReplaceWith(ctx *effects.Ctx, replaced state.ObjID, with *cards.SA, ev *events.Event) {
	// An Effect-created replacement body is a fresh parse (replacementBodySA)
	// whose SubAbility$ chain was never linked -- only cards.Link links a
	// printed body. Resolve it from the source's own SVar table so a body
	// that carries a chain actually runs it: the ChooseSource family's
	// ReplaceWith$ body chains SubAbility$ ExileEffect
	// (`DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile`),
	// the idiom that ends the effect after one use. A printed body keeps its
	// already-linked chain (Sub non-nil), so this only touches the
	// Effect-created parse.
	if with != nil && with.Sub == nil && ctx != nil && ctx.SVars != nil {
		if name := strings.TrimSpace(with.ParamStr(cards.PKSubAbility)); name != "" {
			if sub := cards.ResolveSVar(ctx.SVars, name); sub != nil {
				linked := *with
				linked.Sub = sub
				with = &linked
			}
		}
	}
	savedRepl, savedEvent, savedSource, savedAction, savedPlayer :=
		e.replReplaced, e.replacingEvent, e.replacingSource, e.replAction, e.replReplacedPlayer
	savedReplCards := e.replReplacedCards
	savedRemembered := e.replRemembered
	savedApplying := e.applyingReplacement
	e.applyingReplacement = true
	action := ""
	if ev != nil {
		action = events.ActionMarker(*ev)
	}
	e.replReplaced, e.replacingEvent, e.replacingSource, e.replAction, e.replReplacedPlayer =
		replaced, ev, ctx.Source, action, ctx.Repl.Player
	if ctx != nil {
		e.replReplacedCards = append([]state.ObjID(nil), ctx.Repl.Cards...)
		// The body's own remembered binding is what a VarValue$ Remembered
		// Affected rewrite resolves against. Each body owns its backing array:
		// a nested runReplaceWith must not overwrite the saved outer body's
		// referents through a shared slice.
		e.replRemembered = append([]state.Target(nil), ctx.Remembered...)
	} else {
		e.replReplacedCards = nil
		e.replRemembered = nil
	}
	e.resolveReplacementBody(ctx, with)
	e.replReplaced, e.replacingEvent, e.replacingSource, e.replAction, e.replReplacedPlayer =
		savedRepl, savedEvent, savedSource, savedAction, savedPlayer
	e.replReplacedCards = savedReplCards
	e.replRemembered = savedRemembered
	e.applyingReplacement = savedApplying
}

// resolveReplacementBody resolves a ReplaceWith$ body. Inside a resolution
// pass (contChainOwners > 0) the pass already owns the continuation chain and
// this is resolveReplacementWith. OUTSIDE one -- an event emitted by turn
// structure, above all combat damage, where runCombatAssignments emits one
// Damage event per assignment -- two things differ:
//
//   - A body reached while an earlier body's ask is still unanswered (two
//     attackers hitting a Nefarious Lich / Immortal Coil controller: one
//     hidden graveyard pick per Damage event) must not run now. Its ask would
//     overwrite the pending decision (Engine.ask's guard panics), and even
//     deferred it would offer the cards the first pick is about to take. The
//     WHOLE body is queued instead, as a continuation frame at the tail of the
//     pending chain, so it runs -- and asks, against the then-current state --
//     once everything before it has been answered: event order, one decision
//     at a time. A ReplaceEffect body rewrites the held event synchronously
//     and never asks, so it always runs in place.
//   - A body that posts the first ask becomes its own continuation-chain
//     owner, so the rest of its SubAbility$ chain (the Lich's lose-the-game
//     check and cleanup) is linked after the ask instead of reported into a
//     contChain no pass drains.
//
// A third case arises only outside a pass: an ANSWER handler applying the
// chosen body of a CR 616.1 order choice that was posed mid-resolution
// (handleReplacement), with the proposing resolution still parked on
// e.resume and nothing pending. That frame is not this body's suspension, so
// it is recorded as e.answerParked for the body's duration and Suspended
// ignores it: otherwise effects.Resolve read it as the body's own ask after
// the head, skipped the body's SubAbility$ chain (a one-shot doubler's
// ExileEffect) and reported a continuation into e.contChain that no pass
// drains -- which then outlived the intent and differed from a Clone
// (paymirror control, commander seed 4130: Solphim, Mayhem Dominus and Ojer
// Axonil competing over a trigger's damage). An ask the body poses is
// unaffected: a pending decision makes the resolution suspended again.
func (e *Engine) resolveReplacementBody(ctx *effects.Ctx, with *cards.SA) {
	e.resolveReplacementWith(ctx, with)
}

// applyReplacement applies the ONE chosen replacement to a MoveZone event,
// the exact behaviour the M1 build had for its single matching replacement.
// An "Updated" match emits the original event, fires triggers (in the M1
// order: before the With resolves -- the I-3 caveat stands), then resolves
// the With; anything else ("Replaced", or no ReplacementResult$ at all --
// Task 22's four pins must keep reading as a full replace) discards the
// original and only the With's own effect happens. Returns the event to log
// (stored for Updated, ev for Replaced) and handled=true.
func (e *Engine) applyReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	if m.repl.With == nil {
		if ev.Kind == events.MoveZone {
			// A bodyless Moved replacement's complete replacement is stopping
			// the move, the Layer$ CantHappen shape Grafdigger's Cage,
			// Kunoros, Hound of Athreos, Soulless Jailer, Weathered Runestone
			// and Worms of the Earth all carry (CR 614.1a). The event must
			// not reach the battlefield, so report it handled. Scoped to
			// MoveZone deliberately: the bodyless R:Event$ Draw lines are a
			// different defect and keep their old behaviour.
			return ev, true
		}
		return ev, false
	}
	ctx := e.replCtx(m, ev)
	if m.repl.ParamStr(cards.PKReplacementResult) == "Updated" {
		// Review finding M-6: an exact case-sensitive "Updated" compare is
		// deliberate (the corpus spells it one way). Apply the ORIGINAL event
		// first (not routing back through e.emit, which is what keeps this
		// from re-running replacement matching on the event it just
		// matched), fire triggers (an ETB trigger watching this Move fires
		// exactly as it would for an unreplaced entry), THEN resolve the With
		// so a Tap lands on an object already in its new zone (an object
		// still on the stack is a no-op to effTap).
		departing, link, controller := e.captureSourceLifelinkLKI(ev)
		stored, absorbed := e.foldEntryMove(ev)
		e.loop.observeFrom(&stored, e.damaging, len(e.G.Objs))
		// The move-driven Effect lifetimes (the ExileOnMoved$/ForgetOnMoved$
		// sweep) run on Engine.emit's own MoveZone path right here in the
		// ordering; the raw events.Emit above bypasses that path, so the sweep
		// is replayed inline (task wildgrowth1: the "enters with N additional
		// counters" Effect must end exactly after the one entry it upgraded,
		// not linger to re-upgrade the same remembered card's next entry).
		e.effectMoveSweep(ev)
		e.finishSourceLifelinkLKI(ev, departing, link, controller)
		if !bodyAbsorbed(absorbed, m) {
			e.runReplaceWith(ctx, ev.Obj, m.repl.With, nil)
		}
		// The entry's own triggers are matched AFTER the Updated body: the
		// body is how the permanent ENTERS (CR 614.1c/614.12 -- "enters
		// tapped", "enters with counters"), so a leaves/enters trigger's
		// ValidCard$ must see the permanent as it entered. Matched before the
		// body, Amulet of Vigor's and Tiller Engine's `Permanent.tapped`
		// never matched an enters-tapped land (cardfuzz coverage audit: zero
		// fires in ~800 casts each).
		e.checkTriggers(&stored, nil, 0, 0, false)
		if e.pending == nil && stored.Kind == events.MoveZone && stored.To == state.ZBattlefield {
			e.finishLandPlay(stored.Obj)
		}
		return stored, true
	}
	savedRedirect := e.replRedirect
	if ev.Kind == events.MoveZone {
		e.replRedirect = &replRedirect{orig: ev,
			applied: append(append([]string(nil), e.replExclude...), replIdentity(m))}
	} else {
		e.replRedirect = nil
	}
	e.runReplaceWith(ctx, ev.Obj, m.repl.With, &ev)
	e.replRedirect = savedRedirect
	return ev, true
}

// resolveReplacementWith runs a ReplaceWith$ effect with e.damaging set to
// the permanent that owns the replacement and then restores whatever it was
// beforehand (Task 15 fix round 1, Important I3). A ReplaceWith$ resolves
// inside emit, where the only write to e.damaging so far was around
// resolveTop's own resolution calls and damageStep's assignment loop -- so a
// replacement that emits damage normally inherited whichever value that outer
// context happened to hold: the combat attacker when the replaced event came
// from damageStep's loop, or 0 (no source) during ordinary turn structure.
// Either way the damage was attributed to the wrong thing, or to nothing.
// The reading chosen here is: the damage a replacement emits is dealt by the
// permanent whose R: line owns the replacement (ctx.Source) -- it is an
// effect of that permanent, exactly as resolveTop attributes a resolved
// ability's damage to its source -- and CR 609.7a asks for the source of the
// effect, which is the permanent granting the replacement. The PREVIOUS
// damaging is saved and restored (never zeroed) so an outer in-flight
// assignment keeps its own attribution once the replacement returns.
func (e *Engine) resolveReplacementWith(ctx *effects.Ctx, with *cards.SA) {
	// ReplaceEffect rewrites the held event and must retain e.damaging as the
	// ORIGINAL damage source (Affected$ ReplacedSourceController needs it).
	// A body that emits its own damage still attributes that new event to the
	// permanent owning the replacement.
	if with.API == "ReplaceEffect" {
		effects.Resolve(e, ctx, with)
		return
	}
	saved := e.damaging
	e.damaging = ctx.Source
	effects.Resolve(e, ctx, with)
	e.damaging = saved
}

// replacementMatches implements the per-event match predicates for the five
// replacement events the engine routes through applyReplacements: R:Event$
// Moved (Origin$/Destination$/ValidCard$/ValidLKI$), Untap (the "doesn't
// untap during its controller's untap step" class), BeginPhase (the
// "skip your draw step" class), Transform (the "as this transforms" class)
// and ProduceMana (the "produces three times as much" class). All five share
// the ActiveZones$ gate; the per-event parameters each fail closed on a
// value this build cannot evaluate, the same contract filter.go's matcher
// gives card filters.
func (e *Engine) replacementMatches(r cards.Repl, source state.ObjID, ev events.Event) bool {
	return e.replacementMatchesRemembered(r, source, ev, nil, nil)
}

// replacementMatchesRemembered is replacementMatches with an optional
// remembered set: the remembered ids an Effect-created replacement carries
// (DealDamage's ReplaceDyingDefined$ registration) are what its ValidCard$/
// ValidLKI$ Card.IsRemembered spec is matched against — the Effect captured
// them when it resolved, and its continuous registry entry is the only place
// that set still lives. Printed R: lines pass nil and never see a remembered
// binding; a spec carrying IsRemembered against an empty set fails closed,
// the matcher's standing contract.
func (e *Engine) replacementMatchesRemembered(r cards.Repl, source state.ObjID, ev events.Event, remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool {
	if !e.activeZonesGateOK(r, source, ev) {
		return false
	}
	return e.replacementMatchesRememberedUngated(r, source, ev, remembered, rememberedPlayers, nil)
}
