package effects

import "github.com/adams-shaun/gorge/cards"

// revealParams compiles the reveal family's imprint rider once per resolution.
type revealParams struct{ ImprintRevealed bool }

func compileRevealParams(sa *cards.SA) revealParams {
	return revealParams{ImprintRevealed: digUntilSharedImprintRevealed(sa)}
}
