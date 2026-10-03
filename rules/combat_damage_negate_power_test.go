package rules

// stat:CombatDamageNegatePower -- Loot, the Anomaly: "If Loot's power is
// negative, he assigns combat damage as though his power were positive."
// (S:Mode$ CombatDamageNegatePower | ValidCard$ Card.Self+powerLT0.) A -2/4
// Loot assigns 2 combat damage, attacking or blocking, through the one
// assignment-amount read (combatDamageAmount); a creature with no such
// static and negative power still assigns nothing (CR 510.1a).

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestLootTheAnomalyAssignsNegatedPowerUnblocked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	loot := onBoardReadyCard(t, e, 0, mustCorpusCard(t, reg, "Loot, the Anomaly"))
	if p := e.Power(loot); p != -2 {
		t.Fatalf("precondition: Loot's power = %d, want -2", p)
	}
	e.askAttackers()
	submitAttackers(t, e, loot)
	drainCombatDamagePriority(t, e)
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("defending player life = %d, want 18 (Loot's -2 power assigned as 2)", got)
	}
}

func TestLootTheAnomalyBlockerHitsBackWithNegatedPower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Wall Breaker\nManaCost:2 R\nTypes:Creature Giant\nPT:1/5\nOracle:x\n")
	loot := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Loot, the Anomaly"))
	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, loot)
	if got := e.G.Obj(atk).Damage; got != 2 {
		t.Fatalf("attacker damage = %d, want 2 (blocking Loot's -2 power assigned as 2)", got)
	}
}

// TestNegativePowerWithoutTheStaticAssignsNothing is the control: the same
// -2/4 body with no CombatDamageNegatePower static deals no combat damage.
func TestNegativePowerWithoutTheStaticAssignsNothing(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	neg := onBoardReady(t, e, 0, "Name:Shrunk Beast\nManaCost:2 B\nTypes:Creature Beast\nPT:-2/4\nOracle:x\n")
	e.askAttackers()
	submitAttackers(t, e, neg)
	drainCombatDamagePriority(t, e)
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defending player life = %d, want 20 (negative power assigns no damage)", got)
	}
	if e.G.Obj(neg).Zone != state.ZBattlefield {
		t.Fatal("precondition: the attacker left the battlefield")
	}
}
