// Zone-change trigger modes.
//
// Mode$ ChangesZone / ChangesZoneAll and the zone gate every mode consults,
// plus the Sacrificed and TokenCreated modes, which are zone events too.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package trigmatch

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// ZoneChangeMatches implements Mode$ ChangesZone. The two keyword triggers
// this task expands through it are both handled here (cards/keywords.go):
// Undying's Origin$ Graveyard -> Destination$ Battlefield return is an
// ordinary ChangeZone, and its ValidCard$ Card.Self+counters_EQ0_P1P1 "no
// counters when it died" is read against the LKI; Evolve's Evolve$ True
// gating (the entering creature's derived power OR toughness must exceed the
// source's, CR 702.99a) is checked below once the rest of the spec matches.
func ZoneChangeMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	return ZoneChangeMatchesWithCapture(e, t, source, ev, lki, nil)
}

func ZoneChangeMatchesWithCapture(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object, remembered []state.Target) bool {
	// Only a delayed registration passes a capture. Printed triggers see nil.
	capture := SpecOpts{DelayedRemembered: remembered}
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
		if !listOK || (!all && !slices.Contains(zones, from)) {
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
		ctrl := e.ControllerOf(source)
		if _, overlaid := e.EffectMatchControllerFor(source); !overlaid &&
			source == ev.Obj && lki != nil && LeftBattlefield(ev) {
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
			(source == ev.Obj || LeftBattlefield(ev) ||
				(ev.From == state.ZExile && ev.To != state.ZBattlefield)) {
			spec, ok := e.CastProvenanceAdmits(v, lki.ID, ctrl)
			// The IsGoaded static route (staticgoad1), passed as an override
			// the same shape matchesSpec keeps (this LKI reader runs per
			// zone-change event, so the filter context is built inside
			// Board.MatchesObject and never escapes).
			opts := capture
			if e.Facts().GoadProbe == 0 && strings.Contains(spec, "IsGoaded") {
				opts.StaticGoads = e.StaticallyGoadedWithLKI(lki)
			}
			if !ok || !e.MatchesObject(spec, lki, source, ctrl, opts) {
				return false
			}
		} else {
			spec, ok := e.CastProvenanceAdmits(v, ev.Obj, e.ControllerOf(source))
			if !ok || !e.MatchesSpec(spec, ev.Obj, source, e.ControllerOf(source), capture) {
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

// LeftBattlefield reports a zone change whose object left the battlefield.
func LeftBattlefield(ev events.Event) bool {
	return ev.Kind == events.MoveZone && ev.From == state.ZBattlefield && ev.To != state.ZBattlefield
}

// sacrificedMatches and discardedMatches identify the two actions from the
// existing, replayed zone-change event. Discard producers use events.Discard
// or events.DiscardCost, so the action marker and its cost provenance survive
// a replacement changing the destination.
func sacrificedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if !events.IsSacrifice(ev) {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" {
		// A sacrificed permanent is already in its destination zone when
		// triggers are checked. Its validity -- especially bare Permanent --
		// is a last-known-information question at the moment it was sacrificed.
		// The IsGoaded static route (staticgoad1) is passed as an override,
		// the same shape matchesSpec keeps.
		var opts SpecOpts
		if e.Facts().GoadProbe == 0 && strings.Contains(v, "IsGoaded") {
			opts.StaticGoads = e.StaticallyGoadedWithLKI(lki)
		}
		if lki == nil || !e.MatchesObject(v, lki, source, ctrl, opts) {
			return false
		}
	}
	// The sacrificing player is the permanent's controller as it was
	// sacrificed (Forge GameAction.sacrifice reads the LKI): a stolen
	// permanent its taker sacrifices is the taker's sacrifice, although the
	// move has already returned it to its owner.
	sacrificer := e.ControllerOf(ev.Obj)
	if lki != nil {
		sacrificer = lki.Controller
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, sacrificer, ctrl) {
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
func tokenCreatedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.TokenCreate {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, ev.Player, ctrl) {
		return false
	}
	if v, ok := t.Params["ValidToken"]; ok && v != "" {
		tok := e.TokenSnapshot(ev)
		if tok == nil || !e.MatchesObject(v, tok, source, ctrl, SpecOpts{}) {
			return false
		}
	}
	return true
}

func init() {
	// ChangesZoneAll shares the per-object matcher; the action brackets (RepeatEach ChangeZoneTable, api:Phases, api:Mill, api:Dig) collapse one action's moves to one queueing.
	registerTrigMatcher(ZoneChangeMatches, "ChangesZone", "ChangesZoneAll")
	registerTrigMatcher(sacrificedMatches, "Sacrificed")
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return tokenCreatedMatches(e, t, source, ev)
	}, "TokenCreated", "TokenCreatedOnce")
}
