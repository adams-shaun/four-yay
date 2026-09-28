package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Legate Lanius, Caesar's Ace is the corpus's one DivideEvenlyUp carrier:
//
//	SVar:X:Count$Valid Creature.RememberedPlayerCtrl/DivideEvenlyUp.10
//	"each opponent sacrifices a tenth of the creatures they control,
//	 rounded up"
//
// Before applyCountOp grew its Divide arm, a DivideEvenlyUp suffix fell
// through untouched, so X was the WHOLE count: with the RepeatEach +
// Sacrifice Amount$ X chain live, an opponent with eleven creatures was
// asked to sacrifice eleven (in fact not asked at all -- the eligible set
// never exceeded the count) instead of ceil(11/10) = two. These tests pin the
// real corpus card end to end; the compiled card is fetched by name so no
// Forge script text is committed (the licensing rule).
//
// Legate Lanius is in no repo deck and in no pinned legacy deck, so the chain
// heads and the acceptance ratchet are safe by construction.

// legateEngine seats seat 0 the corpus Legate Lanius plus basics and seat 1 a
// library of Grizzly Bears, forces seat 0 the toss, drives to Main 1, moves
// opponentCreatures Grizzly Bears from seat 1's library onto seat 1's
// battlefield, then moves Legate Lanius from seat 0's hand onto seat 0's
// battlefield (a real MoveZone, so its ChangesZone Decimate trigger fires).
func legateEngine(t *testing.T, reg *cards.Registry, opponentCreatures int) *Engine {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, "Legate Lanius, Caesar's Ace"),
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: 9314, Names: []string{"caesar", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	moved := 0
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...) {
		if moved >= opponentCreatures {
			break
		}
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Face().Name != "Grizzly Bears" {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
		moved++
	}
	if moved != opponentCreatures {
		t.Fatalf("seeded %d opponent creatures, want %d", moved, opponentCreatures)
	}
	e.pending = nil
	e.priorityRound()

	// Move Legate Lanius onto seat 0's battlefield: its ChangesZone trigger
	// (RepeatEach | RepeatPlayers$ Opponent | DB$ Sacrifice Amount$ X) queues.
	searchMoveByName(t, e, "Legate Lanius, Caesar's Ace", state.ZBattlefield)
	return e
}

// TestLegateLaniusSacrificesATenthRoundedUp is the reported wrong-value
// divergence: eleven opponent creatures must sacrifice ceil(11/10) = 2, not
// the whole eleven the pre-fix undivided count produced.
func TestLegateLaniusSacrificesATenthRoundedUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := legateEngine(t, reg, 11)

	battlefield := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 1)...)
	if len(battlefield) != 11 {
		t.Fatalf("opponent battlefield has %d creatures, want 11", len(battlefield))
	}

	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after Legate Lanius enters: %+v, want a KChoose sacrifice decision", d)
	}
	if d.ResumeKind != "sacrifice" {
		t.Fatalf("sacrifice ask ResumeKind = %q, want \"sacrifice\"", d.ResumeKind)
	}
	if d.Player != 1 {
		t.Fatalf("sacrifice ask player = %d, want the opponent (1)", d.Player)
	}
	// ceil(11/10) = 2. The pre-fix undivided read was 11, which exceeds the
	// eligible count, so no ask was posed at all and all eleven were taken.
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("sacrifice ask range = %d..%d, want 2..2 (a tenth of 11, rounded up)", d.Min, d.Max)
	}
	if len(d.Options) != 11 {
		t.Fatalf("sacrifice ask has %d options, want one per eligible creature (11)", len(d.Options))
	}

	// Answer with the third and fifth creatures in zone order, so honouring
	// the answer is distinguishable from any deterministic-first pick.
	wantSacs := []state.ObjID{battlefield[2], battlefield[4]}
	submitChoices(t, e, d.Options[2].Index, d.Options[4].Index)
	passUntilNonPriority(t, e, 20)

	sacs := sacrificeEvents(t, e)
	if len(sacs) != 2 {
		t.Fatalf("got %d sacrifice moves, want exactly 2 (a tenth of 11, rounded up)", len(sacs))
	}
	for i, id := range wantSacs {
		if sacs[i].Obj != id {
			t.Fatalf("sacrifice %d = obj %d (%s), want answered creature %d (%s)",
				i, sacs[i].Obj, objName(t, e, sacs[i].Obj), id, objName(t, e, id))
		}
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("answered creature %d (%s) zone = %+v, want graveyard", id, objName(t, e, id), o)
		}
	}
	// The other nine stay.
	for _, id := range battlefield {
		if id == wantSacs[0] || id == wantSacs[1] {
			continue
		}
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("unanswered creature %d (%s) left the battlefield: %+v", id, objName(t, e, id), o)
		}
	}
}

// TestLegateLaniusRoundsATenthUp is the tight-boundary rounding case: exactly
// ten creatures converts to one sacrificed (ceil(10/10) = 1). Ten eligibles
// against an amount of one still leaves a real WHICH-one choice, so the ask
// is a Min == Max == 1 KChoose -- not the whole ten the pre-fix undivided
// count would have taken (eligible 10 == amount 10: no ask, all ten gone).
func TestLegateLaniusRoundsATenthUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := legateEngine(t, reg, 10)

	battlefield := append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 1)...)
	if len(battlefield) != 10 {
		t.Fatalf("opponent battlefield has %d creatures, want 10", len(battlefield))
	}

	d := passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("with ten creatures, want the KChoose sacrifice ask: %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("sacrifice ask range = %d..%d, want 1..1 (a tenth of 10, rounded up)", d.Min, d.Max)
	}

	submitChoices(t, e, d.Options[0].Index)
	sacs := sacrificeEvents(t, e)
	if len(sacs) != 1 {
		t.Fatalf("got %d sacrifice moves, want exactly 1 (a tenth of 10, rounded up)", len(sacs))
	}
}
