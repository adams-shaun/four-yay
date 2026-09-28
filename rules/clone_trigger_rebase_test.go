package rules

// The overlap re-base path for the TRIGGER form of api:Clone GainThisAbility$
// (ticket agent-20260923T090459Z-a4d7eb3b): when a clone unit expires while a
// surviving unit whose root is a TRIGGER remains on the same permanent,
// settleExpiredClones must re-emit the survivor as Counter
// "gain-this-trigger" with its one-based trigger index, not the ability form
// (which would append a wrong/none ability). No corpus carrier stacks two
// clone units on one permanent, so this registers the two markers directly --
// the machinery, not a card route.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// triggerCloneFixture is a permanent whose printed face carries one trigger
// (the recurring-copy root) so the re-base's fold has a trigger to append.
const triggerCloneFixture = "Name:Fixture Recurring Copy\nManaCost:2\nTypes:Artifact\n" +
	"T:Mode$ Phase | Phase$ Upkeep | Execute$ CopyBody\n" +
	"SVar:CopyBody:DB$ Clone | GainThisAbility$ True\n" +
	"Oracle:x\n"

func TestSettleExpiredCloneRebasesOntoTriggerFormSurvivor(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 145, triggerCloneFixture, cloneOxSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	ox := moveSeeded(t, e, 0, cloneOxSrc, state.ZBattlefield)
	obj := e.G.Obj(id)
	if obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the fixture permanent must be on the battlefield, got %+v", obj)
	}
	if src := e.G.Obj(ox); src == nil || src.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the copy source must be on the battlefield, got %+v", src)
	}
	if f := obj.Face(); f == nil || len(f.Triggers) != 1 || f.Triggers[0].Effect == nil {
		t.Fatalf("precondition: the fixture must carry one compiled trigger, got %+v", obj.Face())
	}
	// A survivor clone unit whose root is the trigger, plus an older unit that
	// is the one expiring. Both target the same permanent; the survivor has
	// the higher timestamp (CR 613.1a). CloneSource is the separate Ox face,
	// as a real copy's source is, so the fold appends exactly one trigger.
	older := ContinuousEffect{Layer: LCopy, CloneTarget: id, CloneSource: ox,
		CloneGainThisAbility: true, CloneAbilityIndex: 1, Timestamp: 1, Controller: 0}
	survivor := ContinuousEffect{Layer: LCopy, CloneTarget: id, CloneSource: ox,
		CloneGainThisAbility: true, CloneTriggerIndex: 1, Timestamp: 2, Controller: 0}
	e.continuous = append(e.continuous, older, survivor)

	e.settleExpiredClones([]cloneExpiry{{Target: id}})

	var sawTrigger, sawAbility bool
	for _, ev := range e.L.Events {
		if ev.Kind != events.ClonePermanent {
			continue
		}
		if ev.Counter == "gain-this-trigger" {
			sawTrigger = true
			if ev.Amount != 1 {
				t.Fatalf("re-base gain-this-trigger Amount = %d, want 1", ev.Amount)
			}
		}
		if ev.Counter == "gain-this-ability" {
			sawAbility = true
		}
	}
	if !sawTrigger {
		t.Fatalf("re-base did not re-emit the trigger form; events: %+v", e.L.Events)
	}
	if sawAbility {
		t.Fatal("re-base emitted the ability form for a trigger-root survivor")
	}
	// The fold applied the survivor's copy and the trigger rode it.
	if f := e.G.Obj(id).Face(); f == nil || len(f.Triggers) != 1 {
		t.Fatalf("re-based copy must carry the survivor's one trigger, got %+v", e.G.Obj(id).Face())
	}
}
