package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins Battles (CR 310) end to end on real corpus cards: a Battle
// Siege enters with defense counters equal to its printed Defense (CR
// 310.6/310.8, the entry grant in events.Apply's Move), a Battle Siege
// entering poses the CR 310.10 protector choice and records the chosen
// opponent through a Choose "protector" event. The zero-defense defeat
// (exile by the SBA plus the CR 310.11 transformed-cast offer) is pinned in
// rules/battle_defeated_test.go.
//
// Invasion of Tolvada is the canonical carrier (Defense:5, Types:Battle
// Siege); it is in no repo deck, so these games never touch the golden heads.

// battleBoard seeds seat 0 with the named corpus Battle, puts it onto the
// battlefield through a LOGGED MoveZone event (so the entry grant runs for
// real), answers the CR 310.10 protector ask, and stops at turn 2 seat 0
// Main1. It returns the engine, config, the battle's object id and the id of
// the entry MoveZone event.
func battleBoard(t *testing.T, reg *cards.Registry, name string) (*Engine, Config, state.ObjID) {
	t.Helper()
	battle := mustCorpusCard(t, reg, name)
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
		t.Fatalf("no %q copy in seat 0's deck", name)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	answerProtector(t, e)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg, id
}

// answerProtector submits option 0 on the pending CR 310.10 protector ask.
func answerProtector(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no protector ask pending after battle entry, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit protector choice: %v", err)
	}
}

// TestBattleEntersWithPrintedDefense is the reported defect: a Battle Siege
// must enter with Counter("DEFENSE") equal to its printed Defense. Invasion
// of Tolvada prints Defense:5.
func TestBattleEntersWithPrintedDefense(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, id := battleBoard(t, reg, "Invasion of Tolvada")
	o := e.G.Obj(id)
	if o == nil {
		t.Fatal("battle object missing")
	}
	if got := o.Counter("DEFENSE"); got != 5 {
		t.Fatalf("battle entered with %d defense counters, want 5", got)
	}
	if f := o.Face(); f == nil || !f.IsBattle() {
		t.Fatal("battle face is not recognised as a Battle")
	}
}

// TestSiegeEntryPosesProtectorChoice pins CR 310.10: a Battle Siege entering
// poses a real choice of opponent to protect it, and the answer is recorded
// through a Choose "protector" event (never a direct field write).
func TestSiegeEntryPosesProtectorChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
	deck := append([]*cards.Card{battle}, mountainDeck(t, 39)...)
	cfg := Config{Seed: 31, Names: []string{"a", "b", "c", "d"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
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
		t.Fatal("battle copy missing")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no protector ask posed on Siege entry, got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("protector ask went to seat %d, want the battle's controller seat 0", d.Player)
	}
	if len(d.Options) != 3 {
		t.Fatalf("protector ask offered %d options, want 3 opponents", len(d.Options))
	}
	for _, opt := range d.Options {
		if opt.Player == 0 {
			t.Fatalf("protector option names the controller seat 0: %+v", opt)
		}
	}
	// Answer with the second option so the recorded protector is a specific
	// non-first opponent, proving the answer (not a fixed default) is stored.
	want := d.Options[1].Player
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatalf("submit protector choice: %v", err)
	}
	o := e.G.Obj(id)
	if !o.ProtectorValid {
		t.Fatal("protector was not recorded on the battle")
	}
	if o.Protector != want {
		t.Fatalf("recorded protector seat %d, want %d (the answered option)", o.Protector, want)
	}
	// The record is an event, not a direct write: the log carries the Choose.
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == id && ev.Counter == "protector" {
			found = true
			if ev.Player != want {
				t.Fatalf("Choose protector event names seat %d, want %d", ev.Player, want)
			}
		}
	}
	if !found {
		t.Fatal("no Choose \"protector\" event in the log")
	}
}

// TestBattleFaceDownEntryGrantsNothingAndDoesNotPark is the manifest/cloak
// boundary at the rules level: a face-down Battle entry (CR 708.5) is a
// vanilla 2/2 creature, so the CR 310.10 protector ask must not be posed for
// it (applySiegeProtector reads the entry event's face-down marker, since
// the FaceDown state is only folded by Apply's Move after the replacement
// dispatch runs), and the CR 704.5h zero-defense SBA must not sweep it (it
// has no defense counters by construction). Pins both face-down guards that
// a manifested or cloaked Battle reaches in real play, over BOTH battlefield
// face-down entry markers -- the manifest/FaceDown$ one and Cloak's, which
// applySiegeProtector reaches through the shared events.IsFaceDownEntry.
func TestBattleFaceDownEntryGrantsNothingAndDoesNotPark(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, counter string }{
		{"manifest", events.FaceDownEntryCounter},
		{"cloak", events.CloakEntryCounter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
			deck := append([]*cards.Card{battle}, mountainDeck(t, 39)...)
			cfg := Config{Seed: 31, Names: []string{"a", "b", "c", "d"},
				Decks: [][]*cards.Card{deck, mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
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
				t.Fatal("battle copy missing")
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand,
				To: state.ZBattlefield, Counter: tc.counter})
			if d := e.Pending(); d != nil {
				t.Fatalf("face-down battle entry parked on a decision, want none: %+v", d)
			}
			o := e.G.Obj(id)
			if !o.FaceDown {
				t.Fatal("face-down entry did not fold FaceDown")
			}
			if got := o.Counter("DEFENSE"); got != 0 {
				t.Fatalf("face-down battle entered with %d defense counters, want 0", got)
			}
			if o.ProtectorValid {
				t.Fatalf("face-down battle recorded a protector (seat %d)", o.Protector)
			}
			e.checkStateBased()
			if o.Zone != state.ZBattlefield {
				t.Fatalf("face-down battle was swept to %v by the zero-defense SBA", o.Zone)
			}
		})
	}
}

// TestBattleFaceDownEntryEmitsNoProtectorChoose is the transcript half of the
// same boundary, and it is seat-count-independent: in a TWO-player game the
// strict-supersets skip records the sole opponent through a Choose "protector"
// event without an ask, and that event is not Secret -- view.Describe renders
// the object's printed name -- so a face-down entry that reached the protector
// code at all would leak the hidden card's identity into the public
// transcript even though nothing parked. Both markers must emit nothing.
func TestBattleFaceDownEntryEmitsNoProtectorChoose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, counter string }{
		{"manifest", events.FaceDownEntryCounter},
		{"cloak", events.CloakEntryCounter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
			deck := append([]*cards.Card{battle}, mountainDeck(t, 39)...)
			cfg := seatZeroStart(Config{Seed: 31, Names: []string{"a", "b"},
				Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
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
				t.Fatal("battle copy missing")
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand,
				To: state.ZBattlefield, Counter: tc.counter})
			for _, ev := range e.L.Events {
				if ev.Kind == events.Choose && ev.Obj == id && ev.Counter == "protector" {
					t.Fatalf("face-down battle entry emitted a public Choose %q naming seat %d",
						ev.Counter, ev.Player)
				}
			}
			if o := e.G.Obj(id); o.ProtectorValid {
				t.Fatalf("face-down battle recorded a protector (seat %d)", o.Protector)
			}
		})
	}
}

// TestSiegeTwoPlayerRecordsSoleOpponentWithoutAsking pins the strict-supersets
// convention on the CR 310.10 ask: in a two-player game exactly one opponent
// is a legal protector, so no decision is posed and the sole opponent is
// recorded through the same Choose "protector" event.
func TestSiegeTwoPlayerRecordsSoleOpponentWithoutAsking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
	deck := append([]*cards.Card{battle}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 31, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
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
		t.Fatal("battle copy missing")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if d := e.Pending(); d != nil {
		t.Fatalf("two-player Siege posed a decision, want none: %+v", d)
	}
	o := e.G.Obj(id)
	if !o.ProtectorValid || o.Protector != 1 {
		t.Fatalf("sole opponent not recorded: valid=%v protector=%d, want true/1",
			o.ProtectorValid, o.Protector)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Obj == id && ev.Counter == "protector" {
			found = true
			if ev.Player != 1 {
				t.Fatalf("Choose protector event names seat %d, want 1", ev.Player)
			}
		}
	}
	if !found {
		t.Fatal("no Choose \"protector\" event in the log")
	}
}

// TestBattlePathReplaysExactly proves the entry grant, the protector choice
// and the zero-defense SBA are all re-derived from the event stream alone:
// folding the log's post-genesis events onto a genesis clone of the game
// reaches the exact state the live engine holds (the regeneration_test
// fold idiom). replayFor cannot be used here because the fixture drives the
// game with direct emits outside the intent flow, which an intent-only
// replay cannot reproduce; the fold proves the same property these fixtures
// can -- every mutation on the path is event-derived, never engine memory.
func TestBattlePathReplaysExactly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	battle := mustCorpusCard(t, reg, "Invasion of Tolvada")
	deck := append([]*cards.Card{battle}, mountainDeck(t, 40-1)...)
	cfg := Config{Seed: 31, Names: []string{"a", "b", "c", "d"},
		Decks: [][]*cards.Card{
			deck,
			mountainDeck(t, 40),
			mountainDeck(t, 40),
			mountainDeck(t, 40),
		}}
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
		t.Fatal("no Invasion of Tolvada copy in seat 0's deck")
	}
	replayed := e.G.Clone()
	start := len(e.L.Events)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	answerProtector(t, e)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "DEFENSE", Amount: -5})
	e.checkStateBased()
	for _, ev := range e.L.Events[start:] {
		events.Apply(replayed, ev)
	}
	if !reflect.DeepEqual(replayed, e.G) {
		t.Fatal("battle entry, protector choice and zero-defense SBA do not replay exactly from the events")
	}
	ro := replayed.Obj(id)
	if ro == nil || ro.Zone != state.ZExile || ro.Counter("DEFENSE") != 0 {
		t.Fatalf("replayed battle zone %v defense %d, want exile at 0", ro, ro.Counter("DEFENSE"))
	}
}
