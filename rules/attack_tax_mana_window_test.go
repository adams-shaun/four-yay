// attackcost-manawindow: the published attack-declaration mana budget must
// account for the sources a declaration COMMITS. A creature declared as
// attacking cannot also be tapped for mana (CR 508.1), so a CantAttackUnless
// tax on one attacker can be paid only from the sources the declaration does
// NOT commit; attackBudget/Decision.MaxSum counted every source, so a bot
// could submit a declaration whose folded cost (tax + the production the
// declared mana creatures forgo) exceeded what the remaining sources can pay,
// and validateAttackers rejected it -- which host.runMatch turns into a
// crashed table (a deterministic livelock: the bot re-derives the same
// answer).
//
// This is the multi-source shape from the filed report: Myr Prototype at a
// {9} counter tax with THREE separate one-unit mana-dork creatures and eight
// Forests. The fix folds each offered pair's own production into its
// published Option.Value whenever the declaration carries a mana tax
// (rules' attackOptionBudgetValue), so the ONE cumulative-budget rule the
// engine validates with (Decision.MaxSum / Decision.Validate) and the bot
// repairs with (Clamp -> FitRequired -> ChargeOptionConstraints) reads the
// same currency the board-aware check uses. These tests pin the shape end to
// end: the folded Values differ across boards, the production bot's OWN
// answer is accepted by Submit, and a taxed attacker still attacks legally
// when the only source it must forgo is itself.
//
// The sibling suite rules/attack_tax_mana_source_test.go covers the
// single-source (Oasis Ritualist, two units) shape; this one covers the
// report's several-separate-one-unit-dorks shape and the
// exclusion-of-the-taxed-attacker-itself direction.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// manaWindowDork is a one-unit creature mana source: a free {G} tap.
const manaWindowDork = "Name:Window Dork\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G | Oracle:x\n"

// manaWindowForest is a plain one-unit land source.
const manaWindowForest = "Name:Window Forest\nTypes:Basic Land Forest\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G | Oracle:x\n"

// attackWindowSeat builds the report's board on seat 0, parked in the
// declare-attackers step: Myr Prototype carrying `counters` +1/+1 counters
// (its CantAttackUnless Cost$ Y is {1} per counter), `dorks` one-unit mana
// creatures, and `forests` plain lands. Every creature is attack-ready. It
// returns the engine, the Myr's id, and the dork ids in zone order.
func attackWindowSeat(t *testing.T, counters int32, dorks, forests int) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	myr := onBoardReadyCard(t, e, 0, mshCorpusCard(t, "Myr Prototype"))
	e.G.Obj(myr).AddCounter("P1P1", counters)
	ids := make([]state.ObjID, 0, dorks)
	for i := 0; i < dorks; i++ {
		ids = append(ids, onBoardReady(t, e, 0, manaWindowDork))
	}
	for i := 0; i < forests; i++ {
		onBoard(t, e, 0, manaWindowForest)
	}
	e.G.Active = 0
	e.G.Step = state.StepDeclareAttackers
	return e, myr, ids
}

// TestBotAttackDeclarationFitsManaWindowWithDeclaredAttackers is the filed
// report's board: a {9} Myr Prototype tax, three one-unit mana dorks, and
// eight Forests. The full declaration would fold to 9 (Myr) + 1+1+1 (the
// three committed dorks) = 12 against a published budget of 11 -- one over.
// The production bot's answer must be a payable subset Submit accepts, and
// the declaration it leaves must still raise the {9} from the sources it did
// NOT commit.
func TestBotAttackDeclarationFitsManaWindowWithDeclaredAttackers(t *testing.T) {
	e, myr, dorks := attackWindowSeat(t, 9, 3, 8)

	// PRECONDITION the whole test depends on: Myr is a battlefield creature
	// with nine +1/+1 counters and prices to exactly {9}.
	if o := e.G.Obj(myr); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 9 {
		t.Fatalf("precondition: Myr Prototype = %+v, want 9 +1/+1 counters on the battlefield", o)
	}
	charge := e.attackPairCharge(myr, 1, 0)
	if charge.unpriceable || charge.mana != 9 {
		t.Fatalf("precondition: Myr Prototype attack charge = %+v, want a priceable {9}", charge)
	}
	// Each dork is a real one-unit source, and the two budgets under
	// comparison genuinely differ: committing all three removes three units.
	selfUnits := e.attackSourceUnits(0)
	for i, d := range dorks {
		if got := selfUnits[d]; got != 1 {
			t.Fatalf("precondition: dork %d contributes %d units, want 1", i, got)
		}
	}
	if all, committed := e.attackBudget(0), e.attackBudget(0)-3*selfUnits[dorks[0]]; all != 11 || committed != 8 {
		t.Fatalf("precondition: budget with all three dorks = %d, without = %d, want 11 / 8", all, committed)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || d.Player != 0 {
		t.Fatalf("pending = %+v, want seat 0's attackers", d)
	}
	myrOpt := findAttackOptionOrFail(t, d, myr, 1)
	if d.MaxSum != 11 {
		t.Fatalf("published MaxSum = %d, want 11 (eight Forests + three one-unit dorks)", d.MaxSum)
	}
	dorkOpts := make([]*decision.Option, 0, len(dorks))
	for _, dk := range dorks {
		dorkOpts = append(dorkOpts, findAttackOptionOrFail(t, d, dk, 1))
	}

	// The production bot's OWN answer must be a payable subset, and it must
	// include the taxed creature or the test proves nothing about the window.
	bot := newTestBot(7)
	in := bot.answer(e, d)
	if len(in.Choices) == 0 {
		t.Fatal("the bot passed the declaration; this board must be worth attacking on")
	}
	chosenMyr := false
	committedUnits := int32(0)
	for _, ci := range in.Choices {
		o := d.Options[ci]
		if o.Obj == myr {
			chosenMyr = true
		}
		committedUnits += selfUnits[o.Obj]
	}
	if !chosenMyr {
		t.Fatalf("the bot's answer %v omits the taxed creature; the window claim is vacuous", in.Choices)
	}
	// The declaration's OWN window (budget minus the sources it commits) must
	// still cover the declared mana charge -- the exact board-aware rule
	// validateAttackers applies. Asserting the real values here is what makes
	// a regression that widens the fold or shrinks the window fail by name.
	if window := e.attackBudget(0) - committedUnits; window < charge.mana {
		t.Fatalf("the bot's answer %v commits %d units, leaving %d, under the %d charge",
			in.Choices, committedUnits, window, charge.mana)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("the bot's own declaration %v (budget %d) was rejected by Submit: %v", in.Choices, d.MaxSum, err)
	}
	// Drive any mana payment window the declaration opened, so a declaration
	// accepted but left unpayable fails here too.
	driveAttackPayWindow(t, e)
	if o := e.G.Obj(myr); o == nil || !o.IsAttacking {
		t.Fatal("Myr Prototype was not actually declared attacking")
	}

	// Pin the published currency the shared rule reads: Myr pays its {9} and
	// each dork's free attack must forgo its {G}. These are the values the
	// fix folds; without the fold they are 0, and the declaration above can be
	// assembled over budget. Placed AFTER the end-to-end Submit so a revert
	// first fails on the real crash path.
	if myrOpt.Value != 9 {
		t.Fatalf("Myr folded Value = %d, want 9 (its tax; Myr is not a mana source)", myrOpt.Value)
	}
	for _, o := range dorkOpts {
		if o.Value != 1 {
			t.Fatalf("dork %d folded Value = %d, want 1 (its forgone {G})", o.Obj, o.Value)
		}
	}
	// The naive whole-declaration answer (Myr + all three dorks) folds to
	// 9 + 1 + 1 + 1 = 12 > 11 and must be refused by the shared wire rule --
	// this is the declaration that crashed tables before the fold.
	naive := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{myrOpt.Index}}
	for _, o := range dorkOpts {
		naive.Choices = append(naive.Choices, o.Index)
	}
	if err := d.Validate(naive); err == nil {
		t.Fatal("the folded budget admitted the over-committing declaration (9 + three dorks = 12 > 11)")
	}
}

// TestBotAttackDeclarationSingleTaxedAttackerNeedsOnlyItsOwnExclusion pins
// the direction the fix must NOT over-prune: when the taxed creature is the
// only one that can attack, excluding IT is the whole exclusion, and the
// eight Forests still raise the {9}. A blanket "exclude every offered
// attacker" repair would wrongly deny this legal declaration.
func TestBotAttackDeclarationSingleTaxedAttackerNeedsOnlyItsOwnExclusion(t *testing.T) {
	e, myr, dorks := attackWindowSeat(t, 9, 3, 8)
	// Make the dorks unable to attack (a Wall's Defender), so the only
	// offered pair is Myr's, while their mana production still counts toward
	// the budget the window may tap.
	for _, dk := range dorks {
		o := e.G.Obj(dk)
		o.Face().Keywords = append(o.Face().Keywords, "Defender")
	}
	e.staticEpoch = -1
	if !e.attackPairCharge(myr, 1, 0).payable() {
		t.Fatal("precondition: Myr's charge must be priceable")
	}
	selfUnits := e.attackSourceUnits(0)
	if selfUnits[myr] != 0 {
		t.Fatalf("precondition: Myr contributes %d units, want 0 (not a mana source)", selfUnits[myr])
	}
	if got := e.attackBudget(0); got != 11 {
		t.Fatalf("precondition: budget = %d, want 11 (three dorks + eight Forests)", got)
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want attackers", d)
	}
	myrOpt := findAttackOptionOrFail(t, d, myr, 1)
	for _, dk := range dorks {
		if o := findAttackOption(d, dk, 1); o != nil {
			t.Fatalf("precondition: a Defender dork was offered as an attacker: %+v", o)
		}
	}
	// Excluding only Myr leaves all 11 units, so the single-attacker
	// declaration is legal and must not be pruned.
	if myrOpt.Value != 9 || d.MaxSum != 11 {
		t.Fatalf("Myr Value = %d, MaxSum = %d, want 9 / 11", myrOpt.Value, d.MaxSum)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{myrOpt.Index}}); err != nil {
		t.Fatalf("the single taxed attacker (excluding only itself) was rejected: %v", err)
	}
	driveAttackPayWindow(t, e)
	if o := e.G.Obj(myr); o == nil || !o.IsAttacking {
		t.Fatal("Myr Prototype was not actually declared attacking")
	}
	// Nine mana sources must have been tapped to pay the {9} (the pool starts
	// empty), proving the window really settled rather than the charge being
	// free. The Defender dorks are still untapped mana sources the window may
	// tap, so count every Window-named permanent.
	tapped := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || !o.Tapped || o.Face() == nil {
			continue
		}
		if o.Face().Name == "Window Forest" || o.Face().Name == "Window Dork" {
			tapped++
		}
	}
	if tapped != 9 {
		t.Fatalf("tapped mana sources = %d, want 9 (the {9} charge really paid)", tapped)
	}
}

// TestAttackWindowFoldIsInertWithoutAManaTax pins the byte-identity guard
// from the other side: with zero counters Myr's charge is free, no mana tax
// exists, so no fold applies and every option keeps Value 0 with MaxSum 0.
func TestAttackWindowFoldIsInertWithoutAManaTax(t *testing.T) {
	e, myr, _ := attackWindowSeat(t, 0, 3, 8)
	if ch := e.attackPairCharge(myr, 1, 0); ch.unpriceable {
		t.Fatal("precondition: the untaxed board must be priceable")
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
}
