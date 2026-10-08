package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// staticCondition is a static line's Condition$ keyword (trimmed), the one
// vocabulary the continuous-effect, restriction and timing gates dispatch
// on. It is the PKCondition ParamCoder, so every printed node stores its code
// at load and a gate reads it through ParamCode instead of re-classifying the
// text on each layer pass. 0 is a keyword outside the vocabulary.
type staticCondition uint16

const (
	condBlank staticCondition = iota + 1
	condDelirium
	condPlayerTurn
	condNotPlayerTurn
	condMetalcraft
	condThreshold
	condHellbent
	condBlessing
	condEnduringStory
	condFerocious
	condEvolve
	condMaxSpeed
)

var staticConditionCodes = state.NewStrCodes(
	state.StrEntry[staticCondition]{Key: "", Val: condBlank},
	state.StrEntry[staticCondition]{Key: "Delirium", Val: condDelirium},
	state.StrEntry[staticCondition]{Key: "PlayerTurn", Val: condPlayerTurn},
	state.StrEntry[staticCondition]{Key: "NotPlayerTurn", Val: condNotPlayerTurn},
	state.StrEntry[staticCondition]{Key: "Metalcraft", Val: condMetalcraft},
	state.StrEntry[staticCondition]{Key: "Threshold", Val: condThreshold},
	state.StrEntry[staticCondition]{Key: "Hellbent", Val: condHellbent},
	state.StrEntry[staticCondition]{Key: "Blessing", Val: condBlessing},
	state.StrEntry[staticCondition]{Key: "EnduringStory", Val: condEnduringStory},
	state.StrEntry[staticCondition]{Key: "Ferocious", Val: condFerocious},
	state.StrEntry[staticCondition]{Key: "Evolve", Val: condEvolve},
	state.StrEntry[staticCondition]{Key: "MaxSpeed", Val: condMaxSpeed},
)

func init() {
	cards.RegisterParamCoder(cards.PKCondition, "rules.staticCondition", func(s string) uint16 {
		return uint16(staticConditionCodes.Code(strings.TrimSpace(s)))
	})
}

// staticConditionOf is the view's Condition$ code; an absent key reads as blank,
// exactly as its empty ParamStr did.
func staticConditionOf(sv staticView) staticCondition {
	c, ok := sv.ParamCode(cards.PKCondition)
	if !ok {
		return condBlank
	}
	return staticCondition(c)
}

// staticIsMaxSpeed reports whether a printed static is a "Max speed --" line
// (Condition$ MaxSpeed, CR 702.179e), read through the compiled code. The layer
// walk skips such a static's AddAbility$: rules/speed.go's maxSpeedAbilities
// offers it (kind granted), so emitting it there too would offer it twice.
func staticIsMaxSpeed(st cards.Static) bool {
	c, ok := st.ParamCode(cards.PKCondition)
	return ok && staticCondition(c) == condMaxSpeed
}
