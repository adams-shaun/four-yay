package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestUnlessCostOfferExcludesInstantSpeedSources is the review regression for
// the InstantSpeed$ offer/window mismatch: UnlessCostPayable must compute its
// reach from the SAME membership the mid-resolution window can actually tap,
// so an InstantSpeed$ True mana ability ("Activate only as an instant", Lion's
// Eye Diamond) must not make a {U} tax look payable. Before the fix the offer
// gate folded in AvailableMana, which counts priority-window abilities
// including InstantSpeed$; the window (untappedManaSource / the activation
// gate) withholds them, so Pay was offered with no activation to complete it
// and the payment necessarily declined.
func TestUnlessCostOfferExcludesInstantSpeedSources(t *testing.T) {
	t.Parallel()
	// A bare-tap, single-ability U source that is ONLY activatable at priority.
	led := card(t, "Name:Instant Lotus\nManaCost:0\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ T | Produced$ U | InstantSpeed$ True\nOracle:x\n")
	// A plain ordinary U source, for the positive control.
	island := card(t, "Name:Plain Island\nTypes:Basic Land Island\n"+
		"A:AB$ Mana | Cost$ T | Produced$ U\nOracle:x\n")

	e := handEngine(t, island)
	e.G.Players[0].Pool = state.Mana{}

	// Precondition: the InstantSpeed land is on the battlefield and untapped,
	// and the engine's own payment-window gate refuses it.
	ledID := onBoardCard(t, e, 0, led)
	if o := e.G.Obj(ledID); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("InstantSpeed source precondition failed: %+v", o)
	}
	if e.untappedManaSource(0, ledID) {
		t.Fatal("InstantSpeed source is a payment-window source — premise false")
	}
	if e.UnlessCostPayable(0, "U") {
		t.Fatal("InstantSpeed-only U source made a {U} unless cost payable; the window cannot tap it")
	}

	// Positive control: the ordinary Island DOES make it payable, proving the
	// gate is not simply failing closed for every source.
	plainID := onBoardCard(t, e, 0, island)
	if !e.untappedManaSource(0, plainID) {
		t.Fatal("Plain Island is not a payment-window source — premise false")
	}
	if !e.UnlessCostPayable(0, "U") {
		t.Fatal("an ordinary untapped Island must make a {U} unless cost payable")
	}
	// And the ordinary source by itself supplies exactly one U, not more: a
	// {U}{U} tax is still unreachable.
	if e.UnlessCostPayable(0, "U U") {
		t.Fatal("one Island made {U}{U} payable")
	}
}
