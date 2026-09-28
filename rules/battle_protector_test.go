package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file closes the battle-protector half of AGENTS.md's (battle1) row:
// the CR 310.10 protector ask now has a real botpolicy arm (botpolicy's own
// test), is re-derived when the recorded protector leaves the game, and is
// posed for every Battle rather than the Siege subtype alone. Attacking a
// battle and the zero-defense SBA are the sibling tickets' scope and are not
// touched here.

// battleProtectorDeck builds a four-player game whose seat 0 deck holds the
// given Battle card first, puts the battle onto the battlefield through a
// logged MoveZone event (so the entry grant and the CR 310.10 protector ask
// run for real), and returns the engine, the battle's object id and the
// pending protector ask. The entry is PARKED until the ask is answered --
// applySiegeProtector holds the MoveZone -- so the caller answers (or
// inspects) the ask before reading the battle's zone.
//
// It mirrors battle_test.go's battleBoard but takes a *cards.Card so a
// synthetic non-Siege Battle can be driven (no real corpus card is a
// non-Siege Battle: all 37 print the Siege subtype).
func battleProtectorDeck(t *testing.T, battle *cards.Card) (*Engine, state.ObjID, *decision.Decision) {
	t.Helper()
	deck := append([]*cards.Card{battle}, mountainDeck(t, 40-1)...)
	cfg := Config{Seed: 31, Names: []string{"a", "b", "c", "d"},
		Decks: [][]*cards.Card{
			deck,
			mountainDeck(t, 40),
			mountainDeck(t, 40),
			mountainDeck(t, 40),
		},
	}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	var id state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == battle {
			id = o.ID
			break
		}
	}
	if id == 0 {
		t.Fatalf("no battle copy in seat 0's deck")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	return e, id, e.Pending()
}

// answerBattleProtector answers the pending protector ask with the option at
// choice and returns the seat that option named.
func answerBattleProtector(t *testing.T, e *Engine, choice int) state.PlayerID {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no protector ask pending, got %+v", d)
	}
	if choice < 0 || choice >= len(d.Options) {
		t.Fatalf("protector choice %d out of range (options=%d)", choice, len(d.Options))
	}
	player := d.Options[choice].Player
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[choice].Index}}); err != nil {
		t.Fatalf("submit protector choice: %v", err)
	}
	return player
}

// TestSiegeBattleProtectorIsRechosenWhenProtectorLeaves pins the CR 310.10
// re-derive: a Battle whose recorded protector leaves the game gets a fresh
// living opponent, recorded through the same Choose "protector" event the
// entry ask uses, so the protector stays replay-derived. Invasion of Tolvada
// is the real corpus Siege carrier.
func TestSiegeBattleProtectorIsRechosenWhenProtectorLeaves(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
	e, id, ask := battleProtectorDeck(t, battle)
	if ask == nil {
		t.Fatal("precondition: entry ask was not posed")
	}
	protector := answerBattleProtector(t, e, 0)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().IsBattle() {
		t.Fatalf("precondition: battle must be on the battlefield after the ask; object=%+v", o)
	}
	if !o.ProtectorValid || o.Protector != protector {
		t.Fatalf("precondition: entry protector = valid:%v seat:%d, want true/%d",
			o.ProtectorValid, o.Protector, protector)
	}
	if e.G.Players[protector].Lost || e.G.Players[2].Lost {
		t.Fatal("precondition: the candidate opponents must be alive")
	}

	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlayerLost, Player: protector, Text: "test protector departure"})
	if !e.G.Players[protector].Lost {
		t.Fatal("precondition: PlayerLost did not mark the protector departed")
	}
	if !o.ProtectorValid || o.Protector != 2 {
		t.Fatalf("protector after departure = valid:%v seat:%d, want living opponent seat 2",
			o.ProtectorValid, o.Protector)
	}
	// The re-derive must land on the PlayerLost's own tail -- this is what
	// proves the hook runs from the departure rather than at some later step.
	tail := e.L.Events[start:]
	if len(tail) < 2 || tail[0].Kind != events.PlayerLost || tail[1].Kind != events.Choose {
		t.Fatalf("expected PlayerLost immediately followed by the protector Choose, got %v", tail)
	}
	if tail[1].Counter != "protector" || tail[1].Obj != id || tail[1].Player != 2 {
		t.Fatalf("reselection event = %+v, want Choose protector obj=%d player=2", tail[1], id)
	}
}

// TestBattleProtectorNotRechosenWhileProtectorLives guards the re-derive from
// firing on an unrelated player's departure: a battle whose protector is
// still alive must keep that protector and log no reselection.
func TestBattleProtectorNotRechosenWhileProtectorLives(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
	e, id, ask := battleProtectorDeck(t, battle)
	if ask == nil {
		t.Fatal("precondition: entry ask was not posed")
	}
	protector := answerBattleProtector(t, e, 0)

	o := e.G.Obj(id)
	if !o.ProtectorValid || o.Protector != protector {
		t.Fatalf("precondition: entry protector = valid:%v seat:%d, want true/%d",
			o.ProtectorValid, o.Protector, protector)
	}
	if e.G.Players[3].Lost {
		t.Fatal("precondition: seat 3 must be alive before the departure")
	}
	if protector == 3 {
		t.Fatal("precondition: seat 3 must not be the battle's protector")
	}

	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.PlayerLost, Player: 3, Text: "unrelated departure"})
	if !e.G.Players[3].Lost {
		t.Fatal("precondition: PlayerLost did not mark seat 3 departed")
	}
	if !o.ProtectorValid || o.Protector != protector {
		t.Fatalf("protector changed after an unrelated departure: valid:%v seat:%d, want true/%d",
			o.ProtectorValid, o.Protector, protector)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Choose && ev.Counter == "protector" {
			t.Fatalf("re-derived a protector after an unrelated departure: %+v", ev)
		}
	}
}

// TestNonSiegeBattlePosesProtectorAsk pins the generalisation: the protector
// ask is posed for every Battle, not only the Siege subtype. No real corpus
// Battle is non-Siege, so a synthetic Battle Campaign card drives it. The
// preconditions assert the card really parses as a non-Siege Battle (so the
// test cannot pass on a Siege or a non-battle), and the ask is what a parker
// only reaches for a real Battle entry.
func TestNonSiegeBattlePosesProtectorAsk(t *testing.T) {
	t.Parallel()
	battle := card(t, "Name:Test Campaign\nTypes:Battle Campaign\nDefense:3\nOracle:x\n")
	f := battle.Faces[0]
	if !f.IsBattle() {
		t.Fatalf("precondition: synthetic card is not a Battle; types=%v", f.Types)
	}
	for _, ty := range f.Types {
		if ty == "Siege" {
			t.Fatalf("precondition: synthetic card is a Siege, not a non-Siege Battle; types=%v", f.Types)
		}
	}

	_, _, d := battleProtectorDeck(t, battle)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no protector ask pending after a non-Siege Battle entered, got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("protector ask went to seat %d, want the battle's controller seat 0", d.Player)
	}
	if len(d.Options) != 3 {
		t.Fatalf("protector ask offered %d options, want 3 opponents", len(d.Options))
	}
	for _, opt := range d.Options {
		if opt.Kind != "protector" {
			t.Fatalf("ask option kind = %q, want protector", opt.Kind)
		}
		if opt.Player == 0 {
			t.Fatalf("ask offered the controller seat 0: %+v", opt)
		}
	}
}

// TestNonSiegeBattleProtectorIsRechosenWhenProtectorLeaves is the synthetic
// counterpart of the Siege re-derive test: a non-Siege Battle's protector is
// re-derived too.
func TestNonSiegeBattleProtectorIsRechosenWhenProtectorLeaves(t *testing.T) {
	t.Parallel()
	battle := card(t, "Name:Test Campaign\nTypes:Battle Campaign\nDefense:3\nOracle:x\n")
	e, id, ask := battleProtectorDeck(t, battle)
	if ask == nil || len(ask.Options) < 2 {
		t.Fatalf("precondition: need a protector ask with >=2 options, got %+v", ask)
	}
	// Answer with the second option so the recorded protector is not option 0.
	protector := answerBattleProtector(t, e, 1)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().IsBattle() {
		t.Fatalf("precondition: battle must be on the battlefield after the ask; object=%+v", o)
	}
	if got := o.Counter("DEFENSE"); got != 3 {
		t.Fatalf("precondition: battle entered with %d defense counters, want 3", got)
	}
	if !o.ProtectorValid || o.Protector != protector {
		t.Fatalf("precondition: recorded protector = valid:%v seat:%d, want true/%d",
			o.ProtectorValid, o.Protector, protector)
	}

	e.emit(events.Event{Kind: events.PlayerLost, Player: protector, Text: "test protector departure"})
	if !e.G.Players[protector].Lost {
		t.Fatal("precondition: PlayerLost did not mark the protector departed")
	}
	want := state.PlayerID(0)
	found := false
	for _, p := range e.G.AliveFrom(0) {
		if p != o.Controller {
			want = p
			found = true
			break
		}
	}
	if !found {
		t.Fatal("precondition: no living opponent remained")
	}
	if !o.ProtectorValid || o.Protector != want {
		t.Fatalf("protector after departure = valid:%v seat:%d, want living opponent seat %d",
			o.ProtectorValid, o.Protector, want)
	}
}
