package bots_test

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// spellbenchRungs is the inline ladder lifted verbatim from
// cmd/botbench/spellbench.go's sbSubmitWithFallback (the minimal rung, then
// the default-bot rung), so TestFallbacksMatchTheSpellbenchRunner proves
// bots.Fallbacks is that code and not a parallel re-implementation that can
// drift from the bench.
func spellbenchRungs(d *decision.Decision, brd botpolicy.Board, seatIdx int) []decision.Intent {
	fb := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				fb.Choices = []int{o.Index}
				break
			}
		}
	} else {
		fb = botpolicy.Clamp(d, fb)
	}
	minimal := fb
	bot := botpolicy.Decide(brd, d, rand.New(rand.NewPCG(d.Seq, uint64(seatIdx)+1)))
	bot.Seq, bot.Player = d.Seq, d.Player
	return []decision.Intent{minimal, bot}
}

// TestFallbacksMatchTheSpellbenchRunner pins bots.Fallbacks to the runner's
// inline rungs over a table of decisions, including the two branches the
// minimal rung splits on: at priority it picks the offered pass option, and
// for every other kind it is the clamped empty answer. seatIdx varies so the
// seeded bot rung is exercised on both a zero and a nonzero index.
func TestFallbacksMatchTheSpellbenchRunner(t *testing.T) {
	brdMain := botpolicy.Board{
		IsMain: true,
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{
			7:  {Controller: 0, Power: 2, Toughness: 2},
			11: {Controller: 1, Power: 3, Toughness: 3},
		}),
		Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
			7:  {Creature: true, Power: 2, CMC: 2},
			11: {Creature: true, Power: 3, CMC: 3},
		}),
	}
	cases := []struct {
		name    string
		d       *decision.Decision
		brd     botpolicy.Board
		seatIdx int
	}{
		{
			name: "priority-with-pass",
			d: &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "pass"}}},
			brd:     brdMain,
			seatIdx: 0,
		},
		{
			name: "priority-without-pass",
			d: &decision.Decision{Seq: 2, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "activate"}}},
			brd:     brdMain,
			seatIdx: 1,
		},
		{
			name: "attackers",
			d: &decision.Decision{Seq: 3, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "attack", Obj: 7}}},
			brd:     brdMain,
			seatIdx: 0,
		},
		{
			name: "blockers",
			d: &decision.Decision{Seq: 4, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 2,
				Options: []decision.Option{{Index: 0, Kind: "block", Obj: 11}}},
			brd:     brdMain,
			seatIdx: 1,
		},
		{
			name: "choose",
			d: &decision.Decision{Seq: 5, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "choose"}, {Index: 1, Kind: "choose"}}},
			brd:     botpolicy.Board{IsMain: true},
			seatIdx: 0,
		},
		// Mulligan decisions are the table's randomness-sensitive arm:
		// botpolicy.Decide draws PCG(Seq, seatIdx+1) per mulligan option, so
		// these cases pin the bot rung's exact answer to the seed derivation,
		// not merely its shape. Several Seq/seatIdx pairs make at least one
		// pair's draw differ if the seed changes.
		{
			name: "mulligan-10-0",
			d: &decision.Decision{Seq: 10, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "mulligan"}, {Index: 1, Kind: "mulligan"}, {Index: 2, Kind: "mulligan"}}},
			brd:     botpolicy.Board{},
			seatIdx: 0,
		},
		{
			name: "mulligan-11-1",
			d: &decision.Decision{Seq: 11, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "mulligan"}, {Index: 1, Kind: "mulligan"}, {Index: 2, Kind: "mulligan"}}},
			brd:     botpolicy.Board{},
			seatIdx: 1,
		},
		{
			name: "mulligan-12-2",
			d: &decision.Decision{Seq: 12, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "mulligan"}, {Index: 1, Kind: "mulligan"}, {Index: 2, Kind: "mulligan"}}},
			brd:     botpolicy.Board{},
			seatIdx: 2,
		},
		{
			name: "mulligan-13-1",
			d: &decision.Decision{Seq: 13, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "mulligan"}, {Index: 1, Kind: "mulligan"}, {Index: 2, Kind: "mulligan"}}},
			brd:     botpolicy.Board{},
			seatIdx: 1,
		},
	}
	rungsDiffer := false
	for _, tc := range cases {
		want := spellbenchRungs(tc.d, tc.brd, tc.seatIdx)
		got := bots.Fallbacks(tc.d, tc.brd, tc.seatIdx)
		if len(got) != 2 {
			t.Fatalf("%s: Fallbacks returned %d rungs, want 2", tc.name, len(got))
		}
		if len(want) != 2 {
			t.Fatalf("%s: runner returned %d rungs, want 2", tc.name, len(want))
		}
		for i := range want {
			if !intentEqual(got[i], want[i]) {
				t.Errorf("%s: rung %d = %+v, runner has %+v", tc.name, i, got[i], want[i])
			}
			if got[i].Seq != tc.d.Seq || got[i].Player != tc.d.Player {
				t.Errorf("%s: rung %d carries seq/player %d/%d, want %d/%d", tc.name, i,
					got[i].Seq, got[i].Player, tc.d.Seq, tc.d.Player)
			}
		}
		if !intentEqual(got[0], got[1]) {
			rungsDiffer = true
		}
	}
	// The test only proves anything if the two rungs are distinguishable on
	// at least one decision: an implementation that returned the same intent
	// twice would otherwise pass while losing the ladder's second rung.
	if !rungsDiffer {
		t.Fatal("the minimal and bot rungs were identical on every table case; the comparison proves nothing")
	}
}

func intentEqual(a, b decision.Intent) bool {
	if a.Seq != b.Seq || a.Player != b.Player || len(a.Choices) != len(b.Choices) {
		return false
	}
	for i := range a.Choices {
		if a.Choices[i] != b.Choices[i] {
			return false
		}
	}
	return true
}
