package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The round-2 review's Optional$ fix (Liar's Pendulum's "you may reveal your
// hand" used to be a forced reveal), driven through the REAL engine: the
// may-reveal ask suspends the resolution, the decline reveals nothing, the
// acceptance emits the ordinary public reveal Note. The effects-level halves
// of this contract are pinned in effects/look_test.go; this file is the
// engine-level end to end (suspend via e.resume, resume via reveal_optional).

// mayRevealEngine builds a seat-0-start engine whose seat 0 holds one
// may-reveal spell (ManaCost 0, no targets) and one Bear to reveal.
func mayRevealEngine(t *testing.T, line string) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	c := card(t, "Name:MayReveal\nManaCost:0\nTypes:Sorcery\nA:"+line+"\nOracle:x\n")
	o := e.G.AddObject(c, state.PlayerID(0))
	o.Zone = state.ZHand
	e.G.Clock++
	o.Timestamp = e.G.Clock
	bearCard := card(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bear := e.G.AddObject(bearCard, state.PlayerID(0))
	bear.Zone = state.ZHand
	e.G.Clock++
	bear.Timestamp = e.G.Clock
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID, bear.ID})
	return e, o.ID
}

// castAndReachRevealAsk casts the spell directly (cr601's harness shape: the
// direct proposal bypasses the offer gate, which a poolless seat needs for
// even a {0} cost's offer path) and drives every intermediate decision until
// the may-reveal ask itself is pending.
func castAndReachRevealAsk(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()
	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision and no may-reveal ask")
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "reveal_optional" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected pending decision: %+v", d)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
				break
			}
		}
		if pass < 0 {
			t.Fatalf("no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatal("never reached the may-reveal ask")
	return nil
}

// revealNotes returns the non-Secret Notes carrying ids (the reveal Notes).
func mayRevealNotes(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			out = append(out, ev)
		}
	}
	return out
}

// TestMayRevealHandAskSuspendsAndDeclineRevealsNothing drives the decline
// arm: the ask suspends mid-resolution, "no" reveals nothing, and the
// resolution finishes (the game continues past it).
func TestMayRevealHandAskSuspendsAndDeclineRevealsNothing(t *testing.T) {
	t.Parallel()
	e, id := mayRevealEngine(t, "SP$ RevealHand | Defined$ You | Optional$ True")
	d := castAndReachRevealAsk(t, e, id)
	if d.Player != 0 || len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("ask = %+v, want a yes/no for the hand's owner", d)
	}
	if len(mayRevealNotes(e)) != 0 {
		t.Fatalf("notes before the answer: %+v", mayRevealNotes(e))
	}
	submitChoices(t, e, 1) // "no"
	if notes := mayRevealNotes(e); len(notes) != 0 {
		t.Fatalf("a declined may-reveal left reveal Notes: %+v", notes)
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("spell zone = %v, want the graveyard after a declined reveal", e.G.Obj(id).Zone)
	}
}

// TestMayRevealHandAcceptEmitsThePublicReveal drives the accept arm: "yes"
// emits exactly one public reveal Note naming the hand, and the
// project-attached decision reaches the deciding seat alone while it is
// pending (the delver peek's privacy property, on the hand shape).
func TestMayRevealHandAcceptEmitsThePublicReveal(t *testing.T) {
	t.Parallel()
	e, id := mayRevealEngine(t, "SP$ RevealHand | Defined$ You | Optional$ True")
	d := castAndReachRevealAsk(t, e, id)

	// While pending, only the deciding seat's view carries the decision.
	if v := view.Project(e.G, e, 0, d); v.Decision == nil {
		t.Fatal("the deciding seat's view carries no decision")
	}
	if v := view.Project(e.G, e, 1, d); v.Decision != nil {
		t.Fatal("the other seat's view carries the may-reveal ask")
	}

	bear := e.G.Zone(state.ZHand, 0)
	submitChoices(t, e, 0) // "yes"
	notes := mayRevealNotes(e)
	if len(notes) != 1 || len(notes[0].IDs) != len(bear) {
		t.Fatalf("reveal Notes = %+v, want exactly one naming the hand %v", notes, bear)
	}
	if notes[0].Secret {
		t.Fatal("an accepted hand reveal must be the public Note")
	}
	for i, want := range bear {
		if notes[0].IDs[i] != want {
			t.Fatalf("reveal ids = %v, want %v", notes[0].IDs, bear)
		}
	}
}
