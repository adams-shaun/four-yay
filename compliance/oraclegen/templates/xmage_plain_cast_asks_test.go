package templates

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The sixteen cast-resolve scenarios whose plain cast XMage still asks a
// yes/no or either-or question about (measured on the host, 2026-10-05 forced
// full replay). The generator emits the cast for its mana cost only; the
// driver (ScenarioReplay.ScriptedChoicePlayer.chooseUse) answers each ask as
// gorge played it. The driver needs no new scenario field: every shape below
// is already in the scenario or its xmage_answers.
var plainCastAskCards = map[string][]string{
	"leyline":   {"Leyline of Hope", "Leyline of Mutation", "Leyline of Resonance", "Leyline of Transformation", "Leyline of the Void", "Leyline of the Guildpact", "Quicksilver, Brash Blur"},
	"offspring": {"Flowerfoot Swordmaster", "Iridescent Vinelasher", "Pawpatch Recruit", "Starscape Cleric", "Thundertrap Trainer"},
	"waterbend": {"Ruinous Waterbending", "Spirit Water Revival"},
	"orcost":    {"Deadly Precision"},
	"kicker":    {"The Eagles Are Coming!"},
}

// TestPlainCastAsksScenarioShape pins what the driver's answer rule reads: a
// plain cast step (no kicked/cast_mode field, which the driver rejects), the
// Leyline in the casting seat's opening hand, and Deadly Precision's recorded
// cost pick on the cast step.
func TestPlainCastAsksScenarioShape(t *testing.T) {
	reg := loadGenRegistry(t)
	seen := 0
	for class, names := range plainCastAskCards {
		for _, name := range names {
			it, skip := Generate(reg, name)
			if skip != nil {
				t.Fatalf("%s: no scenario (vacuous test): %s", name, skip.Reason)
			}
			var sc struct {
				Setup struct {
					P0 struct {
						Hand []string `json:"hand"`
					} `json:"p0"`
				} `json:"setup"`
				Steps []map[string]any `json:"steps"`
			}
			if err := json.Unmarshal(it.Raw(), &sc); err != nil {
				t.Fatal(err)
			}
			cast := -1
			for i, st := range sc.Steps {
				if st["op"] == "cast" && st["card"] == "p0:"+name {
					cast = i
					break
				}
			}
			if cast < 0 {
				t.Fatalf("%s: no cast step: %s", name, it.Raw())
			}
			st := sc.Steps[cast]
			if _, ok := st["kicked"]; ok {
				t.Errorf("%s: cast step carries kicked; the driver rejects it", name)
			}
			if _, ok := st["cast_mode"]; ok {
				t.Errorf("%s: cast step carries cast_mode; the driver rejects it", name)
			}
			if class == "leyline" {
				inHand := false
				for _, h := range sc.Setup.P0.Hand {
					inHand = inHand || h == name
				}
				if !inHand {
					t.Errorf("%s: the Leyline is not in p0's opening hand: %s", name, it.Raw())
				}
			}
			if class == "orcost" {
				picked := false
				for _, a := range it.Steps[cast].Answers {
					for _, p := range a.Pick {
						picked = picked || (a.Kind == "choose" && p == "Sacrifice artifact or creature")
					}
				}
				if !picked {
					t.Errorf("%s: the cast step records no sacrifice cost pick: %s", name, it.Raw())
				}
			}
			seen++
		}
	}
	if seen != 16 {
		t.Fatalf("checked %d scenarios, want 16", seen)
	}
}

// TestPlainCastAsksDriverPins couples the answer rule to the Java driver:
// reverting the chooseUse override (or any of its three ask keys) must fail
// here even though the generator output is unchanged. STRUCTURAL: XMage
// cannot run in a seat, so the behaviour is proved by the host forced replay
// of the sixteen scenarios in plainCastAskCards.
func TestPlainCastAsksDriverPins(t *testing.T) {
	source, err := os.ReadFile("../../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"public boolean chooseUse(Outcome outcome, String message, String secondMessage, String trueText, String falseText, Ability source, Game game)",
		"source instanceof LeylineAbility",
		`askedBy(OptionalAdditionalSourceCosts.class, "addOptionalAdditionalCosts")`,
		`askedBy(OrCost.class, "pay")`,
		"owner.recordedCostIsFirst(trueText, falseText)",
		"castCostPicks.addAll(names(a, \"pick\"))",
		"this.owner = player.owner;",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer implements the plain-cast ask rule %q", required)
		}
	}
}
