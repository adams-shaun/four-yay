package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins War Room, the corpus's only carrier of BOTH the
// ActivationGameTypes$ format gate and the Count$ColorsColorIdentity count
// head: "{3}, {T}, Pay life equal to the number of colors in your
// commanders' color identity: Draw a card.", offered only in a game of
// Commander (Brawl/TinyLeaders/Oathbreaker are not modelled formats here).
//
// Before the fix the ability was dead everywhere, fail-closed: fixLifeXCost
// could not resolve SVar:X (Count$ColorsColorIdentity had no evalCountBody
// head), so offerCastable withheld it -- including in a Commander game,
// where the ability should be playable and priced at the identity count.
// The fix adds the count head (through the Host's
// CommanderIdentityColourCount, replay-derivable) AND the ActivationGameTypes$
// gate in the same change: without the gate the now-resolvable count (0 in
// a Constructed game, which has no commanders) would have made the ability
// a free {3},{T} draw there -- the worse defect.

// TestWarRoomDrawWithheldInConstructed: in FormatConstructed the draw
// ability is NOT among seat 0's priority options (ActivationGameTypes$ has
// no Constructed token), while the land's mana ability still is.
func TestWarRoomDrawWithheldInConstructed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	warRoom := mustCorpusCard(t, reg, "War Room")
	valgavoth := corpusCommander(t, reg, "Valgavoth, Harrower of Souls")
	e, cfg := colourIdentityGame(t, 83, FormatConstructed, valgavoth, nil, warRoom)
	room := moveToBattlefieldByName(t, e, 0, "War Room")
	addMana(t, e, 0, "CCC")
	if _, ok := findAbilityOption(e, room, 1); ok {
		t.Fatal("War Room's format-gated draw ability offered in a Constructed game")
	}
	if !hasActivateOption(e, room) {
		t.Fatal("War Room's mana ability should still be offered in Constructed")
	}
	commanderReplayCheck(t, e, cfg)
}

// TestWarRoomDrawOfferedInCommanderPricesTwoColourIdentity: in
// FormatCommander with a two-colour commander (Valgavoth, Harrower of
// Souls → B|R, count 2) the draw ability IS offered, activating it costs
// {3} + {T} + 2 life, and the draw resolves -- the full settle path
// (fixLifeXCost folds the resolvable count into Cost.Life, payMana charges
// it) end to end.
func TestWarRoomDrawOfferedInCommanderPricesTwoColourIdentity(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	warRoom := mustCorpusCard(t, reg, "War Room")
	valgavoth := corpusCommander(t, reg, "Valgavoth, Harrower of Souls")
	e, cfg := colourIdentityGame(t, 84, FormatCommander, valgavoth, nil, warRoom)
	room := moveToBattlefieldByName(t, e, 0, "War Room")
	// Fund {3} from the test pool (War Room itself stays untapped: the
	// ability's {T} part needs it untapped).
	addMana(t, e, 0, "CCC")
	opt, ok := findAbilityOption(e, room, 1)
	if !ok {
		t.Fatalf("draw ability not offered in Commander: %+v", e.Pending().Options)
	}
	if opt.Label != "War Room: Draw a card." {
		t.Fatalf("label %q, want the ability's SpellDescription", opt.Label)
	}
	life0 := e.G.Players[0].Life
	hand0 := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool = %v, want the {3} paid at activation", pool)
	}
	if got := e.G.Players[0].Life; got != life0-2 {
		t.Fatalf("life = %d, want %d (identity count 2 paid)", got, life0-2)
	}
	if len(e.G.Zone(state.ZHand, 0)) != hand0 {
		t.Fatal("hand changed before the ability resolved")
	}
	passUntilStackEmpty(t, e, 20)
	if len(e.G.Zone(state.ZHand, 0)) != hand0+1 {
		t.Fatalf("hand = %d, want %d (the draw resolved)", len(e.G.Zone(state.ZHand, 0)), hand0+1)
	}
	commanderReplayCheck(t, e, cfg)
}

// TestWarRoomDrawPricesSingleColourIdentity: a single-colour commander
// (Krenko, Mob Boss → R) prices the life part at 1.
func TestWarRoomDrawPricesSingleColourIdentity(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	warRoom := mustCorpusCard(t, reg, "War Room")
	krenko := corpusCommander(t, reg, "Krenko, Mob Boss")
	e, cfg := colourIdentityGame(t, 85, FormatCommander, krenko, nil, warRoom)
	room := moveToBattlefieldByName(t, e, 0, "War Room")
	addMana(t, e, 0, "CCC")
	opt := abilityOption(t, e, room, 1)
	life0 := e.G.Players[0].Life
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Life; got != life0-1 {
		t.Fatalf("life = %d, want %d (identity count 1 paid)", got, life0-1)
	}
	passUntilStackEmpty(t, e, 20)
	commanderReplayCheck(t, e, cfg)
}
