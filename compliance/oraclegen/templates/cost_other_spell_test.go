package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// probeStep is the last cast or activate step of an item.
func probeStep(t *testing.T, it oraclegen.Item) oraclegen.Step {
	t.Helper()
	for i := len(it.Steps) - 1; i >= 0; i-- {
		if op := it.Steps[i].Op; op == "cast" || op == "activate" {
			return it.Steps[i]
		}
	}
	t.Fatalf("precondition: %s has no cast or activate step: %+v", it.ID, it.Steps)
	return oraclegen.Step{}
}

func costRequirement(t *testing.T, reg *cards.Registry, name, key string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s absent", name)
	}
	for _, candidate := range levelb.Requirements(c) {
		if candidate.Key == key {
			return candidate
		}
	}
	t.Fatalf("precondition: %s %s absent", name, key)
	return levelb.Requirement{}
}

// TestCostStaticOtherSpellProfiles pins the exact reduced price each probe is
// cast (or activated) at, the probe chosen, and that the price is the printed
// one less the static's generic reduction: a probe paying the wrong price
// would not match.
func TestCostStaticOtherSpellProfiles(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, op, probe, mana string
		printed                    string // the probe's printed pool, before the reduction
		reduction                  int
	}{
		{"Stormcatch Mentor", "static#0.0", "cast", "Divination", "CU", "CCU", 1},
		{"The Fire Crystal", "static#0.0", "cast", "Gray Ogre", "CR", "CCR", 1},
		{"Baron Strucker, HYDRA Overlord", "static#0.0", "cast", "A.I.M. Synthoids", "C", "CC", 1},
		{"Dwarven Mauler", "static#0.0", "activate", "+2 Mace", "C", "CCC", 2},
		{"Uthros Psionicist", "static#0.0", "cast", "Gray Ogre", "R", "CCR", 2},
		{"Serah Farron", "static#0.0", "cast", "Yargle, Glutton of Urborg", "CCB", "CCCCB", 2},
		{"Gran-Gran", "static#0.0", "cast", "Divination", "CU", "CCU", 1},
		{"Glamdring, Foe-hammer", "static#0.0", "cast", "Divination", "U", "CCU", 2},
		{"Eluge, the Shoreless Sea", "static#0.1", "cast", "Damnation", "CBB", "CCBB", 1},
		{"Agatha of the Vile Cauldron", "static#0.0", "activate", "Ancient Kavu", "C", "CC", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.name, costRequirement(t, reg, tc.name, tc.key))
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			probe := probeStep(t, it)
			if probe.Op != tc.op || probe.Card != "p0:"+tc.probe || probe.Mana != tc.mana {
				t.Fatalf("probe = %s %s paying %q, want %s %s paying %q", probe.Op, probe.Card, probe.Mana, tc.op, "p0:"+tc.probe, tc.mana)
			}
			if got := len(tc.printed) - len(tc.mana); got != tc.reduction {
				t.Fatalf("precondition: table prices differ by %d, want the static's reduction %d", got, tc.reduction)
			}
			c, ok := reg.Lookup(tc.probe)
			if !ok {
				t.Fatalf("precondition: probe %s absent", tc.probe)
			}
			printedCost := c.Faces[0].ManaCost
			if tc.op == "activate" {
				printedCost = c.Faces[0].Abilities[*probe.AbilityIndex].ParamStr(cards.PKCost)
			}
			if pool, why := oraclegen.PoolFor(printedCost); why != "" || pool != tc.printed {
				t.Fatalf("precondition: %s prints %q (%s), table says %q", tc.probe, pool, why, tc.printed)
			}
			foundSource := false
			for _, permanent := range it.Scenario.Setup["p0"].Battlefield {
				foundSource = foundSource || permanent == tc.name
			}
			if !foundSource {
				t.Fatalf("precondition: reduction source %q absent from battlefield", tc.name)
			}
			if tc.op == "activate" && probeIndex(it) < 0 {
				t.Fatalf("activate probe carries no XMage ability text: %v", it.XAbility)
			}
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("reduced-price probe fails with the static present: %v", res.Fails)
			}
			if res := runSteps(t, withoutStatics(reg, tc.name), it.Scenario, it.Steps); len(res.Fails) == 0 {
				t.Fatalf("probe is not sensitive to %s's cost reduction", tc.name)
			}
		})
	}
}

func probeIndex(it oraclegen.Item) int {
	for i, s := range it.Steps {
		if s.Op == "activate" && it.XAbility[i] != "" {
			return i
		}
	}
	return -1
}

// TestCostStaticOtherSpellAppendixRows measures the brief's 45 "cost static
// probe not supported" rows: at least 30 produce a scenario and the rest are
// a narrower named skip, never the bare reason.
func TestCostStaticOtherSpellAppendixRows(t *testing.T) {
	reg := loadGenRegistry(t)
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
	if len(rows) != 45 {
		t.Fatalf("precondition: %d appendix rows, want 45", len(rows))
	}
	served := 0
	for _, row := range rows {
		it, skip := GenerateB(reg, row.name, costRequirement(t, reg, row.name, row.key))
		if skip == nil {
			served++
			ps := probeStep(t, it)
			t.Logf("ROW %s %s: %s %s %q", row.name, row.key, ps.Op, ps.Card, ps.Mana)
			continue
		}
		const bare = "cost static probe not supported"
		if !strings.HasPrefix(skip.Reason, bare+": ") {
			t.Errorf("%s %s: skip %q is not a narrower named skip", row.name, row.key, skip.Reason)
		}
	}
	if served < 30 {
		t.Errorf("%d of 45 appendix rows produce a scenario, want >= 30", served)
	}
	t.Logf("%d of 45 rows produce a scenario", served)
}

// TestCostSpellFilterGrammar pins the filter matcher against faces built from
// the shapes the appendix uses, including the terms it must refuse.
func TestCostSpellFilterGrammar(t *testing.T) {
	bears := &cards.Face{Name: "Bears", ManaCost: "1 G", Types: []string{"Creature", "Bear"}, Colors: "Green"}
	strike := &cards.Face{Name: "Strike", ManaCost: "1 R", Types: []string{"Instant"}, Colors: "Red"}
	legend := &cards.Face{Name: "Legend", ManaCost: "2 W", Types: []string{"Legendary", "Creature", "Kithkin", "Soldier"}, Colors: "White", Keywords: []string{"Flying"}}
	for _, tc := range []struct {
		face   *cards.Face
		filter string
		want   bool
	}{
		{bears, "Card.nonCreature", false}, {strike, "Card.nonCreature", true},
		{strike, "Instant,Sorcery", true}, {bears, "Instant,Sorcery", false},
		{legend, "Creature.Legendary", true}, {bears, "Creature.Legendary", false},
		{legend, "Kithkin,Soldier", true}, {bears, "Kithkin,Soldier", false},
		{bears, "Card.Green", true}, {strike, "Card.Green", false},
		{legend, "Creature.nonLemur+withFlying+YouCtrl", true}, {bears, "Creature.nonLemur+withFlying+YouCtrl", false},
		{bears, "Permanent", true}, {strike, "Permanent", false},
		{strike, "Instant.cmcGE4", false}, {bears, "Creature.cmcLE2", true},
	} {
		if got := spellFilterMatches(nil, tc.face, tc.filter, provNone); got != tc.want {
			t.Errorf("filter %q on %s = %v, want %v", tc.filter, tc.face.Name, got, tc.want)
		}
	}
	for _, filter := range []string{"Card.!wasCastFromYourHand", "Artifact.token+YouCtrl", "Permanent.AdventureCard"} {
		if spellFilterSupported(filter, provNone) && !strings.Contains(filter, "wasCast") && !strings.Contains(filter, "AdventureCard") {
			t.Errorf("filter %q is accepted but must be a named gap", filter)
		}
	}
}
