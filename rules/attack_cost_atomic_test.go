// attackcost-atomic: the review-round fixes on top of attackcost-nonmana.
// Four defects: (1) the generic mana and the Phyrexian pips were checked
// independently against the same pool, so a `Cost$ 2 WP` the payer could not
// actually pay was offered and then committed unpaid; (2) a floating pool
// that covered the Phyrexian colour branch settled inline and never offered
// the legal CR 107.4f life choice; (3) a matching static whose Cost$ cannot
// be priced was skipped, letting the creature attack/block for free; (4) the
// tap/sac/return obligation reservation was greedy, so a tap could claim the
// only permanent a sacrifice uniquely needed, and a permanent reserved for a
// tap obligation could also be tapped for mana. These tests pin each fix end
// to end and assert their own preconditions.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// combatAtomicFixture charges every attacker the inline `2 WP`: two generic
// mana plus one white Phyrexian pip, the shape the reviewer's probe used.
const combatAtomicFixture = "Name:Atomic Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ 2 WP | Description$x\n" +
	"Oracle:x\n"

// TestAtomicallyUnpayableGenericAndPipPairIsNeverOffered pins the atomicity
// fix: `2 WP` needs three mana units (two generic plus the pip's white) and
// the payer has only two white and one life, so NEITHER branch is payable.
// The pair must not be offered at all -- the round-1 read checked the generic
// against the budget and the pip against the pool separately and admitted it.
func TestAtomicallyUnpayableGenericAndPipPairIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatAtomicFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.G.Players[1].Life = 1
	e.G.Players[1].Pool[state.MW], e.G.Players[1].Pool[state.MC] = 2, 0

	// PRECONDITION: the charge is generic 2 plus one W pip, the pool holds
	// exactly two white (three units short of the colour branch), and life is
	// one (two short of the pip's life branch).
	ch := e.attackPairCharge(bear, 0, 0)
	if ch.mana != 2 || len(ch.phyrexian) != 1 || ch.phyrexian[0] != 'W' || ch.life != 0 {
		t.Fatalf("precondition: attackPairCharge = %+v, want 2 mana + one W pip", ch)
	}
	if e.G.Players[1].Pool[state.MW] != 2 || e.G.Players[1].Life != 1 {
		t.Fatalf("precondition: pool W=%d life=%d, want 2 and 1", e.G.Players[1].Pool[state.MW], e.G.Players[1].Life)
	}
	if e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: the charge reads AFFORDABLE; the atomicity fix is not in force")
	}

	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, bear, 0); opt != nil {
			t.Fatalf("atomically unpayable pair still offered: %+v", opt)
		}
	}
	// The pool is untouched: no part of an unpayable charge was spent.
	if got := e.G.Players[1].Pool[state.MW]; got != 2 {
		t.Fatalf("pool W after the refused declaration = %d, want 2", got)
	}
	if got := e.G.Players[1].Life; got != 1 {
		t.Fatalf("life after the refused declaration = %d, want 1", got)
	}
}

// TestAtomicChargePaysGenericAndPipFromOnePool pins the payable half: with
// THREE white floating and no life for the pip, the payer pays both the two
// generic and the pip's colour from the same pool, and the attack commits.
func TestAtomicChargePaysGenericAndPipFromOnePool(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatAtomicFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.G.Players[1].Life = 1 // the pip's life branch is NOT affordable
	e.G.Players[1].Pool[state.MW], e.G.Players[1].Pool[state.MC] = 3, 0

	ch := e.attackPairCharge(bear, 0, 0)
	if ch.mana != 2 || len(ch.phyrexian) != 1 {
		t.Fatalf("precondition: charge = %+v, want 2 mana + one pip", ch)
	}
	if !e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: three white and one life must afford 2 generic + one W pip")
	}
	if both, _, _ := e.combatPhyBothBranches(1, ch); both {
		t.Fatal("precondition: the life branch must be unaffordable at one life")
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("payable atomic pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit atomic-charged attack: %v", err)
	}
	if got := e.G.Players[1].Pool[state.MW]; got != 0 {
		t.Fatalf("pool W after paying 2 generic + one W pip = %d, want 0", got)
	}
	if got := e.G.Players[1].Life; got != 1 {
		t.Fatalf("life after the colour-branch payment = %d, want 1 (unchanged)", got)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the atomic-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestAtomicChargeLifeBranchWhenGenericIsShort pins the per-pip CR 107.4f
// model: `2 WP` with TWO white and at least two life is payable by spending
// the two white on the generic and two life on the pip. The pairing of a
// branch to a pip is the payer's legal choice.
func TestAtomicChargeLifeBranchWhenGenericIsShort(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatAtomicFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.G.Players[1].Life = 20
	e.G.Players[1].Pool[state.MW], e.G.Players[1].Pool[state.MC] = 2, 0

	ch := e.attackPairCharge(bear, 0, 0)
	if ch.mana != 2 || len(ch.phyrexian) != 1 {
		t.Fatalf("precondition: charge = %+v, want 2 mana + one pip", ch)
	}
	if !e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: two white plus twenty life must afford 2 generic + one W pip")
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("life-branch atomic pair not offered: %+v", d.Options)
	}
	before := e.G.Players[1].Life
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit atomic-charged attack: %v", err)
	}
	// The white covers the generic; the pip is paid with two life.
	if got := e.G.Players[1].Pool[state.MW]; got != 0 {
		t.Fatalf("pool W after the generic payment = %d, want 0", got)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("life after the pip's life branch = %d, want %d", got, before-2)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the atomic-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// TestNornsAnnexFloatingColourStillOffersTheLifeChoice pins the election fix
// for the case the reviewer named: Norn's Annex with a FLOATING white mana
// and at least two life. The pool could pay the pip's colour outright, but
// both branches are legal, so the payment window must pose the real
// CR 107.4f choice rather than settling inline.
func TestNornsAnnexFloatingColourStillOffersTheLifeChoice(t *testing.T) {
	e, bear := attackTaxSeat(t, "Norn's Annex")
	e.G.Players[1].Life = 20
	e.G.Players[1].Pool[state.MW] = 1

	ch := e.attackPairCharge(bear, 0, 0)
	if len(ch.phyrexian) != 1 || ch.phyrexian[0] != 'W' || ch.mana != 0 {
		t.Fatalf("precondition: charge = %+v, want one W pip", ch)
	}
	if e.G.Players[1].Pool[state.MW] != 1 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: floating W=%d life=%d, want 1 and 20", e.G.Players[1].Pool[state.MW], e.G.Players[1].Life)
	}
	both, canColour, canLife := e.combatPhyBothBranches(1, ch)
	if !both || !canColour || !canLife {
		t.Fatalf("precondition: colour=%v life=%v, want both branches affordable", canColour, canLife)
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
		t.Fatalf("expected the Phyrexian election window with a floating colour, got %+v", pay)
	}
	lifeOpt, colourOpt := -1, -1
	for _, o := range pay.Options {
		switch o.Kind {
		case "attack_phy_life":
			lifeOpt = o.Index
		case "attack_phy_colour":
			colourOpt = o.Index
		}
	}
	if lifeOpt < 0 || colourOpt < 0 {
		t.Fatalf("election missing a branch: %+v", pay.Options)
	}
	before := e.G.Players[1].Life
	if err := e.Submit(decision.Intent{Seq: pay.Seq, Player: pay.Player, Choices: []int{lifeOpt}}); err != nil {
		t.Fatalf("choose the life branch: %v", err)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("life after the life branch = %d, want %d", got, before-2)
	}
	if got := e.G.Players[1].Pool[state.MW]; got != 1 {
		t.Fatalf("floating W after the life branch = %d, want 1 (unspent)", got)
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the Phyrexian-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// combatUnpriceableFixture charges `Cost$ W`: a plain coloured pip this
// build's combat-cost grammar deliberately cannot price.
const combatUnpriceableFixture = "Name:Unpriceable Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ W | Description$x\n" +
	"Oracle:x\n"

// TestUnpriceableAttackCostFailsClosed pins the fail-closed fix: a matching
// static whose Cost$ cannot be priced must NOT be skipped (which let the
// creature attack for free); the pair is unpriceable and never offered.
func TestUnpriceableAttackCostFailsClosed(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatUnpriceableFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	// A white source is available: if the cost were merely "hard to pay" the
	// pair would still be offered. It must fail closed as UNPRICEABLE.
	onBoardReady(t, e, 1, "Name:Test Plains\nTypes:Basic Land Plains\nOracle:x\n")

	ch := e.attackPairCharge(bear, 0, 0)
	if !ch.unpriceable {
		t.Fatalf("precondition: charge = %+v, want unpriceable for a plain coloured pip", ch)
	}
	if e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}, map[state.ObjID]bool{bear: true}) {
		t.Fatal("an unpriceable charge must never read affordable")
	}
	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, bear, 0); opt != nil {
			t.Fatalf("unpriceable charge still offered (free attack): %+v", opt)
		}
	}
}

// blockUnpriceableFixture charges `Cost$ W` on the block side.
const blockUnpriceableFixture = "Name:Block Unpriceable Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantBlockUnless | ValidCard$ Creature | Cost$ W | Description$x\n" +
	"Oracle:x\n"

// TestUnpriceableBlockCostFailsClosed is the block-direction sibling.
func TestUnpriceableBlockCostFailsClosed(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, blockUnpriceableFixture))
	blocker := onBoardReady(t, e, 0, memniteSrc)
	attacker := onBoardReady(t, e, 1, "Name:Attacker Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	attackSeat0(t, e, attacker)
	onBoardReady(t, e, 0, "Name:Test Plains\nTypes:Basic Land Plains\nOracle:x\n")

	ch := e.blockPairCharge(blocker, attacker)
	if !ch.unpriceable {
		t.Fatalf("precondition: charge = %+v, want unpriceable", ch)
	}
	if e.blockChargeAffordable(0, ch, map[state.ObjID]bool{blocker: true}, nil) {
		t.Fatal("an unpriceable block charge must never read affordable")
	}
	d := askBlockersFresh(t, e)
	if d != nil {
		if opt := findBlockOption(d, blocker, attacker); opt != nil {
			t.Fatalf("unpriceable block charge still offered (free block): %+v", opt)
		}
	}
}

// combatJointFixture charges `tapXType<1/Creature> Sac<1/Artifact>`: one tap
// obligation and one sacrifice obligation, whose candidates overlap.
const combatJointFixture = "Name:Joint Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ tapXType<1/Creature> Sac<1/Artifact> | Description$x\n" +
	"Oracle:x\n"

// TestJointTapSacrificeAssignmentPaysWhenAGreedyPlanWouldFail pins the joint
// assignment fix. The artifact creature sits FIRST in zone order, so a greedy
// tap reservation claims it; the sacrifice then has no candidate and the
// charge reads unpayable, even though tapping the non-artifact creature and
// sacrificing the artifact is a valid payment.
func TestJointTapSacrificeAssignmentPaysWhenAGreedyPlanWouldFail(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatJointFixture))
	artifact := onBoardReady(t, e, 1, "Name:Test Golem\nManaCost:2\nTypes:Artifact Creature Golem\nPT:1/1\nOracle:x\n")
	plain := onBoardReady(t, e, 1, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the artifact is the only artifact creature and precedes
	// the plain creature in zone order; the charge carries one tap and one
	// sacrifice.
	zone := e.G.Zone(state.ZBattlefield, 1)
	ai, pi := -1, -1
	for i, id := range zone {
		switch id {
		case artifact:
			ai = i
		case plain:
			pi = i
		}
	}
	if ai < 0 || pi < 0 || ai > pi {
		t.Fatalf("precondition: artifact zone index %d must precede the plain creature's %d", ai, pi)
	}
	ch := e.attackPairCharge(bear, 0, 0)
	if len(ch.taps) != 1 || len(ch.sacs) != 1 || ch.mana != 0 || ch.life != 0 {
		t.Fatalf("precondition: charge = %+v, want one tap + one sacrifice", ch)
	}
	if !e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: the charge IS payable (tap the plain creature, sacrifice the artifact)")
	}

	e.askAttackers()
	d := e.Pending()
	opt := findAttackOption(d, bear, 0)
	if opt == nil {
		t.Fatalf("jointly payable pair not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit joint-charged attack: %v", err)
	}
	if o := e.G.Obj(artifact); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the artifact was not sacrificed: %+v", e.G.Obj(artifact))
	}
	if !e.G.Obj(plain).Tapped {
		t.Fatal("the plain creature was not tapped as the tap obligation")
	}
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatal("the joint-charged attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// combatTapVsManaFixture charges `1 tapXType<1/Creature>` where a
// creature-land is both the only mana source and the only tap candidate.
const combatTapVsManaFixture = "Name:Tap Versus Mana Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ 1 tapXType<1/Creature> | Description$x\n" +
	"Oracle:x\n"

// TestTapObligationCannotAlsoPayTheMana pins the double-tap fix: a permanent
// reserved for the charge's tapXType obligation must be withheld from the
// mana window, so a board where one creature-land is both the only source
// and the only tapper cannot pay -- the round-1 code tapped it for mana and
// then tapped it again as the cost.
func TestTapObligationCannotAlsoPayTheMana(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatTapVsManaFixture))
	creatureLand := onBoardReady(t, e, 1, "Name:Test Grove\nTypes:Land Creature\nPT:1/1\n"+
		"A:AB$ Mana | Cost$ T | Produced$ G | Oracle:x\n")
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the creature-land is a legal tap candidate and the only
	// mana source on the battlefield; with the plan's tap reservation
	// withheld, no source remains for the generic one.
	ch := e.attackPairCharge(bear, 0, 0)
	if ch.mana != 1 || len(ch.taps) != 1 || len(ch.sacs) != 0 || len(ch.phyrexian) != 0 {
		t.Fatalf("precondition: charge = %+v, want 1 mana + one tap", ch)
	}
	if len(e.blockTapCandidates(1, ch.taps[0], map[state.ObjID]bool{bear: true})) == 0 {
		t.Fatal("precondition: the creature-land is not a tap candidate")
	}
	if len(e.attackManaSources(1)) == 0 {
		t.Fatal("precondition: the creature-land is not a mana source")
	}
	if e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: one permanent cannot pay both a {1} and its own tap; the charge must be unaffordable")
	}

	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, bear, 0); opt != nil {
			t.Fatalf("double-tap charge still offered: %+v", opt)
		}
	}
	// Nothing was tapped or spent.
	if e.G.Obj(creatureLand).Tapped {
		t.Fatal("the creature-land was tapped by a refused declaration")
	}
}

// combatBotTapFixture is a creature carrying a SELF-scoped tapXType charge,
// so only that creature's attacks are taxed an obligation the wire cannot
// verify.
const combatBotTapFixture = "Name:Charged Warden\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Card.Self | Cost$ tapXType<1/Creature.!attacking> | Description$x\n" +
	"Oracle:x\n"

// TestAtomicTapChargedBotAnswerIsAccepted engages the engine's KAttackers
// validator with the shipped bot policy where a tap obligation binds: the
// bot's own answer must be accepted (no livelock), and because the published
// CostTaps cannot be verified on the wire the guard drops the tap-costed
// pair, so the declaration the engine sees is the free remainder.
func TestAtomicTapChargedBotAnswerIsAccepted(t *testing.T) {
	e := threeSeatEngine(t)
	charged := onBoardReady(t, e, 1, combatBotTapFixture)
	free := onBoardReady(t, e, 1, "Name:Free Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITIONS: the charged creature really carries a tap obligation
	// while the free creature carries none, and at least one other untapped
	// creature exists (so the charge is payable, not merely unpriceable).
	ch := e.attackPairCharge(charged, 0, 0)
	if len(ch.taps) != 1 || ch.mana != 0 || ch.life != 0 || ch.unpriceable {
		t.Fatalf("precondition: charged pair charge = %+v, want one priceable tap", ch)
	}
	if fch := e.attackPairCharge(free, 0, 0); !fch.zero() || fch.unpriceable {
		t.Fatalf("precondition: free pair charge = %+v, want zero", fch)
	}
	if !e.combatChargeAffordable(1, ch, map[state.ObjID]bool{charged: true}, map[state.ObjID]bool{charged: true}) {
		t.Fatal("precondition: the tap charge must be payable by the free creature")
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("no attackers decision: %+v", d)
	}
	if opt := findAttackOption(d, charged, 0); opt == nil || opt.CostTaps == 0 {
		t.Fatalf("precondition: charged pair not offered with CostTaps published: %+v", d.Options)
	}
	bot := newTestBot(99)
	in := bot.answer(e, d)
	for _, ci := range in.Choices {
		if ci >= 0 && ci < len(d.Options) && d.Options[ci].Obj == charged {
			t.Fatalf("the bot guard kept the unverifiable tap-costed pair: choices %v", in.Choices)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: in.Choices}); err != nil {
		t.Fatalf("the bot's own answer was rejected by validateAttackers: %v (choices %v)", err, in.Choices)
	}
	drainCombatPriority(t, e)
}
