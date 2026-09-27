package paymirror

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRoundEightControlFindings replays the two round-8 paymirror games whose
// live-vs-clone control mismatched (testdata/round8.jsonl: random-deck card
// name lists, no script text) and requires every control equivalent.
//
//   - 8090 (random2, 5x control|mismatch|state_differs|pending.Options.len):
//     the live engine offered Island of Wak-Wak's "Target creature with
//     flying" ability and the clone did not. The Derived memo stored a
//     computed empty keyword list as nil -- SpecContext's "unbound, read the
//     printed face" -- in an entry that had never held a keyword, so a
//     clone's fresh memo answered a with<Keyword> filter from the printed
//     face while the live engine's older entry answered from the derived
//     list (rules/derivedmemo.go memoOwned; the rules-side pin is
//     TestDerivedMemoKeepsEmptyKeywordListBound).
//   - 8529 (random4, 34x control|mismatch|state_differs|discardAllFirstTime):
//     rules.Engine.discardAllFirstTime is the DiscardedAll matcher's FirstTime$
//     scratch, written on every match and read only right after it; Clone
//     copies none of it by design, so it is excluded from the comparison like
//     the other scratch fields (diff.go's excluded table).
func TestRoundEightControlFindings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/round8.jsonl")
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
	if len(specs) != 2 {
		t.Fatalf("testdata holds %d specs, want 2", len(specs))
	}
	for _, spec := range specs {
		reports := round6Game(t, d, spec)
		if len(reports) == 0 {
			t.Errorf("seed %d: no planned cast checked", spec.Seed)
		}
		for _, r := range reports {
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", spec.Seed, r.Seq, r.Card, r.Control)
			}
			if st, key := r.Verdict(); st == Mismatch || (st == Unmirrorable && !r.ExpectedUnmirrorable()) {
				t.Errorf("seed %d seq %d %q: %s %s", spec.Seed, r.Seq, r.Card, st, key)
			}
		}
	}
}
