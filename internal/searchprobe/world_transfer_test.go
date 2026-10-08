package searchprobe

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestMaterializeWorldsTransfersUniqueAndClonesRepeats(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	names := testutil.LegacyDeckNames()
	cfg := rules.Config{
		Seed: 3, Names: []string{names[0], names[1]},
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])},
		Tokens: reg.Tokens, NameUniverse: reg.Universe(),
	}
	mk := func() World {
		return World{Engine: rules.New(cfg), Observer: NewCollector(0)}
	}
	p0, p1 := mk(), mk()

	// Unique selections transfer the proposal's own engine.
	got := materializeWorlds([]World{p0, p1}, []int{0, 1})
	if got[0].Engine != p0.Engine || got[1].Engine != p1.Engine {
		t.Fatalf("unique selections must transfer the engine, not clone")
	}

	// A repeat clones, so the two returned worlds never alias one engine.
	rep := materializeWorlds([]World{p0}, []int{0, 0})
	if rep[0].Engine == rep[1].Engine {
		t.Fatalf("a repeated proposal must yield independent engines")
	}
	if rep[0].Engine.L.Head() != rep[1].Engine.L.Head() {
		t.Fatalf("cloned world head %s != original %s", rep[1].Engine.L.Head(), rep[0].Engine.L.Head())
	}
}
