package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 601.2c / CR 603.3d: a SubAbility$ link's announced target is a real
// targeting. Both announcement paths -- the cast flow's chain pre-ask
// (rules/cast_targets.go) and the CR 603.3d trigger placement ask
// (rules/trigger_subtargets.go) -- must record it through the same
// TargetsChosen fold the root target uses, or ward (CR 702.21a) and
// "becomes the target" triggers never see it.

// triggerChainWithTargetSource builds a triggered ability whose chain link
// targets an opponent's creature.
func triggerChainWithTargetSource(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	src := onBoard(t, e, 0,
		"Name:Chain Trigger\nTypes:Creature\nPT:1/1\n"+
			"T:Mode$ ChangesZone | ValidCard$ Card.Self | Destination$ Battlefield | Execute$ Root | TriggerDescription$ x\n"+
			"SVar:Root:DB$ Pump | SubAbility$ TargetOne\n"+
			"SVar:TargetOne:DB$ Pump | ValidTgts$ Creature.OppCtrl\n"+
			"Oracle:x\n")
	o := e.G.Obj(src)
	if o == nil || o.Face() == nil || len(o.Face().Triggers) != 1 {
		t.Fatalf("precondition: printed trigger compiled, got %+v", o)
	}
	root := o.Face().Triggers[0].Effect
	if root == nil || root.Sub == nil || root.Sub.ParamStr(cards.PKValidTgts) != "Creature.OppCtrl" {
		t.Fatalf("precondition: trigger has a targeting chain link: root=%+v sub=%+v", root, root.Sub)
	}
	return e, src
}

// answerPlacementSubAsk pushes the trigger, poses the placement chain-link
// ask, and answers it with target. It returns the chain-link decision so the
// caller can assert its shape.
func answerPlacementSubAsk(t *testing.T, e *Engine, src, target state.ObjID) {
	t.Helper()
	e.pushTrigger(pendingTrigger{Source: src, Controller: 0, Idx: 0,
		SA: e.G.Obj(src).Face().Triggers[0].Effect})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("chain target ask = %+v, want placement KTarget trig_sub before priority", d)
	}
	idx := indexOfObjOption(d, target)
	if idx < 0 {
		t.Fatalf("chain target options = %+v, want target %d", d.Options, target)
	}
	submitChoices(t, e, idx)
}

// TestTriggerSubTargetRecordsWardTargeting: answering the placement trig_sub
// ask with a warded creature must emit the link's TargetsChosen
// (SubTargetNotice) so ward triggers, exactly as the root's answer does.
func TestTriggerSubTargetRecordsWardTargeting(t *testing.T) {
	t.Parallel()
	e, src := triggerChainWithTargetSource(t)
	warded := onBoard(t, e, 1, "Name:Warded Bear\nTypes:Creature\nPT:2/2\nK:Ward:2\nOracle:x\n")
	if o := e.G.Obj(warded); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: warded creature is not on the battlefield: %+v", o)
	}
	answerPlacementSubAsk(t, e, src, warded)

	// The link's answer is a targeting: it must be recorded on the trigger's
	// stack object as a SubTargets entry.
	var rec []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.TargetsChosen && ev.Text == events.SubTargetNotice &&
			len(ev.IDs) == 1 && ev.IDs[0] == warded {
			rec = append(rec, ev)
		}
	}
	if len(rec) != 1 {
		t.Fatalf("placement chain link recorded %d sub-target events for the warded creature, want 1: %+v", len(rec), rec)
	}
	// And ward must have fired for it (a TriggerPush whose Obj is the warded
	// permanent; the printed K:Ward face trigger pushes an ordinary
	// TriggerPush, not a granted KeywordTriggerPush).
	if !triggerPushedFor(e, warded) {
		t.Fatalf("ward did not trigger for a placement chain-link target: events=%v", e.L.Events)
	}
}

// TestTriggerSubTargetRecordsBecomesTargetTrigger: the same recording must
// fire a "Whenever CARDNAME becomes the target" trigger on a chain link.
func TestTriggerSubTargetRecordsBecomesTargetTrigger(t *testing.T) {
	t.Parallel()
	e, src := triggerChainWithTargetSource(t)
	watcher := onBoard(t, e, 1,
		"Name:Rotpriest Watcher\nTypes:Creature\nPT:1/2\n"+
			"T:Mode$ BecomesTarget | ValidTarget$ Card.Self | TriggerZones$ Battlefield | Execute$ TrigPoison | TriggerDescription$ x\n"+
			"SVar:TrigPoison:DB$ Poison | ValidTgts$ Opponent | Num$ 1\n"+
			"Oracle:x\n")
	o := e.G.Obj(watcher)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || len(o.Face().Triggers) != 1 {
		t.Fatalf("precondition: watcher has no printed BecomesTarget trigger: %+v", o)
	}
	answerPlacementSubAsk(t, e, src, watcher)

	if !triggerPushedFor(e, watcher) {
		t.Fatalf("'becomes the target' trigger did not fire for a placement chain-link target; pending=%+v events=%v",
			e.pendingTriggers, e.L.Events)
	}
}

// TestCastSubTargetRecordsWardTargeting is the cast path's twin: Bite Down's
// announced sub-link (the creature you don't control) hitting a warded
// creature must fire ward. The cast path already routes through
// recordSubTargets (commit 3f9a1d9fa); this pins it so a regression that
// drops the recording fails here.
func TestCastSubTargetRecordsWardTargeting(t *testing.T) {
	t.Parallel()
	e, cfg, mine, theirs := cr601Board(t, 9911,
		map[string]state.Zone{"Bite Down": state.ZHand, "Centaur Courser": state.ZBattlefield},
		map[string]state.Zone{"Kwia Vigorbloom": state.ZBattlefield})
	spell := mine["Bite Down"]
	courser := mine["Centaur Courser"]
	kwia := theirs["Kwia Vigorbloom"]
	if o := e.G.Obj(kwia); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: Kwia Vigorbloom is not the opponent's battlefield permanent: %+v", o)
	}
	if !derivedHasKeyword(e.Derived(kwia), "Ward") {
		t.Fatalf("precondition: Kwia Vigorbloom has no ward keyword: %v", e.Derived(kwia).Keywords)
	}
	addMana(t, e, 0, "G1")
	edrSeatZeroPriority(t, e)
	cr601Cast(t, e, spell, "")
	answerTargetAsk(t, e, []state.ObjID{courser})
	answerCastSubObj(t, e, kwia)

	if !triggerPushedFor(e, kwia) {
		t.Fatalf("ward did not trigger for a cast chain-link target on Kwia Vigorbloom: events=%v", e.L.Events)
	}
	replayCheck(t, e, cfg)
}

// triggerPushedFor reports whether a trigger whose source is src was pushed
// into the log (printed ward and BecomesTarget triggers both push an ordinary
// TriggerPush carrying the source id).
func triggerPushedFor(e *Engine, src state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == src {
			return true
		}
	}
	return false
}

func derivedHasKeyword(d Derived, kw string) bool {
	for _, k := range d.Keywords {
		if k == kw || strings.HasPrefix(k, kw+":") {
			return true
		}
	}
	return false
}
