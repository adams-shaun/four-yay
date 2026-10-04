package rules

// Batch-0 test-restoration helpers (prefix kr0): the W3 legacy removal
// deleted effects-package tests that drove the suspend/resume ask protocol
// through fake hosts. Their behaviour is restored here against a REAL engine:
// a synthetic SA resolves under a kernel probe (e.probe), the ask it poses is
// read off e.Pending, and the answering Submit re-executes the resolution
// from its checkpoint with the answer served by the tape.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr0Engine is a seats-player engine (all-Mountain decks) driven to seat 0's
// first main phase, ready for eventless fixture placement.
func kr0Engine(t *testing.T, seats int) *Engine {
	t.Helper()
	return kr0EngineTokens(t, seats, map[string]*cards.Card{})
}

// kr0EngineCorpus is kr0Engine with the corpus token scripts available.
func kr0EngineCorpus(t *testing.T, seats int) *Engine {
	t.Helper()
	return kr0EngineTokens(t, seats, searchTestRegistry(t).Tokens)
}

func kr0EngineTokens(t *testing.T, seats int, tokens map[string]*cards.Card) *Engine {
	t.Helper()
	names := []string{"a", "b", "c", "d"}[:seats]
	decks := make([][]*cards.Card, seats)
	for i := range decks {
		decks[i] = mountainDeck(t, 40)
	}
	e := New(seatZeroStart(Config{Seed: 1, Names: names, Decks: decks, Tokens: tokens}))
	e.Advance()
	toMain1(t, e)
	return e
}

// kr0SA parses one ability line ("SP$ ..." / "DB$ ...") into a linked SA.
func kr0SA(t *testing.T, line string, svars ...string) *cards.SA {
	t.Helper()
	src := "Name:T\nTypes:Sorcery\nA:" + line + "\n"
	for _, s := range svars {
		src += s + "\n"
	}
	return card(t, src+"Oracle:x\n").Faces[0].Abilities[0]
}

// kr0Place adds a card eventlessly to p's zone (appended at the end; for a
// library that is the bottom).
func kr0Place(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, z state.Zone) state.ObjID {
	t.Helper()
	if z == state.ZBattlefield {
		return onBoardCard(t, e, p, c)
	}
	o := e.G.AddObject(c, p)
	o.Zone = z
	e.G.SetZone(z, p, append(e.G.Zone(z, p), o.ID))
	e.staticEpoch = -1
	e.activeEpoch = -1
	return o.ID
}

// kr0Src is kr0Place for a script string.
func kr0Src(t *testing.T, e *Engine, p state.PlayerID, src string, z state.Zone) state.ObjID {
	t.Helper()
	return kr0Place(t, e, p, card(t, src), z)
}

// kr0Library replaces p's library with n fresh copies of src (top first).
func kr0Library(t *testing.T, e *Engine, p state.PlayerID, src string, n int) []state.ObjID {
	t.Helper()
	c := card(t, src)
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		e.G.Obj(id).Zone = state.ZExile
		e.G.SetZone(state.ZExile, p, append(e.G.Zone(state.ZExile, p), id))
	}
	ids := make([]state.ObjID, 0, n)
	for i := 0; i < n; i++ {
		o := e.G.AddObject(c, p)
		o.Zone = state.ZLibrary
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZLibrary, p, ids)
	return ids
}

// kr0Run resolves sa under a kernel probe with a fresh Ctx from mk on every
// (re-)execution. It returns the posed decision, or nil when the resolution
// completed without asking. *last (when non-nil) receives the Ctx of the
// latest execution, so a test can read what the resolution left on it.
func kr0Run(t *testing.T, e *Engine, sa *cards.SA, mk func() *effects.Ctx, last **effects.Ctx) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.probe(func() {
		c := mk()
		if last != nil {
			*last = c
		}
		effects.Resolve(e, c, sa)
	})
	return e.Pending()
}

// kr0Answer submits choices to the pending decision and returns whatever is
// pending afterwards (nil once the probed resolution completed).
func kr0Answer(t *testing.T, e *Engine, choices ...int) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("kr0Answer: no decision pending")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit %v to %s/%s: %v", choices, d.Kind, d.ResumeKind, err)
	}
	return e.Pending()
}

// kr0Opt returns the index of d's option carrying obj, failing when absent.
func kr0Opt(t *testing.T, d *decision.Decision, obj state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("no option for object %d in %+v", obj, d.Options)
	return -1
}

// kr0Kind returns the index of d's option of the given Kind, failing when absent.
func kr0Kind(t *testing.T, d *decision.Decision, kind string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	t.Fatalf("no %q option in %+v", kind, d.Options)
	return -1
}

// kr0Label returns the index of d's option labelled label, failing when absent.
func kr0Label(t *testing.T, d *decision.Decision, label string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Label == label {
			return o.Index
		}
	}
	t.Fatalf("no option labelled %q in %+v", label, d.Options)
	return -1
}

// kr0Since returns the events logged from index start on.
func kr0Since(e *Engine, start int) []events.Event {
	return append([]events.Event(nil), e.L.Events[start:]...)
}

// kr0Count counts the events of kind in evs.
func kr0Count(evs []events.Event, kind events.Kind) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// kr0Corpus looks a corpus card up by name (skipping without .cards).
func kr0Corpus(t *testing.T, name string) *cards.Card {
	t.Helper()
	return searchCorpusCard(t, searchTestRegistry(t), name)
}

// kr0SVar resolves a corpus card's named SVar into its SA.
func kr0SVar(t *testing.T, c *cards.Card, name string) *cards.SA {
	t.Helper()
	sa := cards.ResolveSVar(c.Faces[0].SVars, name)
	if sa == nil {
		t.Fatalf("%s has no compiled %s", c.Faces[0].Name, name)
	}
	return sa
}

// kr0SetHand replaces p's hand with fresh cards from srcs (in order), the
// previous hand set aside in exile.
func kr0SetHand(t *testing.T, e *Engine, p state.PlayerID, srcs ...string) []state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		e.G.Obj(id).Zone = state.ZExile
		e.G.SetZone(state.ZExile, p, append(e.G.Zone(state.ZExile, p), id))
	}
	e.G.SetZone(state.ZHand, p, nil)
	ids := make([]state.ObjID, 0, len(srcs))
	for _, s := range srcs {
		ids = append(ids, kr0Src(t, e, p, s, state.ZHand))
	}
	return ids
}

// kr0Creature is a vanilla 1/1 creature script named name.
func kr0Creature(name string) string {
	return "Name:" + name + "\nTypes:Creature\nPT:1/1\nOracle:x\n"
}

// kr0In reports whether id sits in p's zone z.
func kr0In(e *Engine, z state.Zone, p state.PlayerID, id state.ObjID) bool {
	for _, v := range e.G.Zone(z, p) {
		if v == id {
			return true
		}
	}
	return false
}

// kr0SetLibrary replaces p's library with fresh cards from srcs (top first),
// the previous library set aside in exile.
func kr0SetLibrary(t *testing.T, e *Engine, p state.PlayerID, srcs ...string) []state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		e.G.Obj(id).Zone = state.ZExile
		e.G.SetZone(state.ZExile, p, append(e.G.Zone(state.ZExile, p), id))
	}
	e.G.SetZone(state.ZLibrary, p, nil)
	ids := make([]state.ObjID, 0, len(srcs))
	for _, s := range srcs {
		ids = append(ids, kr0Src(t, e, p, s, state.ZLibrary))
	}
	return ids
}

// kr0OfferSet asserts d offers exactly want's objects, in order.
func kr0OfferSet(t *testing.T, d *decision.Decision, want ...state.ObjID) {
	t.Helper()
	got := make([]state.ObjID, 0, len(d.Options))
	for _, o := range d.Options {
		got = append(got, o.Obj)
	}
	if len(got) != len(want) {
		t.Fatalf("options = %v, want exactly %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("options = %v, want %v (slot %d differs)", got, want, i)
		}
	}
}

// kr0Zone asserts id sits in zone z.
func kr0Zone(t *testing.T, e *Engine, id state.ObjID, z state.Zone) {
	t.Helper()
	if got := e.G.Obj(id).Zone; got != z {
		t.Fatalf("object %d (%s) in %v, want %v", id, e.G.Obj(id).Face().Name, got, z)
	}
}
