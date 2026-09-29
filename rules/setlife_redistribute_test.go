package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSetLifeRedistributeReverseTheSands(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngineThree(t, reg, []*cards.Card{lookup(t, reg, "Reverse the Sands")}, nil, nil)
	card := searchMoveByName(t, e, "Reverse the Sands", state.ZHand)
	addMana(t, e, 0, "WWCCCCCC")
	e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life = 20, 5, 2
	if e.G.Players[0].Life == e.G.Players[1].Life || e.G.Players[0].Life == e.G.Players[2].Life || e.G.Players[1].Life == e.G.Players[2].Life {
		t.Fatal("precondition: seats must have distinct totals")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority to cast")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == card {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Reverse the Sands not castable: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	for n := 0; n < 6; n++ {
		d = e.Pending()
		if d != nil && d.Kind == decision.KChoose {
			break
		}
		if d == nil || d.Kind != decision.KPriority || len(e.G.Stack) == 0 {
			t.Fatalf("no redistribution ask while spell is on stack: %+v", d)
		}
		submitChoices(t, e, 0) // pass priority to resolve the spell
	}
	// A three-cycle makes every recipient's assigned total differ from their
	// original, so neither an identity-only nor a two-seat swap can pass.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" || d.Min != 0 || d.Max != 3 {
		t.Fatalf("subset ask: %+v", d)
	}
	choose := func(want state.PlayerID) int {
		t.Helper()
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending assignment")
		}
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == want {
				return o.Index
			}
		}
		t.Fatalf("player %d not offered: %+v", want, d.Options)
		return -1
	}
	submitChoices(t, e, choose(0), choose(1), choose(2))
	for recipient, src := range []state.PlayerID{1, 2, 0} {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" || d.Min != 1 || d.Max != 1 || d.ResumeTarget != recipient+1 {
			t.Fatalf("assignment %d ask: %+v", recipient, d)
		}
		submitChoices(t, e, choose(src))
	}
	passUntilStackEmpty(t, e, 40)
	if e.G.Players[0].Life != 5 || e.G.Players[1].Life != 2 || e.G.Players[2].Life != 20 {
		t.Fatalf("Reverse the Sands: life totals = (%d,%d,%d), want (5,2,20)", e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[2].Life)
	}
}
