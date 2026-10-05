package effects

// The behavioural consequence of the `Card.OwnedBy Player.NotedFor<label>`
// referent: TMT Turtles in Time / TMT Step Between Worlds' DBTimetwister
// ChangeZoneAll sweep must move exactly the noted seats' cards from hand and
// graveyard into their libraries. Before this ticket the predicate was
// unknown and failed closed, so the sweep moved NOTHING for anyone -- the
// player-visible bug this ticket exists to fix. This drives the real corpus
// SA rather than a hand-built spec string, so a corpus spelling change is
// caught here too.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mkInZone places a fresh copy of src into owner's zone and returns its id,
// asserting the placement took.
func mkInZone(t *testing.T, h *fakeHost, owner state.PlayerID, z state.Zone, src string) state.ObjID {
	t.Helper()
	o := h.g.AddObject(mkCard(t, src), owner)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: z})
	if !inZone(h.g, z, owner, o.ID) {
		t.Fatalf("setup: object %d not in zone %s of seat %d", o.ID, z, owner)
	}
	return o.ID
}

func TestTurtlesInTimeChangeZoneAllSweepsOnlyNotedSeat(t *testing.T) {
	card, dbTimetwister := corpusSA(t, "Turtles in Time", "DBTimetwister")
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})

	h.Emit(events.Event{Kind: events.PlayerNoted, Player: 0, Text: "Stargate"})

	// PRECONDITION: seat 0 is noted and seat 1 is not, so the sweep's scope
	// (UseAllOriginZones$ True covers both seats) is separated ONLY by the
	// ChangeType$ predicate -- exactly the rule under test.
	if !MatchesPlayerSpecFrom(h.g, "Player.NotedForStargate", 0, 0, 0) {
		t.Fatal("setup: seat 0 is not noted Stargate")
	}
	if MatchesPlayerSpecFrom(h.g, "Player.NotedForStargate", 1, 0, 0) {
		t.Fatal("setup: seat 1 must not be noted Stargate")
	}
	if h.g.Obj(src.ID).Zone != state.ZBattlefield {
		t.Fatal("setup: source is not on the battlefield")
	}

	// Distinct card identities per seat and zone so a mis-attributed move
	// cannot hide behind a confusion of ids.
	notedHand := mkInZone(t, h, 0, state.ZHand, "Name:Noted Hand\nTypes:Sorcery\nOracle:x\n")
	notedYard := mkInZone(t, h, 0, state.ZGraveyard, "Name:Noted Yard\nTypes:Sorcery\nOracle:x\n")
	otherHand := mkInZone(t, h, 1, state.ZHand, "Name:Other Hand\nTypes:Sorcery\nOracle:x\n")
	otherYard := mkInZone(t, h, 1, state.ZGraveyard, "Name:Other Yard\nTypes:Sorcery\nOracle:x\n")

	svars := h.g.Obj(src.ID).Face().SVars
	effChangeZoneAll(h, &Ctx{Source: src.ID, Controller: 0, SVars: svars}, dbTimetwister)

	// Seat 0's hand and graveyard cards moved to seat 0's library...
	for _, tc := range []struct {
		name string
		id   state.ObjID
	}{
		{"noted hand", notedHand},
		{"noted graveyard", notedYard},
	} {
		o := h.g.Obj(tc.id)
		if o.Zone != state.ZLibrary || !inZone(h.g, state.ZLibrary, 0, tc.id) {
			t.Fatalf("seat 0's %s card (obj %d) = zone %s, want moved to seat 0's library", tc.name, tc.id, o.Zone)
		}
	}
	// ...and the unnoted seat's cards did not.
	if h.g.Obj(otherHand).Zone != state.ZHand || !inZone(h.g, state.ZHand, 1, otherHand) {
		t.Fatalf("seat 1's hand card (obj %d) = zone %s, want left in seat 1's hand", otherHand, h.g.Obj(otherHand).Zone)
	}
	if h.g.Obj(otherYard).Zone != state.ZGraveyard || !inZone(h.g, state.ZGraveyard, 1, otherYard) {
		t.Fatalf("seat 1's graveyard card (obj %d) = zone %s, want left in seat 1's graveyard", otherYard, h.g.Obj(otherYard).Zone)
	}
}

// TestStepBetweenWorldsChangeZoneAllSweepsOnlyNotedSeat is the second TMT
// carrier (the same DBTimetwister spelling) so a per-card copying error would
// show up as a gap here even if Turtles in Time passed.
func TestStepBetweenWorldsChangeZoneAllSweepsOnlyNotedSeat(t *testing.T) {
	card, dbTimetwister := corpusSA(t, "Step Between Worlds", "DBTimetwister")
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	h.Emit(events.Event{Kind: events.PlayerNoted, Player: 1, Text: "Stargate"})

	// PRECONDITION: this time the OTHER seat is the noted one.
	if !MatchesPlayerSpecFrom(h.g, "Player.NotedForStargate", 1, 0, 0) {
		t.Fatal("setup: seat 1 is not noted Stargate")
	}
	if MatchesPlayerSpecFrom(h.g, "Player.NotedForStargate", 0, 0, 0) {
		t.Fatal("setup: seat 0 must not be noted Stargate")
	}

	notedHand := mkInZone(t, h, 1, state.ZHand, "Name:Noted Hand\nTypes:Sorcery\nOracle:x\n")
	notedYard := mkInZone(t, h, 1, state.ZGraveyard, "Name:Noted Yard\nTypes:Sorcery\nOracle:x\n")
	otherHand := mkInZone(t, h, 0, state.ZHand, "Name:Other Hand\nTypes:Sorcery\nOracle:x\n")
	otherYard := mkInZone(t, h, 0, state.ZGraveyard, "Name:Other Yard\nTypes:Sorcery\nOracle:x\n")

	svars := h.g.Obj(src.ID).Face().SVars
	effChangeZoneAll(h, &Ctx{Source: src.ID, Controller: 0, SVars: svars}, dbTimetwister)

	if h.g.Obj(notedHand).Zone != state.ZLibrary || !inZone(h.g, state.ZLibrary, 1, notedHand) {
		t.Fatalf("seat 1's hand card (obj %d) = zone %s, want moved to seat 1's library", notedHand, h.g.Obj(notedHand).Zone)
	}
	if h.g.Obj(notedYard).Zone != state.ZLibrary || !inZone(h.g, state.ZLibrary, 1, notedYard) {
		t.Fatalf("seat 1's graveyard card (obj %d) = zone %s, want moved to seat 1's library", notedYard, h.g.Obj(notedYard).Zone)
	}
	if h.g.Obj(otherHand).Zone != state.ZHand || !inZone(h.g, state.ZHand, 0, otherHand) {
		t.Fatalf("seat 0's hand card (obj %d) = zone %s, want left in seat 0's hand", otherHand, h.g.Obj(otherHand).Zone)
	}
	if h.g.Obj(otherYard).Zone != state.ZGraveyard || !inZone(h.g, state.ZGraveyard, 0, otherYard) {
		t.Fatalf("seat 0's graveyard card (obj %d) = zone %s, want left in seat 0's graveyard", otherYard, h.g.Obj(otherYard).Zone)
	}
}
