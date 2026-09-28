package rules

// The report's real card: a creature printed "enters the battlefield with two
// +1/+1 counters on it" (K:etbCounter:P1P1:2) entering under Doubling Season
// used to stay at 2 counters. CR 614.5: a replacement effect does not "use
// up" an event, and the counters its instruction places are themselves an
// event that other CR 614 AddCounter replacements can modify; CR 616.1e: each
// applicable replacement reads the running total. The placement is routed
// through the AddCounter pass (counterReplacementFold, cli-20260922T225140Z);
// what this ticket adds is the PROVENANCE the matcher's own gates read when
// the body emits: the wrapper is off the stack by then, so EffectOnly$
// (Doubling Season) and ValidSource$ (Vorinclex) failed the new event closed.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDoublingSeasonDoublesEntryCounterBodyPlacement is the report's exact
// card pair, over the harness move path (no cast): the "enters with two
// +1/+1 counters" body's placement is a new event, and Doubling Season's
// "if an effect would put one or more counters" replacement must read it.
func TestDoublingSeasonDoublesEntryCounterBodyPlacement(t *testing.T) {
	t.Parallel()
	// Precondition: with no doubler on the board the body really does place
	// 2 counters (2 != 4, so the doubled assertion below cannot pass vacuously).
	cre := entryCounterEtbCreature(t)
	e, cfg := tokenReplGame(t, 221, cre)
	id := moveSeededCard(t, e, 0, cre, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: recipient not on battlefield: %+v", o)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: entry body placement = %d, want 2", got)
	}
	replayCheck(t, e, cfg)

	ds := tokenReplCorpusCard(t, "Doubling Season")
	cre = entryCounterEtbCreature(t)
	e, cfg = tokenReplGame(t, 222, ds, cre)
	did := moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	if o := e.G.Obj(did); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Doubling Season not on the battlefield")
	}
	id = moveSeededCard(t, e, 0, cre, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: recipient not on battlefield: %+v", o)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != 4 {
		t.Fatalf("entry body placement under Doubling Season = %d, want 4 (2 doubled by the AddCounter replacement)", got)
	}
	replayCheck(t, e, cfg)
}

// TestDoublingSeasonDoublesCastEntryCounterBody is the player-visible path: a
// real cast. When the body's CounterChange emits, the spell wrapper has
// already left the stack (the entry move applied it), so the matcher's
// actionCause-based EffectOnly$ read saw nothing -- the same 2-instead-of-4
// through the exact same gate.
func TestDoublingSeasonDoublesCastEntryCounterBody(t *testing.T) {
	t.Parallel()
	ds := tokenReplCorpusCard(t, "Doubling Season")
	cre := entryCounterEtbCreature(t)
	e, cfg := tokenReplGame(t, 223, ds, cre)
	moveSeededCard(t, e, 0, ds, state.ZBattlefield)
	moveSeededCard(t, e, 0, cre, state.ZHand)
	toMain1(t, e)
	e.priorityRound()
	castSpellOption(t, e, "Entry Etb")
	passPriorityOnce(t, e)
	passPriorityOnce(t, e)
	found := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Entry Etb" {
			found++
			if o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: recipient not on battlefield: %+v", o)
			}
			if got := o.Counter("P1P1"); got != 4 {
				t.Fatalf("cast entry body placement under Doubling Season = %d, want 4", got)
			}
		}
	}
	if found != 1 {
		t.Fatalf("precondition: entering creature not on the battlefield exactly once (%d)", found)
	}
	replayCheck(t, e, cfg)
}

// TestVorinclexReadsCastEntryCounterBodySource pins the ValidSource$ half of
// the same provenance hole, both directions of Vorinclex's pair: ITS
// controller's creature enters with double counters ("If YOU would put"),
// an OPPONENT's enters with half rounded down ("If an OPPONENT would put").
// Neither has a published adder or a stack cause at the nested emit.
func TestVorinclexReadsCastEntryCounterBodySource(t *testing.T) {
	t.Parallel()
	cre := entryCounterEtbCreature(t)
	vori := tokenReplCorpusCard(t, "Vorinclex, Monstrous Raider")

	// Own Vorinclex: the body's adder is the entering creature's controller,
	// which is Vorinclex's controller too -- ValidSource$ You doubles 2 to 4.
	e, cfg := tokenReplGame(t, 224, vori, cre)
	moveSeededCard(t, e, 0, vori, state.ZBattlefield)
	moveSeededCard(t, e, 0, cre, state.ZHand)
	toMain1(t, e)
	e.priorityRound()
	castSpellOption(t, e, "Entry Etb")
	passPriorityOnce(t, e)
	passPriorityOnce(t, e)
	found := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Entry Etb" {
			found++
			if got := o.Counter("P1P1"); got != 4 {
				t.Fatalf("own Vorinclex: cast entry body placement = %d, want 4 (ValidSource$ You doubles)", got)
			}
		}
	}
	if found != 1 {
		t.Fatalf("precondition: entering creature not on the battlefield exactly once (%d)", found)
	}
	replayCheck(t, e, cfg)

	// Opponent's Vorinclex: ValidSource$ Opponent halves 2 to 1.
	cre = entryCounterEtbCreature(t)
	e, cfg = tokenReplGameSeats(t, 225, []*cards.Card{cre}, []*cards.Card{vori})
	if v := moveSeededCard(t, e, 1, vori, state.ZBattlefield); e.G.Obj(v) == nil || e.G.Obj(v).Zone != state.ZBattlefield {
		t.Fatal("precondition: opponent's Vorinclex not on the battlefield")
	}
	moveSeededCard(t, e, 0, cre, state.ZHand)
	toMain1(t, e)
	e.priorityRound()
	castSpellOption(t, e, "Entry Etb")
	passPriorityOnce(t, e)
	passPriorityOnce(t, e)
	found = 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Entry Etb" {
			found++
			if got := o.Counter("P1P1"); got != 1 {
				t.Fatalf("opponent's Vorinclex: cast entry body placement = %d, want 1 (ValidSource$ Opponent halves, rounded down)", got)
			}
		}
	}
	if found != 1 {
		t.Fatalf("precondition: entering creature not on the battlefield exactly once (%d)", found)
	}
	replayCheck(t, e, cfg)
}

// TestCR614EntryBodyCounterIsReplaceable is the CR-lane statement of the same
// verdict on a REAL corpus card: Triskelion ("enters the battlefield with
// three +1/+1 counters on it", K:etbCounter:P1P1:3) cast under Doubling
// Season enters with SIX counters, not three -- CR 614.5 (the entry body's
// placement is itself an event) with CR 616.1e (each applicable replacement
// reads the running total).
func TestCR614EntryBodyCounterIsReplaceable(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Triskelion", "Doubling Season"}, nil)
	ds := crAbortMove(t, e, 0, "Doubling Season", state.ZBattlefield)
	lineOK := false
	for _, r := range e.G.Obj(ds).Face().Repls {
		if r.Event == "AddCounter" && r.Params["EffectOnly"] == "True" {
			lineOK = true
		}
	}
	if !lineOK || e.G.Obj(ds).Zone != state.ZBattlefield {
		t.Fatal("precondition: Doubling Season's EffectOnly AddCounter replacement not live on the battlefield")
	}
	trisk := crAbortMove(t, e, 0, "Triskelion", state.ZHand)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 6})
	e.askPriority(0)
	crAbortAnswer(t, e, "Triskelion", crAbortOption(t, e, "Triskelion", "cast", trisk))
	crResolutionRound(t, e)
	if e.G.Obj(trisk).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Triskelion not on the battlefield: zone=%s", e.G.Obj(trisk).Zone)
	}
	if got := e.G.Obj(trisk).Counter("P1P1"); got != 6 {
		t.Errorf("CR 614.5/616.1e Triskelion/Doubling Season seq %d: entry body placement = %d counters; want 6 (3 doubled)", len(e.L.Events), got)
	}
}
