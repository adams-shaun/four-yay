package rules

// Restores effects/player_opponentof_test.go on the kernel: Bend or Break's
// `ChoosePlayer | Defined$ Remembered | Choices$ Player.OpponentOf
// Remembered` asks the REMEMBERED player (each RepeatEach iteration's
// player) to choose among that player's opponents only, in seat order after
// it, and the answer is the chosen player the chain reads.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestChoosePlayerOpponentOfRemembered(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 4)
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Bend Fixture",
		"A:SP$ RepeatEach | RepeatPlayers$ Player | RepeatSubAbility$ DBChoosePlayer",
		"SVar:DBChoosePlayer:DB$ ChoosePlayer | Defined$ Remembered | Choices$ Player.OpponentOf Remembered | SubAbility$ DBDraw",
		"SVar:DBDraw:DB$ Draw | Defined$ ChosenPlayer | NumCards$ 1 | SubAbility$ DBCleanup",
		"SVar:DBCleanup:DB$ Cleanup | ClearChosenPlayer$ True"), state.ZHand, false)
	var hands [4]int
	for p := range hands {
		hands[p] = len(e.G.Zone(state.ZHand, state.PlayerID(p)))
	}
	hands[0]-- // the spell leaves seat 0's hand
	d := kr2Cast(t, e, 0, spell)
	// Each player answers with the opponent two seats after it, except seat
	// 1, which picks seat 2 (proving a non-first option is honoured is the
	// point of the others).
	wantDraws := [4]int{}
	for p := state.PlayerID(0); p < 4; p++ {
		if d == nil || d.Kind != decision.KChoose || d.Player != p {
			t.Fatalf("iteration %d ask = %+v, want a KChoose for remembered player %d", p, d, p)
		}
		var want []state.PlayerID
		for i := state.PlayerID(1); i < 4; i++ {
			want = append(want, (p+i)%4)
		}
		if len(d.Options) != 3 {
			t.Fatalf("player %d options = %+v, want its three opponents %v", p, d.Options, want)
		}
		for i, o := range d.Options {
			if o.Player != want[i] || o.Player == p {
				t.Fatalf("player %d option %d = %+v, want opponent %d", p, i, o, want[i])
			}
		}
		pick := (p + 2) % 4
		if p == 1 {
			pick = 2
		}
		wantDraws[pick]++
		d = kr2Answer(t, e, d, kr2PlayerIdx(t, d, pick))
	}
	if d != nil {
		t.Fatalf("unexpected ask after the loop: %+v", d)
	}
	for p := range hands {
		if got := len(e.G.Zone(state.ZHand, state.PlayerID(p))); got != hands[p]+wantDraws[p] {
			t.Fatalf("seat %d hand = %d, want %d (drew %d as a chosen player)", p, got, hands[p]+wantDraws[p], wantDraws[p])
		}
	}
}
