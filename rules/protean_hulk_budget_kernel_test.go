package rules

// Kernel-era restorations of the tests W3 removed from protean_hulk_budget_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestProteanHulkBudgetSearchEndToEnd: killing the Hulk asks the budgeted
// library search whose options exclude the over-budget Kraken, an
// over-budget answer is rejected on the wire, and the answered (in-budget)
// pair lands exactly those two creatures on the battlefield.
func TestProteanHulkBudgetSearchEndToEndKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, window := hulkWindowEngine(t, reg, 4409)
	// Kill the Hulk: Battlefield -> Graveyard fires the death trigger.
	hulkID := findByName(e, "Protean Hulk", 0)
	if hulkID == 0 {
		t.Fatal("Protean Hulk is not on the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: hulkID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()  // begin the death trigger's resolution; the search ask suspends it
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want the budget search ask", d)
	}
	if d.MaxSum != 6 {
		t.Fatalf("MaxSum = %d, want 6 (WithTotalCMC$ 6)", d.MaxSum)
	}
	// The 11-MV Polar Kraken is individually unaffordable and must not be
	// offered; every offered option names its own mana value.
	offerSet := map[state.ObjID]bool{}
	for _, o := range d.Options {
		offerSet[o.Obj] = true
		if o.Value > 6 {
			t.Fatalf("option %+v exceeds the budget 6 and must not be offered", o)
		}
	}
	if offerSet[window[0]] {
		t.Fatalf("the 11-MV Polar Kraken was offered under a budget of 6: options=%+v", d.Options)
	}
	for _, i := range []int{1, 2, 3, 4, 6} {
		if !offerSet[window[i]] {
			t.Fatalf("budget-eligible window card %d was not offered: options=%+v", window[i], d.Options)
		}
	}
	idxOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		t.Fatalf("card %d not offered", id)
		return -1
	}
	// Over-budget answers rejected on the wire: 4+5=9 and 4+2+6=12 both
	// exceed the budget 6.
	for _, pair := range [][]state.ObjID{{window[1], window[6]}, {window[1], window[2], window[3]}} {
		choices := make([]int, 0, len(pair))
		for _, id := range pair {
			choices = append(choices, idxOf(id))
		}
		if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err == nil {
			t.Fatalf("an over-budget answer %v validated under the budget 6", choices)
		}
	}
	// Take exactly Hill Giant + a Bear (sum 6).
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{idxOf(window[1]), idxOf(window[2])}}); err != nil {
		t.Fatalf("in-budget answer rejected: %v", err)
	}
	// Drain the rest of the (empty) stack.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		e.resolveTop()
	}
	// Hill Giant and the Bear are on the battlefield; the Kraken, Craw Wurm
	// and Serra Angel are not, and they stay in seat 0's library.
	if findByName(e, "Hill Giant", 0) == 0 || findByName(e, "Grizzly Bears", 0) == 0 {
		t.Fatal("the answered creatures were not put onto the battlefield")
	}
	for _, name := range []string{"Polar Kraken", "Craw Wurm", "Serra Angel"} {
		if id := findByName(e, name, 0); id != 0 {
			if o := e.G.Obj(id); o.Zone == state.ZBattlefield {
				t.Fatalf("%s (%d) reached the battlefield under a budget of 6", name, id)
			}
		}
	}
	replayCheck(t, e, cfg)
}
