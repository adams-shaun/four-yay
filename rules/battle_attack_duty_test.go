package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A battle's protector is not the player defender for CR 508.1d. Attacking
// the protector's battle cannot satisfy a named MustAttack duty to that player.
func TestBattleDoesNotSatisfyNamedPlayerAttackDuty(t *testing.T) {
	t.Parallel()
	e, battle, attacker, protector := battleAttackBoard(t)
	b := e.G.Obj(battle)
	a := e.G.Obj(attacker)
	if b == nil || b.Zone != state.ZBattlefield || !b.ProtectorValid || b.Protector != protector {
		t.Fatalf("precondition: battle must be protected on the battlefield, got %+v", b)
	}
	if a == nil || a.Zone != state.ZBattlefield || a.Controller != e.G.Active {
		t.Fatalf("precondition: attacker must be an active player's battlefield creature, got %+v", a)
	}
	if !e.canAttackPair(attacker, protector) || !e.canAttackBattle(battle, e.G.Active) {
		t.Fatal("precondition: both the named-player and battle attacks must be legal")
	}

	// Establish that the battle attack is genuinely offered absent the duty.
	foundBattle := false
	for _, of := range e.attackOffers() {
		if of.id == attacker && of.battle == battle {
			foundBattle = true
		}
	}
	if !foundBattle {
		t.Fatal("precondition: battle attack is not in the offer list before adding the duty")
	}

	e.AddContinuous(ContinuousEffect{
		Source: attacker, Controller: e.G.Active, UntilEOT: true,
		Restriction:       "MustAttack",
		RestrictParams:    map[string]string{"Mode": "MustAttack", "ValidCreature": "Card.Self", "MustAttack": "RememberedPlayer"},
		RememberedPlayers: []state.PlayerID{protector},
	})
	reqs := e.attackRequirements(attacker)
	if reqs.satisfiedBy(protector) != 1 || reqs.broad || reqs.goad {
		t.Fatalf("precondition: expected exactly one named-player duty to protector %d, got %+v", protector, reqs)
	}
	if !e.mustAttackRequired(attacker) {
		t.Fatal("precondition: the named player remains a legally attackable defender, so the creature must attack")
	}

	playerPair, battlePair := false, false
	for _, of := range e.attackOffers() {
		if of.id != attacker {
			continue
		}
		if of.def == protector && of.battle == 0 {
			playerPair = true
		}
		if of.battle == battle {
			battlePair = true
		}
	}
	if !playerPair {
		t.Fatal("named-player attack pair was lost")
	}
	if battlePair {
		t.Fatal("attacking the protector's battle was treated as satisfying the named-player duty")
	}
}
