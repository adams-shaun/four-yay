// replacement.go applies R: line replacement effects ahead of an event's
// own logging, from engine.go's emit. applyReplacements discovers every
// applicable effect in deterministic order, then applies the event-specific
// CR 616 ordering and optional-effect rules.
package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
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

// applyReplacementsDispatch is applyReplacements' common match-collection
// and per-event dispatch, shared by every replacement-eligible event kind
// (Moved/Untap/BeginPhase/Transform/ProduceMana/DamageDone). Factored out so
// applyReplacements can wrap a player-targeted Damage event with the
// repl:LifeReduced fallback above without duplicating this body.
// bloodthirstEntryMatch builds the synthetic Moved replacement a permanent
// with bloodthirst enters by (CR 702.54: "Bloodthirst N means 'If an
// opponent was dealt damage this turn, this permanent enters the battlefield
// with N +1/+1 counters on it.'"). The keyword is read from the entering
// object's DERIVED keyword list (derivedKeywordParam), so a printed
// K:Bloodthirst:<N> and a layer-6 `AddKeyword$ Bloodthirst:<N>` grant are
// ONE identical shape -- the grant path is the shape a cards-side expansion
// could never see (Twins of Discord; the primitive ratchet counts only
// Face.Primitives()'s printed-keyword walk, so the grant was previously a
// silent no-op the census could not even name).
//
// A fixed N gates on the existing CheckSVar$/SVarCompare$ pair -- the SVar
// name is an INLINE Count body (replacementCheckValue falls through to
// effects.EvalCount for a name no face SVar table defines), reading the
// DamageOppsTakenThisTurn head compared GT0. Bloodthirst X has no gate: the
// count IS the amount, so the body's CounterNum$ is the same inline Count
// body. A param that is neither a positive literal nor X (unmeasured in the
// corpus, all 23 printed lines spell <N> or X) fails closed to no match --
// the conservative direction for a counter put.
func (e *Engine) bloodthirstEntryMatch(ev events.Event) *replMatch {
	param, ok := e.derivedKeywordParamH(ev.Obj, kwhBloodthirst)
	if !ok {
		return nil
	}
	body := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined":     "Self",
		"CounterType": "P1P1",
		"ETB":         "True",
	}}
	r := &cards.Repl{Event: "Moved", Params: map[string]string{
		"Destination":       "Battlefield",
		"ValidCard":         "Card.Self",
		"ReplacementResult": "Updated",
		"Keyword":           "Bloodthirst",
		"KeywordLine":       "Bloodthirst:" + param,
	},
		With: body,
	}
	if param == "X" {
		body.Params["CounterNum"] = "Count$DamageOppsTakenThisTurn"
	} else {
		n, err := strconv.Atoi(param)
		if err != nil || n <= 0 {
			return nil
		}
		body.Params["CounterNum"] = strconv.Itoa(n)
		r.Params["CheckSVar"] = "Count$DamageOppsTakenThisTurn"
		r.Params["SVarCompare"] = "GT0"
	}
	return &replMatch{id: ev.Obj, repl: r}
}

// sunburstEntryMatch builds the synthetic Moved replacement a permanent with
// sunburst enters by (CR 702.47: "This object enters with a +1/+1 counter on
// it for each color of mana spent to cast it. If it isn't a creature, it
// instead enters with that many charge counters on it.").
//
// The keyword is read from the entering object's DERIVED keyword list
// (derivedKeywordParam), but this synthetic covers the layer-6
// `DB$ Animate | Keywords$ Sunburst` GRANT shape only (Solar Array, Lux
// Artillery): a PRINTED K:Sunburst line is expanded cards-side
// (cards/kw_sunburst.go) onto the face's own Repls, which the face-Repl scan
// above already collects, so the printed-face check below skips it -- a
// synthetic on top of the expansion would put the entry counters twice. The
// counter KIND follows Forge's own Sunburst expansion
// (CardFactoryUtil: `host.isCreature() ? P1P1 : CHARGE`), decided from the
// entering object's PRINTED face (CR 702.47a's "if it isn't a creature" is
// evaluated on the card's own types, ignoring type-changing effects), so a
// creature gets +1/+1 counters and an artifact gets charge counters.
//
// The count is the existing CR 107.4f converge head: the number of DISTINCT
// colours spent to cast the spell, carried on the object as ConvergeColours
// by the pay-time FlagConverged CastInfo (rules/cast.go's faceWantsConverge
// gate, widened to cover sunburst's cast faces). An inline Count body keeps
// this a one-line body with no SVar minted on the face.
func (e *Engine) sunburstEntryMatch(ev events.Event) *replMatch {
	if _, ok := e.derivedKeywordParamH(ev.Obj, kwhSunburst); !ok {
		return nil
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil {
		return nil
	}
	if o.Face().HasKeyword("Sunburst") {
		// Printed K:Sunburst: already expanded cards-side (cards/kw_sunburst.go);
		// the face-Repl scan collected it. The grant shape's printed face never
		// carries the line, so this gate admits only the granted case.
		return nil
	}
	kind := "CHARGE"
	if o.Face().IsCreature() {
		kind = "P1P1"
	}
	body := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined":     "Self",
		"CounterType": kind,
		"CounterNum":  "Count$Converge",
		"ETB":         "True",
	}}
	r := &cards.Repl{Event: "Moved", Params: map[string]string{
		"Destination":       "Battlefield",
		"ValidCard":         "Card.Self",
		"ReplacementResult": "Updated",
		"Keyword":           "Sunburst",
		"KeywordLine":       "Sunburst",
	}, With: body}
	return &replMatch{id: ev.Obj, repl: r}
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
	for _, ce := range e.active() {
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
		} else if ce.ReplacementBody == "" && (strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Layer"]), "CantHappen") ||
			(event == "DamageDone" && strings.EqualFold(ce.ReplacementParams["Prevent"], "True"))) {
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
	e.forEachReplacementSource(func(id state.ObjID) {
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
		if m.repl.Params["ReplacementResult"] != "Updated" {
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
			if strings.EqualFold(m.repl.Params["Prevent"], "True") {
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

// damageReplacementPrevents reports whether this replacement is a
// PREVENTION body: either the legacy Prevent$ True shape or a DB$
// ReplaceDamage body, which subtracts its Amount from the held damage event
// and prevents exactly that much (the Thunderstaff/Battletide shield
// family). stat:CantPreventDamage must exclude BOTH shapes, so every
// damage-replacement selection and application path classifies prevention
// through this one predicate and cannot drift apart.
func damageReplacementPrevents(r cards.Repl) bool {
	// Case-insensitive (dponce1 r2): the registration (effEffect's
	// replacementLinePrevents) and the collection
	// (applyReplacementsDispatch) read the param with EqualFold, so this
	// classifier must too — a non-canonical `Prevent$ true` bodyless
	// registration would otherwise be admitted to the competition and then
	// silently erased by the With==nil CantHappen drop arm.
	if strings.EqualFold(r.Params["Prevent"], "True") {
		return true
	}
	return r.With != nil && r.With.API == "ReplaceDamage"
}

func (e *Engine) applicableDamageReplacements(ev events.Event, matches []replMatch) []replMatch {
	out := matches[:0]
	for _, m := range matches {
		// CR 616.1's recheck must use the same matcher class the initial
		// collection used: an Effect-created match's lifetime is active()'s,
		// not its source's zone (task wildgrowth1), so re-gating it on
		// ActiveZones$ here would silently drop every Effect-granted
		// DamageDone replacement the scan just admitted (Taii Wakeen).
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if damageReplacementPrevents(*m.repl) && e.cantPreventDamage(e.damaging, ev.Obj) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (e *Engine) damageAffectedPlayer(ev events.Event) (state.PlayerID, bool) {
	if ev.Obj == 0 {
		return ev.Player, int(ev.Player) < len(e.G.Players)
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Controller) >= len(e.G.Players) {
		return 0, false
	}
	return o.Controller, true
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
	sc := e.withNames(effects.SpecContext{You: you, Source: source})
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
		if strings.EqualFold(m.repl.Params["Optional"], "True") {
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
	if strings.TrimSpace(r.Params["OptionalDecider"]) != "You" {
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
	if len(matches) == 1 && strings.EqualFold(matches[0].repl.Params["Optional"], "True") {
		if dp, ok := e.replacementOptionalDecider(*matches[0].repl, matches[0].id); ok &&
			!e.G.Players[dp].Lost {
			return dp
		}
	}
	return affected
}

// replaceDamageAmount resolves a DB$ ReplaceDamage body's Amount$ in the
// replacement source's context: the corpus's prevention-shield family prices
// it with a literal (Thunderstaff's 1), an SVar (Battletide Alchemist's
// AlchemicX, the card-defined ShieldAmount of Forcefield's "prevent all but
// 1") or an inline expression. The bool distinguishes an unresolvable value
// frame (Power Leak's PaidAmount) -- which the match gate turns into a
// non-match -- from a resolvable amount of zero, which legitimately prevents
// nothing.
func (e *Engine) replaceDamageAmount(ev events.Event, m replMatch) (int32, bool) {
	if m.repl.With == nil || m.repl.With.API != "ReplaceDamage" {
		return 0, false
	}
	return effects.NumResolved(e, e.replCtx(m, ev), m.repl.With, "Amount", 0)
}

// applyReplaceDamageBody applies a DB$ ReplaceDamage body to the held damage
// event: it subtracts the body's Amount from the event's remaining amount and
// reports whether the event is TERMINAL (fully prevented -- the prevention
// Note this records is the log's witness, and no reduced Damage event is
// emitted) or still stands with its reduced amount for the next modifier in
// the chain (CR 616.1e: each later opportunity reads the changed event). A
// resolvable amount of zero prevents nothing and leaves the event standing.
// The body's SubAbility$ chain is deliberately NOT run here: every corpus
// body that carries one (Divine Deflection's counter-deal, Forcefield's
// self-exile) is an Effect-shield shape whose sub reads bindings this
// per-event application does not have, and running them would fire a
// wrong-outcome rider -- the prevention itself is the correct core.
func (e *Engine) applyReplaceDamageBody(ev *events.Event, m replMatch) bool {
	n, ok := e.replaceDamageAmount(*ev, m)
	if !ok || n <= 0 {
		return false
	}
	prevented := n
	if prevented > ev.Amount {
		prevented = ev.Amount
	}
	ev.Amount -= prevented
	who := "a replacement effect"
	if o := e.G.Obj(m.id); o != nil && o.Face() != nil && o.Face().Name != "" {
		who = o.Face().Name
	}
	e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
		Amount: prevented,
		Text:   who + " prevented " + strconv.Itoa(int(prevented)) + " of the damage"})
	// The shield bookkeeping (effects' PreventDamage registration): deplete
	// the matched shield's pool by what this application prevented and run
	// its registered PreventionSubAbility$ rider. A PRINTED ReplaceDamage
	// body (Thunderstaff) carries no pool and no rider: m.key is empty.
	e.applyReplaceDamageTail(m, prevented, ev)
	// Obj carries the damaged object (0 for a player hit) like the full-
	// prevention arm's Note above, not the preventing source: the log text
	// names the preventer, and Mode$ DamagePreventedOnce triggers key their
	// ValidTarget$ on the damaged side. (Prevention Notes carried no Amount
	// before dponce1 and zero prevention-text events are logged in the
	// golden-shape games, so the field's presence is stream-neutral there.)
	return ev.Amount <= 0
}

// applyReplaceDamageTail is the bookkeeping an Effect-created prevention
// shield owes after one application (a printed ReplaceDamage body carries no
// pool and no rider and never reaches here -- m.key is empty):
//
//   - DEPLETION: "prevent the next N" is a total across events (CR 615), so
//     the matched shield's ChosenNumber pool — the binding its body's
//     Amount$ Count$ChosenNumber reads both at match time and at application
//     time — is decremented by what this application prevented, and the
//     shield is dropped from the registry the moment the pool is spent. The
//     mutation is engine-runtime (like every ContinuousEffect field),
//     deterministic, and rebuilt identically by replay's re-execution; the
//     continuousVersion bump keeps active()'s cache honest for the CR 616.1e
//     rechecks the same emit may still run.
//
//   - THE RIDER: a shield registered with PreventionSubAbility$ (Acolyte's
//     Reward, Vengeful Archon) runs that sub once per application, with the
//     amount this application prevented bound as NumDmg$ PreventedDamage and
//     the parent SA's targets (ShieldEffectTarget$ ParentTarget) bound as
//     the resolution's Remembered list (the sub's Defined$ ShieldEffectTarget
//     is rewritten to the known Remembered selector). The sub runs inside
//     the replacement re-entrancy guard, so its own emissions (the
//     retribution DealDamage) are ordinary events: replacements and triggers
//     see them, and a shield scoped to the rider's own recipient terminates
//     because the pool it just spent does not refill.
func (e *Engine) applyReplaceDamageTail(m replMatch, prevented int32, ev *events.Event) {
	if m.key == "" || prevented <= 0 {
		return
	}
	source, ts, ok := parseEffectKey(m.key)
	if !ok {
		return
	}
	rider := ""
	var objs []state.ObjID
	var players []state.PlayerID
	idx := -1
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.Source != source || ce.Timestamp != ts || ce.ReplacementEvent != "DamageDone" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["PreventionShield"]), "True") {
			continue
		}
		idx = i
		ce.ChosenNumber -= prevented
		rider = strings.TrimSpace(ce.ReplacementParams["PreventionSubAbility"])
		objs = append([]state.ObjID(nil), ce.ShieldTargets...)
		players = append([]state.PlayerID(nil), ce.ShieldTargetPlayers...)
		break
	}
	if idx < 0 {
		return
	}
	if e.continuous[idx].ChosenNumber <= 0 {
		e.continuous = append(e.continuous[:idx], e.continuous[idx+1:]...)
	}
	e.continuousVersion++
	if rider == "" {
		return
	}
	e.runPreventionShieldRider(m, rider, objs, players, prevented, ev)
}

// runPreventionShieldRider resolves one PreventionSubAbility$ application.
// Only the corpus's DB$ DealDamage rider is resolved (both carriers:
// Acolyte's Retribution, Archon's Vengeance); any other API is loud and the
// shield's prevention itself stands.
func (e *Engine) runPreventionShieldRider(m replMatch, name string,
	objs []state.ObjID, players []state.PlayerID, prevented int32, ev *events.Event) {
	f := m.face
	if f == nil {
		if o := e.G.Obj(m.id); o != nil {
			f = o.Face()
		}
	}
	if f == nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented PreventionSubAbility$ " + name + " (source face gone)"})
		return
	}
	sub := cards.ResolveSVar(f.SVars, name)
	if sub == nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented PreventionSubAbility$ " + name + " (SVar unresolved)"})
		return
	}
	if sub.API != "DealDamage" {
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented PreventionSubAbility$ " + name + " (" + sub.API + ")"})
		return
	}
	// ResolveSVar parses fresh on every call, so the rewrite below cannot
	// corrupt a shared parsed graph; the copy keeps that guarantee explicit.
	rsub := *sub
	rsub.Params = make(map[string]string, len(sub.Params))
	for k, v := range sub.Params {
		rsub.Params[k] = v
	}
	if strings.TrimSpace(rsub.Params["NumDmg"]) == "PreventedDamage" {
		rsub.Params["NumDmg"] = strconv.Itoa(int(prevented))
	}
	if strings.TrimSpace(rsub.Params["Defined"]) == "ShieldEffectTarget" {
		rsub.Params["Defined"] = "Remembered"
	}
	ctx := e.replCtx(m, *ev)
	ctx.Remembered = nil
	for _, id := range objs {
		ctx.Remembered = append(ctx.Remembered, state.Target{Obj: id})
	}
	for _, p := range players {
		ctx.Remembered = append(ctx.Remembered, state.Target{Player: p, IsPlayer: true})
	}
	// The rider's own emissions are ordinary events; nil ev (no action
	// marker, no held-event rewrite surface) keeps them from reading the
	// damage event this shield was applying to.
	e.runReplaceWith(ctx, ev.Obj, &rsub, nil)
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

// drawMatchAmount is the replacement-CONTEXT amount of a Draw event: a
// per-card Draw carries no Amount, and the body view of "the number of cards
// this draw would draw" is 1 (Quantum Riddler's NumCards$
// ReplaceCount$Number/Plus.1). The EMITTED event is never touched -- this is
// a context read only -- which is what keeps every unrelated game's Draw
// events byte-identical.
func drawMatchAmount(ev events.Event) int32 {
	if ev.Kind == events.Draw && ev.Amount == 0 {
		return 1
	}
	return ev.Amount
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

// extraTurnSkipped reports whether a live R:Event$ BeginTurn replacement
// would skip the extra turn `seat` is about to begin (Trouble in Pairs,
// Stranglehold, Ugin's Nexus, Gerrard's Hourglass Pendant; CR 500.7's "that
// player skips it instead" reading of R:Event$ BeginTurn | ExtraTurn$ True |
// Skip$ True). There is no per-turn "would begin" log event to hang
// replacement matching on, so the helper poses a SYNTHETIC
// events.ExtraTurn{Amount: 0, Player: seat} event to the ordinary matcher --
// the ActiveZones$ gate, the ValidPlayer$ read and replacementConditionHolds
// are then the shared ones and cannot drift from the other replacement
// families. The read is pure: it emits nothing, and the caller owns every
// event (including the loud Note for a matched ExtraTurn$ line whose action
// this build does not implement -- Skip$ absent, or a ReplaceWith$ body --
// reported in the second return so the turn proceeds loudly rather than
// being skipped silently).
func (e *Engine) extraTurnSkipped(seat state.PlayerID) (skip, unsupported bool) {
	ev := events.Event{Kind: events.ExtraTurn, Player: seat}
	e.forEachReplacementSource(func(id state.ObjID) {
		f := e.replacementFace(id, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event != "BeginTurn" || !e.replacementMatches(*r, id, ev) {
				continue
			}
			if r.Params["Skip"] == "True" && r.With == nil {
				skip = true
			} else {
				unsupported = true
			}
		}
	})
	return skip, unsupported
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
	return o.Face()
}

// continueUntapReplacements applies the sole replacement automatically, but
// parks a competition for the untapped permanent's controller. Each Untap
// replacement prevents the original event (and may run its ReplaceWith$), so
// the chosen effect completes the event and the others get no second pass.
// This is the same CR 616.1 affected-player choice as MoveZone, with Untap's
// event-specific applySimpleReplacement semantics.
func (e *Engine) continueUntapReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	if len(matches) == 1 {
		return e.applySimpleReplacement(ev, matches[0])
	}
	o := e.G.Obj(ev.Obj)
	if o != nil && int(o.Controller) < len(e.G.Players) && !e.G.Players[o.Controller].Lost {
		e.poseUntapReplacementChoice(ev, matches)
		return ev, true
	}
	// A departed affected player cannot choose; retain the deterministic scan
	// order fallback used by the other replacement competitions.
	return e.applySimpleReplacement(ev, matches[0])
}

// applySimpleReplacement handles events whose replacement prevents the event
// (no ReplaceWith$, the CantHappen form) or replaces it with a body. Untap is
// the important example: an ordinary activated DB$ Untap is outside the
// untap step and consequently does not match ValidStepTurnToController$.
func (e *Engine) applySimpleReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	return ev, true
}

// applyBeginPhaseReplacement skips the phase by emitting the following phase
// entry. The skipped StepChange never enters the log, so replay performs
// exactly the same transition without needing an ephemeral "skipped" bit in
// game state. The skip itself is emitted through the UNGUARDED emit --
// applyingReplacement is false here, outside runReplaceWith -- so a chain of
// skips (one effect skipping untap, another upkeep) keeps skipping: the step
// number strictly increases, so the recursion terminates. The landing step's
// own StepChange is the log entry that exists, and every Phase trigger sees
// exactly the steps that actually happened.
func (e *Engine) applyBeginPhaseReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), 0, m.repl.With, nil)
	}
	// Cleanup is never skipped: the turn's 514.1/514.2 work is what makes the
	// next turn begin correctly, and no corpus line names it. Bounding the
	// emission here also bounds the chain recursion.
	if ev.Step < state.StepCleanup {
		e.emit(events.Event{Kind: events.StepChange, Step: ev.Step + 1})
	}
	return ev, true
}

// continuePhaseReplacements processes every replacement applicable to one
// proposed step entry. With several effects the active (affected) player
// chooses which gets the first opportunity under CR 616.1. Choosing an
// optional effect leads to its separate apply/decline ask; declining marks
// only that effect used and continues through the remaining mandatory or
// optional effects instead of bypassing replacement matching on the parked
// StepChange. Applying any effect skips the step and completes this event.
func (e *Engine) continuePhaseReplacements(ev events.Event, candidates []replMatch, used []bool) (events.Event, bool) {
	if used == nil {
		used = make([]bool, len(candidates))
	}
	applicable := e.applicablePhaseReplacements(ev, candidates, used)
	if len(applicable) == 0 {
		return ev, false
	}
	if len(applicable) > 1 && int(e.G.Active) < len(e.G.Players) && !e.G.Players[e.G.Active].Lost {
		e.posePhaseOrderChoice(ev, candidates, used, applicable)
		return ev, true
	}
	i := applicable[0]
	if candidates[i].repl.Params["Optional"] == "True" && int(e.G.Active) < len(e.G.Players) &&
		!e.G.Players[e.G.Active].Lost {
		e.posePhaseOptionalChoice(ev, candidates, used, i)
		return ev, true
	}
	return e.applyBeginPhaseReplacement(ev, candidates[i])
}

func (e *Engine) applicablePhaseReplacements(ev events.Event, candidates []replMatch, used []bool) []int {
	var applicable []int
	for i, m := range candidates {
		if !used[i] && e.replacementMatches(*m.repl, m.id, ev) {
			applicable = append(applicable, i)
		}
	}
	return applicable
}

// finishParkedPhase settles the old step boundary exactly once, then either
// applies the selected skip or enters the original proposed step under a
// guard (all applicable replacements have already had their opportunity).
func (e *Engine) finishParkedPhase(rc replChoice, selected int) {
	if rc.boundary {
		e.finishStepBoundary(rc.leaving, rc.ev.Step)
	}
	if selected >= 0 {
		e.applyBeginPhaseReplacement(rc.ev, rc.cands[selected])
	} else {
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(rc.ev)
		e.applyingReplacement = saved
	}
	if e.pending == nil {
		e.finishEnteredStep()
	}
}

// resumeParkedPhase continues after an optional replacement was declined.
// It preserves the original boundary ownership while either posing the next
// order/optional ask or completing with the remaining mandatory replacement.
func (e *Engine) resumeParkedPhase(rc replChoice) {
	applicable := e.applicablePhaseReplacements(rc.ev, rc.cands, rc.applied)
	if len(applicable) == 0 {
		e.finishParkedPhase(rc, -1)
		return
	}
	if len(applicable) > 1 && int(e.G.Active) < len(e.G.Players) && !e.G.Players[e.G.Active].Lost {
		rc.kind = replChoicePhaseOrder
		rc.applicable = append(rc.applicable[:0], applicable...)
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		return
	}
	i := applicable[0]
	if rc.cands[i].repl.Params["Optional"] == "True" && int(e.G.Active) < len(e.G.Players) &&
		!e.G.Players[e.G.Active].Lost {
		rc.kind = replChoicePhaseOptional
		rc.selected = i
		rc.applicable = nil
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		return
	}
	e.finishParkedPhase(rc, i)
}

// applyTransformReplacement lets every matching "as this transforms" body
// resolve, then leaves the FlipFace event intact. Forge writes these as an
// augmentation (Sephiroth gains its emblem as it becomes the Angel), not as
// a substitute that cancels the transformation.
func (e *Engine) applyTransformReplacement(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		if m.repl.With != nil {
			e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
		}
	}
	return ev, false
}

// applyTurnFaceUpReplacements is the TurnFaceUp event's replacement pass
// (R:Event$ TurnFaceUp, CR 614.1a with CR 708.6/702.36e, task
// cli-20260924T031747Z-6d0658fc). The bodies resolve BEFORE the turn-up event
// is folded: the counters Hooded Hydra's body places land on the permanent as
// it turns face up, and only then does events.Apply retire the CR 708.5
// face-down set. An augmenting body never prevents the flip -- Forge's
// Replaced reading of these lines: the physical turn-up belongs to the
// turn-up action/effect, the replacement only adds to it -- so after every
// body the event is left intact (handled=false) for the ordinary fold and
// its own trigger matching. A With==nil (Layer$ CantHappen) body -- Karlov
// Watchdog's "permanents your opponents control can't be turned face up",
// Unable to Scream -- IS the complete replacement: the turn-up never happens
// and a Note records the prevention, the same shape the damage-prevention
// arm emits.
func (e *Engine) applyTurnFaceUpReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		if m.repl.With == nil {
			return e.emit(events.Event{Kind: events.Note, Obj: ev.Obj,
				Text: "turn face up prevented by replacement effect"}), true
		}
	}
	if len(matches) == 1 && strings.EqualFold(matches[0].repl.Params["Optional"], "True") {
		e.replChoices = append(e.replChoices, replChoice{kind: replChoiceFaceUp, ev: ev, cands: matches, selected: 0})
		if e.pending == nil {
			e.askReplacementChoice(e.replacementOptionalDeciderOrController(matches[0]))
		}
		return ev, true
	}
	// Park the transition for the body's whole continuation: a body that asks
	// (Aquamorph Entity's GenericChoice, Gift of Doom's attach) must not let
	// the fold reveal the face before the answer, so resolveReplacementBody --
	// the sole owner of a ReplaceWith$ body's continuation linkage -- appends
	// the re-emit frame to the body's suspension chain. A body that asks
	// nothing leaves the field set and the caller's clear folds the event
	// through the ordinary path.
	e.turnUpMove = &ev
	for _, m := range matches {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
		if e.pending != nil {
			e.turnUpMove = nil
			return ev, true
		}
	}
	e.turnUpMove = nil
	return ev, false
}

func (e *Engine) replacementOptionalDeciderOrController(m replMatch) state.PlayerID {
	if p, ok := e.replacementOptionalDecider(*m.repl, m.id); ok {
		return p
	}
	return e.controllerOf(m.id)
}

// turnFaceUpCantHappen is the ONE legality predicate for a blocked turn-up:
// it reports whether a live R:Event$ TurnFaceUp CantHappen replacement (a
// printed line with no ReplaceWith$, or an Effect-created bodyless
// registration with Layer$ CantHappen) would prevent id's turn-up outright.
// The special action is then ILLEGAL (CR 614.1a's "can't" stops the action
// before it starts), so the offer (rules/legal.go) and the submitted-option
// guard (the same file's validate switch) both consult this one predicate
// instead of re-implementing the match, and the dispatch's CantHappen arm
// above stays as defence-in-depth for emit routes that never consult the
// offer (an effect-driven SetState turn-up).
func (e *Engine) turnFaceUpCantHappen(id state.ObjID) bool {
	ev := events.Event{Kind: events.TurnFaceUp, Obj: id}
	for _, ce := range e.active() {
		if ce.ReplacementEvent != "TurnFaceUp" || ce.ReplacementBody != "" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Layer"]), "CantHappen") {
			continue
		}
		r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
		if e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) {
			return true
		}
	}
	blocked := false
	e.forEachReplacementSource(func(source state.ObjID) {
		if blocked {
			return
		}
		f := e.replacementFace(source, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if !replacementEventNameMatches(r.Event, "TurnFaceUp") || r.With != nil {
				continue
			}
			if e.replacementMatches(*r, source, ev) {
				blocked = true
				return
			}
		}
	})
	return blocked
}

// continueManaReplacements implements CR 616.1 for one in-flight ManaAdd.
// Each replacement may apply once. After every rewrite the full candidate set
// is re-checked against the NEW amount/type; if several apply, the player
// receiving the mana chooses which is applied next. A lone applicable effect
// is automatic. Only the final rewritten ManaAdd enters the event log, so a
// log-only replay needs no transient provenance or replacement state.
func (e *Engine) continueManaReplacements(ev events.Event, candidates []replMatch,
	applied []bool, changed, tapped bool, producer state.ObjID) (events.Event, bool) {
	if applied == nil {
		applied = make([]bool, len(candidates))
	}
	// A colour/order answer resumes after resolveManaEffectColor restored its
	// synchronous scratch. Rebind producer for every applicability recheck so
	// a parked replacement still sees the permanent that produced this mana.
	savedProducer := e.manaProducer
	e.manaProducer = producer
	defer func() { e.manaProducer = savedProducer }()
	for {
		var applicable []int
		savedTap := e.manaFromTap
		e.manaFromTap = tapped
		for i, m := range candidates {
			if !applied[i] && e.replacementMatches(*m.repl, m.id, ev) {
				applicable = append(applicable, i)
			}
		}
		e.manaFromTap = savedTap
		if len(applicable) == 0 {
			if !changed {
				return ev, false
			}
			stored := events.Emit(e.G, e.L, ev)
			e.loop.observeFrom(stored, e.damaging, len(e.G.Objs))
			e.checkTriggers(stored, nil, 0, 0, false)
			return stored, true
		}
		if len(applicable) > 1 && int(ev.Player) < len(e.G.Players) && !e.G.Players[ev.Player].Lost {
			e.poseManaReplacementChoice(ev, candidates, applied, applicable, changed, tapped, producer)
			return ev, true
		}
		// A sole applicable replacement is mandatory. A choice-valued colour
		// still belongs to the affected player, so park the in-flight event
		// before applying it. A departed player cannot answer and deterministically
		// takes the first colour, just as it takes the first competing effect.
		i := applicable[0]
		if manaReplacementNeedsColor(candidates[i]) && int(ev.Player) < len(e.G.Players) &&
			!e.G.Players[ev.Player].Lost {
			e.poseManaColorReplacementChoice(ev, candidates, applied, i, changed, tapped, producer)
			return ev, true
		}
		choice := ""
		if manaReplacementNeedsColor(candidates[i]) {
			choice = "W"
		}
		ev = e.applyOneManaReplacement(ev, candidates[i], choice)
		applied[i] = true
		changed = true
	}
}

// applyOneManaReplacement applies a body while its producer is bound in
// Engine scratch by continueManaReplacements (or by the resume wrapper).
func (e *Engine) applyOneManaReplacement(ev events.Event, m replMatch, color string) events.Event {
	if m.repl.With == nil {
		return ev
	}
	ctx := &effects.Ctx{Source: m.id, Controller: e.controllerOf(m.id),
		ManaAmount: ev.Amount, ManaType: ev.Counter, ManaChoice: color}
	if m.face != nil {
		effects.SetSVars(ctx, m.face.SVars)
	}
	// The producer is contextual (not ManaAdd.Obj), but ReplaceWith$ still
	// resolves against that object for Defined$/Remembered$ references.
	e.runReplaceWith(ctx, e.manaProducer, m.repl.With, nil)
	ev.Amount, ev.Counter = ctx.ManaAmount, ctx.ManaType
	return ev
}

// applyOneManaReplacementWithProducer restores the contextual producer for
// the one rewrite that occurs immediately after an answered replacement
// decision; continueManaReplacements then rebinds it for later rechecks.
func (e *Engine) applyOneManaReplacementWithProducer(ev events.Event, m replMatch, color string, producer state.ObjID) events.Event {
	saved := e.manaProducer
	e.manaProducer = producer
	defer func() { e.manaProducer = saved }()
	return e.applyOneManaReplacement(ev, m, color)
}

// manaReplacementNeedsColor identifies every choice-valued spelling the
// ReplaceMana primitive accepts. It follows effReplaceMana's precedence
// (ReplaceMana, then ReplaceType, then ReplaceColor), so an ignored lower-
// precedence parameter cannot accidentally pose a second choice.
func manaReplacementNeedsColor(m replMatch) bool {
	if m.repl == nil || m.repl.With == nil {
		return false
	}
	p := m.repl.With.Params
	kind := strings.TrimSpace(p["ReplaceMana"])
	if kind == "" {
		kind = strings.TrimSpace(p["ReplaceType"])
	}
	if kind == "" {
		kind = strings.TrimSpace(p["ReplaceColor"])
	}
	return strings.EqualFold(kind, "Any") || strings.EqualFold(kind, "Chosen")
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
		ctx := &effects.Ctx{Source: m.id, ReplacementTarget: target,
			ReplacementSource: e.protectionSource(e.damaging), ReplacementAmount: drawMatchAmount(ev)}
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
			ctx.ExcludeFromBattlefieldCount = ev.Obj
		}
		e.seedEffectReplCtx(ctx, m)
		return ctx
	}
	controller := o.Controller
	if m.frozenController {
		controller = m.controller
	}
	ctx := &effects.Ctx{Source: m.id, Controller: controller,
		ReplacementTarget: target, ReplacementSource: e.protectionSource(e.damaging),
		ReplacementAmount: drawMatchAmount(ev),
		// X is the {X} paid for the moving object, so an ETB replacement that
		// reads it (etbCounter's CounterNum$ X, e.g. Endless One / Walking
		// Ballista / Chalice of the Void) sees the value the player actually
		// chose. Move preserves X from the stack onto the permanent (events/
		// apply.go, the "hand/stack -> battlefield must NOT reset them"
		// comment), so o.X is the cast-time value here.
		X:        o.X,
		Captured: []state.Target{{Obj: ev.Obj}},
		// Replaced names the object the replaced event (ev) was about, so a
		// ReplaceWith$ that says Defined$ ReplacedCard (the Rest in Peace /
		// Dryad Militant / Leyline of the Void shape: "exile it instead") can
		// act on exactly the card being kept out of the graveyard -- not the
		// source that owns the replacement.
		Replaced: ev.Obj}
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
		ctx.ExcludeFromBattlefieldCount = ev.Obj
	}
	// The cascade instruction's ordered exiled batch, the plural referent
	// Averna's ReplaceWith$ body reads as Defined$ ReplacedCards.<qual>. Only a
	// Cascade proposal carries it; every other replacement leaves the field
	// empty (the singular Replaced seed above is unchanged).
	if ev.Kind == events.Cascade {
		ctx.ReplacedCards = append([]state.ObjID(nil), ev.IDs...)
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
		ctx.ReplacedPlayer = state.Target{Player: ev.Player, IsPlayer: true}
		ctx.Remembered, ctx.Captured = nil, nil
	}
	if f != nil {
		effects.SetSVars(ctx, f.SVars)
	}
	if o != nil && m.repl != nil && m.repl.Params["Keyword"] == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "Clone" {
		ctx.CloneETB = true
		ctx.CloneBecome = ev.Obj
		ctx.CloneBecomeValid = true
		ctx.CloneChoiceValid = o.ETBCloneChoiceValid
		ctx.CloneChoice = o.ETBCloneChoice
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
	if o != nil && m.repl != nil && m.repl.Params["Keyword"] == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "ChooseColor" {
		ctx.ETBColorRecorded = true
	}
	// The as-enters NUMBER-choice body (K:ETBReplacement:Other:ChooseNumber,
	// Talion the Kindly Lord) marks itself the same way: resumeETBEntry already
	// posed the entry ask and recorded the answer on the entering object, so
	// effChooseNumber keeps the historical no-op for THIS invocation rather
	// than posing a second ask (task cli-20260923T060000Z-choose-number). The
	// flag is exact where a bare o.ChosenNumber guard is not: a recorded entry
	// answer of 0 is indistinguishable from unset on the object, but the flag
	// is set precisely when the machinery recorded one.
	if o != nil && m.repl != nil && m.repl.Params["Keyword"] == "ETBReplacement" && m.repl.With != nil && m.repl.With.API == "ChooseNumber" {
		ctx.ETBNumberRecorded = true
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
	ctx.ChosenNumber = m.chosen
	// The bound flag is the Count$ChosenNumber head's verdict: effect-created
	// only, so a printed or choose-event context stays UNRESOLVED and the
	// EvalCountOK consumers keep their fail direction (see Ctx.ChosenNumberBound).
	ctx.ChosenNumberBound = m.key != ""
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
			if name := strings.TrimSpace(with.Params["SubAbility"]); name != "" {
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
		if name := strings.TrimSpace(with.Params["SubAbility"]); name != "" {
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
		replaced, ev, ctx.Source, action, ctx.ReplacedPlayer
	if ctx != nil {
		e.replReplacedCards = append([]state.ObjID(nil), ctx.ReplacedCards...)
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
	if e.contChainOwners > 0 {
		e.resolveReplacementWith(ctx, with)
		return
	}
	savedParked := e.answerParked
	if e.resume != nil && e.pending == nil {
		e.answerParked = e.resume
	}
	defer func() { e.answerParked = savedParked }()
	if with.API == "ReplaceEffect" {
		// Rewrites the held event synchronously and never asks, so it runs in
		// place. No pass owns e.contChain here: a report its walk makes on a
		// suspension it did not cause (an earlier body's still-pending ask)
		// belongs to no chain, so it must not survive in the per-pass scratch.
		savedChain, savedReported := e.contChain, e.repeatReported
		e.contChain, e.repeatReported = nil, nil
		e.resolveReplacementWith(ctx, with)
		e.contChain, e.repeatReported = savedChain, savedReported
		return
	}
	if e.resume != nil && e.pending != nil {
		// buildContinuationChain resumes each frame at sa.Sub, so a parent
		// whose Sub is the body makes the frame run the body from its start.
		// Its replacement context (replaced object, damage target and amount,
		// damage source) is read from the live replacement state here.
		frame := e.buildContinuationChain([]contFrame{{sa: &cards.SA{Sub: with}}}, ctx.Source, nil)
		tail := e.resume
		for tail.outer != nil {
			tail = tail.outer
		}
		tail.outer = frame
	} else {
		savedChain, savedReported := e.contChain, e.repeatReported
		e.contChain, e.repeatReported = nil, nil
		prior := e.resume
		e.contChainOwners++
		e.resolveReplacementWith(ctx, with)
		e.contChainOwners--
		if e.resume != nil && e.resume != prior && len(e.contChain) > 0 {
			e.resume.outer = e.buildContinuationChain(e.contChain, ctx.Source, e.resume.outer)
		}
		e.contChain, e.repeatReported = savedChain, savedReported
	}
	// A TurnFaceUp replacement body that posed its own answer must not let
	// the transition fold yet: park the marker's re-emit at the tail of the
	// body's suspension chain, the discipline resumeETBEntry keeps for an
	// as-enters choice (etbMove). The write lives HERE -- in the function the
	// resume-state archtest names as the owner of a ReplaceWith$ body's
	// continuation linkage -- not in the turn-up dispatch that set the field.
	if e.turnUpMove != nil && e.resume != nil {
		tail := e.resume
		for tail.outer != nil {
			tail = tail.outer
		}
		tail.outer = &resumePoint{kind: "turn_face_up_event", event: *e.turnUpMove}
		e.turnUpMove = nil
	}
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
		return ev, false
	}
	ctx := e.replCtx(m, ev)
	if m.repl.Params["ReplacementResult"] == "Updated" {
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
		e.loop.observeFrom(stored, e.damaging, len(e.G.Objs))
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
		e.checkTriggers(stored, nil, 0, 0, false)
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

// cascadeReplacementEvent builds the synthetic Cascade proposal
// ProposeCascadeReplacement holds out to the replacement matcher. It is never
// logged: the IDs carry the ordered exiled batch the body's ReplacedCards
// selector reads, and Obj is the cascade spell whose trigger is resolving.
func cascadeReplacementEvent(source state.ObjID, controller state.PlayerID, batch []state.ObjID) events.Event {
	return events.Event{Kind: events.Cascade, Obj: source, Player: controller,
		IDs: append([]state.ObjID(nil), batch...)}
}

// applyCascadeReplacements runs the matching Cascade replacement bodies against
// the held instruction proposal. There is no original event to emit (the
// cascade instruction's remainder is the caller's residue), so unlike
// applyReplacement this never folds or logs the proposal. The LAST body gets
// the residue chained after it (via runCascadeReplaceWith) so the bottoming
// and the free-cast election run once, after the body -- or after it resumes,
// when the body suspends at its hidden land pick.
func (e *Engine) applyCascadeReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	residue := e.cascadeResidue
	if residue == nil {
		return ev, false
	}
	var bodies []replMatch
	for _, m := range matches {
		if m.repl.With != nil {
			bodies = append(bodies, m)
		}
	}
	if len(bodies) == 0 {
		return ev, false
	}
	for i, m := range bodies {
		var tail *cards.SA
		if i == len(bodies)-1 {
			tail = residue
		}
		ctx := e.replCtx(m, ev)
		e.runCascadeReplaceWith(ctx, ev.Obj, m.repl.With, tail)
	}
	return ev, true
}

// runCascadeReplaceWith resolves one Cascade replacement body with an optional
// residue SA chained at the end of its SubAbility$ chain. The chain is
// DEEP-COPIED before the tail is attached: the body is shared, immutable
// corpus data, and mutating a shared SA's Sub would corrupt every later use of
// the same script.
func (e *Engine) runCascadeReplaceWith(ctx *effects.Ctx, replaced state.ObjID, with *cards.SA, residue *cards.SA) {
	body := with
	if residue != nil {
		body = cloneChainWithTail(with, residue)
	}
	e.runReplaceWith(ctx, replaced, body, nil)
}

// cloneChainWithTail returns a copy of the SubAbility$ chain rooted at sa with
// tail appended after its last link. Only the SA structs along Sub are copied;
// Params, Line and the compiled fields are shared (the chain is immutable).
func cloneChainWithTail(sa *cards.SA, tail *cards.SA) *cards.SA {
	cp := *sa
	if sa.Sub == nil {
		cp.Sub = tail
	} else {
		cp.Sub = cloneChainWithTail(sa.Sub, tail)
	}
	return &cp
}

// CascadeReplacement implements effects.Host. It holds one synthetic
// events.Cascade proposal out to the ordinary replacement collection and runs
// the matching bodies, returning whether anything matched. e.cascadeResidue
// carries the caller's residue through the synchronous dispatch (the same
// scoped-scratch pattern as e.scrySA/e.scryTarget in Engine.Scry).
func (e *Engine) CascadeReplacement(source state.ObjID, controller state.PlayerID, batch []state.ObjID, residue *cards.SA) bool {
	if e.applyingReplacement {
		return false
	}
	saved := e.cascadeResidue
	e.cascadeResidue = residue
	_, handled := e.applyReplacements(cascadeReplacementEvent(source, controller, batch))
	e.cascadeResidue = saved
	return handled
}

// replRedirect records one destination-changing move replacement in flight:
// the move it replaced and the identities (replIdentity) of every
// replacement already applied to that event.
type replRedirect struct {
	orig    events.Event
	applied []string
}

// replIdentity names one replacement match stably across re-collection: an
// Effect-created one by its registration key, a printed one by its source
// object and line.
func replIdentity(m replMatch) string {
	if m.key != "" {
		return m.key
	}
	return strconv.Itoa(int(m.id)) + "|" + m.repl.Event + "|" + m.repl.Params["ReplaceWith"] +
		"|" + m.repl.Params["Description"]
}

// redirectRecheck reports whether a move emitted inside a replacement body is
// the MODIFIED event of the in-flight destination-changing replacement: the
// replaced object going to a zone other than the one it originally would
// have. CR 616.1f: once a replacement has applied, any other replacement
// that now applies to the modified event gets its opportunity -- Magus of
// the Will exiling the Mox Diamond its own replacement puts into the
// graveyard. The one already applied never re-applies (CR 614.5).
func (e *Engine) redirectRecheck(ev events.Event) bool {
	r := e.replRedirect
	return r != nil && ev.Kind == events.MoveZone && r.orig.Kind == events.MoveZone &&
		ev.Obj == r.orig.Obj && ev.To != r.orig.To
}

// applyRedirectReplacements is the CR 616.1f pass over a redirect's modified
// move, skipping every replacement already applied to it.
func (e *Engine) applyRedirectReplacements(ev events.Event) (events.Event, bool) {
	savedExclude, savedRedirect := e.replExclude, e.replRedirect
	e.replExclude = e.replRedirect.applied
	e.replRedirect = nil
	replaced, handled := e.applyReplacements(ev)
	e.replExclude, e.replRedirect = savedExclude, savedRedirect
	return replaced, handled
}

// dropAppliedReplacements removes the matches a CR 616.1f recheck excludes.
func (e *Engine) dropAppliedReplacements(matches []replMatch) []replMatch {
	if len(e.replExclude) == 0 {
		return matches
	}
	out := matches[:0:0]
	for _, m := range matches {
		skip := false
		id := replIdentity(m)
		for _, x := range e.replExclude {
			if x == id {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, m)
		}
	}
	return out
}

// composeUpdatedReplacements applies every applicable "Updated" replacement
// to one MoveZone event: the original event is emitted and triggers fire
// once, then each With is resolved in the deterministic scan order (CR
// 616.1f, each applicable replacement gets one opportunity). This is the
// composition a competing set of entry replacements needs -- a permanent that
// both "enters tapped" (Blind Obedience) and "enters with three +1/+1
// counters" (Triskelion) finishes BOTH attrs set, not whichever the scan
// reached first.
func (e *Engine) composeUpdatedReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	// CR 616.1's order choice for the one Updated shape where the order can
	// change the result: a tap and an untap competing over the same tapped
	// bit (the last body applied wins), or any body the commute
	// classification cannot name. The pure-augment competitions (taps with
	// PutCounter riders) land the same result in every order, so no decision
	// nobody answers differently is posed. The pose is a queue append: a
	// competition that arrived while another decision was outstanding parks
	// behind it and is asked when the queue drains (Submit's tail), and a
	// competition whose affected controller has left the game makes no
	// choices (CR 800.4a) and falls through to the deterministic scan-order
	// composition below.
	var cands []replMatch
	for _, m := range matches {
		if m.repl.With != nil {
			cands = append(cands, m)
		}
	}
	if p, ok := e.moveAffectedPlayer(ev); ok && len(cands) > 1 && !updatedReplacementsCommute(cands) {
		e.replChoices = append(e.replChoices, replChoice{kind: replChoiceUpdated,
			ev: ev, cands: cands, before: e.triggerBefore,
			damaging: e.damaging, combatDamaging: e.combatDamaging, dmgSrcOverride: e.dmgSrcOverride,
			inResolution: e.resolvingObj != 0 || e.answerInResolution})
		if e.pending == nil {
			e.askReplacementChoice(p)
		}
		return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "entry awaiting replacement-order choice"}, true
	}
	departing, link, controller := e.captureSourceLifelinkLKI(ev)
	stored, absorbed := e.foldEntryMove(ev)
	e.loop.observeFrom(stored, e.damaging, len(e.G.Objs))
	// The move-driven Effect lifetimes, replayed inline exactly as the
	// single-match Updated branch does (the raw events.Emit above bypasses
	// Engine.emit's own sweep point).
	e.effectMoveSweep(ev)
	e.finishSourceLifelinkLKI(ev, departing, link, controller)
	for _, m := range matches {
		if m.repl.With == nil {
			continue
		}
		if bodyAbsorbed(absorbed, m) {
			// Its PutCounter|ETB$ True placement was folded into the move's
			// Pairs payload (rules/entry_counters.go); running the body would
			// place the counters twice.
			continue
		}
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	// Matched after every Updated body, as in the single-match branch.
	e.checkTriggers(stored, nil, 0, 0, false)
	if e.pending == nil && stored.Kind == events.MoveZone && stored.To == state.ZBattlefield {
		e.finishLandPlay(stored.Obj)
	}
	return stored, true
}

// moveAffectedPlayer is the CR 616.1 affected player of an object-carried
// event: its controller. The shared affected-player read of the Updated
// composition's pose and resume.
func (e *Engine) moveAffectedPlayer(ev events.Event) (state.PlayerID, bool) {
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Controller) >= len(e.G.Players) {
		return 0, false
	}
	return o.Controller, true
}

// updatedBodyClass classifies one Updated replacement's With body for the
// commute check: "tap", "untap", "counter" (a PutCounter rider), or
// "other" for anything the classification cannot name.
// updatedNeutralBody reports whether one body in a ReplaceWith$ chain
// records an as-enters CHOICE without touching any characteristic another
// Updated body could reorder: a colour or type pick, a hand reveal -- the
// answer lands in the entering object's own record (ChosenColor,
// Remembered), so composing the record before or after a tap/untap/counter
// rider changes nothing. Any other API fails closed (the caller poses).
func updatedNeutralBody(sa *cards.SA) bool {
	switch sa.API {
	case "Reveal", "ChooseColor", "ChooseType", "ChooseNumber", "ChooseCard", "Cleanup", "Hideaway":
		return true
	}
	return false
}

func updatedBodyClass(m replMatch) string {
	body := m.repl.With
	if body == nil {
		return ""
	}
	// The class of a whole chain is the most order-sensitive member of it:
	// a Reveal whose SubAbility$ chain ends in a conditional Tap fights an
	// untap competitor as a tap does (the conditional depends only on the
	// chain's own recorded choice, but the conservative direction is to
	// pose). A chain of neutral recording bodies alone commutes with
	// everything ("record").
	tap, untap := false, false
	for sa := body; sa != nil; sa = sa.Sub {
		if updatedNeutralBody(sa) {
			continue
		}
		switch sa.API {
		case "Tap":
			tap = true
		case "Untap":
			untap = true
		case "PutCounter":
			// Additive on a fresh entry: rides with anything.
		default:
			return "other"
		}
	}
	switch {
	case tap && untap:
		// One chain tapping and untapping the same entry is order-sensitive
		// within itself; the conservative direction is to pose.
		return "other"
	case tap:
		return "tap"
	case untap:
		return "untap"
	default:
		return "record"
	}
}

// updatedReplacementsCommute reports whether the all-Updated competition's
// bodies compose order-insensitively: taps, untaps and PutCounter riders
// touch disjoint or purely additive characteristics (two "enters tapped"
// bodies land the same state either order; two PutCounter riders add),
// while a tap and an untap fight over the same tapped bit and any body the
// classification cannot name is conservatively order-sensitive.
func updatedReplacementsCommute(matches []replMatch) bool {
	tap, untap := false, false
	for _, m := range matches {
		switch updatedBodyClass(m) {
		case "tap":
			tap = true
		case "untap":
			untap = true
		case "counter", "record":
		default:
			return false
		}
	}
	return !(tap && untap)
}

// resumeUpdatedComposition continues a parked all-Updated competition: the
// original event is emitted once (the composeUpdatedReplacements preamble,
// exactly what a lone Updated replacement's applyReplacement arm does), the
// chosen body resolves, and the remaining candidates re-check their gates
// against the event as it now stands (CR 616.1e) before the composition
// either re-poses a live non-commuting remainder or finishes it in
// deterministic scan order.
func (e *Engine) resumeUpdatedComposition(rc replChoice, selected int) {
	if !rc.emitted {
		departing, link, controller := e.captureSourceLifelinkLKI(rc.ev)
		// The fold, not a raw Emit: the original event may carry entry-
		// characteristic counter grants (rules/entry_counters.go), which a
		// raw Emit would silently drop -- a walker entering under a parked
		// tap-vs-untap competition would land at zero loyalty. foldEntryMove
		// folds the grants with the move exactly as applyReplacement's
		// Updated arm does, and never re-runs the replacement dispatch (so
		// the just-answered competition cannot re-pose).
		stored, absorbed := e.foldEntryMove(rc.ev)
		rc.absorbed = absorbed
		e.loop.observeFrom(stored, e.damaging, len(e.G.Objs))
		// The move-driven Effect lifetimes, replayed inline exactly as the
		// synchronous composition does (see applyReplacement's Updated arm).
		e.effectMoveSweep(rc.ev)
		e.checkTriggers(stored, nil, 0, 0, false)
		e.finishSourceLifelinkLKI(rc.ev, departing, link, controller)
		rc.emitted = true
	}
	chosen := rc.cands[selected]
	if !bodyAbsorbed(rc.absorbed, chosen) {
		e.runReplaceWith(e.replCtx(chosen, rc.ev), rc.ev.Obj, chosen.repl.With, nil)
	}
	var remaining []replMatch
	for i, m := range rc.cands {
		if i == selected {
			continue
		}
		if e.replacementMatches(*m.repl, m.id, rc.ev) {
			remaining = append(remaining, m)
		}
	}
	if p, ok := e.moveAffectedPlayer(rc.ev); ok && !e.G.Players[p].Lost &&
		len(remaining) > 1 && !updatedReplacementsCommute(remaining) {
		rc.cands = remaining
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		if e.pending == nil {
			e.askReplacementChoice(p)
		}
		return
	}
	for _, m := range remaining {
		if bodyAbsorbed(rc.absorbed, m) {
			continue
		}
		e.runReplaceWith(e.replCtx(m, rc.ev), rc.ev.Obj, m.repl.With, nil)
	}
	if e.pending == nil && rc.ev.Kind == events.MoveZone && rc.ev.To == state.ZBattlefield {
		e.finishLandPlay(rc.ev.Obj)
	}
}

// continueCreateTokenReplacements applies every applicable CreateToken
// replacement to one TokenCreate event. The engine mints ONE token per
// event, so the plan starts as that single mint. Each match applies at most
// once, in deterministic scan order, and its ValidToken$ gate is re-checked
// PER PLAN MINT — a later match sees the mints earlier matches produced
// (Divine Visitation after a doubler replaces each doubled mint that is
// still a creature token), which is CR 616.1's re-application over the
// changed event. The final plan is emitted directly through events.Emit
// (+ observe + checkTriggers per mint, the composeUpdatedReplacements
// pattern), which BYPASSES applyReplacements: no mint can re-match, so a
// doubler can never loop on its own output. The deviation from CR 616.1 is
// deliberate (the CR 616.1 order choice for competing CreateToken
// replacements is a follow-up ticket, not this build): competing CreateToken
// replacements apply in
// scan order, NOT through a posed KReplacement order choice (non-commuting
// compositions are reachable in Commander, but no repo deck carries any of
// this family, so no golden game exercises one).
func (e *Engine) continueCreateTokenReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	plan, parked := e.driveTokenReplacements(ev, matches, []tokenPlanMint{{script: ev.Text}}, 0)
	if parked {
		// A mid-drive ask is outstanding: either the CR 616.1 order choice
		// (parked on the replChoices queue) or the chosen-copy body's
		// election (e.tokenChoice). The mints are emitted from the answer's
		// resume, the siegeMove/attachedChoice discipline.
		return ev, true
	}
	if len(plan) == 1 && plan[0].copyOf == 0 && plan[0].script == ev.Text && !plan[0].hasController {
		// No replacement changed the plan: the ordinary emit path logs the
		// original event untouched (with its full LKI/trigger treatment).
		return ev, false
	}
	if len(plan) == 0 {
		// Every mint was removed (halving_season's HalfDown on a one-token
		// event): the Note is the log's witness that nothing was created.
		e.emit(events.Event{Kind: events.Note, Obj: 0, Player: ev.Player,
			Text: "no tokens created (replacement effect rounded the creation down to zero)"})
		return ev, true
	}
	last := e.emitTokenPlanMints(ev, plan)
	return last, true
}

// tokenPlanMint is one planned mint of a CreateToken replacement plan: an
// ordinary scripted token (script) or a copy of a battlefield permanent
// (copyOf -- the chosen-copy body's TokenScript$ Chosen shape).
type tokenPlanMint struct {
	script string
	copyOf state.ObjID
	// controller, when hasController is set, is the player the mint enters
	// under (a Type$ ReplaceController body's NewController$ -- Crafty
	// Cutpurse's "created under your control instead"). The zero value leaves
	// the mint under the original event's player; PlayerID 0 is a real seat,
	// so the flag, not the value, is the sentinel.
	controller    state.PlayerID
	hasController bool
}

// tokenMintPlayer is the player one plan mint enters under: its own
// ReplaceController answer when it carries one, else the original event's
// creator. Every mint site and the per-mint re-match read this ONE helper so
// a controller rewrite can never drift between the emit and the recheck.
func tokenMintPlayer(mint tokenPlanMint, ev events.Event) state.PlayerID {
	if mint.hasController {
		return mint.controller
	}
	return ev.Player
}

// tokenReplApplies reports whether the drive would apply m at all: a body
// the drive does not read, the deterministic-decline optional and the
// loud-unimplemented ReplaceController are not competition candidates,
// everything else applies.
func tokenReplApplies(m replMatch) bool {
	body := m.repl.With
	if body == nil || body.API != "ReplaceToken" {
		return false
	}
	chosenShape := strings.EqualFold(strings.TrimSpace(body.Params["TokenScript"]), "Chosen") ||
		strings.TrimSpace(body.Params["ValidChoices"]) != ""
	if !chosenShape && strings.EqualFold(m.repl.Params["Optional"], "True") {
		return false
	}
	if strings.TrimSpace(body.Params["Type"]) == "ReplaceController" {
		return false
	}
	return true
}

// tokenCompetitionCandidates is matches' applicable candidates, in order:
// exactly the matches the drive would act on.
func tokenCompetitionCandidates(matches []replMatch) []replMatch {
	var out []replMatch
	for _, m := range matches {
		if tokenReplApplies(m) {
			out = append(out, m)
		}
	}
	return out
}

// tokenReplacementsCommute reports whether the candidate ReplaceToken bodies
// compose order-insensitively: pure multipliers (Type$ Amount) commute with
// each other and pure adders (Type$ AddToken) with each other, while a
// script rewriter (Type$ ReplaceToken) or an unclassifiable Type$ makes the
// order observable (a rewriter composed after a multiplier rewrites every
// duplicated mint; composed before, only the original). HalfDown/HalfUp
// multipliers floor, so they do not commute with anything but themselves.
func tokenReplacementsCommute(cands []replMatch) bool {
	kind := ""
	for _, m := range cands {
		body := m.repl.With
		if body == nil || body.API != "ReplaceToken" {
			return false
		}
		cls := ""
		switch strings.TrimSpace(body.Params["Type"]) {
		case "Amount":
			raw := strings.TrimSpace(body.Params["Amount"])
			if raw == "" {
				raw = "Twice"
			}
			if raw == "HalfDown" || raw == "HalfUp" {
				return false
			}
			cls = "mult"
		case "AddToken":
			cls = "add"
		default:
			return false
		}
		if kind != "" && cls != kind {
			return false
		}
		kind = cls
	}
	return true
}

// dropReplMatch is cands minus the one match (source id plus repl pointer
// identity), order kept: the CR 616.1 answer's "apply the chosen one first,
// then the rest in order" walk.
func dropReplMatch(cands []replMatch, m replMatch) []replMatch {
	out := make([]replMatch, 0, len(cands))
	for _, c := range cands {
		if c.id != m.id || c.repl != m.repl {
			out = append(out, c)
		}
	}
	return out
}

// tokenChoiceState is the parked chosen-copy replacement's continuation: the
// original event, the full match list, the plan as rewritten so far, the
// cursor of the next unapplied match, the match the outstanding election
// belongs to and the decline option's index (negative when the body is not
// Optional -- a decline is then not an offered answer).
type tokenChoiceState struct {
	ev         events.Event
	matches    []replMatch
	plan       []tokenPlanMint
	next       int
	match      replMatch
	declineIdx int
	// parkedResume carries the CR 616.1 order competition's own suspension
	// record when the election was posed inside the answer that was applying
	// that competition (pending nil, the record still on e.resume): the
	// election's answer tail resumes it once the plan has settled, exactly
	// as handleReplacement's tail would have.
	parkedResume *resumePoint
	// plainOptional marks an Optional$ True replacement with no copy source to
	// choose (Type$ Amount/AddToken/ReplaceToken): the election is a bare
	// apply/decline, and option declineIdx declines it while any other answer
	// applies the match.
	plainOptional bool
	// mintSink names the parked-mint collector (rules/token_rest.go) the
	// election's answer mints into when a resolving DB$ Token is waiting on
	// this creation; 0 otherwise.
	mintSink uint64
}

// driveTokenReplacements applies matches[from:] to the plan, in the
// dispatcher's deterministic scan order, parking (with e.tokenChoice
// populated) at the first chosen-copy match whose election must be asked.
// A nil/unchanged plan rides out as the caller's signal that the original
// event stands.
func (e *Engine) driveTokenReplacements(ev events.Event, matches []replMatch, plan []tokenPlanMint, from int) ([]tokenPlanMint, bool) {
	for i := from; i < len(matches); i++ {
		m := matches[i]
		body := m.repl.With
		if body == nil || body.API != "ReplaceToken" {
			// A body this dispatcher does not read leaves the plan untouched;
			// the mint stands (the fail-safe direction).
			continue
		}
		chosenShape := strings.EqualFold(strings.TrimSpace(body.Params["TokenScript"]), "Chosen") ||
			strings.TrimSpace(body.Params["ValidChoices"]) != ""
		if !chosenShape && strings.EqualFold(m.repl.Params["Optional"], "True") {
			// A "may" replacement with no copy source to point at: pose the
			// bare apply/decline election. The chosen-copy bodies below pose
			// their own election and so never take this arm.
			var parked bool
			plan, parked = e.posePlainOptionalTokenReplacement(ev, matches, plan, i, m)
			if parked {
				return plan, true
			}
			continue
		}
		// CR 616.1's order competition: two or more applicable matches from
		// here whose bodies do not all commute, and a creator who can still
		// decide, park the whole plan on the queue and ask which applies
		// first. The pose is a queue append: a competition that arrived while
		// another decision was outstanding parks behind it and is asked when
		// the queue drains (Submit's tail), never overwriting it; a creator
		// who has left the game makes no choices (CR 800.4a) and the drive
		// continues in scan order.
		if rest := tokenCompetitionCandidates(matches[i:]); len(rest) > 1 && !tokenReplacementsCommute(rest) {
			if p := ev.Player; int(p) < len(e.G.Players) && !e.G.Players[p].Lost {
				applicable := make([]int, 0, len(rest))
				for j, c := range matches[i:] {
					if tokenReplApplies(c) {
						applicable = append(applicable, j)
					}
				}
				e.replChoices = append(e.replChoices, replChoice{kind: replChoiceToken,
					ev: ev, cands: matches[i:], applicable: applicable, before: e.triggerBefore,
					tokenPlan: plan, tokenNext: i, player: p, inResolution: e.resolvingObj != 0 || e.answerInResolution})
				if e.pending == nil {
					e.askReplacementChoice(p)
				}
				return plan, true
			}
		}
		if chosenShape {
			var parked bool
			plan, parked = e.poseChosenTokenReplacement(ev, matches, plan, i, m)
			if parked {
				return plan, true
			}
			continue
		}
		plan = e.applyTokenReplacementToPlan(ev, plan, m)
	}
	return plan, false
}

// poseChosenTokenReplacement handles ONE Type$ ReplaceToken body whose copy
// source is a player choice (ValidChoices$ <spec> / TokenScript$ Chosen --
// the measured population is exactly Esix, Fractal Bloom, Moonlit Meditation
// and Mirrormind Crown). The controller's election is one KChoose over the
// permanents the spec matches (deterministic battlefield scan order), with a
// decline option FIRST when the R: line carries Optional$ True -- the
// replicate/exert convention, so botpolicy's KChoose default arm (first
// offer) declines a may and never wrongly replaces. On accept every mint the
// body gates onto becomes a copy of the chosen creature; on decline the match
// is skipped and the plan's remaining matches still run (declining one
// CR 616.1 competitor never declines the others). No eligible permanent is
// the forced decline (nobody could answer differently -- the strict-supersets
// convention), and a second concurrent decision keeps the loud stand-in
// rather than overwrite an outstanding ask.
func (e *Engine) poseChosenTokenReplacement(ev events.Event, matches []replMatch, plan []tokenPlanMint, idx int, m replMatch) ([]tokenPlanMint, bool) {
	you := e.controllerOf(m.id)
	cands := e.tokenChosenCandidates(strings.TrimSpace(m.repl.With.Params["ValidChoices"]), m.id, you)
	optional := strings.EqualFold(m.repl.Params["Optional"], "True")
	if len(cands) == 0 {
		return plan, false
	}
	if !optional && len(cands) == 1 {
		return e.applyChosenToPlan(ev, m, plan, cands[0]), false
	}
	// pending != nil means a real decision is outstanding: the election parks
	// (queue discipline) rather than displacing it. e.resume != nil with
	// pending nil is the CR 616.1 answer window itself -- the order
	// competition's own suspension record, which this pose keeps (e.ask
	// never overwrites) and its answer tail resumes through parkedResume.
	if e.tokenChoice != nil || e.pending != nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
			Text: "ReplaceToken ValidChoices (TokenScript$ Chosen) cannot ask while another decision is pending; the token is created unchanged"})
		return plan, false
	}
	parkedResume := (*resumePoint)(nil)
	if e.resume != nil {
		parkedResume = e.resume
	}
	asker := e.tokenElectionAsk
	opts := make([]decision.Option, 0, len(cands)+1)
	declineIdx := -1
	if optional {
		opts = append(opts, decision.Option{Index: 0, Kind: "decline",
			Label: "create the tokens as they would have been"})
		declineIdx = 0
	}
	for i, id := range cands {
		label := ""
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		opts = append(opts, decision.Option{Index: declineIdx + 1 + i, Kind: "creature", Label: label, Obj: id})
	}
	st := &tokenChoiceState{ev: ev, matches: matches, plan: plan,
		next: idx + 1, match: m, declineIdx: declineIdx, parkedResume: parkedResume}
	e.tokenChoice = st
	prompt := "Choose a creature to copy"
	if optional {
		prompt = "You may instead create tokens that are copies of a creature: choose one, or decline"
	}
	d := &decision.Decision{Player: you, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: m.id, Prompt: prompt, Options: opts}
	e.choosing = chooseTokenReplace
	asker(d, st)
	return plan, true
}

// tokenElectionAsk poses a CreateToken replacement election (the chosen-copy
// and the plain Optional$ shapes) and, when the token event interrupted a
// stack resolution, SUSPENDS that resolution on it -- the same discipline
// askReplacementChoice applies to the CR 616.1 order competition. A bare
// e.ask here left the interrupted effect chain running past the park: the
// resolving ability's next sub-ability asked its own question through
// Engine.Ask, overwriting the election, the election's flow then consumed
// that answer, and the resolving object stayed on the stack with nothing to
// finish it, so resolveTop re-resolved it from the top on every priority
// pass (fuzz batch6 line 4: Vivien, Monsters' Advocate's +1 minting a Beast
// per pass under an equipped Mirrormind Crown). Through Engine.Ask the
// effects.Resolve loops see Suspended and record their continuation, and the
// record is remembered as the election's parkedResume so the answer tail
// (settleTokenElection) resumes the resolution once the plan has settled.
// A pose that already runs under a resume record (the order competition's
// answer window), or with nothing resolving, keeps the plain ask: there is
// nothing further to suspend, and a stale record would make resolveTop
// abandon a resolution that actually finished.
func (e *Engine) tokenElectionAsk(d *decision.Decision, st *tokenChoiceState) {
	if e.resume == nil && e.pending == nil && (e.resolvingObj != 0 || e.applyingReplacement) {
		d.ResumeKind = "replacement"
		e.Ask(d)
		st.parkedResume = e.resume
		return
	}
	e.ask(d)
}

// settleTokenElection is the answer tail of a CreateToken replacement
// election: rp is the suspension record the election's state carried
// (tokenReplAnswer's return). With nothing further outstanding the
// suspended resolution resumes now. When settling the plan posed a nested
// ask of its own (a minted copy's as-enters choice through Engine.Ask), that
// ask owns e.resume: rp runs after its whole continuation chain, the
// settleReplacementQueue discipline -- dropping it would leave the
// interrupted object on the stack for resolveTop to re-resolve unbounded.
// A queued order competition keeps rp on e.resume for its own answer tail.
func (e *Engine) settleTokenElection(rp *resumePoint) {
	if rp == nil {
		return
	}
	if e.resume == rp {
		if e.pending == nil && len(e.replChoices) == 0 {
			e.resume = nil
			e.resumeResolution(rp, nil)
		}
		return
	}
	if e.resume == nil {
		return
	}
	tail := e.resume
	for {
		if tail == rp {
			return
		}
		if tail.outer == nil {
			break
		}
		tail = tail.outer
	}
	tail.outer = rp
}

// posePlainOptionalTokenReplacement handles ONE Optional$ True
// ReplaceToken body with no copy source to point at (Flitwing Lyev,
// Detective: "if you would create one or more tokens, you may create that
// many Clue tokens instead"). The controlling player's election is one
// KChoose over a bare apply/decline pair, with the decline FIRST so
// botpolicy's KChoose default arm (first offer) never wrongly applies a may
// -- the same replicate/exert convention poseChosenTokenReplacement uses.
// The ask is parked on e.tokenChoice and the flow resumes through
// tokenReplAnswer (the chosen-copy path's own resume).
func (e *Engine) posePlainOptionalTokenReplacement(ev events.Event, matches []replMatch, plan []tokenPlanMint, idx int, m replMatch) ([]tokenPlanMint, bool) {
	if e.tokenChoice != nil || e.pending != nil || e.resume != nil {
		e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
			Text: "an optional ReplaceToken election cannot ask while another decision is pending; the replacement is declined"})
		return plan, false
	}
	you := e.controllerOf(m.id)
	opts := []decision.Option{
		{Index: 0, Kind: "decline", Label: "create the tokens as they would have been"},
		{Index: 1, Kind: "apply", Label: "apply this replacement"},
	}
	st := &tokenChoiceState{ev: ev, matches: matches, plan: plan,
		next: idx + 1, match: m, declineIdx: 0, plainOptional: true}
	e.tokenChoice = st
	d := &decision.Decision{Player: you, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: m.id, Prompt: "You may apply this token replacement effect: apply it?", Options: opts}
	e.choosing = chooseTokenReplace
	e.tokenElectionAsk(d, st)
	return plan, true
}

// tokenReplAnswer resumes the parked chosen-copy replacement (the
// chooseTokenReplace case of handleChoose): the answered option either
// rewrites the plan to copies of the chosen creature or skips the match (the
// decline), and the flow then drives the plan's remaining matches.
func (e *Engine) tokenReplAnswer(chosen []decision.Option) *resumePoint {
	st := e.tokenChoice
	e.tokenChoice = nil
	e.choosing = chooseNone
	if st == nil {
		e.emit(events.Event{Kind: events.Note, Text: "token copy choice answered with no replacement pending"})
		return nil
	}
	// A resolving DB$ Token waiting on this creation collects what the
	// answer mints (and hands the collector to any election or order ask the
	// answer poses next) before its continuation resumes.
	var rp *resumePoint
	saved := e.answerInResolution
	e.answerInResolution = saved || st.parkedResume != nil
	e.withMintSink(st.mintSink, func() { rp = e.settleTokenAnswer(st, chosen) })
	e.answerInResolution = saved
	return rp
}

// settleTokenAnswer is tokenReplAnswer's body: apply the answered election to
// the parked plan, drive the remaining matches and emit the settled plan.
func (e *Engine) settleTokenAnswer(st *tokenChoiceState, chosen []decision.Option) *resumePoint {
	if st.plainOptional {
		// A bare Optional$ election: any non-decline answer applies the
		// match itself; the decline skips it and the remaining matches run.
		if len(chosen) == 1 && chosen[0].Index != st.declineIdx {
			st.plan = e.applyTokenReplacementToPlan(st.ev, st.plan, st.match)
		}
	} else if len(chosen) != 1 || chosen[0].Index == st.declineIdx {
		// The decline (or a malformed answer, treated as one): the match is
		// skipped, the event's remaining replacements still run.
	} else if o := e.G.Obj(chosen[0].Obj); o != nil && o.Zone == state.ZBattlefield {
		st.plan = e.applyChosenToPlan(st.ev, st.match, st.plan, chosen[0].Obj)
	} else {
		// The chosen creature vanished between the ask and the answer: one
		// loud Note and the match is skipped, never a mint of nothing.
		e.emit(events.Event{Kind: events.Note, Obj: st.match.id, Player: st.ev.Player,
			Text: "the chosen creature is no longer on the battlefield; the token is created unchanged"})
	}
	plan, parked := e.driveTokenReplacements(st.ev, st.matches, st.plan, st.next)
	if parked {
		return nil
	}
	e.emitTokenPlan(st.ev, plan)
	e.askNextReplacementChoice()
	return st.parkedResume
}

// emitTokenPlan settles the parked plan after every match has been applied
// or declined. An unchanged plan re-emits the original event verbatim under
// the applyingReplacement guard (the finishParkedPhase convention -- asking
// the ordinary path to re-collect would re-pose the declined elections), the
// empty plan is the rounded-to-zero Note, and each mint rides the
// continueCreateTokenReplacements tail's discipline.
func (e *Engine) emitTokenPlan(ev events.Event, plan []tokenPlanMint) {
	if len(plan) == 1 && plan[0].copyOf == 0 && plan[0].script == ev.Text && !plan[0].hasController {
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(ev)
		e.applyingReplacement = saved
		return
	}
	if len(plan) == 0 {
		e.emit(events.Event{Kind: events.Note, Obj: 0, Player: ev.Player,
			Text: "no tokens created (replacement effect rounded the creation down to zero)"})
		return
	}
	e.emitTokenPlanMints(ev, plan)
}

// emitTokenPlanMints logs the final plan: one TokenCreate per scripted mint
// through Engine.emit under applyingReplacement (so no doubler can loop on its
// own output), one CopyToken + genuine MoveZone per copy mint (the DB$
// CopyPermanent mint shape -- the entry stays a ChangesZone-matchable event
// every "a creature enters" trigger observes, and the MoveZone rides the
// ordinary entry machinery a real copy gets).
func (e *Engine) emitTokenPlanMints(ev events.Event, plan []tokenPlanMint) events.Event {
	var last events.Event
	for i, mint := range plan {
		if mint.copyOf != 0 {
			last = e.emitChosenCopyToken(ev, mint.copyOf, tokenMintPlayer(mint, ev))
			if e.suspendTokenPlanTail(ev, plan, i) {
				return last
			}
			continue
		}
		mintEv := events.Event{Kind: events.TokenCreate, Player: tokenMintPlayer(mint, ev), Text: mint.script}
		// This plan has already passed token-creation replacements. Keep that
		// no-rematch boundary while routing the actual mint through the entry
		// staging/fold path (which may park for entry-counter order).
		savedApplying := e.applyingReplacement
		e.applyingReplacement = true
		stored := e.emit(mintEv)
		e.applyingReplacement = savedApplying
		if e.suspendTokenPlanTail(ev, plan, i) {
			return last
		}
		last = stored
	}
	return last
}

// suspendTokenPlanTail parks the mints still owed when the just-emitted
// plan[at] mint staged behind an entry-counter order ask, so the plan's
// mints land in plan order after the answer rather than the later ones
// overtaking the staged one. Returns whether it suspended. The stage's
// completion re-drive continues the tail (resumeEntryCounterOrder).
func (e *Engine) suspendTokenPlanTail(ev events.Event, plan []tokenPlanMint, at int) bool {
	if at+1 >= len(plan) {
		return false
	}
	st := e.outstandingEntryStage()
	if st == nil {
		return false
	}
	st.tokenPlan = &tokenPlanResume{ev: ev, plan: append([]tokenPlanMint(nil), plan[at+1:]...)}
	return true
}

// emitChosenCopyToken mints one copy of a battlefield creature: the CopyToken
// mint predicted by id (the effToken/effMyriad prediction pattern), then the
// genuine MoveZone that puts it on the battlefield.
func (e *Engine) emitChosenCopyToken(ev events.Event, src state.ObjID, player state.PlayerID) events.Event {
	want := e.G.NextID
	stored := e.emit(events.Event{Kind: events.CopyToken, Obj: src, Player: player})
	if e.G.Obj(want) == nil {
		return stored
	}
	// The copy is published to the mint sink by the emit tail when this
	// MoveZone's entry actually completes (publishTokenEntry) -- never here:
	// the move may park behind an entry-counter order or an as-enters
	// election, and the id must not reach a rider before it has entered.
	return e.emit(events.Event{Kind: events.MoveZone, Obj: want,
		From: state.ZLibrary, To: state.ZBattlefield})
}

// tokenChosenCandidates resolves one chosen-copy body's ValidChoices$ spec
// against the battlefield: every permanent, any controller's (deterministic
// AliveFrom/zone order), the spec matches, with the replacement's source as
// the filter's referent -- Esix's `Creature.Other` excludes Esix itself and
// Moonlit's `Card.EnchantedBy` reaches exactly the enchanted permanent. An
// unmatchable spec fails closed to the empty set (the forced decline).
func (e *Engine) tokenChosenCandidates(spec string, source state.ObjID, you state.PlayerID) []state.ObjID {
	var out []state.ObjID
	sc := e.specCtx(source, you)
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if effects.MatchesObjectCtx(e.G, spec, o, sc) {
				out = append(out, id)
			}
		}
	}
	return out
}

// applyChosenToPlan rewrites every mint ONE chosen-copy match gates onto a
// copy of the chosen creature -- one election covers the whole creation
// event ("choose a creature ... and create that many tokens that are copies
// of that creature").
func (e *Engine) applyChosenToPlan(ev events.Event, m replMatch, plan []tokenPlanMint, chosen state.ObjID) []tokenPlanMint {
	out := make([]tokenPlanMint, 0, len(plan))
	for _, mint := range plan {
		if e.tokenReplacementMatchesMint(ev, m, mint) {
			out = append(out, tokenPlanMint{copyOf: chosen})
		} else {
			out = append(out, mint)
		}
	}
	return out
}

// applyAddCounterReplacements rewrites a CounterChange/PlayerCounterChange
// event's Amount through every applicable R:Event$ AddCounter replacement,
// then returns the event UNHANDLED so emit's ordinary path logs and folds the
// rewritten amount -- the in-place-rewrite shape the DamageDone ReplaceDamage
// bodies use, one event kind over. Each match applies at most once, and each
// body's Amount$ reads the amount the earlier matches produced (the running
// total, CR 616.1e), so Hardened Scales then Branching Evolution composes
// 1 -> +1 -> double = 4 exactly as the two cards' combined oracle reads. No
// predicate re-check is needed between modifiers: this class's gates
// (ValidCounterType$/ValidCard$/ValidObject$/ValidPlayer$) never depend on
// the amount, unlike CreateToken's per-mint ValidToken$ re-check.
//
// CR 616.1's order choice: two or more applicable candidates whose bodies do
// not all commute and an affected player who can still decide park the event
// on the queue and ask (continueAddCounterReplacements applies the answer
// and re-drives; the pose is a queue append, so a competition that arrived
// while another decision was outstanding parks behind it and is asked when
// the queue drains, never overwriting it). A competition whose affected
// player has left the game makes no choices (CR 800.4a) and takes the
// deterministic scan-order composition.
//
// A body whose Amount$ this build cannot price, or whose resolved value is
// negative, leaves the event verbatim -- never a silent erase. A resolved
// zero IS applied, though: "instead put zero" is a legitimate replacement
// result (Vizier of Remedies' Minus.1 on a single -1/-1 counter resolves to
// zero, and the oracle's "that many minus one" then places none). An
// unpriceable body is skipped, never read as zero.
func (e *Engine) applyAddCounterReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	var cands []replMatch
	for _, m := range matches {
		body := m.repl.With
		if body == nil || body.API != "ReplaceCounter" {
			continue
		}
		if _, ok := e.priceAddCounterBody(ev, m, ev.Amount); ok {
			cands = append(cands, m)
		}
	}
	if len(cands) == 0 {
		return ev, false
	}
	if p, ok := e.addCounterAffectedPlayer(ev); ok && !e.G.Players[p].Lost &&
		len(cands) > 1 && !e.addCounterReplacementsCommute(cands) {
		e.poseAddCounterOrderChoice(ev, cands, p)
		return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "counter change awaiting replacement-order choice"}, true
	}
	amount := ev.Amount
	changed := false
	for _, m := range cands {
		n, ok := e.applyAddCounterBody(ev, m, amount)
		if !ok {
			continue
		}
		if n != amount {
			amount = n
			changed = true
		}
	}
	if !changed {
		return ev, false
	}
	ev.Amount = amount
	return ev, false
}

// addCounterAffectedPlayer is the CR 616.1 affected player of a counter
// event: the counter's recipient for the player form (ev.Player), the
// affected object's controller for the object form.
func (e *Engine) addCounterAffectedPlayer(ev events.Event) (state.PlayerID, bool) {
	if ev.Kind == events.PlayerCounterChange {
		return ev.Player, int(ev.Player) < len(e.G.Players)
	}
	return e.moveAffectedPlayer(ev)
}

// poseAddCounterOrderChoice parks a counter event whose competing AddCounter
// replacements do not all commute and asks the affected player (CR 616.1)
// which applies first. The synchronous-damage context the event was proposed
// under rides the park, the way the life competition's does, so the resumed
// emit sees the same provenance. The counter ADDER (the player putting the
// counters, the ValidSource$/EffectOnly$ role) is captured here too: the
// answer may arrive after the proposing window has closed -- a body's
// CounterChange poses from inside the replacement body, whose
// applyingReplacement/replacingSource provenance is gone by resume time --
// so the adder cannot be re-derived at the final emit.
func (e *Engine) poseAddCounterOrderChoice(ev events.Event, cands []replMatch, p state.PlayerID) {
	adderPlusOne := state.PlayerID(0)
	if a, ok := e.inFlightCounterAdder(); ok {
		adderPlusOne = a + 1
	}
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceAddCounter,
		ev: ev, cands: cands, before: e.triggerBefore, player: p,
		damaging: e.damaging, combatDamaging: e.combatDamaging, dmgSrcOverride: e.dmgSrcOverride,
		inResolution: e.resolvingObj != 0 || e.answerInResolution, counterAdderPlusOne: adderPlusOne})
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

// addCounterReplacementsCommute reports whether the candidate ReplaceCounter
// bodies compose order-insensitively: the same op family throughout, where
// the family is one of the arithmetically safe ones (identity, Plus, Minus,
// Twice, Thrice, HalfDown, HalfUp each commute with itself -- two Plus.1
// bodies land the same total either order). A MIXED set (Hardened Scales'
// Plus.1 and Branching Evolution's Twice: 1 -> 2 -> 4 one way, 1 -> 2 -> 3
// the other) does not commute; neither does a body whose Amount$ resolves to
// something the CounterNum grammar cannot name (a literal, another count
// head) or one that carries a SubAbility$ chain (side-effect riders do not
// commute with anything).
func (e *Engine) addCounterReplacementsCommute(cands []replMatch) bool {
	family := ""
	for _, m := range cands {
		body := m.repl.With
		if body == nil || body.API != "ReplaceCounter" {
			return false
		}
		if body.Sub != nil {
			return false
		}
		op, ok := e.counterReplaceOp(m.id, body)
		if !ok {
			return false
		}
		if op == "" {
			// A bare ReplaceCount$CounterNum identity body applies nothing and
			// commutes with any order.
			continue
		}
		k := strings.TrimPrefix(op, "/")
		if i := strings.IndexByte(k, '.'); i >= 0 {
			k = k[:i]
		}
		if family != "" && k != family {
			return false
		}
		family = k
	}
	return family != ""
}

// counterReplaceOp resolves a DB$ ReplaceCounter body's Amount$ to its
// ReplaceCount$CounterNum op suffix ("/Plus.1", "/Twice", ...). The Amount$
// names an SVar (Hardened Scales' X:ReplaceCount$CounterNum/Plus.1); a value
// that is not a CounterNum ReplaceCount body (a literal, another count head,
// or an SVar name with no face entry) returns false -- the conservative
// not-known-to-commute verdict.
func (e *Engine) counterReplaceOp(source state.ObjID, body *cards.SA) (string, bool) {
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return "", false
	}
	expr := strings.TrimSpace(body.Params["Amount"])
	if v, ok := o.Face().SVars[expr]; ok {
		expr = v
	}
	const prefix = "ReplaceCount$CounterNum"
	if !strings.HasPrefix(expr, prefix) {
		return "", false
	}
	return strings.TrimPrefix(expr, prefix), true
}

// priceAddCounterBody is the side-effect-free applicability verdict shared by
// the order offer and the application. A candidate whose amount cannot be
// resolved (or would remove counters) must never be offered as an effect that
// can apply first. Price again after each rewrite against the running amount.
func (e *Engine) priceAddCounterBody(ev events.Event, m replMatch, amount int32) (int32, bool) {
	body := m.repl.With
	if body == nil || body.API != "ReplaceCounter" {
		return amount, false
	}
	if ct := strings.TrimSpace(body.Params["ValidCounterType"]); ct != "" && ct != ev.Counter {
		return amount, false
	}
	hold := ev
	hold.Amount = amount
	ctx := e.replCtx(m, hold)
	n, ok := e.replaceCounterAmount(body, ctx, amount)
	// A negative result would be a counter REMOVAL, which this class
	// does not express; leave the event verbatim. An unpriceable body
	// (!ok) is likewise skipped, never read as zero.
	if !ok || n < 0 {
		return amount, false
	}
	return n, true
}

// applyAddCounterBody applies a priced body and then resolves its riders.
func (e *Engine) applyAddCounterBody(ev events.Event, m replMatch, amount int32) (int32, bool) {
	n, ok := e.priceAddCounterBody(ev, m, amount)
	if !ok {
		return amount, false
	}
	body := m.repl.With
	ctx := e.replCtx(m, ev)
	ctx.ReplacementAmount = amount
	// The body APPLIES from here on. A sub-ability chain on a ReplaceCounter
	// body is part of the replacement (Forge resolves it as the replaced
	// event happens): Melira, the Living Cure's lock ("and you can't get
	// additional poison counters this turn") rides SVar:OnlyOnePoison's
	// SubAbility$ DBImmediateTrigger, an
	// ImmediateTrigger | Execute$ TrigEffect | StaticAbilities$ CantPutCounter
	// that registers the real CantPutCounter restriction. Running the chain
	// here -- through the same runReplaceWith / resolveReplacementWith machine
	// every other ReplaceWith$ rider rides -- is what makes the lock real;
	// its DBImmediateTrigger resolves the Effect inline, so the lock is
	// installed before this function returns and before the replacement
	// event's own fold. A body that only rewrites without a chain (Hardened
	// Scales, Branching Evolution, Vizier of Remedies) is unchanged.
	if body.Sub != nil {
		e.runReplaceWith(ctx, m.id, body.Sub, nil)
	}
	return n, true
}

// sameReplMatchIn reports whether the applied set already holds m: identity
// by source id plus the repl pointer (the same value identity the damage
// path's alreadyUsed uses).
func sameReplMatchIn(applied []replMatch, m replMatch) bool {
	for _, u := range applied {
		if u.id == m.id && (u.repl == m.repl || (m.key != "" && u.key == m.key)) {
			return true
		}
	}
	return false
}

// continueAddCounterReplacements drives a parked AddCounter competition after
// one order answer: the chosen body already applied, so the remaining
// candidates re-check (CR 616.1e), a live non-commuting remainder re-poses
// at the queue's front, and the fully rewritten event is emitted once none
// is left (the emitLifeReplacement convention -- no new replacement pass).
// The adder captured when the competition was posed (rc.counterAdderPlusOne)
// rides this final emit so its observers see the placement attributed even
// though the proposing window has closed by now.
func (e *Engine) continueAddCounterReplacements(rc replChoice) {
	e.driveAddCounterCompetition(rc,
		func(ev events.Event, _ []replMatch) { e.emitAddCounterReplacement(ev, rc.counterAdderPlusOne) },
		e.reposeAddCounterCompetition)
}

// reposeAddCounterCompetition is the live re-pose of a non-commuting
// remainder: the competition returns to the FRONT of the queue (the same
// event, partially applied) and the affected player is asked again.
func (e *Engine) reposeAddCounterCompetition(rc replChoice, p state.PlayerID) {
	e.replChoices = append([]replChoice{rc}, e.replChoices...)
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

// driveAddCounterCompetition is the CR 616.1e loop shared by the live
// counter path (continueAddCounterReplacements) and the staged-entry resume
// (rules/entry_counters.go): it re-prices the unapplied candidates against
// the running amount, applies the first remaining one until the competition
// empties or a live non-commuting remainder must be ordered, and reports the
// fully rewritten event through complete (with the bodies applied, in
// answer order). repose parks a remainder that needs another answer; the
// live path re-queues at the front, the staged path mirrors a real pose.
func (e *Engine) driveAddCounterCompetition(rc replChoice,
	complete func(events.Event, []replMatch), repose func(replChoice, state.PlayerID)) {
	ev := rc.ev
	applied := rc.appliedRepls
	for {
		var remaining []replMatch
		for _, m := range rc.cands {
			if !sameReplMatchIn(applied, m) {
				if _, ok := e.priceAddCounterBody(ev, m, ev.Amount); ok {
					remaining = append(remaining, m)
				}
			}
		}
		if len(remaining) == 0 {
			complete(ev, applied)
			return
		}
		if p, ok := e.addCounterAffectedPlayer(ev); ok && !e.G.Players[p].Lost &&
			len(remaining) > 1 && !e.addCounterReplacementsCommute(remaining) {
			rc.ev, rc.appliedRepls = ev, applied
			repose(rc, p)
			return
		}
		m := remaining[0]
		applied = append(applied[:len(applied):len(applied)], m)
		n, ok := e.applyAddCounterBody(ev, m, ev.Amount)
		if ok && n != ev.Amount {
			ev.Amount = n
		}
	}
}

// emitAddCounterReplacement logs a fully rewritten counter event without
// starting a new replacement pass: every candidate has had its one
// opportunity (the emitLifeReplacement convention). adderPlusOne is the
// competition's captured adder (PLUS ONE; 0 = unknown), published for the
// emit so a trigger or the per-turn ledger reads the same "who put these"
// role the original placement carried -- the counterReplacementFold pairing
// below deliberately disables replacement-body inference for this settled
// echo, so an unpublished adder would leave the placement attributed to
// nobody.
func (e *Engine) emitAddCounterReplacement(ev events.Event, adderPlusOne state.PlayerID) {
	savedAdder := state.PlayerID(counterAdderUnset)
	if adderPlusOne != 0 {
		savedAdder = e.SetCounterAdder(adderPlusOne - 1)
	}
	saved, folded := e.applyingReplacement, e.counterReplacementFold
	e.applyingReplacement, e.counterReplacementFold = true, true
	e.emit(ev)
	e.applyingReplacement, e.counterReplacementFold = saved, folded
	if adderPlusOne != 0 {
		e.SetCounterAdder(savedAdder)
	}
}

// replaceCounterAmount resolves a DB$ ReplaceCounter body's new counter count
// against the amount the event would place. Forge's corpus expresses it as
// Amount$ X with X:ReplaceCount$CounterNum/Plus.1 (Hardened Scales, +1) or
// X:ReplaceCount$CounterNum/Twice (Branching Evolution, double); the shared
// numeric grammar resolves both once CounterNum is a recognised ReplaceCount
// field (effects/count.go). The base is the HELD amount, not the original
// event's, so a chain of modifiers reads the running total (CR 616.1e).
// NumResolved's verdict distinguishes an unmodelled frame (fail the match)
// from a legitimate zero.
func (e *Engine) replaceCounterAmount(body *cards.SA, ctx *effects.Ctx, base int32) (int32, bool) {
	ctx.ReplacementAmount = base
	return effects.NumResolved(e, ctx, body, "Amount", base)
}

// planarDieFaceName names one planar-die roll result (CR 901.3a): the die
// is a six-sided die with four blank faces, one planeswalk face and one
// chaos face. Forge rolls it as an ordinary d6 with the 5/6 split.
func planarDieFaceName(result int32) string {
	switch result {
	case 5:
		return "planeswalk"
	case 6:
		return "chaos"
	default:
		return "blank"
	}
}

// continuePlanarRollReplacements applies every applicable planar-dice
// replacement (the Ichor Elixir class: "if you would roll one or more
// planar dice, instead roll that many planar dice plus one and ignore
// one") to one PlanarRoll event, then performs the roll itself — the
// emitted event is only the proposal (its Amount is the pre-replacement
// count), so the dispatch is where the dice actually roll, through the
// engine rng exactly like effRollDice's dice.
//
// Each match applies in deterministic scan order with a fresh recheck
// (CR 616.1e, the applyNonMoveReplacements discipline), its ReplaceWith$
// chain rewriting the held event's Number (Amount) and Ignore (Counter)
// through ReplaceEvent. The dice then roll: one Note per die ("rolls the
// planar die: chaos", the transcript's die roll), the ignored count is
// recorded on the event's Counter and the KEPT results — the FIRST
// count-ignore rolls, a deterministic stand-in for the roller's choice
// (CR 901.4's ignore choice is vacuous here: no plane deck exists, so no
// roll result differs in effect from any other) — ride IDs in roll order.
// The completed event returns handled=false so the ordinary emit path
// logs it with its full trigger treatment; the per-die Notes and the
// ignore Note are the log's other witnesses. A bodyless match (a
// CantHappen planar replacement) has no corpus carrier and is skipped —
// documented inertness, not modelled cancellation.

// continueExploreReplacements is the events.Explore replacement dispatch.
// Two event shapes reach it:
//
//   - the SYNTHETIC PROPOSAL effects/explore.go's ExploreReplaced hook builds
//     (no revealed card yet — IDs empty): this is CR 701.35a's "would
//     explore" moment, exactly the window CR 614.4 puts replacement
//     effects in, and a matching replacement's ReplaceWith$ body replaces
//     the whole explore process (reveal, counter, move) with its own
//     resolution — run synchronously inside the hook's call, under the
//     applyingReplacement guard (so the body's own fresh explores cannot
//     re-match the same replacement, the CreateToken once-per-event
//     discipline). The proposal is never logged, and the caller learns
//     "replaced" from the hook's true return.
//   - the COMPLETED RECORD (IDs carry the revealed card): the explore
//     already happened, so nothing is replaceable — the record returns
//     unhandled so the ordinary emit path logs it and trig:Explores
//     matches it with its full trigger treatment.
//
// Multiple competing Explore replacements apply in deterministic scan order
// (the first match wins), the same no-CR-616.1-order-choice stand-in the
// CreateToken path documents; the corpus carries no competing pair.
func (e *Engine) continueExploreReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	if len(ev.IDs) > 0 {
		return ev, false
	}
	if len(matches) == 0 {
		return ev, false
	}
	m := matches[0]
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	return ev, true
}

// ExploreReplaced is the effects.Host hook effects/explore.go consults before
// it would process one explorer's explore (CR 614.4: the replacement window
// is before the process). It builds the synthetic Explore proposal — Obj the
// explorer, Player its controller, no revealed card (the replacee never
// reveals) — and runs it through the ordinary replacement collection and
// dispatch: a matching R:Event$ Explore replacement's body resolves
// synchronously inside this call and the hook returns true, telling the
// effect its explore was replaced whole. Mirrors emit's own guard: while a
// replacement body is already resolving (applyingReplacement), no replacement
// applies — the body's own explores are fresh, un-replaced events.
func (e *Engine) ExploreReplaced(explorer state.ObjID) bool {
	if e.applyingReplacement {
		return false
	}
	o := e.G.Obj(explorer)
	if o == nil {
		return false
	}
	_, handled := e.applyReplacements(events.Event{Kind: events.Explore, Obj: explorer, Player: o.Controller})
	return handled
}

// Scry is the effects.Host hook effects/cardflow.go's effLookAndArrange
// consults at the scry instruction boundary, BEFORE any card of the player's
// library is looked at (CR 614.4: an R:Event$ Scry replacement applies to the
// scry action itself). It builds the synthetic instruction PROPOSAL, applies
// every matching R:Event$ Scry replacement to it, and returns the surviving
// instruction's count. proceed is false when a replacement replaced the scry
// whole (Eligeth, Crossroads Augur: "draw that many cards instead") -- the
// caller must then look at and arrange nothing.
//
// The proposal is NEVER logged; it exists only to give the replacement
// matcher a held event. The completed scry's own events.Scry record is
// emitted later, by handleArrange, carrying the number of cards actually put
// on the bottom (the count trig:Scry's ToBottom$ gate reads) and outside the
// replacement pass, because a finished action is nothing left to replace.
// That split is why the same events.Scry kind serves two roles: a proposal is
// never emitted, a record is never matched (emitScryRecord).
func (e *Engine) Scry(p state.PlayerID, source state.ObjID, count int32, sa *cards.SA, target int) (int32, bool, bool) {
	if count < 0 {
		count = 0
	}
	if e.applyingReplacement {
		// Inside another replacement's own resolution no further replacement
		// applies (the emit guard's rule); the instruction stands.
		return count, true, false
	}
	// The proposal carries its SA/target only through this synchronous call;
	// the parked choice owns plain value data for the later continuation.
	oldSA, oldTarget := e.scrySA, e.scryTarget
	e.scrySA, e.scryTarget = sa, target
	defer func() { e.scrySA, e.scryTarget = oldSA, oldTarget }()
	ev, handled := e.applyReplacements(events.Event{Kind: events.Scry, Player: p, Obj: source, Amount: count})
	if !handled {
		return count, true, false // no replacement matched
	}
	if e.pending != nil && len(e.replChoices) > 0 && e.replChoices[0].kind == replChoiceScry {
		return 0, false, true // proposal parked; do not inspect the library
	}
	if ev.Kind != events.Scry {
		return 0, false, false // replaced whole: nothing is looked at
	}
	return ev.Amount, true, false
}

// continueScryReplacements applies the collected R:Event$ Scry matches to the
// held instruction proposal. Two corpus shapes:
//
//   - DB$ ReplaceEffect | VarName$ Num (Kenessos, Priest of Thassa): the
//     proposed count is rewritten in place ("scry that many cards plus
//     one"), the instruction survives and the caller arranges the new count;
//   - DB$ Draw | Defined$ You | NumCards$ <that many> (Eligeth, Crossroads
//     Augur): the whole scry is replaced by a draw and the instruction is
//     consumed -- returned as a zero event so the caller (Scry, above) sees
//     proceed=false and never looks at a library.
//
// Every count expression is evaluated against the HELD instruction's own
// count through the ReplaceCount$Num grammar only the replacement context has
// (scryReplacementCount), never a global Count read. An unmodelled body emits
// the loud unimplemented Note and leaves the instruction intact -- the
// conservative direction, never a silent whole-scry drop.
//
// Each applicable effect can apply once. A competition parks the proposal
// for the affected scrying player's order choice, even if the sources have
// different controllers. Recheck candidates after each count rewrite.
func (e *Engine) continueScryReplacements(ev events.Event, matches []replMatch, used []bool, sa *cards.SA, target int) (events.Event, bool) {
	if used == nil {
		used = make([]bool, len(matches))
	}
	for {
		var applicable []int
		for i, m := range matches {
			if !used[i] && e.scryReplacementMatches(m, ev) {
				applicable = append(applicable, i)
			}
		}
		if len(applicable) == 0 {
			return ev, true
		}
		if len(applicable) > 1 && int(ev.Player) < len(e.G.Players) && !e.G.Players[ev.Player].Lost {
			if sa == nil {
				sa, target = e.scrySA, e.scryTarget
			}
			e.replChoices = append([]replChoice{{kind: replChoiceScry, ev: ev, cands: matches,
				applied: used, applicable: applicable, before: e.triggerBefore, player: ev.Player}}, e.replChoices...)
			if e.pending == nil {
				d := e.scryReplacementDecision(e.replChoices[0], sa, target)
				if e.resume == nil {
					e.Ask(d)
				} else {
					e.ask(d)
				}
			}
			return ev, true
		}
		i := applicable[0]
		used[i] = true
		m := matches[i]
		// CR 616.1e: the recheck uses the same matcher class the collection
		// used -- an Effect-created match is never re-gated on ActiveZones$.
		if m.repl.With == nil {
			continue
		}
		with := m.repl.With
		ctx := e.replCtx(m, ev)
		switch with.API {
		case "ReplaceEffect":
			if with.Params["VarName"] != "Num" {
				break
			}
			if n, ok := e.scryReplacementCount(ctx, with.Params["VarValue"], ev.Amount); ok {
				ev.Amount = n
				continue
			}
		case "Draw":
			// "Instead": the draw must be the scrying player's own
			// (Defined$ You, or absent = the controller). Any other Defined$
			// is unmodelled and fails loud below rather than drawing for the
			// wrong seat.
			if d := strings.TrimSpace(with.Params["Defined"]); d != "" && !strings.EqualFold(d, "You") {
				break
			}
			if n, ok := e.scryReplacementCount(ctx, with.Params["NumCards"], ev.Amount); ok {
				e.lifeReplacementDraw(ev.Player, n)
				return events.Event{}, true
			}
		}
		e.emit(events.Event{Kind: events.Note, Obj: m.id,
			Text: "unimplemented Scry replacement"})
	}
}

func (e *Engine) scryReplacementMatches(m replMatch, ev events.Event) bool {
	if m.key != "" {
		return e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
	}
	return e.replacementMatches(*m.repl, m.id, ev)
}

// scryReplacementCount resolves a Scry replacement body's count expression
// against the held instruction's own count. Forge writes "that many" in terms
// of the held event as ReplaceCount$Num (Kenessos' SVar X ->
// ReplaceCount$Num/Plus.1; Eligeth's NumCards$ X -> ReplaceCount$Num), a
// grammar only the replacement context carries -- the ordinary Count$
// evaluator does not know it. A plain literal or Count$ body falls through to
// the shared evaluator, so a future `NumCards$ 2` shape works unchanged.
func (e *Engine) scryReplacementCount(ctx *effects.Ctx, raw string, base int32) (int32, bool) {
	expr := strings.TrimSpace(raw)
	if ctx.SVars != nil {
		if body, ok := ctx.SVars[expr]; ok {
			expr = strings.TrimSpace(body)
		}
	}
	if expr == "ReplaceCount$Num" {
		return base, true
	}
	if op, ok := strings.CutPrefix(expr, "ReplaceCount$Num/"); ok {
		return replCountOp(base, op), true
	}
	if strings.HasPrefix(expr, "ReplaceCount$") {
		// A held-event field other than the instruction's own count is not
		// bindable here; fail closed rather than guess.
		return 0, false
	}
	return effects.EvalCountOK(e, ctx, expr)
}

// continueRollDiceReplacements applies the collected R:Event$ RollDice
// matches to the held roll proposal (CR 616.1e: each applicable effect
// applies once, in deterministic scan order, with a fresh recheck against
// the rewritten proposal after every application -- the discipline the
// planar-dice class keeps). The corpus's supported shape is the
// DB$ ReplaceEffect body whose chain rewrites the held proposal's Number
// (the dice count) and Ignore (the ignored-low count) in place --
// ReplaceCount$Number/Plus.1 / ReplaceCount$Ignore/Plus.1 (Wyll, Blade of
// Frontiers; Barbarian Class; Pixie Guide); the rewrite goes through the
// same ReplaceEvent seam the planar arm uses, so the two roll classes
// cannot drift. A match whose body chain steps off ReplaceEffect, or whose
// ReplaceEffect rewrite touches no modelled field (SwapRoll's
// DicePTExchanges), cannot be modelled: the match is skipped LOUDLY (the
// roll proceeds unmodified, the conservative direction) and the shape is
// named here for the next ticket, never silently widened.
func (e *Engine) continueRollDiceReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		// CR 616.1e: the recheck uses the same matcher class the collection
		// used -- an Effect-created match is never re-gated on ActiveZones$.
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if m.repl.With == nil || !rollDiceBodyChainSupported(m.repl.With) {
			e.emit(events.Event{Kind: events.Note, Obj: m.id,
				Text: "unimplemented RollDice replacement"})
			continue
		}
		before := ev
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, &ev)
		if ev.Amount == before.Amount && ev.Counter == before.Counter {
			// The body resolved but rewrote nothing this build models (the
			// SwapRoll exchange): loud inertness, the roll proceeds
			// unmodified.
			e.emit(events.Event{Kind: events.Note, Obj: m.id,
				Text: "unimplemented RollDice replacement"})
		}
	}
	return ev, true
}

// rollDiceBodyChainSupported reports whether a replacement body's whole
// SubAbility$ chain is ReplaceEffect rewrites -- the only shape that both
// never asks (so the pre-roll hook can never suspend the roll it belongs to)
// and only ever rewrites the modelled proposal fields. A chain that steps
// off ReplaceEffect fails closed.
func rollDiceBodyChainSupported(with *cards.SA) bool {
	for s := with; s != nil; s = s.Sub {
		if s.API != "ReplaceEffect" {
			return false
		}
	}
	return true
}

// RollDiceProposed is the effects.Host hook effRollDice consults before any
// die of one roll action is rolled (CR 614.4). It builds the synthetic
// events.RollDice PROPOSAL -- Player the roller, Obj the rolling source,
// Amount the proposed dice count, Counter the proposed ignored-low count --
// and runs it through the ordinary replacement collection and dispatch (the
// Scry proposal's discipline): a matching R:Event$ RollDice replacement's
// body rewrites the proposal synchronously inside this call and the
// rewritten Amount/Counter come back to the caller. The proposal is NEVER
// logged. Mirrors emit's own guard: while a replacement body is already
// resolving (applyingReplacement), no replacement applies -- a body's own
// rolls are fresh, un-replaced events.
func (e *Engine) RollDiceProposed(p state.PlayerID, source state.ObjID, amount, ignore int32) (int32, int32) {
	if e.applyingReplacement {
		return amount, ignore
	}
	if ignore < 0 {
		ignore = 0
	}
	ev, _ := e.applyReplacements(events.Event{Kind: events.RollDice, Player: p, Obj: source,
		Amount: amount, Counter: strconv.FormatInt(int64(ignore), 10)})
	n, err := strconv.Atoi(ev.Counter)
	if err != nil || n < 0 {
		n = 0
	}
	if ev.Amount < 1 {
		ev.Amount = 1
	}
	return ev.Amount, int32(n)
}

// emitScryRecord logs a completed scry instruction's events.Scry record
// OUTSIDE the replacement pass: the record is a finished action's marker, so
// no R:Event$ Scry replacement can apply to it (CR 614.4's window is before
// the action). It still folds and queues triggers normally, so a Mode$ Scry
// trigger fires from exactly this record. Called from handleArrange for the
// Scry verb only (a plain RearrangeTopOfLibrary shares the Option.Kind but
// must record nothing), carrying the number of cards actually put on the
// bottom -- the count trig:Scry's ToBottom$ gate reads.
func (e *Engine) emitScryRecord(ev events.Event) {
	saved := e.applyingReplacement
	e.applyingReplacement = true
	e.emit(ev)
	e.applyingReplacement = saved
}

func (e *Engine) continuePlanarRollReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	for _, m := range matches {
		// CR 616.1e: the recheck uses the same matcher class the collection
		// used — an Effect-created match is never re-gated on ActiveZones$.
		matched := false
		if m.key != "" {
			matched = e.replacementMatchesEffectCreated(*m.repl, m.id, ev, m.remembered, m.rememberedPlayers)
		} else {
			matched = e.replacementMatches(*m.repl, m.id, ev)
		}
		if !matched {
			continue
		}
		if m.repl.With == nil {
			continue
		}
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, &ev)
	}
	count := ev.Amount
	if count < 0 {
		count = 0
	}
	ignore := int32(0)
	if n, err := strconv.Atoi(ev.Counter); err == nil && n > 0 {
		ignore = int32(n)
	}
	if ignore > count {
		ignore = count
	}
	keep := count - ignore
	results := make([]state.ObjID, 0, keep)
	for i := int32(0); i < count; i++ {
		die := int32(e.Rand(6)) + 1
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj,
			Text: "rolls the planar die: " + planarDieFaceName(die)})
		if i < keep {
			results = append(results, state.ObjID(die))
		}
	}
	if ignore > 0 {
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj,
			Text: "ignores " + strconv.FormatInt(int64(count-keep), 10) + " planar-dice result(s)"})
	}
	ev.IDs = results
	return ev, false
}

// applyTokenReplacementToPlan transforms the plan by ONE match, per mint,
// with the match's own ValidToken$ re-checked against each mint's script.
func (e *Engine) applyTokenReplacementToPlan(ev events.Event, plan []tokenPlanMint, m replMatch) []tokenPlanMint {
	body := m.repl.With
	switch strings.TrimSpace(body.Params["Type"]) {
	case "ReplaceToken":
		// "... instead create those tokens as <scripts>" — a pure rewrite:
		// each matched mint is replaced by one mint per script in the CSV
		// (Academy Manufactor's one Clue -> Clue+Food+Treasure; Divine
		// Visitation's squirrel -> angel).
		scripts := e.knownTokenScripts(m.id, body.Params["TokenScript"])
		if len(scripts) == 0 {
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan)*len(scripts))
		for _, mint := range plan {
			if e.tokenReplacementMatchesMint(ev, m, mint) {
				for _, s := range scripts {
					out = append(out, tokenPlanMint{script: s})
				}
			} else {
				out = append(out, mint)
			}
		}
		return out
	case "AddToken":
		// Corpus convention: Amount$ present is a fixed add for the whole
		// creation event; absent Amount$ means "that many" (one per matched
		// mint), as on Chatterfang. Append fixed extras at plan end so their
		// replay-visible mint order is deterministic. (cli-20260927T005250Z-c5ac2e83)
		raw := strings.TrimSpace(body.Params["Amount"])
		fixed := raw != ""
		n := int32(1)
		if fixed {
			v, ok := e.tokenReplacementAmount(m, ev, raw, 1)
			if !ok || v < 0 {
				e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
					Text: "ReplaceToken Amount$ " + raw + " is not implemented; the token is created unchanged"})
				return plan
			}
			n = v
		}
		extra := e.knownTokenScripts(m.id, body.Params["TokenScript"])
		if len(extra) == 0 {
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan)+int(n)*len(extra))
		var fixedController tokenPlanMint
		matched := false
		for _, mint := range plan {
			out = append(out, mint)
			if e.tokenReplacementMatchesMint(ev, m, mint) {
				if fixed {
					fixedController = mint
					matched = true
					continue
				}
				for i := int32(0); i < n; i++ {
					for _, s := range extra {
						out = append(out, tokenPlanMint{script: s, controller: mint.controller, hasController: mint.hasController})
					}
				}
			}
		}
		if fixed && matched {
			for i := int32(0); i < n; i++ {
				for _, s := range extra {
					out = append(out, tokenPlanMint{script: s, controller: fixedController.controller, hasController: fixedController.hasController})
				}
			}
		}
		return out
	case "ReplaceController":
		// "... is created under <NewController$>'s control instead" (Crafty
		// Cutpurse). The new controller is resolved through the ordinary
		// Defined$ player grammar against the replacement source -- `You` is
		// the source's controller, so an opponent's token creation becomes
		// the source controller's -- and every matched mint carries it. A
		// controller the grammar cannot resolve fails closed: the Note names
		// it and the mint is left with its original creator rather than
		// guessed onto a seat.
		p, ok := e.tokenNewController(m, ev)
		if !ok {
			e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
				Text: "ReplaceToken NewController$ " + strings.TrimSpace(body.Params["NewController"]) + " is not implemented; the token is created unchanged"})
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan))
		for _, mint := range plan {
			if e.tokenReplacementMatchesMint(ev, m, mint) {
				mint.controller = p
				mint.hasController = true
			}
			out = append(out, mint)
		}
		return out
	default:
		// "Amount" (and an absent Type$ — the corpus always names one, Amount
		// is the natural default for the "twice that many" doubler family):
		// each matched mint becomes replCountOp(1, Amount$) copies of itself.
		raw := strings.TrimSpace(body.Params["Amount"])
		if raw == "" {
			raw = "Twice"
		}
		n, ok := e.tokenReplacementAmount(m, ev, raw, 1)
		if !ok || n < 0 {
			e.emit(events.Event{Kind: events.Note, Obj: m.id, Player: ev.Player,
				Text: "ReplaceToken Amount$ " + raw + " is not implemented; the token is created unchanged"})
			return plan
		}
		out := make([]tokenPlanMint, 0, len(plan)*int(n)+1)
		for _, mint := range plan {
			if !e.tokenReplacementMatchesMint(ev, m, mint) {
				out = append(out, mint)
				continue
			}
			if n >= 1 {
				for i := int32(0); i < n; i++ {
					out = append(out, mint)
				}
				continue
			}
			// n == 0 (HalfDown against this engine's one-token events): the
			// matched mint is not created. A halving composed AFTER a
			// multiplier applies per mint rather than to the plan total — a
			// deliberate scan-order composition approximation, tracked with
			// the CR 616.1 order-choice follow-up.
		}
		return out
	}
}

// tokenReplacementMatchesMint re-checks ONE replacement against ONE plan
// mint: a scripted mint rides its script, a copy mint (copyOf) rides the
// copied object's printed face -- the would-be token's characteristics are
// the copy source's (CR 706.2), which is what lets a later match re-check a
// copy mint (Divine Visitation after Esix rewrites each copy that is still a
// creature token).
func (e *Engine) tokenReplacementMatchesMint(ev events.Event, m replMatch, mint tokenPlanMint) bool {
	mintEv := events.Event{Kind: events.TokenCreate, Player: tokenMintPlayer(mint, ev), Text: mint.script}
	// A copy mint's would-be token is the copied object's printed face, not a
	// token-script registry key (CR 706.2), so BOTH the matcher's ValidToken$
	// read and the ValidCard$ read below must see that snapshot -- one helper,
	// never two derivations that can drift (the mint-snapshot class).
	tok := e.mintSnapshot(mintEv, mint)
	// The recheck uses the same matcher class the initial collection used:
	// an effect-created match re-matches through the remembered-scoped effect
	// matcher, a printed match through the ordinary one -- never the ungated
	// effect matcher for a printed key. For a TokenCreate mint the two agree
	// on every printed Repl today (the mint, like ev, carries no To), but the
	// split keeps the recheck from ever widening what the initial gated
	// collection admitted, the same discipline remainingDamageReplacements
	// and counterReplacementMatchesAll follow.
	if m.key != "" {
		if !e.replacementMatchesEffectCreatedToken(*m.repl, m.id, mintEv, m.remembered, m.rememberedPlayers, tok) {
			return false
		}
	} else if !e.replacementMatchesToken(*m.repl, m.id, mintEv, tok) {
		return false
	}
	if m.repl.With != nil {
		if v := strings.TrimSpace(m.repl.With.Params["ValidCard"]); v != "" {
			if tok == nil {
				return false
			}
			// The provenance qualifier split applies here too (task castprov1):
			// a would-be TOKEN was never cast at all, so an alternative carrying
			// the qualifier is dropped for it.
			spec, ok := e.castProvenanceAdmits(v, tok.ID, e.controllerOf(m.id))
			if !ok || !effects.MatchesObjectCtx(e.G, spec, tok,
				e.rememberedSpecContext(e.controllerOf(m.id), m.id, m.remembered)) {
				return false
			}
		}
	}
	return true
}

// knownTokenScripts splits a ReplaceToken body's TokenScript$ CSV and keeps
// only the stems the game's token registry knows, one loud Note per unknown
// stem. An empty result leaves the caller's plan untouched.
func (e *Engine) knownTokenScripts(source state.ObjID, csv string) []string {
	var out []string
	for s := range strings.SplitSeq(csv, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := e.G.Tokens[s]; !ok {
			e.emit(events.Event{Kind: events.Note, Obj: source,
				Text: "unknown token script " + s})
			continue
		}
		out = append(out, s)
	}
	return out
}

// tokenReplaceCount resolves a ReplaceToken body's Amount$: a literal
// integer (the AddToken family's Amount$ 1) or one of replCountOp's word
// ops read against the single-mint base (Twice -> 2, Thrice -> 3, HalfDown
// -> 0 against the one-token event this engine mints — replCountOp is the
// shared word-op parser). An unresolvable value (X, an SVar name) reports
// not-ok and the match is dropped with a loud Note.
func tokenReplaceCount(raw string) (int32, bool) {
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n), true
	}
	switch {
	case raw == "Twice", raw == "Thrice", raw == "HalfDown", raw == "HalfUp",
		strings.HasPrefix(raw, "Plus."), strings.HasPrefix(raw, "Minus."),
		strings.HasPrefix(raw, "Times."):
		return replCountOp(1, raw), true
	}
	return 0, false
}

// tokenReplacementAmount resolves a ReplaceToken body's Amount$: the literal
// and op-word grammar of tokenReplaceCount first, then the shared numeric
// grammar (effects.NumResolved) so an SVar body or inline Count$ amount
// resolves too -- `Amount$ X` with `X:ReplaceCount$CounterNum/Twice` reads as
// the doubler the corpus's own ReplaceCounter bodies already express. base is
// the single-mint ReplaceCount base. An unpriceable value reports not-ok and
// the match is dropped with a loud Note, never read as zero.
func (e *Engine) tokenReplacementAmount(m replMatch, ev events.Event, raw string, base int32) (int32, bool) {
	if n, ok := tokenReplaceCount(raw); ok {
		return n, true
	}
	ctx := e.replCtx(m, ev)
	ctx.ReplacementAmount = base
	return effects.NumResolved(e, ctx, m.repl.With, "Amount", base)
}

// tokenNewController resolves a Type$ ReplaceController body's NewController$
// value through the ordinary Defined$ player grammar against the replacement
// source (replCtx binds the source's controller, so `You` -- the corpus's
// only spelling, Crafty Cutpurse -- is that controller). An absent,
// unrecognised, or playerless selector reports not-ok and the match is
// skipped with a loud Note, never guessed onto a seat.
func (e *Engine) tokenNewController(m replMatch, ev events.Event) (state.PlayerID, bool) {
	raw := strings.TrimSpace(m.repl.With.Params["NewController"])
	if raw == "" {
		return 0, false
	}
	sub := &cards.SA{Params: map[string]string{"Defined": raw}}
	for _, t := range effects.Defined(e, e.replCtx(m, ev), sub) {
		if t.IsPlayer {
			return t.Player, true
		}
	}
	return 0, false
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

// applyETBChoiceReplacement parks an entry before it is folded into the
// battlefield and asks through the ordinary mid-resolution suspension path.
// This is the CR 614.12 boundary: the chooser sees the question only when the
// permanent would enter, whether the move came from a resolving spell, a land
// play, reanimation or a search. etbMove/etbNext are the only transient
// continuation state; the answer itself is the existing Choose event.
func (e *Engine) applyETBChoiceReplacement(ev events.Event) bool {
	if ev.Kind != events.MoveZone || ev.To != state.ZBattlefield || e.pending != nil ||
		events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	ordinal := 0
	if e.etbMove != nil {
		if e.etbMove.Obj != ev.Obj {
			return false
		}
		ordinal = e.etbNext
	}
	choice, ok := e.entryETBChoice(ev, ordinal)
	if !ok {
		e.etbMove = nil
		e.etbNext = 0
		return false
	}
	move := ev
	e.etbMove = &move
	e.etbNext = ordinal + 1
	d := &decision.Decision{Player: e.G.Obj(ev.Obj).Controller, Kind: decision.KChoose,
		Min: 1, Max: 1, ResumeKind: "etb", Source: ev.Obj,
		Prompt: "Choose" + etbChoicePrompt(choice.kind), Options: choice.options}
	e.choosing = chooseETBEntry
	e.Ask(d)
	return true
}

// applyRiotReplacement is retained as a defensive fallback for an entry that
// bypasses the shared ETB-choice walker. Ordinary entries are handled by
// applyETBChoiceReplacement above, so this path is not used by normal casts.
func (e *Engine) applyRiotReplacement(ev events.Event) bool {
	// A battlefield entry reached while another decision is outstanding must
	// not overwrite it (the orphaned-pending failure poseLifeReplacementChoice
	// already guards, and the same class findings-sol4 proved on the
	// life-replacement draw loop): let the entry happen verbatim rather than
	// parking on an ask that can never be posed. Unreachable in the corpus
	// today; the guard keeps a future caller from shipping the overwrite.
	if ev.To != state.ZBattlefield || e.riotMove != nil || e.pending != nil {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone == state.ZBattlefield || o.Face() == nil ||
		!o.Face().HasKeyword("Riot") || o.RiotChoice != "" {
		return false
	}
	// A face-down entry (manifest or cloak, CR 708.5) is a vanilla 2/2
	// creature: no riot choice is posed for it, and no public Choose "riot"
	// event may leak the hidden card. The guard MUST sit BEFORE the parking
	// assignment below -- a face-down entry that parked its move and then
	// returned false would leak a stale e.riotMove that is never emitted and
	// never cleared (chooseRiot's answer arm cannot fire for it), so every
	// later non-cast Riot entry would hit the parked-move guard above and
	// never ask again. The FaceDown state is folded by Apply's Move AFTER
	// this dispatch, so the incoming event's counter, not o.FaceDown, is
	// what names the face-down entry (the Siege guard's exact shape, in the
	// Siege guard's exact place -- before every return-past-parking; unleash
	// carries the identical guard in the identical place).
	if events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	move := ev
	e.riotMove = &move
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: o.ID, Prompt: "Choose how this creature enters (counter or haste)",
		Options: []decision.Option{
			{Index: 0, Kind: "riot", Label: "Enter with a +1/+1 counter", Obj: o.ID, Player: o.Controller},
			{Index: 1, Kind: "riot", Label: "Gain haste", Obj: o.ID, Player: o.Controller},
		}}
	e.choosing = chooseRiot
	e.ask(d)
	return true
}

// applySiegeProtector parks every non-cast Battle entry while its controller
// makes the protector choice. CR 310.10: "As a battle enters, its controller
// chooses an opponent to protect it; that player is its protector." The
// choice is a construct rule, not a card script -- none of the 37 real Battle
// cards carries a GenericChoice/ChosenMode script -- so the engine poses it
// here for every entry path, exactly as applyRiotReplacement does for Riot.
// The rule is general over the Battle type, not the Siege subtype: the
// protector is what the CR 310.7 combat defender and the effects-side
// OppProtect predicate read, so every Battle gets one rather than Siege
// alone (all 37 real corpus Battles happen to print the Siege subtype, so
// this is reachable today only for a synthetic or a future non-Siege card).
// The parked move is emitted after handleChoose logs the choice (a Choose
// "protector" event), so a log-only replay re-derives the protector from the
// same event stream. Only the controller's LIVING opponents are offered
// (protectorOpponents is the single eligibility home, shared with the
// re-derive after a protector leaves); a controller with no living opponent
// (a battle entering after everyone else lost -- unreachable in a real match)
// is recorded with no protector rather than parking on an unanswerable ask.
type attachedChoice struct {
	move   events.Event
	source state.ObjID
	stage  int
	// body is the replacement body's API (NameCard, ChooseCard, ChooseColor),
	// so the resume in rules/turn.go dispatches on the primitive that posed
	// the ask rather than on the card. NameCard carries two stages (name then
	// the paired creature type); ChooseCard and ChooseColor are single-stage.
	body string
}

// attachedBodyPoses reports whether an Attached replacement body is one this
// engine can honestly pose. It is the ONE eligibility home: applyAttachedReplacement
// selects only poseable bodies, and a body whose parameter shape cannot be
// honored is not selected, so it keeps today's untouched-Attach fallback
// rather than silently choosing an unrelated object or colour.
func attachedBodyPoses(sa *cards.SA) bool {
	if sa == nil {
		return false
	}
	switch sa.API {
	case "NameCard":
		return true
	case "ChooseCard":
		// The only corpus Attached ChooseCard is Pick-Axe's exiled-craft-card
		// pick; its pool must be the source's own exile association. Any other
		// DefinedCards$ role is a different pool this path does not read.
		return strings.EqualFold(strings.TrimSpace(sa.Params["DefinedCards"]), "ExiledWith")
	case "ChooseColor":
		return true
	default:
		return false
	}
}

// applyAttachedReplacement handles the Attached replacement bodies that pose
// an election before the Attach applies: Psychic Paper's ChooseName
// (NameCard), Pick-Axe's exiled-craft-card ChooseCard, and Sanctuary Blade's
// ChooseColor. It parks the Attach before events.Apply and records the
// answers on the source; the resume in rules/turn.go releases the parked
// Attach exactly once through emitAttachedMove.
func (e *Engine) applyAttachedReplacement(ev events.Event) bool {
	if e.attachedChoice != nil || e.pending != nil || len(ev.IDs) == 0 {
		return false
	}
	var source state.ObjID
	var repl *cards.Repl
	e.forEachReplacementSource(func(id state.ObjID) {
		if source != 0 {
			return
		}
		f := e.replacementFace(id, ev)
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			// Key on the replacement body's own API: `ReplaceWith$ ChooseName`
			// resolves to an SVar whose body IS `DB$ NameCard` (never the SVar
			// name), and the siblings are `DB$ ChooseCard` / `DB$ ChooseColor`.
			// The API is the primitive that poses the ask, so a future card
			// reusing one of these bodies is covered by the same dispatch.
			if r.Event == "Attached" && attachedBodyPoses(r.With) && e.replacementMatches(*r, id, ev) {
				source, repl = id, r
				return
			}
		}
	})
	if source == 0 || repl == nil {
		return false
	}
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	ch := &attachedChoice{move: ev, source: source, body: repl.With.API}
	e.attachedChoice = ch
	switch repl.With.API {
	case "ChooseCard":
		return e.askAttachedCard(o, repl)
	case "ChooseColor":
		return e.askAttachedColor(o, repl)
	default: // NameCard
		return e.askAttachedName(o, repl)
	}
}

// askAttachedName poses the NameCard body's first stage (the name). It is the
// extracted Psychic Paper path, unchanged in behaviour.
func (e *Engine) askAttachedName(o *state.Object, repl *cards.Repl) bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	// ValidDescription$ rides along exactly as it does at the cast-time ETB
	// site (rules/cast.go): it is Forge prompt text, read by
	// effects.NameChoices only as a safety fallback when ValidCards$ is absent.
	opts := e.etbOptions(o.Controller, ch.source, "name", repl.With.Params["ValidCards"], repl.With.Params["ValidDescription"], "", "")
	if len(opts) <= 1 {
		if len(opts) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "name", Text: opts[0].Label})
		}
		return e.askAttachedType()
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose a creature card name", Options: opts}
	e.choosing = chooseAttached
	e.ask(d)
	return true
}

// askAttachedCard poses the ChooseCard body's card ask over the pool its
// DefinedCards$ role names -- Pick-Axe's `DefinedCards$ ExiledWith`, the
// source's own ChangeZone exile association -- further restricted to the
// ChoiceZone$ set. The answer is recorded by the resume as the event-backed
// Choose "chosen" fold on the source (state.Object.Chosen), which is what
// `Defined$ ChosenCard` reads. A pool with no eligible card records nothing
// and releases the Attach (a mandatory choice that finds nothing legal is the
// fail-to-find shape, never a silent pick); a single eligible card is forced
// and recorded without an ask (the effChooseType strict-superset convention
// the sibling stages already use).
func (e *Engine) askAttachedCard(o *state.Object, repl *cards.Repl) bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	opts := e.attachedCardOptions(ch.source, repl)
	if len(opts) == 1 {
		e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "chosen", IDs: []state.ObjID{opts[0].Obj}})
		e.releaseAttachedChoice()
		return true
	}
	if len(opts) == 0 {
		e.releaseAttachedChoice()
		return true
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose an exiled card", Options: opts}
	e.choosing = chooseAttached
	e.ask(d)
	return true
}

// attachedCardOptions builds the card options a ChooseCard Attached body
// offers: the source's ExiledWith association, filtered to the ChoiceZone$
// zones. The pool is read from the event-backed ExiledCards list, never from
// the shared exile zone, so a card this source did not exile is not offered.
func (e *Engine) attachedCardOptions(source state.ObjID, repl *cards.Repl) []decision.Option {
	src := e.G.Obj(source)
	if src == nil || !strings.EqualFold(strings.TrimSpace(repl.With.Params["DefinedCards"]), "ExiledWith") {
		return nil
	}
	zones := attachedChoiceZones(repl.With.Params["ChoiceZone"])
	out := make([]decision.Option, 0, len(src.ExiledCards))
	for _, id := range src.ExiledCards {
		co := e.G.Obj(id)
		if co == nil || co.Face() == nil {
			continue
		}
		if zones != nil && !zones[co.Zone] {
			continue
		}
		out = append(out, decision.Option{Index: len(out), Kind: "card", Obj: id, Label: e.Name(id)})
	}
	return out
}

// attachedChoiceZones parses a ChooseCard body's ChoiceZone$ restriction into
// the zone set it names; a nil result means unrestricted. Unknown zone tokens
// contribute nothing (fail closed), never a widened pool.
func attachedChoiceZones(raw string) map[state.Zone]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[state.Zone]bool{}
	for z := range strings.SplitSeq(raw, ",") {
		switch strings.TrimSpace(z) {
		case "Battlefield":
			out[state.ZBattlefield] = true
		case "Hand":
			out[state.ZHand] = true
		case "Library":
			out[state.ZLibrary] = true
		case "Graveyard":
			out[state.ZGraveyard] = true
		case "Exile":
			out[state.ZExile] = true
		case "Stack":
			out[state.ZStack] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// askAttachedColor poses the ChooseColor body's colour ask (Sanctuary Blade's
// `Defined$ You`). The option list is the same WUBRG list the cast-time
// as-enters colour ask offers, so the two can never disagree about what a
// colour choice ranges over. The chooser is the source's controller (the
// corpus body's `Defined$ You`); the answer is recorded by the resume as the
// event-backed Choose "color" fold on the source (state.Object.ChosenColor).
func (e *Engine) askAttachedColor(o *state.Object, repl *cards.Repl) bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	opts := e.etbOptions(o.Controller, ch.source, "color", "", "", "", "")
	if len(opts) <= 1 {
		if len(opts) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "color", Text: etbColourLetter(opts[0].Label)})
		}
		e.releaseAttachedChoice()
		return true
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose a color", Options: opts}
	e.choosing = chooseAttached
	e.ask(d)
	return true
}

// releaseAttachedChoice drops the parked Attach continuation and re-emits the
// stored Attach through emitAttachedMove. It is the ONE release site shared
// by the no-ask siblings of every body's poser.
func (e *Engine) releaseAttachedChoice() {
	ch := e.attachedChoice
	if ch == nil {
		return
	}
	move := ch.move
	e.attachedChoice = nil
	e.choosing = chooseNone
	e.emitAttachedMove(move)
}

func (e *Engine) askAttachedType() bool {
	ch := e.attachedChoice
	if ch == nil {
		return false
	}
	o := e.G.Obj(ch.source)
	if o == nil {
		e.attachedChoice = nil
		return false
	}
	ch.stage = 1
	opts := e.creatureTypeOptions(o.Controller)
	if len(opts) <= 1 {
		if len(opts) == 1 {
			e.emit(events.Event{Kind: events.Choose, Obj: ch.source, Counter: "type", Text: opts[0].Label})
		}
		move := ch.move
		e.attachedChoice = nil
		e.choosing = chooseNone
		e.emitAttachedMove(move)
		return true
	}
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: ch.source, Prompt: "Choose a creature type", Options: opts}
	e.choosing = chooseAttached
	e.ask(d)
	return true
}

func (e *Engine) emitAttachedMove(move events.Event) {
	e.attachedApplying = true
	e.emit(events.Event{Kind: events.Attach, Obj: move.Obj, IDs: append([]state.ObjID(nil), move.IDs...)})
	e.attachedApplying = false
}

// protectorOpponents lists the living opponents who may protect a battle
// whose controller is controller, in the deterministic seat order
// AliveFrom(0) yields. This is the ONE home for protector eligibility:
// applySiegeProtector's ask, its two-player auto-record and
// rechooseDepartedBattleProtector all read it, so eligibility cannot drift
// between the entry ask and the re-derive after a protector leaves.
// CR 310.10: a battle's controller chooses an opponent to be its protector;
// a player who has left the game is no longer an opponent.
func (e *Engine) protectorOpponents(controller state.PlayerID) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.G.AliveFrom(0) {
		if p == controller {
			continue
		}
		out = append(out, p)
	}
	return out
}

// rechooseDepartedBattleProtector gives every Battle whose recorded
// protector has just left the game a fresh living opponent as its protector
// (CR 310.10: "If a battle's protector leaves the game, that battle's
// controller chooses a new protector"). The choice rides the same Choose
// "protector" event the entry ask records, so the protector stays
// replay-derived and no new event kind or state field is introduced.
//
// The replacement is DERIVED, not re-posed: this runs from the PlayerLost
// event inside emit, which is very often mid-resolution or inside the
// state-based-action fixed point (a concession, a zero-life sweep, an
// empty-library draw), and the engine has no way to park a decision there.
// protectorOpponents is the deterministic fallback: the first living
// opponent in seat order. A controller with no living opponent is the last
// player in the game, so the stale protector is left in place rather than
// re-pointed at nobody.
func (e *Engine) rechooseDepartedBattleProtector(departed state.PlayerID) {
	for _, controller := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, controller) {
			o := e.G.Obj(id)
			if o == nil || !o.ProtectorValid || o.Protector != departed || o.Face() == nil || !o.Face().IsBattle() {
				continue
			}
			if next := e.protectorOpponents(o.Controller); len(next) > 0 {
				e.emit(events.Event{Kind: events.Choose, Obj: o.ID,
					Counter: "protector", Player: next[0]})
			}
		}
	}
}

func (e *Engine) applySiegeProtector(ev events.Event) bool {
	// Same overwrite guard applyRiotReplacement documents: never park on an ask
	// while another decision is outstanding.
	if ev.To != state.ZBattlefield || e.siegeMove != nil || e.pending != nil {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone == state.ZBattlefield || o.Face() == nil {
		return false
	}
	// A face-down entry is a vanilla 2/2 creature (CR 708.5), not a Battle;
	// the entry grant grants it no defense counters, so it must not be parked
	// on the CR 310.10 protector ask either -- and must not emit the
	// Choose "protector" event at all, which is not Secret and would name the
	// hidden card in the public transcript. The FaceDown state is folded by
	// Apply's Move AFTER this replacement dispatch runs, so the incoming
	// event's counter -- not o.FaceDown -- is what names the face-down entry.
	// events.IsFaceDownEntry is the shared predicate covering BOTH markers,
	// the manifest/FaceDown$ one and Cloak's, so this guard and Apply's own
	// fold cannot disagree about which entries are face down.
	if events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	if !o.Face().IsBattle() {
		return false
	}
	// A protector already recorded (a re-entering object keeps none -- Move
	// resets it -- but an object parked twice in one entry sequence must not
	// ask twice).
	if o.ProtectorValid {
		return false
	}
	var opts []decision.Option
	for idx, p := range e.protectorOpponents(o.Controller) {
		opts = append(opts, decision.Option{Index: idx, Kind: "protector",
			Label: e.G.Players[p].Name, Obj: o.ID, Player: p})
	}
	if len(opts) == 0 {
		return false
	}
	// Strict-supersets convention: a decision nobody could answer differently
	// is never posed. In a two-player game exactly one opponent is legal, so
	// record it through the same Choose "protector" event without an ask.
	if len(opts) == 1 {
		e.emit(events.Event{Kind: events.Choose, Obj: o.ID,
			Counter: "protector", Player: opts[0].Player})
		return false
	}
	move := ev
	e.siegeMove = &move
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose,
		Min: 1, Max: 1, Source: o.ID,
		Prompt:  "Choose an opponent to protect this battle",
		Options: opts}
	e.choosing = chooseSiege
	e.ask(d)
	return true
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

// commandReplZoneAdmits is the one home for the command-zone half of the
// printed-replacement zone gate: a replacement whose source currently sits in
// the command zone may act only when its script explicitly declares
// ActiveZones$ Command. The ordinary object walk (rules/trigger_match.go's
// forEachObject) now reaches the command zone because it shares that walk with
// the printed-trigger scan, which must see command-zone triggers such as
// Sidar Jabari of Zhalfir's Eminence. Every printed-replacement path that
// reads a face's R: lines off that walk therefore applies this gate, keeping
// ordinary card text parked in the command zone inert (CR 611.3b's declared-
// zone rule). A source outside the command zone always admits.
func (e *Engine) commandReplZoneAdmits(r cards.Repl, source state.ObjID) bool {
	o := e.G.Obj(source)
	if o == nil || o.Zone != state.ZCommand {
		return true
	}
	active, ok := r.Param(cards.PKActiveZones)
	return ok && zoneSpecContains(active, state.ZCommand)
}

// activeZonesGateOK is the ActiveZones$ zone gate the PRINTED replacement
// paths share:
//
//   - the ordinary object walk now reaches the command zone (it shares
//     forEachObject with the printed-trigger scan); a replacement source
//     there is admitted only when its script explicitly declares that zone
//     (commandReplZoneAdmits), which keeps ordinary card text parked there
//     inert.
//
//   - CR 611.3b/614.4: a static replacement only applies from one of its
//     declared active zones. Accept the comma-separated list grammar used by
//     other Forge zone parameters; the pinned corpus currently uses only
//     singleton ActiveZones values. Replacements with no ActiveZones
//     parameter apply from anywhere (the corpus does not thereby declare a
//     zone, and historically this engine has allowed those from anywhere).
//
//   - A permanent's own entry replacement is active for the event that puts
//     it into the declared zone even though the source has not arrived there
//     yet (CR 614.12): the prospective ev.To clause. ev.To is only
//     meaningful for a MoveZone; the other events leave it at its zero
//     value, so the clause is MoveZone-only rather than reading a
//     meaningless zero zone.
func (e *Engine) activeZonesGateOK(r cards.Repl, source state.ObjID, ev events.Event) bool {
	if !e.commandReplZoneAdmits(r, source) {
		return false
	}
	if active, ok := r.Param(cards.PKActiveZones); ok {
		o := e.G.Obj(source)
		currentlyActive := o != nil && zoneSpecContains(active, o.Zone)
		enteringActive := ev.Kind == events.MoveZone && source == ev.Obj &&
			zoneSpecContains(active, ev.To)
		if !currentlyActive && !enteringActive {
			return false
		}
	}
	return true
}

// replacementMatchesEffectCreated is the matcher for an EFFECT-created
// registration: same predicates as replacementMatchesRemembered, but the
// ActiveZones$ gate is skipped — an Effect's lifetime is active()'s, not its
// source's zone (the rule the bodyless Counter CantHappen branch already
// documents). Forge registers an Effect SA's replacements as command-zone
// entities, so their R: lines declare ActiveZones$ Command (Taii Wakeen's
// RepDamage, 18 measured DamageDone carriers) while this engine holds the
// registration in the continuous registry with the source on the battlefield
// — gating on the source's zone would permanently silence every one of them
// (task wildgrowth1). active() still ends the effect on its own lifetime.
func (e *Engine) replacementMatchesEffectCreated(r cards.Repl, source state.ObjID, ev events.Event, remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool {
	return e.replacementMatchesRememberedUngated(r, source, ev, remembered, rememberedPlayers, nil)
}

// replacementMatchesEffectCreatedBy evaluates the temporary replacement in
// the controller frame captured when the granting Effect resolved. Its source
// may subsequently change controllers, but that does not rewrite the Effect's
// meaning of You.
func (e *Engine) replacementMatchesEffectCreatedBy(r cards.Repl, source state.ObjID, ev events.Event,
	remembered []state.ObjID, rememberedPlayers []state.PlayerID, controller state.PlayerID) bool {
	return e.replacementMatchesRememberedUngatedBy(r, source, ev, remembered, rememberedPlayers, nil, controller)
}

// replacementMatchesToken / replacementMatchesEffectCreatedToken are the
// mint-recheck entry points: tokenOverride overrides what the ValidToken$
// matcher reads as the would-be token (a copy plan mint's snapshot, CR
// 706.2). Only tokenReplacementMatchesMint calls them; every other caller
// passes nil and the matcher builds the snapshot from the event's script.
func (e *Engine) replacementMatchesToken(r cards.Repl, source state.ObjID, ev events.Event, tokenOverride *state.Object) bool {
	return e.replacementMatchesRememberedUngated(r, source, ev, nil, nil, tokenOverride)
}

func (e *Engine) replacementMatchesEffectCreatedToken(r cards.Repl, source state.ObjID, ev events.Event, remembered []state.ObjID, rememberedPlayers []state.PlayerID, tokenOverride *state.Object) bool {
	return e.replacementMatchesRememberedUngated(r, source, ev, remembered, rememberedPlayers, tokenOverride)
}

// replacementMatchesRememberedUngated is replacementMatchesRemembered's
// predicate body without the ActiveZones$ gate; only the wrappers above
// reach it.
func (e *Engine) replacementMatchesRememberedUngated(r cards.Repl, source state.ObjID, ev events.Event, remembered []state.ObjID, rememberedPlayers []state.PlayerID, tokenOverride *state.Object) bool {
	return e.replacementMatchesRememberedUngatedBy(r, source, ev, remembered, rememberedPlayers, tokenOverride, e.controllerOf(source))
}

func (e *Engine) replacementMatchesRememberedUngatedBy(r cards.Repl, source state.ObjID, ev events.Event,
	remembered []state.ObjID, rememberedPlayers []state.PlayerID, tokenOverride *state.Object, you state.PlayerID) bool {
	switch r.Event {
	case "Attached":
		if ev.Kind != events.Attach || len(ev.IDs) == 0 {
			return false
		}
		if v := r.Params["ValidCard"]; v != "" && !e.matchesSpecFrom(v, source, you, source) {
			return false
		}
		if v := r.Params["ValidTarget"]; v != "" && !e.matchesSpecFrom(v, ev.IDs[0], you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "TurnFaceUp":
		// The "as this is turned face up" class (CR 614.1a with CR 708.6/
		// CR 702.36e): the turned permanent is the turn-up event's own Obj
		// (events.TurnFaceUp), and ValidCard$ scopes it in the replacement
		// source's frame -- "Card.Self" for the source's own turn-up (Hooded
		// Hydra, Gift of Doom), "Permanent.OppCtrl" for an opponent's (Karlov
		// Watchdog). A non-turn-up event carries nothing the clause can name,
		// so it fails closed. The match reads the object's live pre-flip state
		// (still face down), which is exactly the state the replacement applies
		// to; no face-down override is needed because no carrier spec names a
		// face-down characteristic here.
		if ev.Kind != events.TurnFaceUp || ev.Obj == 0 {
			return false
		}
		if v, ok := r.Params["ValidCard"]; ok && v != "" {
			if !e.matchesSpec(v, ev.Obj, e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		return e.replacementConditionHolds(r, source, you)
	case "Counter":
		// The Effect-created bodyless CantHappen form (Mistrise Village's
		// AntiMagic, reached only from counterReplacementMatchesAll's scan,
		// which passes a synthetic Event{Obj: target}): the remembered-scoped
		// ValidCard$ gates the countered stack object — the promise covers
		// exactly the spell the firing trigger captured. ValidSA$ is the
		// caller's counterValidSA read (shared with the printed-Repls path);
		// no ActiveZones read — an Effect's lifetime is active()'s, not its
		// source's zone.
		if v, ok := r.Params["ValidCard"]; ok {
			if !e.matchesSpec(v, ev.Obj, e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		return true
	case "Moved":
		if ev.Kind != events.MoveZone {
			return false
		}
		// Discard$ True narrows a Moved replacement to a discard. EffectOnly$
		// excludes cost and cleanup discards; ValidCause$ names its cause.
		if r.ParamStr(cards.PKDiscard) == "True" {
			if !events.IsDiscard(ev) {
				return false
			}
			if r.ParamStr(cards.PKEffectOnly) == "True" && (events.IsDiscardCost(ev) || e.actionCause() == 0) {
				return false
			}
			if spec := r.ParamStr(cards.PKValidCause); spec != "" && !e.discardCauseAdmits(spec, source, ev) {
				return false
			}
		}
		if o, ok := r.Param(cards.PKOrigin); ok && o != "Any" && effects.ParseZone(o) != ev.From {
			return false
		}
		// A creature's "would die" replacement is about a permanent moving
		// from the battlefield to the graveyard (CR 700.4), not a creature
		// card being milled or otherwise moved there from another zone. Forge
		// scripts commonly encode this with a creature ValidLKI and a graveyard
		// destination but omit Origin$; keep that shape from matching non-BF
		// moves while leaving explicit from-anywhere replacements alone.
		if hasOrigin := r.HasParam(cards.PKOrigin); !hasOrigin && ev.To == state.ZGraveyard {
			if validLKI := r.ParamStr(cards.PKValidLKI); strings.HasPrefix(validLKI, "Creature.") && ev.From != state.ZBattlefield {
				return false
			}
		}
		if d, ok := r.Param(cards.PKDestination); ok && d != "Any" && effects.ParseZone(d) != ev.To {
			return false
		}
		// FoundSearchingLibrary$ True (Opposition Agent's "While an opponent
		// is searching their library, they exile each card they find"): the
		// replacement applies only to the moves a library search emits. The
		// host scopes that fact (BeginLibrarySearch/EndLibrarySearch around
		// effects' applyLibrarySearch); with no search in flight, or with the
		// repl's own controller the one searching, the replacement is inert.
		if raw, ok := r.Param(cards.PKFoundSearchingLibrary); ok &&
			strings.EqualFold(strings.TrimSpace(raw), "True") {
			if e.searchingBy == 0 || e.controllerOf(source) == e.searchingBy {
				return false
			}
		}
		if v, ok := r.Param(cards.PKValidCard); ok {
			// The bare wasCastFromYourHandByYou qualifier (epochrasite's
			// etbCounter gate field `ValidCard$ Card.Self+
			// !wasCastFromYourHandByYou`: "enters with three +1/+1 counters on
			// it if you didn't cast it from your hand") is split out and
			// evaluated against the log here (task castprov1); the remainder
			// matches as before.
			spec, ok2 := e.castProvenanceAdmits(v, ev.Obj, you)
			sc := e.rememberedSpecContext(you, source, remembered)
			// CR 708.5: a face-down battlefield entry (Manifest, Cloak, or a
			// ChangeZone FaceDown$ True) has not yet been folded onto the
			// object -- events.Apply sets Object.FaceDown DURING the move it
			// intercepts, so at match time the object is still in its origin
			// zone with FaceDown false. Pass the derived override so a
			// ValidCard$ naming `faceDown` (Veiled Ascension's
			// `Creature.faceDown+YouCtrl`) admits the entry it names instead
			// of failing closed. events.IsFaceDownEntry is the one predicate
			// covering BOTH markers (the manifest/FaceDown$ Counter and the
			// cloak literal), so a third face-down marker cannot be missed.
			if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield &&
				events.IsFaceDownEntry(ev.Counter) {
				sc.AsFaceDown = true
			}
			if !ok2 || !e.matchesSpec(spec, ev.Obj, sc) {
				return false
			}
		}
		// CR 603.10/Forge ValidLKI: a look-back-in-time gate on the moving
		// object, evaluated against it as it is right before the move applies --
		// which for a replacement is its live state, since a replacement runs
		// ahead of the Move it intercepts. The same filter grammar as ValidCard,
		// evaluated with MatchesObjectCtx (the LKI-form matcher) so the object is
		// matched by value. CastSa qualifiers are evaluated from the paid cast's
		// CastInfo before the remaining card spec is matched, just as at the
		// other rules-side provenance sites.
		if v, ok := r.Params["ValidLKI"]; ok {
			mo := e.G.Obj(ev.Obj)
			if mo == nil {
				return false
			}
			spec, admitted := e.castSaAdmits(v, ev.Obj)
			if !admitted || !effects.MatchesObjectCtx(e.G, spec, mo, e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		// CheckSVar$/SVarCompare$ (kw:etbCounter's CheckSVar$ third field --
		// Lupine Harbingers' "enters with X +1/+1 counters ... since it was
		// foretold" gate) shares replacementConditionHolds with the
		// damage/counter families. The comment above its own declaration used
		// to say the Moved case never carries these gates in the corpus; the
		// etbCounter passthrough is the one carrier, and the shared read is a
		// no-op for every Moved line without the params.
		return e.replacementConditionHolds(r, source, you)
	case "Untap":
		if ev.Kind != events.Untap {
			return false
		}
		// ValidStepTurnToController$ You scopes the replacement to the untap
		// step's own turn-based action -- an activated or triggered untap
		// outside that step is not replaced (Basalt Monolith can still pay {3}
		// to untap itself). "Its controller's untap step" reads against the
		// card being untapped, which is what every corpus description says
		// (Sleep Paralysis's enchanted artifact vs. Basalt's itself); for a
		// ValidCard$ Card.Self line the two are the same player. A value other
		// than You fails closed.
		if s, ok := r.Params["ValidStepTurnToController"]; ok {
			if s != "You" || e.G.Step != state.StepUntap ||
				e.G.Active != e.controllerOf(ev.Obj) {
				return false
			}
		}
		if v, ok := r.Params["ValidCard"]; ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "BeginPhase":
		if ev.Kind != events.StepChange {
			return false
		}
		if ph, ok := r.Params["Phase"]; ok {
			step, known := phaseStep(ph)
			if !known || step != ev.Step {
				return false
			}
		}
		// ValidPlayer$ You scopes "skip YOUR draw step" to the replacement
		// controller's own turn; a line with no ValidPlayer$ (Sands of Time's
		// "players skip their untap step") applies every turn.
		if vp, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, vp, e.G.Active, you) {
			return false
		}
		// Optional$ True is handled after matching by
		// posePhaseReplacementChoice: applicability is independent of whether
		// the affected player eventually chooses to apply it.
		// Hellbent$ True gates the skip on an empty hand (one corpus line).
		if r.Params["Hellbent"] == "True" && len(e.G.Zone(state.ZHand, you)) > 0 {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "BeginTurn":
		// The skip-an-extra-turn class (Trouble in Pairs, Stranglehold,
		// Ugin's Nexus, Gerrard's Hourglass Pendant). Reached ONLY through the
		// synthetic events.ExtraTurn{Amount: 0} event extraTurnSkipped poses
		// at consumption time: no per-turn "would begin" log event exists, so
		// replacementEvent deliberately maps none and applyReplacements never
		// routes a BeginTurn replacement. The synthetic event carries Amount 0,
		// which no real ExtraTurn event ever carries (grants are positive,
		// consumptions -1), so the synthetic shape cannot collide with a real
		// one even if one were ever scanned.
		if ev.Kind != events.ExtraTurn || ev.Amount != 0 {
			return false
		}
		// Requiring ExtraTurn$ True is what keeps Time Vault out: its R:
		// Event$ BeginTurn line skips a NORMAL turn (Optional$ True, a
		// ReplaceWith$ body, IsPresent$ Card.Self+tapped, no ExtraTurn$), a
		// different shape this task deliberately does not implement -- a
		// matcher without the requirement would change that card's behaviour
		// without implementing it.
		if r.Params["ExtraTurn"] != "True" {
			return false
		}
		// ValidPlayer$ Opponent scopes the skip to opponents of the
		// replacement's controller (Trouble in Pairs, Stranglehold); a line
		// with no ValidPlayer$ (Ugin's Nexus, Gerrard's Hourglass Pendant)
		// applies to ANY player's extra turn, the controller's own included.
		if vp, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		// Optional$-gated and ReplaceWith$-bearing shapes are not implemented:
		// extraTurnSkipped reports a matched line whose action is not Skip$
		// True loudly instead of silently skipping, and never silently skips.
		return e.replacementConditionHolds(r, source, you)
	case "Transform":
		if ev.Kind != events.FlipFace {
			return false
		}
		// The "as this transforms" replacement is written on the destination
		// face and applies to its own card's flip; replacementFace already
		// scanned the destination face for this event.
		if v, ok := r.Params["ValidCard"]; ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "GainLife":
		if ev.Kind != events.LifeChange || ev.Amount <= 0 {
			return false
		}
		if vp := strings.TrimSpace(r.Params["ValidPlayer"]); vp != "" {
			if vp == "Player.IsRemembered" {
				found := false
				for _, p := range rememberedPlayers {
					if p == ev.Player {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			} else if !effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
				return false
			}
		}
		return e.replacementConditionHolds(r, source, you)
	case "DamageDone":
		// A Damage event with a non-positive Amount is not damage being
		// dealt: it is the cleanup step's CR 514.2 removal of marked damage
		// (cleanupBody's negative Damage) or a hit already reduced to zero
		// (CR 120.8: 0 damage is never dealt). No DamageDone replacement
		// applies to it -- matching it posed CR 616.1 order asks at every
		// cleanup under two Ghosts of the Innocent, and each answer's
		// halving re-emitted the removal, forever (fuzz batch6 line 9).
		if ev.Kind != events.Damage || ev.Amount <= 0 ||
			!e.damageReplacementMatches(r, source, ev, remembered, rememberedPlayers) {
			return false
		}
		if v, ok := r.Params["ValidCard"]; ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "ProduceMana":
		// Only genuine production replaces: a ManaAdd without a producing
		// source (a test seed, a spend) and a negative Amount (spending, not
		// producing) are outside the class.
		if ev.Kind != events.ManaAdd || e.manaProducer == 0 || ev.Amount <= 0 || !e.manaFromTap {
			return false
		}
		if v, ok := r.Params["ValidCard"]; ok &&
			!e.matchesSpecFrom(v, e.manaProducer, you, source) {
			return false
		}
		// ValidActivator$ You: the player adding the mana (whoever activated
		// the mana ability) must be the replacement controller's side of the
		// spec. MatchesPlayerSpec fails closed on unknown qualifiers.
		if va, ok := r.Params["ValidActivator"]; ok &&
			!effects.MatchesPlayerSpec(e.G, va, ev.Player, you) {
			return false
		}
		// ReplaceOnly$ lives on the ReplaceWith$ body (Quarum Trench
		// Gnomes), but it is an applicability gate: converting a different
		// colour is not applying that replacement. Reading it here lets a
		// prior rewrite make the effect newly applicable during CR 616.1's
		// mandatory post-rewrite recheck.
		if r.With != nil {
			if only := strings.TrimSpace(r.With.Params["ReplaceOnly"]); only != "" && only != ev.Counter {
				return false
			}
		}
		// ManaAmount$ <op><n> gates on the size of the production being
		// replaced (Damping Sphere's "two or more mana").
		if ma, ok := r.Params["ManaAmount"]; ok {
			op, n, parsed := splitCompare(ma)
			if !parsed || !applyCompare(int(ev.Amount), op, n) {
				return false
			}
		}
		return e.replacementConditionHolds(r, source, you)
	case "Explore":
		// The explore replacement (R:Event$ Explore, task explore1 —
		// Topography Tracker, Twists and Turns). ValidExplorer$ names the
		// creature that would explore (the synthetic proposal's Obj), matched
		// with the replacement source's controller as You exactly like every
		// other object-spec gate here.
		if ev.Kind != events.Explore {
			return false
		}
		if v, ok := r.Params["ValidExplorer"]; ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "Scry":
		// The scry replacement (R:Event$ Scry, task scryrepl — Kenessos,
		// Priest of Thassa; Eligeth, Crossroads Augur). The event is the
		// synthetic instruction PROPOSAL effects' effLookAndArrange builds
		// before looking (there is no printed-forge "Scry" event); ValidPlayer$
		// names the scrying seat, matched with the replacement source's
		// controller as You exactly like every other player-spec gate here.
		// The completed events.Scry record (the bottom-card marker trig:Scry
		// matches) is emitted OUTSIDE the replacement pass, so it can never
		// reach this case.
		if ev.Kind != events.Scry {
			return false
		}
		if v, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, v, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "RollDice":
		// The roll-action replacement (R:Event$ RollDice, task rolldice-repl
		// -- Wyll, Blade of Frontiers; Barbarian Class; Pixie Guide; the
		// SwapRoll carrier Vedalken Squirrel-Whacker). The event is the
		// synthetic roll PROPOSAL effects' effRollDice builds before any die
		// is rolled (CR 614.4's before-the-action window); the per-die and
		// batch Notes are the roll's only log witnesses and are emitted after
		// this window, so a proposal can never collide with a logged event.
		// ValidPlayer$ names the roller, matched with the replacement source's
		// controller as You exactly like every other player-spec gate here.
		if ev.Kind != events.RollDice {
			return false
		}
		if v, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, v, ev.Player, you) {
			return false
		}
		// ValidSides$ (the Whacker's six-sided gate) is unread: the proposal
		// carries no die-size field to gate on, and its one corpus carrier is
		// the unmodelled SwapRoll body the dispatch skips loudly -- a sides
		// gate is never silently widened onto a die of another size.
		return e.replacementConditionHolds(r, source, you)
	case "Draw", "DrawCards":
		if ev.Kind != events.Draw {
			return false
		}
		// CR 611.3b: the ActiveZones gate above applies (a Draw replacement's
		// source is already on the battlefield — there is no entering case,
		// a card cannot replace the draw of the event that would put it into
		// play).
		if v, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, v, ev.Player, you) {
			return false
		}
		// NotFirstCardInDrawStep$ True exempts the player's own turn-based
		// draw (CR 504.1) — the "except the first one they draw in each of
		// their draw steps" clause on Notion Thief, Hullbreacher, Chains of
		// Mephistopheles and the other five carriers. Applied at match time,
		// before the proposed Draw is logged, so the pre-emit helper is the
		// one that can see it.
		if strings.EqualFold(strings.TrimSpace(r.Params["NotFirstCardInDrawStep"]), "True") &&
			e.pendingDrawIsFirstInDrawStep(ev.Player) {
			return false
		}
		// ActivePhases$ <spec>: the step set the replacement is confined to
		// (Island Sanctuary's "during your draw step", the class's one
		// carrier). An unresolvable element or a step outside the set fails
		// closed, never widened — the same shared, cached phase parser and
		// idiom activationPhasesOK and phaseGate use, so the phase-name
		// semantics cannot drift between the offer, trigger and replacement
		// gates. Pure read: no event is emitted from a match.
		if raw, ok := r.Params["ActivePhases"]; ok {
			spec := strings.TrimSpace(raw)
			if spec != "" {
				pp := e.parsedPhaseSpec(spec)
				if !pp.valid || pp.set.Empty() || !pp.set.Has(e.G.Step) {
					return false
				}
			}
		}
		// FirstExtraCardDrawnThisTurn$ True (Reed Richards, Smartest Man) is
		// CR 614.1a's "the first time each turn": the replacement applies to
		// the first extra draw of the turn only. The pending draw is exempt if
		// it is the CR 504.1 turn-based draw (pendingDrawIsFirstInDrawStep,
		// the pre-emit test) OR if an earlier extra draw already happened this
		// turn (extraDrawsThisTurn). The body's own re-draws run under the
		// applyingReplacement guard and are not re-matched, so this counts only
		// draws the player would otherwise make.
		if strings.EqualFold(strings.TrimSpace(r.Params["FirstExtraCardDrawnThisTurn"]), "True") {
			if e.pendingDrawIsFirstInDrawStep(ev.Player) || e.extraDrawsThisTurn(ev.Player) > 0 {
				return false
			}
		}
		// ValidCause$ <stack spec>: the Draw replacement is confined to draws
		// caused by a matching spell or ability (Unpredictable Cyclone's
		// `Activated.Cycling+nonLand`, the class's only carrier). The cause is
		// the top of the resolving stack -- a Draw emitted during an ability's
		// resolution happens while that ability is still there. An absent or
		// empty spec keeps the replacement unscoped; an ordinary draw with
		// nothing on the stack is not caused by anything and so never admits.
		if spec := strings.TrimSpace(r.Params["ValidCause"]); spec != "" &&
			!e.drawCauseAdmits(spec, source, ev) {
			return false
		}
		// The shared condition gate (CheckSVar$/IsPresent$/Hellbent$/...) —
		// every sibling case ends with it; the Draw class never read it, so
		// Quantum Riddler's LE1-over-Count$ValidHand gate (and the Hellbent
		// DrawTwo / library-empty Win carriers) fired unconditionally. No
		// repo deck carries any of the class's 39 carriers, so no golden
		// game changes (measured).
		return e.replacementConditionHolds(r, source, you)
	case "CreateToken":
		// The token-creation replacement class (Divine Visitation, Doubling
		// Season, Academy Manufactor, Xorn, ...). Applied by
		// continueCreateTokenReplacements, which reads each body's Type$
		// directly in rules — the replaceDamageAmount precedent — rather than
		// dispatching through the effects registry (no api:ReplaceToken
		// resolver exists; the census registers the name via RegisterNonAPI).
		if ev.Kind != events.TokenCreate {
			return false
		}
		// ValidToken$ names the WOULD-BE token, which does not exist yet: the
		// match is taken against a shallow read-side snapshot built off the
		// token script the event names (the same never-added-to-the-game
		// discipline StackCopy's snapshot keeps). The spec's You-side
		// predicates (YouCtrl, ...) read against the replacement SOURCE's
		// controller, while the token's controller is ev.Player — exactly how
		// Divine Visitation's "creature tokens under YOUR control" must read.
		// An unknown token key fails closed to no match.
		if v, ok := r.Params["ValidToken"]; ok {
			tok := tokenOverride
			if tok == nil {
				tok = e.tokenSnapshot(ev)
			}
			if tok == nil || !effects.MatchesObjectCtx(e.G, v, tok,
				e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		if vp, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpecFrom(e.G, vp, ev.Player, you, source) {
			return false
		}
		// EffectOnly$ True ("If an EFFECT would create ...", Doubling Season's
		// family) is held by construction: the engine's only TokenCreate
		// emitters are effect resolution (effects/token.go's effToken and
		// effects/amass.go), so every token creation IS effect-created and the
		// gate is vacuously satisfiable. No code reads the param yet -- a
		// cost-created-token provenance marker, when one lands, must read it
		// here. This "vacuously satisfiable" reading is the TOKEN class's
		// alone: the AddCounter case below DOES read EffectOnly$, because
		// CounterChange has non-effect emitters (turn-based actions, costs).
		return e.replacementConditionHolds(r, source, you)
	case "AddCounter":
		// The counter-placement replacement class (Hardened Scales, Branching
		// Evolution, Doubling Season, Vorinclex, ...). Applied by
		// applyAddCounterReplacements, which reads each body's ReplaceCounter
		// params directly in rules -- the ReplaceToken/replaceDamageAmount
		// precedent -- rather than dispatching through the effects registry
		// (no api:ReplaceCounter resolver exists; the census registers the
		// name via RegisterNonAPI).
		//
		// Only a POSITIVE placement is replaceable: a CounterChange that
		// removes counters (a SubCounter cost, a -1/-1 wipe) is never an
		// AddCounter event.
		if ev.Amount <= 0 {
			return false
		}
		// ... and neither is one of the engine's own status markers, which
		// ride a CounterChange for want of a status field and are emitted
		// with a POSITIVE amount, so the sign guard above does not exclude
		// them. See state.InternalCounterMarker.
		if state.InternalCounterMarker(ev.Counter) {
			return false
		}
		// ValidCounterType$ names the kind of counter being added and appears
		// on the R: line (Hardened Scales) or the body (Melira). A line naming
		// a kind other than the event's fails closed; an absent kind admits
		// every kind (Winding Constrictor's "one or more counters").
		if ct := strings.TrimSpace(r.Params["ValidCounterType"]); ct != "" && ct != ev.Counter {
			return false
		}
		// ValidPlayer$ scopes the counter's RECIPIENT PLAYER, so it only
		// applies to the player form (PlayerCounterChange). An object
		// CounterChange leaves ev.Player at its zero value, so without this
		// form gate a ValidPlayer$ You line reduces to ev.Player == you ->
		// 0 == 0 -> true and fires on every object placement (Winding
		// Constrictor has both an object line and a ValidPlayer$ You line).
		if vp, ok := r.Params["ValidPlayer"]; ok {
			if ev.Kind != events.PlayerCounterChange ||
				!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
				return false
			}
		}
		// ValidCard$/ValidObject$ name the counter RECIPIENT. The object form
		// (CounterChange) matches it by that object's filter; the player form
		// (PlayerCounterChange) carries no object, so an object-scoped line
		// fails closed for it. A line with neither key applies to either form,
		// which is what the "any counters / any permanent or player" shapes
		// (Doubling Season's ValidCard$ Permanent, Vorinclex's ValidObject$)
		// mean.
		spec := strings.TrimSpace(r.Params["ValidCard"])
		if spec == "" {
			spec = strings.TrimSpace(r.Params["ValidObject"])
		}
		if spec != "" {
			if ev.Kind != events.CounterChange {
				return false
			}
			if !e.matchesSpecFrom(spec, ev.Obj, you, source) {
				return false
			}
		}
		// ValidSource$ You/Opponent names the player CAUSING the placement -- a
		// role the CounterChange event does not carry (it records the recipient
		// only), so it is read from the engine's in-flight adder scratch
		// (counterAdder, published at cost/turn-based sites) or, absent a
		// publication, from the resolving ability's controller. When NEITHER is
		// known (an SBA or other bare placement) the line fails closed rather
		// than matching every placement -- the conservative direction
		// (Vorinclex's "If you would put ...", Halving Season's opponent
		// half). This is the Vorinclex source scope: one placement has exactly
		// one adder, so its "you" and "opponent" lines are mutually exclusive
		// and never compete.
		if vs := strings.TrimSpace(r.Params["ValidSource"]); vs != "" {
			adder, ok := e.inFlightCounterAdder()
			if !ok || !effects.MatchesPlayerSpec(e.G, vs, adder, you) {
				return false
			}
		}
		// ValidCause$ names the object that caused the placement (Zabaz's
		// "a modular triggered ability would put ..."): the resolving stack
		// object, exactly the provenance the Moved case's ValidCause$ reads.
		// An absent cause (0) fails closed in replacementCauseMatches.
		if vc := strings.TrimSpace(r.Params["ValidCause"]); vc != "" {
			if !e.replacementCauseMatches(vc, source, e.actionCause()) {
				return false
			}
		}
		// EffectOnly$ True (Doubling Season, Selesnya Loft Gardens) admits only
		// placements that are the EFFECT of a resolving spell or ability ("If an
		// EFFECT would put one or more counters ..."). It excludes a placement
		// with no object on the stack: a turn-based action (a Saga's lore
		// counter, rules/saga.go advanceSagas) and a cost (a planeswalker's [+N]
		// loyalty counter, rules/cast.go emitChoiceCosts; a station counter,
		// rules/station.go handleStation) are not effects, and admitting them
		// doubled counters they must not touch. This is exactly the
		// actionCause()==0 provenance the Moved case's EffectOnly$ gate reads
		// (costs are paid before an activated ability exists on the stack, so
		// they deliberately have no cause) -- one shared test, not a second
		// hand-built identity stamp. A resolving TRIGGERED ability's instruction
		// (a cumulative-upkeep age counter, rules/cumulative.go) IS an effect
		// (CR 609.1), so it still qualifies.
		if r.Params["EffectOnly"] == "True" && e.actionCause() == 0 {
			// A replacement BODY's counter placement (the K:etbCounter entry
			// body's DB$ PutCounter) also has no stack cause by the time it
			// emits -- the entry move has already applied and the wrapper is
			// off the stack -- but it IS the action of a replacement effect
			// (CR 614.1c), and this wording reaches it: CR 614.5, the
			// replacement's instruction is a new event the AddCounter class
			// modifies (Doubling Season doubles a planeswalker's starting
			// loyalty, the class's own precedent). A cost or turn-based
			// placement is never made from inside a replacement body, so the
			// exclusion keeps its teeth there.
			if _, isBody := e.replacementBodyCounterAdder(); !isBody {
				return false
			}
		}
		return e.replacementConditionHolds(r, source, you)
	case "RollPlanarDice":
		// The planar-dice replacement class (Ichor Elixir, task rollplanar1):
		// "if you would roll one or more planar dice, instead roll that many
		// planar dice plus one and ignore one". ValidPlayer$ scopes the roller
		// the same way the Draw class reads it; the count/ignore rewrites are
		// the With's own ReplaceEffect bodies (ReplaceEvent's PlanarRoll arm).
		if ev.Kind != events.PlanarRoll {
			return false
		}
		if vp, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "Cascade":
		// The cascade instruction's replacement boundary (CR 614.4;
		// Averna, the Chaos Bloom's `ValidPlayer$ You | ActiveZones$
		// Battlefield`). Only the synthetic proposal reaches here, so there
		// is no logged event and no ValidCard$: the caster is ev.Player and
		// ValidPlayer$ scopes it exactly as the Draw/ProduceMana classes do.
		if ev.Kind != events.Cascade {
			return false
		}
		if vp, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case "GameLoss", "GameWin":
		// The "you can't lose the game" / "your opponents can't win the
		// game" class (CR 104.3 / 704.5a-c, task fdn-repl-cant-lose). Only
		// the SYNTHETIC proposal reaches here (Engine.gameLossPrevented /
		// gameWinPrevented pose it with the affected player and, for a loss,
		// the Forge lose reason in ev.Text): the event log has no GameLoss or
		// GameWin event, so replacementEvent deliberately maps none and
		// applyReplacements never routes one. Requiring the matching event
		// kind keeps a stray face line from matching a real PlayerLost or
		// GameOver log event through some future route. ValidLoseReason$ is
		// Forge's reason discriminator (life/poison/commander/mill/effect): an
		// ABSENT reason applies to every cause, a PRESENT one only to its own
		// (Lich's Tomb's "don't lose for having 0 or less life" must not stop
		// a deck-out), so an unknown reason fails closed. The shared condition
		// gate below carries IsPresent$ (Pact Weapon's attach rider) and
		// CheckSVar$ (Platinum Angel Avatar's four-type gate, which fails
		// closed until those SVars resolve).
		//
		// The Layer$ CantHappen requirement is the class's whole meaning (a
		// GameLoss line with a ReplaceWith$ body -- Lich's Mirror, Exquisite
		// Archangel -- is the out-of-scope Lich family, never a "can't").
		// gameEventCantHappen already pre-filters on it; repeating it here
		// keeps this matcher self-consistent for any future caller.
		if !strings.EqualFold(strings.TrimSpace(r.Params["Layer"]), "CantHappen") {
			return false
		}
		if r.Event == "GameLoss" {
			if ev.Kind != events.PlayerLost {
				return false
			}
			if reason, ok := r.Params["ValidLoseReason"]; ok &&
				!strings.EqualFold(strings.TrimSpace(reason), ev.Text) {
				return false
			}
		} else if ev.Kind != events.GameOver {
			return false
		}
		if vp, ok := r.Params["ValidPlayer"]; ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	}
	return false
}

// mintSnapshot builds the would-be token a plan mint creates: the token
// script the event names, or -- for a copy mint -- the copied object's
// printed face as a never-added-to-the-game token (CR 706.2, the
// chosenCopySnapshot discipline). Both the ValidToken$ matcher read and the
// ValidCard$ re-check route through this one helper so a copy mint can never
// be rechecked as an empty script again.
func (e *Engine) mintSnapshot(mintEv events.Event, mint tokenPlanMint) *state.Object {
	if mint.copyOf != 0 {
		return e.chosenCopySnapshot(mint.copyOf, mintEv.Player)
	}
	return e.tokenSnapshot(mintEv)
}

// tokenSnapshot builds the would-be token a TokenCreate event would mint,
// as a shallow read-side object for ValidToken$ matching. Never added to
// the game — a value snapshot like StackCopy's discipline. A nil return
// (unknown token key) fails the caller's match closed.
func (e *Engine) tokenSnapshot(ev events.Event) *state.Object {
	def := e.G.Tokens[ev.Text]
	if def == nil {
		return nil
	}
	// Zone is the battlefield: the would-be token is being created ONTO the
	// battlefield, and a `ValidToken$ Permanent...` gate (Flitwing Lyev's
	// `Permanent.YouCtrl`) reads matchesBase's battlefield requirement for
	// the bare Permanent base. Leaving the zero (library) value made that
	// spelling fail closed and the replacement never apply.
	return &state.Object{Card: def, IsToken: true, Owner: ev.Player, Controller: ev.Player,
		Zone: state.ZBattlefield}
}

// chosenCopySnapshot is the ValidCard$ re-check's read-side snapshot of a
// COPY plan mint: the copied object's printed face as a never-added-to-
// the-game token (the tokenSnapshot discipline). A vanished source
// snapshots nothing (fail closed).
func (e *Engine) chosenCopySnapshot(src state.ObjID, player state.PlayerID) *state.Object {
	o := e.G.Obj(src)
	if o == nil || o.Card == nil {
		return nil
	}
	return &state.Object{Card: o.Card, FaceIdx: o.FaceIdx, IsToken: true,
		Owner: player, Controller: player, Zone: state.ZBattlefield}
}

// phaseStep maps a Forge Phase$ value on a BeginPhase replacement onto the
// engine's step constants. Only the three steps the corpus names are known;
// any other value (or a comma list this build does not split) fails closed,
// leaving the phase to run normally.
func phaseStep(ph string) (state.Step, bool) {
	switch strings.TrimSpace(ph) {
	case "Untap":
		return state.StepUntap, true
	case "Upkeep":
		return state.StepUpkeep, true
	case "Draw":
		return state.StepDraw, true
	}
	return 0, false
}

// replacementConditionHolds evaluates the condition parameters a replacement
// R: line can carry besides its event gates: IsPresent$ with an optional
// PresentCompare$ (default "at least one", the intervening-if reading the
// corpus's aura lines use: "if you control a Reflection"), and
// CheckSVar$/SVarCompare$ (an SVar value compared against a threshold). The
// SVar is evaluated in the replacement source's own context, exactly as a
// trigger's condition would be. A clause this build cannot evaluate -- an
// unknown compare literal, a missing SVar, a non-Count$ body -- fails
// closed: the replacement does not apply, never that an unreadable count is
// presumed large enough to let it.
func (e *Engine) replacementConditionHolds(r cards.Repl, source state.ObjID, you state.PlayerID) bool {
	// A kw:Class level band (ClassBand$) is an independent AND gate: this
	// function reads only IsPresent$, so a band written anywhere else would be
	// silently ignored and a level-N granted replacement would be live from
	// level 1.
	if !e.classBandGateHolds(r.Params, source) {
		return false
	}
	if spec, ok := r.Params["IsPresent"]; ok {
		cmp := r.Params["PresentCompare"]
		if cmp == "" {
			cmp = "GE1"
		}
		var n int
		if _, hasZone := r.Params["PresentZone"]; hasZone || r.Params["PresentDefined"] != "" {
			// A damage/counter/CantPreventDamage line's IsPresent$ can name a
			// non-battlefield zone (PresentZone$) or a defined subject
			// (PresentDefined$ Self, "is this exact permanent still present");
			// countPresentInZone generalises past countPresent's fixed
			// battlefield scan for exactly those two params.
			zone := state.ZBattlefield
			if z := r.Params["PresentZone"]; z != "" {
				zone = effects.ParseZone(z)
			}
			n = e.countPresentInZone(spec, source, you, zone, r.Params["PresentDefined"])
		} else {
			n = e.countPresent(spec, source, you)
		}
		if !comparePresent(n, e.presentCompareFor(cmp, source, you)) {
			return false
		}
	}
	// PlayerTurn$/Hellbent$/Revolt$/Delirium$/CheckDefinedPlayer$ are the
	// remaining condition gates shared by damage, counter and
	// CantPreventDamage text (the Moved/Untap/BeginPhase/Transform/
	// ProduceMana cases above never carry them in the corpus, so folding them
	// in here rather than duplicating the switch costs those cases nothing).
	if strings.EqualFold(r.Params["PlayerTurn"], "True") && e.G.Active != you {
		return false
	}
	if strings.EqualFold(r.Params["Hellbent"], "True") && len(e.G.Zone(state.ZHand, you)) != 0 {
		return false
	}
	if strings.EqualFold(r.Params["Revolt"], "True") && !e.revoltThisTurn(you) {
		return false
	}
	if strings.EqualFold(r.Params["Delirium"], "True") && e.graveyardCardTypeCount(you) < 4 {
		return false
	}
	// EnduringStory$ (Bombur, Gentle Dreamer) is the CR 702.175 "unless you
	// have an enduring story" gate in the PARAMETER form, the sibling of
	// Condition$ EnduringStory's continuous-static read. The value is a
	// boolean literal compared against the seat's one-way latch: False holds
	// only while the replacement's controller has NO enduring story (so the
	// can't-untap replacement applies), True only while they do. Bombur is the
	// corpus's sole carrier; an unrecognised value fails closed like every
	// other condition gate here.
	if raw, ok := r.Params["EnduringStory"]; ok {
		switch strings.TrimSpace(raw) {
		case "True", "true":
			if !e.playerHasEnduringStory(you) {
				return false
			}
		case "False", "false":
			if e.playerHasEnduringStory(you) {
				return false
			}
		default:
			return false
		}
	}
	if _, ok := r.Params["CheckDefinedPlayer"]; ok {
		// The only corpus shape is You.isMonarch. Monarch state is not yet
		// represented, so fail closed instead of preventing damage always.
		return false
	}
	if check, ok := r.Params["CheckSVar"]; ok {
		n := e.replacementCheckValue(source, check)
		if cmp := r.Params["SVarCompare"]; cmp != "" {
			op, rhs, valid := splitCompare(strings.TrimSpace(cmp))
			if !valid || !applyCompare(int(n), op, rhs) {
				return false
			}
		} else if n == 0 {
			return false
		}
	}
	return true
}

// damageReplacementMatches applies the damage-specific R: filters before the
// common active-zone gate: source and target are the actual damage source and
// recipient, and IsCombat$/DamageAmount$ describe this in-flight event. The
// remembered/rememberedPlayers lists are an EFFECT-created match's own capture
// (nil/nil for every printed line); a shield scopes by them directly, every
// other filter evaluates as before.
func (e *Engine) damageReplacementMatches(r cards.Repl, source state.ObjID, ev events.Event,
	remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool {
	ctrl := e.controllerOf(source)
	// A prevention shield (effects' PreventDamage registration, marker
	// PreventionShield) scopes by ITS OWN captured recipients: membership in
	// the registration's Remembered (objects) / RememberedPlayers (players)
	// lists, never a ValidTarget$ filter spec. The filter grammar is
	// deliberately bypassed — its IsRemembered predicate UNIONs the source's
	// event-backed remembered list with the registration's capture, which
	// would let unrelated remembered state widen the promise. A shield with
	// neither list (unreachable from the registering primitive) fails closed.
	if strings.EqualFold(strings.TrimSpace(r.Params["PreventionShield"]), "True") {
		if ev.Obj != 0 {
			for _, id := range remembered {
				if id == ev.Obj {
					return true
				}
			}
			return false
		}
		for _, p := range rememberedPlayers {
			if p == ev.Player {
				return true
			}
		}
		return false
	}
	if v := r.Params["ValidCause"]; v != "" && !e.replacementCauseMatches(v, source, e.damaging) {
		return false
	}
	// A DB$ ReplaceDamage body must RESOLVE its Amount$ before this
	// replacement may match: the body is Forge's "prevent N of that damage"
	// idiom, and a match this build cannot price would previously be applied
	// as a silent FULL prevention (the body's emissions were supposed to
	// replace the event, so the engine discarded it -- and the body emitted
	// nothing). Amount$ values resolvable through the shared numeric grammar
	// (a literal; an SVar such as Battletide's AlchemicX or a card-defined
	// ShieldAmount; an inline Count$/ReplaceCount$ expression) match; an
	// unmodelled value frame (Power Leak's PaidAmount, an undefined name)
	// fails closed and leaves the damage untouched, per CR 616.1's "only
	// applicable replacements apply".
	if r.With != nil && r.With.API == "ReplaceDamage" {
		if _, ok := e.replaceDamageAmount(ev, replMatch{id: source, repl: &r}); !ok {
			return false
		}
	}
	// An Optional$ True damage replacement whose OptionalDecider$ names a
	// frame this build does not resolve also fails closed: asking the damaged
	// player would answer a "may" that belongs to somebody else (the
	// Battletide Alchemist round-2 finding). An ABSENT parameter keeps the
	// historical default, where the affected player answers.
	if strings.EqualFold(r.Params["Optional"], "True") {
		if v := strings.TrimSpace(r.Params["OptionalDecider"]); v != "" && v != "You" {
			return false
		}
	}
	if v := r.Params["ValidSource"]; v != "" {
		// The source filter is evaluated through the shared remembered/chosen
		// context, not a bare MatchesSpecFrom: a ChooseSource replacement names
		// the chosen damage source with a ChosenCard/ChosenCardStrict predicate
		// (Deflecting Palm's `Card.ChosenCardStrict,Emblem.ChosenCard`), which
		// reads the chosen list the Choose event recorded on the replacement's
		// OWN source object. Source-specific rather than the resolution's
		// Ctx.Chosen: the damage replacement fires while some later object
		// resolves, and the promise belongs to the object that chose.
		if e.damaging == 0 ||
			// nil remembered: only the chosen half is added here, so an
			// Effect-created `ValidSource$ Card.IsRemembered` line keeps the
			// exact match it had before ChooseSource landed.
			!e.matchesSpec(v, e.damaging, e.rememberedSpecContext(ctrl, source, nil)) {
			return false
		}
	}
	if v := r.Params["ValidTarget"]; v != "" {
		if ev.Obj != 0 {
			if !e.matchesSpecFrom(v, ev.Obj, ctrl, source) {
				return false
			}
		} else if !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
			return false
		}
	}
	if combat := strings.TrimSpace(r.Params["IsCombat"]); combat != "" &&
		((strings.EqualFold(combat, "True") && !e.combatDamaging) ||
			(strings.EqualFold(combat, "False") && e.combatDamaging)) {
		return false
	}
	return e.replacementAmountMatches(r.Params["DamageAmount"], ev.Amount, e.replCtx(replMatch{id: source, repl: &r}, ev))
}

// replacementAmountMatches understands Forge's comparison shorthand such as
// LTX (Ojer Axonil). Its RHS is resolved in the replacement source's context.
func (e *Engine) replacementAmountMatches(spec string, amount int32, c *effects.Ctx) bool {
	if spec == "" {
		return true
	}
	for _, op := range []string{"GE", "GT", "LE", "LT", "EQ"} {
		if rhs, ok := strings.CutPrefix(spec, op); ok {
			v := effects.Num(e, c, &cards.SA{Params: map[string]string{"N": rhs}}, "N", 0)
			switch op {
			case "GE":
				return amount >= v
			case "GT":
				return amount > v
			case "LE":
				return amount <= v
			case "LT":
				return amount < v
			case "EQ":
				return amount == v
			}
		}
	}
	return false
}

// ReplaceEvent implements effects.Host. It rewrites the amount of the Damage
// event currently being replaced -- the one held in e.replacingEvent -- from
// either a resolved numeric value (a literal, or a Count$/SVar expression
// effects.Num already evaluated in the replacement source's context) or a
// ReplaceCount$ body, whose base is the HELD event's own amount and which
// only the host reading the in-flight event can resolve. Anything else -- an
// unresolvable value, an unknown field, a non-Damage event -- leaves the held
// event untouched: an amount replacement that cannot be computed is closer to
// the card than one that erases the damage or discards the event.
func (e *Engine) ReplaceEvent(name, raw string, resolved int32) {
	ev := e.replacingEvent
	if ev == nil {
		return
	}
	if ev.Kind == events.PlanarRoll || ev.Kind == events.RollDice {
		// The roll rewrite (task rollplanar1's Ichor Elixir pair, extended to
		// the regular-dice proposal in rolldice-repl -- Wyll, Blade of
		// Frontiers; Barbarian Class; Pixie Guide): Number is the dice count
		// (the held event's Amount), Ignore the ignored-result count (the held
		// event's Counter, decimal; "" reads 0). A resolved literal value
		// applies as-is; an unresolvable ReplaceCount body leaves the field
		// alone, the same fail-closed direction the Damage arm keeps. Any
		// other VarName (SwapRoll's DicePTExchanges) is unmodelled and leaves
		// the proposal untouched; the dispatch reports that inertness loudly.
		// The planar-dice rewrite (Ichor Elixir's ReplaceEffect pair): Number
		// is the dice count (the held event's Amount), Ignore the ignored-
		// result count (the held event's Counter, decimal; "" reads 0). A
		// resolved literal value applies as-is; an unresolvable ReplaceCount
		// body leaves the field alone, the same fail-closed direction the
		// Damage arm keeps.
		if body, ok := strings.CutPrefix(raw, "ReplaceCount$"); ok {
			field, op, hasOp := strings.Cut(body, "/")
			if !hasOp {
				return
			}
			switch field {
			case "Number":
				ev.Amount = replCountOp(ev.Amount, op)
			case "Ignore":
				base := int32(0)
				if n, err := strconv.Atoi(ev.Counter); err == nil {
					base = int32(n)
				}
				ev.Counter = strconv.FormatInt(int64(replCountOp(base, op)), 10)
			}
			return
		}
		if name == "Number" && resolved > 0 {
			ev.Amount = resolved
		} else if name == "Ignore" && resolved > 0 {
			ev.Counter = strconv.FormatInt(int64(resolved), 10)
		}
		return
	}
	if ev.Kind != events.Damage {
		return
	}
	if body, ok := strings.CutPrefix(raw, "ReplaceCount$"); ok {
		field, op, hasOp := strings.Cut(body, "/")
		if (field != "DamageAmount" && field != "Amount") || !hasOp {
			return
		}
		ev.Amount = replCountOp(ev.Amount, op)
		return
	}
	if (name == "DamageAmount" || name == "Amount") && resolved > 0 {
		ev.Amount = resolved
		return
	}
	if name != "Affected" {
		return
	}
	switch raw {
	case "You":
		ev.Obj, ev.Player = 0, e.controllerOf(e.replacingSource)
	case "Self":
		ev.Obj, ev.Player = e.replacingSource, 0
	case "Enchanted", "Equipped":
		if source := e.G.Obj(e.replacingSource); source != nil && source.AttachedTo != 0 {
			ev.Obj, ev.Player = source.AttachedTo, 0
		}
	case "ReplacedSourceController":
		if source := e.G.Obj(e.damaging); source != nil {
			ev.Obj, ev.Player = 0, source.Controller
		}
	case "ReplacedTargetController":
		if target := e.G.Obj(ev.Obj); target != nil {
			ev.Obj, ev.Player = 0, target.Controller
		}
	case "Remembered":
		// Infer the destination kind from the captured GameEntity, rather than
		// parsing VarType$. Remembered permanents must still be on the
		// battlefield; remembered players are valid damage recipients while
		// they remain in the game. With no live referent the held event is
		// unchanged (fail closed).
		for _, target := range e.replRemembered {
			if target.IsPlayer {
				if int(target.Player) < len(e.G.Players) && !e.G.Players[target.Player].Lost {
					ev.Obj, ev.Player = 0, target.Player
					break
				}
				continue
			}
			if obj := e.G.Obj(target.Obj); target.Obj != 0 && obj != nil && obj.Zone == state.ZBattlefield {
				ev.Obj, ev.Player = target.Obj, 0
				break
			}
		}
	}
	// CR 702.90b: the infect marker on a Damage event encodes the FORM the
	// damage is dealt in, and the form depends on the RECIPIENT. A redirect
	// just changed the recipient (a player-targeted hit moved onto a
	// permanent, or vice versa), so the marker's recipient half is recomputed
	// here. The source-infect fact is preserved: the marker is only ever set
	// by an emitter whose source had infect, so a non-empty marker still means
	// infect. Without this a bare "infect" (player form) survives onto a
	// creature recipient: events.Apply treats a bare marker on an object as
	// ordinary marked damage while convertInfectDamage then also emits -1/-1
	// counters, so a redirected infect hit would land in BOTH forms.
	e.recomputeInfectMarker(ev)
	e.recomputeWitherMarker(ev)
}

// replCountOp applies Forge's ReplaceCount$ arithmetic to a base amount: the
// corpus carries Twice (Bloodletter of Aclazotz), Thrice (Fiery Emancipation)
// and Plus.N (Torture Pit). The arithmetic is the ONE shared implementation,
// effects.ApplyCountOp -- so the next op added to the /Op vocabulary (this
// adapter previously duplicated the switch by hand and silently lacked
// Negative and the whole Divide family) reaches ReplaceCount$ for free and
// the count grammar and the replacement grammar cannot drift.
//
// The one thing ReplaceCount$ adds over the count grammar is the CLAMP: a
// replacement cannot deal, gain or place a negative amount, so a body whose
// arithmetic drives the base below zero yields 0 (the count grammar has no
// such clamp -- a negative count is meaningful there). An op the shared
// applier does not parse is returned unchanged by it, the same fail-closed
// direction this adapter always kept.
func replCountOp(base int32, op string) int32 {
	v := effects.ApplyCountOp(base, op)
	if v < 0 {
		return 0
	}
	return v
}

func (e *Engine) replacementCheckValue(source state.ObjID, check string) int32 {
	o := e.G.Obj(source)
	if o == nil {
		return 0
	}
	ctx := e.replCtx(replMatch{id: source}, events.Event{})
	// The face's own SVar table comes FIRST: a CheckSVar$ X gate whose face
	// defines a real SVar:X body (Steel Exemplar's Count$Converge, Walking
	// Dream's PlayerCountOpponents$ head, the multiclass_baldric and
	// spirit_of_resistance bodies the switch below implements) must evaluate
	// THAT body through the machinery below -- the announced-X shortcut is
	// only the fallback for a face that defines no X. For a body that READS
	// the announced X (banefire's Count$xPaid) the two coincide, so the
	// reorder changes nothing for it.
	body := check
	if ctx.SVars != nil {
		if v, ok := ctx.SVars[check]; ok {
			body = v
		}
	}
	if body == check && check == "X" {
		return o.X
	}
	switch body {
	case "Count$Party":
		roles := map[string]bool{}
		for _, id := range e.G.Zone(state.ZBattlefield, o.Controller) {
			if f := e.G.Obj(id).Face(); f != nil {
				for _, typ := range f.Types {
					switch typ {
					case "Cleric", "Rogue", "Warrior", "Wizard":
						roles[typ] = true
					}
				}
			}
		}
		return int32(len(roles))
	case "Count$Valid Permanent.YouCtrl$Colors":
		colors := ""
		for _, id := range e.G.Zone(state.ZBattlefield, o.Controller) {
			colors += e.objColors(e.G.Obj(id))
		}
		var n int32
		for _, c := range "WUBRG" {
			if strings.ContainsRune(colors, c) {
				n++
			}
		}
		return n
	case "Count$Presence_Dragon.1.0":
		for _, id := range e.G.Zone(state.ZBattlefield, o.Controller) {
			if faceHasType(e.G.Obj(id), "Dragon") {
				return 1
			}
		}
		return 0
	}
	return effects.EvalCount(e, ctx, body)
}

func (e *Engine) countPresentInZone(spec string, source state.ObjID, you state.PlayerID, zone state.Zone, defined string) int {
	if defined == "Self" {
		o := e.G.Obj(source)
		if o == nil || o.Zone != zone {
			return 0
		}
		if spec == "Card.equipping" {
			if o.AttachedTo != 0 {
				return 1
			}
			return 0
		}
		if e.matchesSpecFrom(spec, source, you, source) {
			return 1
		}
		return 0
	}
	n := 0
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o != nil && o.Zone == zone && e.matchesSpecFrom(spec, id, you, source) {
			n++
		}
	})
	return n
}

func (e *Engine) revoltThisTurn(controller state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield {
			// Move preserves the object's pre-move controller outside the
			// battlefield, so this is the controller at the moment it left —
			// exactly Revolt's "a permanent you controlled" test (not owner).
			if o := e.G.Obj(ev.Obj); o != nil && o.Controller == controller {
				return true
			}
		}
	}
	return false
}

// RevoltHolds is the effects.Host bridge (the bare Condition$ Revolt gate in
// effects/conditions.go and the Count$Revolt.<yes>.<no> branch head in
// effects/count.go): the same revoltThisTurn scan the replacement path's
// Revolt$ clause and the trigger path's Revolt$ clause read, so all four
// spellings answer identically and a replay derives each from the log.
func (e *Engine) RevoltHolds(controller state.PlayerID) bool {
	return e.revoltThisTurn(controller)
}

// DeliriumHolds is the effects.Host bridge (the bare Condition$ Delirium
// gate in effects/conditions.go): the same graveyardCardTypeCount census the
// replacement path's Delirium$ clause, the Continuous static gate
// (rules/layers.go) and the ability-offer gate (rules/legal.go) read, so
// every Delirium spelling answers identically.
func (e *Engine) DeliriumHolds(controller state.PlayerID) bool {
	return e.graveyardCardTypeCount(controller) >= 4
}

// MetalcraftHolds is the effects.Host bridge for bare Condition$ Metalcraft:
// it shares the metalcraftHolds census used by cost, Continuous and offer gates.
func (e *Engine) MetalcraftHolds(controller state.PlayerID) bool {
	return e.metalcraftHolds(controller)
}

// thresholdHolds is the ONE census for the Threshold ability word's
// "seven or more cards in your graveyard" clause, shared by the trigger-side
// gate (triggerConditionHoldsWithSVars' Threshold$ clause) and the Continuous
// static gate (layers.go continuousConditionHolds' Condition$ Threshold arm),
// so the two spellings cannot drift apart. The count is every card in the
// controller's graveyard zone; an out-of-range controller has no graveyard.
func (e *Engine) thresholdHolds(controller state.PlayerID) bool {
	return len(e.G.Zone(state.ZGraveyard, controller)) >= 7
}

func (e *Engine) graveyardCardTypeCount(controller state.PlayerID) int {
	seen := map[string]bool{}
	for _, id := range e.G.Zone(state.ZGraveyard, controller) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			for _, typ := range o.Face().Types {
				switch typ {
				case "Artifact", "Battle", "Creature", "Enchantment", "Instant", "Kindred", "Land", "Planeswalker", "Sorcery":
					seen[typ] = true
				}
			}
		}
	}
	return len(seen)
}

func faceHasType(o *state.Object, typ string) bool {
	if o == nil || o.Face() == nil {
		return false
	}
	for _, got := range o.Face().Types {
		if got == typ {
			return true
		}
	}
	return false
}

func (e *Engine) replacementCauseMatches(spec string, replacementSource, cause state.ObjID) bool {
	o := e.G.Obj(cause)
	if o == nil {
		return false
	}
	kind, quals, _ := strings.Cut(strings.TrimSpace(spec), ".")
	switch kind {
	case "Spell":
		if o.Ability != nil {
			return false
		}
	case "SpellAbility":
		// Both spell cards and minted ability objects qualify.
	case "Triggered":
		// A triggered-ability wrapper (TriggerPush/DelayedPush). Classified
		// through state.StackKindOf -- the ONE classifier view's
		// StackView.Kind and rules' TargetType$ legality also use, so the
		// three can never disagree (a delayed trigger counts as triggered,
		// CR 603.7).
		if state.StackKindOf(e.G, o) != state.StackKindTriggered {
			return false
		}
	default:
		return false
	}
	if quals == "" {
		return true
	}
	if strings.HasPrefix(quals, "IsTargeting Self") {
		for _, t := range o.Targets {
			if !t.IsPlayer && t.Obj == replacementSource {
				return true
			}
		}
		return false
	}
	switch quals {
	case "YouCtrl":
		return o.Controller == e.controllerOf(replacementSource)
	case "OppCtrl", "YouDontCtrl":
		return o.Controller != e.controllerOf(replacementSource)
	case "Modular":
		// ValidCause$ Triggered.Modular names the modular keyword's own
		// put-counters trigger (Zabaz, the Glimmerwasp): the wrapper's source
		// card must carry K:Modular. HasKeyword reads the printed plus
		// layer-6-granted keyword list, so a granted Modular qualifies too.
		// An absent source, or any other keyword qualifier this build does
		// not model, fails closed (the standing convention).
		if o.Source == 0 {
			return false
		}
		return e.hasKeywordH(o.Source, kwhModular)
	}
	return false
}

// CounterAllowed implements effects.Host. A Counter event is the attempted
// removal of a stack object, not a CounterChange event, so it is checked at
// Counter's sole stack-removal path before the MoveZone is emitted.
func (e *Engine) CounterAllowed(target, cause state.ObjID) bool {
	matches := e.counterReplacementMatchesAll(target, cause)
	switch len(matches) {
	case 0:
		return true
	case 1:
		e.applyCounterReplacement(target, matches[0])
		return false
	default:
		t := e.G.Obj(target)
		if t == nil || int(t.Controller) >= len(e.G.Players) || e.G.Players[t.Controller].Lost {
			e.applyCounterReplacement(target, matches[0])
			return false
		}
		e.replChoices = append(e.replChoices, replChoice{
			kind: replChoiceCounter,
			ev:   events.Event{Obj: target}, cands: matches, before: e.triggerBefore,
			player: t.Controller, cause: cause,
		})
		if e.pending == nil {
			e.askReplacementChoice(t.Controller)
		}
		return false
	}
}

func (e *Engine) counterReplacementMatchesAll(target, cause state.ObjID) []replMatch {
	var matches []replMatch
	// Effect-created Counter replacements (Mistrise Village's AntiMagic:
	// "the next spell you cast this turn can't be countered", a delayed
	// Effect whose body is a bodyless Layer$ CantHappen R:): the continuous
	// registry is the only place these live, so the Counter path — whose
	// ordinary scan reads printed face Repls — matches them here through the
	// same remembered-scoped matcher the general replacement scan uses, plus
	// the shared ValidSA$ subset gate. Stopping the Counter event (the
	// With-less form) is the complete replacement.
	for _, ce := range e.active() {
		if ce.ReplacementEvent != "Counter" || ce.ReplacementBody != "" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Layer"]), "CantHappen") {
			continue
		}
		r := cards.Repl{Event: "Counter", Params: ce.ReplacementParams}
		if !e.replacementMatchesEffectCreated(r, ce.Source, events.Event{Obj: target}, ce.Remembered, ce.RememberedPlayers) {
			continue
		}
		t := e.G.Obj(target)
		src := e.G.Obj(ce.Source)
		if t == nil || src == nil {
			continue
		}
		if spec := r.Params["ValidSA"]; spec != "" && !e.counterValidSA(t, spec, e.controllerOf(ce.Source), ce.Source) {
			continue
		}
		matches = append(matches, replMatch{id: ce.Source, repl: &r,
			remembered: ce.Remembered, chosen: ce.ChosenNumber,
			key: "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))})
	}
	e.forEachObject(func(source state.ObjID) {
		o := e.G.Obj(source)
		if o == nil || o.Face() == nil {
			return
		}
		for i := range o.Face().Repls {
			r := &o.Face().Repls[i]
			if r.Event == "Counter" && e.counterReplacementMatches(*r, source, target, cause) {
				matches = append(matches, replMatch{id: source, repl: r})
			}
		}
	})
	return matches
}

func (e *Engine) applyCounterReplacement(target state.ObjID, m replMatch) {
	if m.repl.With != nil {
		e.runReplaceWith(e.replCtx(m, events.Event{Obj: target}), target, m.repl.With, nil)
	}
}

func (e *Engine) applyChosenCounterReplacement(rc replChoice, selected int) {
	m := rc.cands[selected]
	// CR 616.1e: applicability is checked against the event as it exists when
	// the answer is applied. No state can normally change while the choice is
	// pending, but recomputing keeps this path correct for released/departed
	// decisions and mirrors damage replacement ordering.
	if !e.counterReplacementMatches(*m.repl, m.id, rc.ev.Obj, rc.cause) {
		matches := e.counterReplacementMatchesAll(rc.ev.Obj, rc.cause)
		if len(matches) == 0 {
			return
		}
		m = matches[0]
	}
	e.applyCounterReplacement(rc.ev.Obj, m)
}

func (e *Engine) counterReplacementMatches(r cards.Repl, source, target, cause state.ObjID) bool {
	o := e.G.Obj(source)
	t := e.G.Obj(target)
	if o == nil || t == nil || t.Zone != state.ZStack {
		return false
	}
	if !e.commandReplZoneAdmits(r, source) {
		return false
	}
	if active := r.Params["ActiveZones"]; active != "" && !zoneSpecContains(active, o.Zone) {
		return false
	}
	if v := r.Params["ValidCard"]; v != "" &&
		!e.matchesSpecFrom(v, target, o.Controller, source) {
		return false
	}
	if v := r.Params["ValidCause"]; v != "" && !e.replacementCauseMatches(v, source, cause) {
		return false
	}
	if !e.replacementConditionHolds(r, source, o.Controller) {
		return false
	}
	return e.counterValidSA(t, r.Params["ValidSA"], o.Controller, source)
}

// counterValidSA is the Spell/Activated/Triggered subset used by R:Event$
// Counter. A qualifier scopes the stack object's controller relative to the
// replacement source; an unrecognised qualifier fails closed.
func (e *Engine) counterValidSA(target *state.Object, spec string, you state.PlayerID, source state.ObjID) bool {
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		kind, quals, _ := strings.Cut(strings.TrimSpace(alt), ".")
		isKind := (kind == "Spell" && target.Ability == nil) ||
			(kind == "SpellAbility") ||
			(kind == "Activated" && target.Ability != nil && !isTriggered(e.G, target)) ||
			(kind == "Triggered" && target.Ability != nil && isTriggered(e.G, target))
		if !isKind {
			continue
		}
		if quals == "" {
			return true
		}
		// Spell qualifiers are card characteristics plus controller-relative
		// predicates. Reuse the ordinary object-filter grammar rather than a
		// hand-maintained qualifier allowlist, so Creature/Instant/colour/P/T
		// and future recognised predicates cannot drift from targeting.
		if target.Ability == nil && e.counterSpellQualifiers(target, quals, you, source) {
			return true
		}
		// Ability objects have no card face; their corpus qualifiers are the
		// controller-relative forms, evaluated explicitly against the wrapper.
		if target.Ability != nil {
			switch quals {
			case "YouCtrl":
				if target.Controller == you {
					return true
				}
			case "OppCtrl", "YouDontCtrl":
				if target.Controller != you {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) counterSpellQualifiers(target *state.Object, quals string, you state.PlayerID, source state.ObjID) bool {
	var ordinary []string
	for q := range strings.SplitSeq(quals, "+") {
		switch q {
		case "hasKeywordFlash":
			if target.Face() == nil || !target.Face().HasKeyword("Flash") {
				return false
			}
		case "wasCastByYou":
			if target.Controller != you {
				return false
			}
		default:
			ordinary = append(ordinary, q)
		}
	}
	if len(ordinary) == 0 {
		return true
	}
	return e.matchesSpecFrom("Card."+strings.Join(ordinary, "+"), target.ID, you, source)
}

func isTriggered(g *state.Game, o *state.Object) bool {
	_, ok := state.TriggerOf(g, o)
	return ok
}

// replChoice is one CR 616.1 order-selection suspension: the MoveZone event
// more than one replacement would modify, parked (never emitted, never
// applied -- the moving object stays put) along with the competing
// replacements, until the affected controller picks the order. The answer
// applies the chosen replacement for real. Plain value data (an events.Event
// plus a []replMatch whose *cards.Repl pointers are shared immutable corpus
// data), so Clone copies the queue with one slice copy, the same class as
// cmdZone.
type replChoiceKind uint8

const (
	replChoiceMove replChoiceKind = iota
	replChoiceMana
	replChoiceManaColor
	replChoicePhaseOrder
	replChoicePhaseOptional
	// replChoiceUntap parks competing effects that would replace one Untap.
	// It is appended so existing in-memory enum values remain unchanged.
	replChoiceUntap
	// replChoiceDamage parks a CR 616.1 order competition for an object
	// Damage event (repl:DamageDone): two or more applicable damage
	// replacements (a Prevent$ True shield, a DB$ ReplaceDamage subtraction,
	// a DB$ ReplaceEffect amount rewrite) and the affected player orders
	// them. Recomputed every cycle (CR 616.1e) rather than driven by
	// applied/applicable like the mana/phase kinds, because a redirect or
	// amount rewrite can change which of the ORIGINAL candidates still apply.
	replChoiceDamage
	// replChoiceCounter parks CR 616.1's order competition when two or more
	// repl:Counter replacements would stop the same spell/ability.
	replChoiceCounter
	// replChoiceAddCounter parks CR 616.1's order competition when two or
	// more AddCounter (CounterChange/PlayerCounterChange) replacements whose
	// bodies do not all commute would rewrite the same placement (Hardened
	// Scales' Plus.1 and Branching Evolution's Twice: 1 -> 2 -> 4 one way,
	// 1 -> 2 -> 3 the other). It is appended so existing in-memory enum
	// values remain unchanged.
	replChoiceAddCounter
	// replChoiceToken parks CR 616.1's order competition when two or more
	// CreateToken (repl:CreateToken) replacements whose bodies do not all
	// commute would rewrite the same creation (a multiplier and a script
	// rewriter: the rewriter composed after the multiplier rewrites every
	// duplicated mint, composed before only the original).
	replChoiceToken
	// replChoiceUpdated parks CR 616.1's order competition for an all-Updated
	// MoveZone composition whose bodies do not commute (a tap and an untap
	// fighting over the same tapped bit; the last body applied wins).
	replChoiceUpdated
	replChoiceScry
	// replChoiceEntryOrder parks the CR 616.1 order competition over an
	// ENTRY-CHARACTERISTIC counter grant (rules/entry_counters.go): the
	// staged move has not folded, and the answer resumes the competition on
	// an isolated post-entry preview before the staged entry re-emits. It is
	// appended so existing in-memory enum values remain unchanged.
	replChoiceEntryOrder
	replChoiceFaceUp
)

type lifeExchangeTransaction struct {
	source         state.ObjID
	controller     state.PlayerID
	oldLife        int32
	player         state.PlayerID
	lifeBefore     int32
	setPower       bool
	setToughness   bool
	second         events.Event
	stage          uint8
	rememberLoss   bool
	rememberCtx    *effects.Ctx
	controllerLife int32
	staged         []events.Event
}

type replChoice struct {
	kind         replChoiceKind
	ev           events.Event
	cands        []replMatch
	applied      []bool      // mana/phase: candidates that already had their opportunity
	applicable   []int       // order decision option -> candidate index
	selected     int         // mana colour / phase optional: candidate awaiting its answer
	changed      bool        // mana: at least one rewrite already happened
	manaTapped   bool        // mana: tap provenance survives the decision boundary
	manaProducer state.ObjID // mana: producer survives the decision boundary
	boundary     bool        // phase: this choice owns setStep's boundary cleanup
	leaving      state.Step
	before       *triggerSnapshot // immutable SBA look-back, safe to share in Clone
	untap        *untapStep       // remaining turn-based untaps after an order answer
	// life marks a life gain/loss competition (applyLifeReplacements): ev is
	// then the event as already modified by appliedRepls, the affected player
	// is ev.Player, and the answer continues the CR 616.1 loop rather than
	// finishing after one application. damaging, combatDamaging and
	// dmgSrcOverride are the synchronous damage context the parked event was
	// proposed under, restored while the answer emits it so provenance-reading
	// triggers and protection see the same source.
	life           bool
	exchange       *lifeExchangeTransaction
	appliedRepls   []replMatch
	damaging       state.ObjID
	combatDamaging bool
	dmgSrcOverride state.ObjID
	// used is kind == replChoiceDamage's own applied-set: the matches already
	// settled this CR 616.1e recomputation cycle for an object Damage event
	// (repl:DamageDone), tracked by value rather than applied's per-candidate
	// bool index because remainingDamageReplacements recomputes the candidate
	// set itself after every rewrite instead of indexing a fixed cands slice.
	// player is the resolved asking player (replacementAskPlayer's result,
	// which may differ from the CR 616.1e affected player recomputed fresh
	// into damageAffectedPlayer at every cycle) for kind == replChoiceDamage
	// or replChoiceCounter.
	used   []replMatch
	player state.PlayerID
	// combat marks a damage competition parked from the combat-damage step's
	// own assignment loop (rules/combat.go): the chosen replacement's
	// lifelink/deathtouch riders and commander-damage tally pay the way
	// ordinary combat damage does, and finishChosenDamage resumes
	// runCombatAssignments once this batch's parked choices all settle.
	// lifelink/deadly cache the damage source's keywords at park time (Task
	// 15's provenance-freezing discipline), toxic caches the source's total
	// CR 702.164 toxic N the same way (runCombatAssignments' synchronous emit
	// site reads it at the moment of the hit; a parked hit must poison the
	// same amount when it lands in finishChosenDamage), and cause is the
	// Counter replacement's cause object (counterReplacementMatches' third
	// argument), set only when kind == replChoiceCounter.
	combat   bool
	lifelink bool
	deadly   bool
	toxic    int
	cause    state.ObjID
	// counterAdderPlusOne is kind == replChoiceAddCounter's adder provenance
	// captured when the competition was POSED (PLUS ONE; 0 = unknown). The
	// answer can arrive after the proposing window has closed -- a body's
	// CounterChange poses from inside the replacement body, and the
	// applyingReplacement/replacingSource that named the body is gone by resume
	// time -- so the final emit in emitAddCounterReplacement republishes this
	// captured value instead of re-deriving it (rules/replacement.go).
	counterAdderPlusOne state.PlayerID
	// tokenPlan/tokenNext are kind == replChoiceToken's parked plan state: the
	// mints the already-applied matches produced and the cursor this choice
	// was posed at (candidates are cands[cursor:]). Plain value data, the
	// same Clone class as the rest of the queue.
	tokenPlan []tokenPlanMint
	tokenNext int
	// emitted marks kind == replChoiceUpdated's original event as already
	// emitted (the composition's preamble ran); a re-parked continuation
	// skips the emit and resolves only the remaining bodies.
	emitted bool
	// absorbed is kind == replChoiceUpdated's set of Updated PutCounter|
	// ETB$ True bodies (replIdentity) whose placement the entry move's Pairs
	// payload already carries (rules/entry_counters.go). It is captured from
	// foldEntryMove on the preamble pass and consulted again on every
	// re-parked continuation, so a later-answered absorbed body is never run
	// a second time.
	absorbed []string
	// stage is kind == replChoiceEntryOrder's parked entry (rules/
	// entry_counters.go): the move that has not folded and the competition
	// state its resume continues. Pointer data, the same Clone class as
	// before (*triggerSnapshot) -- the stage is reached only through its
	// own answer.
	stage *entryCounterStage
	// mintSink names the parked-mint collector (Engine.mintSinks) this
	// competition's answer mints into, when it parked a DB$ Token's mint
	// that a "token_rest" continuation is waiting on (rules/token_rest.go).
	// 0 for every other competition.
	mintSink uint64
	// inResolution marks a competition posed while a stack resolution was in
	// flight (e.resolvingObj != 0): the pose's Engine.Ask then parked that
	// resolution on e.resume with the interrupted object still on the stack,
	// and the LAST answer round of the queue must resume it through its
	// recorded chain -- the same discipline the damage branch applies -- or
	// resolveTop re-resolves the interrupted object from the top on the next
	// priority pass, unbounded. A pose from turn structure or a cast window
	// records no suspension (Engine.Ask's frame there is the flow's own
	// bookkeeping, consumed by its own handler) and must not be resumed.
	inResolution bool
	// resumeAtPose is the Engine.Ask record the pose itself created, captured
	// when askReplacementChoice posed an ask with e.resume nil. For an
	// inResolution competition it IS the suspended resolution (and the answer
	// resumes it); otherwise it is the pose's own bookkeeping from a cast
	// window or turn structure -- nothing was suspended -- and the tail drops
	// it once the competition's work completed synchronously, because
	// resolveTop reads e.resume to decide whether its resolution suspended
	// and a stale frame makes it abandon a resolution that actually finished,
	// re-resolving it on every pass.
	resumeAtPose *resumePoint
}

// replacementChoicePlayer is the affected player a parked competition asks:
// a life event's player (the player whose life total changes), else mana/
// phase candidates by role, else the moving object's controller.
func (e *Engine) replacementChoicePlayer(rc replChoice) (state.PlayerID, bool) {
	if rc.life {
		return rc.ev.Player, int(rc.ev.Player) < len(e.G.Players)
	}
	switch rc.kind {
	case replChoiceMana, replChoiceManaColor, replChoiceScry:
		return rc.ev.Player, int(rc.ev.Player) < len(e.G.Players)
	case replChoicePhaseOrder, replChoicePhaseOptional:
		return e.G.Active, int(e.G.Active) < len(e.G.Players)
	case replChoiceDamage, replChoiceCounter:
		// Already resolved by poseDamageReplacementChoice/the recomputation
		// cycle (replacementAskPlayer over the freshly recomputed CR 616.1e
		// affected player), never re-derived from rc.ev.Obj's controller here
		// -- a redirect or an OptionalDecider$ can make the asking player
		// differ from that.
		return rc.player, int(rc.player) < len(e.G.Players)
	case replChoiceAddCounter, replChoiceEntryOrder:
		// Resolved at pose time (the counter's recipient or the affected
		// object's controller; the entry order's posed player; the token's
		// creator) and recomputed at every
		// re-pose (continueAddCounterReplacements / the drive).
		return rc.player, int(rc.player) < len(e.G.Players)
	default:
		// A replaced DRAW event has no object whose controller could be
		// consulted -- the affected player is the draw-er itself (the same
		// binding the replacement's ReplacedPlayer context carries).
		if rc.ev.Kind == events.Draw {
			return rc.ev.Player, int(rc.ev.Player) < len(e.G.Players)
		}
		o := e.G.Obj(rc.ev.Obj)
		if o == nil || int(o.Controller) >= len(e.G.Players) {
			return 0, false
		}
		return o.Controller, true
	}
}

// poseUntapReplacementChoice starts the CR 616.1 choice between effects
// replacing one Untap. The affected player is the untapped object's
// controller, not either replacement source's controller.
func (e *Engine) poseUntapReplacementChoice(ev events.Event, matches []replMatch) {
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Controller) >= len(e.G.Players) {
		return
	}
	rc := replChoice{kind: replChoiceUntap, ev: ev, cands: matches, before: e.triggerBefore}
	if e.untapResume != nil {
		resume := *e.untapResume
		rc.untap = &resume
	}
	e.replChoices = append(e.replChoices, rc)
	if e.pending == nil {
		e.askReplacementChoice(o.Controller)
	}
}

// poseReplacementChoice starts a CR 616.1 order-selection suspension: the
// competing event is parked and the affected controller -- the controller of
// the moving object, CR 616.1's "affected player" -- is asked which
// replacement applies first. Mirrors commander-zone parking: the ask is posed
// only when no other decision is already pending (the caller has already
// ruled out a departed controller, which makes no choices under CR 800.4a).
func (e *Engine) poseReplacementChoice(ev events.Event, matches []replMatch) {
	p := ev.Player
	if ev.Kind != events.Draw {
		o := e.G.Obj(ev.Obj)
		if o == nil {
			return
		}
		p = o.Controller
	}
	if int(p) >= len(e.G.Players) {
		return
	}
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceMove,
		ev: ev, cands: matches, before: e.triggerBefore, inResolution: e.resolvingObj != 0 || e.answerInResolution})
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

// poseManaReplacementChoice parks a partially rewritten mana event until the
// player receiving it chooses the next applicable effect (CR 616.1). The full
// candidate set and applied bitmap survive the choice so applicability can be
// re-evaluated after the selected rewrite, including effects newly enabled by
// a changed ManaAmount$.
func (e *Engine) poseManaReplacementChoice(ev events.Event, candidates []replMatch,
	applied []bool, applicable []int, changed, tapped bool, producer state.ObjID) {
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceMana, ev: ev,
		cands: candidates, applied: append([]bool(nil), applied...),
		applicable: append([]int(nil), applicable...), changed: changed,
		manaTapped: tapped, manaProducer: producer, before: e.triggerBefore})
	if e.pending == nil {
		e.askReplacementChoice(ev.Player)
	}
}

// poseManaColorReplacementChoice parks a partially rewritten mana event while
// the receiving player chooses W/U/B/R/G for one choice-valued ReplaceMana
// body. Candidate state and the applied bitmap are retained so the answer can
// resume the same CR 616.1 applicability loop.
func (e *Engine) poseManaColorReplacementChoice(ev events.Event, candidates []replMatch,
	applied []bool, selected int, changed, tapped bool, producer state.ObjID) {
	e.replChoices = append(e.replChoices, replChoice{kind: replChoiceManaColor, ev: ev,
		cands: candidates, applied: append([]bool(nil), applied...), selected: selected,
		changed: changed, manaTapped: tapped, manaProducer: producer, before: e.triggerBefore})
	if e.pending == nil {
		e.askReplacementChoice(ev.Player)
	}
}

func (e *Engine) phaseChoice(ev events.Event, candidates []replMatch, used []bool) replChoice {
	rc := replChoice{ev: ev, cands: candidates, applied: append([]bool(nil), used...),
		before: e.triggerBefore}
	if e.stepLeaving != nil {
		rc.boundary = true
		rc.leaving = *e.stepLeaving
	}
	return rc
}

// posePhaseOrderChoice parks a step entry with all currently applicable
// BeginPhase replacements so the active player chooses which gets the first
// opportunity under CR 616.1.
func (e *Engine) posePhaseOrderChoice(ev events.Event, candidates []replMatch,
	used []bool, applicable []int) {
	rc := e.phaseChoice(ev, candidates, used)
	rc.kind = replChoicePhaseOrder
	rc.applicable = append([]int(nil), applicable...)
	e.replChoices = append(e.replChoices, rc)
	if e.pending == nil {
		e.askReplacementChoice(e.G.Active)
	}
}

// posePhaseOptionalChoice parks the selected Optional$ BeginPhase replacement
// for its apply/decline answer. Declining resumes the remaining candidate set.
func (e *Engine) posePhaseOptionalChoice(ev events.Event, candidates []replMatch,
	used []bool, selected int) {
	rc := e.phaseChoice(ev, candidates, used)
	rc.kind = replChoicePhaseOptional
	rc.selected = selected
	e.replChoices = append(e.replChoices, rc)
	if e.pending == nil {
		e.askReplacementChoice(e.G.Active)
	}
}

// askReplacementChoice poses the CR 616.1 order choice for the FRONT parked
// competition to the affected controller: Min == Max == 1 over one option
// per competing replacement, in the deterministic scan order the engine found
// them in (the order the player reorders, never a coincidence of map
// iteration). Only the front of the queue is ever asked -- see
// handleReplacement's resumption for how the queue hands from one choice to
// the next.
func (e *Engine) poseDamageReplacementChoice(ev events.Event, matches []replMatch, p state.PlayerID) {
	source := e.protectionSource(e.damaging)
	e.replChoices = append(e.replChoices, replChoice{
		kind: replChoiceDamage, ev: ev, cands: matches, before: e.triggerBefore, player: p,
		damaging: source, combat: e.combatDamaging,
		lifelink: e.hasKeywordH(source, kwhLifelink), deadly: e.hasKeywordH(source, kwhDeathtouch),
		toxic: e.ToxicValue(source),
	})
	if e.pending == nil {
		e.askReplacementChoice(p)
	}
}

func (e *Engine) scryReplacementDecision(rc replChoice, sa *cards.SA, target int) *decision.Decision {
	d := &decision.Decision{Player: rc.ev.Player, Kind: decision.KReplacement, Min: 1, Max: 1,
		Source: rc.ev.Obj, ResumeKind: "scry_replacement", ResumeSA: sa, ResumeTarget: target,
		Prompt: "Several replacement effects would modify this scry: choose which applies next."}
	for _, i := range rc.applicable {
		m := rc.cands[i]
		label := "Apply a replacement"
		if o := e.G.Obj(m.id); o != nil && o.Face() != nil {
			label = "Apply " + o.Face().Name + "'s replacement"
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replacement", Obj: m.id, Label: label})
	}
	return d
}

func (e *Engine) askReplacementChoice(p state.PlayerID) {
	rc := e.replChoices[0]
	if rc.kind == replChoiceScry {
		// The first pose's resume point retains the SA and target. Subsequent
		// choices keep that frame parked and need no new suspension record.
		e.ask(e.scryReplacementDecision(rc, e.resume.sa, e.resume.target))
		return
	}
	d := &decision.Decision{Player: p, Kind: decision.KReplacement, Min: 1, Max: 1,
		Source: rc.ev.Obj, ResumeKind: "replacement"}
	indices := make([]int, len(rc.cands))
	for i := range indices {
		indices[i] = i
	}
	switch rc.kind {
	case replChoiceDamage:
		d.Prompt = "Several replacement effects would modify damage: choose which applies next."
	case replChoiceCounter:
		d.Prompt = "Several replacement effects would modify this counter event: choose which applies."
	case replChoiceAddCounter:
		d.Prompt = "Several replacement effects would modify how many counters are put: choose which applies first."
	case replChoiceEntryOrder:
		d.Prompt = "Several replacement effects would modify how many counters this permanent enters with: choose which applies first."
	case replChoiceToken:
		d.Prompt = "Several replacement effects would modify this token creation: choose which applies first."
	case replChoiceScry:
		d.Prompt = "Several replacement effects would modify this scry: choose which applies next."
		indices = rc.applicable
		indices = rc.applicable
	case replChoiceMana:
		d.Prompt = "Several replacement effects would change mana production: choose which applies next."
		indices = rc.applicable
	case replChoiceManaColor:
		d.Prompt = "Choose the colour of the replacement mana."
		for i, color := range []string{"W", "U", "B", "R", "G"} {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mana", Obj: rc.cands[rc.selected].id,
				Label: "Add " + color, ManaSymbol: color})
		}
		e.ask(d)
		return
	case replChoicePhaseOrder:
		d.Prompt = "Several replacement effects would change this step: choose which applies first."
		indices = rc.applicable
	case replChoicePhaseOptional:
		m := rc.cands[rc.selected]
		name := "this replacement effect"
		if so := e.G.Obj(m.id); so != nil && so.Face() != nil && so.Face().Name != "" {
			name = so.Face().Name
		}
		d.Prompt = "Apply " + name + "'s optional replacement and skip this step?"
		d.Options = []decision.Option{
			{Index: 0, Kind: "apply", Obj: m.id, Label: "Yes — skip this step"},
			{Index: 1, Kind: "decline", Obj: m.id, Label: "No — do not apply this replacement"},
		}
		e.ask(d)
		return
	case replChoiceFaceUp:
		m := rc.cands[rc.selected]
		name := "this replacement effect"
		if o := e.G.Obj(m.id); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		d.Prompt = "Apply " + name + "'s optional turn-face-up replacement?"
		d.Options = []decision.Option{{Index: 0, Kind: "apply", Obj: m.id, Label: "Yes — apply this replacement"},
			{Index: 1, Kind: "decline", Obj: m.id, Label: "No — do not apply this replacement"}}
		e.ask(d)
		return
	case replChoiceUntap:
		name := "this object"
		if o := e.G.Obj(rc.ev.Obj); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		d.Prompt = "Several replacement effects would change how " + name + " untaps: choose which applies first."
	default:
		name := "this object"
		if o := e.G.Obj(rc.ev.Obj); o != nil && o.Face() != nil && o.Face().Name != "" {
			name = o.Face().Name
		}
		prompt := "Several replacement effects would change how " + name + " moves: choose which applies first."
		if rc.life {
			what := "life gain"
			if _, _, loss := lifeLoss(rc.ev); loss {
				what = "life loss"
			}
			prompt = "Several replacement effects would change this " + what + ": choose the one that applies first."
		}
		d.Prompt = prompt
	}
	for _, candidate := range indices {
		c := rc.cands[candidate]
		label := "Apply a replacement"
		if so := e.G.Obj(c.id); so != nil && so.Face() != nil && so.Face().Name != "" {
			label = "Apply " + so.Face().Name + "'s replacement"
		} else if dsc := c.repl.Params["Description"]; dsc != "" {
			label = "Apply: " + dsc
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replacement", Obj: c.id, Label: label})
	}
	if rc.ev.Kind == events.Damage && hasOptionalReplacement(rc.cands) {
		d.Options = append(d.Options, decision.Option{Index: len(rc.cands), Kind: "skip_replacement",
			Label: "Do not apply an optional replacement"})
		// A single-optional competition asked of its OptionalDecider$ is the
		// replacement's own "may", not an order among several: pose it as
		// one (Battletide Alchemist round-2 finding).
		if len(rc.cands) == 1 && strings.EqualFold(rc.cands[0].repl.Params["Optional"], "True") {
			label := "Apply the replacement"
			if o := e.G.Obj(rc.cands[0].id); o != nil && o.Face() != nil && o.Face().Name != "" {
				label = "Apply " + o.Face().Name + "'s replacement"
			}
			d.Prompt = label + " to this damage?"
		}
	}
	// A replacement-order choice can arise in the middle of an effect's Emit.
	// Enter through Host.Ask so effects.Resolve sees Suspended and records the
	// remaining SA chain — but only when something is actually resolving (a
	// stack resolution in flight, or a replacement body whose own chain the
	// ask would interrupt): Host.Ask's record is that suspension's
	// continuation, remembered on the parked choice so the tail can tell it
	// from the pose's own bookkeeping. A pose from a cast window or turn
	// structure has nothing to suspend — Engine.Ask's record there would
	// stale-resume (resolveTop reads e.resume to decide whether its
	// resolution suspended and abandons a resolution that finished) — so the
	// pose takes the plain ask and creates no record at all. A recomputation
	// ask already owns a resume point; pose it directly without overwriting
	// the original continuation.
	if e.resume == nil && (e.resolvingObj != 0 || e.applyingReplacement) {
		e.Ask(d)
		e.replChoices[0].resumeAtPose = e.resume
	} else {
		e.ask(d)
	}
}

// handleReplacement applies an answered CR 616.1 order choice: the front
// parked competition's chosen replacement is applied for real -- the SAME
// applyReplacement a lone matching replacement would run -- and, if more
// competitions are parked, the next one's controller is asked. Because the
// chosen replacement is the one the player decided applies FIRST, and a
// "Replaced" replacement discards the modified event (so the remaining
// replacements then see a destination the original no longer matches), one
// application completes the relocation for the competing shape. The choice
// is in the log as the Intents entry plus the DecisionAsk/DecisionMade
// events every decision emits; the relocation is the MoveZone (or absence of
// it) the applied replacement emits, so a log-only replay reproduces both
// branches. An answer with no parked competition is only reachable from a
// hand-built decision and degrades to a Note, the same totality stance as
// handleCmdZone.
func (e *Engine) handleReplacement(d *decision.Decision, in decision.Intent) {
	if len(d.Options) > 0 && strings.HasPrefix(d.Options[0].Kind, "madness_") {
		e.handleMadnessReplacement(d, in)
		return
	}
	if len(e.replChoices) == 0 {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "replacement decision answered with no event parked"})
		return
	}
	rc := e.replChoices[0]
	e.replChoices = e.replChoices[1:]
	rp := e.resume
	savedAnswerInRes := e.answerInResolution
	e.answerInResolution = savedAnswerInRes || rc.inResolution
	defer func() { e.answerInResolution = savedAnswerInRes }()
	if rp == nil && rc.inResolution && rc.resumeAtPose != nil {
		// The competition was posed while a stack resolution was suspended,
		// but the suspension's frame is no longer on e.resume: an earlier
		// answer in the same queue ran a replacement body that ASKED (a shock
		// land's UnlessCost PayLife under a mass return), the nested
		// Engine.Ask replaced e.resume with its own frame, and that nested
		// answer has since completed. The earlier answer handed the
		// suspended frame to this queued competition (settleReplacementQueue);
		// reinstate it so this answer's tail resumes the resolution exactly
		// once, when the queue drains.
		rp = rc.resumeAtPose
		e.resume = rp
	}
	chosen := d.Chosen(in)
	if rc.kind == replChoiceFaceUp {
		if len(chosen) > 0 && chosen[0].Index == 0 {
			m := rc.cands[rc.selected]
			e.turnUpMove = &rc.ev
			e.runReplaceWith(e.replCtx(m, rc.ev), rc.ev.Obj, m.repl.With, nil)
			e.turnUpMove = nil
		}
		if e.pending != nil {
			// The body asked (the copy election): resolveReplacementBody
			// already parked the marker's re-emit on the body's chain, so
			// the transition folds only after that answer.
			return
		}
		prior := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(rc.ev)
		e.applyingReplacement = prior
		return
	}
	damageKind := rc.kind == replChoiceDamage || rc.kind == replChoiceCounter
	if len(chosen) == 0 || (damageKind && (chosen[0].Index < 0 || chosen[0].Index > len(rc.cands) ||
		(chosen[0].Index == len(rc.cands) && !(rc.kind == replChoiceDamage && hasOptionalReplacement(rc.cands))))) {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "replacement answer had no choice"})
		return
	}
	before := e.triggerBefore
	e.triggerBefore = rc.before
	if damageKind {
		completed := true
		switch rc.kind {
		case replChoiceCounter:
			e.applyChosenCounterReplacement(rc, chosen[0].Index)
		case replChoiceDamage:
			completed = e.handleDamageReplacementChoice(rc, chosen[0].Index)
		}
		e.triggerBefore = before
		if completed && len(e.replChoices) > 0 {
			// The same parked resolution produced more than one replacement
			// choice: a multi-recipient DealDamage/DamageAll parks one per
			// recipient event before its enclosing chain suspends. The original
			// resume point must stay parked until the LAST of them is answered --
			// resuming after the first would run the remaining SA chain (and move
			// the spell off the stack) while a later recipient's damage is still
			// awaiting its CR 616.1 order choice, and the chained riders would
			// fire before the effect's own damage settled. If the application
			// itself posed a nested ask (e.resume no longer rp), chain this
			// frame's continuation behind the new one so nothing is dropped.
			if rp != nil && e.resume != rp {
				e.resume.outer = &resumePoint{kind: rp.kind, obj: rp.obj, outer: rp.outer}
			}
			for len(e.replChoices) > 0 {
				next := e.replChoices[0]
				// CR 616.1e: the affected player is recomputed from the parked
				// event at ask time, so a competition whose recipient changed
				// while parked asks the NEW affected player -- except that a
				// single-optional competition's "may" still belongs to its
				// OptionalDecider$ (replacementAskPlayer).
				if p, ok := e.damageAffectedPlayer(next.ev); ok && !e.G.Players[p].Lost {
					e.replChoices[0].player = e.replacementAskPlayer(next.cands, p)
					if e.pending == nil {
						e.askReplacementChoice(e.replChoices[0].player)
					}
					return
				}
				// CR 800.4a: an affected player who has left the game or lost
				// makes no choices. Its candidates apply in deterministic scan
				// order and the queue drains on.
				e.replChoices = e.replChoices[1:]
				switch {
				case next.kind == replChoiceCounter:
					e.applyChosenCounterReplacement(next, 0)
				case next.kind == replChoiceDamage:
					completed = e.handleDamageReplacementChoice(next, 0)
				default:
					e.applyReplacement(next.ev, next.cands[0])
				}
				if !completed {
					return
				}
			}
		}
		if completed && rc.combat && e.combatRound.assignments != nil {
			// A combat damage pass was parked at this assignment. Finish the
			// remaining precomputed assignments before running SBAs or the
			// regular pass; a newly parked replacement simply returns again.
			e.damageStep(false)
			if e.pending == nil && e.combatRound.assignments == nil && e.combatRound.active {
				e.completeCombatPass(e.combatRound.pass)
			}
		}
		if completed && rp != nil && e.resume == rp {
			// The parked event and its riders are complete. Resume only the
			// chain after the effect that proposed it; the effect itself must
			// not emit the same damage/counter event a second time.
			e.resume = nil
			e.resumeResolution(rp, nil)
		}
		return
	}
	if rc.kind == replChoiceScry {
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player, Text: "scry replacement answer out of range"})
			return
		}
		i := rc.applicable[chosen[0].Index]
		// Drive the selected candidate alone first, then recompute the
		// remaining matches against the rewritten instruction. The selected
		// effect is marked used before the recheck (CR 614.5).
		selected := rc.cands[i]
		rc.applied[i] = true
		// The parked Scry frame is not itself an outstanding draw ask. Clear
		// it while a Draw-instead body runs so DrawFor can draw every card (or
		// pose its own Dredge ask), then restore/chain it afterwards.
		e.resume = nil
		next, _ := e.continueScryReplacements(rc.ev, []replMatch{selected}, []bool{false}, nil, 0)
		if e.resume != nil {
			if rp != nil {
				rp.scryProceed = false
				e.resume.outer = rp
			}
			e.triggerBefore = before
			return
		}
		if next.Kind == events.Scry {
			next, _ = e.continueScryReplacements(next, rc.cands, rc.applied, d.ResumeSA, d.ResumeTarget)
		}
		// A remaining Draw-instead body may suspend on Dredge too (e.g.
		// Kenessos selected first, then Eligeth). Keep that draw's fresh
		// resume point and chain the original Scry continuation AFTER it;
		// overwriting it with rp would answer Dredge as a Scry order choice.
		// A re-posed Scry order ask instead uses the original frame below.
		if e.resume != nil && (len(e.replChoices) == 0 || e.replChoices[0].kind != replChoiceScry) {
			if rp != nil {
				rp.scryProceed = false
				e.resume.outer = rp
			}
			e.triggerBefore = before
			return
		}
		// A re-pose used the same SA/target; retain the ORIGINAL frame rather
		// than the bookkeeping frame Ask may have created for its next ask.
		e.resume = rp
		e.triggerBefore = before
		if e.pending == nil && (len(e.replChoices) == 0 || e.replChoices[0].kind != replChoiceScry) {
			if rp != nil {
				rp.scryCount, rp.scryProceed = next.Amount, next.Kind == events.Scry
				e.resume = nil
				e.resumeResolution(rp, nil)
			}
		}
		e.askNextReplacementChoice()
		return
	}
	if rc.life {
		// Apply the chosen replacement, then re-evaluate what still applies
		// to the modified event (CR 616.1e); a further non-commuting
		// competition asks again at the front of the queue.
		if chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "life replacement answer out of range"})
			return
		}
		damaging, combat, override := e.damaging, e.combatDamaging, e.dmgSrcOverride
		e.damaging, e.combatDamaging, e.dmgSrcOverride = rc.damaging, rc.combatDamaging, rc.dmgSrcOverride
		priorExchange := e.lifeExchange
		e.lifeExchange = rc.exchange
		m := rc.cands[chosen[0].Index]
		if next, consumed := e.applyLifeReplacement(rc.ev, m); !consumed {
			applied := append(append([]replMatch(nil), rc.appliedRepls...), m)
			e.continueLifeReplacements(next, applied)
		}
		if rc.exchange != nil && e.pending == nil && len(e.replChoices) == 0 {
			e.finishLifeExchange(rc.exchange)
		}
		e.lifeExchange = priorExchange
		e.damaging, e.combatDamaging, e.dmgSrcOverride = damaging, combat, override
		e.triggerBefore = before
		e.settleReplacementQueue(rc, rp)
		e.askNextReplacementChoice()
		return
	}
	manaDecision := rc.kind == replChoiceMana || rc.kind == replChoiceManaColor
	switch rc.kind {
	case replChoiceMana:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "mana replacement answer out of range"})
			return
		}
		i := rc.applicable[chosen[0].Index]
		if manaReplacementNeedsColor(rc.cands[i]) {
			rc.kind = replChoiceManaColor
			rc.selected = i
			rc.applicable = nil
			e.replChoices = append([]replChoice{rc}, e.replChoices...)
			break
		}
		rc.ev = e.applyOneManaReplacementWithProducer(rc.ev, rc.cands[i], "", rc.manaProducer)
		rc.applied[i] = true
		e.continueManaReplacements(rc.ev, rc.cands, rc.applied, true, rc.manaTapped, rc.manaProducer)
	case replChoiceManaColor:
		// The chosen colour is structured data (Option.ManaSymbol); the
		// label is presentation-only.
		color := chosen[0].ManaSymbol
		if len(color) != 1 || !strings.Contains("WUBRG", color) ||
			rc.selected < 0 || rc.selected >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "mana colour replacement answer out of range"})
			return
		}
		rc.ev = e.applyOneManaReplacementWithProducer(rc.ev, rc.cands[rc.selected], color, rc.manaProducer)
		rc.applied[rc.selected] = true
		e.continueManaReplacements(rc.ev, rc.cands, rc.applied, true, rc.manaTapped, rc.manaProducer)
	case replChoicePhaseOrder:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "phase replacement answer out of range"})
			return
		}
		i := rc.applicable[chosen[0].Index]
		if rc.cands[i].repl.Params["Optional"] == "True" {
			rc.kind = replChoicePhaseOptional
			rc.selected = i
			rc.applicable = nil
			e.replChoices = append([]replChoice{rc}, e.replChoices...)
		} else {
			e.finishParkedPhase(rc, i)
		}
	case replChoicePhaseOptional:
		if rc.selected < 0 || rc.selected >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "optional phase replacement answer out of range"})
			return
		}
		if chosen[0].Kind == "apply" {
			e.finishParkedPhase(rc, rc.selected)
		} else {
			rc.applied[rc.selected] = true
			e.resumeParkedPhase(rc)
		}
	case replChoiceAddCounter:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "counter replacement-order answer out of range"})
			return
		}
		damaging, combat, override := e.damaging, e.combatDamaging, e.dmgSrcOverride
		e.damaging, e.combatDamaging, e.dmgSrcOverride = rc.damaging, rc.combatDamaging, rc.dmgSrcOverride
		m := rc.cands[chosen[0].Index]
		if n, ok := e.applyAddCounterBody(rc.ev, m, rc.ev.Amount); ok {
			rc.ev.Amount = n
		}
		rc.appliedRepls = append(rc.appliedRepls, m)
		e.continueAddCounterReplacements(rc)
		e.damaging, e.combatDamaging, e.dmgSrcOverride = damaging, combat, override
	case replChoiceEntryOrder:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) || rc.stage == nil {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "entry counter replacement-order answer out of range"})
			return
		}
		e.withMintSink(rc.mintSink, func() { e.resumeEntryCounterOrder(rc, chosen[0].Index) })
	case replChoiceToken:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.applicable) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "token replacement-order answer out of range"})
			return
		}
		m := rc.cands[rc.applicable[chosen[0].Index]]
		rest := dropReplMatch(rc.cands, m)
		e.withMintSink(rc.mintSink, func() {
			var plan []tokenPlanMint
			var parked bool
			if body := m.repl.With; body != nil &&
				(strings.EqualFold(strings.TrimSpace(body.Params["TokenScript"]), "Chosen") ||
					strings.TrimSpace(body.Params["ValidChoices"]) != "") {
				// A chosen-copy match: the election the scan-order drive poses for
				// it (driveTokenReplacements' chosenShape arm), with the remaining
				// matches and the plan as they stand. idx -1 makes the pose's resume
				// cursor re-drive rest from 0 (m itself is already gone from rest).
				plan, parked = e.poseChosenTokenReplacement(rc.ev, rest, rc.tokenPlan, -1, m)
			} else {
				plan = e.applyTokenReplacementToPlan(rc.ev, rc.tokenPlan, m)
			}
			if !parked {
				plan, parked = e.driveTokenReplacements(rc.ev, rest, plan, 0)
			}
			if !parked {
				e.emitTokenPlan(rc.ev, plan)
			}
		})
	case replChoiceUpdated:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "entry replacement-order answer out of range"})
			return
		}
		e.resumeUpdatedComposition(rc, chosen[0].Index)
	case replChoiceUntap:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "untap replacement-order answer out of range"})
			return
		}
		e.applySimpleReplacement(rc.ev, rc.cands[chosen[0].Index])
		if rc.untap != nil && e.pending == nil {
			e.finishUntapStep(rc.untap.next)
		}
	default:
		if chosen[0].Index < 0 || chosen[0].Index >= len(rc.cands) {
			e.triggerBefore = before
			e.emit(events.Event{Kind: events.Note, Player: in.Player,
				Text: "replacement-order answer out of range"})
			return
		}
		e.applyReplacement(rc.ev, rc.cands[chosen[0].Index])
	}
	e.triggerBefore = before
	// A mana replacement or colour decision can interrupt CR 601.2g's mana
	// window. Resume the parked cast only after the final rewrite is logged
	// and no next replacement decision is pending.
	if manaDecision && e.pending == nil && len(e.replChoices) == 0 && e.cast != nil {
		e.continueCast()
	}
	e.settleReplacementQueue(rc, rp)
	e.askNextReplacementChoice()
}

// settleReplacementQueue is the shared tail of an answered replacement-order
// competition (every kind but the damage/counter and scry branches, which own
// their own resume discipline).
//
// A competition posed while a stack resolution was in flight parked that
// resolution: the pose's Engine.Ask recorded the interrupted resolution on
// e.resume (rp here) and the interrupted object stayed on the stack. Once the
// whole queue is answered and nothing is pending, the resolution must resume
// through its recorded chain, or resolveTop re-resolves the interrupted
// object from the top on the next priority pass, unbounded (observed: a
// resolving AB$ PutCounter under two non-commuting count replacements
// re-emitted its counter event on every pass).
//
// The chosen body can itself ASK (a shock land's "pay 2 life or it enters
// tapped" UnlessCost): the nested Engine.Ask then replaces e.resume with its
// own frame, so rp survives only here. Two shapes follow:
//
//   - the queue is drained: chain rp behind the nested frame (fx34's
//     discipline), so the resolution resumes once that inner question
//     settles;
//   - more competitions are queued: the resolution must NOT resume until the
//     last of them is answered, so rp cannot ride the nested frame (which
//     completes first). It is handed to every queued in-resolution
//     competition instead (resumeAtPose), and handleReplacement reinstates it
//     when the next answer finds e.resume empty. Without the hand-off the
//     frame was lost and the next answer resumed a nil frame (the botbench
//     panic: Lumra, Bellow of the Woods returning Overgrown Tomb and other
//     lands under Horizon Explorer).
//
// The pose record of a competition answered while nothing was resolving
// (turn structure, a cast window) is the flow's own bookkeeping: once the
// composition completed synchronously the stale frame is dropped --
// resolveTop reads e.resume to decide whether its resolution suspended, and a
// stale frame makes it abandon a resolution that actually finished,
// re-resolving it on every pass (observed: a land entry's order pose left the
// frame and a later resolving ability re-resolved unbounded).
func (e *Engine) settleReplacementQueue(rc replChoice, rp *resumePoint) {
	if !rc.inResolution {
		// A ReplaceWith$ body resolved OUTSIDE any resolution pass
		// (resolveReplacementBody's non-resolution arm) that posed this ask
		// linked its own continuation chain -- the body's SubAbility$ work
		// reported into e.contChain -- onto the ask frame's outer. e.resume is
		// then not the flow's own bookkeeping but a real continuation: run it
		// instead of dropping the frame. Without this, a body whose nested
		// mint parked on a CR 616.1 entry-counter order lost its rider (the
		// token entered with the answered counters, the controller never
		// gained the life). A frame with no outer continuation keeps the
		// historical synchronous drop.
		if rp != nil && e.resume == rp && rp.outer != nil &&
			e.pending == nil && len(e.replChoices) == 0 {
			e.resume = nil
			e.resumeResolution(rp, nil)
			return
		}
		if rc.resumeAtPose != nil && e.resume == rc.resumeAtPose &&
			e.pending == nil && len(e.replChoices) == 0 {
			e.resume = nil
		}
		return
	}
	if rp == nil {
		// Nothing suspended to resume: a competition posed under an
		// already-owned resume point whose owner consumed it. Resuming a nil
		// frame is the panic this tail exists to avoid.
		return
	}
	if len(e.replChoices) > 0 {
		if e.resume != rp {
			for i := range e.replChoices {
				if e.replChoices[i].inResolution && e.replChoices[i].resumeAtPose == nil {
					e.replChoices[i].resumeAtPose = rp
				}
			}
		}
		return
	}
	if e.resume == rp {
		if e.pending == nil {
			e.resume = nil
			e.resumeResolution(rp, nil)
		}
		return
	}
	if e.resume == nil {
		// The body's own flow consumed the frame; it owns the continuation.
		return
	}
	// A nested ask the chosen body posed owns e.resume: run rp after its
	// whole continuation chain, unless it is already on that chain.
	tail := e.resume
	for {
		if tail == rp {
			return
		}
		if tail.outer == nil {
			break
		}
		tail = tail.outer
	}
	tail.outer = rp
}

// askNextReplacementChoice hands over to either an ordinary replacement
// competition or a simultaneous Madness choice after the current answer.
func (e *Engine) askNextReplacementChoice() {
	if e.pending != nil {
		return
	}
	if len(e.replChoices) > 0 {
		if p, ok := e.replacementChoicePlayer(e.replChoices[0]); ok {
			e.askReplacementChoice(p)
		}
		return
	}
	if len(e.madnessChoices) > 0 {
		if o := e.G.Obj(e.madnessChoices[0].Obj); o != nil && int(o.Owner) < len(e.G.Players) {
			e.askMadnessReplacement(o.Owner)
		}
	}
}

// handleDamageReplacementChoice returns false only when recomputation leaves
// another genuine order choice pending; true means the parked damage event is
// fully prevented/replaced or has landed with all riders.
func (e *Engine) handleDamageReplacementChoice(rc replChoice, selected int) bool {
	savedDamaging, savedCombat := e.damaging, e.combatDamaging
	e.damaging, e.combatDamaging = rc.damaging, rc.combat
	defer func() { e.damaging, e.combatDamaging = savedDamaging, savedCombat }()
	var m replMatch
	if selected == len(rc.cands) {
		// This is the explicit "do not apply" answer for an Optional$ True
		// replacement. Mark every currently applicable optional replacement as
		// used so recomputation cannot immediately pose the same question again;
		// non-optional replacements remain eligible and still apply.
		for _, m := range rc.cands {
			if strings.EqualFold(m.repl.Params["Optional"], "True") {
				rc.used = append(rc.used, m)
			}
		}
	} else {
		m := rc.cands[selected]
		rc.used = append(rc.used, m)
		if e.applyChosenDamageReplacement(&rc.ev, m) {
			return true
		}
	}
	for {
		rc.cands = e.remainingDamageReplacements(rc.ev, rc.used)
		switch len(rc.cands) {
		case 0:
			e.finishChosenDamage(rc)
			return true
		case 1:
			m = rc.cands[0]
			rc.used = append(rc.used, m)
			if e.applyChosenDamageReplacement(&rc.ev, m) {
				return true
			}
		default:
			// The first modification can leave several effects applicable. Ask
			// again over exactly that recomputed set (CR 616.1e), preserving
			// the already-modified amount and the original damage rider
			// metadata. CR 616.1e also recomputes the AFFECTED player: after a
			// redirection the choice belongs to the new recipient's controller,
			// never the original one, so re-derive it from the modified event.
			if p, ok := e.damageAffectedPlayer(rc.ev); ok && !e.G.Players[p].Lost {
				rc.player = p
				e.replChoices = append([]replChoice{rc}, e.replChoices...)
				if e.pending == nil {
					e.askReplacementChoice(p)
				}
				return false
			}
			// CR 800.4a: the recomputed affected player is lost or gone and
			// makes no choices, so the remaining candidates apply in
			// deterministic scan order -- the same fallback the initial pose
			// takes -- and the parked event settles here.
			for {
				if len(rc.cands) == 0 {
					e.finishChosenDamage(rc)
					return true
				}
				m = rc.cands[0]
				rc.used = append(rc.used, m)
				if e.applyChosenDamageReplacement(&rc.ev, m) {
					return true
				}
				rc.cands = e.remainingDamageReplacements(rc.ev, rc.used)
			}
		}
	}
}

func (e *Engine) remainingDamageReplacements(ev events.Event, used []replMatch) []replMatch {
	var out []replMatch
	alreadyUsed := func(m replMatch) bool {
		for _, u := range used {
			if u.id == m.id && (u.repl == m.repl || (m.key != "" && u.key == m.key)) {
				return true
			}
		}
		return false
	}
	for _, ce := range e.active() {
		if ce.ReplacementEvent == "" {
			continue
		}
		if with := replacementBodySA(ce.ReplacementBody); with != nil {
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams, With: with}
			m := replMatch{id: ce.Source, repl: r,
				remembered: ce.Remembered, rememberedPlayers: ce.RememberedPlayers,
				chosen: ce.ChosenNumber,
				key:    "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))}
			if !alreadyUsed(m) && e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) &&
				!(damageReplacementPrevents(*r) && e.cantPreventDamage(e.damaging, ev.Obj)) {
				out = append(out, m)
			}
		} else if ce.ReplacementBody == "" && ce.ReplacementEvent == "DamageDone" &&
			strings.EqualFold(ce.ReplacementParams["Prevent"], "True") {
			// Mirror applyReplacementsDispatch's bodyless-Prevent admission
			// (dponce1 r2): after a first NONTERMINAL application (a partial
			// DB$ ReplaceDamage body reduced the held event), the recomputed
			// CR 616.1e candidate set must still hold the bodyless "prevent
			// all" effect — otherwise the remaining damage the registration
			// exists to prevent lands silently.
			r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
			m := replMatch{id: ce.Source, repl: r,
				remembered: ce.Remembered, rememberedPlayers: ce.RememberedPlayers,
				key: "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))}
			if !alreadyUsed(m) && e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) &&
				!(damageReplacementPrevents(*r) && e.cantPreventDamage(e.damaging, ev.Obj)) {
				out = append(out, m)
			}
		}
	}
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		for i := range o.Face().Repls {
			r := &o.Face().Repls[i]
			if alreadyUsed(replMatch{id: id, repl: r}) || !e.replacementMatches(*r, id, ev) {
				continue
			}
			if damageReplacementPrevents(*r) && e.cantPreventDamage(e.damaging, ev.Obj) {
				continue
			}
			out = append(out, replMatch{id: id, repl: r})
		}
	})
	return out
}

// applyChosenDamageReplacement modifies ev in place. It reports terminal when
// the chosen effect prevented/replaced the damage entirely; ReplaceEffect is
// nonterminal and lets applicability be recomputed against its new amount.
func (e *Engine) applyChosenDamageReplacement(ev *events.Event, m replMatch) bool {
	// Defense in depth: both candidate-producing paths already filter
	// prevention bodies out under CantPreventDamage, but the application
	// point re-checks so a future selection path cannot reintroduce the
	// leak. A skipped body is nonterminal, so the caller recomputes the
	// remaining candidates against the still-standing event (m is in
	// rc.used, so it cannot be picked twice).
	if damageReplacementPrevents(*m.repl) && e.cantPreventDamage(e.damaging, ev.Obj) {
		return false
	}
	if strings.EqualFold(m.repl.Params["Prevent"], "True") {
		// The ordered path's full prevention is terminal — the held event
		// never lands — so this re-entrant Note is the prevention's only log
		// record, the same shape applyNonMoveReplacements' Prevent$ arm
		// stores (dponce1 r2: silently returning terminal recorded nothing,
		// so no DamagePreventedOnce trigger could fire off a chosen
		// prevention). Amount is the held event's REMAINING amount: a
		// partial DB$ ReplaceDamage body may already have reduced it (CR
		// 616.1e), and TriggerCount$DamageAmount reads the amount THIS
		// prevention prevented.
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Amount: ev.Amount, Text: "damage prevented by replacement effect"})
		return true
	}
	if m.repl.With == nil {
		return true
	}
	if m.repl.With.API == "ReplaceDamage" {
		// The body subtracts its Amount from the held event; a fully
		// prevented event is terminal, a reduced one stands for the
		// recomputation below (CR 616.1e).
		return e.applyReplaceDamageBody(ev, m)
	}
	e.runReplaceWith(e.replCtx(m, *ev), ev.Obj, m.repl.With, ev)
	return m.repl.With.API != "ReplaceEffect"
}

func (e *Engine) finishChosenDamage(rc replChoice) {
	savedDamaging, savedCombat, savedApplying := e.damaging, e.combatDamaging, e.applyingReplacement
	e.damaging, e.combatDamaging, e.applyingReplacement = rc.damaging, rc.combat, true
	applied := e.emit(rc.ev)
	e.damaging, e.combatDamaging, e.applyingReplacement = savedDamaging, savedCombat, savedApplying
	if applied.Kind != events.Damage || applied.Amount <= 0 {
		return
	}
	if rc.deadly && applied.Obj != 0 {
		e.emit(events.Event{Kind: events.CounterChange, Obj: applied.Obj,
			Counter: "Deathtouched", Amount: 1})
	}
	if rc.lifelink {
		e.emit(events.Event{Kind: events.LifeChange, Player: e.controllerOf(rc.damaging), Amount: applied.Amount})
	}
	if rc.combat && applied.Obj == 0 {
		if e.format == FormatCommander {
			e.tallyCmdDamage(applied.Player, rc.damaging, applied.Amount)
		}
		// The combat-damage ledger's SECOND append site, mirroring the
		// commander tally's established twin path: runCombatAssignments parks
		// any player-targeted combat damage whose CR 616.1 competition is
		// posed (len(matches) > 1, or ANY Optional$ True damage replacement —
		// Battletide Alchemist's "you may prevent X" alone) and never reaches
		// its own append, so the parked event's resolution must record the hit
		// here or a player who WAS dealt combat damage never enters the ledger
		// and Lost Monarch of Ifnir's intervening-if reads 0. All terminal
		// paths of handleDamageReplacementChoice route through here; a fully
		// prevented/replaced event returned above (applied.Kind != Damage or
		// Amount <= 0), and a redirect ONTO a permanent zeroes nothing but
		// fails the Obj == 0 guard exactly as the capture site's guard does.
		e.combatHitsThisTurn = append(e.combatHitsThisTurn, e.combatHit(applied.Player, rc.damaging, applied.Amount))
		// CR 702.164's SECOND poison site, mirroring the ledger append's twin
		// path: a parked player-targeted combat hit never reaches
		// runCombatAssignments' synchronous toxic emit, so the landed event
		// must place the cached toxic poison here or a dealt player keeps 0
		// poison (Battletide Alchemist's optional prevention, declined or
		// applying a 0-amount prevent, both land here). The rc.combat flag and
		// the Obj == 0 guard are the same ones the capture site guards with: a
		// non-combat Damage event or a redirect ONTO a permanent means no
		// player was dealt combat damage, so no poison is placed. Toxic rides
		// the LANDED amount (the early return above already skipped a fully
		// prevented/replaced event, where CR 702.164b's trigger never met).
		if rc.toxic > 0 {
			e.emit(events.Event{Kind: events.PlayerCounterChange,
				Player: applied.Player, Counter: "POISON", Amount: int32(rc.toxic)})
		}
	}
}

// applyLifeReplacements evaluates the GainLife and LifeReduced replacements
// that apply to a proposed life gain or life loss before it reaches the log.
// The transformations are read from the replacement body's ReplaceCount$
// grammar, not card names, so Archives, Reflection, Cleric Class, Bloodletter
// and their corpus siblings share one path.
//
// CR 616.1: when more than one replacement would modify the event, the
// affected player chooses one to apply, then applicability is re-checked
// against the modified event (CR 616.1e) and the choice repeats until none is
// left. Each replacement applies at most once to the event (CR 614.5). The
// order choice reuses the engine's one KReplacement decision path
// (poseLifeReplacementChoice / handleReplacement); it is skipped only when
// every competing replacement is the same commuting operator (all doublers,
// all "plus N", all prevention), where every order produces the same event.
func (e *Engine) applyLifeReplacements(ev events.Event) (events.Event, bool) {
	if ev.Kind == events.LifeChange && ev.Amount > 0 && e.lifeGainForbidden(ev.Player) {
		return e.emit(events.Event{Kind: events.Note, Player: ev.Player, Text: "prevented: cannot gain life"}), true
	}
	return e.continueLifeReplacements(ev, nil)
}

// continueLifeReplacements applies the remaining applicable replacements to
// ev, which the replacements in applied have already modified. It returns
// handled=true whenever anything replaced the event or a choice was parked.
func (e *Engine) continueLifeReplacements(ev events.Event, applied []replMatch) (events.Event, bool) {
	for {
		cands := e.lifeReplacementCandidates(ev, applied)
		if len(cands) == 0 {
			if e.lifeExchange != nil && e.lifeExchange.second.Kind != 0 {
				return e.stageExchangeLife(ev), true
			}
			if len(applied) == 0 {
				return ev, false
			}
			return e.emitLifeReplacement(ev)
		}
		if len(cands) > 1 && !e.lifeReplacementsCommute(ev, cands) && e.poseLifeReplacementChoice(ev, cands, applied) {
			return ev, true
		}
		m := cands[0]
		next, consumed := e.applyLifeReplacement(ev, m)
		if consumed {
			return ev, true
		}
		ev = next
		applied = append(applied[:len(applied):len(applied)], m)
	}
}

// poseLifeReplacementChoice parks a life event whose competing replacements
// do not commute and asks the affected player (CR 616.1: the player whose life
// total the event changes) which applies first. It declines, and the caller
// applies the first candidate in deterministic scan order, only where no
// choice can be made: the player has left the game (CR 800.4a -- they make
// no choices, and the event must still apply). While another decision is
// outstanding the competition parks on the queue BEHIND it and is asked when
// the queue drains (Submit's tail) -- never overwritten, never applied
// silently in its shadow.
func (e *Engine) poseLifeReplacementChoice(ev events.Event, cands, applied []replMatch) bool {
	p := ev.Player
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost {
		return false
	}
	rc := replChoice{ev: ev, cands: cands, before: e.triggerBefore, life: true,
		exchange:     e.lifeExchange,
		appliedRepls: applied, damaging: e.damaging, combatDamaging: e.combatDamaging,
		dmgSrcOverride: e.dmgSrcOverride, inResolution: e.resolvingObj != 0 || e.answerInResolution}
	if e.pending == nil {
		// The front of the queue is the competition being asked. A life choice
		// is asked immediately (pending is nil), so it goes first.
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		e.askReplacementChoice(p)
	} else {
		// CR 616.1 with the queue: the competition parks behind the
		// outstanding decision and the parked event stays in hand (applyLife
		// Replacements returns handled=true) until the answer.
		e.replChoices = append(e.replChoices, rc)
	}
	return true
}

// lifeReplacementCandidates collects, in forEachObject's deterministic scan
// order, every replacement not yet applied to ev that would modify it now.
// A gain is only ever modified by GainLife replacements and a loss only by
// LifeReduced ones, so a replacement that turns a gain into a loss (Tainted
// Remedy) leaves every other GainLife replacement inapplicable.
func (e *Engine) lifeReplacementCandidates(ev events.Event, applied []replMatch) []replMatch {
	event, p, loss := "", state.PlayerID(0), int32(0)
	if ev.Kind == events.LifeChange && ev.Amount > 0 {
		event, p = "GainLife", ev.Player
	} else if q, amount, ok := lifeLoss(ev); ok {
		event, p, loss = "LifeReduced", q, amount
	} else {
		return nil
	}
	var out []replMatch
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		for i := range o.Face().Repls {
			r := &o.Face().Repls[i]
			if r.Event != event || !replacementActive(e, id, r) || !replacementPlayerMatches(e, id, r, p) ||
				lifeReplacementApplied(applied, replMatch{id: id, repl: r}) {
				continue
			}
			if e.lifeReplacementApplies(ev, id, r, p, loss) {
				out = append(out, replMatch{id: id, repl: r})
			}
		}
	})
	for _, ce := range e.active() {
		if ce.ReplacementEvent != event || ce.ReplacementBody != "" ||
			!strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Prevent"]), "True") {
			continue
		}
		r := &cards.Repl{Event: ce.ReplacementEvent, Params: ce.ReplacementParams}
		m := replMatch{id: ce.Source, repl: r, remembered: ce.Remembered,
			rememberedPlayers: ce.RememberedPlayers,
			key:               "effect:" + strconv.Itoa(int(ce.Source)) + ":" + strconv.Itoa(int(ce.Timestamp))}
		if lifeReplacementApplied(applied, m) ||
			!e.replacementMatchesEffectCreated(*r, ce.Source, ev, ce.Remembered, ce.RememberedPlayers) {
			continue
		}
		if e.lifeReplacementApplies(ev, ce.Source, r, p, loss) {
			out = append(out, m)
		}
	}
	return out
}

func lifeReplacementApplied(applied []replMatch, candidate replMatch) bool {
	for _, m := range applied {
		if candidate.key != "" || m.key != "" {
			if m.key != "" && m.key == candidate.key {
				return true
			}
			continue
		}
		if m.id == candidate.id && m.repl == candidate.repl {
			return true
		}
	}
	return false
}

// lifeReplacementApplies reports whether replacement r of source would do
// something to ev. A replacement whose body this engine cannot perform is not
// a candidate, so it never occupies an order choice.
func (e *Engine) lifeReplacementApplies(ev events.Event, source state.ObjID, r *cards.Repl, p state.PlayerID, loss int32) bool {
	if r.Event == "GainLife" {
		if strings.EqualFold(r.Params["Prevent"], "True") {
			return true
		}
		if !e.replacementCondition(source, r) || r.Params["ValidSource"] != "" {
			return false
		}
		if _, ok := e.replaceCount(source, r, "LifeGained", ev.Amount); ok {
			return true
		}
		return r.With != nil && (r.With.API == "LoseLife" || r.With.API == "Draw")
	}
	if strings.EqualFold(r.Params["IsDamage"], "True") && ev.Kind != events.Damage {
		return false
	}
	if strings.EqualFold(r.Params["PlayerTurn"], "True") && e.G.Active != e.controllerOf(source) {
		return false
	}
	if !e.replacementCondition(source, r) {
		return false
	}
	if result := r.Params["Result"]; result != "" && !compareLife(e.G.Players[p].Life-loss, result) {
		return false
	}
	if _, ok := e.replaceCount(source, r, "Amount", loss); ok {
		return true
	}
	// A non-ReplaceEffect body (Enduring Angel's transform then SetLife)
	// wholly replaces the loss.
	o := e.G.Obj(source)
	return r.With != nil && o != nil && o.Face() != nil
}

// lifeReplacementsCommute reports whether every order of cands yields the same
// event: all prevent the gain, all double, or all add a constant, and none
// gates its own applicability on the running amount (Result$).
func (e *Engine) lifeReplacementsCommute(ev events.Event, cands []replMatch) bool {
	name := "Amount"
	if ev.Kind == events.LifeChange && ev.Amount > 0 {
		name = "LifeGained"
	}
	kind := ""
	for _, m := range cands {
		if m.repl.Params["Result"] != "" {
			return false
		}
		k := ""
		switch op, ok := e.replaceCountOp(m.id, m.repl, name); {
		case m.repl.Event == "GainLife" && strings.EqualFold(m.repl.Params["Prevent"], "True"):
			k = "prevent"
		case ok && op == "/Twice":
			k = "twice"
		case ok && strings.HasPrefix(op, "/Plus."):
			k = "plus"
		default:
			return false
		}
		if kind != "" && k != kind {
			return false
		}
		kind = k
	}
	return true
}

// applyLifeReplacement applies one chosen replacement to ev. It returns the
// modified event, or consumed=true when the replacement's own body wholly
// replaced the event (prevention, a draw or a primitive chain instead).
func (e *Engine) applyLifeReplacement(ev events.Event, m replMatch) (events.Event, bool) {
	r := m.repl
	if r.Event == "GainLife" {
		if strings.EqualFold(r.Params["Prevent"], "True") {
			e.emit(events.Event{Kind: events.Note, Player: ev.Player, Text: "prevented: cannot gain life"})
			return ev, true
		}
		if amount, ok := e.replaceCount(m.id, r, "LifeGained", ev.Amount); ok {
			ev.Amount = amount
			return ev, false
		}
		switch r.With.API {
		case "LoseLife":
			// That player loses that much life instead: the event is now a
			// loss, so only LifeReduced replacements can modify it further.
			ev.Amount = -ev.Amount
			return ev, false
		case "Draw":
			e.lifeReplacementDraw(ev.Player, ev.Amount)
			return ev, true
		}
		return ev, false
	}
	_, loss, _ := lifeLoss(ev)
	if amount, ok := e.replaceCount(m.id, r, "Amount", loss); ok {
		if ev.Kind == events.Damage {
			ev.Amount = amount
		} else {
			ev.Amount = -amount
		}
		return ev, false
	}
	o := e.G.Obj(m.id)
	if o == nil || o.Face() == nil || r.With == nil {
		// The source no longer exists (a parked choice answered after it
		// left): its replacement has nothing left to perform.
		return ev, false
	}
	e.runReplaceWith(&effects.Ctx{Source: m.id, Controller: o.Controller, SVars: o.Face().SVars}, 0, r.With, nil)
	return ev, true
}

// lifeReplacementDraw draws n cards for a GainLife→Draw replacement body
// (Lich's "If you would gain life, draw that many cards instead"),
// suspension-aware: each DrawFor may pose a Dredge ask (CR 702.55) and
// suspend. The loop parks the remaining count on the ask's resume point
// (resolution.go) and returns, instead of looping on -- looping on would
// pose a SECOND ask while the first is outstanding, orphaning it and losing
// the remaining draws (findings-sol4 MAJOR). The answered dredge re-drives
// the rest from handleModes' direct arm (stack empty) or resumeResolution's
// dredge arm (a resolving object on the stack), both of which drain any
// replacement-order queue the interrupted pass left behind.
func (e *Engine) lifeReplacementDraw(p state.PlayerID, n int32) {
	for i := int32(0); i < n; i++ {
		effects.DrawFor(e, p)
		if e.Suspended() {
			e.resume.lifeDraws = n - (i + 1)
			return
		}
	}
}

// emitLifeReplacement logs a fully transformed event without starting a new
// replacement pass: every applicable replacement has had its one opportunity.
// A gain reduced to nothing (LimitMax of zero) is no event at all.
func (e *Engine) emitLifeReplacement(ev events.Event) (events.Event, bool) {
	if e.lifeExchange != nil && e.lifeExchange.second.Kind != 0 {
		return e.stageExchangeLife(ev), true
	}
	if ev.Kind == events.LifeChange && ev.Amount == 0 {
		return ev, true
	}
	saved := e.applyingReplacement
	e.applyingReplacement = true
	stored := e.emit(ev)
	e.applyingReplacement = saved
	return stored, true
}

// replacementCondition reads the common CheckSVar$/SVarCompare$ gate (Phial
// of Galadriel) from the replacement source's current context.
func (e *Engine) replacementCondition(source state.ObjID, r *cards.Repl) bool {
	if !e.classBandGateHolds(r.Params, source) {
		return false
	}
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return false
	}
	if spec := r.Params["IsPresent"]; spec != "" {
		found := false
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				if e.matchesSpec(spec, id, e.specCtx(source, o.Controller)) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	name := r.Params["CheckSVar"]
	if name == "" {
		return true
	}
	value := effects.EvalCount(e, &effects.Ctx{Source: source, Controller: o.Controller, SVars: o.Face().SVars}, o.Face().SVars[name])
	return compareLife(value, r.Params["SVarCompare"])
}

// replaceCount evaluates ReplaceEffect's ReplaceCount$Amount/LifeGained
// forms. It follows SVar indirection and supports the three corpus operators:
// Twice, Plus.N and LimitMax.<SVar>.
func (e *Engine) replaceCount(source state.ObjID, r *cards.Repl, name string, amount int32) (int32, bool) {
	op, ok := e.replaceCountOp(source, r, name)
	if !ok {
		return 0, false
	}
	o := e.G.Obj(source)
	if op == "/Twice" {
		return amount * 2, true
	}
	if n, err := strconv.ParseInt(strings.TrimPrefix(op, "/Plus."), 10, 32); strings.HasPrefix(op, "/Plus.") && err == nil {
		return amount + int32(n), true
	}
	if arg, ok := strings.CutPrefix(op, "/LimitMax."); ok {
		limit := effects.EvalCount(e, &effects.Ctx{Source: source, Controller: o.Controller, SVars: o.Face().SVars}, o.Face().SVars[arg])
		if limit < 0 {
			limit = 0
		}
		if amount > limit {
			amount = limit
		}
		return amount, true
	}
	return 0, false
}

// replaceCountOp returns the operator suffix ("/Twice", "/Plus.1", ...) of a
// ReplaceEffect body's ReplaceCount$<name> value, following SVar indirection.
func (e *Engine) replaceCountOp(source state.ObjID, r *cards.Repl, name string) (string, bool) {
	if r.With == nil || r.With.API != "ReplaceEffect" || !strings.EqualFold(r.With.Params["VarName"], name) {
		return "", false
	}
	expr := r.With.Params["VarValue"]
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return "", false
	}
	if body, ok := o.Face().SVars[expr]; ok {
		expr = body
	}
	prefix := "ReplaceCount$" + name
	if !strings.HasPrefix(expr, prefix) {
		return "", false
	}
	return strings.TrimPrefix(expr, prefix), true
}

// lifeGainForbidden checks active CantGainLife statics against the player who
// would gain life. R:Event$ GainLife Prevent$ True lines are replacement
// effects and compete in the CR 616.1 order choice instead. The static's
// ValidPlayer$ scope is read here, in the static's own parameter bucket.
func (e *Engine) lifeGainForbidden(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("CantGainLife") {
		if spec := sv.Params["ValidPlayer"]; spec == "" ||
			effects.MatchesPlayerSpec(e.G, spec, p, sv.Controller) {
			return true
		}
	}
	return false
}

// drawForbidden checks active CantDraw statics against the player who would
// draw a card (CR 121.6). It is lifeGainForbidden's sibling: the same
// battlefield-static collector, the same ValidPlayer$ scope read in the
// static's own parameter bucket, consulted by the same replacement pass.
//
// One parameter is deliberately not read: DrawLimit$ N ("each opponent can't
// draw more than one card each turn", Leovold / Narset, Parter of Veils /
// Spirit of the Labyrinth -- 3 of the class's 7 corpus carriers) is a per-turn
// COUNT CAP, not a total prohibition: the draws at or below N still happen,
// so enforcing it as "cannot draw at all" would over-block. The unread shape
// is skipped in the permissive direction, matching every other unwhitelisted
// static parameter in this file (see cantRestrictionParamsReadable): a
// static carrying it prohibits nothing this build enforces, rather than
// prohibiting everything.
func (e *Engine) drawForbidden(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("CantDraw") {
		if spec := sv.Params["ValidPlayer"]; spec != "" &&
			!effects.MatchesPlayerSpec(e.G, spec, p, sv.Controller) {
			continue
		}
		if _, hasLimit := sv.Params["DrawLimit"]; hasLimit {
			continue
		}
		return true
	}
	return false
}

func replacementActive(e *Engine, source state.ObjID, r *cards.Repl) bool {
	if !e.commandReplZoneAdmits(*r, source) {
		return false
	}
	active, ok := r.Params["ActiveZones"]
	if !ok {
		return true
	}
	o := e.G.Obj(source)
	return o != nil && zoneSpecContains(active, o.Zone)
}

func replacementPlayerMatches(e *Engine, source state.ObjID, r *cards.Repl, p state.PlayerID) bool {
	if int(p) >= len(e.G.Players) {
		return false
	}
	v := r.Params["ValidPlayer"]
	return v == "" || effects.MatchesPlayerSpec(e.G, v, p, e.controllerOf(source))
}

func init() {
	// kw:etbCounter and kw:ETBReplacement are implemented wholly by the
	// machinery above: both are R:Event$ Moved replacements (expanded from a
	// K: line by cards/keywords.go) matched and applied here. Reading a card's
	// own tags is what a replacement registration means -- nothing elsewhere
	// in the tree registers them.
	//
	// kw:Devour and kw:Ravenous (CR 702.148) are the same idea for a K: line
	// that expands to an ETB trigger instead of a replacement (kw:Devour's
	// optional sacrifice + counter put, kw:Ravenous's X +1/+1-counter put
	// plus the X>=5 conditional draw, both in cards/kw_*.go). The marker
	// exists only so the coverage ratchet sees the head as supported; the
	// machinery it needs (trig:ChangesZone, api:PutCounter, api:Draw, the
	// SVar-condition gate) is all registered under its own primitives.
	//
	// The four turn/mana replacement events register the same way: repl:Untap
	// (the Basalt Monolith class), repl:BeginPhase (the Necropotence class),
	// repl:Transform (the Sephiroth class) and repl:ProduceMana (the Virtue
	// of Strength class, whose ReplaceWith$ body DB$ ReplaceMana is a
	// registered API). Each is matched by replacementMatches's per-event
	// branch above and applied by applyReplacements's dispatch.
	//
	// The life classes register too: repl:GainLife (the Prevent$ GainLife
	// shape plus the CantGainLife static's replacement arm) and
	// repl:LifeReduced (the ReplaceCount$Amount/Twice shape), both applied by
	// applyLifeReplacements. repl:DamageDone and repl:Counter are this
	// ticket's own additions, matched by replacementMatches's DamageDone case
	// and CounterAllowed respectively.
	// kw:Bloodthirst (CR 702.54) is implemented by this file's
	// bloodthirstEntryMatch: the keyword line (printed K:Bloodthirst:<N> or a
	// layer-6 AddKeyword$ grant) is read at MoveZone→Battlefield collection
	// time from the entering object's DERIVED keyword list, so one read
	// covers both shapes -- a printed carrier and Twins of Discord's
	// `Affected$ Creature.Other+YouCtrl+Colorless | AddKeyword$ Bloodthirst:2`
	// grant, which cards-side expansion could never see. No cards-side
	// expansion exists: bloodthirst is a static ability whose whole meaning
	// is an entry-time conditional counter put, which is exactly what the
	// synthetic Repl below expresses.
	effects.RegisterNonAPI("kw:etbCounter", "kw:ETBReplacement", "kw:Devour", "kw:Ravenous", "kw:Bloodthirst", "kw:Sunburst",
		"repl:Untap", "repl:BeginPhase", "repl:Transform", "repl:ProduceMana",
		"repl:GainLife", "repl:LifeReduced", "repl:DamageDone", "repl:Counter",
		"repl:CreateToken", "repl:RollPlanarDice", "repl:Explore", "repl:Attached", "repl:Scry", "api:ReplaceToken",
		"repl:AddCounter", "api:ReplaceCounter",
		// repl:GameLoss / repl:GameWin (task fdn-repl-cant-lose) are the
		// "you can't lose the game" / "your opponents can't win the game"
		// CantHappen class (Herald of Eternal Dawn, the Platinum Angel family,
		// Abyssal Persecutor, Lich's Mastery). Matching and application live in
		// rules/cantlose.go; this registration is what makes the coverage
		// ratchet see the heads as supported.
		"repl:GameLoss", "repl:GameWin",
		// repl:TurnFaceUp (task cli-20260924T031747Z-6d0658fc) is the "as this
		// is turned face up" class (Hooded Hydra's five +1/+1 counters, Karlov
		// Watchdog's CantHappen, Gift of Doom's attach), matched by
		// replacementMatches's TurnFaceUp case and applied by
		// applyTurnFaceUpReplacements.
		"repl:TurnFaceUp",
		// api:ReplaceDamage is handled inline by applyReplaceDamageBody (this
		// file) via the ReplaceDamage intercept in applyReplacements, never
		// through effects.Resolve/runReplaceWith -- this registration is the
		// census token only; a stub effects.Register handler would be dead code.
		"api:ReplaceDamage")
}

// cmdZoneMove is one parked commander zone change (CR 903.9, Task m32): the
// MoveZone event a commander is about to undergo, deferred until its owner
// decides whether to put it into the command zone instead. The answer
// re-emits the park as a real MoveZone -- to ZCommand on an accept, to the
// parked destination verbatim on a decline -- so the event log always
// carries the zone change that actually happened and a log-only replay
// reproduces it. Plain value data (an events.Event plus the moving object's
// id), so Clone copies the queue with one slice copy.
type cmdZoneMove struct {
	ev     events.Event
	obj    state.ObjID
	before *triggerSnapshot // immutable SBA look-back, safe to share in Clone
}

// commanderZoneReplacementApplies is CR 903.9's match predicate: a
// commander, in a Commander-format game, about to be put into its owner's
// graveyard, hand or library -- from ANYWHERE -- or about to be exiled from
// anywhere. The five points of the rule each live in exactly one place here
// (mutation guards, Task m32 brief item 7):
//
//   - FormatCommander gate: no format check, no mechanic. A Constructed
//     game never runs any of this.
//   - The four destinations: graveyard, hand, library, exile -- a
//     commander moving to the battlefield or the stack is not replaced.
//   - "From anywhere": ev.From is never consulted. The battlefield, the
//     stack, the graveyard, a hand a library or exile are all sources.
//   - Ownership, not control: the lookup is the owner's Commanders list,
//     not the controller's -- a commander stolen by an opponent goes to its
//     owner's command zone and its owner is asked.
func (e *Engine) commanderZoneReplacementApplies(ev events.Event) bool {
	if e.format != FormatCommander {
		return false
	}
	switch ev.To {
	case state.ZGraveyard, state.ZHand, state.ZLibrary, state.ZExile:
	default:
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Owner) >= len(e.G.Players) {
		return false
	}
	for _, c := range e.G.Players[o.Owner].Commanders {
		if c == ev.Obj {
			return true
		}
	}
	return false
}

// parkCommanderZoneMove applies the CR 903.9 replacement to one matching
// zone change: the event is deferred (never logged, never applied -- the
// commander stays where it is) and its owner is asked whether to put the
// commander into the command zone instead. handleCmdZone emits the parked
// move for real when the answer lands.
//
// Dedup by object: while a commander's decision is outstanding the only
// engine work that can run is (a) the resolution-chain tail that already
// emitted the first park and (b) Submit's repeating state-based-action pass.
// A second park for the same commander is therefore either the SAME zone
// change being re-offered (the SBA pass re-finding a commander it already
// had tried -- the no-progress shape a replacement must not loop on, the
// stalledCastLimit lesson: the already-pending decision covers it, so the
// duplicate is dropped) or a second effect in the same chain that in the
// real rules would resolve AFTER the commander has already moved (its own
// CR 608.2b Origin$-driven recheck would then skip it, so dropping is the
// more-correct outcome, not merely safe). The move itself always happens
// exactly once, through the front decision.
//
// An owner who has left the game cannot exercise a "may" choice (CR 800.4a:
// a departed player makes no choices), so the unexercised choice is a
// decline: the original move happens unchanged, emitted here under
// applyingReplacement so the commander check that just matched cannot
// re-park it (CR 616.1, a replacement applies only once).
func (e *Engine) parkCommanderZoneMove(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil {
		return
	}
	owner := o.Owner
	if e.G.Players[owner].Lost {
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(ev)
		e.applyingReplacement = saved
		return
	}
	for _, pm := range e.cmdZone {
		if pm.obj == ev.Obj {
			return
		}
	}
	e.cmdZone = append(e.cmdZone, cmdZoneMove{ev: ev, obj: ev.Obj, before: e.triggerBefore})
	if e.pending == nil {
		e.askCommandZone(owner)
	}
}

// askCommandZone poses the CR 903.9 choice for the FRONT parked move to its
// owner, following askTriggerOptional's shape: Min == Max == 1 over two
// options, first the "change the outcome" one, then the "let it happen"
// one. Only the front of the queue is ever asked -- see handleCmdZone's
// resumption for how the queue hands from one decision to the next.
func (e *Engine) askCommandZone(owner state.PlayerID) {
	pm := e.cmdZone[0]
	name := "this commander"
	if o := e.G.Obj(pm.obj); o != nil && o.Face() != nil && o.Face().Name != "" {
		name = o.Face().Name
	}
	dest := pm.ev.To.String()
	into := "Put " + name + " into the command zone"
	d := &decision.Decision{Player: owner, Kind: decision.KCommanderZone, Min: 1, Max: 1,
		Prompt: name + " would go to the " + dest + ": put it into the command zone instead?",
		Source: pm.obj,
		Options: []decision.Option{
			{Index: 0, Kind: "command_zone", Label: into, Obj: pm.obj, Player: owner},
			{Index: 1, Kind: "leave", Label: "Let it go to the " + dest, Obj: pm.obj, Player: owner},
		}}
	e.ask(d)
}

// handleCmdZone applies an answered CR 903.9 decision: the front parked move
// is emitted for real -- to the command zone if the owner chose
// "command_zone", verbatim (the destination it was heading for) if they
// chose "leave" -- and, if more moves are parked, the next one's owner is
// asked. The de-park emit runs under applyingReplacement: the CR 903.9
// replacement has already applied to this zone change, and CR 616.1 lets a
// replacement effect apply only once, so the final move is never re-parked
// and never re-asked -- a decline therefore cannot spin the engine by
// re-offering the same choice, and the SBA pass that re-finds the commander
// after this answer degrades the same way it would for any other completed
// replacement (the dedup in parkCommanderZoneMove handled its in-flight
// copy).
//
// The owner's choice is in the log as the Intents entry plus the
// DecisionAsk/DecisionMade events every decision emits; the outcome is the
// MoveZone event below, so a log-only replay reproduces both branches from
// the log alone. An answer with no parked move (only reachable from a
// hand-built decision -- every real ask parks one) degrades to a Note, the
// same totality stance as handleModes.
func (e *Engine) handleCmdZone(d *decision.Decision, in decision.Intent) {
	if len(e.cmdZone) == 0 {
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "commander-zone decision answered with no move parked"})
		return
	}
	pm := e.cmdZone[0]
	e.cmdZone = e.cmdZone[1:]
	to := pm.ev.To
	if opts := d.Chosen(in); len(opts) == 1 && opts[0].Kind == "command_zone" {
		to = state.ZCommand
	}
	saved, before := e.applyingReplacement, e.triggerBefore
	e.applyingReplacement, e.triggerBefore = true, pm.before
	e.emit(events.Event{Kind: events.MoveZone, Obj: pm.ev.Obj, From: pm.ev.From,
		To: to, Player: pm.ev.Player, Text: pm.ev.Text})
	e.applyingReplacement, e.triggerBefore = saved, before
	if len(e.cmdZone) > 0 && e.pending == nil {
		// More commanders were parked in the same burst (a board wipe, a
		// multiple-SBA pass): hand the front of the queue to its owner.
		if o := e.G.Obj(e.cmdZone[0].obj); o != nil && int(o.Owner) < len(e.G.Players) {
			e.askCommandZone(o.Owner)
		}
	}
}
