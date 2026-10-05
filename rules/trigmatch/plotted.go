package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// BecomesPlottedMatches implements Mode$ BecomesPlotted ("When CARDNAME
// becomes plotted ...", CR 701.34; Longhorn Sharpshooter, Aloe Alchemist).
// The plot special action (rules/cast_commit.go's "plot" mode) exiles the
// card and then emits the events.AlterAttribute grant with Text "Plotted",
// folded by events/apply_player.go's foldAlterAttribute into
// Object.PlottedTurn. This matcher reads that grant's Obj -- the PLOTTED
// card itself, which sits in exile when the grant is emitted (the MoveZone
// to exile precedes it), so the carriers' TriggerZones$ Exile gate sees the
// card in the right zone. Forge's ValidCard$ names the plotted card and is
// filtered against ev.Obj.
func BecomesPlottedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if !events.IsAlterAttribute(ev, "Plotted") || ev.Amount < 1 {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher(BecomesPlottedMatches, "BecomesPlotted")
}
