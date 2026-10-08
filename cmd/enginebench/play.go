package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
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

// intentScratch is the random client's per-game reusable buffers, so a draw
// allocates nothing in steady state (action-walk W3). pool and perm are
// overwritten every draw; ch is built fresh only when the answer is retained.
type intentScratch struct {
	pool []int
	perm []int
}

// permInto fills s.perm[:n] with the identity and shuffles it in place with
// the SAME inside-out choice sequence math/rand/v2.(*Rand).Perm uses
// (Shuffle's Fisher-Yates, j = IntN(i+1)), so the RNG stream -- and therefore
// every game -- is unchanged. TestPermIntoMatchesPerm pins it.
func (s *intentScratch) permInto(n int, r *rand.Rand) []int {
	if cap(s.perm) < n {
		s.perm = make([]int, n)
	}
	s.perm = s.perm[:n]
	for i := range s.perm {
		s.perm[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		s.perm[i], s.perm[j] = s.perm[j], s.perm[i]
	}
	return s.perm
}

// randomIntent draws uniformly: a choice count uniform in [Min, Max] (capped
// at the option count), then that many distinct options uniformly, in random
// order. At a priority decision (Min = Max = 1) that is uniform over the
// offered options, a mana ability's activation included: gorge's random
// player taps lands one at a time (docs/015 §6). The "concede" option every
// priority decision carries is never drawn (a uniform player would concede
// on turn 1 in most games).
//
// Every draw goes through s, so the only allocation left is the returned
// Choices slice (the engine may retain it).
func randomIntent(d *decision.Decision, r *rand.Rand, s *intentScratch) decision.Intent {
	pool := s.pool[:0]
	for i := range d.Options {
		if d.Options[i].Kind != "concede" {
			pool = append(pool, i)
		}
	}
	s.pool = pool
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
		perm := s.permInto(n, r)
		ch = make([]int, 0, k)
		// Options sharing a non-empty Group are mutually exclusive up to the
		// decision's cap (a blocker blocks one attacker): walk the whole
		// permutation and skip an option whose Group is full, so a uniform
		// draw never submits an answer Decision.Validate rejects. Without
		// groups this takes exactly perm[:k], as before.
		var used map[string]int
		for _, j := range perm {
			if len(ch) == k {
				break
			}
			o := pool[j]
			if g := d.Options[o].Group; g != "" {
				if used == nil {
					used = map[string]int{}
				}
				if !d.GroupAdmits(used, g) {
					continue
				}
				used[g]++
			}
			ch = append(ch, o)
		}
	}
	if d.Kind == decision.KBlockers {
		ch = fitBlockBounds(d, ch, 0)
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
}

// fitBlockBounds drops every chosen blocker of an attacker whose blocker
// count falls outside its published MinBlockers/MaxBlockers bounds, and every
// lone blocker of menace (the attacker the engine named in a rejection;
// 0 = none), so the random player's block declaration is legal without the
// client knowing combat rules. Dropping a block is always a legal
// direction: Blockers asks carry Min 0.
func fitBlockBounds(d *decision.Decision, ch []int, menace state.ObjID) []int {
	if len(ch) == 0 {
		return ch
	}
	counts := map[state.ObjID]int{}
	for _, c := range ch {
		counts[d.Options[c].Attacker]++
	}
	out := ch[:0]
	for _, c := range ch {
		o := d.Options[c]
		n := counts[o.Attacker]
		if n < o.MinBlockers || (o.MaxBlockers > 0 && n > o.MaxBlockers) || (o.Attacker == menace && n < 2) {
			continue
		}
		out = append(out, c)
	}
	return out
}

var menaceReject = regexp.MustCompile(`attacker (\d+) with menace`)

// menaceRepair turns a blockers answer the engine rejected for menace into
// the same answer without that attacker's lone blocker.
func menaceRepair(d *decision.Decision, in decision.Intent, err error) (decision.Intent, bool) {
	if d.Kind != decision.KBlockers || err == nil {
		return in, false
	}
	m := menaceReject.FindStringSubmatch(err.Error())
	if m == nil {
		return in, false
	}
	id, _ := strconv.Atoi(m[1])
	in.Choices = fitBlockBounds(d, append([]int(nil), in.Choices...), state.ObjID(id))
	return in, true
}

// playRandom plays one uniform-random game. A submit the engine rejects is
// redrawn (Submit preserves the pending decision on a rejection); after
// `retries` rejections the decision is answered by the heuristic bot.
func playRandom(cfg rules.Config, st *randomStats) {
	playRandomWithPool(cfg, st, &sparePool)
}

func playRandomWithPool(cfg rules.Config, st *randomStats, pool *bench.SparePool) {
	r := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x5bd1e9955bd1e995))
	// W3: the random client's reusable draw buffers, per game.
	var scratch intentScratch
	// The finished game's log and object arrays back the next game this run
	// plays (rules.Spare; reuse never changes a game). The pooled spare is
	// returned only after the stats below read the finished engine.
	var spare *rules.Spare
	if pool != nil {
		spare = pool.Get()
		cfg.Spare = spare
	}
	e := rules.New(cfg)
	if walkStatsFlag {
		wm.reset()
	}
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
					return randomIntent(d, r, &scratch), true
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
			if walkStatsFlag {
				wm.observe(e, d)
			}
			ok := false
			for try := 0; try < retries; try++ {
				in := randomIntent(d, r, &scratch)
				err := e.Submit(in)
				if fixed, ok := menaceRepair(d, in, err); ok {
					err = e.Submit(fixed)
				}
				if err == nil {
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
	hooks := bench.Hooks{SyncAnswer: syncAnswer}
	if walkStatsFlag {
		// A Decision hook disables the kernel's synchronous answerer; it does
		// not change priority asks. It is how the observer sees each posed
		// decision on the bot row (spec §9.1).
		var eng *rules.Engine
		hooks.SyncAnswer = false
		hooks.Setup = func(e *rules.Engine) { eng = e; wm.reset() }
		hooks.Decision = func(_ int, d *decision.Decision, _ decision.Intent, _ *botpolicy.Board) error {
			wm.observe(eng, d)
			return nil
		}
	}
	// The finished game's log and object arrays back the next game this run
	// plays (rules.Spare; reuse never changes a game). Nothing reads the
	// engine after the hooks above, so a clean game is recycled as the last
	// use; the walkStats observer runs during the game (Setup/Decision), not
	// after it.
	var o bench.Outcome
	var err error
	if pool == nil {
		o, _, err = bench.PlayGame(cfg, seats, maxTurns, maxIntents, hooks)
	} else {
		o, err = pool.PlayGameRecycled(cfg, seats, maxTurns, maxIntents, hooks, nil)
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
