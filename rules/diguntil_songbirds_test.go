package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The diguntil1 end-to-end pin on Songbirds' Blessing's REAL compiled corpus
// card: enchanted creature attacks → the reveal-until trigger resolves → the
// optional put/hand ask is posed → the answer (or the R-9 decline on a
// no-host is the effects-side leaf's business; here the answered branches)
// moves the found Aura, the revealed rest go to the bottom, and the
// library's tail order is asserted exactly. Songbirds' Blessing is in NO
// repo deck and NO legacy golden deck, so no chain head depends on this
// card (measured: grepping every DigUntil carrier name against
// internal/testutil/decks/*.json returns nothing).
//
// The helpers come from search_library_test.go (same package): the deck is
// built from compiled corpus cards only, so no Forge script text is
// committed here either.

// songbirdsTestEngine deals seat 0 a 40-card deck containing Songbirds'
// Blessing, Grizzly Bears and Holy Strength among the basics, puts the Bear
// on the battlefield, Songbirds in hand, casts the Aura onto the Bear, and
// reorders seat 0's library to a KNOWN exact order whose second card is the
// Holy Strength — so the reveal-until scan turns over a non-matching card
// first and the match is NOT the first card (a first-card match would pass
// by coincidence). It returns (engine, config, aura-id-in-library,
// first-card-id, bear-id).
func songbirdsTestEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, "Songbirds' Blessing"),
		bear,
		searchCorpusCard(t, reg, "Holy Strength"),
		searchCorpusCard(t, reg, "Island"),
	}
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
	cfg := seatZeroStart(Config{Seed: 9204, Names: []string{"songbirds", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	bearID := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	songbirdsID := searchMoveByName(t, e, "Songbirds' Blessing", state.ZHand)
	// The Holy Strength must be IN the library for the reveal scan: if the
	// opening hand took it, put it back at the bottom first.
	lib := e.G.Zone(state.ZLibrary, 0)
	auraID := state.ObjID(0)
	for _, id := range lib {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Holy Strength" {
			auraID = id
		}
	}
	if auraID == 0 {
		auraID = searchMoveByName(t, e, "Holy Strength", state.ZLibrary)
		lib = e.G.Zone(state.ZLibrary, 0)
	}
	// Known exact order: [draw-fodder, x, aura, rest...] — turn 3's draw
	// step removes the top card before the attack, so at scan time the top
	// is [x, aura, ...]: the scan reveals x (no match), then the aura
	// (match), and stops; the tail is never turned over. The match is NOT
	// the first card, so a first-card match cannot pass by coincidence.
	first := lib[0]
	if first == auraID {
		first = lib[1]
	}
	fodder := lib[0]
	if fodder == first || fodder == auraID {
		fodder = lib[2]
	}
	order := []state.ObjID{fodder, first, auraID}
	for _, id := range lib {
		if id != first && id != auraID {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order})
	// Cast the Aura onto the Bear ({3}{W}: the pool covers three generic and
	// the white pip), and let it resolve and attach.
	addMana(t, e, 0, "WWWW")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == songbirdsID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Songbirds' Blessing: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d = e.Pending(); d != nil && d.Kind == decision.KTarget {
		// The Bear is the only eligible creature on either battlefield.
		if len(d.Options) != 1 || d.Options[0].Obj != bearID {
			t.Fatalf("target options = %+v, want exactly the Bear", d.Options)
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(songbirdsID); o.AttachedTo != bearID {
		t.Fatalf("Songbirds' Blessing AttachedTo = %d, want the Bear %d", o.AttachedTo, bearID)
	}
	return e, cfg, auraID, first, bearID
}

// driveSongbirdsToAttack drives to turn 3's declare-attackers step, attacks
// with the enchanted Bear, and returns the DigUntil optional-move ask (the
// first non-priority decision after the attack — the trigger resolves
// mid-step, before blockers).
func driveSongbirdsToAttack(t *testing.T, e *Engine, bearID state.ObjID) *decision.Decision {
	t.Helper()
	// Turn 2 is the OPPONENT's turn in this 2-seat game; the Bear's own next
	// turn — the first it can attack — is turn 3.
	driveToStepAll(t, e, 3, 0, state.StepDeclareAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("pending = %+v, want the attackers decision", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the enchanted Bear was not offered as an attacker: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "diguntil_move" {
		t.Fatalf("after the attack: %+v, want the DigUntil optional-move KChoose", d)
	}
	if d.Player != 0 {
		t.Fatalf("ask player = %d, want the library's owner (0)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("ask options = %+v, want a yes/no pair", d.Options)
	}
	return d
}

// TestSongbirdsBlessingAttackRevealsUntilAuraPutsItOnTheBattlefield is the
// "yes" branch: the Aura enters the battlefield attached to the bearer, the
// revealed non-matching card goes to the bottom, and the tail keeps its
// order.
func TestSongbirdsBlessingAttackRevealsUntilAuraPutsItOnTheBattlefield(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, auraID, first, bearID := songbirdsTestEngine(t, reg)
	d := driveSongbirdsToAttack(t, e, bearID)
	// The library is captured at ASK time: turn 3's draw already removed the
	// fodder card from the top.
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	submitChoices(t, e, d.Options[0].Index) // "yes"
	// The reveal Note is public and names BOTH turned-over cards (the
	// non-match first, then the Aura).
	reveal := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && ev.Player == 0 &&
			len(ev.IDs) == 2 && ev.IDs[0] == first && ev.IDs[1] == auraID {
			reveal++
		}
	}
	if reveal != 1 {
		t.Fatalf("public reveal Notes = %d, want 1 naming [first, aura]", reveal)
	}
	// The Aura is on the battlefield, attached to the Bear.
	if o := e.G.Obj(auraID); o.Zone != state.ZBattlefield || o.AttachedTo != bearID {
		t.Fatalf("Aura zone/attach = %s/%d, want battlefield/%d", o.Zone, o.AttachedTo, bearID)
	}
	// The library: the tail (everything after the Aura) keeps its order on
	// top; the revealed non-match sits at the bottom.
	after := e.G.Zone(state.ZLibrary, 0)
	want := append(append([]state.ObjID(nil), before[2:]...), before[0])
	if len(after) != len(want) {
		t.Fatalf("library length = %d, want %d", len(after), len(want))
	}
	for i := range want {
		if after[i] != want[i] {
			t.Fatalf("library[%d] = %d, want %d (full after: %v, want %v)", i, after[i], want[i], after, want)
		}
	}
	replayCheck(t, e, cfg)
}

// TestSongbirdsBlessingAttackDeclinePutsTheAuraInTheHand is the "no"
// branch: the declined Aura goes to OptionalNoDestination$ Hand; the
// revealed rest and the tail are the same as the "yes" branch.
func TestSongbirdsBlessingAttackDeclinePutsTheAuraInTheHand(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, auraID, _, bearID := songbirdsTestEngine(t, reg)
	d := driveSongbirdsToAttack(t, e, bearID)
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	submitChoices(t, e, d.Options[1].Index) // "no"
	if o := e.G.Obj(auraID); o.Zone != state.ZHand {
		t.Fatalf("declined Aura zone = %s, want hand (OptionalNoDestination$ Hand)", o.Zone)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Bear zone = %v, want still on the battlefield", e.G.Obj(bearID))
	}
	after := e.G.Zone(state.ZLibrary, 0)
	want := append(append([]state.ObjID(nil), before[2:]...), before[0])
	if len(after) != len(want) {
		t.Fatalf("library length = %d, want %d", len(after), len(want))
	}
	for i := range want {
		if after[i] != want[i] {
			t.Fatalf("library[%d] = %d, want %d (full after: %v, want %v)", i, after[i], want[i], after, want)
		}
	}
	replayCheck(t, e, cfg)
}
