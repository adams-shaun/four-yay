package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestBreenaAttackTriggerReadsLifeGTX(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	breena := searchCorpusCard(t, reg, "Breena, the Demagogue")
	var trig cards.Trigger
	found := false
	for _, f := range breena.Faces {
		for _, candidate := range f.Triggers {
			if candidate.Mode == "AttackersDeclaredOneTarget" && candidate.Params["AttackedTarget"] == "Opponent.lifeGTX" {
				trig, found = candidate, true
				break
			}
		}
	}
	if !found {
		t.Fatal("Breena AttackedTarget Opponent.lifeGTX trigger missing")
	}
	deck := make([]*cards.Card, 40)
	for i := range deck {
		deck[i] = searchCorpusCard(t, reg, "Grizzly Bears")
	}
	e := New(Config{Seed: 19, Names: []string{"Breena", "one", "two"}, Decks: [][]*cards.Card{deck, deck, deck}, Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e.Advance()
	source := e.G.AddObject(breena, 0)
	attacker := e.G.AddObject(searchCorpusCard(t, reg, "Grizzly Bears"), 2)
	if source.Zone != state.ZLibrary || attacker.Zone != state.ZLibrary {
		t.Fatalf("setup zones: source=%v attacker=%v", source.Zone, attacker.Zone)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: source.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: attacker.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life = 40, 20, 30
	if e.G.Players[2].Life <= e.G.Players[1].Life {
		t.Fatal("setup: attacked opponent must have more life than the lowest opponent")
	}
	ev := events.Event{Kind: events.DeclareAttackers, Player: 2, IDs: []state.ObjID{attacker.ID}}
	if !e.attackersDeclaredOneTargetMatches(trig, source.ID, ev) {
		ctx := &effects.Ctx{Source: source.ID, Controller: 0, SVars: source.Face().SVars}
		n, ok := effects.EvalCountOK(e, ctx, source.Face().SVars["X"])
		t.Fatalf("Breena trigger did not match: params=%v sourceSVars=%v threshold=%d/%v filter=%v alive=%v ctrl=%d attacked=%d lives=%d/%d/%d", trig.Params, source.Face().SVars, n, ok, effects.MatchesPlayerSpecWithSVars(e, ctx, "Opponent.lifeGTX", 2, 0), e.G.AliveFrom(0), e.controllerOf(source.ID), ev.Player, e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life)
	}
	e.G.Players[1].Life, e.G.Players[2].Life = 25, 25
	if e.attackersDeclaredOneTargetMatches(trig, source.ID, ev) {
		t.Fatal("Breena trigger matched when all opponents had equal life")
	}
}
