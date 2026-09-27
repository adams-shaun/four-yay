package main

// The az seat's front door and cost report (spec 2026-09-27-alphazero-mcts
// §1, §4). The clock lives here -- cmd/botbench may import time
// (internal/archtest) -- and reaches the seat only through azmcts.Millis;
// azmcts.Watch delivers one Diag per decision of a searched kind. Both are
// installed once before any game starts; nothing recorded here reaches a
// game, an event, a view or a replay.

import (
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// The az policy's configuration, write-once before any game starts and
// read-only afterwards (the searchKnobs pattern). azNet is the -checkpoint
// model when one was given (nil = generation 0).
var (
	azCfg        = azmcts.DefaultSeatConfig()
	azNet        *policynet.Model
	azWorldArg   string
	azKindsArg   = "priority,attackers,blockers,target"
	azFlagsGiven bool
)

// registerAZFlags defines the -az-* flags on fs, bound to azCfg.
func registerAZFlags(fs *flag.FlagSet) {
	d := azmcts.DefaultOptions()
	fs.IntVar(&azCfg.Search.Sims, "az-sims", d.Sims, "az policy: simulations per searched decision (spec default 100; 0 plays the bot)")
	fs.StringVar(&azWorldArg, "az-world", "", "az policy, required: clairvoyant (every simulation walks a clone of the REAL engine, hidden zones and future chance included -- bench and training only). sampled arrives with ticket 5")
	fs.Float64Var(&azCfg.Search.CPUCT, "az-cpuct", d.CPUCT, "az policy: PUCT exploration constant c")
	fs.Float64Var(&azCfg.Search.FPU, "az-fpu", d.FPU, "az policy: first-play urgency (an unvisited child's Q is its parent's Q minus this)")
	fs.IntVar(&azCfg.Search.Limit, "az-candidates", d.Limit, "az policy: candidates per searched decision, the bot's answer first")
	fs.IntVar(&azCfg.Search.MaxSteps, "az-max-steps", d.MaxSteps, "az policy: environment submits per simulation before the walk stops and its leaf is evaluated")
	fs.StringVar(&azKindsArg, "az-kinds", azKindsArg, "az policy: comma list of searched decision kinds (priority, attackers, blockers, target)")
}

// azFrontDoor validates the az configuration before any game starts. It is a
// no-op without an az side, except that -az-* flags then are an error. m is
// the -checkpoint model, nil when none was given. On success it stores the
// validated config and opens the clairvoyant source for this process -- the
// only azmcts.AllowClairvoyant call outside tests.
func azFrontDoor(aName, bName string, m *policynet.Model) error {
	if aName != "az" && bName != "az" {
		if azFlagsGiven {
			return fmt.Errorf("-az-* flags were given but neither side is az")
		}
		return nil
	}
	switch azWorldArg {
	case "clairvoyant":
	case "sampled":
		return fmt.Errorf("-az-world sampled is not implemented yet (ticket 5); use -az-world clairvoyant")
	case "":
		return fmt.Errorf("policy az requires -az-world clairvoyant (the only world source implemented; sampled arrives with ticket 5)")
	default:
		return fmt.Errorf("-az-world %q: want clairvoyant or sampled", azWorldArg)
	}
	kinds, err := azmcts.ParseKinds(azKindsArg)
	if err != nil {
		return fmt.Errorf("-az-kinds: %w", err)
	}
	cfg := azCfg
	cfg.Search.Kinds = kinds
	if err := cfg.Search.Validate(m); err != nil {
		return fmt.Errorf("policy az: %w", err)
	}
	azCfg, azNet = cfg, m
	azmcts.AllowClairvoyant()
	return nil
}

var azStats struct {
	mu    sync.Mutex
	diags []azmcts.Diag
}

// installAZCostStats wires the az seat's hooks to this command's collector,
// starting from an empty collection.
func installAZCostStats() {
	azStats.mu.Lock()
	azStats.diags = nil
	azStats.mu.Unlock()
	t0 := time.Now()
	azmcts.Millis = func() float64 { return float64(time.Since(t0).Microseconds()) / 1000 }
	azmcts.Watch = func(dg azmcts.Diag) {
		azStats.mu.Lock()
		azStats.diags = append(azStats.diags, dg)
		azStats.mu.Unlock()
	}
}

func azBucket(turn int32) string {
	switch {
	case turn > 12:
		return "t13+"
	case turn > 6:
		return "t07-12"
	default:
		return "t01-06"
	}
}

func azRatio(num float64, den int) float64 {
	if den == 0 {
		return 0
	}
	return num / float64(den)
}

// azCostReport renders the per-searched-decision cost and every azmcts
// counter. totalGames is the run's game count (pairs x games in matrix
// mode). Kinds and buckets print in fixed order; no map is ranged.
func azCostReport(totalGames int) string {
	azStats.mu.Lock()
	diags := append([]azmcts.Diag(nil), azStats.diags...)
	azStats.mu.Unlock()
	var b strings.Builder
	if len(diags) == 0 {
		b.WriteString("az cost report: no decisions of a searched kind (no az seat ran, or none was asked)\n")
		return b.String()
	}
	type kindAgg struct {
		asked, searched, overrides int
		ms                         []float64
	}
	kinds := []string{"priority", "attackers", "blockers", "target"}
	byKind := make(map[string]*kindAgg, len(kinds))
	for _, k := range kinds {
		byKind[k] = &kindAgg{}
	}
	buckets := []string{"t01-06", "t07-12", "t13+"}
	byBucket := make(map[string][]float64, len(buckets))
	var total azmcts.Stats
	var searchedMS []float64
	asked, searched, overrides := 0, 0, 0
	msSum := 0.0
	for _, dg := range diags {
		total.Add(dg.Stats)
		ka := byKind[dg.Kind]
		if ka == nil {
			continue // a FeedStopped record: counted in total only
		}
		asked++
		ka.asked++
		if !dg.Searched {
			continue
		}
		searched++
		ka.searched++
		if dg.Choice != 0 {
			overrides++
			ka.overrides++
		}
		ka.ms = append(ka.ms, dg.MS)
		searchedMS = append(searchedMS, dg.MS)
		msSum += dg.MS
		bk := azBucket(dg.Turn)
		byBucket[bk] = append(byBucket[bk], dg.MS)
	}
	fmt.Fprintf(&b, "az cost report: %d decisions of a searched kind over %d games, searched %d (%.1f/game), overrides %d (%.1f%% of searched)\n",
		asked, totalGames, searched, azRatio(float64(searched), totalGames), overrides, 100*azRatio(float64(overrides), searched))
	fmt.Fprintf(&b, "ms/searched decision: mean %.1f p50 %.1f p95 %.1f; ms/simulation mean %.3f; simulations/decision mean %.1f; env steps/simulation mean %.1f\n",
		meanF(searchedMS), quantF(searchedMS, .5), quantF(searchedMS, .95),
		azRatio(msSum, total.Simulations), azRatio(float64(total.Simulations), searched), azRatio(float64(total.EnvSteps), total.Simulations))
	for _, k := range kinds {
		ka := byKind[k]
		if ka.asked == 0 {
			fmt.Fprintf(&b, "  kind %s: none\n", k)
			continue
		}
		fmt.Fprintf(&b, "  kind %s: asked %d, searched %d, overrides %d, ms mean %.1f p95 %.1f\n",
			k, ka.asked, ka.searched, ka.overrides, meanF(ka.ms), quantF(ka.ms, .95))
	}
	for _, bk := range buckets {
		ms := byBucket[bk]
		if len(ms) == 0 {
			fmt.Fprintf(&b, "  %s: searched 0\n", bk)
			continue
		}
		fmt.Fprintf(&b, "  %s: searched %d, ms mean %.1f p95 %.1f\n", bk, len(ms), meanF(ms), quantF(ms, .95))
	}
	fmt.Fprintf(&b, "counters: simulations %d, completed %d, chance-failures %d, panics %d, submit-errors %d, bad-worlds %d, no-world %d, all-failed %d, step-capped %d, terminal %d, expanded %d, unavailable %d, prior-fallbacks %d, skipped %d, feed-stopped %d\n",
		total.Simulations, total.Completed, total.ChanceFailures, total.Panics, total.SubmitErrors, total.BadWorlds, total.NoWorld,
		total.AllFailed, total.StepCapped, total.Terminal, total.Expanded, total.Unavailable, total.PriorFallbacks, total.Skipped, total.FeedStopped)
	return b.String()
}
