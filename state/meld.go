package state

import "github.com/adams-shaun/gorge/cards"

// Meld (CR 701.42, 712.4) is represented by ONE battlefield object backed by
// two cards:
//
//   - the RESULT object is the meld card whose Card carries the meld-result
//     face (Forge's AlternateMode:Meld card with an ALTERNATE section: Gisela,
//     the Broken Blade carries Brisela, Voice of Nightmares). While melded its
//     FaceIdx names that face, so every characteristic read (Face(), the layer
//     walk, keywords, abilities) sees the melded permanent's characteristics.
//   - the PARTNER object is the other meld card (Bruna, the Fading Light). It
//     keeps its own ObjID, Card and Owner, parked in ZCeased (no membership
//     list, so no zone scan sees it as a second permanent) and named by the
//     result's MeldedWith.
//
// When the melded permanent leaves the battlefield, events.Move splits it:
// the result object turns back to its front face and the partner moves from
// ZCeased to the same destination zone, each card landing in its OWN owner's
// zone. Both halves are written only inside events.Apply (the Meld fold and
// Move), so a log-only replay rebuilds the identical pair.

// MeldResultFace reports the face index of c's meld-result face: the
// AlternateMode:Meld card that carries the melded permanent's face as its
// second face. A meld card without an ALTERNATE section (the pair's other
// half) or any non-meld card reports false.
func MeldResultFace(c *cards.Card) (uint8, bool) {
	if c == nil || c.AlternateMode != "Meld" || len(c.Faces) < 2 || c.Faces[1] == nil {
		return 0, false
	}
	return 1, true
}

// IsMeldCard reports whether c is half of a meld pair (AlternateMode:Meld),
// whichever half carries the result face.
func IsMeldCard(c *cards.Card) bool {
	return c != nil && c.AlternateMode == "Meld"
}

// Melded reports whether o is a melded permanent's result object.
func (o *Object) Melded() bool { return o != nil && o.MeldedWith != 0 }

// MeldComponents returns the two card objects a melded permanent is
// represented by, result object first. ok is false when o is not melded.
func (o *Object) MeldComponents() (result, partner ObjID, ok bool) {
	if !o.Melded() {
		return 0, 0, false
	}
	return o.ID, o.MeldedWith, true
}

// MeldOptions are the entry riders a meld instruction may carry: CR 701.42a
// puts the melded permanent onto the battlefield, and Mishra, Claimed by
// Gix's meld has it enter "tapped and attacking". Attacking requires a
// defending player (Defender); DefenderBattle names a planeswalker or battle
// it attacks instead of the player, 0 for the player. The three names are
// the meld instruction's Name$/Primary$/Secondary$ (Forge's MeldEffect): when
// non-empty, the result face must carry ResultName and the two cards' front
// faces must be PrimaryName and SecondaryName (in either order), or the pair
// does not meld (CR 701.42c). The card IR does not keep MeldPair:, so these
// names are the only pairing evidence a caller can supply.
type MeldOptions struct {
	Tapped         bool
	Attacking      bool
	Defender       PlayerID
	DefenderBattle ObjID
	ResultName     string
	PrimaryName    string
	SecondaryName  string
}
