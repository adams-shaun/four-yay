package main

import (
	"context"
	"fmt"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// searchOutcome is one timed search at one root.
type searchOutcome struct {
	Searched bool    // a tree was built / the teacher was asked
	Work     int     // simulations (az) or rollouts (sampler)
	Extra    int     // az: environment submits; sampler: submits
	MS       float64 // wall ms of the whole decision
	CPUMS    float64 // process CPU ms of the whole decision
}

// searchFn searches one root decision of seat 0 from its observation feed.
// It must not change e.
type searchFn func(env searchseat.Env, d decision.Decision) (searchOutcome, error)

// azSearch is set by az.go (build tag enginebench_az); nil means azmcts is
// not linked into this build (it does not exist at a4af596).
var azSearch func(seed uint64, sims int) searchFn

// samplerSearch is the pre-tree search seat docs/015 described ("sampled
// worlds, each played to the end by its bot; no tree"): searchseat.SearchBot
// at its measured Defaults (8 worlds, 6 candidates, attackers + cast).
func samplerSearch(seed uint64) searchFn {
	bot := searchseat.NewSearchBot(seed, searchseat.Defaults())
	return func(env searchseat.Env, d decision.Decision) (searchOutcome, error) {
		var got *searchseat.Diag
		searchseat.Watch = func(dg searchseat.Diag) { got = &dg }
		t0, c0 := time.Now(), cpuNow()
		_, err := bot.DecideSearch(context.Background(), env, d)
		ms, cms := float64(time.Since(t0).Nanoseconds())/1e6, (cpuNow()-c0)*1e3
		searchseat.Watch = nil
		if err != nil || got == nil || !got.Trace.Covered {
			return searchOutcome{MS: ms, CPUMS: cms}, err
		}
		return searchOutcome{Searched: true, Work: got.Trace.Rollouts, Extra: got.Trace.Submits, MS: ms, CPUMS: cms}, nil
	}
}

type searchDetail struct {
	Mode          string
	Sims          int
	Asked, Roots  int
	Work, Extra   int
	SearchedMS    float64
	SearchedCPUMS float64
	AllMS         float64
	RootTurns     map[int32]int
	GamesStarted  int
	RootsPerGame  int
	FeedDeadGames int
}

// maxRootsPerGame bounds how many seat-0 decisions of turns 5-8 one game
// contributes, so the roots spread over several games.
const maxRootsPerGame = 6

// runSearch plays FDN pairs A and B alternately (seats swapping as ever)
// with the heuristic bot on both seats, keeps seat 0's observation feed, and
// at seat 0's decisions of turns 5-8 times one search; the bot's answer is
// what is played, so every search mode sees the same roots. The rate is
// work / wall time over the decisions a search actually ran.
func runSearch(r *result, w workload, base uint64, secs float64, mode string, sims int) error {
	var mk func(seed uint64) searchFn
	switch mode {
	case "az":
		if azSearch == nil {
			return fmt.Errorf("unavailable: internal/azmcts is not linked (build with -tags enginebench_az at a commit that has it)")
		}
		mk = func(seed uint64) searchFn { return azSearch(seed, sims) }
		r.Row = fmt.Sprintf("az%d", sims)
	case "sampler":
		mk = samplerSearch
	}
	det := searchDetail{Mode: mode, Sims: sims, RootTurns: map[int32]int{}, RootsPerGame: maxRootsPerGame}
	c0 := cpuNow()
	for g := 0; cpuNow()-c0 < secs && g < 400; g++ {
		pair := "A"
		if (g/2)%2 == 1 {
			pair = "B"
		}
		decks, names, err := w.decks(pair)
		if err != nil {
			return err
		}
		cfg := w.config(decks, names, base, g)
		det.GamesStarted++
		if err := searchGame(cfg, mk(cfg.Seed^1), &det); err != nil {
			return fmt.Errorf("game %d: %w", g, err)
		}
	}
	r.Secs, r.CPU = det.SearchedMS/1000, det.SearchedCPUMS/1000
	r.Roots = det.Roots
	r.Sims = det.Work
	if det.SearchedMS > 0 {
		r.SimsPS = float64(det.Work) / (det.SearchedMS / 1000)
		r.MSPerRt = det.SearchedMS / float64(det.Roots)
	}
	if det.SearchedCPUMS > 0 {
		r.SimsPSC = float64(det.Work) / (det.SearchedCPUMS / 1000)
	}
	r.Detail = det
	printQuietStats()
	return nil
}

func searchGame(cfg rules.Config, search searchFn, det *searchDetail) error {
	e := rules.New(cfg)
	e.Advance()
	bots := []*seat.Bot{seat.NewBot(cfg.Seed ^ 1), seat.NewBot(cfg.Seed ^ 2)}
	feed := searchseat.NewFeed(0)
	setup := searchprobe.PublicGame{Names: cfg.Names, Decks: cfg.Decks, Tokens: cfg.Tokens, StartingLife: cfg.StartingLife}
	board := botpolicy.NewBoard(2)
	roots := 0
	for !e.G.Over && e.Pending() != nil && e.G.Turn <= rootTurns[len(rootTurns)-1] && roots < maxRootsPerGame {
		d := e.Pending()
		_, fdOK := feed.Observe(e)
		if d.Player == 0 && fdOK && e.G.Turn >= rootTurns[0] {
			b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
			out, err := search(searchseat.Env{Setup: setup, Engine: e, Board: b, Feed: feed}, *d)
			if err != nil {
				return err
			}
			det.Asked++
			det.AllMS += out.MS
			if out.Searched {
				det.Roots++
				roots++
				det.Work += out.Work
				det.Extra += out.Extra
				det.SearchedMS += out.MS
				det.SearchedCPUMS += out.CPUMS
				det.RootTurns[e.G.Turn]++
			}
		}
		b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
		in, err := bots[d.Player].DecideBoard(context.Background(), b, *d)
		if err != nil {
			return err
		}
		if d.Player == 0 && fdOK {
			if err := feed.RecordAnswer(d, in); err != nil {
				return err
			}
		}
		if err := e.Submit(in); err != nil {
			return err
		}
	}
	if !feed.Live() {
		det.FeedDeadGames++
	}
	return nil
}
