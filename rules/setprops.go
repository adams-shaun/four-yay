package rules

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/chars"
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
//
// Two more keys in the same family constrain a candidate against a REFERENCE
// object rather than against the other chosen targets, so they are plain
// legality filters too:
//
//   - TargetsWithSharedCardType$ <reference>  (shares a card type with the
//     reference -- ParentTarget, the parent ability's chosen target, or
//     TriggeredCard, the card an event triggered on)
//   - TargetsWithSharedTypes$ <list>          (narrows which card types count
//     for the shared-card-type intersection to the listed ones)
//
// Both are enforced by filterTargetsWithSharedCardType at the census and by
// sharedCardTypeAdmits at the resolution recheck, from the SAME predicate, so
// offer and recheck cannot disagree.

// targetSetPropMode reports which set-property constraint this targeting SA
// carries, or SetPropNone. Forge writes at most one such key per SA; the
// corpus order checked (card type, creature type, toughness) then (mana
// value, name) is fixed so the choice is deterministic if a future SA ever
// carried two.
func targetSetPropMode(sa *cards.SA) (decision.SetPropMode, string) {
	tp := effects.TargetsOf(sa)
	return tp.SetProp, tp.SetPropKind
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
	case "color":
		// Reveal<N/SameColor>'s token family (Illuminated Folio): the card's
		// DERIVED colours, one WUBRG letter per token -- effects.ColorsOf
		// reads the mana cost, or an explicit Colors: line, and is
		// Devoid-aware -- never Face().Colors, which is empty for every
		// plain coloured card. A colourless card's empty set shares
		// nothing, which is the rules read: two colourless cards do not
		// share a colour (CR 106.1).
		if s := effects.ColorsOf(o); s != "" {
			return strings.Split(s, "")
		}
		return nil
	}
	return nil
}

// --- Reveal<N/SameColor> (Illuminated Folio) -------------------------------
//
// The relational reveal cost. SameColor is a relation BETWEEN the revealed
// cards, not a card filter, so the offer gate cannot walk it through
// costCandidates' ordinary spec match (nothing matches) and the payment ask
// carries the set constraint itself. The reading: the cost is payable iff N
// DISTINCT eligible hand cards share one colour, and the payment ask is a
// decision.SetPropShared KChoose over each candidate's colour tokens, so
// Decision.Validate (seat answers) and botpolicy's Clamp (the bot) enforce
// the SAME rule the offer gate measured -- one home. The running-intersection
// semantics of SetPropShared equal pairwise "share a colour" because the
// corpus carrier's N is exactly 2.

// sameColorRevealSets returns the eligible hand-card candidates for one
// Reveal<N/SameColor> part and their per-card colour tokens. SameColor
// matches no card as a filter, so the walk admits every card the ordinary
// candidate walk admits (the "Card" base matches any card), with the same
// excludeSource rule the filter path applies: a cast's own card is on the
// stack and cannot pay its own reveal, an ability's source stays where it is
// and can.
func (e *Engine) sameColorRevealSets(p state.PlayerID, source state.ObjID, excludeSource bool) ([]state.ObjID, [][]string) {
	cands := e.costCandidates(p, source, state.ZHand, "Card", excludeSource, false)
	sets := make([][]string, 0, len(cands))
	for _, id := range cands {
		sets = append(sets, e.setPropTokens("color", id))
	}
	return cands, sets
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
		if chars.IsCardType(t) {
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
		if !chars.IsCreatureSubtype(t) {
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

// sharedCardTypeRef reports the reference object spec TargetsWithSharedCardType$
// names (ParentTarget, TriggeredCard, ...), or "" when the key is absent/not
// True-shaped. Forge writes a name here, never "True".
func sharedCardTypeRef(sa *cards.SA) string {
	return effects.TargetsOf(sa).SharedCardType
}

// sharedTypesWhitelist parses TargetsWithSharedTypes$ ("Artifact,Creature,Land")
// into lowercase card-type tokens, or nil when absent. An empty list is nil, so
// the intersection falls back to the reference's own card types.
func sharedTypesWhitelist(sa *cards.SA) []string {
	return effects.TargetsOf(sa).SharedTypes
}

// sharedCardTypeReference resolves the reference object a
// TargetsWithSharedCardType$ spec names. ParentTarget/ParentTargeted/
// ThisTargetedCard/Targeted read the FIRST object target recorded on the
// source stack object -- the parent ability's target, recorded before any
// sub-ability target, which is exactly the object Forge's ParentTarget names
// for a DB$ sub-ability (ExchangeControl's `Defined$ ParentTarget`).
// TriggeredCard reads the trigger context's captured card. An unresolvable
// reference returns 0 and the filter fails closed (drops every candidate),
// the same direction every unread spec qualifier takes. The two forms are the
// complete set the corpus carries
// (`/usr/bin/grep -rl TargetsWithSharedCardType .cards/cardsfolder`: 5 files,
// all ParentTarget or TriggeredCard); any other spelling fails closed rather
// than guessing a referent the corpus never exercises.
func (e *Engine) sharedCardTypeReference(ref string, source state.ObjID, sc effects.SpecContext) state.ObjID {
	switch strings.TrimSpace(ref) {
	case "ParentTarget", "ParentTargeted", "ThisTargetedCard", "Targeted":
		if o := e.G.Obj(source); o != nil {
			for _, t := range o.Targets {
				if !t.IsPlayer && t.Obj != 0 && t.Obj != source {
					return t.Obj
				}
			}
		}
		return 0
	case "TriggeredCard", "TriggeredCardLKICopy":
		return sc.TriggerCard
	}
	return 0
}

// sharedCardTypeAdmits reports whether obj shares at least one card type with
// ref, restricted to the TargetsWithSharedTypes$ whitelist when present. It is
// the ONE predicate the census and the resolution recheck both call. A zero
// reference (unresolved) or an object with no card types fails closed.
func (e *Engine) sharedCardTypeAdmits(obj, ref state.ObjID, whitelist []string) bool {
	if obj == 0 || ref == 0 || obj == ref {
		return false
	}
	tokens := e.setPropTokens("cardtype", obj)
	if len(tokens) == 0 {
		return false
	}
	refTokens := e.setPropTokens("cardtype", ref)
	if len(refTokens) == 0 {
		return false
	}
	for _, t := range tokens {
		if len(whitelist) > 0 && !slices.Contains(whitelist, t) {
			continue
		}
		if slices.Contains(refTokens, t) {
			return true
		}
	}
	return false
}

// filterTargetsWithSharedCardType drops the candidates that do not share a
// card type with the reference TargetsWithSharedCardType$ names. Like the
// other census post-filters it can only REMOVE candidates, and the resolution
// recheck applies the SAME predicate, so offer and recheck cannot disagree.
func (e *Engine) filterTargetsWithSharedCardType(in []targetCandidate, sa *cards.SA, source state.ObjID, sc effects.SpecContext) []targetCandidate {
	ref := sharedCardTypeRef(sa)
	if ref == "" {
		return in
	}
	refObj := e.sharedCardTypeReference(ref, source, sc)
	whitelist := sharedTypesWhitelist(sa)
	out := in[:0]
	for _, c := range in {
		if c.kind == "player" {
			continue
		}
		if e.sharedCardTypeAdmits(c.obj, refObj, whitelist) {
			out = append(out, c)
		}
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
	kind := effects.TargetsOf(sa).ControllerProperty
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
