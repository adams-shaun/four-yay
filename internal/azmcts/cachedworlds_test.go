package azmcts

import (
	"context"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchprobe"
)

// cachedWorldsSearch runs one honest search at a fed position under
// Options.CachedWorlds k with the given node cache.
func cachedWorldsSearch(t *testing.T, p fedPosition, sims, k, cache int) Result {
	t.Helper()
	obs := searchprobe.NewCollector(0)
	src, err := NewRedeal(p.input(t, p.e), obs, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	opts.Sims, opts.Seed, opts.CachedWorlds, opts.NodeCache = sims, 7, k, cache
	res, err := Search(context.Background(), Root{Engine: p.e, Decision: p.d, Bot: p.bot, Observer: obs}, src, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestCachedWorldsNodeCacheIsExact pins the node cache's exactness under
// CachedWorlds: each tree walks a fixed world, so the cache changes only
// the walk's cost counters, never the visits, values or the answer.
func TestCachedWorldsNodeCacheIsExact(t *testing.T) {
	defer searchprobe.SetRedealProbeVerify(searchprobe.SetRedealProbeVerify(false))
	for _, s := range []uint64{testSeed, testSeed + 1, testSeed + 2} {
		p := feedPosition(t, testConfig(t, "mono-red-prowess", "mono-blue-tempo", s), 5, 1, nil)
		for _, k := range []int{1, 3} {
			off := cachedWorldsSearch(t, p, 40, k, 0)
			on := cachedWorldsSearch(t, p, 40, k, DefaultNodeCache)
			if off.Stats.Searched != 1 {
				t.Fatalf("seed %d k %d: not searched (%+v)", s, k, off.Stats)
			}
			if !reflect.DeepEqual(off.Visits, on.Visits) || !reflect.DeepEqual(off.Q, on.Q) || !reflect.DeepEqual(off.Avail, on.Avail) ||
				off.RootValue != on.RootValue || off.Choice != on.Choice {
				t.Fatalf("seed %d k %d: the node cache changed the result:\noff %v %v %v %v\non  %v %v %v %v",
					s, k, off.Visits, off.Q, off.RootValue, off.Choice, on.Visits, on.Q, on.RootValue, on.Choice)
			}
			if on.Stats.Completed != 40 || on.Stats.NodeResumes == 0 {
				t.Fatalf("seed %d k %d: completed %d, resumes %d", s, k, on.Stats.Completed, on.Stats.NodeResumes)
			}
			n := 0
			for _, v := range on.Visits {
				n += v
			}
			if n != 40 {
				t.Fatalf("seed %d k %d: root visits %d, want 40", s, k, n)
			}
		}
	}
}
