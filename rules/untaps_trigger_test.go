package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestUntapsTriggerKeyToTheCityDrawsAfterPaying(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	key := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Key to the City"))
	if o := e.G.Obj(key); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Key to the City is not on the battlefield: %+v", o)
	}
	if !effects.Supported()["trig:Untaps"] {
		t.Fatal("trig:Untaps is not reported as supported")
	}
	if missing := reg.Unsupported(e.G.Obj(key).Card, effects.Supported()); untapsContains(missing, "trig:Untaps") {
		t.Fatalf("Key to the City still reports trig:Untaps unsupported: %v", missing)
	}
	if !actionTriggerModes["Untaps"] {
		t.Fatal("Untaps is not in actionTriggerModes; ActivationLimit$ would be skipped")
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got == 0 {
		t.Fatal("precondition: seat 0 has no card available to draw")
	}
	e.G.Players[0].Pool = state.Mana{state.MC: 2}
	e.emit(events.Event{Kind: events.Tap, Obj: key})
	if !e.G.Obj(key).Tapped {
		t.Fatal("precondition: Key to the City did not become tapped")
	}
	before := len(e.L.Events)
	e.emit(events.Event{Kind: events.Untap, Obj: key})
	if e.G.Obj(key).Tapped {
		t.Fatal("precondition: the real Untap event did not untap Key to the City")
	}
	requireOneEventTrigger(t, e, "Key to the City")
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack after putting trigger = %v, want one Key trigger", e.G.Stack)
	}
	e.resolveTop()
	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KChoose {
		t.Fatalf("trigger-cost payment decision = %+v, want KChoose", d)
	}
	pay := -1
	for _, option := range d.Options {
		if option.Kind == "trigger_cost_pay" {
			pay = option.Index
		}
	}
	if pay < 0 {
		t.Fatalf("Key to the City did not offer the {2} payment: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 40)
	if got := untapsDrawsFor(e, before, 0); got != 1 {
		t.Fatalf("Key to the City produced %d draw events after paying {2}, want exactly 1", got)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("mana pool after paying {2} = %d, want 0", e.G.Players[0].Pool.Total())
	}
}

func TestUntapsTriggerScopesToUntappedObjectAndBattlefield(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	key := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Key to the City"))
	other := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swamp"))
	if key == other || e.G.Obj(key).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatalf("precondition: distinct Key and other permanent must both be on battlefield (key=%+v other=%+v)", e.G.Obj(key), e.G.Obj(other))
	}

	// A different object's real Untap must not satisfy Key's Card.Self filter.
	e.emit(events.Event{Kind: events.Tap, Obj: other})
	if !e.G.Obj(other).Tapped {
		t.Fatal("precondition: other permanent did not become tapped")
	}
	e.emit(events.Event{Kind: events.Untap, Obj: other})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("untapping a different object queued %d Key triggers, want 0", len(e.pendingTriggers))
	}

	// A source outside TriggerZones$ Battlefield must not observe an untap.
	e.emit(events.Event{Kind: events.MoveZone, Obj: key, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(key).Zone != state.ZGraveyard {
		t.Fatalf("precondition: Key is in %s, want graveyard", e.G.Obj(key).Zone)
	}
	e.emit(events.Event{Kind: events.Tap, Obj: other})
	e.emit(events.Event{Kind: events.Untap, Obj: other})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("Key outside the battlefield observed unrelated Untap: queued %d", len(e.pendingTriggers))
	}

	// Returning it to the battlefield, then doing two actual tap/untap cycles,
	// proves exactly one trigger is queued for each real Untap event.
	e.emit(events.Event{Kind: events.MoveZone, Obj: key, From: state.ZGraveyard, To: state.ZBattlefield})
	if e.G.Obj(key).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Key returned in %s, want battlefield", e.G.Obj(key).Zone)
	}
	for i := 1; i <= 2; i++ {
		e.emit(events.Event{Kind: events.Tap, Obj: key})
		if !e.G.Obj(key).Tapped {
			t.Fatalf("cycle %d precondition: Key did not tap", i)
		}
		e.emit(events.Event{Kind: events.Untap, Obj: key})
		if e.G.Obj(key).Tapped {
			t.Fatalf("cycle %d precondition: Key did not untap", i)
		}
		if got := len(e.pendingTriggers); got != i {
			t.Fatalf("after real Untap %d, queued %d triggers, want %d", i, got, i)
		}
	}
}

func untapsDrawsFor(e *Engine, from int, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

func untapsContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
