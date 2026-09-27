package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCR701RingTemptsYouAsksThePlayerOnTheRealEngine is the integration pin
// for the brief's defect: at the REAL engine (not the effects double) a
// player controlling two eligible creatures is posed a ring_bearer KChoose
// when Call of the Ring's upkeep trigger resolves DB$ RingTemptsYou, and the
// answer -- a creature other than the deterministic first-in-zone-order
// default -- becomes the Ring-bearer. It drives the whole wiring the effects
// test stubs: Engine.Ask parking the resume point, handleChoose's
// by-role mid-resolution arm routing the KChoose answer into resumeResolution,
// the "ring_bearer" resume arm, and the re-entered effRingTemptsYou emitting
// exactly one RingTemptsYou event.
func TestCR701RingTemptsYouAsksThePlayerOnTheRealEngine(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	// Zone order is placement order: Bear first, Gorilla second. The
	// deterministic default would be Bear; the test answers Gorilla, so the
	// choice and the default differ.
	bear := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	gorilla := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Gorilla Berserkers"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Call of the Ring"))
	// Precondition: two distinct eligible creatures, Gorilla is NOT the
	// first-in-zone-order default, and Bear is -- so choosing Gorilla is a
	// real, observable switch, not a vacuous confirmation of the default.
	if bear == gorilla {
		t.Fatal("precondition: fixtures are the same object")
	}
	if first := e.G.Zone(state.ZBattlefield, 0)[0]; first != bear {
		t.Fatalf("precondition: first battlefield object %d, want Bear %d", first, bear)
	}
	if !e.IsCreature(bear) || !e.IsCreature(gorilla) {
		t.Fatal("precondition: both fixtures must be creatures")
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()

	d := e.Pending()
	if d == nil || d.ResumeKind != "ring_bearer" {
		t.Fatalf("pending = %+v, want the ring_bearer KChoose posed by the trigger", d)
	}
	if d.Player != 0 {
		t.Fatalf("ask seat = %d, want 0", d.Player)
	}
	// The offered list must contain Gorilla with a real option index; answer
	// by index (the same encoding botpolicy and the wire use).
	gorillaIdx := -1
	for _, o := range d.Options {
		if o.Obj == gorilla {
			gorillaIdx = o.Index
		}
	}
	if gorillaIdx < 0 {
		t.Fatalf("options = %+v, want Gorilla %d offered", d.Options, gorilla)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %+v, want exactly the two eligible creatures", d.Options)
	}

	submitChoices(t, e, gorillaIdx)

	if n := countRingTempts(e); n != 1 {
		t.Fatalf("RingTemptsYou events = %d, want exactly 1 (a resume must not re-emit)", n)
	}
	if e.G.Players[0].RingTempted != 1 {
		t.Fatalf("tempt count = %d, want 1 (a resume must not double-count)", e.G.Players[0].RingTempted)
	}
	if e.G.Players[0].RingBearer != gorilla {
		t.Fatalf("bearer = %d, want the chosen Gorilla %d (not the default Bear %d)",
			e.G.Players[0].RingBearer, gorilla, bear)
	}
}
