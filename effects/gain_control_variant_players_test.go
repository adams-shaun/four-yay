package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// pickHost is a fakeHost whose seeded Rand returns a scripted sequence, so a
// test can prove Scrambleverse draws one INDEPENDENT random player per
// permanent (the plain fakeHost always returns 0). The remaining Host methods
// are promoted from *fakeHost.
type pickHost struct {
	*fakeHost
	picks []int
	next  int
}

func (h *pickHost) Rand(n int) int {
	if n <= 0 {
		return 0
	}
	if h.next < len(h.picks) {
		v := h.picks[h.next]
		h.next++
		return ((v % n) + n) % n
	}
	return 0
}

// playersBoard builds a three-seat game with the named source object on the
// battlefield under seat 0 and returns the host and the object ids. Each
// permanent is placed through a logged MoveZone so the board is assembled the
// way a real resolution sees it.
func playersBoard(t *testing.T, seats int, src *cards.Card) (*fakeHost, *state.Game, state.ObjID) {
	t.Helper()
	h := newHost(t, seats)
	srcID := h.g.AddObject(src, 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: srcID.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return h, h.g, srcID.ID
}

// addPermanent puts a fresh nonland permanent (or the given type) under
// owner p and returns its id. The object is created in the library and moved
// through a logged MoveZone event.
func addPermanent(t *testing.T, h *fakeHost, p state.PlayerID, typeLine string) state.ObjID {
	t.Helper()
	c := mkCard(t, "Name:P\nTypes:"+typeLine+"\nPT:1/1\nOracle:x\n")
	o := h.g.AddObject(c, p)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

// TestChooseDirectionAsksLeftRightAndCarriesTheAnswer pins the direction root
// the two direction-consuming cards sit on: the ask offers exactly "left" and
// "right" to the resolving controller, and an answered re-entry carries the
// word on Ctx.ChosenDirection (the resolution-scratch transport the chained
// GainControlVariant reads) without posing a second ask.
func TestChooseDirectionAsksLeftRightAndCarriesTheAnswer(t *testing.T) {
	h, _, src := playersBoard(t, 3, mkCard(t, "Name:Dir\nTypes:Sorcery\nOracle:x\n"))
	h.askResult = true
	dirSA := sa(t, "SP$ ChooseDirection")
	Resolve(h, &Ctx{Source: src, Controller: 0}, dirSA)

	d := h.lastAsk
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choosedirection" {
		t.Fatalf("direction ask = %+v, want a KChoose with ResumeKind choosedirection", d)
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("direction ask player/min/max = %d/%d/%d, want 0/1/1", d.Player, d.Min, d.Max)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "direction" || d.Options[0].Label != "left" ||
		d.Options[1].Kind != "direction" || d.Options[1].Label != "right" {
		t.Fatalf("direction options = %+v, want left/right", d.Options)
	}
	// Precondition: the pre-answer Ctx really carries no direction, so the
	// answered assertion below is not vacuous.
	if got := (&Ctx{}).ChosenDirection; got != "" {
		t.Fatalf("precondition: fresh Ctx direction = %q, want empty", got)
	}
	asks := h.askCount
	answered := &Ctx{Source: src, Controller: 0, ChosenDirection: "right"}
	Resolve(h, answered, dirSA)
	if h.askCount != asks {
		t.Fatalf("answered re-entry posed a second ask (count %d -> %d)", asks, h.askCount)
	}
	if answered.ChosenDirection != "right" {
		t.Fatalf("answered Ctx direction = %q, want right (the chained Sub reads this)", answered.ChosenDirection)
	}
}

// TestGainControlVariantScrambleverseRandomTransfersAndUntaps drives
// Scrambleverse's real corpus SA: ChangeController$ Random draws one random
// LIVING player per nonland permanent, transfers control, then the chained
// DBUntap untaps those same permanents. The scripted pickHost proves the
// draws are per-permanent (the first permanent goes to seat 0, the second to
// seat 1) rather than one player for the whole batch.
func TestGainControlVariantScrambleverseRandomTransfersAndUntaps(t *testing.T) {
	card, scramble := corpusSA(t, "Scrambleverse", "")
	if scramble.API != "GainControlVariant" || scramble.Params["ChangeController"] != "Random" {
		t.Fatalf("unexpected SA: %+v", scramble)
	}
	if scramble.Sub == nil || scramble.Sub.API != "UntapAll" || scramble.Sub.Params["ValidCards"] != "Permanent.nonLand" {
		t.Fatalf("Scrambleverse Sub = %+v, want the DBUntap UntapAll", scramble.Sub)
	}
	if scramble.Params["AllValid"] != "Permanent.nonLand" {
		t.Fatalf("AllValid$ = %q, want Permanent.nonLand", scramble.Params["AllValid"])
	}
	h := newHost(t, 2)
	srcID := h.g.AddObject(card, 0)
	// Scrambleverse is a sorcery: while it resolves it is on the STACK, not
	// the battlefield, so it is not one of its own AllValid$ Permanent.nonLand
	// subjects. Leaving it in the library keeps that fact true here.
	if o := h.g.Obj(srcID.ID); o.Zone == state.ZBattlefield {
		t.Fatalf("precondition: the resolving source is on the battlefield")
	}
	rock := addPermanent(t, h, 1, "Artifact")
	gem := addPermanent(t, h, 1, "Creature")
	land := addPermanent(t, h, 1, "Land")
	// Precondition: the two nonland permanents really belong to seat 1, the
	// land is a land, and every permanent starts TAPPED (so the untap below
	// is observable and the land's staying tapped proves the filter).
	for _, id := range []state.ObjID{rock, gem, land} {
		h.Emit(events.Event{Kind: events.Tap, Obj: id})
	}
	if o := h.g.Obj(rock); o.Controller != 1 || o.Owner != 1 || !o.Tapped {
		t.Fatalf("precondition: rock owner/controller/tapped = %d/%d/%v, want 1/1/true", o.Owner, o.Controller, o.Tapped)
	}
	if o := h.g.Obj(gem); o.Controller != 1 || !o.Tapped {
		t.Fatalf("precondition: gem controller/tapped = %d/%v, want 1/true", o.Controller, o.Tapped)
	}
	if o := h.g.Obj(land); !hasType(o, "Land") || !o.Tapped {
		t.Fatalf("precondition: land types/tapped = %v/%v, want a tapped Land", o.Face().Types, o.Tapped)
	}
	// The first permanent's draw picks seat 0; the second's picks seat 1.
	ph := &pickHost{fakeHost: h, picks: []int{0, 1}}
	Resolve(ph, &Ctx{Source: srcID.ID, Controller: 0}, scramble)

	if len(h.log) > 0 {
		for _, e := range h.log {
			if e.Kind == events.Note {
				t.Fatalf("unexpected Note during Scrambleverse: %q", e.Text)
			}
		}
	}
	if got := h.g.Obj(rock).Controller; got != 0 {
		t.Fatalf("first nonland controller = %d, want 0 (the first random draw)", got)
	}
	if got := h.g.Obj(gem).Controller; got != 1 {
		t.Fatalf("second nonland controller = %d, want 1 (an independent per-permanent draw)", got)
	}
	if got := h.g.Obj(land).Controller; got != 1 {
		t.Fatalf("land controller = %d, want 1 (AllValid$ Permanent.nonLand must exclude it)", got)
	}
	if h.g.Obj(rock).Tapped || h.g.Obj(gem).Tapped {
		t.Fatalf("nonland permanents tapped = %v/%v, want both untapped (DBUntap after the transfer)",
			h.g.Obj(rock).Tapped, h.g.Obj(gem).Tapped)
	}
	if !h.g.Obj(land).Tapped {
		t.Fatal("land was untapped, want it left tapped (the untap is Permanent.nonLand too)")
	}
	if len(h.controls) != 2 {
		t.Fatalf("control grants = %d, want 2 (one per nonland permanent)", len(h.controls))
	}
}

// TestGainControlVariantAminatouHandsOffInBothDirections pins Aminatou's
// NextPlayerInChosenDirection shape on a 3-seat ring, in BOTH directions:
// every player gains all nonland permanents controlled by the next player in
// the chosen direction, and the source (Aminatou herself, excluded by
// AllValid$ Permanent.nonLand+Other) never moves.
func TestGainControlVariantAminatouHandsOffInBothDirections(t *testing.T) {
	_, control := corpusSA(t, "Aminatou, the Fateshifter", "DBControl")
	if control.API != "GainControlVariant" || control.Params["ChangeController"] != "NextPlayerInChosenDirection" {
		t.Fatalf("unexpected SA: %+v", control)
	}
	// left: next(0)=1, next(1)=2, next(2)=0; right is the reverse.
	type expect struct {
		dir        string
		s1, s2, o0 state.PlayerID
	}
	for _, want := range []expect{
		{dir: "left", s1: 0, s2: 1, o0: 2},
		{dir: "right", s1: 2, s2: 0, o0: 1},
	} {
		t.Run(want.dir, func(t *testing.T) {
			h := newHost(t, 3)
			am := addPermanent(t, h, 0, "Planeswalker") // the source, +Other excludes it
			o0 := addPermanent(t, h, 0, "Artifact")     // seat 0's OTHER nonland
			s1 := addPermanent(t, h, 1, "Creature")
			s2 := addPermanent(t, h, 2, "Creature")
			l1 := addPermanent(t, h, 1, "Land")
			// Precondition: the source and every subject really start with
			// their owner's controller, so the hand-off below is measurable.
			for _, id := range []state.ObjID{am, o0, s1, s2, l1} {
				if got := h.g.Obj(id).Controller; got != h.g.Obj(id).Owner {
					t.Fatalf("precondition: %d controller %d, want owner %d", id, got, h.g.Obj(id).Owner)
				}
			}
			Resolve(h, &Ctx{Source: am, Controller: 0, ChosenDirection: want.dir}, control)

			if got := h.g.Obj(s1).Controller; got != want.s1 {
				t.Fatalf("seat 1's creature controller = %d, want %d (direction %s)", got, want.s1, want.dir)
			}
			if got := h.g.Obj(s2).Controller; got != want.s2 {
				t.Fatalf("seat 2's creature controller = %d, want %d (direction %s)", got, want.s2, want.dir)
			}
			if got := h.g.Obj(o0).Controller; got != want.o0 {
				t.Fatalf("seat 0's other artifact controller = %d, want %d (direction %s)", got, want.o0, want.dir)
			}
			if got := h.g.Obj(am).Controller; got != 0 {
				t.Fatalf("Aminatou controller = %d, want 0 (+Other must exclude the source)", got)
			}
			if got := h.g.Obj(l1).Controller; got != 1 {
				t.Fatalf("seat 1's land controller = %d, want 1 (nonland only)", got)
			}
		})
	}
}

// TestGainControlNeighborSkipsEliminatedSeats pins the checked turn/seat
// order the direction variants depend on: the "next player in the chosen
// direction" is the next LIVING seat, so an eliminated seat between two
// survivors is skipped consistently. Seat 1 (Lost) must not be named as
// either neighbour.
func TestGainControlNeighborSkipsEliminatedSeats(t *testing.T) {
	h := newHost(t, 3)
	if h.g.Players[1].Lost {
		t.Fatal("precondition: seat 1 already eliminated")
	}
	h.g.Players[1].Lost = true
	if h.g.AliveCount() != 2 {
		t.Fatalf("precondition: alive count = %d, want 2", h.g.AliveCount())
	}
	if got, ok := gainControlNeighbor(h.g, 0, directionLeft); !ok || got != 2 {
		t.Fatalf("left neighbour of seat 0 = %d/%v, want 2/true (seat 1 eliminated)", got, ok)
	}
	if got, ok := gainControlNeighbor(h.g, 0, directionRight); !ok || got != 2 {
		t.Fatalf("right neighbour of seat 0 = %d/%v, want 2/true (seat 1 eliminated)", got, ok)
	}
	// The survivor's own direction ring visits both living seats once.
	ring := gainControlDirectionRing(h.g, 0, directionLeft)
	if len(ring) != 2 || ring[0] != 0 || ring[1] != 2 {
		t.Fatalf("direction ring = %v, want [0 2]", ring)
	}
}
