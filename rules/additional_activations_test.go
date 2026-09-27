package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

const additionalActivationsStatic = "Name:ActivationGrant\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Activations | ValidCard$ Creature.YouCtrl | ValidSA$ Activated.Exhaust | MinLimit$ 2\nOracle:x\n"

const powerUpActivationsStatic = "Name:PowerUpGrant\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Activations | ValidCard$ Creature.YouCtrl | ValidSA$ Activated.PowerUp | MinLimit$ 2\nOracle:x\n"

func TestAdditionalActivationsRaisesExhaustToFiniteLimit(t *testing.T) {
	const source = "Name:ExhaustTarget\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
		"A:AB$ Pump | Cost$ 1 | Defined$ Self | Power$ 1 | Exhaust$ True | SpellDescription$ Pump.\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 17, source, additionalActivationsStatic)
	moveByName(t, e, 0, "ExhaustTarget", state.ZBattlefield)
	staticID := moveByName(t, e, 0, "ActivationGrant", state.ZBattlefield)
	exhaustBattlefield(t, e, id)
	if o := e.G.Obj(staticID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: activation-grant static must be on battlefield: %+v", o)
	}
	addMana(t, e, 0, "CCC")
	exhaustPriorityAtSeatZero(t, e)
	if e.G.Players[0].Pool.Total() < 3 {
		t.Fatalf("precondition: need payment for three activations, pool=%d", e.G.Players[0].Pool.Total())
	}
	for n := 1; n <= 2; n++ {
		opt, ok := findAbilityOption(e, id, 0)
		if !ok {
			t.Fatalf("finite bonus should allow Exhaust activation %d: %+v", n, e.Pending().Options)
		}
		submitChoices(t, e, opt.Index)
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("MinLimit$ 2 must not make Exhaust unlimited: %+v", e.Pending().Options)
	}
}

func TestAdditionalActivationsRaisesPowerUpToFiniteLimit(t *testing.T) {
	const source = "Name:PowerUpTarget\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
		"A:AB$ Pump | Cost$ 1 | Defined$ Self | Power$ 1 | PowerUp$ True | SpellDescription$ Pump.\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 18, source, powerUpActivationsStatic)
	moveByName(t, e, 0, "PowerUpTarget", state.ZBattlefield)
	staticID := moveByName(t, e, 0, "PowerUpGrant", state.ZBattlefield)
	exhaustBattlefield(t, e, id)
	if o := e.G.Obj(staticID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: activation-grant static must be on battlefield: %+v", o)
	}
	addMana(t, e, 0, "CCC")
	exhaustPriorityAtSeatZero(t, e)
	if e.G.Players[0].Pool.Total() < 3 {
		t.Fatalf("precondition: need payment for three activations, pool=%d", e.G.Players[0].Pool.Total())
	}
	for n := 1; n <= 2; n++ {
		opt, ok := findAbilityOption(e, id, 0)
		if !ok {
			t.Fatalf("finite bonus should allow PowerUp activation %d: %+v", n, e.Pending().Options)
		}
		submitChoices(t, e, opt.Index)
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("MinLimit$ 2 must not make PowerUp unlimited: %+v", e.Pending().Options)
	}
}

func TestAdditionalActivationsRaisesGameActivationLimit(t *testing.T) {
	const source = "Name:GameLimitedTarget\nManaCost:0\nTypes:Creature Beast\nPT:2/2\n" +
		"A:AB$ Pump | Cost$ 1 | Defined$ Self | Power$ 1 | Exhaust$ True | GameActivationLimit$ 1 | SpellDescription$ Pump.\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 19, source, additionalActivationsStatic)
	moveByName(t, e, 0, "GameLimitedTarget", state.ZBattlefield)
	staticID := moveByName(t, e, 0, "ActivationGrant", state.ZBattlefield)
	exhaustBattlefield(t, e, id)
	if o := e.G.Obj(staticID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: activation-grant static must be on battlefield: %+v", o)
	}
	addMana(t, e, 0, "CCC")
	exhaustPriorityAtSeatZero(t, e)
	if e.G.Players[0].Pool.Total() < 3 {
		t.Fatalf("precondition: need payment for three activations, pool=%d", e.G.Players[0].Pool.Total())
	}
	for n := 1; n <= 2; n++ {
		opt, ok := findAbilityOption(e, id, 0)
		if !ok {
			t.Fatalf("MinLimit$ 2 should raise GameActivationLimit$ 1 for activation %d: %+v", n, e.Pending().Options)
		}
		submitChoices(t, e, opt.Index)
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("GameActivationLimit$ bonus must remain finite at two: %+v", e.Pending().Options)
	}
}
