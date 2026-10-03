package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A6 (turnSkipWorthIt): an ability that makes its activator skip a turn is
// taken only as the first activation of another player's permanent, in the
// seat's own main phase, when the creature cards it frees are worth at
// least turnSkipPrice per skipped turn. Every other shape -- the seat's own
// Chronatog, a repeat, the opponent's turn, a hand with too little to free --
// is declined, and an ability with no rider is untouched.
func TestTurnSkipRiderIsPriced(t *testing.T) {
	const src, hand1 state.ObjID = 10, 20
	board := func(activated int32, myTurn, main bool, handCMC ...int32) Board {
		var b Board
		b.MyTurn, b.IsMain = myTurn, main
		b.Cards.Set(src, Card{Activated: activated})
		for i, cmc := range handCMC {
			b.Cards.Set(hand1+state.ObjID(i), Card{Creature: true, Castable: true, CMC: cmc})
		}
		return b
	}
	vapors := decision.Option{Kind: "ability", Obj: src, Label: "Lethal Vapors: Destroy Lethal Vapors. You skip your next turn.", SelfSkipTurns: 1, ForeignSource: true}
	own := vapors
	own.ForeignSource = false
	plain := vapors
	plain.SelfSkipTurns = 0
	cases := []struct {
		name string
		b    Board
		o    decision.Option
		want bool
	}{
		{"foreign, first, own main, 3+4 freed", board(0, true, true, 3, 4), vapors, true},
		{"own source (Chronatog)", board(0, true, true, 3, 4), own, false},
		{"already activated this turn", board(1, true, true, 3, 4), vapors, false},
		{"opponent's turn", board(0, false, true, 3, 4), vapors, false},
		{"not a main phase", board(0, true, false, 3, 4), vapors, false},
		{"too little to free (5 < 6)", board(0, true, true, 2, 3), vapors, false},
		{"nothing to free", board(0, true, true), vapors, false},
		{"no rider: ordinary scoring", board(0, false, false), plain, true},
	}
	for _, tc := range cases {
		if got := tc.b.AbilityWorthTaking(tc.o, 1); got != tc.want {
			t.Errorf("%s: AbilityWorthTaking = %v, want %v", tc.name, got, tc.want)
		}
	}
}
