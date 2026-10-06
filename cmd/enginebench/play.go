package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// Game caps. docs/015 reports gorge's random games at 36-43 turns; a game
// that reaches a cap is counted as a stall, and its turns still count.
const (
	maxTurns   = 300
	maxIntents = 200000
	// warmGames are played untimed before the clock starts.
	warmGames = 2
	// retries is how many uniform draws a decision gets before the random
	// player falls back to the heuristic bot's answer for it (counted).
	retries = 32
)

// syncAnswer installs the resolution kernel's synchronous answerer on the
// random and bot rows (GORGE_TAPE_SYNC=1, or baked in with
// -ldflags "-X main.syncAnswerBuild=1"). With the kernel off it changes
// nothing.
var syncAnswer = os.Getenv("GORGE_TAPE_SYNC") == "1" || syncAnswerBuild == "1"

// syncAnswerBuild and sparePool sit beside the row helpers: sparePool
// recycles finished games' storage (rules.Spare) between the games a row
// plays back to back, so the random and bot rows measure reuse the way
// production self-play does.
var syncAnswerBuild string

var sparePool bench.SparePool

type randomStats struct {
	Games, Turns, Decisions, Rejected, Fallbacks, Stalls int
	StallKinds                                           map[string]int
	// Panics names the first maxPanics recovered panics by seed and turn, so
	// a stall of kind "panic" can be replayed (playRandom with that seed).
	Panics []string `json:",omitempty"`
}

const maxPanics = 8

// randomIntent draws uniformly: a choice count uniform in [Min, Max] (capped
// at the option count), then that many distinct options uniformly, in random
// order. At a priority decision (Min = Max = 1) that is uniform over the
// offered options, a mana ability's activation included: gorge's random
// player taps lands one at a time (docs/015 §6). The "concede" option every
// priority decision carries is never drawn (a uniform player would concede
// on turn 1 in most games).
func randomIntent(d *decision.Decision, r *rand.Rand) decision.Intent {
	pool := make([]int, 0, len(d.Options))
	for i := range d.Options {
		if d.Options[i].Kind != "concede" {
			pool = append(pool, i)
		}
	}
	n := len(pool)
	lo, hi := d.Min, d.Max
	if hi > n {
		hi = n
	}
	if lo < 0 {
		lo = 0
	}
	if lo > hi {
		lo = hi
	}
	k := lo + r.IntN(hi-lo+1)
	var ch []int
	if k > 0 {
		perm := r.Perm(n)[:k]
		ch = make([]int, k)
		for i, j := range perm {
			ch[i] = pool[j]
		}
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
}

// playRandom plays one uniform-random game. A submit the engine rejects is
// redrawn (Submit preserves the pending decision on a rejection); after
// `retries` rejections the decision is answered by the heuristic bot.
func playRandom(cfg rules.Config, st *randomStats) {
	playRandomWithPool(cfg, st, &sparePool)
}

func playRandomWithPool(cfg rules.Config, st *randomStats, pool *bench.SparePool) {
	r := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x5bd1e9955bd1e995))
	// The finished game's log and object arrays back the next game this run
	// plays (rules.Spare; reuse never changes a game). The pooled spare is
	// returned only after the stats below read the finished engine.
	var spare *rules.Spare
	if pool != nil {
		spare = pool.Get()
		cfg.Spare = spare
	}
	e := rules.New(cfg)
	var fb [2]*seat.Bot
	board := botpolicy.NewBoard(2)
	stall := ""
	func() {
		defer func() {
			if x := recover(); x != nil {
				stall = "panic"
				if len(st.Panics) < maxPanics {
					st.Panics = append(st.Panics, fmt.Sprintf("seed=%d turn=%d: %v", cfg.Seed, e.G.Turn, x))
				}
			}
		}()
		if syncAnswer {
			// Answer a converted mid-resolution ask inline with exactly the
			// draws the loop below would make for it.
			tries := 0
			rules.SetTapeAnswerer(e, func(d *decision.Decision, reject error) (decision.Intent, bool) {
				if reject == nil {
					tries = 0
					st.Decisions++
				} else {
					st.Rejected++
					tries++
				}
				if tries < retries {
					return randomIntent(d, r), true
				}
				if tries > retries {
					return decision.Intent{}, false
				}
				p := int(d.Player)
				if fb[p] == nil {
					fb[p] = seat.NewBot(cfg.Seed ^ uint64(p+1))
				}
				b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
				in, err := fb[p].DecideBoard(context.Background(), b, *d)
				st.Fallbacks++
				return in, err == nil
			})
		}
		e.Advance()
		n := 0
		for !e.G.Over && e.Pending() != nil {
			if e.G.Turn >= maxTurns {
				stall = "turns"
				return
			}
			if n >= maxIntents {
				stall = "intents"
				return
			}
			d := e.Pending()
			ok := false
			for try := 0; try < retries; try++ {
				if err := e.Submit(randomIntent(d, r)); err == nil {
					ok = true
					break
				}
				st.Rejected++
			}
			if !ok {
				p := int(d.Player)
				if fb[p] == nil {
					fb[p] = seat.NewBot(cfg.Seed ^ uint64(p+1))
				}
				b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
				in, err := fb[p].DecideBoard(context.Background(), b, *d)
				if err != nil || e.Submit(in) != nil {
					stall = "stuck"
					return
				}
				st.Fallbacks++
			}
			st.Decisions++
			n++
		}
	}()
	st.Games++
	st.Turns += int(e.G.Turn)
	if stall != "" {
		st.Stalls++
		if st.StallKinds == nil {
			st.StallKinds = map[string]int{}
		}
		st.StallKinds[stall]++
	}
	// A clean engine is at its last use here (nothing reads e.L or e.G past
	// this function); a panicked one is dropped rather than recycled, so a
	// suspect log never backs a later game.
	if stall != "panic" && pool != nil {
		pool.Put(spare, e)
	}
}

func runRandom(r *result, w workload, pair string, base uint64, secs float64) error {
	decks, names, err := w.decks(pair)
	if err != nil {
		return err
	}
	var warm randomStats
	for g := 0; g < warmGames; g++ {
		playRandom(w.config(decks, names, base, g), &warm)
	}
	var st randomStats
	t0, c0 := time.Now(), cpuNow()
	for g := 0; cpuNow()-c0 < secs; g++ {
		playRandom(w.config(decks, names, base, g), &st)
	}
	el, cpu := time.Since(t0).Seconds(), cpuNow()-c0
	fill(r, el, cpu, st.Games, st.Turns, st.Decisions, st.Stalls)
	r.Detail = st
	return nil
}

type botStats struct {
	Games, Turns, Intents, Stalls int
	Wins                          [2]int
	StallKinds                    map[string]int
}

func playBot(cfg rules.Config, autopay bool, st *botStats) error {
	return playBotWithPool(cfg, autopay, st, &sparePool)
}

func playBotWithPool(cfg rules.Config, autopay bool, st *botStats, pool *bench.SparePool) error {
	seats := make([]seat.Seat, 2)
	for i := range seats {
		b := seat.NewBot(cfg.Seed ^ uint64(i+1))
		if autopay {
			b = b.EnableAutoPayMana()
		}
		seats[i] = b
	}
	var o bench.Outcome
	var err error
	if pool == nil {
		o, _, err = bench.PlayGame(cfg, seats, maxTurns, maxIntents, bench.Hooks{SyncAnswer: syncAnswer})
	} else {
		o, err = pool.PlayGameRecycled(cfg, seats, maxTurns, maxIntents, bench.Hooks{SyncAnswer: syncAnswer}, nil)
	}
	if err != nil {
		return err
	}
	st.Games++
	st.Turns += int(o.Turns)
	st.Intents += o.Intents
	if o.IsStalled() {
		st.Stalls++
		if st.StallKinds == nil {
			st.StallKinds = map[string]int{}
		}
		st.StallKinds[o.StallOn]++
	} else if !o.Draw {
		st.Wins[o.WinnerSeat]++
	}
	return nil
}

func runBot(r *result, w workload, pair string, base uint64, secs float64, autopay bool) error {
	decks, names, err := w.decks(pair)
	if err != nil {
		return err
	}
	if autopay {
		r.Row = "bot-autopay"
	}
	var warm botStats
	for g := 0; g < warmGames; g++ {
		if err := playBot(w.config(decks, names, base, g), autopay, &warm); err != nil {
			return err
		}
	}
	var st botStats
	t0, c0 := time.Now(), cpuNow()
	for g := 0; cpuNow()-c0 < secs; g++ {
		if err := playBot(w.config(decks, names, base, g), autopay, &st); err != nil {
			return fmt.Errorf("game %d: %w", g, err)
		}
	}
	el, cpu := time.Since(t0).Seconds(), cpuNow()-c0
	fill(r, el, cpu, st.Games, st.Turns, st.Intents, st.Stalls)
	r.Detail = st
	return nil
}

// fill records a game-playing row. The rates are per CPU second (see
// cpuNow); wall seconds are recorded beside them.
func fill(r *result, el, cpu float64, games, turns, decisions, stalls int) {
	r.Secs, r.CPU = el, cpu
	el = cpu
	r.Games, r.Turns, r.Decisions, r.Stalls = games, turns, decisions, stalls
	r.TurnsPS = float64(turns) / el
	r.GamesPS = float64(games) / el
	r.DecPS = float64(decisions) / el
}
