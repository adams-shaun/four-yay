package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// attackcost-affordability: the l2 review-round narrowing of the shared
// combatChargeAffordable read.
//
// A resolved zero is a free charge, not an unpriceable cost. On main a
// Count$/SVar combat price that resolved to 0 was skipped as a free pair; the
// fail-closed round initially classified it `unpriceable`, so the creature
// could not attack or block AT ALL -- a real regression on corpus cards whose
// counted resource is legitimately empty (Myr Prototype at 0 counters,
// Collective Restraint with 0 land types, Cowed by Wisdom with an empty hand).
//
// A multi-pip charge payable ONLY by mixing branches (one pip with colour, one
// with life) is admitted by the joint Cost{Generic, Phyrexian} reachability
// read but the all-or-nothing election cannot settle it, so the pair used to
// be offered, validated, and then silently committed nothing. The read is now
// narrowed to the branches the window CAN settle.
//
// These tests pin both directions at the charge resolver and end to end
// through the offer list, each asserting its own precondition.

// TestCombatPropResolvedZeroCountIsFree pins the resolver: a Count$ body that
// resolves to 0 with a valid SVar table is a priceable FREE charge, never
// `unpriceable`.
func TestCombatPropResolvedZeroCountIsFree(t *testing.T) {
	e := threeSeatEngine(t)
	source := onBoardReady(t, e, 0, "Name:Count source\nTypes:Creature\nPT:1/1\nOracle:x\n")
	sv := staticView{
		Source:     source,
		Controller: 0,
		Params:     map[string]string{"Cost": "Count$CardCounters.P1P1"},
		SVars:      map[string]string{},
	}
	attack, ok := e.attackUnlessCharge(sv, 0)
	if !ok || !attack.zero() || attack.unpriceable {
		t.Fatalf("precondition/result: resolved zero-counter attack count = %+v, ok=%v; want a priceable free charge", attack, ok)
	}
	block, ok := e.blockUnlessCharge(sv)
	if !ok || !block.zero() || block.unpriceable {
		t.Fatalf("precondition/result: resolved zero-counter block count = %+v, ok=%v; want a priceable free charge", block, ok)
	}
	// PRECONDITION: the source the count reads really is on the battlefield
	// and really has zero counters, so the zero is a resolved zero and not a
	// missing object.
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || len(o.Counters) != 0 {
		t.Fatal("precondition: zero-counter count source is not on the battlefield")
	}
}

// TestMyrPrototypeZeroCountersAttacksFree is the corpus-shaped half the
// review's finding asked for: Myr Prototype (`Cost$ Y`, `SVar:Y:
// Count$CardCounters.P1P1`) has no +1/+1 counters the turn it lands, so its
// self-charge resolves to a free price and the creature MUST still be offered
// as an attacker. Before the resolved-zero fix the pair was unpriceable and
// disappeared from the offer list entirely -- the card could never attack.
func TestMyrPrototypeZeroCountersAttacksFree(t *testing.T) {
	e := threeSeatEngine(t)
	myr := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Myr Prototype"))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: Myr Prototype is seat 0's battlefield creature with zero
	// counters, the charge it prices really reads those counters, and the
	// defender is seat 1 (the absent Target$ scopes every defender).
	o := e.G.Obj(myr)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || len(o.Counters) != 0 {
		t.Fatalf("precondition: Myr Prototype = %+v, want a 0-counter creature controlled by seat 0", o)
	}
	ch := e.attackPairCharge(myr, 1, 0)
	if ch.unpriceable || !ch.zero() {
		t.Fatalf("precondition: Myr Prototype charge = %+v, want priceable/zero (the resolved-zero fix is not in force)", ch)
	}
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{myr: true}) {
		t.Fatal("precondition: the resolved-zero charge reads unaffordable")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	opt := findAttackOption(d, myr, 1)
	if opt == nil {
		t.Fatalf("0-counter Myr Prototype cannot attack at all: %+v", d.Options)
	}
	if opt.Value != 0 {
		t.Fatalf("the resolved-zero charge was priced at Value %d, want 0", opt.Value)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit the resolved-zero attack: %v", err)
	}
	if o := e.G.Obj(myr); o == nil || !o.IsAttacking {
		t.Fatal("the resolved-zero attacker was never declared")
	}
	drainCombatPriority(t, e)
}

// combatMultiPipFixture charges two Phyrexian pips of different colours: the
// shape whose MIXED branch affordability (one pip with colour, one with life)
// the all-or-nothing election cannot settle.
const combatMultiPipFixture = "Name:Multi Pip Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ W/P U/P | Description$x\n" +
	"Oracle:x\n"

// TestMixedOnlyMultiPipChargeIsNeverOffered pins the l2 MINOR's consequence:
// a two-pip charge payable ONLY by mixing branches (one colour, one life) is
// admitted by the joint `Cost{Generic, Phyrexian}` reachability read, but
// openCombatPayPlan can settle neither an all-colour nor an all-life branch,
// so the window would decline a pair it had already offered and validated.
// combatChargeAffordable now narrows to the branches the window CAN settle:
// such a pair is never offered. All-colour and all-life multi-pip charges are
// still offered (asserted for the all-life branch here).
func TestMixedOnlyMultiPipChargeIsNeverOffered(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatMultiPipFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers

	ch := e.attackPairCharge(bear, 0, 0)
	if len(ch.phyrexian) != 2 || ch.phyrexian[0] != 'W' || ch.phyrexian[1] != 'U' || ch.mana != 0 {
		t.Fatalf("precondition: charge = %+v, want two pips W then U", ch)
	}

	// MIXED-ONLY: one white source (cannot pay both pips with colour) and
	// three life (cannot pay both pips with life). The joint cost read admits
	// the mix; neither all-pips branch can settle, so the pair must not be
	// offered.
	onBoardReady(t, e, 1, "Name:Test Plains\nTypes:Basic Land Plains\nOracle:x\n")
	e.G.Players[1].Life = 3
	if !e.unlessManaReachable(1, chCost(ch), e.G.Players[1].Pool, e.G.Players[1].Snow,
		e.G.Players[1].ManaUnits(), e.G.Players[1].Life, e.paymentConv(1, 0, false),
		e.attackWindowUnits(1, nil)) {
		t.Fatal("precondition: the MIXED branch is payable, but the joint read says no (test is vacuous)")
	}
	if e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: the mixed-only charge reads affordable; the anti-strand narrowing is not in force")
	}
	e.askAttackers()
	d := e.Pending()
	if d != nil && d.Kind == decision.KAttackers {
		if opt := findAttackOption(d, bear, 0); opt != nil {
			t.Fatalf("a mixed-only multi-pip charge was offered; the window cannot settle it: %+v", opt)
		}
	}

	// ALL-LIFE control: four life covers both pips, so the pair IS offered.
	e.G.Players[1].Life = 4
	if !e.combatChargeAffordable(1, ch, map[state.ObjID]bool{bear: true}) {
		t.Fatal("precondition: an all-life multi-pip charge must read affordable with 4 life")
	}
	e.askAttackers()
	if d := e.Pending(); d == nil || d.Kind != decision.KAttackers || findAttackOption(d, bear, 0) == nil {
		t.Fatalf("the all-life-settleable multi-pip pair was not offered: %+v", d)
	}
}

// chCost is the joint mana requirement combatChargeAffordable's reachability
// call prices: the generic plus every pip (each colour OR two life).
func chCost(c blockCharge) Cost {
	out := Cost{Generic: c.mana, Phyrexian: append([]byte(nil), c.phyrexian...)}
	return out
}

// combatTapPipFixture charges `tapXType<1/Creature> W/P`: one creature tap
// obligation plus a Phyrexian pip. The tap obligation and the pip's colour
// branch compete for the same creature when it is the payer's only white
// source.
const combatTapPipFixture = "Name:Tap Pip Tax\nTypes:Enchantment\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ tapXType<1/Creature> W/P | Description$x\n" +
	"Oracle:x\n"

// TestPhyrexianColourBranchExcludesReservedTapSource pins the l2 MINOR: a
// plan-reserved tap permanent must be withheld from the Phyrexian colour
// branch probe too, not only from combatChargeAffordable's reachability. When
// the payer's only white source IS the creature the tap obligation must tap,
// the colour branch is NOT reachable through the plan; only the life branch
// is. Reading the colour branch as reachable would offer a CR 107.4f election
// whose colour half the window cannot settle, aborting an otherwise payable
// declaration.
func TestPhyrexianColourBranchExcludesReservedTapSource(t *testing.T) {
	e := threeSeatEngine(t)
	onBoardCard(t, e, 0, card(t, combatTapPipFixture))
	bear := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	dork := onBoardReady(t, e, 1, "Name:White Dork\nManaCost:1 W\nTypes:Creature Elf\nPT:1/1\n"+
		"A:AB$ Mana | Cost$ T | Produced$ W | Oracle:x\n")
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.G.Players[1].Life = 2

	ch := e.attackPairCharge(bear, 0, 0)
	if len(ch.taps) != 1 || len(ch.phyrexian) != 1 || ch.phyrexian[0] != 'W' {
		t.Fatalf("precondition: charge = %+v, want one tap obligation + one W pip", ch)
	}
	// PRECONDITION: the plan really reserves the dork for the tap obligation,
	// and the dork really is a window mana source, so the exclusion matters.
	taps, _, _, ok := e.chargeObjPlan(1, ch, map[state.ObjID]bool{bear: true})
	if !ok || len(taps) != 1 || taps[0] != dork {
		t.Fatalf("precondition: plan taps = %v (ok=%v), want exactly the white dork %d reserved", taps, ok, dork)
	}
	found := false
	for _, s := range e.attackWindowUnits(1, nil) {
		if s.id == dork {
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: the dork is not a window mana source, so the exclusion test is vacuous")
	}

	// WITHOUT the caller's exclusion the colour branch reads reachable (the
	// dork pays the pip); WITH it the colour branch cannot be reached and only
	// the life branch remains, so the plan must settle deterministically on
	// life (phyElection=false, phyToLife=1) rather than pose a CR 107.4f
	// election whose colour half the window cannot settle.
	committed := map[state.ObjID]bool{bear: true}
	if _, colour, life := e.combatPhyBothBranches(1, ch); !colour || !life {
		t.Fatalf("precondition: unexcluded colour=%v life=%v, want both reachable", colour, life)
	}
	if !e.combatChargeAffordable(1, ch, committed) {
		t.Fatal("precondition: the charge must still be affordable through the life branch")
	}
	plan, ok := e.openCombatPayPlan(1, ch, committed)
	if !ok {
		t.Fatal("precondition: the plan must open for the life branch")
	}
	if plan.phyElection {
		t.Fatal("the plan poses a Phyrexian election whose colour half is unreachable through the reserved tap source")
	}
	if plan.phyToLife != int32(len(ch.phyrexian)) {
		t.Fatalf("plan routed %d pips to life, want all %d (the colour branch is unreachable)", plan.phyToLife, len(ch.phyrexian))
	}
}
