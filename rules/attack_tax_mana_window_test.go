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
// 11612818469535247757) hits exactly this: Myr Prototype has 9 +1/+1 counters
// ({9} tax), the board offers six attackers, and the bot declares all six. The
// rejection message used to print only the non-mana components
// "(0 life, 0 taps, ...)" -- all zero -- which reads as an unpriceable FREE
// charge. It is not unpriceable at all: blockCharge.unpriceable is false and
// blockCharge.mana is the real, unpaid component. The tests below pin the
// structural failure and the diagnostic that names the mana.

// flatAttackTaxFixture charges a flat {9} to attack, independent of the board
// and of the declaration's size: the Myr Prototype shape (Cost$ Y on a
// Card.Self CantAttackUnless static), with the SVar folded away to a literal
// so the fixture is deterministic without a counter engine.
const flatAttackTaxFixture = "Name:Flat Tax Golem\nManaCost:3\nTypes:Creature Golem\nPT:3/3\n" +
	"S:Mode$ CantAttackUnless | ValidCard$ Card.Self | Cost$ 9 | Description$ CARDNAME can't attack unless you pay {9}.\n" +
	"Oracle:x\n"

// manaDorkFixture is the taxed creature's sibling: a creature whose only role
// is to tap for {G}, and which the policy also offers as a free attacker.
const manaDorkFixture = "Name:Mana Dork\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:1/1\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G\nOracle:x\n"

// forestFixture is an untapped basic Forest: a pure, non-attacker mana source.
const forestFixture = "Name:Test Forest\nTypes:Basic Land Forest\nOracle:x\n"

// TestZeroComponentUnpriceableAttackDeclaration is the structural regression:
// an offered flat {9} attack-tax pair is admitted (the offer gate excludes only
// the taxed creature, leaving eleven window units), but a declaration that also
// names the three mana-dork creatures leaves only the eight Forests in the
// payment window, so the whole declaration is unpayable and Submit must reject
// it. The rejected charge carries mana 9 with every non-mana component zero --
// the misleading shape the recorded report saw -- and the message must name the
// mana so it cannot be mistaken for an unpriceable FREE charge.
func TestZeroComponentUnpriceableAttackDeclaration(t *testing.T) {
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
	// component zero and unpriceable false -- the exact shape the report
	// misread -- and the other three attackers are creatures that are ALSO
	// window mana sources, so declaring them removes their units.
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
	if units := len(e.attackWindowUnits(0, map[state.ObjID]bool{golem: true})); units == 0 {
		t.Fatal("precondition: the offer-gate window (only the golem excluded) has no mana sources")
	}
	// The offer gate admits the pair: with only the taxed creature excluded the
	// window can reach the charge.
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{golem: true}) {
		t.Fatal("precondition: the golem's {9} charge is unaffordable even excluding only the golem")
	}
	// The declaration is not: with all four creatures excluded only the eight
	// Forests remain, fewer than {9}. The two reads really differ.
	all := map[state.ObjID]bool{golem: true}
	for _, id := range dorks {
		all[id] = true
	}
	if e.combatChargeAffordable(0, ch, all) {
		t.Fatal("precondition: the {9} charge is affordable with every attacker excluded, so the fixture cannot produce the divergence")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("precondition: pending = %+v, want seat 0's attackers decision", d)
	}
	// PRECONDITION: the taxed pair really is offered -- the rejection below is
	// the whole-declaration check, not a missing option.
	opt := findAttackOption(d, golem, 1)
	if opt == nil {
		t.Fatalf("precondition: the flat {9} pair is not offered: %+v", d.Options)
	}
	if opt.Value != 9 {
		t.Fatalf("precondition: offered golem option Value = %d, want 9", opt.Value)
	}
	// PRECONDITION: the combined Value sum of the declaration the test submits
	// fits the published MaxSum, so Decision.Validate lets it through and the
	// engine's board-aware check is what rejects it (the recorded shape).
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
	if d.HasBudget() && sum > d.MaxSum {
		t.Fatalf("precondition: declaration Value sum %d exceeds MaxSum %d, so Validate rejects before the engine check", sum, d.MaxSum)
	}

	err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices})
	if err == nil {
		t.Fatal("the unpayable combined declaration was accepted; fail-closed behaviour regressed")
	}
	if !strings.Contains(err.Error(), "is not payable") {
		t.Fatalf("rejection = %q, want the declaration-payability diagnostic", err.Error())
	}
	// The diagnostic must name the mana component. Before the fix it printed
	// only the non-mana parts -- all zero -- which reads as an unpriceable FREE
	// charge and hid the real {9} the payer could not raise.
	if !strings.Contains(err.Error(), "9 mana") || !strings.Contains(err.Error(), "unpriceable=false") {
		t.Fatalf("rejection %q does not name the unpaid 9 mana charge and its fail-closed flag; the all-zero non-mana fields misread as a free unpriceable charge", err.Error())
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

// TestFlatAttackTaxSingleAttackerIsPayable is the fail-closed guard's
// counterpart: the same fixture with ONLY the taxed creature declared leaves
// the payer's ten non-attacker forests untapped and in the window, so the {9}
// is payable and Submit accepts. The diagnostic fix must not have narrowed
// this.
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
	if !e.combatChargeAffordable(0, ch, map[state.ObjID]bool{golem: true}) {
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
