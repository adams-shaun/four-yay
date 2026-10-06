package templates

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// activateCensusSets are the four level-A sets the activate census pins,
// matching compliance/levelb's requirement census.
var activateCensusSets = []string{"BIG", "EOE", "FDN", "FRA"}

// wantActivateCensus pins, per set, every non-gap activate requirement's
// outcome: served, split by sub-family, or skipped, keyed by the skip reason
// with the no-fixture target list folded away. Measured 2026-10-06 with this
// ticket's generator. The test fails in both directions: a template or
// classifier change that changes a count shows up as a diff, and so does a
// stale pin. activate.zone:* requirements are gaps the level-B classifier
// owns, so they are not counted here (compliance/levelb/census_test.go pins
// them).
var wantActivateCensus = map[string]map[string]int{
	"BIG": {
		"served:activate.battlefield":             7,
		"served:activate.mana":                    4,
		"skip:activate cost gap: Discard<...>":    1,
		"skip:activate cost gap: Exile<...>":      1,
		"skip:activate cost gap: Sac<...>":        2,
		"skip:activate cost gap: SubCounter<...>": 1,
		"skip:activate no fixture":                1,
	},
	"EOE": {
		"served:activate.battlefield":                 29,
		"served:activate.mana":                        3,
		"skip:activate cost gap: ExileFromGrave<...>": 1,
		"skip:activate cost gap: Sac<...>":            4,
		"skip:activate cost gap: SubCounter<...>":     1,
		"skip:activate cost gap: tapXType<...>":       2,
		"skip:activate no fixture":                    2,
		"skip:activate xmage text ambiguous":          20,
	},
	"FDN": {
		"served:activate.battlefield":             67,
		"served:activate.mana":                    50,
		"skip:activate cost gap: AddCounter<...>": 2,
		"skip:activate cost gap: Exile<...>":      1,
		"skip:activate cost gap: Return<...>":     1,
		"skip:activate cost gap: Sac<...>":        6,
		"skip:activate cost gap: SubCounter<...>": 4,
		"skip:activate cost gap: tapXType<...>":   2,
		"skip:activate no fixture":                9,
		"skip:activate xmage text ambiguous":      2,
	},
	"FRA": {
		"served:activate.battlefield":        59,
		"served:activate.mana":               24,
		"skip:activate cost gap: Sac<...>":   1,
		"skip:activate no fixture":           7,
		"skip:activate xmage text ambiguous": 10,
	},
}

// TestActivateCensus measures every non-gap activate requirement in the four
// level-A sets and pins served versus skipped counts.
func TestActivateCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	got := map[string]map[string]int{}
	for _, set := range activateCensusSets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		counts := map[string]int{}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Errorf("%s: %q not in the corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Family != "activate" || r.Gap != "" {
					continue
				}
				_, skip := GenerateB(reg, card, r)
				if skip == nil {
					counts["served:"+r.Sub]++
					continue
				}
				counts[activateSkipBucket(skip.Reason)]++
			}
		}
		got[set] = counts
	}

	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantActivateCensus[set], got[set]); diff != "" {
			t.Errorf("%s activate census mismatch:\n%s", set, diff)
		}
	}
}

// activateSkipBucket folds the no-fixture reason's target list away so the
// ratchet groups by cause, not by fixture detail.
func activateSkipBucket(reason string) string {
	if strings.HasPrefix(reason, "activate no fixture") {
		return "skip:activate no fixture"
	}
	return "skip:" + reason
}

// activateCensusDiff reports, in both directions, every bucket whose count
// moved.
func activateCensusDiff(want, got map[string]int) string {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var lines []string
	for _, k := range sorted {
		if want[k] != got[k] {
			lines = append(lines, fmt.Sprintf("  %-45s want %4d got %4d", k, want[k], got[k]))
		}
	}
	return strings.Join(lines, "\n")
}
