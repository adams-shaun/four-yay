package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestOutlawBatchWord pins Forge's Outlaw batch word against real corpus
// cards. "Assassins, Mercenaries, Pirates, Rogues, and Warlocks are outlaws"
// (the reminder text on every carrier), so the predicate is the union of
// those five creature subtypes and nothing else:
//
//   - Card.Outlaw matches a Pirate and a Rogue; Creature.!Outlaw (the
//     generic negation) matches a Bear and not the Pirate.
//   - A card with an outlaw subtype is matched through the layer-aware
//     subtype list, so an unrelated card never matches.
//   - UnknownPredicates no longer reports Outlaw -- it is a recognised word,
//     not a fail-closed hole.
//
// The two corpus carriers this unlocks are Outlaws' Fury's
// ConditionPresent$ Card.Outlaw+YouCtrl and Shoot the Sheriff's
// ValidTgts$ Creature.!Outlaw.
func TestOutlawBatchWord(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})

	pirate := corpusObject(t, reg, g, "Kitesail Freebooter") // Creature Human Pirate
	bear := corpusObject(t, reg, g, "Grizzly Bears")         // Creature Bear

	// Precondition: the two objects really differ on the outlaw subtypes the
	// predicate reads, otherwise the assertions below are vacuous.
	if !hasTypeCtx(pirate, "Pirate", SpecContext{}) {
		t.Fatalf("Kitesail Freebooter must carry the Pirate subtype")
	}
	if hasTypeCtx(bear, "Pirate", SpecContext{}) || hasTypeCtx(bear, "Rogue", SpecContext{}) {
		t.Fatalf("Grizzly Bears must carry no outlaw subtype")
	}

	if !MatchesObjectCtx(g, "Card.Outlaw", pirate, SpecContext{You: 0}) {
		t.Errorf("Card.Outlaw must match a Pirate (Kitesail Freebooter)")
	}
	if MatchesObjectCtx(g, "Card.Outlaw", bear, SpecContext{You: 0}) {
		t.Errorf("Card.Outlaw must not match a Bear (Grizzly Bears)")
	}

	// The generic negation (Shoot the Sheriff's Creature.!Outlaw): the Bear
	// is a legal non-outlaw target, the Pirate is not.
	if !MatchesObjectCtx(g, "Creature.!Outlaw", bear, SpecContext{You: 0}) {
		t.Errorf("Creature.!Outlaw must match a non-outlaw creature (Grizzly Bears)")
	}
	if MatchesObjectCtx(g, "Creature.!Outlaw", pirate, SpecContext{You: 0}) {
		t.Errorf("Creature.!Outlaw must not match a Pirate (Kitesail Freebooter)")
	}

	if unk := UnknownPredicates("Card.Outlaw"); len(unk) != 0 {
		t.Errorf("Outlaw must be a recognised predicate word, got unknown %v", unk)
	}
}
