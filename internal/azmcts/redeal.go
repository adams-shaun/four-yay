package azmcts

import (
	"errors"
	"fmt"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
)

// The world sources a seat can search with (SeatConfig.World).
const (
	// WorldClairvoyant clones the REAL engine: bench and training only, and
	// only through a source injected as SeatConfig.Source
	// (internal/azmcts/clairvoyant, refused unless that package's
	// AllowClairvoyant was called). azmcts never links the clone itself.
	WorldClairvoyant = "clairvoyant"
	// WorldRedeal is the honest source (NewRedeal): every world keeps what
	// the searching seat can see and re-deals everything it cannot.
	WorldRedeal = "redeal"
)

// RedealInput is everything the honest source reads, all of it the
// searching seat's own knowledge at one decision boundary:
//
//   - Setup: the declared game -- the deck lists the seat believes each
//     player holds (SpellBench's rotating pools are deck mirrors, so the
//     opponent's list is the seat's own; any explicit list may be given) and
//     the token definitions;
//   - History: the seat's observation stream through this decision (a
//     searchseat.Feed's History), whose last frame is Base.Engine's current
//     boundary;
//   - Known: History's known-card projection (searchprobe.ProjectKnownCards,
//     or KnownTracker's incremental fold of the same frames);
//   - Base: the engine the decision was asked of and the seat's feed
//     collector. The engine is read for public state, zone SIZES and the
//     hidden objects to permute -- never for which hidden card is where.
type RedealInput struct {
	Setup   searchprobe.PublicGame
	History searchprobe.History
	Known   searchprobe.KnownCards
	Base    searchprobe.RedealBase
}

// RedealSource is the honest WorldSource (SpellBench M1; AZ spec §1's
// sampled source without the behaviour-consistent rejection sampler). Every
// world is a searchprobe.Redealer deal: public state, zone sizes and every
// card the seat knows (its own hand, anything the known-card projection
// pins) stay; each player's remaining hidden cards -- the seat's own library
// ORDER, the opponent's hand and library -- are dealt uniformly from the pool
// the seat can derive (its list minus every card seen outside the hidden
// zones minus every known hidden card); future chance is re-seeded. What a
// world is dealt is a function of the seat's observation and the seed alone
// (TestRedealWorldsIgnoreTheRealHiddenCards).
//
// Face-down and other identity-hidden PUBLIC objects are not re-dealt: the
// redeal only moves cards between hands and libraries, and a pool that the
// deck list cannot account for (a face-down card's identity, a card owned by
// a list the setup does not declare) refuses rather than leak -- the refusal
// is counted in Stats.RedealRefused and every simulation then reports
// ErrNoWorld, so the bot's answer is played.
//
// Worlds: with K <= 0 (or K >= the simulation count) every simulation gets
// its own deal. With 0 < K the source deals K worlds, lazily, and simulation
// i walks a hypothetical clone of world i mod K with its own future chance:
// fewer determinizations, more simulations each (the strategy-fusion knob).
type RedealSource struct {
	in       RedealInput
	prepared bool
	r        *searchprobe.Redealer
	refused  string
	failed   string // the first Deal failure, "" when every deal landed
	obs      *searchprobe.Collector
	forks    *searchprobe.Forker // every simulation's observer (World)
	seed     uint64
	k        int
	worlds   []*rules.Engine
	prev     *rules.Engine
	spare    rules.Spare
}

// NewRedeal prepares the honest source for one decision. obs must be the
// FRESH collector Search is given as Root.Observer (every world's observer
// is a clone of it, taken lazily after Search observed the root); seed is the
// per-decision seed (DecisionSeed); worlds is K (see RedealSource). A
// refused redeal is not an error here: the source reports it through
// Refused and every World call. The redeal is prepared at the first World
// call, so a decision Search does not search costs nothing.
func NewRedeal(in RedealInput, obs *searchprobe.Collector, seed uint64, worlds int) (*RedealSource, error) {
	if obs == nil {
		return nil, errors.New("azmcts: the redeal source needs the root observer")
	}
	return &RedealSource{in: in, obs: obs, seed: seed, k: worlds}, nil
}

func (s *RedealSource) prepare() {
	if s.prepared {
		return
	}
	s.prepared = true
	s.r, s.refused = searchprobe.NewRedealer(s.in.Setup, s.in.History, s.in.Known, s.in.Base)
}

// Refused is the reason the redeal could not be prepared at this boundary,
// or "". It prepares the redeal if no World call has yet.
func (s *RedealSource) Refused() string {
	s.prepare()
	return s.refused
}

// DealFailed is the first reason a prepared redeal failed to deal a world
// at this decision (each such simulation reported ErrNoWorld), or "".
func (s *RedealSource) DealFailed() string { return s.failed }

func (s *RedealSource) dealFailed(reason string) (World, error) {
	if s.failed == "" {
		s.failed = reason
	}
	return World{}, fmt.Errorf("%w: %s", ErrNoWorld, reason)
}

// RedealSeed is world i's deal seed under the per-decision seed: two
// SplitMix64 words, independent of everything but (seed, i).
func RedealSeed(seed uint64, i int) [2]uint64 {
	a := splitmix(seed ^ 0x7265_6465_616c_2d77 ^ splitmix(uint64(i)+1))
	return [2]uint64{a, splitmix(a ^ 0x9e3779b97f4a7c15)}
}

func (s *RedealSource) World(sim int) (World, error) {
	s.prepare()
	if s.refused != "" {
		return World{}, fmt.Errorf("%w: redeal refused: %s", ErrNoWorld, s.refused)
	}
	// The previous simulation's world is spent when the next is asked for
	// (simulations run one at a time), so its arrays build this one.
	if s.prev != nil {
		s.spare = s.prev.Release()
		s.prev = nil
	}
	var w *rules.Engine
	if s.k <= 0 {
		var reason string
		if w, reason = s.r.Deal(RedealSeed(s.seed, sim), &s.spare); reason != "" {
			return s.dealFailed(reason)
		}
	} else {
		i := sim % s.k
		for len(s.worlds) <= i {
			s.worlds = append(s.worlds, nil)
		}
		if s.worlds[i] == nil {
			base, reason := s.r.Deal(RedealSeed(s.seed, i), nil)
			if reason != "" {
				return s.dealFailed(reason)
			}
			s.worlds[i] = base
		}
		// A fresh chance stream per simulation; the deal is world i's.
		w = s.worlds[i].CloneHypotheticalInto(splitmix(s.seed^splitmix(uint64(sim)+0x51)), &s.spare)
	}
	s.prev = w
	// The previous simulation's observer is spent too: one fork, rolled back
	// to the root observer's boundary, serves every simulation.
	if s.forks == nil {
		s.forks = s.obs.Forker()
	}
	return World{Engine: w, Observer: s.forks.Fork(), Hypothetical: true}, nil
}

// reclaim ends the source's use once the Search that asked it for worlds
// has returned: it releases the last world it handed out and returns the
// recycled storage (rules.Spare), so the seat's next decision's source
// starts from it instead of allocating its first world's arrays again.
func (s *RedealSource) reclaim() rules.Spare {
	if s.prev != nil {
		s.spare = s.prev.Release()
		s.prev = nil
	}
	sp := s.spare
	s.spare = rules.Spare{}
	return sp
}

// KnownTracker is the feed-owned incremental known-card projection
// (searchseat.KnownTracker): it lives there because the Feed owns and rebuilds
// it (Feed.Known). The alias keeps this package's callers and tests on the
// same name without a second implementation that could drift.
type KnownTracker = searchseat.KnownTracker
