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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// The az policy's configuration, write-once before any game starts and
// read-only afterwards (the searchKnobs pattern). azNet is the -checkpoint
// model when one was given (nil = generation 0).
var (
	azCfg          = azmcts.DefaultSeatConfig()
	azNet          *policynet.Model
	azWorldArg     string
	azKindsArg     = "priority,attackers,blockers,target"
	azExploreTurns = 4
	azFlagsGiven   bool
	// M1b: -az-corpus (the visit corpus path, spellbench mode only) and
	// -az-label (the ledger display name).
	azCorpusPath string
	// azWorldPrior is -az-world prior, M1b's search-free student seat.
	azWorldPrior = "prior"
	azLabel      string
	azRecordArg  = "mz"
)

// registerAZFlags defines the -az-* flags on fs, bound to azCfg.
func registerAZFlags(fs *flag.FlagSet) {
	d := azmcts.DefaultOptions()
	fs.IntVar(&azCfg.Search.Sims, "az-sims", d.Sims, "az policy: simulations per searched decision (spec default 100; 0 plays the bot)")
	fs.StringVar(&azWorldArg, "az-world", "", "az policy, required: clairvoyant (every simulation walks a clone of the REAL engine, hidden zones and future chance included -- bench and training only), redeal (honest: every simulation walks a world that keeps what the seat can see and re-deals the hidden cards it cannot, azmcts.RedealSource; the az-redeal policy is az with -az-world redeal fixed), or prior (M1b's student seat: no simulation, play the argmax of the -checkpoint prior over the candidates the search would build; honest; no checkpoint = the bot).")
	fs.BoolVar(&azCfg.Explore, "az-explore", false, "az policy, generation only: sample moves proportional to visits on turns <= -az-explore-turns, with root Dirichlet noise unless -az-no-noise")
	fs.IntVar(&azExploreTurns, "az-explore-turns", 4, "az policy with -az-explore: the last turn whose moves are sampled")
	fs.BoolVar(&azCfg.NoNoise, "az-no-noise", false, "az policy with -az-explore: no root Dirichlet noise (the recorded visits are the search's own)")
	fs.StringVar(&azCorpusPath, "az-corpus", "", "M1b, -spellbench mode only: write every decision an az seat searched (redacted mz state, the candidates' options, visits, prior, Q, root value, outcome) as a gzip visit corpus (policynet.VisitRecord) to this new file. Observational: the games are unchanged")
	fs.StringVar(&azRecordArg, "az-corpus-features", azRecordArg, "M1b: the checkpointable feature set -az-corpus records encode under (mz or entity)")
	fs.BoolVar(&azCfg.Search.HeuristicLeaf, "az-heuristic-leaf", false, "az policy with -checkpoint: use the network only as the prior; the leaf stays the frozen heuristic")
	fs.StringVar(&azLabel, "az-label", "", "the az policy's ledger display name (default az-clairvoyant-simsN, or az-prior)")
	fs.IntVar(&azCfg.Worlds, "az-worlds", 0, "az policy, redeal world only: K distinct deals per searched decision, simulation i walking deal i mod K (0 = a fresh deal per simulation)")
	fs.Float64Var(&azCfg.Search.CPUCT, "az-cpuct", d.CPUCT, "az policy: PUCT exploration constant c")
	fs.Float64Var(&azCfg.Search.FPU, "az-fpu", d.FPU, "az policy: first-play urgency (an unvisited child's Q is its parent's Q minus this)")
	fs.IntVar(&azCfg.Search.Limit, "az-candidates", d.Limit, "az policy: candidates per searched decision, the bot's answer first")
	fs.IntVar(&azCfg.Search.MaxSteps, "az-max-steps", d.MaxSteps, "az policy: environment submits per simulation before the walk stops and its leaf is evaluated")
	fs.IntVar(&azCfg.Search.NodeCache, "az-node-cache", d.NodeCache, "az policy: tree nodes whose engine state a fixed-world search (clairvoyant) stores so a simulation resumes there instead of re-walking from the root (0 = off); the result is identical either way, and a redeal world never uses it")
	fs.StringVar(&azKindsArg, "az-kinds", azKindsArg, "az policy: comma list of searched decision kinds (priority, attackers, blockers, target)")
}

// azFrontDoor validates the az configuration before any game starts. It is a
// no-op without an az side, except that -az-* flags then are an error. m is
// the -checkpoint model, nil when none was given. On success it stores the
// validated config and opens the clairvoyant source for this process -- the
// only clairvoyant.AllowClairvoyant call outside tests.
func azFrontDoor(aName, bName string, m *policynet.Model) error {
	if !isAZPolicy(aName) && !isAZPolicy(bName) {
		if azFlagsGiven {
			return fmt.Errorf("-az-* flags were given but neither side is az")
		}
		return nil
	}
	plainAZ := aName == "az" || bName == "az"
	azCfg.ExploreTurns = int32(azExploreTurns)
	fsRec, err := policynet.ParseFeatureSet(azRecordArg)
	if err != nil || fsRec.Diagnostic() || fsRec == policynet.FeaturesV1 {
		return fmt.Errorf("-az-corpus-features %q: want mz or entity", azRecordArg)
	}
	azCfg.RecordFeatures = fsRec
	azCfg.PriorOnly = false
	switch azWorldArg {
	case azmcts.WorldClairvoyant, azmcts.WorldRedeal:
	case azWorldPrior:
		azCfg.PriorOnly = true
		if azCorpusPath != "" {
			return fmt.Errorf("-az-corpus records searched decisions; -az-world prior searches none")
		}
	case "sampled":
		return fmt.Errorf("-az-world sampled (the behaviour-consistent rejection sampler) is not implemented; use -az-world redeal (honest) or clairvoyant")
	case "":
		if plainAZ {
			return fmt.Errorf("policy az requires -az-world clairvoyant or redeal")
		}
	default:
		return fmt.Errorf("-az-world %q: want clairvoyant, redeal or sampled", azWorldArg)
	}
	if azCfg.Worlds < 0 {
		return fmt.Errorf("-az-worlds %d must be >= 0", azCfg.Worlds)
	}
	kinds, err := azmcts.ParseKinds(azKindsArg)
	if err != nil {
		return fmt.Errorf("-az-kinds: %w", err)
	}
	cfg := azCfg
	cfg.Search.Kinds = kinds
	cfg.World = azWorldArg
	if cfg.PriorOnly {
		cfg.World = "" // the student never asks for a world
	}
	if err := cfg.Search.Validate(m); err != nil {
		return fmt.Errorf("policy az: %w", err)
	}
	azCfg, azNet = cfg, m
	if plainAZ && azWorldArg == azmcts.WorldClairvoyant {
		clairvoyant.AllowClairvoyant()
	}
	return nil
}

// isAZPolicy is true for the az policies: az (world from -az-world) and
// az-redeal (the honest redeal world, whatever -az-world says).
func isAZPolicy(name string) bool { return name == "az" || name == "az-redeal" }

// azSeatConfig is the configuration the named az policy's seat runs with.
func azSeatConfig(policy string) azmcts.SeatConfig {
	cfg := azCfg
	if policy == "az-redeal" {
		// az-redeal always searches: -az-world prior is the plain az seat's.
		cfg.World, cfg.PriorOnly = azmcts.WorldRedeal, false
	}
	if cfg.World != azmcts.WorldRedeal {
		// azmcts never links the real-engine clone itself: the clairvoyant
		// source is injected here (the prior student never asks for a world,
		// but NewSeat requires the source whenever the world is not the
		// honest redeal one). Never set for az-redeal.
		cfg.Source = clairvoyant.Source
	}
	return cfg
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
	fmt.Fprintf(&b, "counters: simulations %d, completed %d, chance-failures %d, panics %d, submit-errors %d, bad-worlds %d, no-world %d, all-failed %d, step-capped %d, terminal %d, expanded %d, unavailable %d, prior-fallbacks %d, skipped %d, feed-stopped %d, redeal-refused %d\n",
		total.Simulations, total.Completed, total.ChanceFailures, total.Panics, total.SubmitErrors, total.BadWorlds, total.NoWorld,
		total.AllFailed, total.StepCapped, total.Terminal, total.Expanded, total.Unavailable, total.PriorFallbacks, total.Skipped, total.FeedStopped, total.RedealRefused)
	if total.RedealRefused > 0 || total.NoWorld > 0 {
		var reasons []string
		count := make(map[string]int)
		for _, dg := range diags {
			for _, r := range []string{dg.Refused, dg.DealFailed} {
				if r == "" {
					continue
				}
				if r == dg.DealFailed {
					r = "deal failed: " + r
				}
				if count[r] == 0 {
					reasons = append(reasons, r)
				}
				count[r]++
			}
		}
		sort.Strings(reasons)
		for _, r := range reasons {
			fmt.Fprintf(&b, "  redeal %d: %s\n", count[r], r)
		}
	}
	b.WriteString("searched by kind:")
	for k, name := range azmcts.KindNames {
		sep := ","
		if k == 0 {
			sep = ""
		}
		fmt.Fprintf(&b, "%s %s %d", sep, name, total.KindSearched[k])
	}
	b.WriteString("\n")
	for k, name := range azmcts.KindNames {
		fmt.Fprintf(&b, "  skipped %s: %s\n", name, azReasons(total.KindSkipped[k]))
	}
	for bk, name := range azmcts.BaseKindNames {
		fmt.Fprintf(&b, "  skipped priority, bot answered %s: %s\n", name, azReasons(total.PrioritySkipped[bk]))
	}
	return b.String()
}

// azReasons renders one row of skip counts in azmcts.SkipReasonNames order.
func azReasons(row [azmcts.NumSkipReasons]int) string {
	parts := make([]string, len(row))
	for r, n := range row {
		parts[r] = fmt.Sprintf("%s %d", azmcts.SkipReasonNames[r], n)
	}
	return strings.Join(parts, ", ")
}
