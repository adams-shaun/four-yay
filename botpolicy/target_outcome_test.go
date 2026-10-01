package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestTargetEffectDamageAPIControlsTargetRanking(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	bolt, ok := r.Lookup("Lightning Bolt")
	if !ok || bolt.Faces[0].SpellAbility() == nil {
		t.Fatal("corpus fixture Lightning Bolt or its spell ability missing")
	}
	boltSA := bolt.Faces[0].SpellAbility()
	if boltSA.API != "DealDamage" {
		t.Fatalf("Lightning Bolt API = %q, want DealDamage", boltSA.API)
	}
	draw, ok := r.Lookup("Divination")
	if !ok || draw.Faces[0].SpellAbility() == nil {
		t.Fatal("corpus fixture Divination or its spell ability missing")
	}
	drawSA := draw.Faces[0].SpellAbility()
	if drawSA.API == "DealDamage" || drawSA.API == "DamageAll" {
		t.Fatalf("Divination unexpectedly has damage API %q", drawSA.API)
	}
	amount := 3
	b := boardOf(def(1, 6, 6))
	b.Life.Set(0, 20)
	b.Life.Set(1, 2)
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		TargetEffect: &decision.TargetEffect{API: drawSA.API, Damage: &decision.DamageEffect{Amount: &amount}},
		Options:      []decision.Option{{Index: 0, Kind: "player", Player: 1}, {Index: 1, Kind: "permanent", Obj: 201, Player: 1}}}
	got := Decide(b, d, rng(1)).Choices
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("non-damage API carrying 3 selected %v; must not treat opponent at 2 life as lethal", got)
	}
	if got, ok := b.effectDamage(&decision.Decision{TargetEffect: &decision.TargetEffect{API: boltSA.API, Damage: &decision.DamageEffect{Amount: &amount}}}); !ok || got != 3 {
		t.Fatalf("Lightning Bolt damage = %d, %v; want 3, true", got, ok)
	}
}

func TestTargetRemovalRanksNoncreatureValueFromCorpus(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	card, ok := r.Lookup("Doom Blade")
	if !ok {
		t.Fatal("corpus fixture Doom Blade missing")
	}
	sa := card.Faces[0].SpellAbility()
	if sa == nil || sa.API != "Destroy" {
		t.Fatalf("Doom Blade spell API = %v, want Destroy", sa)
	}
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		301: {CMC: 1},
		302: {CMC: 5},
	})}
	if b.Cards.Get(301).CMC == b.Cards.Get(302).CMC {
		t.Fatal("fixture target values must differ")
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		TargetEffect: &decision.TargetEffect{API: sa.API, Removal: &decision.RemovalEffect{Kind: "destroy"}},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Obj: 301, Player: 1},
			{Index: 1, Kind: "permanent", Obj: 302, Player: 1},
		}}
	got := Decide(b, d, rng(1)).Choices
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("Destroy target choices = %v, want higher-value opponent permanent 302", got)
	}
}

func TestTargetCounterRanksOpponentSpellByValueFromCorpus(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	card, ok := r.Lookup("Counterspell")
	if !ok {
		t.Fatal("corpus fixture Counterspell missing")
	}
	sa := card.Faces[0].SpellAbility()
	if sa == nil || sa.API != "Counter" {
		t.Fatalf("Counterspell spell API = %v, want Counter", sa)
	}
	b := Board{Stack: []StackEntry{
		{ID: 401, Controller: 1, IsSpell: true, CMC: 1},
		{ID: 402, Controller: 1, IsSpell: true, CMC: 6},
	}}
	if b.Stack[0].CMC == b.Stack[1].CMC {
		t.Fatal("fixture stack spell values must differ")
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		TargetEffect: &decision.TargetEffect{API: sa.API},
		Options: []decision.Option{
			{Index: 0, Kind: "stack", Obj: 401, Player: 1},
			{Index: 1, Kind: "stack", Obj: 402, Player: 1},
		}}
	got := Decide(b, d, rng(1)).Choices
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("Counter target choices = %v, want higher-value opposing spell 402", got)
	}
}

func TestTargetSpareManaRequiresUsableSurplus(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		1: {OnBattlefield: true, Basic: true, Produces: manaProductionForTest()},
		2: {Castable: true, InstantSpeed: true, CMC: 2},
	})}
	if b.hasSpareMana() {
		t.Fatal("one source is not surplus to the two-mana instant reserve")
	}
	b.Cards.Set(3, Card{OnBattlefield: true, Basic: true, Produces: manaProductionForTest()})
	b.Cards.Set(4, Card{OnBattlefield: true, Basic: true, Produces: manaProductionForTest()})
	if !b.hasSpareMana() {
		t.Fatal("three usable mana sources exceed the two-mana reserve")
	}
	c := b.Cards.Get(4)
	c.Tapped = true
	b.Cards.Set(4, c)
	if b.hasSpareMana() {
		t.Fatal("tapped source must not count toward surplus")
	}
	c.Tapped = false
	c.Produces.Any = true
	b.Cards.Set(4, c)
	if b.hasSpareMana() {
		t.Fatal("conditional any-colour source is not guaranteed usable surplus")
	}
}
