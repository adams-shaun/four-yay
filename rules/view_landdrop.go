package rules

import "github.com/adams-shaun/gorge/state"

// LandDropOpen reports whether p still has a land drop this turn by the count
// alone: lands played so far is below one plus every live AdjustLandPlays
// grant (Exploration, Azusa). It is view.PlayerView.LandDropSpent's source
// (the optional capability view.landDropSpent probes); timing is not part of
// the answer, exactly as MageZero's canPlayLand ignores it.
func (e *Engine) LandDropOpen(p state.PlayerID) bool {
	return e.G.Players[p].LandsPlayed < int32(1+e.adjustLandPlays(p))
}
