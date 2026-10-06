package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestXMageKnownRejectsNonXMageProbes: with the manifests' names installed, a
// card XMage's database lacks (Un-set, Alchemy, Commander-only) is not a legal
// probe, and a card it holds, in any accent or split spelling, is.
func TestXMageKnownRejectsNonXMageProbes(t *testing.T) {
	// The names are what the manifests hold: one printing per entry.
	SetXMageKnown([]string{"Grizzly Bears", "Pyre-Sledge Arsonist", "Extremis Elite", "Dain Ironfoot", "Fire // Ice"})
	t.Cleanup(func() { SetXMageKnown(nil) })

	for _, n := range []string{"Disguise Agent", "1996 World Champion", `"Lifetime" Pass Holder`, "A-Pyre-Sledge Arsonist", "A Golden Opportunity", "Adorable Kitten", ""} {
		if XMageKnown(n) {
			t.Errorf("XMageKnown(%q) = true, want false: XMage's database has no such card", n)
		}
	}
	for _, n := range []string{"Grizzly Bears", "Pyre-Sledge Arsonist", "Extremis Elite", "Dáin Ironfoot", "Fire", "Ice"} {
		if !XMageKnown(n) {
			t.Errorf("XMageKnown(%q) = false, want true", n)
		}
	}
}

// TestXMageKnownFallsBackToNameHeuristic: with no set installed (unit tests
// that never loaded the manifests) a name is known unless it is an Alchemy
// rebalance or quoted, the two shapes a deck list never holds.
func TestXMageKnownFallsBackToNameHeuristic(t *testing.T) {
	SetXMageKnown(nil)
	for n, want := range map[string]bool{"Grizzly Bears": true, "Disguise Agent": true, "A-Pyre-Sledge Arsonist": false, `"Lifetime" Pass Holder`: false, "": false} {
		if got := XMageKnown(n); got != want {
			t.Errorf("fallback XMageKnown(%q) = %v, want %v", n, got, want)
		}
	}
}

// TestXMageKnownRegistryPickersSkipUnknown: the corpus-order pickers return the
// first card XMage holds. "A Golden Opportunity" sorts and lists first, as the
// Alchemy Saga that Rydia's and Guru Pathik's scenarios once placed.
func TestXMageKnownRegistryPickersSkipUnknown(t *testing.T) {
	SetXMageKnown([]string{"Real Saga", "Real Elf", "Real Equipment"})
	t.Cleanup(func() { SetXMageKnown(nil) })
	mk := func(name string, types ...string) *cards.Card {
		return &cards.Card{Faces: []*cards.Face{{Name: name, Types: types}}}
	}
	reg := cards.NewRegistry()
	reg.Add(mk("A Golden Opportunity", "Enchantment", "Saga"))
	reg.Add(mk("Real Saga", "Enchantment", "Saga"))
	reg.Add(mk("Adorable Kitten", "Creature", "Elf"))
	reg.Add(mk("Real Elf", "Creature", "Elf"))
	reg.Add(mk("Un Equipment", "Artifact", "Equipment"))
	reg.Add(mk("Real Equipment", "Artifact", "Equipment"))

	if got, ok := registrySubtype(reg, "Saga"); !ok || got != "Real Saga" {
		t.Errorf("registrySubtype Saga = %q,%v, want Real Saga", got, ok)
	}
	if got, ok := registryQuietSubtype(reg, "Elf"); !ok || got != "Real Elf" {
		t.Errorf("registryQuietSubtype Elf = %q,%v, want Real Elf", got, ok)
	}
	if got, ok := registryCardType(reg, "Equipment"); !ok || got != "Real Equipment" {
		t.Errorf("registryCardType Equipment = %q,%v, want Real Equipment", got, ok)
	}
	if got, ok := registryEquipment(reg); !ok || got != "Real Equipment" {
		t.Errorf("registryEquipment = %q,%v, want Real Equipment", got, ok)
	}
}
