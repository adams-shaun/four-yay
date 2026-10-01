package view

import (
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
)

// OwnLibrary folds the honest remaining-library composition of seat's own
// library (deck.LibraryComposition) out of a projected View alone: seat's
// genesis list (v.OwnDeck) minus every card seat owns that the View shows
// outside the library -- seat's own hand, every battlefield, graveyard,
// exile and command zone, and every spell on the stack -- checked against
// seat's public library size.
//
// It deliberately never reads PlayerView.Library or LibraryTop: those are
// the engine's library contents, which the composition must be derivable
// WITHOUT (the Library projection shows the true contents even when a card
// went missing unseen; this fold marks that case unknown instead). v must be
// seat's own Seat projection; any other view (a spectator, another seat's)
// has no OwnDeck for seat and folds to unknown.
//
// What counts as seen, card by card (the game-shaped twin,
// botpolicy.BoardFromGameInto, applies the same rules to state.Game):
//
//   - only cards seat OWNS (a permanent it merely controls came from another
//     library; one it owns but an opponent controls came from this one);
//   - never a token or a copy (IsToken/IsCopy): neither is a card of a deck;
//   - a face-down card whose face seat may not look at (the redacted
//     CardView: FaceDown with no Name) is SeeHidden -- its identity is not
//     derivable; one seat may look at is counted by name;
//   - the name is the printed front-face name (CardName, else Name), the
//     name the manifest records;
//   - a stack entry counts only as a spell with a projected Card (an ability
//     object's Card is its source, which is counted in its own zone; a
//     face-down spell redacted for seat projects no owner at all).
func OwnLibrary(v View, seat state.PlayerID, lc *deck.LibraryComposition) {
	var manifest *deck.Manifest
	if v.Viewer == seat {
		manifest = v.OwnDeck
	}
	lc.Begin(manifest)
	librarySize := -1
	see := func(cv *CardView) {
		if cv.Owner != seat || cv.IsToken || cv.IsCopy {
			return
		}
		if cv.FaceDown && cv.Name == "" {
			lc.SeeHidden()
			return
		}
		if cv.CardName != "" {
			lc.See(cv.CardName)
		} else {
			lc.See(cv.Name)
		}
	}
	for i := range v.Players {
		p := &v.Players[i]
		if p.ID == seat {
			librarySize = p.LibrarySize
			for j := range p.Hand {
				see(&p.Hand[j])
			}
		}
		for j := range p.Battlefield {
			see(&p.Battlefield[j])
		}
		for j := range p.Graveyard {
			see(&p.Graveyard[j])
		}
		for j := range p.Exile {
			see(&p.Exile[j])
		}
		for j := range p.Command {
			see(&p.Command[j])
		}
	}
	for i := range v.Stack {
		if sv := &v.Stack[i]; sv.Kind == "spell" && sv.Card != nil {
			see(sv.Card)
		}
	}
	if librarySize < 0 {
		lc.Begin(nil)
		librarySize = 0
	}
	lc.Finish(librarySize)
}
