package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The WithTotalCMC$ budget tests: a Dig's "total mana value N or less" cap.
// The askHost double (primitives_test.go) captures the posed decision and
// suspends; re-entry is a second Resolve with Ctx.Dig/DigDone set, exactly
// the engine's contract. Every fixture builds its own cards with explicit
// ManaCost values so Face().Cmc() is deterministic.

// digBudgetFixture builds seat 0's library as one nonland permanent per MV,
// in the given order, with the given mana costs, and returns (host, ids).
func digBudgetFixture(t *testing.T, mvs ...int) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	ids := make([]state.ObjID, 0, len(mvs))
	for i, mv := range mvs {
		col := "W"
		if i%2 == 1 {
			col = "U"
		}
		pips := make([]string, 0, mv)
		for j := 0; j < mv; j++ {
			pips = append(pips, "{"+col+"}")
		}
		src := "Name:Artifact" + string(rune('A'+i)) + "\nTypes:Artifact\nManaCost:" +
			strings.Join(pips, "") + "\nOracle:x\n"
		ids = append(ids, h.g.AddObject(mkCard(t, src), 0).ID)
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	return h, ids
}

// TestDigWithTotalCMCNoAskWhenAllFit: when the whole affordable set fits under
// the budget there is no choice to pose -- the forced greedy take consumes
// every eligible card -- so no decision is emitted and every card moves.
// This documents the shared no-ask precondition rather than guarding the
// budget fix: no post-budget state is distinguishable here (taking all
// equals the greedy take whenever no ask is warranted), so it is not by
// itself a regression guard -- TestDigWithTotalCMCNarrowsEligible and
// TestDigWithTotalCMCCapsCumulative are.
func TestDigWithTotalCMCNoAskWhenAllFit(t *testing.T) {
	h, ids := digBudgetFixture(t, 2, 2)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ Dig | Defined$ You | DigNum$ 2 | ChangeNum$ 2 | ChangeValid$ Artifact | WithTotalCMC$ 4 | DestinationZone$ Hand"))
	if h.asked != nil {
		t.Fatalf("a decision was posed (%+v); the whole affordable set fits, so the take is forced", h.asked)
	}
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 2 || hand[0] != ids[0] || hand[1] != ids[1] {
		t.Fatalf("hand = %v, want both cards", hand)
	}
}

// TestDigWithTotalCMCSVarSacrificedX: the budget value resolves through the
// same Num grammar as every numeric parameter -- here smelting_vat's
// SVar:X:Sacrificed$CardManaCost, which reads the LKI of the artifact
// sacrificed to pay the activation cost. A 3-MV artifact was sacrificed, so
// the budget is 3: a 2-MV card fits and a 4-MV card does not.
func TestDigWithTotalCMCSVarSacrificedX(t *testing.T) {
	h, ids := digBudgetFixture(t, 4, 2)
	ctx := &Ctx{Controller: 0,
		SVars:      map[string]string{"X": "Sacrificed$CardManaCost"},
		Sacrificed: []state.SacrificedInfo{{ManaValue: 3}}}
	Resolve(h, ctx, sa(t,
		"AB$ Dig | Defined$ You | DigNum$ 2 | ChangeNum$ Any | ChangeValid$ Artifact | WithTotalCMC$ X | DestinationZone$ Hand"))
	if h.asked == nil {
		t.Fatal("no decision: budget 3 admits only the 2-MV card, so the forced take cannot consume both")
	}
	if len(h.asked.Options) != 1 || h.asked.Options[0].Obj != ids[1] {
		t.Fatalf("options = %+v, want only the 2-MV card %d (the 4-MV card exceeds the resolved budget 3)", h.asked.Options, ids[1])
	}
	if h.asked.MaxSum != 3 {
		t.Fatalf("MaxSum = %d, want the resolved budget 3", h.asked.MaxSum)
	}
}

// TestDigWithTotalCMCGreedySkipsNonFittingBeforeCap: the forced greedy take
// is bounded by the running pick COUNT, not by the number of options scanned.
// michelangelos_technique's real shape -- window [4,4,2], budget 6,
// ChangeNum$ 2 -- takes the first 4 (fits), SKIPS the second 4 (would make 8),
// and takes the 2 (sum 6). A scan bounded by index would stop at index 2
// having taken only one card. Pinned on the no-host fallback (a plain
// fakeHost whose Ask returns false), which is the take botpolicy's budget arm
// mirrors.
func TestDigWithTotalCMCGreedySkipsNonFittingBeforeCap(t *testing.T) {
	h := &fakeHost{g: state.NewGame(names(2))}
	ids := make([]state.ObjID, 0, 3)
	for i, mv := range []int{4, 4, 2} {
		pips := make([]string, 0, mv)
		for j := 0; j < mv; j++ {
			pips = append(pips, "{W}")
		}
		src := "Name:Artifact" + string(rune('A'+i)) + "\nTypes:Artifact\nManaCost:" +
			strings.Join(pips, "") + "\nOracle:x\n"
		ids = append(ids, h.g.AddObject(mkCard(t, src), 0).ID)
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 2 | ChangeValid$ Artifact | WithTotalCMC$ 6 | DestinationZone$ Hand"))
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 2 || hand[0] != ids[0] || hand[1] != ids[2] {
		t.Fatalf("hand = %v, want the greedy [%d %d] (4 fits, second 4 skipped, 2 fits)", hand, ids[0], ids[2])
	}
}
