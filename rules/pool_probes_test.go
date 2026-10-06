package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestPoolGatesMatchesTheSingleProbes pins poolGates (the fused genesis pass)
// to the three single-gate walks it replaces, over every split of three
// carriers and a plain card between the decks and the token table.
func TestPoolGatesMatchesTheSingleProbes(t *testing.T) {
	plain := card(t, "Name:Plain Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	rename := card(t, "Name:Renamer\nManaCost:1 U\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Creature | SetName$ Frog | Description$ x\nOracle:x\n")
	retype := card(t, "Name:Retyper\nManaCost:1 U\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Creature | AddType$ Artifact | Description$ x\nOracle:x\n")
	control := card(t, "Name:Taker\nManaCost:1 U\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Creature.EnchantedBy | GainControl$ You | Description$ x\nOracle:x\n")
	carriers := []*cards.Card{rename, retype, control}
	if !rename.SetsName() || !retype.ChangesTypes() || !control.MayCarryControlStatic() {
		t.Fatal("precondition: a fixture carrier is not recognised by its own probe")
	}
	// mask bit i: carrier i present; deckBit i: in a deck (else a token).
	for mask := 0; mask < 8; mask++ {
		for deckBits := 0; deckBits < 8; deckBits++ {
			deck := []*cards.Card{plain}
			tokens := map[string]*cards.Card{"plain": plain}
			for i, c := range carriers {
				if mask&(1<<i) == 0 {
					continue
				}
				if deckBits&(1<<i) != 0 {
					deck = append(deck, c)
				} else {
					tokens[string(rune('a'+i))] = c
				}
			}
			cfg := Config{Decks: [][]*cards.Card{deck, {plain}}, Tokens: tokens}
			s, l, c := poolGates(cfg)
			if s != poolHasSetNameStatic(cfg) || l != poolHasLayer4Static(cfg) || c != poolHasControlStatic(cfg) {
				t.Fatalf("mask %03b decks %03b: poolGates = (%v %v %v), single probes = (%v %v %v)", mask, deckBits,
					s, l, c, poolHasSetNameStatic(cfg), poolHasLayer4Static(cfg), poolHasControlStatic(cfg))
			}
		}
	}
}
