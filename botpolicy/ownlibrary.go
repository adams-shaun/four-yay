package botpolicy

import (
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
)

// ownLibraryZones are the per-player zone lists the own-library fold walks
// for every player (the deciding seat's hand is added separately): exactly
// the lists view.OwnLibrary reads off the projected View.
var ownLibraryZones = [...]state.Zone{state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZCommand}

// fillOwnLibrary is the game-shaped half of the honest own-library fold
// (deck.LibraryComposition; the view-shaped half is view.OwnLibrary). It
// walks exactly the objects the deciding seat's projected View would show
// and applies view.OwnLibrary's per-card rules to them, reading each
// object's printed identity the way the projection does, so the two halves
// fold the same composition (seat/ownlibrary_test.go pins them equal on
// every decision of whole games). It never reads the library zone except
// for its public size.
//
// The mirrored projection rules: an object the View drops (no face,
// Ephemeral, phased out) is not seen; a face-down object whose looker is not
// me is the redacted CardView (SeeHidden); a face-down stack spell another
// seat controls projects no card and no owner, so it is not seen at all.
func fillOwnLibrary(g *state.Game, me state.PlayerID, m *deck.Manifest, lc *deck.LibraryComposition) {
	lc.Begin(m)
	if int(me) >= len(g.Players) {
		lc.Begin(nil)
		lc.Finish(0)
		return
	}
	see := func(o *state.Object) {
		if o.Owner != me || o.IsToken || o.IsCopy || o.Card == nil {
			return
		}
		if o.FaceDown {
			looker := o.Controller
			if o.HasMayLook {
				looker = o.MayLookPlayer
			}
			if o.Zone == state.ZPlanarDeck || looker != me {
				lc.SeeHidden()
				return
			}
		}
		if len(o.Card.Faces) > 0 && o.Card.Faces[0] != nil {
			lc.See(o.Card.Faces[0].Name)
		}
	}
	visible := func(o *state.Object) bool {
		return o != nil && o.Face() != nil && !o.Ephemeral() && !o.PhasedOut
	}
	for _, id := range g.Zone(state.ZHand, me) {
		if o := g.Obj(id); visible(o) {
			see(o)
		}
	}
	for i := range g.Players {
		p := g.Players[i].ID
		for _, z := range ownLibraryZones {
			for _, id := range g.Zone(z, p) {
				if o := g.Obj(id); visible(o) {
					see(o)
				}
			}
		}
	}
	for _, id := range g.Stack {
		o := g.Obj(id)
		if o == nil || o.Ability != nil || o.Face() == nil {
			continue
		}
		if o.FaceDown && o.Controller != me {
			continue
		}
		// The stack projection shows a copy too (its Card), which see skips.
		see(o)
	}
	lc.Finish(len(g.Zone(state.ZLibrary, me)))
}
