package main

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/rules/resolve"
)

// The resolution kernel's predicate-miss census ratchet (W3 step 1; lasagna
// spec §7.7 and the S3b results): a "miss" is a resolution the ask-free
// predicate exempted from its checkpoint that asked anyway. Today a miss
// costs nothing but the legacy path in place; once legacy is deleted
// (§7.7 step 4) every miss costs a replay from the nearest retained
// snapshot, so the target is 0.
//
// tapeMissCeiling is the misses measured over the fixed sample below -- the
// same games `cardfuzz -games 200 -batch 200 -seed 11101 -tape` plays from
// an empty coverage state -- when the kernel landed. It is SHRINK-ONLY: a
// predicate change that removes a miss class lowers it in the same commit;
// NEVER raise it. A rise means a new resolution shape asks where the
// predicate said it could not: fix the predicate at its root (cards/mayask.go
// for text, rules/resolve_mayask.go for the stack object, or a board gate for
// an engine-posed ask) rather than the constant. The test logs the census by
// class (resolving shape -> decision kind/resume kind), the key a fix
// targets. The run is also the dual run: every game plays on the kernel and
// its replay is verified on the legacy resume path, so a "replay" failure is
// a kernel divergence and fails the test outright.
const (
	tapeMissCeiling    = 10
	tapeCensusGames    = 200
	tapeCensusSeed     = 11101
	tapeCensusMaxTurns = 100
)

func TestTapeMissCensus(t *testing.T) {
	if testing.Short() {
		t.Skip("plays 200 random-deck games on the kernel and replays each on legacy")
	}
	reg := testutil.CorpusRegistry(t)
	p, err := buildPool(reg)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := loadCov(t.TempDir() + "/none.json")
	type job struct {
		seed  uint64
		decks []genDeck
	}
	jobs := make([]job, tapeCensusGames)
	for i := range jobs {
		gs := mix(tapeCensusSeed, uint64(i))
		r := rand.New(rand.NewPCG(gs, gs^0xdeadbeefcafef00d))
		jobs[i] = job{seed: gs, decks: []genDeck{generate(r, p, c), generate(r, p, c)}}
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
	for w := 0; w < 4; w++ {
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
			t.Errorf("dual-run divergence (kernel vs legacy), seed %d:\n%s", f.Seed, f.Diag)
		} else {
			t.Logf("game failure (not a kernel divergence; replay it on legacy with -repro): %s seed %d: %s", f.Kind, f.Seed, trunc(f.Diag, 200))
		}
	}
	rows := census.rows()
	total := 0
	for _, r := range rows {
		total += r.n
		t.Logf("miss %4d  %s", r.n, r.class)
	}
	switch {
	case total > tapeMissCeiling:
		t.Errorf("predicate misses: %d over %d games, above the frozen ceiling of %d. This is a SHRINK-ONLY "+
			"ratchet: fix the predicate for the new class at its root instead of raising "+
			"tapeMissCeiling (a raised constant is a MAJOR review finding).", total, tapeCensusGames, tapeMissCeiling)
	case total < tapeMissCeiling:
		t.Logf("predicate misses: %d, below the ceiling of %d: lower tapeMissCeiling in "+
			"cmd/cardfuzz/tape_census_test.go to %d in the same commit.", total, tapeMissCeiling, total)
	default:
		t.Logf("predicate misses: %d (at ceiling; target 0)", total)
	}
}
