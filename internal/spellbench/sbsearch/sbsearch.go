// Package sbsearch is sb-search: determinized search over honest redealt
// worlds with sb-tactical as both the candidate prior and the rollout
// policy.
//
// At a priority decision where sb-tactical has a real choice, the seat takes
// sb-tactical's own pick plus the next best candidates by its score (TopK in
// all, pass always among them). For each of W worlds -- a
// searchprobe.Redealer deal from the seat's own observation stream: public
// state, zone sizes and every card the seat knows stay, the opponent's hand
// and library and the seat's own library order are dealt from what the seat
// can derive (the SpellBench mirror's decklist minus everything seen), future
// chance is re-seeded -- every candidate is applied (forced through the
// rollout seat's own sb-tactical machinery, so a pursuit or a lowered payment
// plays exactly as sb-tactical would play it) and the game is rolled forward
// with sb-tactical seats on BOTH sides to game end, or to Horizon turns and
// the frozen material leaf (searchprobe.LeafValue). Every candidate of a
// world shares that world's deal, chance stream and rollout seeds (common
// random numbers), so the comparison is paired. The candidate with the best
// mean wins if it beats sb-tactical's own pick by more than Margin; a tie, a
// world that fails for any candidate (dropped from every mean), no valid
// world, or any error at all plays sb-tactical's pick. With Attack, a
// KAttackers decision is searched the same way over sb-tactical's own
// declaration, no attack, and the alpha strike.
//
// Honesty: nothing here reads the real engine's hidden state. The
// Redealer's inputs are the seat's feed (its History and known-card
// projection) and the declared game (the decklists -- in SpellBench's mirror
// pool the opponent's list is the seat's own, "visible" per the spec); the
// real engine is read for public state and zone sizes only (see
// searchprobe.RedealBase).
//
// Determinism: every world, chance stream and rollout seat is seeded from
// the seat's seed and the decision's sequence number (azmcts.DecisionSeed);
// candidates, worlds and seats are iterated in slice order. The package does
// not read a clock: Millis, installed by the driving command, only times
// decisions for Watch.
package sbsearch

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// Config is sb-search's budget and selection.
type Config struct {
	// Worlds is W, the redealt worlds per searched decision. 0 is plain
	// sb-tactical (decision-identical: no world is dealt).
	Worlds int
	// TopK is the candidate count: sb-tactical's pick plus the next best by
	// its score, pass added when it is not among them.
	TopK int
	// Horizon is the rollout length in engine turns after the root's turn;
	// 0 rolls to game end. A rollout cut short is scored with
	// searchprobe.LeafValue on the searching seat's view.
	Horizon int32
	// MaxSteps caps the submits of one rollout (then the leaf is scored).
	MaxSteps int
	// Margin is how much a candidate's mean value must beat sb-tactical's
	// own pick's before it is played instead.
	Margin float64
	// Attack searches KAttackers decisions too.
	Attack bool
	// Block searches KBlockers decisions too: sb-tactical's declaration,
	// no block, and the default gorge bot's declaration.
	Block bool
}

// DefaultConfig is sb-search's registered configuration.
func DefaultConfig() Config {
	return Config{Worlds: 16, TopK: 3, Horizon: 0, MaxSteps: 4000, Margin: 0.05}
}

// Diag is one searched decision as a cost report sees it.
type Diag struct {
	Turn       int32
	Kind       string // "priority" or "attackers"
	Candidates int
	Worlds     int // worlds every candidate completed
	Failed     int // worlds dropped (deal refused or a rollout failed)
	Refused    string
	Override   bool // a candidate other than sb-tactical's pick was played
	Rollouts   int
	// Gap is sb-tactical's score margin of its pick over the next searched
	// candidate (priority only; 0 for attackers).
	Gap float64
	MS  float64 // wall ms of the whole decision; 0 when untimed
}

// Millis is the monotonic elapsed-milliseconds clock the driving command
// installs before any game starts (this package may not import time). Nil
// is untimed. It never reaches an answer.
var Millis func() float64

// Watch receives one Diag per searched decision. Installed once before any
// game starts; games run on several goroutines, so the consumer
// synchronises itself. Nil is silent.
var Watch func(Diag)

// Seat is one sb-search seat of one game. It wraps an sb-tactical
// builtins.Seat (UnwrapSeat exposes it, so the driver's planner hand-off,
// refused-answer retry and stats collection reach it) and is a
// searchseat.SearchSeat, so internal/bench.PlayGame hands it the engine and
// its observation feed at its own decisions.
type Seat struct {
	inner *builtins.Seat
	seed  uint64
	cfg   Config
	known azmcts.KnownTracker
	cache *builtins.ProfileCache
	// spareBase recycles each spent world's arrays into the next deal,
	// spareCand each spent rollout's into the next candidate clone.
	spareBase, spareCand rules.Spare
	// failRollout forces every rollout to fail (tests: the fallback path).
	failRollout bool
	// prop are the next searched decision's extra roots (Propose).
	prop Proposals
	// ProposalRoots counts proposed roots searched; ProposalWins those the
	// search played.
	ProposalRoots, ProposalWins int
}

// Proposals are one decision's extra root candidates from another policy
// of a collection (spec D§8, arbitrated per decision instead of routed per
// deck): priority plays named by key, whole attack or block answers. A
// proposal is searched exactly like sb-tactical's own candidates and played
// only when it beats sb-tactical's pick by the margin.
type Proposals struct {
	Priority []builtins.PriorityKey
	Answers  []decision.Intent
}

// Propose sets the extra roots of the next decision searched through
// DecideSearch or DecideWorlds; they are dropped after it either way.
func (s *Seat) Propose(p Proposals) { s.prop = p }

var (
	_ seat.Seat                           = (*Seat)(nil)
	_ searchseat.SearchSeat               = (*Seat)(nil)
	_ seat.PaymentPlanConsumer            = (*Seat)(nil)
	_ interface{ UnwrapSeat() seat.Seat } = (*Seat)(nil)
)

// New wraps inner (an sb-tactical seat) for one game. seed is the seat's
// per-game seed.
func New(inner *builtins.Seat, seed uint64, cfg Config) *Seat {
	if cfg.TopK < 2 {
		cfg.TopK = 2
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 4000
	}
	return &Seat{inner: inner, seed: seed, cfg: cfg, cache: builtins.NewProfileCache()}
}

// UnwrapSeat exposes the sb-tactical seat underneath.
func (s *Seat) UnwrapSeat() seat.Seat { return s.inner }

// WantsPaymentActions is the inner seat's (AutoPay: yes).
func (s *Seat) WantsPaymentActions() bool { return s.inner.WantsPaymentActions() }

// Decide is the plain-seat path (no engine, no feed): sb-tactical.
func (s *Seat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.inner.Decide(ctx, v, d)
}

// DecideBoard is the driver's path once the seat's observation feed has
// stopped: sb-tactical on the view of the engine it was handed as its
// planner (cmd/botbench hands every builtin the live engine), else the
// default bot on the board.
func (s *Seat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	if e, ok := s.inner.Planner().(*rules.Engine); ok && e != nil {
		pd := e.Pending()
		if pd == nil || pd.Seq != d.Seq {
			pd = &d
		}
		v := view.Project(e.G, e, d.Player, pd)
		v.Round = view.RoundOf(e.G, e.L.Events)
		return s.inner.Decide(ctx, v, d)
	}
	in := botpolicy.Decide(b, &d, rand.New(rand.NewPCG(s.seed, d.Seq)))
	in.Seq, in.Player = d.Seq, d.Player
	return in, nil
}

// DecideSearch answers one of the seat's own decisions (package doc).
func (s *Seat) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	e := env.Engine
	if e == nil {
		return s.DecideBoard(ctx, env.Board, d)
	}
	return s.decideOn(ctx, e, func() (dealer, string) {
		rd, reason := s.redealer(env)
		if reason != "" {
			return nil, reason
		}
		return redealDealer{s: s, rd: rd}, ""
	}, d)
}

// Dealer deals world w of a searched decision from its two seeds
// (azmcts.RedealSeed of the decision seed): an engine positioned at the
// root decision whose hidden cards the searching seat cannot see are
// redealt. A non-empty reason refuses the world (it is dropped from every
// mean, exactly like a refused redeal).
type Dealer func(w int, seed [2]uint64) (*rules.Engine, string)

// DecideWorlds is DecideSearch with an explicit world source instead of the
// live observation feed: e is the engine at the decision (for a SpellBench
// v2 seat, the shadow rebuilt from its observation, internal/spellbench/
// v2shadow) and deal redeals its hidden zones. Everything else -- the
// candidates, the rollouts, the margin -- is sb-search's own.
func (s *Seat) DecideWorlds(ctx context.Context, e *rules.Engine, deal Dealer, d decision.Decision) (decision.Intent, error) {
	return s.decideOn(ctx, e, func() (dealer, string) { return funcDealer(deal), "" }, d)
}

// dealer is a world source: the live feed's redeal or a caller's Dealer.
type dealer interface {
	deal(w int, dseed uint64) (*rules.Engine, string)
	release(e *rules.Engine)
}

type redealDealer struct {
	s  *Seat
	rd *searchprobe.Redealer
}

func (r redealDealer) deal(w int, dseed uint64) (*rules.Engine, string) {
	return r.rd.Deal(azmcts.RedealSeed(dseed, w), &r.s.spareBase)
}

func (r redealDealer) release(e *rules.Engine) { r.s.spareBase = e.Release() }

type funcDealer Dealer

func (f funcDealer) deal(w int, dseed uint64) (*rules.Engine, string) {
	return f(w, azmcts.RedealSeed(dseed, w))
}

func (f funcDealer) release(*rules.Engine) {}

// decideOn is the search at e's decision d; worlds come from mk, called
// only when a search runs.
func (s *Seat) decideOn(ctx context.Context, e *rules.Engine, mk func() (dealer, string), d decision.Decision) (decision.Intent, error) {
	defer func() { s.prop = Proposals{} }()
	// The view sb-tactical is shown in plain play (bench.PlayGame's
	// View-seat branch builds exactly this), projected with d itself: a
	// caller may hand a reduced copy of the pending decision (DecideWorlds),
	// and sb-tactical indexes the view's decision by d's option positions.
	v := view.Project(e.G, e, d.Player, &d)
	v.Round = view.RoundOf(e.G, e.L.Events)
	if s.cfg.Worlds <= 0 || len(e.G.Players) != 2 {
		return s.inner.Decide(ctx, v, d)
	}
	switch {
	case d.Kind == decision.KPriority:
		return s.priority(ctx, e, mk, v, d)
	case d.Kind == decision.KAttackers && s.cfg.Attack:
		return s.attackers(ctx, e, mk, v, d)
	case d.Kind == decision.KBlockers && s.cfg.Block:
		return s.blockers(ctx, e, mk, v, d)
	}
	return s.inner.Decide(ctx, v, d)
}

// root is one candidate as the rollout applies it at the root decision:
// a forced priority pick, or a whole answer.
type root struct {
	key *builtins.PriorityKey
	in  decision.Intent
}

func (s *Seat) priority(ctx context.Context, e *rules.Engine, mk func() (dealer, string), v view.View, d decision.Decision) (decision.Intent, error) {
	cands, best, ok := s.inner.TacticalPriority(v, d)
	if !ok || cands[best].Key.IsLand() {
		// No choice to search, or a land drop (sb-tactical always makes it
		// first; the next priority decision searches the spells).
		return s.inner.Decide(ctx, v, d)
	}
	roots, gap := pickRoots(cands, best, s.cfg.TopK)
	own := len(roots)
	for _, k := range s.prop.Priority {
		dup, offered := false, false
		for _, r := range roots {
			dup = dup || *r.key == k
		}
		for _, c := range cands {
			offered = offered || c.Key == k
		}
		if !dup && offered {
			kk := k
			roots = append(roots, root{key: &kk})
			s.ProposalRoots++
		}
	}
	if len(roots) < 2 {
		return s.inner.Decide(ctx, v, d)
	}
	choice := s.search(e, mk, d, "priority", roots, gap)
	if choice > 0 {
		if choice >= own {
			s.ProposalWins++
		}
		s.inner.ForcePriority(*roots[choice].key)
	}
	return s.inner.Decide(ctx, v, d)
}

// addProposed appends the proposed whole answers that validate and are not
// already roots; it returns how many roots were there before.
func (s *Seat) addProposed(d decision.Decision, roots []root) ([]root, int) {
	own := len(roots)
	for _, in := range s.prop.Answers {
		in.Seq, in.Player = d.Seq, d.Player
		if d.Validate(in) != nil {
			continue
		}
		dup := false
		for _, r := range roots {
			dup = dup || builtins.SameChoices(r.in, in)
		}
		if !dup {
			roots = append(roots, root{in: in})
			s.ProposalRoots++
		}
	}
	return roots, own
}

// pickRoots is sb-tactical's pick first, then the rest by score (ties to
// the earlier candidate) up to k, then pass if it is not among them.
func pickRoots(cands []builtins.ScoredCandidate, best, k int) (roots []root, gap float64) {
	order := []int{best}
	used := make([]bool, len(cands))
	used[best] = true
	for len(order) < k {
		j := -1
		for i := range cands {
			if !used[i] && (j < 0 || cands[i].Score > cands[j].Score) {
				j = i
			}
		}
		if j < 0 {
			break
		}
		used[j] = true
		order = append(order, j)
	}
	for i := range cands {
		if !used[i] && cands[i].Key.IsPass() {
			order = append(order, i)
			break
		}
	}
	roots = make([]root, len(order))
	for i, j := range order {
		k := cands[j].Key
		roots[i] = root{key: &k}
	}
	if len(order) > 1 {
		gap = cands[best].Score - cands[order[1]].Score
	}
	return roots, gap
}

func (s *Seat) attackers(ctx context.Context, e *rules.Engine, mk func() (dealer, string), v view.View, d decision.Decision) (decision.Intent, error) {
	own, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return own, err
	}
	roots := []root{{in: own}}
	for _, alt := range builtins.AttackAlternatives(v, d) {
		if !builtins.SameChoices(alt, own) {
			roots = append(roots, root{in: alt})
		}
	}
	roots, first := s.addProposed(d, roots)
	if len(roots) < 2 {
		return own, nil
	}
	choice := s.search(e, mk, d, "attackers", roots, 0)
	if choice >= first && first < len(roots) {
		s.ProposalWins++
	}
	return roots[choice].in, nil
}

// blockers searches a KBlockers decision over sb-tactical's declaration, no
// block, and the default gorge bot's declaration (each a valid whole
// answer; duplicates dropped).
func (s *Seat) blockers(ctx context.Context, e *rules.Engine, mk func() (dealer, string), v view.View, d decision.Decision) (decision.Intent, error) {
	own, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return own, err
	}
	roots := []root{{in: own}}
	add := func(in decision.Intent) {
		in.Seq, in.Player = d.Seq, d.Player
		if d.Validate(in) != nil {
			return
		}
		for _, r := range roots {
			if builtins.SameChoices(r.in, in) {
				return
			}
		}
		roots = append(roots, root{in: in})
	}
	add(botpolicy.Clamp(&d, decision.Intent{Seq: d.Seq, Player: d.Player}))
	add(botpolicy.Decide(seat.BoardFromView(v), &d, rand.New(rand.NewPCG(s.seed, d.Seq^0xb10c))))
	roots, first := s.addProposed(d, roots)
	if len(roots) < 2 {
		return own, nil
	}
	choice := s.search(e, mk, d, "blockers", roots, 0)
	if choice >= first && first < len(roots) {
		s.ProposalWins++
	}
	return roots[choice].in, nil
}

// search values every root over the configured worlds and returns the
// index to play: 0 (sb-tactical's own answer) on any failure or when no
// other candidate beats it by the margin.
func (s *Seat) search(e *rules.Engine, mk func() (dealer, string), d decision.Decision, kind string, roots []root, gap float64) (choice int) {
	var t0 float64
	if Millis != nil {
		t0 = Millis()
	}
	dg := Diag{Turn: e.G.Turn, Kind: kind, Candidates: len(roots), Gap: gap}
	defer func() {
		if p := recover(); p != nil {
			// Never lose an action to the search: sb-tactical's pick.
			choice = 0
			dg.Refused = fmt.Sprintf("panic: %v", p)
			dg.Override = false
		}
		if Watch != nil {
			if Millis != nil {
				dg.MS = Millis() - t0
			}
			Watch(dg)
		}
	}()
	means, valid, failed, rollouts, refused := s.evaluate(e, mk, d, roots)
	dg.Worlds, dg.Failed, dg.Rollouts, dg.Refused = valid, failed, rollouts, refused
	if valid == 0 {
		return 0
	}
	best := 0
	for i := 1; i < len(means); i++ {
		if means[i] > means[best] {
			best = i
		}
	}
	if best != 0 && means[best]-means[0] > s.cfg.Margin {
		dg.Override = true
		return best
	}
	return 0
}

// evaluate plays every root on every world and returns each root's mean
// value over the worlds every root completed.
func (s *Seat) evaluate(e *rules.Engine, mk func() (dealer, string), d decision.Decision, roots []root) (means []float64, valid, failed, rollouts int, refused string) {
	rd, reason := mk()
	if reason != "" {
		return nil, 0, s.cfg.Worlds, 0, reason
	}
	dseed := azmcts.DecisionSeed(s.seed, d.Seq)
	sums := make([]float64, len(roots))
	vals := make([]float64, len(roots))
	rootTurn := e.G.Turn
	for w := 0; w < s.cfg.Worlds; w++ {
		base, why := rd.deal(w, dseed)
		if why != "" {
			failed++
			if refused == "" {
				refused = "deal: " + why
			}
			continue
		}
		chance := mix(dseed ^ 0x6368616e6365 ^ mix(uint64(w)+1))
		seedA, seedB := mix(chance^0x5eed_a), mix(chance^0x5eed_b)
		ok := true
		for c := range roots {
			wc := base.CloneHypotheticalInto(chance, &s.spareCand)
			val, err := s.rollout(wc, d, rootTurn, roots[c], seedA, seedB)
			rollouts++
			s.spareCand = wc.Release()
			if err != nil {
				ok = false
				if refused == "" {
					refused = "rollout: " + err.Error()
				}
				break
			}
			vals[c] = val
		}
		rd.release(base)
		if !ok {
			failed++
			continue
		}
		valid++
		for c := range roots {
			sums[c] += vals[c]
		}
	}
	if valid == 0 {
		return nil, 0, failed, rollouts, refused
	}
	means = make([]float64, len(roots))
	for c := range sums {
		means[c] = sums[c] / float64(valid)
	}
	return means, valid, failed, rollouts, refused
}

// redealer prepares the honest redeal at this decision from the driver's
// feed (azmcts.Seat.redealSource's inputs). A non-empty reason is a
// refusal: sb-tactical's pick is played.
func (s *Seat) redealer(env searchseat.Env) (*searchprobe.Redealer, string) {
	if env.Feed == nil || !env.Feed.Live() || env.Feed.Frames() == 0 {
		return nil, "no live observation feed"
	}
	h := env.Feed.History()
	known, err := s.known.Update(h)
	if err != nil {
		return nil, "known-card projection: " + err.Error()
	}
	return searchprobe.NewRedealer(env.Setup, h, known, searchprobe.RedealBase{Engine: env.Engine, Observer: env.Feed.Collector()})
}

var errRollout = errors.New("sbsearch: rollout failed")

// rollout applies r at w's root decision and plays sb-tactical on both
// sides until the game ends, the horizon passes or the step cap is spent;
// the value is the searching seat's (1 win, 0 loss, 0.5 draw, else the
// material leaf). Any panic is an error.
func (s *Seat) rollout(w *rules.Engine, d decision.Decision, rootTurn int32, r root, seedA, seedB uint64) (val float64, err error) {
	defer func() {
		if p := recover(); p != nil {
			val, err = 0, fmt.Errorf("%w: panic: %v", errRollout, p)
		}
	}()
	if s.failRollout {
		return 0, fmt.Errorf("%w: forced", errRollout)
	}
	actor := d.Player
	var seats [2]*builtins.Seat
	seats[actor] = s.inner.RolloutClone(seedA, s.cache, true)
	seats[1-actor] = s.inner.RolloutClone(seedB, s.cache, false)
	for _, st := range seats {
		st.SetPlanner(w)
	}
	ctx := context.Background()
	for steps := 0; ; steps++ {
		g := w.G
		if g.Over {
			switch {
			case g.Draw:
				return 0.5, nil
			case g.Winner == actor:
				return 1, nil
			}
			return 0, nil
		}
		pd := w.Pending()
		if pd == nil {
			return 0, fmt.Errorf("%w: no pending decision", errRollout)
		}
		if steps > 0 && ((s.cfg.Horizon > 0 && g.Turn > rootTurn+s.cfg.Horizon) || steps >= s.cfg.MaxSteps) {
			return searchprobe.LeafValue(view.Project(g, w, actor, pd), actor), nil
		}
		if int(pd.Player) >= len(seats) {
			return 0, fmt.Errorf("%w: player %d", errRollout, pd.Player)
		}
		w.EnsurePaymentActions()
		st := seats[pd.Player]
		v := view.Project(g, w, pd.Player, pd)
		var in decision.Intent
		if steps == 0 {
			if pd.Seq != d.Seq || pd.Player != actor || pd.Kind != d.Kind {
				return 0, fmt.Errorf("%w: the world is not at the root decision", errRollout)
			}
			if r.key == nil {
				in = r.in
				if err := pd.Validate(in); err != nil {
					return 0, fmt.Errorf("%w: root answer: %v", errRollout, err)
				}
			} else {
				cands, _, ok := st.TacticalPriority(v, *pd)
				found := false
				for i := 0; ok && i < len(cands); i++ {
					found = found || cands[i].Key == *r.key
				}
				if !found {
					return 0, fmt.Errorf("%w: candidate not offered in this world", errRollout)
				}
				st.ForcePriority(*r.key)
				in, _ = st.Decide(ctx, v, *pd)
			}
			if err := w.SubmitHypothetical(in); err != nil {
				return 0, fmt.Errorf("%w: root answer refused: %v", errRollout, err)
			}
			continue
		}
		in, _ = st.Decide(ctx, v, *pd)
		if err := submit(w, st, v, pd, in); err != nil {
			return 0, err
		}
	}
}

// submit is cmd/botbench's refused-answer contract on a world: the answer,
// else the seat's next choice (builtins.Seat.Refused), else the minimal
// answer (pass), else the default bot's.
func submit(w *rules.Engine, st *builtins.Seat, v view.View, pd *decision.Decision, in decision.Intent) error {
	if w.SubmitHypothetical(in) == nil {
		return nil
	}
	d := pd.CloneValue()
	if w.SubmitHypothetical(st.Refused(v, d, in)) == nil {
		return nil
	}
	fb := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				fb.Choices = []int{o.Index}
				break
			}
		}
	} else {
		fb = botpolicy.Clamp(&d, fb)
	}
	if w.SubmitHypothetical(fb) == nil {
		return nil
	}
	brd := botpolicy.BoardFromGame(w.G, w, d.Player)
	fb = botpolicy.Decide(brd, &d, rand.New(rand.NewPCG(d.Seq, uint64(d.Player)+1)))
	fb.Seq, fb.Player = d.Seq, d.Player
	if err := w.SubmitHypothetical(fb); err != nil {
		return fmt.Errorf("%w: every answer refused: %v", errRollout, err)
	}
	return nil
}

// mix is SplitMix64's finaliser.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
