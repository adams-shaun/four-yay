package rules

// Room entry unlock trigger (CR 709.5d/709.5h): casting a Room spell gives its
// CAST face the unlocked designation as it enters the battlefield, so that
// face's own T:Mode$ UnlockDoor trigger must fire on entry -- not only on the
// paid special-action unlock of the other door. This leaf drives the real
// corpus Spiked Corridor // Torture Pit: the front door's trigger creates
// three 1/1 red Devil tokens when the room is cast. It lives in its own file
// (never appended to mass_primitives_test.go's Room leaves) so a concurrent
// ticket cannot collide on the same test file.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// countDevils counts seat 0's battlefield Devil tokens.
func countDevils(e *Engine) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Devil") {
			n++
		}
	}
	return n
}

// TestSpikedCorridorCastEntryFiresUnlockDoor is the CR 709.5h leaf: casting the
// Spiked Corridor (front) half mints three Devils on entry, because the door
// the room was cast as is given the unlocked designation as it enters. The
// test asserts the preconditions that actually drive the rule (the room is a
// battlefield Room, FaceIdx is the cast face, Unlocked is false -- this model's
// "the alternate door is still locked") and that the cast face really carries
// the trigger, then that a later paid unlock of the OTHER door does not
// re-fire it.
func TestSpikedCorridorCastEntryFiresUnlockDoor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Spiked Corridor")}, nil)
	moveByName(t, e, 0, "Spiked Corridor", state.ZHand)

	// Precondition: the cast face really carries the Mode$ UnlockDoor trigger
	// the entry path must reach -- otherwise the assertion below is vacuous.
	spiked := lookup(t, reg, "Spiked Corridor")
	hasTrigger := false
	for _, tr := range spiked.Faces[0].Triggers {
		if tr.Mode == "UnlockDoor" {
			hasTrigger = true
		}
	}
	if !hasTrigger {
		t.Fatal("precondition: the Spiked Corridor front face carries no Mode$ UnlockDoor trigger")
	}

	addMana(t, e, 0, "RRRR")
	castNamed(t, e, "Spiked Corridor")
	passUntilStackEmpty(t, e, 30)
	answerQuiet(t, e, 60)

	// Precondition: the room is a battlefield Room, entered as the front face
	// (FaceIdx 0), and its alternate door is still locked.
	var room state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Spiked Corridor" {
			room = id
		}
	}
	if room == 0 {
		t.Fatal("the Spiked Corridor never entered the battlefield")
	}
	ro := e.G.Obj(room)
	if !isRoom(ro) {
		t.Fatalf("precondition: the entered object is not a Room permanent: %+v", ro)
	}
	if ro.FaceIdx != 0 {
		t.Fatalf("precondition: the room entered as FaceIdx %d, want 0 (the cast front half)", ro.FaceIdx)
	}
	if ro.Unlocked {
		t.Fatal("precondition: the room entered already fully unlocked, so no entry designation is at issue")
	}

	if got := countDevils(e); got != 3 {
		t.Fatalf("Spiked Corridor cast on entry made %d Devils, want 3 (CR 709.5d/709.5h)", got)
	}

	// The later paid unlock of the OTHER door (Torture Pit) must not re-fire
	// the front door's trigger: no double-fire on the DoorUnlock path.
	addMana(t, e, 0, "RRRR")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority to unlock Torture Pit (got %+v)", d)
	}
	unlock := -1
	for _, o := range d.Options {
		if o.Kind == "unlock" && o.Obj == room && o.Label == "Unlock Torture Pit" {
			unlock = o.Index
		}
	}
	if unlock < 0 {
		t.Fatalf("no Torture Pit unlock option in %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{unlock}}); err != nil {
		t.Fatalf("submit Torture Pit unlock: %v", err)
	}
	answerQuiet(t, e, 60)
	if !e.G.Obj(room).Unlocked {
		t.Fatal("the paid unlock did not unlock the alternate door")
	}
	if got := countDevils(e); got != 3 {
		t.Fatalf("the paid unlock of the other door re-fired the entry trigger: %d Devils, want 3", got)
	}
}
