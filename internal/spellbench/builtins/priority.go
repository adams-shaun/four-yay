package builtins

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/payexec"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// class is a priority candidate's SpellBench kind family.
type class int

const (
	clsPass    class = iota
	clsLand          // play_land
	clsCast          // cast_spell
	clsAbility       // activate_ability
	clsMana          // activate_mana_ability (Manual only)
	clsSpecial       // special_action: plot, unlock, turn face up, specialize
	clsOther
)

// classify maps a gorge priority option (or potential action) kind to its
// SpellBench family, following SpellBench's own gorge adapter
// (engines/gorge/internal/mapping/priority.go on its gorge-adapter branch).
func classify(kind, mode string) class {
	switch kind {
	case "pass":
		return clsPass
	case "play_land":
		return clsLand
	case "cast":
		if mode == "plot" {
			return clsSpecial
		}
		return clsCast
	case "ability", "granted", "station":
		return clsAbility
	case "activate":
		return clsMana
	case "unlock", "turn_face_up", "specialize":
		return clsSpecial
	}
	return clsOther
}

// actionKey identifies a play across the offered options and the
// PotentialActions projection, which share Kind/Obj/Ability/Mode.
type actionKey struct {
	kind    string
	obj     state.ObjID
	ability int
	mode    string
}

func optionKey(o *decision.Option) actionKey {
	return actionKey{kind: o.Kind, obj: o.Obj, ability: o.Ability, mode: o.Mode}
}

// cand is one priority candidate: an offered option (opt >= 0), a
// planner-paid cast (plan), a potential ability with a mana witness (pot and
// witness), or a potential play to pursue (pot alone).
type cand struct {
	cls     class
	opt     int
	plan    *decision.PaymentAction
	pot     *actionKey
	witness *decision.PaymentPlan
}

// priority answers a KPriority decision (package doc, 1). depth bounds the
// re-choice after a pursuit fails on the spot.
func (s *Seat) priority(v view.View, d *decision.Decision, depth int) decision.Intent {
	if s.pursuit != nil {
		if in, ok := s.pursue(v, d); ok {
			return in
		}
	}
	cands := s.candidates(v, d)
	if len(cands) == 0 {
		return repaired(d, decision.Intent{})
	}
	var i int
	switch s.policy {
	case Uniform:
		i = s.rng.Index(len(cands))
	case Heuristic:
		i = heuristicPick(cands)
	}
	c := cands[i]
	switch {
	case c.opt >= 0:
		return one(d, c.opt)
	case c.plan != nil:
		return s.payWith(v, d, c.plan, depth)
	case c.witness != nil:
		return s.lowerPlay(v, d, *c.pot, *c.witness, depth)
	}
	k := *c.pot
	s.pursuit = &k
	s.Stats.Pursuits++
	if in, ok := s.pursue(v, d); ok {
		return in
	}
	// No source to tap: the play is now in the failed set, so choose again
	// (bounded: every retry excludes one more play).
	return s.rechoose(v, d, depth, len(cands))
}

// rechoose answers d with a fresh choice after a play was excluded, bounded
// by depth (every retry excludes one more play), else pass.
func (s *Seat) rechoose(v view.View, d *decision.Decision, depth, bound int) decision.Intent {
	if depth < bound+maxAttempts {
		return s.priority(v, d, depth+1)
	}
	if i, ok := firstKind(d, "pass"); ok {
		return one(d, i)
	}
	return repaired(d, decision.Intent{})
}

// heuristicPick is the builtin's preference: the first play_land, else the
// first cast_spell, else the first activated (or mana) ability, else
// candidate 0 (pass).
func heuristicPick(cands []cand) int {
	for _, want := range []func(class) bool{
		func(c class) bool { return c == clsLand },
		func(c class) bool { return c == clsCast },
		func(c class) bool { return c == clsAbility || c == clsMana },
	} {
		for i, c := range cands {
			if want(c.cls) {
				return i
			}
		}
	}
	return 0
}

// candidates builds the ordered priority candidate list: pass first, then
// the offered options in engine order (mana abilities only in Manual), then
// (AutoPay) planner-only casts and the potential plays not yet offered.
func (s *Seat) candidates(v view.View, d *decision.Decision) []cand {
	cands := make([]cand, 0, len(d.Options)+len(d.PaymentActions))
	if i, ok := firstKind(d, "pass"); ok {
		cands = append(cands, cand{cls: clsPass, opt: i})
	}
	offered := make(map[actionKey]bool, len(d.Options)) // lookup only
	ordinaryCast := make(map[state.ObjID]bool)          // lookup only
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "pass" || o.Kind == "concede" {
			continue
		}
		cls := classify(o.Kind, o.Mode)
		if cls == clsMana && s.mana != Manual {
			continue
		}
		cands = append(cands, cand{cls: cls, opt: o.Index})
		offered[optionKey(o)] = true
		if o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 {
			ordinaryCast[o.Obj] = true
		}
	}
	if s.mana == Manual {
		return cands
	}
	for i := range d.PaymentActions {
		a := &d.PaymentActions[i]
		if len(a.Plans) == 0 || a.BaseOptionIndex != nil || ordinaryCast[a.Cast.Object] {
			continue
		}
		k := actionKey{kind: "cast", obj: a.Cast.Object}
		if s.failed[k] {
			continue
		}
		ordinaryCast[a.Cast.Object] = true
		offered[k] = true
		cands = append(cands, cand{cls: clsCast, opt: -1, plan: a})
	}
	for _, p := range potential(v, d.Player) {
		k := actionKey{kind: p.Kind, obj: p.Obj, ability: p.Ability, mode: p.Mode}
		if p.Kind == "play_land" || offered[k] || s.failed[k] {
			continue
		}
		offered[k] = true
		kk := k
		c := cand{cls: classify(p.Kind, p.Mode), opt: -1, pot: &kk}
		if pp := s.potentialPlan(d, k); pp != nil {
			switch {
			case pp.Reason == "insufficient":
				// Proven unpayable: not a legal action (the potential walk is
				// an over-bound), so not a candidate.
				s.Stats.ExcludedUnpayable++
				continue
			case pp.Plan != nil:
				c.witness = pp.Plan
			}
		}
		cands = append(cands, c)
	}
	return cands
}

// potentialPlan is the planner's verdict on play k at d, nil without a
// planner or for a play it did not list. One planner call per decision.
func (s *Seat) potentialPlan(d *decision.Decision, k actionKey) *rules.PotentialPlan {
	if s.planner == nil {
		return nil
	}
	if s.plans == nil || s.planSeq != d.Seq {
		s.planSeq = d.Seq
		if s.plans == nil {
			s.plans = map[actionKey]*rules.PotentialPlan{}
		}
		clear(s.plans)
		pps := s.planner.PotentialPaymentPlans(d.Player)
		for i := range pps {
			a := pps[i].Action
			s.plans[actionKey{kind: a.Kind, obj: a.Obj, ability: a.Ability, mode: a.Mode}] = &pps[i]
		}
	}
	return s.plans[k]
}

// potential returns the deciding seat's own PotentialActions from the view.
func potential(v view.View, me state.PlayerID) []decision.PotentialAction {
	for i := range v.Players {
		if v.Players[i].ID == me {
			return v.Players[i].PotentialActions
		}
	}
	return nil
}

// pursue advances the current pursuit: take the play once it is offered
// (or planned), else tap the first offered mana source. With no source left
// the pursuit fails: the play joins the step's failed set and ok is false.
func (s *Seat) pursue(v view.View, d *decision.Decision) (decision.Intent, bool) {
	k := *s.pursuit
	for i := range d.Options {
		if optionKey(&d.Options[i]) == k {
			s.pursuit = nil
			return one(d, d.Options[i].Index), true
		}
	}
	if k.kind == "cast" && k.mode == "" {
		for i := range d.PaymentActions {
			a := &d.PaymentActions[i]
			if a.Cast.Object == k.obj && len(a.Plans) > 0 {
				s.pursuit = nil
				return s.payWith(v, d, a, 0), true
			}
		}
	}
	if i, ok := firstKind(d, "activate"); ok {
		s.Stats.PursuitTaps++
		return one(d, i), true
	}
	s.failed[k] = true
	s.pursuit = nil
	s.Stats.PursuitFailures++
	if pp := s.potentialPlan(d, k); pp != nil && pp.Reason == "" && (pp.Plan != nil || k.kind == "cast") {
		// The planner could pay it: a lost play, not an over-bound.
		s.Stats.PursuitFailuresPriced++
		s.Stats.LostPlays++
	}
	return decision.Intent{}, false
}

// pursuitColour answers a mana-colour ask met while pursuing (the options
// all name a ManaSymbol): the colour the floating pool holds least of, ties
// broken toward the colour the seat's remaining untapped sources can make
// least, then option order. ok is false for any other shape of ask.
func pursuitColour(v view.View, d *decision.Decision) (int, bool) {
	const syms = "WUBRGC"
	var me *view.PlayerView
	for i := range v.Players {
		if v.Players[i].ID == d.Player {
			me = &v.Players[i]
		}
	}
	if me == nil || len(d.Options) == 0 {
		return 0, false
	}
	var producible [6]int
	for _, cv := range me.Battlefield {
		if cv.Tapped || cv.Produces == nil {
			continue
		}
		// Any with listed colours is "one of these" (a dual); Any with none
		// listed is a true any-colour source.
		listed := cv.Produces.Colour != [6]int32{}
		for c := 0; c < 6; c++ {
			if cv.Produces.Colour[c] > 0 || (cv.Produces.Any && !listed) {
				producible[c]++
			}
		}
	}
	best, bestPool, bestProd := -1, 0, 0
	for _, o := range d.Options {
		if len(o.ManaSymbol) != 1 {
			return 0, false
		}
		c := -1
		for j := 0; j < len(syms); j++ {
			if syms[j] == o.ManaSymbol[0] {
				c = j
			}
		}
		if c < 0 {
			return 0, false
		}
		pool := int(me.Pool[o.ManaSymbol])
		if best < 0 || pool < bestPool || (pool == bestPool && producible[c] < bestProd) {
			best, bestPool, bestProd = o.Index, pool, producible[c]
		}
	}
	return best, true
}

// maxAttempts bounds one play's lowerings in one step: a play whose second
// lowering aborts too is excluded for the rest of the step.
const maxAttempts = 2

// payWith pays for a planner-offered cast. AutoPay submits the first plan
// as the Intent.Payment witness. Planned lowers the plan onto the manual
// surface through payexec, answering this decision with its first step.
func (s *Seat) payWith(v view.View, d *decision.Decision, a *decision.PaymentAction, depth int) decision.Intent {
	if s.mana != Planned {
		return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{
			ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0]),
		}}
	}
	s.exec = payexec.Start(d.Player, a, payexec.PoolFromView(v, d.Player))
	s.execKey = actionKey{kind: "cast", obj: a.Cast.Object}
	s.Stats.Lowerings++
	return s.lowerFirst(v, d, depth)
}

// lowerPlay lowers witness, the planner's mana witness for potential play
// k (an activated ability), in every mode that hides mana abilities.
func (s *Seat) lowerPlay(v view.View, d *decision.Decision, k actionKey, witness decision.PaymentPlan, depth int) decision.Intent {
	play := payexec.Play{Kind: k.kind, Obj: k.obj, Ability: k.ability}
	s.exec = payexec.StartPlay(d.Player, play, witness, payexec.PoolFromView(v, d.Player))
	s.execKey = k
	s.Stats.Lowerings++
	s.Stats.AbilityLowerings++
	return s.lowerFirst(v, d, depth)
}

// lowerFirst answers d with a fresh lowering's first step.
func (s *Seat) lowerFirst(v view.View, d *decision.Decision, depth int) decision.Intent {
	if in, ok := s.lower(v, d, depth); ok {
		return in
	}
	// A fresh lowering at priority never yields; an abort was answered by
	// lower's re-plan. Defensive only.
	return repaired(d, decision.Intent{})
}

// lower feeds d to the lowering in progress. ok is false when the policy
// must answer d: the lowering yielded a foreign decision (and continues), or
// aborted on a non-priority ask. An abort at priority is answered here: the
// same play re-planned from the pool now held, else the policy's next
// choice (rechoose), else pass.
func (s *Seat) lower(v view.View, d *decision.Decision, depth int) (decision.Intent, bool) {
	in, st := s.exec.StepOn(d, payexec.Surface{Pool: payexec.PoolFromView(v, d.Player), Stack: len(v.Stack)})
	switch st {
	case payexec.InProgress:
		return in, true
	case payexec.Yield:
		s.Stats.LoweringYields++
		return decision.Intent{}, false
	case payexec.Done:
		if s.exec.Play.Kind == "ability" {
			s.Stats.LoweredAbilities++
		} else {
			s.Stats.LoweredCasts++
		}
		if s.lost[s.execKey] {
			delete(s.lost, s.execKey)
			s.Stats.LostPlays--
			s.Stats.RecoveredPlays++
		}
		s.finishLowering()
		return in, true
	}
	if len(s.Stats.AbortSamples) < maxAbortSamples {
		s.Stats.AbortSamples = append(s.Stats.AbortSamples, fmt.Sprintf("turn %d %s: %s %d: %s: %s", v.Turn, v.Step, s.exec.Play.Kind, s.exec.Play.Obj, s.exec.Reason, s.exec.Detail))
	}
	k := s.execKey
	s.abortLowering(s.exec.Reason)
	if d.Kind != decision.KPriority {
		return decision.Intent{}, false
	}
	if s.attempts[k] < maxAttempts && depth < maxAttempts {
		// Re-plan the same play from the pool now held: the decision's own
		// plans (and the planner's) are priced from it.
		for i := range d.PaymentActions {
			a := &d.PaymentActions[i]
			if k.kind == "cast" && k.mode == "" && a.Cast.Object == k.obj && len(a.Plans) > 0 {
				s.Stats.Replans++
				if a.BaseOptionIndex != nil {
					// The floating pool now pays it outright.
					return one(d, *a.BaseOptionIndex), true
				}
				return s.payWith(v, d, a, depth+1), true
			}
		}
		if pp := s.potentialPlan(d, k); pp != nil && pp.Plan != nil {
			s.Stats.Replans++
			return s.lowerPlay(v, d, k, *pp.Plan, depth+1), true
		}
		for i := range d.Options {
			if optionKey(&d.Options[i]) == k {
				s.Stats.Replans++
				return one(d, d.Options[i].Index), true
			}
		}
	}
	s.failed[k] = true
	in = s.rechoose(v, d, depth, len(d.Options)+len(d.PaymentActions))
	if len(in.Choices) == 1 && in.Payment == nil && d.Options[in.Choices[0]].Kind == "pass" {
		s.Stats.AbortPasses++
	}
	return in, true
}

func (s *Seat) finishLowering() {
	s.Stats.LoweringTaps += s.exec.Taps
	s.Stats.LoweringAsks += s.exec.Asks
	s.Stats.LoweringWaits += s.exec.Waits
	s.exec = nil
}

// abortLowering ends the lowering in progress, counting the abort and
// opening its play as lost until a re-plan completes it.
func (s *Seat) abortLowering(reason string) {
	s.Stats.Aborts++
	if s.Stats.AbortsByCause == nil {
		s.Stats.AbortsByCause = map[string]int{}
	}
	s.Stats.AbortsByCause[reason]++
	s.attempts[s.execKey]++
	if !s.lost[s.execKey] {
		s.lost[s.execKey] = true
		s.Stats.LostPlays++
	}
	s.finishLowering()
}
