package rules

// approx combatrestriction1 (the walker Target$ half): a combat-restriction
// Target$ list's Planeswalker.<player-spec> clause is read by
// restrictionPlayerTargetMatches (rules/layers.go), which both enforcement
// call sites consume — attackBlocked (the CantAttack read behind the offer
// filter and validateAttackers) and attackPairCharge (the CantAttackUnless
// pricing read). These tests bind the walker clause to BOTH call sites:
//
//   - the corpus carrier (Vow of Lightning's
//     `Target$ You,Planeswalker.YouCtrl`) through attackBlocked with the
//     static live on the table;
//   - a walker-ONLY Target$ through attackBlocked, where the walker clause is
//     the sole operative half — the shape the You half of the Vow's comma
//     list cannot discriminate (with the Vow, `You` already blocks the vow's
//     controller, so deleting the walker matcher changes no attackBlocked
//     verdict a Vow-only test could see);
//   - the real corpus carrier of the walker-only shape (Onakke Oathkeeper's
//     `Target$ Planeswalker.YouCtrl | Cost$ 1`) through attackPairCharge.
//
// Each test asserts its own precondition: the carrier is on the battlefield
// with the Vow attached where the static read needs it, the walker is a
// battlefield planeswalker under the clause's controller, and a
// non-planeswalker permanent does not satisfy the clause.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// targetWalkerFixture is the synthetic battlefield planeswalker the tests
// place under the restriction's controller.
const targetWalkerFixture = "Name:Target Walker\nTypes:Planeswalker\nPT:0\nOracle:x\n"

// TestCantAttackTargetPlaneswalkerController drives Vow of Lightning's real
// corpus static
// `S:Mode$ CantAttack | ValidCard$ Creature.EnchantedBy | Target$ You,Planeswalker.YouCtrl`
// end to end: the walker clause matches the vow controller's battlefield
// planeswalker through restrictionPlayerTargetMatches, and with the vow cast
// and attached the (attacker, vow-controller) pair is blocked through
// attackBlocked while the uninvolved defender is not.
func TestCantAttackTargetPlaneswalkerController(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	vow, ok := reg.Lookup("Vow of Lightning")
	if !ok {
		t.Fatal("corpus missing Vow of Lightning")
	}
	var target, walkerTarget string
	for _, line := range vow.Faces[0].Statics {
		if line.Mode != "CantAttack" {
			continue
		}
		target = line.Params["Target"]
		for _, part := range strings.Split(target, ",") {
			if strings.HasPrefix(strings.TrimSpace(part), "Planeswalker.") {
				walkerTarget = strings.TrimSpace(part)
				break
			}
		}
		break
	}
	if walkerTarget != "Planeswalker.YouCtrl" {
		t.Fatalf("precondition: Vow of Lightning Target$ = %q, want its corpus Planeswalker.YouCtrl clause", target)
	}
	walker := card(t, targetWalkerFixture)
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 6130,
		[][]*cards.Card{{vow}, nil, nil},
		[][]*cards.Card{{walker}, {bear}, nil})
	walkerID := bearOnBoard(t, e, 0, walker)
	walkerObj := e.G.Obj(walkerID)
	if walkerObj == nil || walkerObj.Zone != state.ZBattlefield || !faceHasType(walkerObj, "Planeswalker") || walkerObj.Controller != 0 {
		t.Fatalf("precondition: Target Walker is not seat 0's battlefield planeswalker: %+v", walkerObj)
	}
	bear1 := bearOnBoard(t, e, 1, bear)
	if e.G.Obj(bear1).Controller == walkerObj.Controller {
		t.Fatal("precondition: attacker and planeswalker controller must differ")
	}
	if !restrictionPlayerTargetMatches(e.G, walkerTarget, 0, 0, 0, nil, walkerID) {
		t.Fatal("Planeswalker.YouCtrl did not match the controller's battlefield planeswalker")
	}
	if restrictionPlayerTargetMatches(e.G, walkerTarget, 1, 0, 0, nil, walkerID) {
		t.Fatal("Planeswalker.YouCtrl matched a defender who controls no such planeswalker")
	}

	// Integration: cast the vow (2R) onto seat 1's bear so the CantAttack
	// static goes live, then measure the pair read every consumer shares.
	vowID := findAndMoveToHand(t, e, 0, "Vow of Lightning")
	addMana(t, e, 0, "CCRR")
	castFromPriority(t, e, vowID)
	answerKTarget(t, e, bear1)
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(vowID).AttachedTo != bear1 {
		t.Fatalf("precondition: vow AttachedTo = %d, want the bear %d", e.G.Obj(vowID).AttachedTo, bear1)
	}
	if !e.attackBlocked(bear1, 0, 0) {
		t.Fatal("with the vow live the enchanted bear is not blocked from attacking the vow controller, whose planeswalker the walker clause names")
	}
	if e.attackBlocked(bear1, 2, 0) {
		t.Fatal("the enchanted bear was blocked from attacking the uninvolved defender")
	}
	replayCheck(t, e, cfg)
}

// TestCantAttackWalkerOnlyTargetBindsThroughAttackBlocked pins the walker
// clause as LOAD-BEARING through attackBlocked itself. No corpus CantAttack
// carrier spells a walker-only Target$ (every one pairs it with `You`, which
// already blocks the static's controller), so the discriminator is a
// synthetic face, the same substitution attack_restriction_conditions_test.go
// makes for the uncarried Condition$ shape:
// `S:Mode$ CantAttack | ValidCard$ Creature | Target$ Planeswalker.YouCtrl`.
// With only a non-planeswalker permanent on the static controller's
// battlefield the clause matches nobody and the attack is legal; the
// planeswalker's presence is what blocks the pair, and a defender who
// controls no such walker stays unblocked.
func TestCantAttackWalkerOnlyTargetBindsThroughAttackBlocked(t *testing.T) {
	e := layerEngine(t)
	avenger := onBoard(t, e, 0, "Name:Walker Avenger\nManaCost:1 W\nTypes:Creature Ogre Spirit\n"+
		"S:Mode$ CantAttack | ValidCard$ Creature | Target$ Planeswalker.YouCtrl"+
		" | Description$ Creatures can't attack planeswalkers you control.\nOracle:x\n")
	if o := e.G.Obj(avenger); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the walker-only carrier is not on the battlefield")
	}
	bear1 := onBoard(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(bear1); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the attacking bear is not seat 1's battlefield creature: %+v", e.G.Obj(bear1))
	}
	// A non-planeswalker permanent under the clause's controller must not
	// satisfy Planeswalker.YouCtrl.
	crowd := onBoard(t, e, 0, "Name:Crowd Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(crowd); o == nil || o.Zone != state.ZBattlefield || faceHasType(o, "Planeswalker") {
		t.Fatalf("precondition: the discriminator permanent is not a non-planeswalker: %+v", e.G.Obj(crowd))
	}
	if e.attackBlocked(bear1, 0, 0) {
		t.Fatal("seat 1's bear was blocked although the clause's controller controls no planeswalker")
	}
	walker := onBoard(t, e, 0, targetWalkerFixture)
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || !faceHasType(o, "Planeswalker") || o.Controller != 0 {
		t.Fatalf("precondition: Target Walker is not seat 0's battlefield planeswalker: %+v", e.G.Obj(walker))
	}
	if e.attackBlocked(bear1, 0, 0) {
		t.Fatal("walker-only clause blocked a player attack")
	}
	if !e.attackBlocked(bear1, 0, walker) {
		t.Fatal("the walker clause did not block an attack at the planeswalker")
	}
	if e.attackBlocked(bear1, 1, 0) {
		t.Fatal("seat 1's bear was blocked from the defender whose battlefield holds no clause-matching planeswalker")
	}
}

// TestCantAttackUnlessWalkerTargetPricesAttackCharge drives the REAL corpus
// carrier of the walker-only shape, Onakke Oathkeeper's
// `S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ Planeswalker.YouCtrl | Cost$ 1`,
// through attackPairCharge — the CantAttackUnless pricing read the offer
// list, validator and payer share. Without a planeswalker under the
// oathkeeper's controller the clause matches nobody and nothing is charged;
// with one, attacking the oathkeeper's controller costs {1}; the defender
// who controls no such walker is charged nothing.
func TestCantAttackUnlessWalkerTargetPricesAttackCharge(t *testing.T) {
	e := layerEngine(t)
	oathkeeper := onBoardCard(t, e, 0, corpusCard(t, "Onakke Oathkeeper"))
	if o := e.G.Obj(oathkeeper); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Onakke Oathkeeper is not on the battlefield")
	}
	bear1 := onBoard(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if o := e.G.Obj(bear1); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the attacking bear is not seat 1's battlefield creature: %+v", e.G.Obj(bear1))
	}
	if got := e.attackPairCharge(bear1, 0, 0).mana; got != 0 {
		t.Fatalf("precondition: the pair was charged %d before the oathkeeper's controller controlled a planeswalker, want 0", got)
	}
	walker := onBoard(t, e, 0, targetWalkerFixture)
	if o := e.G.Obj(walker); o == nil || o.Zone != state.ZBattlefield || !faceHasType(o, "Planeswalker") || o.Controller != 0 {
		t.Fatalf("precondition: Target Walker is not seat 0's battlefield planeswalker: %+v", e.G.Obj(walker))
	}
	if got := e.attackPairCharge(bear1, 0, walker).mana; got != 1 {
		t.Fatalf("attacking the planeswalker was charged %d, want the static's Cost$ 1 via its walker-only Target$", got)
	}
	if got := e.attackPairCharge(bear1, 1, 0).mana; got != 0 {
		t.Fatalf("the defender whose battlefield holds no clause-matching planeswalker was charged %d, want 0", got)
	}
}
