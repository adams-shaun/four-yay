package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Temporal Anchor (task scrybottom) is the corpus's one carrier of
// "Whenever you choose to put one or more cards on the bottom of your
// library while scrying, exile that many cards from the bottom of your
// library." Its two halves are the scry trigger (`T:Mode$ Scry |
// ValidPlayer$ You | ToBottom$ True`, matched against the completed
// events.Scry record) and the amount (`SVar:X:TriggerCount$ScryBottom`,
// read by `DB$ Dig | DigNum$ X | FromBottom$ True`).
//
// The tests pin the two answers a scry can give against the REAL card body:
// keep every looked-at card on top -> the ToBottom$ True gate sees a
// zero-card bottom pile and fires nothing; put one or more on the bottom ->
// exactly one trigger, sized by the number bottomed (NOT the number looked
// at), exiling that many from the BOTTOM of the library. The measured
// corpus population at the pin is 1 file carrying `T:Mode$ Scry` with
// `ToBottom$ True` and 20 files carrying `T:Mode$ Scry`
// (`/usr/bin/grep -rlIE` over .cards/cardsfolder).

// scryMarkers returns every events.Scry marker in the log (the completed
// scry record the trigger matches).
func scryMarkers(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Scry {
			out = append(out, ev)
		}
	}
	return out
}

// libraryExiled returns the object ids the log moved from seat p's LIBRARY
// to exile (the Anchor's Dig is the only such mover here). It deliberately
// ignores the stack->exile removal of resolved ability objects, which the
// zone read cannot distinguish from a real exile.
func libraryExiled(e *Engine, p state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Player == p &&
			ev.From == state.ZLibrary && ev.To == state.ZExile {
			out = append(out, ev.Obj)
		}
	}
	return out
}

// anchorTriggerPushes counts the Anchor's own triggered abilities that
// reached the stack (TriggerPush, whose Obj is the pushed trigger's source
// permanent). It is the direct "did the trigger fire" observable, stronger
// than an effect trace: a ToBottom$ True trigger whose ScryBottom resolved
// to a zero-card Dig would move nothing yet still be a wrongful firing.
func anchorTriggerPushes(e *Engine, anchorID state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == anchorID {
			n++
		}
	}
	return n
}

// driveToScryArrange drives seat 0's real upkeep (the Anchor's Phase
// trigger) through to the KArrange the Scry 2 poses, answering every
// priority/combat/trigger-order ask on the way. It returns the pending
// arrange decision without answering it.
func driveToScryArrange(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		if e.G.Over {
			t.Fatalf("game ended before the scry arrange")
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending while driving to the scry arrange")
		}
		if d.Kind == decision.KArrange {
			return d
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KTriggerOrder:
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			submitChoices(t, e, choices...)
		case decision.KChoose:
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected decision %+v while driving to the scry arrange", d)
		}
	}
	t.Fatalf("no scry KArrange within %d decisions", limit)
	return nil
}

// anchorOnBattlefield builds a 2-seat game whose seat 0 deck carries the
// real The Temporal Anchor, moves it onto seat 0's battlefield, and drives
// to the next scry arrange (the Anchor's upkeep Scry 2). It returns the
// engine, the replay config, the Anchor's object id, and the pending
// KArrange. The Anchor's zone is asserted as a precondition: the trigger
// only exists while it is on the battlefield.
func anchorOnBattlefield(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, *decision.Decision) {
	t.Helper()
	anchor := tokenReplCorpusCard(t, "The Temporal Anchor")
	e, cfg := tokenReplGame(t, seed, anchor)
	anchorID := moveSeededCard(t, e, 0, anchor, state.ZBattlefield)
	if o := e.G.Obj(anchorID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("setup: The Temporal Anchor is not on the battlefield (%+v)", o)
	}
	d := driveToScryArrange(t, e, 400)
	if d.Player != 0 {
		t.Fatalf("scry arrange player = %d, want 0 (the library owner and the Anchor's controller)", d.Player)
	}
	if len(d.Options) != 2 {
		t.Fatalf("scry arrange offered %d options, want 2 (The Temporal Anchor's `ScryNum$ 2`)", len(d.Options))
	}
	return e, cfg, anchorID, d
}

// TestTemporalAnchorScryBottomTrigger is the task's end-to-end pin against
// the real card. Two answers to the same real Scry 2:
//
//   - (a) keep both looked-at cards on top -> the completed Scry marker
//     records a ZERO bottom pile, the `ToBottom$ True` gate declines, and
//     nothing is exiled;
//   - (b) put one card on the bottom -> exactly one trigger fires, sized by
//     the one card bottomed (not the two looked at), exiling exactly that
//     card from the BOTTOM of the library.
func TestTemporalAnchorScryBottomTrigger(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:Scry"] {
		t.Fatal("effects.Supported() is missing trig:Scry: the mode would not count as implemented")
	}

	t.Run("KeepAllOnTopFiresNothing", func(t *testing.T) {
		e, cfg, anchorID, d := anchorOnBattlefield(t, 501)
		libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
		if len(libBefore) < 4 {
			t.Fatalf("setup: library has %d cards, need >= 4 so the looked-at window and the remainder are both non-empty", len(libBefore))
		}
		// Precondition: the bottom pile is empty for this answer, which is
		// what must make the `ToBottom$ True` "one or more" gate decline.
		// Answer with both offered options in offered order: every card
		// stays on top.
		choices := []int{d.Options[0].Index, d.Options[1].Index}

		submitChoices(t, e, choices...)

		// The Scry handler ran: one completed-scry marker, recording a
		// zero-card bottom pile. This is the "nothing happens" handler-ran
		// assertion -- without it the test would pass with the whole Scry
		// marker path unregistered.
		marks := scryMarkers(e)
		if len(marks) != 1 {
			t.Fatalf("log carries %d Scry markers after one scry, want exactly 1 (0 = the scry record was never emitted)", len(marks))
		}
		if marks[0].Amount != 0 {
			t.Fatalf("completed Scry marker Amount = %d, want 0 (both looked-at cards were kept on top)", marks[0].Amount)
		}
		if marks[0].Player != 0 {
			t.Fatalf("completed Scry marker Player = %d, want 0 (the scrying seat)", marks[0].Player)
		}

		// No ToBottom$ True trigger: nothing was exiled, the library is
		// untouched (every card kept on top in offered order), and -- the
		// direct observable -- the Anchor pushed only its own upkeep Phase
		// trigger, never a second ScryBottom one. A zero-count Dig is a no-op
		// too, so the push count is what catches a wrongful firing.
		if n := anchorTriggerPushes(e, anchorID); n != 1 {
			t.Fatalf("The Temporal Anchor pushed %d triggered abilities, want 1 (its upkeep Phase scry; 2 = the ToBottom$ True trigger fired on a bottom-less scry)", n)
		}
		if got := libraryExiled(e, 0); len(got) != 0 {
			t.Fatalf("seat 0 exiled %v after a bottom-less scry, want nothing (the trigger fired with no cards bottomed)", got)
		}
		if got := e.G.Zone(state.ZLibrary, 0); !sameObjIDs(got, libBefore) {
			t.Fatalf("library after keeping both = %v, want the untouched %v", got, libBefore)
		}
		if o := e.G.Obj(anchorID); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("The Temporal Anchor left the battlefield while nothing happened: %+v", o)
		}
		passUntilStackEmpty(t, e, 40)
		replayCheck(t, e, cfg)
	})

	t.Run("BottomOneFiresOnceAndExilesItFromTheBottom", func(t *testing.T) {
		e, cfg, anchorID, d := anchorOnBattlefield(t, 502)
		libBefore := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
		if len(libBefore) < 4 {
			t.Fatalf("setup: library has %d cards, need >= 4 so the arranged card really lands at the bottom", len(libBefore))
		}
		// Precondition: the two piles under comparison differ -- 2 cards were
		// looked at (the arrange's Max) and the answer bottomed exactly 1.
		if d.Max != 2 {
			t.Fatalf("scry arrange Max = %d, want 2 (the number LOOKED AT must be distinguishable from the number BOTTOMED)", d.Max)
		}
		keep := d.Options[0].Obj
		bottom := d.Options[1].Obj
		if keep == bottom {
			t.Fatalf("offered cards are identical (%v): the two piles cannot differ", keep)
		}
		// Answer with only option 0: option 1 goes to the very bottom.
		submitChoices(t, e, d.Options[0].Index)

		// The arrange has been applied and the trigger is on the stack: the
		// bottom card of the library must be the card we sent there, BEFORE
		// the trigger resolves -- this is the precondition the "exiled from
		// the bottom" assertion depends on.
		libArranged := e.G.Zone(state.ZLibrary, 0)
		if len(libArranged) != len(libBefore) {
			t.Fatalf("library length %d after the scry, want %d (the scry only reorders)", len(libArranged), len(libBefore))
		}
		if libArranged[len(libArranged)-1] != bottom {
			t.Fatalf("bottom of library after answering [option 0] = %v, want the unchosen %v", libArranged[len(libArranged)-1], bottom)
		}
		if libArranged[0] != keep {
			t.Fatalf("top of library after answering [option 0] = %v, want the chosen %v", libArranged[0], keep)
		}

		marks := scryMarkers(e)
		if len(marks) != 1 {
			t.Fatalf("log carries %d Scry markers after one scry, want exactly 1", len(marks))
		}
		if marks[0].Amount != 1 {
			t.Fatalf("completed Scry marker Amount = %d, want 1 (the number actually BOTTOMED, not the %d looked at)", marks[0].Amount, d.Max)
		}
		// Exactly one ToBottom$ True trigger fired: the upkeep Phase push plus
		// the ScryBottom one, and no more.
		if n := anchorTriggerPushes(e, anchorID); n != 2 {
			t.Fatalf("The Temporal Anchor pushed %d triggered abilities after bottoming one card, want 2 (upkeep Phase + exactly one ScryBottom; 3+ = the trigger fired per looked-at card)", n)
		}

		passUntilStackEmpty(t, e, 60)

		// Exactly one trigger fired, and its Dig exiled exactly one card --
		// the one that was on the bottom.
		exile := libraryExiled(e, 0)
		if len(exile) != 1 {
			t.Fatalf("seat 0 exiled %v after bottoming one card, want exactly one card (none = trig:Scry never fired, two = the trigger fired per looked-at card)", exile)
		}
		if exile[0] != bottom {
			t.Fatalf("exiled card = %v, want the card that was put on the BOTTOM (%v); the chosen top card was %v", exile[0], bottom, keep)
		}
		if len(scryMarkers(e)) != 1 {
			t.Fatalf("a second Scry marker appeared: %d, want 1", len(scryMarkers(e)))
		}
		// The library lost exactly that one card; the kept card is still on
		// top and the bottomed card is gone.
		libAfter := e.G.Zone(state.ZLibrary, 0)
		if len(libAfter) != len(libBefore)-1 {
			t.Fatalf("library length %d after the exile, want %d", len(libAfter), len(libBefore)-1)
		}
		if libAfter[0] != keep {
			t.Fatalf("top of library after the trigger = %v, want the kept %v", libAfter[0], keep)
		}
		for _, id := range libAfter {
			if id == bottom {
				t.Fatalf("the bottomed card %v is still in the library after being exiled", bottom)
			}
		}
		if o := e.G.Obj(anchorID); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("The Temporal Anchor left the battlefield: %+v", o)
		}
		replayCheck(t, e, cfg)
	})
}
