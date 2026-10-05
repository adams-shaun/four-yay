package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPumpReplaceDyingDefinedTargetedExiles pins Gnashing of Teeth's first
// mode end to end: its -5/-5 makes the targeted bear lethal, and the
// ReplaceDyingDefined$ Targeted rider exiles it instead of sending it to the
// graveyard. Before 73c9cc2e5, Pump did not register that replacement.
func TestPumpReplaceDyingDefinedTargetedExiles(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	gnash := mustCorpusCard(t, reg, "Gnashing of Teeth")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 7331, []*cards.Card{gnash, bear}, nil)
	cardToHand(t, e, gnash)
	bear0 := moveOwnerCard(t, e, 0, bear, state.ZBattlefield)
	if o := e.G.Obj(bear0); o.Zone != state.ZBattlefield || e.Toughness(bear0) != 2 {
		t.Fatalf("precondition: target bear is zone %s with toughness %d, want battlefield 2/2", o.Zone, e.Toughness(bear0))
	}
	addMana(t, e, 0, "BBB")
	castFirst(t, e, "cast")
	submitChoices(t, e, modeOptionContaining(t, e.Pending(), "-5/-5"))
	targetObject(t, e, bear0)
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(bear0); o.Zone != state.ZExile {
		t.Fatalf("the -5/-5'd bear should be exiled, not dying: %s", o.Zone)
	}
	n := 0
	for _, ce := range e.continuous {
		if ce.ReplacementEvent == "Moved" {
			n++
			if len(ce.Remembered) != 1 || ce.Remembered[0] != bear0 {
				t.Fatalf("replacement remembered %v, want the bear %d", ce.Remembered, bear0)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one Moved replacement registered, got %d", n)
	}
	replayCheck(t, e, cfg)
}
