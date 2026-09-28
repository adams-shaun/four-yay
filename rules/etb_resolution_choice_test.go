package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestETBChoiceIsAskedAtTheEntryBoundary uses Cavern of Souls' real
// ETBReplacement ChooseType carrier. The move is proposed from the hand, so
// the object must still be in the hand while the mid-resolution-style ask is
// pending; only the answered move may put it on the battlefield.
func TestETBChoiceIsAskedAtTheEntryBoundary(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Cavern of Souls"))
	id := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatalf("precondition: Cavern zone = %s, want hand", e.G.Obj(id).Zone)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || len(d.Options) == 0 || d.Options[0].Kind != "type" {
		t.Fatalf("entry ask = %+v, want an ETB type choice", d)
	}
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("precondition after parked move: Cavern zone = %s, want hand", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == id && ev.Counter == "type" {
			t.Fatal("type choice was recorded before the entry decision was answered")
		}
	}

	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("submit ETB choice: %v", err)
	}
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.ChosenType == "" {
		t.Fatalf("after entry answer: zone=%s chosen type=%q, want battlefield and a type", o.Zone, o.ChosenType)
	}
}

// TestETBChoiceSuspendsAResolvingPermanent uses the real Sanctum Prelate
// carrier to pin the spell path too: the permanent remains on the stack while
// resolveTop is suspended, and only the answered MoveZone reaches the
// battlefield.
func TestETBChoiceSuspendsAResolvingPermanent(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Sanctum Prelate"))
	id := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatalf("precondition: Prelate zone = %s, want hand", e.G.Obj(id).Zone)
	}
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MW] = 1, 2
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	if e.G.Obj(id).Zone != state.ZStack {
		t.Fatalf("precondition: Prelate was not committed to the stack: %s", e.G.Obj(id).Zone)
	}
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || len(d.Options) != 13 || d.Options[0].Kind != "number" {
		t.Fatalf("resolution ask = %+v, want the 13-number ETB ask", d)
	}
	if e.G.Obj(id).Zone != state.ZStack {
		t.Fatalf("parked resolution moved Prelate to %s, want stack", e.G.Obj(id).Zone)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{2}}); err != nil {
		t.Fatalf("submit number: %v", err)
	}
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.ChosenNumber != 2 {
		t.Fatalf("after resolution answer: zone=%s number=%d, want battlefield / 2", o.Zone, o.ChosenNumber)
	}
}
