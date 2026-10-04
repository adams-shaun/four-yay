package rules

// Kernel-era restorations of the fdn_trigger_gates_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestKioraThresholdOptionalTokenGate pins Kiora, the Rising Tide's OPTIONAL
// "Threshold -- ... you may create Scion of the Deep" attacks trigger: below
// threshold the optional ask is never offered, at threshold it is.
func TestKioraThresholdOptionalTokenGate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	t.Run("below threshold no optional ask", func(t *testing.T) {
		e, cfg, id := fdnAttackEngine(t, reg, "Kiora, the Rising Tide", 6)
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition failed: Kiora zone=%s", e.G.Obj(id).Zone)
		}
		before := e.Pending()
		if before == nil || before.Kind != decision.KPriority {
			t.Fatalf("precondition failed: expected a priority decision before the declaration, got %+v", before)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
		if n := len(e.pendingTriggers); n != 0 {
			t.Fatalf("Kiora's Threshold trigger queued %d instance(s) with 6 graveyard cards, want 0", n)
		}
		e.putTriggersOnStack()
		if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOptional {
			t.Fatalf("Kiora's optional token ask was offered below threshold: %+v", d)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("at threshold offers the optional ask", func(t *testing.T) {
		e, cfg, id := fdnAttackEngine(t, reg, "Kiora, the Rising Tide", 7)
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 7 {
			t.Fatalf("precondition failed: seat 0 graveyard holds %d cards, want 7", got)
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
		if n := len(e.pendingTriggers); n != 1 {
			t.Fatalf("Kiora's Threshold trigger queued %d instance(s) with 7 graveyard cards, want 1", n)
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("Kiora's Threshold trigger did not reach the stack")
		}
		// Kernel era: the direct resolveTop is a kernel probe, which poses
		// its tape ask only with no other decision pending -- drop the stale
		// priority snapshot the raw emits above left behind.
		e.pending = nil
		kr6ResolveTop(e)
		d := e.Pending()
		if d == nil || d.Kind != decision.KTriggerOptional {
			t.Fatalf("Kiora's optional token ask = %+v, want KTriggerOptional at threshold", d)
		}
		replayCheck(t, e, cfg)
	})
}
