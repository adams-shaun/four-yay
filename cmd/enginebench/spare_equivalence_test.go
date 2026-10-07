package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestEnginebenchPooledRowsMatchFresh(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	a, err := testutil.LoadRepoDeck(reg, "mono-black-aggro")
	if err != nil {
		t.Fatal(err)
	}
	b, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	cfg := rules.Config{Seed: 77123, Names: []string{"a", "b"}, Decks: [][]*cards.Card{a, b}, Tokens: reg.Tokens}
	var randomFresh, randomPooled randomStats
	playRandomWithPool(cfg, &randomFresh, nil)
	var randomPool bench.SparePool
	playRandomWithPool(cfg, &randomPooled, &randomPool)
	if !reflect.DeepEqual(randomFresh, randomPooled) {
		t.Fatalf("random row changed with spare recycling:\nfresh=%+v\npooled=%+v", randomFresh, randomPooled)
	}
	var botFresh, botPooled botStats
	if err := playBotWithPool(cfg, false, &botFresh, nil); err != nil {
		t.Fatal(err)
	}
	var botPool bench.SparePool
	if err := playBotWithPool(cfg, false, &botPooled, &botPool); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(botFresh, botPooled) {
		t.Fatalf("bot row changed with spare recycling:\nfresh=%+v\npooled=%+v", botFresh, botPooled)
	}
	if randomFresh.Games != 1 || botFresh.Games != 1 {
		t.Fatal("precondition: row did not complete exactly one game")
	}
}
