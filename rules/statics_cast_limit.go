package rules

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// castLimitReached (a free function over the event log, not an Engine method)
// reports whether a CantBeCast restriction's
// NumLimitEachTurn$ gate lets the restriction bite for caster p: a static
// without the parameter always bites, and one carrying N bites only once p
// has put N spells on the stack this turn (High Noon's "can't cast more than
// one spell each turn" refuses the SECOND spell, never the first). Only the
// spells the restriction's own ValidCard$ names count (Deafening Silence
// limits noncreature spells only): match evaluates that spec against a spell
// already on the log, and a spec carrying an origin-zone token (which has no
// meaning for a past cast) counts every spell. exclude is an object whose own
// PutOnStack is not counted (the in-flight cast the CR 608.2b recheck
// evaluates after its push). The count is
// read from the event log like CastThisTurn, so a replay derives the same
// answer; an unparsable limit fails closed (the restriction bites).
func castLimitReached(log []events.Event, sv staticView, p state.PlayerID, exclude state.ObjID, match func(spec string, id state.ObjID) bool) bool {
	raw, ok := sv.Param(cards.PKNumLimitEachTurn)
	if !ok {
		return true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return true
	}
	spec := sv.ParamStr(cards.PKValidCard)
	filtered := len(spec) > 0 && !specCarriesCastOrigin(spec)
	n := 0
	for i := len(log) - 1; i >= 0 && n < limit; i-- {
		ev := log[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p && (exclude == 0 || ev.Obj != exclude) && (!filtered || match(spec, ev.Obj)) {
			n++
		}
	}
	return n >= limit
}
