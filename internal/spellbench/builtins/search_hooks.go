package builtins

import (
	"math/rand/v2"
	"sort"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The hooks a searching wrapper (internal/spellbench/sbsearch, sb-search)
// needs to use sb-tactical as both its candidate prior and its rollout
// policy: the scored priority candidates (TacticalPriority), a way to make
// the seat's own machinery play a chosen candidate (ForcePriority: the
// pursuit and plan-lowering state stays the seat's), fresh rollout copies of
// the seat (RolloutClone), and the attack alternatives worth comparing
// (AttackAlternatives). None of them changes a plain sb-tactical game: they
// are read only by the wrapper.

// Priority candidate sources (PriorityKey.Src).
const (
	KeyOption    uint8 = iota // an offered option
	KeyPlan                   // a planner-paid cast (Decision.PaymentActions)
	KeyPotential              // a potential play (pursued or lowered)
)

// PriorityKey names one priority candidate stably across worlds that share
// the decision's public facts: the same key picks the same play in a
// redealt world of the same decision.
type PriorityKey struct {
	Src     uint8
	Opt     int // the option index (KeyOption), else -1
	Kind    string
	Obj     state.ObjID
	Ability int
	Mode    string
	Alt     int
}

func candKey(d *decision.Decision, c cand) PriorityKey {
	switch {
	case c.opt >= 0:
		o := optAt(d, c.opt)
		return PriorityKey{Src: KeyOption, Opt: c.opt, Kind: o.Kind, Obj: o.Obj, Ability: o.Ability, Mode: o.Mode, Alt: o.AltCostIndex}
	case c.plan != nil:
		return PriorityKey{Src: KeyPlan, Opt: -1, Kind: "cast", Obj: c.plan.Cast.Object}
	case c.pot != nil:
		return PriorityKey{Src: KeyPotential, Opt: -1, Kind: c.pot.kind, Obj: c.pot.obj, Ability: c.pot.ability, Mode: c.pot.mode}
	}
	return PriorityKey{Opt: -1}
}

// IsPass reports a pass candidate.
func (k PriorityKey) IsPass() bool { return k.Src == KeyOption && k.Kind == "pass" }

// IsLand reports a land-play candidate.
func (k PriorityKey) IsLand() bool { return k.Kind == "play_land" }

// ScoredCandidate is one priority candidate and its sb-tactical score.
type ScoredCandidate struct {
	Key   PriorityKey
	Score float64
}

// TacticalPriority scores d's priority candidates exactly as the seat's
// next Decide would rank them, and names the index it would pick. ok is
// false -- and the caller must simply Decide -- when the seat is not
// sb-tactical, d is not a priority decision, the Archetype group is on (its
// modulation is not reproduced here), a pursuit or plan lowering is in
// progress (the next answer is that play's next step, not a fresh choice),
// or fewer than two candidates exist. It performs the same idempotent
// bookkeeping Decide opens with (the step window, the blocking
// observation), so calling it before Decide changes no answer.
func (s *Seat) TacticalPriority(v view.View, d decision.Decision) (cands []ScoredCandidate, best int, ok bool) {
	if s.tac == nil || d.Kind != decision.KPriority || s.tac.base.Archetype {
		return nil, 0, false
	}
	s.sync(v)
	if s.exec != nil || s.pursuit != nil {
		return nil, 0, false
	}
	t := s.tac
	t.observe(v, d.Player)
	t.observeArch(v)
	excluded := s.Stats.ExcludedUnpayable
	cs := s.candidates(v, &d)
	s.Stats.ExcludedUnpayable = excluded // Decide counts them once
	if len(cs) < 2 {
		return nil, 0, false
	}
	_, scores := t.scorePriority(v, &d, cs)
	best, bestScore := 0, -1e18
	out := make([]ScoredCandidate, len(cs))
	for i, c := range cs {
		out[i] = ScoredCandidate{Key: candKey(&d, c), Score: scores[i]}
		if scores[i] > bestScore {
			best, bestScore = i, scores[i]
		}
	}
	return out, best, true
}

// ForcePriority makes the seat's next fresh priority choice the candidate
// named k (its own pursuit or lowering then plays it); a key no candidate
// carries leaves the seat's scored pick standing. Decide clears an unused
// force, so it never outlives the decision it was set for.
func (s *Seat) ForcePriority(k PriorityKey) {
	if s.tac != nil {
		kk := k
		s.tac.force = &kk
	}
}

// ProfileCache is a card-profile cache rollout seats share (one goroutine:
// every seat of one search). Profiles are a function of the card name (and,
// for a card the IR cannot read, its first-seen view facts), so sharing it
// across rollout seats is only a saving; keeping it apart from the real
// seat's cache keeps rollouts from reaching the real seat's answers.
type ProfileCache struct{ m map[string]*tProfile }

// NewProfileCache returns an empty cache.
func NewProfileCache() *ProfileCache { return &ProfileCache{m: map[string]*tProfile{}} }

// RolloutClone returns a fresh AutoPay sb-tactical seat for a simulated
// continuation of s's game: s's weights and card lookup, its own stream from
// seed, and cache as its profile cache (nil: a private one). With keep it
// also carries s's step-scoped candidate state (the plays excluded this
// step) and its observation of the opponent's blocking; without, it is the
// seat a fresh opponent model starts as. It shares nothing mutable with s.
// Its planner is unset: the caller hands it the world it plays in.
func (s *Seat) RolloutClone(seed uint64, cache *ProfileCache, keep bool) *Seat {
	if s.tac == nil {
		return nil
	}
	c := NewTactical(AutoPay, seed, s.tac.lookup, s.tac.base)
	if cache != nil {
		c.tac.cache = cache.m
	}
	if keep {
		c.window = s.window
		for k, v := range s.failed {
			c.failed[k] = v
		}
		c.tac.blockChances, c.tac.blocks, c.tac.lastBlockTurn = s.tac.blockChances, s.tac.blocks, s.tac.lastBlockTurn
	}
	return c
}

// AttackAlternatives are the whole attacking declarations worth comparing
// with sb-tactical's own answer at a KAttackers decision: no attack beyond
// what is required, and every creature that can attack the opponent doing
// so (the alpha strike). Each is repaired to a valid answer; duplicates of
// one another are dropped (the caller drops its own pick's duplicate).
func AttackAlternatives(v view.View, d decision.Decision) []decision.Intent {
	if d.Kind != decision.KAttackers {
		return nil
	}
	brd := seat.BoardFromView(v)
	var all []int
	for _, g := range groupByObj(&d) {
		for _, i := range g.opts {
			o := &d.Options[i]
			if o.Battle == 0 && o.Player != d.Player {
				all = append(all, i)
				break
			}
		}
	}
	none := repaired(&d, decision.Intent{})
	alpha := repaired(&d, decision.Intent{Choices: botpolicy.LegalAttackChoices(brd, &d, all)})
	out := []decision.Intent{none}
	if !SameChoices(none, alpha) {
		out = append(out, alpha)
	}
	return out
}

// AttackNeighbours are the declarations one creature away from own at a
// KAttackers decision: own without each attacker it names (unless that
// attacker is required), and own plus each creature it leaves home that can
// attack the opponent. Each is repaired to a valid answer and made legal
// (botpolicy.LegalAttackChoices); duplicates of own and of one another are
// dropped. At most max are returned, removals first.
func AttackNeighbours(v view.View, d decision.Decision, own decision.Intent, max int) []decision.Intent {
	if d.Kind != decision.KAttackers || own.Payment != nil || own.Announce != nil {
		return nil
	}
	brd := seat.BoardFromView(v)
	chosen := map[int]bool{} // lookup only
	for _, c := range own.Choices {
		chosen[c] = true
	}
	var out []decision.Intent
	add := func(choices []int) {
		in := repaired(&d, decision.Intent{Choices: botpolicy.LegalAttackChoices(brd, &d, choices)})
		if SameChoices(in, own) {
			return
		}
		for _, o := range out {
			if SameChoices(o, in) {
				return
			}
		}
		out = append(out, in)
	}
	groups := groupByObj(&d)
	for _, g := range groups {
		if g.required || len(out) >= max {
			continue
		}
		in := false
		for _, i := range g.opts {
			in = in || chosen[i]
		}
		if !in {
			continue
		}
		var rest []int
		for _, c := range own.Choices {
			if d.Options[c].Obj != g.obj {
				rest = append(rest, c)
			}
		}
		add(rest)
	}
	for _, g := range groups {
		if len(out) >= max {
			break
		}
		in := false
		for _, i := range g.opts {
			in = in || chosen[i]
		}
		if in {
			continue
		}
		for _, i := range g.opts {
			o := &d.Options[i]
			if o.Battle == 0 && o.Player != d.Player {
				add(append(append([]int(nil), own.Choices...), i))
				break
			}
		}
	}
	return out
}

// BlockAlternatives are the whole blocking declarations worth comparing
// with sb-tactical's own answer at a KBlockers decision: no blocks; the
// value blocks (each attacker, biggest first, met by the blocker that best
// kills it and survives, else survives it, else trades up, never a chump);
// the maximal blocks (the same, chumping with the least valuable free
// creature where nothing better exists); and gorge's default bot's blocks.
// A menace attacker is never assigned (its two-blocker minimum is not
// published per option). Each is repaired to a valid answer; duplicates of
// one another are dropped (the caller drops its own pick's duplicate). It
// reads no seat state that Decide changes and draws nothing from the
// seat's stream. nil when the seat is not sb-tactical or the decision has
// a shape the tactical blocker leaves to its incumbent.
func (s *Seat) BlockAlternatives(v view.View, d decision.Decision) []decision.Intent {
	if s.tac == nil || d.Kind != decision.KBlockers || len(d.Options) == 0 {
		return nil
	}
	st := s.tac.newState(&v, d.Player)
	type pair struct{ blk, atk state.ObjID }
	opt := map[pair]int{} // lookup only
	var atks, blks []*tcre
	seen := map[state.ObjID]bool{} // lookup only
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != "block" || o.MinBlockers != 0 || o.MaxBlockers != 0 {
			return nil
		}
		a, b := st.cre[o.Attacker], st.cre[o.Obj]
		if a == nil || b == nil {
			return nil
		}
		opt[pair{o.Obj, o.Attacker}] = o.Index
		if !seen[a.id] {
			seen[a.id] = true
			atks = append(atks, a)
		}
		if !seen[b.id] {
			seen[b.id] = true
			blks = append(blks, b)
		}
	}
	sortCre(atks, func(x, y *tcre) bool { return x.pow > y.pow || (x.pow == y.pow && x.id < y.id) })
	sortCre(blks, func(x, y *tcre) bool {
		vx, vy := st.creValue(x), st.creValue(y)
		return vx < vy || (vx == vy && x.id < y.id)
	})
	greedy := func(chump bool) []int {
		used := map[state.ObjID]bool{} // lookup only
		var choices []int
		for _, a := range atks {
			if a.menace {
				continue
			}
			bi, bestV := -1, 0.0
			for j, b := range blks {
				if used[b.id] {
					continue
				}
				if _, ok := opt[pair{b.id, a.id}]; !ok {
					continue
				}
				r := fightOutcome(b, []*tcre{a}, 0, 0, false)
				var val float64
				switch {
				case !r.cDies && r.foeDies[0]:
					val = 1000 + st.creValue(a) - st.creValue(b)/100
				case !r.cDies:
					val = 500 - st.creValue(b)/100
				case r.foeDies[0] && st.creValue(a) >= st.creValue(b):
					val = 100 + st.creValue(a) - st.creValue(b)
				case chump:
					val = 1 / (1 + st.creValue(b))
				default:
					continue
				}
				if bi < 0 || val > bestV {
					bi, bestV = j, val
				}
			}
			if bi >= 0 {
				used[blks[bi].id] = true
				choices = append(choices, opt[pair{blks[bi].id, a.id}])
			}
		}
		return choices
	}
	brd := seat.BoardFromView(v)
	var out []decision.Intent
	add := func(in decision.Intent) {
		in = repaired(&d, in)
		for _, o := range out {
			if SameChoices(o, in) {
				return
			}
		}
		out = append(out, in)
	}
	add(decision.Intent{})
	add(decision.Intent{Choices: botpolicy.LegalBlockChoices(brd, &d, greedy(false))})
	add(decision.Intent{Choices: botpolicy.LegalBlockChoices(brd, &d, greedy(true))})
	add(botpolicy.Decide(brd, &d, rand.New(rand.NewPCG(d.Seq, 0xb10c))))
	return out
}

// TargetAlternatives are up to k single-target answers of a KTarget
// decision (Max 1), best first by sb-tactical's own target value, each
// admissible and valid. nil when the seat is not sb-tactical, the decision
// takes more than one target, or the effect cannot be read (sb-tactical
// then answers with the default policy).
func (s *Seat) TargetAlternatives(v view.View, d decision.Decision, k int) []decision.Intent {
	if s.tac == nil || d.Kind != decision.KTarget || d.Max != 1 {
		return nil
	}
	sc, ok := s.tac.scoreTargets(v, &d)
	if !ok {
		return nil
	}
	var out []decision.Intent
	for _, x := range sc {
		if len(out) >= k {
			break
		}
		o := &d.Options[x.idx]
		if !admissible(&d, nil, o) {
			continue
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{x.idx}}
		if d.Validate(in) != nil {
			continue
		}
		out = append(out, in)
	}
	return out
}

// SameChoices reports two answers naming the same option set (order
// ignored) with no payment or announcement.
func SameChoices(a, b decision.Intent) bool {
	if a.Payment != nil || b.Payment != nil || a.Announce != nil || b.Announce != nil || len(a.Choices) != len(b.Choices) {
		return false
	}
	x, y := append([]int(nil), a.Choices...), append([]int(nil), b.Choices...)
	sort.Ints(x)
	sort.Ints(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// Planner is the seat's potential-play planner (SetPlanner), nil when none.
func (s *Seat) Planner() Planner { return s.planner }
