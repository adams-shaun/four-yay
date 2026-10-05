package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestMustBlockUnimplementedSelectorFailsLoud(t *testing.T) {
	if !Supported()["api:MustBlock"] {
		t.Fatal("precondition: API not registered")
	}
	h := &fakeHost{g: state.NewGame([]string{"A", "B"})}
	for _, params := range []map[string]string{
		{"ValidTgts": "Creature"},
		{"ValidTgts": "Creature", "DefinedAttacker": "TriggeredAttacker", "TargetMin": "0"},
		{"ValidTgts": "Creature", "DefinedAttacker": "TriggeredAttacker", "BlockAllDefined": "True"},
	} {
		sa := &cards.SA{API: "MustBlock", Params: params}
		if cards.MustBlockNamedTargetShape(sa) {
			t.Fatalf("precondition: unsupported shape accepted: %v", params)
		}
		before := len(h.log)
		Resolve(h, &Ctx{}, sa)
		if len(h.log) != before+1 || h.log[before].Kind != events.Note || !strings.Contains(h.log[before].Text, "unimplemented") || len(h.continuous) != 0 {
			t.Fatalf("unsupported MustBlock %v: log=%v, continuous=%v", params, h.log[before:], h.continuous)
		}
	}
}
