package mzplay

import (
	"math"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLabelValues pins ParallelDataGenerator.generateLabeledStatesForGame:
// backward from the terminal +1 / -1, y = lambda*y_next + (1-lambda)*score.
func TestLabelValues(t *testing.T) {
	rows := []Row{{Score: 0.2}, {Score: -0.4}, {Score: 0.6}}
	LabelValues(rows, true, 0.95)
	y2 := 0.95*1 + 0.05*0.6
	y1 := 0.95*y2 + 0.05*-0.4
	y0 := 0.95*y1 + 0.05*0.2
	for i, want := range []float64{y0, y1, y2} {
		if math.Abs(rows[i].Value-want) > 1e-15 {
			t.Fatalf("won: row %d value %v, want %v", i, rows[i].Value, want)
		}
	}
	LabelValues(rows, false, 0.95)
	y2 = 0.95*-1 + 0.05*0.6
	y1 = 0.95*y2 + 0.05*-0.4
	y0 = 0.95*y1 + 0.05*0.2
	for i, want := range []float64{y0, y1, y2} {
		if math.Abs(rows[i].Value-want) > 1e-15 {
			t.Fatalf("lost: row %d value %v, want %v", i, rows[i].Value, want)
		}
	}
	// lambda 1 is the pure result, lambda 0 the root score.
	LabelValues(rows, true, 1)
	if rows[0].Value != 1 || rows[2].Value != 1 {
		t.Fatalf("lambda 1: %v", rows)
	}
	LabelValues(rows, true, 0)
	if rows[0].Value != 0.2 || rows[1].Value != -0.4 || rows[2].Value != 0.6 {
		t.Fatalf("lambda 0: %v", rows)
	}
	LabelValues(nil, true, 0.95) // no rows: nothing to do
}

// TestSeatWonKeepsTheNoWinnerQuirk: upstream labels seat A with
// playerA.hasWon() and seat B with its negation (ParallelDataGenerator.java
// 379-380), so a game nobody won -- a drawn game, or one stopped at the turn
// cap -- trains A toward -1 and B toward +1.
func TestSeatWonKeepsTheNoWinnerQuirk(t *testing.T) {
	for _, tc := range []struct {
		winner       int
		wantA, wantB bool
	}{{0, true, false}, {1, false, true}, {NoWinner, false, true}} {
		if a, b := SeatWon(0, tc.winner), SeatWon(1, tc.winner); a != tc.wantA || b != tc.wantB {
			t.Errorf("winner %d: A %v B %v, want %v %v", tc.winner, a, b, tc.wantA, tc.wantB)
		}
	}
	rowsA, rowsB := []Row{{Score: 0}}, []Row{{Score: 0}}
	LabelValues(rowsA, SeatWon(0, NoWinner), 0.95)
	LabelValues(rowsB, SeatWon(1, NoWinner), 0.95)
	if rowsA[0].Value != -0.95 || rowsB[0].Value != 0.95 {
		t.Fatalf("no winner: A %v B %v, want -0.95 and +0.95", rowsA[0].Value, rowsB[0].Value)
	}
}

func intent(choices ...int) decision.Intent { return decision.Intent{Choices: choices} }

// TestAttackMarginals: a root over whole attack declarations projected onto
// one yes/no per potential attacker. Option indices are deliberately not
// their positions.
func TestAttackMarginals(t *testing.T) {
	const bear, elf, ox = state.ObjID(11), state.ObjID(12), state.ObjID(13)
	d := &decision.Decision{Kind: decision.KAttackers, Options: []decision.Option{
		{Index: 5, Obj: bear}, {Index: 6, Obj: elf}, {Index: 7, Obj: ox},
	}}
	cands := []decision.Intent{intent(5, 6), intent(), intent(5, 6, 7), intent(6), intent(5)}
	visits := []int{100, 20, 60, 15, 5}
	got := AttackMarginals(d, cands, visits, 2)
	want := []AttackerMarginal{
		{Obj: bear, Yes: 165, No: 35, Chosen: true},
		{Obj: elf, Yes: 175, No: 25, Chosen: true},
		{Obj: ox, Yes: 60, No: 140, Chosen: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("marginals:\n got %+v\nwant %+v", got, want)
	}
	// Every row's counts sum to the root's visits.
	for _, m := range got {
		if m.Yes+m.No != 200 {
			t.Fatalf("attacker %d: %d + %d != 200", m.Obj, m.Yes, m.No)
		}
	}
	// The chosen declaration decides Chosen: candidate 3 attacks with the elf only.
	got = AttackMarginals(d, cands, visits, 3)
	if got[0].Chosen || !got[1].Chosen || got[2].Chosen {
		t.Fatalf("chosen flags for the elf-only declaration: %+v", got)
	}
	// An attacker offered against two defenders (a planeswalker) is one
	// potential attacker: attacking either counts as yes.
	d2 := &decision.Decision{Kind: decision.KAttackers, Options: []decision.Option{
		{Index: 0, Obj: bear}, {Index: 1, Obj: bear, Battle: 99}, {Index: 2, Obj: elf},
	}}
	got = AttackMarginals(d2, []decision.Intent{intent(0), intent(1, 2), intent()}, []int{3, 4, 5}, 1)
	want = []AttackerMarginal{{Obj: bear, Yes: 7, No: 5, Chosen: true}, {Obj: elf, Yes: 4, No: 8, Chosen: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("two defenders:\n got %+v\nwant %+v", got, want)
	}
}

// TestBlockMarginals: a root over whole block declarations projected onto
// one "which attacker" choice per potential blocker, with Stop Choosing for
// the declarations in which it does not block.
func TestBlockMarginals(t *testing.T) {
	const wall, cat = state.ObjID(21), state.ObjID(22)
	const drake, ogre = state.ObjID(31), state.ObjID(32)
	d := &decision.Decision{Kind: decision.KBlockers, Options: []decision.Option{
		{Index: 0, Obj: wall, Attacker: drake}, {Index: 1, Obj: wall, Attacker: ogre},
		{Index: 2, Obj: cat, Attacker: ogre},
	}}
	cands := []decision.Intent{intent(1), intent(), intent(0, 2), intent(1, 2), intent(2)}
	visits := []int{40, 10, 30, 15, 5}
	got := BlockMarginals(d, cands, visits, 3)
	want := []BlockerMarginal{
		{Obj: wall, Attackers: []state.ObjID{drake, ogre}, Visits: []int{30, 55}, Stop: 15, Chosen: ogre},
		{Obj: cat, Attackers: []state.ObjID{ogre}, Visits: []int{50}, Stop: 50, Chosen: ogre},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("marginals:\n got %+v\nwant %+v", got, want)
	}
	for _, m := range got {
		sum := m.Stop
		for _, v := range m.Visits {
			sum += v
		}
		if sum != 100 {
			t.Fatalf("blocker %d: counts sum to %d, want 100", m.Obj, sum)
		}
	}
	// No block chosen: Chosen is 0 for every blocker.
	got = BlockMarginals(d, cands, visits, 1)
	if got[0].Chosen != 0 || got[1].Chosen != 0 {
		t.Fatalf("chosen for the empty declaration: %+v", got)
	}
}

// TestPolicyFromVisits: candidates sharing an action index add up, as
// upstream adds the visits of children that share one (getActionVec), and a
// candidate with no visits adds no entry.
func TestPolicyFromVisits(t *testing.T) {
	got := policyFromVisits([]int{0, 22, 22, 700}, []int{10, 5, 7, 0})
	want := map[int]float32{0: 10, 22: 12}
	if len(got) != len(want) {
		t.Fatalf("policy %v, want %v", got, want)
	}
	for _, p := range got {
		if want[p.Index] != p.Visits {
			t.Fatalf("policy %v, want %v", got, want)
		}
	}
}
