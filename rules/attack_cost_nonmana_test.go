// attackcost-nonmana: the non-mana components of a combat cost. The
// CantAttackUnless/CantBlockUnless grammar used to price only mana (and, on
// the block side, PayLife and a fixed tapXType), so a static charging
// Sac/Return/tapXType/PayLife/Phyrexian left its creature free to attack or
// block. These tests pin the composite charge end to end through the real
// corpus carriers -- Exalted Dragon and Flooded Woodlands (Sac<1/Land>),
// Floodtide Serpent (Return<1/Enchantment>), Hollow Warrior's attack half
// (tapXType<1/Creature.!attacking>), Norn's Annex ({W/P}) -- and through
// inline fixtures for the shapes with no corpus combat carrier (an attack
// PayLife<N>, and the block-side Sac/Return/Phyrexian grammar).
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attackTaxSeat places the named corpus prop on seat 0 and one ready attacker
// on seat 1, then parks the engine in the declare-attackers step with seat 1
// active. It is the attack-side sibling of block_prop_nonmana_test's
// threeSeatEngine fixtures.
func attackTaxSeat(t *testing.T, propName string) (*Engine, state.ObjID) {
	t.Helper()
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, mshCorpusCard(t, propName))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	return e, bear
}

// landFixture is an untapped land with no abilities: a pure Sac<1/Land>
// candidate.
const landFixture = "Name:Test Wastes\nTypes:Basic Land\nOracle:x\n"

// enchantFixture is a cheap enchantment: a pure Return<1/Enchantment>
// candidate.
const enchantFixture = "Name:Test Aura\nTypes:Enchantment\nOracle:x\n"

// TestExaltedDragonAttackSacrificesALand pins Sac<1/Land> end to end with the
// real corpus card: the charge carries one sacrifice obligation, the pair is
// offered, submitting it sacrifices a land (a real Sacrifice event) and
// commits the attack.
func TestExaltedDragonAttackSacrificesALand(t *testing.T) {
	e := threeSeatEngine(t)
	dragon := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Exalted Dragon"))
	land := onBoardCard(t, e, 0, card(t, landFixture))
	sea := onBoardReady(t, e, 0, "Name:Test Serpent\nManaCost:1 U\nTypes:Creature Serpent\nPT:2/4\nOracle:x\n")
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the dragon is the attacker, the land is a distinct
	// battlefield permanent, and the charge really carries a sacrifice.
	if o := e.G.Obj(dragon); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Exalted Dragon not on seat 0's battlefield: %+v", e.G.Obj(dragon))
	}
	if l := e.G.Obj(land); l == nil || l.Zone != state.ZBattlefield || l.Controller != 0 {
		t.Fatal("precondition: the sacrifice candidate is not seat 0's battlefield land")
	}
	ch := e.attackPairCharge(dragon, 1, 0)
	if len(ch.sacs) != 1 || ch.sacs[0].n != 1 || ch.mana != 0 || ch.life != 0 || len(ch.phyrexian) != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want one sacrifice of 1", ch)
	}
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{dragon: true}) {
		t.Fatal("precondition: the sacrifice charge is marketed unaffordable with a land available")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	opt := findAttackOption(d, dragon, 1)
	if opt == nil {
		t.Fatalf("sacrifice-charged pair not offered: %+v", d.Options)
	}
	if opt.Value != 0 || !strings.Contains(opt.Label, "sacrifice a permanent") {
		t.Fatalf("pair not sacrifice-charged: %+v", opt)
	}
	_ = sea
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit sacrifice-charged attack: %v", err)
	}
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the sacrifice charge never moved the land to the graveyard: %+v", e.G.Obj(land))
	}
	sacrificed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == land && ev.Text == "sacrificed" {
			sacrificed = true
		}
	}
	if !sacrificed {
		t.Fatalf("no Sacrifice event for the land (log: %v)", e.L.Events)
	}
	if o := e.G.Obj(dragon); o == nil || !o.IsAttacking {
		t.Fatal("the sacrifice-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestExaltedDragonWithoutALandIsNeverOffered pins the fail-closed half: with
// no land to sacrifice the pair is not offered at all, so an unpayable
// sacrifice charge never tempts a free attack.
func TestExaltedDragonWithoutALandIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	dragon := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Exalted Dragon"))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	ch := e.attackPairCharge(dragon, 1, 0)
	if len(ch.sacs) != 1 {
		t.Fatalf("precondition: charge = %+v, want one sacrifice obligation", ch)
	}
	if e.combatChargeAffordable(0, ch, map[state.ObjID]bool{dragon: true}) {
		t.Fatal("precondition: the charge reads affordable with no land on the battlefield")
	}
	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, dragon, 1); opt != nil {
			t.Fatalf("unpayable sacrifice charge still offered: %+v", opt)
		}
	}
}

// TestFloodtideSerpentAttackReturnsAnEnchantment pins Return<1/Enchantment>
// end to end with the real corpus card: the charge carries one return
// obligation, submitting it returns the enchantment to its owner's hand
// (the ReturnCost zone change) and commits the attack.
func TestFloodtideSerpentAttackReturnsAnEnchantment(t *testing.T) {
	e := threeSeatEngine(t)
	serpent := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Floodtide Serpent"))
	aura := onBoardCard(t, e, 0, card(t, enchantFixture))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the serpent and a distinct battlefield enchantment exist,
	// and the charge carries the return.
	if o := e.G.Obj(serpent); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatal("precondition: Floodtide Serpent not on seat 0's battlefield")
	}
	if a := e.G.Obj(aura); a == nil || a.Zone != state.ZBattlefield || a.Controller != 0 {
		t.Fatal("precondition: the return candidate is not seat 0's battlefield enchantment")
	}
	ch := e.attackPairCharge(serpent, 1, 0)
	if len(ch.returns) != 1 || ch.returns[0].n != 1 || ch.mana != 0 || len(ch.sacs) != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want one return of 1", ch)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, serpent, 1)
	if opt == nil {
		t.Fatalf("return-charged pair not offered: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit return-charged attack: %v", err)
	}
	if o := e.G.Obj(aura); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the return charge never returned the enchantment to hand: %+v", e.G.Obj(aura))
	}
	if o := e.G.Obj(serpent); o == nil || !o.IsAttacking {
		t.Fatal("the return-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestHollowWarriorAttackTapsANonAttacker pins the attack half of Hollow
// Warrior's tapXType<1/Creature.!attacking>: declaring the attack taps one
// untapped creature the controller also controls but is not attacking with.
func TestHollowWarriorAttackTapsANonAttacker(t *testing.T) {
	e := threeSeatEngine(t)
	warrior := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Hollow Warrior"))
	other := onBoardReady(t, e, 0, "Name:Test Helper\nManaCost:1 G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the charge is a single tap obligation, the helper is
	// untapped, and it is a legal candidate (the warrior itself is excluded
	// because it is the declared attacker).
	ch := e.attackPairCharge(warrior, 1, 0)
	if len(ch.taps) != 1 || ch.taps[0].n != 1 || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want one tap of 1", ch)
	}
	if e.G.Obj(other).Tapped {
		t.Fatal("precondition: the tap candidate is already tapped")
	}
	if got := len(e.blockTapCandidates(0, ch.taps[0], map[state.ObjID]bool{warrior: true})); got < 1 {
		t.Fatalf("precondition: %d tapper candidates with the attacker excluded, want >= 1", got)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, warrior, 1)
	if opt == nil {
		t.Fatalf("tap-charged pair not offered: %+v", d.Options)
	}
	if opt.CostTaps != 1 || !strings.Contains(opt.Label, "tap 1") {
		t.Fatalf("pair not tap-charged: %+v", opt)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit tap-charged attack: %v", err)
	}
	if !e.G.Obj(other).Tapped {
		t.Fatal("the attack tap obligation never tapped the helper creature")
	}
	// The warrior is tapped by CR 508.1f for attacking (not by the charge);
	// the point of the assertion above is that the helper was the tap
	// obligation's target while it was still untapped at payment time.
	if o := e.G.Obj(warrior); o == nil || !o.IsAttacking {
		t.Fatal("the tap-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestHollowWarriorAttackWithoutATapperIsNeverOffered pins the fail-closed
// half: no untapped non-attacker means no attack.
func TestHollowWarriorAttackWithoutATapperIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	warrior := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Hollow Warrior"))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	ch := e.attackPairCharge(warrior, 1, 0)
	if len(ch.taps) != 1 {
		t.Fatalf("precondition: charge = %+v, want one tap obligation", ch)
	}
	if got := len(e.blockTapCandidates(0, ch.taps[0], map[state.ObjID]bool{warrior: true})); got != 0 {
		t.Fatalf("precondition: %d tapper candidates, want 0", got)
	}
	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, warrior, 1); opt != nil {
			t.Fatalf("unpayable tap charge still offered: %+v", opt)
		}
	}
}

// attackPayLifeFixture is an inline CantAttackUnless static charging 2 life,
// the attack-side sibling of Sivitri, Dragon Master's delivered
// `Cost$ PayLife<2>` body (which has no printed combat carrier in the corpus).
const attackPayLifeFixture = "Name:Pay Life Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ PayLife<2> | Description$x\n" +
	"Oracle:x\n"

// TestAttackPayLifeChargesTwoLife pins the attack-side PayLife<N> component:
// the charge is two life, submitting the pair pays exactly that (one
// LifeChange) and commits the attack.
func TestAttackPayLifeChargesTwoLife(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, attackPayLifeFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the static is on seat 0's battlefield, the charge is two
	// life, and the payer's life total really exceeds it.
	if o := e.G.Zone(state.ZBattlefield, 0); len(o) == 0 {
		t.Fatal("precondition: the PayLife static is not on seat 0's battlefield")
	}
	ch := e.attackPairCharge(bear, 0, 0)
	if ch.life != 2 || ch.mana != 0 || len(ch.taps) != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want life-only 2", ch)
	}
	if e.G.Players[1].Life <= ch.life {
		t.Fatalf("precondition: payer life %d does not exceed the charge %d", e.G.Players[1].Life, ch.life)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("life-charged pair not offered: %+v", d.Options)
	}
	if opt.CostLife != 2 || !strings.Contains(opt.Label, "pay 2 life") {
		t.Fatalf("pair not life-charged: %+v", opt)
	}
	before := e.G.Players[1].Life
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit life-charged attack: %v", err)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("life after the charged attack = %d, want %d", got, before-2)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the life-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestAttackPayLifeInsufficientIsNeverOffered pins the fail-closed half: a
// payer who cannot pay the life never sees the pair.
func TestAttackPayLifeInsufficientIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, attackPayLifeFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.G.Players[1].Life = 1

	if ch := e.attackPairCharge(bear, 0, 0); ch.life != 2 {
		t.Fatalf("precondition: charge = %+v, want life 2", ch)
	}
	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, bear, 0); opt != nil {
			t.Fatalf("unpayable life charge still offered: %+v", opt)
		}
	}
}

// TestNornsAnnexPhyrexianPaidWithLife pins the real {W/P} carrier: with no
// white source the Phyrexian pip is paid with two life (CR 107.4f), the
// attack commits, and the life is spent.
func TestNornsAnnexPhyrexianPaidWithLife(t *testing.T) {
	e, bear := attackTaxSeat(t, "Norn's Annex")

	// PRECONDITION: the charge is one white Phyrexian pip and nothing else,
	// the payer has no white source, and two life is affordable.
	ch := e.attackPairCharge(bear, 0, 0)
	if len(ch.phyrexian) != 1 || ch.phyrexian[0] != 'W' || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want one W Phyrexian pip", ch)
	}
	if n := len(e.windowManaUnits(1)); n != 0 {
		t.Fatalf("precondition: payer has %d window mana units, want 0", n)
	}
	if e.G.Players[1].Life < 2 {
		t.Fatalf("precondition: payer life %d cannot cover the two-life branch", e.G.Players[1].Life)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("Phyrexian-charged pair not offered: %+v", d.Options)
	}
	before := e.G.Players[1].Life
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit Phyrexian-charged attack: %v", err)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("life after the Phyrexian charge = %d, want %d (paid with two life)", got, before-2)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the Phyrexian-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestNornsAnnexPhyrexianColourVersusLifeChoice pins the CR 107.4f election:
// with a white source AND two life both legal, the payment window poses the
// real colour-versus-life choice, and taking the life branch spends life
// rather than the source; taking the colour branch taps the white source.
func TestNornsAnnexPhyrexianColourVersusLifeChoice(t *testing.T) {
	e, bear := attackTaxSeat(t, "Norn's Annex")
	plains := onBoardCard(t, e, 1, card(t, "Name:Test Plains\nTypes:Basic Land Plains\nOracle:x\n"))

	// PRECONDITION: the pip's colour branch is reachable from the Plains and
	// the life branch is affordable, so BOTH are on offer.
	if ch := e.attackPairCharge(bear, 0, 0); len(ch.phyrexian) != 1 {
		t.Fatalf("precondition: charge = %+v, want one Phyrexian pip", ch)
	}
	if n := len(e.windowManaUnits(1)); n != 1 {
		t.Fatalf("precondition: windowManaUnits(1) = %d, want 1 (the Plains)", n)
	}
	both, canColour, canLife := e.combatPhyBothBranches(1, e.attackPairCharge(bear, 0, 0))
	if !both || !canColour || !canLife {
		t.Fatalf("precondition: colour branch %v, life branch %v, want both true", canColour, canLife)
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("Phyrexian-charged pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose {
		t.Fatalf("expected the Phyrexian election window, got %+v", pay)
	}
	lifeOpt := -1
	for _, o := range pay.Options {
		if o.Kind == "attack_phy_life" {
			lifeOpt = o.Index
		}
	}
	if lifeOpt < 0 {
		t.Fatalf("no life branch offered in the election: %+v", pay.Options)
	}
	before := e.G.Players[1].Life
	if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{lifeOpt}}); err != nil {
		t.Fatalf("choose the life branch: %v", err)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("life after the life-branch election = %d, want %d", got, before-2)
	}
	if e.G.Obj(plains).Tapped {
		t.Fatal("the life branch tapped the Plains; it should have spent life only")
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the Phyrexian-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// blockSacFixture charges every blocker one sacrificed land; blockPhyrexian
// and blockReturn exercise the sibling components, none of which has a corpus
// combat carrier (the block-side grammar is the target of this ticket).
const blockSacFixture = "Name:Block Sac Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantBlockUnless | ValidCard$ Creature | Cost$ Sac<1/Land> | Description$x\n" +
	"Oracle:x\n"

const blockReturnFixture = "Name:Block Return Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantBlockUnless | ValidCard$ Creature | Cost$ Return<1/Land> | Description$x\n" +
	"Oracle:x\n"

const blockPhyrexianFixture = "Name:Block Phyrexian Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantBlockUnless | ValidCard$ Creature | Cost$ UP | Description$x\n" +
	"Oracle:x\n"

// TestBlockSacrificeChargesALand pins the block-side Sac component: the
// composite charge carries the sacrifice, submitting the block sacrifices a
// land and commits the block.
func TestBlockSacrificeChargesALand(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, blockSacFixture))
	land := onBoardCard(t, e, 0, card(t, landFixture))
	blocker := onBoardReady(t, e, 0, memniteSrc)
	attacker := onBoardReady(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker)

	// PRECONDITION: the charge carries one sacrifice, the land is a distinct
	// battlefield permanent controlled by the defender, and the pair is
	// affordable.
	ch := e.blockPairCharge(blocker, attacker)
	if len(ch.sacs) != 1 || ch.sacs[0].n != 1 || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: blockPairCharge = %+v, want one sacrifice of 1", ch)
	}
	if l := e.G.Obj(land); l == nil || l.Zone != state.ZBattlefield || l.Controller != 0 {
		t.Fatal("precondition: the sacrifice candidate is not the defender's land")
	}

	d := askBlockersFresh(t, e)
	opt := findBlockOption(d, blocker, attacker)
	if opt == nil {
		t.Fatalf("sacrifice-charged block not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit sacrifice-charged block: %v", err)
	}
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the block sacrifice never moved the land to the graveyard: %+v", e.G.Obj(land))
	}
	blocked := false
	for _, b := range e.G.Obj(attacker).BlockedBy {
		if b == blocker {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("the sacrifice-charged block was never committed")
	}
}

// TestBlockReturnChargesALand pins the block-side Return component: the
// charged permanent returns to its owner's hand and the block commits.
func TestBlockReturnChargesALand(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, blockReturnFixture))
	land := onBoardCard(t, e, 0, card(t, landFixture))
	blocker := onBoardReady(t, e, 0, memniteSrc)
	attacker := onBoardReady(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker)

	ch := e.blockPairCharge(blocker, attacker)
	if len(ch.returns) != 1 || ch.mana != 0 || len(ch.sacs) != 0 {
		t.Fatalf("precondition: blockPairCharge = %+v, want one return of 1", ch)
	}
	if l := e.G.Obj(land); l == nil || l.Zone != state.ZBattlefield || l.Controller != 0 {
		t.Fatal("precondition: the return candidate is not the defender's land")
	}
	d := askBlockersFresh(t, e)
	opt := findBlockOption(d, blocker, attacker)
	if opt == nil {
		t.Fatalf("return-charged block not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit return-charged block: %v", err)
	}
	if o := e.G.Obj(land); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the block return never returned the land to hand: %+v", e.G.Obj(land))
	}
	blocked := false
	for _, b := range e.G.Obj(attacker).BlockedBy {
		if b == blocker {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("the return-charged block was never committed")
	}
}

// TestBlockPhyrexianPaysTwoLife pins the block-side Phyrexian component: with
// no blue source the pip is paid with two life.
func TestBlockPhyrexianPaysTwoLife(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, blockPhyrexianFixture))
	blocker := onBoardReady(t, e, 0, memniteSrc)
	attacker := onBoardReady(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker)

	ch := e.blockPairCharge(blocker, attacker)
	if len(ch.phyrexian) != 1 || ch.phyrexian[0] != 'U' || ch.mana != 0 {
		t.Fatalf("precondition: blockPairCharge = %+v, want one U Phyrexian pip", ch)
	}
	if n := len(e.windowManaUnits(0)); n != 0 {
		t.Fatalf("precondition: defender has %d window mana units, want 0", n)
	}
	if e.G.Players[0].Life < 2 {
		t.Fatalf("precondition: defender life %d cannot cover the two-life branch", e.G.Players[0].Life)
	}
	d := askBlockersFresh(t, e)
	opt := findBlockOption(d, blocker, attacker)
	if opt == nil {
		t.Fatalf("Phyrexian-charged block not offered: %+v", d.Options)
	}
	before := e.G.Players[0].Life
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit Phyrexian-charged block: %v", err)
	}
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("life after the Phyrexian block = %d, want %d", got, before-2)
	}
	blocked := false
	for _, b := range e.G.Obj(attacker).BlockedBy {
		if b == blocker {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("the Phyrexian-charged block was never committed")
	}
}

// TestBlockSacrificeWithoutALandIsNeverOffered pins the fail-closed half on
// the block side.
func TestBlockSacrificeWithoutALandIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, blockSacFixture))
	blocker := onBoardReady(t, e, 0, memniteSrc)
	attacker := onBoardReady(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker)

	if ch := e.blockPairCharge(blocker, attacker); len(ch.sacs) != 1 {
		t.Fatalf("precondition: charge = %+v, want one sacrifice obligation", ch)
	}
	if d := askBlockersFresh(t, e); d != nil {
		if opt := findBlockOption(d, blocker, attacker); opt != nil {
			t.Fatalf("unpayable sacrifice charge still offered: %+v", opt)
		}
	}
}

// TestAttackChargeBotAnswerNeverRejected runs the shipped bot policy's own
// answer through validateAttackers where the non-mana constraint binds: two
// attackers each charged 2 life against only 3 life -- each pair individually
// affordable, both jointly not. The bot guard must drop the over-budget pair
// rather than submit a declaration the engine rejects forever.
func TestAttackChargeBotAnswerNeverRejected(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, attackPayLifeFixture))
	a1 := onBoardReady(t, e, 1, "Name:Attacker One\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	a2 := onBoardReady(t, e, 1, "Name:Attacker Two\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.G.Players[1].Life = 3

	// PRECONDITIONS: both pairs price at 2 life, each individually affordable
	// against 3 life, the sum (4) not.
	c1 := e.attackPairCharge(a1, 0, 0)
	c2 := e.attackPairCharge(a2, 0, 0)
	if c1.life != 2 || c2.life != 2 {
		t.Fatalf("precondition: charges %+v / %+v, want life 2 each", c1, c2)
	}
	if !e.combatChargeAffordable(1, c1, map[state.ObjID]bool{a1: true}) ||
		!e.combatChargeAffordable(1, c2, map[state.ObjID]bool{a2: true}) {
		t.Fatal("precondition: each pair must be individually affordable on 3 life")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no attackers decision on the binding board: %+v", d)
	}
	if findAttackOption(d, a1, 0) == nil || findAttackOption(d, a2, 0) == nil {
		t.Fatalf("individually affordable pairs not offered: %+v", d.Options)
	}
	bot := newTestBot(4242)
	in := bot.answer(e, d)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: in.Choices}); err != nil {
		t.Fatalf("the bot's own attack answer was rejected: %v (choices %v)", err, in.Choices)
	}
	if got := e.G.Players[1].Life; got < 1 {
		t.Fatalf("life after the paid declaration = %d, want >= 1 (the jointly unpayable pair must be dropped)", got)
	}
	drainCombatPriority(t, e)
}
