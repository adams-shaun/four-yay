// This file is package view_test, the same external view the existing
// visibility_test.go uses, so it can reuse playSome/mustJSON to drive a real
// 4-seat game and project an omniscient view of it (seat imports view, so an
// internal package-view test file could not import seat to drive the game).
package view_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// jsonKeys is the set of JSON keys a struct type can actually emit. It is
// the source of truth, so it tracks the type's own public fields rather than
// a hand-maintained list and will catch a genuinely new field.
//
// encoding/json's rule: an EXPORTED field with an explicit json name carries
// that name; an exported field with no name in its tag (no tag at all, or a
// bare ",omitempty") marshals under its Go field NAME. Only an explicit
// json:"-" suppresses a field, and unexported fields never marshal at all.
// So the two things skipped here are exactly unexported fields and explicit
// "-" — a newly added untagged exported field is still a leak carrier and
// must appear under its field name.
func jsonKeys(typ reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue // unexported: encoding/json never marshals it
		}
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "-" {
			continue // explicit suppression only
		}
		if key == "" {
			key = f.Name // untagged (or unnamed-tag) exported field → field name
		}
		keys[key] = true
	}
	return keys
}

// assertKeysClosed pins a type's JSON key set to exactly want: any key the
// type can emit that is not allowed is a new leak carrier (the god-view
// defect D6 exists to keep off the wire), and any wanted key the type lost
// is a regression in what a client can see. The two-way check makes this an
// allowlist, not a blocklist.
func assertKeysClosed(t *testing.T, typeName string, typ reflect.Type, want map[string]bool) {
	t.Helper()
	got := jsonKeys(typ)
	for key := range got {
		if !want[key] {
			t.Errorf("%s carries a JSON key %q — a new key that could carry a hidden zone fails review", typeName, key)
		}
	}
	for key := range want {
		if !got[key] {
			t.Errorf("%s lost JSON key %q", typeName, key)
		}
	}
}

// TestViewMarshalsClosed is the god-view structural guarantee, in two halves.
//
// First it pins the View type's JSON key SET — its exact public fields — to
// the closure D6 allows and nothing else. A brand-new field that could carry
// a hidden-zone order (a raw "library" list, or any other hidden-zone
// carrier) fails here before it can ship to a client, even when it is
// currently nil (omitempty would hide it from any single marshal). View's 15
// fields are exactly {viewer, visibility, turn, round, step, phase, active,
// priority, over, draw, winner, players, stack, pending, decision}. The same
// allowlist treatment is applied to PlayerView (the type that actually
// carries hand/pool/library_size — the leak surface D6 most protects against
// — 16 fields, four of them this Task's public commander facts) and
// StackView (8 fields, 10 with Ruling VW-1's optional/decider).
//
// Second it marshals a real OMniscient projection — the widest-everywhere
// view, the one a leaked endpoint would send — and asserts that what is
// actually present carries every hand and every mana pool (proving the test
// is exercising a genuinely omniscient god view) while never carrying a raw
// library order field. (decision is legitimately absent from that marshal:
// an omniscient view is for watching, not acting, so its Decision is always
// nil and omitempty drops the key.)
func TestViewMarshalsClosed(t *testing.T) {
	// Half 1: each of the three wire types' JSON keys is exactly its
	// allowlist. Reflect over the types themselves so the set tracks the
	// source of truth (the fields/tags), not a hand-maintained duplicate.
	assertKeysClosed(t, "view.View", reflect.TypeOf(view.View{}), map[string]bool{
		"viewer": true, "visibility": true, "turn": true, "round": true, "step": true,
		"phase": true, "active": true, "priority": true, "over": true,
		"draw": true, "winner": true, "players": true, "stack": true,
		"pending": true, "decision": true,
	})
	// round (added by task ui12) is a projection of the public turn counter
	// and the public per-seat lost flags -- how many full passes the surviving
	// seats have made -- not a hidden-zone carrier, so it joins the public
	// facts here rather than the hidden-zone class this test guards against.
	// PlayerView is where the hidden zones actually live, so it gets the
	// same strict allowlist View does — a future LibraryTop/Sideboard/
	// Revealed/untagged Deck here fails even when it is not one of the two
	// names this test used to spot-check. The four commander fields added
	// by Task m37 (command/commanders/commander_casts/cmd_damage) carry
	// public-for-every-seat facts — the command zone, the roster, the CR
	// 903.8 cast counts and the CR 903.10 clock — so they join the other
	// public zones here, not the hidden-zone class the test guards against.
	assertKeysClosed(t, "view.PlayerView", reflect.TypeOf(view.PlayerView{}), map[string]bool{
		"seat": true, "name": true, "life": true, "lost": true,
		"library_size": true, "hand_size": true, "graveyard_size": true,
		"hand": true, "battlefield": true, "graveyard": true, "exile": true,
		// planar_deck is a face-down hidden-zone projection; view.Project
		// strips every face-down card's identity even from its owner.
		"planar_deck": true,
		"pool":        true, "command": true, "commanders": true,
		"commander_casts": true, "cmd_damage": true,
		// available (task mp1) is a public battlefield-derived quantity —
		// what the seat could tap for right now — never a hidden-zone
		// carrier, so it joins the public facts class here. See the
		// Pool/available doc on view.PlayerView for how the two differ.
		"available": true,
		// library_top (task inbox-paramcensus-static-grant-misc) IS a hidden-
		// zone carrier — exactly the class this allowlist exists to gate —
		// admitted deliberately: it is the Continuous MayLookAt grant's
		// reveal (Oracle of Mul Daya), projected only for the grant's own
		// controller's seat (Engine.MayLookAtLibraryTop evaluates the
		// Affected$ spec), never a raw library order and never for another
		// seat. The grant-gated reveal rules/static_grants_test.go pins the
		// seat scoping end to end.
		"library_top": true,
		// potential_actions (rv2c) is DERIVED from the seat's own hidden zones
		// (the legal-offer walk reads the seat's hand, command zone and
		// graveyard), which is exactly why it is gated on "is this the
		// viewer's own seat" in project(), like Hand above — for every other
		// seat it is never filled and omitempty drops the key. The payload
		// itself carries only action kinds and object ids, never a card list.
		"potential_actions": true,
		// pool_restrictions (task fb-20260922T145544Z) annotates Pool: it is
		// derived from a public battlefield permanent's own mana ability (the
		// CR 106.4a/106.4b announcement the Pool field already documents), so
		// like available it is a public fact projected for every seat under
		// every visibility, never a hidden-zone carrier. It names a colour, an
		// amount and a spend text; it never carries a card list.
		"pool_restrictions": true,
	})
	// StackView is public (R3) so it is a lesser leak surface, but the
	// reflection is the same shape and cheap, so it is pinned too. The two
	// Ruling VW-1 keys (optional/decider) are public facts — an optional
	// triggered ability on the stack reports that it is optional and who
	// answers it — never a hidden-zone carrier, so they join the public
	// facts class here rather than the hidden-zone class the test guards.
	assertKeysClosed(t, "view.StackView", reflect.TypeOf(view.StackView{}), map[string]bool{
		"id": true, "kind": true, "name": true, "text": true,
		"controller": true, "source": true, "targets": true, "card": true,
		"optional": true, "decider": true,
	})

	// Half 2: the omniscient projection this type produces actually shows
	// the hidden zones (so the closure is being exercised, not vacuously
	// passing) yet never a raw library order.
	e := playSome(t, 5, 60)
	v := view.ProjectFor(e.G, e, view.NoSeat, view.Omniscient, e.Pending())
	m := map[string]any{}
	if err := json.Unmarshal([]byte(mustJSON(t, v)), &m); err != nil {
		t.Fatalf("omniscient View does not unmarshal to a JSON object: %v", err)
	}
	for key := range m {
		if !jsonKeys(reflect.TypeOf(view.View{}))[key] {
			t.Errorf("omniscient view JSON carries unexpected key %q", key)
		}
	}
	players, ok := m["players"].([]any)
	if !ok || len(players) == 0 {
		t.Fatalf("omniscient view has no players to inspect")
	}
	for i, rawp := range players {
		p, ok := rawp.(map[string]any)
		if !ok {
			t.Fatalf("players[%d] is not an object", i)
		}
		if p["hand"] == nil || p["pool"] == nil {
			t.Fatalf("omniscient player %d omitted hand or pool — not a genuinely omniscient view", i)
		}
		// The library must appear only as a size count, never as content.
		if _, has := p["library"]; has {
			t.Errorf("player %d carries a raw \"library\" order field in the omniscient view", i)
		}
		if _, has := p["library_order"]; has {
			t.Errorf("player %d carries a \"library_order\" field in the omniscient view", i)
		}
		// And the zone content that IS shown never names a hidden card's
		// identity beyond the counts the type allows.
		if lib := p["library_size"]; lib == nil {
			t.Errorf("player %d has no library_size count", i)
		}
	}
	// The projection really was omniscient: every seat has non-empty hand
	// content (a real game has cards in hand) whose sizes match the game.
	for _, p := range v.Players {
		if len(p.Hand) != len(e.G.Zone(state.ZHand, p.ID)) {
			t.Errorf("seat %d omniscient hand size %d != game hand %d", p.ID, len(p.Hand), len(e.G.Zone(state.ZHand, p.ID)))
		}
	}
}
