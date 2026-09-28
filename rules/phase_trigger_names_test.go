package rules

// The brief's behaviour gate for the shared Forge phase-name parser
// (state/phase.go): each of the corpus's real Phase$ shapes -- `End of Turn`
// (Braids, Arisen Nightmare), `BeginCombat` (Kamahl, Heart of Krosa),
// `EndCombat` (Kjeldoran Frostbeast) and the comma list `Main1,Main2`
// (Carpet of Flowers) -- fires its trigger exactly once at its step and
// never at another. Before the shared parser, every multi-word script name
// (`End of Turn`, `BeginCombat`, `EndCombat`) was a substring miss against
// the engine step names and NEVER fired at any step; `Main` matched both
// mains by substring but the `Main1,Main2` list matched nothing.
//
// The fixtures drive the REAL compiled corpus triggers (never a re-written
// copy) with direct StepChange emissions -- the same shape
// trigger_referents_test.go uses -- and count the queued triggers per step.
// Firing, not resolving, is what this gate is about; each effect's own asks
// are owned by its own primitives' tests.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// phaseCardEngine builds a two-seat game, seat 0 active (the CR 103.1 toss
// pinned via seatZeroStart), with the REAL corpus card parked on seat 0's
// battlefield and no pending decision.
func phaseCardEngine(t *testing.T, name string) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus fixture: %s missing", name)
	}
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(mountainDeck(t, 40), card), mountainDeck(t, 41)}}))
	e.Advance()
	id := crAbortMove(t, e, 0, name, state.ZBattlefield)
	e.pending = nil
	return e, id
}

// queuedPhaseTriggers counts the triggers queued (pending, not yet placed)
// for the source object.
func queuedPhaseTriggers(e *Engine, id state.ObjID) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == id {
			n++
		}
	}
	return n
}

// assertPhaseFires drives the card through every step: each step in wants
// queues exactly one trigger, every other step queues none, and the queue is
// drained between steps. The card's controller is seat 0, who is also the
// active player, so ValidPlayer$ You/Player gates hold throughout turn 1.
func assertPhaseFires(t *testing.T, e *Engine, id state.ObjID, wants ...state.Step) {
	t.Helper()
	wantSet := map[state.Step]bool{}
	for _, s := range wants {
		wantSet[s] = true
	}
	for s := state.Step(0); s <= state.StepCleanup; s++ {
		e.emit(events.Event{Kind: events.StepChange, Step: s})
		n := queuedPhaseTriggers(e, id)
		if wantSet[s] && n != 1 {
			t.Fatalf("step %s queued %d triggers, want exactly 1", s, n)
		}
		if !wantSet[s] && n != 0 {
			t.Fatalf("step %s queued %d triggers, want 0", s, n)
		}
		e.pendingTriggers = nil
	}
}

func TestBraidsEndOfTurnTriggerFiresOnlyAtTheEndStep(t *testing.T) {
	t.Parallel()
	e, id := phaseCardEngine(t, "Braids, Arisen Nightmare")
	tr := crTriggerFixture(t, e, id, "Phase", "Sacrifice")
	if tr.Params["Phase"] != "End of Turn" {
		t.Fatalf("corpus fixture changed: Phase = %q", tr.Params["Phase"])
	}
	assertPhaseFires(t, e, id, state.StepEnd)
}

func TestKamahlBeginCombatTriggerFiresOnlyAtBeginCombat(t *testing.T) {
	t.Parallel()
	e, id := phaseCardEngine(t, "Kamahl, Heart of Krosa")
	tr := crTriggerFixture(t, e, id, "Phase", "PumpAll")
	if tr.Params["Phase"] != "BeginCombat" {
		t.Fatalf("corpus fixture changed: Phase = %q", tr.Params["Phase"])
	}
	assertPhaseFires(t, e, id, state.StepBeginCombat)
}

func TestFrostbeastEndCombatTriggerFiresOnlyAtEndCombat(t *testing.T) {
	t.Parallel()
	e, id := phaseCardEngine(t, "Kjeldoran Frostbeast")
	tr := crTriggerFixture(t, e, id, "Phase", "DestroyAll")
	if tr.Params["Phase"] != "EndCombat" {
		t.Fatalf("corpus fixture changed: Phase = %q", tr.Params["Phase"])
	}
	assertPhaseFires(t, e, id, state.StepEndCombat)
}

// TestCarpetOfFlowersListFiresAtBothMains pins the comma-list form
// (`Phase$ Main1,Main2`): under the deleted substring parser this spec
// matched NO step at all; now each main phase queues exactly one trigger
// and no other step queues any.
func TestCarpetOfFlowersListFiresAtBothMains(t *testing.T) {
	t.Parallel()
	e, id := phaseCardEngine(t, "Carpet of Flowers")
	tr := crTriggerFixture(t, e, id, "Phase", "Pump")
	if tr.Params["Phase"] != "Main1,Main2" {
		t.Fatalf("corpus fixture changed: Phase = %q", tr.Params["Phase"])
	}
	assertPhaseFires(t, e, id, state.StepMain1, state.StepMain2)
}

// Phase$ is Forge's validPhases gate for EVERY trigger mode, not a property
// of Mode$ Phase. These real corpus probes pin the two other already-supported
// modes carrying it: Moraug's landfall must occur only in a main phase, and
// Dovin's Acuity must see an instant only in a main phase. Calling the common
// matcher directly isolates the event/step contract from each effect's own
// resolution tests.
func TestPhaseGateAppliesToChangesZoneAndSpellCast(t *testing.T) {
	t.Parallel()
	t.Run("ChangesZone Moraug", func(t *testing.T) {
		e, id := phaseCardEngine(t, "Moraug, Fury of Akoum")
		tr := crTriggerFixture(t, e, id, "ChangesZone", "AddPhase")
		if tr.Params["Phase"] != "Main1,Main2" {
			t.Fatalf("corpus fixture changed: Phase = %q", tr.Params["Phase"])
		}
		land := onBoard(t, e, 0, "Name:Phase Gate Land\nTypes:Land\nOracle:x\n")
		ev := events.Event{Kind: events.MoveZone, Obj: land, From: state.ZHand, To: state.ZBattlefield}
		for _, tc := range []struct {
			step state.Step
			want bool
		}{
			{state.StepUpkeep, false},
			{state.StepMain1, true},
			{state.StepMain2, true},
			{state.StepBeginCombat, false},
		} {
			e.G.Step = tc.step
			if got := e.triggerMatches(tr, id, ev, nil); got != tc.want {
				t.Errorf("ChangesZone at %s = %t, want %t", tc.step, got, tc.want)
			}
		}
	})
	t.Run("SpellCast Dovins Acuity", func(t *testing.T) {
		e, id := phaseCardEngine(t, "Dovin's Acuity")
		tr := crTriggerFixture(t, e, id, "SpellCast", "ChangeZone")
		if tr.Params["Phase"] != "Main1,Main2" {
			t.Fatalf("corpus fixture changed: Phase = %q", tr.Params["Phase"])
		}
		instant := onBoard(t, e, 0, "Name:Phase Gate Instant\nManaCost:U\nTypes:Instant\nOracle:x\n")
		ev := events.Event{Kind: events.PutOnStack, Obj: instant, Player: 0, From: state.ZHand, To: state.ZStack}
		for _, tc := range []struct {
			step state.Step
			want bool
		}{
			{state.StepUpkeep, false},
			{state.StepMain1, true},
			{state.StepMain2, true},
			{state.StepBeginCombat, false},
		} {
			e.G.Step = tc.step
			if got := e.triggerMatches(tr, id, ev, nil); got != tc.want {
				t.Errorf("SpellCast at %s = %t, want %t", tc.step, got, tc.want)
			}
		}
	})
}

// TestPhaseTriggerUnknownNameReportsAndNeverFires pins the reporting half of
// the gate: a Phase$ value no Forge script name resolves (here the bare
// "End" the old substring parser used to accept) queues nothing and emits
// exactly one Note naming it, once per engine no matter how often the
// trigger is walked.
func TestPhaseTriggerUnknownNameReportsAndNeverFires(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 41)}}))
	e.Advance()
	e.pending = nil
	// A synthetic face (the real corpus never writes this) carries the
	// unresolvable spec through the ordinary trigger walk.
	src := onBoard(t, e, 0, `Name:Endless End
Types:Enchantment
T:Mode$ Phase | Phase$ End | Execute$ TrigGain
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`)
	for i := 0; i < 3; i++ {
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
		if n := queuedPhaseTriggers(e, src); n != 0 {
			t.Fatalf("unresolvable Phase$ queued %d triggers, want 0", n)
		}
		e.pendingTriggers = nil
	}
	count := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Phase$ End ") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unknown Phase$ spec emitted %d Notes, want exactly 1", count)
	}
}

// The same reporting contract covers non-Phase modes. An invalid Phase$ on
// a zone trigger must fail closed and report rather than silently firing on
// every zone change (the bug that the old Mode$ Phase-only parser left open).
func TestPhaseGateReportsUnknownForChangesZone(t *testing.T) {
	t.Parallel()
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 41)}}))
	e.Advance()
	e.pending = nil
	src := onBoard(t, e, 0, `Name:Unknown Phase Gate
Types:Enchantment
T:Mode$ ChangesZone | Phase$ End | Origin$ Any | Destination$ Battlefield | Execute$ TrigGain
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`)
	target := onBoard(t, e, 0, "Name:Entering Card\nTypes:Creature\nPT:1/1\nOracle:x\n")
	ev := events.Event{Kind: events.MoveZone, Obj: target, From: state.ZHand, To: state.ZBattlefield}
	for i := 0; i < 2; i++ {
		e.checkFaceTriggers(e, ev, nil, 0, 0, false, false, false)
		if n := queuedPhaseTriggers(e, src); n != 0 {
			t.Fatalf("unknown ChangesZone Phase$ queued %d triggers, want 0", n)
		}
		e.pendingTriggers = nil
	}
	count := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Phase$ End ") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unknown ChangesZone Phase$ emitted %d Notes, want exactly 1", count)
	}
}
