package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// exhaustPriorityAtSeatZero ensures the offered options belong to the
// activating player, not the opponent who may hold priority on entering main1.
func exhaustPriorityAtSeatZero(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 4 && e.Pending() != nil &&
		e.Pending().Kind == decision.KPriority && e.Pending().Player != 0; i++ {
		submitPass(t, e)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != 0 || e.G.Step != state.StepMain1 {
		t.Fatalf("expected seat 0 main1 priority, got %+v at %s", d, e.G.Step)
	}
}

func exhaustBattlefield(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("exhaust source %d must be on battlefield, got %+v", id, o)
	}
}

// With no other limit or target, the {1} ability must be withheld after one
// use, even after the per-turn ActivationLimit$ window resets.
func TestExhaustInlineWithheldSameTurnAndLaterTurn(t *testing.T) {
	t.Parallel()
	const src = "Name:ExhaustBeast\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
		"A:AB$ Pump | Cost$ 1 | Defined$ Self | Power$ 1 | Toughness$ 1 | Exhaust$ True | SpellDescription$ CARDNAME gets +1/+1. Activate each exhaust ability only once.\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 11, src)
	moveByName(t, e, 0, "ExhaustBeast", state.ZBattlefield)
	exhaustBattlefield(t, e, id)
	addMana(t, e, 0, "CC") // enough for two activations
	exhaustPriorityAtSeatZero(t, e)
	if got := e.G.Players[0].Pool.Total(); got < 2 {
		t.Fatalf("need two payable activations, pool=%d", got)
	}
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 || e.G.Players[0].Pool.Total() < 1 {
		t.Fatalf("first activation did not push or leave mana for a second: stack=%v pool=%d", e.G.Stack, e.G.Players[0].Pool.Total())
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("Exhaust ability offered twice this turn: %+v", e.Pending().Options)
	}

	// Come back to seat 0's next turn, rather than inspecting seat 1's
	// options: absence on the wrong player's decision proves nothing.
	turnBefore := e.G.Turn
	driveToStep(t, e, turnBefore+2, 0, state.StepMain1)
	exhaustBattlefield(t, e, id)
	addMana(t, e, 0, "CC")
	exhaustPriorityAtSeatZero(t, e)
	if got := e.G.Players[0].Pool.Total(); got < 2 {
		t.Fatalf("later turn needs two payable activations, pool=%d", got)
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("Exhaust ability re-offered after turn boundary: %+v", e.Pending().Options)
	}
}

// The real {3} PutCounter leaf must pass the same gate as the inline Pump.
func TestExhaustMaiJadedEdgeWithheldAfterOneUse(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mai, ok := reg.Lookup("Mai, Jaded Edge")
	if !ok {
		t.Fatal("corpus fixture: Mai, Jaded Edge missing")
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{mai}, mountainDeck(t, 40)...), mountainDeck(t, 40)}})
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Mai, Jaded Edge", state.ZBattlefield)
	exhaustBattlefield(t, e, id)
	exhaustPriorityAtSeatZero(t, e)
	addMana(t, e, 0, "CCCCCC") // two full {3} payments
	exhaustPriorityAtSeatZero(t, e)
	if got := e.G.Players[0].Pool.Total(); got < 6 {
		t.Fatalf("need two payable Mai activations, pool=%d", got)
	}
	opt := abilityOptionByLabel(t, e, id, "Put a double strike counter")
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 || e.G.Players[0].Pool.Total() < 3 {
		t.Fatalf("Mai did not push or leave mana for a second activation: stack=%v pool=%d", e.G.Stack, e.G.Players[0].Pool.Total())
	}
	if _, ok := findAbilityOptionByLabel(e, id, "Put a double strike counter"); ok {
		t.Fatalf("Mai exhaust ability re-offered: %+v", e.Pending().Options)
	}
}
