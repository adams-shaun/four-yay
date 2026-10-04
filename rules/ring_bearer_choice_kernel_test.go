package rules

// Kernel-era restorations of effects/ring_bearer_choice_test.go's hosted
// leaves (deleted with the W3 legacy removal): the Ring tempts you poses a
// real "choose a creature you control" (CR 701.54a) and applies the answer.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const kr3RingSrc = "Name:Ring Sorcery\nManaCost:B\nTypes:Sorcery\nA:SP$ RingTemptsYou\nOracle:x\n"

// kr3RingBoard puts Bear then Gorilla onto seat 0's battlefield and the Ring
// sorcery into seat 0's hand.
func kr3RingBoard(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	e, cfg := kr3Game(t, seed, kr3Cards(t, kr3RingSrc, kr3Creature("Bear"), kr3Creature("Gorilla")), nil)
	kr3Move(t, e, 0, "Ring Sorcery", state.ZHand)
	bear := kr3Move(t, e, 0, "Bear", state.ZBattlefield)
	gorilla := kr3Move(t, e, 0, "Gorilla", state.ZBattlefield)
	return e, cfg, bear, gorilla
}

func kr3RingTempts(e *Engine, from int) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.RingTemptsYou {
			out = append(out, ev)
		}
	}
	return out
}

// TestRingTemptsYouAsksAndAppliesTheChosenBearer: with two eligible
// creatures the controller is posed a choose-one ring_bearer KChoose over
// both, nothing is emitted while it is posed, and answering with the SECOND
// creature (not the first-in-zone-order default) designates it -- one
// RingTemptsYou event, one count (a re-entry never double-counts).
func TestRingTemptsYouAsksAndAppliesTheChosenBearer(t *testing.T) {
	t.Parallel()
	e, cfg, bear, gorilla := kr3RingBoard(t, 91)
	mark := len(e.L.Events)
	d := kr3Cast(t, e, "Ring Sorcery", "B", -1)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "ring_bearer" {
		t.Fatalf("decision = %+v, want a ring_bearer KChoose", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("decision = %+v, want seat 0 choose-one over two creatures", d)
	}
	kr3OptionObj(t, d, bear)
	if n := len(kr3RingTempts(e, mark)); n != 0 || e.G.Players[0].RingTempted != 0 {
		t.Fatalf("emitted %d temptations (count %d) while the ask is posed", n, e.G.Players[0].RingTempted)
	}
	if d := kr3Answer(t, e, kr3OptionObj(t, d, gorilla)); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	evs := kr3RingTempts(e, mark)
	if len(evs) != 1 || evs[0].Obj != gorilla || evs[0].Amount != 1 || evs[0].Player != 0 {
		t.Fatalf("temptations = %+v, want one naming gorilla %d at amount 1", evs, gorilla)
	}
	if p := e.G.Players[0]; p.RingTempted != 1 || p.RingBearer != gorilla {
		t.Fatalf("tempted %d bearer %d, want 1/%d", p.RingTempted, p.RingBearer, gorilla)
	}
	replayCheck(t, e, cfg)
}

// TestRingTemptsYouCanKeepTheExistingBearer: the current Ring-bearer (here
// the second creature, so keeping and first-in-zone-order disagree) is
// offered, and answering with it keeps the designation while the count rises.
func TestRingTemptsYouCanKeepTheExistingBearer(t *testing.T) {
	t.Parallel()
	e, cfg, _, gorilla := kr3RingBoard(t, 92)
	e.emit(events.Event{Kind: events.RingTemptsYou, Player: 0, Obj: gorilla, Amount: 1})
	e.pending = nil
	if e.G.Players[0].RingBearer != gorilla {
		t.Fatalf("precondition: bearer %d, want %d", e.G.Players[0].RingBearer, gorilla)
	}
	d := kr3Cast(t, e, "Ring Sorcery", "B", -1)
	if d == nil || d.ResumeKind != "ring_bearer" {
		t.Fatalf("decision = %+v, want a ring_bearer ask", d)
	}
	if d := kr3Answer(t, e, kr3OptionObj(t, d, gorilla)); d != nil {
		t.Fatalf("unexpected follow-up %+v", d)
	}
	if p := e.G.Players[0]; p.RingTempted != 2 || p.RingBearer != gorilla {
		t.Fatalf("count %d bearer %d, want 2/%d -- the kept bearer was re-designated", p.RingTempted, p.RingBearer, gorilla)
	}
	replayCheck(t, e, cfg)
}
