package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Level B D8: a triggered ability whose effect sits behind a counter-removal
// cost (Cost$ SubCounter<1/P1P1>, Cost$ RemoveAnyCounter<1/Any/CARDNAME>)
// offers "pay" when the source holds the counters (CR 117.3 / 603.3). Before
// the fix the window offered only "Do not pay". The corpus cards come from
// the shared corpus registry (searchTestRegistry); the helpers are
// search_library_test.go's searchEngine/searchMoveByName and
// resolution_test.go's passUntilNonPriority.

// subCounterCostAsk drives seat 0's turn forward until the trigger-cost
// pay/decline ask is pending, answering an OptionalDecider "yes" and empty
// attack/block declarations on the way, and returns it.
func subCounterCostAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 200 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		switch d.Kind {
		case decision.KPriority:
			passUntilNonPriority(t, e, 200)
			continue
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
			continue
		}
		for _, o := range d.Options {
			if o.Kind == "trigger_cost_decline" {
				return d
			}
		}
		if len(d.Options) > 0 && d.Options[0].Kind == "yes" {
			// The OptionalDecider "you may" ask: option 0 accepts.
			submitChoices(t, e, 0)
			continue
		}
		t.Fatalf("unexpected decision while driving to the cost ask: %+v", d)
	}
	t.Fatal("never reached the trigger-cost ask")
	return nil
}

func subCounterPayIndex(d *decision.Decision) int {
	for _, o := range d.Options {
		if o.Kind == "trigger_cost_pay" {
			return o.Index
		}
	}
	return -1
}

// subCounterFixture builds the corpus engine with src on seat 0's
// battlefield carrying n counters of kind (any etbCounter replacement is
// bypassed by the direct move, so the count is exactly n).
func subCounterFixture(t *testing.T, src, kind string, n int32, extra ...string) (*Engine, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, append([]string{src}, extra...)...)
	id := searchMoveByName(t, e, src, state.ZBattlefield)
	if have := e.G.Obj(id).Counter(kind); have != n {
		e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: n - have})
	}
	if got := e.G.Obj(id).Counter(kind); got != n {
		t.Fatalf("%s holds %d %s counters, want %d (fixture)", src, got, kind, n)
	}
	e.pending = nil
	e.priorityRound()
	return e, id
}

func TestTriggeredCostSubCounterPayable(t *testing.T) {
	t.Run("SlumberingWalkerRemoveAnyCounter", func(t *testing.T) {
		e, walker := subCounterFixture(t, "Slumbering Walker", "M1M1", 2)
		// A creature card with power 2 or less in the graveyard, so the
		// reflexive "return target creature card" has a target.
		bear := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("Grizzly Bears is not in the graveyard (fixture)")
		}
		d := subCounterCostAsk(t, e)
		if e.G.Step != state.StepEnd {
			t.Fatalf("cost ask posed in step %s, want the end step", e.G.Step)
		}
		payIdx := subCounterPayIndex(d)
		if payIdx < 0 {
			t.Fatalf("RemoveAnyCounter<1/Any/CARDNAME> with 2 -1/-1 counters offers no pay: %+v", d.Options)
		}
		stackBefore := len(e.G.Stack)
		submitChoices(t, e, payIdx)
		if got := e.G.Obj(walker).Counter("M1M1"); got != 1 {
			t.Fatalf("Slumbering Walker M1M1 = %d after paying, want 1", got)
		}
		// The reflexive "when you do" trigger is now on the stack (or asking
		// for its target on the way there).
		reflexive := len(e.G.Stack) > stackBefore
		if p := e.Pending(); p != nil && p.Kind == decision.KTarget {
			reflexive = true
		}
		if !reflexive {
			t.Fatalf("paying the cost put no reflexive trigger on the stack (stack %d, pending %+v)", len(e.G.Stack), e.Pending())
		}
	})
	t.Run("AnyCounterKindChoice", func(t *testing.T) {
		// Two counter kinds on the source: paying the RemoveAnyCounter cost
		// asks which kind to remove, and only the picked kind drops.
		e, walker := subCounterFixture(t, "Slumbering Walker", "M1M1", 1)
		e.emit(events.Event{Kind: events.CounterChange, Obj: walker, Counter: "CHARGE", Amount: 1})
		if e.G.Obj(walker).Counter("CHARGE") != 1 || e.G.Obj(walker).Counter("M1M1") != 1 {
			t.Fatal("Slumbering Walker does not hold one M1M1 and one CHARGE counter (fixture)")
		}
		d := subCounterCostAsk(t, e)
		payIdx := subCounterPayIndex(d)
		if payIdx < 0 {
			t.Fatalf("RemoveAnyCounter with two counters offers no pay: %+v", d.Options)
		}
		submitChoices(t, e, payIdx)
		kind := e.Pending()
		if kind == nil || kind.Kind != decision.KChoose || len(kind.Options) != 2 {
			t.Fatalf("expected a two-option counter-kind ask, got %+v", kind)
		}
		pick := -1
		for _, o := range kind.Options {
			if o.Kind == "trigger_cost_counter" && o.Counter == "CHARGE" {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("counter-kind ask offers no CHARGE option: %+v", kind.Options)
		}
		submitChoices(t, e, pick)
		if got := e.G.Obj(walker).Counter("CHARGE"); got != 0 {
			t.Fatalf("CHARGE = %d after removing the picked counter, want 0", got)
		}
		if got := e.G.Obj(walker).Counter("M1M1"); got != 1 {
			t.Fatalf("M1M1 = %d, want 1 (the unpicked kind stays)", got)
		}
	})
	t.Run("GuidingHydraSubCounter", func(t *testing.T) {
		e, hydra := subCounterFixture(t, "Guiding Hydra", "P1P1", 2)
		bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
		if e.G.Obj(bear).Counter("P1P1") != 0 {
			t.Fatal("Grizzly Bears starts with a +1/+1 counter (fixture)")
		}
		d := subCounterCostAsk(t, e)
		payIdx := subCounterPayIndex(d)
		if payIdx < 0 {
			t.Fatalf("SubCounter<1/P1P1> with 2 +1/+1 counters offers no pay: %+v", d.Options)
		}
		submitChoices(t, e, payIdx)
		passUntilStackEmpty(t, e, 20)
		if got := e.G.Obj(hydra).Counter("P1P1"); got != 1 {
			t.Fatalf("Guiding Hydra P1P1 = %d after paying, want 1", got)
		}
		if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
			t.Fatalf("Grizzly Bears P1P1 = %d after the Hydra paid, want 1", got)
		}
	})
	t.Run("NoCountersDeclineOnly", func(t *testing.T) {
		// Slumbering Walker (4/7) survives with no counters; a 0-counter
		// Guiding Hydra is a 1/0 that dies before its trigger.
		e, walker := subCounterFixture(t, "Slumbering Walker", "M1M1", 0)
		if e.G.Obj(walker).Zone != state.ZBattlefield || e.G.Obj(walker).Counter("ALL") != 0 {
			t.Fatal("Slumbering Walker is not a counterless permanent (fixture)")
		}
		d := subCounterCostAsk(t, e)
		if len(d.Options) != 1 || d.Options[0].Kind != "trigger_cost_decline" {
			t.Fatalf("a source with no counters must offer only the decline, got %+v", d.Options)
		}
	})
}
