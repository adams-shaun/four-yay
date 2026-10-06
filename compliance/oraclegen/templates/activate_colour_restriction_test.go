package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// XMage does not model every "add mana of a chosen colour" ability as one
// colour dialog. Two shapes that are NOT that dialog were scripted a colour
// anyway and made stable HARNESS rows out of main's agree rows
// (driver-batch-20261006T105913Z):
//
//   - a multi-unit "any combination of colors" allocation (Baxter Building's
//     "Add four mana in any combination of colors"): XMage asks a multi-amount
//     distribution among colours, and a joined "^" colour selection throws
//     "Missing choice in multi amount";
//   - an ability whose dynamic amount resolved to zero (Three Tree City with
//     no creature of the chosen type): XMage poses no colour dialog at all,
//     and the queued colour is consumed by an unrelated dialog and throws
//     "Choice key [White] not found in []".
//
// Both must script no colour; the plain single-choice shapes (Crystal Grotto,
// Ronin, Temur) still do (activate_colour_choice_test.go).

// TestActivateMultiUnitColourManaScriptsNoColour pins Baxter Building's
// four-unit colour allocation: gauge offers 4 picks of ONE colour each, XMage
// asks a distribution, so no joined colour answer may be scripted. The
// precondition (pool non-empty, a multi-pick allocation) proves the empty
// answer is the multi-unit rule firing, not the zero-mana rule. Thornvault
// Forager ("Add two mana in any combination of colors", Amount$ 2) is the
// sibling the same rule must cover.
func TestActivateMultiUnitColourManaScriptsNoColour(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, c := range []struct {
		card, key string
		units     int
	}{
		{"Baxter Building", "activate#0.1", 4},
		{"Thornvault Forager", "activate#0.1", 2},
	} {
		it, req := activateRequirement(t, reg, c.card, c.key)
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Snapshots) < 2 {
			t.Fatalf("%s: precondition: scenario must play through gorge", c.card)
		}
		pool := res.Snapshots[1].Players[0].Pool
		if len(pool) < 2 {
			t.Fatalf("%s: precondition: pool %q must be several mana (the multi-unit allocation produced something)", c.card, pool)
		}
		// The ability allocates one unit per colour: an engine that allocated a
		// single colour would make this test pass for the wrong reason.
		multi := false
		for _, d := range res.Decisions {
			if d.Step < 0 || d.Kind != "choose_n" || len(d.Picks) != c.units || d.Min != c.units || d.Max != c.units {
				continue
			}
			multi = true
		}
		if !multi {
			t.Fatalf("%s: precondition: no %d-pick colour allocation decision (decisions %+v); the shape this test guards is gone", c.card, c.units, res.Decisions)
		}
		if got := colourAnswers(it); len(got) != 0 {
			t.Errorf("%s %s: colour answers = %v, want none (XMage asks a multi-amount distribution, not one colour dialog)", c.card, req.Key, got)
		}
	}
}

// TestActivateZeroManaColourManaScriptsNoColour pins the zero-mana class: a
// dynamic amount that resolved to nothing still posed a colour choice in
// gorge, but XMage never asks, so a queued colour is an error. Each case's
// precondition (empty pool) is what makes the empty answer the zero-mana rule.
func TestActivateZeroManaColourManaScriptsNoColour(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, c := range []struct{ card, key string }{
		{"Three Tree City", "activate#0.1"},   // Amount$ X over chosen creature type
		{"Astral Cornucopia", "activate#0.0"}, // Amount$ Y over charge counters
		{"Prismatic Geoscope", "activate#0.0"},
		{"Vivi Ornitier", "activate#0.0"}, // Combo U R, Amount$ X over power
		{"White Lotus Tile", "activate#0.0"},
	} {
		it, _ := activateRequirement(t, reg, c.card, c.key)
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Snapshots) < 2 {
			t.Fatalf("%s: precondition: scenario must play through gorge", c.card)
		}
		if pool := res.Snapshots[1].Players[0].Pool; pool != "" {
			t.Fatalf("%s: precondition: pool %q must be empty (the dynamic amount resolved to zero)", c.card, pool)
		}
		// The activation must really have asked a colour in gorge, or this
		// test would pass with the colour routing absent entirely.
		asked := false
		for _, d := range res.Decisions {
			for _, p := range d.Picks {
				if strings.Contains(p, "Add W") || strings.Contains(p, "Add U") ||
					strings.Contains(p, "Add B") || strings.Contains(p, "Add R") ||
					strings.Contains(p, "Add G") {
					asked = true
				}
			}
		}
		if !asked {
			t.Fatalf("%s: precondition: gorge posed no colour pick (decisions %+v)", c.card, res.Decisions)
		}
		if got := colourAnswers(it); len(got) != 0 {
			t.Errorf("%s %s: colour answers = %v, want none (gorge produced no coloured mana, so XMage asks no colour)", c.card, c.key, got)
		}
	}
}

// TestActivateSingleColourManaStillScriptsColour is the complement: a fixed
// single-colour choice that DID produce mana keeps its answer, so the two
// restrictions above cannot silently suppress the whole class.
func TestActivateSingleColourManaStillScriptsColour(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, c := range []struct{ card, key string }{
		{"Crystal Grotto", "activate#0.1"},
		{"Ronin, Shadow Stalker", "activate#0.0"},
		{"Temur Devotee", "activate#0.0"},
	} {
		it, _ := activateRequirement(t, reg, c.card, c.key)
		res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
		if !ok || len(res.Snapshots) < 2 {
			t.Fatalf("%s: precondition: scenario must play through gorge", c.card)
		}
		pool := res.Snapshots[1].Players[0].Pool
		if pool == "" || colourNames[pool[0]] == "" {
			t.Fatalf("%s: precondition: pool %q must hold at least one coloured mana", c.card, pool)
		}
		got := colourAnswers(it)
		if len(got) != 1 || got[0] != colourNames[pool[0]] {
			t.Errorf("%s %s: colour answers = %v, want exactly [%s] (gorge's pool %q)", c.card, c.key, got, colourNames[pool[0]], pool)
		}
	}
}
