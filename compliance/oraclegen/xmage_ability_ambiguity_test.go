package oraclegen_test

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The shapes that used to make the ordinal line/AB count disagree, each on a
// corpus face (level-B "activate xmage text ambiguous").
func TestXMageAbilityResolvesCountMismatchShapes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		// STATION / LEVEL sections grant abilities Forge keeps in an SVar.
		{"Evendo, Waking Haven", map[int]string{0: "{T}"}},
		{"Brimstone Mage", map[int]string{0: "Level up {3}{R}"}},
		{"Under-Construction Skyscraper", map[int]string{0: "{T}", 1: "Level up {1}"}},
		// "Max speed —" lines have no AB.
		{"Mutant Surveyor", map[int]string{0: "{2}"}},
		// Printed without the space after the cost colon.
		{"Alaundo the Seer", map[int]string{0: "{T}"}},
		{"Argoth, Sanctum of Nature", map[int]string{0: "{T}", 1: "{2}{G}{G}, {T}"}},
		{"Commodore Guff", map[int]string{0: "+1", 1: "-3"}},
		{"The Wandering Emperor", map[int]string{0: "+1", 1: "-1", 2: "-2"}},
		// A non-mana cost reads "Equip&mdash;Cost." in XMage (EquipAbility).
		{"Demonmail Hauberk", map[int]string{0: "Equip&mdash;Sacrifice a creature."}},
		{"Street Wraith", map[int]string{0: "Cycling&mdash;Pay 2 life."}},
		// A compound cost is printed mana-first but rendered non-mana-first
		// (EquipAbility.getRule() appends the mana cost last).
		{"My Precious", map[int]string{0: "Equip&mdash;Pay 2 life.{2}"}},
		// The word-led alternate-cost form keeps its printed order.
		{"Bloodthorn Flail", map[int]string{0: "Equip&mdash;Pay {3} or discard a card."}},
		{"Hand of Vecna", map[int]string{0: "Equip&mdash;Pay 1 life for each card in your hand.", 1: "Equip {2}"}},
		{"Shredder's Armor", map[int]string{0: "Equip&mdash;Sacrifice another nonland permanent. Activate only once each turn."}},
		{"Dark Knight's Greatsword", map[int]string{0: "<i>Chaosbringer</i> &mdash; Equip&mdash;Pay 3 life. Activate only once each turn."}},
		{"Sunscourge Champion", map[int]string{0: "Eternalize {2}{W}{W}, Discard a card"}},
		{"Phyrexian Dragon Engine", map[int]string{0: "Unearth {3}{R}{R}"}},
		// Forge omits the Equip line from these Oracle texts.
		{"Glamdring, Foe-hammer", map[int]string{0: "Equip {2}"}},
		{"Buster Sword", map[int]string{0: "Equip {2}"}},
		// One printed line lists several keyword abilities.
		{"Igneous Pouncer", map[int]string{0: "Swampcycling {2}", 1: "Mountaincycling {2}"}},
		{"Pale Recluse", map[int]string{0: "Forestcycling {2}", 1: "Plainscycling {2}"}},
		{"Blast from the Past", map[int]string{1: "Cycling {1}{R}"}},
	} {
		t.Run(tc.card, func(t *testing.T) {
			face := activatedFace(t, reg, tc.card)
			got, why := oraclegen.XMageAbility(face)
			if why != "" {
				t.Fatalf("mapping ambiguous: %s", why)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("mapped %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("ability %d prefix = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

// These stay ambiguous by design: one printed line stands for several Forge
// ABs (the selector cannot tell them apart), or XMage renders the cost in a
// form the printed line does not give.
func TestXMageAbilityStaysAmbiguousByDesign(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, card := range []string{
		"Acquisition Octopus",        // Reconfigure: attach and unattach share one line
		"Jinxed Choker",              // "{3}: Put a charge counter on it or remove one"
		"Ion Storm",                  // "Remove a +1/+1 counter or a charge counter"
		"M'Odo, the Gnarled Oracle",  // Eminence: battlefield and command-zone ABs
		"Luxior and Shadowspear",     // "Equip creature or planeswalker {3}"
		"Salvation Colossus",         // "Unearth—Pay eight {E}.": no XMage rendering known
		"Yuriko, the Tiger's Shadow", // "Commander ninjutsu"
		"Phyrexian Esthetician",      // ability-word keyword ("Oil Scavenge") with no colon line
		"Cadaverous Bloom",           // "{B}{B} or {G}{G}"
	} {
		t.Run(card, func(t *testing.T) {
			if got, why := oraclegen.XMageAbility(activatedFace(t, reg, card)); why == "" {
				t.Fatalf("expected ambiguous, mapped %v", got)
			}
		})
	}
}

func activatedFace(t *testing.T, reg *cards.Registry, name string) *cards.Face {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("precondition: %s missing from corpus", name)
	}
	face := c.Faces[0]
	for _, sa := range face.Abilities {
		if sa.IsActivated() {
			return face
		}
	}
	t.Fatalf("precondition: %s has no activated ability", name)
	return nil
}
