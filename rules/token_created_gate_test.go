package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func hasAbilityAction(actions []decision.Option, obj state.ObjID, ability int) bool {
	for _, action := range actions {
		if action.Kind == "ability" && action.Obj == obj && action.Ability == ability {
			return true
		}
	}
	return false
}

// TestIdolOfOblivionTokenCreatedGateOpensThisTurn exercises the real corpus
// card's CheckSVar gate through the real TokenCreate path. The entry history
// makes the ability legal only for the turn in which the token was created.
func TestIdolOfOblivionTokenCreatedGateOpensThisTurn(t *testing.T) {
	t.Parallel()
	idolCard := choiceCorpusCard(t, "Idol of Oblivion")
	makerCard := cardByName(t, tokenForgeSrc("c_a_food_sac"))
	e, cfg := tokenReplGame(t, 940, idolCard, makerCard)
	idol := moveSeededCard(t, e, 0, idolCard, state.ZBattlefield)
	maker := moveSeededCard(t, e, 0, makerCard, state.ZBattlefield)
	if e.G.Obj(idol).Zone != state.ZBattlefield || e.G.Obj(maker).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Idol/maker not on battlefield: %v/%v", e.G.Obj(idol).Zone, e.G.Obj(maker).Zone)
	}

	addMana(t, e, 0, "")
	if hasAbilityAction(e.legalActions(0), idol, 0) {
		t.Fatal("Idol's draw ability was offered before a token was created")
	}

	activateTokenForge(t, e, maker)
	if len(e.G.Entered) == 0 {
		t.Fatal("precondition: token creation did not record a zone entry")
	}
	if !hasAbilityAction(e.legalActions(0), idol, 0) {
		t.Fatalf("Idol's draw ability was withheld after token creation: %+v", e.legalActions(0))
	}

	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	addMana(t, e, 0, "")
	if hasAbilityAction(e.legalActions(0), idol, 0) {
		t.Fatal("Idol's draw ability remained offered after TurnChange")
	}
	replayCheck(t, e, cfg)
}
