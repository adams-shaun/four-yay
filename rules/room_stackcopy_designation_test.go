package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A Room spell copy is put onto the stack but was never cast (CR 707.10).
// Resolving it must not designate a door or fire the cast-face entry trigger.
func TestStackCopyOfRoomDoesNotDesignateOrFireEntryTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	roomCard := lookup(t, reg, "Spiked Corridor")
	hasTrigger := false
	for _, tr := range roomCard.Faces[0].Triggers {
		if tr.Mode == "UnlockDoor" {
			hasTrigger = true
		}
	}
	if !hasTrigger {
		t.Fatal("precondition: Spiked Corridor has no cast-face UnlockDoor trigger")
	}
	e := corpusEngine(t, reg, []*cards.Card{roomCard}, nil)
	moveByName(t, e, 0, "Spiked Corridor", state.ZHand)
	addMana(t, e, 0, "RRRR")
	castNamed(t, e, "Spiked Corridor")
	if len(e.G.Stack) != 1 {
		t.Fatalf("precondition: original Room spell stack size = %d, want 1", len(e.G.Stack))
	}
	original := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.StackCopy, Obj: original, Player: 0})
	if len(e.G.Stack) != 2 {
		t.Fatalf("StackCopy did not put a Room copy on the stack: stack=%v", e.G.Stack)
	}
	copyID := e.G.Stack[len(e.G.Stack)-1]
	copy := e.G.Obj(copyID)
	if copy == nil || !copy.IsCopy || copy.Zone != state.ZStack || copy.Face() == nil || !copy.Face().IsRoom() {
		t.Fatalf("precondition: expected a copied Room spell on stack, got %+v", copy)
	}

	passUntilStackEmpty(t, e, 30)
	answerQuiet(t, e, 60)

	if got := countDevils(e); got != 3 {
		t.Fatalf("Room cast and its copy made %d Devils, want 3 (only the cast Room's entry trigger)", got)
	}
	copy = e.G.Obj(copyID)
	if copy.Zone != state.ZBattlefield || !copy.IsToken {
		t.Fatalf("resolved Room copy did not become a battlefield token: %+v", copy)
	}
	if copy.CastDoor || copy.DoorUnlocked(0) || copy.DoorUnlocked(1) {
		t.Fatalf("resolved never-cast Room copy has a designated door: CastDoor=%v face0=%v face1=%v", copy.CastDoor, copy.DoorUnlocked(0), copy.DoorUnlocked(1))
	}
}
