package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestFormlessGenesisRetraceFromGraveyard proves the repo-deck card can use
// Retrace: it is offered from its graveyard with mana and a land to discard,
// then the additional cost is paid and the spell reaches the stack.
func TestFormlessGenesisRetraceFromGraveyard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Formless Genesis"))
	cardID := graveyardCorpus(t, e)
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: Formless Genesis zone = %v, want graveyard", got)
	}
	if !e.G.Obj(cardID).Face().HasKeyword("Retrace") {
		t.Fatalf("setup: Formless Genesis lost Retrace keyword: %v", e.G.Obj(cardID).Face().Keywords)
	}

	landInHand := e.G.AddObject(card(t, "Name:Test Forest\nTypes:Basic Land Forest\nOracle:x\n"), 0)
	landInHand.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{landInHand.ID})
	landInGraveyard := e.G.AddObject(card(t, "Name:Test Plains\nTypes:Basic Land Plains\nOracle:x\n"), 0)
	landInGraveyard.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{cardID, landInGraveyard.ID})
	addMana(t, e, 0, "GG2")

	var retrace *decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Mode == "retrace" && o.Obj == cardID {
			copy := o
			retrace = &copy
			break
		}
	}
	if retrace == nil {
		t.Fatalf("Formless Genesis Retrace cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, retrace.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Obj != landInHand.ID {
		t.Fatalf("Retrace discard ask = %+v, want the land in hand", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Obj(landInHand.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("discarded land zone = %v, want graveyard", got)
	}
	if got := e.G.Obj(cardID).Zone; got != state.ZStack {
		t.Fatalf("Formless Genesis zone after Retrace = %v, want stack", got)
	}
}
