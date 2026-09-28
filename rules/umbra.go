package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Umbra armor (CR 702.90). The keyword is a static replacement property, the
// same class as Protection or Indestructible (cards/keywords.go's doc: such
// keywords are NOT expanded — rules reads them directly), so there is no
// expansion here: effects.ReplaceUmbraArmor consults the bearer's attached
// Auras through the method below at the three destruction choke points
// (effects/zone.go's effDestroy and effDestroyAll, rules/sba.go's
// lethal-damage sweep).

// UmbraArmorAura implements effects.Host: the first Aura attached to bearer
// id whose DERIVED keyword set carries "Umbra armor", in the deterministic
// AliveFrom(0) × battlefield-slice scan hasAttachmentOfKind uses (never a
// map, so a replay reproduces the same choice). Derived, never the printed
// face: the layer-6 grants (Umbra Mystic's "Auras attached to permanents you
// control have umbra armor", Dog Umbra's conditional self-grant) must be
// seen. Returns 0 when the bearer wears none.
func (e *Engine) UmbraArmorAura(id state.ObjID) state.ObjID {
	for _, p := range e.G.AliveFrom(0) {
		for _, sid := range e.G.Zone(state.ZBattlefield, p) {
			s := e.G.Obj(sid)
			if s == nil || s.AttachedTo != id {
				continue
			}
			if e.hasKeywordH(sid, kwhUmbraArmor) {
				return sid
			}
		}
	}
	return 0
}

func init() {
	// The printed keyword is now read: the 15 corpus carriers (the umbra
	// cycle) stop being unsupported in the coverage census.
	effects.RegisterNonAPI("kw:Umbra armor")
}
