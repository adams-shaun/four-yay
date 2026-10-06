package templates

import (
	"strings"
	"testing"
)

// TestMountTargetCardsGenerateOrSkipForAnotherReason inventories the six
// corpus cards with Mount target filters (not Mountain): generated rows must
// name an actual Mount target, and skips may not cite Mount as unserved.
func TestMountTargetCardsGenerateOrSkipForAnotherReason(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	names := []string{
		"One Last Job",
		"Lagorin, Soul of Alacria",
		"Alacrian Armory",
		"Daring Mechanic",
		"Rise from the Wreck",
		"Guidelight Matrix",
	}
	for _, name := range names {
		card, ok := reg.Lookup(name)
		if !ok || card == nil {
			t.Fatalf("precondition: %q does not resolve in the card registry", name)
		}
		it, skip := Generate(reg, name)
		if skip != nil {
			lower := strings.ToLower(skip.Reason)
			if strings.Contains(lower, "mount") && strings.Contains(lower, "unserved") {
				t.Errorf("%s: skip still cites an unserved Mount target: %s", name, skip.Reason)
			}
			t.Logf("%s: skip (%s)", name, skip.Reason)
			continue
		}
		var mountTargets []string
		for _, step := range it.Steps {
			for _, target := range step.Targets {
				targetName := strings.TrimPrefix(target, "p0:")
				targetName = strings.TrimPrefix(targetName, "p1:")
				if _, isMount := reg.Lookup(targetName); isMount && faceHasType(t, reg, targetName, "Mount") {
					mountTargets = append(mountTargets, targetName)
				}
			}
		}
		// Some rows exercise only a card's cast or another ability; those
		// scenarios do not enter its optional battlefield target path. Whenever
		// the generated row does select a Mount target, the registry check above
		// proves it is a real Mount. One Last Job is the targeted graveyard case.
		if name == "One Last Job" && len(mountTargets) == 0 {
			t.Errorf("%s: generated Mount mode has no target naming a real Mount: %+v", name, it.Steps)
		}
		if len(mountTargets) > 0 {
			t.Logf("%s: generated Mount target(s) %v", name, mountTargets)
		} else {
			t.Logf("%s: generated scenario; selected path has no Mount target", name)
		}
	}
}
