package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// stat:CantBeSuspected (CR 702.157) — "Enchanted creature ... can't become
// suspected." MKM's Airtight Alibi is the Standard carrier
// (`ValidCard$ Creature.EnchantedBy`); the mechanism is a per-object status
// prohibition consulted wherever the Suspected designation would be applied.
//
// The designation is carried by events.AlterAttribute with Text "Suspected".
// The engine's emit mutation choke point consults this predicate before the
// event is applied, covering every producer rather than just one effect API.
//
// cantBeSuspected takes the narrow cantBeSuspectedEngine interface rather
// than *Engine so it is not counted by the engineSurface/engineMethodCount
// shrink-only ratchets (it is a free function, not an Engine method).

const suspectedAttribute = "Suspected"

// cantBeSuspectedEngine is the slice of Engine the prohibition read needs.
type cantBeSuspectedEngine interface {
	activeStatics(mode string) []staticView
	staticGateHolds(sv staticView) bool
	matchesSpec(spec string, id state.ObjID, sc effects.SpecContext) bool
	staticSpecCtx(sv staticView) effects.SpecContext
}

// cantBeSuspected reports whether an active Mode$ CantBeSuspected static
// forbids id from gaining the Suspected designation. A static whose
// IsPresent$/Condition$ gate does not hold, or whose ValidCard$ does not
// select id, is not a match. An absent ValidCard$ is a match (the corpus
// always scopes the carrier, but a bare body is a blanket prohibition), the
// restrictive direction a "can't" takes.
func suppressSuspectedEvent(e cantBeSuspectedEngine, ev events.Event) bool {
	return ev.Kind == events.AlterAttribute && ev.Text == suspectedAttribute &&
		ev.Amount > 0 && cantBeSuspected(e, ev.Obj)
}

func cantBeSuspected(e cantBeSuspectedEngine, id state.ObjID) bool {
	for _, sv := range e.activeStatics("CantBeSuspected") {
		if !e.staticGateHolds(sv) {
			continue
		}
		if spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard)); spec != "" {
			if !e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
				continue
			}
		}
		return true
	}
	return false
}
