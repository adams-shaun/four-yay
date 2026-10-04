package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Restored from the W3 legacy removal.

// TestAoTheDawnSkyBudgetDigEndToEnd is the real-corpus pin: Ao's death trigger
// asks its Charm modes, the chosen Dig mode poses a budget ask whose options
// exclude the 5- and 6-MV permanents, an over-budget answer is rejected on
// the wire, and the answered (in-budget) take lands exactly the chosen card.
func TestAoTheDawnSkyBudgetDigEndToEnd(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, window := aoWindowEngine(t, reg, 9137)
	// Kill Ao: Battlefield -> Graveyard fires the death trigger.
	aoID := findByName(e, "Ao, the Dawn Sky", 0)
	if aoID == 0 {
		t.Fatal("Ao is not on the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: aoID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil // the fixture moves replace the stale priority snapshot
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the Charm placement KModes ask", d)
	}
	// Choose the Dig mode (declared first).
	submitChoices(t, e, 0)
	e.pending = nil // the kernel probe serves asks only with no decision posed
	e.resolveTop()
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "dig" {
		t.Fatalf("pending = %+v, want the budget Dig ask", d)
	}
	if d.MaxSum != 4 {
		t.Fatalf("MaxSum = %d, want 4 (WithTotalCMC$ 4)", d.MaxSum)
	}
	// The 5-MV permanents (Air Elemental, Serra Angel) and the 6-MV Craw Wurm
	// are individually unaffordable and must not be offered.
	offerSet := map[state.ObjID]bool{}
	for _, o := range d.Options {
		offerSet[o.Obj] = true
		if o.Value > 4 {
			t.Fatalf("option %+v exceeds the budget 4 and must not be offered", o)
		}
	}
	if offerSet[window[0]] || offerSet[window[3]] || offerSet[window[6]] {
		t.Fatalf("offered a budget-exceeding card: options=%+v window=%v", d.Options, window)
	}
	if !offerSet[window[1]] || !offerSet[window[2]] || !offerSet[window[4]] {
		t.Fatalf("the three budget-eligible permanents were not all offered: options=%+v", d.Options)
	}
	// Map option index by object so the answer names exactly Hill Giant.
	idxOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		t.Fatalf("card %d not offered", id)
		return -1
	}
	// An over-budget pair (4 + 2 = 6 > 4) must not validate.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{idxOf(window[1]), idxOf(window[2])}}); err == nil {
		t.Fatal("a 4+2 pair exceeding the budget 4 validated")
	}
	// Take exactly Hill Giant.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{idxOf(window[1])}}); err != nil {
		t.Fatalf("in-budget answer rejected: %v", err)
	}
	// Drain the rest of the (empty) stack.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		e.pending = nil
		e.resolveTop()
	}
	// Hill Giant is on the battlefield; the budget-exceeding permanents are
	// not, and the rest of the window stays on seat 0's library.
	if findByName(e, "Hill Giant", 0) == 0 {
		t.Fatal("Hill Giant was not put onto the battlefield")
	}
	for _, name := range []string{"Air Elemental", "Serra Angel", "Craw Wurm"} {
		if id := findByName(e, name, 0); id != 0 {
			o := e.G.Obj(id)
			if o.Zone == state.ZBattlefield {
				t.Fatalf("%s (%d) reached the battlefield under a budget of 4", name, id)
			}
		}
	}
	replayCheck(t, e, cfg)
}
