package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestReplayFromLogFoldsCommanderDamage(t *testing.T) {
	e, cfg := commanderGame(t, commanderDamageSeed, FormatCommander, 40, [][]string{{cmdCreature(2)}, {}})
	cmd := fieldCommander(t, e, 0, 0)
	if !contains(e.G.Zone(state.ZBattlefield, 0), cmd) {
		t.Fatal("commander must be on the battlefield before combat")
	}
	swing(t, e, cmd)

	live := e.G.Players[1].CmdDamage
	if len(live) == 0 || live[0] == 0 {
		t.Fatalf("precondition: live commander damage must be non-zero, got %v", live)
	}
	hasCmdDamage := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CmdDamage {
			hasCmdDamage = true
			break
		}
	}
	if !hasCmdDamage {
		t.Fatal("precondition: combat log must contain a CmdDamage event")
	}

	re := replayFromLog(t, cfg, e.L.Events)
	got := re.Players[1].CmdDamage
	if !reflect.DeepEqual(got, live) {
		t.Fatalf("replayFromLog CmdDamage = %v, want %v (live)", got, live)
	}
}
