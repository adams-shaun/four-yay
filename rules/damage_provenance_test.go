package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestEmitRecordsGameLongDamageProvenance pins the ONE emission choke point
// (brief game-long damage-by-source provenance): every landed Damage event
// emitted through Engine.emit appends a DamageProvenance event naming the
// APPLIED recipient and the in-flight damage source, and events.Apply folds
// it into the game-long (recipient, source) membership record. A negative or
// zero amount (a cleanup hit, a prevented hit) emits nothing.
func TestEmitRecordsGameLongDamageProvenance(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	src := e.G.Zone(state.ZLibrary, 0)[0]
	// Precondition: the source object exists and the recipient seat is
	// undamaged before we start.
	if e.G.Obj(src) == nil {
		t.Fatal("precondition: no source object on the battlefield")
	}
	if len(e.G.Players[1].DamageTakenByGame) != 0 {
		t.Fatalf("precondition: seat 1 already has a game-long record (%v)", e.G.Players[1].DamageTakenByGame)
	}
	before := len(e.L.Events)
	e.damaging = src
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})

	var prov []events.Event
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.DamageProvenance {
			prov = append(prov, ev)
		}
	}
	if len(prov) != 1 {
		t.Fatalf("want exactly one DamageProvenance event for one landed hit, got %d (%v)", len(prov), prov)
	}
	if prov[0].Obj != src || prov[0].Amount != 3 {
		t.Fatalf("provenance names source/amount %d/%d, want %d/3", prov[0].Obj, prov[0].Amount, src)
	}
	if len(prov[0].IDs) != 1 {
		t.Fatalf("provenance must carry one recipient id, got %v", prov[0].IDs)
	}
	if p, isPlayer := prov[0].IDs[0].PlayerRef(); !isPlayer || p != 1 {
		t.Fatalf("provenance recipient must be seat 1, got %v (isPlayer=%v)", prov[0].IDs[0], isPlayer)
	}
	rec := e.G.Players[1].DamageTakenByGame
	if len(rec) != 1 || rec[0] != src {
		t.Fatalf("seat 1's game-long record = %v, want [%d]", rec, src)
	}
	// A second hit from the same source DEDUPS the record but still logs a
	// provenance event.
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	if rec := e.G.Players[1].DamageTakenByGame; len(rec) != 1 {
		t.Fatalf("the record must dedup a repeated source, got %v", rec)
	}
	// A non-positive amount emits no provenance (the cleanup negative).
	before = len(e.L.Events)
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: -2})
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.DamageProvenance {
			t.Fatalf("a non-positive Damage must emit no provenance, got %+v", ev)
		}
	}
}

// TestEmitRecordsObjectDamageProvenance pins the object-recipient half:
// damage dealt to a permanent records the source on the OBJECT's game-long
// record (the record The Fallen's ValidCards$ Planeswalker half reads).
func TestEmitRecordsObjectDamageProvenance(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	src := e.G.Zone(state.ZLibrary, 0)[0]
	victim := e.G.Zone(state.ZLibrary, 1)[0]
	// Put the victim on the battlefield so the damage is dealt to a real
	// permanent (the planeswalker/creature-recipient shape the record reads).
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZLibrary, To: state.ZBattlefield})
	if e.G.Obj(victim).Zone != state.ZBattlefield {
		t.Fatal("precondition: the victim did not reach the battlefield")
	}
	if src == victim {
		t.Fatal("precondition: source and victim are the same object")
	}
	if len(e.G.Obj(victim).DamageTakenByGame) != 0 {
		t.Fatalf("precondition: victim already has a record (%v)", e.G.Obj(victim).DamageTakenByGame)
	}
	e.damaging = src
	e.emit(events.Event{Kind: events.Damage, Obj: victim, Amount: 1})
	rec := e.G.Obj(victim).DamageTakenByGame
	if len(rec) != 1 || rec[0] != src {
		t.Fatalf("victim's game-long record = %v, want [%d]", rec, src)
	}
	// The recipient seat is untouched: provenance is per-recipient.
	if len(e.G.Players[1].DamageTakenByGame) != 0 {
		t.Fatalf("object damage must not write the player record, got %v", e.G.Players[1].DamageTakenByGame)
	}
}

// TestDiseasedVerminAskOffersOnlyPreviouslyDamagedOpponents drives the
// player-side qualifier end to end through the offer: a ValidTgts$
// Opponent.wasDealtDamageThisGameBy Self ask must offer only the opponent the
// source has damaged this game. The disqualifying opponent (never damaged)
// must not be offered, and the controller's own seat is excluded by the
// Opponent base as always.
func TestDiseasedVerminAskOffersOnlyPreviouslyDamagedOpponents(t *testing.T) {
	t.Parallel()
	// Precondition on the corpus: Diseased Vermin's real spec is the shape
	// this test drives (through an equivalent GainControl card so the ask is
	// reachable in the ETB harness).
	reg := testutil.CorpusRegistry(t)
	if dv, ok := reg.Lookup("Diseased Vermin"); ok {
		spec := dv.Faces[0].SVars["DBDisease"]
		if !strings.Contains(spec, "ValidTgts$ Opponent.wasDealtDamageThisGameBy Self") {
			t.Fatalf("corpus moved: Diseased Vermin's DBDisease no longer carries the qualifier: %q", spec)
		}
	}
	e, src := provTriggerEngine(t, 3, func(g *state.Game, source state.ObjID) {
		g.Players[1].DamageTakenByGame = append(g.Players[1].DamageTakenByGame, source)
	})
	_ = src
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("wanted the player-target ask, got %+v", d)
	}
	got := playerTargetOptions(t, d)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("ask offered players %v, want exactly [1] (the previously damaged opponent)", got)
	}
	if indexOfPlayerOption(d, 2) >= 0 {
		t.Fatalf("the never-damaged opponent seat 2 is offered: %+v", d.Options)
	}
	if indexOfPlayerOption(d, 0) >= 0 {
		t.Fatalf("the controller seat 0 is offered: %+v", d.Options)
	}
}

// provTriggerEngine is etbTriggerEngine with a seam: record runs after the
// trigger creature is on the battlefield (so its id is known) but before
// Advance fires its ETB, letting a test seed the game-long damage record the
// ask's qualifier reads. The trigger card's ValidTgts$ is the qualifier under
// test; GainControl makes the ask reachable in the ETB harness.
func provTriggerEngine(t *testing.T, n int, record func(g *state.Game, source state.ObjID)) (*Engine, state.ObjID) {
	t.Helper()
	names := make([]string, n)
	decks := make([][]*cards.Card, n)
	for i := range names {
		names[i] = string(rune('a' + i))
		decks[i] = mountainDeck(t, 40)
	}
	trig := etbGainControlCard(t, "ProvAgent", "Opponent.wasDealtDamageThisGameBy Self")
	decks[0] = append([]*cards.Card{trig}, decks[0]...)
	e := New(seatZeroStart(Config{Seed: 723, Names: names, Decks: decks}))
	obj := e.G.AddObject(trig, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: obj.ID, From: state.ZLibrary, To: state.ZBattlefield})
	record(e.G, obj.ID)
	e.Advance()
	return e, obj.ID
}
