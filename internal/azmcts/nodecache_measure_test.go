package azmcts

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// measureRoot is one searched root of the node-cache measurements.
type measureRoot struct {
	name string
	e    *rules.Engine
	d    *decision.Decision
	bot  decision.Intent
}

// measureRoots finds up to n searchable seat-0 roots over two deck pairs,
// a spread of seeds and minimum turns (early, mid, late game).
func measureRoots(t testing.TB, n int) []measureRoot {
	t.Helper()
	pairs := [][2]string{{"uw-tempo", "mono-blue-tempo"}, {"mono-red-prowess", "mono-blue-tempo"}, {"mono-green-stompy", "ur-delver"}}
	var out []measureRoot
	for s := uint64(0); len(out) < n && s < 64; s++ {
		for _, p := range pairs {
			for _, turn := range []int32{3, 6, 9} {
				if len(out) >= n {
					return out
				}
				cfg := testConfig(t, p[0], p[1], 90000000+s)
				e, d, bot, err := findPosition(cfg, "", turn, 6000)
				if err != nil {
					continue
				}
				out = append(out, measureRoot{name: p[0] + "/" + strconv.FormatUint(s, 10) + "/t" + strconv.Itoa(int(turn)), e: e, d: d, bot: bot})
			}
		}
	}
	return out
}

func measureSource(kind string, e *rules.Engine, obs *searchprobe.Collector) WorldSource {
	switch kind {
	case "clairvoyant":
		src, _ := newTestClairvoyant(e, obs)
		return src
	case "pimc":
		return &FixedChance{Base: e, Observer: obs, Seed: 0x5eed}
	}
	panic("unknown source " + kind)
}

func vmHWM() string {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "?"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmHWM:") || strings.HasPrefix(l, "VmRSS:") {
			return strings.Join(strings.Fields(l), " ")
		}
	}
	return "?"
}

// TestNodeCacheMeasure is the node cache's cost report, not a check: it runs
// only with AZ_MEASURE=1. AZ_SIMS (default 100), AZ_SOURCE (clairvoyant or
// pimc), AZ_CACHE (the Options.NodeCache value; 0 is off) and AZ_ROOTS pick
// one configuration, so a separate process per configuration reports its
// own peak RSS.
func TestNodeCacheMeasure(t *testing.T) {
	if os.Getenv("AZ_MEASURE") != "1" {
		t.Skip("measurement only: AZ_MEASURE=1")
	}
	atoi := func(k string, d int) int {
		if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
			return v
		}
		return d
	}
	sims, cache, nroots := atoi("AZ_SIMS", 100), atoi("AZ_CACHE", 0), atoi("AZ_ROOTS", 12)
	kind := os.Getenv("AZ_SOURCE")
	if kind == "" {
		kind = "clairvoyant"
	}
	roots := measureRoots(t, nroots)
	runtime.GC()
	rssBase := vmHWM()
	var tot Stats
	var elapsed time.Duration
	cpu0 := cpuTime()
	for i, r := range roots {
		opts := DefaultOptions()
		opts.Sims, opts.Seed = sims, uint64(i)+1
		setNodeCache(&opts, cache)
		obs := searchprobe.NewCollector(r.d.Player)
		src := measureSource(kind, r.e, obs)
		start := time.Now()
		res, err := Search(context.Background(), Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs}, src, nil, opts)
		elapsed += time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		tot.Add(res.Stats)
	}
	cpu := cpuTime() - cpu0
	s := float64(max(tot.Simulations, 1))
	t.Logf("source=%s sims=%d cache=%d roots=%d searched=%d", kind, sims, cache, len(roots), tot.Searched)
	t.Logf("sims/s=%.0f  us/sim=%.1f  cpu sims/s=%.0f (process user+sys, GC included)", float64(tot.Simulations)/elapsed.Seconds(),
		float64(elapsed.Microseconds())/s, float64(tot.Simulations)/cpu.Seconds())
	t.Logf("per sim: envSteps=%.2f replaySteps=%.2f (%.1f%%) plays=%.2f replayPlays=%.2f expanded=%.3f terminal=%.3f capped=%.3f",
		float64(tot.EnvSteps)/s, float64(tot.ReplaySteps)/s, 100*float64(tot.ReplaySteps)/float64(max(tot.EnvSteps, 1)),
		float64(tot.Plays)/s, float64(tot.ReplayPlays)/s, float64(tot.Expanded)/s, float64(tot.Terminal)/s, float64(tot.StepCapped)/s)
	t.Logf("per sim: nodeSaves=%.3f nodeResumes=%.3f nodeEvicts=%.3f  completed=%d failures=%d",
		float64(tot.NodeSaves)/s, float64(tot.NodeResumes)/s, float64(tot.NodeEvicts)/s, tot.Completed,
		tot.ChanceFailures+tot.Panics+tot.SubmitErrors+tot.BadWorlds+tot.NoWorld)
	t.Logf("rss before searches: %s   after: %s", rssBase, vmHWM())
}

func setNodeCache(o *Options, n int) { o.NodeCache = n }

// cpuTime is the process's user+sys CPU time: with GOMAXPROCS=1 it is the
// search's own cost, GC included, whatever else the machine runs.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
