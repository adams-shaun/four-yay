package trigmatch

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// BecomesSaddledMatches implements Mode$ BecomesSaddled ("Whenever CARDNAME
// becomes saddled ...", CR 702.171; Stubborn Burrowfiend). The designation
// rides the events.AlterAttribute grant the `K:Saddle` body's
// `Attributes$ Saddled` emits (effects/misc.go's effAlterAttribute; folded
// by events/apply_player.go's foldAlterAttribute into Object.SaddledTurn), so
// this matcher reads that grant's Obj -- the MOUNT that became saddled, NOT
// the saddling creature (that is Mode$ Saddled's ValidCrew$ subject).
// Forge's ValidSaddled$ names the Mount and is filtered against ev.Obj.
//
// FirstTimeSaddled$ True (Stubborn Burrowfiend's "for the first time each
// turn") is a per-turn latch over the same grant, evaluated by scanning the
// replayed log for an earlier Saddled grant to this object since the last
// TurnChange -- the firstMarkerThisTurn contract, keyed by object rather
// than player. The current event is already logged when triggers match, so
// exactly one record means this is the first.
func BecomesSaddledMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.AlterAttribute || !strings.EqualFold(ev.Text, "Saddled") || ev.Amount < 1 {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidSaddled); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKFirstTimeSaddled)), "True") &&
		!firstSaddledThisTurn(e, ev.Obj) {
		return false
	}
	return true
}

// firstSaddledThisTurn is the BecomesSaddled FirstTimeSaddled$ latch: true
// only when the Saddled grant being matched is this object's first in the
// current turn. It walks the log backwards to the TurnChange boundary,
// counting Saddled grants to obj -- exactly one (the current event) means
// first. Replay-stable: the log is the same on a rebuilt stream, so no
// mutable counter survives a Clone and no reset hook is needed.
func firstSaddledThisTurn(e Board, obj state.ObjID) bool {
	seen := false
	for i := len(e.Log().Events) - 1; i >= 0; i-- {
		ev := e.Log().Events[i]
		if ev.Kind == events.TurnChange {
			return seen
		}
		if ev.Kind != events.AlterAttribute || ev.Amount < 1 || ev.Obj != obj ||
			!strings.EqualFold(ev.Text, "Saddled") {
			continue
		}
		if seen {
			return false
		}
		seen = true
	}
	return seen
}

func init() {
	registerTrigMatcher(BecomesSaddledMatches, "BecomesSaddled")
}
