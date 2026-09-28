// Package builtins ports SpellBench's three builtin bots -- uniform,
// heuristic and first (github.com/jackmaiorino/spellbench,
// python/spellbench/arena/bots/*.py for protocol v1 and
// python/spellbench/builtins/*.py on branch protocol-v2) -- onto gorge's
// decision model, so gorge can run a SpellBench-style workup natively
// (cmd/botbench -spellbench).
//
// # What the originals do
//
// SpellBench poses one decision at a time as an ordered candidate list and a
// bot picks one candidate:
//
//   - uniform draws SplitMix64.next() % len(candidates) from a per-game
//     stream seeded with (agent_seed ^ seed) (v2; v1 mixed the game id in);
//     the benchmark configures seed 11. It is the rating anchor (Elo 1000).
//   - heuristic answers, in order of preference: mulligan keep; the first
//     play_land; the first cast_spell; the first activate_mana_ability or
//     activate_ability; every attacker attacks (the first non-null
//     defender); no creature blocks (null attacker); choose_starting_player
//     naming itself; otherwise candidate 0.
//   - first always answers candidate 0, which is pass whenever passing is
//     legal (v2 spec 7.1).
//
// # How gorge's decisions are mapped (every choice made here)
//
// gorge answers a Decision with an Intent naming option indices, and poses
// composite asks (an attacker subset, a target set) as ONE decision where
// SpellBench decomposes them into per-item scans. The mapping emulates the
// decomposition inside the seat, drawing once per emulated wire decision:
//
//  1. Priority (decision.KPriority). The candidate list is pass first (the v2
//     7.1 rule), then gorge's offered options in engine order (play_land,
//     cast, ability, special actions), concede never (the host owns
//     concession, v2 Annex B). The mana surface depends on the ManaMode:
//
//     AutoPay (sb-uniform, sb-heuristic, sb-first): mana abilities ("activate"
//     options) are NOT candidates -- the v2 "engine_autopay" default. A spell
//     is a candidate when its cast is offered (the floating pool pays it) or
//     when gorge's payment planner offers a plan for it
//     (Decision.PaymentActions, requested through PaymentPlanConsumer); a
//     chosen plan is submitted as the Intent.Payment witness. Because gorge
//     plans only casts, every other play the seat could afford after tapping
//     out -- a mana-costed activated ability, a kicked or otherwise
//     non-ordinary cast -- is taken from the view's PotentialActions (the
//     engine's own offer walk priced against the tapped-out pool) and is a
//     candidate too. Choosing one starts a pursuit: the seat taps its first
//     offered mana source at each following priority decision of the same
//     step until the play is offered, then takes it; when no source is left
//     the pursuit fails, the play is excluded for the rest of that step and
//     the seat chooses again. A colour asked while pursuing is the one the
//     floating pool holds least of, ties to the colour the remaining
//     untapped sources can make least (coverage for multi-colour costs).
//     This is a naive payer (it can strand mana, and PotentialActions is an
//     over-bound, so some pursuits fail after tapping), which is the right
//     strength class for these bots; the planner-paid casts are exact.
//
//     Planned (sb-uniform-planned, sb-heuristic-planned): the AutoPay
//     candidate set and choice logic -- mana abilities hidden, planner-paid
//     casts, pursuits -- played on the manual surface. A chosen
//     planner-paid cast is not submitted as a witness; its plan (the first
//     one with no last-resort step, internal/spellbench/payexec.SelectPlan)
//     is LOWERED: the seat answers the following decisions by activating
//     each plan source in order, answering any mana-ability or colour ask
//     with the witness's production, and then selecting the ordinary cast
//     option. payexec verifies every step (the option is offered, the pool
//     is what the plan predicts); on a divergence the lowering aborts, the
//     seat passes (at priority; any other ask goes to the policy) and the
//     abort is counted in Stats. So the policy never sees or picks a raw
//     tap: taps happen only inside a chosen cast's lowering (or, exactly as
//     under AutoPay, a pursuit of a play the planner does not price).
//
//     Manual (sb-uniform-manual, sb-heuristic-manual): the literal protocol
//     surface SpellBench's own mtg-kernel and gorge adapters expose, where
//     every mana ability is a priority candidate (activate_mana_ability) and
//     a cast is offered only once the floating pool pays it. No plans and no
//     pursuit. The heuristic's third preference then taps the first mana
//     source, exactly as the original's "first of activate_mana_ability or
//     activate_ability" does.
//
//     heuristic ranks play_land > cast > ability (and, Manual, mana ability)
//     and otherwise answers candidate 0 (pass); special actions (plot,
//     unlock, turn face up) are unranked, as in the original. uniform is
//     uniform over the list; first answers pass.
//
//  2. Attackers (KAttackers, a subset of (creature, defender) pairs). Emulated
//     as v2's declare_attack group: one draw per creature that can attack,
//     over [null, each legal defender] (null omitted for a creature that must
//     attack), in the engine's option order. uniform is thus an independent
//     coin flip per creature in a two-player game (v1's inclusion scan);
//     heuristic attacks with every creature at its first defender; first
//     answers null (attacks only with required creatures).
//
//  3. Blockers (KBlockers). v2's declare_block group: one draw per potential
//     blocker over [null, each attacker it may block] (null omitted when the
//     blocker is required). heuristic and first never block beyond what is
//     required. A second block by one creature (a GroupCap above 1) is not
//     emulated: each blocker blocks at most once.
//
//  4. Every other ask -- targets, KChoose (X, sacrifice, discard, search,
//     colours, yes/no), KModes, KTriggerOptional, KReplacement, the trigger
//     order -- is a Min..Max subset of gorge's options. It is emulated as the
//     v2 sequential selection: at each step the candidates are "finish" (first,
//     and only once Min picks are made -- the pass-first rule) followed by
//     every still-admissible option in engine order (group caps, budget,
//     set-property and same-controller constraints respected), until finish
//     or Max. uniform draws each step; heuristic and first take candidate 0,
//     so they answer the Min lowest-index options. A trigger order (Min == Max
//     == n) is therefore a uniform permutation for uniform and gorge's
//     offered order for the other two.
//
//  5. KArrange (scry, surveil, look-and-rearrange): uniform draws pile A (the
//     kept, ordered cards) sequentially as in 4 and shuffles the complement
//     into a uniform order; heuristic and first keep the arrangement as
//     offered (as many cards in pile A, in offered order, as Max allows).
//
//  6. KMulligan and KStartingPlayer are not posed by the runner (SpellBench's
//     "mulligan: none" and "starting_player: host_assigned"); should they be,
//     heuristic keeps and names its own seat, the others follow 4.
//
// Every answer is repaired through botpolicy.Clamp and checked with
// Decision.Validate (falling back to the minimal clamped answer), so an
// emulated answer that breaks a whole-declaration quota the per-item draws
// cannot see is repaired rather than submitted. Constraints the wire does
// not publish (for example a lone blocker on a menace attacker) can still be
// refused by the engine; the runner then substitutes a fallback and counts
// it (cmd/botbench's spellbench mode).
//
// Determinism: a seat's answers are a pure function of its seed and the
// decisions and views it is shown. The only maps are lookup sets, never
// ranged into an answer.
package builtins

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/payexec"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Policy names one of the three SpellBench builtin bots.
type Policy int

const (
	// Uniform picks a uniformly random candidate (the rating anchor).
	Uniform Policy = iota
	// Heuristic applies the builtin's fixed kind preference.
	Heuristic
	// First always answers the first candidate.
	First
	// Tactical is sb-tactical, the scored seat-visible heuristic
	// (tactical.go); construct it with NewTactical.
	Tactical
)

func (p Policy) String() string {
	switch p {
	case Uniform:
		return "uniform"
	case Heuristic:
		return "heuristic"
	case First:
		return "first"
	case Tactical:
		return "tactical"
	}
	return "unknown"
}

// ManaMode selects how mana abilities surface at priority (package doc, 1).
type ManaMode int

const (
	// AutoPay hides mana abilities and pays casts through gorge's planner.
	AutoPay ManaMode = iota
	// Manual offers every mana ability as a priority candidate.
	Manual
	// Planned plays the manual surface with the AutoPay candidate set: mana
	// abilities are hidden from the policy, and a chosen planner-paid cast
	// is lowered onto the manual surface by internal/spellbench/payexec
	// (activate each plan source, then cast) instead of being submitted as
	// an Intent.Payment witness (package doc, 1).
	Planned
)

// UniformSeed is the seed the pauper-kernel benchmark configures for its
// uniform bot (benchmark.json "seed": 11). Callers XOR it into the per-seat
// seed, mirroring v2's (agent_seed ^ seed).
const UniformSeed uint64 = 11

// Stats counts the seat's AutoPay pursuit and Planned lowering activity
// (package doc, 1).
type Stats struct {
	Decisions       int // decisions this seat answered
	Pursuits        int // potential plays chosen
	PursuitTaps     int // mana sources tapped while pursuing
	PursuitFailures int // pursuits abandoned with no source left

	// Planned only: plan lowerings started, completed (cast submitted) and
	// aborted, the sources they activated, and the aborts by payexec
	// reason. An abort met at priority is answered with pass
	// (AbortPasses); one met at any other ask is answered by the policy.
	Lowerings     int
	LoweredCasts  int
	LoweringTaps  int
	LoweringAsks  int
	Aborts        int
	AbortPasses   int
	AbortsByCause map[string]int
	// AbortSamples keeps the first few aborts' payexec reason and detail.
	AbortSamples []string
}

const maxAbortSamples = 4

// Add accumulates o into s.
func (s *Stats) Add(o Stats) {
	s.Decisions += o.Decisions
	s.Pursuits += o.Pursuits
	s.PursuitTaps += o.PursuitTaps
	s.PursuitFailures += o.PursuitFailures
	s.Lowerings += o.Lowerings
	s.LoweredCasts += o.LoweredCasts
	s.LoweringTaps += o.LoweringTaps
	s.LoweringAsks += o.LoweringAsks
	s.Aborts += o.Aborts
	s.AbortPasses += o.AbortPasses
	for _, a := range o.AbortSamples {
		if len(s.AbortSamples) < maxAbortSamples {
			s.AbortSamples = append(s.AbortSamples, a)
		}
	}
	for k, v := range o.AbortsByCause {
		if s.AbortsByCause == nil {
			s.AbortsByCause = map[string]int{}
		}
		s.AbortsByCause[k] += v
	}
}

// Seat is one SpellBench builtin bot playing one seat of one game.
type Seat struct {
	policy Policy
	mana   ManaMode
	rng    SplitMix64

	// pursuit is the potential play (AutoPay) the seat is tapping mana
	// for, nil when none. failed holds the plays a pursuit could not pay
	// for in the current step (lookup only), and window names that step.
	pursuit *actionKey
	failed  map[actionKey]bool
	window  stepWindow

	// exec is the plan lowering in progress (Planned), nil when none.
	exec *payexec.Execution

	// tac is the Tactical policy's state, nil for the SpellBench ports.
	tac *tactical

	Stats Stats
}

type stepWindow struct {
	turn int32
	step string
}

var _ interface {
	Decide(context.Context, view.View, decision.Decision) (decision.Intent, error)
	WantsPaymentActions() bool
} = (*Seat)(nil)

// New returns a builtin seat whose stream is seeded with seed. For Uniform
// the caller supplies (per-seat seed ^ UniformSeed); the other two policies
// draw nothing.
func New(p Policy, m ManaMode, seed uint64) *Seat {
	return &Seat{policy: p, mana: m, rng: SplitMix64{state: seed}, failed: map[actionKey]bool{}}
}

// Policy reports the seat's policy.
func (s *Seat) Policy() Policy { return s.policy }

// WantsPaymentActions opts an AutoPay or Planned seat into gorge's payment
// plans. A Planned seat mid-lowering needs none: it is following the plan it
// already holds.
func (s *Seat) WantsPaymentActions() bool {
	return s.mana == AutoPay || (s.mana == Planned && s.exec == nil)
}

// Decide answers d. The View supplies the step (to scope a pursuit) and the
// seat's own PotentialActions; nothing else of it is read.
func (s *Seat) Decide(_ context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	s.sync(v)
	s.Stats.Decisions++
	if s.exec != nil {
		if in, ok := s.lower(v, &d); ok {
			in.Seq, in.Player = d.Seq, d.Player
			return in, nil
		}
	}
	in := s.decide(v, &d)
	in.Seq, in.Player = d.Seq, d.Player
	return in, nil
}

// sync clears the pursuit and the failed set when the step changes: the
// floating mana a pursuit tapped empties with the step.
func (s *Seat) sync(v view.View) {
	w := stepWindow{turn: v.Turn, step: v.Step}
	if w == s.window {
		return
	}
	s.window = w
	s.pursuit = nil
	clear(s.failed)
	if s.exec != nil {
		// The pool empties with the step: a lowering cannot span it.
		s.abortLowering("step_changed")
	}
}

func (s *Seat) decide(v view.View, d *decision.Decision) decision.Intent {
	if s.tac != nil {
		return s.tac.decide(s, v, d)
	}
	switch d.Kind {
	case decision.KPriority:
		return s.priority(v, d, 0)
	case decision.KAttackers:
		return s.attackers(d)
	case decision.KBlockers:
		return s.blockers(d)
	case decision.KArrange:
		return s.arrange(d)
	case decision.KChoose:
		if s.pursuit != nil {
			if i, ok := pursuitColour(v, d); ok {
				return one(d, i)
			}
		}
	case decision.KMulligan:
		if s.policy == Heuristic {
			if i, ok := firstKind(d, "keep"); ok {
				return one(d, i)
			}
		}
	case decision.KStartingPlayer:
		if s.policy == Heuristic {
			for _, o := range d.Options {
				if o.Player == d.Player {
					return one(d, o.Index)
				}
			}
		}
	}
	return s.sequential(d)
}

// pick draws the candidate index for a list of n candidates: uniform draws,
// the other two take candidate 0.
func (s *Seat) pick(n int) int {
	if s.policy == Uniform && n > 1 {
		return s.rng.Index(n)
	}
	return 0
}

func one(d *decision.Decision, i int) decision.Intent {
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}
}

func firstKind(d *decision.Decision, kind string) (int, bool) {
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index, true
		}
	}
	return 0, false
}

// repaired returns the clamped answer when it validates, else the minimal
// clamped answer when that does, else the clamped answer (the runner's
// fallback then handles the refusal).
func repaired(d *decision.Decision, in decision.Intent) decision.Intent {
	in.Seq, in.Player = d.Seq, d.Player
	c := botpolicy.Clamp(d, in)
	if d.Validate(c) == nil {
		return c
	}
	m := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player})
	if d.Validate(m) == nil {
		return m
	}
	return c
}

// objGroup is one creature's options in a combat subset decision.
type objGroup struct {
	obj      state.ObjID
	opts     []int
	required bool
}

// groupByObj groups a combat decision's options by Obj in first-seen order.
func groupByObj(d *decision.Decision) []objGroup {
	var gs []objGroup
	at := make(map[state.ObjID]int, len(d.Options)) // lookup only
	for _, o := range d.Options {
		i, ok := at[o.Obj]
		if !ok {
			i = len(gs)
			at[o.Obj] = i
			gs = append(gs, objGroup{obj: o.Obj})
		}
		gs[i].opts = append(gs[i].opts, o.Index)
		if o.Required {
			gs[i].required = true
		}
	}
	return gs
}

// combat emulates a declare_attack / declare_block group: one draw per
// creature over [null (unless required), its options...]. take reports
// whether the non-uniform policies commit the creature to its first option.
func (s *Seat) combat(d *decision.Decision, heuristicTakes bool) decision.Intent {
	var choices []int
	for _, g := range groupByObj(d) {
		null := !g.required
		n := len(g.opts)
		if null {
			n++
		}
		var r int
		switch s.policy {
		case Uniform:
			r = s.rng.Index(n)
		case Heuristic:
			if heuristicTakes && null {
				r = 1 // the first non-null candidate
			}
		}
		if null {
			if r == 0 {
				continue
			}
			r--
		}
		choices = append(choices, g.opts[r])
	}
	return repaired(d, decision.Intent{Choices: choices})
}

func (s *Seat) attackers(d *decision.Decision) decision.Intent { return s.combat(d, true) }
func (s *Seat) blockers(d *decision.Decision) decision.Intent  { return s.combat(d, false) }

// admissible reports whether option o may join chosen without breaking a
// published whole-answer constraint (group cap, budget, set property, same
// controller).
func admissible(d *decision.Decision, chosen []int, o *decision.Option) bool {
	if o.Group != "" {
		n := 0
		for _, c := range chosen {
			if d.Options[c].Group == o.Group {
				n++
			}
		}
		if n >= d.GroupCapFor(o.Group) {
			return false
		}
	}
	if d.HasBudget() {
		sum := o.Value
		for _, c := range chosen {
			sum += d.Options[c].Value
		}
		if sum > d.MaxSum {
			return false
		}
	}
	if d.SetPropMode != "" && !decision.SetPropAdmits(d.SetPropMode, d.SetPropsOf(chosen), o.SetProps) {
		return false
	}
	if d.TargetsWithSameController && len(chosen) > 0 && d.Options[chosen[0]].Controller != o.Controller {
		return false
	}
	return true
}

// selectSeq is the v2 sequential selection (package doc, 4): finish first
// once Min picks are made, then every admissible option in engine order.
func (s *Seat) selectSeq(d *decision.Decision) []int {
	min, max := d.Min, d.Max
	chosen := []int{}
	used := make([]bool, len(d.Options))
	opts := make([]int, 0, len(d.Options))
	for len(chosen) < max {
		opts = opts[:0]
		for i := range d.Options {
			o := &d.Options[i]
			if o.Index < 0 || o.Index >= len(used) || (used[o.Index] && !d.Repeatable) {
				continue
			}
			if admissible(d, chosen, o) {
				opts = append(opts, o.Index)
			}
		}
		finish := len(chosen) >= min
		n := len(opts)
		if finish {
			n++
		}
		if n == 0 {
			break
		}
		r := s.pick(n)
		if finish {
			if r == 0 {
				break
			}
			r--
		}
		chosen = append(chosen, opts[r])
		used[opts[r]] = true
	}
	return chosen
}

func (s *Seat) sequential(d *decision.Decision) decision.Intent {
	return repaired(d, decision.Intent{Choices: s.selectSeq(d)})
}

// arrange answers a KArrange (package doc, 5).
func (s *Seat) arrange(d *decision.Decision) decision.Intent {
	if s.policy != Uniform {
		var choices []int
		for _, o := range d.Options {
			if len(choices) >= d.Max {
				break
			}
			choices = append(choices, o.Index)
		}
		return repaired(d, decision.Intent{Choices: choices})
	}
	choices := s.selectSeq(d)
	in := set(choices)
	var rest []int
	for _, o := range d.Options {
		if !in[o.Index] {
			rest = append(rest, o.Index)
		}
	}
	// A uniform order of the complement: sequential draws without
	// replacement, the permutation the v2 order_pick steps produce.
	for i := 0; i < len(rest)-1; i++ {
		j := i + s.rng.Index(len(rest)-i)
		rest[i], rest[j] = rest[j], rest[i]
	}
	return repaired(d, decision.Intent{Choices: choices, Rest: rest})
}

func set(xs []int) map[int]bool {
	m := make(map[int]bool, len(xs)) // lookup only
	for _, x := range xs {
		m[x] = true
	}
	return m
}
