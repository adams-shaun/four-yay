package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTurnDrawDredgeOnlyFromGraveyard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	thug, ok := reg.Lookup("Golgari Thug")
	if !ok {
		t.Fatal("Golgari Thug missing from corpus")
	}
	deck := append([]*cards.Card{thug}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 244, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var tid state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Golgari Thug" {
				tid = id
			}
		}
	}
	if tid == 0 {
		t.Fatal("Golgari Thug was not dealt")
	}
	from := e.G.Obj(tid).Zone
	if from != state.ZHand && from != state.ZLibrary {
		t.Fatalf("precondition: Thug starts in unexpected zone %v", from)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: from, To: state.ZBattlefield})
	if o := e.G.Obj(tid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Thug not on battlefield: %+v", o)
	}
	if len(e.G.Zone(state.ZBattlefield, 0)) == 0 || len(e.G.Zone(state.ZGraveyard, 0)) != 0 {
		t.Fatalf("precondition zones not distinct: battlefield=%d graveyard=%d", len(e.G.Zone(state.ZBattlefield, 0)), len(e.G.Zone(state.ZGraveyard, 0)))
	}

	driveToStep(t, e, 3, 0, state.StepUpkeep)
	since := len(e.L.Events)
	driveToStep(t, e, 3, 0, state.StepDraw)
	if d := e.Pending(); d != nil && d.Kind == decision.KModes && d.ResumeKind == "dredge" {
		t.Fatalf("battlefield Thug incorrectly offered dredge: %+v", d)
	}
	drawAt := -1
	for i := since; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.StepChange && e.L.Events[i].Step == state.StepDraw {
			drawAt = i
			break
		}
	}
	if drawAt < 0 {
		t.Fatal("no StepChange{StepDraw} after upkeep")
	}
	draws := 0
	for _, ev := range e.L.Events[drawAt+1:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			t.Fatalf("draw handler did not complete normally: %+v", ev)
		}
	}
	if draws != 1 {
		t.Fatalf("ordinary turn draw events = %d, want 1", draws)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after ordinary draw pending = %+v, want priority", d)
	}
	replayCheck(t, e, cfg)
}
