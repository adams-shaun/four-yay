package view

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestWindowDiagnosticsOnlyProjectsToAskedSeat(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, WindowReasons: []decision.WindowReason{{Obj: 7, Kind: "card", Reason: "cost:insufficient_mana"}}}
	asked := ProjectFor(g, nil, 0, Seat, d)
	if asked.Decision == nil || len(asked.Decision.WindowReasons) != 1 {
		t.Fatalf("asked seat decision = %#v", asked.Decision)
	}
	if v := ProjectFor(g, nil, 1, Seat, d); v.Decision != nil {
		t.Errorf("opponent received decision: %#v", v.Decision)
	}
	if v := ProjectFor(g, nil, NoSeat, Public, d); v.Decision != nil {
		t.Errorf("spectator received decision: %#v", v.Decision)
	}
}
