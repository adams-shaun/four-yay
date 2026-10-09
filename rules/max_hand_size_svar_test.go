package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The SVar-valued SetMaxHandSize$ read (ticket
// agent-20261009T055739Z-b43f3480): Winter, Misanthropic Guide prints
// "Delirium -- each opponent's maximum hand size is equal to seven minus the
// number of those card types", Y = Number$7/Minus.X over X = the card types
// in the controller's graveyard. maxHandSizeFor prices the SVar through the
// shared count evaluator and evaluates the static's own Condition$ Delirium
// gate, so the maximum is 7 - types only while Delirium holds and the
// default otherwise.

// winterSrc is Winter's real compiled static.
const winterSrc = "Name:Winter, Misanthropic Guide\nManaCost:1 B R G\nTypes:Legendary Creature Human Warlock\nPT:3/4\n" +
	"S:Mode$ Continuous | Condition$ Delirium | Affected$ Opponent | SetMaxHandSize$ Y | Description$ Delirium -- each opponent's maximum hand size is equal to seven minus the number of those card types.\n" +
	"SVar:X:Count$ValidGraveyard Card.YouOwn$CardTypes\n" +
	"SVar:Y:Number$7/Minus.X\nOracle:x\n"

// The three non-land card types the graveyard fixture needs.
const (
	instantTestSrc = "Name:Test Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"
	sorceryTestSrc = "Name:Test Divination\nManaCost:1 U\nTypes:Sorcery\nOracle:x\n"
)

// TestMaxHandSizeSVarDeliriumValue pins the priced maximum and its gate:
// with four card types in the CONTROLLER's graveyard the affected opponent's
// maximum is 7-4 = 3, the controller's is the default (Affected$ Opponent),
// and once the graveyard drops below four types Delirium fails and the
// opponent's maximum returns to the default. The distinct-type count is
// asserted before each read, so a fixture that did not make the count what
// the test claims cannot pass silently.
func TestMaxHandSizeSVarDeliriumValue(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	src := onBoardGrant(t, e, 0, winterSrc)
	if e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Winter not on the battlefield")
	}
	if got := e.maxHandSizeFor(0); got != maxHandSize {
		t.Fatalf("controller max hand size = %d, want the default %d (Affected$ Opponent)", got, maxHandSize)
	}
	if got := e.maxHandSizeFor(1); got != maxHandSize {
		t.Fatalf("opponent max hand size before Delirium = %d, want the default %d", got, maxHandSize)
	}
	graveCard(e, card(t, landSrc("Mountain")), 0, 0)
	instant := graveCard(e, card(t, instantTestSrc), 0, 0)
	graveCard(e, card(t, sorceryTestSrc), 0, 0)
	graveCard(e, card(t, creatureSrc("Test Bear")), 0, 0)
	if got := e.graveyardCardTypeCount(0); got != 4 {
		t.Fatalf("precondition: graveyard card types = %d, want 4 for Delirium", got)
	}
	if got := e.maxHandSizeFor(1); got != 3 {
		t.Fatalf("opponent max hand size under Delirium = %d, want 3 (7 minus 4 types)", got)
	}
	if got := e.maxHandSizeFor(0); got != maxHandSize {
		t.Fatalf("controller max hand size under Delirium = %d, want the default %d", got, maxHandSize)
	}
	// Below four types the gate fails: the maximum returns to the default.
	e.emit(events.Event{Kind: events.MoveZone, Obj: instant, From: state.ZGraveyard, To: state.ZExile})
	if got := e.graveyardCardTypeCount(0); got != 3 {
		t.Fatalf("precondition: graveyard card types after the exile = %d, want 3", got)
	}
	if got := e.maxHandSizeFor(1); got != maxHandSize {
		t.Fatalf("opponent max hand size with Delirium off = %d, want the default %d", got, maxHandSize)
	}
}
