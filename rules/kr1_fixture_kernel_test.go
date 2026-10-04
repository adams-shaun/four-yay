package rules

// Batch-1 test-restore fixture helpers (prefix kr1): a two-seat real engine
// with freely-authored fixture cards in BOTH seats' decks, plus the setup
// moves the restored kernel-era tests share (stack a library top, put a
// named card into a zone, settle the stack answering the deterministic
// bottom-order asks). Every setup move is a logged event, so replayCheck
// rebuilds the game from the log alone.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr1New is newFixtureDeck with extra fixture cards for seat 1 too: seat 0's
// deck is fixtureSrc + extras0 + Mountains, seat 1's is extras1 + Mountains.
// It returns at seat 0's turn-1 upkeep priority with the fixture in hand.
func kr1New(t *testing.T, seed uint64, fixtureSrc string, extras0, extras1 []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	return kr1Build(t, seed, card(t, fixtureSrc), kr1Cards(t, extras0), kr1Cards(t, extras1))
}

// kr1Cards parses each fixture source.
func kr1Cards(t *testing.T, srcs []string) []*cards.Card {
	t.Helper()
	out := make([]*cards.Card, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, card(t, s))
	}
	return out
}

// kr1Build is kr1New over parsed cards and any number of seats (one extras
// slice per seat; seat 0's deck also holds fixture first).
func kr1Build(t *testing.T, seed uint64, fixture *cards.Card, extras ...[]*cards.Card) (*Engine, Config, state.ObjID) {
	t.Helper()
	name := fixture.Faces[0].Name
	names := []string{"a", "b", "c", "d", "e", "f"}[:len(extras)]
	build := func(s uint64) Config {
		decks := make([][]*cards.Card, len(extras))
		for i, ex := range extras {
			d := append([]*cards.Card(nil), ex...)
			if i == 0 {
				d = append([]*cards.Card{fixture}, d...)
			}
			decks[i] = append(d, mountainDeck(t, 40-len(d))...)
		}
		return Config{Seed: s, Names: names, Decks: decks, Tokens: map[string]*cards.Card{}}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	id := kr1Find(e, 0, name, state.ZHand)
	if id == 0 {
		id = kr1Find(e, 0, name, state.ZLibrary)
		if id == 0 {
			t.Fatalf("fixture %q not found in seat 0's hand or library", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	return e, cfg, id
}

// kr1Find is the first object named name in seat p's zone z, or 0.
func kr1Find(e *Engine, p state.PlayerID, name string, z state.Zone) state.ObjID {
	for _, id := range e.G.Zone(z, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// kr1Top lays seat p's library out with one card per name on top, in the
// given order (a name may repeat). A named card found only in the hand is
// first returned to the library with a logged MoveZone. The rest of the
// library keeps its relative order beneath. It returns the stacked ids.
func kr1Top(t *testing.T, e *Engine, p state.PlayerID, names ...string) []state.ObjID {
	t.Helper()
	used := map[state.ObjID]bool{}
	top := make([]state.ObjID, 0, len(names))
	for _, n := range names {
		var got state.ObjID
		for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
			for _, id := range e.G.Zone(z, p) {
				if used[id] {
					continue
				}
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == n {
					got = id
					if z == state.ZHand {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary, Player: p})
					}
					break
				}
			}
			if got != 0 {
				break
			}
		}
		if got == 0 {
			t.Fatalf("kr1Top: no unused %q in seat %d's library or hand", n, p)
		}
		used[got] = true
		top = append(top, got)
	}
	order := append([]state.ObjID(nil), top...)
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		if !used[id] {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: p, IDs: order, Secret: true})
	return top
}

// kr1Put moves seat p's card named name (from its library, else its hand)
// to zone z with a logged MoveZone and returns it.
func kr1Put(t *testing.T, e *Engine, p state.PlayerID, name string, z state.Zone) state.ObjID {
	t.Helper()
	for _, from := range []state.Zone{state.ZLibrary, state.ZHand} {
		if id := kr1Find(e, p, name, from); id != 0 {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: z})
			return id
		}
	}
	t.Fatalf("kr1Put: %q not in seat %d's library or hand", name, p)
	return 0
}

// kr1Settle passes priority and answers every KArrange (a dig/remainder
// bottom order) in its offered order until either the stack is empty
// (returns nil) or another non-priority decision is pending (returns it).
func kr1Settle(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 60 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return nil
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KArrange:
			ch := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				ch = append(ch, o.Index)
			}
			if d.Max >= 0 && d.Max < len(ch) {
				ch = ch[:d.Max]
			}
			submitChoices(t, e, ch...)
		default:
			return d
		}
	}
	if e.G.Over {
		return nil
	}
	t.Fatalf("kr1Settle did not settle (stack depth %d)", len(e.G.Stack))
	return nil
}

// kr1Answer submits choices to the pending decision (which must be of kind
// k and asked of seat p) and returns whatever is pending after settling.
func kr1Answer(t *testing.T, e *Engine, k decision.Kind, p state.PlayerID, choices ...int) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != k || d.Player != p {
		t.Fatalf("pending = %+v, want a %s asked of seat %d", d, k, p)
	}
	submitChoices(t, e, choices...)
	return kr1Settle(t, e)
}

// kr1OptIndex is the index of the option whose Obj is id, or fatal.
func kr1OptIndex(t *testing.T, d *decision.Decision, id state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("object %d not offered: %+v", id, d.Options)
	return -1
}

// kr1Zone is id's current zone.
func kr1Zone(e *Engine, id state.ObjID) state.Zone { return e.G.Obj(id).Zone }

// kr1HasNote reports whether any Note on the log contains substr.
func kr1HasNote(e *Engine, substr string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, substr) {
			return true
		}
	}
	return false
}

// kr1Cast casts seat 0's fixture id from the pending priority decision and
// settles (kr1Settle): it returns the first mid-resolution ask, or nil once
// the stack is empty with nothing asked.
func kr1Cast(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return kr1Settle(t, e)
		}
	}
	t.Fatalf("no cast option for %d: %+v", id, d.Options)
	return nil
}

// kr1Pick submits choices to whatever decision is pending (any kind, any
// seat) and returns whatever is pending after settling.
func kr1Pick(t *testing.T, e *Engine, choices ...int) *decision.Decision {
	t.Helper()
	submitChoices(t, e, choices...)
	return kr1Settle(t, e)
}

// kr1RelicName is the battlefield source kr1Relic builds.
const kr1RelicName = "Fixture Source"

// kr1Relic is an artifact whose one tap ability is "AB$ <body> | Cost$ T":
// a permanent source, so the source's persistent Remembered list survives
// the resolution for the test to read.
// Text after the first newline in body is appended as further script lines
// (the ability's SVars).
func kr1Relic(body string) string {
	line, rest, _ := strings.Cut(body, "\n")
	if rest != "" {
		rest += "\n"
	}
	return "Name:" + kr1RelicName + "\nManaCost:1\nTypes:Artifact\nA:AB$ " + line + " | Cost$ T\n" + rest + "Oracle:x\n"
}

// kr1Activate seeds src's persistent remembered set with remembered (a
// logged Choose "remembered"), activates src's first ability, seeds the
// ability object's remembered set the same way when activation did not
// already carry it, and settles: it returns the first mid-resolution ask,
// or nil once the stack is empty.
func kr1Activate(t *testing.T, e *Engine, src state.ObjID, remembered ...state.ObjID) *decision.Decision {
	t.Helper()
	if len(remembered) > 0 {
		e.emit(events.Event{Kind: events.Choose, Obj: src, Counter: "remembered", IDs: remembered})
	}
	toMain1(t, e)
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == src {
			idx = o.Index
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no ability option for %d: %+v", src, d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || len(e.G.Stack) == 0 {
		t.Fatalf("after activating: pending %+v, stack %v", d, e.G.Stack)
	}
	if top := e.G.Obj(e.G.Stack[len(e.G.Stack)-1]); len(remembered) > 0 && len(top.Remembered) == 0 {
		e.emit(events.Event{Kind: events.Choose, Obj: top.ID, Counter: "remembered", IDs: remembered})
	}
	return kr1Settle(t, e)
}

// kr1Remembered is obj's persistent remembered object ids.
func kr1Remembered(e *Engine, obj state.ObjID) []state.ObjID {
	var out []state.ObjID
	for _, tg := range e.G.Obj(obj).Remembered {
		if !tg.IsPlayer {
			out = append(out, tg.Obj)
		}
	}
	return out
}

// kr1CastTargeting casts seat 0's fixture id, answering each cast-time
// KTarget with the option for player p (isPlayer) or object obj, then
// settles like kr1Cast.
func kr1CastTargeting(t *testing.T, e *Engine, id state.ObjID, isPlayer bool, p state.PlayerID, obj state.ObjID) *decision.Decision {
	t.Helper()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	for i := 0; i < 4; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			break
		}
		ti := -1
		for _, o := range d.Options {
			if isPlayer && o.Kind == "player" && o.Player == p || !isPlayer && o.Obj == obj && obj != 0 {
				ti = o.Index
			}
		}
		if ti < 0 {
			t.Fatalf("wanted target not offered: %+v", d.Options)
		}
		submitChoices(t, e, ti)
	}
	return kr1Settle(t, e)
}
