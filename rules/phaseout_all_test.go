package rules

// Mode$ PhaseOutAll (CR 702.25b): "Whenever one or more other permanents phase
// out ..." (The War Doctor). The batch-level reading is the point: one
// api:Phases resolution that phases out several permanents is ONE occurrence,
// so the trigger fires once for the group, not once per permanent. The test
// drives a real corpus carrier (Guardian of Faith's "any number of other
// target creatures you control phase out") over two real creatures and reads
// the printed PutCounter body's TIME counter on the real The War Doctor.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestTheWarDoctorPhaseOutAllFiresOncePerBatch proves the batch cadence: two
// permanents phase out in one resolution and The War Doctor puts exactly ONE
// time counter on itself. Its filter is `Permanent.phasedOutOther`, so the
// Doctor (phase-out-excluded) is not counted, and if the latch were per-event
// the counter would be 2.
func TestTheWarDoctorPhaseOutAllFiresOncePerBatch(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 601, "The War Doctor", "Guardian of Faith", "Grizzly Bears", "Grizzly Bears")
	toMain1(t, e)
	doctor := moveByName(t, e, 0, "The War Doctor", state.ZBattlefield)
	if o := e.G.Obj(doctor); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: The War Doctor is not a phased-in battlefield permanent: %+v", o)
	}
	if got := e.G.Obj(doctor).Counter("TIME"); got != 0 {
		t.Fatalf("precondition: The War Doctor starts with %d TIME counters, want 0", got)
	}
	b1 := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	b2 := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	for _, bid := range []state.ObjID{b1, b2} {
		if o := e.G.Obj(bid); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
			t.Fatalf("precondition: bear %d is not a phased-in battlefield permanent: %+v", bid, o)
		}
	}
	// Precondition: the carrier's ETB really is the printed Phases body.
	guardian := moveByName(t, e, 0, "Guardian of Faith", state.ZBattlefield)
	gf := e.G.Obj(guardian).Face()
	if gf == nil || len(gf.Triggers) == 0 || gf.Triggers[0].Effect == nil ||
		gf.Triggers[0].Effect.API != "Phases" {
		t.Fatal("precondition: Guardian of Faith's ETB is not the printed DB$ Phases body")
	}
	addMana(t, e, 0, "")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Player != 0 {
		t.Fatalf("expected Guardian of Faith's target ask, got %+v", d)
	}
	// Choose BOTH bears, so the one Phases resolution emits two PhaseOut
	// events (the batch under test). Never the Doctor or the Guardian itself.
	var picks []int
	for _, o := range d.Options {
		if o.Obj == b1 || o.Obj == b2 {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("precondition: Guardian's ask offered %d of the two bears (options %+v)", len(picks), d.Options)
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(b1); o == nil || !o.PhasedOut {
		t.Fatal("precondition: bear 1 was not phased out by Guardian of Faith")
	}
	if o := e.G.Obj(b2); o == nil || !o.PhasedOut {
		t.Fatal("precondition: bear 2 was not phased out by Guardian of Faith")
	}
	if o := e.G.Obj(doctor); o == nil || o.PhasedOut {
		t.Fatalf("precondition: The War Doctor phased out, but the chosen targets were the two bears: %+v", o)
	}
	// The batch-level trigger queued once and resolved: exactly one counter.
	e.putTriggersOnStack()
	if len(e.G.Stack) > 0 {
		e.resolveTop()
	}
	if got := e.G.Obj(doctor).Counter("TIME"); got != 1 {
		t.Fatalf("PhaseOutAll fired %d times for one two-permanent batch, want exactly 1 (per-event would be 2)", got)
	}
	replayCheck(t, e, cfg)
}
