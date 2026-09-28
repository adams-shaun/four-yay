package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tidewalkerEngine seeds the REAL corpus Tidewalker -- a bare K:Vanishing
// carrier whose own script supplies its counter count (K:etbCounter:TIME:X
// with X = the number of Islands its controller has) -- plus `islands` real
// Islands on seat 0's battlefield, and returns the engine and Tidewalker's id.
// Tidewalker enters with that many TIME counters from its own entry
// replacement, never from any Vanishing expansion, which is exactly the
// dynamic count a synthesized fixed-N Vanishing replacement would corrupt.
func tidewalkerEngine(t *testing.T, islands int) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	tw, ok := reg.Lookup("Tidewalker")
	if !ok {
		t.Fatal("corpus fixture: Tidewalker missing")
	}
	deck := []*cards.Card{tw}
	for i := 0; i < islands; i++ {
		isl, ok := reg.Lookup("Island")
		if !ok {
			t.Fatal("corpus fixture: Island missing")
		}
		deck = append(deck, isl)
	}
	e := New(Config{Seed: 7, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append(deck, mountainDeck(t, 40-len(deck))...), mountainDeck(t, 40),
	}, Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)
	for i := 0; i < islands; i++ {
		moveByName(t, e, 0, "Island", state.ZBattlefield)
	}
	id := moveByName(t, e, 0, "Tidewalker", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Tidewalker is not on battlefield: %+v", o)
	}
	// Tidewalker's own entry replacement only; no Vanishing ETB is expected.
	e.pendingTriggers = nil
	return e, id
}

// TestVanishingTidewalkerDynamicCountUpkeepAndLastCounter proves the bare
// keyword's independent clock on a real carrier whose own script supplies a
// board-derived counter count: Tidewalker enters with three TIME counters
// (three Islands), one controller upkeep removes one, and reaching zero
// sacrifices it.
func TestVanishingTidewalkerDynamicCountUpkeepAndLastCounter(t *testing.T) {
	t.Parallel()
	e, id := tidewalkerEngine(t, 3)
	if got := e.G.Obj(id).Counter("TIME"); got != 3 {
		t.Fatalf("precondition: Tidewalker entered with %d TIME counters, want 3 from its own script (3 Islands)", got)
	}

	// The controller's upkeep queues the Vanishing removal trigger; removal
	// is not a turn-based action.
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if got := e.G.Obj(id).Counter("TIME"); got != 3 {
		t.Fatalf("precondition: TIME changed before the upkeep trigger resolved: %d, want 3", got)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("controller upkeep put %d stack objects, want one Vanishing removal trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if got := e.G.Obj(id).Counter("TIME"); got != 2 {
		t.Fatalf("after resolving upkeep trigger TIME = %d, want 2 (ticked down from 3)", got)
	}

	// Drain the remainder through the log. The last-counter trigger is a
	// separate stack object, not an immediate sacrifice from CounterChange.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -2})
	if got := e.G.Obj(id).Counter("TIME"); got != 0 {
		t.Fatalf("precondition: last-counter removal left %d TIME counters, want zero (distinct from 2)", got)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: last-counter removal sacrificed immediately; zone=%s", e.G.Obj(id).Zone)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("last-counter removal put %d stack objects, want one sacrifice trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("after resolving last-counter trigger zone = %s, want graveyard", z)
	}
}

// outOfTimeEngine puts the REAL corpus Out of Time -- the other bare
// K:Vanishing carrier -- on seat 0's battlefield with `creatures` real
// Grizzly Bears already on that battlefield, each TAPPED, and resolves Out of
// Time's own printed enters trigger. api:Phases is registered
// (effects/phases.go), so its printed AllValid$ Creature body phases the
// bears out for real and remembers them through RememberAffected$. Its
// printed DB$ PutCounter then reads
// Count$RememberedSize from that event-backed list. This tests the real
// count expression and Vanishing clock, NOT the phase-in half.
func outOfTimeEngine(t *testing.T, creatures int) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	root, ok := reg.Lookup("Out of Time")
	if !ok {
		t.Fatal("corpus fixture: Out of Time missing")
	}
	bear, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("corpus fixture: Grizzly Bears missing")
	}
	deck := []*cards.Card{root}
	for i := 0; i < creatures; i++ {
		deck = append(deck, bear)
	}
	e := New(Config{Seed: 7, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append(deck, mountainDeck(t, 40-len(deck))...), mountainDeck(t, 40),
	}, Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)
	var bears []state.ObjID
	for i := 0; i < creatures; i++ {
		bid := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		if o := e.G.Obj(bid); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: bear is not on battlefield: %+v", o)
		}
		e.emit(events.Event{Kind: events.Tap, Obj: bid})
		if !e.G.Obj(bid).Tapped {
			t.Fatalf("precondition: bear %d is not tapped", bid)
		}
		bears = append(bears, bid)
	}
	id := moveByName(t, e, 0, "Out of Time", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Out of Time is not on battlefield: %+v", o)
	}
	// The printed Phases action both phases the bears out and remembers the
	// affected set for the following Count$RememberedSize instruction.
	// Resolve the real PRINTED enters trigger: untap, unsupported Phases,
	// Effect, then DBPutCounter reading the remembered source list.
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Out of Time's entry queued %d stack objects, want exactly its printed enters trigger", len(e.G.Stack))
	}
	e.resolveTop()
	// The UntapAll step ran before the count step.
	for _, bid := range bears {
		if e.G.Obj(bid).Tapped {
			t.Fatalf("bear %d stayed tapped: the printed UntapAll step did not run", bid)
		}
	}
	return e, id, bears
}

// TestVanishingOutOfTimeDynamicCountUpkeepAndLastCounter is the brief's
// integration case: the real corpus Out of Time acquires its TIME counters
// through its OWN printed Count$RememberedSize mechanism (the fixture
// RememberAffected$ event-captures the affected creatures;
// no TIME counters are injected), the controller's Vanishing
// upkeep removes one of that dynamic count, and reaching zero queues the
// last-counter sacrifice.
func TestVanishingOutOfTimeDynamicCountUpkeepAndLastCounter(t *testing.T) {
	t.Parallel()
	e, id, bears := outOfTimeEngine(t, 2)
	if got := e.G.Obj(id).Counter("TIME"); got != 2 {
		t.Fatalf("precondition: Out of Time entered with %d TIME counters, want 2 from its printed count (2 creatures captured in fixture)", got)
	}
	if got := e.G.Obj(id).Counter("TIME"); got == 0 {
		t.Fatal("precondition: dynamic count must be positive before the clock runs")
	}
	// api:Phases is registered: Out of Time's real printed AllValid$ Creature
	// body phases every bear out. The status proves the primitive ran, not
	// merely that the fixture captured a count input.
	if phasesHasNote(e, "unimplemented API Phases") {
		t.Fatal("api:Phases is unregistered (fallback Note present)")
	}
	for _, bid := range bears {
		if o := e.G.Obj(bid); o == nil || !o.PhasedOut || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: bear %d not phased out by Out of Time's printed Phases: %+v", bid, o)
		}
	}

	// The controller's upkeep queues the Vanishing removal trigger; removal
	// is not a turn-based action.
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if got := e.G.Obj(id).Counter("TIME"); got != 2 {
		t.Fatalf("precondition: TIME changed before the upkeep trigger resolved: %d, want 2", got)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("controller upkeep put %d stack objects, want one Vanishing removal trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if got := e.G.Obj(id).Counter("TIME"); got != 1 {
		t.Fatalf("after resolving upkeep trigger TIME = %d, want 1 (ticked down from the dynamic 2)", got)
	}

	// Drain the remainder through the log. The last-counter trigger is a
	// separate stack object, not an immediate sacrifice from CounterChange.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -1})
	if got := e.G.Obj(id).Counter("TIME"); got != 0 {
		t.Fatalf("precondition: last-counter removal left %d TIME counters, want zero (distinct from 1)", got)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: last-counter removal sacrificed immediately; zone=%s", e.G.Obj(id).Zone)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("last-counter removal put %d stack objects, want one sacrifice trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("after resolving last-counter trigger zone = %s, want graveyard", z)
	}
	// Out of Time's own Vanishing sacrifice is its departure: the printed
	// comeback trigger fires and phases the creatures back in (CR 702.25's
	// "phase out until CARDNAME leaves the battlefield"). They were never
	// removed from the battlefield by phasing (it is not a zone change), so
	// the only thing that changed at the end of the clock is the status.
	for _, bid := range bears {
		if o := e.G.Obj(bid); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
			t.Fatalf("bear %d should have phased back in when Out of Time was sacrificed: %+v", bid, o)
		}
	}
}

// TestVanishingOutOfTimeSeededCounterClock is the supplemental clock-only
// coverage (kept per the r2 review): with NO creatures on the battlefield the
// printed count legitimately reads zero, so a one-counter setup is seeded
// through the event log and the single controller upkeep must remove it and
// then sacrifice the enchantment.
func TestVanishingOutOfTimeSeededCounterClock(t *testing.T) {
	t.Parallel()
	e, id, _ := outOfTimeEngine(t, 0)
	if got := e.G.Obj(id).Counter("TIME"); got != 0 {
		t.Fatalf("precondition: no creatures to count, Out of Time holds %d TIME counters, want 0", got)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: 1})
	if got := e.G.Obj(id).Counter("TIME"); got != 1 {
		t.Fatalf("precondition: seeded Out of Time holds %d TIME counters, want 1", got)
	}

	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if got := e.G.Obj(id).Counter("TIME"); got != 1 {
		t.Fatalf("precondition: TIME changed before the upkeep trigger resolved: %d, want 1", got)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("controller upkeep put %d stack objects, want one Vanishing removal trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if got := e.G.Obj(id).Counter("TIME"); got != 0 {
		t.Fatalf("after resolving upkeep trigger TIME = %d, want 0 (distinct from 1)", got)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: last-counter removal sacrificed immediately; zone=%s", e.G.Obj(id).Zone)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("last-counter removal put %d stack objects, want one sacrifice trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("after resolving last-counter trigger zone = %s, want graveyard", z)
	}
}
