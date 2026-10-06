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
		// The implicit-source, optional-target and BlockAllDefined$ True
		// grammars are supported now; these neighbours must still fail loud.
		{"Defined": "Bogus"},
		{"ValidTgts": "Creature", "DefinedAttacker": "Bogus"},
		{"ValidTgts": "Creature", "DefinedAttacker": "TriggeredAttacker", "BlockAllDefined": "Maybe"},
		{"Choices": "Creature.DefenderCtrl", "Chooser": "TriggeredDefendingPlayer"},
		{"ValidTgts": "Player", "DefinedAttacker": "TriggeredAttacker", "Duration": "UntilEndOfCombat"},
		{"ValidTgts": "Creature.OppCtrl,Player", "DefinedAttacker": "TriggeredAttacker", "Duration": "UntilEndOfCombat"},
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
