package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func ordinaryModeSource(t *testing.T, e *Engine, mode, clauses string) state.ObjID {
	t.Helper()
	return onBoard(t, e, 0, "Name:Mode witness\nTypes:Enchantment\n"+
		"T:Mode$ "+mode+" | TriggerZones$ Battlefield | "+clauses+" Execute$ Trig\n"+
		"SVar:Trig:DB$ Draw | NumCards$ 1\nOracle:x\n")
}

func assertOrdinaryTriggerPushed(t *testing.T, e *Engine) {
	t.Helper()
	e.putTriggersOnStack()
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 1 {
		t.Fatalf("TriggerPush count = %d, want 1", n)
	}
}

func TestExiledTriggerFiresOnBattlefieldToExile(t *testing.T) {
	e := layerEngine(t)
	source := ordinaryModeSource(t, e, "Exiled", "Origin$ Battlefield | ValidCard$ Creature |")
	creature := onBoard(t, e, 1, "Name:Exile target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature must be on battlefield: %+v", o)
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: trigger source must be on battlefield")
	}
	// An unrelated hand-to-graveyard move must not satisfy Exiled.
	unrelated := onBoard(t, e, 1, "Name:Unrelated card\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: unrelated, From: state.ZHand, To: state.ZGraveyard})
	e.putTriggersOnStack()
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 0 {
		t.Fatalf("unrelated Hand-to-Graveyard move produced %d TriggerPush events", n)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: creature, From: state.ZBattlefield, To: state.ZExile})
	assertOrdinaryTriggerPushed(t, e)
}

func TestChangesControllerPrintedTriggerFiresOnControlChange(t *testing.T) {
	e := layerEngine(t)
	ordinaryModeSource(t, e, "ChangesController", "ValidCard$ Creature | ValidOriginalController$ Opponent | ValidNewController$ You |")
	creature := onBoard(t, e, 1, "Name:Control target\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	o := e.G.Obj(creature)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller == 0 {
		t.Fatalf("precondition: target must be an opponent-controlled battlefield creature: %+v", o)
	}
	// A same-controller event is not a control change and must not fire.
	e.emit(events.Event{Kind: events.ControlChange, Obj: creature, Player: 1})
	e.putTriggersOnStack()
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 0 {
		t.Fatalf("same-controller ControlChange produced %d TriggerPush events", n)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: creature, Player: 0})
	assertOrdinaryTriggerPushed(t, e)
}

func TestLosesGameChosenPlayerTriggerFiresOnlyForChosenPlayer(t *testing.T) {
	e := layerEngine(t)
	source := ordinaryModeSource(t, e, "LosesGame", "ValidPlayer$ Player.Chosen |")
	e.emit(events.Event{Kind: events.Choose, Obj: source, Counter: "chosen", IDs: []state.ObjID{state.PlayerRef(2)}})
	chosen := e.G.Obj(source).Chosen
	if len(chosen) != 1 || !chosen[0].IsPlayer || chosen[0].Player != 2 {
		t.Fatalf("precondition: source must record player 2 as its chosen player: %+v", chosen)
	}
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: trigger source must be on battlefield: %+v", o)
	}

	e.emit(events.Event{Kind: events.PlayerLost, Player: 1})
	e.putTriggersOnStack()
	if n := countEvents(e, func(ev events.Event) bool { return ev.Kind == events.TriggerPush }); n != 0 {
		t.Fatalf("loss by unchosen player produced %d TriggerPush events", n)
	}
	e.emit(events.Event{Kind: events.PlayerLost, Player: 2})
	assertOrdinaryTriggerPushed(t, e)
}

func TestTurnBeginTriggerFiresOnTurnChange(t *testing.T) {
	e := layerEngine(t)
	ordinaryModeSource(t, e, "TurnBegin", "ValidPlayer$ Opponent |")
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	assertOrdinaryTriggerPushed(t, e)
}

func TestOrdinaryEventTriggerModeCarrierCensus(t *testing.T) {
	t.Parallel()
	root := "../.cards/cardsfolder"
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, root+string(filepath.Separator))] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, list string }{
		{"TurnBegin", "testdata/ordinary-modes-carriers/turn-begin.txt"},
		{"LosesGame", "testdata/ordinary-modes-carriers/loses-game.txt"},
		{"ChangesController", "testdata/ordinary-modes-carriers/changes-controller.txt"},
		{"Exiled", "testdata/ordinary-modes-carriers/exiled.txt"},
	} {
		b, err := os.ReadFile(tc.list)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Split(strings.TrimSpace(string(b)), "\n")
		got := []string{}
		for name, content := range files {
			if hasTriggerModeLine(content, tc.mode) {
				got = append(got, name)
			}
		}
		sort.Strings(got)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("Mode$ %s corpus carriers: got %d, pinned %d; got %v want %v", tc.mode, len(got), len(want), got, want)
		}
	}
}
