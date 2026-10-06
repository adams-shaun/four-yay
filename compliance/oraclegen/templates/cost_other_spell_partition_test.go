package templates

import (
	"testing"
)

// TestCostStaticOtherSpellAppendixPartition pins the exact disposition of the
// brief's 45 "cost static probe not supported" rows: 36 produce a generated
// scenario and the remaining nine are narrower named skips. The sibling
// TestCostStaticOtherSpellAppendixRows asserts only the aggregate (>= 30); a
// regression that turned a served row back into a skip, or changed a named
// skip's reason to the bare form, would still satisfy that aggregate, so this
// test pins each row.
func TestCostStaticOtherSpellAppendixPartition(t *testing.T) {
	reg := loadGenRegistry(t)

	// served rows (produce a scenario) and skipped rows (named skip reason).
	served := map[string]bool{
		"Eluge, the Shoreless Sea":       true,
		"Stormcatch Mentor":              true,
		"Boom Scholar":                   true,
		"Voyager Quickwelder":            true,
		"Inquisitive Glimmer":            true, // static#0.0 only; static#0.1 is skipped below
		"Doran, Besieged by Time":        true,
		"Uthros Psionicist":              true,
		"Ballyrush Banneret":             true,
		"Cloud, Planet's Champion":       true,
		"Serah Farron":                   true,
		"The Darkness Crystal":           true,
		"The Earth Crystal":              true,
		"The Fire Crystal":               true,
		"The Water Crystal":              true,
		"The Wind Crystal":               true,
		"Dwarven Mauler":                 true,
		"Glamdring, Foe-hammer":          true,
		"Radagast of Rhosgobel":          true,
		"Case of the Ransacked Lab":      true,
		"Forensic Gadgeteer":             true,
		"Melek, Reforged Researcher":     true,
		"Baron Strucker, HYDRA Overlord": true,
		"Hulk, Gamma Goliath":            true,
		"Shuri, Wakandan Inventor":       true,
		"The Scarlet Witch":              true,
		"Honest Rutstein":                true,
		"Tombstone, Career Criminal":     true,
		"Highspire Bell-Ringer":          true,
		"Temur Battlecrier":              true,
		"The Sibsig Ceremony":            true,
		"Gran-Gran":                      true,
		"Momo, Friendly Flier":           true,
		"Uncle Iroh":                     true,
		"Agatha of the Vile Cauldron":    true,
		"Blossoming Tortoise":            true,
		"Raging Battle Mouse":            true,
	}
	// skipped is keyed by "<name>/<key>" because two cards have a served face
	// and a skipped face.
	skipped := map[string]string{
		"Artist's Talent/static#0.0":              "class-level prelude unsupported",
		"Samut, the Driving Force/static#0.1":     "speed fixture unavailable",
		"Inquisitive Glimmer/static#0.1":          "static-ability cost probe unsupported",
		"Doc Aurlock, Grizzled Genius/static#0.0": "cast-provenance probe unsupported",
		"Doc Aurlock, Grizzled Genius/static#0.1": "static-ability cost probe unsupported",
		"Bilbo, Thief in the Night/static#0.0":    "cast-provenance probe unsupported",
		"Geyser Drake/static#0.0":                 "NotPlayerTurn needs an opponent-turn probe",
		"Mutagen Man, Living Ooze/static#0.0":     "token fixture unavailable",
		"Beluna Grandsquall/static#0.0":           "cast-provenance probe unsupported",
	}
	if len(served)+len(skipped) != 45 {
		t.Fatalf("precondition: %d served + %d skipped = %d, want 45", len(served), len(skipped), len(served)+len(skipped))
	}

	rows := []struct{ name, key string }{
		{"Artist's Talent", "static#0.0"}, {"Eluge, the Shoreless Sea", "static#0.1"}, {"Stormcatch Mentor", "static#0.0"},
		{"Boom Scholar", "static#0.0"}, {"Samut, the Driving Force", "static#0.1"}, {"Voyager Quickwelder", "static#0.0"},
		{"Inquisitive Glimmer", "static#0.0"}, {"Inquisitive Glimmer", "static#0.1"}, {"Doran, Besieged by Time", "static#0.0"},
		{"Uthros Psionicist", "static#0.0"}, {"Ballyrush Banneret", "static#0.0"}, {"Cloud, Planet's Champion", "static#0.1"},
		{"Serah Farron", "static#0.0"}, {"The Darkness Crystal", "static#0.0"}, {"The Earth Crystal", "static#0.0"},
		{"The Fire Crystal", "static#0.0"}, {"The Water Crystal", "static#0.0"}, {"The Wind Crystal", "static#0.0"},
		{"Bilbo, Thief in the Night", "static#0.0"}, {"Dwarven Mauler", "static#0.0"}, {"Glamdring, Foe-hammer", "static#0.0"},
		{"Radagast of Rhosgobel", "static#0.0"}, {"Case of the Ransacked Lab", "static#0.0"}, {"Forensic Gadgeteer", "static#0.0"},
		{"Melek, Reforged Researcher", "static#0.1"}, {"Baron Strucker, HYDRA Overlord", "static#0.0"}, {"Hulk, Gamma Goliath", "static#0.0"},
		{"Shuri, Wakandan Inventor", "static#0.0"}, {"The Scarlet Witch", "static#0.0"}, {"Doc Aurlock, Grizzled Genius", "static#0.0"},
		{"Doc Aurlock, Grizzled Genius", "static#0.1"}, {"Geyser Drake", "static#0.0"}, {"Honest Rutstein", "static#0.0"},
		{"Tombstone, Career Criminal", "static#0.0"}, {"Highspire Bell-Ringer", "static#0.0"}, {"Temur Battlecrier", "static#0.0"},
		{"The Sibsig Ceremony", "static#0.0"}, {"Gran-Gran", "static#0.0"}, {"Momo, Friendly Flier", "static#0.0"},
		{"Uncle Iroh", "static#0.0"}, {"Mutagen Man, Living Ooze", "static#0.0"}, {"Agatha of the Vile Cauldron", "static#0.0"},
		{"Beluna Grandsquall", "static#0.0"}, {"Blossoming Tortoise", "static#0.0"}, {"Raging Battle Mouse", "static#0.0"},
	}

	scenarios, named := 0, 0
	for _, row := range rows {
		req := costRequirement(t, reg, row.name, row.key)
		it, skip := GenerateB(reg, row.name, req)
		id := row.name + "/" + row.key
		if want, wantSkipped := skipped[id]; wantSkipped {
			if skip == nil {
				t.Errorf("%s: produced a scenario, want the named skip %q", id, want)
				continue
			}
			if skip.Reason != "cost static probe not supported: "+want {
				t.Errorf("%s: skip = %q, want %q", id, skip.Reason, "cost static probe not supported: "+want)
			}
			named++
			continue
		}
		if !served[row.name] {
			t.Errorf("precondition: %s is neither in served nor skipped", id)
			continue
		}
		if skip != nil {
			t.Errorf("%s: skip = %q, want a scenario", id, skip.Reason)
			continue
		}
		if probeStep(t, it).Op == "" {
			t.Errorf("%s: scenario has no probe step", id)
		}
		scenarios++
	}
	if scenarios != 36 || named != 9 {
		t.Errorf("partition = %d scenarios + %d named skips, want 36 + 9", scenarios, named)
	}
}
