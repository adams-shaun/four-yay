package templates

import (
	"strings"
	"testing"
)

func TestActivateCostTokenShapes(t *testing.T) {
	cases := []struct {
		cost, wantPool, wantGap string
	}{
		{"2 T Discard<1/Card>", "CC", ""},
		{"1 T Discard<1/Card.Legendary/legendary>", "C", ""},
		{"2 T Sac<1/Artifact.Other/another artifact>", "CC", ""},
		{"5 T Exile<1/CARDNAME>", "CCCCC", ""},
		{"tapXType<2/Artifact>", "", ""},
		{"Return<1/CARDNAME>", "", "Return<...>"},
	}
	for _, tc := range cases {
		pool, gap := activationCost(tc.cost)
		if pool != tc.wantPool || gap != tc.wantGap {
			t.Errorf("activationCost(%q) = (%q, %q), want (%q, %q)", tc.cost, pool, gap, tc.wantPool, tc.wantGap)
		}
	}
}

func TestActivateCostFixtureShapes(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, key, prefix, fixture, zone string
	}{
		{"Hallway Heckler", "activate#0.0", "{T}, Discard a card", "Wastes", "hand"},
		{"Liliana the Faultless", "activate#0.0", "{1}, {T}, Discard a card", "Wastes", "hand"},
		{"Murmuring Volume", "activate#0.1", "{2}, {T}, Discard a card", "Wastes", "hand"},
		{"Solitary Cell", "activate#0.0", "{1}, {T}, Discard a legendary card", "Ajani, Caller of the Pride", "hand"},
		{"Edgar, Ancient Bloodlord", "activate#0.0", "{2}, Sacrifice another creature or planeswalker", "Llanowar Elves", "battlefield"},
		{"Hungering Puppetbeast", "activate#0.0", "{1}, Sacrifice another artifact", "Ornithopter", "battlefield"},
		{"Marwyn, the Clearcutter", "activate#0.0", "{2}, {T}, Sacrifice an artifact or land", "Ornithopter", "battlefield"},
		{"The Echoverse Fulcrum", "activate#0.0", "{5}, {T}, Exile {this}", "", ""},
		{"Tenured Tethermage", "activate#0.0", "Tap two untapped artifacts you control", "Sol Ring", "battlefield"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s is missing from corpus", tc.name)
			}
			idx := 0
			if tc.name == "Murmuring Volume" {
				idx = 1
			}
			if idx >= len(card.Faces[0].Abilities) || !card.Faces[0].Abilities[idx].IsActivated() {
				t.Fatalf("precondition: %s ability %d is not activated", tc.name, idx)
			}
			assertActivateItem(t, reg, tc.name, tc.key, tc.prefix)
			it, _ := activateRequirement(t, reg, tc.name, tc.key)
			if tc.fixture != "" {
				seat := it.Scenario.Setup["p0"]
				zone := seat.Battlefield
				if tc.zone == "hand" {
					zone = seat.Hand
				}
				found := false
				for _, n := range zone {
					if n == tc.fixture {
						found = true
					}
				}
				if !found {
					t.Fatalf("fixture %q absent from p0 %s: hand=%v battlefield=%v", tc.fixture, tc.zone, seat.Hand, seat.Battlefield)
				}
				// The selected cost object must be translated for XMage too.
				matches := 0
				for _, stepAnswers := range it.XAnswers {
					for _, answer := range stepAnswers {
						if answer.Kind == "choice" && strings.EqualFold(answer.Value, tc.fixture) {
							matches++
						}
					}
				}
				if matches != 1 {
					t.Fatalf("XMage answers name selected cost fixture %q %d times, want once: %+v", tc.fixture, matches, it.XAnswers)
				}
			}
			if tc.name == "Tenured Tethermage" {
				bf := it.Scenario.Setup["p0"].Battlefield
				artifacts := 0
				for _, permanent := range bf {
					c, found := reg.Lookup(permanent)
					if found && c.Faces[0].IsArtifact() {
						artifacts++
					}
				}
				if artifacts < 2 {
					t.Fatalf("precondition: tapXType needs two artifacts, got %v", bf)
				}
			}
		})
	}
}
