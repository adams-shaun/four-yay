package builtins

import (
	"github.com/adams-shaun/gorge/decision"
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
// planner-paid cast (plan), or a potential play to pursue (pot).
type cand struct {
	cls  class
	opt  int
	plan *decision.PaymentAction
	pot  *actionKey
}

// priority answers a KPriority decision (package doc, 1). depth bounds the
// re-choice after a pursuit fails on the spot.
func (s *Seat) priority(v view.View, d *decision.Decision, depth int) decision.Intent {
	if s.pursuit != nil {
		if in, ok := s.pursue(d); ok {
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
		return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{
			ActionID: c.plan.ID, Plan: decision.ClonePaymentPlan(c.plan.Plans[0]),
		}}
	}
	k := *c.pot
	s.pursuit = &k
	s.Stats.Pursuits++
	if in, ok := s.pursue(d); ok {
		return in
	}
	// No source to tap: the play is now in the failed set, so choose again
	// (bounded: every retry excludes one more play).
	if depth < len(cands) {
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
		if cls == clsMana && s.mana == AutoPay {
			continue
		}
		cands = append(cands, cand{cls: cls, opt: o.Index})
		offered[optionKey(o)] = true
		if o.Kind == "cast" && o.Mode == "" && o.AltCostIndex == 0 {
			ordinaryCast[o.Obj] = true
		}
	}
	if s.mana != AutoPay {
		return cands
	}
	for i := range d.PaymentActions {
		a := &d.PaymentActions[i]
		if len(a.Plans) == 0 || a.BaseOptionIndex != nil || ordinaryCast[a.Cast.Object] {
			continue
		}
		ordinaryCast[a.Cast.Object] = true
		offered[actionKey{kind: "cast", obj: a.Cast.Object}] = true
		cands = append(cands, cand{cls: clsCast, opt: -1, plan: a})
	}
	for _, p := range potential(v, d.Player) {
		k := actionKey{kind: p.Kind, obj: p.Obj, ability: p.Ability, mode: p.Mode}
		if p.Kind == "play_land" || offered[k] || s.failed[k] {
			continue
		}
		offered[k] = true
		kk := k
		cands = append(cands, cand{cls: classify(p.Kind, p.Mode), opt: -1, pot: &kk})
	}
	return cands
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
func (s *Seat) pursue(d *decision.Decision) (decision.Intent, bool) {
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
				return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{
					ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0]),
				}}, true
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
