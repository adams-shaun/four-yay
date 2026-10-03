package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// synthEradicatorOffersPlay reports whether the engine's may-play cast walk
// would consume the Effect-delivered grant for the exiled card --
// mayPlayEffectGrantsCast is the exact predicate the offer walk's effect arm
// (legal.go) consults for a PLAIN (non-free) grant like Synth Eradicator's
// STPlay, so it asserts the real permission rather than a registered
// continuous effect. It is card-type-agnostic (a land in exile takes the
// same effect arm), unlike mayPlaySpellIds, whose land skip and sorcery-speed
// gate would make this assertion depend on which card the dig happened to
// exile.
func synthEradicatorOffersPlay(e *Engine, card state.ObjID) bool {
	o := e.G.Obj(card)
	return o != nil && e.mayPlayEffectGrantsCast(0, o)
}
