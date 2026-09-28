package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// resolveSephirothDeath queues and resolves one "another creature dies"
// trigger on sep: it emits the labelled MoveZone, lets putTriggersOnStack
// place the trigger (answering the trigger's ValidTgts$ Opponent ask), and
// resolves it off the stack. The caller owns the board.
func resolveSephirothDeath(t *testing.T, e *Engine, reg *cards.Registry, sep state.ObjID) {
	t.Helper()
	victim := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	// No Text:"sacrificed" label -- Sephiroth's trigger is the plain graveyard
	// move, and the label would also fire any board's Rakdos, the Muscle
	// (Mode$ Sacrificed), turning this into a two-trigger ordering ask.
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield,
		To: state.ZGraveyard})
	if n := observedTriggerCount(e, sep); n == 0 {
		t.Fatalf("precondition: another creature dying queued no Sephiroth trigger")
	}
	e.putTriggersOnStack()
	if d := e.Pending(); d != nil {
		if d.Kind != decision.KTarget {
			t.Fatalf("Sephiroth trigger ask = %v, want KTarget", d.Kind)
		}
		submitTargetTo(t, e, 1)
	}
	for len(e.G.Stack) > 0 {
		e.resolveTop()
	}
}

// TestSephirothTransformsOnlyOnFourthResolution is the reported defect
// (feedback 20260923T005857Z, symptom 1): Sephiroth, Fabled SOLDIER's
// "Whenever another creature dies ... If this is the fourth time this ability
// has resolved this turn, transform Sephiroth" flipped on the FIRST
// resolution. The gate is `ConditionCheckSVar$ X` with X =
// Count$ResolvedThisTurn; the head was unmodelled, so the SVar condition
// failed OPEN and SetState transformed every time. With the head modelled
// the face holds until the fourth resolution.
func TestSephirothTransformsOnlyOnFourthResolution(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	sep := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sephiroth, Fabled SOLDIER"))

	// Precondition: on the battlefield, showing the FRONT face with mana
	// value 3 -- the transform is what this test observes and the back face's
	// "no cost" would make the face check vacuous.
	o := e.G.Obj(sep)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sephiroth not on the battlefield (%+v)", o)
	}
	if f := o.Face(); f == nil || f.Name != "Sephiroth, Fabled SOLDIER" {
		t.Fatalf("precondition: Sephiroth face = %v, want the front face", f)
	}
	if mv := o.Face().ManaValue(); mv != 3 {
		t.Fatalf("precondition: front-face mana value = %d, want 3", mv)
	}

	for i := 1; i <= 4; i++ {
		resolveSephirothDeath(t, e, reg, sep)
		face := e.G.Obj(sep).Face().Name
		if i < 4 {
			if face != "Sephiroth, Fabled SOLDIER" {
				t.Fatalf("after %d resolution(s) face = %q, want the front face (transformed too early)", i, face)
			}
			continue
		}
		if face != "Sephiroth, One-Winged Angel" {
			t.Fatalf("after 4 resolutions face = %q, want the back face (never transformed)", face)
		}
	}
	// Symptom 2's cause: once transformed, the back face prints
	// "ManaCost:no cost", so every mana-value read is 0. That is the printed
	// rule, and the fix is the gate above, not the value.
	if mv := e.G.Obj(sep).Face().ManaValue(); mv != 0 {
		t.Fatalf("back-face mana value = %d, want 0 (ManaCost:no cost)", mv)
	}
}

// TestRakdosExilesUntransformedSephirothManaValue pins symptom 2 end to end
// on the real Rakdos, the Muscle trigger: sacrificing a FRONT-face Sephiroth
// (mana value 3) exiles three cards, the count the player saw collapse to
// zero once the spurious transform gave Rakdos a "no cost" object. It is the
// same TriggeredCard$CardManaCost read rules/rakdos_muscle_deck_test.go pins,
// driven through the actual sacrifice trigger rather than a cast.
func TestRakdosExilesUntransformedSephirothManaValue(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	rakdos := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Rakdos, the Muscle"))
	sep := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sephiroth, Fabled SOLDIER"))

	// Precondition: Sephiroth is the front face with mana value 3, and the
	// library the trigger digs into holds at least four cards -- the top three
	// are the expected exile set, the fourth proves the count is not larger.
	if f := e.G.Obj(sep).Face(); f == nil || f.ManaValue() != 3 {
		t.Fatalf("precondition: sacrifice target mana value = %v, want the front-face 3", f)
	}

	// The player's exact sequence: Sephiroth's dies-trigger resolves ONCE
	// (another creature dying), then Rakdos sacrifices it. Without the fix
	// that one resolution flips the back face and the Glue reads 0.
	resolveSephirothDeath(t, e, reg, sep)
	if f := e.G.Obj(sep).Face(); f == nil || f.Name != "Sephiroth, Fabled SOLDIER" || f.ManaValue() != 3 {
		t.Fatalf("after one resolution face = %v, want the front face with mana value 3", f)
	}

	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 4 {
		t.Fatalf("precondition: library holds %d cards, want at least 4", len(lib))
	}
	topThree := append([]state.ObjID(nil), lib[:3]...)
	fourth := lib[3]

	// Sacrifice Sephiroth to its controller's own board: Rakdos's
	// T:Mode$ Sacrificed | ValidPlayer$ You trigger observes the label.
	e.emit(events.Event{Kind: events.MoveZone, Obj: sep, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "sacrificed"})
	if n := observedTriggerCount(e, rakdos); n == 0 {
		t.Fatalf("precondition: sacrificing Sephiroth queued no Rakdos trigger")
	}
	e.putTriggersOnStack()
	if d := e.Pending(); d != nil {
		if d.Kind != decision.KTarget {
			t.Fatalf("Rakdos dig ask = %v, want KTarget", d.Kind)
		}
		submitTargetTo(t, e, 0)
	}
	for len(e.G.Stack) > 0 {
		e.resolveTop()
	}

	for _, id := range topThree {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("top-library card %d zone = %s, want exile (Rakdos exiles mana-value 3 cards)", id, z)
		}
	}
	if z := e.G.Obj(fourth).Zone; z != state.ZLibrary {
		t.Fatalf("fourth library card zone = %s, want library -- Rakdos exiled more than the mana value", z)
	}
}

// TestSephirothResolvedTallyReplaysExactly proves the per-ability tally is
// replay-deterministic: it is folded from the existing Resolve events (no new
// Kind or field) and reset at TurnChange, so a log-only replay rebuilds the
// same ordinals and the fourth resolution still transforms. Every placement
// here is event-driven (moveByName), unlike the onBoardCard fixtures above, so
// the replay comparison is meaningful; Sephiroth's own ETB trigger is dropped
// with the established e.pending = nil pattern before the deaths are driven.
func TestSephirothResolvedTallyReplaysExactly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sep, ok := reg.Lookup("Sephiroth, Fabled SOLDIER")
	if !ok {
		t.Fatal("corpus fixture: Sephiroth, Fabled SOLDIER missing")
	}
	bear, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("corpus fixture: Grizzly Bears missing")
	}
	deck := []*cards.Card{sep, bear, bear, bear, bear}
	cfg := seatZeroStart(Config{Seed: 9, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck, mountainDeck(t, 40-len(deck))...), mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	sepid := moveByName(t, e, 0, "Sephiroth, Fabled SOLDIER", state.ZBattlefield)
	// Drop Sephiroth's own ETB trigger (the "may sacrifice another creature"
	// cost window): it is not what this test drives, and the established
	// pattern discards it rather than resolving it. Both the queued trigger
	// and any posed decision are cleared so the deaths below place only their
	// own trigger.
	e.pendingTriggers, e.orderedTriggers, e.pending = nil, 0, nil
	e.priorityRound()

	for i := 0; i < 4; i++ {
		victim := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		e.pending = nil
		e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
		if n := observedTriggerCount(e, sepid); n == 0 {
			t.Fatalf("death %d queued no Sephiroth trigger", i+1)
		}
		e.putTriggersOnStack()
		if d := e.Pending(); d != nil {
			if d.Kind != decision.KTarget {
				t.Fatalf("death %d ask = %v, want KTarget", i+1, d.Kind)
			}
			submitTargetTo(t, e, 1)
		}
		for len(e.G.Stack) > 0 {
			e.resolveTop()
		}
	}
	if e.G.Obj(sepid).FaceIdx != 1 {
		t.Fatalf("after four replayed resolutions FaceIdx = %d, want 1", e.G.Obj(sepid).FaceIdx)
	}
	replayCheck(t, e, cfg)
}

// TestSephirothResolvedTallyResetsAtTurnChange closes the round-1 review's
// MINOR: the per-ability resolution tally is a per-turn fact, so TurnChange
// must clear it — asserted both directly (the map is gone at the boundary)
// and behaviourally (three resolutions on the FRESH turn leave the face
// front, where a carried-over count of 1 would have made the first of them
// the fourth and flipped Sephiroth mid-way).
func TestSephirothResolvedTallyResetsAtTurnChange(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	sep := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sephiroth, Fabled SOLDIER"))
	if f := e.G.Obj(sep).Face(); f == nil || f.Name != "Sephiroth, Fabled SOLDIER" {
		t.Fatalf("precondition: Sephiroth face = %v, want the front face", f)
	}

	// Turn 1: one resolution puts one entry in the tally — the reset below
	// has something real to clear.
	resolveSephirothDeath(t, e, reg, sep)
	if f := e.G.Obj(sep).Face().Name; f != "Sephiroth, Fabled SOLDIER" {
		t.Fatalf("after one resolution face = %q, want the front face", f)
	}
	if len(e.G.ResolvedThisTurn) == 0 {
		t.Fatal("precondition: one resolution left no ResolvedThisTurn entry to reset")
	}

	// The turn boundary the engine emits every turn.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	if len(e.G.ResolvedThisTurn) != 0 {
		t.Fatalf("after TurnChange ResolvedThisTurn = %v, want empty (per-turn tally must reset)", e.G.ResolvedThisTurn)
	}

	// Fresh turn: the count restarts at 1, so THREE more resolutions stay
	// below the EQ4 gate and the face holds. If the tally had carried over,
	// the first of these would have been the fourth and transformed.
	for i := 1; i <= 3; i++ {
		resolveSephirothDeath(t, e, reg, sep)
		if f := e.G.Obj(sep).Face().Name; f != "Sephiroth, Fabled SOLDIER" {
			t.Fatalf("fresh-turn resolution %d face = %q, want the front face (tally did not reset at the turn boundary)", i, f)
		}
	}
}
