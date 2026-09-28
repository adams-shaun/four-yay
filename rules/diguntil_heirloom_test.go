package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The sharesCreatureTypeWith end-to-end pin on Heirloom Blade's REAL
// compiled corpus card: the equipped creature dies → the OptionalDecider$
// trigger's CR 603.5 resolution-time ask is answered "yes" → the DB$ DigUntil
// scan (Valid$ Creature.sharesCreatureTypeWith TriggeredCardLKICopy) reveals
// the library's top until a creature sharing a type with the DEAD card, puts
// it in hand, and returns the revealed rest to the bottom in a seeded-random
// order (RevealRandomOrder$ True). The no-match library case reveals
// everything and bottoms it in a fresh random order — the CORRECT reading of
// "reveal ... until you reveal" over a library that never satisfies the stop
// condition, asserted as such. Heirloom Blade is in NO repo deck and NO
// legacy golden deck, so no chain head depends on this card.
//
// The helpers come from search_library_test.go (same package); the deck is
// built from compiled corpus cards only, so no Forge script text is
// committed here either.

// heirloomTestEngine deals seat 0 a 40-card deck containing Heirloom Blade,
// Balduvian Bears (the dying bearer), Hill Giant (a NON-matching creature
// that must be turned over first — a first-card match would pass by
// coincidence) and, when withMatch, Bear Cub (the match). It puts the Bears
// on the battlefield, the Blade in hand, reorders the library to a KNOWN
// exact order [Hill Giant, Bear Cub, ...rest], casts and equips the Blade,
// and returns everything the tests need (matchID is 0 when !withMatch).
func heirloomTestEngine(t *testing.T, reg *cards.Registry, withMatch bool) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	bear := searchCorpusCard(t, reg, "Balduvian Bears")
	blade := searchCorpusCard(t, reg, "Heirloom Blade")
	giant := searchCorpusCard(t, reg, "Hill Giant")
	murder := searchCorpusCard(t, reg, "Murder")
	deck := make([]*cards.Card, 0, 40)
	if withMatch {
		deck = append(deck, searchCorpusCard(t, reg, "Bear Cub"))
	}
	deck = append(deck, giant, blade, bear, murder)
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, forest, mountain)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9208, Names: []string{"heirloom", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	bearID := searchMoveByName(t, e, "Balduvian Bears", state.ZBattlefield)
	bladeID := searchMoveByName(t, e, "Heirloom Blade", state.ZHand)
	searchMoveByName(t, e, "Murder", state.ZHand)
	giantID := searchMoveByName(t, e, "Hill Giant", state.ZLibrary)
	cubID := state.ObjID(0)
	if withMatch {
		cubID = searchMoveByName(t, e, "Bear Cub", state.ZLibrary)
	}
	// Known exact library order: [Hill Giant, Bear Cub, rest...]. Nothing
	// draws between this reorder and the Murder resolution (the whole setup
	// stays inside turn 1's Main1), so the dig's scan sees exactly this.
	lib := e.G.Zone(state.ZLibrary, 0)
	order := []state.ObjID{giantID}
	if withMatch {
		order = append(order, cubID)
	}
	for _, id := range lib {
		if id != giantID && id != cubID {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order})
	// Cast the Blade ({3}: the pool's four green pips cover three generic and
	// nothing else) and let it enter the battlefield.
	addMana(t, e, 0, "GGGG")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bladeID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Heirloom Blade: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	// Equip {1} onto the Bears — the minted Attach AB is the face's only
	// activated ability (find it by API, not index).
	equipIdx := -1
	if o := e.G.Obj(bladeID); o == nil || o.Face() == nil {
		t.Fatalf("Heirloom Blade missing after cast")
	} else {
		for i, sa := range o.Face().Abilities {
			if sa.Kind == "AB" && sa.API == "Attach" {
				equipIdx = i
			}
		}
	}
	if equipIdx < 0 {
		t.Fatalf("Heirloom Blade has no Equip Attach AB: %+v", e.G.Obj(bladeID).Face().Abilities)
	}
	addMana(t, e, 0, "G")
	opt := abilityOption(t, e, bladeID, equipIdx)
	submitChoices(t, e, opt.Index)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != bearID {
		t.Fatalf("equip target = %+v, want exactly the Bears %d", d, bearID)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bladeID); o.AttachedTo != bearID {
		t.Fatalf("Heirloom Blade AttachedTo = %d, want the Bears %d", o.AttachedTo, bearID)
	}
	return e, cfg, bearID, bladeID, cubID, giantID
}

// heirloomMurderBearer casts Murder ({1}{B}{B}) on the equipped Bears and
// drives to the Blade's resolution-time optional-trigger ask (the dying card
// is the trigger's TriggeredCardLKICopy). It returns the ask.
func heirloomMurderBearer(t *testing.T, e *Engine, murderID, bearID state.ObjID) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, "BBB")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == murderID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Murder: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != bearID {
		t.Fatalf("Murder target = %+v, want exactly the Bears %d", d, bearID)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("after the Bears died: %+v, want the optional-trigger ask", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("optional-trigger options = %+v, want a yes/no pair", d.Options)
	}
	return d
}

// findHandCard returns seat 0's hand card with the given face name.
func findHandCard(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("%q absent from seat 0's hand", name)
	return 0
}

// mustBearer returns seat 0's battlefield Balduvian Bears (the equipped
// bearer). Only one exists in these fixtures.
func mustBearer(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Balduvian Bears" {
			return id
		}
	}
	t.Fatalf("Balduvian Bears absent from the battlefield")
	return 0
}

// countPublicRevealNote counts the public reveal Notes naming exactly the
// given ids in order (the diguntil reveal's one non-Secret ids-Note).
func countPublicRevealNote(e *Engine, ids []state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note || ev.Secret || ev.Player != 0 || len(ev.IDs) != len(ids) {
			continue
		}
		same := true
		for i := range ids {
			if ev.IDs[i] != ids[i] {
				same = false
				break
			}
		}
		if same {
			n++
		}
	}
	return n
}

// TestHeirloomBladeDigUntilSharesCreatureTypeWithTheDeadBearer is the
// match branch: the dead Bears' Bear type matches Bear Cub — but NOT before
// the Hill Giant is turned over first. The Cub reaches hand; the revealed
// Giant returns to the bottom (a one-card pile has no order to shuffle).
func TestHeirloomBladeDigUntilSharesCreatureTypeWithTheDeadBearer(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, _, _, cubID, giantID := heirloomTestEngine(t, reg, true)
	d := heirloomMurderBearer(t, e, findHandCard(t, e, "Murder"), mustBearer(t, e))
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if before[0] != giantID || before[1] != cubID {
		t.Fatalf("library head = [%d %d ...], want [%d %d ...]", before[0], before[1], giantID, cubID)
	}
	submitChoices(t, e, d.Options[0].Index) // "yes"
	passUntilStackEmpty(t, e, 20)
	// One public reveal Note naming BOTH turned-over cards (the non-match
	// first, then the match).
	if n := countPublicRevealNote(e, []state.ObjID{giantID, cubID}); n != 1 {
		t.Fatalf("public reveal Notes naming [giant, cub] = %d, want 1", n)
	}
	// The Cub is in hand.
	if o := e.G.Obj(cubID); o.Zone != state.ZHand {
		t.Fatalf("Bear Cub zone = %s, want hand (FoundDestination$ Hand)", o.Zone)
	}
	// The library: the tail (everything after the Cub) keeps its order on
	// top; the revealed non-match sits alone at the bottom.
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

// TestHeirloomBladeDigUntilNoMatchRevealsAndBottomsTheWholeLibrary is the
// no-match branch: with no Bear in the library the reveal-until runs to the
// END of the library, every card is publicly revealed IN SCAN ORDER, and the
// whole library returns to the bottom in a fresh seeded-random order
// (RevealRandomOrder$ True) — the CORRECT CR reading, asserted as such, no
// special case.
func TestHeirloomBladeDigUntilNoMatchRevealsAndBottomsTheWholeLibrary(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, _, _, _, giantID := heirloomTestEngine(t, reg, false)
	d := heirloomMurderBearer(t, e, findHandCard(t, e, "Murder"), mustBearer(t, e))
	before := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	if before[0] != giantID {
		t.Fatalf("library head = %d, want the Hill Giant %d", before[0], giantID)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, d.Options[0].Index) // "yes"
	passUntilStackEmpty(t, e, 20)
	// One public reveal Note naming EVERY library card in revealed (scan) order.
	if n := countPublicRevealNote(e, before); n != 1 {
		t.Fatalf("public reveal Notes naming the whole library = %d, want 1", n)
	}
	// Nothing was found, so the hand did not grow.
	if handAfter := len(e.G.Zone(state.ZHand, 0)); handAfter != handBefore {
		t.Fatalf("hand size %d -> %d, want unchanged (nothing found)", handBefore, handAfter)
	}
	after := e.G.Zone(state.ZLibrary, 0)
	if len(after) != len(before) {
		t.Fatalf("library length %d -> %d, want unchanged", len(before), len(after))
	}
	// The returned pile is exactly a permutation of the pre-resolution
	// library, it occupies the whole (bottom) segment, and the random return
	// differs from the pre-resolution order (the mutation-failing assertion:
	// the old existing-order stand-in reproduced `before` exactly).
	seen := map[state.ObjID]bool{}
	for _, id := range before {
		seen[id] = true
	}
	same := true
	for i := range before {
		if !seen[after[i]] {
			t.Fatalf("library[%d] = %d is not a permutation of the pre-resolution library %v", i, after[i], before)
		}
		if after[i] != before[i] {
			same = false
		}
	}
	if same {
		t.Fatalf("library returned in its existing order %v; RevealRandomOrder$ must shuffle the bottom return", after)
	}
	// Deterministic: the same fixture and seed produce the same order. Build
	// it a second time from the same Config and compare order-for-order.
	e2, cfg2, _, _, _, _ := heirloomTestEngine(t, reg, false)
	if cfg2.Seed != cfg.Seed {
		t.Fatalf("second fixture seed = %d, want %d", cfg2.Seed, cfg.Seed)
	}
	d2 := heirloomMurderBearer(t, e2, findHandCard(t, e2, "Murder"), mustBearer(t, e2))
	submitChoices(t, e2, d2.Options[0].Index) // "yes"
	passUntilStackEmpty(t, e2, 20)
	after2 := e2.G.Zone(state.ZLibrary, 0)
	if len(after2) != len(after) {
		t.Fatalf("second run library length = %d, want %d", len(after2), len(after))
	}
	for i := range after {
		if after2[i] != after[i] {
			t.Fatalf("second run library[%d] = %d, want %d (seeded RNG must replay)", i, after2[i], after[i])
		}
	}
	replayCheck(t, e, cfg)
}
