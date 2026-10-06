package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// jaceEngine seats two corpus Jace Beleren copies (Legendary Planeswalker,
// Loyalty:3) in seat 0's deck and returns their object ids.
func jaceEngine(t *testing.T) (*Engine, [2]state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	jace := mustCorpusCard(t, reg, "Jace Beleren")
	deck := append([]*cards.Card{jace, jace}, mountainDeck(t, 38)...)
	cfg := Config{Seed: 37, Names: []string{"a", "b", "c", "d"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(seatZeroStart(cfg))
	var ids [2]state.ObjID
	n := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == jace && n < 2 {
			ids[n] = o.ID
			n++
		}
	}
	if n != 2 {
		t.Fatalf("found %d Jace Beleren copies, want 2", n)
	}
	return e, ids
}

// TestFaceDownPlaneswalkerSurvivesZeroLoyaltySBA: a manifested planeswalker
// is a face-down 2/2 creature (CR 708.5) with no loyalty counters, so the
// CR 704.5i zero-loyalty SBA must leave it on the battlefield.
func TestFaceDownPlaneswalkerSurvivesZeroLoyaltySBA(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, counter string }{
		{"manifest", events.FaceDownEntryCounter},
		{"cloak", events.CloakEntryCounter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, ids := jaceEngine(t)
			id := ids[0]
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand,
				To: state.ZBattlefield, Counter: tc.counter})
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield || !o.FaceDown {
				t.Fatalf("precondition: zone %v facedown %v, want face-down on battlefield", o.Zone, o.FaceDown)
			}
			if !o.Face().IsPlaneswalker() || o.Counter("LOYALTY") != 0 {
				t.Fatalf("precondition: printed walker %v, loyalty %d, want printed walker with 0 counters",
					o.Face().IsPlaneswalker(), o.Counter("LOYALTY"))
			}
			e.checkStateBased()
			if o.Zone != state.ZBattlefield {
				t.Fatalf("face-down planeswalker was swept to %v by the zero-loyalty SBA", o.Zone)
			}
			if !e.IsCreature(id) {
				t.Fatal("face-down planeswalker is not a creature")
			}
		})
	}
}

// TestFaceDownPlaneswalkerIsNotLegendaryForTheLegendRule: two face-down Jace
// Beleren copies are two vanilla 2/2s, not a CR 704.5j duplicate pair.
func TestFaceDownPlaneswalkerIsNotLegendaryForTheLegendRule(t *testing.T) {
	t.Parallel()
	e, ids := jaceEngine(t)
	for _, id := range ids {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand,
			To: state.ZBattlefield, Counter: events.FaceDownEntryCounter})
	}
	for _, id := range ids {
		if o := e.G.Obj(id); o.Zone != state.ZBattlefield || !o.FaceDown || !o.Face().IsLegendary() {
			t.Fatalf("precondition: obj %d zone %v facedown %v legendary face %v", id, o.Zone, o.FaceDown, o.Face().IsLegendary())
		}
	}
	e.checkStateBased()
	if d := e.Pending(); d != nil {
		t.Fatalf("face-down duplicate Jace posed a decision: %+v", d)
	}
	for _, id := range ids {
		if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
			t.Fatalf("face-down Jace %d left the battlefield for %v", id, z)
		}
	}
}

// TestFaceUpPlaneswalkerWithoutLoyaltyStillSwept: the face-down guard must not
// disable the CR 704.5i sweep for a real, face-up walker.
func TestFaceUpPlaneswalkerWithoutLoyaltyStillSwept(t *testing.T) {
	t.Parallel()
	e, ids := jaceEngine(t)
	id := ids[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	o := e.G.Obj(id)
	if o.FaceDown || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") != 3 {
		t.Fatalf("precondition: facedown %v zone %v loyalty %d, want face-up with 3 loyalty",
			o.FaceDown, o.Zone, o.Counter("LOYALTY"))
	}
	e.checkStateBased()
	if o.Zone != state.ZBattlefield {
		t.Fatalf("loyal walker left the battlefield for %v", o.Zone)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LOYALTY", Amount: -3})
	if o.Counter("LOYALTY") != 0 {
		t.Fatalf("precondition: loyalty %d, want 0", o.Counter("LOYALTY"))
	}
	e.checkStateBased()
	if o.Zone != state.ZGraveyard {
		t.Fatalf("zero-loyalty face-up walker is in %v, want graveyard", o.Zone)
	}
}
