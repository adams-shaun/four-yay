package cards

import (
	"slices"
	"testing"
)

// An API registration must not make an ability with an unimplemented selector
// appear fully supported. Only the mandatory targeted TriggeredAttacker shape
// is currently resolved into a named pair.
func TestMustBlockCoverageRejectsOtherShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		supported  bool
	}{
		{"named target", "DB$ MustBlock | ValidTgts$ Creature.OppCtrl | DefinedAttacker$ TriggeredAttacker | Duration$ UntilEndOfCombat", true},
		{"creature target", "DB$ MustBlock | ValidTgts$ Creature | DefinedAttacker$ TriggeredAttacker", true},
		{"Crashing Boars choice pool", "DB$ MustBlock | Choices$ Creature.untapped+DefenderCtrl | Chooser$ TriggeredDefendingPlayer", true},
		{"other choice selector", "DB$ MustBlock | Choices$ Creature.DefenderCtrl | Chooser$ TriggeredDefendingPlayer", false},
		{"other chooser", "DB$ MustBlock | Choices$ Creature.untapped+DefenderCtrl | Chooser$ You", false},
		{"choice pool with attacker override", "DB$ MustBlock | Choices$ Creature.untapped+DefenderCtrl | Chooser$ TriggeredDefendingPlayer | DefinedAttacker$ TriggeredAttacker", false},
		{"choice pool unknown duration", "DB$ MustBlock | Choices$ Creature.untapped+DefenderCtrl | Chooser$ TriggeredDefendingPlayer | Duration$ UntilYourNextTurn", false},
		{"player target", "DB$ MustBlock | ValidTgts$ Player | DefinedAttacker$ TriggeredAttacker | Duration$ UntilEndOfCombat", false},
		{"mixed targets", "DB$ MustBlock | ValidTgts$ Creature.OppCtrl,Player | DefinedAttacker$ TriggeredAttacker | Duration$ UntilEndOfCombat", false},
		{"any target", "DB$ MustBlock | ValidTgts$ Any | DefinedAttacker$ TriggeredAttacker", false},
		{"unknown creature qualifier", "DB$ MustBlock | ValidTgts$ Creature.UnknownSelector | DefinedAttacker$ TriggeredAttacker", false},
		{"unknown duration", "DB$ MustBlock | ValidTgts$ Creature.OppCtrl | DefinedAttacker$ TriggeredAttacker | Duration$ UntilYourNextTurn", false},
		{"implicit source attacker", "DB$ MustBlock | ValidTgts$ Creature", true},
		{"all defined blocker duties", "DB$ MustBlock | Defined$ ParentTarget | DefinedAttacker$ Valid Card.attacking | BlockAllDefined$ True", true},
		{"invalid all-defined value", "DB$ MustBlock | ValidTgts$ Creature | DefinedAttacker$ TriggeredAttacker | BlockAllDefined$ Maybe", false},
		{"optional", "DB$ MustBlock | ValidTgts$ Creature | DefinedAttacker$ TriggeredAttacker | TargetMin$ 0", true},
		{"parent target selectors", "DB$ MustBlock | Defined$ ParentTarget | DefinedAttacker$ ParentTarget", true},
		{"unknown defined selector fails closed", "DB$ MustBlock | Defined$ Bogus", false},
		{"triggered attacker LKI copy", "DB$ MustBlock | ValidTgts$ Creature | DefinedAttacker$ TriggeredAttackerLKICopy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseBytes("test.txt", []byte("Name:Test\nTypes:Sorcery\nA:"+tc.body+"\nOracle:x\n"))
			if err != nil {
				t.Fatal(err)
			}
			c.Link()
			p := c.Primitives()
			if !slices.Contains(p, "api:MustBlock") {
				t.Fatalf("precondition: no MustBlock API: %v", p)
			}
			if got := slices.Contains(p, "api:MustBlock.OtherShape"); got == tc.supported {
				t.Fatalf("unimplemented shape marker = %v, supported shape = %v: %v", got, tc.supported, p)
			}
		})
	}
}
