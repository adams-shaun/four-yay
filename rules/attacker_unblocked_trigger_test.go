package rules

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// These fixtures pin Mode$ AttackerUnblocked (task trigunblk2) end to end on
// the real corpus cards. The mode is a dedicated round-complete hook,
// checkAttackerUnblockedTriggers, that queues ONE instance per matching
// UNBLOCKED attacker -- unlike its AttackerUnblockedOnce sibling (Coveted
// Jewel, rules/coveted_jewel_trigger_test.go), whose fire cardinality is one
// per combat. Swamp Mosquito is the minimal self-source carrier whose body
// names the defending player; Stinkdrinker Bandit is the multi-attacker,
// ValidCard$-filtered carrier whose body names its own attacker.

func unblockedCorpusCard(t *testing.T, path string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", path))
	if len(ds) != 0 {
		t.Fatalf("parse %s: %v", path, ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", path, ds)
	}
	return c
}

// swampMosquitoFixture builds combatEngine with the real Swamp Mosquito under
// seat 0, ready to attack, and a seat-1 blocker candidate so the
// declare-blockers step poses a KBlockers decision both the empty intent
// (unblocked) and a real block can answer.
func swampMosquitoFixture(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := combatEngine(t)
	mosquito := onBoardCard(t, e, 0, unblockedCorpusCard(t, "s/swamp_mosquito.txt"))
	e.G.Obj(mosquito).SummonSick = false
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nK:Flying\nPT:1/1\nOracle:x\n")
	e.askAttackers()
	return e, mosquito
}

// TestSwampMosquitoUnblockedAttackPoisonsDefender drives a real unblocked
// attack to declare-blockers round completion and proves the queued trigger
// carries the defending player as its captured role, then resolves the body
// against that captured seat. Swamp Mosquito's body is
// `DB$ Poison | Defined$ TriggeredDefendingPlayer | Num$ 1`, and since
// api:Poison is registered (task poison) the resolution is observed directly
// as seat 1's POISON counter going 0 -> 1 -- the real effect, not the
// "unimplemented API Poison" Note this leaf used as a stand-in while the
// primitive was missing. The Note must now be ABSENT, which is asserted, so
// this leaf also fails if the registration is reverted. The blocked negative
// case queues nothing and places no counter.
func TestSwampMosquitoUnblockedAttackPoisonsDefender(t *testing.T) {
	t.Parallel()
	e, mosquito := swampMosquitoFixture(t)

	// Preconditions the rule reads: the attacker is on the battlefield, is a
	// declared attacker, and seat 1 is a live defending player.
	if o := e.G.Obj(mosquito); o == nil || o.Zone != state.ZBattlefield || o.IsAttacking {
		t.Fatalf("post-placement precondition: %+v", e.G.Obj(mosquito))
	}
	if e.G.Players[1].Lost {
		t.Fatal("seat 1 precondition: defending player is already lost")
	}
	if got := e.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("seat 1 starts with %d poison, want 0", got)
	}

	submitAttackersOnly(t, e, mosquito)
	drainCombatPriority(t, e)
	if !e.G.Obj(mosquito).IsAttacking {
		t.Fatal("attacker precondition: mosquito did not declare as an attacker")
	}

	// Round completion puts exactly one instance on the stack, carrying the
	// captured defending player as seat 1 and the attacking player as seat 0.
	declareUnblocked(t, e)
	if len(e.G.Stack) != 1 {
		t.Fatalf("unblocked attack put %d entries on the stack, want exactly one AttackerUnblocked trigger", len(e.G.Stack))
	}
	tc := e.triggerContexts[e.G.Stack[0]]
	if !tc.DefendingPlayer.IsPlayer || tc.DefendingPlayer.Player != 1 {
		t.Fatalf("queued trigger captured defending player %+v, want seat 1", tc.DefendingPlayer)
	}
	if !tc.AttackingPlayer.IsPlayer || tc.AttackingPlayer.Player != 0 {
		t.Fatalf("queued trigger captured attacking player %+v, want seat 0", tc.AttackingPlayer)
	}

	// Clone AFTER round completion, then resolve in lockstep.
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		if len(eng.G.Stack) != 1 {
			t.Fatalf("stack = %d, want exactly one AttackerUnblocked trigger", len(eng.G.Stack))
		}
		if got := eng.G.Players[1].Counter("POISON"); got != 0 {
			t.Fatalf("poison body ran before the trigger resolved: seat 1 has %d poison", got)
		}
		eng.resolveTop()
		if got := eng.G.Players[1].Counter("POISON"); got != 1 {
			t.Fatalf("seat 1 poison after resolution = %d, want 1 (TriggeredDefendingPlayer gets Num$ 1)", got)
		}
		if hasNote(eng, "unimplemented API Poison") {
			t.Fatal("api:Poison resolved through the unregistered fallback Note, not the registered handler")
		}
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the AttackerUnblocked resolution")
	}

	// Blocked negative case: a declared blocker leaves no unblocked attacker,
	// so the round-complete hook queues nothing and the body never runs.
	eb, mb := swampMosquitoFixture(t)
	submitAttackersOnly(t, eb, mb)
	drainCombatPriority(t, eb)
	d := eb.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	blocker := state.ObjID(0)
	for _, o := range d.Options {
		if o.Obj != 0 && o.Obj != mb {
			blocker = o.Obj
			break
		}
	}
	if blocker == 0 {
		t.Fatalf("no blocker option for the negative case: %+v", d.Options)
	}
	submitBlockersOnly(t, eb, blocker)
	if len(eb.G.Stack) != 0 {
		t.Fatalf("a blocked attack left %d stack entries, want 0", len(eb.G.Stack))
	}
	if got := eb.G.Players[1].Counter("POISON"); got != 0 {
		t.Fatalf("blocked attack reached the poison body: seat 1 has %d poison", got)
	}
}

// TestKeeperOfTresserhornUnblockedAttackDrainsDefender proves the mode's
// captured defending-player role observably drives a resolved body through a
// SECOND primitive, so the role capture is not pinned on api:Poison alone.
// Keeper of Tresserhorn carries the same ValidCard$ Card.Self shape with
// DB$ LoseLife | Defined$ TriggeredDefendingPlayer | LifeAmount$ 2, so seat
// 1's life drops by exactly 2, and only after the queued trigger resolves.
func TestKeeperOfTresserhornUnblockedAttackDrainsDefender(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	keeper := onBoardCard(t, e, 0, unblockedCorpusCard(t, "k/keeper_of_tresserhorn.txt"))
	e.G.Obj(keeper).SummonSick = false
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender precondition: life %d, want 20", got)
	}
	e.askAttackers()
	submitAttackersOnly(t, e, keeper)
	drainCombatPriority(t, e)
	declareUnblocked(t, e)
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %d, want one AttackerUnblocked trigger", len(e.G.Stack))
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life changed before resolution: %d", got)
	}
	e.resolveTop()
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("defender life = %d after resolution, want 18 (LoseLife 2 on TriggeredDefendingPlayer)", got)
	}
}

// TestStinkdrinkerBanditPumpsEachUnblockedRogue declares two unblocked Rogues
// alongside the real Stinkdrinker Bandit (ValidCard$ Rogue.YouCtrl,
// DB$ Pump | Defined$ TriggeredAttacker). Two trigger instances queue -- one
// per attacker, not the Once-style single batch -- and each resolves against
// its OWN captured attacker: each Rogue gains +2/+1 and the Bandit (a Goblin
// Rogue that did not attack) is untouched.
func TestStinkdrinkerBanditPumpsEachUnblockedRogue(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	bandit := onBoardCard(t, e, 0, unblockedCorpusCard(t, "s/stinkdrinker_bandit.txt"))
	r1 := onBoardReady(t, e, 0, "Name:Rogue One\nManaCost:0\nTypes:Creature Rogue\nPT:1/1\nOracle:x\n")
	r2 := onBoardReady(t, e, 0, "Name:Rogue Two\nManaCost:0\nTypes:Creature Rogue\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature\nPT:1/1\nOracle:x\n")

	// Precondition: the three bodies start at distinct, known sizes, so a
	// pump cannot be mistaken for the wrong creature.
	if (e.Power(r1) != 1 || e.Toughness(r1) != 1) || (e.Power(r2) != 2 || e.Toughness(r2) != 2) {
		t.Fatalf("rogue precondition: r1 %d/%d r2 %d/%d", e.Power(r1), e.Toughness(r1), e.Power(r2), e.Toughness(r2))
	}
	if e.Power(bandit) != 2 || e.Toughness(bandit) != 1 {
		t.Fatalf("bandit precondition: %d/%d, want 2/1", e.Power(bandit), e.Toughness(bandit))
	}

	e.askAttackers()
	submitAttackersOnly(t, e, r1, r2)
	drainCombatPriority(t, e)
	declareUnblocked(t, e)
	if len(e.pendingTriggers) != 2 {
		t.Fatalf("two unblocked Rogues queued %d pending triggers, want 2 (one per attacker)", len(e.pendingTriggers))
	}
	// Each queued instance must remember its OWN attacker -- a regression that
	// captured the Bandit (the source) or the same Rogue twice would pass a
	// bare count check.
	got := map[state.ObjID]bool{}
	for _, pt := range e.pendingTriggers {
		if pt.Ctx.TriggerContext.TriggerCard != pt.Ctx.Remembered[0].Obj {
			t.Fatal("queued trigger's TriggerCard does not match its Remembered attacker")
		}
		got[pt.Ctx.TriggerContext.TriggerCard] = true
	}
	if !got[r1] || !got[r2] || got[bandit] {
		t.Fatalf("queued attackers = %v, want exactly {%d,%d} and never the source %d", got, r1, r2, bandit)
	}

	e.putTriggersOnStack()
	answerTriggerOrders(t, e)
	if len(e.G.Stack) != 2 {
		t.Fatalf("trigger stack = %d, want 2", len(e.G.Stack))
	}
	for i := 0; i < 2; i++ {
		e.resolveTop()
	}

	if e.Power(r1) != 3 || e.Toughness(r1) != 2 {
		t.Fatalf("Rogue One after pumps = %d/%d, want 3/2", e.Power(r1), e.Toughness(r1))
	}
	if e.Power(r2) != 4 || e.Toughness(r2) != 3 {
		t.Fatalf("Rogue Two after pumps = %d/%d, want 4/3", e.Power(r2), e.Toughness(r2))
	}
	if e.Power(bandit) != 2 || e.Toughness(bandit) != 1 {
		t.Fatalf("Stinkdrinker Bandit after pumps = %d/%d, want 2/1 (it did not attack)", e.Power(bandit), e.Toughness(bandit))
	}
}

// TestAttackerUnblockedValidCardAndDefenderGates isolates the two gate params
// the mode reads. ValidCard$ is matched against the ATTACKER, not the trigger
// source: a non-Rogue attacker is rejected while the Rogue passes.
// ValidDefender$ is matched against that attacker's actual defender: an attack
// at a defender other than the source's controller is rejected.
func TestAttackerUnblockedValidCardAndDefenderGates(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	source := onBoard(t, e, 1, "Name:Watcher\nTypes:Creature\nPT:1/1\n"+
		"T:Mode$ AttackerUnblocked | ValidCard$ Rogue | ValidDefender$ You | TriggerZones$ Battlefield | Execute$ X | TriggerDescription$ x.\n"+
		"SVar:X:DB$ Draw | Defined$ TriggeredDefendingPlayer | NumCards$ 1\n"+
		"Oracle:x\n")
	rogue := onBoardReady(t, e, 0, "Name:Rogue\nTypes:Creature Rogue\nPT:1/1\nOracle:x\n")
	bear := onBoardReady(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Preconditions: both trigger source and would-be attackers exist where
	// the hook's battlefield walk can find them.
	for _, id := range []state.ObjID{source, rogue, bear} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d not on the battlefield", id)
		}
	}
	attacking := func(id state.ObjID, defender state.PlayerID) {
		e.G.Obj(id).IsAttacking = true
		e.G.Obj(id).Attacking = defender
	}
	clear := func() {
		for _, id := range []state.ObjID{rogue, bear} {
			e.G.Obj(id).IsAttacking = false
			e.G.Obj(id).Attacking = 0
		}
	}

	// Rogue attacking the source's controller (seat 1): both gates hold.
	attacking(rogue, 1)
	e.pendingTriggers = nil
	e.checkAttackerUnblockedTriggers()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("matching Rogue queued %d triggers, want 1", len(e.pendingTriggers))
	}

	// Non-Rogue attacking seat 1: ValidCard$ must reject it.
	clear()
	attacking(bear, 1)
	e.pendingTriggers = nil
	e.checkAttackerUnblockedTriggers()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("non-Rogue attacker queued %d triggers, want 0 (ValidCard$ must reject it)", len(e.pendingTriggers))
	}

	// Rogue attacking a different player: ValidDefender$ You must reject it.
	clear()
	attacking(rogue, 0)
	e.pendingTriggers = nil
	e.checkAttackerUnblockedTriggers()
	if len(e.pendingTriggers) != 0 {
		t.Fatal("ValidDefender$ You accepted an attack at a different defender")
	}

	// ActivationLimit$ is an action-trigger gate even though this mode's
	// matching happens at the dedicated round-complete hook. Two eligible
	// scans in one turn must reserve the first firing and reject the second.
	limited := combatEngine(t)
	limitSource := onBoard(t, limited, 1, "Name:Limited Watcher\nTypes:Creature\nPT:1/1\n"+
		"T:Mode$ AttackerUnblocked | ValidCard$ Rogue | TriggerZones$ Battlefield | ActivationLimit$ 1 | Execute$ X | TriggerDescription$ x.\n"+
		"SVar:X:DB$ Draw | Defined$ TriggeredDefendingPlayer | NumCards$ 1\n"+
		"Oracle:x\n")
	limitRogue := onBoardReady(t, limited, 0, "Name:Limited Rogue\nTypes:Creature Rogue\nPT:1/1\nOracle:x\n")
	for _, id := range []state.ObjID{limitSource, limitRogue} {
		if o := limited.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("ActivationLimit precondition: object %d not on the battlefield", id)
		}
	}
	limited.G.Obj(limitRogue).IsAttacking = true
	limited.G.Obj(limitRogue).Attacking = 1
	limited.checkAttackerUnblockedTriggers()
	if len(limited.pendingTriggers) != 1 {
		t.Fatalf("first eligible scan queued %d triggers, want 1", len(limited.pendingTriggers))
	}
	limited.pendingTriggers = nil
	limited.checkAttackerUnblockedTriggers()
	if len(limited.pendingTriggers) != 0 {
		t.Fatalf("ActivationLimit$ 1 queued %d triggers on the second eligible scan", len(limited.pendingTriggers))
	}
}
