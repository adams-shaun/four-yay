package rules

// Kernel-era restorations of the tests W3 removed from payment_plan_one_pass_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernel<chunk> drives
// fixed-seed auto-pay games -- two-seat Legacy games and the repo's
// Commander pairings (command zone, tax, 40 life) -- and at EVERY priority
// decision compares the one-pass offers against the reference builder at the
// same state.
//
// The drive is split into independent tests (the operator's per-test
// budget, 2026-10-05: 2 GB, 2 vCPU, 1 min): the Legacy chunk plays seeds
// until its own floor (at least 400 priority decisions and 100 offered
// casts, at most 16 seeds) exactly as the unsplit drive did, and each of the
// first three Commander pairings is a chunk of its own. The unsplit drive's
// combined floor (decisions>=200, offered>=50, planned>=20) is held by the
// Legacy chunk alone, so no chunk can pass vacuously; each Commander chunk
// must reach at least one priority decision.
func TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernelLegacy(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	var st onePassGameStats
	for seed := uint64(0); (st.decisions < 400 || st.offered < 100) && seed < 16; seed++ {
		names := []string{all[int(seed)%len(all)], all[(int(seed)+3)%len(all)]}
		decks := [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])}
		onePassDriveGame(t, Config{Seed: 5550101 + seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}, seed, &st)
	}
	t.Logf("legacy: %+v", st)
	if st.decisions < 200 || st.offered < 50 || st.planned < 20 {
		t.Fatalf("coverage floor: %+v, want decisions>=200 offered>=50 planned>=20", st)
	}
}

func TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernelCommander0(t *testing.T) {
	onePassCommanderGame(t, 0)
}
func TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernelCommander1(t *testing.T) {
	onePassCommanderGame(t, 1)
}
func TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernelCommander2(t *testing.T) {
	onePassCommanderGame(t, 2)
}

// onePassCommanderGame drives repoCommanderGames[i] under the one-pass
// comparison.
func onePassCommanderGame(t *testing.T, i int) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	g := repoCommanderGames[i]
	cfg := Config{
		Seed: g.seed, Names: []string{g.file, g.opp},
		Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, g.file), testutil.RepoDeck(t, reg, g.opp)},
		Tokens: reg.Tokens, Format: FormatCommander, StartingLife: 40,
		Commanders: [][]int{{testutil.RepoDeckFile(t, g.file).CommanderIndex()}, {testutil.RepoDeckFile(t, g.opp).CommanderIndex()}},
	}
	var st onePassGameStats
	onePassDriveGame(t, cfg, g.bot, &st)
	t.Logf("commander game %d: %+v", i, st)
	if st.decisions == 0 {
		t.Fatalf("commander game %d reached no priority decision: %+v", i, st)
	}
}
