package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSentinelSarahLyonsBattalionSurvivesAttackerLeaving pins the real
// Battalion IsPresent$ condition and NoResolvingCheck$ rider together. The
// declaration satisfies GE2; after the trigger is queued, one supporting
// attacker leaves, so a resolution-time recheck would fizzle the damage.
func TestSentinelSarahLyonsBattalionSurvivesAttackerLeaving(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	sarah := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sentinel Sarah Lyons"))
	e.G.Obj(sarah).SummonSick = false
	artifact := onBoardCard(t, e, 0, card(t, "Name:Artifact\nTypes:Artifact\nOracle:x\n"))
	if got := e.G.Obj(artifact).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition: damage-count artifact zone = %s, want battlefield", got)
	}
	if got := e.countPresent("Artifact.YouCtrl", sarah, 0); got != 1 {
		t.Fatalf("precondition: Sarah controls %d artifacts for her damage amount, want exactly 1", got)
	}
	first := onBoardReady(t, e, 0, "Name:First Ally\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")
	second := onBoardReady(t, e, 0, "Name:Second Ally\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")

	// One declaration event carries Sarah and both other attackers.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{sarah, first, second}})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("precondition: Battalion gate queued %d triggers with two other attackers, want 1", len(e.pendingTriggers))
	}
	// The IsPresent count is now only one: the source plus one other attacker
	// does not meet Creature.attacking+Other GE2. Move it before putting the
	// trigger on the stack, so the CR 603.4 resolving check sees the changed board.
	e.emit(events.Event{Kind: events.MoveZone, Obj: second, From: state.ZBattlefield, To: state.ZHand, Text: "left before resolution"})
	if got := e.G.Obj(second).Zone; got != state.ZHand {
		t.Fatalf("precondition: supporting attacker zone = %s, want hand", got)
	}
	if got := e.countPresent("Creature.attacking+Other", sarah, 0); got >= 2 {
		t.Fatalf("precondition: remaining other attackers = %d, want fewer than 2", got)
	}

	e.putTriggersOnStack()
	trigID := state.ObjID(0)
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Source == sarah && o.Ability != nil {
			trigID = id
		}
	}
	if trigID == 0 {
		t.Fatalf("precondition: Sarah's trigger was not put on the stack: %v", e.G.Stack)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("precondition: Sarah's damage trigger did not ask for a player target: %+v", d)
	}
	var target int = -1
	for _, option := range d.Options {
		if option.Player == 1 {
			target = option.Index
			break
		}
	}
	if target < 0 {
		t.Fatalf("precondition: target decision omitted opponent: %+v", d.Options)
	}
	submitChoices(t, e, target)
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 19 {
		for _, ev := range e.L.Events {
			if ev.Obj == trigID && ev.Kind == events.MoveZone && ev.To == state.ZExile {
				t.Fatalf("Sentinel Sarah Lyons trigger did not deal damage; it left the stack with %q", ev.Text)
			}
		}
		t.Fatalf("Sentinel Sarah Lyons life=%d, want 19 after the 1-damage Battalion trigger", got)
	}
	resolved := false
	for _, ev := range e.L.Events {
		if ev.Obj == trigID && ev.Kind == events.Resolve {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("precondition/fix failed: Sarah's trigger did not resolve")
	}
}
