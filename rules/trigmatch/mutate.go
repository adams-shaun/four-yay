package trigmatch

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mutatesMatches implements Forge's Mode$ Mutates trigger (CR 702.140f,
// "whenever this creature mutates" / "whenever a creature you control
// mutates"). The event's Obj is the surviving permanent (the mutated pile);
// it is the object ValidCard$ is matched against, NOT the trigger's own
// source. That distinction matters for the two ValidCard$ shapes the corpus
// uses: `Card.Self` (the pile IS the scanning source -- the instance that
// mutated) and `Creature.YouCtrl` (Essence Symbiote's "whenever a creature
// you control mutates": the scanning source is the lord, the event's Obj is
// the pile its controller owns). Requiring `ev.Obj == source` here would make
// the lord shape dead -- the pile is never the lord -- while still registering
// the mode, so the card would report supported and silently do nothing.
//
// The `you` for the spec context stays the TRIGGER's controller (the lord's),
// never the pile's, so a `YouCtrl` qualifier reads from the right player. The
// Mutate event is folded before triggers run, so the pile's top card and
// merged list are already current, and triggerRemembered binds the event's Obj
// so Defined$ TriggeredCardLKICopy resolves to the pile.
func mutatesMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Mutate {
		return false
	}
	o := e.Game().Obj(ev.Obj)
	if o == nil || o.Zone != state.ZBattlefield || e.FaceDownPrintedHides(o) {
		return false
	}
	if v := strings.TrimSpace(t.ParamStr(cards.PKValidCard)); v != "" {
		ctrl := e.ControllerOf(source)
		if !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
			return false
		}
	}
	return true
}

func init() {
	registerTrigMatcher(mutatesMatches, "Mutates")
}
