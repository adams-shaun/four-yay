package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestManaAbilityCounterIsPutByItsController pins CR 605: a mana ability
// resolves immediately and is never a stack object, so a counter its own
// effect chain puts (a SubAbility$ PutCounter) must be recorded in the
// Count$CountersAddedThisTurn ledger with the activating player as actor --
// not with whatever spell happens to top the stack (the old actionCause
// fallback), and not with nobody at an empty-stack priority.
func TestManaAbilityCounterIsPutByItsController(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	toMain1(t, e)
	src := onBoard(t, e, 0, "Name:Charge Rock Test\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ C | SubAbility$ DBCharge | SpellDescription$ Add C.\nSVar:DBCharge:DB$ PutCounter | Defined$ Self | CounterType$ CHARGE | CounterNum$ 1\nOracle:x\n")
	// Precondition: the fixture actually entered the battlefield under seat 0.
	if o := e.G.Obj(src); o == nil || o.Counter("CHARGE") != 0 {
		t.Fatalf("fixture not on the battlefield with 0 CHARGE (obj=%v)", o)
	}
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision pending")
	}
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
	if n := e.G.Obj(src).Counter("CHARGE"); n != 1 {
		t.Fatalf("CHARGE counters = %d, want 1 (fixture did not resolve)", n)
	}
	if got := len(e.counterAddsThisTurn); got != 1 {
		t.Fatalf("counterAddsThisTurn has %d entries, want the one CHARGE put by seat 0", got)
	}
	if a := e.counterAddsThisTurn[0]; a.actor != 0 || a.kind != "CHARGE" || a.amount != 1 {
		t.Errorf("ledger entry = actor %d kind %q amount %d, want seat 0 CHARGE 1", a.actor, a.kind, a.amount)
	}
}
