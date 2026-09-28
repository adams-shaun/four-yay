package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const undauntedSrc = "Name:Undaunted Test\nManaCost:6 G\nTypes:Sorcery\nK:Undaunted\nOracle:x\n"
const plainCostSrc = "Name:Plain Cost Test\nManaCost:6 G\nTypes:Sorcery\nOracle:x\n"

func TestSeedsOfRenewalUndaunted(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card := lookup(t, reg, "Seeds of Renewal")
	if !card.Faces[0].HasKeyword("Undaunted") {
		t.Fatal("precondition: corpus Seeds of Renewal lacks K:Undaunted")
	}
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{card}, nil)
	id := moveByName(t, e, 0, "Seeds of Renewal", state.ZHand)
	if got := reduceOf(t, e, 0, id); got != 1 {
		t.Fatalf("two-player opponent reduction = %d, want 1", got)
	}
}

func TestUndauntedReducesGenericAndNotColoredCost(t *testing.T) {
	t.Parallel()
	e, cfg, card := newFixtureDeck(t, 1705, undauntedSrc, plainCostSrc)
	o := e.G.Obj(card)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil || !o.Face().HasKeyword("Undaunted") {
		t.Fatalf("precondition: Undaunted spell not in hand with keyword: %+v", o)
	}
	if got := reduceOf(t, e, 0, card); got != 1 {
		t.Fatalf("two-player opponent reduction = %d, want 1", got)
	}
	addMana(t, e, 0, "CCCCCG")
	opt := castByName(t, e, 0, "Undaunted Test")
	if opt == nil {
		t.Fatal("spell should cast for {5}{G} with one opponent")
	}
	submitChoices(t, e, opt.Index)
	if e.G.Obj(card).Zone != state.ZStack {
		t.Fatalf("after cast object zone = %s, want stack", e.G.Obj(card).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after cast = %d, want 0", e.G.Players[0].Pool.Total())
	}

	// Undaunted reduces generic mana only: three green mana cannot pay the
	// remaining {5}{G}, even though its coloured portion is covered.
	e2, _, card2 := newFixtureDeck(t, 1706, undauntedSrc)
	addMana(t, e2, 0, "GGG")
	if got := reduceOf(t, e2, 0, card2); got != 1 {
		t.Fatalf("reduction = %d, want 1", got)
	}
	if got := castByName(t, e2, 0, "Undaunted Test"); got != nil {
		t.Fatal("spell unexpectedly cast with only three green mana")
	}
	_ = cfg
	replayCheck(t, e, cfg)
}

func TestUndauntedDoesNotReducePlainSpell(t *testing.T) {
	t.Parallel()
	e, _, plain := newFixtureDeck(t, 1707, plainCostSrc)
	if got := reduceOf(t, e, 0, plain); got != 0 {
		t.Fatalf("plain spell reduction = %d, want 0", got)
	}
}
