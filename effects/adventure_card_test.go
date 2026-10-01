package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestAdventureCardPredicate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	adventure := corpusObject(t, reg, g, "Bonecrusher Giant")
	ordinary := corpusObject(t, reg, g, "Grizzly Bears")
	if adventure.Card.AlternateMode != "Adventure" || len(adventure.Card.Faces) != 2 {
		t.Fatalf("Bonecrusher Giant lacks valid Adventure shape: mode=%q faces=%d", adventure.Card.AlternateMode, len(adventure.Card.Faces))
	}
	if ordinary.Card.AlternateMode == "Adventure" {
		t.Fatal("Grizzly Bears unexpectedly has Adventure mode")
	}
	if adventure.Zone != state.ZBattlefield || ordinary.Zone != state.ZBattlefield ||
		adventure.FaceIdx != 0 || adventure.Face() == nil || !adventure.Face().IsCreature() {
		t.Fatal("filter fixtures must be front-face battlefield creatures")
	}
	if !MatchesObjectCtx(g, "Creature.AdventureCard", adventure, SpecContext{You: 0}) {
		t.Fatal("Creature.AdventureCard did not match Bonecrusher Giant")
	}
	if MatchesObjectCtx(g, "Creature.AdventureCard", ordinary, SpecContext{You: 0}) {
		t.Fatal("Creature.AdventureCard matched Grizzly Bears")
	}
	if !MatchesObjectCtx(g, "Creature.!AdventureCard", ordinary, SpecContext{You: 0}) {
		t.Fatal("Creature.!AdventureCard did not match Grizzly Bears")
	}
	if MatchesObjectCtx(g, "Creature.!AdventureCard", adventure, SpecContext{You: 0}) {
		t.Fatal("Creature.!AdventureCard matched Bonecrusher Giant")
	}
	// The card identity survives selection of the spell face. Use Card as the
	// base here: the current Adventure face is an instant, not a creature.
	spellFace := *adventure
	spellFace.SetFaceIdx(1)
	if spellFace.Face() == nil || !spellFace.Face().IsInstant() {
		t.Fatal("Bonecrusher Giant's second face must be an Adventure instant")
	}
	if !MatchesObjectCtx(g, "Card.AdventureCard", &spellFace, SpecContext{You: 0}) {
		t.Fatal("Card.AdventureCard lost the card identity on its spell face")
	}
	// Wrong mode, wrong face count, absent spell face, and a spell face with
	// no Adventure type all fail closed, even with an Adventure mode label.
	for name, card := range map[string]*cards.Card{
		"nil card":      nil,
		"wrong mode":    ordinary.Card,
		"one face":      {AlternateMode: "Adventure", Faces: adventure.Card.Faces[:1]},
		"missing face":  {AlternateMode: "Adventure", Faces: []*cards.Face{adventure.Card.Faces[0], nil}},
		"no spell type": {AlternateMode: "Adventure", Faces: []*cards.Face{adventure.Card.Faces[0], ordinary.Card.Faces[0]}},
		"no Adventure":  {AlternateMode: "Adventure", Faces: []*cards.Face{adventure.Card.Faces[0], &cards.Face{Types: []string{"Instant"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			malformed := *adventure
			malformed.SetCard(card, malformed.FaceIdx)
			if MatchesObjectCtx(g, "Creature.AdventureCard", &malformed, SpecContext{You: 0}) {
				t.Fatal("malformed or non-Adventure card matched")
			}
		})
	}
	if MatchesObjectCtx(g, "Card.AdventureCard", nil, SpecContext{You: 0}) {
		t.Fatal("nil object matched")
	}
	for _, spec := range []string{"Creature.AdventureCard", "Creature.!AdventureCard", "Card.AdventureCard"} {
		if un := UnknownPredicates(spec); len(un) != 0 {
			t.Fatalf("UnknownPredicates(%q) = %v, want empty", spec, un)
		}
	}
	if un := UnknownPredicates("Creature.!NoSuchPredicate"); len(un) != 1 || un[0] != "!NoSuchPredicate" {
		t.Fatalf("unknown negation widened: %v", un)
	}
}
