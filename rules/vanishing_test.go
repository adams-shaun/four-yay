package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func deepForestHermitEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	hermit, ok := reg.Lookup("Deep Forest Hermit")
	if !ok {
		t.Fatal("corpus fixture: Deep Forest Hermit missing")
	}
	e := New(Config{Seed: 17, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append([]*cards.Card{hermit}, mountainDeck(t, 39)...), mountainDeck(t, 40),
	}, Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Deep Forest Hermit", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Deep Forest Hermit is not on battlefield: %+v", o)
	}
	if got := e.G.Obj(id).Counter("TIME"); got != 3 {
		t.Fatalf("precondition: Deep Forest Hermit entered with %d TIME counters, want 3", got)
	}
	// The printed ETB token trigger is unrelated to Vanishing and must not
	// obscure the trigger under test.
	e.pendingTriggers = nil
	return e, id
}

func TestVanishingDeepForestHermitUpkeepAndLastCounter(t *testing.T) {
	t.Parallel()
	e, id := deepForestHermitEngine(t)
	// A controller's upkeep queues the ordinary Phase trigger; it does not
	// remove counters as a turn-based action.
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if got := e.G.Obj(id).Counter("TIME"); got != 3 {
		t.Fatalf("TIME changed before upkeep trigger resolved: %d, want 3", got)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("controller upkeep put %d stack objects, want one Vanishing trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if got := e.G.Obj(id).Counter("TIME"); got != 2 {
		t.Fatalf("after resolving upkeep trigger TIME = %d, want 2", got)
	}

	// Seed the last counter through the event log. The last-counter trigger is
	// a separate stack object, not an immediate sacrifice from CounterChange.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -2})
	if got := e.G.Obj(id).Counter("TIME"); got != 0 {
		t.Fatalf("precondition: last-counter removal left %d TIME counters, want zero", got)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: last-counter removal sacrificed immediately; zone=%s", e.G.Obj(id).Zone)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("last-counter removal put %d stack objects, want one sacrifice trigger", len(e.G.Stack))
	}
	e.resolveTop()
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("after resolving last-counter trigger zone = %s, want graveyard", got)
	}
}

func TestVanishingOnlyTriggersOnControllersUpkeepAndNotAtZero(t *testing.T) {
	t.Parallel()
	e, id := deepForestHermitEngine(t)
	e.pendingTriggers = nil
	e.G.Active, e.G.Priority = 1, 1
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if got := e.G.Obj(id).Counter("TIME"); got != 3 {
		t.Fatalf("opponent upkeep changed TIME to %d, want 3", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("opponent upkeep put %d Vanishing triggers on stack, want none", len(e.G.Stack))
	}

	// Remove all counters once, then clear the resulting last-counter trigger
	// to isolate the no-counter upkeep boundary. The permanent remains on the
	// battlefield at zero until its real triggered ability resolves.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -3})
	if got := e.G.Obj(id).Counter("TIME"); got != 0 || got == 3 {
		t.Fatalf("precondition: TIME count after removal = %d, want zero distinct from 3", got)
	}
	e.pendingTriggers = nil
	// A redundant decrement of a zero count is not a removal of the last
	// counter, even though events.Apply clamps the resulting count to zero.
	// This exercises the counter trigger independently of the upkeep gate.
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Counter("TIME") != 0 {
		t.Fatalf("precondition: zero-counter Vanishing object is not on battlefield: %+v", o)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("redundant decrement at zero queued %d sacrifice triggers, want none", len(e.pendingTriggers))
	}
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("zero-counter upkeep put %d triggers on stack, want none: stack=%v", len(e.G.Stack), e.G.Obj(e.G.Stack[0]).Ability)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("zero-counter upkeep moved Vanishing permanent: %+v", o)
	}
}
