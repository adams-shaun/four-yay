package searchbench

import (
	"context"
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

type SearchArm string

const (
	ArmClairvoyant SearchArm = "clairvoyant-mcts"
	ArmPIMC1       SearchArm = "pimc-1"
	ArmPIMC4       SearchArm = "pimc-4"
	ArmISMCTS      SearchArm = "is-mcts"
)

type NativeSearchResult struct {
	Arm        SearchArm
	Intent     decision.Intent
	Choice     int
	Candidates []decision.Intent
	Keys       []azmcts.Key
	Visits     []int
	Stats      azmcts.Stats
}

type rootWorldSource struct {
	engines   []*rules.Engine
	observers []*searchprobe.Collector
}

func newRootWorldSource(worlds []ReplayedRoot) (*rootWorldSource, error) {
	if len(worlds) == 0 {
		return nil, fmt.Errorf("searchbench: empty world pool")
	}
	out := &rootWorldSource{engines: make([]*rules.Engine, len(worlds)), observers: make([]*searchprobe.Collector, len(worlds))}
	for i := range worlds {
		if worlds[i].Engine == nil || worlds[i].Decision == nil {
			return nil, fmt.Errorf("searchbench: nil world %d", i)
		}
		observer := searchprobe.NewCollector(worlds[i].Decision.Player)
		if _, err := observer.Capture(worlds[i].Engine, nil); err != nil {
			return nil, fmt.Errorf("searchbench: capture world %d: %w", i, err)
		}
		out.engines[i], out.observers[i] = worlds[i].Engine, observer
	}
	return out, nil
}

func (s *rootWorldSource) World(sim int) (azmcts.World, error) {
	if s == nil || len(s.engines) == 0 {
		return azmcts.World{}, fmt.Errorf("searchbench: empty root world source")
	}
	i := sim % len(s.engines)
	return azmcts.World{Engine: s.engines[i].Clone(), Observer: s.observers[i].Clone(), Hypothetical: true}, nil
}

// RunNativeSearch runs one of the four Draft Zero benchmark methods. Worlds
// must be reconstructed and validated against root before calling it.
func RunNativeSearch(root ReplayedRoot, worlds []ReplayedRoot, arm SearchArm, opts azmcts.Options, seed uint64) (NativeSearchResult, error) {
	if root.Engine == nil || root.Decision == nil {
		return NativeSearchResult{}, fmt.Errorf("searchbench: nil native root")
	}
	if len(worlds) < 1 {
		return NativeSearchResult{}, fmt.Errorf("searchbench: %s needs at least one world", arm)
	}
	observer := searchprobe.NewCollector(root.Decision.Player)
	if _, err := observer.Capture(root.Engine, nil); err != nil {
		return NativeSearchResult{}, fmt.Errorf("searchbench: capture root: %w", err)
	}
	v := view.Project(root.Engine.G, root.Engine, root.Decision.Player, root.Decision)
	bot, err := seat.NewBot(seed).Decide(context.Background(), v, *root.Decision)
	if err != nil {
		return NativeSearchResult{}, err
	}
	opts.Seed = azmcts.DecisionSeed(seed, root.Decision.Seq)
	opts.AutoPayment = true
	search := func(source azmcts.WorldSource, local azmcts.Options) (azmcts.Result, error) {
		return azmcts.Search(context.Background(), azmcts.Root{Engine: root.Engine, Decision: root.Decision, Bot: bot, Observer: observer}, source, nil, local)
	}
	pack := func(result azmcts.Result) NativeSearchResult {
		return NativeSearchResult{Arm: arm, Intent: result.Intent, Choice: result.Choice, Candidates: result.Candidates, Keys: result.Keys, Visits: result.Visits, Stats: result.Stats}
	}
	switch arm {
	case ArmClairvoyant:
		clairvoyant.AllowClairvoyant()
		src, err := clairvoyant.NewClairvoyant(root.Engine, observer)
		if err != nil {
			return NativeSearchResult{}, err
		}
		result, err := search(src, opts)
		return pack(result), err
	case ArmPIMC1, ArmISMCTS:
		pool := worlds
		if arm == ArmPIMC1 {
			pool = worlds[:1]
		}
		src, err := newRootWorldSource(pool)
		if err != nil {
			return NativeSearchResult{}, err
		}
		result, err := search(src, opts)
		return pack(result), err
	case ArmPIMC4:
		if len(worlds) < 4 {
			return NativeSearchResult{}, fmt.Errorf("searchbench: pimc-4 needs four worlds, got %d", len(worlds))
		}
		return runPIMC4(search, worlds[:4], opts, pack)
	default:
		return NativeSearchResult{}, fmt.Errorf("searchbench: unknown search arm %q", arm)
	}
}

func runPIMC4(search func(azmcts.WorldSource, azmcts.Options) (azmcts.Result, error), worlds []ReplayedRoot, opts azmcts.Options, pack func(azmcts.Result) NativeSearchResult) (NativeSearchResult, error) {
	var merged NativeSearchResult
	byKey := make(map[azmcts.Key]int)
	for i := range worlds {
		src, err := newRootWorldSource(worlds[i : i+1])
		if err != nil {
			return NativeSearchResult{}, err
		}
		local := opts
		local.Sims = opts.Sims / len(worlds)
		if i < opts.Sims%len(worlds) {
			local.Sims++
		}
		result, err := search(src, local)
		if err != nil {
			return NativeSearchResult{}, err
		}
		if i == 0 {
			merged = pack(result)
			merged.Arm = ArmPIMC4
			merged.Visits = make([]int, len(result.Visits))
			// The first result supplies the shared root vocabulary only. All
			// counters below are accumulated across every independent tree.
			merged.Stats = azmcts.Stats{}
			for j, key := range result.Keys {
				byKey[key] = j
			}
		}
		for j, key := range result.Keys {
			at, ok := byKey[key]
			if !ok {
				return NativeSearchResult{}, fmt.Errorf("searchbench: pimc-4 root vocabulary differs in world %d", i)
			}
			merged.Visits[at] += result.Visits[j]
		}
		merged.Stats.Add(result.Stats)
	}
	best := 0
	for i := 1; i < len(merged.Visits); i++ {
		if merged.Visits[i] > merged.Visits[best] {
			best = i
		}
	}
	merged.Choice = best
	if best > 0 && best < len(merged.Candidates) {
		merged.Intent = merged.Candidates[best]
	}
	return merged, nil
}
