package rules

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestPaymentPlanWithholdsCastWhoseSourceGateLeavesWithTheCard pins the
// round-10 paymirror finding (random2 seed 11828 seq 4243, "Vorinclex, Voice
// of Hunger": a_fallback=source_changed, cast aborted "cost no longer
// payable"). The plan tapped Fanatic of Rhonas for {G}{G}{G}{G} ("Activate
// only if you control a creature with power 4 or greater"); the only such
// creature was Syr Elenora, whose power is the number of cards in its
// controller's hand. CR 601.2a moves the spell to the stack before CR 601.2g
// opens the mana-ability window the plan executes in, so the hand was one
// card smaller there, Syr Elenora was a 3/4, the ability could not be
// activated, and the planned cast fell back and reversed (CR 733.1). The
// payment offer now proves every planned activation with the card on the
// stack (castprobe.go paymentPlanHoldsOnStack) and withholds a plan that
// only holds while the card is in hand.
//
// Fixture: seat 0's own 0/1 gets +X/+0, X = cards in seat 0's hand, and a
// dork's {T}: Add {W}{W}{W} is gated on "a creature you control with power
// GE g". A {3} spell can be paid only by that ability. With n cards in hand
// (the spell included) the gate g = n holds only in hand (withheld); g = n-1
// holds on the stack as well (offered).
func TestPaymentPlanWithholdsCastWhoseSourceGateLeavesWithTheCard(t *testing.T) {
	for _, tc := range []struct {
		name  string
		slack int // the gate is powerGE n-slack
		offer bool
	}{
		{"gate_n_in_hand_only", 0, false},
		{"gate_n_minus_1_holds_on_stack", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, _ := newFixtureDeck(t, 10828, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
			onBoard(t, e, 0, "Name:Hand Glory\nManaCost:W\nTypes:Enchantment\n"+
				"S:Mode$ Continuous | Affected$ Creature.Elf+YouCtrl | AddPower$ X | Description$ x\n"+
				"SVar:X:Count$ValidHand Card.YouOwn\nOracle:x\n")
			counter := onBoard(t, e, 0, "Name:Hand Counter\nManaCost:1\nTypes:Creature Elf\nPT:0/1\nOracle:x\n")
			spell := putInHand(t, e, 0, card(t, "Name:Big Idea\nManaCost:3\nTypes:Sorcery\n"+
				"A:SP$ Draw | NumCards$ 1 | SpellDescription$ x\nOracle:x\n"))
			toMain1(t, e)
			n := len(e.G.Zone(state.ZHand, 0))
			dork := onBoard(t, e, 0, fmt.Sprintf("Name:Gated Dork\nManaCost:G\nTypes:Creature Test\nPT:1/1\n"+
				"A:AB$ Mana | Cost$ T | Produced$ W | Amount$ 3 | IsPresent$ Creature.YouCtrl+powerGE%d | SpellDescription$ x\n"+
				"Oracle:x\n", n-tc.slack))
			e.G.Obj(dork).SummonSick = false
			e.G.Obj(counter).SummonSick = false
			if got := int(e.Power(counter)); got != n {
				t.Fatalf("precondition: counter power = %d, want %d (cards in hand)", got, n)
			}

			handBefore := slices.Clone(e.G.Zone(state.ZHand, 0))
			d := paymentPlanReask(t, e)
			var action *decision.PaymentAction
			for i := range d.PaymentActions {
				if d.PaymentActions[i].Cast.Object == spell && len(d.PaymentActions[i].Plans) > 0 {
					action = &d.PaymentActions[i]
				}
			}
			if !slices.Equal(e.G.Zone(state.ZHand, 0), handBefore) || e.G.Obj(spell).Zone != state.ZHand {
				t.Fatalf("the offer's probe left hand %v (spell zone %s), want %v", e.G.Zone(state.ZHand, 0), e.G.Obj(spell).Zone, handBefore)
			}
			if got := int(e.Power(counter)); got != n {
				t.Fatalf("after the offer counter power = %d, want %d", got, n)
			}
			if !tc.offer {
				if action != nil {
					t.Fatalf("payment actions offer %+v: its gated source is unavailable once the spell leaves the hand, so the planned cast would reverse", action.Plans[0])
				}
				return
			}
			if action == nil {
				t.Fatalf("payment actions %+v miss the spell whose gated source holds on the stack", d.PaymentActions)
			}
			submitPaymentPlan(t, e, d, *action)
			if z := e.G.Obj(spell).Zone; z != state.ZStack {
				t.Fatalf("planned cast ended in zone %s, want the stack", z)
			}
		})
	}
}
