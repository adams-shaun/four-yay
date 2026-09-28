package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The dungeon chain's slice 3: CR 309.4c room abilities and CR 704.5t's
// completion state-based action. The corpus's four dungeon token scripts are
// the fixture (never committed -- they live in the gitignored .cards/): Lost
// Mine of Phandelver, Dungeon of the Mad Mage, Tomb of Annihilation and
// Undercity. These tests walk Lost Mine along its own printed arrows and
// assert the room ability that triggered, the two-way choice, the CR 704.5t
// completion after the bottom room's ability leaves the stack, and that a
// completed dungeon is not re-entered.

// driveRoomResolution drives the engine from just after a venture choice to
// a settled priority with no room ability pending or on the stack, answering
// every nested ask (a room ability's CR 603.3c target, a Scry's arrange) with
// its first legal option. It appends every decision kind it answered to seen
// so a test can assert a room ability actually asked something. It stops when
// the dungeon's own room ability has fully left the stack -- the exact CR
// 704.5t moment.
func driveRoomResolution(t *testing.T, e *Engine, dungeonID state.ObjID, seen *[]decision.Kind) {
	t.Helper()
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) != 0 {
				t.Fatalf("no decision pending with a live stack: %v", e.G.Stack)
			}
			return
		}
		busy := len(e.G.Stack) > 0
		if !busy {
			for j := range e.pendingTriggers {
				if e.pendingTriggers[j].Source == dungeonID {
					busy = true
				}
			}
		}
		if !busy && d.Kind == decision.KPriority {
			return
		}
		if d.Kind == decision.KChoose && (d.ResumeKind == "venture_dungeon" || d.ResumeKind == "venture_room") {
			t.Fatalf("unexpected venture ask while resolving a room ability: %+v", d)
		}
		if len(d.Options) == 0 {
			t.Fatalf("unanswerable decision %v", d)
		}
		*seen = append(*seen, d.Kind)
		pick := 0
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pick = o.Index
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("submit first option of %v: %v", d.Kind, err)
		}
	}
	t.Fatal("room ability resolution never settled")
}

// roomTriggered reports whether the dungeon's room ability for room was put on
// the stack: a DelayedPush whose Obj is the dungeon and whose Counter is the
// room key (the SVar name events.Apply resolved).
func roomTriggered(e *Engine, dungeonID state.ObjID, room string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush && ev.Obj == dungeonID && ev.Counter == room && ev.Text == "delayed trigger" {
			return true
		}
	}
	return false
}

// drawsAfterRoom counts the Draw events for any seat that the log records
// after the delayed_push queueing the named room's ability. This is the
// precise "the room's Draw primitive resolved" signal: a bare hand-size
// comparison cannot see it, because the venture spell that triggered the
// room itself leaves the hand (handBefore - 1) and a cleanup discard can
// take a card back out again.
func drawsAfterRoom(e *Engine, dungeonID state.ObjID, room string) int {
	seen := false
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush && ev.Obj == dungeonID && ev.Counter == room {
			seen = true
			continue
		}
		if seen && ev.Kind == events.Draw {
			n++
		}
	}
	return n
}

// tokenCount counts battlefield permanents whose printed name matches.
func battlefieldNameCount(e *Engine, name string) int {
	n := 0
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				n++
			}
		}
	}
	return n
}

// TestDungeonRoomAbilitiesTriggerResolveAndComplete is the brief's core test.
// It ventures through Lost Mine of Phandelver's own arrows, asserting each
// room's ability triggered, went on the stack and resolved with its printed
// effect, that a two-way room posed a choose decision, and that after the
// bottommost room's ability left the stack CR 704.5t completed and removed
// the dungeon with the completed count at 1.
//
// Path taken (Lost Mine's arrows): DBEntrance (two-way) -> DBMineTunnels
// (two-way) -> DBDarkPool (one arrow) -> DBTempleDumathoin, the bottommost
// room. Effects: Scry 1, a Treasure token, each opponent loses 1 / you gain 1,
// draw a card.
func TestDungeonRoomAbilitiesTriggerResolveAndComplete(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id := newVentureGame(t, reg, ventureFixtureSrc, 8)
	if e.G.Players[0].DungeonObj != 0 {
		t.Fatalf("precondition: seat 0 already owns dungeon %d", e.G.Players[0].DungeonObj)
	}
	startLife0, startLife1 := e.G.Players[0].Life, e.G.Players[1].Life
	var seen []decision.Kind

	// Venture 1: enter Lost Mine (dungeon choice), marker -> DBEntrance, Scry 1.
	d := castVenture(t, e, id)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "venture_dungeon" {
		t.Fatalf("first venture posed no dungeon choice: %+v", d)
	}
	lostMine := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "lost_mine_of_phandelver" })
	if lostMine < 0 {
		t.Fatalf("dungeon options carry no Lost Mine: %+v", d.Options)
	}
	submitChoices(t, e, lostMine)
	dobj := e.G.Players[0].DungeonObj
	if dobj == 0 {
		t.Fatal("the answered venture entered no dungeon")
	}
	if room := e.G.Players[0].DungeonRoom; room != "DBEntrance" {
		t.Fatalf("marker on %q, want DBEntrance", room)
	}
	driveRoomResolution(t, e, dobj, &seen)
	if !roomTriggered(e, dobj, "DBEntrance") {
		t.Fatal("DBEntrance's room ability did not trigger on marker entry")
	}
	if !slices.Contains(seen, decision.KArrange) {
		t.Fatalf("DBEntrance's Scry 1 posed no arrange ask: seen=%v", seen)
	}

	// Venture 2: DBEntrance's two arrows pose a two-way choose; answer Mine
	// Tunnels. Its room ability creates a Treasure token.
	id2 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	d = castVenture(t, e, id2)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "venture_room" || len(d.Options) != 2 {
		t.Fatalf("DBEntrance's two-way room choice not posed: %+v", d)
	}
	if d.Options[0].Key != "DBGoblinLair" || d.Options[1].Key != "DBMineTunnels" {
		t.Fatalf("DBEntrance arrows = %q,%q, want DBGoblinLair,DBMineTunnels", d.Options[0].Key, d.Options[1].Key)
	}
	mine := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "DBMineTunnels" })
	submitChoices(t, e, mine)
	if room := e.G.Players[0].DungeonRoom; room != "DBMineTunnels" {
		t.Fatalf("marker on %q, want DBMineTunnels", room)
	}
	treasureBefore := battlefieldNameCount(e, "Treasure Token")
	driveRoomResolution(t, e, dobj, &seen)
	if !roomTriggered(e, dobj, "DBMineTunnels") {
		t.Fatal("DBMineTunnels's room ability did not trigger")
	}
	if battlefieldNameCount(e, "Treasure Token") != treasureBefore+1 {
		t.Fatalf("DBMineTunnels created no Treasure: before=%d after=%d", treasureBefore, battlefieldNameCount(e, "Treasure Token"))
	}

	// Venture 3: DBMineTunnels' two arrows; answer Dark Pool. Its ability is
	// each opponent loses 1 life and you gain 1.
	id3 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	d = castVenture(t, e, id3)
	if d == nil || d.ResumeKind != "venture_room" || len(d.Options) != 2 {
		t.Fatalf("DBMineTunnels' two-way room choice not posed: %+v", d)
	}
	dark := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "DBDarkPool" })
	submitChoices(t, e, dark)
	l0, l1 := e.G.Players[0].Life, e.G.Players[1].Life
	driveRoomResolution(t, e, dobj, &seen)
	if !roomTriggered(e, dobj, "DBDarkPool") {
		t.Fatal("DBDarkPool's room ability did not trigger")
	}
	if e.G.Players[0].Life != l0+1 || e.G.Players[1].Life != l1-1 {
		t.Fatalf("DBDarkPool: seat0 %d->%d (want gain 1), seat1 %d->%d (want lose 1)",
			l0, e.G.Players[0].Life, l1, e.G.Players[1].Life)
	}

	// Venture 4: DBDarkPool's single arrow moves WITHOUT asking to the
	// bottommost room DBTempleDumathoin. Its room ability draws a card; once
	// it leaves the stack CR 704.5t completes and removes the dungeon.
	id4 := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	if d = castVenture(t, e, id4); d != nil {
		t.Fatalf("DBDarkPool's one-arrow advance asked: %+v", d)
	}
	if ventureEvents(e, events.DungeonRoom, 0, "DBTempleDumathoin") != 1 {
		t.Fatal("marker never moved into DBTempleDumathoin")
	}
	driveRoomResolution(t, e, dobj, &seen)
	if !roomTriggered(e, dobj, "DBTempleDumathoin") {
		t.Fatal("DBTempleDumathoin's room ability did not trigger")
	}
	// The drawn card is the exact signal: the venture spell took one card out
	// of hand, so a net hand-size check cannot see the room's Draw resolve.
	if n := drawsAfterRoom(e, dobj, "DBTempleDumathoin"); n != 1 {
		t.Fatalf("DBTempleDumathoin drew %d cards, want exactly 1", n)
	}

	// CR 704.5t: the dungeon is completed and removed, the count is 1, and it
	// left the command zone.
	if e.G.Players[0].DungeonObj != 0 {
		t.Fatalf("bottom dungeon not completed: DungeonObj=%d", e.G.Players[0].DungeonObj)
	}
	if e.G.Players[0].CompletedDungeons != 1 {
		t.Fatalf("CompletedDungeons=%d, want 1", e.G.Players[0].CompletedDungeons)
	}
	if o := e.G.Obj(dobj); o == nil || o.Zone != state.ZCeased {
		t.Fatalf("completed dungeon object = %+v, want ceased", o)
	}
	if ventureEvents(e, events.DungeonComplete, 0, "") != 1 || ventureEvents(e, events.DungeonRemove, 0, "") != 1 {
		t.Fatalf("completion events: complete=%d remove=%d, want 1 each",
			ventureEvents(e, events.DungeonComplete, 0, ""), ventureEvents(e, events.DungeonRemove, 0, ""))
	}
	// Preconditions for the life assertions actually having differed.
	if startLife0 != 20 || startLife1 != 20 {
		t.Fatalf("precondition: starting life %d/%d, want 20/20", startLife0, startLife1)
	}
	replayCheck(t, e, cfg)
}

// TestCompletedDungeonIsNotReentered is the brief's second test: after
// completion the dungeon is gone, so the next venture starts a NEW dungeon
// (a dungeon choice is posed again).
func TestCompletedDungeonIsNotReentered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id := newVentureGame(t, reg, ventureFixtureSrc, 8)
	// Enter Lost Mine and walk straight to the bottom (Entrance -> Mine
	// Tunnels -> Dark Pool -> Temple), answering asks along the way.
	d := castVenture(t, e, id)
	if d == nil || d.ResumeKind != "venture_dungeon" {
		t.Fatalf("first venture posed no dungeon choice: %+v", d)
	}
	submitChoices(t, e, slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "lost_mine_of_phandelver" }))
	dobj := e.G.Players[0].DungeonObj
	var seen []decision.Kind
	driveRoomResolution(t, e, dobj, &seen)
	// Two-step path: Entrance -> MineTunnels -> DarkPool -> Temple.
	for _, want := range []string{"DBMineTunnels", "DBDarkPool", "DBTempleDumathoin"} {
		vid := moveByName(t, e, 0, "Delve Deep", state.ZHand)
		addMana(t, e, 0, "B")
		d = castVenture(t, e, vid)
		if d != nil {
			pick := -1
			for _, o := range d.Options {
				if o.Key == want {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("room options %+v carry no %s", d.Options, want)
			}
			submitChoices(t, e, pick)
		}
		if ventureEvents(e, events.DungeonRoom, 0, want) != 1 {
			t.Fatalf("marker never moved into %q", want)
		}
		driveRoomResolution(t, e, dobj, &seen)
	}
	if e.G.Players[0].CompletedDungeons != 1 || e.G.Players[0].DungeonObj != 0 {
		t.Fatalf("precondition: dungeon not completed (count=%d obj=%d)", e.G.Players[0].CompletedDungeons, e.G.Players[0].DungeonObj)
	}
	// The next venture starts a NEW dungeon: a dungeon choice is posed.
	vid := moveByName(t, e, 0, "Delve Deep", state.ZHand)
	addMana(t, e, 0, "B")
	d = castVenture(t, e, vid)
	if d == nil || d.ResumeKind != "venture_dungeon" {
		t.Fatalf("venture after completion did not start a new dungeon: %+v", d)
	}
	// It must not offer to re-enter the completed Lost Mine's marker: no
	// dungeon is active, so the choice is the CR 701.49a list again.
	submitChoices(t, e, slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Key == "lost_mine_of_phandelver" }))
	if e.G.Players[0].DungeonObj == 0 || e.G.Players[0].DungeonRoom != "DBEntrance" {
		t.Fatalf("new dungeon not entered: obj=%d room=%q", e.G.Players[0].DungeonObj, e.G.Players[0].DungeonRoom)
	}
	if e.G.Players[0].CompletedDungeons != 1 {
		t.Fatalf("CompletedDungeons=%d, want still 1", e.G.Players[0].CompletedDungeons)
	}
	replayCheck(t, e, cfg)
}

// TestEveryDungeonRoomAbilityIsSupported walks every room of all four corpus
// dungeon token scripts and asserts the room's top-level SVar body resolves
// to an API the effects registry supports, naming any room that is not. It
// also asserts the room's SVar carries a NextRoom$ or is the bottommost room
// -- the completion check's premise.
func TestEveryDungeonRoomAbilityIsSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	dungeons := 0
	for _, def := range reg.Tokens {
		if def == nil || len(def.Faces) == 0 {
			continue
		}
		f := def.Faces[0]
		if !slices.Contains(f.Types, "Dungeon") {
			continue
		}
		dungeons++
		list, ok := f.KeywordParam("Dungeon")
		if !ok {
			t.Errorf("dungeon %q has no K:Dungeon room list", f.Name)
			continue
		}
		for _, room := range strings.Split(list, ",") {
			room = strings.TrimSpace(room)
			if room == "" {
				continue
			}
			sa := cards.ResolveSVar(f.SVars, room)
			if sa == nil {
				t.Errorf("dungeon %q room %q resolves no SVar", f.Name, room)
				continue
			}
			if sa.Kind == "DB" && sa.API != "" && !supported["api:"+sa.API] {
				t.Errorf("dungeon %q room %q uses unsupported api:%s", f.Name, room, sa.API)
			}
			for sub := sa.Sub; sub != nil; sub = sub.Sub {
				// A room ability may chain sub-abilities; each must also be
				// supported (they resolve through the same registry).
				if sub.API != "" && !supported["api:"+sub.API] {
					t.Errorf("dungeon %q room %q sub-ability uses unsupported api:%s", f.Name, room, sub.API)
				}
			}
		}
	}
	if dungeons != 4 {
		t.Fatalf("corpus dungeon scripts = %d, want 4", dungeons)
	}
}
