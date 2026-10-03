package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestPlayerTurnGatesEveryTriggerMode pins PlayerTurn$ True on a mode outside
// actionTriggerModes: Quilled Greatwurm's DamageDealtOnce trigger ("deals
// combat damage during your turn") must not fire for its controller's
// creature dealing combat damage during an opponent's turn.
func TestPlayerTurnGatesEveryTriggerMode(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Quilled Greatwurm", "Grizzly Bears")
	wurm, bears := ids["Quilled Greatwurm"], ids["Grizzly Bears"]
	if actionTriggerModes["DamageDealtOnce"] {
		t.Fatal("precondition: DamageDealtOnce is in actionTriggerModes; this test must exercise an unlisted mode")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	if e.G.Active != 1 {
		t.Fatalf("precondition: seat 1 is not the active player after TurnChange (%d)", e.G.Active)
	}
	e.pendingTriggers = nil
	pcdrCombat(e, bears, 1, 2)
	if n := queuedPhaseTriggers(e, wurm); n != 0 {
		t.Fatalf("Greatwurm queued %d triggers during the opponent's turn, want 0 (PlayerTurn$ True)", n)
	}
}
