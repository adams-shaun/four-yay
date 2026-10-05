package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func init() { effects.RegisterNonAPI("stat:PlotZone") }

type plotZoneEngine interface {
	activeStatics(mode string) []staticView
	staticGateHolds(sv staticView) bool
	matchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool
	staticSpecCtx(sv staticView) effects.SpecContext
}

// plotZoneAllowed applies only the explicit ValidCard$ scope. Unknown static
// parameters fail closed rather than turning into broader plot permissions.
func plotZoneAllowed(e plotZoneEngine, id state.ObjID) bool {
	for _, sv := range e.activeStatics("PlotZone") {
		if !e.staticGateHolds(sv) {
			continue
		}
		knownParams := 0
		for _, key := range [...]cards.ParamKey{cards.PKMode, cards.PKDescription, cards.PKValidCard} {
			if sv.HasParam(key) {
				knownParams++
			}
		}
		if len(sv.Params) != knownParams {
			continue
		}
		spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard))
		if spec != "" && e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}
