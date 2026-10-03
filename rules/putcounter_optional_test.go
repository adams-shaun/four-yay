// Task put-optional: api:PutCounter.Optional$ True was unread -- every "you
// may put a counter" PutCounter ALWAYS put, unconditionally, with no
// election recorded anywhere in the log. effPutCounter
// (effects/counters.go) now poses a real yes/no KChoose through the shared
// Ask boundary with the "put_optional" resume arm (the attach_optional
// precedent, rules/attach_optional_test.go): the answer rides Ctx.PutOpt,
// the decline places nothing while the chained SubAbility$ still runs (the
// chain is owned by Resolve, never skipped by a decline -- the oracle texts
// agree; the chain-skip mechanism is the DIFFERENT UnlessCost$ +
// UnlessResolveSubs$ pair none of these lines carry), and the no-host
// fallback takes the deterministic decline (R-9) with option 0 = "yes" so
// the bot clamp keeps bot games byte-identical to the silent always-put.
//
// These are the engine-side end-to-end pins on REAL corpus cards, driven
// through a logged TriggerPush + resolveTop (the TestVampireLacerator
// pattern) or a real cast, with replayCheck on every test -- never copied
// script text (the licensing rule). The effects-package unit pins on the
// same SAs live in effects/putcounter_optional_test.go.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const gearTrinketSrc = "Name:Gear Trinket\nTypes:Artifact\nOracle:x\n"

// putCounterCountersOf sums obj's P1P1 counters.
func putCounterCountersOf(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("object %d gone", id)
	}
	n := 0
	for _, c := range o.Counters {
		if c.Kind == "P1P1" {
			n += int(c.N)
		}
	}
	return n
}

// counterChangeEventsFor returns the CounterChange events recorded for id.
func counterChangeEventsFor(e *Engine, id state.ObjID) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == id {
			out = append(out, ev)
		}
	}
	return out
}

// playerCounterEventsFor returns the PlayerCounterChange events of the named
// counter kind for player.
func playerCounterEventsFor(e *Engine, p state.PlayerID, kind string) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlayerCounterChange && ev.Player == p && ev.Counter == kind {
			out = append(out, ev)
		}
	}
	return out
}

// answerPutOptional submits an index against the pending put_optional
// election (0 = yes, 1 = no).
func answerPutOptional(t *testing.T, e *Engine, idx int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "put_optional" {
		t.Fatalf("pending = %+v, want the put_optional election", d)
	}
	submitChoices(t, e, idx)
}

// talusPaladinGame builds a 2-seat game with the real corpus Talus Paladin on
// seat 0's battlefield at Main 1 of turn 1, then pushes its entry trigger
// directly (the TestVampireLacerator pattern: the push IS the trigger match)
// and resolves it up to the may-put election.
func talusPaladinGame(t *testing.T, seed uint64) (*Engine /*cfg*/, Config, state.ObjID) {
	t.Helper()
	e, cfg, pal := gateFixture(t, seed, "Talus Paladin")
	pal = gateMoveFromLibrary(t, e, "Talus Paladin", state.ZBattlefield)
	e.emit(events.Event{Kind: events.TriggerPush, Obj: pal, Player: 0, Amount: 0})
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "put_optional" {
		t.Fatalf("pending = %+v, want the may-put election", d)
	}
	if d.Player != 0 {
		t.Fatalf("ask player = %d, want 0 (the controller)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("options = %+v, want yes then no (option 0 = yes)", d.Options)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("bounds %d..%d, want 1..1", d.Min, d.Max)
	}
	return e, cfg, pal
}

// TestZimonesHypothesisMayPutElectionWrapsThePick: the bare-Choices$ spell
// carrier asks the ELECTION FIRST (accept -> the recipient pick ask; decline
// -> neither), now that the bare-Choices$ pick machinery (task vow1) is
// merged. The chained DBGenericChoice runs on BOTH paths (the chain is owned
// by Resolve): api:GenericChoice is a registered primitive since it was
// routed to effCharm (effects/misc.go, pinned in
// rules/generic_choice_test.go), so after either answer the chain poses the
// chained modal ask (Odd/Even, mid-resolution via effCharm) and the test
// answers it so the stack drains.
func TestZimonesHypothesisMayPutElectionWrapsThePick(t *testing.T) {
	t.Parallel()
	const bearSrc = "Name:Test Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"
	news := func(seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
		e, cfg, zim := gateFixture(t, seed, "Zimone's Hypothesis", bearSrc, bearSrc)
		b1 := gateMoveFromLibrary(t, e, "Test Bear", state.ZBattlefield)
		b2 := gateMoveFromLibrary(t, e, "Test Bear", state.ZBattlefield)
		addMana(t, e, 0, "UUUUU") // 3UU
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("expected priority, got %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == zim {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no cast option for Zimone's Hypothesis: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit cast: %v", err)
		}
		if d := passUntilAsk(t, e); d == nil || d.Kind != decision.KChoose || d.ResumeKind != "put_optional" {
			t.Fatalf("pending after the cast = %+v, want the may-put election", d)
		}
		return e, cfg, zim, b1, b2
	}

	// Decline: no pick ask, no counter; the chained DBGenericChoice modal
	// ask (Odd/Even) follows the election, correctly mid-resolution now that
	// the primitive is registered.
	e, cfg, _, b1, b2 := news(917)
	answerPutOptional(t, e, 1) // no
	dDecline := e.Pending()
	if dDecline == nil || dDecline.Kind != decision.KModes || dDecline.ResumeKind != "modes" {
		t.Fatalf("pending after the decline = %+v, want the chained GenericChoice modes ask", dDecline)
	}
	labels := map[string]bool{}
	for _, o := range dDecline.Options {
		labels[o.Label] = true
	}
	if !labels["Odd"] || !labels["Even"] {
		t.Fatalf("modal options = %+v, want Odd and Even", dDecline.Options)
	}
	submitChoices(t, e, 0) // Odd; the bodies are outside this test's scope
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the decline: %+v", d)
	}
	for _, b := range []state.ObjID{b1, b2} {
		if evs := counterChangeEventsFor(e, b); len(evs) != 0 {
			t.Fatalf("decline countered bear %d: %+v", b, evs)
		}
	}
	replayCheck(t, e, cfg)

	// Accept: the recipient pick follows the election; the picked bear takes
	// the counter.
	e2, cfg2, _, c1, c2 := news(918)
	answerPutOptional(t, e2, 0) // yes
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_pick" {
		t.Fatalf("pending after the accept = %+v, want the counter_pick ask", d)
	}
	pickIdx := -1
	for _, o := range d.Options {
		if o.Obj == c1 {
			pickIdx = o.Index
		}
	}
	if pickIdx < 0 {
		t.Fatalf("bear %d not offered: %+v", c1, d.Options)
	}
	submitChoices(t, e2, pickIdx)
	if got := putCounterCountersOf(t, e2, c1); got != 1 {
		t.Fatalf("picked bear P1P1 = %d, want 1", got)
	}
	if got := putCounterCountersOf(t, e2, c2); got != 0 {
		t.Fatalf("unpicked bear P1P1 = %d, want 0", got)
	}
	dChain := e2.Pending()
	if dChain == nil || dChain.Kind != decision.KModes || dChain.ResumeKind != "modes" {
		t.Fatalf("pending after the accept = %+v, want the chained GenericChoice modes ask", dChain)
	}
	submitChoices(t, e2, 0)
	passUntilStackEmpty(t, e2, 60)
	replayCheck(t, e2, cfg2)
}
