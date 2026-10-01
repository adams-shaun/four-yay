package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// This file is the PLAYER half of the derived-keyword surface (task
// approx-player-shroud). Until now players had no keyword machinery at all:
// a continuous static granting a keyword to a seat — True Believer's and
// Ivory Mask's `Affected$ You | AddKeyword$ Shroud`, Leyline of Sanctity's
// hexproof — emitted its ContinuousEffect like every other static (the
// layer walk in rules/layers.go) but no target walk ever read it, because
// `Affected$ You` never matches an OBJECT (matchesPlayerSingleSpec fails
// closed on an object base) and the targeting gates below were keyed on
// state.ObjID. A player so granted stayed targetable by everything.
//
// The surface is DERIVED, never stored: playerKeywords walks the same cached
// continuous-effect list every permanent's Derived() reads (e.active(), the
// one layer walk), so replay, determinism and the memo-epoch discipline are
// inherited rather than re-implemented, and a grant expires with its source
// exactly as an object's grant does. Only AddKeyword$ grants with a PLAYER
// spec are consulted (Shroud CR 702.18 and Hexproof CR 702.11 read them;
// protection CR 702.16 for players is a follow-up — see the ticket report).
// The caller owns every zone/interaction gate, exactly as shroudBlocksTarget
// and hexproofBlocksTarget's callers do for objects.

// playerKeywords returns the keywords every live continuous effect grants to
// player p through an `Affected$ <player spec> | AddKeyword$ ...` static.
// Each grant's Affects spec is judged by the shared player filter
// (effects.MatchesPlayerSpecFrom) from the STATIC'S CONTROLLER's perspective
// (the "You" of "you have shroud" is the granting card's controller), against
// the static's own source for the Chosen/IsRemembered predicates. An object-
// spec alternative (`Creature.YouCtrl`, the Card.Self default) matches no
// player — the player grammar fails closed on an object base — so the grant
// simply never reaches this surface, the same route it took before this file
// existed. The walk is e.active()'s deterministic order (sorted effects,
// fixed seat and zone slices), never a map, so the returned list is stable
// run to run and replays byte-identically.
func (e *Engine) playerKeywords(p state.PlayerID) []string {
	var out []string
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if len(ce.AddKeywords) == 0 {
			continue
		}
		if !effects.MatchesPlayerSpecFrom(e.G, ce.Affects, p, ce.Controller, ce.Source) {
			continue
		}
		out = append(out, ce.AddKeywords...)
	}
	return out
}

// playerForetellsAnyTurn reports whether player p carries the foretell
// TIMING grant (Cosmos Charger's `Affected$ You | AddKeyword$ Foretell on
// any player's turn`): while it holds, the CR 702.126a foretell special
// action's "during your turn" window is widened to any player's turn for
// every hand card p could foretell. The grant is a PLAYER keyword (the
// layer walk grants it to no object — `Affected$ You` never matches an
// object — exactly like the shroud/hexproof grants above), so the read is
// this surface, not the object Derived() walk. The entry is matched by its
// text: the head begins with Foretell and the value carries the any-turn
// modifier — the exact spelling both corpus carriers (Cosmos Charger and
// its rebalanced twin) write. Any other spelling of a timing rider is a
// keyword this read does not recognise and grants nothing (the
// fail-closed convention).
func (e *Engine) playerForetellsAnyTurn(p state.PlayerID) bool {
	for _, kw := range e.playerKeywords(p) {
		low := strings.ToLower(kw)
		if strings.HasPrefix(low, "foretell") && strings.Contains(low, "any player's turn") {
			return true
		}
	}
	return false
}

// playerShroudBlocksTarget reports whether player p carries shroud
// (CR 702.18): a granted `Affected$ You | AddKeyword$ Shroud` static
// (True Believer, Ivory Mask) makes p an illegal target for ANY spell or
// ability, p's own controller's included — shroud is symmetric, unlike
// hexproof. Players have no zone: the permanent's CR 604.3 battlefield gate
// has no player analogue (a player is always "in play"), so no zone gate is
// consulted and none is owed.
func (e *Engine) playerShroudBlocksTarget(p state.PlayerID) bool {
	for _, kw := range e.playerKeywords(p) {
		if strings.EqualFold(cards.KeywordHead(kw), "Shroud") {
			return true
		}
	}
	return false
}

// playerHexproofBlocksTarget reports whether player p carries hexproof
// (CR 702.11) that withholds p from a spell or ability controlled by
// targeting. It mirrors hexproofBlocksTarget for players: hexproof is
// ASYMMETRIC (CR 702.11b), so p's own controller may still target p, and a
// quality-bearing grant ("hexproof from black", Witchbane Orb; CR 702.11c)
// withholds only against a source carrying that quality, reusing protection's
// sourceHasQuality. A quality this build cannot evaluate fails closed and
// never withholds. source is the targeting spell/ability's protection source
// (the same e.protectionSource resolution the object gate uses); a zero
// source still withholds a plain hexproof — only the parameterised quality
// reads it.
func (e *Engine) playerHexproofBlocksTarget(p, targeting state.PlayerID, source state.ObjID) bool {
	if p == targeting {
		return false
	}
	for _, kw := range e.playerKeywords(p) {
		q, ok := hexproofQuality(kw)
		if !ok {
			continue
		}
		if q == "" || e.sourceHasQuality(source, q) {
			return true
		}
	}
	return false
}

// playerProtectedFrom reports whether player p carries protection from the
// targeting/affecting source's quality (CR 702.16c): Absolute Virtue's "You
// have protection from each of your opponents" and Gor Muldrak's "You and
// permanents you control have protection from Salamanders" are `Affected$
// You` statics whose grant reaches this surface through playerKeywords, but
// only the permanent arm of protectedFrom ever read it, so the player half
// stayed targetable. It mirrors playerHexproofBlocksTarget for players, with
// protection's asymmetry taken from CR 702.16c's quality test rather than a
// controller comparison: a plain protection grant with no quality is not a
// thing (protectionQuality yields ok=false), so every supported quality
// reads the source.
//
// The quality is judged in two steps, in this order so an unresolvable
// player spec never widens a quality the object grammar already answers:
//   - the shared source-quality evaluator (sourceHasQuality) covers the
//     colour/type words and every parameterised object spec the permanent
//     arm already evaluates (Gor Muldrak's `Salamander` is a bare type word
//     that resolves through the object filter);
//   - Forge's PLAYER-relative family (`Player.Opponent`, Absolute Virtue)
//     is not an object quality at all: the SOURCE'S CONTROLLER is judged as
//     a seat against the spec through the same shared player filter every
//     other Opponent spec resolves through, so `Player.Opponent` means "the
//     incoming source is controlled by an opponent of the protected player".
//
// A quality neither path resolves -- the Chosen-bound `ChosenType` (Serra's
// Emissary) and `ChosenName` (Runed Halo), whose value lives on the GRANTING
// static's own source object and is not carried by the flat keyword string --
// fails closed and never withholds, exactly as hexproofQuality's unresolvable
// arm does. source == 0 (no live source object) returns false for the same
// reason: no supported quality can be evaluated without it.
func (e *Engine) playerProtectedFrom(p state.PlayerID, source state.ObjID) bool {
	if source == 0 {
		return false
	}
	for _, kw := range e.playerKeywords(p) {
		q, ok := protectionQuality(kw)
		if !ok {
			continue
		}
		if e.sourceHasQuality(source, q) {
			return true
		}
		// Player-relative qualities (Player.Opponent): judge the SOURCE'S
		// CONTROLLER as the seat the spec names, with the protected player p
		// as the "you" the Opponent relation is measured against. Only a live
		// source object is judged -- e.controllerOf on a dead id degrades to
		// seat 0, which would falsely name a controller, so a missing object
		// skips this arm.
		if e.G.Obj(source) != nil &&
			effects.MatchesPlayerSpecFrom(e.G, q, e.controllerOf(source), p, 0) {
			return true
		}
	}
	return false
}
