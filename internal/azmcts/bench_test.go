package azmcts

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// stagePosition finds a searchable seat-0 decision at turn >= minTurn with
// uw-tempo in seat 0 against mono-blue-tempo (the Stage 0 deck set), trying
// the eval block's first ten seeds in order.
func stagePosition(b *testing.B, minTurn int32) (*rules.Engine, *decision.Decision, decision.Intent) {
	b.Helper()
	for s := uint64(90000000); s < 90000010; s++ {
		cfg := testConfig(b, "uw-tempo", "mono-blue-tempo", s)
		if e, d, bot, err := findPosition(cfg, "", minTurn, 6000); err == nil {
			return e, d, bot
		}
	}
	b.Fatalf("no seed in 90000000..90000009 reached a searchable seat-0 decision at turn %d", minTurn)
	return nil, nil, decision.Intent{}
}

// benchSearch reports ns per simulation (the spec's per-simulation cost)
// plus -benchmem's allocations per searched decision (one op = one Search of
// 25 simulations, heuristic leaf, clairvoyant).
func benchSearch(b *testing.B, minTurn int32) {
	e, d, bot := stagePosition(b, minTurn)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 25, 1
	b.ReportAllocs()
	b.ResetTimer()
	sims := 0
	for i := 0; i < b.N; i++ {
		obs := searchprobe.NewCollector(d.Player)
		src, err := newTestClairvoyant(e, obs)
		if err != nil {
			b.Fatal(err)
		}
		res, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, src, nil, opts)
		if err != nil {
			b.Fatal(err)
		}
		sims += res.Stats.Simulations
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(max(sims, 1)), "ns/sim")
}

func BenchmarkSearchDecisionEarly(b *testing.B) { benchSearch(b, 3) }

// Late: the event log is long here, so the first append after each Clone
// copies it (spec §2 "Known cost risk").
func BenchmarkSearchDecisionLate(b *testing.B) { benchSearch(b, 9) }

// BenchmarkSearchDecisionRedeal is the honest source's per-simulation cost
// (ns/sim): per op, one Search of 25 simulations with the redeal world
// source at each of three fed mid-game positions (feedPosition), heuristic
// leaf -- every simulation clones the base and re-deals the hidden zones.
func BenchmarkSearchDecisionRedeal(b *testing.B) {
	defer searchprobe.SetRedealProbeVerify(searchprobe.SetRedealProbeVerify(false))
	var ps []fedPosition
	for _, s := range []uint64{testSeed, testSeed + 1, testSeed + 2} {
		ps = append(ps, feedPosition(b, testConfig(b, "mono-red-prowess", "mono-blue-tempo", s), 5, 1, nil))
	}
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 25, 1
	b.ReportAllocs()
	b.ResetTimer()
	sims := 0
	for i := 0; i < b.N; i++ {
		for _, p := range ps {
			obs := searchprobe.NewCollector(0)
			src, err := NewRedeal(p.input(b, p.e), obs, uint64(i), 0)
			if err != nil {
				b.Fatal(err)
			}
			res, err := Search(context.Background(), Root{Engine: p.e, Decision: p.d, Bot: p.bot, Observer: obs}, src, nil, opts)
			if err != nil {
				b.Fatal(err)
			}
			sims += res.Stats.Simulations
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(max(sims, 1)), "ns/sim")
}
