// Ability target slots for the level-B activate template. SlotSpecs
// (candidates.go) walks a spell ability chain; ChainSlotSpecs walks an SVar
// chain by name. An activated ability is neither: it is a *cards.SA whose
// ValidTgts$ lives on the ability itself and whose chain continues through
// its Sub links. AbilitySlotSpecs mirrors the two, starting the walk at the
// ability and following Sub, so an activated ability's targets become the
// same []Slot the existing Fixtures cross product consumes.
package oraclegen

import "github.com/adams-shaun/gorge/cards"

// AbilitySlotSpecs lists the target slots along one activated ability's
// chain, in the order the activation asks for them. It mirrors SlotSpecs'
// per-ability walk (spell ability -> Sub chain) and ChainSlotSpecs' SVar
// walk (Sub chain), differing only in its entry point: the ability's own
// params, then its Sub links.
func AbilitySlotSpecs(f *cards.Face, sa *cards.SA) []Slot {
	if sa == nil {
		return nil
	}
	var out []Slot
	for s := sa; s != nil; s = s.Sub {
		v := s.Params["ValidTgts"]
		if v == "" {
			continue
		}
		// An ability that names a zone finds its cards there, exactly as
		// SlotSpecs reads a spell ability's origin zone.
		if z := targetZone(s.Params, false); z != "" && !playerTargetHead(v) {
			v += "@" + z
		}
		// combat is true for the ability's own filter, matching SlotSpecs'
		// spell walk; a combat-state filter expands to its target maximum.
		out = append(out, repeatedSlots(f, s.Params, v, true)...)
	}
	return out
}
