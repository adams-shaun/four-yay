package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// matchesTargetSpec is the ONE judge of a stack-object target candidate's
// ValidTgts$ filter, shared by the placement offer (candidatesForLimitInto's
// ZStack arm) and the resolution recheck (legalTargets' object arm), so a
// target offered at announcement cannot fizzle at resolution (Critical C2's
// one-definition rule).
//
// An activated or triggered ability on the stack is a Face-less wrapper
// (CR 113.1c: it has no card characteristics of its own), so a card-type
// qualifier on it -- `Card.Creature` (Echo, Perceptive Prodigy), a bare
// `Artifact` (Scientist Supreme of A.I.M.) -- read the wrapper's empty printed
// face and no type ever matched. The ability's types are those of its SOURCE
// (CR 113.7a, 112.7a), so the candidate's own type words are bound to the
// source's derived type list through SpecContext.ExtraTypes -- the seam every
// type predicate and card-type base already reads for the ONE object it is
// matching. Every other predicate (controller, zone, Targeted$/Self
// referents) keeps reading the wrapper. sc is taken by value, so the binding
// never outlives this call: ExtraTypes otherwise carries the layer walk's own
// types-so-far list.
func (e *Engine) matchesTargetSpec(spec string, id state.ObjID, sc effects.SpecContext) bool {
	if o := e.G.Obj(id); o != nil && o.Face() == nil && o.Ability != nil && o.Source != 0 {
		if src := e.G.Obj(o.Source); src != nil && src.Face() != nil {
			d := e.Derived(o.Source)
			sc.ExtraTypes, sc.ExtraTypesOwner = effects.TypeMatchWords(d.Types, d.AllCreatureTypes), id
		}
	}
	return e.matchesSpec(spec, id, sc)
}
