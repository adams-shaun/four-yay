package cards

import "strings"

// manaChainDepth bounds the SubAbility$ walk IsManaAbilitySA performs. Real
// chains are four nodes deep (ChooseColor -> Mana -> Animate -> Cleanup); the
// cap exists so a runtime-built cyclic Sub chain cannot hang a classifier
// that runs on every offer walk.
const manaChainDepth = 16

// IsManaAbilityAPI reports whether api is one of the two supported activated
// mana ability APIs (CR 605.1a): AB$ Mana and AB$ ManaReflected. It is a pure
// function of the ability's API word, so it lives beside the IR rather than in
// the engine; rules' activation walks and trigmatch's ValidSA$ Activated.Mana
// gate share it. A caller that classifies an ABILITY -- not an API word --
// wants IsManaAbilitySA, which also recognises a chain whose head is not the
// Mana API itself.
func IsManaAbilityAPI(api string) bool { return api == "Mana" || api == "ManaReflected" }

// IsManaAbilitySA reports whether sa is an activated mana ability (CR 605.1a):
// the head-word APIs above, or Forge's "AB$ ChooseColor | SubAbility$ DBMana"
// chain (Foraging Wickermaw, Nykthos Shrine to Nyx, Nyx Lotus, Rhystic Cave).
// The chain's head carries the activation cost and the colour ask, and the
// DB$ Mana sub-ability produces the mana, so classifying by the head word
// alone left the whole ability on the ordinary stack path. Every
// mana-vs-ordinary split must share this one classifier, or the chain is
// offered twice (wheel and priority "ability"). Kind must be AB: an SP$ Mana
// is a mana ritual spell (Battle Hymn), never an activated ability.
//
// The chain arm is deliberately scoped to ChooseColor heads. A generic "any
// AB whose SubAbility$ chain reaches a DB$ Mana" walk sweeps in chains that
// are NOT mana abilities: Red Death, Shipwrecker's goad targets a creature
// (CR 605.1a), and planeswalker ultimates (Chandra, Heart of Fire) are
// loyalty abilities (CR 605.1b). Measured 2026-10-09: 25 activated abilities
// across 21 other head APIs reach a DB$ Mana; this ticket owns only the four
// ChooseColor carriers, and the generic CR-correct classification (the
// loyalty gate) is a separate change.
//
// An ability whose own parameters declare ValidTgts$ requires a target and is
// not a mana ability (CR 605.1a): Radiant Lotus, The Warring Triad and Jetfire
// stay on the ordinary stack path (the corpus population is pinned by
// TestManaAbilityTargetGateMovesOnlyTheHeadTargetingCarriers). The gate reads
// the head only. A target that lives on a SubAbility$ (Witch Engine's
// ChangeControl, the two Chandra loyalty abilities' damage) is the separate
// whole-chain change this classifier still owes.
func IsManaAbilitySA(sa *SA) bool {
	if sa == nil || !sa.IsActivated() {
		return false
	}
	if !IsManaAbilityAPI(sa.API) && !(sa.API == "ChooseColor" && ManaChainProduction(sa) != nil) {
		return false
	}
	return !requiresTarget(sa)
}

// requiresTarget reports whether sa itself declares ValidTgts$ (the head read
// effects.TargetsOf(sa).Targeted() makes; CR 605.1a).
func requiresTarget(sa *SA) bool {
	return strings.TrimSpace(sa.ParamStr(PKValidTgts)) != ""
}

// ManaChainProduction returns the first DB$ Mana sub-ability reachable through
// sa's SubAbility$ chain, or nil when none is. It is the one chain walk behind
// IsManaAbilitySA and the label/payability readers that must name the
// production the head itself does not carry.
func ManaChainProduction(sa *SA) *SA {
	if sa == nil {
		return nil
	}
	for sub, d := sa.Sub, 0; sub != nil && d < manaChainDepth; sub, d = sub.Sub, d+1 {
		if sub.Kind == "DB" && sub.API == "Mana" {
			return sub
		}
	}
	return nil
}

// manaAbilityListEntry is the Face.ManaAbilities list membership: an AB$ Mana
// head, or a chain that reaches a DB$ Mana. ManaReflected is excluded by the
// list's contract -- Face.ManaReflectedAbilities is its own list, and the
// activation walk has a separate reflected pass, so listing a reflected
// ability here would offer it twice.
func manaAbilityListEntry(sa *SA) bool {
	return sa != nil && sa.API != "ManaReflected" && IsManaAbilitySA(sa)
}
