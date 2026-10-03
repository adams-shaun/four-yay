package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// host_read.go is the engine's implementation of effects.HostRead, the read
// role of effects.Host (rules-engine refactor spec W1d): the live game and
// the characteristics queries an effect reads it through.

// Chars is effects.HostRead's characteristics query: the object's current,
// layer-derived characteristics (effects.Chars, which Derived aliases). It is
// Derived(id) answered by pointer: inside a Derived memo scope (a legal-actions
// walk, a BeginDerivedReads board build) the pointer is the memo entry
// itself, otherwise it is the engine's one charsScratch record.
//
// The pointer -- and its Keywords/Types -- is valid until the next Chars or
// Derived call or the next emit, whichever comes first; a caller that holds
// characteristics across either copies the record (and the slices it keeps),
// and never makes two Chars reads in one expression: Go does not order the
// first read's field load before the second call, so both can see the second
// record (the api:ExchangeTextBox capture hit exactly this).
// derivedMemoVerify (the rules test binary) recomputes every memo hit, the
// empirical check that a served entry is the current answer.
func (e *Engine) Chars(id state.ObjID) *effects.Chars {
	if e.derivedMemoDepth > 0 && e.derivedMemoUsable() {
		if d := e.derivedMemoRef(id, 0); d != nil {
			return d
		}
	}
	e.charsScratch = e.derivedCompute(id, 0)
	return &e.charsScratch
}
