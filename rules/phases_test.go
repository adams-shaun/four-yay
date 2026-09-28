package rules

// CR 702.25 — Phasing. A phased-out permanent is treated as though it does
// not exist: it is not sacrificed, not targeted, its static abilities are
// off, it does not participate in combat, and it phases in at its
// controller's next untap step (CR 702.25d, CR 502.4). These are
// engine-level card tests on real corpus carriers; the shared harness is the
// tokenRepl one (corpus cards by name, a 2-seat engine, real logged moves).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPhasesPrimitiveSupported: the coverage census (make report) reads
// effects.Supported(), so a missing registration would silently keep every
// carrier unplayable.
func TestPhasesPrimitiveSupported(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["api:Phases"] {
		t.Fatal("effects.Supported() is missing api:Phases")
	}
}

// phasesGame builds a 2-seat engine whose seat 0 deck carries the named
// corpus cards, padded with Mountains.
func phasesGame(t *testing.T, seed uint64, names ...string) (*Engine, Config) {
	t.Helper()
	cs := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		cs = append(cs, tokenReplCorpusCard(t, n))
	}
	return tokenReplGame(t, seed, cs...)
}

// phasesHasNote reports whether any Note in the log names the given API's
// unimplemented fallback, so a "nothing happened" assertion cannot pass with
// the primitive unregistered.
func phasesHasNote(e *Engine, want string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == want {
			return true
		}
	}
	return false
}

// TestTalonGatesOfMadaraPhasesOutAndIn is the filing card end to end: Talon
// Gates of Madara enters, its ETB "up to one target creature phases out"
// resolves on a creature, the creature is phased out (CS 702.25d status),
// and it phases in at its controller's next untap step.
func TestTalonGatesOfMadaraPhasesOutAndIn(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 301, "Talon Gates of Madara", "Grizzly Bears")
	bears := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	// Precondition: the creature is really a battlefield permanent before the
	// effect runs (a vacuous setup must fail loudly).
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: Bears not a phased-in battlefield permanent: %+v", o)
	}
	// Precondition: the carrier's ETB is the real DB$ Phases body, not
	// something the engine rewrote.
	gate := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Talon Gates of Madara"), state.ZBattlefield)
	gf := e.G.Obj(gate).Face()
	if gf == nil || len(gf.Triggers) == 0 || gf.Triggers[0].Effect == nil {
		t.Fatal("precondition: Talon Gates ETB trigger is missing")
	}
	sa := gf.Triggers[0].Effect
	if sa.API != "Phases" || sa.Params["ValidTgts"] == "" {
		t.Fatalf("precondition: Talon Gates trigger body = API %q ValidTgts %q, want Phases/Creature", sa.API, sa.Params["ValidTgts"])
	}
	// Flush the queued ETB trigger into the target ask and answer it.
	addMana(t, e, 0, "")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Player != 0 {
		t.Fatalf("expected Talon Gates' target ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 60)
	if phasesHasNote(e, "unimplemented API Phases") {
		t.Fatal("api:Phases is not registered (unimplemented fallback Note present)")
	}
	if o := e.G.Obj(bears); o == nil || !o.PhasedOut || o.Zone != state.ZBattlefield {
		t.Fatalf("after Talon Gates ETB: Bears PhasedOut=%v Zone=%v, want phased out on the battlefield", o != nil && o.PhasedOut, o.Zone)
	}
	// It phases in at its controller's next untap step (CR 702.25d). Seat 0's
	// next turn is two turns after its current one in a 2-seat game, and the
	// untap step's action has run by the time upkeep is entered.
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	if o := e.G.Obj(bears); o == nil || o.PhasedOut {
		t.Fatal("Bears did not phase in at its controller's next untap step")
	}
	replayCheck(t, e, cfg)
}

// TestGuardianOfFaithPhasesOutOtherCreatures pins the multi-target shape: a
// second carrier, Guardian of Faith's "any number of other target creatures
// you control phase out".
func TestGuardianOfFaithPhasesOutOtherCreatures(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 302, "Guardian of Faith", "Grizzly Bears", "Hill Giant")
	bears := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	giant := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Hill Giant"), state.ZBattlefield)
	guardian := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Guardian of Faith"), state.ZBattlefield)
	if o := e.G.Obj(guardian); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Guardian of Faith is not on the battlefield")
	}
	addMana(t, e, 0, "")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Player != 0 {
		t.Fatalf("expected Guardian of Faith's target ask, got %+v", d)
	}
	// Both other creatures are offered; pick both.
	if len(d.Options) < 2 {
		t.Fatalf("Guardian target ask offered %d options, want at least 2", len(d.Options))
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(bears); o == nil || !o.PhasedOut {
		t.Fatal("Bears were not phased out by Guardian of Faith")
	}
	if o := e.G.Obj(giant); o == nil || !o.PhasedOut {
		t.Fatal("Hill Giant was not phased out by Guardian of Faith")
	}
	if o := e.G.Obj(guardian); o == nil || o.PhasedOut {
		t.Fatal("Guardian of Faith phased itself out, but its spec is Creature.Other")
	}
	replayCheck(t, e, cfg)
}

// TestPhaseoutFalsePhasesIn pins the Phaseout$ direction: a body carrying
// `Phaseout$ False` phases a phased-out permanent back in early, without
// waiting for its untap step.
func TestPhaseoutFalsePhasesIn(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 303, "Grizzly Bears")
	bears := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	// Phase it out directly (the effect's own emit path), so the test is
	// about the Phaseout$ False arm.
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: 1})
	if o := e.G.Obj(bears); o == nil || !o.PhasedOut {
		t.Fatal("precondition: Bears are not phased out after a direct PhaseOut event")
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: -1})
	if o := e.G.Obj(bears); o == nil || o.PhasedOut {
		t.Fatal("Amount -1 did not phase the permanent back in")
	}
	replayCheck(t, e, cfg)
}

// TestPhasedOutPermanentLeavesBattlefieldPhasesIn pins CR 702.25e: a
// phased-out permanent that leaves the battlefield phases in as it does so,
// so a later return is a fresh, phased-in permanent.
func TestPhasedOutPermanentLeavesBattlefieldPhasesIn(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 304, "Grizzly Bears")
	bears := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: 1})
	if o := e.G.Obj(bears); o == nil || !o.PhasedOut {
		t.Fatal("precondition: Bears are not phased out")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bears, From: state.ZBattlefield, To: state.ZGraveyard})
	o := e.G.Obj(bears)
	if o == nil || o.PhasedOut {
		t.Fatal("a phased-out permanent that left the battlefield kept its phased-out status")
	}
	if o.Zone != state.ZGraveyard {
		t.Fatalf("Bears zone = %v, want graveyard", o.Zone)
	}
	replayCheck(t, e, cfg)
}
