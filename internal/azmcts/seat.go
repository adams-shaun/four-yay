package azmcts

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// SeatConfig is the az seat's configuration.
type SeatConfig struct {
	// Search are the per-decision knobs; Seed, Noise and Sample are set per
	// decision by the seat and ignored here.
	Search Options
	// Explore is generation mode (ticket 3's azgen): Dirichlet noise at every
	// searched root, and moves sampled proportional to visits while the game
	// turn is <= ExploreTurns (spec §2: turns 1-4). Eval leaves it false:
	// argmax, no noise.
	Explore      bool
	ExploreTurns int32
}

// DefaultSeatConfig is the eval seat with the spec's knobs.
func DefaultSeatConfig() SeatConfig { return SeatConfig{Search: DefaultOptions(), ExploreTurns: 4} }

// Diag is one decision of a searched kind as the cost report sees it.
type Diag struct {
	Turn       int32
	Kind       string // priority, attackers, blockers, target; "" for a FeedStopped record
	Searched   bool   // a tree was built
	Candidates int
	Choice     int
	MS         float64 // wall ms of the whole decision (Millis); 0 when untimed
	Stats      Stats
}

// Millis is the monotonic elapsed-milliseconds clock the driving command
// installs before any game starts (this package may not import time --
// internal/archtest). Nil means untimed. It never reaches an answer.
var Millis func() float64

// Watch receives one Diag per decision of a searched kind the seat answers,
// plus one FeedStopped record per decision the driver routed around the
// search. Installed once before any game starts; games run on several
// goroutines, so the consumer synchronises itself. Nil is silent.
var Watch func(Diag)

// Seat is the az policy: the default bot (seat.NewBot, the same PCG
// derivation, so its delegation consumes exactly the bot's stream) with
// Search answering the searched kinds. It is a searchseat.SearchSeat, so
// internal/bench.PlayGame hands it the live engine at its own decisions --
// the route the L10 search seat takes. Stage 1 reads only the engine; the
// driver's observation feed is for ticket 5's sampled worlds.
type Seat struct {
	def  *seat.Bot
	seed uint64
	net  *policynet.Model
	cfg  SeatConfig
}

var (
	_ seat.Seat             = (*Seat)(nil)
	_ seat.BoardSeat        = (*Seat)(nil)
	_ searchseat.SearchSeat = (*Seat)(nil)
)

// NewSeat builds one seat of one game. seed is the per-seat seed the bench
// derives; net nil is generation 0.
func NewSeat(seed uint64, net *policynet.Model, cfg SeatConfig) (*Seat, error) {
	if err := cfg.Search.Validate(net); err != nil {
		return nil, err
	}
	return &Seat{def: seat.NewBot(seed), seed: seed, net: net, cfg: cfg}, nil
}

// Decide is the plain Seat half: the wrapped bot.
func (s *Seat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.def.Decide(ctx, v, d)
}

// DecideBoard is the driver's fallback when the seat's observation feed has
// stopped: the wrapped bot, counted (FeedStopped) so it is never silent.
func (s *Seat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	if Watch != nil {
		Watch(Diag{Stats: Stats{FeedStopped: 1}})
	}
	return s.def.DecideBoard(ctx, b, d)
}

// DecideSearch answers one of the seat's own decisions: the bot's answer
// first (candidate 0 and every fallback), then Search over clairvoyant
// clones of env.Engine. A refused clairvoyant source is an error: the game
// fails loudly rather than playing an unsearched seat under the az name.
func (s *Seat) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	botIn, err := s.def.DecideBoard(ctx, env.Board, d)
	if err != nil {
		return decision.Intent{}, err
	}
	if s.cfg.Search.Sims <= 0 || env.Engine == nil {
		return botIn, nil
	}
	var t0 float64
	if Millis != nil {
		t0 = Millis()
	}
	obs := searchprobe.NewCollector(d.Player)
	src, err := NewClairvoyant(env.Engine, obs)
	if err != nil {
		return decision.Intent{}, err
	}
	opts := s.cfg.Search
	opts.Seed = DecisionSeed(s.seed, d.Seq)
	opts.Noise, opts.Sample = false, false
	if s.cfg.Explore {
		opts.Noise = true
		opts.Sample = env.Engine.G.Turn <= s.cfg.ExploreTurns
	}
	res, err := Search(Root{Engine: env.Engine, Decision: &d, Bot: botIn, Observer: obs}, src, s.net, opts)
	if err != nil {
		return decision.Intent{}, err
	}
	if res.Kind != "" && Watch != nil {
		dg := Diag{
			Turn: env.Engine.G.Turn, Kind: res.Kind, Searched: res.Stats.Searched == 1,
			Candidates: len(res.Candidates), Choice: res.Choice, Stats: res.Stats,
		}
		if Millis != nil {
			dg.MS = Millis() - t0
		}
		Watch(dg)
	}
	return res.Intent, nil
}
