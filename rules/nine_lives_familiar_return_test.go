package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Nine-Lives Familiar's return half: `WithCountersAmount$ X` over
// `SVar:X:Spawner>TriggeredCard$CardCounters.REVIVAL/Minus.1`. The amount is
// an SVar name, not a literal: the familiar that died with eight revival
// counters comes back with seven, not the malformed-amount default of one.
func TestNineLivesFamiliarReturnsWithOneFewerRevival(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Nine-Lives Familiar"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 1, 2
	castMode(t, e, id, "")
	finishCast(t, e, id)
	if got := e.G.Obj(id).Counter("REVIVAL"); got != 8 {
		t.Fatalf("precondition: entered with %d revival counters, want 8", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	drainQueuedTriggers(t, e)
	if len(e.G.Delayed) != 1 {
		t.Fatalf("death registered %d delayed triggers, want 1", len(e.G.Delayed))
	}
	back := func() state.ObjID {
		for _, o := range e.G.Zone(state.ZBattlefield, 0) {
			if e.G.Obj(o).Face().Name == "Nine-Lives Familiar" {
				return o
			}
		}
		return 0
	}
	passUntil(t, e, func() bool { return back() != 0 })
	if got := e.G.Obj(back()).Counter("REVIVAL"); got != 7 {
		t.Fatalf("returned with %d revival counters, want 7", got)
	}
	// The second life: the returned object was not cast (no fresh eight), and
	// its seven counters arrived as an ordinary post-move CounterChange.
	e.emit(events.Event{Kind: events.MoveZone, Obj: back(), From: state.ZBattlefield, To: state.ZGraveyard})
	passUntil(t, e, func() bool { return back() != 0 })
	if got := e.G.Obj(back()).Counter("REVIVAL"); got != 6 {
		t.Fatalf("returned a second time with %d revival counters, want 6", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "malformed WithCountersAmount X" {
			t.Fatal("the return still reports a malformed WithCountersAmount")
		}
	}
}
