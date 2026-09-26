package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// This file is the ONE home for Forge's target-SET property constraints:
//
//   - TargetsWithSameCardType$ True      (share a card type)
//   - TargetsWithSameCreatureType$ True  (share a creature type)
//   - TargetsWithEqualToughness$ True    (equal toughness)
//   - TargetsWithDifferentCMC$ True      (pairwise different mana values)
//   - TargetsWithDifferentNames$ True    (pairwise different card names)
//
// Each is expressed as a decision.SetPropMode over each candidate's
// SetProps tokens, so the offer, Decision.Validate, the resolution recheck
// (legalTargets) and botpolicy's Clamp all read ONE definition. The
// per-candidate controller-relative predicate (TargetsWithControllerProperty$)
// is a plain legality filter, not a set constraint, and lives beside the
// census it narrows (targetControllerPropertyAdmits, below).

// targetSetPropMode reports which set-property constraint this targeting SA
// carries, or SetPropNone. Forge writes at most one such key per SA; the
// corpus order checked (card type, creature type, toughness) then (mana
// value, name) is fixed so the choice is deterministic if a future SA ever
// carried two.
func targetSetPropMode(sa *cards.SA) (decision.SetPropMode, string) {
	if sa == nil {
		return decision.SetPropNone, ""
	}
	switch {
	case strings.EqualFold(sa.Params["TargetsWithSameCardType"], "True"):
		return decision.SetPropShared, "cardtype"
	case strings.EqualFold(sa.Params["TargetsWithSameCreatureType"], "True"):
		return decision.SetPropShared, "creaturetype"
	case strings.EqualFold(sa.Params["TargetsWithEqualToughness"], "True"):
		return decision.SetPropShared, "toughness"
	case strings.EqualFold(sa.Params["TargetsWithDifferentCMC"], "True"):
		return decision.SetPropDistinct, "cmc"
	case strings.EqualFold(sa.Params["TargetsWithDifferentNames"], "True"):
		return decision.SetPropDistinct, "name"
	}
	return decision.SetPropNone, ""
}

// setPropTokens builds the token set for one candidate under a resolved
// (mode, kind) pair. kind is the token family from targetSetPropMode.
func (e *Engine) setPropTokens(kind string, obj state.ObjID) []string {
	if obj == 0 {
		return nil
	}
	o := e.G.Obj(obj)
	if o == nil || o.Face() == nil {
		return nil
	}
	switch kind {
	case "cardtype":
		return cardTypeTokens(e.Derived(obj).Types)
	case "creaturetype":
		return creatureTypeTokens(e.Derived(obj).Types)
	case "toughness":
		return []string{strconv.Itoa(int(e.Toughness(obj)))}
	case "cmc":
		return []string{strconv.Itoa(int(o.Face().Cmc()))}
	case "name":
		return []string{strings.ToLower(strings.TrimSpace(o.Face().Name))}
	}
	return nil
}

// setPropTokensFor is setPropTokens over a candidate, returning nil for a
// player option (these constraints never name a seat).
func (e *Engine) setPropTokensFor(kind string, c targetCandidate) []string {
	if c.kind == "player" {
		return nil
	}
	return e.setPropTokens(kind, c.obj)
}

// cardTypeTokens filters a derived type list to the CR card types ("share a
// permanent type" -- Artifact, battle, creature, enchantment, kindred, land,
// planeswalker, and the legacy/miscellaneous card types). Supertypes and
// subtypes are excluded so two permanents sharing only, say, the subtype
// "Equipment" do not match a card-type check.
func cardTypeTokens(types []string) []string {
	var out []string
	for _, t := range types {
		if isCardType(t) {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}

// creatureTypeTokens filters a derived type list to creature SUBTYPES: every
// word that is neither a card type nor a supertype. For a creature's type line
// ("Creature — Elf Warrior") that is exactly its creature types; a noncreature
// subtype (Aura, Equipment) is included but only matters when the card also
// carries "Creature", which the specs already require.
func creatureTypeTokens(types []string) []string {
	var out []string
	for _, t := range types {
		if !isCreatureSubtype(t) {
			continue
		}
		out = append(out, strings.ToLower(t))
	}
	return out
}

// setPropTargetBounds applies a target-SET property constraint to an ask's
// bounds, mirroring sameControllerTargetBounds: it caps Max at the capacity
// (the largest number of candidates that can be chosen together) and returns
// that capacity so a mandatory Min above it can fizzle instead of posing an
// unsatisfiable decision. It returns the mode and the per-candidate token kind
// so the caller can populate Option.SetProps with the same definition.
func (e *Engine) setPropTargetBounds(sa *cards.SA, candidates []targetCandidate, min, max int) (int, int, int, decision.SetPropMode, string) {
	mode, kind := targetSetPropMode(sa)
	if mode == decision.SetPropNone {
		return min, max, 0, decision.SetPropNone, ""
	}
	sets := make([][]string, 0, len(candidates))
	for _, c := range candidates {
		sets = append(sets, e.setPropTokensFor(kind, c))
	}
	capacity := decision.SetPropCapacity(mode, sets)
	if capacity > 0 && max > capacity {
		max = capacity
	}
	return min, max, capacity, mode, kind
}

// narrowSetProps is CR 608.2b's "does as much as possible" read of a target-set
// property constraint at resolution: walk the still-legal targets in recorded
// order and keep a maximal subset that satisfies the constraint (the running
// intersection stays non-empty for a shared constraint; each kept target's
// tokens are disjoint from the earlier kept ones for a distinct constraint).
// It only ever REMOVES a target, exactly like narrowSameController, so a
// mandatory ask that has lost its shared property or its distinctness fizzles
// through the ordinary count check rather than acting on an illegal set.
func (e *Engine) narrowSetProps(sa *cards.SA, targets []state.Target) []state.Target {
	mode, kind := targetSetPropMode(sa)
	if mode == decision.SetPropNone || len(targets) < 2 {
		return targets
	}
	var acc []string
	out := targets[:0:0]
	for _, t := range targets {
		c := stateTargetCandidate(t)
		if c.kind == "player" {
			out = append(out, t)
			continue
		}
		props := e.setPropTokens(kind, c.obj)
		if !decision.SetPropAdmits(mode, acc, props) {
			continue
		}
		acc = decision.SetPropMerge(mode, acc, props)
		out = append(out, t)
	}
	return out
}

// targetControllerPropertyAdmits applies Forge's TargetsWithControllerProperty$
// per-candidate predicate (Drown in the Loch, Neural Network): the candidate's
// own mana value (cmc) or power (power) must be less than or equal to the
// number of cards in ITS CONTROLLER's graveyard. It is a pure legality filter,
// not a set constraint, so it is enforced at the census (candidatesFor) and at
// the resolution recheck (legalTargets) through this ONE helper. An unknown
// property string or a seat-less candidate fails closed (false), matching the
// every-other-spec convention.
func (e *Engine) targetControllerPropertyAdmits(kind string, obj state.ObjID) bool {
	switch strings.TrimSpace(kind) {
	case "cmcLECardsInGraveyard", "powerLECardsInGraveyard":
	default:
		return false
	}
	o := e.G.Obj(obj)
	if o == nil {
		return false
	}
	seat := o.Controller
	n := len(e.G.Zone(state.ZGraveyard, seat))
	if strings.HasPrefix(kind, "cmc") {
		if o.Face() == nil {
			return false
		}
		return int(o.Face().Cmc()) <= n
	}
	return int(e.Power(obj)) <= n
}

// filterTargetControllerProperty drops the candidates TargetsWithControllerProperty$
// refuses. Like filterTargetsWithDefinedController it is a pure post-filter on
// the census: it can only remove candidates, and the resolution recheck
// (legalTargets) applies the SAME predicate, so offer and recheck cannot
// disagree. An unknown property string fails closed (every candidate is
// dropped), which is the same direction every unread spec qualifier takes.
func (e *Engine) filterTargetControllerProperty(in []targetCandidate, sa *cards.SA) []targetCandidate {
	if sa == nil {
		return in
	}
	kind := strings.TrimSpace(sa.Params["TargetsWithControllerProperty"])
	if kind == "" {
		return in
	}
	out := in[:0]
	for _, c := range in {
		if c.kind == "player" {
			continue
		}
		if e.targetControllerPropertyAdmits(kind, c.obj) {
			out = append(out, c)
		}
	}
	return out
}
