package rules

import "github.com/adams-shaun/gorge/state"

// ViewCharacteristics is Name(id), Keywords(id), Power(id) and Toughness(id)
// in one call -- the four derived facts a view.CardView projects -- built
// from one Derived and one derivedScalar instead of two of each. Each value
// is exactly what its single-fact accessor returns. Keywords aliases Engine
// scratch storage exactly as Derived does: copy it before the next
// characteristics query.
func (e *Engine) ViewCharacteristics(id state.ObjID) (name string, keywords []string, power, toughness int32) {
	power, toughness = e.derivedScalar(id)
	d := e.Derived(id)
	return d.Name, d.Keywords, power, toughness
}
