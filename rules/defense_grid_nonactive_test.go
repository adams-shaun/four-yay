package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDefenseGridNonActiveCost uses the real corpus card and checks both sides
// of the Player.NonActive predicate. Defense Grid's RaiseCost must tax a spell
// cast by the non-active player, but not one cast by the active player.
func TestDefenseGridNonActiveCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	grid, ok := reg.Lookup("Defense Grid")
	if !ok {
		t.Fatal("Defense Grid missing from corpus")
	}
	bolt, ok := reg.Lookup("Lightning Bolt")
	if !ok {
		t.Fatal("Lightning Bolt missing from corpus")
	}
	e := corpusEngine(t, reg, []*cards.Card{grid}, []*cards.Card{bolt})
	gridID := moveByName(t, e, 0, "Defense Grid", state.ZBattlefield)
	boltID := moveByName(t, e, 1, "Lightning Bolt", state.ZHand)
	if e.G.Obj(gridID).Zone != state.ZBattlefield || e.G.Obj(boltID).Zone != state.ZHand {
		t.Fatal("setup did not place the real corpus cards in the expected zones")
	}
	if e.G.Active == 1 {
		t.Fatal("fixture unexpectedly started on seat 1")
	}
	for _, tc := range []struct {
		name string
		spec string
		p    state.PlayerID
		want bool
	}{
		{"Player active", "Player.NonActive", 0, false},
		{"Player non-active", "Player.NonActive", 1, true},
		{"You active", "You.NonActive", 0, false},
		{"Opponent non-active", "Opponent.NonActive", 1, true},
		{"Other non-active", "Other.NonActive", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := effects.MatchesPlayerSpec(e.G, tc.spec, tc.p, 0); got != tc.want {
				t.Fatalf("Player.NonActive for player %d = %v, want %v", tc.p, got, tc.want)
			}
		})
	}
	mods := e.costModifiers(1, boltID, spellScope(""))
	priced := mods.apply(e.parseCost(e.G.Obj(boltID).Face().ManaCost))
	if priced.Generic != 3 {
		t.Fatalf("Defense Grid did not add its real {3} tax: priced cost %+v", priced)
	}
}
