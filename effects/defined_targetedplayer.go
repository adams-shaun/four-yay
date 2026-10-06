package effects

import "github.com/adams-shaun/gorge/state"

// targetedPlayers returns the player targets named anywhere in the resolving
// ability chain, in first-seen order. TargetedPlayer is a player referent, not
// a controller projection of the chain's inherited object targets.
func targetedPlayers(c *Ctx) []state.Target {
	var out []state.Target
	seen := make(map[state.PlayerID]struct{})
	appendPlayers := func(targets []state.Target) {
		for _, target := range targets {
			if !target.IsPlayer {
				continue
			}
			if _, ok := seen[target.Player]; ok {
				continue
			}
			seen[target.Player] = struct{}{}
			out = append(out, target)
		}
	}
	appendPlayers(c.Targets)
	for _, targets := range c.parentLinks {
		appendPlayers(targets)
	}
	appendPlayers(c.AllTargets)
	return out
}
