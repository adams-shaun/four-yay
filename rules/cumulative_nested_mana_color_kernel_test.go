package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCumulativeWindowSubAbilityColourChoiceResumesWindow(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	charm := onBoard(t, e, 0, upkeepEnchantment)
	grotto := onBoard(t, e, 0, luckGrotto)
	e.emit(events.Event{Kind: events.CounterChange, Obj: grotto, Counter: "LUCK", Amount: 1})
	resolveUpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil || !strings.Contains(d.Prompt, "cumulative upkeep") {
		t.Fatalf("precondition: want the cumulative mana window, got %+v", d)
	}
	// Tap the grotto for mana inside the window. Before the fix this panicked.
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == grotto }))
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || !strings.Contains(strings.ToLower(d.Prompt), "colo") {
		t.Fatalf("want the sub-ability's colour ask, got %+v", d)
	}
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Label == "Add U" }))
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after the colour answer = %d, want 1", got)
	}
	// The payment window resumed: the cost is now payable, so the pay/sac ask.
	d = e.Pending()
	if d == nil {
		t.Fatal("the payment window was dropped after the colour answer")
	}
	pay := optionIndex(t, d, func(o decision.Option) bool { return o.Kind == "cumulative_pay" })
	submitChoices(t, e, pay)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("paying the upkeep left %d mana in the pool", got)
	}
	if o := e.G.Obj(charm); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the upkeep was paid but the permanent left the battlefield")
	}
	if e.cumulative != nil {
		t.Fatal("the cumulative window never closed")
	}
}
