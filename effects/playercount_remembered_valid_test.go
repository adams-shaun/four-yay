package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPlayerCountRememberedValidCountsRememberedPlayersCards pins Forge's
// PlayerCountRemembered$Valid semantic against the corpus carriers. It is a
// CARD count that takes a filter argument, NOT a count of remembered players:
//
//	Pox              SVar:E:PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl/ThirdUp
//	Pox Plague       SVar:Z:PlayerCountRemembered$Valid Permanent.RememberedPlayerCtrl/HalfDown
//	Fraying Omnipotence SVar:E:PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl/HalfUp
//	Batwing Brume    SVar:X:PlayerCountRemembered$Valid Creature.YouCtrl+attacking
//
// The Pox family sizes "a third/half of the creatures/permanents they
// control"; Batwing Brume sizes "each player loses 1 life for each attacking
// creature they control". With one remembered player controlling three
// creatures and another controlling one, the head must read 4 on the
// battlefield, not the 2 distinct remembered players.
func TestPlayerCountRememberedValidCountsRememberedPlayersCards(t *testing.T) {
	g := state.NewGame(names(4))
	h := &fakeHost{g: g}
	// Three creatures controlled by remembered player 1, one by remembered
	// player 3. The precondition assert below proves the four are live
	// battlefield permanents, so a battlefield-scan regression cannot pass
	// this by counting nothing.
	mk := func(owner state.PlayerID) state.ObjID {
		o := g.AddObject(mkCard(t, "Name:Remembered creature\nTypes:Creature\nPT:1/1\nOracle:x\n"), owner)
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		return o.ID
	}
	ids := []state.ObjID{mk(1), mk(1), mk(1), mk(3)}
	// A non-creature permanent controlled by a remembered player must not be
	// counted: the spec's `Creature` half has to bind.
	land := g.AddObject(mkCard(t, "Name:Remembered land\nTypes:Land\nOracle:x\n"), 1)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: land.ID, From: state.ZLibrary, To: state.ZBattlefield})

	c := &Ctx{Controller: 0, Remembered: []state.Target{
		{Player: 1, IsPlayer: true},
		{Player: 3, IsPlayer: true},
	}}
	// Preconditions the real assertion depends on: four live battlefield
	// creatures, one live non-creature, and two distinct remembered players
	// that are not the resolving controller. A vacuous setup fails loudly.
	battleCreatures := 0
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("fixture: creature %d not on the battlefield: %+v", id, o)
		}
		battleCreatures++
	}
	if battleCreatures != 4 {
		t.Fatalf("fixture: %d battlefield creatures, want 4", battleCreatures)
	}
	if lo := g.Obj(land.ID); lo == nil || lo.Zone != state.ZBattlefield {
		t.Fatalf("fixture: remembered land not on the battlefield: %+v", lo)
	}
	if c.Remembered[0].Player == c.Remembered[1].Player || c.Remembered[0].Player == c.Controller {
		t.Fatal("fixture must remember two distinct non-controller players")
	}

	got, ok := EvalCountOK(h, c, "Count$PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl")
	if !ok {
		t.Fatal("PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl reported UNRESOLVED")
	}
	if got != 4 {
		t.Fatalf("PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl = %d, want 4 "+
			"(three creatures of player 1 plus one of player 3, not the 2 remembered players)", got)
	}

	// The /Op suffix the corpus writes (Pox Plague's HalfDown) must keep
	// flowing through the same arithmetic: half of 4, rounded down, is 2.
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl/HalfDown"); !ok || got != 2 {
		t.Fatalf("PlayerCountRemembered$Valid Creature.RememberedPlayerCtrl/HalfDown = (%d, %v), want (2, true)", got, ok)
	}

	// Pox Plague's spec is `Permanent.RememberedPlayerCtrl` and Pox's land leg
	// is `Land.RememberedPlayerCtrl`: the same fold must honour any card-type
	// prefix, not only Creature. The land from the fixture is a permanent
	// controlled by remembered player 1, so both reads include it (+1).
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountRemembered$Valid Permanent.RememberedPlayerCtrl"); !ok || got != 5 {
		t.Fatalf("PlayerCountRemembered$Valid Permanent.RememberedPlayerCtrl = (%d, %v), want (5, true)", got, ok)
	}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountRemembered$Valid Land.RememberedPlayerCtrl"); !ok || got != 1 {
		t.Fatalf("PlayerCountRemembered$Valid Land.RememberedPlayerCtrl = (%d, %v), want (1, true)", got, ok)
	}
}

// TestPlayerCountRememberedValidFailsClosedWithoutSpec pins the other half of
// the reading: the head is a card count whose whole point is its filter, so a
// bare `PlayerCountRemembered$Valid` with no spec is unresolved rather than a
// silently wrong player count.
func TestPlayerCountRememberedValidFailsClosedWithoutSpec(t *testing.T) {
	g := state.NewGame(names(2))
	g.AddObject(mkCard(t, "Name:Creature\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
	if got, ok := EvalCountOK(h, c, "Count$PlayerCountRemembered$Valid"); ok {
		t.Fatalf("bare PlayerCountRemembered$Valid = (%d, true); want unresolved", got)
	}
}
