package view

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestWindowDiagnosticsOnlyProjectsToAskedSeat pins the seat-privacy boundary
// for the window-diagnostics sidecar: the asked seat's View carries it, and
// an opponent seat and a spectator each get a View with no Decision at all
// (view.project's d.Player == viewer gate already withholds the whole
// decision, so the field needs no new redaction).
func TestWindowDiagnosticsOnlyProjectsToAskedSeat(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	d := &decision.Decision{
		Player: 0, Kind: decision.KPriority,
		WindowReasons: []decision.WindowReason{
			{Obj: 7, Kind: "card", Reason: "cost:insufficient_mana"},
			{Obj: 9, Kind: "land", Reason: "land:drop_exhausted"},
		},
	}
	asked := ProjectFor(g, nil, 0, Seat, d)
	if asked.Decision == nil {
		t.Fatal("asked seat received no decision")
	}
	if len(asked.Decision.WindowReasons) != 2 {
		t.Fatalf("asked seat WindowReasons = %+v, want 2 entries", asked.Decision.WindowReasons)
	}
	if asked.Decision.WindowReasons[0] != d.WindowReasons[0] {
		t.Fatalf("asked seat reasons %+v, want %+v", asked.Decision.WindowReasons, d.WindowReasons)
	}
	if v := ProjectFor(g, nil, 1, Seat, d); v.Decision != nil {
		t.Errorf("opponent received decision: %+v", v.Decision)
	}
	if v := ProjectFor(g, nil, NoSeat, Public, d); v.Decision != nil {
		t.Errorf("spectator received decision: %+v", v.Decision)
	}
}

// TestWindowDiagnosticsProjectionOwnsItsSlice pins copyDecision's deep copy
// (Decision.CloneValue): mutating the projected sidecar must not reach the
// engine's original decision. A shallow *d copy would alias the backing
// array and fail this.
func TestWindowDiagnosticsProjectionOwnsItsSlice(t *testing.T) {
	orig := &decision.Decision{
		Player: 0, Kind: decision.KPriority,
		WindowReasons: []decision.WindowReason{
			{Obj: 7, Kind: "card", Reason: "cost:insufficient_mana"},
		},
	}
	if len(orig.WindowReasons) != 1 || orig.WindowReasons[0].Reason == "target:no_legal_target" {
		t.Fatal("fixture comparison values unexpectedly equal")
	}
	cp := copyDecision(orig)
	if cp == nil || len(cp.WindowReasons) != 1 {
		t.Fatalf("copyDecision dropped WindowReasons: %+v", cp)
	}
	if &cp.WindowReasons[0] == &orig.WindowReasons[0] {
		t.Fatal("copyDecision aliased the WindowReasons backing array")
	}
	cp.WindowReasons[0].Reason = "target:no_legal_target"
	if orig.WindowReasons[0].Reason != "cost:insufficient_mana" {
		t.Fatalf("projected mutation reached original: %q", orig.WindowReasons[0].Reason)
	}
}
