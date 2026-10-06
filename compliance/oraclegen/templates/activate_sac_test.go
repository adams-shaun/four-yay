package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestActivateSacFilterFixtures is the level-B Sac regression: representative
// costs from the appendix (the common "another creature", a typed artifact, a
// two-type alternative, and the subtype fixtures) now generate a scenario
// whose setup carries the right sacrifice fixture and whose XMage answers name
// that same permanent exactly once.
func TestActivateSacFilterFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, fixture string
	}{
		{"Nita, Forum Conciliator", "activate#0.0", "Llanowar Elves"},
		{"Snowslope Hunter", "activate#0.0", "Llanowar Elves"},
		{"Ezrim, Agency Chief", "activate#0.0", "Ornithopter"},
		{"Stone-Giant of High Pass", "activate#0.0", "Ornithopter"},
		{"Intruding Soulrager", "activate#0.0", "Bottomless Pool"},
		{"Wick, the Whorled Mind", "activate#0.0", "Skullcap Snail"},
		{"Quina, Qu Gourmet", "activate#0.0", "Anurid Murkdiver"},
		{"Bolg's Company", "activate#0.0", "Goblin Piker"},
		{"Ripchain Razorkin", "activate#0.0", "Forest"},
		{"Syr Ginger, the Meal Ender", "activate#0.0", ""}, // NICKNAME: sacrifices itself, no fixture
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it, req := activateRequirement(t, reg, tc.name, tc.key)
			// Precondition: the named card actually carries a Sac cost, or
			// this test would pass on a template that changed under it.
			c, ok := reg.Lookup(tc.name)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s is missing from the corpus", tc.name)
			}
			idx := 0
			if req.Slot != "" {
				if n, err := strconv.Atoi(req.Slot); err == nil {
					idx = n
				}
			}
			if idx >= len(c.Faces[0].Abilities) || !strings.Contains(c.Faces[0].Abilities[idx].ParamStr(cards.PKCost), "Sac") {
				t.Fatalf("precondition: %s ability %d has no Sac cost", tc.name, idx)
			}
			seat := it.Scenario.Setup["p0"]
			seen := false
			for _, n := range seat.Battlefield {
				if n == tc.fixture {
					seen = true
				}
			}
			if tc.fixture != "" && !seen {
				t.Fatalf("sacrifice fixture %q absent from p0 battlefield: %v", tc.fixture, seat.Battlefield)
			}
			if tc.fixture == "" {
				return
			}
			matches := 0
			for _, stepAnswers := range it.XAnswers {
				for _, answer := range stepAnswers {
					if answer.Kind == "choice" && strings.EqualFold(answer.Value, tc.fixture) {
						matches++
					}
				}
			}
			if matches != 1 {
				t.Fatalf("XMage answers name sacrifice fixture %q %d times, want once: %+v", tc.fixture, matches, it.XAnswers)
			}
		})
	}
}

// TestActivateSacFixtureAndAnswerShareTheTable pins the structural fix: one
// lookup (sacFilterFixture) drives both the battlefield fixture and the XMage
// answer, so they can never name different permanents. Every case asserts the
// two agree with the table's own result, in both helper call sites.
func TestActivateSacFixtureAndAnswerShareTheTable(t *testing.T) {
	cases := []struct {
		cost, fixture string
	}{
		{"Sac<1/Creature.Other/another creature>", "Llanowar Elves"},
		{"Sac<1/Creature>", "Llanowar Elves"},
		{"Sac<1/Creature.Other;Enchantment.Other/another creature or enchantment>", "Llanowar Elves"},
		{"Sac<1/Creature.Other;Permanent.token+Other/another creature or token>", "Llanowar Elves"},
		{"Sac<1/Creature.Other+cmcEQX/a creature you control with mana value X other than NICKNAME>", "Llanowar Elves"},
		{"Sac<1/Goblin.Other/another Goblin>", "Goblin Piker"},
		{"Sac<1/Frog>", "Anurid Murkdiver"},
		{"Sac<1/Snail>", "Skullcap Snail"},
		{"Sac<1/Rat>", "Bog Rats"},
		{"Sac<1/Room>", "Bottomless Pool"},
		{"Sac<1/Artifact>", "Ornithopter"},
		{"Sac<1/Artifact/an artifact>", "Ornithopter"},
		{"Sac<1/Artifact.Other;Land.Other/another artifact or land>", "Ornithopter"},
		{"Sac<1/Artifact;Creature/artifact or creature>", "Ornithopter"},
		{"Sac<1/Artifact.Other;Creature.Other/another artifact or creature>", "Ornithopter"},
		{"Sac<1/Land>", "Forest"},
		{"Sac<1/Enchantment>", "Glorious Anthem"},
		{"Sac<1/Planeswalker>", "Jace Beleren"},
	}
	for _, tc := range cases {
		got, ok := sacFilterFixture(tc.cost)
		if !ok || got != tc.fixture {
			t.Fatalf("sacFilterFixture(%q) = (%q, %v), want (%q, true)", tc.cost, got, ok, tc.fixture)
		}
		var p0 oraclegen.Seat
		addActivationCostFixtures(&p0, tc.cost)
		if len(p0.Battlefield) != 1 || p0.Battlefield[0] != tc.fixture {
			t.Fatalf("addActivationCostFixtures(%q) battlefield = %v, want [%s]", tc.cost, p0.Battlefield, tc.fixture)
		}
		answers := make([][]oraclegen.XAnswer, 1)
		addActivationCostAnswers(answers, 0, tc.cost, nil)
		matches := 0
		for _, answer := range answers[0] {
			if answer.Kind == "choice" && strings.EqualFold(answer.Value, tc.fixture) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("addActivationCostAnswers(%q) names %q %d times, want once: %+v", tc.cost, tc.fixture, matches, answers[0])
		}
	}
}

// TestActivateSacSelfAndNICKNAME: CARDNAME and NICKNAME both sacrifice the
// source, so neither places a fixture nor scripts an answer.
func TestActivateSacSelfAndNICKNAME(t *testing.T) {
	for _, cost := range []string{"Sac<1/CARDNAME>", "Sac<1/CARDNAME/this creature>", "Sac<1/NICKNAME>"} {
		if !sacSelf(cost) {
			t.Fatalf("sacSelf(%q) = false, want true", cost)
		}
		if _, ok := sacFilterFixture(cost); ok {
			t.Fatalf("sacFilterFixture(%q) returned a fixture for a self-sacrifice", cost)
		}
		var p0 oraclegen.Seat
		addActivationCostFixtures(&p0, cost)
		if len(p0.Battlefield) != 0 {
			t.Fatalf("addActivationCostFixtures(%q) added %v for a self-sacrifice", cost, p0.Battlefield)
		}
		if _, gap := activationCost(cost); gap != "" {
			t.Fatalf("activationCost(%q) = gap %q, want cellable", cost, gap)
		}
	}
}

// TestActivateSacGapClasses is the fail-closed direction: an unsupported Sac
// cost names its cause rather than an opaque Sac<...>, and the token class is
// never silently celled.
func TestActivateSacGapClasses(t *testing.T) {
	cases := []struct{ cost, gap string }{
		// A '+' modifier narrows the accepted token; this build does not
		// evaluate a Sac spec modifier, so both stay named gaps.
		{"Sac<3/Artifact.token+WithDifferentNames/artifact tokens with different names>", "Sac<token>"},
		{"Sac<1/Permanent.token+namedWood/token named Wood>", "Sac<token>"},
		{"Sac<1/Blood>", "Sac<unsupported-filter>"},
		{"Sac<1/Equipment.Attached/an Equipment attached to NICKNAME>", "Sac<attached>"},
		{"Sac<1/Aura.Attached>", "Sac<attached>"},
		// A count above the distinct fixtures the table names.
		{"Sac<4/Artifact>", "Sac<count>"},
		{"Sac<7/Creature.Other/other creatures>", "Sac<count>"},
		{"Sac<4/Rat>", "Sac<count>"},
		{"Sac<X/Artifact>", "Sac<announced>"},
		{"Sac<1/Creature.IsSuspected/suspected creature>", "Sac<unsupported-filter>"},
	}
	for _, tc := range cases {
		if _, gap := activationCost(tc.cost); gap != tc.gap {
			t.Errorf("activationCost(%q) gap = %q, want %q", tc.cost, gap, tc.gap)
		}
	}
}
