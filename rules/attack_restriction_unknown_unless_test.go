package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestCantAttackNegatedUnknownUnlessDefenderStaysBlocked(t *testing.T) {
	e := attackBlockedRegressionEngine(t)
	attacker := onBoard(t, e, 0, "Name:Unknown Gate Attacker\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
		"S:Mode$ CantAttack | ValidCard$ Card.Self | UnlessDefender$ !unmodelledPredicate\nOracle:x\n")
	if o := e.G.Obj(attacker); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: attacker not on controller's battlefield: %+v", o)
	}
	statics := e.activeStatics("CantAttack")
	found := false
	for _, sv := range statics {
		if sv.Source == attacker && sv.Params["UnlessDefender"] == "!unmodelledPredicate" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("precondition: the face CantAttack static with the unsupported negated gate was not active")
	}
	if !e.attackBlocked(attacker, 1, 0) {
		t.Fatal("attacker was allowed to attack because negation converted an unknown UnlessDefender$ predicate into permission")
	}
}
