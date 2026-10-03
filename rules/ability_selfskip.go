package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// abilitySelfSkipTurns is the decision.Option.SelfSkipTurns fact for an
// offered activated ability: how many of the ACTIVATOR's own turns its
// resolution skips -- the NumTurns$ of every api:SkipTurn on the ability's
// linked SubAbility$ chain whose Defined$ is absent or You (effSkipTurn's
// default is the resolving controller, the activator). Lethal Vapors' "{0}:
// Destroy Lethal Vapors. You skip your next turn.", Chronatog's "{0}: +3/+3,
// you skip your next turn", Chronosavant, Magosi and Chronatog Totem carry
// the rider. A non-literal NumTurns$ reads as 1; the sum saturates at 127.
// It walks the already-linked Sub chain (no SVar resolution), so it costs a
// few pointer loads per offered ability.
func abilitySelfSkipTurns(ab *cards.SA) int8 {
	n := 0
	for sa, depth := ab, 0; sa != nil && depth < 32; sa, depth = sa.Sub, depth+1 {
		if sa.API != "SkipTurn" {
			continue
		}
		if k := effects.DefinedRefOf(sa).Kind; k != effects.RefAbsent && k != effects.RefYou {
			continue
		}
		k := 1
		if v, err := strconv.Atoi(strings.TrimSpace(sa.ParamStr(cards.PKNumTurns))); err == nil && v > 0 {
			k = v
		}
		n += k
	}
	if n > 127 {
		n = 127
	}
	return int8(n)
}
