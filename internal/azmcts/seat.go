package azmcts

import (
	"context"
	"fmt"

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
	// NoNoise drops the root Dirichlet noise from Explore: moves are still
	// sampled proportional to visits on turns <= ExploreTurns, but the
	// recorded visit distribution is the search's own (M1b distillation: at a
	// small budget the root noise would be a large share of every target).
	NoNoise bool
	// PriorOnly is the STUDENT seat (M1b): no simulation at all; at every
	// decision Search would search, play the argmax of the network prior
	// over the same candidates (ties on the lowest index, the bot's answer).
	// It reads only the seat's own decision and redacted view, never a
	// world, so it is honest. With no network the prior is uniform and the
	// seat is exactly the bot it wraps (the same-seed control).
	PriorOnly bool
	// RecordFeatures is the feature set SetRecorder's records encode under.
	RecordFeatures policynet.FeatureSet
	// World is the world source: WorldClairvoyant ("" is clairvoyant, the
	// stage-1 default) or WorldRedeal, the honest source.
	World string
	// Worlds is the redeal source's K (RedealSource): 0 deals a fresh world
	// per simulation. Ignored by the clairvoyant source.
	Worlds int
}

// DefaultSeatConfig is the eval seat with the spec's knobs.
func DefaultSeatConfig() SeatConfig {
	return SeatConfig{Search: DefaultOptions(), ExploreTurns: 4, RecordFeatures: policynet.FeaturesMZ}
}

// Diag is one decision of a searched kind as the cost report sees it.
type Diag struct {
	Turn       int32
	Kind       string // priority, attackers, blockers, target; "" for a FeedStopped record
	Searched   bool   // a tree was built
	Candidates int
	Choice     int
	MS         float64 // wall ms of the whole decision (Millis); 0 when untimed
	Stats      Stats
	// Refused is the redeal source's refusal reason at this decision, "" when
	// it prepared (or the source is clairvoyant); DealFailed its first
	// per-world deal failure (those simulations counted in Stats.NoWorld).
	Refused, DealFailed string
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
	// record, when set (SetRecorder), receives one visit-corpus record per
	// decision this seat searched to completion. nil records nothing.
	record func(policynet.VisitRecord)
	// known is the redeal source's incremental known-card projection over
	// this game's feed.
	known KnownTracker
}

// SetRecorder installs the visit-corpus recorder (M1b; cmd/botbench
// -az-corpus). The records carry the searched decision (redacted state,
// the candidates' options, visits, prior, Q, root value, the played
// candidate) and the diagnostic opponent-hand rows; the driver fills the
// game fields and the outcome. world is the world source's name. Recording
// only reads: the seat's answers are unchanged.
func (s *Seat) SetRecorder(fn func(policynet.VisitRecord)) {
	s.record = fn
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
	switch cfg.World {
	case "", WorldClairvoyant, WorldRedeal:
	default:
		return nil, fmt.Errorf("azmcts: world source %q: want %s or %s", cfg.World, WorldClairvoyant, WorldRedeal)
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
// first (candidate 0 and every fallback), then Search over the configured
// worlds: clairvoyant clones of env.Engine, or honest redeals built from the
// driver's feed (SeatConfig.World). A refused clairvoyant source is an
// error: the game fails loudly rather than playing an unsearched seat under
// the az name. A refused redeal is counted (Stats.RedealRefused) and plays
// the bot's answer -- it never falls back to the clairvoyant source.
func (s *Seat) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	botIn, err := s.def.DecideBoard(ctx, env.Board, d)
	if err != nil {
		return decision.Intent{}, err
	}
	if s.cfg.PriorOnly && env.Engine != nil {
		return s.decidePrior(env, d, botIn)
	}
	if s.cfg.Search.Sims <= 0 || env.Engine == nil {
		return botIn, nil
	}
	var t0 float64
	if Millis != nil {
		t0 = Millis()
	}
	obs := searchprobe.NewCollector(d.Player)
	opts := s.cfg.Search
	opts.Seed = DecisionSeed(s.seed, d.Seq)
	var src WorldSource
	var redeal *RedealSource
	if s.cfg.World == WorldRedeal {
		rs, err := s.redealSource(env, obs, opts.Seed)
		if err != nil {
			return decision.Intent{}, err
		}
		src, redeal = rs, rs
	} else {
		cs, err := NewClairvoyant(env.Engine, obs)
		if err != nil {
			return decision.Intent{}, err
		}
		src = cs
	}
	opts.Noise, opts.Sample = false, false
	if s.cfg.Explore {
		opts.Noise = !s.cfg.NoNoise
		opts.Sample = env.Engine.G.Turn <= s.cfg.ExploreTurns
	}
	res, err := Search(Root{Engine: env.Engine, Decision: &d, Bot: botIn, Observer: obs}, src, s.net, opts)
	if err != nil {
		return decision.Intent{}, err
	}
	refused, dealFailed := "", ""
	if redeal != nil && res.Stats.Searched == 1 {
		if refused = redeal.Refused(); refused != "" {
			res.Stats.RedealRefused = 1
		}
		dealFailed = redeal.DealFailed()
	}
	if s.record != nil && res.Stats.Searched == 1 && res.Stats.AllFailed == 0 && refused == "" {
		s.record(visitRecord(env.Engine, &d, res, s.cfg.RecordFeatures, s.worldName(), opts.Sims))
	}
	if res.Kind != "" && Watch != nil {
		dg := Diag{
			Turn: env.Engine.G.Turn, Kind: res.Kind, Searched: res.Stats.Searched == 1,
			Candidates: len(res.Candidates), Choice: res.Choice, Stats: res.Stats, Refused: refused, DealFailed: dealFailed,
		}
		if Millis != nil {
			dg.MS = Millis() - t0
		}
		Watch(dg)
	}
	return res.Intent, nil
}

// decidePrior is the PriorOnly seat's decision: the candidates Search would
// build, answered by the argmax of the network prior (ties on the lowest
// index). A decision Search would not search is the bot's.
func (s *Seat) decidePrior(env searchseat.Env, d decision.Decision, botIn decision.Intent) (decision.Intent, error) {
	obs := searchprobe.NewCollector(d.Player)
	opts := s.cfg.Search
	opts.Sims, opts.Noise, opts.Sample = 0, false, false
	opts.Seed = DecisionSeed(s.seed, d.Seq)
	res, err := Search(Root{Engine: env.Engine, Decision: &d, Bot: botIn, Observer: obs}, nil, s.net, opts)
	if err != nil {
		return decision.Intent{}, err
	}
	if len(res.Candidates) < 2 {
		return botIn, nil
	}
	best := 0
	for i, p := range res.Prior {
		if p > res.Prior[best] {
			best = i
		}
	}
	if Watch != nil {
		Watch(Diag{Turn: env.Engine.G.Turn, Kind: res.Kind, Candidates: len(res.Candidates), Choice: best, Stats: res.Stats})
	}
	if best == 0 {
		return botIn, nil
	}
	return res.Candidates[best], nil
}

// redealSource builds the honest source at one of the seat's decisions from
// the driver's feed: the seat's History, its known-card projection (folded
// incrementally across the game) and the feed's collector. A feed that has
// no frames, or a known-card fold that failed, is a refusal -- counted, and
// the bot's answer is played -- never a clairvoyant fallback.
func (s *Seat) redealSource(env searchseat.Env, obs *searchprobe.Collector, seed uint64) (*RedealSource, error) {
	in := RedealInput{Setup: env.Setup}
	if env.Feed != nil && env.Feed.Live() && env.Feed.Frames() > 0 {
		in.History = env.Feed.History()
		in.Base = searchprobe.RedealBase{Engine: env.Engine, Observer: env.Feed.Collector()}
		known, err := s.known.Update(in.History)
		if err != nil {
			// A dead projection: the redealer refuses without a base.
			in.Base = searchprobe.RedealBase{}
		}
		in.Known = known
	}
	return NewRedeal(in, obs, seed, s.cfg.Worlds)
}

// worldName is the record's world source name.
func (s *Seat) worldName() string {
	if s.cfg.World == "" {
		return WorldClairvoyant
	}
	return s.cfg.World
}
