package botpolicy

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLegacyDecideBlockNeverLoneBlocksATeamNeedingAttacker pins the legacy
// KBlockers arm's post-coin legality drop across the whole class the ticket
// names, not just the lone-block case: a Menace attacker (derived keyword on
// Board.Creatures) needs >= 2, and an attacker whose offered options publish
// MinBlockers: 3 needs >= 3. A count strictly between 0 and the required
// minimum is the illegal shape the engine rejects (`attacker ... can't be
// blocked by fewer than N creatures`), so the arm must drop the whole partial
// team, not only a single block.
func TestLegacyDecideBlockNeverLoneBlocksATeamNeedingAttacker(t *testing.T) {
	const menace, bounded, plain = state.ObjID(10), state.ObjID(11), state.ObjID(12)
	d := decision.Decision{Seq: 1, Kind: decision.KBlockers, Min: 0, Max: 3}
	for i, attacker := range []state.ObjID{menace, bounded, plain} {
		for blocker := 1; blocker <= 3; blocker++ {
			o := decision.Option{Index: len(d.Options), Kind: "block", Obj: state.ObjID(blocker), Attacker: attacker}
			if i == 1 {
				o.MinBlockers = 3
			}
			d.Options = append(d.Options, o)
		}
	}
	// Precondition: the facts each rule reads really differ. The Menace
	// attacker carries the keyword on the board but no published bound; the
	// bounded attacker publishes Min$ 3 with no keyword; the plain attacker
	// has neither.
	if d.Options[0].Attacker != menace || d.Options[0].MinBlockers != 0 {
		t.Fatal("precondition: Menace attacker must have no published bound")
	}
	if d.Options[3].Attacker != bounded || d.Options[3].MinBlockers != 3 {
		t.Fatal("precondition: bounded attacker must publish MinBlockers 3")
	}
	if d.Options[6].Attacker != plain || d.Options[6].MinBlockers != 0 {
		t.Fatal("precondition: plain attacker must publish no bound")
	}
	plainPicked := false
	first := []int(nil)
	for seed := uint64(0); seed < 250; seed++ {
		board := Board{Creatures: TableOf(map[state.ObjID]Creature{menace: {Keywords: []string{"Menace"}}})}
		in := LegacyDecide(board, &d, rand.New(rand.NewPCG(seed, seed+1)))
		counts := map[state.ObjID]int{}
		for _, idx := range in.Choices {
			if idx >= 0 && idx < len(d.Options) {
				counts[d.Options[idx].Attacker]++
			}
		}
		// The whole class: a team-needing attacker may be unblocked (0) or
		// fully blocked (>= minimum), never partial. `counts == 1` alone is
		// the blind spot that let a Min$ 3 attacker through with 2 blocks.
		if c := counts[menace]; c != 0 && c < 2 {
			t.Fatalf("seed %d: Menace attacker left with %d blockers: %v", seed, c, in.Choices)
		}
		if c := counts[bounded]; c != 0 && c < 3 {
			t.Fatalf("seed %d: Min$ 3 attacker left with %d blockers: %v", seed, c, in.Choices)
		}
		plainPicked = plainPicked || counts[plain] > 0
		if seed < 4 {
			first = append(first, in.Choices...)
		}
	}
	if !plainPicked {
		t.Fatal("plain attacker was never selected")
	}
	if len(first) == 0 {
		t.Fatal("expected pinned choices")
	}
	want := []int{0, 1, 2, 0, 1, 2, 0, 1}
	if len(first) != len(want) {
		t.Fatalf("first choices %v, want %v", first, want)
	}
	for i := range want {
		if first[i] != want[i] {
			t.Fatalf("first choices %v, want %v", first, want)
		}
	}
}
