package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// api:Venture (CR 701.49, "Venture into the Dungeon"). These tests prove the
// primitive is registered for every corpus carrier, that a first plain
// venture offers exactly the three non-Undercity dungeons (never Undercity),
// that the explicit Dungeon$ Undercity form enters the Undercity without
// asking, that a two-arrow room poses a choose decision to the venturing
// seat whose answer moves the marker, that a one-arrow room moves without
// asking, that venturing from the bottommost room is a LOUD nothing (dungeon
// completion is the chain's slice 3), and that the whole scenario replays
// byte-identically from its log.

// ventureFixtureSrc is a freely-authored fixture card (never a corpus .txt,
// per the licensing rule) whose resolution is one plain venture.
const ventureFixtureSrc = "Name:Delve Deep\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Venture | SpellDescription$ Venture into the dungeon.\nOracle:x\n"

// ventureUndercitySrc is the explicit "venture into Undercity" form
// (CR 701.49d): the Dungeon$ quality names the dungeon the choice enters.
const ventureUndercitySrc = "Name:Descend\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Venture | Dungeon$ Undercity | SpellDescription$ Venture into Undercity.\nOracle:x\n"

// plainDungeonKeys / undercityKey pin the corpus's dungeon token scripts:
// plain venture must offer exactly the three non-Undercity dungeons, and
// Dungeon$ Undercity must enter the Undercity. A corpus pin that adds a
// dungeon token changes this set and fails here, to be examined -- not
// silently offered to venturing players.
var (
	plainDungeonKeys = []string{"dungeon_of_the_mad_mage", "lost_mine_of_phandelver", "tomb_of_annihilation"}
	undercityKey     = "undercity"
)

// ventureExceptionTable is the corpus-walk ratchet's named exception table:
// each entry names the primitives a Venture carrier is STILL missing. api:
// Venture itself must never appear here; an entry whose card is now fully
// supported is stale and fails, and a carrier missing something not listed
// is a new gap and fails.
var ventureExceptionTable = map[string][]string{
	"Immovable Rod":             {"kw:You may choose not to untap CARDNAME during your untap step."},
	"Sefris of the Hidden Ways": {"trig:DungeonCompleted"},
	"Varis, Silverymoon Ranger": {"trig:DungeonCompleted"},
}

// TestVenturePrimitiveRegisteredForEveryCorpusCarrier walks the corpus for
// every card whose parsed primitives include api:Venture and asserts the
// registry names it supported, with every remaining gap named in the
// exception table above. The carrier count is pinned so a corpus-pin change
// is noticed rather than silently skipped.
func TestVenturePrimitiveRegisteredForEveryCorpusCarrier(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	if !supported["api:Venture"] {
		t.Fatal("effects.Supported() does not name api:Venture: the primitive is not registered")
	}
	carriers := 0
	for _, c := range reg.Cards {
		if !slices.Contains(c.Primitives(), "api:Venture") {
			continue
		}
		carriers++
		name := c.Faces[0].Name
		miss := reg.Unsupported(c, supported)
		if slices.Contains(miss, "api:Venture") {
			t.Errorf("card %q still reports api:Venture unsupported", name)
			continue
		}
		want, ok := ventureExceptionTable[name]
		if len(miss) == 0 && ok {
			t.Errorf("stale exception: card %q is now fully supported; drop it from ventureExceptionTable", name)
		}
		if len(miss) > 0 && !slices.Equal(miss, want) {
			t.Errorf("card %q missing %v; ventureExceptionTable holds %v", name, miss, want)
		}
	}
	if carriers != 45 {
		t.Errorf("corpus carriers of api:Venture = %d, want 45 (the measured count at this corpus pin)", carriers)
	}
}

// newVentureGame builds a two-seat game whose libraries carry copies of the
// fixture venture spell, with the corpus's token scripts (the four dungeon
// scripts among them) wired into Config.Tokens. The first copy is moved into
// seat 0's hand with a LOGGED MoveZone (a replayable move -- a replayed game
// must reconstruct the hand from the log), and mana is added.
func newVentureGame(t *testing.T, reg *cards.Registry, src string, copies int) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, src)
	name := fixture.Faces[0].Name
	deck0 := make([]*cards.Card, 0, copies+40)
	for i := 0; i < copies; i++ {
		deck0 = append(deck0, fixture)
	}
	mountains := mountainDeck(t, 40)
	deck0 = append(deck0, mountains...)
	cfg := seatZeroStart(Config{Seed: 17, Names: []string{"a", "b"},
		Tokens: reg.Tokens,
		Decks:  [][]*cards.Card{deck0, mountains}})
	e := New(cfg)
	e.Advance()
	id := moveByName(t, e, 0, name, state.ZHand)
	if id == 0 {
		t.Fatalf("fixture %q was not dealt into a zone", name)
	}
	addMana(t, e, 0, "B")
	return e, cfg, id
}

// castVenture submits the cast option for the fixture spell, then drives
// until a venture ask (a KChoose whose resume kind is a venture choice) is
// pending or the stack is empty, answering whatever the passing turns pose
// along the way (priority passes, and a cleanup discard's first legal
// option) so the game makes real progress. It returns the venture ask, or
// nil when the stack emptied without one.
func castVenture(t *testing.T, e *Engine, id state.ObjID) *decision.Decision {
	t.Helper()
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
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	for i := 0; i < 80; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) != 0 {
				t.Fatalf("no decision pending with a live stack: %+v", e.G.Stack)
			}
			return nil
		}
		if d.Kind == decision.KChoose && (d.ResumeKind == "venture_dungeon" || d.ResumeKind == "venture_room") {
			return d
		}
		if len(e.G.Stack) == 0 {
			// The venture spell resolved: whatever is pending now (the next
			// priority ask, a later turn's discard) is not this resolution's.
			return nil
		}
		if d.Kind == decision.KPriority {
			passIdx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					passIdx = o.Index
				}
			}
			if passIdx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{passIdx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		// A non-priority decision the passing turns posed (a cleanup discard):
		// answer with its first offered option so the game keeps moving. If it
		// cannot be answered the Submit fails loudly here rather than the test
		// silently stalling.
		if len(d.Options) == 0 {
			t.Fatalf("unanswerable decision with no options: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("submit first option of %+v: %v", d.Kind, err)
		}
	}
	t.Fatal("resolution never settled")
	return nil
}

// dungeonRoom reads the marker key off the game state, failing when the
// player has no dungeon -- the precondition every room assertion below leans
// on.
func dungeonRoom(t *testing.T, e *Engine, p state.PlayerID) (state.ObjID, string) {
	t.Helper()
	id := e.G.Players[p].DungeonObj
	if id == 0 {
		t.Fatalf("player %d has no dungeon in the command zone", p)
	}
	return id, e.G.Players[p].DungeonRoom
}

// ventureEvents counts the dungeon-lifecycle events of one kind naming one
// player, so the tests below can assert the exact event shape the fold
// replays.
func ventureEvents(e *Engine, kind events.Kind, p state.PlayerID, text string) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == kind && ev.Player == p && (text == "" || ev.Text == text) {
			n++
		}
	}
	return n
}

// TestFirstVentureOffersTheThreeDungeons is CR 701.49a: a plain venture with
// no dungeon owned poses a choose decision offering exactly the three
// non-Undercity dungeons (never Undercity), and the answered choice puts the
// dungeon into the command zone with the marker on its first room.
func TestFirstVentureOffersTheThreeDungeons(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id := newVentureGame(t, reg, ventureFixtureSrc, 2)
	if e.G.Players[0].DungeonObj != 0 {
		t.Fatalf("precondition: seat 0 already owns dungeon %d", e.G.Players[0].DungeonObj)
	}
	d := castVenture(t, e, id)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("first venture did not pose a choose decision to seat 0: %+v", d)
	}
	var keys []string
	for _, o := range d.Options {
		if o.Kind != "dungeon" {
			t.Errorf("first venture option kind %q, want \"dungeon\": %+v", o.Kind, o)
		}
		if o.Label == "" || o.Key == "" {
			t.Errorf("dungeon option without label/key: %+v", o)
		}
		keys = append(keys, o.Key)
		if o.Label == "Undercity" {
			t.Errorf("plain venture offered Undercity: %+v", o)
		}
	}
	slices.Sort(keys)
	if !slices.Equal(keys, plainDungeonKeys) {
		t.Fatalf("plain venture offered %v, want %v", keys, plainDungeonKeys)
	}
	// Answer with Lost Mine of Phandelver and assert the enter: the dungeon
	// object lands in the command zone and the marker on its first room, and
	// the two events are logged in that order.
	lostMine := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "lost_mine_of_phandelver" })
	submitChoices(t, e, lostMine)
	if e.G.Players[0].DungeonObj == 0 {
		t.Fatal("the answered venture entered no dungeon")
	}
	dobj, room := dungeonRoom(t, e, 0)
	if got := e.G.Obj(dobj).Face().Name; got != "Lost Mine of Phandelver" {
		t.Fatalf("entered dungeon %q, want Lost Mine of Phandelver", got)
	}
	if room != "DBEntrance" {
		t.Fatalf("marker on %q, want the first room DBEntrance", room)
	}
	if e.G.Obj(dobj).Zone != state.ZCommand {
		t.Fatalf("dungeon zone %s, want command zone", e.G.Obj(dobj).Zone)
	}
	if ventureEvents(e, events.DungeonCreate, 0, "lost_mine_of_phandelver") != 1 ||
		ventureEvents(e, events.DungeonRoom, 0, "DBEntrance") != 1 {
		t.Fatal("the enter was not logged as one DungeonCreate + one DungeonRoom")
	}
	// Replay the whole game from its log and assert the reconstructed state
	// matches and the recomputed chain head equals the live one.
	replayCheck(t, e, cfg)
	lg := events.NewLog(cfg.Seed)
	g := state.NewGameLife(cfg.Names, 20)
	g.Tokens = cfg.Tokens
	for i := range cfg.Decks {
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(cfg.Decks[i]))
		for _, c := range cfg.Decks[i] {
			ids = append(ids, g.AddObject(c, p).ID)
		}
		g.SetZone(state.ZLibrary, p, ids)
	}
	for _, ev := range e.L.Events {
		events.Emit(g, lg, ev)
	}
	if lg.Head() != e.L.Head() {
		t.Fatalf("chain head %s, log replay %s", e.L.Head(), lg.Head())
	}
}

// TestVentureIntoUndercityPicksUndercity is CR 701.49d: the explicit
// Dungeon$ Undercity form enters the Undercity (the only dungeon of that
// quality) WITHOUT asking, marker on its first room.
func TestVentureIntoUndercityPicksUndercity(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id := newVentureGame(t, reg, ventureUndercitySrc, 1)
	if e.G.Players[0].DungeonObj != 0 {
		t.Fatalf("precondition: seat 0 already owns dungeon %d", e.G.Players[0].DungeonObj)
	}
	if d := castVenture(t, e, id); d != nil {
		t.Fatalf("venture into Undercity asked: %+v", d)
	}
	dobj, room := dungeonRoom(t, e, 0)
	if got := e.G.Obj(dobj).Face().Name; got != "Undercity" {
		t.Fatalf("entered dungeon %q, want Undercity", got)
	}
	if room != "Entrance" {
		t.Fatalf("marker on %q, want Undercity's first room Entrance", room)
	}
	if ventureEvents(e, events.DungeonCreate, 0, undercityKey) != 1 {
		t.Fatal("the enter was not logged as one DungeonCreate")
	}
	replayCheck(t, e, cfg)
}

// TestVentureAdvancesAlongNextRoomArrows is CR 701.49b: a later venture
// moves the marker to one of the current room's NextRoom$ rooms. A two-arrow
// room poses a choose decision to the venturing seat whose answer moves the
// marker; a one-arrow room moves without asking; and each room's ability
// (CR 309.4c, the dungeon chain's slice 3) triggers and resolves. Venturing
// after the marker reached the bottommost room does NOT move a marker: once
// the bottom room's ability has left the stack CR 704.5t completes and
// removes the dungeon, so the next venture starts a new dungeon (CR
// 701.49a) and poses the dungeon choice again. The whole scenario replays
// byte-identically from its log.
//
// The walk follows the Lost Mine of Phandelver script's own arrows (this
// corpus pin): DBEntrance --(DBGoblinLair, DBMineTunnels); DBMineTunnels
// --(DBDarkPool, DBFungiCavern); DBDarkPool --(DBTempleDumathoin); and
// DBTempleDumathoin is the bottommost room, no NextRoom$.
func TestVentureAdvancesAlongNextRoomArrows(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id := newVentureGame(t, reg, ventureFixtureSrc, 6)
	var seen []decision.Kind
	// First venture: enter Lost Mine (the ask answered with its key), then
	// resolve the entering room's own ability (Cave Entrance's Scry 1).
	d := castVenture(t, e, id)
	if d == nil {
		t.Fatal("first venture posed no dungeon choice")
	}
	lostMine := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "lost_mine_of_phandelver" })
	submitChoices(t, e, lostMine)
	dobj, room := dungeonRoom(t, e, 0)
	if room != "DBEntrance" {
		t.Fatalf("precondition: first venture marker on %q, want DBEntrance", room)
	}
	driveRoomResolution(t, e, dobj, &seen)

	// Second venture: Cave Entrance's two arrows are Goblin Lair and Mine
	// Tunnels, in the script's printed NextRoom$ order.
	id2 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	d = castVenture(t, e, id2)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("two-way room venture did not pose a two-option choose to seat 0: %+v", d)
	}
	wantRooms := []string{"DBGoblinLair", "DBMineTunnels"}
	wantLabels := []string{"Goblin Lair", "Mine Tunnels"}
	for i, o := range d.Options {
		if o.Kind != "room" || o.Key != wantRooms[i] || o.Label != wantLabels[i] {
			t.Errorf("room option %d = %+v, want kind \"room\" key %s label %q", i, o, wantRooms[i], wantLabels[i])
		}
	}
	// The answer MOVES the marker: choose Mine Tunnels.
	mine := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "DBMineTunnels" })
	submitChoices(t, e, mine)
	if _, room = dungeonRoom(t, e, 0); room != "DBMineTunnels" {
		t.Fatalf("answered room choice left the marker on %q, want DBMineTunnels", room)
	}
	if ventureEvents(e, events.DungeonRoom, 0, "DBMineTunnels") != 1 {
		t.Fatal("the room answer was not logged as one DungeonRoom")
	}
	driveRoomResolution(t, e, dobj, &seen)

	// Third venture: Mine Tunnels' arrows are Dark Pool and Fungi Cavern --
	// two arrows again; answer with Dark Pool. Then a one-arrow room must
	// move WITHOUT asking: Dark Pool leads only to Temple of Dumathoin.
	id3 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	d = castVenture(t, e, id3)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("Mine Tunnels venture did not pose a two-option choose: %+v", d)
	}
	darkPool := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "DBDarkPool" })
	submitChoices(t, e, darkPool)
	if _, room = dungeonRoom(t, e, 0); room != "DBDarkPool" {
		t.Fatalf("marker on %q, want DBDarkPool", room)
	}
	driveRoomResolution(t, e, dobj, &seen)
	id4 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	if d = castVenture(t, e, id4); d != nil {
		t.Fatalf("one-arrow room (Dark Pool) asked: %+v", d)
	}
	// The single-arrow advance moved the marker into the bottommost room
	// WITHOUT asking. The room ability resolved during castVenture's own
	// priority passing (it is the only non-ask the venture can leave behind),
	// so the marker is asserted from the logged DungeonRoom, not from live
	// state -- the dungeon may already be completed by the time we look.
	if ventureEvents(e, events.DungeonRoom, 0, "DBTempleDumathoin") != 1 {
		t.Fatal("one-arrow room (Dark Pool) did not move the marker to DBTempleDumathoin")
	}

	// Temple of Dumathoin is the bottommost room; its room ability resolved
	// and left the stack, so CR 704.5t completed and removed the dungeon. The
	// next venture therefore starts a NEW dungeon.
	if e.G.Players[0].DungeonObj != 0 || e.G.Players[0].CompletedDungeons != 1 {
		t.Fatalf("bottom room did not complete the dungeon: obj=%d count=%d",
			e.G.Players[0].DungeonObj, e.G.Players[0].CompletedDungeons)
	}
	if ventureEvents(e, events.DungeonComplete, 0, "") != 1 || ventureEvents(e, events.DungeonRemove, 0, "") != 1 {
		t.Fatal("completion was not logged as one DungeonComplete + one DungeonRemove")
	}

	// The next venture starts a new dungeon: a dungeon choice is posed, and
	// it is the CR 701.49a list because no dungeon is active.
	id6 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	d = castVenture(t, e, id6)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "venture_dungeon" {
		t.Fatalf("venture after completion did not start a new dungeon: %+v", d)
	}
	if e.G.Players[0].CompletedDungeons != 1 {
		t.Fatalf("starting a new dungeon changed the completed count to %d", e.G.Players[0].CompletedDungeons)
	}
	replayCheck(t, e, cfg)
}

// TestVentureChoiceAnswerIsLegalForTheGenericValidator runs the exact shape
// the deterministic bot answers (the first offered option, the KChoose
// policy's pick) through Decision.Validate on a live two-arrow ask, so the
// bot's own answer can never be a rejected answer (the livelock contract).
func TestVentureChoiceAnswerIsLegalForTheGenericValidator(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, id := newVentureGame(t, reg, ventureFixtureSrc, 2)
	d := castVenture(t, e, id)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("first venture posed no dungeon choice: %+v", d)
	}
	for _, pick := range []int{0, len(d.Options) - 1} {
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}
		if err := d.Validate(in); err != nil {
			t.Fatalf("first-option answer %d rejected by the generic validator: %v", pick, err)
		}
	}
}
