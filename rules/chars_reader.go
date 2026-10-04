package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// chars_reader.go is package rules' side of chars.Reader (lasagna spec §9.2):
// the read direction of the characteristics layer and the shared activation
// gates, for rules/pay. charsReader is the Engine under another method set:
// asCharsReader is a pointer conversion, so handing it out neither allocates
// nor copies, and its methods are not Engine methods. Every method forwards
// to the engine query its caller used before the move.
type charsReader Engine

var _ chars.Reader = (*charsReader)(nil)

func (r *charsReader) eng() *Engine { return (*Engine)(r) }

func (r *charsReader) DerivedTypes(id state.ObjID) []string {
	e := r.eng()
	return e.derivedTypesOf(id)
}

func (r *charsReader) HasKeyword(id state.ObjID, kw chars.KW) bool {
	e := r.eng()
	return e.hasKeywordH(id, kw)
}

func (r *charsReader) SVarGate(p state.PlayerID, id state.ObjID, ab *cards.SA, merged int) bool {
	e := r.eng()
	return e.sVarGateOK(p, id, ab, merged)
}

func (r *charsReader) ActivationPhasesOK(p state.PlayerID, sa *cards.SA) bool {
	e := r.eng()
	return e.activationPhasesOK(p, sa)
}

func (r *charsReader) PresentGate(spec, cmp string, source state.ObjID, you state.PlayerID) bool {
	e := r.eng()
	n := e.countPresent(spec, source, you)
	if cmp != "" {
		return comparePresent(n, e.presentCompareFor(cmp, source, you))
	}
	return n > 0
}

func (r *charsReader) GrantedAbilities(p state.PlayerID, id state.ObjID) []chars.Granted {
	e := r.eng()
	return e.grantedAbilities(p, id)
}

// Chars (pay.Engine) is the engine's chars.Reader.
func (pe *payer) Chars() chars.Reader { return (*charsReader)(pe) }

// ConfiguredCost (pay.Engine) is the compiled-text sidecar's frozen parse of
// raw, nil outside the configured set.
func (pe *payer) ConfiguredCost(raw string) *pay.CompiledCost {
	e := (*Engine)(pe)
	return e.configuredCost(raw)
}

func (r *charsReader) SameColorRevealSets(p state.PlayerID, source state.ObjID, excludeSource bool) ([]state.ObjID, [][]string) {
	e := r.eng()
	return e.sameColorRevealSets(p, source, excludeSource)
}

func (r *charsReader) TapPower(id state.ObjID, saKind string) int32 {
	e := r.eng()
	return e.tapPowerValue(id, saKind)
}

// ZoneEntrySeq (chars.Reader) is the zone-entry index's sequence for id.
func (r *charsReader) ZoneEntrySeq(id state.ObjID) uint64 {
	return (*Engine)(r).zoneEntrySeq(id)
}
