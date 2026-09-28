// Fix round (findings-sol4 MAJOR): the draw step's turn-based draw (CR 504.1)
// can SUSPEND on a Dredge replacement ask (CR 702.55) posed through the same
// DrawFor the Draw primitive shares. advanceStep must then return without
// emitting the step's own Priority event: CR 405.1 grants priority only after
// the turn-based action completes, and the dredge answer's resume path
// (resolution.go's dredge arm -> Advance -> priorityRound) grants that one
// priority itself. Emitting one while the ask is outstanding granted TWO, and
// put a Priority event in the log before the player had answered whether to
// replace the draw at all.
//
// The probe that broke this drove an ACTUAL turn draw (upkeep -> draw through
// the turn structure, not e.drawCard directly) with Golgari Thug in its
// controller's graveyard and failed on the pre-fix code with
// "priority emitted while dredge decision is pending". This test is that
// probe, committed.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTurnDrawDredgeSuspendsBeforePriority(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	thug, ok := reg.Lookup("Golgari Thug")
	if !ok {
		t.Fatal("Golgari Thug missing from corpus")
	}
	if d := thug.Link(); len(d) != 0 {
		t.Fatalf("link Golgari Thug: %v", d)
	}
	deck := append([]*cards.Card{thug}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 242, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var tid state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == "Golgari Thug" {
				tid = id
			}
		}
	}
	if tid == 0 {
		t.Fatal("Golgari Thug was not dealt")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: e.G.Obj(tid).Zone, To: state.ZGraveyard})

	// Seat 0's SECOND turn (Turn 3 in a two-player game: turn 1 seat 0, turn
	// 2 seat 1, turn 3 seat 0). The first turn-based draw Dredge 4 can replace
	// is this one -- turn 1's draw is skipped in a two-player game (CR
	// 103.8a). driveToStep walks the real turn structure, so the draw below
	// is the one advanceStep performs on entry to StepDraw.
	driveToStep(t, e, 3, 0, state.StepUpkeep)
	since := len(e.L.Events)
	driveToStep(t, e, 3, 0, state.StepDraw)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "dredge" {
		t.Fatalf("turn draw did not suspend on a dredge ask: %+v", d)
	}
	// The upkeep's own priority passes legitimately precede the draw, so the
	// scan starts at the StepChange{StepDraw} the transition emitted -- the
	// same boundary assertDrawPrecedesPriorityInStep uses.
	drawAt := -1
	for i := since; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.StepChange && e.L.Events[i].Step == state.StepDraw {
			drawAt = i
			break
		}
	}
	if drawAt < 0 {
		t.Fatal("no StepChange{StepDraw} after the upkeep")
	}
	for i, ev := range e.L.Events[drawAt+1:] {
		if ev.Kind == events.Priority {
			t.Fatalf("priority emitted while dredge decision is pending (event +%d after StepChange: %+v)", i, ev)
		}
	}
	// Decline the dredge; the ordinary draw completes and only THEN does the
	// step grant priority -- exactly once.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{len(d.Options) - 1}}); err != nil {
		t.Fatalf("submit dredge decline: %v", err)
	}
	draws, priorities := 0, 0
	for _, ev := range e.L.Events[drawAt+1:] {
		switch ev.Kind {
		case events.Draw:
			if ev.Player == 0 {
				draws++
			}
		case events.Priority:
			priorities++
		}
	}
	if draws != 1 {
		t.Fatalf("draw-step draws after declining dredge = %d, want 1", draws)
	}
	if priorities != 1 {
		t.Fatalf("draw-step priority grants = %d, want exactly 1", priorities)
	}
	// The priority decision resumes the step's round from the current holder
	// (the seat after the last passer -- ordinary rotation, not this fix's
	// concern); what matters is that a priority ask, not the dredge ask, is
	// what is now outstanding.
	pd := e.Pending()
	if pd == nil || pd.Kind != decision.KPriority {
		t.Fatalf("post-draw pending = %+v, want a priority ask", pd)
	}
	replayCheck(t, e, cfg)
}

// TestTurnDrawDredgeAcceptAlsoGrantsExactlyOnePriority is the same drive with
// the OTHER answer: accepting the replacement must also leave exactly one
// priority grant in the draw step, after the mill-and-return completes.
func TestTurnDrawDredgeAcceptAlsoGrantsExactlyOnePriority(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	thug, ok := reg.Lookup("Golgari Thug")
	if !ok {
		t.Fatal("Golgari Thug missing from corpus")
	}
	deck := append([]*cards.Card{thug}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 243, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var tid state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == "Golgari Thug" {
				tid = id
			}
		}
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: e.G.Obj(tid).Zone, To: state.ZGraveyard})
	driveToStep(t, e, 3, 0, state.StepUpkeep)
	since := len(e.L.Events)
	driveToStep(t, e, 3, 0, state.StepDraw)
	d := e.Pending()
	if d == nil || d.ResumeKind != "dredge" {
		t.Fatalf("turn draw did not suspend on a dredge ask: %+v", d)
	}
	drawAt := -1
	for i := since; i < len(e.L.Events); i++ {
		if e.L.Events[i].Kind == events.StepChange && e.L.Events[i].Step == state.StepDraw {
			drawAt = i
			break
		}
	}
	if drawAt < 0 {
		t.Fatal("no StepChange{StepDraw} after the upkeep")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit dredge accept: %v", err)
	}
	milled := 0
	for _, ev := range e.L.Events[drawAt+1:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZGraveyard && ev.Player == 0 {
			milled++
		}
	}
	if milled != 4 {
		t.Fatalf("accepted dredge milled %d cards, want 4", milled)
	}
	for _, ev := range e.L.Events[drawAt+1:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			t.Fatal("accepted dredge still drew the ordinary card")
		}
	}
	priorities := 0
	for _, ev := range e.L.Events[drawAt+1:] {
		if ev.Kind == events.Priority {
			priorities++
		}
	}
	if priorities != 1 {
		t.Fatalf("draw-step priority grants after accepting dredge = %d, want exactly 1", priorities)
	}
	replayCheck(t, e, cfg)
}
