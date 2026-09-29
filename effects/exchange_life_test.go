package effects

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"testing"
)

func TestExchangeLifeShapes(t *testing.T) {
	for _, tc := range []struct {
		name, line           string
		wantA, wantB, number int32
	}{
		{"Mister Negative", "DB$ ExchangeLife | ValidTgts$ Opponent | RememberOwnLoss$ True", 15, 20, 5},
		{"gain", "DB$ ExchangeLife | ValidTgts$ Opponent | RememberOwnLoss$ True", 25, 20, 0},
		{"Axis of Mortality", "DB$ ExchangeLife | ValidTgts$ Player | RememberDifference$ True", 15, 20, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, c := fixtureHost(t)
			if tc.name == "gain" {
				h.Emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 5})
			} else {
				h.Emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
			}
			if h.g.Players[0].Life != 20 || h.g.Players[1].Life == 20 {
				t.Fatal("precondition: life totals must differ")
			}
			c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
			if tc.name == "Axis of Mortality" {
				c.Targets = append([]state.Target{{Player: 0, IsPlayer: true}}, c.Targets...)
			}
			effExchangeLife(h, c, sa(t, tc.line))
			if h.g.Players[0].Life != tc.wantA || h.g.Players[1].Life != tc.wantB {
				t.Fatalf("life = %d/%d, want %d/%d", h.g.Players[0].Life, h.g.Players[1].Life, tc.wantA, tc.wantB)
			}
			n, ok := EvalCountOK(h, c, "Count$RememberedNumber")
			if !ok || n != tc.number {
				t.Fatalf("remembered number = %d ok=%v, want %d", n, ok, tc.number)
			}
		})
	}
}
