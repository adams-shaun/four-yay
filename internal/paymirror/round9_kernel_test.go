package paymirror

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRoundNineFindingsMirrorKernel replays the round-9 paymirror finding
// games (testdata/round9.jsonl: repo deck names or random card-name lists,
// no script text) end to end on the resolution kernel: no cast is a
// mismatch, the live-vs-clone control is equivalent, and each named cast
// gets its root-caused verdict (the history of every pin is in the deleted
// TestRoundNineFindingsMirror's doc, round9_test.go at 19fa2a40d):
//
//   - 10860 seq 3238: Worldly Tutor (the Command Tower production fix),
//     equivalent;
//   - 11056: the Artisan finding is no longer reached; a clean,
//     control-equivalent game;
//   - 10056 seq 6128: Lagomos, Hand of Hatred, equivalent;
//   - 8175 seq 3695: Songs of the Damned, float_trigger_precedes_cast.
//     Re-measured on the kernel: one event earlier than the legacy pin 3696.
func TestRoundNineFindingsMirrorKernel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/round9.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var specs []GameSpec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var s GameSpec
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		specs = append(specs, s)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("testdata holds %d specs, want 4", len(specs))
	}
	const precedes = "expected:float_then_cast:float_trigger_precedes_cast"
	want := map[uint64]map[uint64]string{ // seed -> seq -> verdict key ("" = equivalent)
		10860: {3238: ""},
		11056: {},
		10056: {6128: ""},
		8175:  {3695: precedes},
	}
	for _, spec := range specs {
		reports := round6Game(t, d, spec)
		if len(reports) == 0 {
			t.Errorf("seed %d: no planned-cast reports; clean-game assertions would be vacuous", spec.Seed)
		}
		seen := map[uint64]bool{}
		for _, r := range reports {
			st, key := r.Verdict()
			if st == Mismatch || (st == Unmirrorable && !r.ExpectedUnmirrorable()) {
				t.Errorf("seed %d seq %d %q: %s %s", spec.Seed, r.Seq, r.Card, st, key)
			}
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", spec.Seed, r.Seq, r.Card, r.Control)
			}
			w, ok := want[spec.Seed][r.Seq]
			if !ok {
				continue
			}
			seen[r.Seq] = true
			if key != w {
				t.Errorf("seed %d seq %d %q: verdict %s %q, want %q", spec.Seed, r.Seq, r.Card, st, key, w)
			}
		}
		for seq := range want[spec.Seed] {
			if !seen[seq] {
				t.Errorf("seed %d: no planned cast at seq %d (the game no longer reaches the finding)", spec.Seed, seq)
			}
		}
	}
}
