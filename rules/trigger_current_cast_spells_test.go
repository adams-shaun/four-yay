package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// TestTriggerObjectsCurrentCastSpellsSnapshotsAtTheTriggeringCast pins the
// TriggerObjectsCurrentCastSpells$Valid <spec> count head (Thousand-Year
// Storm's copy count, Sentinel Tower's damage) against the real event log:
// the count is the matching spells cast this turn up to AND INCLUDING the
// triggering spell's own cast, so a spell cast in response to the trigger is
// not counted, while every matching spell cast earlier in the turn is --
// whether or not the trigger's source was on the battlefield then. Before
// the head was read it evaluated to zero, so Thousand-Year Storm's trigger
// resolved making no copies at all.
func TestTriggerObjectsCurrentCastSpellsSnapshotsAtTheTriggeringCast(t *testing.T) {
	t.Parallel()
	instant := card(t, "Name:Quick\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 1\nOracle:x\n")
	creature := card(t, "Name:Body\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e := handEngine(t, instant, creature, instant, instant)
	hand := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
	first, body, second, response := hand[0], hand[1], hand[2], hand[3]
	e.G.Players[0].Pool[state.MU], e.G.Players[0].Pool[state.MG] = 3, 1

	castMode(t, e, first, "")
	e.resolveTop()
	castMode(t, e, body, "")
	e.resolveTop()
	castMode(t, e, second, "")
	// Cast in response, while the second instant is still on the stack.
	castMode(t, e, response, "")
	for _, id := range []state.ObjID{first, body, second, response} {
		if !castThisTurn(e, id) {
			t.Fatalf("precondition: object %d was not cast this turn", id)
		}
	}

	const storm = "TriggerObjectsCurrentCastSpells$Valid Sorcery.YouCtrl,Instant.YouCtrl/Minus.1"
	const tower = "TriggerObjectsCurrentCastSpells$Valid Sorcery,Instant"
	for _, tc := range []struct {
		name    string
		trigger state.ObjID
		expr    string
		want    int32
	}{
		{"storm on the first instant copies nothing", first, storm, 0},
		{"storm on the second instant ignores the creature and the response", second, storm, 1},
		{"storm on the response counts both earlier instants", response, storm, 2},
		{"tower on the second instant is inclusive", second, tower, 2},
		{"no trigger binding counts every match this turn", 0, tower, 3},
	} {
		c := &effects.Ctx{Controller: 0, TriggerContext: effects.TriggerContext{TriggerCard: tc.trigger}}
		if got, ok := effects.EvalCountOK(e, c, tc.expr); !ok || got != tc.want {
			t.Errorf("%s: EvalCountOK = (%d, %v), want (%d, true)", tc.name, got, ok, tc.want)
		}
	}
	if _, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0}, "TriggerObjectsCurrentCastSpells$Amount"); ok {
		t.Error("a body without the Valid argument must stay unevaluated")
	}
}

func castThisTurn(e *Engine, id state.ObjID) bool {
	for _, got := range e.EachSpellCastThisTurnMatching(0, "Card", 0) {
		if got == id {
			return true
		}
	}
	return false
}
