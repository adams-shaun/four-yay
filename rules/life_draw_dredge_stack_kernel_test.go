package rules

// Kernel-era restorations of the life_draw_dredge_stack_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLifeReplacementDredgeStackDeclinesAll is the review's exact repro, on
// the fixed engine: a resolving spell's 7 replaced draws each pose their own
// ask (the final one is a lifeDraws == 0 frame -- the old gate dropped its
// answer), every decline delivers its draw, and the DBDraw sub-ability after
// the GainLife still runs (2 more draws, each asking) because the re-park
// early returns now carry the continuation chain. Card total: 9 asks, 9 draws.
func TestLifeReplacementDredgeStackDeclinesAll(t *testing.T) {
	e, cfg, ids := lichKissEngine(t, 251)
	since := len(e.L.Events)
	castKiss(t, e, ids[2])
	d := e.Pending()
	if d == nil || d.ResumeKind != "dredge" {
		t.Fatalf("pending after the spell reached resolution = %+v, want the first dredge ask", d)
	}
	asks := answerDredges(t, e, func(d *decision.Decision) int {
		return len(d.Options) - 1 // decline: the trailing "Draw card" option
	})
	if asks != 9 {
		t.Fatalf("dredge asks for 7 life draws + 2 DBDraw draws = %d, want 9", asks)
	}
	if n := drawEvents(t, e, since); n != 9 {
		t.Fatalf("Draw events = %d, want 9 (7 replaced life draws + 2 DBDraw)", n)
	}
	if n := millEvents(t, e, since); n != 0 {
		t.Fatalf("mill MoveZone events on an all-decline run = %d, want 0", n)
	}
	if o := e.G.Obj(ids[2]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Kiss of the Amesha zone = %+v, want the graveyard after resolving", o)
	}
	if thug := e.G.Obj(ids[0]); thug == nil || thug.Zone != state.ZGraveyard {
		t.Fatalf("Golgari Thug zone = %+v, want the graveyard (declines never mill it away)", thug)
	}
	assertNoOrphanNote(t, e, since)
	replayCheck(t, e, cfg)
}

// TestLifeReplacementDredgeStackAcceptsFinalAsk pins the lifeDraws == 0
// frame specifically: decline the first six asks, then ACCEPT the seventh
// (the body's final draw). The accepted dredge must apply -- mill 4, Thug
// back to hand -- with no Draw event for the draw it replaced, and the DBDraw
// sub-ability then draws 2 more (ask-free: the Thug is in hand). On the
// unfixed engine the seventh answer was dropped entirely: no mill, no return,
// and the Note where the draw belongs.
func TestLifeReplacementDredgeStackAcceptsFinalAsk(t *testing.T) {
	e, cfg, ids := lichKissEngine(t, 252)
	since := len(e.L.Events)
	castKiss(t, e, ids[2])
	d := e.Pending()
	if d == nil || d.ResumeKind != "dredge" {
		t.Fatalf("pending = %+v, want the first dredge ask", d)
	}
	ask := 0
	asks := answerDredges(t, e, func(d *decision.Decision) int {
		ask++
		if ask == 7 {
			return 0 // accept the FINAL ask: mill 4, return the Thug
		}
		return len(d.Options) - 1 // decline the first six
	})
	if asks != 7 {
		t.Fatalf("dredge asks = %d, want 7 (six declines + the accepted final; the Thug is back in hand afterwards)", asks)
	}
	if n := millEvents(t, e, since); n != 4 {
		t.Fatalf("accepted final dredge milled %d cards, want 4", n)
	}
	if n := drawEvents(t, e, since); n != 8 {
		t.Fatalf("Draw events = %d, want 8 (six declined life draws + the accepted draw is REPLACED + 2 DBDraw)", n)
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
	if o := e.G.Obj(ids[2]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Kiss of the Amesha zone = %+v, want the graveyard after resolving", o)
	}
	assertNoOrphanNote(t, e, since)
	replayCheck(t, e, cfg)
}
