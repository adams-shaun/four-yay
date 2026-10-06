package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestOutlawHistoricBase(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	pirate := corpusObject(t, reg, g, "Kitesail Freebooter")
	solRing := corpusObject(t, reg, g, "Sol Ring")
	bear := corpusObject(t, reg, g, "Grizzly Bears")

	sc := SpecContext{You: 0}
	if !hasTypeCtx(pirate, "Pirate", sc) || !hasTypeCtx(solRing, "Artifact", sc) {
		t.Fatal("precondition: corpus fixtures lack Pirate/Artifact types")
	}
	if hasTypeCtx(bear, "Pirate", sc) || hasTypeCtx(bear, "Assassin", sc) ||
		hasTypeCtx(bear, "Mercenary", sc) || hasTypeCtx(bear, "Rogue", sc) || hasTypeCtx(bear, "Warlock", sc) ||
		hasTypeCtx(bear, "Artifact", sc) || hasTypeCtx(bear, "Legendary", sc) || hasTypeCtx(bear, "Saga", sc) {
		t.Fatal("precondition: Grizzly Bears must match neither batch word")
	}

	if !MatchesObjectCtx(g, "Outlaw.YouCtrl", pirate, sc) {
		t.Error("Outlaw.YouCtrl must match Kitesail Freebooter")
	}
	if !MatchesObjectCtx(g, "Historic.YouCtrl", solRing, sc) {
		t.Error("Historic.YouCtrl must match Sol Ring")
	}
	if MatchesObjectCtx(g, "Outlaw.YouCtrl", bear, sc) {
		t.Error("Outlaw.YouCtrl must not match Grizzly Bears")
	}
	if MatchesObjectCtx(g, "Historic.YouCtrl", bear, sc) {
		t.Error("Historic.YouCtrl must not match Grizzly Bears")
	}
	// Predicate spellings continue to use the same batch-word semantics.
	if !MatchesObjectCtx(g, "Card.Outlaw", pirate, sc) || !MatchesObjectCtx(g, "Card.Historic", solRing, sc) {
		t.Error("predicate spellings Card.Outlaw/Card.Historic must still match")
	}
}
