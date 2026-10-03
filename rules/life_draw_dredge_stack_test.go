// Findings-sol5 stack-path siblings: the committed direct-arm probe
// (life_draw_dredge_test.go) answers the parked GainLife→Draw dredge asks
// while the stack is EMPTY, so it exercises only handleModes' direct arm.
// The review round proved two further defects that live ONLY in
// resumeResolution's parked-frame arm -- a resolving spell on the stack:
//
//   - the FINAL parked draw of the body (lifeDraws == 0, i.e. the draw that
//     asked was the last one) had its answer silently dropped -- neither
//     applyDredge nor resumeOrdinaryDraw ran, and the flow fell into the
//     misleading "mid-resolution answer resumed with no sub-ability
//     recorded" Note; and
//   - every re-park early return discarded the interrupted resolution's
//     continuation chain (the new pending point is built bare, outer nil,
//     and rp.outer was never attached), so after the cascade completed the
//     object was finished without ever running the sub-ability that
//     followed the GainLife.
//
// The probe is on real card scripts: Nefarious Lich on the battlefield
// ("If you would gain life, draw that many cards instead"), Kiss of the
// Amesha resolving on the stack (SP$ GainLife 7 | SubAbility$ DBDraw, draw
// 2) targeting its caster, and a Golgari Thug (Dredge 4) in the graveyard.
// Seven life draws each pose their own dredge ask; the card's answer is
// 7+2=9 Draw events and Kiss in its owner's graveyard.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lichKissEngine is lichThugEngine with a per-card destination, because the
// stack path needs a resolving SPELL: Golgari Thug to the graveyard, Nefarious
// Lich to the battlefield, and Kiss of the Amesha left in seat 0's hand (moved
// there with a logged MoveZone only when genesis dealt it into the library, so
// the log-only replay reproduces the same zones either way).
func lichKissEngine(t *testing.T, seed uint64) (*Engine, Config, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	lead := []string{"Golgari Thug", "Nefarious Lich", "Kiss of the Amesha"}
	deck := []*cards.Card{}
	for _, name := range lead {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from corpus", name)
		}
		if d := c.Link(); len(d) != 0 {
			t.Fatalf("link %s: %v", name, d)
		}
		deck = append(deck, c)
	}
	deck = append(deck, mountainDeck(t, 40-len(lead))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	ids := make([]state.ObjID, 0, len(lead))
	for _, name := range lead {
		var tid state.ObjID
		var from state.Zone
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == name {
					tid = id
					from = z
				}
			}
		}
		if tid == 0 {
			t.Fatalf("%s was not dealt", name)
		}
		to := state.ZGraveyard
		switch name {
		case "Nefarious Lich":
			to = state.ZBattlefield
		case "Kiss of the Amesha":
			to = state.ZHand
		}
		if from != to {
			e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: from, To: to})
		}
		ids = append(ids, tid)
	}
	return e, cfg, ids
}

// castKiss funds the pool, casts Kiss of the Amesha from seat 0's hand
// targeting seat 0 (whose Lich turns the 7 life into 7 draws), and passes
// priority until the spell resolves far enough to pose its first dredge ask.
func castKiss(t *testing.T, e *Engine, kiss state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "WWUUCC") // {4}{W}{U}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == kiss {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Kiss of the Amesha: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	targetPlayer(t, e, 0)
	for i := 0; i < 10; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending while waiting for the spell to resolve")
		}
		if d.Kind != decision.KPriority {
			return
		}
		passIdx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				passIdx = o.Index
			}
		}
		if passIdx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{passIdx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatal("the spell never started resolving (no ask appeared within 10 priority rounds)")
}

// answerDredges answers every pending dredge ask with answerIdx, returning
// the count answered; it fatals if more than 20 are posed (a runaway cascade).
func answerDredges(t *testing.T, e *Engine, answerIdx func(d *decision.Decision) int) int {
	t.Helper()
	n := 0
	for {
		d := e.Pending()
		if d == nil || d.ResumeKind != "dredge" {
			return n
		}
		n++
		if n > 20 {
			t.Fatalf("runaway dredge cascade: %d asks and counting", n)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
			Choices: []int{answerIdx(d)}}); err != nil {
			t.Fatalf("submit dredge answer: %v", err)
		}
	}
}

// drawEvents counts seat 0's Draw events since `since`.
func drawEvents(t *testing.T, e *Engine, since int) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			n++
		}
	}
	return n
}

// millEvents counts library→graveyard MoveZone events since `since` (seat 0's
// dredge mill; nothing else in this fixture moves library→graveyard).
func millEvents(t *testing.T, e *Engine, since int) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZGraveyard {
			n++
		}
	}
	return n
}

// assertNoOrphanNote fails if the misleading no-sub-ability Note (the
// findings-sol5 finding-1 symptom) appears since `since`.
func assertNoOrphanNote(t *testing.T, e *Engine, since int) {
	t.Helper()
	for _, ev := range e.L.Events[since:] {
		if ev.Kind == events.Note && ev.Text == "mid-resolution answer resumed with no sub-ability recorded" {
			t.Fatalf("misleading no-sub-ability Note emitted for an answer that WAS made: %+v", ev)
		}
	}
}

// TestLifeReplacementDredgeStackAcceptsFirstAsk accepts the FIRST ask (no
// re-park: the Thug returns to hand, so the remaining six life draws are
// ordinary and ask nothing), then the DBDraw sub-ability runs to its full 2.
// This is the shape the review round already verified holds; committed so the
// ordinary suite keeps defending the no-re-park continuation against
// regressions.
func TestLifeReplacementDredgeStackAcceptsFirstAsk(t *testing.T) {
	e, cfg, ids := lichKissEngine(t, 253)
	since := len(e.L.Events)
	castKiss(t, e, ids[2])
	d := e.Pending()
	if d == nil || d.ResumeKind != "dredge" {
		t.Fatalf("pending = %+v, want the first dredge ask", d)
	}
	asks := answerDredges(t, e, func(d *decision.Decision) int {
		return 0 // accept: mill 4 and return the Thug; no further asks follow
	})
	if asks != 1 {
		t.Fatalf("dredge asks after accepting the first = %d, want 1 (the Thug is back in hand)", asks)
	}
	if n := millEvents(t, e, since); n != 4 {
		t.Fatalf("accepted dredge milled %d cards, want 4", n)
	}
	if n := drawEvents(t, e, since); n != 8 {
		t.Fatalf("Draw events = %d, want 8 (the dredged draw is replaced; 6 remain + 2 DBDraw)", n)
	}
	if hand := e.G.Zone(state.ZHand, 0); len(hand) == 0 {
		t.Fatal("dredger did not return to hand: hand empty")
	} else {
		found := false
		for _, id := range hand {
			if id == ids[0] {
				found = true
			}
		}
		if !found {
			t.Fatal("dredger did not return to hand")
		}
	}
	if o := e.G.Obj(ids[2]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Kiss of the Amesha zone = %+v, want the graveyard after resolving", o)
	}
	assertNoOrphanNote(t, e, since)
	replayCheck(t, e, cfg)
}
