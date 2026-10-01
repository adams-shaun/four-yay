package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Cheap exact prefilters for the state-based-action pass loop's battlefield
// scans (sba.go). Each one answers "can this action possibly apply?" from the
// object fields alone, and the full layer-reading scan runs only when it can.
//
// The type prefilters rest on typeCharacteristicsActive's no-layer-4 arm: with
// no LType effect in the active list a battlefield permanent's derived type
// list is its base list (the printed face, or the CR 708.5 face-down words)
// put through CopyNonLegendary's Legendary strip, bestowedTypeSwitch (drops
// Creature, adds Aura) and reconfigureTypeSwitch (drops Creature). None of the
// three can ADD World or Legendary, and only the bestow switch can add Aura,
// so on a face-up permanent the derived answer for those words is the face
// answer (plus the bestow switch for Aura). A face-down permanent and any
// board with a layer-4 effect take the full derived read.
//
// In the rules test binary (sbaQuietVerify) each negative answer is checked
// against the full scan and panics on a disagreement.

// activeHasLType reports whether any active continuous effect is a layer-4
// type effect -- the test typeCharacteristicsActive makes per object, made
// once per SBA scan instead.
func (e *Engine) activeHasLType() bool {
	act := e.active()
	for i := range act {
		if act[i].Layer == LType {
			return true
		}
	}
	return false
}

func faceHasTypeFold(f *cards.Face, t string) bool {
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// sbaTypeFast is hasType for a battlefield permanent with the board's
// layer-4 presence already known (anyLType). t must be "World", "Legendary"
// or "Aura" (see the file comment for why those three are exact).
func (e *Engine) sbaTypeFast(o *state.Object, t string, anyLType bool) bool {
	if anyLType || o.Zone != state.ZBattlefield || (o.FaceDown && o.Zone == state.ZBattlefield) {
		return e.hasType(o, t)
	}
	f := o.Face()
	if f == nil {
		return false
	}
	if t == "Aura" && (o.BestowedAttached() || o.BestowedAuraSpell()) {
		return true
	}
	if t == "Legendary" && o.CopyNonLegendary {
		return false
	}
	return faceHasTypeFold(f, t)
}

// sbaIsAura is isAura through sbaTypeFast.
func (e *Engine) sbaIsAura(o *state.Object, anyLType bool) bool {
	r := e.sbaTypeFast(o, "Aura", anyLType)
	if sbaQuietVerify && r != e.isAura(o) {
		panic(fmt.Sprintf("rules: SBA Aura prefilter disagrees with the derived read for object %d", o.ID))
	}
	return r
}

// mayHaveWorldPair reports whether two or more battlefield permanents could
// carry the World supertype, the precondition of CR 704.5k.
func (e *Engine) mayHaveWorldPair() bool {
	if e.activeHasLType() {
		return true
	}
	n := 0
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || !e.sbaTypeFast(o, "World", false) {
				continue
			}
			if n++; n >= 2 {
				return true
			}
		}
	}
	if sbaQuietVerify && len(e.worldPermanents()) >= 2 {
		panic("rules: SBA world prefilter missed a world pair")
	}
	return false
}

// mayHaveLegendPair reports whether some controller has two or more
// phased-in permanents with a printed Legendary supertype -- legendGroups'
// own first filter, so with no such pair it provably returns no group.
func (e *Engine) mayHaveLegendPair() bool {
	for _, p := range e.G.AliveFrom(0) {
		n := 0
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.PhasedOut || o.Face() == nil || !o.Face().IsLegendary() {
				continue
			}
			if n++; n >= 2 {
				return true
			}
		}
	}
	return false
}
