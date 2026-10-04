package rules

// Kernel-era restorations of the life_draw_dredge_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestLifeReplacementDredgeAsksOnceAndDrawsAll is the finding's probe: a
// replaced 2-card gain must never have two asks outstanding at once. Each
// draw poses its OWN dredge ask (CR 702.55 is per draw), sequentially: the
// first is answered before the second is posed, and both draws are delivered
// on declines.
func TestLifeReplacementDredgeAsksOnceAndDrawsAll(t *testing.T) {
	e, cfg, _ := lichThugEngine(t, 244, "Golgari Thug", "Nefarious Lich")
	since := len(e.L.Events)
	kr6Probe(e, func() { e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2}) })
	if n := countAsks(t, e, since); n != 1 {
		t.Fatalf("DecisionAsk events for one replaced 2-card gain = %d, want exactly 1 (a second ask before the first is answered orphans it)", n)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "dredge" {
		t.Fatalf("pending = %+v, want the dredge ask", d)
	}
	// Decline (the trailing "Draw card" option): the ordinary draw for THIS
	// ask, then the parked remainder re-driven — which asks again, since the
	// dredger is still in the graveyard.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{len(d.Options) - 1}}); err != nil {
		t.Fatalf("submit first dredge decline: %v", err)
	}
	if n := countAsks(t, e, since); n != 2 {
		t.Fatalf("asks after the first decline = %d, want 2 (the second draw's own ask)", n)
	}
	d2 := e.Pending()
	if d2 == nil || d2.ResumeKind != "dredge" {
		t.Fatalf("pending after the first decline = %+v, want the second draw's ask", d2)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player,
		Choices: []int{len(d2.Options) - 1}}); err != nil {
		t.Fatalf("submit second dredge decline: %v", err)
	}
	if n := countAsks(t, e, since); n != 2 {
		t.Fatalf("total DecisionAsk events = %d, want 2", n)
	}
	draws := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 2 {
		t.Fatalf("draws delivered for a replaced 2-card gain = %d, want 2", draws)
	}
	kr6Settle(e) // the probed emit ends with no priority grant of its own
	if e.Pending() == nil {
		t.Fatal("no decision pending after the replacement body completed")
	}
	replayCheck(t, e, cfg)
}

// TestLifeReplacementDredgeAcceptAppliesThenContinues accepts the dredge:
// the mill-and-return applies to the draw that asked, and the parked
// remainder is still drawn afterwards — not lost the way the unfixed loop
// lost it.
func TestLifeReplacementDredgeAcceptAppliesThenContinues(t *testing.T) {
	e, cfg, ids := lichThugEngine(t, 245, "Golgari Thug", "Nefarious Lich")
	since := len(e.L.Events)
	kr6Probe(e, func() { e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2}) })
	d := e.Pending()
	if d == nil || d.ResumeKind != "dredge" {
		t.Fatalf("pending = %+v, want the dredge ask", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit dredge accept: %v", err)
	}
	milled := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZGraveyard {
			milled++
		}
	}
	if milled != 4 {
		t.Fatalf("accepted dredge milled %d cards, want 4", milled)
	}
	if hand := e.G.Zone(state.ZHand, 0); len(hand) == 0 {
		t.Fatal("dredger did not return to hand: hand empty")
	} else {
		found := false
		for _, id := range hand {
			if id == ids[0] {
				found = true
			}
		}
		if !found {
			t.Fatal("dredger did not return to hand")
		}
	}
	if z := e.G.Zone(state.ZGraveyard, 0); len(z) != 4 {
		t.Fatalf("graveyard after the dredge = %d cards, want the 4 milled", len(z))
	}
	draws := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("remaining draws after the accepted dredge = %d, want 1", draws)
	}
	replayCheck(t, e, cfg)
}

// TestLifeReplacementDredgeReParksForEachDraw proves the re-drive is itself
// suspension-aware: with a SECOND dredger still in the graveyard, the second
// draw poses its own ask after the first answer landed — two sequential
// asks, each answered, never two outstanding at once.
func TestLifeReplacementDredgeReParksForEachDraw(t *testing.T) {
	e, cfg, _ := lichThugEngine(t, 246, "Golgari Thug", "Golgari Thug", "Nefarious Lich")
	since := len(e.L.Events)
	kr6Probe(e, func() { e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2}) })
	if n := countAsks(t, e, since); n != 1 {
		t.Fatalf("first draw's asks = %d, want 1", n)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "dredge" {
		t.Fatalf("pending = %+v, want the first dredge ask", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit first dredge accept: %v", err)
	}
	if n := countAsks(t, e, since); n != 2 {
		t.Fatalf("asks after the first answer = %d, want 2 (the second dredger's own sequential ask)", n)
	}
	d2 := e.Pending()
	if d2 == nil || d2.ResumeKind != "dredge" {
		t.Fatalf("pending after the first answer = %+v, want the second dredge ask", d2)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player,
		Choices: []int{len(d2.Options) - 1}}); err != nil {
		t.Fatalf("submit second dredge decline: %v", err)
	}
	if n := countAsks(t, e, since); n != 2 {
		t.Fatalf("total asks = %d, want 2", n)
	}
	draws := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 1 {
		t.Fatalf("declined second draw = %d Draw events, want 1", draws)
	}
	replayCheck(t, e, cfg)
}
