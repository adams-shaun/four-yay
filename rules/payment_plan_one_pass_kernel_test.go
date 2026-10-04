package rules

// Kernel-era restorations of the tests W3 removed from payment_plan_one_pass_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestPaymentPlanOnePassMatchesReferenceOverAutoPayGame drives fixed-seed
// auto-pay games -- two-seat Legacy games and the repo's Commander pairings
// (command zone, tax, 40 life) -- and at EVERY priority decision compares
// the one-pass offers against the reference builder at the same state. It
// demands at least 200 priority decisions and floors of offered and
// planned casts, so the drive cannot pass vacuously.
func TestPaymentPlanOnePassMatchesReferenceOverAutoPayGameKernel(t *testing.T) {
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
		onePassDriveGame(t, Config{Seed: 5550101 + seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Cards}, seed, &st)
	}
	legacy := st
	for _, g := range repoCommanderGames[:3] {
		cfg := Config{
			Seed: g.seed, Names: []string{g.file, g.opp},
			Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, g.file), testutil.RepoDeck(t, reg, g.opp)},
			Tokens: reg.Tokens, Format: FormatCommander, StartingLife: 40,
			Commanders: [][]int{{testutil.RepoDeckFile(t, g.file).CommanderIndex()}, {testutil.RepoDeckFile(t, g.opp).CommanderIndex()}},
		}
		onePassDriveGame(t, cfg, g.bot, &st)
	}
	t.Logf("legacy: %+v; with commander games: %+v", legacy, st)
	if st.decisions < 200 || st.offered < 50 || st.planned < 20 {
		t.Fatalf("coverage floor: %+v, want decisions>=200 offered>=50 planned>=20", st)
	}
}
