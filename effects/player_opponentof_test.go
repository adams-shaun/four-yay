package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestPlayerOpponentOfRememberedRequiresResolvedPlayer(t *testing.T) {
	g := state.NewGame(names(4))
	remembered := state.Target{Player: 1, IsPlayer: true}
	if remembered.Player == 2 {
		t.Fatal("test requires distinct remembered and candidate players")
	}
	ctx := PlayerSpecCtx{OpponentOf: []state.Target{remembered}}
	if !MatchesPlayerSpecCtx(g, "Player.OpponentOf Remembered", 2, 0, ctx) {
		t.Fatal("player 2 should be an opponent of remembered player 1")
	}
	if MatchesPlayerSpecCtx(g, "Player.OpponentOf Remembered", 1, 0, ctx) {
		t.Fatal("remembered player must not be their own opponent")
	}
	for name, invalid := range map[string]PlayerSpecCtx{
		"missing":       {},
		"object target": {OpponentOf: []state.Target{{Obj: 42}}},
		"out of range":  {OpponentOf: []state.Target{{Player: 9, IsPlayer: true}}},
		"unsupported":   {OpponentOf: []state.Target{{Player: 1, IsPlayer: true}}},
	} {
		spec := "Player.OpponentOf Remembered"
		if name == "unsupported" {
			spec = "Player.OpponentOf TriggeredPlayer"
		}
		if MatchesPlayerSpecCtx(g, spec, 2, 0, invalid) {
			t.Errorf("%s referent unexpectedly matched", name)
		}
	}
}
