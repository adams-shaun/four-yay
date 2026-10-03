package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// kindRuns run-length encodes a slice of events as (kind, count) pairs.
func kindRuns(evs []events.Event) [][2]int {
	var runs [][2]int
	for _, ev := range evs {
		if n := len(runs); n > 0 && runs[n-1][0] == int(ev.Kind) {
			runs[n-1][1]++
			continue
		}
		runs = append(runs, [2]int{int(ev.Kind), 1})
	}
	return runs
}

// A seat that pays its last life to a PayLife mana ability loses to
// state-based actions while that ability's colour choice is posed. Releasing
// the departed seat's decision must also end the flow it routed: the next
// seat's priority is posed with no choose flow armed and no parked colour
// activation (the A/B payment-plan mirror measured both surviving).
func TestStateBasedLossDuringManaColourAskClearsTheFlow(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 3)
	toMain1(t, e)
	src := onBoard(t, e, 0, "Name:Confluence Test\nTypes:Land\nA:AB$ Mana | Cost$ T PayLife<1> | Produced$ Any | SpellDescription$ Add one mana of any color.\nOracle:x\n")
	e.G.Obj(src).SummonSick = false
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: 1 - e.G.Players[0].Life})
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	pick := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == src {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("no activate option for the fixture in %#v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{pick}}); err != nil {
		t.Fatal(err)
	}
	if !e.G.Players[0].Lost {
		t.Fatalf("seat 0 life %d, lost %v: the fixture did not reach the state-based loss", e.G.Players[0].Life, e.G.Players[0].Lost)
	}
	if d := e.Pending(); d == nil || d.Player == 0 {
		t.Fatalf("pending = %#v, want a surviving seat's decision", d)
	}
	if e.choosing != chooseNone || e.manaColorActivation != nil {
		t.Errorf("after seat 0 left the game: choosing=%d manaColorActivation set=%v, want the lost seat's colour flow cleared",
			e.choosing, e.manaColorActivation != nil)
	}
}
