// Task 28 (Ruling T23-x): the draw-step draw is a turn-based action (CR
// 504.1) that must run exactly once, on entry to the step, no matter what
// resolves later in the step. Before this fix it was inferred inside
// priorityRound from a Passes/Priority proxy for "the step just began" --
// see turn.go's advanceStep for the full defect writeup -- and a mandatory
// "whenever you draw a card" trigger resolving during the draw step made
// that proxy true again, drawing a second card, and a third, until the
// library ran out.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// drawTriggerSrc is the Forge shape the brief names: a MANDATORY (no
// OptionalDecider$) "whenever you draw a card" trigger, scoped to its own
// controller's draws only (ValidCard$ Card.YouOwn) so it does not also fire
// on an opponent's draw step in the 4-seat variant.
const drawTriggerSrc = `Name:Chronicler
ManaCost:U
Types:Enchantment
T:Mode$ ChangesZone | Origin$ Library | Destination$ Hand | ValidCard$ Card.YouOwn | Execute$ TrigGain | TriggerDescription$ gain 1 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`

// driveToStep answers every pending KPriority decision with "pass" (mountain
// decks have no creatures, so combat's own askAttackers/askBlockers always
// auto-skip to the next step and never interrupt this) until the engine
// reaches turn, seat active's own turn, at step -- and then stops, leaving
// whatever is pending there (a plain priority ask, or a trigger decision if
// one fired on the way in) for the caller to inspect. It fails the test
// outright on a non-priority decision or a finished game encountered before
// arrival, or if the pass budget below is exhausted first -- which bounds
// this helper the same way maxTriggerFires bounds the engine itself, rather
// than let a real regression hang the test suite.
func driveToStep(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s (stopped at turn %d seat %d step %s)",
				turn, active, step, e.G.Turn, e.G.Active, e.G.Step)
		}
		if answerIfDiscard(t, e) {
			// CR 514.1: a cleanup-step discard crossed on the way to the
			// target step. Answer it naively (first-Max) and keep driving;
			// it is a real, expected decision, not a stopping condition.
			continue
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v encountered while driving to turn %d seat %d step %s",
				d, turn, active, step)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s within the pass budget", turn, active, step)
}

// passThroughStep answers "pass" priority decisions for as long as the
// engine's current step is still `step`, stopping the instant it changes (or
// on a non-priority decision, or the game ending) -- so, unlike passAll, a
// caller can bound a measurement to exactly one step regardless of how many
// passes that step and its neighbours actually need. Against the pre-Task-28
// code this is what lets the draw step's own runaway show up as "hit the
// limit without Step ever changing" instead of silently running into the
// NEXT step's decisions too.
func passThroughStep(t *testing.T, e *Engine, step state.Step, limit int) int {
	t.Helper()
	n := 0
	for ; n < limit && !e.G.Over && e.G.Step == step; n++ {
		if answerIfDiscard(t, e) {
			// CR 514.1: answer a cleanup discard naively; it does not end the
			// step and must not be counted as a pass (the step it happens in
			// is the caller's own bound, but a StepNeverChanges failure should
			// never be a discard stuck un-answered).
			continue
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return n
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	return n
}

// assertDrawPrecedesPriorityInStep is Fix round 1 (review Important #1b):
// the brief calls "the Draw event now precedes the step's own Priority
// event" the one externally observable consequence of moving the draw from
// priorityRound to advanceStep, and nothing had asserted it -- moving the
// e.drawCard call to AFTER advanceStep's trailing Priority emit (instead of
// before it) left the whole rules suite green.
//
// since is the log index captured by the caller just before driving into the
// draw step. This locates that transition's own StepChange{StepDraw} event
// first, rather than just taking the first Draw/Priority anywhere after
// since: the pass(es) still needed to leave the PREVIOUS step can themselves
// emit Priority events (one per pass, to the next seat) that would otherwise
// be mistaken for the draw step's own.
func assertDrawPrecedesPriorityInStep(t *testing.T, e *Engine, since int) {
	t.Helper()
	log := e.L.Events
	stepChangeIdx := -1
	for i := since; i < len(log); i++ {
		if log[i].Kind == events.StepChange && log[i].Step == state.StepDraw {
			stepChangeIdx = i
			break
		}
	}
	if stepChangeIdx < 0 {
		t.Fatalf("no StepChange{Draw} event found at or after log index %d", since)
	}
	drawIdx, priorityIdx := -1, -1
	for i := stepChangeIdx + 1; i < len(log); i++ {
		if log[i].Kind == events.Draw && drawIdx < 0 {
			drawIdx = i
		}
		if log[i].Kind == events.Priority && priorityIdx < 0 {
			priorityIdx = i
		}
		if drawIdx >= 0 && priorityIdx >= 0 {
			break
		}
	}
	if drawIdx < 0 || priorityIdx < 0 {
		t.Fatalf("entering the draw step logged Draw at index %d and Priority at index %d, want both present", drawIdx, priorityIdx)
	}
	if drawIdx > priorityIdx {
		t.Fatalf("the draw step's Draw event (index %d) did not precede its Priority event (index %d) -- "+
			"CR 504.1 requires the turn-based draw before anyone gets priority", drawIdx, priorityIdx)
	}
}

// TestDrawTriggerResolvingInTheDrawStepDoesNotRedraw is the brief's own
// measurement case. Against the pre-fix code, resolveTop's post-resolution
// Priority{Active, 0} emit (CR 117.3b) recreates exactly the state
// priorityRound's old guard read as "the step just began", so the mandatory
// trigger this test attaches makes the engine draw again, and again, until
// the library runs dry -- a large hand delta instead of exactly 1. See the
// RED measurement pasted into the Task 28 report for the actual pre-fix
// number.
func TestDrawTriggerResolvingInTheDrawStepDoesNotRedraw(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	onBoard(t, e, 1, drawTriggerSrc)
	// Baseline is captured one step EARLY, at upkeep, not at the draw step
	// itself: the very Submit that transitions into StepDraw already
	// performs the (single, correct) draw as part of that same call, before
	// this test ever gets to observe hand/library sizes in between.
	driveToStep(t, e, 2, 1, state.StepUpkeep)

	hand := len(e.G.Zone(state.ZHand, 1))
	lib := len(e.G.Zone(state.ZLibrary, 1))
	life := e.G.Players[1].Life

	beforeDraw := len(e.L.Events)
	driveToStep(t, e, 2, 1, state.StepDraw)
	assertDrawPrecedesPriorityInStep(t, e, beforeDraw)
	passThroughStep(t, e, state.StepDraw, 100)

	handDelta := len(e.G.Zone(state.ZHand, 1)) - hand
	libDelta := lib - len(e.G.Zone(state.ZLibrary, 1))
	lifeDelta := e.G.Players[1].Life - life
	t.Logf("hand delta=%d library delta=%d life delta=%d step=%s over=%v",
		handDelta, libDelta, lifeDelta, e.G.Step, e.G.Over)

	if e.G.Step == state.StepDraw {
		t.Fatalf("the draw step never advanced (pending = %+v)", e.Pending())
	}
	if handDelta != 1 {
		t.Fatalf("hand grew by %d card(s) in the draw step, want exactly 1 -- the draw step ran %d times", handDelta, handDelta)
	}
	if libDelta != 1 {
		t.Fatalf("library shrank by %d card(s), want exactly 1", libDelta)
	}
	if lifeDelta != 1 {
		t.Fatalf("life changed by %d, want exactly 1 -- the mandatory trigger must resolve exactly once", lifeDelta)
	}
	// Seat 0 gets turn 3: the game did not hang or skip a seat.
	driveToStep(t, e, 3, 0, state.StepUpkeep)
}

// TestDrawTriggerResolvingInTheDrawStepDoesNotRedrawAtFourSeats is the same
// scenario with four seats: turn 2 still belongs to seat 1 (NextAlive cycles
// 0,1,2,3 from beginTurn), so the trigger's owner is again the active player,
// but now three OTHER seats' own priority passes are interleaved with the
// resolution round-trip.
func TestDrawTriggerResolvingInTheDrawStepDoesNotRedrawAtFourSeats(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 4)
	onBoard(t, e, 1, drawTriggerSrc)
	driveToStep(t, e, 2, 1, state.StepUpkeep)

	hand := len(e.G.Zone(state.ZHand, 1))
	lib := len(e.G.Zone(state.ZLibrary, 1))
	life := e.G.Players[1].Life

	driveToStep(t, e, 2, 1, state.StepDraw)
	passThroughStep(t, e, state.StepDraw, 200)

	if e.G.Step == state.StepDraw {
		t.Fatalf("the draw step never advanced (pending = %+v)", e.Pending())
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - hand; got != 1 {
		t.Fatalf("hand grew by %d card(s) in the draw step, want exactly 1 -- the draw step ran %d times", got, got)
	}
	if got := lib - len(e.G.Zone(state.ZLibrary, 1)); got != 1 {
		t.Fatalf("library shrank by %d card(s), want exactly 1", got)
	}
	if got := e.G.Players[1].Life - life; got != 1 {
		t.Fatalf("life changed by %d, want exactly 1 -- the mandatory trigger must resolve exactly once", got)
	}
}

// TestDrawHappensOnceEvenWhenTwoDrawTriggersAreOrdered attaches TWO copies
// of the same mandatory trigger to the active player, so the single draw
// queues two pendingTriggers and putTriggersOnStack asks a KTriggerOrder
// decision before either resolves. Answering it must not itself cause a
// second draw, and both copies must still resolve exactly once each.
func TestDrawHappensOnceEvenWhenTwoDrawTriggersAreOrdered(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	onBoard(t, e, 1, drawTriggerSrc)
	onBoard(t, e, 1, drawTriggerSrc)
	driveToStep(t, e, 2, 1, state.StepUpkeep)

	hand := len(e.G.Zone(state.ZHand, 1))
	lib := len(e.G.Zone(state.ZLibrary, 1))
	life := e.G.Players[1].Life

	driveToStep(t, e, 2, 1, state.StepDraw)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want the two draw triggers to ask for an order", d)
	}
	submit(t, e, 1, 0)

	passThroughStep(t, e, state.StepDraw, 100)

	if e.G.Step == state.StepDraw {
		t.Fatalf("the draw step never advanced (pending = %+v)", e.Pending())
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - hand; got != 1 {
		t.Fatalf("hand grew by %d card(s), want exactly 1 -- one draw, not one per trigger", got)
	}
	if got := lib - len(e.G.Zone(state.ZLibrary, 1)); got != 1 {
		t.Fatalf("library shrank by %d card(s), want exactly 1", got)
	}
	if got := e.G.Players[1].Life - life; got != 2 {
		t.Fatalf("life changed by %d, want exactly 2 -- both triggers resolved exactly once each", got)
	}
}

// TestStartingPlayerStillSkipsTheirFirstDraw pins CR 103.8a across the move:
// turn 1 (the starting player's own first turn) still draws nothing, and
// turn 2 (the next seat's first turn, but not the game's first turn) draws
// exactly once. The starting seat is the engine's CR 103.1 toss, read off
// e.G.Active after New -- never re-derived from the seed, which would make
// the test compete with the implementation for the same rng stream.
func TestStartingPlayerStillSkipsTheirFirstDraw(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(Config{Seed: 1, Names: names, Decks: decks}) // measured: the toss gives seat 1
	e.Advance()
	start := e.G.Active
	if start == 0 {
		t.Fatalf("fixture seed must toss the game to someone other than seat 0, got active=%d", start)
	}
	other := 1 - start
	handStart := len(e.G.Zone(state.ZHand, start))
	driveToStep(t, e, 1, start, state.StepDraw)
	if got := len(e.G.Zone(state.ZHand, start)); got != handStart {
		t.Fatalf("starting player (seat %d) hand at turn 1's draw step = %d, want %d unchanged -- CR 103.8a", start, got, handStart)
	}

	handOther := len(e.G.Zone(state.ZHand, other))
	driveToStep(t, e, 2, other, state.StepDraw)
	if got := len(e.G.Zone(state.ZHand, other)); got != handOther+1 {
		t.Fatalf("seat %d hand at turn 2's draw step = %d, want %d -- seat %d is not the starting player", other, got, handOther+1, other)
	}
}

// TestDrawStepDeckOutEndsTheGameBeforePriority is CR 104.4a via CR 704.5c: an
// empty-library draw is itself a loss, and in a two-seat game that ends the
// match outright. drawCard's own tail checkStateBased must catch this before
// advanceStep's trailing Priority emit runs, so a finished game never hands
// out one more decision.
func TestDrawStepDeckOutEndsTheGameBeforePriority(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	driveToStep(t, e, 2, 1, state.StepUpkeep)
	e.G.SetZone(state.ZLibrary, 1, nil)

	// Both seats still need to pass to leave upkeep (turn 2's own pass count
	// starts at 0), so this drains upkeep rather than assuming one pass
	// suffices; it stops the instant advanceStep's mandatory draw ends the
	// game, which is the assertion below. Ordinary priority asks happen along
	// the way (each pass grants priority to the next seat, which is its own
	// DecisionAsk) -- those are expected and fine; only a DecisionAsk AFTER
	// the GameOver event itself would mean a finished game handed out one
	// more decision.
	passThroughStep(t, e, state.StepUpkeep, 10)

	if !e.G.Players[1].Lost {
		t.Fatal("seat 1 should have lost: their library was empty at the mandatory draw")
	}
	if !e.G.Over {
		t.Fatal("a two-seat game must end once the only opponent is eliminated")
	}
	overIdx := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.GameOver {
			overIdx = i
			break
		}
	}
	if overIdx < 0 {
		t.Fatal("e.G.Over is true but no GameOver event was logged")
	}
	// Fix round 1 (review Important #1a): Advance's own `for !e.G.Over` loop
	// already suppresses any DecisionAsk once the game ends, so the scan
	// above passes whether or not advanceStep's own `if e.G.Over { return }`
	// (turn.go, right after the draw) is even there -- deleting that guard
	// left the whole suite green. What it actually suppresses is the
	// trailing `Priority` emit advanceStep would otherwise make right after
	// the draw: without the guard, a deck-out logs one extra Priority event
	// AFTER GameOver (measured: 102 events instead of 101). This is the
	// name's own promise ("...BeforePriority") made into an assertion.
	for _, ev := range e.L.Events[overIdx+1:] {
		if ev.Kind == events.DecisionAsk {
			t.Fatalf("a DecisionAsk (%+v) was emitted after GameOver -- a finished game must not hand out a decision", ev)
		}
		if ev.Kind == events.Priority {
			t.Fatalf("a Priority event (%+v) was emitted after GameOver -- advanceStep must return before emitting it", ev)
		}
	}
}

// TestReplayFoldsADrawTriggerFaithfully is Ruling U4 (see
// TestReplayFromLogAloneReconstructsOrderedAndOptionalTriggers) applied to
// this task: a game whose log contains a draw-step trigger firing folds,
// from the log alone via events.Apply, into exactly the live game. The draw
// moving from priorityRound to advanceStep changes the ORDER two event kinds
// appear in relative to each other (Draw now precedes the step's own
// Priority event) but adds no new Kind and no new Event field, so a replay
// built from nothing but L.Events must still reconstruct it exactly.
func TestReplayFoldsADrawTriggerFaithfully(t *testing.T) {
	t.Parallel()
	deck1 := append([]*cards.Card{card(t, drawTriggerSrc)}, mountainDeck(t, 39)...)
	cfg := Config{Seed: 5, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), deck1}}
	e := New(cfg)
	findAndPlay(t, e, 1, "Chronicler")
	e.Advance()

	driveToStep(t, e, 2, 1, state.StepDraw)
	passThroughStep(t, e, state.StepDraw, 100)
	if e.G.Step == state.StepDraw {
		t.Fatalf("setup failed: the draw step never advanced (pending = %+v)", e.Pending())
	}
	driveToStep(t, e, 3, 0, state.StepUpkeep)

	fresh := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, fresh); diff != "" {
		t.Fatalf("replay from the log alone diverged from the live game:\n%s", diff)
	}
}
