package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestDigDefaultsChangeNumToOne pins Forge's absent-ChangeNum default: a
// two-card Dig window may move only one card, even when both match.
func TestDigDefaultsChangeNumToOne(t *testing.T) {
	h, src, ids := riderBoard(t, riderBear, riderBear)
	sa := &cards.SA{API: "Dig", Params: map[string]string{
		"Defined": "You", "DigNum": "2", "DestinationZone": "Exile",
	}}
	effDig(h, &Ctx{Source: src, Controller: 0}, sa)
	moved := 0
	for _, id := range ids {
		if o := h.g.Obj(id); o != nil && o.Zone == state.ZExile {
			moved++
		}
	}
	if moved != 1 {
		t.Fatalf("moved %d cards from a two-card Dig window, want default ChangeNum 1; log=%+v", moved, h.log)
	}
}
