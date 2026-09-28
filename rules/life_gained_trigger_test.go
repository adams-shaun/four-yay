package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigger-level gates Mode$ LifeGained honours (the r2 fix round):
// FirstTime$ (8 corpus lines over 7 files), PlayerTurn$ (5 lines over 5
// files) and the actionTriggerModes membership that makes ActivationLimit$
// (2 lines) apply at queue time. Before this round the mode matched every
// LifeChange unconditionally: a "once each turn" trigger fired every time
// and a "during your turn" trigger fired on opponents' turns.

// lifeGainedFixture seats a synthetic carrier with one T:Mode$ LifeGained
// trigger carrying the named extra params, beside the ValidPlayer$ You the
// corpus lines uniformly carry.
func lifeGainedFixture(t *testing.T, extraParams string) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Gainer\nTypes:Creature\n"+
		"T:Mode$ LifeGained | ValidPlayer$ You | "+extraParams+
		" | Execute$ TrigDraw\nSVar:TrigDraw:DB$ Draw\nOracle:x\n")
	return e, id
}

// gain emits a positive LifeChange for p, the event kind and shape
// effGainLife uses (events has no GainLife kind).
func gain(t *testing.T, e *Engine, p state.PlayerID, amount int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: amount})
}

func findPlayerOption(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no target decision pending")
	}
	for _, o := range d.Options {
		if o.Player == p {
			return o.Index
		}
	}
	t.Fatalf("no target option for seat %d in %v", p, d.Options)
	return -1
}

// FirstTime$ True ("whenever you gain life for the first time each turn")
// admits exactly the first gain of that player this turn. Synthetic pin: the
// once-per-turn contract is exactly one, so a second same-turn gain must not
// queue a second trigger.
func TestLifeGainedFirstTimeFiresOnceEachTurn(t *testing.T) {
	t.Parallel()
	e, src := lifeGainedFixture(t, "FirstTime$ True")
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != src {
		t.Fatalf("first gain of the turn queued %#v, want exactly one trigger from %d",
			e.pendingTriggers, src)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("second same-turn gain queued %#v, want no second trigger (FirstTime$ True)",
			e.pendingTriggers)
	}
}

// FirstTime$ on a REAL corpus carrier: Attended Healer ("Whenever you gain
// life for the first time each turn, create a 1/1 white Cat"). Two same-turn
// gains queue one trigger, not two.
func TestAttendedHealerFirstTimeLifeGainedQueuesOnce(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoardCard(t, e, 0, corpusCard(t, "Attended Healer"))
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 3})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != src {
		t.Fatalf("first gain queued %#v, want exactly one Attended Healer trigger", e.pendingTriggers)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("second same-turn gain queued %#v, want no second trigger", e.pendingTriggers)
	}
}

// PlayerTurn$ True ("whenever you gain life during your turn"): a gain on an
// opponent's turn queues nothing; the same gain on the controller's turn
// queues exactly one. Real corpus carrier: Vampire Scrivener's LifeGained
// line (its LifeLost twin shares the gate but does not match a gain).
func TestVampireScrivenerLifeGainedPlayerTurnGate(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoardCard(t, e, 0, corpusCard(t, "Vampire Scrivener"))
	e.G.Active = 1
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("gain on an opponent's turn queued %#v, want none (PlayerTurn$ True)",
			e.pendingTriggers)
	}
	e.G.Active = 0
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != src {
		t.Fatalf("gain on the controller's turn queued %#v, want exactly one trigger from %d",
			e.pendingTriggers, src)
	}
}

// ActivationLimit$ applies at queue time now the mode is in
// actionTriggerModes: "this ability triggers only once each turn" queues the
// first activation and blocks the second.
func TestLifeGainedActivationLimitCapsQueueTime(t *testing.T) {
	t.Parallel()
	e, src := lifeGainedFixture(t, "ActivationLimit$ 1")
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 2})
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != src {
		t.Fatalf("first activation queued %#v, want exactly one trigger from %d",
			e.pendingTriggers, src)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("second activation queued %#v, want ActivationLimit$ 1 to stop it",
			e.pendingTriggers)
	}
}

func TestTreebeardLifeGainedPutsThatManyCounters(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Treebeard, Gracious Host"))
	if got := e.G.Obj(source).Face().Triggers[1].Mode; got != "LifeGained" {
		t.Fatalf("Treebeard trigger mode = %q, want LifeGained", got)
	}

	// A life LOSS is not a gain: no trigger, no counters.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -3})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("life loss queued %v, want no Treebeard trigger", e.G.Stack)
	}

	// Gain 3 -> 3 +1/+1 counters on the answered target (Treebeard itself,
	// the only Halfling or Treefolk on the battlefield).
	gain(t, e, 0, 3)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != source {
		t.Fatalf("gain trigger stack = %v, want Treebeard's one trigger", e.G.Stack)
	}
	submitChoices(t, e, 0)
	e.resolveTop()
	if got := e.G.Obj(source).Counter("P1P1"); got != 3 {
		t.Fatalf("Treebeard counters after +3 = %d, want 3", got)
	}

	// Second, different value: gain 1 -> exactly 1 more counter.
	gain(t, e, 0, 1)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("second-gain trigger stack = %v, want one trigger", e.G.Stack)
	}
	submitChoices(t, e, 0)
	e.resolveTop()
	if got := e.G.Obj(source).Counter("P1P1"); got != 4 {
		t.Fatalf("Treebeard counters after +3 then +1 = %d, want 4", got)
	}
}

func TestSanguineBondDrainsTheGainedAmount(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Sanguine Bond"))

	// A life LOSS emits no Sanguine Bond trigger (the loss is seat 1's).
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -2})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("life loss queued %v, want no Sanguine Bond trigger", e.G.Stack)
	}

	// Gain 2 -> the targeted OPPONENT (seat 1) loses exactly 2; the caster
	// keeps the full gain.
	gain(t, e, 0, 2)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != source {
		t.Fatalf("gain trigger stack = %v, want Sanguine Bond's one trigger", e.G.Stack)
	}
	idx := findPlayerOption(t, e, 1)
	submitChoices(t, e, idx)
	e.resolveTop()
	if got := e.G.Players[0].Life; got != 22 {
		t.Fatalf("caster life after +2 = %d, want 22", got)
	}
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("targeted opponent life after drain = %d, want 16 (20, -2 loss, -2 drain)", got)
	}
}

func TestVanguardSeraphFirstLifeGainOnly(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoardCard(t, e, 0, corpusCard(t, "Vanguard Seraph"))
	if got := e.G.Obj(source).Face().Triggers[0].Params["FirstTime"]; got != "True" {
		t.Fatalf("Vanguard Seraph FirstTime = %q, want True", got)
	}

	// The first gain of the turn queues the trigger. emit logs the event
	// BEFORE checkTriggers runs, so the gate must find the current gain in
	// the log and still count it as first.
	gain(t, e, 0, 2)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != source {
		t.Fatalf("first-gain stack = %v, want Vanguard Seraph's one trigger", e.G.Stack)
	}

	// A second gain the same turn queues nothing. A matcher that returned
	// true on the first matching log entry would fire here.
	gain(t, e, 0, 2)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("second-gain stack = %v, want no second trigger", e.G.Stack)
	}
}
