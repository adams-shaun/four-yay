package rules

// The attack payment window's source membership must admit a choice-shaped
// mana source (Produced$ Any / Combo <colours> / Chosen). Before this fix
// cards.ProducedCounts reported an Any production as one COLOURLESS unit and
// the membership walk required exactly that shape; once the parser reported
// the real five-colour alternatives the old check (total == 1 && counts[5]
// == 1) rejected every Any source, so an any-colour land could no longer pay
// a generic CantAttackUnless tax. The window must also actually TAP such a
// source without losing its colour choice: resolveManaAbility resolves inline
// and a choice-shaped Produced$ would pose a mid-resolution colour ask that
// attackPayAnswer's next tap ask silently displaces (e.resume is nil for a
// window, not a suspended resolution). The membership walk therefore pins the
// choice to one concrete colour before recording the ability.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAttackManaAnySourcePaysAGenericTax is the regression for the changed
// ProducedCounts result: Ghostly Prison charges {2}, the payer floats nothing
// and controls only untapped "Produced$ Any" lands. The pair must be offered
// (the affordability bound saw the sources), the window must tap exactly two,
// pose NO colour sub-ask, and commit the attack with the pool emptied.
func TestAttackManaAnySourcePaysAGenericTax(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeat(t, "Ghostly Prison", 0)
	anyLand := card(t, "Name:Cavern\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | Oracle:x\n")
	var lands []state.ObjID
	for i := 0; i < 2; i++ {
		lands = append(lands, onBoardCard(t, e, 1, anyLand))
	}
	// Precondition: the payer really has two untapped Any sources and an
	// empty pool, and {2} is out of reach without tapping.
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0", got)
	}
	if got := len(e.attackManaSources(1)); got != 2 {
		t.Fatalf("precondition: attackManaSources(1) = %d, want 2 (both Any lands)", got)
	}
	for _, id := range lands {
		if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Tapped {
			t.Fatalf("precondition: Any land %d not on battlefield untapped: %+v", id, o)
		}
	}

	e.askAttackers()
	d := e.Pending()
	var opt *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Player == 0 && o.Obj == bear {
			opt = o
		}
	}
	if opt == nil {
		t.Fatalf("An untapped Any source cannot pay {2}, pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged attack: %v", err)
	}

	for round := 0; round < 2; round++ {
		pay := e.Pending()
		if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) == 0 ||
			pay.Options[0].Kind != "attack_mana" {
			t.Fatalf("round %d: expected the attack payment window, got %+v", round, pay)
		}
		if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("tap Any source: %v", err)
		}
	}
	if pay := e.Pending(); pay != nil && pay.Kind == decision.KChoose && len(pay.Options) > 0 && pay.Options[0].Kind == "mana" {
		t.Fatalf("window lost its tap and fell through to a colour sub-ask: %+v", pay)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying {2} = %d, want 0 (the tax was paid, not stranded)", got)
	}
	for _, id := range lands {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("Any land %d not tapped as the payment", id)
		}
	}
	if o := e.G.Obj(bear); !o.IsAttacking {
		t.Fatal("charged attacker was never declared: the {2} was not paid")
	}
	drainCombatPriority(t, e)
}
