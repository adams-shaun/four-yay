// attackcost-manatax: the published attack-declaration budget must count a
// declared mana source's own production as SPENT. A creature that attacks
// cannot also be tapped for mana (CR 508.1), so a declaration that includes a
// mana-source attacker can only pay its CantAttackUnless tax with what the
// OTHER sources produce. Before this fix attackBudget (rules/attack_cost.go)
// and the published Decision.MaxSum counted every source INCLUDING the
// would-be attackers, so a bot could submit a declaration whose folded price
// (tax + the production the attackers forgo) exceeded what the remaining
// sources could pay; validateAttackers rejected the whole declaration and a
// hosted bot rejection crashes the table.
//
// The fix folds each offered pair's own mana production into its published
// Option.Value (rules' attackOptionBudgetValue) whenever the declaration
// carries a mana tax, so Decision.Validate, decision.RequiredQuota /
// FitRequired and botpolicy.Clamp -- the ONE cumulative-budget rule -- all
// read the same currency the engine's whole-declaration check uses. These
// tests pin the shape end to end: the folded values differ with and without
// the mana creature, the bot's OWN answer is accepted by Submit, a hand-built
// declaration that leaves the source uncommitted is legal, one where it
// attacks with enough OTHER mana is legal, and a zero-tax attack is unchanged.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// manaTaxLand is a plain untapped {C} source: the "other mana" that funds a
// generic attack tax when the mana creature is not committed.
const manaTaxLand = "Name:Test Wastes\nTypes:Basic Land\nA:AB$ Mana | Cost$ T | Produced$ C | Oracle:x\n"

// attackTaxManaSourceSeat stages the bug board on a fresh engine: Myr
// Prototype carrying counters (its CantAttackUnless Cost$ Y is {1} per
// +1/+1 counter), Oasis Ritualist (a creature whose BEST mana production is
// two units: the Exert ability adds {2}), `others` plain {C} lands, and one
// vanilla creature, all on seat 0, parked in the declare-attackers step. It
// returns the engine and the three creature ids.
func attackTaxManaSourceSeat(t *testing.T, counters int32, others int) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	myr := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Myr Prototype"))
	ritualist := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Oasis Ritualist"))
	bear := onBoardReady(t, e, 0, "Name:Tax Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(myr).AddCounter("P1P1", counters)
	for i := 0; i < others; i++ {
		onBoardCard(t, e, 0, card(t, manaTaxLand))
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	return e, myr, ritualist, bear
}

// TestAttackTaxAttackingManaSourceBotAnswer is the brief's failure shape: a
// {9} Myr Prototype tax plus an Oasis Ritualist whose two units are the only
// reason the naive per-pair budget (9) looks reachable while the declaration's
// folded cost (tax 9 + Ritualist 2 = 11) is not. The bot's own answer must be
// a payable subset Submit accepts, and both a Ritualist-free declaration and
// one where the Ritualist attacks with enough OTHER mana must be legal.
func TestAttackTaxAttackingManaSourceBotAnswer(t *testing.T) {
	t.Parallel()
	// Board A: seven {C} lands + Ritualist's two units == the published
	// budget 9 that the buggy MaxSum handed the bot.
	e, myr, ritualist, bear := attackTaxManaSourceSeat(t, 9, 7)

	// PRECONDITION: the tax and the source preconditions the rule reads are
	// both real -- Myr carries 9 counters on the battlefield and its charge
	// prices to exactly {9}, and the Ritualist's best production is 2.
	if o := e.G.Obj(myr); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 9 {
		t.Fatalf("precondition: Myr Prototype = %+v, want 9 +1/+1 counters on the battlefield", o)
	}
	charge := e.attackPairCharge(myr, 1, 0)
	if charge.unpriceable || charge.mana != 9 {
		t.Fatalf("precondition: Myr Prototype attack charge = %+v, want a priceable {9}", charge)
	}
	selfUnits := e.attackSourceUnits(0)
	if got := selfUnits[ritualist]; got != 2 {
		t.Fatalf("precondition: Oasis Ritualist contributes %d units, want 2 (T: Any + Exert {2})", got)
	}
	// The compared budgets must actually differ: excluding the mana creature
	// drops the reachable total by its two units.
	if all, without := e.attackBudget(0), e.attackBudget(0)-selfUnits[ritualist]; all != 9 || without != 7 {
		t.Fatalf("precondition: budget with Ritualist = %d, without = %d, want 9 / 7", all, without)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's attackers", d)
	}
	myrOpt := findAttackOption(d, myr, 1)
	ritOpt := findAttackOption(d, ritualist, 1)
	if myrOpt == nil || ritOpt == nil {
		t.Fatalf("precondition: Myr/Ritualist pairs must both be offered: %+v", d.Options)
	}
	// The published budget cost is the folded one: Myr pays {9} (no own
	// production), the Ritualist's free attack must forgo its two units.
	if d.MaxSum != 9 {
		t.Fatalf("published MaxSum = %d, want 9 (the payer's reachable total)", d.MaxSum)
	}
	if myrOpt.Value != 9 || ritOpt.Value != 2 {
		t.Fatalf("folded option Values = Myr %d / Ritualist %d, want 9 / 2", myrOpt.Value, ritOpt.Value)
	}

	// The naive whole-declaration answer (Myr + Ritualist + bear) sums to
	// 9 + 2 + 0 = 11 > 9 and must be rejected -- this is what crashed tables.
	bearOpt := findAttackOptionOrFail(t, d, bear, 1)
	naive := decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{myrOpt.Index, ritOpt.Index, bearOpt.Index}}
	if got := myrOpt.Value + ritOpt.Value + bearOpt.Value; got != 11 {
		t.Fatalf("precondition: naive declaration folds to %d, want 11", got)
	}
	if err := d.Validate(naive); err == nil {
		t.Fatal("the folded budget admitted the over-tax declaration (9 + Ritualist's 2 > 9)")
	}

	// A hand-built declaration that leaves the Ritualist uncommitted is
	// legal: Myr (9) + bear (0) == 9.
	legalFree := decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{myrOpt.Index, bearOpt.Index}}
	if err := d.Validate(legalFree); err != nil {
		t.Fatalf("a legal declaration leaving the mana source uncommitted was rejected: %v", err)
	}

	// The bot's OWN answer must already be a payable subset, and it must
	// actually include the taxed creature or the test proves nothing.
	bot := newTestBot(7)
	in := bot.answer(e, d)
	if len(in.Choices) == 0 {
		t.Fatal("the bot passed the declaration; this board must be worth attacking on")
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("the bot's own declaration %v (budget %d) was rejected by Submit: %v", in.Choices, d.MaxSum, err)
	}
	// Drive any mana payment window the declaration opened, so a stranded
	// window (a declaration accepted but unpayable) fails here too.
	driveAttackPayWindow(t, e)
}

// TestAttackTaxAttackingManaSourceWithOtherManaIsLegal is the other direction:
// with NINE other {C} lands the payer's total is eleven, so the folded
// declaration Myr (9) + Ritualist (2) is exactly affordable and must Submit.
// This proves the fix does not blanket-ban a mana creature from attacking.
func TestAttackTaxAttackingManaSourceWithOtherManaIsLegal(t *testing.T) {
	t.Parallel()
	e, myr, ritualist, _ := attackTaxManaSourceSeat(t, 9, 9)
	if o := e.G.Obj(myr); o == nil || o.Counter("P1P1") != 9 {
		t.Fatal("precondition: Myr Prototype is not a 9-counter battlefield creature")
	}
	if got := e.attackSourceUnits(0)[ritualist]; got != 2 {
		t.Fatalf("precondition: Ritualist contributes %d units, want 2", got)
	}
	if all := e.attackBudget(0); all != 11 {
		t.Fatalf("precondition: budget = %d, want 11 (nine lands + Ritualist's two)", all)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want attackers", d)
	}
	myrOpt := findAttackOption(d, myr, 1)
	ritOpt := findAttackOption(d, ritualist, 1)
	if myrOpt == nil || ritOpt == nil {
		t.Fatalf("precondition: both pairs must be offered: %+v", d.Options)
	}
	if myrOpt.Value+ritOpt.Value != 11 || d.MaxSum != 11 {
		t.Fatalf("folded Myr+Ritualist = %d against MaxSum %d, want 11 / 11", myrOpt.Value+ritOpt.Value, d.MaxSum)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{myrOpt.Index, ritOpt.Index}}
	if err := e.Submit(in); err != nil {
		t.Fatalf("Ritualist attacking with enough OTHER mana was rejected: %v", err)
	}
	// Settle the {9} payment window (the nine lands pay it; the Ritualist is
	// committed to attacking and cannot tap), then confirm the declaration
	// really landed.
	driveAttackPayWindow(t, e)
	if o := e.G.Obj(ritualist); o == nil || !o.IsAttacking {
		t.Fatal("the Ritualist was not actually declared attacking")
	}
	if o := e.G.Obj(myr); o == nil || !o.IsAttacking {
		t.Fatal("Myr Prototype was not actually declared attacking")
	}
}

// TestAttackTaxZeroTaxManaSourceAttacksFree pins the byte-identity guard: with
// no counters Myr's charge resolves to free, no mana tax exists, so no fold
// applies and the mana creature attacks at Value 0 with MaxSum 0 (omitted).
func TestAttackTaxZeroTaxManaSourceAttacksFree(t *testing.T) {
	t.Parallel()
	e, myr, ritualist, _ := attackTaxManaSourceSeat(t, 0, 2)
	if o := e.G.Obj(myr); o == nil || o.Counter("P1P1") != 0 {
		t.Fatal("precondition: Myr Prototype must be a 0-counter battlefield creature")
	}
	if ch := e.attackPairCharge(myr, 1, 0); !ch.zero() || ch.unpriceable {
		t.Fatalf("precondition: 0-counter charge = %+v, want a priceable free charge", ch)
	}
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want attackers", d)
	}
	if d.MaxSum != 0 {
		t.Fatalf("a zero-tax declaration published MaxSum = %d, want 0 (no fold, byte-identical)", d.MaxSum)
	}
	for _, o := range d.Options {
		if o.Value != 0 {
			t.Fatalf("a zero-tax option carries Value %d, want 0: %+v", o.Value, o)
		}
	}
	myrOpt := findAttackOption(d, myr, 1)
	ritOpt := findAttackOption(d, ritualist, 1)
	if myrOpt == nil || ritOpt == nil {
		t.Fatalf("precondition: both free pairs must be offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{myrOpt.Index, ritOpt.Index}}); err != nil {
		t.Fatalf("a zero-tax attack by the mana creature was rejected: %v", err)
	}
}

// findAttackOptionOrFail is findAttackOption with a loud precondition failure.
func findAttackOptionOrFail(t *testing.T, d *decision.Decision, obj state.ObjID, def state.PlayerID) *decision.Option {
	t.Helper()
	o := findAttackOption(d, obj, def)
	if o == nil {
		t.Fatalf("no offered pair for object %d at defender %d: %+v", obj, def, d.Options)
	}
	return o
}

// driveAttackPayWindow answers the declare-attackers mana payment window
// (rules' chooseAttackPay) until the attack is committed or another decision
// kind is posed. It stops after a bounded number of taps, so a window that
// never settles fails loudly rather than hanging.
func driveAttackPayWindow(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 32; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 {
			return
		}
		if d.Options[0].Kind != "attack_mana" && d.Options[0].Kind != "mana" {
			return
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("answer the attack payment window: %v", err)
		}
	}
	t.Fatal("the attack payment window never settled (bounded 32 taps)")
}
