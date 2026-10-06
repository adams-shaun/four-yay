package templates

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// tapCombatOps renders an item's cause as op@seat words, the trailing
// resolves dropped, so a test can pin which cause served a card.
func tapCombatOps(steps []oraclegen.Step) string {
	for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
		steps = steps[:len(steps)-1]
	}
	words := make([]string, len(steps))
	for i, st := range steps {
		words[i] = fmt.Sprintf("%s@%d", st.Op, st.Seat)
	}
	return strings.Join(words, " ")
}

// TestTapCombatTriggerRecipes generates each card's trigger, replays the item
// through gorge and checks the card's own trigger is on the stack, with the
// cause the sub-family promises (an attack, a block, a tap spell, an attach).
func TestTapCombatTriggerRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, sub, ops string
	}{
		{"Silvergill Peddler", "trigger#0.0", "trigger.tapped", "attack@0"},
		{"Wylie Duke, Atiin Hero", "trigger#0.0", "trigger.tapped", "cast@0"},
		{"Hawkeye's Bow", "trigger#0.0", "trigger.tapped", "attach@0 attack@0"},
		{"Icewrought Sentry", "trigger#0.1", "trigger.tapped", "cast@0"},
		{"Saw", "trigger#0.0", "trigger.attacks-attached", "attach@0 attack@0"},
		{"Rewrite History", "trigger#0.0", "trigger.tapped", "attack@0"},
		{"Aurelia, the Law Above", "trigger#0.0", "trigger.attacks", "attack@0"},
		{"Aurelia, the Law Above", "trigger#0.1", "trigger.attacks", "attack@0"},
		{"Sabotage Strategist", "trigger#0.0", "trigger.opponent-attacks", "attack@1"},
		{"Doran, Besieged by Time", "trigger#0.1", "trigger.blocks", "attack@1 block@0"},
		{"Anzrag, the Quake-Mole", "trigger#0.0", "trigger.blocked", "attack@0 block@1"},
		{"Skewer Slinger", "trigger#0.1", "trigger.blocked", "attack@0 block@1"},
		{"Encumbered Reejerey", "trigger#0.0", "trigger.tapped", "attack@0"},
		{"Burning Sun Cavalry", "trigger#0.1", "trigger.blocks", "attack@1 block@0"},
		{"Skystinger", "trigger#0.0", "trigger.blocks", "attack@1 block@0"},
		{"Hylda of the Icy Crown", "trigger#0.0", "trigger.tapped", "cast@0"},
		{"Captain America, Living Legend", "trigger#0.0", "trigger.tapped", "attack@0"},
		{"Tattered Ratter", "trigger#0.0", "trigger.blocked", "attack@0 block@1"},
		{"Norin, Swift Survivalist", "trigger#0.0", "trigger.blocked", "attack@0 block@1"},
		{"Deeproot Pilgrimage", "trigger#0.0", "trigger.tapped", "attack@0"},
		{"Thunder Lasso", "trigger#0.1", "trigger.attacks-attached", "attach@0 attack@0"},
		{"Ordeal of Nylea", "trigger#0.0", "trigger.attacks-attached", "cast@0 resolve@0 attack@0"},
	} {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, tc.sub)
			if got := tapCombatOps(it.Scenario.Steps); got != tc.ops {
				t.Fatalf("cause = %q, want %q", got, tc.ops)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("scenario does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, tc.name) {
				t.Fatalf("%s's trigger never appears on stack: %+v", tc.name, it.Scenario.Steps)
			}
		})
	}
}

// TestTapCombatCauseDetails pins what the ops alone do not say: how many
// creatures attack, who the blocker's attacker is, and that the fixtures the
// condition needs are on the board.
func TestTapCombatCauseDetails(t *testing.T) {
	reg := loadGenRegistry(t)
	attackers := func(it oraclegen.Item) []string {
		for _, st := range it.Scenario.Steps {
			if st.Op == "attack" {
				return st.Attackers
			}
		}
		return nil
	}
	for _, tc := range []struct {
		key  string
		want int
	}{{"trigger#0.0", 3}, {"trigger#0.1", 5}} {
		it := triggerRequirement(t, reg, "Aurelia, the Law Above", tc.key, "trigger.attacks")
		if got := len(attackers(it)); got < tc.want {
			t.Errorf("Aurelia %s: %d attackers, want at least %d", tc.key, got, tc.want)
		}
	}
	// Sabotage Strategist's cause is p1's attack on p0, with p1 owning the attacker.
	sab := triggerRequirement(t, reg, "Sabotage Strategist", "trigger#0.0", "trigger.opponent-attacks")
	if got := attackers(sab); len(got) != 1 || !strings.HasPrefix(got[0], "p1:") || len(sab.Scenario.Setup["p1"].Battlefield) == 0 {
		t.Errorf("Sabotage Strategist attackers %v, p1 battlefield %v", got, sab.Scenario.Setup["p1"].Battlefield)
	}
	// Burning Sun Cavalry needs a Dinosaur on p0's board when it blocks.
	cav := triggerRequirement(t, reg, "Burning Sun Cavalry", "trigger#0.1", "trigger.blocks")
	if !inZone(cav.Scenario.Setup["p0"].Battlefield, "Orazca Frillback") {
		t.Errorf("Burning Sun Cavalry p0 battlefield %v lacks the Dinosaur", cav.Scenario.Setup["p0"].Battlefield)
	}
	// Skystinger blocks a flyer, which the Bears are not.
	sky := triggerRequirement(t, reg, "Skystinger", "trigger#0.0", "trigger.blocks")
	if got := attackers(sky); len(got) != 1 || got[0] == "p1:"+bearsProbe {
		t.Errorf("Skystinger's attacker %v must not be the Bears", got)
	}
	// The Rat and the Merfolk are picked by the trigger's own filter.
	for _, tc := range []struct{ card, sub string }{{"Tattered Ratter", "trigger.blocked"}, {"Deeproot Pilgrimage", "trigger.tapped"}} {
		it := triggerRequirement(t, reg, tc.card, "trigger#0.0", tc.sub)
		if got := attackers(it); len(got) != 1 || got[0] == "p0:"+bearsProbe {
			t.Errorf("%s's attacker %v must not be the Bears", tc.card, got)
		}
	}
	// A vigilant creature is not tapped by attacking: the tap spell is the cause.
	wylie := triggerRequirement(t, reg, "Wylie Duke, Atiin Hero", "trigger#0.0", "trigger.tapped")
	if !inZone(wylie.Scenario.Setup["p0"].Hand, tapSpellProbe) {
		t.Errorf("Wylie Duke hand %v lacks %s", wylie.Scenario.Setup["p0"].Hand, tapSpellProbe)
	}
	// Ordeal of Nylea starts in hand: setup would otherwise leave an
	// unattached Aura that state-based actions put into the graveyard.
	ordeal := triggerRequirement(t, reg, "Ordeal of Nylea", "trigger#0.0", "trigger.attacks-attached")
	if p0 := ordeal.Scenario.Setup["p0"]; !inZone(p0.Hand, "Ordeal of Nylea") || inZone(p0.Battlefield, "Ordeal of Nylea") {
		t.Errorf("Ordeal of Nylea hand %v battlefield %v", p0.Hand, p0.Battlefield)
	}
}

// TestTapCombatBlockerHalfOfFlanking pins the one served-by-recipe row gorge
// cannot fire: Skewer Slinger's "blocks a creature" slot. The recipe is the
// same as Doran's block, but gorge keeps the blocker half of
// AttackerBlockedByCreature inert (rules/trigger_blocks.go
// attackerBlockedByPairCandidates), so the fire probe skips it. When the
// engine learns the half, this item is served and the test accepts that.
func TestTapCombatBlockerHalfOfFlanking(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("Skewer Slinger")
	if !ok {
		t.Fatal("Skewer Slinger not in the corpus")
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != "trigger#0.0" {
			continue
		}
		if r.Sub != "trigger.blocks" || r.Gap != "" {
			t.Fatalf("classification = %+v, want an admitted trigger.blocks", r)
		}
		it, skip := GenerateB(reg, "Skewer Slinger", r)
		switch {
		case skip != nil && skip.Reason != "trigger did not fire":
			t.Fatalf("skip = %q, want the fire probe's own \"trigger did not fire\"", skip.Reason)
		case skip == nil && !triggerShownOnStack(t, reg, it.Scenario, "Skewer Slinger"):
			t.Fatal("served without the trigger on the stack")
		}
		return
	}
	t.Fatal("Skewer Slinger has no trigger#0.0")
}
