package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// multiLibraryContinuationHost gives each of three players two distinct top
// cards. The distinct order is a precondition: a test that accidentally asks
// or resolves only one library must not pass by comparing identical values.
func multiLibraryContinuationHost(t *testing.T) (*askHost, [][]state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(3))
	cards := []*cards.Card{
		mkCard(t, "Name:Alpha\nTypes:Creature\nPT:2/2\nOracle:x\n"),
		mkCard(t, "Name:Beta\nTypes:Creature\nPT:3/3\nOracle:x\n"),
	}
	libs := make([][]state.ObjID, 3)
	for p := state.PlayerID(0); p < 3; p++ {
		for _, card := range cards {
			libs[p] = append(libs[p], h.g.AddObject(card, p).ID)
		}
		h.g.SetZone(state.ZLibrary, p, libs[p])
		if libs[p][0] == libs[p][1] {
			t.Fatal("precondition: library cards must have distinct object ids")
		}
	}
	return h, libs
}

func requireSecondLibraryAsk(t *testing.T, h *askHost, want decision.Kind) {
	t.Helper()
	if h.asked == nil || h.asked.Kind != want || h.asked.ResumeTarget != 1 {
		t.Fatalf("decision = %+v, want %v for library target 1", h.asked, want)
	}
	if len(h.asked.Options) == 0 || h.asked.Options[0].Obj == 0 {
		t.Fatal("precondition: the second library ask must offer a real card")
	}
}
