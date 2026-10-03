// The Proliferate trigger mode.
//
// Mode$ Proliferate: "Whenever you proliferate, ..." (CR 701.27, task
// trig-proliferate). Its own file because the mode's marker event, emitter
// and matcher were all added together and the per-mode split exists so
// tickets touching one mode stop colliding on a shared trigmatch_*.go.

package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// proliferateMatches implements the "whenever you proliferate" trigger family
// (Forge Mode$ Proliferate, task trig-proliferate: 6 corpus carrier files at
// the corpus pin -- Voidwing Hybrid, Ezuri Stalker of Spheres, Scheming
// Aspirant, Venser Corpse Puppet, Ichor Aberration, Contagion Dispenser; all
// six read `ValidPlayer$ You`). The causing event is the completed
// events.Proliferate marker (a pure Apply no-op api:Proliferate's
// effProliferate emits once per completed proliferate action -- one ask or
// silent no-eligible completion, never once per recipient or counter, and
// never for an ordinary counter addition, which carries no marker): Player is
// the proliferating seat (what ValidPlayer$ matches), Obj the resolving
// source permanent (what a ValidCard$ spec would match; no corpus carrier
// uses one, the surveilMatches shape). Trigger-level params the shared gates
// already read (TriggerZones$, PlayerTurn$, ActivationLimit$, the
// intervening-if) need nothing here.
func (e *Engine) proliferateMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Proliferate {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && ev.Obj != 0 &&
		!e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	if strings.EqualFold(t.Params["FirstTime"], "True") &&
		!e.firstMarkerThisTurn(events.Proliferate, ev.Player) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher((*Engine).proliferateMatches, "Proliferate")
}
