// Event folds for meld (CR 701.42, 712.4): the Meld pairing fold and the
// split a melded permanent makes when it stops being one. The representation
// is documented in state/meld.go.
package events

import "github.com/adams-shaun/gorge/state"

// MeldText is the Text of a Meld pairing event.
const MeldText = "meld"

// foldMeld folds Kind Meld into state.
func foldMeld(g *state.Game, e *Event) {
	res := g.Obj(e.Obj)
	if res == nil || res.Card == nil || len(e.IDs) != 1 {
		return
	}
	partner := g.Obj(e.IDs[0])
	if partner == nil || partner.Card == nil || partner.ID == res.ID ||
		res.Zone != state.ZExile || partner.Zone != state.ZExile ||
		res.MeldedWith != 0 || partner.MeldedWith != 0 || !validPlayer(g, e.Player) {
		return
	}
	face, ok := state.MeldResultFace(res.Card)
	if !ok || int32(face) != e.Amount {
		return
	}
	partnerID := partner.ID
	// The partner card is represented by the melded permanent from here on:
	// park its object (ZCeased has no membership list) under its own ID so
	// the split can move that exact card later. Move may not reallocate
	// g.Objs, but re-read res after it anyway (the foldMutate discipline).
	Move(g, partnerID, state.ZExile, state.ZCeased)
	res = g.Obj(e.Obj)
	res.SetFaceIdx(face)
	res.MeldedWith = partnerID
	// CR 701.42a/110.2a: the melded permanent enters under the control of
	// the player who melded it. Move's exile->battlefield placement reads
	// Controller for the battlefield zone owner, so the controller is set
	// here, while both cards are in exile, and the entry lands under it.
	res.Controller = e.Player
}

// unmeld is events.Move's CR 712.4 split: when a melded permanent's result
// object o moves anywhere but the battlefield it stops being melded, turns
// back to its front face (a meld card off the battlefield has only its front
// face's characteristics), and the parked partner card moves from ZCeased to
// the same destination, into its OWN owner's zone. The partner's zone-entry
// provenance names `from` -- the zone the melded permanent (which that card
// was half of) actually left -- so a ThisTurnEntered_<zone>_from_Battlefield
// read counts both cards.
func unmeld(g *state.Game, o *state.Object, from, to state.Zone) {
	partnerID := o.MeldedWith
	o.MeldedWith = 0
	o.SetFaceIdx(0)
	p := g.Obj(partnerID)
	if p == nil || p.Zone != state.ZCeased {
		return
	}
	Move(g, partnerID, state.ZCeased, to)
	if p = g.Obj(partnerID); p == nil || p.Zone != to {
		return
	}
	p.EnteredFrom = from
	for i := len(g.Entered) - 1; i >= 0; i-- {
		if g.Entered[i].Obj == partnerID {
			g.Entered[i].From = from
			break
		}
	}
}
