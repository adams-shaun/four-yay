package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Squad Rallier's Oracle text is the RestRandomOrder$ contract:
//
//	{2}{W}: Look at the top four cards of your library. You may reveal a
//	creature card with power 2 or less from among them and put it into your
//	hand. Put the rest on the bottom of your library in a random order.
//
// Before task fdn-dig-rest-random-order the untaken window cards went to the
// bottom in the offered order (and, for a >=2 remainder, the controller was
// asked to arrange them). This file pins the fix end to end on the real corpus
// card: the keep goes to the hand, the other three are the bottom three in an
// order drawn from the engine's seeded generator (never the offered order for
// at least one seed), no arrange decision is posed, and the game replays to
// the same chain head.

// squadRallierDigIndex returns the face-ability index of Squad Rallier's Dig
// ability and checks the corpus shape the test relies on. Squad Rallier's Dig
// is an activated ability (A:AB$), so it lives in the face's Abilities list.
func squadRallierDigIndex(t *testing.T) int {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	rallier, ok := reg.Lookup("Squad Rallier")
	if !ok {
		t.Fatal("Squad Rallier missing from the corpus")
	}
	idx := -1
	for i, ab := range rallier.Faces[0].Abilities {
		if ab.API == "Dig" {
			idx = i
			sa := ab
			if sa.Params["DigNum"] != "4" || sa.Params["ChangeNum"] != "1" ||
				sa.Params["ChangeValid"] != "Creature.powerLE2" ||
				sa.Params["DestinationZone"] != "Hand" ||
				sa.Params["DestinationZone2"] != "Library" || sa.Params["LibraryPosition"] != "-1" ||
				sa.Params["RestRandomOrder"] != "True" {
				t.Fatalf("Squad Rallier corpus shape drifted: %+v", sa.Params)
			}
		}
	}
	if idx < 0 {
		t.Fatalf("Squad Rallier face has no Dig ability: %+v", rallier.Faces[0].Abilities)
	}
	if !strings.Contains(rallier.Faces[0].Oracle, "Put the rest on the bottom of your library in a random order") {
		t.Fatal("Squad Rallier Oracle no longer states the random-order remainder")
	}
	return idx
}

// squadRallierEngine builds a two-seat engine whose seat 0 has the real corpus
// Squad Rallier ON THE BATTLEFIELD, ready to activate (the caller funds mana),
// and whose library the caller then pins through digReorder. Both decks are
// otherwise all Forests, and the deck holds exactly one Grizzly Bears, so a
// pinned window of [Bears, Forest, Forest, Forest] is guaranteed.
func squadRallierEngine(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	squadRallierDigIndex(t)
	rallier, _ := reg.Lookup("Squad Rallier")
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("Forest missing from the corpus")
	}
	bears, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("Grizzly Bears missing from the corpus")
	}
	if bears.Faces[0].PT != "2/2" {
		t.Fatalf("Grizzly Bears PT = %q, want 2/2 (the test's eligible-window assumption)", bears.Faces[0].PT)
	}
	filler := func(n int) []*cards.Card {
		out := make([]*cards.Card, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, forest)
		}
		return out
	}
	cfg := Config{Seed: seed, Tokens: reg.Tokens,
		Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{rallier, bears}, filler(37)...),
			filler(40),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// Put Squad Rallier onto the battlefield through a logged move (never a
	// direct state write): find it in the opening hand or library and emit the
	// MoveZone, then re-drive the priority snapshot so the activation option is
	// offered.
	id := findInZones(t, e, 0, "Squad Rallier")
	if id == 0 {
		t.Fatal("Squad Rallier not found in seat 0's hand or library")
	}
	from := state.ZHand
	for _, cand := range e.G.Zone(state.ZLibrary, 0) {
		if cand == id {
			from = state.ZLibrary
		}
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield, Player: 0})
	e.pending = nil
	e.priorityRound()
	return e, cfg
}

// findInZone returns the id of the first card with the given face name in the
// seat's zone.
func findInZone(t *testing.T, e *Engine, p state.PlayerID, z state.Zone, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(z, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("%q not found in seat %d's %s zone", name, p, z)
	return 0
}

// activateSquadRallier funds {2}{W} on seat 0 and submits the real "ability"
// activation option for Squad Rallier (already on the battlefield), then drains
// to the non-priority decision the Dig poses. It returns the Dig ability index.
func activateSquadRallier(t *testing.T, e *Engine) int {
	t.Helper()
	idx := squadRallierDigIndex(t)
	id := findInZone(t, e, 0, state.ZBattlefield, "Squad Rallier")
	addMana(t, e, 0, "WWW")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority after funding Squad Rallier: %+v", d)
	}
	ability := -1
	for _, opt := range d.Options {
		if opt.Kind == "ability" && opt.Obj == id && opt.Ability == idx {
			ability = opt.Index
		}
	}
	if ability < 0 {
		t.Fatalf("no ability option for Squad Rallier [%d]: %+v", idx, d.Options)
	}
	submitChoices(t, e, ability)
	return idx
}

// TestSquadRallierBottomsTheRestInARandomOrder is the core leaf: with a pinned
// top-4 of one eligible 2/2 and three Forests, taking the 2/2 puts it into the
// hand, the three Forests close the library, NO arrange decision is posed, and
// the bottom order is not the offered order -- proving the shuffle ran.
func TestSquadRallierBottomsTheRestInARandomOrder(t *testing.T) {
	t.Parallel()
	// Scan seeds until one shows the shuffle: an unshuffled remainder leaves
	// the bottom three in offered (window) order, so a difference is direct
	// evidence the Fisher-Yates ran. At most 3! = 6 orders exist, so among 16
	// seeds at least one must differ from the offered order.
	var sawDifference bool
	var checked int
	for seed := uint64(100); seed < 116; seed++ {
		e, cfg := squadRallierEngine(t, seed)
		libBefore := digReorder(t, e, "Grizzly Bears")
		if len(libBefore) < 5 {
			t.Fatalf("seed %d: library %d cards, need a window plus a below-window tail", seed, len(libBefore))
		}
		top := append([]state.ObjID(nil), libBefore[:4]...)
		belowWindow := append([]state.ObjID(nil), libBefore[4:]...)
		bears := top[0]
		if e.G.Obj(bears).Face().Name != "Grizzly Bears" {
			t.Fatalf("seed %d: top card = %s, want Grizzly Bears pinned first", seed, e.G.Obj(bears).Face().Name)
		}
		offered := top[1:] // the three Forests in offered (window) order

		activateSquadRallier(t, e)
		ask := passUntilNonPriority(t, e, 20)
		if ask == nil || ask.Kind != decision.KChoose || len(ask.Options) != 1 || ask.Options[0].Kind != "dig" {
			t.Fatalf("seed %d: take ask = %+v, want a one-option KChoose (the eligible 2/2)", seed, ask)
		}
		if ask.Options[0].Obj != bears {
			t.Fatalf("seed %d: take option = %v, want the pinned Grizzly Bears %v", seed, ask.Options[0].Obj, bears)
		}
		submitChoices(t, e, 0) // take the 2/2

		// No order ask: the controller never sees the bottom order.
		if arr := e.Pending(); arr != nil && arr.Kind == decision.KArrange {
			t.Fatalf("seed %d: an arrange/order decision was posed: %+v", seed, arr)
		}
		passUntilStackEmpty(t, e, 20)

		// Precondition the assertions depend on: the take landed, and the
		// remainder is exactly the three non-matching Forests.
		if o := e.G.Obj(bears); o == nil || o.Zone != state.ZHand {
			t.Fatalf("seed %d: taken card zone = %v, want Hand", seed, o.Zone)
		}
		libAfter := e.G.Zone(state.ZLibrary, 0)
		if len(libAfter) != len(belowWindow)+3 {
			t.Fatalf("seed %d: library %d cards, want %d", seed, len(libAfter), len(belowWindow)+3)
		}
		for i, oid := range belowWindow {
			if libAfter[i] != oid {
				t.Fatalf("seed %d: library[%d] = %v, want %v (the below-window cards surfaced unchanged)", seed, i, libAfter[i], oid)
			}
		}
		bottom := libAfter[len(libAfter)-3:]
		// The bottom three are exactly the three offered Forests (as a set).
		seen := map[state.ObjID]bool{}
		for _, oid := range bottom {
			seen[oid] = true
			if e.G.Obj(oid).Face().Name != "Forest" {
				t.Fatalf("seed %d: bottom card = %s, want a Forest", seed, e.G.Obj(oid).Face().Name)
			}
		}
		for _, oid := range offered {
			if !seen[oid] {
				t.Fatalf("seed %d: offered card %v missing from the bottom three %v", seed, oid, bottom)
			}
		}
		// The log-only rebuild reproduces the live game exactly (the strongest
		// in-package replay check: rules cannot import the replay package).
		replayCheck(t, e, cfg)
		checked++
		differs := false
		for i := range bottom {
			if bottom[i] != offered[i] {
				differs = true
			}
		}
		if differs {
			sawDifference = true
		}
	}
	if checked == 0 {
		t.Fatal("no seed ran: the loop's setup is vacuous")
	}
	if !sawDifference {
		t.Fatalf("across %d seeds the bottom order always matched the offered order: the remainder was never shuffled", checked)
	}
}

// TestSquadRallierRandomRestIsSeededAndReplaysExactly re-runs the SAME seed
// twice through independent engines and requires the identical bottom order
// and chain head -- the replay-exactness the seeded generator promises.
func TestSquadRallierRandomRestIsSeededAndReplaysExactly(t *testing.T) {
	t.Parallel()
	run := func() ([]state.ObjID, string) {
		e, cfg := squadRallierEngine(t, 707)
		libBefore := digReorder(t, e, "Grizzly Bears")
		if len(libBefore) < 5 {
			t.Fatalf("library %d cards, need a window plus a tail", len(libBefore))
		}
		bears := libBefore[0]
		if e.G.Obj(bears).Face().Name != "Grizzly Bears" {
			t.Fatalf("top card = %s, want Grizzly Bears pinned first", e.G.Obj(bears).Face().Name)
		}
		activateSquadRallier(t, e)
		ask := passUntilNonPriority(t, e, 20)
		if ask == nil || ask.Kind != decision.KChoose || len(ask.Options) == 0 || ask.Options[0].Obj != bears {
			t.Fatalf("take ask = %+v, want the eligible Grizzly Bears %v", ask, bears)
		}
		submitChoices(t, e, 0)
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(bears); o == nil || o.Zone != state.ZHand {
			t.Fatalf("taken card zone = %v, want Hand", o.Zone)
		}
		libAfter := e.G.Zone(state.ZLibrary, 0)
		bottom := append([]state.ObjID(nil), libAfter[len(libAfter)-3:]...)
		// Precondition the determinism check depends on: the three untaken
		// window cards really are the bottom three (so the compared list is the
		// shuffled remainder, not an arbitrary library tail).
		want := map[state.ObjID]bool{libBefore[1]: true, libBefore[2]: true, libBefore[3]: true}
		for _, oid := range bottom {
			if !want[oid] {
				t.Fatalf("bottom card %v is not one of the untaken window cards %v", oid, want)
			}
		}
		replayCheck(t, e, cfg)
		return bottom, e.L.Head()
	}
	b1, h1 := run()
	b2, h2 := run()
	for i := range b1 {
		if b1[i] != b2[i] {
			t.Fatalf("bottom order differs between identical seeds: %v vs %v", b1, b2)
		}
	}
	if h1 != h2 {
		t.Fatalf("chain head differs between identical seeds: %s vs %s", h1, h2)
	}
}

// TestSquadRallierRestRandomOrderEmitsOneLibraryOrder pins the event record:
// the shuffle is one Secret LibraryOrder carrying the complete reordered
// library (the same single-event record an answered arrange emits), so a log
// reader cannot mistake the random bottom for a player-chosen order and no
// per-card order event leaks.
func TestSquadRallierRestRandomOrderEmitsOneLibraryOrder(t *testing.T) {
	t.Parallel()
	e, _ := squadRallierEngine(t, 313)
	digReorder(t, e, "Grizzly Bears")
	activateSquadRallier(t, e)
	ask := passUntilNonPriority(t, e, 20)
	if ask == nil || ask.Kind != decision.KChoose || len(ask.Options) != 1 {
		t.Fatalf("take ask = %+v, want the one-option KChoose", ask)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)

	var orders []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.LibraryOrder {
			orders = append(orders, ev)
		}
	}
	// The pinned digReorder emits one LibraryOrder itself; the Dig's shuffle
	// is the second.
	if len(orders) != 2 {
		t.Fatalf("LibraryOrder events = %d, want 2 (the fixture's pin plus the Dig's remainder shuffle)", len(orders))
	}
	dig := orders[len(orders)-1]
	if !dig.Secret {
		t.Fatal("the Dig's remainder LibraryOrder is not Secret; a hidden bottom order must not leak")
	}
	if len(dig.IDs) != len(e.G.Zone(state.ZLibrary, 0)) {
		t.Fatalf("LibraryOrder carries %d ids, want the complete library (%d)", len(dig.IDs), len(e.G.Zone(state.ZLibrary, 0)))
	}
	for i, oid := range e.G.Zone(state.ZLibrary, 0) {
		if dig.IDs[i] != oid {
			t.Fatalf("LibraryOrder[%d] = %v, want the applied order %v", i, dig.IDs[i], oid)
		}
	}
	// A KArrange is the only kind that would let a seat choose the order.
	for _, ev := range e.L.Events {
		if ev.Kind == events.LibraryOrder && !ev.Secret {
			t.Fatal("a non-Secret LibraryOrder would leak the hidden bottom order")
		}
	}
}
