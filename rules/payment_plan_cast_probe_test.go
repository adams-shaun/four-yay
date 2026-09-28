package rules

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestPaymentPlanWithholdsCastWhoseTargetsLeaveWithTheCard pins the round-9
// cardfuzz explore finding (seed 9606575608234985872, planrev "cast aborted:
// no legal target (plan-only) {Guiding Bolt}"): Empyrial Armor ("+1/+1 for
// each card in your hand") on an opponent's creature lifted it to the power
// Guiding Bolt's "target creature with power 4 or greater" needs only while
// the Bolt was still in hand. CR 601.2a moves the card to the stack before
// CR 601.2c asks for targets, so the planned cast found no legal target and
// reversed (CR 733.1). The payment offer now judges the target census with
// the card on the stack (castprobe.go).
//
// Fixture: every opposing creature gets +X/+0, X = cards in seat 0's hand.
// With n cards in hand, a 0/1 is an n/1 before the cast and an (n-1)/1 once
// a spell leaves the hand. "powerGE n" is feasible only in hand (withheld);
// "powerGE n-1" stays feasible on the stack (offered).
func TestPaymentPlanWithholdsCastWhoseTargetsLeaveWithTheCard(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9630, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
	for i := 0; i < 2; i++ {
		onBoard(t, e, 0, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n")
	}
	onBoard(t, e, 0, "Name:Hand Glory\nManaCost:W\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.OppCtrl | AddPower$ X | Description$ x\n"+
		"SVar:X:Count$ValidHand Card.YouOwn\nOracle:x\n")
	dummy := onBoard(t, e, 1, "Name:Target Dummy\nManaCost:1\nTypes:Creature Test\nPT:0/1\nOracle:x\n")
	// The fixture Plains in hand is played nowhere; the two bolts join the
	// hand, so n counts them.
	n := len(e.G.Zone(state.ZHand, 0)) + 2
	bolt := func(name string, ge int) state.ObjID {
		return putInHand(t, e, 0, card(t, fmt.Sprintf("Name:%s\nManaCost:W\nTypes:Instant\n"+
			"A:SP$ Destroy | ValidTgts$ Creature.powerGE%d | TgtPrompt$ x\nOracle:x\n", name, ge)))
	}
	vanishing := bolt("Vanishing Bolt", n)
	steady := bolt("Steady Bolt", n-1)
	toMain1(t, e)
	if got := int(e.Power(dummy)); got != n {
		t.Fatalf("precondition: dummy power = %d, want %d (cards in hand)", got, n)
	}
	sa := e.G.Obj(vanishing).Face().SpellAbility()
	if !e.castTargetsAvailable(0, vanishing, sa) {
		t.Fatal("precondition: the in-hand census must admit the vanishing bolt (the finding's shape)")
	}
	handBefore := slices.Clone(e.G.Zone(state.ZHand, 0))
	stackBefore := slices.Clone(e.G.Stack)
	if e.castTargetsAvailableOnStack(0, vanishing) {
		t.Error("on-stack census admits the vanishing bolt; its only target shrinks below power n once it leaves the hand")
	}
	if !e.castTargetsAvailableOnStack(0, steady) {
		t.Error("on-stack census withholds the steady bolt, whose target stays legal")
	}
	// The probe is a scoped read: zones, the object and the derived power
	// are exactly as before.
	if !slices.Equal(e.G.Zone(state.ZHand, 0), handBefore) || !slices.Equal(e.G.Stack, stackBefore) {
		t.Fatalf("probe left hand %v stack %v, want %v %v", e.G.Zone(state.ZHand, 0), e.G.Stack, handBefore, stackBefore)
	}
	if z := e.G.Obj(vanishing).Zone; z != state.ZHand {
		t.Fatalf("probe left the bolt in zone %s", z)
	}
	if got := int(e.Power(dummy)); got != n {
		t.Fatalf("after the probe dummy power = %d, want %d", got, n)
	}

	d := paymentPlanReask(t, e)
	var offered []state.ObjID
	for _, a := range d.PaymentActions {
		if len(a.Plans) > 0 {
			offered = append(offered, a.Cast.Object)
		}
	}
	if slices.Contains(offered, vanishing) {
		t.Errorf("payment actions offer the vanishing bolt (%v): its planned cast would reverse with no legal target", offered)
	}
	if !slices.Contains(offered, steady) {
		t.Errorf("payment actions %v miss the steady bolt", offered)
	}
}
