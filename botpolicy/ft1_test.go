package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestFT1DamageRequiresDamageAPI(t *testing.T) {
	b := Board{}
	dmg := 3
	d := &decision.Decision{TargetEffect: &decision.TargetEffect{API: "Draw", Damage: &decision.DamageEffect{Amount: &dmg}}}
	if _, ok := b.effectDamage(d); ok {
		t.Fatal("Draw must not be classified as damage despite a supplied amount")
	}
	d.TargetEffect.API = "DealDamage"
	if got, ok := b.effectDamage(d); !ok || got != 3 {
		t.Fatalf("DealDamage = %d, %v; want 3, true", got, ok)
	}
}

func TestFT1UntappedManaSourceIsSpare(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: manaProductionForTest()},
	})}
	if !b.hasSpareMana() {
		t.Fatal("untapped battlefield mana source should count as available spare mana")
	}
	c := b.Cards.Get(1)
	c.Tapped = true
	b.Cards.Set(1, c)
	if b.hasSpareMana() {
		t.Fatal("tapped source must not count as spare mana")
	}
}

func manaProductionForTest() cards.ManaProduction {
	var p cards.ManaProduction
	p.Colour[0] = 1
	return p
}
