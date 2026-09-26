package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestOnakkeWalkerTaxSeparatesPlayerAndWalkerOffers(t *testing.T) {
	e := combatEngine(t)
	oathkeeper := onBoardCard(t, e, 1, corpusCard(t, "Onakke Oathkeeper"))
	if o := e.G.Obj(oathkeeper); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Onakke Oathkeeper is not on the battlefield")
	}
	walker := onBoard(t, e, 1, targetWalkerFixture)
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || !faceHasType(o, "Planeswalker") || o.Controller != 1 {
		t.Fatalf("precondition: target is not seat 1's battlefield planeswalker: %+v", e.G.Obj(walker))
	}
	bear := onBoardReady(t, e, 0, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: attacker is not seat 0's battlefield creature: %+v", e.G.Obj(bear))
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	playerCharge := e.attackPairCharge(bear, 1, 0)
	walkerCharge := e.attackPairCharge(bear, 1, walker)
	if !playerCharge.zero() || walkerCharge.mana != 1 {
		t.Fatalf("pair charges player=%+v walker=%+v; want player free, walker {1}", playerCharge, walkerCharge)
	}
	var playerFound, walkerFound bool
	for _, of := range e.attackOffers() {
		if of.id != bear || of.def != 1 {
			continue
		}
		switch of.battle {
		case 0:
			playerFound = of.charge.zero()
		case walker:
			walkerFound = of.charge.mana == 1
		}
	}
	if !playerFound || !walkerFound {
		t.Fatalf("offer list did not separately offer correct charges: player=%v walker=%v", playerFound, walkerFound)
	}
	if got := e.attackCharge([]decision.Option{{Obj: bear, Player: 1}}); !got.zero() {
		t.Fatalf("payer price for player option = %+v, want free", got)
	}
	if got := e.attackCharge([]decision.Option{{Obj: bear, Player: 1, Battle: walker}}); got.mana != 1 {
		t.Fatalf("payer price for walker option = %+v, want {1}", got)
	}
}

func TestWalkerOnlyCantAttackBlocksWalkerPairOnly(t *testing.T) {
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Walker Avenger\nTypes:Creature Ogre\nS:Mode$ CantAttack | ValidCard$ Creature | Target$ Planeswalker.YouCtrl\nOracle:x\n")
	bear := onBoard(t, e, 1, "Name:Attacker Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	walker := onBoard(t, e, 0, targetWalkerFixture)
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || !faceHasType(o, "Planeswalker") {
		t.Fatalf("precondition: attack target is not a battlefield planeswalker: %+v", o)
	}
	if e.attackBlocked(bear, 0, 0) {
		t.Fatal("walker-only CantAttack blocked the player pair")
	}
	if !e.attackBlocked(bear, 0, walker) {
		t.Fatal("walker-only CantAttack did not block the planeswalker pair")
	}
}

func TestCantAttackUnlessCommaTargetChargesBothPairKinds(t *testing.T) {
	e := layerEngine(t)
	onBoard(t, e, 0, "Name:Comma Tax\nTypes:Creature\nS:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You,Planeswalker.YouCtrl | Cost$ 1\nOracle:x\n")
	bear := onBoard(t, e, 1, "Name:Attacker Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	walker := onBoard(t, e, 0, targetWalkerFixture)
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || !faceHasType(o, "Planeswalker") || o.Controller != 0 {
		t.Fatalf("precondition: target walker is not on seat 0 battlefield: %+v", o)
	}
	playerCharge := e.attackPairCharge(bear, 0, 0)
	walkerCharge := e.attackPairCharge(bear, 0, walker)
	if playerCharge.mana != 1 || walkerCharge.mana != 1 || playerCharge.mana == 0 || walkerCharge.mana == 0 {
		t.Fatalf("comma Target$ charges player=%+v walker=%+v; both pairs must cost {1}", playerCharge, walkerCharge)
	}
}
