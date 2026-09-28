package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// flushOrderAsks answers any pending CR 603.3b trigger_order ask with the
// offered indices in ascending order, leaving the ordered triggers on the
// stack (or in pendingTriggers) for the caller to resolve or count.
func flushOrderAsks(t *testing.T, e *Engine) {
	t.Helper()
	for n := 0; n < 50; n++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTriggerOrder {
			return
		}
		choices := make([]int, 0, d.Max)
		for i := 0; i < d.Max && i < len(d.Options); i++ {
			choices = append(choices, d.Options[i].Index)
		}
		submitChoices(t, e, choices...)
	}
	t.Fatal("trigger_order asks did not settle")
}

// driveBlock declares attacker(s) from seat 0, has seat 1 block with the given
// blockers, answers any trigger-order ask, and leaves the engine with the
// block triggers on the stack. It asserts the blockers decision really
// appeared so a setup failure cannot masquerade as "no trigger fired".
func driveBlock(t *testing.T, e *Engine, attacker state.ObjID, blockers ...state.ObjID) {
	t.Helper()
	e.askAttackers()
	submitAttackersOnly(t, e, attacker)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, blockers...)
	flushOrderAsks(t, e)
}

// resolveAll drains every queued trigger, running the SBA check a real game
// runs between priority rounds.
func resolveAll(t *testing.T, e *Engine) {
	t.Helper()
	for n := 0; n < 100; n++ {
		flushOrderAsks(t, e)
		if len(e.G.Stack) == 0 {
			return
		}
		e.resolveTop()
		e.checkStateBased()
	}
	t.Fatal("triggers did not drain")
}

// TestFlankingGrantedAttackerDebuffsBlocker pins finding 1 of the flanking
// review: a creature GRANTED flanking (a layer-6 AddKeyword$ Flanking, here
// Sidewinder Sliver's "All Sliver creatures have flanking") must fire the
// become-blocked trigger exactly like a printed K:Flanking carrier -- before
// this, the expansion only existed on the printed face and a granted flanking
// attacker debuffed nobody. Sidewinder Sliver has NO printed K:Flanking, so
// the trigger reaches the stack through the synthesized __kwFlanking: payload.
func TestFlankingGrantedAttackerDebuffsBlocker(t *testing.T) {
	t.Parallel()
	sliver := mshCorpusCard(t, "Sidewinder Sliver")
	e := combatEngine(t)
	attacker := onBoardCard(t, e, 0, sliver)
	e.G.Obj(attacker).SummonSick = false

	// Precondition: the grant is live and the card itself prints no flanking
	// (if either fails, the test would pass for the wrong reason).
	if !e.HasKeyword(attacker, "Flanking") {
		t.Fatal("Sidewinder Sliver did not grant itself flanking; setup is vacuous")
	}
	if e.G.Obj(attacker).Face().HasKeyword("Flanking") {
		t.Fatal("Sidewinder Sliver prints K:Flanking; the granted path is not exercised")
	}
	if got := e.flankingInstances(attacker); got != 1 {
		t.Fatalf("flanking instances = %d, want 1", got)
	}

	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	driveBlock(t, e, attacker, bear)
	if len(e.G.Stack) == 0 {
		t.Fatal("granted flanking queued no trigger, want one for the non-flanking blocker")
	}
	resolveAll(t, e)
	if got := e.Power(bear); got != 1 || e.Toughness(bear) != 1 {
		t.Fatalf("blocker = %d/%d, want 1/1 from the granted flanking instance", e.Power(bear), e.Toughness(bear))
	}
	if e.Power(attacker) != 1 || e.Toughness(attacker) != 1 {
		t.Fatalf("attacker = %d/%d, want untouched 1/1", e.Power(attacker), e.Toughness(attacker))
	}
}

// TestFlankingGrantedBlockerIsNotDebuffed pins finding 2: a blocker granted
// flanking by a layer-6 AddKeyword$ HAS flanking, so a printed flanking
// attacker's ValidBlocker$ Creature.withoutFlanking must not match it. Before
// the fix the blocker check read only the printed face (objectHasKeyword), so
// a granted-flanking blocker took -1/-1 it should not have. Sidewinder Sliver
// blocks as a 1/1 with granted flanking: if it were wrongly debuffed it would
// die, so its survival is the assertion.
func TestFlankingGrantedBlockerIsNotDebuffed(t *testing.T) {
	t.Parallel()
	nimbus := mshCorpusCard(t, "Knight of the Holy Nimbus")
	sliverCard := mshCorpusCard(t, "Sidewinder Sliver")
	e := combatEngine(t)
	attacker := onBoardCard(t, e, 0, nimbus)
	e.G.Obj(attacker).SummonSick = false
	blocker := onBoardCard(t, e, 1, sliverCard)

	// Precondition: the blocker has the keyword by GRANT only.
	if !e.HasKeyword(blocker, "Flanking") {
		t.Fatal("blocker did not receive granted flanking; the test cannot bind")
	}
	if e.G.Obj(blocker).Face().HasKeyword("Flanking") {
		t.Fatal("blocker prints K:Flanking; the granted-blocker case is not exercised")
	}

	driveBlock(t, e, attacker, blocker)
	if len(e.G.Stack) != 0 || len(e.pendingTriggers) != 0 {
		t.Fatalf("granted-flanking blocker queued %d triggers / %d stack objects, want 0",
			len(e.pendingTriggers), len(e.G.Stack))
	}
	resolveAll(t, e)
	if got := e.Toughness(blocker); got != 1 {
		t.Fatalf("granted-flanking blocker toughness = %d, want 1 (undebuffed)", got)
	}
	if e.G.Obj(blocker).Zone != state.ZBattlefield {
		t.Fatalf("granted-flanking blocker zone = %v, want battlefield", e.G.Obj(blocker).Zone)
	}
}

// TestFlankingSecondInstanceTriggersSeparately pins finding 3 (CR 702.25b):
// Cavalry Master's "Other creatures you control with flanking have flanking"
// gives a creature that already prints flanking a SECOND instance, and each
// triggers separately -- a 2/2 blocker takes -1/-1 twice and dies. The single
// printed expansion supplied one trigger, so the blocker used to survive at
// 1/1.
func TestFlankingSecondInstanceTriggersSeparately(t *testing.T) {
	t.Parallel()
	master := mshCorpusCard(t, "Cavalry Master")
	nimbus := mshCorpusCard(t, "Knight of the Holy Nimbus")
	e := combatEngine(t)
	onBoardCard(t, e, 0, master) // the lord, not attacking
	attacker := onBoardCard(t, e, 0, nimbus)
	e.G.Obj(attacker).SummonSick = false

	// Precondition: the lord added a second instance to the printed carrier.
	if got := e.flankingInstances(attacker); got != 2 {
		t.Fatalf("flanking instances = %d, want 2 (printed + Cavalry Master's grant)", got)
	}

	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	driveBlock(t, e, attacker, bear)
	if len(e.G.Stack) < 2 {
		t.Fatalf("second flanking instance queued %d stack objects, want at least 2", len(e.G.Stack))
	}
	resolveAll(t, e)
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("2/2 blocker with two -1/-1 instances = %v (toughness %d), want graveyard",
			e.G.Obj(bear).Zone, e.Toughness(bear))
	}
}

// TestFlankingAuraGrantedAttackerDebuffsBlocker covers the Aura carrier the
// review named (Agility: "Enchanted creature gets +1/+1 and has flanking")
// through the same derived-keyword path Sidewinder Sliver exercises, so the
// grant's SOURCE (Aura vs lord static) cannot hide a missed case.
func TestFlankingAuraGrantedAttackerDebuffsBlocker(t *testing.T) {
	t.Parallel()
	agility := mshCorpusCard(t, "Agility")
	e := combatEngine(t)
	aura := onBoardCard(t, e, 0, agility)
	attacker := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(aura).AttachedTo = attacker
	e.G.Obj(attacker).SummonSick = false
	e.activeEpoch, e.staticEpoch = -1, -1

	if !e.HasKeyword(attacker, "Flanking") {
		t.Fatal("Agility did not grant its enchanted creature flanking; setup is vacuous")
	}
	if e.G.Obj(attacker).Face().HasKeyword("Flanking") {
		t.Fatal("the attacker prints K:Flanking; the granted path is not exercised")
	}

	blocker := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	driveBlock(t, e, attacker, blocker)
	resolveAll(t, e)
	if e.G.Obj(blocker).Zone != state.ZGraveyard {
		t.Fatalf("1/1 blocker = %v (toughness %d), want graveyard from the Aura-granted flanking",
			e.G.Obj(blocker).Zone, e.Toughness(blocker))
	}
}

// TestFlankingChainedGrantsStackInstances pins the layer-walk half of CR
// 702.25b: Cavalry Master's `Affected$ Creature.Other+withFlanking+YouCtrl`
// must see a flanking instance ANOTHER layer-6 effect granted, not only a
// printed K:Flanking line. Sidewinder Sliver prints no flanking and gets its
// first instance from its own "All Sliver creatures have flanking" static;
// Cavalry Master then adds a second. Before the fix the layer walk bound only
// the types-so-far list, so `withFlanking` read the printed face alone,
// Cavalry Master's grant never matched the Sliver, and a 2/2 blocker survived
// at 1/1 on one instance instead of dying to two.
func TestFlankingChainedGrantsStackInstances(t *testing.T) {
	t.Parallel()
	sliverCard := mshCorpusCard(t, "Sidewinder Sliver")
	master := mshCorpusCard(t, "Cavalry Master")
	e := combatEngine(t)
	// Sidewinder FIRST: its grant must already be in the accumulated list
	// when Cavalry Master's Affected$ is evaluated.
	attacker := onBoardCard(t, e, 0, sliverCard)
	onBoardCard(t, e, 0, master)
	e.G.Obj(attacker).SummonSick = false

	if e.G.Obj(attacker).Face().HasKeyword("Flanking") {
		t.Fatal("Sidewinder Sliver prints K:Flanking; the chained-grant case is not exercised")
	}
	if got := e.flankingInstances(attacker); got != 2 {
		t.Fatalf("flanking instances = %d, want 2 (Sidewinder's own grant + Cavalry Master's)", got)
	}

	bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	driveBlock(t, e, attacker, bear)
	resolveAll(t, e)
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("2/2 blocker = %v (toughness %d), want graveyard from two chained flanking instances",
			e.G.Obj(bear).Zone, e.Toughness(bear))
	}
}
