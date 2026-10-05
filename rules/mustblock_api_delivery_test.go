package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
	"testing"
)

func TestMustBlockAPIBindsTargetToNamedAttacker(t *testing.T) {
	e := threeSeatEngine(t)
	src := onBoard(t, e, 0, "Name:Test Tolsimir\nTypes:Creature Elf\nPT:3/2\nSVar:Duty:DB$ MustBlock | ValidTgts$ Creature.OppCtrl | DefinedAttacker$ TriggeredAttacker | Duration$ UntilEndOfCombat\nOracle:x\n")
	attacker := onBoard(t, e, 0, "Name:Wolf\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n")
	otherAttacker := onBoard(t, e, 0, "Name:Other Wolf\nTypes:Creature Wolf\nPT:2/2\nOracle:x\n")
	blocker := onBoard(t, e, 1, "Name:Blocker\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	otherBlocker := onBoard(t, e, 1, "Name:Bystander\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	for _, id := range []state.ObjID{attacker, otherAttacker} {
		e.G.Obj(id).IsAttacking = true
		e.G.Obj(id).Attacking = 1
	}
	if !combat.CanBlock(asBoard(e), blocker, attacker) || !combat.CanBlock(asBoard(e), blocker, otherAttacker) {
		t.Fatal("precondition: blocker cannot block both attackers")
	}
	e.G.Step = state.StepDeclareBlockers
	face := e.G.Obj(src).Face()
	sa := cards.ResolveSVar(face.SVars, "Duty")
	if sa == nil {
		t.Fatal("precondition: missing MustBlock body")
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars, Remembered: []state.Target{{Obj: attacker}}, Targets: []state.Target{{Obj: blocker}}, TargetsOffered: true}, sa)
	ceFound := false
	for _, ce := range e.active() {
		if ce.Restriction == "MustBlock" && ce.MustBlockAttacker == attacker {
			ceFound = true
		}
	}
	if !ceFound {
		t.Fatal("api:MustBlock did not register named attacker")
	}
	b := asBoard(e)
	if !combat.MustBlockCandidates(b, 1)[blocker] || combat.MustBlockCandidates(b, 1)[otherBlocker] {
		t.Fatal("MustBlock duty not scoped to target")
	}
	if !combat.MustBlockPairRequired(b, blocker, attacker) {
		t.Fatal("target must block named attacker")
	}
	if combat.MustBlockPairRequired(b, blocker, otherAttacker) {
		t.Fatal("target wrongly required to block another attacker")
	}
}
