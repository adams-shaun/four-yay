package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr6Probe runs f as a resolution kernel probe (e.probe) after dropping any
// stale pending decision. The fixtures restored here set the board up with
// raw emits while a priority snapshot is still pending; the kernel poses a
// tape ask only when no other decision is pending, so the stale snapshot is
// cleared first -- the same e.pending = nil the fixture helpers use after a
// raw setup move (newFixtureDeck).
func kr6Probe(e *Engine, f func()) {
	e.pending = nil
	e.probe(f)
}

// kr6ResolveTop resolves the top of the stack as a kernel probe with no
// stale pending decision (see kr6Probe).
func kr6ResolveTop(e *Engine) {
	e.pending = nil
	e.resolveTop()
}

// kr6ResolveUpkeepCumulative is resolveUpkeepCumulative for the kernel era:
// it drives one beginning-of-upkeep StepChange, places the queued
// cumulative-upkeep trigger on the stack and resolves it as a kernel probe
// with no stale pending decision, leaving the engine at the resolution-time
// payment ask (posed from the tape).
func kr6ResolveUpkeepCumulative(t *testing.T, e *Engine) {
	t.Helper()
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	idx := -1
	for i, sid := range e.G.Stack {
		if o := e.G.Obj(sid); o != nil && o.Ability != nil && o.Ability.API == "CumulativeUpkeep" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("cumulative upkeep was not placed as a triggered ability: stack=%v", e.G.Stack)
	}
	kr6ResolveTop(e)
}

// kr6FlexCumulativeEngine is flexCumulativeEngine over
// kr6ResolveUpkeepCumulative.
func kr6FlexCumulativeEngine(t *testing.T, fixtureSrc string, seat0Lands []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := flexUpkeepEngine(t, fixtureSrc, seat0Lands)
	kr6ResolveUpkeepCumulative(t, e)
	return e, cfg, id
}

// kr6Settle finishes a probed resolution the way a live Submit does: a probe
// (kr6Probe, kr6ResolveTop) re-executes only the probed function when its
// ask is answered, so no state-based check or priority grant follows it.
// With nothing pending, kr6Settle advances the engine the way Submit's tail
// does (Advance: CR 117.5's SBAs and triggers first, then priority).
func kr6Settle(e *Engine) {
	if e.Pending() == nil {
		e.Advance()
	}
}
