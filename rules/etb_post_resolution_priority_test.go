package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func castAndAnswerEntryChoice(t *testing.T, c *cards.Card, choice int) *Engine {
	t.Helper()
	e := handEngine(t, c)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: spell %d is not in seat 0's hand: %+v", id, o)
	}
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: spell %d did not reach stack: %+v", id, o)
	}
	d := passUntilETBChoice(t, e)
	if len(d.Options) <= choice {
		t.Fatalf("entry election = %+v, want ETB choice with index %d", d, choice)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
		t.Fatalf("answer ETB election: %v", err)
	}
	return e
}

func passUntilETBChoice(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("missing decision while passing priority")
		}
		if d.Kind == decision.KChoose {
			if d.ResumeKind != "etb" {
				t.Fatalf("choice decision = %+v, want ETB", d)
			}
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("decision while passing priority = %+v", d)
		}
		pass := -1
		for i, option := range d.Options {
			if option.Kind == "pass" {
				pass = i
				break
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision has no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	t.Fatalf("ETB choice was not reached: %+v", e.Pending())
	return nil
}

func assertActiveMainPriority(t *testing.T, e *Engine) {
	t.Helper()
	if e.G.Active != 0 {
		t.Fatalf("precondition: active player = %d, want casting seat 0", e.G.Active)
	}
	if e.G.Step != state.StepMain1 {
		t.Fatalf("step after ETB election = %s, want unchanged main1", e.G.Step)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack after ETB election = %v, want empty", e.G.Stack)
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("pending after ETB election = nil, want active player's priority")
	}
	if d.Kind != decision.KPriority || d.Player != e.G.Active {
		t.Fatalf("pending after ETB election = kind %s for player %d (%q), want active player's priority", d.Kind, d.Player, d.Prompt)
	}
	if d.Player != 0 {
		t.Fatalf("priority player = %d, want caster seat 0", d.Player)
	}
}

func TestRiotPostElectionPriorityReturnsToCaster(t *testing.T) {
	riot := card(t, "Name:Riot Regression Creature\nManaCost:0\nTypes:Creature Beast\nPT:2/2\nK:Riot\nOracle:x\n")
	if !riot.Faces[0].HasKeyword("Riot") {
		t.Fatal("precondition: fixture does not carry Riot")
	}
	e := castAndAnswerEntryChoice(t, riot, 1)
	assertActiveMainPriority(t, e)
}

func TestETBChoicePostResolutionPriorityReturnsToCaster(t *testing.T) {
	prelate := corpusAlternativeCard(t, "Sanctum Prelate")
	e := handEngine(t, prelate)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Sanctum Prelate is not in hand: %+v", o)
	}
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MW] = 1, 2
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: Sanctum Prelate did not reach stack: %+v", o)
	}
	d := passUntilETBChoice(t, e)
	if len(d.Options) != 13 {
		t.Fatalf("Sanctum Prelate election = %+v, want its 13-number ETB choice", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{2}}); err != nil {
		t.Fatalf("answer Sanctum Prelate election: %v", err)
	}
	assertActiveMainPriority(t, e)
}
