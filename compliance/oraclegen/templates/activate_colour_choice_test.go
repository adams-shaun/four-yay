package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

var colourNames = map[byte]string{'W': "White", 'U': "Blue", 'B': "Black", 'R': "Red", 'G': "Green"}

// colourAnswers is the activate item's scripted XMage "choice" answers, in
// queue order.
func colourAnswers(it oraclegen.Item) []string {
	var out []string
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a.Kind == "choice" {
				out = append(out, a.Value)
			}
		}
	}
	return out
}

// An "any colour" mana activation hands XMage the colour gorge chose, or
// XMage picks one itself and the row flakes (Crystal Grotto replayed alone
// gave G, R, R). The expected colour is read from gorge's own pool after the
// activate step, not from the generator's answer.
func TestActivateAnyColourManaScriptsGorgesColour(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, c := range []struct{ card, key string }{
		{"Crystal Grotto", "activate#0.1"},
		{"Giant's Boulder", "activate#0.0"},
		{"Great Hall of the Biblioplex", "activate#0.1"},
		{"Hidden Grotto", "activate#0.1"},
		{"Ronin, Shadow Stalker", "activate#0.0"}, // two mana of ONE chosen colour
		{"Temur Devotee", "activate#0.0"},         // Produced$ Combo G U R
		{"Conduit Pylons", "activate#0.1"},
		{"Daily Bugle Building", "activate#0.1"},
		{"Three Tree Mascot", "activate#0.0"},
	} {
		it, _ := activateRequirement(t, reg, c.card, c.key)
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Snapshots) < 2 {
			t.Fatalf("%s: precondition: scenario must play through gorge", c.card)
		}
		pool := res.Snapshots[1].Players[0].Pool
		if pool == "" || strings.Trim(pool, pool[:1]) != "" || colourNames[pool[0]] == "" {
			t.Fatalf("%s: precondition: pool %q must be copies of one colour", c.card, pool)
		}
		got := colourAnswers(it)
		if len(got) != 1 || got[0] != colourNames[pool[0]] {
			t.Errorf("%s %s: choice answers = %v, want exactly [%s] (gorge's pool %q)", c.card, c.key, got, colourNames[pool[0]], pool)
		}
	}
}

// Only the ability that asks for a colour scripts one: Crystal Grotto's
// {T}: Add {C} poses no dialog, so a queued colour would be an unused leftover.
func TestActivateColourlessManaScriptsNoColour(t *testing.T) {
	reg := loadGenRegistry(t)
	it, _ := activateRequirement(t, reg, "Crystal Grotto", "activate#0.0")
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Snapshots) < 2 || res.Snapshots[1].Players[0].Pool != "C" {
		t.Fatalf("precondition: Crystal Grotto's {T}: Add {C} must leave pool C, got %+v", res.Snapshots)
	}
	if got := colourAnswers(it); len(got) != 0 {
		t.Errorf("colourless mana scripted choices %v, want none", got)
	}
}

// "As ~ enters, choose a color" on a setup-placed permanent: XMage asks at
// game start, before step 0, so the colour leads step 0's answers.
func TestActivateSetupChosenColourIsScripted(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, card := range []string{"Crossroads Village", "Heraldic Banner"} {
		it, _ := activateRequirement(t, reg, card, "activate#0.0")
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Snapshots) < 2 {
			t.Fatalf("%s: precondition: scenario must play through gorge", card)
		}
		pool := res.Snapshots[1].Players[0].Pool
		if len(pool) != 1 || colourNames[pool[0]] == "" {
			t.Fatalf("%s: precondition: pool %q must be one coloured mana", card, pool)
		}
		if len(it.XAnswers) == 0 || len(it.XAnswers[0]) == 0 {
			t.Fatalf("%s: no step 0 answers: %v", card, it.XAnswers)
		}
		first := it.XAnswers[0][0]
		if first.Kind != "choice" || first.Value != colourNames[pool[0]] {
			t.Errorf("%s: step 0 leads with %+v, want choice %s (gorge's pool %q)", card, first, colourNames[pool[0]], pool)
		}
	}
}
