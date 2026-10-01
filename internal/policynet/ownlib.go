package policynet

import (
	"fmt"

	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The honest own-library tokens (FeaturesMZOwnLib). The composition is
// view.OwnLibrary's fold of the seat's own deck list (v.OwnDeck) minus every
// own card the view shows outside the library, checked against the public
// library size -- a function of the redacted view alone, which is what makes
// this set checkpointable where the diagnostic mz-oracle's library tokens
// are not. The fold never reads PlayerView.Library.
//
//	mz|ownlibleft|<name>  copies of <name> left / library size, one row per
//	                      name with at least one copy left, in the list's
//	                      sorted name order
//	mz|ownlib|land        lands left / library size (emitted even at 0)
//	mz|ownlib|empty       1 when the library is known to be empty
//	mz|ownlib|unknown     1 when the composition is not derivable from the
//	                      view (no deck list, an own card face down out of
//	                      sight, or a count the view cannot account for);
//	                      no other ownlib row is emitted then
const (
	ownLibLeftPrefix = "mz|ownlibleft|"
	ownLibLand       = "mz|ownlib|land"
	ownLibEmpty      = "mz|ownlib|empty"
	ownLibUnknown    = "mz|ownlib|unknown"
)

// ownLibHashSuffix is the token-format part of the mz-ownlib encoder hash:
// a change to the token spellings or their values bumps ownlib-format and so
// refuses every checkpoint written under the old one.
func ownLibHashSuffix() string {
	return fmt.Sprintf("\x1fownlib-format=1\x1fpins=%d,%d,%d", hashID(ownLibLeftPrefix+"Island"), hashID(ownLibLand), hashID(ownLibUnknown))
}

func ownLibState(v view.View, seat state.PlayerID, push func(string, float32)) {
	var lc deck.LibraryComposition
	view.OwnLibrary(v, seat, &lc)
	if !lc.Known {
		push(ownLibUnknown, 1)
		return
	}
	if lc.Size == 0 {
		push(ownLibEmpty, 1)
		return
	}
	size := float32(lc.Size)
	for i, c := range lc.Counts {
		if c > 0 {
			push(ownLibLeftPrefix+lc.Row(i).Name, float32(c)/size)
		}
	}
	push(ownLibLand, float32(lc.Lands)/size)
}
