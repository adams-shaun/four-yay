package main

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/rules/resolve"
)

// TestTapeMissCensusReportedSeeds replays the two specific census jobs a
// filed bug report named -- game 231 (seed 12784773299186375804) and game 382
// (seed 5780855625201442341) of the tapeCensusSeed 11101 sample -- and pins
// that both complete cleanly with zero predicate misses at current main.
//
// It is NOT a portable reproducer of the filed failure. buildPool only admits
// a card when the build fully supports it, and `generate` samples from that
// pool, so as engine support grows the same seed builds a DIFFERENT deck at a
// different commit. The filing branch tip is 147 commits behind main (105 of
// them in effects/ and rules/), so the reported seed->deck mapping describes a
// pool that no longer exists: main's game 231 is a red/green pair with no
// RepeatEach carrier at all. This test therefore pins current-main behaviour
// for these two named jobs, not the reported failure; a seed-based bisect
// would prove nothing. The structural regression test for the reported
// RepeatEach livelock is
// rules.TestThievesAuctionRepeatEachForgetShrinksOuterRemembered (with its
// effects.rememberIteration unit tests).
func TestTapeMissCensusReportedSeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("plays two random-deck games on the kernel and replays each on legacy")
	}
	const (
		wantJobA = 231
		wantJobB = 382
	)
	wantSeeds := map[int]uint64{
		wantJobA: 12784773299186375804,
		wantJobB: 5780855625201442341,
	}
	reg := testutil.CorpusRegistry(t)
	p, err := buildPool(reg)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := loadCov(t.TempDir() + "/none.json")

	type job struct {
		i     int
		seed  uint64
		decks []genDeck
	}
	var jobs []job
	for i := 0; i < tapeCensusGames; i++ {
		gs := mix(tapeCensusSeed, uint64(i))
		r := rand.New(rand.NewPCG(gs, gs^0xdeadbeefcafef00d))
		d := []genDeck{generate(r, p, c), generate(r, p, c)}
		if want, ok := wantSeeds[i]; ok {
			// Precondition: the seed the report named really is this job's
			// mix, so the test replays the reported job and not a neighbour.
			if gs != want {
				t.Fatalf("precondition: job %d seed = %d, want the reported %d", i, gs, want)
			}
			jobs = append(jobs, job{i: i, seed: gs, decks: d})
		}
	}
	if len(jobs) != len(wantSeeds) {
		t.Fatalf("precondition: built %d reported jobs, want %d", len(jobs), len(wantSeeds))
	}

	prevDual := tapeDual
	tapeDual = true
	var census missCensus
	prevObs := rules.SetTapeMissObserver(census.add)
	defer func() {
		tapeDual = prevDual
		rules.SetTapeMissObserver(prevObs)
	}()
	before := resolve.ReadStats()

	var mu sync.Mutex
	var fails []*failure
	ch := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				f, _ := playOne(reg, j.decks, j.seed, tapeCensusMaxTurns, 20000, 20000, true, true)
				if f != nil {
					mu.Lock()
					fails = append(fails, f)
					mu.Unlock()
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	st := resolve.ReadStats().Sub(before)
	t.Logf("kernel stats: %+v", st)
	for _, f := range fails {
		if f.Kind == "replay" {
			t.Errorf("dual-run divergence (kernel vs legacy), job seed %d:\n%s", f.Seed, f.Diag)
		} else {
			t.Errorf("game failure kind=%s seed=%d turns=%d intents=%d:\n%.600s", f.Kind, f.Seed, f.Turns, f.Intents, f.Diag)
		}
	}
	rows := census.rows()
	total := 0
	for _, r := range rows {
		total += r.n
		t.Logf("miss %4d  %s", r.n, r.class)
	}
	if total != 0 || len(rows) != 0 {
		t.Errorf("predicate misses: %d in %d classes, want 0 in 0", total, len(rows))
	}
}
