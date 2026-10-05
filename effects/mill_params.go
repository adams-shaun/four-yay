package effects

import "github.com/adams-shaun/gorge/cards"

// millParams compiles the Mill-specific imprint rider before the zone walk.
type millParams struct{ Imprint bool }

func compileMillParams(sa *cards.SA) millParams {
	return millParams{Imprint: isTrue(sa.ParamStr(cards.PKImprint))}
}
