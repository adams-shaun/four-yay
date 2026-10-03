package rules

import (
	"testing"
)

// TestQuilledGreatwurmCountersGoOnTheDealer pins Defined$
// TriggeredSourceLKICopy: "put that many +1/+1 counters on it" names the
// creature that dealt the combat damage, not the Greatwurm whose trigger is
// resolving. Before the selector was recognised, Defined fell back to the
// ability's source and every counter landed on the Greatwurm.
func TestQuilledGreatwurmCountersGoOnTheDealer(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Quilled Greatwurm", "Grizzly Bears")
	wurm, bears := ids["Quilled Greatwurm"], ids["Grizzly Bears"]
	if e.G.Active != 0 {
		t.Fatalf("precondition: seat 0 is not the active player (%d)", e.G.Active)
	}
	e.pendingTriggers = nil
	pcdrCombat(e, bears, 1, 2)
	if n := queuedPhaseTriggers(e, wurm); n != 1 {
		t.Fatalf("Greatwurm queued %d triggers for the Bears' combat damage, want 1", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := p1p1Count(e, bears); got != 2 {
		t.Errorf("Grizzly Bears +1/+1 counters = %d, want 2", got)
	}
	if got := p1p1Count(e, wurm); got != 0 {
		t.Errorf("Quilled Greatwurm +1/+1 counters = %d, want 0 (the counters belong on the dealer)", got)
	}
}
