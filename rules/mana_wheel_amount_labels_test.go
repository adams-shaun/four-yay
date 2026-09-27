package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestManaWheelLabelsDistinguishAnyColourAmounts is the regression for the
// stage-1 "choose a mana ability" wheel collapsing a source's any-colour
// abilities that differ only in amount into one "Add any color" label.
// Sceptre of Eternal Glory's "Add one mana of any color" and "Add three mana
// of any one color" both read "Add any color", so a manual payer could not
// choose the larger ability on purpose (task mana-wheel-amount-labels).
func TestManaWheelLabelsDistinguishAnyColourAmounts(t *testing.T) {
	e := newSeats(t, 2)
	toMain1(t, e)
	src := onBoard(t, e, 0, "Name:Sceptre Test\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ Any | SpellDescription$ Add one.\nA:AB$ Mana | Cost$ T | Produced$ Any | Amount$ 3 | SpellDescription$ Add three.\nOracle:x\n")
	// Precondition: the fixture must be a seat-0 artifact on the battlefield
	// with an untapped tap ability -- otherwise the activate option below
	// never appears and the whole test is vacuous.
	if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: fixture on seat 0's battlefield: %+v", o)
	}
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
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("pending = %#v, want the two-option mana ability wheel", d)
	}
	// Precondition: both options belong to the fixture and are the wheel's
	// mana options, so the label comparison below is measuring the abilities
	// this test is about.
	for _, o := range d.Options {
		if o.Kind != "mana" || o.Obj != src {
			t.Fatalf("wheel option %+v is not a mana ability of the fixture %d", o, src)
		}
	}
	if d.Options[0].Label == d.Options[1].Label {
		t.Errorf("wheel offers two indistinguishable options %q (one adds 1 mana, the other 3)", d.Options[0].Label)
	}
	// The label belongs to the right ability: the Amount$ 3 option is the
	// one that reads its amount, and choosing it adds three mana of one
	// colour, not one.
	threeIdx := -1
	for _, o := range d.Options {
		if o.Ability == 1 {
			threeIdx = o.Index
			if o.Label != "Add three mana of any one color" {
				t.Errorf("Amount$ 3 Any option label = %q, want %q", o.Label, "Add three mana of any one color")
			}
		}
	}
	if threeIdx < 0 {
		t.Fatalf("no option carries ability index 1: %+v", d.Options)
	}
	submitChoices(t, e, threeIdx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
		t.Fatalf("stage-2 Any colour decision = %+v, want the five-colour ask", d)
	}
	submitChoices(t, e, manaOption(t, d, "G"))
	if got := e.G.Players[0].Pool[state.MG]; got != 3 {
		t.Fatalf("pool after choosing the Amount$ 3 ability = %d green, want 3", got)
	}
}
