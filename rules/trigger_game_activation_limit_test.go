package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const gameLimitedPhaseTrigger = "Name:OncePhase\nManaCost:0\nTypes:Creature\nPT:2/2\n" +
	"T:Mode$ Phase | Phase$ Main2 | ValidPlayer$ You | TriggerZones$ Battlefield | GameActivationLimit$ 1 | Execute$ TrigDraw\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"

func TestTriggerGameActivationLimitPhaseSurvivesTurnAndClone(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 71, gameLimitedPhaseTrigger)
	moveByName(t, e, 0, "OncePhase", state.ZBattlefield)
	tr := crTriggerFixture(t, e, id, "Phase", "Draw")
	if tr.Params["GameActivationLimit"] != "1" || tr.Params["Phase"] != "Main2" {
		t.Fatalf("fixture trigger parameters changed: %+v", tr.Params)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Active != 0 {
		t.Fatalf("precondition: source/active player = %+v/%d", e.G.Obj(id), e.G.Active)
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
	if got := queuedPhaseTriggers(e, id); got != 1 {
		t.Fatalf("first eligible phase queued %d triggers, want 1", got)
	}
	e.pendingTriggers = nil
	clone := e.Clone()
	cloneKey := triggerKey{Source: id, Idx: 0, Face: 0}
	if clone.triggerGameFires == nil || clone.triggerGameFires[cloneKey].N != 1 {
		t.Fatalf("clone did not preserve game trigger count: %+v", clone.triggerGameFires)
	}
	f := clone.triggerGameFires[cloneKey]
	f.N++
	clone.triggerGameFires[cloneKey] = f
	if e.triggerGameFires[cloneKey].N != 1 {
		t.Fatal("clone shares game trigger count map with original")
	}

	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if e.G.Turn != 2 || e.G.Active != 0 {
		t.Fatalf("precondition: turn/active after TurnChange = %d/%d", e.G.Turn, e.G.Active)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
	if got := queuedPhaseTriggers(e, id); got != 0 {
		t.Fatalf("game-limited trigger re-fired on a later turn: %d", got)
	}

	clone.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	clone.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
	if got := queuedPhaseTriggers(clone, id); got != 0 {
		t.Fatalf("cloned engine re-armed game-limited trigger: %d", got)
	}
}

func TestTriggerGameActivationLimitRealCorpusAcrobaticCheerleader(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Acrobatic Cheerleader")
	if !ok {
		t.Fatal("corpus fixture: Acrobatic Cheerleader missing")
	}
	e := New(seatZeroStart(Config{Seed: 72, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append(mountainDeck(t, 40), card), mountainDeck(t, 41),
	}}))
	e.Advance()
	id := crAbortMove(t, e, 0, "Acrobatic Cheerleader", state.ZBattlefield)
	e.pending = nil
	tr := crTriggerFixture(t, e, id, "Phase", "PutCounter")
	if tr.Params["GameActivationLimit"] != "1" || tr.Params["Phase"] != "Main" || tr.Params["PhaseCount"] != "2" || tr.Params["ValidPlayer"] != "You" {
		t.Fatalf("Acrobatic Cheerleader trigger parameters changed: %+v", tr.Params)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Active != 0 {
		t.Fatalf("precondition: source/active player = %+v/%d", e.G.Obj(id), e.G.Active)
	}
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if !e.G.Obj(id).Tapped {
		t.Fatal("precondition: source was not tapped")
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
	if got := queuedPhaseTriggers(e, id); got != 1 {
		t.Fatalf("Acrobatic Cheerleader did not queue on eligible second main: %d", got)
	}
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
	if got := queuedPhaseTriggers(e, id); got != 0 {
		t.Fatalf("Acrobatic Cheerleader queued again after its game limit: %d", got)
	}
}

// gameLimitedBlocksTrigger is the dedicated-hook fixture: a Mode$ Blocks line
// with GameActivationLimit$ 1. Blocks rides checkBlocksTriggers, whose gate
// runs BEFORE its pair scan -- the shape the round-1 defect miscounted (an
// event whose pairs match nothing consumed the lifetime use at the gate).
const gameLimitedBlocksTrigger = "Name:OnceBlocker\nManaCost:0\nTypes:Creature\nPT:2/2\n" +
	"T:Mode$ Blocks | TriggerZones$ Battlefield | GameActivationLimit$ 1 | Execute$ TrigDraw\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"

// TestTriggerGameActivationLimitBlocksEmptyEventDoesNotConsumeUse pins the
// queue-point reservation for the dedicated combat hooks: a DeclareBlockers
// event whose pairs match NOTHING must queue nothing and leave the lifetime
// count untouched -- otherwise the first no-candidate combat would exhaust a
// GameActivationLimit$ 1 line before it ever fired.
func TestTriggerGameActivationLimitBlocksEmptyEventDoesNotConsumeUse(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 73, gameLimitedBlocksTrigger,
		"Name:PairAttacker\nManaCost:0\nTypes:Creature\nPT:2/2\nOracle:x\n")
	blk := moveByName(t, e, 0, "OnceBlocker", state.ZBattlefield)
	atk := moveByName(t, e, 0, "PairAttacker", state.ZBattlefield)
	tr := crTriggerFixture(t, e, blk, "Blocks", "Draw")
	if tr.Params["GameActivationLimit"] != "1" {
		t.Fatalf("fixture trigger parameters changed: %+v", tr.Params)
	}
	if e.G.Obj(blk).Zone != state.ZBattlefield || e.G.Obj(atk).Zone != state.ZBattlefield {
		t.Fatalf("precondition: blocker/attacker zones = %v/%v", e.G.Obj(blk).Zone, e.G.Obj(atk).Zone)
	}
	key := triggerKey{Source: blk, Idx: 0, Face: 0}

	// First event: no pairs, so no candidate can match. Nothing queues and
	// the game count must stay 0.
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1, Pairs: nil})
	if got := queuedPhaseTriggers(e, blk); got != 0 {
		t.Fatalf("empty-pair declare-blockers queued %d triggers, want 0", got)
	}
	if e.triggerGameFires[key].N != 0 {
		t.Fatalf("empty-pair declare-blockers consumed a game use: gameFires=%d", e.triggerGameFires[key].N)
	}

	// Second event: a matching pair. The trigger queues exactly once and
	// consumes its one use HERE, not at the empty event above.
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1, Pairs: [][2]state.ObjID{{atk, blk}}})
	if got := queuedPhaseTriggers(e, blk); got != 1 {
		t.Fatalf("matching declare-blockers queued %d triggers, want 1", got)
	}
	if e.triggerGameFires[key].N != 1 {
		t.Fatalf("matching declare-blockers did not consume its game use: gameFires=%d", e.triggerGameFires[key].N)
	}
	e.pendingTriggers = nil

	// Third event: the limit is spent; nothing queues.
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1, Pairs: [][2]state.ObjID{{atk, blk}}})
	if got := queuedPhaseTriggers(e, blk); got != 0 {
		t.Fatalf("post-limit declare-blockers queued %d triggers, want 0", got)
	}
}

// gameAndTurnLimitedTapsTrigger is the combined-parameter fixture: Mode$ Taps
// (an actionTriggerMode, so BOTH gates apply) carrying ActivationLimit$ 1 and
// GameActivationLimit$ 2 -- the shape the round-1 defect miscounted (the
// second same-turn match consumed a game use at the game gate before the
// per-turn gate rejected it, so the turn-2 use was wrongly spent).
const gameAndTurnLimitedTapsTrigger = "Name:TwiceTap\nManaCost:0\nTypes:Creature\nPT:2/2\n" +
	"T:Mode$ Taps | TriggerZones$ Battlefield | ValidCard$ Card.Self | ActivationLimit$ 1 | GameActivationLimit$ 2 | Execute$ TrigDraw\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"

// TestTriggerGameActivationLimitWithPerTurnLimitKeepsGameUseAcrossTurn pins
// the read-then-commit discipline across BOTH gates: a same-turn match the
// per-turn ActivationLimit$ gate rejects must not consume the
// GameActivationLimit$ count, so the line still has its second game use on
// the next turn.
func TestTriggerGameActivationLimitWithPerTurnLimitKeepsGameUseAcrossTurn(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 74, gameAndTurnLimitedTapsTrigger)
	moveByName(t, e, 0, "TwiceTap", state.ZBattlefield)
	tr := crTriggerFixture(t, e, id, "Taps", "Draw")
	if tr.Params["ActivationLimit"] != "1" || tr.Params["GameActivationLimit"] != "2" {
		t.Fatalf("fixture trigger parameters changed: %+v", tr.Params)
	}
	if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Active != 0 {
		t.Fatalf("precondition: source/active player = %+v/%d", e.G.Obj(id), e.G.Active)
	}
	key := triggerKey{Source: id, Idx: 0, Face: 0}

	// Turn 1, first tap: queues once, spending one game use and one turn use.
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if !e.G.Obj(id).Tapped {
		t.Fatal("precondition: source was not tapped by the first Tap event")
	}
	if got := queuedPhaseTriggers(e, id); got != 1 {
		t.Fatalf("first tap queued %d triggers, want 1", got)
	}
	e.pendingTriggers = nil

	// Turn 1, second tap: the per-turn gate rejects it. The game count must
	// still be 1 -- a rejected match commits nothing.
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if got := queuedPhaseTriggers(e, id); got != 0 {
		t.Fatalf("second same-turn tap queued %d triggers, want 0 (per-turn limit)", got)
	}
	if e.triggerGameFires[key].N != 1 {
		t.Fatalf("rejected same-turn match consumed a game use: gameFires=%d", e.triggerGameFires[key].N)
	}
	e.pendingTriggers = nil

	// Turn 2: the per-turn gate re-arms, and the game limit still has its
	// second use -- the tap queues, and the next tap is exhausted.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if e.G.Turn != 2 || e.G.Active != 0 {
		t.Fatalf("precondition: turn/active after TurnChange = %d/%d", e.G.Turn, e.G.Active)
	}
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if got := queuedPhaseTriggers(e, id); got != 1 {
		t.Fatalf("turn-2 tap queued %d triggers, want 1 (gameFires=%d: a rejected match must not spend the second game use)", got, e.triggerGameFires[key].N)
	}
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.Tap, Obj: id})
	if got := queuedPhaseTriggers(e, id); got != 0 {
		t.Fatalf("turn-2 second tap queued %d triggers, want 0 (game limit exhausted)", got)
	}
}
