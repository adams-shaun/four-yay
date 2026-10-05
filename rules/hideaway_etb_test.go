package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestClivesHideawayETBTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	clive, ok := reg.Lookup("Clive's Hideaway")
	if !ok {
		t.Fatal("Clive's Hideaway missing from corpus")
	}
	if d := clive.Link(); len(d) != 0 {
		t.Fatalf("link Clive's Hideaway: %v", d)
	}
	deck := make([]*cards.Card, 40)
	for i := range deck {
		deck[i] = clive
	}
	e := New(seatZeroStart(Config{Seed: 818, Names: []string{"clive", "other"}, Decks: [][]*cards.Card{deck, deck}}))
	id := e.G.Objs[0].ID
	if got := e.G.Obj(id).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: Clive's Hideaway zone = %s, want library", got)
	}
	libraryBefore := len(e.G.Zone(state.ZLibrary, 0))
	if libraryBefore < 5 {
		t.Fatalf("precondition: library has %d cards, need land plus four Hideaway cards", libraryBefore)
	}
	if got := len(e.G.Zone(state.ZExile, 0)); got != 0 {
		t.Fatalf("precondition: exile has %d cards, want empty", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	e.putTriggersOnStack()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Clive's Hideaway entry = %+v, want battlefield while trigger is pending", o)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("Clive's Hideaway ETB trigger was not put on the stack")
	}
	if got := len(e.G.Zone(state.ZExile, 0)); got != 0 {
		t.Fatalf("Hideaway exiled %d cards before its trigger resolved", got)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libraryBefore-1 {
		t.Fatalf("library before trigger resolution = %d, want %d (only Clive's Hideaway left)", got, libraryBefore-1)
	}

	kr6ResolveTop(e)
	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose || len(pick.Options) != 4 {
		t.Fatalf("Hideaway pick = %+v, want four-card choice after trigger resolution", pick)
	}
	chosen := pick.Options[1].Obj
	if err := e.Submit(decision.Intent{Seq: pick.Seq, Player: pick.Player, Choices: []int{pick.Options[1].Index}}); err != nil {
		t.Fatalf("submit Hideaway choice: %v", err)
	}
	bottom := e.Pending()
	if bottom == nil || bottom.Kind != decision.KArrange || len(bottom.Options) != 3 {
		t.Fatalf("Hideaway bottom order = %+v, want three-card arrangement", bottom)
	}
	if err := e.Submit(decision.Intent{Seq: bottom.Seq, Player: bottom.Player, Choices: []int{2, 0, 1}}); err != nil {
		t.Fatalf("submit Hideaway bottom order: %v", err)
	}

	exiled := e.G.Zone(state.ZExile, 0)
	if len(exiled) != 1 || exiled[0] != chosen {
		t.Fatalf("Hideaway exile = %v, want chosen card %d", exiled, chosen)
	}
	if o := e.G.Obj(chosen); o == nil || !o.FaceDown {
		t.Fatalf("chosen Hideaway card = %+v, want face-down exile", o)
	}
	if got, want := len(e.G.Zone(state.ZLibrary, 0)), libraryBefore-5+3; got != want {
		t.Fatalf("library after Hideaway = %d, want %d after looking at four and bottoming three", got, want)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("Clive's Hideaway zone after resolution = %s, want battlefield", e.G.Obj(id).Zone)
	}
}
