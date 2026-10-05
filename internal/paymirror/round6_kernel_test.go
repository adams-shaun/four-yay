package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRoundSixFindingsMirrorKernel replays the round-6 finding games end to
// end on the resolution kernel: no cast is a mismatch, the live-vs-clone
// control is equivalent everywhere, and each named cast gets its root-caused
// verdict (the history of every pin is in the deleted
// TestRoundSixFindingsMirror's doc, round6_test.go at 19fa2a40d):
//
//   - 4130: control contChain.len; the Firebird cast no longer occurs, so the
//     seed keeps an empty pin and asserts a clean, control-equivalent game;
//   - 2138 seq 1488: G.Stack reorder by a float-triggered ability (expected
//     float_trigger_precedes_cast);
//   - 4098 seq 7828: Master of Dark Rites, equivalent. The Dig absent-
//     ChangeNum default now moves one card rather than the whole window,
//     changing the preceding game's event count (formerly seq 7684);
//   - 4139 seq 6763: Urza's Incubator (damageSourceLKI cost-move mask),
//     equivalent. Re-measured on the kernel: one event earlier than the
//     legacy pin 6764;
//   - 4129 seq 5796: Demonic Tutor, equivalent (the Ogre pay-life-only
//     witness stays pinned by TestWitnessReadsPayLifeOnlySources).
func TestRoundSixFindingsMirrorKernel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seed  uint64
		decks []string
		seq   uint64
		want  string // the named cast's verdict key ("" = equivalent)
	}{
		{4130, []string{"vivi-ornitier-cedh", "foundations-reign-of-dragons", "avengers-assemble", "valgavoth-endless-punishment"}, 0, ""},
		{2138, []string{"vivi-ornitier-cedh", "hearthhull-worldseed-landfall", "pro-shaper", "foundations-keen-engineering"}, 1488, "expected:float_then_cast:float_trigger_precedes_cast"},
		{4098, []string{"foundations-reign-of-dragons", "hearthhull-worldseed-landfall", "avengers-assemble", "rakdos-muscle-scam-exe"}, 7828, ""},
		{4139, []string{"foundations-wretched-ranks", "deadly-disguise", "foundations-reign-of-dragons", "ulalek-eldrazi"}, 6763, ""},
		{4129, []string{"rakdos-muscle-scam-exe", "pro-shaper", "foundations-reign-of-dragons", "foundations-wretched-ranks"}, 5796, ""},
	} {
		reports := round6Game(t, d, GameSpec{Seed: tc.seed, Decks: tc.decks, Commander: true, Policy: "bot"})
		if len(reports) == 0 {
			t.Errorf("seed %d: no planned-cast reports; clean-game assertions would be vacuous", tc.seed)
		}
		found := false
		for _, r := range reports {
			st, key := r.Verdict()
			if st == Mismatch {
				t.Errorf("seed %d seq %d %q: %s (witness %q)", tc.seed, r.Seq, r.Card, key, r.AWitness)
			}
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", tc.seed, r.Seq, r.Card, r.Control)
			}
			if r.Seq != tc.seq {
				continue
			}
			found = true
			if key != tc.want {
				t.Errorf("seed %d seq %d %q: verdict %s %q, want %q", tc.seed, r.Seq, r.Card, st, key, tc.want)
			}
		}
		if !found && tc.seq != 0 {
			t.Errorf("seed %d: no planned cast at seq %d (the game no longer reaches the finding)", tc.seed, tc.seq)
		}
	}
}
