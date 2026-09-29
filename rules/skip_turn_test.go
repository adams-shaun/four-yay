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

// flipSkipSrc is a freely-authored synthetic whose activation flips three
// coins with NoCall$ True and grants SkipTurn NumTurns$ X on heads — the
// shape Ral Zarek, Guest Lecturer's [-7] rides, isolated from the
// planeswalker/targeting machinery. Under a per-flip branch call the grant
// fires once PER HEAD with X = heads-so-far (1+2+…), which is exactly the
// over-grant this test exists to fail on.
const flipSkipSrc = "Name:Test Flip Skip\nManaCost:2 R\nTypes:Sorcery\n" +
	"A:AB$ FlipCoin | Cost$ 0 | Amount$ 3 | NoCall$ True | HeadsSubAbility$ DBSkip | SpellDescription$ Flip three coins. Skip X turns, where X is the number of heads.\n" +
	"SVar:DBSkip:DB$ SkipTurn | NumTurns$ X\n" +
	"Oracle:Flip three coins. Skip X turns, where X is the number of heads.\n"

// countFlipHeads counts the canonical heads-flip Notes a resolution emitted
// (FlipCoinNote is the encoding the FlippedCoin trigger matcher reads).
func countFlipHeads(e *Engine, flipper state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Player == flipper &&
			ev.Text == effects.FlipNotePrefix+"heads" {
			n++
		}
	}
	return n
}

func countSkipTurnEvents(e *Engine, pred func(events.Event) bool) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.SkipTurn && pred(ev) {
			n++
		}
	}
	return n
}

// skipTurnAbilityOption locates the legal action activating the card's
// index-th ability for seat p; the test fails loudly when the engine does
// not offer it.
func skipTurnAbilityOption(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID, index int) decision.Option {
	t.Helper()
	for _, o := range e.legalActions(p) {
		if o.Kind == "ability" && o.Obj == id && o.Ability == index {
			return o
		}
	}
	t.Fatalf("ability %d of obj %d was not offered to seat %d: %+v", index, id, p, e.legalActions(p))
	return decision.Option{}
}

// TestSkipTurnNoCallBranchFiresOnceWithTotalHeads pins Forge's NoCall$
// True branch semantics (the mechanism Ral Zarek's [-7] depends on): the
// outcome branch is deferred to the END of the flip loop and fires ONCE
// with X = the total number of heads — not once per winning flip with X =
// heads so far, which would grant 1+2+…+heads turns instead of heads.
func TestSkipTurnNoCallBranchFiresOnceWithTotalHeads(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, diags := cards.ParseBytes("flip_skip.txt", []byte(flipSkipSrc))
	for _, d := range diags {
		t.Fatalf("synthetic card failed to parse: %v", d)
	}
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{c}, nil)
	id := moveByName(t, e, 0, "Test Flip Skip", state.ZBattlefield)
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("precondition: Test Flip Skip must be on the battlefield")
	}
	opt := skipTurnAbilityOption(t, e, 0, id, 0)
	e.beginActivation(0, opt)
	passUntilStackEmpty(t, e, 60)

	heads := countFlipHeads(e, 0)
	if heads < 1 || heads > 3 {
		t.Fatalf("precondition: seeded flips produced %d heads, want 1..3 so the deferred branch actually fires", heads)
	}
	grants := countSkipTurnEvents(e, func(ev events.Event) bool { return ev.Amount > 0 })
	if grants != 1 {
		t.Fatalf("NoCall$ branch must grant once for the whole resolution, got %d SkipTurn grants (per-flip call would over-grant)", grants)
	}
	if got := e.G.SkipTurns[0]; got != heads {
		t.Fatalf("seat 0's skipped-turn count = %d, want X = total heads = %d", got, heads)
	}
	replayCheck(t, e, cfg)
}

// TestRalZarekGuestLecturerUltimateSkipsOpponent is the end-to-end card
// test: Ral Zarek, Guest Lecturer's [-7] flips five coins, and the targeted
// opponent skips their next X turns, X the number of heads. The skip must
// be real — one grant of X (not a per-flip over-grant), consumed at the
// turn boundary with the skipped player taking no actions.
func TestRalZarekGuestLecturerUltimateSkipsOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ral, ok := reg.Lookup("Ral Zarek, Guest Lecturer")
	if !ok {
		t.Fatal("corpus fixture: Ral Zarek, Guest Lecturer missing")
	}
	// The census the sos set audit reads: api:SkipTurn must no longer be
	// among the primitives the card still needs.
	supported := effects.Supported()
	if m := reg.Unsupported(ral, supported); len(m) > 0 {
		for _, need := range m {
			if need == "api:SkipTurn" {
				t.Fatalf("Ral Zarek, Guest Lecturer still names api:SkipTurn: %v", m)
			}
		}
	}
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{ral}, nil)
	ralID := moveByName(t, e, 0, "Ral Zarek, Guest Lecturer", state.ZBattlefield)
	if e.G.Obj(ralID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Ral Zarek must be on the battlefield")
	}
	// Enter at 3, add 5: 8 loyalty makes [-7] payable and leaves 1, so the
	// planeswalker survives (never at 0: no SBA to reason about).
	e.emit(events.Event{Kind: events.CounterChange, Obj: ralID, Counter: "LOYALTY", Amount: 5})
	if got := e.G.Obj(ralID).Counter("LOYALTY"); got != 8 {
		t.Fatalf("precondition: loyalty = %d, want 8", got)
	}
	opt := skipTurnAbilityOption(t, e, 0, ralID, 3)
	e.beginActivation(0, opt)
	answerPlayerTargetAsk(t, e, 1)
	passUntilStackEmpty(t, e, 60)

	heads := countFlipHeads(e, 0)
	if heads < 1 || heads > 5 {
		t.Fatalf("precondition: seeded ultimate produced %d heads, want 1..5 so the skip actually happens", heads)
	}
	grants := countSkipTurnEvents(e, func(ev events.Event) bool {
		return ev.Amount > 0 && ev.Player == 1
	})
	if grants != 1 {
		t.Fatalf("the opponent must receive ONE SkipTurn grant, got %d (per-flip call would over-grant)", grants)
	}
	if got := e.G.SkipTurns[1]; got != heads {
		t.Fatalf("seat 1's pending skips = %d, want X = total heads = %d", got, heads)
	}
	if got := e.G.Obj(ralID).Counter("LOYALTY"); got != 1 {
		t.Fatalf("Ral Zarek loyalty after [-7] = %d, want 1", got)
	}

	// Seat 0 ends turn 1: seat 1's next turn is consumed at the boundary —
	// no TurnChange, no untap, no draw — and seat 0 takes the next turn.
	skipStart := len(e.L.Events)
	driveToStep(t, e, 2, 0, state.StepMain1)
	if countSkipTurnEvents(e, func(ev events.Event) bool {
		return ev.Player == 1 && ev.Amount == -1
	}) != 1 {
		t.Fatal("seat 1's pending skip was not consumed at the turn boundary")
	}
	for _, ev := range e.L.Events[skipStart:] {
		if ev.Kind == events.Draw && ev.Player == 1 {
			t.Fatal("seat 1 took an action during the skipped turn (drew)")
		}
	}
	var holders []state.PlayerID
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange {
			holders = append(holders, ev.Player)
		}
	}
	if len(holders) != 2 || holders[0] != 0 || holders[1] != 0 || e.G.Turn != 2 {
		t.Fatalf("turn holders=%v turn=%d, want [0 0] at turn 2 (seat 1's turn skipped)", holders, e.G.Turn)
	}
	if got := e.G.SkipTurns[1]; got != heads-1 {
		t.Fatalf("seat 1's remaining skips = %d, want %d after one consumed", got, heads-1)
	}
	replayCheck(t, e, cfg)
}

func TestSkipTurnConsumesNextTurnWithoutTakingActions(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{}, nil)
	if e.G.Active != 0 || e.G.Turn != 1 {
		t.Fatalf("precondition: game starts on seat 0 turn 1; active=%d turn=%d", e.G.Active, e.G.Turn)
	}
	e.emit(events.Event{Kind: events.SkipTurn, Player: 1, Amount: 1})
	if e.G.SkipTurns[1] != 1 {
		t.Fatalf("precondition: seat 1's skipped turn was not recorded: %v", e.G.SkipTurns)
	}

	// Seat 0 ends turn 1: seat 1's turn is consumed without a TurnChange,
	// untap or draw, then the next ordinary turn begins for seat 0.
	skipStart := len(e.L.Events)
	driveToStep(t, e, 2, 0, state.StepMain1)
	var holders []state.PlayerID
	var turnOneSkipped, seatOneDrew bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange {
			holders = append(holders, ev.Player)
		}
	}
	for _, ev := range e.L.Events[skipStart:] {
		if ev.Kind == events.SkipTurn && ev.Player == 1 && ev.Amount == -1 {
			turnOneSkipped = true
		}
		if ev.Kind == events.Draw && ev.Player == 1 {
			seatOneDrew = true
		}
	}
	if !turnOneSkipped {
		t.Fatal("seat 1's pending skip was not consumed at the turn boundary")
	}
	if seatOneDrew {
		t.Fatal("seat 1 took an action during the skipped turn")
	}
	if len(holders) != 2 || holders[0] != 0 || holders[1] != 0 || e.G.Turn != 2 {
		t.Fatalf("turn holders=%v turn=%d, want [0 0] at turn 2", holders, e.G.Turn)
	}
	replayCheck(t, e, cfg)
}
