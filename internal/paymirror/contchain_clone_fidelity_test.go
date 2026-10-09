package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The gate runs this package inside the affected phase's $others wave, whose
// wall is the slowest package in it: measured on the 2026-10-09 gate logs
// (agent-20261009T095157Z-a547477b/tm1, an engine-touching ticket),
// internal/paymirror was that wave's pole at 52.1s wall. The corpus-backed
// tests below replay independent games and share only the read-only corpus
// registry (testutil.CorpusRegistry's once-cached open), so they run in
// parallel, as the effects package's oracle sweep does (commit 134da95fa);
// the corpus-free unit tests stay serial. Measured at 4 vCPU with
// -cpuprofile as the uncached vehicle, interleaved before/after/before/after
// while the box stayed loaded: 33.4s/29.4s -> 16.1s/13.9s wall, peak RSS
// 824 MiB -> ~1.25 GiB (inside the 4 GiB per-test budget).
func TestCloneFidelityAtPlannedCastsSeed1068(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	g := PlayGame(d, GameSpec{Seed: 1068, Decks: []string{"tron", "death-n-taxes"}, Policy: "bot"},
		DriverOptions{Control: true})
	checked := 0
	for _, r := range g.Reports {
		if r.Control == nil {
			continue
		}
		checked++
		if r.Control.Status != Equivalent {
			t.Errorf("seq %d %q: live engine vs clone after the same intents: %s %v", r.Seq, r.Card, r.Control.Signature, r.Control.Diffs)
		}
	}
	if checked == 0 {
		t.Fatal("no planned cast was checked")
	}
}
