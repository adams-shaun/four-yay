package main

import (
	"bytes"
	"reflect"
	"testing"

	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestSpellbenchPooledPlayerMatchesFresh(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	deck, err := spellbench.Deck(reg, spellbench.PauperKernel, "Burn")
	if err != nil {
		t.Fatal(err)
	}
	g := sbGame{id: "spare-equivalence", seed: sbGameSeed(20260926, 1, 2), deck: "Burn", seats: [2]string{"sb-first", "sb-heuristic"}}
	fresh := sbPlayWithPool(g, deck, reg, 60, 20000, nil)
	var pool gbench.SparePool
	pooled := sbPlayWithPool(g, deck, reg, 60, 20000, &pool)
	if fresh.err != nil || pooled.err != nil {
		t.Fatalf("game error: fresh=%v pooled=%v", fresh.err, pooled.err)
	}
	if fresh.outcome.Turns == 0 {
		t.Fatal("precondition: spellbench game did not advance")
	}
	if fresh.outcome != pooled.outcome || !reflect.DeepEqual(fresh.stats, pooled.stats) ||
		!reflect.DeepEqual(fresh.mullAsks, pooled.mullAsks) || !reflect.DeepEqual(fresh.mullTaken, pooled.mullTaken) ||
		fresh.fallbacks != pooled.fallbacks || fresh.visits != pooled.visits || !bytes.Equal(fresh.corpus, pooled.corpus) {
		t.Fatalf("spellbench pooled output differs: fresh=%+v pooled=%+v", fresh, pooled)
	}
}
