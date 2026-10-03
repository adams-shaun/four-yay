package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestDigUntilWithholdsUnsupportedParamsAndStillMoves pins the two riders
// that remain withheld after the withheld-rider work: a non-literal Amount$
// with no SVar in the resolving context, and DigZone$ (every corpus value is
// PlanarDeck, and this build has no planar tier). Each still emits its one
// loud Note, and the core reveal-until move still runs.
func TestDigUntilWithholdsUnsupportedParamsAndStillMoves(t *testing.T) {
	h, ids := digUntilFixture(t)
	ability := sa(t, "SP$ DigUntil | Valid$ Aura | Amount$ X | DigZone$ PlanarDeck | FoundDestination$ Hand | RevealedDestination$ Graveyard")

	// PRECONDITION: the matching Aura is in the scanned library, and its
	// destination differs from the library so a no-op implementation fails.
	if h.g.Obj(ids[1]).Zone != state.ZLibrary {
		t.Fatalf("setup: matching Aura is not in the library: %s", h.g.Obj(ids[1]).Zone)
	}
	Resolve(h, &Ctx{Controller: 0}, ability)
	if h.g.Obj(ids[1]).Zone != state.ZHand {
		t.Fatalf("unsupported params prevented the core move: Aura zone = %s, want hand", h.g.Obj(ids[1]).Zone)
	}
	want := []string{"Amount$ X", "DigZone$ PlanarDeck"}
	var notes []string
	for _, ev := range h.log {
		if strings.HasPrefix(ev.Text, "DigUntil withholds ") {
			notes = append(notes, ev.Text)
		}
	}
	if len(notes) != len(want) {
		t.Fatalf("withheld Notes = %d, want one per unsupported parameter %v: %v", len(notes), want, notes)
	}
	for _, param := range want {
		found := false
		for _, note := range notes {
			if strings.Contains(note, "withholds "+param+";") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing withheld-param Note for %s: %v", param, notes)
		}
	}
}
