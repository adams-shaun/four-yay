package registry

// Tests for the sb-idea-lethal round-3 fix: the damage bound must be a
// PER-ATTACKER minimum over the whole defensive pool, because a shared greedy
// over one assignment is not a lower bound -- the opponent owns the blocking
// and can route a killer (or their biggest soak) to the attacker the greedy
// did not pick. Each test asserts the fixture precondition its assertion
// depends on, so a vacuous setup fails loudly.

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// killerRoutedBoard is the review's over-claim probe: our 5/5 trampler (obj
// 10) against an untapped 2/2 vanilla (obj 20, same toughness as the killer,
// lower object id) AND a 5/2 killer (obj 21). The old shared greedy sorted by
// toughness then object id, took the 2/2, and priced the trample excess of 3;
// the opponent instead blocks with the killer and takes 0.
func killerRoutedBoard(killerKeywords []string, life int32) botpolicy.Board {
	return botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 5, Toughness: 5, Controller: 0, Keywords: []string{"Trample"}},
			20: {Power: 2, Toughness: 2, Controller: 1},
			21: {Power: 5, Toughness: 2, Controller: 1, Keywords: killerKeywords},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: life},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}},
	}
}

// runAttacker drives the decorator's declare-attackers decision over a Board
// and returns the intent it committed to.
func runAttacker(t *testing.T, board botpolicy.Board, d decision.Decision) decision.Intent {
	t.Helper()
	inner := &stubSeat{answer: decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}}
	got, err := newLethal(inner, 1).(seat.BoardSeat).DecideBoard(context.Background(), board, d)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestLethalKillerBlockerRoutedElsewhereDeniesTheTrampler: the first-striking
// killer is in the pool but NOT the blocker the old greedy assigned. The
// opponent chooses the assignment, so the trampler must price 0 and the
// decorator must delegate -- not commit a swing the opponent fully prevents.
func TestLethalKillerBlockerRoutedElsewhereDeniesTheTrampler(t *testing.T) {
	board := killerRoutedBoard([]string{"First Strike"}, 3)
	if board.Creatures[21].Keywords[0] != "First Strike" {
		t.Fatal("fixture: obj 21 must be the first-striking killer")
	}
	if board.Creatures[20].Toughness != board.Creatures[21].Toughness {
		t.Fatal("fixture: the killer must tie the vanilla on toughness so the old sort picks the vanilla")
	}
	if board.Life[1] != 3 {
		t.Fatal("fixture: opponent life not 3")
	}
	got := runAttacker(t, board, attackerDecision(0, 10))
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (the 5/2 first-striker kills the trampler before it deals)", got.Choices)
	}
}

// TestLethalDoubleStrikingKillerRoutedElsewhereDeniesTheTrampler is the same
// shape with a double-striking killer: double strike lands first-strike
// damage too, so it kills the trampler just the same.
func TestLethalDoubleStrikingKillerRoutedElsewhereDeniesTheTrampler(t *testing.T) {
	board := killerRoutedBoard([]string{"Double Strike"}, 3)
	if board.Creatures[21].Keywords[0] != "Double Strike" {
		t.Fatal("fixture: obj 21 must be the double-striking killer")
	}
	got := runAttacker(t, board, attackerDecision(0, 10))
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (the 5/2 double-striker kills the trampler in the first-strike step)", got.Choices)
	}
}

// TestLethalDeathtouchKillerDeniesTheTrampler: a first-striking deathtouch
// blocker kills regardless of power, so a 1/1 kills the 5/5 trampler and the
// bound must read deathtouch, not just power.
func TestLethalDeathtouchKillerDeniesTheTrampler(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 5, Toughness: 5, Controller: 0, Keywords: []string{"Trample"}},
			21: {Power: 1, Toughness: 1, Controller: 1, Keywords: []string{"First Strike", "Deathtouch"}},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 1},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}},
	}
	if board.Creatures[21].Power >= board.Creatures[10].Toughness {
		t.Fatal("fixture: the killer's power must be below the attacker toughness so only deathtouch can kill")
	}
	got := runAttacker(t, board, attackerDecision(0, 10))
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (first-strike deathtouch kills the trampler at any power)", got.Choices)
	}
}

// TestLethalFirstStrikingAttackerPunchesThroughTheKiller: the positive control
// for the two tests above -- when OUR attacker also strikes first its damage
// lands in the same step, so the killer cannot deny it and the kill is taken.
func TestLethalFirstStrikingAttackerPunchesThroughTheKiller(t *testing.T) {
	board := killerRoutedBoard([]string{"First Strike"}, 3)
	board.Creatures[10] = botpolicy.Creature{Power: 5, Toughness: 5, Controller: 0, Keywords: []string{"Trample", "First Strike"}}
	got := runAttacker(t, board, attackerDecision(0, 10))
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("choices = %v, want the attacking option [0] (first strike punches through for 3)", got.Choices)
	}
}

// TestLethalBigNonTramplerDoesNotStealTheSoak is the second instance of the
// same class: the old greedy processed attackers in power order, so a 6/6
// VANILLA took the 2/2 blocker (worth nothing to it) and left the 3/3
// trampler the 1/1, claiming 2 against a 2-life opponent. The opponent blocks
// the trampler with the 2/2 instead, delivering 1, so the decorator must
// delegate.
func TestLethalBigNonTramplerDoesNotStealTheSoak(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 6, Toughness: 6, Controller: 0},
			30: {Power: 3, Toughness: 3, Controller: 0, Keywords: []string{"Trample"}},
			20: {Power: 2, Toughness: 2, Controller: 1},
			21: {Power: 1, Toughness: 1, Controller: 1},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 2},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}, 30: {Sick: false}},
	}
	if board.Creatures[10].Power <= board.Creatures[30].Power {
		t.Fatal("fixture: the vanilla must out-power the trampler to reproduce the old ordering")
	}
	if board.Life[1] != 2 {
		t.Fatal("fixture: opponent life not 2")
	}
	got := runAttacker(t, board, attackerDecision(0, 10, 30))
	if len(got.Choices) != 0 {
		t.Fatalf("choices = %v, want delegation (the trampler is soaked by the 2/2 for only 1)", got.Choices)
	}
}

// TestLethalBigNonTramplerTrampleStillCounts is the positive control: one life
// lower the same burnt line (1) does reach lethal, so the delegate above is
// the bound talking and not a blanket refusal.
func TestLethalBigNonTramplerTrampleStillCounts(t *testing.T) {
	board := botpolicy.Board{
		IsMain: true, FirstMain: true, MyTurn: true,
		Creatures: map[state.ObjID]botpolicy.Creature{
			10: {Power: 6, Toughness: 6, Controller: 0},
			30: {Power: 3, Toughness: 3, Controller: 0, Keywords: []string{"Trample"}},
			20: {Power: 2, Toughness: 2, Controller: 1},
			21: {Power: 1, Toughness: 1, Controller: 1},
		},
		Life:  map[state.PlayerID]int32{0: 20, 1: 1},
		Cards: map[state.ObjID]botpolicy.Card{10: {Sick: false}, 30: {Sick: false}},
	}
	got := runAttacker(t, board, attackerDecision(0, 10, 30))
	if len(got.Choices) != 2 {
		t.Fatalf("choices = %v, want both attackers [0 1] (guaranteed 1 reaches lethal)", got.Choices)
	}
}
