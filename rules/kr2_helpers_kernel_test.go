package rules

// Shared fixtures for the batch-2 kernel-era restorations (the W3 legacy
// removal deleted the effects-package tests that drove the old
// suspend/resume protocol through fake hosts; these re-host their behaviour
// on a real engine, where every mid-resolution ask is posed by the
// resolution kernel and answered with Submit).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// kr2Engine builds a seats-player engine at seat 0's precombat main phase,
// every hand empty (the dealt cards are put back on the library so a hand
// count reads only what the test seats), with the corpus token table so
// Clue and other token scripts resolve.
func kr2Engine(t *testing.T, seats int) *Engine {
	t.Helper()
	return kr2EngineWith(t, seats, nil)
}

// kr2EngineWith is kr2Engine with a hook to adjust the Config first.
func kr2EngineWith(t *testing.T, seats int, mod func(*Config)) *Engine {
	t.Helper()
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := range names {
		names[i] = string(rune('a' + i))
		decks[i] = mountainDeck(t, 40)
	}
	cfg := Config{Seed: 1, Names: names, Decks: decks, Tokens: testutil.CorpusRegistry(t).Tokens}
	if mod != nil {
		mod(&cfg)
	}
	e := New(cfg)
	for p := state.PlayerID(0); int(p) < seats; p++ {
		hand := e.G.Zone(state.ZHand, p)
		lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, p)...)
		for _, id := range hand {
			e.G.Obj(id).Zone = state.ZLibrary
			lib = append(lib, id)
		}
		e.G.SetZone(state.ZHand, p, nil)
		e.G.SetZone(state.ZLibrary, p, lib)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	return e
}

// kr2Corpus returns the named corpus card.
func kr2Corpus(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	return c
}

// kr2Put adds c for owner p into zone z (appended; for the library, top
// puts it on top instead), eventlessly, and returns its id.
func kr2Put(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, z state.Zone, top bool) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = z
	cur := e.G.Zone(z, p)
	if top {
		e.G.SetZone(z, p, append([]state.ObjID{o.ID}, cur...))
	} else {
		e.G.SetZone(z, p, append(append([]state.ObjID(nil), cur...), o.ID))
	}
	if z == state.ZBattlefield {
		e.G.Clock++
		o.Timestamp = e.G.Clock
		e.staticEpoch = -1
		e.activeEpoch = -1
		e.typesEpoch = -1
		if e.layer4InPool {
			e.refreshDerivedTypes()
		}
	}
	return o.ID
}

// kr2Src parses an inline fixture script.
func kr2Src(t *testing.T, src string) *cards.Card { t.Helper(); return card(t, src) }

// kr2Sorcery is an inline sorcery named name whose script lines are body.
func kr2Sorcery(t *testing.T, name string, body ...string) *cards.Card {
	t.Helper()
	return card(t, "Name:"+name+"\nManaCost:B\nTypes:Sorcery\n"+strings.Join(body, "\n")+"\nOracle:x\n")
}

// kr2Fund fills p's pool with plenty of every colour.
func kr2Fund(e *Engine, p state.PlayerID) {
	for _, c := range []int{state.MW, state.MU, state.MB, state.MR, state.MG} {
		e.G.Players[p].Pool[c] = 10
	}
}

// kr2Priority re-poses p's priority decision over the current state.
func kr2Priority(e *Engine, p state.PlayerID) {
	e.pending = nil
	e.askPriority(p)
}

// kr2Cast funds seat p, gives it priority and casts id; it returns the next
// non-priority decision (cast-time or mid-resolution), or nil once the stack
// has emptied at a priority decision.
func kr2Cast(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) *decision.Decision {
	t.Helper()
	kr2Fund(e, p)
	kr2Priority(e, p)
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return kr2Next(t, e)
		}
	}
	t.Fatalf("no cast option for %d (%s): %+v", id, e.G.Obj(id).Face().Name, d.Options)
	return nil
}

// kr2Activate funds seat p and activates id's first offered ability.
func kr2Activate(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) *decision.Decision {
	t.Helper()
	kr2Fund(e, p)
	kr2Priority(e, p)
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return kr2Next(t, e)
		}
	}
	t.Fatalf("no ability option for %d: %+v", id, d.Options)
	return nil
}

// kr2Next passes priority until a non-priority decision is pending (and
// returns it) or the stack is empty at a priority decision (nil).
func kr2Next(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return nil
		}
		if d.Kind != decision.KPriority {
			return d
		}
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			return nil
		}
		passFirst(t, e)
	}
	t.Fatal("kr2Next: the stack never settled")
	return nil
}

// kr2Answer submits choices to the pending decision d and returns the next
// non-priority decision (nil once the stack is empty).
func kr2Answer(t *testing.T, e *Engine, d *decision.Decision, choices ...int) *decision.Decision {
	t.Helper()
	if cur := e.Pending(); cur == nil || cur.Seq != d.Seq {
		t.Fatalf("decision %+v is not the pending one (%+v)", d, cur)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit %v to %s/%s: %v", choices, d.Kind, d.ResumeKind, err)
	}
	return kr2Next(t, e)
}

// kr2Kind returns the index of d's option whose Kind is kind.
func kr2Kind(t *testing.T, d *decision.Decision, kind string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	t.Fatalf("no %q option in %+v", kind, d.Options)
	return -1
}

// kr2ObjIdx returns the index of d's option naming obj.
func kr2ObjIdx(t *testing.T, d *decision.Decision, obj state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("object %d not offered: %+v", obj, d.Options)
	return -1
}

// kr2PlayerIdx returns the index of d's option naming player p.
func kr2PlayerIdx(t *testing.T, d *decision.Decision, p state.PlayerID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == 0 && o.Player == p {
			return o.Index
		}
	}
	t.Fatalf("player %d not offered: %+v", p, d.Options)
	return -1
}

// kr2Want fails unless d is a decision with the given resume kind.
func kr2Want(t *testing.T, d *decision.Decision, resume string) *decision.Decision {
	t.Helper()
	if d == nil || d.ResumeKind != resume {
		t.Fatalf("decision = %+v, want a %q ask", d, resume)
	}
	return d
}

// kr2Events returns the events of kind k logged at or after from.
func kr2Events(e *Engine, from int, k events.Kind) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == k {
			out = append(out, ev)
		}
	}
	return out
}

// kr2SecretLooks returns the Secret look Notes carrying ids logged at or
// after from.
func kr2SecretLooks(e *Engine, from int) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Note && ev.Secret && len(ev.IDs) > 0 {
			out = append(out, ev)
		}
	}
	return out
}

// kr2PublicReveals returns the public Notes carrying ids logged at or after
// from.
func kr2PublicReveals(e *Engine, from int) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			out = append(out, ev)
		}
	}
	return out
}
