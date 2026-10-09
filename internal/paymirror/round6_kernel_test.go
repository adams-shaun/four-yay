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
//   - 2138: admitting Phyrexian costs to auto-pay changes the bot's cast
//     trajectory, so the old seq-1488 float-trigger finding is no longer
//     reached; the seed remains a clean, control-equivalent game;
//   - 4098 seq 7795: Master of Dark Rites, equivalent. The Dig absent-
//     ChangeNum default now moves one card rather than the whole window,
//     changing the preceding game's event count (formerly seq 7684). Ticket
//     agent-20261006T052735Z-5c29adb5: Necropotence's Defined$ TopOfLibrary
//     exile is a position read and no longer emits a mandatory shuffle
//     (CR 401.4), shifting the same cast from seq 7830 to 7795;
//   - 4139: CR 601.2c now announces graveyard ChangeZone sub-targets during
//     casting, changing the bot game's path before the Urza's Incubator
//     finding. The seed remains pinned as a clean, control-equivalent game;
//     the old cast-specific finding is no longer reached after the rules fix.
//   - 4129 seq 5797: Demonic Tutor, equivalent (the Ogre pay-life-only
//     witness stays pinned by TestWitnessReadsPayLifeOnlySources).
func TestRoundSixFindingsMirrorKernel(t *testing.T) {
	t.Parallel()
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
		{2138, []string{"vivi-ornitier-cedh", "hearthhull-worldseed-landfall", "pro-shaper", "foundations-keen-engineering"}, 0, ""},
		{4098, []string{"foundations-reign-of-dragons", "hearthhull-worldseed-landfall", "avengers-assemble", "rakdos-muscle-scam-exe"}, 7795, ""},
		{4139, []string{"foundations-wretched-ranks", "deadly-disguise", "foundations-reign-of-dragons", "ulalek-eldrazi"}, 0, ""},
		{4129, []string{"rakdos-muscle-scam-exe", "pro-shaper", "foundations-reign-of-dragons", "foundations-wretched-ranks"}, 5797, ""},
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
