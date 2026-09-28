package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Trig:RolledDie's leaf (real corpus Mr. House, President and CEO). His whole
// commander kit is one activated ability -- "{4}, {T}: Roll a six-sided die
// plus ..." -- and one trigger -- "Whenever you roll a 4 or higher, create a
// 3/3 colorless Robot artifact creature token. If you rolled 6 or higher,
// instead create that token and a Treasure token." The trigger is
// T:Mode$ RolledDie | ValidResult$ GE4 | Execute$ TrigBranch, where TrigBranch
// is a DB$ Branch on BranchConditionSVar$ TriggerCount$Result compared GE6:
// the roll result must reach the trigger BODY's own SVar resolution, after the
// RollDice resolution that produced it has finished (the TriggerResult
// provenance), for the 6-branch to mint the extra Treasure.
//
// The engine's rng is seeded, so the die is deterministic per seed; the three
// pinned seeds below cover a roll below the filter (6 -> 3), the plain GE4
// branch (4 -> 5) and the GE6 branch (19 -> 6), all measured at the current
// corpus pin.

// activateRollDiceAbility submits the pending priority option that activates
// obj's RollDice ability (the rollAbilityOption shape flipcoin_test.go uses
// for FlipCoin), then drains the stack -- the roll Note, any RolledDie
// trigger it queues, and the trigger's DB$ Branch body all resolve.
func activateRollDiceAbility(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to activate RollDice from: %+v", d)
	}
	idx := -1
	face := e.G.Obj(obj).Face()
	for _, o := range d.Options {
		if o.Kind != "ability" || o.Obj != obj || face == nil ||
			o.Ability < 0 || o.Ability >= len(face.Abilities) {
			continue
		}
		if face.Abilities[o.Ability].API == "RollDice" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("RollDice ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit RollDice activation: %v", err)
	}
	passUntilStackEmpty(t, e, 80)
}

// rolledDieScenario activates real corpus Mr. House's one RollDice ability
// for the given seed and returns the engine, its Config (for replayCheck) and
// the die result the canonical roll Note recorded.
func rolledDieScenario(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, int32) {
	t.Helper()
	e, cfg := flipEngine(t, reg, seed,
		[]*cards.Card{lookup(t, reg, "Mr. House, President and CEO")}, nil)
	moveByName(t, e, 0, "Mr. House, President and CEO", state.ZBattlefield)
	// Summoning sickness clears naturally: drive to seat 0's NEXT turn rather
	// than writing Object.SummonSick directly, which is an unlogged state
	// mutation replayCheck would (correctly) catch.
	driveToTurn(t, e, 2, 0)
	id := firstCreature(t, e, 0)
	addMana(t, e, 0, "CCCC")
	activateRollDiceAbility(t, e, id)
	rolls := 0
	var result int32
	for _, ev := range e.L.Events {
		if _, _, _, res, ok := effects.DieRollResult(ev); ok {
			rolls++
			result = res
		}
	}
	if rolls != 1 {
		t.Fatalf("seed %d: want exactly one die roll Note, got %d", seed, rolls)
	}
	return e, cfg, result
}

// TestRolledDieMrHouseTriggersAndBranchesOnTheResult is the end-to-end leaf:
//
//   - seed 6 rolls 3, below the ValidResult$ GE4 filter, so the trigger does
//     not fire and no token is created;
//   - seed 4 rolls 5, above GE4 but below the Branch's GE6, so the trigger
//     fires and creates exactly one Robot and no Treasure;
//   - seed 19 rolls 6, so the Branch's true leg runs and creates the Robot
//     PLUS a Treasure -- proof that TriggerCount$Result reached the trigger
//     body's own SVar resolution after the roll resolved.
func TestRolledDieMrHouseTriggersAndBranchesOnTheResult(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	eLow, _, low := rolledDieScenario(t, reg, 6)
	if low >= 4 {
		t.Fatalf("pinned seed 6 now rolls %d; want a result below the GE4 filter", low)
	}
	if got := tokensNamed(eLow, 0, "Robot"); got != 0 {
		t.Fatalf("roll %d (< GE4): %d Robot tokens, want 0 (trigger must not fire)", low, got)
	}

	eMid, _, mid := rolledDieScenario(t, reg, 4)
	if mid < 4 || mid >= 6 {
		t.Fatalf("pinned seed 4 now rolls %d; want a result in [4,5]", mid)
	}
	if got := tokensNamed(eMid, 0, "Robot"); got != 1 {
		t.Fatalf("roll %d (GE4): %d Robot tokens, want 1", mid, got)
	}
	if got := tokensNamed(eMid, 0, "Treasure"); got != 0 {
		t.Fatalf("roll %d (below GE6): %d Treasure tokens, want 0", mid, got)
	}

	eHigh, cfgHigh, high := rolledDieScenario(t, reg, 19)
	if high != 6 {
		t.Fatalf("pinned seed 19 now rolls %d; want a 6 for the GE6 branch", high)
	}
	if got := tokensNamed(eHigh, 0, "Robot"); got != 1 {
		t.Fatalf("roll 6: %d Robot tokens, want 1", got)
	}
	if got := tokensNamed(eHigh, 0, "Treasure"); got != 1 {
		t.Fatalf("roll 6: %d Treasure tokens, want 1 (the DB$ Branch GE6 leg)", got)
	}

	// The roll's own resolution must be replay-exact: the seeded rng and the
	// roll Notes it produced fold back to the same chain.
	replayCheck(t, eHigh, cfgHigh)
}

// TestRolledDieFiresOncePerDie confirms the per-die cadence the trigger's
// Natural$/Number$ readers assume: a RollDice that rolls several dice emits
// one canonical roll Note per die, each carrying its own result, so a
// multi-die roll fires a per-die trigger once per die. Mr. House's own
// ability rolls one die with no Treasure spent; this drives a scripted
// two-die roll through effects.DieRollNote and checks the decoder the matcher
// uses sees two independent results.
func TestRolledDieFiresOncePerDie(t *testing.T) {
	t.Parallel()
	first := effects.DieRollNote(7, 1, 6, 3, 4)
	if _, sides, natural, result, ok := effects.DieRollResult(first); !ok ||
		sides != 6 || natural != 3 || result != 4 {
		t.Fatalf("die Note round-trip = (%d,%d,%d,%v), want (6,3,4,true)", sides, natural, result, ok)
	}
	second := effects.DieRollNote(7, 1, 6, 6, 6)
	if _, _, natural, result, ok := effects.DieRollResult(second); !ok || natural != 6 || result != 6 {
		t.Fatalf("second die Note round-trip failed: natural=%d result=%d ok=%v", natural, result, ok)
	}
	// A non-roll Note is not a die roll: the matcher must not fire on it.
	if _, _, _, _, ok := effects.DieRollResult(
		events.Event{Kind: 0}); ok {
		t.Fatal("a zero event decoded as a die roll")
	}
	// The per-die decoder must NOT accept the batch Note, and the batch
	// decoder must NOT accept a per-die Note: the two modes can never
	// cross-fire on each other's event.
	batch := effects.DieRollBatchNote(7, 1, 2, 6, 6)
	if _, _, _, _, ok := effects.DieRollResult(batch); ok {
		t.Fatal("a batch Note decoded as a per-die roll")
	}
	if _, _, _, _, ok := effects.DieRollBatchResult(first); ok {
		t.Fatal("a per-die Note decoded as a batch roll")
	}
}

// rolledDieOnceScenario places real corpus Celebr-8000 (a real multi-die roll
// trigger: "At the beginning of combat on your turn, roll two six-sided
// dice") and real corpus Feywild Trickster (a real Mode$ RolledDieOnce
// trigger: "Whenever you roll one or more dice, create a 1/1 blue Faerie
// Dragon creature token with flying") on seat 0's battlefield, then drives to
// seat 0's turn-2 Main1 so turn 1's begin-combat roll has fully resolved.
// Returns the engine, its Config and the number of canonical batch Notes
// those rolls emitted.
func rolledDieOnceScenario(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, int) {
	t.Helper()
	e, cfg := flipEngine(t, reg, seed,
		[]*cards.Card{lookup(t, reg, "Celebr-8000"), lookup(t, reg, "Feywild Trickster")}, nil)
	moveByName(t, e, 0, "Celebr-8000", state.ZBattlefield)
	moveByName(t, e, 0, "Feywild Trickster", state.ZBattlefield)
	driveToTurn(t, e, 2, 0)
	batches := 0
	for _, ev := range e.L.Events {
		if _, _, _, _, ok := effects.DieRollBatchResult(ev); ok {
			batches++
		}
	}
	return e, cfg, batches
}

// TestRolledDieOnceFiresOncePerMultiDieRoll is the Mode$ RolledDieOnce leaf.
// Celebr-8000 rolls TWO dice at the beginning of combat; Feywild Trickster's
// real compiled "whenever you roll one or more dice" trigger must fire ONCE
// for that one roll action -- a per-die cadence would create two Faerie
// Dragons. Exactly one batch Note per roll action, carrying both dice, is the
// boundary the Once matcher reads.
func TestRolledDieOnceFiresOncePerMultiDieRoll(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, batches := rolledDieOnceScenario(t, reg, 3)
	if batches != 1 {
		t.Fatalf("begin-combat two-die roll emitted %d batch Notes, want 1 (one per roll action)", batches)
	}
	// The roll itself emitted one Note per die, so this really was a
	// multi-die roll, not a one-die roll that trivially fired once.
	dice := 0
	for _, ev := range e.L.Events {
		if _, _, _, _, ok := effects.DieRollResult(ev); ok {
			dice++
		}
	}
	if dice != 2 {
		t.Fatalf("begin-combat roll emitted %d per-die Notes, want 2", dice)
	}
	if got := tokensNamed(e, 0, "Faerie Dragon"); got != 1 {
		t.Fatalf("two-die roll fired the RolledDieOnce trigger %d time(s), want 1 (once per roll action)", got)
	}
	replayCheck(t, e, cfg)
}

// TestRolledDieOnceFaridehDrawsOnTheBatchMax is the end-to-end leaf for the
// second roll head, TriggerCountMax$Result, on real corpus Farideh, Devil's
// Chosen. Her trigger is
// T:Mode$ RolledDieOnce | ValidPlayer$ You,
// SVar:TrigPump:DB$ Pump | Defined$ Self | KW$ Flying & Menace | SubAbility$ DBDraw,
// SVar:DBDraw:DB$ Draw | ConditionCheckSVar$ DiceResult | ConditionSVarCompare$ GE10,
// SVar:DiceResult:TriggerCountMax$Result -- "if any of those results was 10 or
// higher, draw a card".
//
// The canonical multi-die batch Note is emitted directly (the same encoding
// the RollDice primitive produces, decoded by DieRollBatchResult), so the test
// can pin a batch whose INDIVIDUAL results straddle the threshold: [11, 9] has
// its LAST die (the batch Note's reported result) below 10 and its maximum at
// 10+, so a draw proves the head reads the batch MAX, not the last/per-die
// result -- regressing the capture to the reported result leaves draws at 0. The control batch [9, 9] leaves
// the draw silent while the pump still applies, proving the condition is the
// only thing gated and the trigger itself fired in both cases.
func TestRolledDieOnceFaridehDrawsOnTheBatchMax(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	roll := func(t *testing.T, seed uint64, results []int32) (*Engine, Config, int, state.ObjID) {
		t.Helper()
		e, cfg := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Farideh, Devil's Chosen")}, nil)
		id := moveByName(t, e, 0, "Farideh, Devil's Chosen", state.ZBattlefield)
		from := len(e.L.Events)
		var max int32
		for _, r := range results {
			// One canonical per-die Note per die, then the per-resolution batch
			// Note the RolledDieOnce matcher reads.
			e.emit(effects.DieRollNote(id, 0, 20, r, r))
			if r > max {
				max = r
			}
		}
		e.emit(effects.DieRollBatchNote(id, 0, int32(len(results)), max, results[len(results)-1]))
		e.priorityRound()
		passUntilStackEmpty(t, e, 40)
		return e, cfg, from, id
	}

	// [11, 9]: the highest die clears the threshold while the last (reported)
	// die does not, so Farideh draws only if the head reads the batch MAX; the
	// pump's Flying & Menace land regardless of the roll.
	e, cfg, from, id := roll(t, 3, []int32{11, 9})
	if got := logDrawsFor(e, from, 0); got != 1 {
		t.Fatalf("batch max 11 (>=10): drew %d cards, want 1 (the TriggerCountMax$Result head)", got)
	}
	if !e.HasKeyword(id, "Flying") || !e.HasKeyword(id, "Menace") {
		t.Fatalf("batch max 11: Farideh missing the pump keywords (Flying=%v Menace=%v)",
			e.HasKeyword(id, "Flying"), e.HasKeyword(id, "Menace"))
	}
	replayCheck(t, e, cfg)

	// [9, 9]: every die is below 10, so the draw is silent -- but the trigger
	// still fired, so the pump keywords still apply. This is the negative the
	// head must respect.
	eLow, _, fromLow, idLow := roll(t, 3, []int32{9, 9})
	if got := logDrawsFor(eLow, fromLow, 0); got != 0 {
		t.Fatalf("batch max 9 (<10): drew %d cards, want 0", got)
	}
	if !eLow.HasKeyword(idLow, "Flying") || !eLow.HasKeyword(idLow, "Menace") {
		t.Fatalf("batch max 9: Farideh missing the pump keywords (Flying=%v Menace=%v)",
			eLow.HasKeyword(idLow, "Flying"), eLow.HasKeyword(idLow, "Menace"))
	}
}

// TestRolledDieNumberFiresOnlyOnTheThirdDieEachTurn pins the Number$ 3 gate
// on real corpus Resolute Veggiesaur ("Whenever you roll your third die each
// turn, put a +1/+1 counter on CARDNAME"): three real roll Notes for the same
// permanent in one turn queue the trigger exactly once (on the third), a
// fourth does not fire again, and the per-turn count re-arms after the turn
// changes so three more rolls fire it a second time. The die Notes are the
// canonical encoding the DB$ RollDice primitive emits, and e.emit runs the
// ordinary trigger scan, so the whole matcher + queue-point gate is
// exercised.
func TestRolledDieNumberFiresOnlyOnTheThirdDieEachTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := flipEngine(t, reg, 5, []*cards.Card{lookup(t, reg, "Resolute Veggiesaur")}, nil)
	id := moveByName(t, e, 0, "Resolute Veggiesaur", state.ZBattlefield)
	roll := func(n int) {
		for i := 0; i < n; i++ {
			e.emit(effects.DieRollNote(id, 0, 6, 1, 1))
		}
		// A directly emitted Note queues its trigger as a PENDING trigger;
		// priorityRound is what pushes it onto the stack, and
		// passUntilStackEmpty then resolves it.
		e.priorityRound()
		passUntilStackEmpty(t, e, 40)
	}
	roll(2) // dice 1 and 2: the Number$ 3 line is silent
	if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
		t.Fatalf("after two dice in one turn: %d counters, want 0 (fires on the third only)", got)
	}
	roll(1) // die 3: fires
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("after the third die: %d counters, want 1", got)
	}
	roll(1) // die 4: past the gate, does not fire again
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("after a fourth die: %d counters, want 1 (the gate is exactly the third)", got)
	}
	// The count is per TURN: after the turn changes, three fresh dice fire
	// the line a second time (the counter re-arms rather than staying spent).
	driveToTurn(t, e, 2, 0)
	roll(3)
	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("after three dice in the next turn: %d counters, want 2 (the per-turn count re-arms)", got)
	}
}
