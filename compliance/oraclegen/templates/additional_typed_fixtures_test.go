package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestAdditionalCostTypedFixturesGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, subtype string
	}{
		{"Champion of the Clachan", "Kithkin"},
		{"Champion of the Path", "Elemental"},
		{"Champion of the Weird", "Goblin"},
		{"Champions of the Shoal", "Merfolk"},
	} {
		it, skip := Generate(reg, tc.name)
		if skip != nil {
			t.Fatalf("%s: %s", tc.name, skip.Reason)
		}
		var found bool
		for _, name := range it.Setup["p0"].Hand {
			if faceHasType(t, reg, name, tc.subtype) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: hand %v has no %s card to behold", tc.name, it.Setup["p0"].Hand, tc.subtype)
		}
	}

	for _, tc := range []struct {
		name  string
		count int
	}{
		{"Fear of Exposure", 2},
		{"Guardian of the Great Door", 4},
	} {
		it, skip := Generate(reg, tc.name)
		if skip != nil {
			t.Fatalf("%s: %s", tc.name, skip.Reason)
		}
		bf := it.Setup["p0"].Battlefield
		tapped := map[string]int{}
		for _, name := range it.Setup["p0"].Tapped {
			tapped[name]++
		}
		eligible := map[string]bool{}
		for _, name := range bf {
			if !faceHasType(t, reg, name, "Creature") && !faceHasType(t, reg, name, "Land") && (tc.name != "Guardian of the Great Door" || !faceHasType(t, reg, name, "Artifact")) {
				continue
			}
			if tapped[name] == 0 {
				eligible[name] = true
			}
		}
		if len(eligible) < tc.count {
			t.Errorf("%s: only %d distinct eligible untapped permanents in %v (tapped %v), want %d", tc.name, len(eligible), bf, it.Setup["p0"].Tapped, tc.count)
		}
	}

	it, skip := Generate(reg, "Benevolent River Spirit")
	if skip != nil {
		t.Fatalf("Benevolent River Spirit: %s", skip.Reason)
	}
	cast := castStep(t, it, "Benevolent River Spirit")
	if strings.Count(cast.Mana, "C") < 5 {
		t.Fatalf("Benevolent River Spirit mana %q provides fewer than five generic mana for waterbend", cast.Mana)
	}
}

func TestCounterSpellAdditionalFixturesGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Sokka's Haiku", "Repulsive Mutation"} {
		it, skip := Generate(reg, name)
		if skip != nil {
			t.Fatalf("%s: %s", name, skip.Reason)
		}
		face := faceOf(t, reg, name)
		slots := oraclegen.SlotSpecs(face)
		stackSlots := stackSlotIndexes(slots)
		if len(stackSlots) == 0 {
			t.Fatalf("%s: slots %v contain no stack target", name, slots)
		}
		var ownCast, precast *oraclegen.Step
		for i := range it.Scenario.Steps {
			step := &it.Scenario.Steps[i]
			if step.Op != "cast" {
				continue
			}
			if step.Card == "p0:"+name {
				ownCast = step
			} else if precast == nil {
				precast = step
			}
		}
		if ownCast == nil || precast == nil {
			t.Fatalf("%s scenario has no own cast and preceding precast: %+v", name, it.Scenario.Steps)
		}
		if len(ownCast.Targets) != len(slots) {
			t.Fatalf("%s targets %v do not satisfy all slots %v", name, ownCast.Targets, slots)
		}
		for _, idx := range stackSlots {
			if ownCast.Targets[idx] != precast.Card {
				t.Errorf("%s stack slot %d target %q, want precast %q", name, idx, ownCast.Targets[idx], precast.Card)
			}
		}
		if name == "Repulsive Mutation" {
			if len(ownCast.Targets) != 2 || !strings.Contains(ownCast.Targets[0], "p0:") {
				t.Errorf("Repulsive Mutation targets %v; want own creature plus stack spell", ownCast.Targets)
			} else if !faceHasType(t, reg, strings.TrimPrefix(ownCast.Targets[0], "p0:"), "Creature") {
				t.Errorf("Repulsive Mutation first target %q is not a creature", ownCast.Targets[0])
			}
		}
	}
}
