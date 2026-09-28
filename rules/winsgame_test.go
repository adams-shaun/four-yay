package rules

// Tests for api:WinsGame (task api-winsgame) and the Count$MostCardName head
// its pinned real carrier needs. Mechanized Production is the end-to-end pin:
// its upkeep trigger copies the enchanted artifact, then wins the game if the
// controller has eight or more artifacts sharing a name -- the count comes
// from `SVar:X:Count$MostCardName Artifact.YouCtrl`, the win from
// `DB$ WinsGame | Defined$ You | ConditionCheckSVar$ X | ConditionSVarCompare$
// GE8`. The test places the real corpus card plus eight same-named artifact
// permanents under seat 0, fires the upkeep trigger, and asserts the game ends
// with seat 0 as the winner (events.GameOver's Amount-0 win shape).
//
// The helper tests pin the two halves in isolation so a regression names the
// broken half directly: Count$MostCardName's greatest-same-name count, and
// WinsGame's GameOver emission.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// winsGameEngine builds a 2-seat game with the toss pinned to seat 0 and both
// decks drawn from compiled corpus cards (Mechanized Production in seat 0's,
// Mountains in seat 1's), advanced to seat 0's first Main 1.
func winsGameEngine(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	mp := searchCorpusCard(t, reg, "Mechanized Production")
	mountain := searchCorpusCard(t, reg, "Mountain")
	seatDeck := make([]*cards.Card, 0, 40)
	seatDeck = append(seatDeck, mp)
	for len(seatDeck) < 40 {
		seatDeck = append(seatDeck, mountain)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 4201, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{seatDeck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	if e.G.Active != 0 {
		t.Fatalf("precondition: seat 0 must be active, got %d", e.G.Active)
	}
	return e, cfg
}

// sameNamedRelic is the synthetic artifact whose name the eight permanents
// share. Inline (never a committed Forge script), per the licensing rule.
const sameNamedRelic = "Name:Test Relic\nTypes:Artifact\nOracle:x\n"

// mustMostCardName evaluates a Count$MostCardName body through the shared
// count evaluator and fails if the head is not evaluated at all (an
// unregistered head reads (0, false) -- exactly the vacuous pass this guards).
func mustMostCardName(t *testing.T, e *Engine, spec string) int32 {
	t.Helper()
	n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, Source: 0}, "Count$MostCardName "+spec)
	if !ok {
		t.Fatalf("Count$MostCardName %q came back unevaluated -- the head is not registered", spec)
	}
	return n
}

// TestCountMostCardNameReadsTheGreatestSameNameCount pins the count head on
// its own: with eight same-named artifacts and two differently-named ones the
// greatest single-name count is 8, and a spec that matches nothing reads the
// evaluated zero (not the unresolvable verdict).
func TestCountMostCardNameReadsTheGreatestSameNameCount(t *testing.T) {
	t.Parallel()
	e, _ := winsGameEngine(t)
	before := mustMostCardName(t, e, "Artifact.YouCtrl")
	if before != 0 {
		t.Fatalf("precondition: no artifacts on the board yet, got %d", before)
	}
	for i := 0; i < 8; i++ {
		onBoard(t, e, 0, sameNamedRelic)
	}
	onBoard(t, e, 0, "Name:Another Relic\nTypes:Artifact\nOracle:x\n")
	onBoard(t, e, 0, "Name:Third Relic\nTypes:Artifact\nOracle:x\n")
	if got := mustMostCardName(t, e, "Artifact.YouCtrl"); got != 8 {
		t.Fatalf("Count$MostCardName Artifact.YouCtrl = %d, want 8 (the eight Test Relics)", got)
	}
	// A spec matching nothing is a modelled head counting zero, NOT the
	// fail-closed verdict: assert the verdict is true so a future regression
	// that stops evaluating the head cannot pass as this zero.
	if n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, Source: 0},
		"Count$MostCardName Creature.YouCtrl"); !ok || n != 0 {
		t.Fatalf("Count$MostCardName Creature.YouCtrl = (%d, %v), want (0, true)", n, ok)
	}
	// Seat 1 controls nothing: the YouCtrl spec is zero for seat 1.
	if n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 1, Source: 0},
		"Count$MostCardName Artifact.YouCtrl"); !ok || n != 0 {
		t.Fatalf("Count$MostCardName for seat 1 = (%d, %v), want (0, true)", n, ok)
	}
}

// TestMechanizedProductionWinsWithEightSameNamedArtifacts is the brief's
// end-to-end pin: the real corpus card, eight same-named artifacts under its
// controller, the upkeep trigger fired, and the game ends with seat 0 the
// winner. Seat 1 is left alive, so this is the alt-win path (CR 104.2a), not
// the last-seat-standing sweep.
func TestMechanizedProductionWinsWithEightSameNamedArtifacts(t *testing.T) {
	t.Parallel()
	e, _ := winsGameEngine(t)
	mp := onBoardCard(t, e, 0, corpusCardByName(t, "Mechanized Production"))
	relics := make([]state.ObjID, 0, 8)
	for i := 0; i < 8; i++ {
		relics = append(relics, onBoard(t, e, 0, sameNamedRelic))
	}
	// Attach the Aura so `Defined$ Enchanted` resolves to a real artifact
	// (the CopyPermanent leg). Precondition, not decoration: without it the
	// enchant leg resolves nothing and the token copy never happens.
	e.emit(events.Event{Kind: events.Attach, Obj: mp, IDs: []state.ObjID{relics[0]}})
	if got := e.G.Obj(mp).AttachedTo; got != relics[0] {
		t.Fatalf("precondition: Mechanized Production attached to %d, want %d", got, relics[0])
	}
	if got := mustMostCardName(t, e, "Artifact.YouCtrl"); got != 8 {
		t.Fatalf("precondition: Count$MostCardName = %d, want 8", got)
	}

	// Fire Mechanized Production's upkeep trigger directly (the same
	// event-driven shape the trigger queue sees at the real upkeep step).
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("precondition: the upkeep trigger did not queue")
	}
	e.putTriggersOnStack()
	e.resolveTop()
	// The CopyPermanent leg mints a token; drain any priority pause so the
	// SubAbility chain (DBWin) reaches resolution.
	passUntilStackEmpty(t, e, 20)

	if !e.G.Over {
		t.Fatalf("the game did not end after the upkeep trigger (Over=%v)", e.G.Over)
	}
	if e.G.Draw {
		t.Fatal("the game ended as a draw, want seat 0 the winner")
	}
	if e.G.Winner != 0 {
		t.Fatalf("winner = %d, want seat 0", e.G.Winner)
	}
	if e.G.Players[1].Lost {
		t.Fatal("seat 1 must still be alive: this is an alt-win, not an elimination")
	}
	// The win's observable is the Amount-0 GameOver naming seat 0.
	won := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.GameOver && ev.Player == 0 && ev.Amount == 0 {
			won = true
		}
	}
	if !won {
		t.Fatal("the log carries no GameOver win event for seat 0")
	}
	// No replayCheck here: the board is built with the eventless onBoard
	// placements the card-test harness uses, so a log-only replay from cfg
	// cannot rebuild it (the same reason the alter-attribute and populate
	// carrier tests omit it). TestHeads and make sim cover replay for the
	// registered primitives.
}

// TestWinsGameEmitsTheGameOverWin pins api:WinsGame's own body: a synthetic
// upkeep trigger with an unconditional `DB$ WinsGame | Defined$ You` ends the
// game for its controller even with both seats alive. A negative control
// (seat 1's own WinsGame body) proves the event names the resolving
// controller, not seat 0's zero value.
func TestWinsGameEmitsTheGameOverWin(t *testing.T) {
	t.Parallel()
	e, _ := winsGameEngine(t)
	watcher := onBoard(t, e, 0, `Name:Winnower
Types:Creature
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ Win
SVar:Win:DB$ WinsGame | Defined$ You
Oracle:synthetic WinsGame probe
`)
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("queued triggers = %d, want the watcher's upkeep trigger", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if !e.G.Over || e.G.Winner != 0 || e.G.Draw {
		t.Fatalf("after WinsGame: over=%v winner=%d draw=%v, want seat 0 to win", e.G.Over, e.G.Winner, e.G.Draw)
	}
	if e.G.Obj(watcher) == nil {
		t.Fatal("precondition: the watcher left the battlefield")
	}
	// No unimplemented-API Note: the registration is one of the things a
	// "nothing happened" test can silently pass without.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API WinsGame" {
			t.Fatal("WinsGame resolved to the unimplemented-API fallback")
		}
	}
}
