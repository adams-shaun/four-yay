package bench_test

// The resolution kernel's synchronous answerer (W3 step 0; spike S3b
// candidate 3): a bot-vs-bot game whose converted mid-resolution asks are
// answered inline (no checkpoint, no re-execution) must log exactly what the
// legacy resume path logs.

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/seat"
)

func TestSyncAnswerMatchesLegacy(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 18 games twice")
	}
	if os.Getenv("GORGE_TAPE_KERNEL") == "1" {
		t.Skip("GORGE_TAPE_KERNEL=1 puts the legacy arm on the kernel too")
	}
	reg := testutil.CorpusRegistry(t)
	pairs := [][2]string{
		{"dimir-tempo", "mono-black-aggro"},
		{"foundations-wretched-ranks", "ur-delver"},
		{"uw-control", "mono-red-goblins"},
	}
	before := resolve.ReadStats()
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
			play := func(tape bool) *rules.Engine {
				c := cfg
				c.TapeKernel = tape
				seats := []seat.Seat{seat.NewBot(c.Seed ^ 1), seat.NewBot(c.Seed ^ 2)}
				_, e, err := bench.PlayGame(c, seats, 60, 4000, bench.Hooks{SyncAnswer: tape})
				if err != nil {
					t.Fatal(err)
				}
				return e
			}
			legacy, tape := play(false), play(true)
			if legacy.L.Head() != tape.L.Head() || len(legacy.L.Events) != len(tape.L.Events) {
				t.Fatalf("%v seed %d: sync-answered game diverged from legacy (%d vs %d events)",
					p, cfg.Seed, len(legacy.L.Events), len(tape.L.Events))
			}
		}
	}
	st := resolve.ReadStats().Sub(before)
	t.Logf("kernel stats: %+v", st)
	if st.Checkpoints != 0 {
		t.Fatalf("an all-bot engine took %d checkpoints", st.Checkpoints)
	}
	if st.Exempt == 0 {
		t.Fatal("the games never reached the kernel")
	}
}
