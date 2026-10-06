package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/seat"
)

func TestPolicyTunePlayerPooledMatchesFresh(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	a, err := testutil.LoadRepoDeck(reg, "mono-black-aggro")
	if err != nil {
		t.Fatal(err)
	}
	b, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	pd := bench.PairDef{A: "a", B: "b"}
	d := &devSuite{reg: reg, pairs: []bench.PairDef{pd}, deckByName: map[string][]*cards.Card{"a": a, "b": b}, maxTurns: 60, maxIntents: 4000}
	freshPlayer := d.playerWithPool(nil)
	var pool bench.SparePool
	pooledPlayer := d.playerWithPool(&pool)
	seats := [2]seat.Seat{seat.NewBot(881), seat.NewBot(882)}
	fresh, err := freshPlayer(0, 880, 0, seats)
	if err != nil {
		t.Fatal(err)
	}
	pooled, err := pooledPlayer(0, 880, 0, seats)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Turns == 0 {
		t.Fatal("precondition: game did not advance")
	}
	if !reflect.DeepEqual(fresh, pooled) {
		t.Fatalf("policytune pooled outcome differs: fresh=%+v pooled=%+v", fresh, pooled)
	}
}
