package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

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
		if !currentlyActive && !enteringActive && !e.batchMemberReplActive(active, o) {
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
	switch r.EventKind() {
	case cards.ReplAttached:
		if ev.Kind != events.Attach || len(ev.IDs) == 0 {
			return false
		}
		// ValidCard$ names the ATTACHING object (ev.Obj) in the replacement
		// source's frame: Psychic Paper's `ValidCard$ Card.Self` is "as
		// CARDNAME becomes attached", so another Equipment or an Aura
		// attaching never poses its choice.
		if v := r.ParamStr(cards.PKValidCard); v != "" && !e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		if v := r.ParamStr(cards.PKValidTarget); v != "" && !e.matchesSpecFrom(v, ev.IDs[0], you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplTurnFaceUp:
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
		if v, ok := r.Param(cards.PKValidCard); ok && v != "" {
			if !e.matchesSpec(v, ev.Obj, e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplCounter:
		// The Effect-created bodyless CantHappen form (Mistrise Village's
		// AntiMagic, reached only from counterReplacementMatchesAll's scan,
		// which passes a synthetic Event{Obj: target}): the remembered-scoped
		// ValidCard$ gates the countered stack object — the promise covers
		// exactly the spell the firing trigger captured. ValidSA$ is the
		// caller's counterValidSA read (shared with the printed-Repls path);
		// no ActiveZones read — an Effect's lifetime is active()'s, not its
		// source's zone.
		if v, ok := r.Param(cards.PKValidCard); ok {
			if !e.matchesSpec(v, ev.Obj, e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		return true
	case cards.ReplMoved:
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
			if spec := r.ParamStr(cards.PKValidCause); spec != "" && !trigmatch.DiscardCauseAdmits(boardOf(e), spec, source, ev) {
				return false
			}
		}
		// Origin$ is a zone SET: a single zone name or a comma list (the
		// trigger matcher reads it the same way). ParseZones bails closed on
		// an unknown token rather than degrading to graveyard, and Any/All is
		// a wildcard.
		if o, ok := r.ParamCode(cards.PKOrigin); ok && !effects.ZoneList(o).Admits(ev.From) {
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
		if d, ok := r.ParamCode(cards.PKDestination); ok && !effects.Destination(d).Admits(ev.To) {
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
		if v, ok := r.Param(cards.PKValidLKI); ok {
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
	case cards.ReplUntap:
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
		if s, ok := r.Param(cards.PKValidStepTurnToController); ok {
			if s != "You" || e.G.Step != state.StepUntap ||
				e.G.Active != e.controllerOf(ev.Obj) {
				return false
			}
		}
		if v, ok := r.Param(cards.PKValidCard); ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplBeginPhase:
		if ev.Kind != events.StepChange {
			return false
		}
		if ph, ok := r.Param(cards.PKPhase); ok {
			step, known := phaseStep(ph)
			if !known || step != ev.Step {
				return false
			}
		}
		// ValidPlayer$ You scopes "skip YOUR draw step" to the replacement
		// controller's own turn; a line with no ValidPlayer$ (Sands of Time's
		// "players skip their untap step") applies every turn.
		if vp, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, vp, e.G.Active, you) {
			return false
		}
		// Optional$ True is handled after matching by
		// posePhaseReplacementChoice: applicability is independent of whether
		// the affected player eventually chooses to apply it.
		// Hellbent$ True gates the skip on an empty hand (one corpus line).
		if r.ParamStr(cards.PKHellbent) == "True" && len(e.G.Zone(state.ZHand, you)) > 0 {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplBeginTurn:
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
		if r.ParamStr(cards.PKExtraTurn) != "True" {
			return false
		}
		// ValidPlayer$ Opponent scopes the skip to opponents of the
		// replacement's controller (Trouble in Pairs, Stranglehold); a line
		// with no ValidPlayer$ (Ugin's Nexus, Gerrard's Hourglass Pendant)
		// applies to ANY player's extra turn, the controller's own included.
		if vp, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		// Optional$-gated and ReplaceWith$-bearing shapes are not implemented:
		// extraTurnSkipped reports a matched line whose action is not Skip$
		// True loudly instead of silently skipping, and never silently skips.
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplTransform:
		if ev.Kind != events.FlipFace {
			return false
		}
		// The "as this transforms" replacement is written on the destination
		// face and applies to its own card's flip; replacementFace already
		// scanned the destination face for this event.
		if v, ok := r.Param(cards.PKValidCard); ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplPayLife:
		return payLifeReplacementMatches(e, r, source, ev, you)
	case cards.ReplGainLife:
		return gainLifeReplacementMatches(e, r, source, ev, rememberedPlayers, you)
	case cards.ReplDamageDone:
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
		if v, ok := r.Param(cards.PKValidCard); ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplProduceMana:
		// Only genuine production replaces: a ManaAdd without a producing
		// source (a test seed, a spend) and a negative Amount (spending, not
		// producing) are outside the class.
		if ev.Kind != events.ManaAdd || e.manaProducer == 0 || ev.Amount <= 0 || !e.manaFromTap {
			return false
		}
		if v, ok := r.Param(cards.PKValidCard); ok &&
			!e.matchesSpecFrom(v, e.manaProducer, you, source) {
			return false
		}
		// ValidActivator$ You: the player adding the mana (whoever activated
		// the mana ability) must be the replacement controller's side of the
		// spec. MatchesPlayerSpec fails closed on unknown qualifiers.
		if va, ok := r.Param(cards.PKValidActivator); ok &&
			!effects.MatchesPlayerSpec(e.G, va, ev.Player, you) {
			return false
		}
		// ReplaceOnly$ lives on the ReplaceWith$ body (Quarum Trench
		// Gnomes), but it is an applicability gate: converting a different
		// colour is not applying that replacement. Reading it here lets a
		// prior rewrite make the effect newly applicable during CR 616.1's
		// mandatory post-rewrite recheck.
		if r.With != nil {
			if only := strings.TrimSpace(r.With.ParamStr(cards.PKReplaceOnly)); only != "" && only != ev.Counter {
				return false
			}
		}
		// ManaAmount$ <op><n> gates on the size of the production being
		// replaced (Damping Sphere's "two or more mana").
		if ma, ok := r.Param(cards.PKManaAmount); ok {
			op, n, parsed := splitCompare(ma)
			if !parsed || !applyCompare(int(ev.Amount), op, n) {
				return false
			}
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplConnive:
		return replacementMatchesConnive(e, r, source, ev, you)
	case cards.ReplExplore:
		// The explore replacement (R:Event$ Explore, task explore1 —
		// Topography Tracker, Twists and Turns); ValidExplorer$ names the
		// proposed creature, using the replacement source's controller as You.
		if ev.Kind != events.Explore {
			return false
		}
		if v, ok := r.Param(cards.PKValidExplorer); ok &&
			!e.matchesSpecFrom(v, ev.Obj, you, source) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplMill, cards.ReplScry:
		return millScryProposalMatches(r, ev, func(v string) bool { return effects.MatchesPlayerSpec(e.G, v, ev.Player, you) }, func() bool { return e.replacementConditionHolds(r, source, you) })
	case cards.ReplRollDice:
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
		if v, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, v, ev.Player, you) {
			return false
		}
		// ValidSides$ (the Whacker's six-sided gate) is unread: the proposal
		// carries no die-size field to gate on, and its one corpus carrier is
		// the unmodelled SwapRoll body the dispatch skips loudly -- a sides
		// gate is never silently widened onto a die of another size.
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplDraw, cards.ReplDrawCards:
		if ev.Kind != events.Draw {
			return false
		}
		// CR 611.3b: the ActiveZones gate above applies (a Draw replacement's
		// source is already on the battlefield — there is no entering case,
		// a card cannot replace the draw of the event that would put it into
		// play).
		if v, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, v, ev.Player, you) {
			return false
		}
		// NotFirstCardInDrawStep$ True exempts the player's own turn-based
		// draw (CR 504.1) — the "except the first one they draw in each of
		// their draw steps" clause on Notion Thief, Hullbreacher, Chains of
		// Mephistopheles and the other five carriers. Applied at match time,
		// before the proposed Draw is logged, so the pre-emit helper is the
		// one that can see it.
		if strings.EqualFold(strings.TrimSpace(r.ParamStr(cards.PKNotFirstCardInDrawStep)), "True") &&
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
		if raw, ok := r.Param(cards.PKActivePhases); ok {
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
		if strings.EqualFold(strings.TrimSpace(r.ParamStr(cards.PKFirstExtraCardDrawnThisTurn)), "True") {
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
		if spec := strings.TrimSpace(r.ParamStr(cards.PKValidCause)); spec != "" &&
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
	case cards.ReplCreateToken:
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
		if v, ok := r.Param(cards.PKValidToken); ok {
			tok := tokenOverride
			if tok == nil {
				tok = e.tokenSnapshot(ev)
			}
			if tok == nil || !effects.MatchesObjectCtx(e.G, v, tok,
				e.rememberedSpecContext(you, source, remembered)) {
				return false
			}
		}
		if vp, ok := r.Param(cards.PKValidPlayer); ok &&
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
	case cards.ReplAddCounter:
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
		if ct := strings.TrimSpace(r.ParamStr(cards.PKValidCounterType)); ct != "" && ct != ev.Counter {
			return false
		}
		// ValidPlayer$ scopes the counter's RECIPIENT PLAYER, so it only
		// applies to the player form (PlayerCounterChange). An object
		// CounterChange leaves ev.Player at its zero value, so without this
		// form gate a ValidPlayer$ You line reduces to ev.Player == you ->
		// 0 == 0 -> true and fires on every object placement (Winding
		// Constrictor has both an object line and a ValidPlayer$ You line).
		if vp, ok := r.Param(cards.PKValidPlayer); ok {
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
		spec := strings.TrimSpace(r.ParamStr(cards.PKValidCard))
		if spec == "" {
			spec = strings.TrimSpace(r.ParamStr(cards.PKValidObject))
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
		if vs := strings.TrimSpace(r.ParamStr(cards.PKValidSource)); vs != "" {
			adder, ok := e.inFlightCounterAdder()
			if !ok || !effects.MatchesPlayerSpec(e.G, vs, adder, you) {
				return false
			}
		}
		// ValidCause$ names the object that caused the placement (Zabaz's
		// "a modular triggered ability would put ..."): the resolving stack
		// object, exactly the provenance the Moved case's ValidCause$ reads.
		// An absent cause (0) fails closed in replacementCauseMatches.
		if vc := strings.TrimSpace(r.ParamStr(cards.PKValidCause)); vc != "" {
			if !e.replacementCauseMatches(vc, source, e.actionCause()) {
				return false
			}
		}
		// EffectOnly$ True (Doubling Season, Selesnya Loft Gardens) admits only
		// placements that are the EFFECT of a resolving spell or ability ("If an
		// EFFECT would put one or more counters ..."). It excludes a placement
		// with no object on the stack: a turn-based action (a Saga's lore
		// counter, rules/saga.go advanceSagas) and a cost (a planeswalker's [+N]
		// loyalty counter, pay.EmitChoiceCosts; a station counter,
		// rules/station.go handleStation) are not effects, and admitting them
		// doubled counters they must not touch. This is exactly the
		// actionCause()==0 provenance the Moved case's EffectOnly$ gate reads
		// (costs are paid before an activated ability exists on the stack, so
		// they deliberately have no cause) -- one shared test, not a second
		// hand-built identity stamp. A resolving TRIGGERED ability's instruction
		// (a cumulative-upkeep age counter, rules/cumulative.go) IS an effect
		// (CR 609.1), so it still qualifies.
		if r.ParamStr(cards.PKEffectOnly) == "True" && e.actionCause() == 0 {
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
	case cards.ReplRollPlanarDice:
		// The planar-dice replacement class (Ichor Elixir, task rollplanar1):
		// "if you would roll one or more planar dice, instead roll that many
		// planar dice plus one and ignore one". ValidPlayer$ scopes the roller
		// the same way the Draw class reads it; the count/ignore rewrites are
		// the With's own ReplaceEffect bodies (ReplaceEvent's PlanarRoll arm).
		if ev.Kind != events.PlanarRoll {
			return false
		}
		if vp, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplCascade:
		// The cascade instruction's replacement boundary (CR 614.4;
		// Averna, the Chaos Bloom's `ValidPlayer$ You | ActiveZones$
		// Battlefield`). Only the synthetic proposal reaches here, so there
		// is no logged event and no ValidCard$: the caster is ev.Player and
		// ValidPlayer$ scopes it exactly as the Draw/ProduceMana classes do.
		if ev.Kind != events.Cascade {
			return false
		}
		if vp, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	case cards.ReplGameLoss, cards.ReplGameWin:
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
		if !strings.EqualFold(strings.TrimSpace(r.ParamStr(cards.PKLayer)), "CantHappen") {
			return false
		}
		if r.Event == "GameLoss" {
			if ev.Kind != events.PlayerLost {
				return false
			}
			if reason, ok := r.Param(cards.PKValidLoseReason); ok &&
				!strings.EqualFold(strings.TrimSpace(reason), ev.Text) {
				return false
			}
		} else if ev.Kind != events.GameOver {
			return false
		}
		if vp, ok := r.Param(cards.PKValidPlayer); ok &&
			!effects.MatchesPlayerSpec(e.G, vp, ev.Player, you) {
			return false
		}
		return e.replacementConditionHolds(r, source, you)
	}
	return r.EventKind() == cards.ReplLoseMana && loseManaReplacementApplies(ev, replacementPlayerMatches(e, source, &r, ev.Player), e.replacementConditionHolds(r, source, you))
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
	if !e.classBandGateHolds(r.ParamStr(cards.PKClassBand), source) {
		return false
	}
	if spec, ok := r.Param(cards.PKIsPresent); ok {
		cmp := r.ParamStr(cards.PKPresentCompare)
		if cmp == "" {
			cmp = "GE1"
		}
		var n int
		if _, hasZone := r.Param(cards.PKPresentZone); hasZone || r.ParamStr(cards.PKPresentDefined) != "" {
			// A damage/counter/CantPreventDamage line's IsPresent$ can name a
			// non-battlefield zone (PresentZone$) or a defined subject
			// (PresentDefined$ Self, "is this exact permanent still present");
			// countPresentInZone generalises past countPresent's fixed
			// battlefield scan for exactly those two params.
			zone := state.ZBattlefield
			if z := r.ParamStr(cards.PKPresentZone); z != "" {
				zone = effects.ParseZone(z)
			}
			n = e.countPresentInZone(spec, source, you, zone, r.ParamStr(cards.PKPresentDefined))
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
	if strings.EqualFold(r.ParamStr(cards.PKPlayerTurn), "True") && e.G.Active != you {
		return false
	}
	if strings.EqualFold(r.ParamStr(cards.PKHellbent), "True") && len(e.G.Zone(state.ZHand, you)) != 0 {
		return false
	}
	if strings.EqualFold(r.ParamStr(cards.PKRevolt), "True") && !e.revoltThisTurn(you) {
		return false
	}
	if strings.EqualFold(r.ParamStr(cards.PKDelirium), "True") && e.graveyardCardTypeCount(you) < 4 {
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
	if raw, ok := r.Param(cards.PKEnduringStory); ok {
		switch replacementConditionHoldsCodes.Code(string(strings.TrimSpace(raw))) {
		case replacementConditionHoldsTrue:
			if !e.playerHasEnduringStory(you) {
				return false
			}
		case replacementConditionHoldsFalse:
			if e.playerHasEnduringStory(you) {
				return false
			}
		default:
			return false
		}
	}
	if _, ok := r.Param(cards.PKCheckDefinedPlayer); ok {
		// The only corpus shape is You.isMonarch. Monarch state is not yet
		// represented, so fail closed instead of preventing damage always.
		return false
	}
	if check, ok := r.Param(cards.PKCheckSVar); ok {
		n := e.replacementCheckValue(source, check)
		if cmp := r.ParamStr(cards.PKSVarCompare); cmp != "" {
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

// replacementAmountMatches understands Forge's comparison shorthand such as
// LTX (Ojer Axonil). Its RHS is resolved in the replacement source's context.
func (e *Engine) replacementAmountMatches(spec string, amount int32, c *effects.Ctx) bool {
	if spec == "" {
		return true
	}
	for _, op := range []string{"GE", "GT", "LE", "LT", "EQ"} {
		if rhs, ok := strings.CutPrefix(spec, op); ok {
			v := effects.Num(e, c, &cards.SA{Params: map[string]string{"N": rhs}}, "N", 0)
			switch effects.CmpOpOf(op) {
			case effects.CmpGE:
				return amount >= v
			case effects.CmpGT:
				return amount > v
			case effects.CmpLE:
				return amount <= v
			case effects.CmpLT:
				return amount < v
			case effects.CmpEQ:
				return amount == v
			}
		}
	}
	return false
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
	switch replacementCheckBodyCodes.Code(string(body)) {
	case replacementCheckBodyCountParty:
		roles := map[string]bool{}
		for _, id := range e.G.Zone(state.ZBattlefield, o.Controller) {
			if f := e.G.Obj(id).Face(); f != nil {
				for _, typ := range f.Types {
					switch replacementCheckTypeCodes.Code(string(typ)) {
					case replacementCheckTypePartyType:
						roles[typ] = true
					}
				}
			}
		}
		return int32(len(roles))
	case replacementCheckBodyCountValidPermanentYouCtrlCo:
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
	case replacementCheckBodyCountPresenceDragon10:
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
				switch graveyardCardTypeCountCodes.Code(string(typ)) {
				case graveyardCardTypeCountCardType:
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
	switch replacementCauseKindCodes.Code(string(kind)) {
	case replacementCauseKindSpell:
		if o.Ability != nil {
			return false
		}
	case replacementCauseKindSpellAbility:
		// Both spell cards and minted ability objects qualify.
	case replacementCauseKindTriggered:
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
	switch replacementCauseQualCodes.Code(string(quals)) {
	case replacementCauseQualYouCtrl:
		return o.Controller == e.controllerOf(replacementSource)
	case replacementCauseQualOppCtrl:
		return o.Controller != e.controllerOf(replacementSource)
	case replacementCauseQualModular:
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

func isTriggered(g *state.Game, o *state.Object) bool {
	_, ok := state.TriggerOf(g, o)
	return ok
}

type replacementConditionHoldsCode uint16

const (
	replacementConditionHoldsTrue replacementConditionHoldsCode = iota + 1
	replacementConditionHoldsFalse
)

var replacementConditionHoldsCodes = state.NewStrCodes(
	state.StrEntry[replacementConditionHoldsCode]{Key: "True", Val: replacementConditionHoldsTrue},
	state.StrEntry[replacementConditionHoldsCode]{Key: "true", Val: replacementConditionHoldsTrue},
	state.StrEntry[replacementConditionHoldsCode]{Key: "False", Val: replacementConditionHoldsFalse},
	state.StrEntry[replacementConditionHoldsCode]{Key: "false", Val: replacementConditionHoldsFalse},
)

type replacementCheckBodyCode uint16

const (
	replacementCheckBodyCountParty replacementCheckBodyCode = iota + 1
	replacementCheckBodyCountValidPermanentYouCtrlCo
	replacementCheckBodyCountPresenceDragon10
)

var replacementCheckBodyCodes = state.NewStrCodes(
	state.StrEntry[replacementCheckBodyCode]{Key: "Count$Party", Val: replacementCheckBodyCountParty},
	state.StrEntry[replacementCheckBodyCode]{Key: "Count$Valid Permanent.YouCtrl$Colors", Val: replacementCheckBodyCountValidPermanentYouCtrlCo},
	state.StrEntry[replacementCheckBodyCode]{Key: "Count$Presence_Dragon.1.0", Val: replacementCheckBodyCountPresenceDragon10},
)

type replacementCheckTypeCode uint16

const (
	replacementCheckTypePartyType replacementCheckTypeCode = iota + 1
)

var replacementCheckTypeCodes = state.NewStrCodes(
	state.StrEntry[replacementCheckTypeCode]{Key: "Cleric", Val: replacementCheckTypePartyType},
	state.StrEntry[replacementCheckTypeCode]{Key: "Rogue", Val: replacementCheckTypePartyType},
	state.StrEntry[replacementCheckTypeCode]{Key: "Warrior", Val: replacementCheckTypePartyType},
	state.StrEntry[replacementCheckTypeCode]{Key: "Wizard", Val: replacementCheckTypePartyType},
)

type graveyardCardTypeCountCode uint16

const (
	graveyardCardTypeCountCardType graveyardCardTypeCountCode = iota + 1
)

var graveyardCardTypeCountCodes = state.NewStrCodes(
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Artifact", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Battle", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Creature", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Enchantment", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Instant", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Kindred", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Land", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Planeswalker", Val: graveyardCardTypeCountCardType},
	state.StrEntry[graveyardCardTypeCountCode]{Key: "Sorcery", Val: graveyardCardTypeCountCardType},
)

type replacementCauseKindCode uint16

const (
	replacementCauseKindSpell replacementCauseKindCode = iota + 1
	replacementCauseKindSpellAbility
	replacementCauseKindTriggered
)

var replacementCauseKindCodes = state.NewStrCodes(
	state.StrEntry[replacementCauseKindCode]{Key: "Spell", Val: replacementCauseKindSpell},
	state.StrEntry[replacementCauseKindCode]{Key: "SpellAbility", Val: replacementCauseKindSpellAbility},
	state.StrEntry[replacementCauseKindCode]{Key: "Triggered", Val: replacementCauseKindTriggered},
)

type replacementCauseQualCode uint16

const (
	replacementCauseQualYouCtrl replacementCauseQualCode = iota + 1
	replacementCauseQualOppCtrl
	replacementCauseQualModular
)

var replacementCauseQualCodes = state.NewStrCodes(
	state.StrEntry[replacementCauseQualCode]{Key: "YouCtrl", Val: replacementCauseQualYouCtrl},
	state.StrEntry[replacementCauseQualCode]{Key: "OppCtrl", Val: replacementCauseQualOppCtrl},
	state.StrEntry[replacementCauseQualCode]{Key: "YouDontCtrl", Val: replacementCauseQualOppCtrl},
	state.StrEntry[replacementCauseQualCode]{Key: "Modular", Val: replacementCauseQualModular},
)

// batchMemberReplActive reports whether source o, gated by an ActiveZones$
// spec that names the battlefield, is a member of the open simultaneous
// departure batch that the batch's own sequential emit loop has already
// moved off the battlefield. Such a source was on the battlefield
// immediately before the simultaneous event, so its replacement still
// applies to the batch's remaining members (CR 614.6, 616.1, 603.10). A
// token member has ceased to exist (o == nil) and cannot be reached; a
// member back on the battlefield is gated normally.
func (e *Engine) batchMemberReplActive(active string, o *state.Object) bool {
	if o == nil || len(e.batchReplSources) == 0 || o.Zone == state.ZBattlefield ||
		!zoneSpecContains(active, state.ZBattlefield) {
		return false
	}
	for _, id := range e.batchReplSources {
		if id == o.ID {
			return true
		}
	}
	return false
}

// beginReplBatch opens a simultaneous-departure batch for
// batchMemberReplActive; nested batches share the outermost list.
func (e *Engine) beginReplBatch() { e.replBatchDepth++ }

// endReplBatch closes one beginReplBatch; the outermost close forgets the
// batch's departures so none outlives the simultaneous event.
func (e *Engine) endReplBatch() {
	if e.replBatchDepth > 0 {
		e.replBatchDepth--
	}
	if e.replBatchDepth == 0 {
		e.batchReplSources = e.batchReplSources[:0]
	}
}

// noteBatchDeparture records a battlefield departure inside an open batch.
// Called from emit after the replacement pass settled and before the fold.
func (e *Engine) noteBatchDeparture(ev events.Event) {
	if e.replBatchDepth == 0 || ev.Kind != events.MoveZone ||
		ev.From != state.ZBattlefield || ev.To == state.ZBattlefield {
		return
	}
	if o := e.G.Obj(ev.Obj); o != nil && o.Zone == state.ZBattlefield {
		e.batchReplSources = append(e.batchReplSources, ev.Obj)
	}
}
