package main

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// -az-corpus (M1b) is observational, for the clairvoyant and the honest
// (redeal) world alike: a recorded spellbench game plays
// exactly as the unrecorded one, its records decode as a visit corpus, and
// every record carries the recording seat's outcome.
func TestAZCorpusRecordsWithoutChangingTheGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	deck, err := spellbench.Deck(reg, spellbench.PauperKernel, "Burn")
	if err != nil {
		t.Fatal(err)
	}
	prevCfg, prevPath, prevWorld := azCfg, azCorpusPath, azWorldArg
	t.Cleanup(func() { azCfg, azCorpusPath, azWorldArg = prevCfg, prevPath, prevWorld })
	clairvoyant.AllowClairvoyant()
	for _, pol := range []struct{ policy, world string }{
		{"az", azmcts.WorldClairvoyant},
		{"az-redeal", azmcts.WorldRedeal},
	} {
		azCfg = azmcts.DefaultSeatConfig()
		azCfg.Search.Sims = 3
		azCfg.World, azWorldArg = azmcts.WorldClairvoyant, azmcts.WorldClairvoyant
		g := sbGame{id: "m0000p0000g1", seed: sbGameSeed(20260926, 0, 0), deck: "Burn", seats: [2]string{"bot", pol.policy}}
		azCorpusPath = ""
		plain := sbPlay(g, deck, reg, 200, 20000)
		azCorpusPath = filepath.Join(t.TempDir(), "v.jsonl.gz")
		rec := sbPlay(g, deck, reg, 200, 20000)
		if plain.err != nil || rec.err != nil || plain.outcome != rec.outcome || plain.corpus != nil {
			t.Fatalf("%s: plain %+v (%v), recorded %+v (%v)", pol.policy, plain.outcome, plain.err, rec.outcome, rec.err)
		}
		if rec.visits == 0 {
			t.Fatalf("%s: no searched decision recorded", pol.policy)
		}
		n, err := sbWriteCorpus(azCorpusPath, []sbResult{plain, rec})
		if err != nil || n != rec.visits {
			t.Fatalf("%s: wrote %d records (%v), want %d", pol.policy, n, err, rec.visits)
		}
		exs, recs, st, err := policynet.LoadVisits(azCorpusPath, policynet.VisitLoadOptions{})
		if err != nil || len(exs) != rec.visits || st.Features != policynet.FeaturesMZ || st.World != pol.world {
			t.Fatalf("%s: loaded %d (%+v, %v)", pol.policy, len(exs), st, err)
		}
		want := 0.0
		if !rec.outcome.Draw && rec.outcome.WinnerSeat == 1 {
			want = 1
		}
		for _, r := range recs {
			if r.Seat != 1 || r.Opponent != "bot" || r.GameID != g.id || r.Deck != "Burn" || !r.OutcomeKnown || r.Outcome != want {
				t.Fatalf("%s: record %+v, outcome %+v", pol.policy, r, rec.outcome)
			}
		}
	}
}
