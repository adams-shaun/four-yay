package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// optionalRevealEngine builds a seat-0-start 3-seat engine, gives seat 0 a
// zero-cost may-reveal spell, and puts one card named name in the hand of
// each opponent, plus one card in seat 0's own hand so the spell has a card
// to discard into the graveyard when cast. It returns the engine and the
// spell's object id.
func optionalRevealEngine(t *testing.T, line string, oppNames ...string) (*Engine, state.ObjID) {
	t.Helper()
	names := []string{"a", "b", "c"}
	decks := [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}
	e := New(seatZeroStart(Config{Seed: 1, Names: names, Decks: decks}))
	c := card(t, "Name:MayReveal\nManaCost:0\nTypes:Sorcery\nA:"+line+"\nOracle:x\n")
	o := e.G.AddObject(c, state.PlayerID(0))
	o.Zone = state.ZHand
	e.G.Clock++
	o.Timestamp = e.G.Clock
	// Seat 0 needs its own hand card so it can cast (the spell itself is the
	// hand's only other card; Effortless is not required, but a spell cast
	// out of a hand of one still needs the card in the hand set).
	for i, name := range oppNames {
		pc := card(t, "Name:"+name+"\nTypes:Creature\nPT:1/1\nOracle:x\n")
		po := e.G.AddObject(pc, state.PlayerID(i+1))
		po.Zone = state.ZHand
		e.G.Clock++
		po.Timestamp = e.G.Clock
		e.G.SetZone(state.ZHand, state.PlayerID(i+1), []state.ObjID{po.ID})
	}
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	return e, o.ID
}

// castAndReachOptionalRevealAsk drives the cast until the pending decision is
// the may-reveal yes/no (the reveal_optional ask).
func castAndReachOptionalRevealAsk(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
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

// TestOptionalRevealPerTargetAskThroughTheEngine drives the whole engine path
// for a multi-target optional reveal: the rules resume arm must attribute
// seat 1's answer to target 0 while seat 2 is asked its OWN question, not
// silently answered by the first player's choice. The effects-level halves
// are pinned in effects/reveal_optional_multitarget_test.go; this is the
// engine end to end (suspend via e.resume, resume via the reveal_optional
// arm setting Ctx.RevealOptTarget).
func TestOptionalRevealPerTargetAskThroughTheEngine(t *testing.T) {
	t.Parallel()
	e, id := optionalRevealEngine(t, "SP$ RevealHand | Defined$ Player.Opponent | Optional$ True", "Seat2Card", "Seat3Card")

	// Pass 1: target 0 is seat 1 (Defined$ Player.Opponent is seat order).
	d := castAndReachOptionalRevealAsk(t, e, id)
	if d.Player != 1 || d.ResumeTarget != 0 {
		t.Fatalf("pass 1 ask = Player %d ResumeTarget %d, want seat 1 / target 0", d.Player, d.ResumeTarget)
	}
	if len(mayRevealNotes(e)) != 0 {
		t.Fatalf("notes before the answer: %+v", mayRevealNotes(e))
	}

	// Answer DECLINE (option 1). Seat 2 must then be asked its own question;
	// the pre-fix bug applied the "no" to seat 2 and finished the resolution.
	submitChoices(t, e, 1)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "reveal_optional" {
		t.Fatalf("after seat 1 declined pending = %+v, want seat 2's own reveal_optional ask", d)
	}
	if d.Player != 2 || d.ResumeTarget != 1 {
		t.Fatalf("pass 2 ask = Player %d ResumeTarget %d, want seat 2 / target 1", d.Player, d.ResumeTarget)
	}
	if len(mayRevealNotes(e)) != 0 {
		t.Fatalf("a declined target's answer leaked a note: %+v", mayRevealNotes(e))
	}

	// Answer ACCEPT (option 0) for target 1. This is the half that proves the
	// rules resume arm attributes the answer to target 1: without
	// ctx.RevealOptTarget = rp.target the answer would still carry target 0,
	// so target 1 would re-pose its ask instead of revealing and the
	// resolution would never complete.
	seat2Card := e.G.Zone(state.ZHand, 2)
	if len(seat2Card) != 1 {
		t.Fatalf("precondition: seat 2's hand = %v, want its one card", seat2Card)
	}
	submitChoices(t, e, 0)
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("the walk re-posed an ask after every target was answered: %+v", d)
	}
	notes := mayRevealNotes(e)
	if len(notes) != 1 {
		t.Fatalf("reveal notes = %+v, want exactly seat 2's", notes)
	}
	if notes[0].Player != 2 || len(notes[0].IDs) != 1 || notes[0].IDs[0] != seat2Card[0] {
		t.Fatalf("reveal note = %+v, want seat 2 revealing %d", notes[0], seat2Card[0])
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("spell zone = %v, want the graveyard after the reveal resolved", e.G.Obj(id).Zone)
	}
}
