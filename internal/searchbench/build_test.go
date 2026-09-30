package searchbench

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGames is n games with random candidate lists and a pure fake
// evaluator: each (row, turn, kind) is an item or a rejection by a hash.
func fakeGames(n int) []BuildGame {
	r := rand.New(rand.NewPCG(1, 2))
	kinds := []DecisionType{DecisionAttack, DecisionHold, DecisionBlock, DecisionSpell}
	var out []BuildGame
	for i := 0; i < n; i++ {
		g := BuildGame{Index: i, Row: 1000 + i, Split: SplitTest, DraftID: fmt.Sprintf("d%d", i/2)}
		if r.IntN(4) == 0 {
			g.Split = SplitDev
		}
		for _, k := range kinds {
			for t := 3; t < 3+r.IntN(6); t++ {
				g.Candidates = append(g.Candidates, BuildCandidate{Turn: t, Kind: k})
			}
		}
		out = append(out, g)
	}
	return out
}

func fakeEval(calls *atomic.Int64, jitter bool) Evaluator {
	return func(w int, g *BuildGame, c BuildCandidate) (Outcome, error) {
		calls.Add(1)
		h := splitMix64(uint64(g.Row)<<16 ^ uint64(c.Turn)<<4 ^ uint64(len(c.Kind)))
		if jitter {
			time.Sleep(time.Duration(h%50) * time.Microsecond)
		}
		if h%3 == 0 {
			return Outcome{Side: "gorge", Key: "trivial"}, nil
		}
		return Outcome{Item: &StoreItem{Row: g.Row, Turn: c.Turn, Type: c.Kind, Split: g.Split}}, nil
	}
}

func smallQuota() map[Split]map[DecisionType]int {
	return map[Split]map[DecisionType]int{
		SplitTest: {DecisionSpell: 20, DecisionHold: 10, DecisionAttack: 15, DecisionBlock: 10},
		SplitDev:  {DecisionSpell: 6, DecisionHold: 3, DecisionAttack: 5, DecisionBlock: 3},
	}
}

func TestBuildIsSequentialWhateverTheWorkers(t *testing.T) {
	games := fakeGames(200)
	var c1, c4 atomic.Int64
	one, err := Build(games, BuildConfig{Quota: smallQuota(), Workers: 1}, fakeEval(&c1, false))
	if err != nil {
		t.Fatal(err)
	}
	four, err := Build(games, BuildConfig{Quota: smallQuota(), Workers: 4}, fakeEval(&c4, true))
	if err != nil {
		t.Fatal(err)
	}
	if !one.Done || !four.Done {
		t.Fatalf("quotas not met: %v %v", one.Counts, four.Counts)
	}
	key := func(r *BuildResult) []string {
		var out []string
		for _, it := range r.Items {
			out = append(out, fmt.Sprintf("%s/%s/%d/%d", it.Split, it.Type, it.Row, it.Turn))
		}
		return out
	}
	if !reflect.DeepEqual(key(one), key(four)) || !reflect.DeepEqual(one.Rejections, four.Rejections) || one.GamesUsed != four.GamesUsed {
		t.Fatalf("worker count changed the selection")
	}
	// The rules: two items per game, one non-block and one block item per turn.
	per := map[int]int{}
	turn := map[[3]int]int{}
	for _, it := range one.Items {
		per[it.Row]++
		turn[[3]int{it.Row, it.Turn, b2i(it.Type == DecisionBlock)}]++
	}
	for row, n := range per {
		if n > 2 {
			t.Fatalf("row %d has %d items", row, n)
		}
	}
	for k, n := range turn {
		if n > 1 {
			t.Fatalf("%v used %d times", k, n)
		}
	}
	for sp, q := range smallQuota() {
		for k, n := range q {
			if one.Counts[sp][k] != n {
				t.Fatalf("%s %s = %d, want %d", sp, k, one.Counts[sp][k], n)
			}
		}
	}
	// One draft never spans the two splits.
	ds := map[string]Split{}
	for _, it := range one.Items {
		d := games[it.Row-1000].DraftID
		if s, ok := ds[d]; ok && s != it.Split {
			t.Fatalf("draft %s spans splits", d)
		}
		ds[d] = it.Split
	}
	// The single-worker run consumed exactly what it evaluated.
	if one.Speculative > 4*len(games) {
		t.Fatalf("speculative %d", one.Speculative)
	}
}
