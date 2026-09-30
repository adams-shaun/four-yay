package view

// Task c7: pin what each visibility reveals. All the invariants below are
// individually verified live against a running server (the brief), but almost
// none of them were pinned by a test -- the single pre-existing zone test
// projected for viewer 0 and then inspected player 0's OWN zones, so it would
// still pass if an opponent's graveyard were redacted to empty. This file is
// the missing pin.
//
// The board is built so that EVERY player has a non-empty hand, graveyard,
// exile and library with DIFFERENT counts per player (per zone), so no
// assertion below can pass on a coincidental zero or on two seats sharing a
// value. The four tests are deliberately named per visibility so a mutation
// can be shown against the exact test it breaks.
//
// What is being pinned:
//   - A player's hand is private. Other seats and public spectators see its
//     count (HandSize) and never the cards (drawn from the V1 production
//     incident where a snapshot endpoint returned a god view mid-game).
//   - Graveyard and exile are fully public: every seat and every public
//     spectator sees every card in every player's graveyard and exile.
//   - Library contents are never listed except for a real seat's OWN library
//     in Seat mode (own_library_list: the unordered list the reward-loop spec
//     asks for), and the secret ORDER is never listed in ANY mode -- the
//     omniscient spectator included (spec D12: library order spoils draws).
//     Only LibrarySize, plus the canonicalised own list.
//   - A mana pool is PUBLIC in every mode (CR 106.4a/106.4b): it is not one
//     of the seven zones in CR 400.1 and holds no cards, so CR 400.2's
//     hidden-zone framework has no purchase on it; instead 106.4a, 106.4b,
//     117.3d and 118.3a all require a player to ANNOUNCE what is in their
//     pool. The old pin "another seat's pool is nil" encoded the opposite
//     divergence and is retired here.
//   - Omniscient additionally reveals every hand (and every pool, though the
//     pool is now public in every mode and no longer needs the extra arm).

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// visibilityBoard builds a 4-seat game where every player has a non-empty,
// per-player-distinct count of cards in every zone that this rule set talks
// about. Counts per zone per seat:
//
//	zone       seat0 seat1 seat2 seat3
//	library       6     5     7     4
//	hand          3     2     4     1
//	graveyard     2     4     1     3
//	exile         1     3     2     4
//
// No two seats share a hand/graveyard/exile/library count within a zone, so a
// redaction that swaps "just the count" for "the count of someone who happens
// to have the same number of cards" cannot silently pass. Each player also
// gets a mana pool whose green symbol count equals 1+seat, again distinct, so
// the pool-reveal assertions cannot pass on a shared or zero value either.
func visibilityBoard(t *testing.T) *state.Game {
	t.Helper()
	g := state.NewGame([]string{"alice", "bob", "carol", "dave"})
	c, _ := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n"))
	c.Link()
	for _, zc := range []struct {
		z state.Zone
		n [4]int
	}{
		{state.ZLibrary, [4]int{6, 5, 7, 4}},
		{state.ZHand, [4]int{3, 2, 4, 1}},
		{state.ZGraveyard, [4]int{2, 4, 1, 3}},
		{state.ZExile, [4]int{1, 3, 2, 4}},
	} {
		for p := state.PlayerID(0); p < 4; p++ {
			ids := make([]state.ObjID, 0, zc.n[p])
			for i := 0; i < zc.n[p]; i++ {
				o := g.AddObject(c, p)
				o.Zone = zc.z
				ids = append(ids, o.ID)
			}
			g.SetZone(zc.z, p, ids)
		}
	}
	for p := state.PlayerID(0); p < 4; p++ {
		g.Players[p].Pool[state.MG] = int32(1 + int(p))
	}
	return g
}

// assertZoneViewsMatch asserts that a projected []CardView names exactly the
// zone's real object ids, in zone order -- a length check alone could not tell
// "the right cards" from "the right number of the wrong cards".
func assertZoneViewsMatch(t *testing.T, what string, seat state.PlayerID, want []state.ObjID, got []CardView) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("seat %d %s: got %d cards, want %d ids (%v)", seat, what, len(got), len(want), want)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("seat %d %s[%d]: got id %d, want %d", seat, what, i, got[i].ID, want[i])
		}
	}
}

// libIDsOf grabs one seat's real library ids from the game.
func libIDsOf(g *state.Game, seat state.PlayerID) []state.ObjID {
	return append([]state.ObjID(nil), g.Zone(state.ZLibrary, seat)...)
}

// TestSeatVisibilityProjectsViewersHandAndEverySeatsPool is group 1: Seat
// projected for seat 0. Every other seat contributes a count and a fully
// public graveyard/exile/library_size, its hand is a bare nil, and seat 0's
// own hand is fully populated. The mana pool is public (CR 106.4a/106.4b), so
// every seat -- own and opponent alike -- carries a populated pool.
func TestSeatVisibilityProjectsViewersHandAndEverySeatsPool(t *testing.T) {
	g := visibilityBoard(t)
	ch := flatChars{g}
	const viewer = state.PlayerID(0)
	v := ProjectFor(g, ch, viewer, Seat, nil)
	if v.Visibility != "seat" {
		t.Fatalf("Visibility = %q, want %q", v.Visibility, Seat.String())
	}

	own := v.Players[viewer]
	wantHand := g.Zone(state.ZHand, viewer)
	assertZoneViewsMatch(t, "own hand", viewer, wantHand, own.Hand)
	if own.Pool == nil {
		t.Fatal("the viewer's own mana pool is absent")
	}
	if own.Pool["G"] != int32(1) {
		t.Fatalf("the viewer's own pool = %d green, want 1", own.Pool["G"])
	}

	for other := state.PlayerID(1); other < 4; other++ {
		pv := v.Players[other]
		if pv.Hand != nil {
			t.Fatalf("viewer %d reads seat %d's hand: %v", viewer, pv.ID, pv.Hand)
		}
		if pv.HandSize != len(g.Zone(state.ZHand, pv.ID)) {
			t.Fatalf("seat %d hand_size = %d, want %d", pv.ID, pv.HandSize, len(g.Zone(state.ZHand, pv.ID)))
		}
		// The pool is public (CR 106.4a/106.4b): a non-owning viewer reads
		// seat N's pool, which floats 1+N green in this fixture, exactly as
		// the owner does.
		if pv.Pool == nil {
			t.Fatalf("viewer %d reads no pool for seat %d: pool is public", viewer, pv.ID)
		}
		if pv.Pool["G"] != int32(1+int(pv.ID)) {
			t.Fatalf("viewer %d reads seat %d pool = %d green, want %d (public)", viewer, pv.ID, pv.Pool["G"], 1+int(pv.ID))
		}
		// Graveyard and exile are fully public: an opponent's cards must be
		// present, not just their counts.
		assertZoneViewsMatch(t, "graveyard", pv.ID, g.Zone(state.ZGraveyard, pv.ID), pv.Graveyard)
		assertZoneViewsMatch(t, "exile", pv.ID, g.Zone(state.ZExile, pv.ID), pv.Exile)
		if pv.LibrarySize != len(g.Zone(state.ZLibrary, pv.ID)) {
			t.Fatalf("seat %d library_size = %d, want %d", pv.ID, pv.LibrarySize, len(g.Zone(state.ZLibrary, pv.ID)))
		}
	}
}

// TestPublicVisibilityRedactsEveryHandAndShowsEveryPublicZoneAndPool is group
// 2: Public for every viewer (it is viewer-independent, so all four are
// checked). No hand cards anywhere; every graveyard and exile fully visible;
// library only a size. Every seat's pool is present (public, CR
// 106.4a/106.4b).
func TestPublicVisibilityRedactsEveryHandAndShowsEveryPublicZoneAndPool(t *testing.T) {
	g := visibilityBoard(t)
	ch := flatChars{g}
	for viewer := state.PlayerID(0); viewer < 4; viewer++ {
		v := ProjectFor(g, ch, viewer, Public, nil)
		if v.Visibility != "public" {
			t.Fatalf("viewer %d: Visibility = %q, want %q", viewer, v.Visibility, Public.String())
		}
		for _, pv := range v.Players {
			if pv.Hand != nil {
				t.Fatalf("public viewer %d reads seat %d's hand: %v", viewer, pv.ID, pv.Hand)
			}
			// The pool is public (CR 106.4a/106.4b): a public spectator reads
			// every seat's pool, which floats 1+seat green here.
			if pv.Pool == nil {
				t.Fatalf("public viewer %d reads no pool for seat %d: pool is public", viewer, pv.ID)
			}
			if pv.Pool["G"] != int32(1+int(pv.ID)) {
				t.Fatalf("public viewer %d reads seat %d pool = %d green, want %d", viewer, pv.ID, pv.Pool["G"], 1+int(pv.ID))
			}
			if pv.HandSize != len(g.Zone(state.ZHand, pv.ID)) {
				t.Fatalf("seat %d hand_size = %d, want %d", pv.ID, pv.HandSize, len(g.Zone(state.ZHand, pv.ID)))
			}
			assertZoneViewsMatch(t, "graveyard", pv.ID, g.Zone(state.ZGraveyard, pv.ID), pv.Graveyard)
			assertZoneViewsMatch(t, "exile", pv.ID, g.Zone(state.ZExile, pv.ID), pv.Exile)
			if pv.LibrarySize != len(g.Zone(state.ZLibrary, pv.ID)) {
				t.Fatalf("seat %d library_size = %d, want %d", pv.ID, pv.LibrarySize, len(g.Zone(state.ZLibrary, pv.ID)))
			}
		}
	}
}

// TestOmniscientRevealsEveryHandAndPoolNeverTheLibrary is group 3: every hand
// and pool populated, and still no library list and no library object id.
func TestOmniscientRevealsEveryHandAndPoolNeverTheLibrary(t *testing.T) {
	g := visibilityBoard(t)
	ch := flatChars{g}
	for viewer := state.PlayerID(0); viewer < 4; viewer++ {
		v := ProjectFor(g, ch, viewer, Omniscient, nil)
		if v.Visibility != "omniscient" {
			t.Fatalf("viewer %d: Visibility = %q, want %q", viewer, v.Visibility, Omniscient.String())
		}
		for _, pv := range v.Players {
			assertZoneViewsMatch(t, "hand", pv.ID, g.Zone(state.ZHand, pv.ID), pv.Hand)
			if pv.Pool == nil {
				t.Fatalf("omniscient viewer %d sees no pool for seat %d", viewer, pv.ID)
			}
			if pv.Pool["G"] != int32(1+int(pv.ID)) {
				t.Fatalf("seat %d pool = %d green, want %d", pv.ID, pv.Pool["G"], 1+int(pv.ID))
			}
			assertZoneViewsMatch(t, "graveyard", pv.ID, g.Zone(state.ZGraveyard, pv.ID), pv.Graveyard)
			assertZoneViewsMatch(t, "exile", pv.ID, g.Zone(state.ZExile, pv.ID), pv.Exile)
			if pv.LibrarySize != len(g.Zone(state.ZLibrary, pv.ID)) {
				t.Fatalf("seat %d library_size = %d, want %d", pv.ID, pv.LibrarySize, len(g.Zone(state.ZLibrary, pv.ID)))
			}
		}
		// D12: an omniscient spectator sees MANY secrets, never the library.
		// The marshalled payload (what a client actually receives) must leak
		// no seat's library object id. Positive control: the same walk found
		// every seat's hand ids (omniscient reveals all hands), proving the
		// walk reaches into the payload rather than just finding nothing.
		found := idsFoundIn(t, v)
		for seat := state.PlayerID(0); seat < 4; seat++ {
			for _, id := range g.Zone(state.ZHand, seat) {
				if !found[int(id)] {
					t.Fatalf("positive control: omniscient viewer %d does not find seat %d's hand id %d — the walk proves nothing", viewer, seat, id)
				}
			}
			for _, id := range libIDsOf(g, seat) {
				if found[int(id)] {
					t.Fatalf("omniscient viewer %d payload leaks seat %d's library object %d (D12)", viewer, seat, id)
				}
			}
		}
	}
}

// TestNoVisibilityEverListsTheLibrary is group 4's library half brought
// together: no marshalled projection may leak any seat's library object id to
// a viewer that seat does not belong to, and the player's own hand id must
// remain findable (positive control proving the walk reaches in, so a leak
// cannot be silently missed).
//
// The one deliberate exception is a real seat's OWN library in Seat mode:
// `own_library_list` (reward-loop spec §6) exposes the viewer's own library as
// an UNORDERED []CardView, gated on "is this the viewer's own seat" exactly
// like Hand (CR 400.2). That is an amendment to D12's "library order is
// hidden (count only)": the ORDER stays hidden (view.unorderedLibrary
// canonicalises it by name then object id, so the emitted order is a pure
// function of the CONTENTS), and every spectator mode -- Public and
// Omniscient -- still list no library at all. The secret order is asserted
// absent here too: the projection must match the canonical sort, never the
// library's actual order.
func TestNoVisibilityEverListsTheLibrary(t *testing.T) {
	g := visibilityBoard(t)
	ch := flatChars{g}
	for _, vis := range []Visibility{Seat, Public, Omniscient} {
		for viewer := state.PlayerID(0); viewer < 4; viewer++ {
			v := ProjectFor(g, ch, viewer, vis, nil)
			found := idsFoundIn(t, v)
			// Positive control: every seat's graveyard and exile ids are public
			// in every mode, so the walk must have reached them all — without
			// it, an empty result (a broken walk) would prove nothing.
			for seat := state.PlayerID(0); seat < 4; seat++ {
				for _, id := range g.Zone(state.ZGraveyard, seat) {
					if !found[int(id)] {
						t.Fatalf("positive control: visibility %s viewer %d does not find seat %d's graveyard id %d — the walk proves nothing", vis, viewer, seat, id)
					}
				}
				for _, id := range g.Zone(state.ZExile, seat) {
					if !found[int(id)] {
						t.Fatalf("positive control: visibility %s viewer %d does not find seat %d's exile id %d — the walk proves nothing", vis, viewer, seat, id)
					}
				}
				// Only a real seat's own library, and only in Seat mode, may be
				// listed; every other (visibility, seat) pair must not.
				ownSeat := vis == Seat && seat == viewer
				for _, id := range libIDsOf(g, seat) {
					if found[int(id)] && !ownSeat {
						t.Fatalf("visibility %s viewer %d leaks seat %d's library object %d", vis, viewer, seat, id)
					}
					if ownSeat && !found[int(id)] {
						t.Fatalf("seat visibility viewer %d does not list its own library object %d (own_library_list)", viewer, id)
					}
				}
			}
			// The own-seat list must never preserve the secret library order:
			// it is the canonical name-then-id sort of the same contents.
			if vis == Seat {
				own := v.Players[viewer].Library
				want := canonicalLibraryOrder(g, viewer)
				if len(own) != len(want) {
					t.Fatalf("seat viewer %d own library has %d cards, want %d", viewer, len(own), len(want))
				}
				for i := range want {
					if own[i].ID != want[i] {
						t.Fatalf("seat viewer %d own library[%d] = %d, want canonical %d (secret order must not leak)", viewer, i, own[i].ID, want[i])
					}
				}
			}
		}
	}
}

// canonicalLibraryOrder is the name-then-id sort the library projection must
// produce for a seat, derived independently here so the pin can catch a
// projection that echoed the library's real order.
func canonicalLibraryOrder(g *state.Game, seat state.PlayerID) []state.ObjID {
	ids := libIDsOf(g, seat)
	sort.SliceStable(ids, func(i, j int) bool {
		a, b := g.Obj(ids[i]), g.Obj(ids[j])
		if a == nil || b == nil || a.Face() == nil || b.Face() == nil {
			return ids[i] < ids[j]
		}
		if a.Face().Name != b.Face().Name {
			return a.Face().Name < b.Face().Name
		}
		return ids[i] < ids[j]
	})
	return ids
}
