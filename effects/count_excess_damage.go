package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// countExcessDamagedOpponents answers Rith's NumDamageThisTurn check from
// damage-time recipient history. The first argument is Card (any damage
// source); the second is a union of opponent-controlled creature/walker
// recipients with the excess predicate. Other NumDamageThisTurn forms need
// source/recipient pair history, not just the recipient bit, and fail closed.
func countExcessDamagedOpponents(g *state.Game, c *Ctx, arg string) (int32, bool) {
	const victims = "Creature.OppCtrl+wasDealtExcessDamageThisTurn,Planeswalker.OppCtrl+wasDealtExcessDamageThisTurn"
	spec, ok := strings.CutPrefix(arg, "Card ")
	if !ok || strings.Compare(strings.TrimSpace(spec), victims) != 0 {
		return 0, false
	}
	// The victim may have left play by the end step (a planeswalker with
	// zero loyalty always does). Match the type and controller captured by
	// ExcessDamage, not the recipient's current zone or its new controller.
	// Type bit 1 is creature, bit 2 planeswalker (rules/excess_damage_emit.go).
	var n int32
	for i, v := range g.ExcessDamageVictims {
		if v.Controller == c.Controller || v.Type&3 == 0 || excessVictimSeen(g.ExcessDamageVictims[:i], v.Obj, c.Controller) {
			continue
		}
		n++
	}
	return n, true
}

func excessVictimSeen(prior []state.ExcessDamageVictim, id state.ObjID, you state.PlayerID) bool {
	for _, p := range prior {
		if p.Obj == id && p.Controller != you && p.Type&3 != 0 {
			return true
		}
	}
	return false
}
