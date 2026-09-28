package rules

// R:Event$ RollDice replacement effects (task rolldice-repl). The corpus
// carries exactly four R:Event$ RollDice lines (measured with
// `grep -rlE 'R:Event$ RollDice' .cards/cardsfolder | wc -l`):
//
//   - Wyll, Blade of Frontiers, Barbarian Class, Pixie Guide: the SAME
//     PlusRoll shape -- `ReplaceWith$ PlusRoll`, whose body is the
//     ReplaceEffect pair that writes Number from
//     ReplaceCount$Number/Plus.1 and then Ignore from
//     ReplaceCount$Ignore/Plus.1 ("instead roll that many dice plus one and
//     ignore the lowest roll");
//   - Vedalken Squirrel-Whacker: ReplaceWith$ SwapRoll with ValidSides$ 6 --
//     a body (VarName$ DicePTExchanges) this build cannot model, so it is
//     skipped loudly and the roll proceeds unmodified (fail closed).
//
// The real corpus cards drive everything: Mother Kangaroo's ETB trigger
// rolls one d6 and puts that many +1/+1 counters on itself (the
// Count$Result reader, which counts only the RETAINED dice once the
// ignored-lowest is dropped), Valiant Endeavor is the choose-one-result
// ask.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// dieRolls collects the per-die roll results in log order.
func dieRolls(e *Engine) []int32 {
	var out []int32
	for _, ev := range e.L.Events {
		if _, _, _, res, ok := effects.DieRollResult(ev); ok {
			out = append(out, res)
		}
	}
	return out
}

// rollBatchNotes counts the one-per-action batch roll Notes.
func rollBatchNotes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if _, _, _, _, ok := effects.DieRollBatchResult(ev); ok {
			n++
		}
	}
	return n
}

// unimplementedRollNotes counts the loud unimplemented-RollDice-replacement
// notes in the log.
func unimplementedRollNotes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented RollDice replacement" {
			n++
		}
	}
	return n
}

// rollReplacementOnBoard asserts the real precondition the tests depend on:
// the corpus card is on seat 0's battlefield and its compiled R:Event$
// RollDice line is linked to its ReplaceWith$ SVar body.
func rollReplacementOnBoard(t *testing.T, e *Engine, id state.ObjID, name string) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s not on the battlefield: %+v", name, o)
	}
	for i := range o.Face().Repls {
		r := &o.Face().Repls[i]
		if r.Event == "RollDice" && r.With != nil && r.With.API == "ReplaceEffect" {
			return
		}
	}
	t.Fatalf("precondition: %s carries no R:Event$ RollDice replacement linked to a ReplaceEffect body: %+v", name, o.Face().Repls)
}

// retainedInRollOrder replicates retainedDice's documented selection (stable
// value sort, ties drop earlier dice first) for a replacement-ignored count,
// so a test can name the dice the roll's own consumers see.
func retainedInRollOrder(rolls []int32, ignore int32) []int32 {
	type indexed struct {
		value int32
		index int
	}
	ordered := make([]indexed, len(rolls))
	for i, v := range rolls {
		ordered[i] = indexed{value: v, index: i}
	}
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].value < ordered[j-1].value; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	dropped := make([]bool, len(ordered))
	for i := 0; i < int(ignore) && i < len(ordered); i++ {
		dropped[ordered[i].index] = true
	}
	out := make([]int32, 0, len(rolls))
	for i, v := range rolls {
		if !dropped[i] {
			out = append(out, v)
		}
	}
	return out
}

// kangarooRollFixture places the named roll-replacement carriers and Mother
// Kangaroo on seat 0's battlefield, resolves Kangaroo's ETB trigger (the
// roll and its counter), then drains the carriers' own RolledDieOnce
// trigger. It returns the engine, the first carrier's id, Kangaroo's id and
// the dice the KANGAROO roll produced (the carriers' own entry rolls, if
// any, are excluded from the slice).
func kangarooRollFixture(t *testing.T, carriers ...string) (*Engine, state.ObjID, state.ObjID, []int32) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	extras := make([]*cards.Card, 0, len(carriers)+1)
	for _, name := range carriers {
		extras = append(extras, lookup(t, reg, name))
	}
	extras = append(extras, lookup(t, reg, "Mother Kangaroo"))
	e, _ := flipEngine(t, reg, 7, extras, nil)
	first := state.ObjID(0)
	for i, name := range carriers {
		id := moveByName(t, e, 0, name, state.ZBattlefield)
		rollReplacementOnBoard(t, e, id, name)
		if i == 0 {
			first = id
		}
	}
	// The carriers are placed (and any entry rolls of their own are spent)
	// before the snapshot, so the slice below is exactly Kangaroo's roll.
	before := len(dieRolls(e))
	kang := moveByName(t, e, 0, "Mother Kangaroo", state.ZBattlefield)
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("precondition: Kangaroo ETB trigger not queued: stack %d", len(e.G.Stack))
	}
	e.resolveTop()
	if got := e.G.Obj(kang).Counter("P1P1"); got < 1 {
		t.Fatalf("precondition: Kangaroo roll put %d +1/+1 counters", got)
	}
	// Drain the carriers' RolledDieOnce trigger so later counts see a quiet
	// board.
	e.putTriggersOnStack()
	if len(e.G.Stack) > 0 {
		e.resolveTop()
	}
	return e, first, kang, dieRolls(e)[before:]
}

// TestWyllRollDiceReplacementAddsDieAndIgnoresLowest is the Wyll leaf: with
// Wyll on the battlefield, Mother Kangaroo's one-die ETB roll rolls TWO dice
// (the replacement's Plus.1 on Number) and the ignored-lowest die does not
// count toward the published Result the counter sub reads. Wyll's own
// RolledDieOnce trigger still fires exactly once for the roll ACTION.
func TestWyllRollDiceReplacementAddsDieAndIgnoresLowest(t *testing.T) {
	t.Parallel()
	e, wyll, kang, rolls := kangarooRollFixture(t, "Wyll, Blade of Frontiers")
	if n := len(rolls); n != 2 {
		t.Fatalf("want two dice rolled (1 + the replacement's one), got %d (%v)", n, rolls)
	}
	if rolls[0] == rolls[1] {
		t.Fatalf("precondition: the two dice tie (%v), so ignoring the lowest is unobservable", rolls)
	}
	hi, sum := rolls[0], rolls[0]+rolls[1]
	if rolls[1] > hi {
		hi = rolls[1]
	}
	retained := retainedInRollOrder(rolls, 1)
	if len(retained) != 1 || retained[0] != hi {
		t.Fatalf("retained = %v, want the one non-ignored die of %v", retained, rolls)
	}
	if got := e.G.Obj(kang).Counter("P1P1"); got != retained[0] {
		t.Fatalf("Kangaroo put %d +1/+1 counters, want %d (dice %v, lowest ignored)", got, retained[0], rolls)
	}
	if got := e.G.Obj(kang).Counter("P1P1"); got == sum {
		t.Fatalf("Kangaroo put %d counters = the unignored sum: the ignored-lowest die counted", got)
	}
	if got := e.G.Obj(wyll).Counter("P1P1"); got != 1 {
		t.Fatalf("Wyll got %d +1/+1 counters from its RolledDieOnce trigger, want 1", got)
	}
}

// TestRollDiceReplacementCompositionAppliesEachOnce: two carriers (Wyll and
// Pixie Guide) both match one roll; each applies once to the held proposal
// in deterministic scan order (1 + 1 + 1 = 3 dice, 0 + 1 + 1 = 2 ignored),
// and the whole batch is still ONE roll action to the once-per-action
// trigger.
func TestRollDiceReplacementCompositionAppliesEachOnce(t *testing.T) {
	t.Parallel()
	e, wyll, kang, rolls := kangarooRollFixture(t, "Wyll, Blade of Frontiers", "Pixie Guide")
	if n := len(rolls); n != 3 {
		t.Fatalf("want three dice rolled (1 + one per carrier), got %d (%v)", n, rolls)
	}
	hi := rolls[0]
	sum := int32(0)
	for _, r := range rolls {
		sum += r
		if r > hi {
			hi = r
		}
	}
	if sum == hi {
		t.Fatalf("precondition: dice %v cannot show a dropped die (sum %d == max %d)", rolls, sum, hi)
	}
	// Two of the three dice are ignored, so the published Result is the one
	// retained die: the highest.
	retained := retainedInRollOrder(rolls, 2)
	if len(retained) != 1 || retained[0] != hi {
		t.Fatalf("retained = %v, want the single highest die of %v", retained, rolls)
	}
	if got := e.G.Obj(kang).Counter("P1P1"); got != retained[0] {
		t.Fatalf("Kangaroo put %d +1/+1 counters, want %d (dice %v, two lowest ignored)", got, retained[0], rolls)
	}
	if got := e.G.Obj(wyll).Counter("P1P1"); got != 1 {
		t.Fatalf("Wyll got %d +1/+1 counters, want 1 (one roll action, not one per replacement)", got)
	}
	if got := rollBatchNotes(e); got != 1 {
		t.Fatalf("got %d batch roll notes, want 1", got)
	}
}

// TestRollDiceSwapRollCarrierIsFailClosed: Vedalken Squirrel-Whacker's
// SwapRoll body (VarName$ DicePTExchanges) cannot be modelled, so the
// matched replacement is skipped LOUDLY and Kangaroo's roll proceeds with
// exactly its own one die.
func TestRollDiceSwapRollCarrierIsFailClosed(t *testing.T) {
	t.Parallel()
	e, _, _, rolls := kangarooRollFixture(t, "Vedalken Squirrel-Whacker")
	if n := len(rolls); n != 1 {
		t.Fatalf("the unmodelled SwapRoll carrier must not alter the dice count, got %d dice (%v)", n, rolls)
	}
	if got := unimplementedRollNotes(e); got < 1 {
		t.Fatal("the unmodelled SwapRoll carrier left no loud unimplemented-RollDice-replacement note")
	}
}

// TestWyllRollReplacementResumedAskDoesNotReroll: Valiant Endeavor (the
// choose-one-result ask) under Wyll rolls 2+1 = 3 dice and ignores the
// lowest, so the ask offers the two RETAINED results; answering it
// publishes from the carried dice and re-rolls NOTHING -- the resumed
// resolution never re-applies the replacement.
func TestWyllRollReplacementResumedAskDoesNotReroll(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := flipEngine(t, reg, 11, []*cards.Card{
		lookup(t, reg, "Wyll, Blade of Frontiers"),
		lookup(t, reg, "Valiant Endeavor"),
	}, nil)
	wyll := moveByName(t, e, 0, "Wyll, Blade of Frontiers", state.ZBattlefield)
	rollReplacementOnBoard(t, e, wyll, "Wyll, Blade of Frontiers")
	// Valiant Endeavor must be IN HAND to be cast: move it there unless the
	// opening hand already holds it (a hand->hand move is not a move).
	valiant := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Valiant Endeavor" {
			valiant = id
		}
	}
	if valiant == 0 {
		valiant = moveByName(t, e, 0, "Valiant Endeavor", state.ZHand)
	}
	addMana(t, e, 0, "WWWWWW")
	d := castFixture(t, e, valiant, -1)
	rolls := dieRolls(e)
	if n := len(rolls); n != 3 {
		t.Fatalf("want three dice rolled (2 + the replacement's one), got %d (%v)", n, rolls)
	}
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "roll" || len(d.Options) != 2 {
		t.Fatalf("precondition: expected the choose-one-result ask over the two retained dice, got %+v", d)
	}
	retained := retainedInRollOrder(rolls, 1)
	if len(d.Rolls) != 2 || d.Rolls[0] != retained[0] || d.Rolls[1] != retained[1] {
		t.Fatalf("precondition: the ask carries %v, want the retained pair of %v", d.Rolls, rolls)
	}
	pick := d.Options[1].Index // not the default first option
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 60)
	if got := len(dieRolls(e)); got != 3 {
		t.Fatalf("answering the ask rolled %d more dice (total %d): the resumed roll re-applied the replacement", got-3, got)
	}
	// The chosen result is the picked retained die, the other the sum of the
	// rest -- the Knight token count reads the other result.
	other := retained[0] + retained[1] - retained[pick]
	knights := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Knight Token" {
			knights++
		}
	}
	if knights != int(other) {
		t.Fatalf("got %d Knight tokens, want %d (retained %v, picked option %d)", knights, other, retained, pick)
	}
}
