package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The trig:Evolved notification (CR 702.99b, task agent-20260919T183836Z-df01c8be):
// the Evolve keyword's own counter trigger is unchanged, but when it actually
// puts its +1/+1 counter the engine emits the events.Evolved marker so a
// sibling T:Mode$ Evolved ability on the same creature fires. These tests use
// the two real corpus carriers (Watchful Radstag, Renegade Krasis), loaded
// through CorpusRegistry -- the .cards text is never copied into a fixture.
//
// Both tests assert the PRECONDITIONS their conclusion depends on: the
// evolving creature is on the battlefield with zero counters before the
// bigger creature enters, the bigger creature actually entered, and the
// evolve counter actually landed. A vacuous setup therefore fails loudly.

// evolvedMarkerCount counts events.Evolved naming id in the log.
func evolvedMarkerCount(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Evolved && ev.Obj == id {
			n++
		}
	}
	return n
}

// noUnimplementedAPI fails if the log carries an "unimplemented API" note, so
// a test that merely observes "nothing happened" cannot pass while the whole
// feature (or a needed sibling primitive) is unregistered.
func noUnimplementedAPI(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("log carries an unimplemented-API note: %q", ev.Text)
		}
	}
}

// enterFromLibrary moves the named seeded corpus card from seat p's library to
// the battlefield with a logged MoveZone -- the same raw setup move
// rules/keyword_triggers_test.go's evolve test uses, which needs no driveToStep
// (a later priorityRound gates the entry's triggers).
func enterFromLibrary(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				return id
			}
		}
	}
	t.Fatalf("seeded corpus card %q not found in seat %d's library or hand", name, p)
	return 0
}

// evolvedGame seeds the named corpus cards into a two-seat game, advances to a
// priority, and returns the engine, the live Config replayCheck needs, and the
// looked-up cards by name. Cards enter later through enterFromLibrary.
func evolvedGame(t *testing.T, seed uint64, names ...string) (*Engine, Config, map[string]*cards.Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	byName := make(map[string]*cards.Card, len(names))
	deck := make([]*cards.Card, 0, len(names))
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing corpus card %s", name)
		}
		byName[name] = c
		deck = append(deck, c)
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	return e, cfg, byName
}

// TestWatchfulRadstagEvolvesAndCopiesItself drives the headline carrier end to
// end: a bigger creature enters under Radstag's controller, the evolve
// keyword puts the +1/+1 counter, and "whenever CARDNAME evolves" creates a
// token that's a copy of the Radstag.
func TestWatchfulRadstagEvolvesAndCopiesItself(t *testing.T) {
	t.Parallel()
	const radstagName, bigName = "Watchful Radstag", "Craw Wurm"
	e, cfg, byName := evolvedGame(t, 43, radstagName, bigName)

	radstag := enterFromLibrary(t, e, 0, radstagName)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	// Preconditions: the Radstag is on the battlefield and has not evolved
	// yet, so the pass below is the marker's doing, not leftover state.
	if o := e.G.Obj(radstag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Radstag not on the battlefield: %+v", o)
	}
	if got := e.G.Obj(radstag).Counter("P1P1"); got != 0 {
		t.Fatalf("Radstag starts with %d +1/+1 counters, want 0", got)
	}

	big := enterFromLibrary(t, e, 0, bigName)
	if e.Power(big) <= e.Power(radstag) && e.Toughness(big) <= e.Toughness(radstag) {
		t.Fatalf("precondition: %s (%d/%d) must be strictly bigger than the Radstag (%d/%d)",
			bigName, e.Power(big), e.Toughness(big), e.Power(radstag), e.Toughness(radstag))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	// The evolve counter really landed (the notification's own precondition).
	if got := e.G.Obj(radstag).Counter("P1P1"); got != 1 {
		t.Fatalf("Radstag +1/+1 counters = %d after a bigger creature entered, want 1", got)
	}
	// The Evolved marker named the evolving permanent exactly once.
	if n := evolvedMarkerCount(e, radstag); n != 1 {
		t.Fatalf("events.Evolved naming the Radstag = %d, want 1", n)
	}
	// The sibling "whenever this creature evolves" ability fired and made a
	// token copy of the Radstag.
	findTokenCopyOf(t, e, byName[radstagName], radstag)
	noUnimplementedAPI(t, e)
	replayCheck(t, e, cfg)
}

// TestEvolvedDoesNotFireForAnEqualOrSmallerCreature is the negative half: the
// evolve keyword's own CR 702.99a gate (strictly greater power or toughness)
// also gates the notification, so a same-size creature must place no counter,
// emit no Evolved marker and create no token copy.
func TestEvolvedDoesNotFireForAnEqualOrSmallerCreature(t *testing.T) {
	t.Parallel()
	const radstagName, equalName = "Watchful Radstag", "Grizzly Bears"
	e, _, byName := evolvedGame(t, 44, radstagName, equalName)

	radstag := enterFromLibrary(t, e, 0, radstagName)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(radstag); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Radstag not on the battlefield: %+v", o)
	}
	if got := e.G.Obj(radstag).Counter("P1P1"); got != 0 {
		t.Fatalf("Radstag starts with %d +1/+1 counters, want 0", got)
	}

	equal := enterFromLibrary(t, e, 0, equalName)
	if e.Power(equal) > e.Power(radstag) || e.Toughness(equal) > e.Toughness(radstag) {
		t.Fatalf("precondition: %s (%d/%d) must not exceed the Radstag (%d/%d)",
			equalName, e.Power(equal), e.Toughness(equal), e.Power(radstag), e.Toughness(radstag))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(radstag).Counter("P1P1"); got != 0 {
		t.Fatalf("Radstag evolved for an equal creature: %d counters, want 0", got)
	}
	if n := evolvedMarkerCount(e, radstag); n != 0 {
		t.Fatalf("events.Evolved = %d for a non-evolving entry, want 0", n)
	}
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.IsToken && o.Card == byName[radstagName] {
			t.Fatal("a token copy was created although the Radstag never evolved")
		}
	}
	noUnimplementedAPI(t, e)
}

// TestRenegadeKrasisGrowsEachOtherCounterBearerWhenItEvolves drives the
// second carrier's own Execute body: when the Krasis evolves, its sister
// "put a +1/+1 counter on each other creature you control with a +1/+1
// counter on it" (api:PutCounterAll) runs. The Bears starts with one +1/+1
// counter, so it must have two after the evolve; the Krasis itself
// (StrictlyOther) must have exactly its own evolve counter.
func TestRenegadeKrasisGrowsEachOtherCounterBearerWhenItEvolves(t *testing.T) {
	t.Parallel()
	const krasisName, bearName, bigName = "Renegade Krasis", "Grizzly Bears", "Craw Wurm"
	e, _, _ := evolvedGame(t, 45, krasisName, bearName, bigName)

	krasis := enterFromLibrary(t, e, 0, krasisName)
	bear := enterFromLibrary(t, e, 0, bearName)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Bears not on the battlefield: %+v", o)
	}
	// Seed the precondition the Krasis's own trigger filter reads: the Bears
	// must carry at least one +1/+1 counter (counters_GE1_P1P1).
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition: Bears +1/+1 counters = %d, want 1", got)
	}

	big := enterFromLibrary(t, e, 0, bigName)
	if e.Power(big) <= e.Power(krasis) && e.Toughness(big) <= e.Toughness(krasis) {
		t.Fatalf("precondition: %s must be strictly bigger than the Krasis (%d/%d)",
			bigName, e.Power(krasis), e.Toughness(krasis))
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(krasis).Counter("P1P1"); got != 1 {
		t.Fatalf("Krasis +1/+1 counters = %d after evolving, want 1", got)
	}
	if n := evolvedMarkerCount(e, krasis); n != 1 {
		t.Fatalf("events.Evolved naming the Krasis = %d, want 1", n)
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Fatalf("counter-bearing Bears +1/+1 counters = %d after the Krasis evolved, want 2", got)
	}
	noUnimplementedAPI(t, e)
}
