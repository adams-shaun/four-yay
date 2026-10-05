package oraclegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// combatCensusSets are the sets the combat-fixture ratchet names by card.
// They are the fixture census's audit sets plus EOE, whose only
// attacking/blocking spell (Focus Fire) the fixtureCensus list predates.
var combatCensusSets = append(append([]string(nil), censusSets...), "EOE")

// combatCorpusCount pins, corpus-wide, how many nonland named-cost spell faces
// demand a creature that is attacking, blocking or tapped. OppCtrl's
// attacking-or-blocking OR filters are bucketed as blocking: their attacking
// branch cannot be met on p0's turn, but their blocking branch can. A new
// carrier anywhere in .cards/cardsfolder changes its bucket and fails loudly.
// The audit-set names below are pinned alongside it so a new carrier in an
// audited set is named, not just counted.
//
// Measured 2026-10-05 after combat planning learned to choose a feasible OR
// alternative for controller-constrained filters. A raw
// `/usr/bin/grep -rlE 'ValidTgts\$...attacking|blocking' .cards/cardsfolder`
// counts 378 files and 77 tapped because it sees activated abilities and
// non-spell targets too; the registry scan here counts the spell faces by
// demanded-state bucket (103 / 13 / 34).
var combatCorpusCount = map[string]int{
	"attacking": 103,
	"blocking":  13,
	"tapped":    34,
}

// combatFixtureCensus pins, per demanded state, the exact audit-set faces
// whose target slots name a creature that must be attacking, blocking or
// tapped. The bucket is derived from the slot predicates: "attacking" when a
// slot names an attacking creature (a comma-alternative is an OR, and an
// attacker satisfies "attacking or blocking"), "blocking" only when a slot
// names blocking and none names attacking, "tapped" when the demand is a tap
// and not a combat role.
//
// The list may only shrink as cards leave the sets; a new carrier must be
// added deliberately, which is the point -- it fails first.
var combatFixtureCensus = map[string][]string{
	"attacking": {
		"Airbender's Reversal",
		"Cosmium Blast",
		"Dreadmaw's Ire",
		"Elspeth's Smite",
		"Focus Fire",
		"Helicarrier Strike",
		"Joust Through",
		"Kellan's Lightblades",
		"Lagoon Breach",
		"Not on My Watch",
		"Osseous Exhale",
		"Protective Response",
		"Razor Rings",
		"Sonar Strike",
		"Steer Clear",
		"Sudden Strike",
	},
	"tapped": {
		"Deadly Riposte",
		"Eriette's Lullaby",
		"Keep Out",
		"Radiant Strike",
		"Rip the Seams",
	},
}

// demandedState classifies a face's target slots into the combatFixtureCensus
// bucket, or "" for a face that needs no combat or tap arrangement.
func demandedState(slots []string) string {
	attacking, blocking, tapped := false, false, false
	for _, s := range slots {
		role, tap := filterCombat(s)
		switch role {
		case roleAttacker:
			attacking = true
		case roleBlocker:
			blocking = true
		}
		tapped = tapped || tap
	}
	switch {
	case attacking:
		return "attacking"
	case blocking:
		return "blocking"
	case tapped:
		return "tapped"
	default:
		return ""
	}
}

// combatManifestNames loads the card names each combatCensusSets manifest
// lists, lowercased, so a face can be scoped to the audited sets.
func combatManifestNames(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, set := range combatCensusSets {
		raw, err := os.ReadFile(filepath.Join("..", "..", "compliance", "manifests", set+".json"))
		if err != nil {
			t.Fatalf("manifest %s: %v", set, err)
		}
		var m struct {
			Cards []struct {
				Name string `json:"name"`
			} `json:"cards"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("manifest %s: %v", set, err)
		}
		names := map[string]bool{}
		for _, c := range m.Cards {
			names[strings.ToLower(strings.TrimSpace(c.Name))] = true
		}
		out[set] = names
	}
	return out
}

// combatCensus scans every nonland named-cost spell face in the audit sets and
// buckets those whose target slots demand an attacking, blocking or tapped
// creature. inSet is the optional set-name filter (nil = corpus-wide), keyed
// on the front face's catalogue-ish name.
func combatCensus(t *testing.T, inSet map[string]map[string]bool) map[string][]string {
	t.Helper()
	reg := censusRegistry(t)
	out := map[string][]string{}
	for i := range reg.Cards {
		c := reg.Cards[i]
		if inSet != nil {
			name := strings.ToLower(strings.TrimSpace(c.Faces[0].Name))
			matched := false
			for _, names := range inSet {
				if names[name] {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		for fi := range c.Faces {
			f := c.Faces[fi]
			if hasType(f, "Land") || strings.TrimSpace(f.ManaCost) == "" || strings.EqualFold(f.ManaCost, "no cost") {
				continue
			}
			if state := demandedState(TargetSlots(f)); state != "" {
				out[state] = append(out[state], f.Name)
			}
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// TestCombatFixtureCensus is the ratchet: the audit-set faces that demand a
// combat or tapped target are pinned per bucket. A new carrier fails and is
// named.
func TestCombatFixtureCensus(t *testing.T) {
	got := combatCensus(t, combatManifestNames(t))
	for state, want := range combatFixtureCensus {
		if strings.Join(got[state], "|") != strings.Join(want, "|") {
			t.Errorf("combat fixture: %s\n got %v\nwant %v", state, got[state], want)
		}
	}
	for state, g := range got {
		if _, pinned := combatFixtureCensus[state]; !pinned {
			t.Errorf("combat fixture: %s %v (not pinned; add it deliberately)", state, g)
		}
	}
}

// TestCombatFixtureCorpusCount pins the corpus-wide carrier count per bucket,
// so a carrier outside the audited sets fails loudly too.
func TestCombatFixtureCorpusCount(t *testing.T) {
	got := combatCensus(t, nil)
	for state, want := range combatCorpusCount {
		if len(got[state]) != want {
			t.Errorf("corpus combat fixture: %s = %d, want %d; carriers:\n%v", state, len(got[state]), want, got[state])
		}
	}
}

// TestCombatFixtureCensusArranges proves the census is not just a list: for
// every pinned carrier, the fixture for the face carrying the demand names an
// attacker (and, for a blocking demand, a blocker), so the scenario can
// declare combat.
func TestCombatFixtureCensusArranges(t *testing.T) {
	reg := censusRegistry(t)
	for state, names := range combatFixtureCensus {
		if state == "tapped" {
			continue // a tap is arrangement in setup, not a combat step
		}
		for _, name := range names {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				t.Errorf("%s: not in corpus", name)
				continue
			}
			// The demand may sit on a face other than the front (an
			// adventure's back face, Lagoon Breach). Check the face that
			// actually carries it.
			checked := false
			for fi := range c.Faces {
				f := c.Faces[fi]
				if demandedState(TargetSlots(f)) != state {
					continue
				}
				checked = true
				fxs := fixtures(TargetSlots(f))
				arranged := false
				for _, fx := range fxs {
					if fx.Attacker() == "" {
						continue
					}
					if state == "blocking" && fx.Blocker() == "" {
						continue
					}
					arranged = true
					break
				}
				if !arranged {
					t.Errorf("%s (%s): fixture does not arrange a %s creature: %+v", name, f.Name, state, fxs)
				}
			}
			if !checked {
				t.Errorf("%s: no face carries the pinned %s demand", name, state)
			}
		}
	}
}
