package rules

// Demonic Covenant's end-step mill-sacrifice rider, end to end on the real
// compiled corpus SA (the defender the brief names): the Phase$ End of Turn
// trigger creates the Demon token, mills two (RememberMilled$ True records
// them, ShowMilledCards$ True reveals them publicly), and the chained
// sacrifice's gate — ConditionCheckSVar$ MilledSharesAllTypes with the body
// `Remembered$Valid Card.sharesAllCardTypesWithOther Remembered` and
// ConditionSVarCompare$ GE2 — sacrifices the Covenant exactly when the two
// milled cards share all their card types. Before the
// sharesAllCardTypesWithOther predicate landed the count read 0 and the
// sacrifice never happened.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// covenantEngine seats a directly-entered Demonic Covenant for seat 0 (no ETB
// trigger exists on it, so the SetZone entry is inert), orders seat 0's
// library so the mill's window holds exactly the two cards the test wants,
// and drives to seat 0's turn-1 End of Turn step, where the Phase trigger has
// fired and its resolution chain is either mid-flight or already resolved.
// It returns the engine, the Covenant's id, and the two ids the mill will
// move, in top-to-bottom order.
func covenantEngine(t *testing.T, reg *cards.Registry, top ...*cards.Card) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	return covenantEngineFiller(t, reg, searchCorpusCard(t, reg, "Forest"), searchCorpusCard(t, reg, "Grizzly Bears"), top...)
}

// covenantEngineFiller is covenantEngine with configurable deck filler, so a
// test can load spells (the sharesAll gate's instant/sorcery cases) into the
// milled library.
func covenantEngineFiller(t *testing.T, reg *cards.Registry, fillerA, fillerB *cards.Card, top ...*cards.Card) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	covenant := searchCorpusCard(t, reg, "Demonic Covenant")

	deck := []*cards.Card{covenant}
	for len(deck) < 40 {
		deck = append(deck, fillerA, fillerB)
	}
	cfg := seatZeroStart(Config{Seed: 9901, Names: []string{"covenanter", "opponent"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// Order the library: the wanted cards on top (in the given order), then
	// everything the shuffled deal left, through one real LibraryOrder event.
	lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	var wanted, rest []state.ObjID
	for _, wantCard := range top {
		found := false
		for i, id := range lib {
			if id == 0 {
				continue
			}
			if e.G.Obj(id).Card == wantCard {
				wanted = append(wanted, id)
				lib[i] = 0
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("library did not hold a %s to put on top", nameOf(wantCard.Faces[0]))
		}
	}
	for _, id := range lib {
		if id != 0 {
			rest = append(rest, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: append(wanted, rest...)})

	// Demonic Covenant enters without a cast: it has no ETB trigger or
	// replacement, so a direct entry exercises the end-step Phase trigger
	// exactly as a cast would (the same inert-entry convention
	// manaexpend_test.go's Teapot-loom board uses).
	cov := e.G.AddObject(covenant, 0)
	cov.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append([]state.ObjID{cov.ID}, e.G.Zone(state.ZBattlefield, 0)...))

	driveToStep(t, e, 1, 0, state.StepEnd)
	return e, cov.ID, wanted
}

// covenantSacrificed reports whether the trigger chain milled, revealed and
// (when sharesAll holds) sacrificed, by reading the log and the zones.
func covenantAssertions(t *testing.T, e *Engine, covID state.ObjID, milled []state.ObjID, wantSacrifice bool) {
	t.Helper()

	// The two cards were milled into seat 0's graveyard.
	for _, id := range milled {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("milled card %d = %+v, want in seat 0's graveyard", id, o)
		}
	}

	// A Demon token (5/5 flying) was created for seat 0.
	token := false
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Demon Token" {
			token = true
		}
	}
	if !token {
		t.Fatal("no Demon Token on seat 0's battlefield after the trigger")
	}

	// ShowMilledCards$ True: one PUBLIC reveal Note naming the milled ids.
	sawNote := false
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note || ev.Secret {
			continue
		}
		if len(ev.IDs) != len(milled) {
			continue
		}
		match := true
		for i, id := range ev.IDs {
			if id != milled[i] {
				match = false
				break
			}
		}
		if match {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("no public reveal Note naming %v after the mill", milled)
	}

	// The remembered list was recorded and then cleared by DBCleanup.
	sawRemembered, sawClear := false, false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == covID && ev.Counter == "remembered" {
			sawRemembered = true
		}
		if ev.Kind == events.Choose && ev.Obj == covID && ev.Counter == "clear-remembered" {
			sawClear = true
		}
	}
	if !sawRemembered {
		t.Fatal("no Choose/remembered event on the Covenant after the mill")
	}
	if !sawClear {
		t.Fatal("no Choose/clear-remembered event on the Covenant after DBCleanup")
	}

	// The sacrifice gate's own verdict, read off where the Covenant ended up.
	cov := e.G.Obj(covID)
	if wantSacrifice {
		if cov == nil || cov.Zone != state.ZGraveyard {
			t.Fatalf("Demonic Covenant = %+v, want sacrificed to the graveyard", cov)
		}
		// ShowSacrificedCards$ True on the sacrifice line: a public Note
		// naming the sacrificed Covenant.
		sawSacNote := false
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) == 1 && ev.IDs[0] == covID {
				sawSacNote = true
			}
		}
		if !sawSacNote {
			t.Fatalf("no public sacrifice-reveal Note naming %d", covID)
		}
	} else {
		if cov == nil || cov.Zone != state.ZBattlefield {
			t.Fatalf("Demonic Covenant = %+v, want still on the battlefield", cov)
		}
		// No sacrifice: no sacrifice-reveal Note naming the Covenant either.
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note && len(ev.IDs) == 1 && ev.IDs[0] == covID {
				t.Fatalf("a sacrifice-reveal Note was emitted with no sacrifice: %+v", ev)
			}
		}
	}
}

func TestDemonicCovenantSacrificesWhenMilledCardsShareAllTypes(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	e, covID, milled := covenantEngine(t, reg, bear, bear)

	// Two Grizzly Bears share all their card types (Creature): the count is
	// 2, GE2 holds, and the Covenant is sacrificed.
	passUntilStackEmpty(t, e, 40)
	covenantAssertions(t, e, covID, milled, true)
}

func TestDemonicCovenantStaysWhenMilledCardsDifferInType(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	e, covID, milled := covenantEngine(t, reg, bear, forest)

	// A Bear (Creature) and a Forest (Land) share no card type: the count is
	// 0, the gate fails closed the other way, and the Covenant stays.
	passUntilStackEmpty(t, e, 40)
	covenantAssertions(t, e, covID, milled, false)
}

// TestDemonicCovenantStaysWhenMilledSpellsShareNoType is the r2-review
// regression, end to end: the gate's count head enumerates the candidate's
// ACTUAL card types, so two milled SPELLS that share no card type (an
// Instant and a Sorcery) must NOT fire the GE2 gate. Before the fix the
// fixed probe list had no Instant/Sorcery entry, both trivially passed and
// the Covenant sacrificed itself after milling two spells.
func TestDemonicCovenantStaysWhenMilledSpellsShareNoType(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	bolt := searchCorpusCard(t, reg, "Lightning Bolt")
	growth := searchCorpusCard(t, reg, "Rampant Growth")
	e, covID, milled := covenantEngineFiller(t, reg, bolt, growth, bolt, growth)

	// Precondition: the mill's window really holds an Instant and a Sorcery.
	if f := e.G.Obj(milled[0]).Face(); f == nil || f.Name != "Lightning Bolt" {
		t.Fatalf("milled top card = %+v, want Lightning Bolt", f)
	}
	if f := e.G.Obj(milled[1]).Face(); f == nil || f.Name != "Rampant Growth" {
		t.Fatalf("milled second card = %+v, want Rampant Growth", f)
	}

	passUntilStackEmpty(t, e, 40)
	covenantAssertions(t, e, covID, milled, false)
}

// TestDemonicCovenantSacrificesWhenMilledInstantsShareType is the positive
// spell case on the real corpus: two Lightning Bolts share the card type
// Instant, the count is 2 and the Covenant is sacrificed.
func TestDemonicCovenantSacrificesWhenMilledInstantsShareType(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	bolt := searchCorpusCard(t, reg, "Lightning Bolt")
	e, covID, milled := covenantEngineFiller(t, reg, bolt, searchCorpusCard(t, reg, "Rampant Growth"), bolt, bolt)

	if f := e.G.Obj(milled[1]).Face(); f == nil || f.Name != "Lightning Bolt" {
		t.Fatalf("milled second card = %+v, want Lightning Bolt", f)
	}

	passUntilStackEmpty(t, e, 40)
	covenantAssertions(t, e, covID, milled, true)
}

// TestDemonicCovenantTriggerFiredAtAll guards the fixture: without the
// end-step Phase trigger the assertions above would vacuously compare a
// never-run chain. The mill events must exist on the log.
func TestDemonicCovenantTriggerFiredAtAll(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	e, _, milled := covenantEngine(t, reg, bear, forest)

	passUntilStackEmpty(t, e, 40)
	moved := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZGraveyard {
			moved++
		}
	}
	if moved != len(milled) {
		t.Fatalf("library-to-graveyard MoveZone events = %d, want %d (the mill never ran)", moved, len(milled))
	}
}
