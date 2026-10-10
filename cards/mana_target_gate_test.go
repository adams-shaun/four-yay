package cards

// Task agent-20261009T085207Z-2ef0d22c: CR 605.1a -- an ability that requires
// a target is not a mana ability. IsManaAbilitySA gates on a ValidTgts$ on the
// ability itself, so the printed AB$ Mana carriers that
// target (Radiant Lotus, The Warring Triad, Jetfire) leave Face.ManaAbilities
// and take the ordinary activated-ability path, while every other AB$ Mana,
// AB$ ManaReflected and the four ChooseColor chain carriers classify as before.

import (
	"slices"
	"testing"
)

// targetingManaCarriers are the printed activated Mana abilities whose own
// parameters declare ValidTgts$ (measured 2026-10-09 over the pinned corpus;
// AB$ ManaReflected has none).
var targetingManaCarriers = []string{"Radiant Lotus", "The Warring Triad", "Jetfire, Ingenious Scientist"}

// chainTargetingManaCarriers are the printed Mana abilities that target only
// through a SubAbility$ ("Target opponent gains control of Witch Engine"; the
// two Chandra loyalty abilities' "deals damage to target player"). The gate
// reads the head only, so these still classify; the leaf pins that population
// so the whole-chain follow-up moves it deliberately.
var chainTargetingManaCarriers = []string{"Witch Engine", "Chandra, Bold Pyromancer", "Chandra, Dressed to Kill"}

func TestManaAbilityTargetGateMovesOnlyTheHeadTargetingCarriers(t *testing.T) {
	reg := openCardsTestCorpus(t)
	var gated, chained []string
	for _, c := range reg.AllCards() {
		name := c.Faces[0].Name
		for _, f := range c.Faces {
			for _, a := range f.Abilities {
				if a == nil || !a.IsActivated() || !IsManaAbilityAPI(a.API) {
					continue
				}
				if requiresTarget(a) {
					gated = append(gated, name+": "+a.Line)
					if IsManaAbilitySA(a) {
						t.Errorf("%s: a targeting %s ability still classifies as a mana ability: %s", name, a.API, a.Line)
					}
					if slices.Contains(f.ManaAbilities(), a) {
						t.Errorf("%s: a targeting ability is listed in Face.ManaAbilities(): %s", name, a.Line)
					}
				} else if subTargets(a) {
					chained = append(chained, name)
				} else if !IsManaAbilitySA(a) {
					t.Errorf("%s: an untargeted %s ability no longer classifies: %s", name, a.API, a.Line)
				}
			}
		}
	}
	for _, want := range targetingManaCarriers {
		found := false
		for _, g := range gated {
			if len(g) > len(want) && g[:len(want)+1] == want+":" {
				found = true
			}
		}
		if !found {
			t.Errorf("precondition: %s was not among the gated carriers %v", want, gated)
		}
	}
	slices.Sort(chained)
	wantChained := slices.Clone(chainTargetingManaCarriers)
	slices.Sort(wantChained)
	if !slices.Equal(chained, wantChained) {
		t.Errorf("sub-ability-targeting Mana abilities = %v, want %v", chained, wantChained)
	}
	if len(gated) != len(targetingManaCarriers) {
		t.Errorf("the target gate moved %d abilities, want exactly the %d printed carriers: %v",
			len(gated), len(targetingManaCarriers), gated)
	}
}

// subTargets reports whether a SubAbility$ below a (not a itself) declares
// ValidTgts$.
func subTargets(a *SA) bool {
	for s, d := a.Sub, 0; s != nil && d < manaChainDepth; s, d = s.Sub, d+1 {
		if requiresTarget(s) {
			return true
		}
	}
	return false
}
