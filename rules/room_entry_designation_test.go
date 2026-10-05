package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// designateRoomCastFace makes a synthetic battlefield fixture represent a Room
// spell that resolved. Production derives this bit from MoveZone provenance.
func designateRoomCastFace(e *Engine, id state.ObjID) {
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZStack})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZStack, To: state.ZBattlefield})
}

func TestNonCastRoomHasNoLiveDoorsAndCastEntryDesignatesFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	room := card(t, "Name:Designation Room\nManaCost:0\nTypes:Enchantment Room\nOracle:x\nALTERNATE\nName:Designation Chamber\nManaCost:0\nTypes:Enchantment Room\nOracle:y\nAlternateMode:Split\n")
	e := corpusEngine(t, reg, []*cards.Card{room, room}, nil)

	// A hand-to-battlefield move is explicitly non-cast entry.
	noncast := moveByName(t, e, 0, "Designation Room", state.ZBattlefield)
	n := e.G.Obj(noncast)
	if n == nil || n.Zone != state.ZBattlefield || n.EnteredFrom == state.ZStack {
		t.Fatalf("precondition: expected non-stack battlefield entry, got %+v", n)
	}
	if n.DoorUnlocked(0) || n.DoorUnlocked(1) {
		t.Fatalf("non-cast Room has an unlocked door: face0=%v face1=%v", n.DoorUnlocked(0), n.DoorUnlocked(1))
	}
	if _, count := roomTriggerFaces(n, n.Face()); count != 0 {
		t.Fatalf("non-cast Room has %d live trigger faces, want 0", count)
	}

	// The second identical card traverses the stack, the only cast-entry path.
	castID := moveByName(t, e, 0, "Designation Room", state.ZBattlefield)
	designateRoomCastFace(e, castID)
	c := e.G.Obj(castID)
	if c == nil || c.Zone != state.ZBattlefield || c.EnteredFrom != state.ZStack {
		t.Fatalf("precondition: expected stack-origin Room entry, got %+v", c)
	}
	if !c.DoorUnlocked(int(c.FaceIdx)) || c.DoorUnlocked(1-int(c.FaceIdx)) {
		t.Fatalf("cast Room designation incorrect: faceIdx=%d cast=%v alternate=%v", c.FaceIdx, c.DoorUnlocked(int(c.FaceIdx)), c.DoorUnlocked(1-int(c.FaceIdx)))
	}
	if _, count := roomTriggerFaces(c, c.Face()); count != 1 {
		t.Fatalf("cast Room has %d live trigger faces, want 1", count)
	}
}
