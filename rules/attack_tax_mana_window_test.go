package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// attacktax-mana-window: a flat {N} attack tax (Myr Prototype's "pay {1} for
// each +1/+1 counter on it", Cost$ Y with SVar:Y:Count$CardCounters.P1P1)
// prices every offered (attacker, defender) pair at {N}, but the declared
// attackers are withheld from the payment window's mana sources (CR 508.1f:
// the declaration taps them), so a declaration that adds other creature-
// mana-sources shrinks the window below {N} and the whole declaration is
// unpayable. The offer gate admits the pair because it excludes only the taxed
// creature (attackOffers passes map{id:true}), while validateAttackers
// re-checks with every chosen attacker excluded, so the combined charge can be
// rejected even though its priced non-mana components are all zero.
//
// The recorded cardfuzz B-off/batch2.jsonl reproduction (seed
// 11612869535247757) hits exactly this: Myr Prototype has 9 +1/+1 counters
// ({9} tax), the board offers six attackers, and the bot declares all six. The
// rejection message used to print only the non-mana components
// "(0 life, 0 taps, ...)" -- all zero -- which reads as an unpriceable FREE
// charge. It is not unpriceable at all: blockCharge.unpriceable is false and
// blockCharge.mana is the real, unpaid component.
//
// bot-attack-tax-crash then folded each attacker's own forgone mana units into
// its published option Value (attackOptionBudgetValue), so for a pure generic-
// mana tax Decision.MaxSum over the folded Values is EXACTLY the window read
// the engine makes: 9 + 3x1 = 12 over a budget of 11. The wire now rejects the
// recorded declaration before the engine sees it, which is what stops the
// crash. TestManaSourceAttackersShrinkWindowWireBudget pins that, and also
// pins the engine belt that re-derives the same overflow: the two reads cannot
// disagree about what the declaration can pay.
//
// The engine's whole-declaration diagnostic ("... is not payable") is still the
// authoritative read and still the message this ticket fixed, so
// TestWholeDeclarationDiagnosticNamesMana reaches it through Submit by using a
// charge component the folded wire does NOT budget: a tap obligation. The
// offer gate meets it from the non-attacker creatures, but a declaration that
// commits those creatures as attackers leaves too few tap candidates, so the
// folded Values fit the budget while combatChargeAffordable refuses the whole
// declaration. Its message must name the mana and the fail-closed flag.
//
// blockCharge.unpriceable is TRUE only when a matching CantAttackUnless static
// has a Cost$ shape this build cannot price, and attackOffers drops every such
// pair (combatChargeAffordable fails closed on it), so an unpriceable charge
// can never enter a declared declaration through Submit. Both tests assert the
// charge is priceable, which is what the recorded report misread.

// flatAttackTaxFixture charges a flat {9} to attack, independent of the board
// and of the declaration's size: the Myr Prototype shape (Cost$ Y on a
// Card.Self CantAttackUnless static), with the SVar folded away to a literal
// so the fixture is deterministic without a counter engine.
const flatAttackTaxFixture = "Name:Flat Tax Golem\nManaCost:3\nTypes:Creature Golem\nPT:3/3\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Card.Self | Cost$ 9 | Description$ CARDNAME can't attack unless you pay {9}.\n" +
	"Oracle:x\n"

// tapTaxFixture charges {1} AND two tap obligations on creatures you control:
// the second component is priced by combatChargeAffordable (it reserves two
// non-attacker candidates and excludes them from the mana window) but is NOT
// part of any option's published Value, so it can make a whole declaration
// unpayable while the folded Values still fit Decision.MaxSum.
const tapTaxFixture = "Name:Tap Tax Golem\nManaCost:3\nTypes:Creature Golem\nPT:3/3\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Card.Self | Cost$ 1 tapXType<2/Creature> | Description$ CARDNAME can't attack unless you pay {1} and tap two creatures you control.\n" +
	"Oracle:x\n"

// manaDorkFixture is the taxed creature's sibling: a creature whose only role
// is to tap for {G}, and which the policy also offers as a free attacker.
const manaDorkFixture = "Name:Mana Dork\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:1/1\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n"

// forestFixture is an untapped basic Forest: a pure, non-attacker mana source.
const forestFixture = "Name:Test Forest\nTypes:Basic Land Forest\nOracle:x\n"

// TestManaSourceAttackersShrinkWindowWireBudget is the recorded structural
// failure, pinned at the layer that now enforces it. An offered flat {9}
// attack-tax pair is admitted (the offer gate excludes only the taxed
// creature, leaving eleven window units), but a declaration that also names
// the three mana-dork creatures leaves only the eight Forests in the payment
// window, so the whole declaration is unpayable. The folded option Values
// carry that unpayability onto the wire (9 + 3x1 = 12 against a budget of 11),
// so Submit rejects the declaration at Decision.Validate; the engine belt
// re-derives the same overflow when the wire is bypassed. The charge is mana 9
// with every non-mana component zero and unpriceable false -- the shape the
// recorded report misread as an unpriceable FREE charge.
func TestManaSourceAttackersShrinkWindowWireBudget(t *testing.T) {
	e := threeSeatEngine(t)
	golem := onBoardReadyCard(t, e, 0, card(t, flatAttackTaxFixture))
	dorks := []state.ObjID{
		onBoardReadyCard(t, e, 0, card(t, manaDorkFixture)),
		onBoardReadyCard(t, e, 0, card(t, manaDorkFixture)),
		onBoardReadyCard(t, e, 0, card(t, manaDorkFixture)),
	}
	for i := 0; i < 8; i++ {
		onBoardCard(t, e, 0, card(t, forestFixture))
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the taxed creature is seat 0's untaxed-sickness creature,
	// its CANONICAL charge is a flat {9} mana cost with every non-mana
	// component zero and unpriceable false -- the exact shape the recorded
	// report misread -- and the other three attackers are creatures that are
	// ALSO window mana sources, so declaring them removes their units.
	g := e.G.Obj(golem)
	if g == nil || g.Zone != state.ZBattlefield || g.Controller != 0 || g.SummonSick {
		t.Fatalf("precondition: Flat Tax Golem = %+v, want a ready battlefield creature controlled by seat 0", g)
	}
	ch := e.attackPairCharge(golem, 1, 0)
	if ch.unpriceable || ch.mana != 9 || ch.life != 0 || len(ch.taps) != 0 || len(ch.sacs) != 0 || len(ch.returns) != 0 || len(ch.phyrexian) != 0 {
		t.Fatalf("precondition: golem charge = %+v, want priceable mana 9 with every non-mana component zero", ch)
	}
	if ch.zero() {
		t.Fatal("precondition: charge reads zero, so the rejection below cannot exercise the non-zero guard")
	}
	for _, id := range dorks {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.SummonSick {
			t.Fatalf("precondition: mana dork %d = %+v, want a ready battlefield creature controlled by seat 0", id, o)
		}
	}
	if units := len(e.attackWindowUnits(0, map[state.ObjID]bool{golem: true})); units != 11 {
		t.Fatalf("precondition: the offer-gate window (only the golem excluded) has %d units, want 11", units)
	}
	// The offer gate admits the pair: with only the taxed creature excluded the
	// window can reach the charge.
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{golem: true}, map[state.ObjID]bool{golem: true}) {
		t.Fatal("precondition: the golem's {9} charge is unaffordable even excluding only the golem")
	}
	// The declaration is not: with all four creatures excluded only the eight
	// Forests remain, fewer than {9}. The two reads really differ.
	all := map[state.ObjID]bool{golem: true}
	for _, id := range dorks {
		all[id] = true
	}
	if units := len(e.attackWindowUnits(0, all)); units != 8 {
		t.Fatalf("precondition: the declaration window (all four attackers excluded) has %d units, want 8", units)
	}
	if e.combatChargeAffordable(0, ch, all, all) {
		t.Fatal("precondition: the {9} charge is affordable with every attacker excluded, so the fixture cannot produce the divergence")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("precondition: pending = %+v, want seat 0's attackers decision", d)
	}
	// PRECONDITION: the taxed pair really is offered -- the rejection below is
	// the declaration check, not a missing option.
	opt := findAttackOption(d, golem, 1)
	if opt == nil {
		t.Fatalf("precondition: the flat {9} pair is not offered: %+v", d.Options)
	}
	if opt.Value != 9 {
		t.Fatalf("precondition: offered golem option Value = %d, want 9", opt.Value)
	}
	// The declaration's combined published Value carries each declared mana
	// dork's own forgone unit (attackOptionBudgetValue), so it sums over the
	// published MaxSum and the wire rejects it. Summing the actual Values here
	// keeps the test honest about what the wire is asked to judge.
	choices := []int{opt.Index}
	sum := opt.Value
	for _, id := range dorks {
		o := findAttackOption(d, id, 1)
		if o == nil {
			t.Fatalf("precondition: mana dork %d is not offered: %+v", id, d.Options)
		}
		choices = append(choices, o.Index)
		sum += o.Value
	}
	if !d.HasBudget() || d.MaxSum != 11 {
		t.Fatalf("precondition: budget = %v/%d, want a published MaxSum of 11", d.HasBudget(), d.MaxSum)
	}

	err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices})
	if err == nil {
		t.Fatal("the unpayable combined declaration was accepted; fail-closed behaviour regressed")
	}
	// The wire's budget rejection is the layer that now stops the recorded
	// crash: the folded Values (9 + 3x1 = 12) sum over the MaxSum (11). Without
	// the fold the same declaration would instead reach the engine's
	// whole-declaration check and return the "is not payable" diagnostic.
	if !strings.Contains(err.Error(), "exceeds the budget") {
		t.Fatalf("rejection = %q, want the published-budget rejection over the folded sum %d/MaxSum %d", err.Error(), sum, d.MaxSum)
	}
	if strings.Contains(err.Error(), "is not payable") {
		t.Fatalf("rejection = %q, want the wire to catch the declaration before the engine diagnostic", err.Error())
	}
	// The engine belt re-derives the same folded currency and rejects the same
	// overflow when the wire is bypassed: the two reads cannot disagree.
	bypass := *d
	bypass.MaxSum = 0
	bypass.Budgeted = false
	if berr := e.validateAttackers(&bypass, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); berr == nil {
		t.Fatal("the engine belt accepted the declaration the wire rejected; the two reads disagree")
	} else if !strings.Contains(berr.Error(), "exceeds the affordable") {
		t.Fatalf("belt rejection = %q, want the folded-overflow belt diagnostic", berr.Error())
	}
	// The taxed pair must not have been committed: the declaration was refused
	// whole, and the pending decision survives for a legal answer.
	if o := e.G.Obj(golem); o == nil || o.IsAttacking {
		t.Fatal("the rejected declaration still committed the taxed attacker")
	}
	if p := e.Pending(); p == nil || p.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want the attackers decision preserved after the rejection", p)
	}
}

// TestWholeDeclarationDiagnosticNamesMana reaches the engine's whole-
// declaration diagnostic through Submit. The charge is {1} plus a tap
// obligation on two creatures: the folded wire budgets only the {1} and the
// attacker's own unit (1 + 1 = 2 over a budget of 3, so Decision.Validate
// accepts), but the tap obligation is met from the two mana-dork creatures,
// and declaring one of them as an attacker leaves only one tap candidate. The
// whole declaration is therefore unpayable even though each offered pair is
// affordable, and the rejection must name the mana and the fail-closed flag
// instead of printing only the all-zero non-mana parts.
func TestWholeDeclarationDiagnosticNamesMana(t *testing.T) {
	e := threeSeatEngine(t)
	golem := onBoardReadyCard(t, e, 0, card(t, tapTaxFixture))
	d1 := onBoardReadyCard(t, e, 0, card(t, manaDorkFixture))
	d2 := onBoardReadyCard(t, e, 0, card(t, manaDorkFixture))
	onBoardCard(t, e, 0, card(t, forestFixture))
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	// PRECONDITION: the charge is priceable mana 1 WITH a tap obligation on two
	// creatures, and the two dorks are ready battlefield creatures that can
	// satisfy it -- the tap obligation is exactly the component the wire does
	// not budget.
	g := e.G.Obj(golem)
	if g == nil || g.Zone != state.ZBattlefield || g.Controller != 0 || g.SummonSick {
		t.Fatalf("precondition: Tap Tax Golem = %+v, want a ready battlefield creature controlled by seat 0", g)
	}
	ch := e.attackPairCharge(golem, 1, 0)
	if ch.unpriceable || ch.mana != 1 || len(ch.taps) != 1 || ch.taps[0].n != 2 || ch.life != 0 {
		t.Fatalf("precondition: golem charge = %+v, want priceable mana 1 plus a two-creature tap obligation", ch)
	}
	for _, id := range []state.ObjID{d1, d2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.SummonSick {
			t.Fatalf("precondition: tap-obligation creature %d = %+v, want a ready battlefield creature controlled by seat 0", id, o)
		}
	}
	if ch.zero() {
		t.Fatal("precondition: charge reads zero, so the rejection below cannot exercise the non-zero guard")
	}
	// The offer gate admits the pair: excluding only the golem it can tap both
	// dorks and still reach the {1} from the Forest.
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{golem: true}, map[state.ObjID]bool{golem: true}) {
		t.Fatal("precondition: the {1} + tap-two charge is unaffordable even excluding only the golem")
	}
	// Declaring one dork removes it from the tap candidates, leaving one for a
	// two-creature obligation. The two reads really differ.
	declared := map[state.ObjID]bool{golem: true, d1: true}
	if e.combatChargeAffordable(0, ch, declared, declared) {
		t.Fatal("precondition: the charge is affordable with a tap candidate declared, so the fixture cannot produce the divergence")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("precondition: pending = %+v, want seat 0's attackers decision", d)
	}
	gopt := findAttackOption(d, golem, 1)
	dopt := findAttackOption(d, d1, 1)
	if gopt == nil || dopt == nil {
		t.Fatalf("precondition: pair missing (golem=%v dork=%v): %+v", gopt, dopt, d.Options)
	}
	if gopt.Value != 1 || gopt.CostTaps != 2 {
		t.Fatalf("precondition: golem option Value/CostTaps = %d/%d, want 1/2", gopt.Value, gopt.CostTaps)
	}
	// PRECONDITION: the folded Values fit the published budget, so the wire
	// lets the declaration through and the engine diagnostic is what rejects
	// it -- the layer this ticket fixed.
	if !d.HasBudget() || gopt.Value+dopt.Value > d.MaxSum {
		t.Fatalf("precondition: folded sum %d exceeds MaxSum %d, so Validate rejects before the engine check", gopt.Value+dopt.Value, d.MaxSum)
	}

	err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{gopt.Index, dopt.Index}})
	if err == nil {
		t.Fatal("the unpayable tap-obligation declaration was accepted; fail-closed behaviour regressed")
	}
	if !strings.Contains(err.Error(), "is not payable") {
		t.Fatalf("rejection = %q, want the declaration-payability diagnostic", err.Error())
	}
	// The diagnostic must name the mana component and the fail-closed flag.
	// Before the fix it printed only the non-mana parts, hiding the real {1}.
	if !strings.Contains(err.Error(), "1 mana") || !strings.Contains(err.Error(), "1 taps") || !strings.Contains(err.Error(), "unpriceable=false") {
		t.Fatalf("rejection %q does not name the mana, the tap obligation and the fail-closed flag", err.Error())
	}
	if o := e.G.Obj(golem); o == nil || o.IsAttacking {
		t.Fatal("the rejected declaration still committed the taxed attacker")
	}
	if p := e.Pending(); p == nil || p.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want the attackers decision preserved after the rejection", p)
	}
}

// TestFlatAttackTaxSingleAttackerIsPayable is the fail-closed guard's
// counterpart: the same flat-tax fixture with ONLY the taxed creature declared
// leaves the payer's ten non-attacker forests untapped and in the window, so
// the {9} is payable and Submit accepts. The diagnostic fix must not have
// narrowed this.
func TestFlatAttackTaxSingleAttackerIsPayable(t *testing.T) {
	e := threeSeatEngine(t)
	golem := onBoardReadyCard(t, e, 0, card(t, flatAttackTaxFixture))
	for i := 0; i < 10; i++ {
		onBoardCard(t, e, 0, card(t, forestFixture))
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers

	ch := e.attackPairCharge(golem, 1, 0)
	if ch.unpriceable || ch.mana != 9 {
		t.Fatalf("precondition: golem charge = %+v, want priceable mana 9", ch)
	}
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{golem: true}, map[state.ObjID]bool{golem: true}) {
		t.Fatal("precondition: the single-attacker {9} charge reads unaffordable")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("precondition: pending = %+v, want an attackers decision", d)
	}
	opt := findAttackOption(d, golem, 1)
	if opt == nil {
		t.Fatalf("the payable single-attacker {9} pair is not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit the payable single-attacker {9} declaration: %v", err)
	}
	// The {9} does not settle from an empty pool, so the declaration opens the
	// attack payment window: tap one forest per ask until it closes.
	for taps := 0; taps < 12; taps++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "attack_mana" {
			break
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("tap a forest for the {9} attack cost: %v", err)
		}
	}
	if o := e.G.Obj(golem); o == nil || !o.IsAttacking {
		t.Fatal("the payable taxed attacker was never declared")
	}
	drainCombatPriority(t, e)
}
