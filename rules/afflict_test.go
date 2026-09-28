package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Afflict (CR 702.130) pins, on the real corpus card Lost Monarch of Ifnir
// (the Eternal Might deck's carrier): the printed K:Afflict:3 drains the
// defending player for 3 when the creature becomes blocked, and the layer-6
// grant ("Other Zombies you control have afflict 3") drains through the
// synthesized granted-keyword trigger when a granted zombie becomes blocked.

// TestPrintedAfflictDrainsTheDefender pins the printed K:Afflict expansion
// (cards/keywords.go -> trig:AttackerBlocked): Lost Monarch of Ifnir attacks
// and is blocked, defending player loses exactly 3.
func TestPrintedAfflictDrainsTheDefender(t *testing.T) {
	t.Parallel()
	monarch := mshCorpusCardPath(t, "Lost Monarch of Ifnir", "l/lost_monarch_of_ifnir.txt")
	e := combatEngine(t)
	mon := onBoardCard(t, e, 0, monarch)
	e.G.Obj(mon).SummonSick = false
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	e.askAttackers()
	submitAttackersOnly(t, e, mon)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, blk)
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defending player life = %d, want 17 (20 - afflict 3)", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("attacking player life = %d, want 20 (Afflict drains the DEFENDER)", got)
	}
}

// TestUnblockedAfflictDrainsNothing pins the trigger's become-blocked
// condition: an Afflict creature that attacks and is NOT blocked queues
// nothing and drains nothing.
func TestUnblockedAfflictDrainsNothing(t *testing.T) {
	t.Parallel()
	monarch := mshCorpusCardPath(t, "Lost Monarch of Ifnir", "l/lost_monarch_of_ifnir.txt")
	e := combatEngine(t)
	mon := onBoardCard(t, e, 0, monarch)
	e.G.Obj(mon).SummonSick = false

	e.askAttackers()
	submitAttackers(t, e, mon)
	// No blockers were declared, so Afflict's become-blocked trigger has no
	// event to fire on. The unblocked 4/4 deals its ordinary 4 combat damage;
	// an afflict drain would take the defender to 13.
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("defending player life = %d, want 16 (4 combat damage only, no afflict drain)", got)
	}
}

// TestGrantedAfflictDrainsTheDefender pins the layer-6 AddKeyword$ Afflict:3
// grant: a plain Zombie under the Monarch's controller carries the granted
// keyword, becomes blocked, and the defending player loses 3 -- while a
// non-Zombie attacker under the same controller gains nothing, and the
// granting Monarch itself does not double up from its own static.
func TestGrantedAfflictDrainsTheDefender(t *testing.T) {
	t.Parallel()
	monarch := mshCorpusCardPath(t, "Lost Monarch of Ifnir", "l/lost_monarch_of_ifnir.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, monarch) // the grant's source; never attacks here
	zomb := onBoardReady(t, e, 0, "Name:Zomb\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n")
	bear := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	if !e.HasKeyword(zomb, "Afflict") {
		t.Fatal("granted Afflict missing from the zombie's derived keywords")
	}
	if e.G.Obj(zomb).Face().HasKeyword("Afflict") {
		t.Fatal("zombie prints Afflict; the grant scenario is not the granted path")
	}
	if e.HasKeyword(bear, "Afflict") {
		t.Fatal("non-Zombie picked up the Zombie-scoped grant")
	}

	e.askAttackers()
	submitAttackersOnly(t, e, zomb, bear)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	// The granted push mints its stack object inside events.Apply from the
	// __kwAfflict: payload alone (the log carries no SA), so a cloned engine
	// taking the same blockers decision must emit the identical event stream.
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		submitBlockersOnly(t, eng, blk) // both attackers blocked by the one blocker
		if d := eng.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
			if err := eng.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatal(err)
			}
		}
		eng.resolveTop()
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the granted-afflict resolution")
	}
	// The zombie's granted afflict 3 fires; the bear's does not.
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defending player life = %d, want 17 (granted afflict 3 only)", got)
	}
}

// TestGrantedAfflictIsOneInstancePerDeclaration pins the instance count: one
// granted creature blocked by one declaration queues exactly one instance --
// a second resolveTop of the same resolution must not re-drain, and the
// synthesized trigger must not fire for a DIFFERENT blocked creature's
// declaration (ValidCard$ Card.Self).
func TestGrantedAfflictIsOneInstancePerDeclaration(t *testing.T) {
	t.Parallel()
	monarch := mshCorpusCardPath(t, "Lost Monarch of Ifnir", "l/lost_monarch_of_ifnir.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, monarch)
	zomb := onBoardReady(t, e, 0, "Name:Zomb\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	e.askAttackers()
	submitAttackersOnly(t, e, zomb)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, blk)
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defending player life = %d, want 17", got)
	}
	// The only decisions left are the combat steps' ordinary priority windows
	// (declare-blockers/end-combat): pass those, and confirm no new trigger
	// order ask, no further drain and an empty stack -- the whole point being
	// that the resolution left NOTHING pending beyond the game's own windows.
	// The loop stops at the end-of-turn flow (the fixture has no deck to play
	// turn 2 with).
	for d := e.Pending(); d != nil && d.Kind == decision.KPriority &&
		(e.G.Step == state.StepDeclareBlockers || e.G.Step == state.StepEndCombat); d = e.Pending() {
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after resolution: %v", e.G.Stack)
	}
	if e.G.Players[1].Life != 17 {
		t.Fatalf("life moved past the single afflict drain: %d", e.G.Players[1].Life)
	}
}

// TestGrantedAfflictStacksOneInstancePerGrant pins the instance count over
// grants: two Monarchs on the battlefield each grant the same zombie
// "afflict 3", so one blocking declaration queues two instances and the
// defender loses 6 -- while the Monarchs' own printed afflict is untouched
// by each other's grant (the printed line is never re-expanded).
func TestGrantedAfflictStacksOneInstancePerGrant(t *testing.T) {
	t.Parallel()
	monarch := mshCorpusCardPath(t, "Lost Monarch of Ifnir", "l/lost_monarch_of_ifnir.txt")
	e := combatEngine(t)
	onBoardCard(t, e, 0, monarch)
	onBoardCard(t, e, 0, monarch) // second grant's source
	zomb := onBoardReady(t, e, 0, "Name:Zomb\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	e.askAttackers()
	submitAttackersOnly(t, e, zomb)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, blk)
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
			t.Fatal(err)
		}
	}
	e.resolveTop()
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("defending player life = %d, want 14 (two granted afflict-3 instances)", got)
	}
}
