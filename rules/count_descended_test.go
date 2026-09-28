// Count$YouDescendedThisTurn — the per-turn descend count head (ticket
// count-descended). Before the fix the head did not exist in evalCountBody
// and degraded to 0 through the unresolvable-count convention, so The
// Mycotyrant's SVar X fed TokenAmount$ X with X = 0 and its end-step trigger
// minted nothing no matter how many permanent cards had gone to the
// graveyard, and Molten Collapse's CharmNum$ Count$Compare Y GE1.2.1 never
// held.
//
// The head reads the fx20 descend provenance through the SAME helper the
// Player.descended predicate uses (effects.descendedThisTurn), so the head
// and the predicate can never drift apart. The eval-level pin runs on The
// Mycotyrant's real compiled face (its SVar body and permanent anchor the
// ctx); the end-to-end pin drives the real end-step trigger through the real
// TokenAmount$ X and pins two descended permanents -> two Fungus tokens.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// descendThrough emits the real MoveZone event a permanent card's graveyard
// entry produces (the shape rules/player_spec_closure_test.go's
// TestPlayerSpecBroodrageDescendedFromAnyZone relies on) and asserts the move
// applied, so a fixture that fails to descend fails loudly rather than
// reading a silent zero.
func descendThrough(t *testing.T, e *Engine, id state.ObjID, from state.Zone) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("descend source %d has no face", id)
	}
	if !o.Face().IsPermanent() {
		t.Fatalf("test precondition: %q is not a permanent card", o.Face().Name)
	}
	if o.Zone != from {
		t.Fatalf("test precondition: %d is in zone %s, want %s", id, o.Zone, from)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZGraveyard})
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("object %d zone after the emitted move = %s, want graveyard (the move did not apply)", id, got)
	}
}

// TestYouDescendedThisTurnHeadFoldsTheLedger is the eval-level pin on The
// Mycotyrant's real corpus face: the head counts permanent cards that entered
// the controller's graveyard this turn from any zone, ignores nonpermanent
// cards and tokens, isolates by owner, and resets at the TurnChange boundary.
func TestYouDescendedThisTurnHeadFoldsTheLedger(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	myco := onBoardCard(t, e, 0, corpusCard(t, "The Mycotyrant"))
	ctx := &effects.Ctx{Controller: 0, Source: myco}

	// Precondition: the corpus SVar is the head this ticket implements, and
	// the head is MODELLED (a legitimate zero, not the unresolvable verdict).
	body, ok := e.G.Obj(myco).Face().SVars["X"]
	if !ok || body != "Count$YouDescendedThisTurn" {
		t.Fatalf("test precondition: The Mycotyrant SVar X = %q (ok %v)", body, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("baseline head = %d (ok %v), want evaluated 0", n, ok)
	}

	// A permanent card (Grizzly Bears) descends: 1.
	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	descendThrough(t, e, bear, state.ZBattlefield)
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("head after one permanent descends = %d (ok %v), want 1", n, ok)
	}

	// A SECOND permanent card descends: 2. Two distinct counts so a silent
	// zero (or a hardcoded one) cannot pass.
	bear2 := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	descendThrough(t, e, bear2, state.ZBattlefield)
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("head after two permanents descend = %d (ok %v), want 2", n, ok)
	}

	// A NONPERMANENT card (Lightning Bolt) moving to the graveyard is not a
	// descent (CR 700.11: a permanent CARD).
	if corpusCard(t, "Lightning Bolt").Faces[0].IsPermanent() {
		t.Fatal("test precondition: Lightning Bolt must be a nonpermanent card")
	}
	bolt := onBoardCard(t, e, 0, corpusCard(t, "Lightning Bolt"))
	e.emit(events.Event{Kind: events.MoveZone, Obj: bolt, From: state.ZBattlefield, To: state.ZGraveyard})
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 2 {
		t.Fatalf("a nonpermanent card changed the descend count: %d, want 2", n)
	}

	// A TOKEN moving to the graveyard is not a descent either ($PermanentCard
	// is folded false for a token at the move). Mint a real Fungus token the
	// way the engine's DB$ Token body does (TokenCreate) before moving it.
	tokDef, okTok := testutil.CorpusRegistry(t).Tokens["b_1_1_fungus_noblock"]
	if !okTok || tokDef == nil {
		t.Fatal("test precondition: corpus token b_1_1_fungus_noblock missing")
	}
	const tokKey = "fixture:descend_token"
	if e.G.Tokens == nil {
		e.G.Tokens = map[string]*cards.Card{}
	}
	e.G.Tokens[tokKey] = tokDef
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: tokKey})
	var tokID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Fungus Token" {
			tokID = id
			break
		}
	}
	if tokID == 0 {
		t.Fatal("test precondition: failed to mint a Fungus token on the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: tokID, From: state.ZBattlefield, To: state.ZGraveyard})
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 2 {
		t.Fatalf("a token changed the descend count: %d, want 2", n)
	}

	// Another seat's permanent card is another player's descent, not ours.
	other := onBoardCard(t, e, 1, corpusCard(t, "Grizzly Bears"))
	descendThrough(t, e, other, state.ZBattlefield)
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 2 {
		t.Fatalf("head counted another seat's permanent: %d, want 2", n)
	}

	// Seat 1's own head reads seat 1's ledger: the same events give 1 for it.
	if n, _ := effects.EvalCountOK(e, &effects.Ctx{Controller: 1, Source: myco}, body); n != 1 {
		t.Fatalf("seat 1's head = %d, want 1 (seat 1's own bear)", n)
	}

	// The turn boundary resets the ledger.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0})
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("head after the TurnChange = %d (ok %v), want 0", n, ok)
	}
}

// TestMoltenCollapseCharmNumResolvesDescents is an eval-level pin on the
// other corpus carrier: Molten Collapse's real CharmNum$ expression
// (Count$Compare Y GE1.2.1) reads 1 with an empty ledger and 2 once the
// controller has descended. This is exactly the value the charm machinery
// consumes to decide whether both modes may be chosen.
func TestMoltenCollapseCharmNumResolvesDescents(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	reg := searchTestRegistry(t)
	mc := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Molten Collapse"))
	// The charm machinery binds the resolving face's SVar table onto the ctx
	// (effects/registry.go's specCtxSVars), so the eval pin binds it too --
	// the Compare operand Y is an SVar name, not an inline expression.
	ctx := &effects.Ctx{Controller: 0, Source: mc, SVars: e.G.Obj(mc).Face().SVars}

	// Precondition: the corpus face carries the expression the charm uses.
	body, ok := e.G.Obj(mc).Face().Abilities[0].Params["CharmNum"]
	if !ok || body != "Count$Compare Y GE1.2.1" {
		t.Fatalf("test precondition: Molten Collapse CharmNum = %q (ok %v)", body, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("charm count before any descent = %d (ok %v), want 1", n, ok)
	}

	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	descendThrough(t, e, bear, state.ZBattlefield)
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("charm count after one descent = %d (ok %v), want 2", n, ok)
	}
}

// TestMycotyrantTokensEqualDescentsThisTurn is the end-to-end pin: the real
// end-step trigger, the real TokenAmount$ X, and the fixed head — after one
// descent this turn the trigger must mint exactly one 1/1 black Fungus token
// that can't block, and after two descents exactly two (two different N so a
// silent zero cannot pass).
func TestMycotyrantTokensEqualDescentsThisTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	for _, descents := range []int{1, 2} {
		e, _ := searchEngine(t, reg, "The Mycotyrant")
		myco := searchMoveByName(t, e, "The Mycotyrant", state.ZBattlefield)
		if e.G.Obj(myco).Zone != state.ZBattlefield {
			t.Fatal("precondition: The Mycotyrant not on the battlefield")
		}
		if e.G.Active != 0 {
			t.Fatalf("precondition: active seat = %d, want 0", e.G.Active)
		}

		// Descend `descents` times with real permanent cards.
		for i := 0; i < descents; i++ {
			bear := onBoardCard(t, e, 0, searchCorpusCard(t, reg, "Grizzly Bears"))
			e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
		}
		ctx := &effects.Ctx{Controller: 0, Source: myco}
		if n, ok := effects.EvalCountOK(e, ctx, "Count$YouDescendedThisTurn"); !ok || n != int32(descents) {
			t.Fatalf("test precondition: head after %d descents = %d (ok %v), want %d", descents, n, ok, descents)
		}

		// Drive to seat 0's end step and let the trigger resolve. The
		// end-step boundary queues the mandatory trigger onto the stack.
		driveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
		if len(e.G.Stack) == 0 {
			t.Fatal("precondition: Mycotyrant end-step trigger not on the stack")
		}
		passUntilStackEmpty(t, e, 20)

		n := 0
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || o.Face().Name != "Fungus Token" {
				continue
			}
			// The token's can't-block is an S:Mode$ CantBlock static (not a
			// K: keyword), so read it exactly where declare-blockers reads it.
			if !e.blockRestricted(id, myco) {
				t.Errorf("minted %s token is not blocked from blocking", o.Face().Name)
			}
			n++
		}
		if n != descents {
			t.Fatalf("%d descents minted %d Fungus tokens, want %d", descents, n, descents)
		}
	}
}
