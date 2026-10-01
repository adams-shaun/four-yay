package rules

import "github.com/adams-shaun/gorge/state"

// ViewCharacteristics is Name(id), Keywords(id), Power(id) and Toughness(id)
// in one call -- the four derived facts a view.CardView projects -- read off
// one Derived instead of two Derived and two derivedScalar walks. Derived's
// P/T is the same layer-7 walk derivedScalar runs: derivedScalar only skips
// binding the finished keyword list when no layer-7 effect's Affects$ reads
// keywords (and otherwise delegates to Derived itself), and its type list and
// 7a basis are the ones derivedCompute passes. Keywords aliases Engine
// scratch storage exactly as Derived does: copy it before the next
// characteristics query.
func (e *Engine) ViewCharacteristics(id state.ObjID) (name string, keywords []string, power, toughness int32) {
	if name, keywords, power, toughness, ok := e.printedViewCharacteristics(id); ok {
		return name, keywords, power, toughness
	}
	d := e.Derived(id)
	return d.Name, d.Keywords, d.Power, d.Toughness
}
