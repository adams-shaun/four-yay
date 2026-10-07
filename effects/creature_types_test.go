package effects

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// nonCreatureTypeTokens are the card/supertype words and the non-creature
// subtype tokens which can share a type line with Creature in Forge's corpus.
// The corpus validation below requires every other Creature-line token to be
// in CreatureTypeWords, so a new creature type cannot be silently omitted.
var nonCreatureTypeTokens = map[string]bool{
	"-": true, // malformed placeholder in Gandalf, Shadows' Foe

	"Artifact": true, "Battle": true, "Conspiracy": true, "Creature": true,
	"Dungeon": true, "Enchantment": true, "Instant": true, "Kindred": true,
	"Land": true, "Phenomenon": true, "Plane": true, "Planeswalker": true,
	"Scheme": true, "Sorcery": true, "Tribal": true, "Vanguard": true,

	"Basic": true, "Eladamri": true, "Host": true, "Legendary": true,
	"Ongoing": true, "Snow": true, "World": true,

	// These are non-creature subtypes on artifact, land, or enchantment
	// creatures in the corpus. In particular, they must not turn a Changeling
	// into a Food, Equipment, or Shrine.
	"Background": true, "Book": true, "Clue": true, "Equipment": true,
	"Food": true, "Forest": true, "Island": true, "Mountain": true,
	"Saga": true, "Shrine": true, "Treasure": true,
}

func TestCreatureTypeWordsCoverCorpusCreatureTypes(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	cardsToCheck := append([]*cards.Card(nil), r.AllCards()...)
	keys := make([]string, 0, len(r.Tokens))
	for key := range r.Tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cardsToCheck = append(cardsToCheck, r.Tokens[key])
	}

	missing := map[string]bool{}
	for _, c := range cardsToCheck {
		for _, f := range c.Faces {
			if !f.IsCreature() {
				continue
			}
			for _, typ := range f.Types {
				if !nonCreatureTypeTokens[typ] && !CreatureTypeWords(typ) {
					missing[typ] = true
				}
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	words := make([]string, 0, len(missing))
	for typ := range missing {
		words = append(words, typ)
	}
	sort.Strings(words)
	t.Fatalf("CreatureTypeWords omits corpus creature type tokens: %v", words)
}
