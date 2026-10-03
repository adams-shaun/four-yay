package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Task vow1: the bare-Choices$ PutCounter pick (no DividedAsYouChoose$) and
// the RememberCards$ read. Promise of Loyalty is the reported carrier; these
// leaves pin the primitive itself on synthetic scripts, and the full chain on
// the real corpus card lives in rules/promise_of_loyalty_test.go.

func vowBear(t *testing.T, name string) *cards.Card {
	t.Helper()
	return mkCard(t, "Name:"+name+"\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
}

// putThreeBears seats three creatures for player 0 and returns their ids in
// battlefield zone order.
func putThreeBears(t *testing.T, h *askHost) []state.ObjID {
	t.Helper()
	g := h.g
	ids := []state.ObjID{
		g.AddObject(vowBear(t, "Bear A"), 0).ID,
		g.AddObject(vowBear(t, "Bear B"), 0).ID,
		g.AddObject(vowBear(t, "Bear C"), 0).ID,
	}
	for _, id := range ids {
		g.Obj(id).Zone = state.ZBattlefield
	}
	g.SetZone(state.ZBattlefield, 0, ids)
	return ids
}

func vowPickSA(t *testing.T) *cards.SA {
	return sa(t, "DB$ PutCounter | Choices$ Creature.YouCtrl | ChoiceTitle$ Choose a creature you control | Chooser$ You | CounterType$ VOW | CounterNum$ 1 | RememberCards$ True")
}

// TestPutCounterChoicesPickNoAskSingleEligible pins the strict-supersets
// gate: with exactly one eligible creature the choice nobody could answer
// differently is never emitted, and the deterministic placement runs.
func TestPutCounterChoicesPickNoAskSingleEligible(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	ids := putThreeBears(t, h)
	h.g.SetZone(state.ZBattlefield, 0, ids[:1])
	c := &Ctx{Source: 1, Controller: 0}
	src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
	src.Zone = state.ZStack
	c.Source = src.ID
	Resolve(h, c, vowPickSA(t))
	if h.asked != nil {
		t.Fatalf("pick decision posed for a single eligible creature: %+v", h.asked)
	}
	if o := h.g.Obj(ids[0]); o.Counter("VOW") != 1 {
		t.Fatalf("sole eligible bear's VOW counter = %d, want 1", o.Counter("VOW"))
	}
	if len(c.Remembered) != 1 || c.Remembered[0].Obj != ids[0] {
		t.Fatalf("Remembered = %+v, want exactly the countered bear", c.Remembered)
	}
}

// TestPutCounterControlledByRememberedPlayerBridge pins the filter bridge the
// vow chain's SacAllOthers needs: `ControlledBy Player.IsRemembered` resolves
// against the SPEC CONTEXT's remembered PLAYER entries at resolution time,
// matches only those players' creatures, and matches nothing when the
// resolution is not live (an offer-time spec) or the set holds no players.
func TestPutCounterControlledByRememberedPlayerBridge(t *testing.T) {
	h := newHost(t, 2)
	g := h.g
	mine := g.AddObject(vowBear(t, "Mine"), 1).ID
	theirs := g.AddObject(vowBear(t, "Theirs"), 0).ID
	g.SetZone(state.ZBattlefield, 0, []state.ObjID{theirs})
	g.SetZone(state.ZBattlefield, 1, []state.ObjID{mine})
	sc := SpecContext{You: 0, Remembered: []state.Target{{Player: 1, IsPlayer: true}}, Resolving: true}
	if !MatchesSpecCtx(g, "Creature.ControlledBy Player.IsRemembered", mine, sc) {
		t.Fatal("the remembered player's creature did not match")
	}
	if MatchesSpecCtx(g, "Creature.ControlledBy Player.IsRemembered", theirs, sc) {
		t.Fatal("another player's creature matched a Player.IsRemembered control spec")
	}
	// An offer-time SpecContext (Resolving false) fails closed.
	if MatchesSpecCtx(g, "Creature.ControlledBy Player.IsRemembered", mine, SpecContext{You: 0,
		Remembered: []state.Target{{Player: 1, IsPlayer: true}}}) {
		t.Fatal("an unresolved (offer-time) spec matched -- the referent must be resolution-only")
	}
	// The ChosenPlayer spelling (Gluntch's carrier shape) resolves the same
	// way against the chosen player entries.
	scChosen := SpecContext{You: 0, Chosen: []state.Target{{Player: 1, IsPlayer: true}}, Resolving: true}
	if !MatchesSpecCtx(g, "Creature.ControlledBy ChosenPlayer", mine, scChosen) {
		t.Fatal("the chosen player's creature did not match a ControlledBy ChosenPlayer spec")
	}
	if MatchesSpecCtx(g, "Creature.ControlledBy ChosenPlayer", theirs, scChosen) {
		t.Fatal("another player's creature matched a ControlledBy ChosenPlayer spec")
	}
}
