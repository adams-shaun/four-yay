// Zone-change trigger modes.
//
// Mode$ ChangesZone / ChangesZoneAll and the zone gate every mode consults,
// plus the Sacrificed and TokenCreated modes, which are zone events too.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// zoneChangeMatches implements Mode$ ChangesZone. The two keyword triggers
// this task expands through it are both handled here (cards/keywords.go):
// Undying's Origin$ Graveyard -> Destination$ Battlefield return is an
// ordinary ChangeZone, and its ValidCard$ Card.Self+counters_EQ0_P1P1 "no
// counters when it died" is read against the LKI; Evolve's Evolve$ True
// gating (the entering creature's derived power OR toughness must exceed the
// source's, CR 702.99a) is checked below once the rest of the spec matches.
func (e *Engine) zoneChangeMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	return e.zoneChangeMatchesWithCapture(t, source, ev, lki, nil)
}

func (e *Engine) zoneChangeMatchesWithCapture(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object, remembered []state.Target) bool {
	// Only a delayed registration passes a capture. Printed triggers see nil.
	capture := func(sc effects.SpecContext) effects.SpecContext {
		sc.DelayedRemembered = remembered
		return sc
	}
	if ev.Kind != events.MoveZone && ev.Kind != events.Draw && ev.Kind != events.PutOnStack &&
		ev.Kind != events.TokenCreate && ev.Kind != events.CardToken {
		return false
	}
	// A token is minted straight onto the battlefield by the TokenCreate/
	// CardToken fold (events/apply_copy.go's foldTokenCreate/foldCardToken
	// call Move inside Apply), so the mint event carries no move zones of its
	// own: From/To are the zero value (both ZLibrary). Model the mint as
	// Library -> Battlefield for the Origin$/ExcludedOrigins$/Destination$
	// reads below. This is what makes a token's entry fire an ETB trigger
	// (CR 110.5a/603.6a). Zero Origin$ Library | Destination$ Battlefield
	// lines exist in the corpus, so the override cannot over-match an explicit
	// Origin$ Library ETB carrier (re-measure before relying on that: if such
	// a shape appears, a mint must match only Origin$ Any). The minted object
	// id is set on the event copy emit hands the trigger walk, so ev.Obj and
	// the ValidCard$ block below already read the entering token.
	from, to := ev.From, ev.To
	if ev.Kind == events.TokenCreate || ev.Kind == events.CardToken {
		from, to = state.ZLibrary, state.ZBattlefield
	}
	// ResolvedOnly$ True: a trigger fires only on the spell's own RESOLUTION
	// move off the stack, not on any other stack exit. No corpus card prints
	// this parameter today (Cipher, its only former user, now runs its encode
	// as a resolution-tail instruction instead of a trigger; see
	// cards/kw_cipher.go and rules/cipher.go). It is kept as a generic
	// ChangesZone gate: the engine's resolution tail
	// (resolution.moveResolvedOffStack) is the ONE empty-Text stack exit;
	// every counter/fizzle/reversal path tags its move ("countered",
	// "fizzled: ...", "reversed"), so the empty Text is the resolution. This
	// keeps the gate out of the event shape itself -- no existing move changes
	// its Text, so the hash chain is untouched.
	if strings.EqualFold(strings.TrimSpace(t.Params["ResolvedOnly"]), "True") && ev.Text != "" {
		return false
	}
	// Origin$ is a zone SET: either a single zone name or a comma list
	// (Syr Konrad, the Grim's "a creature card is put into a graveyard from
	// anywhere other than the battlefield" is
	// Hand,Graveyard,Exile,Stack,Library,Command; Laelia, the Blade
	// Reforged's ChangesZoneAll carrier is Library,Graveyard). ParseZones is
	// the set reader -- Any/All are wildcards, and an unknown token fails
	// closed rather than degrading to a graveyard origin.
	if o, ok := t.Param(cards.PKOrigin); ok {
		zones, all, listOK := effects.ParseZones(o)
		if !listOK || (!all && !zoneIn(from, zones)) {
			return false
		}
	}
	// ExcludedOrigins$ ("Name Sticker" Goblin's "enters from anywhere other
	// than a graveyard or exile"): a comma-separated list of zones the move
	// must NOT originate in. Absent means unrestricted, exactly as before.
	if excl, ok := t.Params["ExcludedOrigins"]; ok {
		for z := range strings.SplitSeq(excl, ",") {
			if zz := strings.TrimSpace(z); zz != "" && effects.ParseZone(zz) == from {
				return false
			}
		}
	}
	if d, ok := t.Param(cards.PKDestination); ok && d != "Any" && effects.ParseZone(d) != to {
		return false
	}
	// ValidCards$ is the PLURAL key the ChangesZoneAll corpus uses (124 of
	// its 126 lines); ValidCard$ is the singular key ChangesZone uses. One
	// matcher serves both modes, so read the plural first and fall back.
	v, hasSpec := t.Param(cards.PKValidCards)
	if !hasSpec {
		v, hasSpec = t.Param(cards.PKValidCard)
	}
	if hasSpec {
		// The trigger's own source moving (source == ev.Obj) with an LKI
		// snapshot available is a dying card asserting a property about
		// itself, e.g. Undying's counters_EQ0_P1P1: read it against the LKI
		// (what it was the moment before the move reset it), not the live
		// object already in the destination zone.
		// A permanent that LEFT the battlefield is likewise matched as it
		// last existed there (CR 603.10a) -- in particular its controller:
		// "a creature you control dies" must see a stolen creature as the
		// taker's, though the move has already handed it back to its owner.
		// ctrl is the trigger source's controller at that moment too, which
		// for the departed source itself is its LKI controller -- UNLESS the
		// source is a recurring-Effect registration, whose virtual controller
		// is the registration's owner and outranks the creating card's LKI
		// controller (the registration is the presence, not the card; the
		// card's own last-known controller is meaningless for it). Without
		// this guard the LKI overwrite below would clobber the overlay that
		// controllerOf just applied, so a ChangesZone predicate reading the
		// source's controller ("creature you control dies") would match the
		// creating card's controller instead of the Effect owner's.
		ctrl := e.controllerOf(source)
		if _, overlaid := e.effectMatchControllerFor(source); !overlaid &&
			source == ev.Obj && lki != nil && leftBattlefield(ev) {
			ctrl = lki.Controller
		}
		// A departure from EXILE is the third LKI case: the ordinary imprint
		// association's liveness rule is exile-only (CR 607.2a, the IsImprinted
		// predicate), so the departure triggers that read it — Knowledge Pool,
		// Mimic Vat and the other Origin$ Exile carriers — fire on a card
		// LEAVING exile and must judge it as it existed there (CR 603.10's
		// circumstances of the event). The live object is already in the
		// destination zone, where the association is dead, so a live read can
		// never match: the moving card's LKI snapshot (captured in emit before
		// Apply folded the move, its Zone still ev.From) is what the predicate
		// must see. Ordinary live filtering and Defined$ Imprinted keep their
		// live-zone readers (effects.imprintAssociationContains) — this gate
		// widens only the trigger matcher's candidate choice.
		// The bare wasCastFromYourHandByYou qualifier (the "if you cast it
		// from your hand" ETB family) is split out and evaluated against the
		// log here, where the Engine is in scope; the remainder matches as
		// before (task castprov1).
		if ev.Obj != 0 && lki != nil &&
			(source == ev.Obj || leftBattlefield(ev) ||
				(ev.From == state.ZExile && ev.To != state.ZBattlefield)) {
			spec, ok := e.castProvenanceAdmits(v, lki.ID, ctrl)
			// The IsGoaded static route (staticgoad1), bound inline the same
			// shape matchesSpec keeps (this LKI reader runs per zone-change
			// event, so the context must not escape through a helper call).
			sc := capture(e.specCtx(source, ctrl))
			if e.goadProbe == 0 && strings.Contains(spec, "IsGoaded") {
				sc.Layers.StaticGoads = e.staticallyGoadedWithLKI(lki)
			}
			if !ok || !effects.MatchesObjectCtx(e.G, spec, lki, sc) {
				return false
			}
		} else {
			spec, ok := e.castProvenanceAdmits(v, ev.Obj, e.controllerOf(source))
			if !ok || !e.matchesSpec(spec, ev.Obj, capture(e.specCtx(source, e.controllerOf(source)))) {
				return false
			}
		}
	}
	// Evolve$ True (CR 702.99a): the trigger fires only when the entering
	// creature's derived power OR toughness exceeds the source's, so an
	// equal-or-smaller creature entering does not evolve the source. The
	// ordinary ChangesZone path above (Destination$ Battlefield in the
	// expansion) has already narrowed ev.To, so the extra battlefield guard
	// is belt-and-braces.
	if _, hasEvolve := t.Param(cards.PKEvolve); hasEvolve {
		if ev.To != state.ZBattlefield {
			return false
		}
		if e.Power(ev.Obj) <= e.Power(source) && e.Toughness(ev.Obj) <= e.Toughness(source) {
			return false
		}
	}
	return true
}

// zoneGate implements TriggerZones$: a trigger only fires while its source is
// in one of the listed zones. The default is the battlefield, which is why an
// enchantment's upkeep trigger stops when it is destroyed.
//
// checkTriggers calls this after the event has already been folded into
// state (emit logs before it checks triggers), so o.Zone alone only ever
// reflects the zone the object is in *now*. That is correct for an
// entering-the-zone trigger (Snapcaster's ETB: o.Zone is already
// Battlefield by the time this runs) but wrong for a leaving-the-zone one --
// a plain "dies" trigger (Origin$ Battlefield, Destination$ Graveyard,
// ValidCard$ Card.Self, default TriggerZones$ Battlefield) would never see
// its own source "in" the battlefield, because by the time checkTriggers
// runs the move has already happened and o.Zone reads Graveyard. CR 603.10's
// full "look back in time" is not modeled, but the one case Task 20's own
// ChangesZone mode needs it for is narrow and self-contained: when the event
// under test is itself the zone change of this trigger's own source (source
// == ev.Obj), the zone it was in immediately before (ev.From) counts as well
// as the zone it is in now, so both an ETB and a dies trigger with the
// ordinary default work from the same rule.
func (e *Engine) zoneGate(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	spec := t.ParamStr(cards.PKTriggerZones)
	if spec == "" {
		// Forge's ActiveZones$ is the trigger-side spelling of the same gate
		// (the replacement side already reads the key:
		// rules/replacement.go's zone gate). Sower of Discord's two
		// DamageDoneOnce halves declare ActiveZones$ Battlefield; an explicit
		// ActiveZones$ is authoritative exactly like an explicit
		// TriggerZones$, so the two special cases below keep treating it as
		// declared.
		spec = t.ParamStr(cards.PKActiveZones)
	}
	if spec == "" && ev.Kind == events.PutOnStack && source == ev.Obj && t.Mode == "SpellCast" {
		// CR 601.2i: the spell's OWN cast trigger fires while the source is
		// the spell sitting on the stack -- exactly the event being walked.
		// The battlefield default would gate it out (the source is in ZStack,
		// and the PutOnStack look-back zone below is the zone it came FROM,
		// the hand), so every bare "When you cast this spell" script --
		// Hydroid Krasis, Genesis Hydra, Ulamog, World Breaker -- would
		// never fire at all. An EXPLICIT TriggerZones$ stays authoritative:
		// a script naming one knows where its trigger lives.
		return true
	}
	if spec == "" {
		// "When you discard this card" (Orvar, Bartered Cow, Titanbones: 14
		// of the corpus's Mode$ Discarded lines) declares no TriggerZones$,
		// and the only zone a card is discarded from is its owner's hand (CR
		// 701.9a). Forge applies no zone restriction to a trigger without
		// TriggerZones$; the battlefield default below would leave such a
		// trigger unable to fire at all, so the card's own discard admits it
		// wherever the discard (or a replacement redirecting it) put it.
		if t.Mode == "Discarded" && source == ev.Obj && events.IsDiscard(ev) {
			return true
		}
		// The cycled card itself is the moved card (ValidCard$ Card.Self):
		// its own cycle-trigger must fire from wherever the cost discard (or a
		// replacement redirecting it) put it, the same courtesy the Discarded
		// case above extends.
		if t.Mode == "Cycled" && source == ev.Obj && events.IsDiscard(ev) {
			return true
		}
		// A card's own hand->exile move (Lupine Harbingers' "note the number
		// of turns you've begun since it was foretold" exile trigger -- the
		// corpus's ONE ChangesZone self-trigger with no TriggerZones$ whose
		// destination is not the battlefield, measured over the corpus pin):
		// the only zone the trigger can observe the move from is the hand the
		// card sits in. Forge applies no zone restriction to a trigger
		// without TriggerZones$; the battlefield default would leave such a
		// trigger unable to fire at all, so the card's own hand-origin move
		// admits it, the same courtesy the Discarded and Cycled cases above
		// extend. (Self-moves whose DESTINATION is the battlefield need no
		// admission: the post-move zone check below already sees them.)
		if t.Mode == "ChangesZone" && source == ev.Obj && ev.Kind == events.MoveZone &&
			ev.From == state.ZHand {
			return true
		}
		spec = "Battlefield"
	}
	zones := [2]state.Zone{o.Zone, o.Zone}
	n := 1
	if source == ev.Obj && ev.Obj != 0 &&
		(ev.Kind == events.MoveZone || ev.Kind == events.Draw || ev.Kind == events.PutOnStack) {
		zones[1] = ev.From
		n = 2
	}
	for _, zone := range zones[:n] {
		if zoneSpecContains(spec, zone) {
			return true
		}
	}
	return false
}

// zoneSpecContains reports whether spec (a Forge TriggerZones value -- a
// comma-separated list of zone names, constant for the life of the card)
// lists want. It scans the string by slicing comma-separated parts apart with
// strings.Cut, which shares the backing string and allocates nothing, instead
// of strings.Split (whose []string is a fresh allocation per call). zoneGate
// runs from the per-event trigger walk -- the same hot path Task A2 fixed the
// zone copy in -- so this avoids churning an allocation for every trigger on
// every event. Parts are handed to effects.ParseZone unchanged (it trims each
// name itself), so the result is byte-identical to the old
// strings.Split+TrimSpace+ParseZone loop, including its handling of empty
// leading/trailing/double-separator segments: an empty zone name parses to
// the graveyard, exactly as it always did.
func zoneSpecContains(spec string, want state.Zone) bool {
	// Walk separator-delimited segments by index so that, like strings.Split,
	// a spec ending in a separator still yields a final empty segment (which
	// effects.ParseZone resolves to the graveyard). strings.Cut would drop
	// that trailing Phantom Graveyard part and change behaviour on a malformed
	// spec; slicing keeps every segment while allocating nothing.
	i := 0
	for {
		j := strings.IndexByte(spec[i:], ',')
		var part string
		if j < 0 {
			part = spec[i:]
		} else {
			part = spec[i : i+j]
		}
		if effects.ParseZone(part) == want {
			return true
		}
		if j < 0 {
			return false
		}
		i += j + 1
	}
}

// zoneDelayedDestinationAdmits is the delayed-registration Destination$
// reader: a comma-separated zone list admits a move into any listed zone
// (Earthbend's "when it dies or is exiled" promise names Graveyard,Exile in
// one registration). It is deliberately separate from zoneChangeMatches,
// which reads Destination$ through the single-word effects.ParseZone: this
// only ever runs when the delayed-trigger arm sees a comma in the clause, so
// a face trigger -- and every single-zone delayed registration -- keeps the
// existing single-word reading unchanged (the engine-wide comma-Destination$
// defect is ledgered separately and is not fixed here).
func zoneDelayedDestinationAdmits(spec string, to state.Zone) bool {
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "Any" {
			continue
		}
		if z, ok := effects.ParseZoneWord(part); ok && z == to {
			return true
		}
	}
	return false
}

// leftBattlefield reports a zone change whose object left the battlefield.
func leftBattlefield(ev events.Event) bool {
	return ev.Kind == events.MoveZone && ev.From == state.ZBattlefield && ev.To != state.ZBattlefield
}

// sacrificedMatches and discardedMatches identify the two actions from the
// existing, replayed zone-change event. Discard producers use events.Discard
// or events.DiscardCost, so the action marker and its cost provenance survive
// a replacement changing the destination.
func (e *Engine) sacrificedMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if !events.IsSacrifice(ev) {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" {
		// A sacrificed permanent is already in its destination zone when
		// triggers are checked. Its validity -- especially bare Permanent --
		// is a last-known-information question at the moment it was sacrificed.
		// The IsGoaded static route (staticgoad1) is bound inline, the same
		// shape matchesSpec keeps.
		sc := e.specCtx(source, ctrl)
		if e.goadProbe == 0 && strings.Contains(v, "IsGoaded") {
			sc.Layers.StaticGoads = e.staticallyGoadedWithLKI(lki)
		}
		if lki == nil || !effects.MatchesObjectCtx(e.G, v, lki, sc) {
			return false
		}
	}
	// The sacrificing player is the permanent's controller as it was
	// sacrificed (Forge GameAction.sacrifice reads the LKI): a stolen
	// permanent its taker sacrifices is the taker's sacrifice, although the
	// move has already returned it to its owner.
	sacrificer := e.controllerOf(ev.Obj)
	if lki != nil {
		sacrificer = lki.Controller
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.G, v, sacrificer, ctrl) {
		return false
	}
	return true
}

// tokenCreatedMatches identifies Forge's "whenever you create a token"
// trigger (Mode$ TokenCreated / TokenCreatedOnce) on the existing, replayed
// TokenCreate event -- one event per minted token (effects/token.go's effToken
// loop, effects/amass.go, and the token-replacement mint path), so a
// three-token spell fires the trigger three times and the Once mode's
// once-per-turn latch (triggerActivationLimitAllows, actionTriggerModes
// membership above) is what collapses a batch to one queueing. The would-be
// token does not exist as a game object at match time: ValidToken$ is taken
// against the same shallow tokenSnapshot the CreateToken replacement class
// matches against -- an unknown token key fails closed -- with the You-side
// predicates reading against the trigger source's controller while the
// snapshot's controller is ev.Player, the token's creator ("you create").
func (e *Engine) tokenCreatedMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.TokenCreate {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if v, ok := t.Params["ValidToken"]; ok && v != "" {
		tok := e.tokenSnapshot(ev)
		if tok == nil || !effects.MatchesObjectCtx(e.G, v, tok, e.specCtx(source, ctrl)) {
			return false
		}
	}
	return true
}

func init() {
	// ChangesZoneAll shares the per-object matcher; the action brackets (RepeatEach ChangeZoneTable, api:Phases, api:Mill, api:Dig) collapse one action's moves to one queueing.
	registerTrigMatcher((*Engine).zoneChangeMatches, "ChangesZone", "ChangesZoneAll")
	registerTrigMatcher((*Engine).sacrificedMatches, "Sacrificed")
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.tokenCreatedMatches(t, source, ev)
	}, "TokenCreated", "TokenCreatedOnce")
}
