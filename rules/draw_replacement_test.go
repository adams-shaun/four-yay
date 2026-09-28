package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// R:Event$ Draw replacements — the lane the engine never had. The corpus's
// 39 Draw-replacement files (Breathstealer's Crypt, Zur's Weirding, the Words
// family) were permanently inert; their bodies' ReplacedPlayer /
// NonReplacedPlayer payer bindings had nothing to resolve against.

// setupDrawLibrary puts top on seat p's library's top slot, under the
// existing library order.
func setupDrawLibrary(t *testing.T, e *Engine, p state.PlayerID, top *cards.Card) state.ObjID {
	t.Helper()
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...)
	topObj := e.G.AddObject(top, p)
	topObj.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, p, append([]state.ObjID{topObj.ID}, lib...))
	return topObj.ID
}

func emitDraw(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		t.Fatal("empty library for the test draw")
	}
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// TestBreathstealersCryptReplacedPlayerPay drives the crypt's real
// replacement ("If a player would draw a card, instead they draw a card and
// reveal it. If it's a creature card, that player discards it unless they pay
// 3 life."): the replaced draw's body re-draws, the creature is revealed, and
// the UnlessPayer$ ReplacedPlayer ask reaches the draw-ER. Paying costs 3
// life and keeps the card.
func TestBreathstealersCryptReplacedPlayerPay(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Breathstealer's Crypt"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)

	draws := countDraw(e)
	emitDraw(t, e, 1)
	// The replacement discarded the original Draw event; the body drew one.
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("draw events after replacement = %d, want 1 (the body's re-draw)", got)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the ReplacedPlayer unless-pay ask for seat 1", d)
	}
	life := e.G.Players[1].Life
	answerUnlessPay(t, e, true)
	if got := e.G.Players[1].Life; got != life-3 {
		t.Fatalf("draw-er life = %d, want %d (paid 3)", got, life-3)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZHand {
		t.Fatalf("drawn creature zone = %v, want kept in hand (paid)", o)
	}
}

// TestBreathstealersCryptReplacedPlayerDecline is the mirror: declining
// discards the revealed creature, and no life moves.
func TestBreathstealersCryptReplacedPlayerDecline(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Breathstealer's Crypt"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)

	emitDraw(t, e, 1)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the ReplacedPlayer unless-pay ask for seat 1", d)
	}
	life := e.G.Players[1].Life
	answerUnlessPay(t, e, false)
	if got := e.G.Players[1].Life; got != life {
		t.Fatalf("draw-er life = %d, want %d (declined)", got, life)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("drawn creature zone = %v, want discarded", o)
	}
}

// TestBreathstealersCryptNonCreatureDoesNotAsk pins the condition gate: a
// non-creature draw resolves the replacement without any unless ask.
func TestBreathstealersCryptNonCreatureDoesNotAsk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Breathstealer's Crypt"))
	setupDrawLibrary(t, e, 1, mustCorpusCard(t, reg, "Mind Stone"))
	draws := countDraw(e)
	emitDraw(t, e, 1)
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("draw events after replacement = %d, want 1", got)
	}
	if d := e.Pending(); d != nil && d.ResumeKind == "unless_pay" {
		t.Fatalf("non-creature draw posed an unless ask: %+v", d)
	}
}

// TestZursWeirdingNonReplacedPlayerPays drives Zur's Weirding ("If a player
// would draw a card, they reveal it instead. Then any other player may pay 2
// life. If a player does, put that card into its owner's graveyard.
// Otherwise, that player draws a card."): the UnlessPayer$ NonReplacedPlayer
// ask reaches the OTHER player; paying mills the revealed card (UnlessSwitched$
// True: paying CAUSES the mill) and the WhenNotPaid DBDraw sub is skipped;
// declining skips the mill and the sub draws the card.
func TestZursWeirdingNonReplacedPlayerPays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Zur's Weirding"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)
	addMana(t, e, 0, "CC")

	draws := countDraw(e)
	emitDraw(t, e, 1)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 0 {
		t.Fatalf("pending = %+v, want the NonReplacedPlayer unless-pay ask for seat 0", d)
	}
	life := e.G.Players[0].Life
	answerUnlessPay(t, e, true)
	if got := e.G.Players[0].Life; got != life-2 {
		t.Fatalf("payer life = %d, want %d", got, life-2)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("revealed card zone = %v, want milled to the graveyard (paid)", o)
	}
	if got := countDraw(e) - draws; got != 0 {
		t.Fatalf("draw delta = %d, want 0 (the WhenNotPaid draw sub was skipped)", got)
	}
}

// TestZursWeirdingNonReplacedPlayerDeclines is the mirror: declining skips
// the mill and the WhenNotPaid sub draws the card for the draw-er.
func TestZursWeirdingNonReplacedPlayerDeclines(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Zur's Weirding"))
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	top := setupDrawLibrary(t, e, 1, bear)

	draws := countDraw(e)
	emitDraw(t, e, 1)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_pay" || d.Player != 0 {
		t.Fatalf("pending = %+v, want the NonReplacedPlayer unless-pay ask for seat 0", d)
	}
	answerUnlessPay(t, e, false)
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZHand {
		t.Fatalf("revealed card zone = %v, want drawn (declined)", o)
	}
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("draw delta = %d, want 1 (the WhenNotPaid sub drew)", got)
	}
}

// TestNotionThiefExemptsTheDrawStepDraw is the leaf for
// NotFirstCardInDrawStep$ True: CR 504.1's turn-based draw is exactly the
// first card the active player draws in their own draw step, so Notion Thief
// ("except the first one they draw in each of their draw steps") must NOT
// replace it. Before the gate this was the whole defect: the pending Draw
// was matched with no notion of the draw step, the opponent's turn-based draw
// was consumed, and seat 0 drew the card instead.
func TestNotionThiefExemptsTheDrawStepDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Notion Thief"))

	// Seat 1 takes turn 2; drive into its draw step. The turn-based draw is a
	// turn-based action on step entry, so it has already been proposed (and,
	// before the fix, stolen) by the time we arrive.
	driveToStep(t, e, 2, 1, state.StepUpkeep)
	hand1 := len(e.G.Zone(state.ZHand, 1))
	lib1 := len(e.G.Zone(state.ZLibrary, 1))
	hand0 := len(e.G.Zone(state.ZHand, 0))
	driveToStep(t, e, 2, 1, state.StepDraw)

	if got := len(e.G.Zone(state.ZHand, 1)) - hand1; got != 1 {
		t.Fatalf("seat1 hand delta over its draw step = %d, want 1 (the exempt turn-based draw)", got)
	}
	if got := len(e.G.Zone(state.ZLibrary, 1)) - lib1; got != -1 {
		t.Fatalf("seat1 library delta over its draw step = %d, want -1", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand0; got != 0 {
		t.Fatalf("seat0 hand delta = %d, want 0 (Notion Thief must not steal the first draw)", got)
	}
}

// TestNotionThiefRedirectsExtraDrawInDrawStep is the other half: a SECOND
// draw in the same draw step is an extra draw, so it is still fully replaced
// ("instead that player skips that draw and you draw a card").
func TestNotionThiefRedirectsExtraDrawInDrawStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Notion Thief"))

	driveToStep(t, e, 2, 1, state.StepDraw)
	if e.G.Step != state.StepDraw {
		t.Fatalf("step = %s, want the draw step", e.G.Step)
	}
	hand1 := len(e.G.Zone(state.ZHand, 1))
	hand0 := len(e.G.Zone(state.ZHand, 0))
	// The extra draw: a Draw for seat 1 emitted while still in seat 1's draw
	// step. This is the second draw of the step, so Notion Thief replaces it.
	emitDraw(t, e, 1)

	if got := len(e.G.Zone(state.ZHand, 1)) - hand1; got != 0 {
		t.Fatalf("seat1 hand delta for the replaced extra draw = %d, want 0", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand0; got != 1 {
		t.Fatalf("seat0 hand delta for the replaced extra draw = %d, want 1 (Notion Thief's controller draws)", got)
	}
}

// TestNotFirstCardInDrawStepDoesNotOverRestrict is the over-reach guard: the
// gate is keyed on the draw step, so a Draw outside any draw step is an extra
// draw and must still be replaced. stealEngine sits at Main 1, so the
// existing emitDraw helper runs there.
func TestNotFirstCardInDrawStepDoesNotOverRestrict(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Notion Thief"))
	if e.G.Step == state.StepDraw {
		t.Fatal("precondition: expected to be outside the draw step at Main 1")
	}

	hand1 := len(e.G.Zone(state.ZHand, 1))
	hand0 := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 1)

	if got := len(e.G.Zone(state.ZHand, 1)) - hand1; got != 0 {
		t.Fatalf("seat1 hand delta = %d, want 0 (outside the draw step the draw is replaced)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand0; got != 1 {
		t.Fatalf("seat0 hand delta = %d, want 1", got)
	}
}

// TestNotFirstCardInDrawStepOnlyExemptsTheActivePlayersDraw pins the
// active-player half of the gate, which firstCardInDrawStep (the trigger
// helper) does NOT require. Teferi's Ageless Insight replaces "you would draw
// a card except the first one you draw in each of YOUR draw steps"
// (ValidPlayer$ You), so during seat 1's own draw step a Draw for the
// NON-active seat 0 is not in seat 0's own draw step and must still be
// replaced -- with "draw two cards instead". Were the gate to follow the
// trigger helper and drop p == e.G.Active, seat 0's draw would be wrongly
// exempted and it would draw only one.
func TestNotFirstCardInDrawStepOnlyExemptsTheActivePlayersDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Teferi's Ageless Insight"))

	// Seat 1's own turn-2 draw step (seat 0 started): seat 1 active.
	driveToStep(t, e, 2, 1, state.StepDraw)
	if e.G.Step != state.StepDraw || e.G.Active != 1 {
		t.Fatalf("precondition: step=%s active=%d, want draw step with seat 1 active", e.G.Step, e.G.Active)
	}
	hand0 := len(e.G.Zone(state.ZHand, 0))
	// A Draw for the NON-active seat 0 during seat 1's draw step is an extra
	// draw for seat 0, so it is replaced by "draw two instead".
	emitDraw(t, e, 0)
	if got := len(e.G.Zone(state.ZHand, 0)) - hand0; got != 2 {
		t.Fatalf("seat0 hand delta = %d, want 2 (the non-active draw must be replaced by draw-two)", got)
	}
}

// TestIslandSanctuaryActivePhasesDrawOnlyAppliesInDrawStep is the leaf for
// ActivePhases$ on the Draw arm. Island Sanctuary ("If you would draw a card
// during your draw step, instead you may skip that draw") is the corpus's
// only ActivePhases$ carrier, and before the gate the parameter was unread:
// the replacement applied in ANY step of its controller's turn (the
// PlayerTurn$ True gate narrows it to the controller's turn and nothing
// else), silently consuming a main-phase draw and registering the CantAttack
// effect with no log line. stealEngine sits at Main 1, so the reported shape
// is the pre-fix defect.
func TestIslandSanctuaryActivePhasesDrawOnlyAppliesInDrawStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sanctuary := mustCorpusCard(t, reg, "Island Sanctuary")
	// Fixture guard: the real compiled replacement must carry the
	// ActivePhases$ Draw spec this leaf turns on.
	carries := false
	for _, r := range sanctuary.Faces[0].Repls {
		if r.Event == "Draw" && r.Params["ActivePhases"] == "Draw" {
			carries = true
		}
	}
	if !carries {
		t.Fatal("Island Sanctuary seq 0: fixture changed (no ActivePhases$ Draw replacement)")
	}

	e := stealEngine(t, 743)
	sid := onBoardCard(t, e, 0, sanctuary)
	if e.G.Step == state.StepDraw {
		t.Fatalf("precondition: step = %s, want a non-draw step", e.G.Step)
	}
	setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))

	draws := countDraw(e)
	hand := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0)
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("draw delta outside the draw step = %d, want 1 (ActivePhases$ Draw must not apply)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Fatalf("hand delta outside the draw step = %d, want 1", got)
	}
	for _, ce := range e.active() {
		if ce.Source == sid {
			t.Fatalf("continuous effect registered from Island Sanctuary outside the draw step: %+v", ce)
		}
	}
}

// TestIslandSanctuaryActivePhasesAppliesInDrawStep is the positive control
// for the gate: the same setup in the controller's draw step DOES admit the
// replacement, which consumes the draw (Optional$ is separately unread, so
// no ask is asserted -- only that the replacement was applicable).
func TestIslandSanctuaryActivePhasesAppliesInDrawStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Island Sanctuary"))
	setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	e.G.Step = state.StepDraw

	draws := countDraw(e)
	hand := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0)
	if got := countDraw(e) - draws; got != 0 {
		t.Fatalf("draw delta in the draw step = %d, want 0 (the replacement applies)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 0 {
		t.Fatalf("hand delta in the draw step = %d, want 0", got)
	}
}

// TestReedRichardsOnlyFirstExtraDrawIsReplaced pins
// FirstExtraCardDrawnThisTurn$ True (Reed Richards, Smartest Man:
// "The first time you would draw a card each turn except the first card you
// draw during each of your draw steps, you draw four cards instead.").
//
// CR 614.1a: a replacement effect replaces a single event, and the card's
// "the FIRST time each turn" clause means only the first non-exempt draw of
// the turn is replaced. Before the gate the matcher read neither the
// parameter nor any per-turn latch, so every extra draw of the turn was
// replaced and three extra draws gave 4+4+4 instead of 4+1+1.
//
// stealEngine sits at Main 1 of turn 1 (seat 0 active), so each emitDraw is
// an extra draw for seat 0 -- none of them is the exempt CR 504.1 turn-based
// draw.
func TestReedRichardsOnlyFirstExtraDrawIsReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Reed Richards, Smartest Man"))
	if e.G.Step == state.StepDraw {
		t.Fatal("precondition: expected to be outside the draw step at Main 1")
	}

	hand := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // first extra draw of the turn: replaced, draws four
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 4 {
		t.Fatalf("hand delta for the first extra draw = %d, want 4 (Reed Richards replaces it)", got)
	}
	hand = len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // second extra draw: no longer the first, unreplaced
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Fatalf("hand delta for the second extra draw = %d, want 1 (only the first is replaced)", got)
	}
	hand = len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // third extra draw: still unreplaced
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Fatalf("hand delta for the third extra draw = %d, want 1", got)
	}
}

// TestReedRichardsDrawStepDrawIsExemptAndFirstExtraIsReplaced pins the
// "except the first card you draw during each of your draw steps" half: the
// CR 504.1 turn-based draw of the controller's own draw step is not the
// "first time" the replacement waits for, so it is not replaced -- but the
// FIRST extra draw in that same step is (it is the first non-exempt draw of
// the turn), and the next one is not.
func TestReedRichardsDrawStepDrawIsExemptAndFirstExtraIsReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Reed Richards, Smartest Man"))

	// Seat 0 is the active player; drive into its own turn-3 draw step. The
	// turn-based draw is a turn-based action on step entry, so it has already
	// happened by the time we arrive -- and must not have been replaced.
	driveToStep(t, e, 3, 0, state.StepDraw)
	if e.G.Step != state.StepDraw || e.G.Active != 0 {
		t.Fatalf("precondition: step=%s active=%d, want seat 0's draw step", e.G.Step, e.G.Active)
	}

	hand := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // first extra draw in the draw step: replaced
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 4 {
		t.Fatalf("hand delta for the first extra draw in the draw step = %d, want 4", got)
	}
	hand = len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // second extra draw in the draw step: unreplaced
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Fatalf("hand delta for the second extra draw in the draw step = %d, want 1", got)
	}
}

// TestReedRichardsDrawStepTurnBasedDrawNotReplaced is the direct exemption
// guard: entering seat 0's own draw step makes its CR 504.1 turn-based draw,
// which Reed Richards must leave alone (that is the card's explicit
// "except ...", not the FirstExtraCardDrawnThisTurn latch -- the exempt draw
// is the one pendingDrawIsFirstInDrawStep recognises).
func TestReedRichardsDrawStepTurnBasedDrawNotReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Reed Richards, Smartest Man"))

	driveToStep(t, e, 3, 0, state.StepUpkeep)
	hand := len(e.G.Zone(state.ZHand, 0))
	lib := len(e.G.Zone(state.ZLibrary, 0))
	driveToStep(t, e, 3, 0, state.StepDraw)

	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Fatalf("hand delta over the draw step = %d, want 1 (the exempt turn-based draw)", got)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)) - lib; got != -1 {
		t.Fatalf("library delta over the draw step = %d, want -1", got)
	}
}

// TestReedRichardsExtraDrawOutsideStepIsFirstNotExempt guards against the
// gate over-exempting: a non-active player's extra draw during another
// player's draw step is NOT that player's own turn-based draw, so it is the
// first extra draw of the turn for them and must be replaced -- mirroring
// the active-player semantics of pendingDrawIsFirstInDrawStep.
func TestReedRichardsExtraDrawOutsideStepIsFirstNotExempt(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Reed Richards, Smartest Man"))

	// Seat 1's own turn-2 draw step: seat 1 is active, and a Draw for the
	// NON-active seat 0 is not in seat 0's own draw step, so it is an extra
	// draw for seat 0 and Reed Richards replaces it.
	driveToStep(t, e, 2, 1, state.StepDraw)
	if e.G.Step != state.StepDraw || e.G.Active != 1 {
		t.Fatalf("precondition: step=%s active=%d, want seat 1's draw step", e.G.Step, e.G.Active)
	}
	hand := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // seat 0's first extra draw this turn: replaced
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 4 {
		t.Fatalf("hand delta for the non-active extra draw = %d, want 4", got)
	}
	hand = len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0) // now not the first: unreplaced
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 1 {
		t.Fatalf("hand delta for the second non-active extra draw = %d, want 1", got)
	}
}
