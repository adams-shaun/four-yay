package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Ninja Teen's level-3 permission is a distinct Sneak path: a creature in its
// controller's graveyard may be cast only as Spell.Sneak, using the granted
// {3}{B} cost during declare blockers. Exercise the Class grant, may-play
// permission, zone-specific keyword, payment and cast as one flow.
func TestNinjaTeenLevelThreeSneaksCreatureFromGraveyard(t *testing.T) {
	reg := searchTestRegistry(t)
	// Include the inline Haste Bear that attackWithBear seeds from the deck.
	deck := []*cards.Card{searchCorpusCard(t, reg, "Ninja Teen"), searchCorpusCard(t, reg, "Grizzly Bears"), card(t, ninjutsuBearSrc)}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Forest"))
	}
	cfg := seatZeroStart(Config{Seed: 702190, Names: []string{"sneak", "defender"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	class := classMove(t, e, "Ninja Teen", state.ZBattlefield)
	if got := e.G.Obj(class).ClassLevel(); got != 1 {
		t.Fatalf("precondition: Ninja Teen level=%d, want 1", got)
	}
	addMana(t, e, 0, "B")
	classLevelUp(t, e, class, 0)
	addMana(t, e, 0, "B")
	classLevelUp(t, e, class, 1)
	if got := e.G.Obj(class).ClassLevel(); got != 3 {
		t.Fatalf("precondition: Ninja Teen level=%d, want 3", got)
	}

	bearCard := classMove(t, e, "Grizzly Bears", state.ZGraveyard)
	if o := e.G.Obj(bearCard); o == nil || o.Zone != state.ZGraveyard || e.G.Obj(class).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ninja Teen/Class or graveyard creature missing: class=%+v card=%+v", e.G.Obj(class), o)
	}
	attacker := attackWithBear(t, e)
	fundPool(t, e, "CCCB") // exactly the granted {3}{B}, not the Bears' {1}{G}

	opt := castByName(t, e, 0, "Grizzly Bears")
	if opt == nil || opt.Mode != "sneak" {
		t.Fatalf("Ninja Teen's level-3 permission did not offer the graveyard Sneak cast: %+v", castOptions(t, e))
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Sneak did not ask for its return cost: %+v", d)
	}
	chosen := -1
	for _, candidate := range d.Options {
		if candidate.Obj == attacker {
			chosen = candidate.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("unblocked attacker absent from graveyard Sneak return choices: %+v", d.Options)
	}
	submitChoices(t, e, chosen)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(bearCard); got == nil || got.Zone != state.ZBattlefield || !got.Tapped || !got.IsAttacking || got.Attacking != 1 || got.CastFlags&state.FlagSneaked == 0 {
		t.Fatalf("graveyard Sneak result = %+v, want battlefield tapped/attacking seat 1 with FlagSneaked", got)
	}
	if got := e.G.Obj(attacker); got == nil || got.Zone != state.ZHand {
		t.Fatalf("returned attacker = %+v, want hand", got)
	}
	replayCheck(t, e, cfg)
}
