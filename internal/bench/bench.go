// Package bench is the policy-agnostic, head-to-head game matrix shared by
// cmd/botbench and cmd/policytune. It owns the three pieces both callers
// need and neither should re-implement:
//
//   - the engine drive loop (PlayGame), including the two watchdogs and the
//     livelock recovery, with an optional per-decision observer so a caller
//     that wants statistics or a trace can hook it without a second copy of
//     the loop;
//   - the deterministic seed/seat/classification helpers (PlaysSeat,
//     GameSeedPair, PolsFor, Classify, FirstTurnSeat) that make a run a pure
//     function of (base seed, pairs, games) and nothing else;
//   - the pair-matrix scheduler (RunPairs), which plays every pair with its
//     own workers and folds the slots in ascending game order, so the tally
//     and the first error are identical regardless of how many workers ran.
//
// The caller supplies two SIDE seat constructors per pair (side A and side
// B); the scheduler decides which side sits in which seat each game
// (PlaysSeat), builds that game's two seats, and hands them to the caller's
// per-game player. Two different constructors therefore reach the two seats
// even when both sides are the same named policy -- the property policytune
// relies on to put w+ on one side and w- on the other.
//
// Nothing here reads a wall clock, ranges over a map whose order can reach
// its output, or uses the global math/rand; a caller's logs and reports stay
// byte-deterministic across runs.
package bench

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// SeatCtor builds the seat for one side of one game from that seat's seed.
// The seed is the engine's game seed XOR the seat index+1, so a policy's RNG
// is distinct from the engine's and from every other seat's. RunPairs applies
// the XOR when it builds the per-game seats; a caller-supplied ctor therefore
// receives the already-derived per-seat seed.
type SeatCtor func(seed uint64) seat.Seat

// Outcome is one game's result, the policy-agnostic shape both callers fold:
// the winning seat (or a draw / stall), the turn and intent counts, and the
// first-turn seat when the log carried one. Winner is deliberately absent --
// a policy name is the caller's vocabulary, not this package's; the winner's
// SEAT is what the scheduler attributes by (PlaysSeat).
type Outcome struct {
	// WinnerSeat is the seat that won; valid only when Draw is false and the
	// game is not stalled.
	WinnerSeat int
	Draw       bool
	// StallOn names the watchdog that ended the game before it finished:
	// "" for a completed game, "turns" (the -max-turns cap), "intents" (the
	// -max-intents cap), "livelock" (the engine's watcher) or "panic" (the
	// engine panicked mid-game; IsAbort covers both engine-bug kinds), or
	// whatever kind a caller's Hooks.Guard returned. A stalled game
	// is neither a win nor a draw and is excluded from every rate
	// denominator the caller computes.
	StallOn string
	// Livelock carries the diagnostic for an aborted game: the watcher's
	// cycle for a "livelock" stall, the panic value, seed, intent and stack
	// for a "panic" stall.
	Livelock string
	Turns    int32
	Intents  int
	// Starter is the seat that took the first turn (the CR 103.1 toss winner
	// resolved over the survivors); StarterSet separates a real seat 0 from
	// an outcome source that supplied no starter at all.
	Starter    int
	StarterSet bool
}

// IsStalled reports whether either watchdog (or the engine's livelock
// watcher) ended the game early.
func (o Outcome) IsStalled() bool { return o.StallOn != "" }

// IsAbort reports whether a StallOn kind is an engine-bug abort -- the
// livelock watcher or a recovered engine panic -- rather than a watchdog cap.
// Both are recorded against the game and the run continues; callers fail
// the run at the end, since either is an engine bug, not a slow game.
func IsAbort(stallOn string) bool { return stallOn == "livelock" || stallOn == "panic" }

// Hooks is the optional per-decision observer PlayGame runs. A zero Hooks
// records nothing (the fast path a fit's inner loop uses); a caller that
// wants statistics, coverage or a decision trace supplies the callbacks it
// needs. Decision is called after the seat answers and before Submit, so the
// intent it sees is the one that actually reached the engine -- a non-nil
// Decision return aborts the game at that decision (the trace-recording
// failure path); Finish once after the loop.
//
// HonestRoot, when set, routes the search branch's DecideSearch call through
// searchseat.HonestRoot: the seat answers against a redeal of the live
// position, built from its own observation feed at this boundary, instead of
// the live engine -- the hosted-bot contract (spec 2026-09-28
// hosted-bot-packages §5.3). The deal seed is bots.RootSeed over the seat's
// per-seat seed (the game seed XOR the seat index plus one) and the decision
// sequence number, so a root is a pure function of the feed and the decision
// index. A refused redeal (nil root) plays the wrapped bot at that decision
// -- the same DecideBoard fallback the feed's own failure path uses -- and,
// when RootRefused is also set, is counted. A refusal never falls back to the
// live engine: a seat that cannot see an honest world does not get the real
// one.
//
// RootRefused observes one refusal: the deciding seat's index, the decision
// and the root builder's fail-closed reason. Nil (the zero value) is fine
// when the caller does not need the count.
//
// NeedBoard forces the botpolicy.Board to be built for a View-only seat (a
// seat that is not a seat.BoardSeat) even when the decision came from the
// projected View -- the trace wants the board facts regardless. A BoardSeat
// always receives a board and never needs this.
//
// Guard, when set, runs with the live engine before every decision is
// answered. A non-empty stallOn ends the game there as a stalled Outcome
// with that StallOn kind and diag in Livelock -- a harness-side watchdog
// (cmd/cardfuzz's board-size cap) that needs the engine rather than the
// seat's board. It must only read the engine. Nil costs nothing.
type Hooks struct {
	Decision func(seatIdx int, d *decision.Decision, in decision.Intent, board *botpolicy.Board) error
	// Submit may consume an intent instead of the ordinary Engine.Submit. It
	// is used by observational drivers whose check itself commits the intent.
	Submit func(e *rules.Engine, seatIdx int, d *decision.Decision, in decision.Intent) (consumed bool, err error)
	Finish func(o Outcome)
	Guard  func(e *rules.Engine) (stallOn, diag string)
	// Setup, when non-nil, sees the engine once, right after rules.New and
	// before the first Advance: the place a collector installs an engine-side
	// observer (rules.Engine.ManaAbilityHook) that must be live from genesis.
	Setup       func(e *rules.Engine)
	NeedBoard   bool
	HonestRoot  bool
	RootRefused func(seatIdx int, d *decision.Decision, reason string)
}

// PlayGame plays one game between the given per-seat seats to completion, or
// ends it as a stall at whichever watchdog cap fires first. maxTurns and
// maxIntents are the two harness props: 0 means no cap. It is the single
// engine drive loop both cmd/botbench and cmd/policytune run; the optional
// hooks let a statistics or trace collector observe without a second copy of
// the loop.
//
// The outcome is a pure function of (cfg, seats): the seats own their RNG,
// the engine replays from cfg.Seed, and nothing reads the wall clock.
func PlayGame(cfg rules.Config, seats []seat.Seat, maxTurns, maxIntents int, hooks Hooks) (Outcome, *rules.Engine, error) {
	e := rules.New(cfg)
	if hooks.Setup != nil {
		hooks.Setup(e)
	}
	e.Advance()
	board := botpolicy.NewBoard(len(seats))
	// leanViews are the per-seat reusable Views of view.LeanReader seats;
	// rounds folds the board clock incrementally over the append-only log.
	leanViews := make([]view.View, len(seats))
	var rounds view.RoundFold
	// Search-seat feeds (the seat.BoardSeat branch, mirrored one level up):
	// an engine-aware seat needs the driver-maintained observation stream --
	// searchprobe.Collector.Capture takes the *rules.Engine and the raw event
	// burst since the previous capture and must run at EVERY decision of
	// EVERY player, while a seat is invoked only at its own (searchseat's
	// package doc carries the full argument). So PlayGame owns one Feed per
	// search seat, builds the declared public game once from the config the
	// engine itself was built from, captures through every live feed at every
	// decision, hands the deciding seat its Feed at its own decisions, and
	// records the played intent back. A game with no search seat never builds
	// a Feed and never pays a capture: its loop is byte-identical to the
	// pre-feed one, which is what the bot-vs-bot self-control run relies on.
	var feeds []*searchseat.Feed
	var setup searchprobe.PublicGame
	for i, s := range seats {
		if _, ok := s.(searchseat.SearchSeat); !ok {
			continue
		}
		if feeds == nil {
			feeds = make([]*searchseat.Feed, len(seats))
			setup = searchprobe.PublicGame{
				Names: cfg.Names, Decks: cfg.Decks, Tokens: cfg.Tokens, StartingLife: cfg.StartingLife,
			}
		}
		feeds[i] = searchseat.NewFeed(state.PlayerID(i))
	}
	n := 0
	// The livelock watcher fires inside a single Submit call -- the loop is
	// stuck there, so there is no error return to read -- and panics with a
	// *rules.LivelockError. The whole drive loop runs inside one recover: a
	// livelock converts into a stalled outcome carrying the diagnostic (one
	// hung game records a stall and the rest of the run keeps going). Any
	// other engine panic is recorded the same way as a "panic" abort with
	// its seed, intent and stack: it is a bug either way, but one bad game
	// must not kill a multi-thousand-game sweep or training run -- the
	// caller still fails the run at the end (IsAbort).
	var panicDiag string
	lle, exitOutcome, exitErr := func() (lle *rules.LivelockError, exitOutcome *Outcome, exitErr error) {
		defer func() {
			if r := recover(); r != nil {
				if l, ok := r.(*rules.LivelockError); ok {
					lle = l
					return
				}
				panicDiag = fmt.Sprintf("engine panic (seed %d, intent %d): %v\n%s", cfg.Seed, n, r, debug.Stack())
			}
		}()
		for !e.G.Over && e.Pending() != nil && (maxIntents <= 0 || n < maxIntents) {
			// The turn watchdog: a game whose turn count reaches the cap is
			// stalled -- neither a win nor a draw (and so excluded from the
			// win-rate denominator) -- and maxTurns==0 means no cap.
			if maxTurns > 0 && e.G.Turn >= int32(maxTurns) {
				return nil, &Outcome{StallOn: "turns", Turns: e.G.Turn, Intents: n}, nil
			}
			if hooks.Guard != nil {
				if kind, diag := hooks.Guard(e); kind != "" {
					return nil, &Outcome{StallOn: kind, Turns: e.G.Turn, Intents: n, Livelock: diag}, nil
				}
			}
			d := e.Pending()
			if consumer, ok := seats[d.Player].(seat.PaymentPlanConsumer); ok && consumer.WantsPaymentActions() {
				e.EnsurePaymentActions()
			}
			var in decision.Intent
			var err error
			var decisionBoard *botpolicy.Board
			// Capture through EVERY live feed at EVERY decision of EVERY
			// player -- the sampler's epoch constraints need every player's
			// bursts, exactly the teacher's loop (which captures at every
			// decision, not only the search seat's). A feed that fails a
			// capture stops observing; the game plays on.
			var fdFrame bool
			if feeds != nil {
				fd := searchFeed(feeds, d.Player)
				for _, f := range feeds {
					if f == nil {
						continue
					}
					if _, ok := f.Observe(e); ok && f == fd {
						fdFrame = true
					}
				}
			}
			if fd := searchFeed(feeds, d.Player); fd != nil {
				// The search branch: answer from the deciding seat's own
				// feed. A feed that stopped observing (a capture error) or
				// failed THIS capture plays the wrapped bot -- the teacher's
				// own contract. Honest-root mode (Hooks.HonestRoot) narrows
				// what the seat may READ: the DecideSearch call gets a redeal
				// of the live position, built from the seat's feed at this
				// boundary, instead of the live engine; a refused redeal
				// plays the wrapped bot -- never a clairvoyant fallback.
				b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
				decisionBoard = &b
				rootEngine := e
				fdLive := fdFrame
				if hooks.HonestRoot && fdLive {
					hroot, reason := searchseat.HonestRoot(setup, fd, e,
						bots.RootSeed(cfg.Seed^(uint64(d.Player)+1), d.Seq))
					if hroot != nil {
						rootEngine = hroot
					} else {
						if hooks.RootRefused != nil {
							hooks.RootRefused(int(d.Player), d, reason)
						}
						fdLive = false
					}
				}
				if fdLive {
					in, err = seats[d.Player].(searchseat.SearchSeat).DecideSearch(context.Background(),
						searchseat.Env{Setup: setup, Engine: rootEngine, Board: b, Feed: fd}, *d)
					// The played intent is recorded back into the history
					// exactly as the teacher's loop records it (the FINAL
					// intent, post-override). A translation error means the
					// history is missing an answer -- that corrupts every
					// later Sample drawn from it, so the game errors loudly
					// the way the teacher's loop errors loudly.
					if err == nil {
						err = fd.RecordAnswer(d, in)
					}
				} else {
					in, err = seats[d.Player].(searchseat.SearchSeat).DecideBoard(context.Background(), b, *d)
				}
			} else if s, ok := seats[d.Player].(seat.BoardSeat); ok {
				// Match the live host's reusable, seat-private Board path.
				// Seats opting out (including legacy) still receive the full
				// View.
				b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
				in, err = s.DecideBoard(context.Background(), b, *d)
				decisionBoard = &b
			} else if lr, ok := seats[d.Player].(view.LeanReader); ok {
				// A lean reader: its own reusable View, only the parts it reads.
				lv := &leanViews[d.Player]
				view.ProjectLeanInto(lv, e.G, e, d.Player, d, lr.ViewOmit(d))
				lv.Round = rounds.Of(e.G, e.L.Events)
				in, err = seats[d.Player].Decide(context.Background(), *lv, *d)
				if hooks.NeedBoard {
					b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
					decisionBoard = &b
				}
			} else {
				v := view.Project(e.G, e, d.Player, d)
				v.Round = rounds.Of(e.G, e.L.Events)
				in, err = seats[d.Player].Decide(context.Background(), v, *d)
				if hooks.NeedBoard {
					b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
					decisionBoard = &b
				}
			}
			if err != nil {
				return nil, nil, fmt.Errorf("seed %d, intent %d, seat %d: %w", cfg.Seed, n, d.Player, err)
			}
			if hooks.Decision != nil {
				if err := hooks.Decision(int(d.Player), d, in, decisionBoard); err != nil {
					return nil, nil, err
				}
			}
			consumed := false
			if hooks.Submit != nil {
				consumed, err = hooks.Submit(e, int(d.Player), d, in)
				if err != nil {
					return nil, nil, fmt.Errorf("seed %d, intent %d: %w", cfg.Seed, n, err)
				}
			}
			if !consumed {
				if err := e.Submit(in); err != nil {
					return nil, nil, fmt.Errorf("seed %d, intent %d: %w", cfg.Seed, n, err)
				}
			}
			n++
		}
		return nil, nil, nil
	}()
	if exitErr != nil {
		return Outcome{}, e, exitErr
	}
	finish := func(o Outcome) (Outcome, *rules.Engine, error) {
		o = recordStarter(o, e)
		if hooks.Finish != nil {
			hooks.Finish(o)
		}
		return o, e, nil
	}
	if lle != nil {
		return finish(Outcome{StallOn: "livelock", Turns: e.G.Turn, Intents: n, Livelock: lle.Error()})
	}
	if panicDiag != "" {
		return finish(Outcome{StallOn: "panic", Turns: e.G.Turn, Intents: n, Livelock: panicDiag})
	}
	if exitOutcome != nil {
		return finish(*exitOutcome)
	}
	if !e.G.Over {
		// The loop exited with the game still live: the intent cap was the
		// limiter. A stalled outcome -- NOT an error -- so the run records it
		// and steps over the pair instead of killing the whole matrix.
		return finish(Outcome{StallOn: "intents", Turns: e.G.Turn, Intents: n})
	}
	return finish(outcomeFrom(e, n))
}

// searchFeed is the deciding seat's feed, or nil when that seat is not a
// search seat (feeds is nil for a game with no search seat at all -- the
// zero-capture fast path).
func searchFeed(feeds []*searchseat.Feed, p state.PlayerID) *searchseat.Feed {
	if feeds == nil || p < 0 || int(p) >= len(feeds) {
		return nil
	}
	return feeds[p]
}

// outcomeFrom reads a finished game's result. Ruling P14: Draw must be read
// before Winner -- Winner's zero value is seat 0, a real seat, so reading it
// unconditionally would misreport a drawn game as its first seat's policy
// winning.
func outcomeFrom(e *rules.Engine, intents int) Outcome {
	var o Outcome
	o.Turns = e.G.Turn
	o.Intents = intents
	if e.G.Draw {
		o.Draw = true
	} else {
		o.WinnerSeat = int(e.G.Winner)
	}
	return recordStarter(o, e)
}

// FirstTurnSeat reads the seat the game's first TurnChange handed the turn
// to -- the CR 103.1 toss winner resolved over the survivors. The boolean is
// false when the log holds no TurnChange (the game ended during its opening
// deal).
func FirstTurnSeat(e *rules.Engine) (int, bool) {
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange {
			return int(ev.Player), true
		}
	}
	return 0, false
}

// recordStarter attaches the first-turn seat when the game reached turn 1.
// Keeping presence separate from the integer makes an omitted synthetic field
// suppress the optional split rather than inventing a seat-0 start.
func recordStarter(o Outcome, e *rules.Engine) Outcome {
	if starter, ok := FirstTurnSeat(e); ok {
		o.Starter, o.StarterSet = starter, true
	}
	return o
}

// PlaysSeat reports whether policy A (the caller's side A) holds seat s in
// game i. A holds a seat when (game+seat) is even: with two seats the
// assignment flips every game, and for any seat count a seat sees A in
// exactly half the games of an even run -- so a deck-list advantage cannot
// masquerade as a policy advantage. This is the single source of truth for
// both the seat assignment AND the win attribution, so the two cannot drift
// apart.
func PlaysSeat(game, seat int) bool { return (game+seat)%2 == 0 }

// GameSeedPair returns the seed game g of pair number pos in a matrix run at
// base baseSeed, where every pair plays exactly `games` games. Game i of a
// run plays at base+i, so a pair's games are distinct from every other
// pair's because the offset pos*games shifts the whole block; and pair 0 is
// base+0..base+games-1, byte-identical to the single-pair run of the same
// length at the same base.
func GameSeedPair(baseSeed uint64, pos, games, g int) uint64 {
	return baseSeed + uint64(pos*games+g)
}

// PolsFor returns the side name sitting at each seat of game g: B sits
// everywhere and A takes the seats PlaysSeat gives it, so seats trade sides
// every game.
func PolsFor(g, seats int, aName, bName string) []string {
	pols := make([]string, seats)
	for s := 0; s < seats; s++ {
		pols[s] = bName
		if PlaysSeat(g, s) {
			pols[s] = aName
		}
	}
	return pols
}

// PairDef is one deck pair in a matrix run: the repo decks that sit in seat
// 0 (A) and seat 1 (B) for every game of that pair.
type PairDef struct {
	A, B string
}

func (p PairDef) String() string { return p.A + ":" + p.B }

// FullPairs returns every unordered pair of names in sorted i<j order, so
// the order is a pure function of the sorted deck list and never depends on
// map iteration.
func FullPairs(names []string) []PairDef {
	ps := make([]PairDef, 0, len(names)*(len(names)-1)/2)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			ps = append(ps, PairDef{names[i], names[j]})
		}
	}
	return ps
}

// ParsePairs parses a -pairs spec: "all" for every unordered pair of the
// given (sorted) deck names, or a comma-separated list of colon-separated
// deck pairs "a:b,c:d". Deck names must exist in names. The lookup map is
// only probed by key -- never ranged over in the report path -- so iteration
// order cannot reach the output. The error strings are the shared vocabulary
// cmd/botbench and cmd/policytune both report.
func ParsePairs(spec string, names []string) ([]PairDef, error) {
	if spec == "all" {
		return FullPairs(names), nil
	}
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	var ps []PairDef
	for _, tok := range strings.Split(spec, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("-pairs: empty pair in %q", spec)
		}
		parts := strings.SplitN(tok, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("-pairs: %q is not a \"a:b\" deck pair", tok)
		}
		a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if !known[a] {
			return nil, fmt.Errorf("-pairs: unknown deck %q (not one of the %d decks in this format's pool)", a, len(names))
		}
		if !known[b] {
			return nil, fmt.Errorf("-pairs: unknown deck %q (not one of the %d decks in this format's pool)", b, len(names))
		}
		ps = append(ps, PairDef{a, b})
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("-pairs: no deck pairs given")
	}
	return ps, nil
}

// PairResult tallies one deck pair. All fields are exported so a caller can
// read them without a conversion; field order is stable and the tally never
// depends on completion order.
type PairResult struct {
	PD           PairDef
	Games        int
	AWins        int
	BWins        int
	Draws        int
	Stalls       int
	StallTurns   int
	StallIntents int
	// Livelocks counts the games the engine's own livelock watcher aborted,
	// and FirstLivelock records the first game's diagnostic so the pooled
	// report can name one concretely.
	Livelocks     int
	FirstLivelock string
	SeatWins      [2]int
	// Starts / StartWins are the play/draw tally from each game's first
	// TurnChange. A draw counts as a start with no win; a genesis stall
	// (no turn 1) counts as neither.
	Starts     [2]int
	StartWins  [2]int
	TotalTurns int64
}

// OutcomeKind is the classification of one finished game, shared by every
// fold so the tallies cannot drift apart.
type OutcomeKind struct {
	Stall      bool
	StallOn    string
	Draw       bool
	WinnerSeat int
	AWin       bool
	BWin       bool
	Starter    int
	StarterSet bool
}

// Classify splits one Outcome into the classification every fold shares. The
// game index g is needed only because attribution reads it; seats bounds the
// winner-seat check. It returns a bare out-of-range error the callers wrap
// with their own context.
func Classify(g int, oc Outcome, seats int) (OutcomeKind, error) {
	var k OutcomeKind
	if oc.StarterSet {
		if oc.Starter < 0 || oc.Starter >= seats {
			return OutcomeKind{}, fmt.Errorf("starter seat %d out of range [0,%d)", oc.Starter, seats)
		}
		k.Starter, k.StarterSet = oc.Starter, true
	}
	switch {
	case oc.IsStalled():
		k.Stall = true
		k.StallOn = oc.StallOn
	case oc.Draw:
		k.Draw = true
	default:
		if oc.WinnerSeat < 0 || oc.WinnerSeat >= seats {
			return OutcomeKind{}, fmt.Errorf("winner seat %d out of range [0,%d)", oc.WinnerSeat, seats)
		}
		k.WinnerSeat = oc.WinnerSeat
		if PlaysSeat(g, oc.WinnerSeat) {
			k.AWin = true
		} else {
			k.BWin = true
		}
	}
	return k, nil
}

// GamePool is the one shared execution budget for a matrix run. Pair workers
// submit individual jobs to it instead of creating a worker pool per pair,
// so `workers` bounds live games even when many pairs are active.
type GamePool struct {
	jobs chan func()
	wg   sync.WaitGroup
}

// NewGamePool starts a pool of `workers` goroutines (at least one).
func NewGamePool(workers int) *GamePool {
	if workers < 1 {
		workers = 1
	}
	p := &GamePool{jobs: make(chan func())}
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer p.wg.Done()
			for job := range p.jobs {
				job()
			}
		}()
	}
	return p
}

// Submit queues one job for the pool.
func (p *GamePool) Submit(job func()) { p.jobs <- job }

// Close drains the pool and waits for every worker to finish.
func (p *GamePool) Close() {
	close(p.jobs)
	p.wg.Wait()
}

// PairPlayer plays one game of a matrix pair. pos indexes into the pairs
// slice; seed is the game's seed; g is the game index within the pair; seats
// holds the two already-built seats in seat order, seat A/B placement
// decided by PlaysSeat. The scheduler did the seat construction so the
// caller cannot get the side-to-seat mapping wrong.
type PairPlayer func(pos int, seed uint64, g int, seats [2]seat.Seat) (Outcome, error)

// Progress receives live progress lines (nil is silent). It must be safe to
// call from several workers; cmd/botbench's progressWriter serialises with a
// mutex. Progress never reaches a deterministic report.
type Progress func(format string, a ...any)

// RunPairs plays every pair in `pairs` over `games` games each and returns
// the per-pair tallies in pair order. Pairs are independent games -- each is
// a distinct seed block -- so they can be played in parallel and the result
// is byte-deterministic regardless of how many workers run: each result is
// written to results[pos] by position and read back in slice order, never
// through a map. A worker that hits an error records it, closes stop, and the
// whole run returns that error.
//
// a and b are the side-A and side-B seat constructors. For game g, seat s is
// built from whichever side PlaysSeat(g, s) selects, at seed
// GameSeedPair(base, pos, games, g) ^ (s+1) -- the same per-seat derivation
// the single-pair bench uses, so a policy's RNG is distinct from the
// engine's and every other seat's.
func RunPairs(baseSeed uint64, games int, pairs []PairDef, a, b SeatCtor, play PairPlayer, workers int, prog Progress) ([]PairResult, error) {
	total := len(pairs)
	if total == 0 {
		return nil, fmt.Errorf("no deck pairs to run")
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	// TWO budgets, deliberately not the same number. `workers` is the total
	// live-game budget and is NOT clamped by the pair count -- a one-pair run
	// must still play its games across every core. `coords` is how many pair
	// COORDINATORS to spawn, and there is no use for more of those than there
	// are pairs.
	coords := workers
	if coords > total {
		coords = total
	}
	results := make([]PairResult, total)
	// Every pair coordinator submits into this one pool, so the number of
	// games actually in flight is `workers` no matter how many pairs are
	// active -- a matrix cannot multiply pair workers by game workers.
	pool := NewGamePool(workers)
	defer pool.Close()

	var (
		next int32
		mu   sync.Mutex
		fail error
		wg   sync.WaitGroup
	)
	stop := make(chan struct{})
	record := func(e error) {
		mu.Lock()
		if fail == nil {
			fail = e
			close(stop)
		}
		mu.Unlock()
	}
	report := func(format string, a ...any) {
		if prog != nil {
			prog(format, a...)
		}
	}

	wg.Add(coords)
	for w := 0; w < coords; w++ {
		go func() {
			defer wg.Done()
			for {
				pos := int(atomic.AddInt32(&next, 1)) - 1
				if pos >= total {
					return
				}
				select {
				case <-stop:
					return
				default:
				}
				report("pair %d/%d (%s): playing %d games", pos+1, total, pairs[pos], games)
				r, err := playOnePairWithPool(baseSeed, pos, games, pairs[pos], a, b, play, report, pool)
				if err != nil {
					record(err)
					return
				}
				results[pos] = r
				report("pair %d/%d (%s): done -- policy A %d/%d (%.1f%%)",
					pos+1, total, pairs[pos], r.AWins, r.Games, float64(r.AWins)/float64(r.Games)*100)
			}
		}()
	}
	wg.Wait()
	if fail != nil {
		return nil, fail
	}
	return results, nil
}

// playOnePairWithPool runs all games of one pair concurrently and folds their
// slots in ascending game order, so the tally and the first error match a
// sequential loop regardless of completion order.
func playOnePairWithPool(baseSeed uint64, pos, games int, pd PairDef, a, b SeatCtor, play PairPlayer, report Progress, pool *GamePool) (PairResult, error) {
	var r PairResult
	r.PD = pd
	r.Games = games
	step := games
	if step > 1000 {
		step = 1000
	}

	type slot struct {
		outcome Outcome
		err     error
	}
	slots := make([]slot, games)
	var done sync.WaitGroup
	done.Add(games)
	for g := 0; g < games; g++ {
		g := g
		s := GameSeedPair(baseSeed, pos, games, g)
		// Build the two seats HERE, through PlaysSeat, so the caller's
		// PairPlayer cannot mis-seat the sides: seat 0 gets side A when
		// PlaysSeat(g,0), side B otherwise, and vice versa for seat 1. The
		// per-seat seed derivation matches the single-pair bench
		// (seed ^ seat+1).
		var seats [2]seat.Seat
		for sIdx := 0; sIdx < 2; sIdx++ {
			side := b
			if PlaysSeat(g, sIdx) {
				side = a
			}
			seats[sIdx] = side(s ^ uint64(sIdx+1))
		}
		pool.Submit(func() {
			defer done.Done()
			slots[g].outcome, slots[g].err = play(pos, s, g, seats)
		})
	}
	done.Wait()

	for g, result := range slots {
		if result.err != nil {
			// All games have completed, so selecting the first error from the
			// ordered slots preserves the sequential loop's lowest failing
			// game, not whichever worker happened to finish first.
			return PairResult{}, fmt.Errorf("pair %s: %w", pd, result.err)
		}
		oc := result.outcome
		kind, err := Classify(g, oc, 2)
		if err != nil {
			return PairResult{}, fmt.Errorf("pair %s, game %d: %w", pd, g, err)
		}
		switch {
		case kind.Stall:
			r.Stalls++
			if kind.StallOn == "intents" {
				r.StallIntents++
			} else if IsAbort(kind.StallOn) {
				r.Livelocks++
				if r.FirstLivelock == "" {
					r.FirstLivelock = fmt.Sprintf("game %d: %s", g, oc.Livelock)
				}
			} else {
				r.StallTurns++
			}
		case kind.Draw:
			r.Draws++
		default:
			r.SeatWins[kind.WinnerSeat]++
			if kind.AWin {
				r.AWins++
			} else {
				r.BWins++
			}
		}
		if kind.StarterSet {
			r.Starts[kind.Starter]++
			if !kind.Stall && !kind.Draw && kind.WinnerSeat == kind.Starter {
				r.StartWins[kind.Starter]++
			}
		}
		r.TotalTurns += int64(oc.Turns)
		if g > 0 && g%step == 0 {
			if report != nil {
				report("pair %d (%s): %d/%d games", pos+1, pd, g, games)
			}
		}
	}
	return r, nil
}
