// Count$YouDrewThisTurn — the per-turn draw count head (ticket
// count-youdrewthisturn). Before the fix the head did not exist in
// evalCountBody and degraded to 0 through the unresolvable-count convention,
// so Elenda and Azor's `SVar:Y:Count$YouDrewThisTurn` fed `TokenAmount$ Y`
// with Y = 0 and the end-step body created nothing no matter how many cards
// had been drawn.
//
// The head reads Host.CardsDrawnThisTurn — the SAME log fold the
// PlayerCount$CardsDrawn property (Smuggler's Share family) already uses, so
// the head and the property can never drift apart. The eval-level pin runs on
// the real corpus card (Elenda and Azor's own compiled face anchors the ctx
// and supplies the real SVar body); the end-to-end pin runs the real end-step
// trigger through its PayLife<4> body-cost window (trigcost1, already
// landed) and pins two drawn cards -> two tokens.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// drawCardFor emits the real Draw event the engine's own draw path emits
// (rules/engine.go's drawCard): the library's top card to the hand, Secret
// like every hidden-zone move, so the fold under test sees exactly the event
// shape play produces.
func drawCardFor(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		t.Fatalf("seat %d's library is empty before the draw", p)
	}
	before := len(e.G.Zone(state.ZHand, p))
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0], From: state.ZLibrary, To: state.ZHand, Secret: true})
	if got := len(e.G.Zone(state.ZHand, p)); got != before+1 {
		t.Fatalf("hand after the emitted draw = %d cards, want %d (the move did not apply)", got, before+1)
	}
}

// TestYouDrewThisTurnHeadFoldsTheTurnsDraws is the eval-level pin on Elenda
// and Azor's real corpus face (its SVar body and its permanent anchor the
// ctx): the head counts this turn's Draw events for the controller only, and
// resets at the TurnChange boundary.
func TestYouDrewThisTurnHeadFoldsTheTurnsDraws(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	elenda := onBoardCard(t, e, 0, corpusCard(t, "Elenda and Azor"))
	ctx := &effects.Ctx{Controller: 0, Source: elenda}

	// Precondition: the corpus SVar is the head this ticket implements.
	body, ok := e.G.Obj(elenda).Face().SVars["Y"]
	if !ok || body != "Count$YouDrewThisTurn" {
		t.Fatalf("test precondition: Elenda and Azor SVar Y = %q (ok %v)", body, ok)
	}
	// Baseline: a modelled head reads an EVALUATED zero with nothing drawn.
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("baseline head = %d (ok %v), want evaluated 0", n, ok)
	}
	// Two draws this turn: 2.
	drawCardFor(t, e, 0)
	drawCardFor(t, e, 0)
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("head after two draws = %d (ok %v), want 2", n, ok)
	}
	// Player isolation: another seat's draw never counts.
	drawCardFor(t, e, 1)
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 2 {
		t.Fatalf("head read seat 1's draw: %d, want 2", n)
	}
	// The turn boundary resets the fold to the new turn's own total.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("head after the TurnChange = %d (ok %v), want 0", n, ok)
	}
	drawCardFor(t, e, 0)
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 1 {
		t.Fatalf("head after the new turn's draw = %d, want 1", n)
	}
}

// TestElendaAndAzorTokensEqualCardsDrawnThisTurn is the end-to-end pin: the
// real end-step trigger, its real `Cost$ PayLife<4>` body-cost window, and
// the real `TokenAmount$ Y` driven by the fixed head — b baseline draws plus
// two more this turn must mint b+2 Vampire Knight tokens with lifelink, and
// paying the cost must charge the 4 life.
func TestElendaAndAzorTokensEqualCardsDrawnThisTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Elenda and Azor")
	elenda := searchMoveByName(t, e, "Elenda and Azor", state.ZBattlefield)
	ctx := &effects.Ctx{Controller: 0, Source: elenda}

	// b is whatever the turn's own draw step already banked (0 for a
	// non-active seat, 1 for the active seat on turn one) — the baseline the
	// two explicit draws are added on top of.
	b, ok := effects.EvalCountOK(e, ctx, "Count$YouDrewThisTurn")
	if !ok || b < 0 || b > 1 {
		t.Fatalf("test precondition: baseline head = %d (ok %v), want the turn's own draw-step draw (0 or 1)", b, ok)
	}
	drawCardFor(t, e, 0)
	drawCardFor(t, e, 0)
	if n, ok := effects.EvalCountOK(e, ctx, "Count$YouDrewThisTurn"); !ok || n != b+2 {
		t.Fatalf("test precondition: head after the two draws = %d (ok %v), want %d", n, ok, b+2)
	}

	// Elenda is already on the battlefield (the ctx anchor above), so drive
	// to the end step directly -- elendaEndStepCostAsk's own move would not
	// find the card in hand/library a second time.
	myLife := e.G.Players[0].Life
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	d := passUntilNonPriority(t, e, 40)
	if d == nil {
		t.Fatal("no decision pending at the end step")
	}
	pay, _ := triggerCostWindowAskDecision(t, d)
	if pay < 0 {
		t.Fatalf("the PayLife<4> body was not offered as payable: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Players[0].Life; got != myLife-4 {
		t.Fatalf("life = %d after paying the end-step cost, want %d (the payment precondition)", got, myLife-4)
	}
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().Name != "Vampire Knight Token" {
			continue
		}
		if !e.HasKeyword(id, "Lifelink") {
			t.Errorf("minted %s token lacks Lifelink", o.Face().Name)
		}
		n++
	}
	if n != int(b)+2 {
		t.Fatalf("Vampire Knight tokens minted = %d, want %d (b=%d draws this turn)", n, int(b)+2, b)
	}
}
