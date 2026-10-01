package botpolicy

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// blocksDecisionFull is the BLK whole-assignment twin of combat_test.go's
// blockDecisionFull: it builds a KBlockers decision for seat 0 (the
// defender) with one option per (blocker, attacker) pair — blockers 100+n
// (the atk() helper creatures, controller 0), attackers 200+n (the def()
// helper creatures, controller 1) — answers it with BlocksDecide, and
// returns the decision plus the chosen option indices. An option's
// position in the returned pair space is its position in pairs.
func blocksDecisionFull(b Board, pairs ...[2]int) (decision.Decision, []int) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: len(pairs),
		Options: []decision.Option{}}
	for _, p := range pairs {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "block",
			Obj: state.ObjID(100 + p[0]), Attacker: state.ObjID(200 + p[1]), Player: 0})
	}
	return d, BlocksDecide(b, &d, rng(1)).Choices
}

// blocksDecision is blocksDecisionFull without the decision.
func blocksDecision(b Board, pairs ...[2]int) []int {
	_, choices := blocksDecisionFull(b, pairs...)
	return choices
}

// chosenPairs translates chosen option indices back to (blocker, attacker)
// pair positions, so a test asserts in pair space.
func chosenPairs(d *decision.Decision, choices []int) [][2]int {
	out := make([][2]int, 0, len(choices))
	for _, c := range choices {
		o := d.Options[c]
		out = append(out, [2]int{int(o.Obj) - 100, int(o.Attacker) - 200})
	}
	return out
}

// pairsEqual compares two pair lists positionally.
func pairsEqual(a, e [][2]int) bool {
	if len(a) != len(e) {
		return false
	}
	for i := range a {
		if a[i] != e[i] {
			return false
		}
	}
	return true
}

// TestBlocksLethalSurvivesWithChumps is B1: facing lethal (a 6/6 and a 3/3
// against life 5 — 9 power unblocked), the whole assignment blocks to
// survive even with blockers that die without killing anything. The
// largest attacker is answered first and takes the cheapest chump; the
// second attacker takes the remaining blocker.
func TestBlocksLethalSurvivesWithChumps(t *testing.T) {
	b := boardOf(atk(1, 1, 1), atk(2, 2, 2), def(1, 6, 6), def(2, 3, 3))
	b.Life.Set(0, 5)
	d, got := blocksDecisionFull(b, [2]int{1, 1}, [2]int{2, 1}, [2]int{1, 2}, [2]int{2, 2})
	want := [][2]int{{1, 1}, {2, 2}} // the 1/1 chumps the 6/6, the 2/2 chumps the 3/3
	if p := chosenPairs(&d, got); !pairsEqual(p, want) {
		t.Errorf("lethal assignment = %v, want %v", p, want)
	}
}

// TestBlocksFreeKillTaken is B2: not facing lethal, a blocker that kills
// the attacker and survives is a free kill and is taken (the cheapest one
// when several qualify).
func TestBlocksFreeKillTaken(t *testing.T) {
	// A 5/5 killing a 4/4 and surviving (4 < 5): free kill, taken. A 2/2
	// would die without killing and stays home.
	b := boardOf(atk(1, 5, 5), atk(2, 2, 2), def(1, 4, 4))
	b.Life.Set(0, 20)
	d, got := blocksDecisionFull(b, [2]int{1, 1}, [2]int{2, 1})
	if p := chosenPairs(&d, got); !pairsEqual(p, [][2]int{{1, 1}}) {
		t.Errorf("free kill = %v, want {1,1}", p)
	}
}

// TestBlocksBadTradeAvoided is B2: not facing lethal, a blocker that kills
// the attacker but dies while the attacker's power is SMALLER than the
// blocker's power is a trade down and is not taken; nor is a pure chump.
func TestBlocksBadTradeAvoided(t *testing.T) {
	// My 3/2 kills the 2/3 but dies, and 2 < 3: the attacker's power does
	// not reach the blocker's, so the trade is down and is declined.
	b := boardOf(atk(1, 3, 2), def(1, 2, 3))
	b.Life.Set(0, 20)
	if got := blocksDecision(b, [2]int{1, 1}); len(got) != 0 {
		t.Errorf("trade-down taken: %v", got)
	}
	// A pure chump: my 2/2 cannot kill the 5/5 and would die for nothing.
	b = boardOf(atk(1, 2, 2), def(1, 5, 5))
	b.Life.Set(0, 20)
	if got := blocksDecision(b, [2]int{1, 1}); len(got) != 0 {
		t.Errorf("chump taken while not facing lethal: %v", got)
	}
}

// TestBlocksEvenTradeTaken is B2's other half: a blocker that kills the
// attacker and dies is still taken when the attacker's power is at least
// the blocker's (trading up).
func TestBlocksEvenTradeTaken(t *testing.T) {
	// My 2/3 kills the 3/2 (3 power >= 2) and dies taking 3 < 3? No — it
	// takes the attacker's full 2 power, dies (toughness 3? no: 2/3 takes
	// 2 < 3 and survives). Use a blocker that dies: my 3/2 vs a 3/3
	// attacker: blocker deals 3 >= 3, dies to 3 >= 2, and 3 >= 3: trade up,
	// taken.
	b := boardOf(atk(1, 3, 2), def(1, 3, 3))
	b.Life.Set(0, 20)
	d, got := blocksDecisionFull(b, [2]int{1, 1})
	if p := chosenPairs(&d, got); !pairsEqual(p, [][2]int{{1, 1}}) {
		t.Errorf("trade up = %v, want {1,1}", p)
	}
}

// TestBlocksIllegalPairNeverChosen is B0: only offered pairs are ever
// chosen. The flying attacker's only offered pair comes from the Reach
// blocker; the assignment must pick exactly that one and invent nothing
// for the blocker that cannot legally block.
func TestBlocksIllegalPairNeverChosen(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 2, 2, "Reach"), def(1, 3, 3, "Flying"))
	b.Life.Set(0, 3)
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "block", Obj: 102, Attacker: 201, Player: 0},
		}}
	got := BlocksDecide(b, &d, rng(1)).Choices
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("choices = %v, want [0] (the offered Reach pair, the only legal answer)", got)
	}
	if err := d.Validate(BlocksDecide(b, &d, rng(1))); err != nil {
		t.Errorf("intent failed Validate: %v", err)
	}
}

// TestBlocksOneBlockerOneAttacker is B0's exclusivity half: a blocker is
// never assigned to two attackers. Facing lethal with one 1/1 against two
// attackers, the largest takes it and the other goes unblocked.
func TestBlocksOneBlockerOneAttacker(t *testing.T) {
	b := boardOf(atk(1, 1, 1), def(1, 5, 5), def(2, 4, 4))
	b.Life.Set(0, 3)
	d, got := blocksDecisionFull(b, [2]int{1, 1}, [2]int{1, 2})
	if len(got) != 1 {
		t.Fatalf("one blocker assigned %d times: %v", len(got), got)
	}
	if p := chosenPairs(&d, got); !pairsEqual(p, [][2]int{{1, 1}}) {
		t.Errorf("largest attacker did not get the blocker: %v", p)
	}
}

// TestBlocksDeterministic runs the same lethal assignment several times
// with a fresh rng each time and requires identical answers (no map
// iteration order reaches the choice).
func TestBlocksDeterministic(t *testing.T) {
	b := boardOf(atk(1, 1, 1), atk(2, 2, 2), atk(3, 2, 2), def(1, 6, 6), def(2, 3, 3))
	b.Life.Set(0, 5)
	pairs := [][2]int{{1, 1}, {2, 1}, {3, 1}, {1, 2}, {2, 2}, {3, 2}}
	first := blocksDecision(b, pairs...)
	for i := 0; i < 5; i++ {
		if got := blocksDecision(b, pairs...); !slices.Equal(got, first) {
			t.Fatalf("assignment not deterministic: %v vs %v", got, first)
		}
	}
}

// TestBlocksMenaceNeedsTwo is B0's Menace half: a Menace attacker is
// blocked only by a team of at least two (CR 702.111b, which
// validateBlockers enforces over the whole declaration). Not facing lethal,
// two 2/2s that kill the 2/2 Menace attacker and both survive are a free
// kill and are taken; a lone blocker is never declared against it.
func TestBlocksMenaceNeedsTwo(t *testing.T) {
	b := boardOf(atk(1, 2, 2), atk(2, 2, 2), def(1, 2, 2, "Menace"))
	b.Life.Set(0, 20)
	d, got := blocksDecisionFull(b, [2]int{1, 1}, [2]int{2, 1})
	if len(got) != 2 {
		t.Fatalf("menace attacker blocked by %d blockers: %v", len(got), got)
	}
	if p := chosenPairs(&d, got); !pairsEqual(p, [][2]int{{1, 1}, {2, 1}}) {
		t.Errorf("menace team = %v, want both 2/2s", p)
	}
}

// TestBlocksTrampleChumpPrefersAdequate is B1's trample half: against a
// trample attacker nothing kills, the chump chosen is the one whose
// remaining toughness soaks the most overflow (the 2/4, not the 1/1).
func TestBlocksTrampleChumpPrefersAdequate(t *testing.T) {
	b := boardOf(atk(1, 1, 1), atk(2, 2, 4), def(1, 6, 6, "Trample"))
	b.Life.Set(0, 3)
	d, got := blocksDecisionFull(b, [2]int{1, 1}, [2]int{2, 1})
	if p := chosenPairs(&d, got); !pairsEqual(p, [][2]int{{2, 1}}) {
		t.Errorf("trample chump = %v, want {2,1}", p)
	}
}

// TestLegalBlockChoicesDropsIllegalCounts pins the shared KBlockers guard:
// the CR 509.1a MinMaxBlocker bounds ride the option (MinBlockers/
// MaxBlockers), and a policy answer outside them is dropped before it can
// reach the engine, which would reject the whole declaration and crash the
// match. A sub-Min team drops entirely (0 stays legal); an over-Max team is
// trimmed to Max, keeping the earliest-declared pairs.
func TestLegalBlockChoicesDropsIllegalCounts(t *testing.T) {
	d := &decision.Decision{Kind: decision.KBlockers, Min: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "block", Obj: 101, Attacker: 201, MinBlockers: 3},
		{Index: 1, Kind: "block", Obj: 102, Attacker: 201, MinBlockers: 3},
		{Index: 2, Kind: "block", Obj: 103, Attacker: 201, MinBlockers: 3},
	}}
	if got := legalBlockChoices(Board{}, d, []int{0, 1}); len(got) != 0 {
		t.Fatalf("a Min 3 attacker with two chosen blockers kept %v, want none", got)
	}
	if got := legalBlockChoices(Board{}, d, []int{0, 1, 2}); len(got) != 3 {
		t.Fatalf("a legal Min 3 team was trimmed to %v", got)
	}

	max := &decision.Decision{Kind: decision.KBlockers, Min: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "block", Obj: 101, Attacker: 201, MaxBlockers: 1},
		{Index: 1, Kind: "block", Obj: 102, Attacker: 201, MaxBlockers: 1},
		{Index: 2, Kind: "block", Obj: 103, Attacker: 201, MaxBlockers: 1},
	}}
	if got := legalBlockChoices(Board{}, max, []int{0, 1, 2}); len(got) != 1 || got[0] != 0 {
		t.Fatalf("a Max 1 attacker kept %v, want exactly the first pair [0]", got)
	}
	if got := legalBlockChoices(Board{}, max, []int{2}); len(got) != 1 || got[0] != 2 {
		t.Fatalf("a legal single Max 1 block was altered: %v", got)
	}

	// The default policy routes through the guard end to end: three 2/2
	// blockers against one bumped 6/5 attacker under Min 3 must answer with
	// no block, never an illegal one-creature chump.
	b := boardOf(atk(1, 2, 2), atk(2, 2, 2), atk(3, 2, 2), def(1, 6, 5))
	b.Life.Set(state.PlayerID(0), 20)
	in := Decide(b, d, rng(1))
	if len(in.Choices) != 0 {
		t.Fatalf("Decide chose %v against a Min 3 attacker, want no block", in.Choices)
	}
}
