package main

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/rules/resolve"
)

// The resolution kernel's predicate-miss census ratchet (W3; lasagna spec
// §7.7 and the S3b results): a "miss" is a resolution the ask-free
// predicate exempted from its checkpoint that asked anyway. Today a miss
// costs nothing but the legacy path in place; once legacy is deleted (§7.7
// step 4) every miss costs a replay from the nearest retained snapshot, so
// the target is 0.
//
// The ratchet is on the CLASS SET, not a raw count. A class (resolving shape
// -> decision kind/resume kind) is structural: it names a predicate gap that
// exists whatever the games' trajectories, so a new class is always a real
// finding. A raw count over a random-deck sample is not: an unrelated engine
// change that shifts the bots' choices reshuffles which games reach a known
// class and how often, and the count moved with no gap opened or closed. So:
//
//   - tapeMissClasses is a two-directional, SHRINK-ONLY allowlist. A class
//     the sample shows that is not listed fails (fix the predicate at its
//     root -- cards/mayask.go for text, rules/resolve_mayask.go for the stack
//     object or a board gate -- never add it); a listed class the sample no
//     longer shows fails as stale (delete the line in the same commit).
//   - tapeMissCountCeiling is a generous ceiling on the total, a guard
//     against a listed class blowing up, never the ratchet itself.
//
// The sample is the games `cardfuzz -games 1000 -batch 1000 -seed 11101
// -tape` plays from an empty coverage state. The run is also the dual run:
// every game plays on the kernel and its replay is verified on the legacy
// resume path, so a "replay" failure is a kernel divergence and fails the
// test outright.
var tapeMissClasses = []string{}

const (
	tapeMissCountCeiling = 25
	tapeCensusGames      = 1000
	tapeCensusSeed       = 11101
	tapeCensusMaxTurns   = 100
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
	allowed := make(map[string]bool, len(tapeMissClasses))
	for _, c := range tapeMissClasses {
		allowed[c] = true
	}
	seen := make(map[string]bool, len(rows))
	total := 0
	for _, r := range rows {
		total += r.n
		seen[r.class] = true
		t.Logf("miss %4d  %s", r.n, r.class)
		if !allowed[r.class] {
			t.Errorf("predicate-miss class %q (%d) is not in tapeMissClasses: fix the predicate at its root "+
				"(the allowlist is SHRINK-ONLY; adding a class is a MAJOR review finding)", r.class, r.n)
		}
	}
	for _, c := range tapeMissClasses {
		if !seen[c] {
			t.Errorf("tapeMissClasses lists %q but the sample no longer misses it: delete it in the same commit", c)
		}
	}
	if total > tapeMissCountCeiling {
		t.Errorf("predicate misses: %d over %d games, above the guard ceiling of %d", total, tapeCensusGames, tapeMissCountCeiling)
	}
	t.Logf("predicate misses: %d in %d classes (target 0)", total, len(rows))
}
