package bench_test

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/seat"
)

// TestSyncAnswerMatchesTape is the kernel-era successor of
// TestSyncAnswerMatchesLegacy: the legacy resume path is gone, but the
// synchronous answerer's guarantee still has two sides worth pinning. A
// bot-vs-bot game whose converted mid-resolution asks are answered inline
// (SyncAnswer: no checkpoint, no re-execution) must log exactly what the
// same game logs when every such ask is posed and the resolution re-executed
// from its checkpoint with the answer served; and the inline games must take
// no checkpoint at all while still reaching the kernel.
func TestSyncAnswerMatchesTape(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 18 games twice")
	}
	reg := testutil.CorpusRegistry(t)
	pairs := [][2]string{
		{"dimir-tempo", "mono-black-aggro"},
		{"foundations-wretched-ranks", "ur-delver"},
		{"uw-control", "mono-red-goblins"},
	}
	var sync resolve.Stats
	for _, p := range pairs {
		da, err := testutil.LoadRepoDeck(reg, p[0])
		if err != nil {
			t.Fatal(err)
		}
		db, err := testutil.LoadRepoDeck(reg, p[1])
		if err != nil {
			t.Fatal(err)
		}
		for g := uint64(0); g < 6; g++ {
			cfg := rules.Config{Seed: 4100 + g, Names: p[:], Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens}
			play := func(inline bool) *rules.Engine {
				seats := []seat.Seat{seat.NewBot(cfg.Seed ^ 1), seat.NewBot(cfg.Seed ^ 2)}
				_, e, err := bench.PlayGame(cfg, seats, 60, 4000, bench.Hooks{SyncAnswer: inline})
				if err != nil {
					t.Fatal(err)
				}
				return e
			}
			taped := play(false)
			before := resolve.ReadStats()
			inline := play(true)
			st := resolve.ReadStats().Sub(before)
			sync.Checkpoints += st.Checkpoints
			sync.Exempt += st.Exempt
			if taped.L.Head() != inline.L.Head() || len(taped.L.Events) != len(inline.L.Events) {
				for k := 0; k < len(taped.L.Events) && k < len(inline.L.Events); k++ {
					a, b := taped.L.Events[k], inline.L.Events[k]
					if a.Kind != b.Kind || a.Text != b.Text || a.Obj != b.Obj || a.Amount != b.Amount {
						for j := k - 6; j < k+4; j++ {
							t.Logf("%d tape %v %d %q | inline %v %d %q", j, taped.L.Events[j].Kind, taped.L.Events[j].Obj, taped.L.Events[j].Text, inline.L.Events[j].Kind, inline.L.Events[j].Obj, inline.L.Events[j].Text)
						}
						break
					}
				}
				t.Fatalf("%v seed %d: sync-answered game diverged from the checkpointed tape game (%d vs %d events)",
					p, cfg.Seed, len(taped.L.Events), len(inline.L.Events))
			}
		}
	}
	t.Logf("inline kernel stats: checkpoints %d exempt %d", sync.Checkpoints, sync.Exempt)
	if sync.Checkpoints != 0 {
		t.Fatalf("an all-bot inline engine took %d checkpoints", sync.Checkpoints)
	}
	if sync.Exempt == 0 {
		t.Fatal("the inline games never reached the kernel")
	}
}
