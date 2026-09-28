package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The whole-hand wheel (Discard Mode$ Hand) pinned end to end on the REAL
// corpus cards. Feedback fb-20260918T072140Z-4ca8e286 ("reforge the soul
// --played by my enemy... I never discarded my hand, now have like 12
// cards"): the pre-fix engine had no Mode$ Hand case in effDiscard, so the
// wheel fell into the default front-card arm, discarded ONE card per player,
// kept the rest of the hand, and drew seven — everyone ended up with
// old-hand + 7.
//
// The helpers come from search_library_test.go (searchTestRegistry,
// searchCorpusCard, searchMoveByName), cast_test.go (addMana, submitChoices,
// toMain1) and replacement_updated_test.go (passUntilStackEmpty) — all the
// same package. The deck is built from compiled corpus cards only, so no
// Forge script text is committed. Neither Reforge the Soul nor Windfall is
// in any legacy golden deck, so no chain head depends on these cards.

// wheelEngine deals seat 0 a 40-card deck whose first card is the named
// wheel spell, then eight Forests and eight Mountains and Grizzly Bears; the
// opponent's deck is all Mountains. The seed is advanced to start seat 0.
func wheelEngine(t *testing.T, reg *cards.Registry, fixture string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, fixture)}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9204, Names: []string{"wheeler", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// handNames returns player p's hand card face names, in zone order.
func handNames(t *testing.T, e *Engine, p state.PlayerID) []string {
	t.Helper()
	out := make([]string, 0, len(e.G.Zone(state.ZHand, p)))
	for _, id := range e.G.Zone(state.ZHand, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			t.Fatalf("hand object %d missing", id)
		}
		out = append(out, o.Face().Name)
	}
	return out
}

// inGraveyard reports whether a card whose face is name sits in player p's
// graveyard.
func inGraveyard(e *Engine, p state.PlayerID, name string) bool {
	for _, id := range e.G.Zone(state.ZGraveyard, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return true
		}
	}
	return false
}

// castWheel funds the spell's cost from the pool (no tap-to-pay in this
// build), casts it from hand, and passes priority until the stack is empty —
// the wheel has no targets and its resolution asks nothing.
func castWheel(t *testing.T, e *Engine, name, mana string) {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, mana)
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	if n := passUntilStackEmpty(t, e, 20); n == 0 {
		t.Fatal("stack never drained")
	}
}

// wheelDiscards collects the log's canonical discard events.
func wheelDiscards(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if events.IsDiscard(ev) {
			out = append(out, ev)
		}
	}
	return out
}

// TestReforgeTheSoulWheelDiscardsWholeHandsAndDrawsSeven is the reported
// card: "Each player discards their hand, then draws seven cards." Both
// players' ENTIRE hands land in the graveyard (one events.Discard per card),
// the chained DBEachDraw draws exactly seven per player, and the spell
// finishes in the graveyard.
func TestReforgeTheSoulWheelDiscardsWholeHandsAndDrawsSeven(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := wheelEngine(t, reg, "Reforge the Soul")

	// Seat 0 is the starting player, so its turn-1 draw step is skipped
	// (CR 103.7a) and both hands are the opening 7.
	before0 := handNames(t, e, 0)
	before1 := handNames(t, e, 1)
	if len(before0) != 7 || len(before1) != 7 {
		t.Fatalf("pre-cast hands: seat0 %d, seat1 %d — fixture expects 7/7", len(before0), len(before1))
	}

	castWheel(t, e, "Reforge the Soul", "RRRRR")

	// Every pre-cast hand card is in its owner's graveyard.
	for _, name := range before0 {
		if !inGraveyard(e, 0, name) {
			t.Fatalf("seat 0 card %q was not discarded by the wheel", name)
		}
	}
	for _, name := range before1 {
		if !inGraveyard(e, 1, name) {
			t.Fatalf("seat 1 card %q was not discarded by the wheel", name)
		}
	}
	// Exactly 14 discard events (7 + 7), one per card.
	if got := len(wheelDiscards(e)); got != len(before0)+len(before1) {
		t.Fatalf("discard events = %d, want %d (8 + 7)", got, len(before0)+len(before1))
	}
	// Exactly seven cards drawn per player by the chained SubAbility$.
	if got := len(e.G.Zone(state.ZHand, 0)); got != 7 {
		t.Fatalf("seat 0 hand after the wheel = %d, want 7", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 7 {
		t.Fatalf("seat 1 hand after the wheel = %d, want 7", got)
	}
	// The spell itself finished in the graveyard.
	if !inGraveyard(e, 0, "Reforge the Soul") {
		t.Fatal("Reforge the Soul did not finish in the graveyard")
	}
}

// TestWindfallDiscardsWholeHandsAndRemembersThem pins the Windfall DISCARD
// half end to end: both players' entire hands are discarded (one
// events.Discard per card). The chained draw half is DELIBERATELY not pinned
// here: Windfall's SVar X (PlayerCountPlayers$HighestValidGraveyard,Library,
// Exile Card.IsRemembered+YouOwn) is unresolvable in effects/count.go —
// countZone there only knows the single-zone head spellings, so the
// comma-joined multi-zone form degrades to 0 and the draw emits Amount 0 per
// player (measured live: 14 draw events, every one Amount 0). That is a
// separate defect (filed:
// .ds4/new-tickets/windfall-multizone-count-svar-unresolvable.md, population
// 3 corpus files: windfall, whispering_madness, jaces_archivist) — pinning a
// 0-draw hand size here would enshrine it. The RememberDiscarded$ rider is
// pinned at the unit level in effects/cardflow_discard_hand_test.go
// (TestDiscardHandRememberDiscardedRiderAppliesPerCard); Windfall's own
// WindfallCleanup (DB$ Cleanup | ClearRemembered$ True) clears the source's
// persistent list AFTER the draw, so nothing durable is asserted on it here.
func TestWindfallDiscardsWholeHandsAndRemembersThem(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := wheelEngine(t, reg, "Windfall")

	// Give seat 0 one extra card (a logged library→hand move, the same
	// primitive searchMoveByName uses) so the greatest discarded count (8)
	// differs from seat 1's discard (7) — a future draw-count pin (draw == 8
	// for BOTH once the count SVar above is fixed) will discriminate. A
	// Forest is guaranteed to still be in the library (the opening hand is
	// bears and one Mountain, and turn 1's draw step is skipped for the
	// starter).
	extra := searchMoveByName(t, e, "Forest", state.ZHand)
	if extra == 0 {
		t.Fatal("no Forest to move into seat 0's hand")
	}

	before0 := len(e.G.Zone(state.ZHand, 0))
	before1 := len(e.G.Zone(state.ZHand, 1))
	if before0 != 8 || before1 != 7 {
		t.Fatalf("pre-cast hands: seat0 %d, seat1 %d — fixture expects 8/7", before0, before1)
	}

	castWheel(t, e, "Windfall", "UUU")

	if got := len(wheelDiscards(e)); got != before0+before1 {
		t.Fatalf("discard events = %d, want %d", got, before0+before1)
	}
	if !inGraveyard(e, 0, "Windfall") {
		t.Fatal("Windfall did not finish in the graveyard")
	}
	if !inGraveyard(e, 0, "Forest") {
		t.Fatal("the moved Forest was not discarded by the wheel")
	}
}
